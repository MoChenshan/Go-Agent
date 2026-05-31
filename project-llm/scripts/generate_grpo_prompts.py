"""
generate_grpo_prompts.py —— GRPO Prompts 数据集构造（方向二 · 对比实验）

GRPO 无需 pairwise 数据，只需：
    1. 一批 prompt（触发剧情推理 / 复杂决策）
    2. 可组合的 reward 函数（见 grpo_rewards.py）

本脚本输出 alpaca 格式的 prompt-only 数据集：
    [
      {"instruction": "玩家说...", "input": "NPC 设定：...", "expected_keywords": ["..."]},
      ...
    ]

使用：
    python scripts/generate_grpo_prompts.py \\
        --profiles data/raw/npc_profiles/ \\
        --output data/processed/npc_grpo_prompts.json \\
        --n 500
"""
from __future__ import annotations

import argparse


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--profiles", type=str, required=True)
    parser.add_argument("--output", type=str, required=True)
    parser.add_argument("--n", type=int, default=500)
    args = parser.parse_args()

    # TODO(phase-3)：
    # 1. 每个 NPC 设计 3~5 个需要推理的剧情场景
    # 2. 标注 expected_keywords（用于 scenario_coherence_reward）
    raise NotImplementedError("TODO(phase-3)：GRPO prompts 构造")


if __name__ == "__main__":
    main()
