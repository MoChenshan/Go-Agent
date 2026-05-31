"""
grpo_rewards.py —— GRPO 训练自定义奖励函数（对标方案 3.4bis）

设计原则（参考 DeepSeek-R1 / DeepSeekMath rule-based reward）：
    1) 可组合：每个 reward 函数独立返回 list[float]，GRPO 训练器自动加权平均
    2) 可验证：优先用规则 reward（format / scenario_keyword），LLM reward 作为补充
    3) 接口统一：(completions, prompts=None, **kwargs) -> list[float]
       其中 **kwargs 会收到 dataset 中的所有额外列（如 npc_profiles / scenario_expected）

LLaMAFactory 0.9+ / TRL 0.12+ 都支持通过注册此文件的函数名使用：
    reward_funcs:
      - role_consistency_reward
      - scenario_coherence_reward
      - format_reward
      - length_penalty_reward
"""
from __future__ import annotations

import json
import os
import re
from typing import Sequence


# =========================================================================
# 1. 格式奖励：是否产出 <think>...</think>  ⭐ 最有效的 rule-based reward
# =========================================================================
_THINK_PATTERN = re.compile(r"<think>(.*?)</think>", re.DOTALL)


def format_reward(completions: Sequence[str], **kwargs) -> list[float]:
    """格式奖励：thinking 场景必须产出 <think>...</think> 后再回答。
    1.0 = 有 think 且后续有 answer；0.5 = 只有 think；0 = 无 think。"""
    rewards: list[float] = []
    for c in completions:
        m = _THINK_PATTERN.search(c or "")
        if not m:
            rewards.append(0.0)
            continue
        think_body = m.group(1).strip()
        rest = (c[: m.start()] + c[m.end():]).strip()
        if len(think_body) >= 10 and len(rest) >= 5:
            rewards.append(1.0)
        elif len(think_body) >= 10:
            rewards.append(0.5)
        else:
            rewards.append(0.2)
    return rewards


# =========================================================================
# 2. 剧情关键词覆盖奖励：rule-based、可验证
# =========================================================================
def scenario_coherence_reward(
    completions: Sequence[str],
    scenario_expected: Sequence[Sequence[str]] | None = None,
    **kwargs,
) -> list[float]:
    """剧情走通奖励：NPC 回复是否触发了期望的关键词集合。
    scenario_expected：与 completions 等长的关键词列表（每项是一组关键词）。"""
    if not scenario_expected:
        return [0.0] * len(completions)
    rewards: list[float] = []
    for c, expected in zip(completions, scenario_expected):
        if not expected:
            rewards.append(0.0)
            continue
        hit = sum(1 for kw in expected if kw in (c or ""))
        rewards.append(hit / max(len(expected), 1))
    return rewards


# =========================================================================
# 3. 操作指令合规奖励：NPC 输出 [ACTION:xxx] 指令是否正确
# =========================================================================
_ACTION_PATTERN = re.compile(r"\[(GIVE_ITEM|START_QUEST|TRADE|END_QUEST):([^\]]+)\]")


def action_format_reward(
    completions: Sequence[str],
    expected_action: Sequence[str] | None = None,
    **kwargs,
) -> list[float]:
    """操作指令奖励：
    - 未要求指令 + 未输出指令      → 1.0
    - 未要求指令 + 乱输出指令       → 0.0（幻觉）
    - 要求指令 + 输出对应指令       → 1.0
    - 要求指令 + 输出其他指令       → 0.3
    - 要求指令 + 未输出指令         → 0.0"""
    if expected_action is None:
        expected_action = [""] * len(completions)
    rewards: list[float] = []
    for c, exp in zip(completions, expected_action):
        actions = _ACTION_PATTERN.findall(c or "")
        if not exp:
            rewards.append(1.0 if not actions else 0.0)
            continue
        if not actions:
            rewards.append(0.0)
            continue
        if any(a[0] == exp for a in actions):
            rewards.append(1.0)
        else:
            rewards.append(0.3)
    return rewards


# =========================================================================
# 4. 长度惩罚：避免灌水 / 过短
# =========================================================================
def length_penalty_reward(
    completions: Sequence[str],
    min_len: int = 20,
    max_len: int = 500,
    **kwargs,
) -> list[float]:
    """长度合规奖励：[min_len, max_len] 区间内线性过渡，超出则降分。"""
    rewards: list[float] = []
    sweet_min = min_len + 20
    sweet_max = max_len - 100
    for c in completions:
        L = len(c or "")
        if L < min_len or L > max_len:
            rewards.append(0.0)
        elif sweet_min <= L <= sweet_max:
            rewards.append(1.0)
        elif L < sweet_min:
            rewards.append((L - min_len) / (sweet_min - min_len))
        else:
            rewards.append((max_len - L) / (max_len - sweet_max))
    return rewards


# =========================================================================
# 5. 角色一致性奖励（LLM Judge，重量级，占训练中的 ~40% API cost）
# =========================================================================
_JUDGE_CLIENT = None
_JUDGE_MODEL = None


