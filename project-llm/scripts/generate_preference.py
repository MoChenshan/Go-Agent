"""
generate_preference.py —— DPO 偏好对（chosen / rejected）构造

流程（对标方案 3.4.1）：
    输入：SFT 数据（ShareGPT）或 prompts 列表
    1) 对同一 prompt 用不同 temperature 生成两个回复 A / B（同一模型，不同采样）
       或用"强模型 vs 弱模型"构造偏好对（可选）
    2) LLM-as-Judge 异源评审（Kimi-K2 或 GPT-4o），输出 {"chosen":"A"|"B","reason":...}
    3) 输出 DPO 标准格式：{prompt, chosen, rejected}

输出格式（LLaMAFactory pairwise 模板）：
    {
      "conversations": [{"from":"human","value":"..."}],
      "chosen":   {"from":"gpt","value":"..."},
      "rejected": {"from":"gpt","value":"..."},
      "system":   "你是游戏中的NPC..."
    }

使用：
    python scripts/generate_preference.py \\
        --sft_data data/processed/npc_sft.json \\
        --output data/processed/npc_dpo.json \\
        --gen_provider moonshot \\
        --judge_provider openai \\
        --max_samples 200
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

# 复用 build_client
sys.path.insert(0, str(Path(__file__).parent))
from generate_qa import build_client  # noqa: E402


# =========================================================================
# Prompt
# =========================================================================
DPO_JUDGE_PROMPT = """你是游戏对话质量评审专家。请对比以下两个 NPC 回复，选出更好的一个。

评判标准（按优先级）：
1. 角色一致性（是否完全符合角色设定与说话风格）
2. 对话趣味性（是否有代入感，不是 AI 腔）
3. 世界观一致性（用词/背景是否符合游戏设定）
4. 玩家体验（是否让玩家想继续互动）

# NPC 角色系统提示
{system}

# 对话历史
{history}

# 玩家说
{player_input}

# 回复 A
{response_a}

# 回复 B
{response_b}

