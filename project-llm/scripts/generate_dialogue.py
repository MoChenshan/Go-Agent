"""
generate_dialogue.py —— 游戏 AINPC 多轮对话合成（方向二）

主流方案：Kimi-K2-0905 / DeepSeek-V3.2 多角色多场景生成
输入：data/raw/npc_profiles.json（角色卡）、data/raw/world_setting.md（世界观）
输出：data/processed/npc_sft.json（ShareGPT 格式，含 system prompt）

使用：
    python scripts/generate_dialogue.py \\
        --profiles data/raw/npc_profiles.json \\
        --world data/raw/world_setting.md \\
        --output data/processed/npc_sft.json \\
        --provider moonshot \\
        --n_per_pair 2 \\
        --include_control 1 \\
        --include_thinking 0

环境变量：
    MOONSHOT_API_KEY / MOONSHOT_BASE_URL / MOONSHOT_MODEL
    或 DEEPSEEK_API_KEY / DEEPSEEK_BASE_URL / DEEPSEEK_MODEL
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import time
from pathlib import Path

from dotenv import load_dotenv

load_dotenv()


# =========================================================================
# 场景模板
# =========================================================================
SCENARIOS_BASIC = [
    ("greet", "玩家第一次遇到该 NPC，互相问候"),
    ("quest_give", "NPC 向玩家发布一个与其专业知识相关的任务"),
    ("quest_progress", "玩家询问任务进度，NPC 给出线索/鼓励"),
    ("quest_complete", "玩家完成任务，NPC 给予奖励与感谢"),
    ("trade", "玩家与 NPC 进行交易对话"),
    ("lore", "玩家询问世界观/背景故事相关问题"),
    ("idle_chat", "无目的闲聊，拉近玩家与 NPC 距离"),
    ("farewell", "告别，留下伏笔或祝福"),
]

SCENARIOS_EMOTION = [
    ("emotion_angry", "NPC 处于愤怒情绪，玩家来搭话"),
    ("emotion_happy", "NPC 处于高兴情绪，玩家来搭话"),
    ("emotion_sad", "NPC 处于悲伤情绪，玩家来搭话"),
]

# 操作指令场景（进阶，训练 NPC 会调用"技能"）
SCENARIOS_CONTROL = [
    ("action_trade", "玩家想购买装备，NPC 应输出 [TRADE:xxx] 指令"),
    ("action_give", "玩家任务完成，NPC 应输出 [GIVE_ITEM:xxx] 指令"),
    ("action_quest", "NPC 发布任务时应输出 [START_QUEST:xxx] 指令"),
]

# Thinking 剧情场景（高阶，训练 <think>...</think> 能力）
SCENARIOS_THINKING = [
    ("thinking_dilemma", "玩家提出两难选择，NPC 需先思考再回答"),
    ("thinking_deduce", "玩家带来线索，NPC 需推理出幕后真相"),
    ("thinking_judge", "NPC 需判断玩家所言真假再决定下一步"),
]


# =========================================================================
# Prompt
# =========================================================================
GENERATE_DIALOGUE_PROMPT = """你是一位资深游戏文案策划。请根据以下 NPC 角色设定，生成该角色在指定场景下与玩家的一段完整多轮对话。

# NPC 角色卡
- 名字：{name}
- 身份：{identity}
- 性格：{personality}
- 说话风格：{speaking_style}
- 背景：{background}
- 专业知识：{knowledge}

# 世界观
{world_setting}

# 场景
- 场景标识：{scenario}
- 场景描述：{scenario_desc}
{extra_instructions}

要求：
1. 对话必须完全符合角色设定与说话风格，避免"AI 味"
2. 体现角色的专业领域知识
3. 生成 3-5 轮对话（玩家先说，NPC 回复，交替进行）
4. 玩家说话要简短自然，NPC 回复要有代入感
5. **只输出 JSON 数组**，格式：
[
  {{"from": "player", "value": "..."}},
  {{"from": "npc", "value": "..."}},
  {{"from": "player", "value": "..."}},
  {{"from": "npc", "value": "..."}}
]
"""

EXTRA_CONTROL = """

# 操作指令要求（重要）
NPC 在合适的时机**必须**输出以下操作指令中的一个（作为回复的最后一行）：
- [GIVE_ITEM:物品名]     # 给玩家物品
- [START_QUEST:任务名]   # 发布任务
- [TRADE:商品类别]        # 打开交易
- [END_QUEST:任务名]     # 结束任务
"""

EXTRA_THINKING = """