def _lazy_init_judge():
    """延迟初始化 Judge 客户端。优先用 Kimi-K2，回退 GPT-4o-mini。"""
    global _JUDGE_CLIENT, _JUDGE_MODEL
    if _JUDGE_CLIENT is not None:
        return
    try:
        from openai import OpenAI
    except ImportError:
        return
    if os.getenv("MOONSHOT_API_KEY"):
        _JUDGE_CLIENT = OpenAI(
            api_key=os.getenv("MOONSHOT_API_KEY"),
            base_url=os.getenv("MOONSHOT_BASE_URL", "https://api.moonshot.cn/v1"),
        )
        _JUDGE_MODEL = os.getenv("MOONSHOT_MODEL", "moonshot-v1-32k")
    elif os.getenv("OPENAI_API_KEY"):
        _JUDGE_CLIENT = OpenAI(
            api_key=os.getenv("OPENAI_API_KEY"),
            base_url=os.getenv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
        )
        _JUDGE_MODEL = os.getenv("OPENAI_JUDGE_MODEL", "gpt-4o-mini")


_ROLE_JUDGE_PROMPT = """你是游戏 NPC 质量评审。请给以下 NPC 回复按"角色一致性"打 0-1 分（保留一位小数）。

评判要点：
- 是否符合角色的性格/说话风格
- 是否用词得体，无 AI 腔
- 是否体现角色专业知识

# NPC 设定
{npc}

# 玩家
{prompt}

# NPC 回复
{completion}

只输出 JSON：{{"score": 0.0 到 1.0 的浮点数}}"""


def role_consistency_reward(
    completions: Sequence[str],
    prompts: Sequence[str] | None = None,
    npc_profiles: Sequence[dict] | None = None,
    **kwargs,
) -> list[float]:
    """角色一致性（LLM Judge）。若没有 Judge client 或缺失数据，返回 0。"""
    _lazy_init_judge()
    if _JUDGE_CLIENT is None:
        return [0.0] * len(completions)
    if prompts is None:
        prompts = [""] * len(completions)
    if npc_profiles is None:
        npc_profiles = [{}] * len(completions)

    rewards: list[float] = []
    for c, p, npc in zip(completions, prompts, npc_profiles):
        try:
            npc_str = json.dumps(npc, ensure_ascii=False) if isinstance(npc, dict) else str(npc)
            resp = _JUDGE_CLIENT.chat.completions.create(
                model=_JUDGE_MODEL,
                messages=[{"role": "user",
                           "content": _ROLE_JUDGE_PROMPT.format(
                               npc=npc_str, prompt=p, completion=c or "")}],
                temperature=0.0,
                response_format={"type": "json_object"},
            )
            data = json.loads(resp.choices[0].message.content or "{}")
            rewards.append(max(0.0, min(1.0, float(data.get("score", 0.0)))))
        except Exception:  # noqa: BLE001
            rewards.append(0.0)
    return rewards


# =========================================================================
# 组合器：便于独立验证
# =========================================================================
ALL_REWARDS = {
    "format_reward": format_reward,
    "scenario_coherence_reward": scenario_coherence_reward,
    "action_format_reward": action_format_reward,
    "length_penalty_reward": length_penalty_reward,
    "role_consistency_reward": role_consistency_reward,
}


def combined_reward(completions: Sequence[str], weights: dict | None = None,
                    **kwargs) -> list[float]:
    """把多个 reward 加权求和成单个 reward（用于不支持多 reward_funcs 的后端）。"""
    default_w = {
        "format_reward": 0.3,
        "length_penalty_reward": 0.1,
        "action_format_reward": 0.2,
        "scenario_coherence_reward": 0.2,
        "role_consistency_reward": 0.2,
    }
    w = weights or default_w
    totals = [0.0] * len(completions)
    for name, weight in w.items():
        fn = ALL_REWARDS.get(name)
        if fn is None or weight <= 0:
            continue
        vals = fn(completions, **kwargs)
        for i, v in enumerate(vals):
            totals[i] += weight * v
    return totals


# =========================================================================
# CLI：独立测试 reward 函数（不进训练也能跑）
# =========================================================================
if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description="离线测试 reward 函数")
    parser.add_argument("--completion", type=str, required=True)
    parser.add_argument("--prompt", type=str, default="")
    parser.add_argument("--expected_action", type=str, default="")
    args = parser.parse_args()

    c = [args.completion]
    print("== format_reward       :", format_reward(c))
    print("== length_penalty      :", length_penalty_reward(c))
    print("== action_format       :", action_format_reward(c, expected_action=[args.expected_action]))
    print("== scenario_coherence  :", scenario_coherence_reward(c, scenario_expected=[["月光草", "采摘"]]))
    print("== combined            :",
          combined_reward(c, prompts=[args.prompt], expected_action=[args.expected_action],
                          scenario_expected=[["月光草"]]))