只输出 JSON：{{"chosen":"A"或"B","reason":"简短理由"}}
"""


# =========================================================================
# ShareGPT → prompt 列表提取
# =========================================================================
def extract_prompts_from_sft(sft_data: list[dict]) -> list[dict]:
    """从 SFT ShareGPT 样本中抽取 (system, history, last_user_turn)。
    每个样本最后一条 human 作为 DPO 的 prompt。"""
    prompts: list[dict] = []
    for sample in sft_data:
        convs = sample.get("conversations", [])
        if not convs:
            continue
        system = ""
        history: list[dict] = []
        last_human_idx = -1
        for i, c in enumerate(convs):
            if c.get("from") == "system":
                system = c.get("value", "")
            elif c.get("from") == "human":
                last_human_idx = i
        if last_human_idx < 0:
            continue
        # 截取 history = system 之后, last_human_idx 之前的部分
        for c in convs[:last_human_idx]:
            if c.get("from") in ("human", "gpt"):
                history.append(c)
        player_input = convs[last_human_idx].get("value", "")
        if not player_input:
            continue
        prompts.append({
            "system": system,
            "history": history,
            "player_input": player_input,
            "_meta": sample.get("_meta", {}),
        })
    return prompts


def format_history(history: list[dict]) -> str:
    if not history:
        return "（无历史对话）"
    lines = []
    for turn in history:
        tag = "玩家" if turn.get("from") == "human" else "NPC"
        lines.append(f"{tag}: {turn.get('value', '')}")
    return "\n".join(lines)


# =========================================================================
# 回复生成 & Judge
# =========================================================================
def build_messages(system: str, history: list[dict], player_input: str) -> list[dict]:
    msgs: list[dict] = []
    if system:
        msgs.append({"role": "system", "content": system})
    for turn in history:
        role = "user" if turn.get("from") == "human" else "assistant"
        msgs.append({"role": role, "content": turn.get("value", "")})
    msgs.append({"role": "user", "content": player_input})
    return msgs


def gen_response(client, model: str, msgs: list[dict], temperature: float,
                 max_tokens: int = 512, retry: int = 2) -> str:
    for attempt in range(retry + 1):
        try:
            resp = client.chat.completions.create(
                model=model,
                messages=msgs,
                temperature=temperature,
                max_tokens=max_tokens,
            )
            return (resp.choices[0].message.content or "").strip()
        except Exception as e:  # noqa: BLE001
            if attempt == retry:
                print(f"  [warn] gen 失败：{e}", file=sys.stderr)
                return ""
            time.sleep(1.0 * (attempt + 1))
    return ""


def judge_pair(client, model: str, ctx: dict, resp_a: str, resp_b: str,
               retry: int = 2) -> dict | None:
    prompt = DPO_JUDGE_PROMPT.format(
        system=ctx["system"],
        history=format_history(ctx["history"]),
        player_input=ctx["player_input"],
        response_a=resp_a,
        response_b=resp_b,
    )
    for attempt in range(retry + 1):
        try:
            resp = client.chat.completions.create(
                model=model,
                messages=[{"role": "user", "content": prompt}],
                temperature=0.0,
                response_format={"type": "json_object"},
            )
            data = json.loads(resp.choices[0].message.content or "{}")
            choice = str(data.get("chosen", "")).strip().upper()
            if choice in ("A", "B"):
                return {"chosen": choice, "reason": data.get("reason", "")}
        except Exception as e:  # noqa: BLE001
            if attempt == retry:
                print(f"  [warn] judge 失败：{e}", file=sys.stderr)
                return None
            time.sleep(1.0 * (attempt + 1))
    return None


# =========================================================================
# 主流程
# =========================================================================
def build_dpo_record(ctx: dict, chosen_text: str, rejected_text: str,
                     reason: str = "") -> dict:
    """LLaMAFactory pairwise（ShareGPT）格式"""
    # 将 system + history + 当前 player 作为 conversations
    convs: list[dict] = []
    for turn in ctx["history"]:
        convs.append(turn)
    convs.append({"from": "human", "value": ctx["player_input"]})
    return {
        "system": ctx["system"],
        "conversations": convs,
        "chosen":   {"from": "gpt", "value": chosen_text},
        "rejected": {"from": "gpt", "value": rejected_text},
        "_judge_reason": reason,
        "_meta": ctx.get("_meta", {}),
    }


def main():
    parser = argparse.ArgumentParser(description="DPO 偏好对构造")
    parser.add_argument("--sft_data", type=str, required=True,
                        help="SFT ShareGPT 数据（从中抽取 prompt）")
    parser.add_argument("--output", type=str, required=True)

    parser.add_argument("--gen_provider", type=str, default="moonshot",
                        choices=["moonshot", "deepseek", "openai"])
    parser.add_argument("--judge_provider", type=str, default="openai",
                        choices=["openai", "moonshot", "deepseek"])

    parser.add_argument("--temp_a", type=float, default=0.3,
                        help="回复 A 的 temperature（偏稳）")
    parser.add_argument("--temp_b", type=float, default=1.1,
                        help="回复 B 的 temperature（偏随机，更容易生成较差回复）")
    parser.add_argument("--max_samples", type=int, default=0,
                        help="0=不限；>0 用于 smoke test")
    parser.add_argument("--min_answer_len", type=int, default=5)
    args = parser.parse_args()

    sft_data = json.loads(Path(args.sft_data).read_text(encoding="utf-8"))
    contexts = extract_prompts_from_sft(sft_data)
    if args.max_samples:
        contexts = contexts[: args.max_samples]
    print(f"[dpo] 从 {len(sft_data)} 条 SFT 样本抽取 {len(contexts)} 个 prompt")

    gen_client, gen_model = build_client(args.gen_provider)
    judge_client, judge_model = build_client(args.judge_provider)
    print(f"[dpo] gen={args.gen_provider}/{gen_model}  judge={args.judge_provider}/{judge_model}")

    records: list[dict] = []
    stats = {"total": len(contexts), "gen_fail": 0, "judge_fail": 0,
             "too_short": 0, "ties": 0, "ok": 0}

    for i, ctx in enumerate(contexts, 1):
        msgs = build_messages(ctx["system"], ctx["history"], ctx["player_input"])
        resp_a = gen_response(gen_client, gen_model, msgs, temperature=args.temp_a)
        resp_b = gen_response(gen_client, gen_model, msgs, temperature=args.temp_b)
        if not resp_a or not resp_b:
            stats["gen_fail"] += 1
            continue
        if len(resp_a) < args.min_answer_len or len(resp_b) < args.min_answer_len:
            stats["too_short"] += 1
            continue
        if resp_a.strip() == resp_b.strip():
            stats["ties"] += 1
            continue

        verdict = judge_pair(judge_client, judge_model, ctx, resp_a, resp_b)
        if verdict is None:
            stats["judge_fail"] += 1
            continue
        if verdict["chosen"] == "A":
            chosen, rejected = resp_a, resp_b
        else:
            chosen, rejected = resp_b, resp_a

        records.append(build_dpo_record(ctx, chosen, rejected, verdict.get("reason", "")))
        stats["ok"] += 1

        if i % 10 == 0 or i == len(contexts):
            print(f"  [{i}/{len(contexts)}] ok={stats['ok']}  fail={stats['gen_fail']+stats['judge_fail']}")

    out = Path(args.output)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(records, ensure_ascii=False, indent=2), encoding="utf-8")

    print("\n[stats]")
    for k, v in stats.items():
        print(f"  {k:12s}: {v}")
    print(f"[done] {len(records)} DPO pairs → {out}")


if __name__ == "__main__":
    main()