# Thinking Mode 要求（重要）
NPC 的**每一次**回复都必须先产出思考链，再给出最终回答，格式：
<think>
（内心推理过程，2-3 句话）
</think>
（说给玩家听的回答）
"""


# =========================================================================
# 客户端
# =========================================================================
def build_client(provider: str):
    from openai import OpenAI

    provider = provider.lower()
    if provider == "moonshot":
        api_key = os.getenv("MOONSHOT_API_KEY")
        if not api_key:
            raise RuntimeError("MOONSHOT_API_KEY 未配置")
        return OpenAI(
            api_key=api_key,
            base_url=os.getenv("MOONSHOT_BASE_URL", "https://api.moonshot.cn/v1"),
        ), os.getenv("MOONSHOT_MODEL", "kimi-k2-0905-preview")
    if provider == "deepseek":
        api_key = os.getenv("DEEPSEEK_API_KEY")
        if not api_key:
            raise RuntimeError("DEEPSEEK_API_KEY 未配置")
        return OpenAI(
            api_key=api_key,
            base_url=os.getenv("DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1"),
        ), os.getenv("DEEPSEEK_MODEL", "deepseek-chat")
    return OpenAI(
        api_key=os.getenv("OPENAI_API_KEY"),
        base_url=os.getenv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
    ), os.getenv("OPENAI_JUDGE_MODEL", "gpt-4o")


# =========================================================================
# JSON 解析
# =========================================================================
def _parse_turns(text: str) -> list[dict]:
    text = text.strip()
    if text.startswith("```"):
        text = text.split("\n", 1)[1] if "\n" in text else text
        text = text.rsplit("```", 1)[0].strip()
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        s, e = text.find("["), text.rfind("]") + 1
        if s >= 0 and e > s:
            try:
                data = json.loads(text[s:e])
            except json.JSONDecodeError:
                return []
        else:
            return []
    if isinstance(data, dict):
        for k in ("turns", "dialogue", "conversations", "data"):
            if k in data and isinstance(data[k], list):
                data = data[k]
                break
    if not isinstance(data, list):
        return []
    # 校验
    turns = []
    for t in data:
        if not isinstance(t, dict):
            continue
        who = (t.get("from") or t.get("role") or "").lower()
        val = (t.get("value") or t.get("content") or "").strip()
        if not val:
            continue
        if who in ("player", "user", "human"):
            who = "player"
        elif who in ("npc", "assistant", "gpt", "bot"):
            who = "npc"
        else:
            continue
        turns.append({"from": who, "value": val})
    return turns


# =========================================================================
# 生成与格式化
# =========================================================================
def generate_dialogue(
    client, model: str, npc: dict, scenario: tuple, world: str, extra: str = "",
    retry: int = 2
) -> list[dict]:
    prompt = GENERATE_DIALOGUE_PROMPT.format(
        name=npc["name"],
        identity=npc["identity"],
        personality=npc["personality"],
        speaking_style=npc["speaking_style"],
        background=npc["background"],
        knowledge="、".join(npc.get("knowledge", [])),
        scenario=scenario[0],
        scenario_desc=scenario[1],
        world_setting=world,
        extra_instructions=extra,
    )
    for attempt in range(retry + 1):
        try:
            resp = client.chat.completions.create(
                model=model,
                messages=[{"role": "user", "content": prompt}],
                temperature=0.9,
                response_format={"type": "json_object"},
            )
            turns = _parse_turns(resp.choices[0].message.content or "")
            if len(turns) >= 2:
                return turns
        except Exception as e:  # noqa: BLE001
            print(f"  [warn] {npc['name']}/{scenario[0]} 第{attempt+1}次失败：{e}", file=sys.stderr)
            time.sleep(1.5 * (attempt + 1))
    return []


def build_system_prompt(npc: dict, extra_state: str | None = None,
                        enable_thinking: bool = False) -> str:
    lines = [
        f"你是游戏中的NPC「{npc['name']}」。",
        f"身份：{npc['identity']}",
        f"性格：{npc['personality']}",
        f"说话风格：{npc['speaking_style']}",
        f"背景：{npc['background']}",
        f"专业领域：{'、'.join(npc.get('knowledge', []))}",
        "请始终以该角色的身份和风格回复玩家。",
    ]
    if extra_state:
        lines.append(extra_state)
    if enable_thinking:
        lines.append("每次回复前先在 <think>...</think> 中思考，再给出回答。")
    return "\n".join(lines)


def format_for_sharegpt(npc: dict, turns: list[dict],
                        extra_state: str | None = None,
                        enable_thinking: bool = False) -> dict | None:
    """转成 ShareGPT 格式：{conversations:[{from:system/human/gpt,value:...}]}"""
    if not turns:
        return None
    # 必须以 player 开头
    if turns[0]["from"] != "player":
        turns = turns[1:]
    if not turns:
        return None
    convs = [{"from": "system",
              "value": build_system_prompt(npc, extra_state, enable_thinking)}]
    for t in turns:
        role = "human" if t["from"] == "player" else "gpt"
        convs.append({"from": role, "value": t["value"]})
    # 至少 system + human + gpt
    if len(convs) < 3:
        return None
    return {"conversations": convs}


# =========================================================================
# 主流程
# =========================================================================
def main():
    parser = argparse.ArgumentParser(description="NPC 多轮对话合成")
    parser.add_argument("--profiles", type=str, required=True,
                        help="角色卡 JSON 文件（List[dict]）")
    parser.add_argument("--world", type=str, required=True, help="世界观 Markdown")
    parser.add_argument("--output", type=str, required=True)
    parser.add_argument("--provider", type=str, default="moonshot",
                        choices=["moonshot", "deepseek", "openai"])
    parser.add_argument("--n_per_pair", type=int, default=2,
                        help="每个 (NPC, 场景) 生成多少条对话")
    parser.add_argument("--include_control", type=int, default=1,
                        help="是否加入操作指令场景（进阶）")
    parser.add_argument("--include_thinking", type=int, default=0,
                        help="是否加入 thinking 剧情场景（高阶）")
    parser.add_argument("--include_emotion", type=int, default=1)
    parser.add_argument("--max_pairs", type=int, default=0,
                        help="0=不限；>0 用于 smoke test")
    args = parser.parse_args()

    npc_list = json.loads(Path(args.profiles).read_text(encoding="utf-8"))
    world_setting = Path(args.world).read_text(encoding="utf-8")
    out_path = Path(args.output)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    scenarios = list(SCENARIOS_BASIC)
    if args.include_emotion:
        scenarios += SCENARIOS_EMOTION
    control_scenarios = list(SCENARIOS_CONTROL) if args.include_control else []
    thinking_scenarios = list(SCENARIOS_THINKING) if args.include_thinking else []

    client, model = build_client(args.provider)
    print(f"[dialogue] provider={args.provider} model={model}")
    print(f"[dialogue] npc={len(npc_list)} scenarios={len(scenarios)} "
          f"control={len(control_scenarios)} thinking={len(thinking_scenarios)}")

    all_samples: list[dict] = []
    pair_budget = args.max_pairs or 10 ** 9
    produced = 0

    # 主循环：基础 + 情绪场景
    for npc in npc_list:
        for scn in scenarios:
            if produced >= pair_budget:
                break
            for k in range(args.n_per_pair):
                turns = generate_dialogue(client, model, npc, scn, world_setting)
                # 情绪类场景，把情绪写入 system
                extra_state = None
                if scn[0].startswith("emotion_"):
                    mood = scn[0].split("_", 1)[1]
                    extra_state = f"当前情绪：[{mood}]"
                sample = format_for_sharegpt(npc, turns, extra_state=extra_state)
                if sample:
                    sample["_meta"] = {"npc": npc["name"], "scenario": scn[0], "idx": k}
                    all_samples.append(sample)
                produced += 1
                print(f"  [{produced}] {npc['name']} / {scn[0]} #{k}  "
                      f"turns={len(turns)}  ok={sample is not None}")
        if produced >= pair_budget:
            break

    # 操作指令场景
    for npc in npc_list:
        for scn in control_scenarios:
            if produced >= pair_budget:
                break
            turns = generate_dialogue(client, model, npc, scn, world_setting,
                                      extra=EXTRA_CONTROL)
            extra_state = (
                "你可以执行以下操作：\n"
                "[GIVE_ITEM:xxx] 给玩家物品\n"
                "[START_QUEST:xxx] 发布任务\n"
                "[TRADE:xxx] 打开交易\n"
                "[END_QUEST:xxx] 结束任务"
            )
            sample = format_for_sharegpt(npc, turns, extra_state=extra_state)
            if sample:
                sample["_meta"] = {"npc": npc["name"], "scenario": scn[0], "control": True}
                all_samples.append(sample)
            produced += 1
            print(f"  [{produced}] {npc['name']} / {scn[0]}  turns={len(turns)}  ok={sample is not None}")

    # Thinking 剧情场景
    for npc in npc_list:
        for scn in thinking_scenarios:
            if produced >= pair_budget:
                break
            turns = generate_dialogue(client, model, npc, scn, world_setting,
                                      extra=EXTRA_THINKING)
            sample = format_for_sharegpt(npc, turns, enable_thinking=True)
            if sample:
                sample["_meta"] = {"npc": npc["name"], "scenario": scn[0], "thinking": True}
                all_samples.append(sample)
            produced += 1
            print(f"  [{produced}] {npc['name']} / {scn[0]} [thinking]  "
                  f"turns={len(turns)}  ok={sample is not None}")

    out_path.write_text(json.dumps(all_samples, ensure_ascii=False, indent=2),
                        encoding="utf-8")
    print(f"\n[done] 共 {len(all_samples)} 条 ShareGPT 样本 → {out_path}")


if __name__ == "__main__":
    main()
