"""
generate_dpo_data.py —— DPO 偏好数据构造（方向二主线）

思路：
    1. 用 SFT 后的 NPC 模型，对同一 prompt 生成多个 (temperature 不同) 候选回答
    2. 用 Judge LLM（Kimi-K2 / GPT-4o）对候选打分，构造 chosen vs rejected
    3. 也可采用 rule-based（角色关键词命中 / 情绪一致性）补充信号

输出格式（ShareGPT pairwise）：
    {
      "conversations": [{"from":"human","value":"..."}],
      "chosen":   {"from":"gpt","value":"符合角色设定的回答"},
      "rejected": {"from":"gpt","value":"偏离角色的回答"},
      "system":   "NPC system prompt"
    }

使用：
    python scripts/generate_dpo_data.py \\
        --sft_model ./output/npc_sft_merged \\
        --prompts data/processed/npc_dialogues.json \\
        --output data/processed/npc_dpo.json \\
        --n_candidates 4
"""
from __future__ import annotations

import argparse


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--sft_model", type=str, required=True)
    parser.add_argument("--prompts", type=str, required=True)
    parser.add_argument("--output", type=str, required=True)
    parser.add_argument("--n_candidates", type=int, default=4)
    parser.add_argument("--judge_provider", type=str, default="moonshot")
    args = parser.parse_args()

    # TODO(phase-3)：
    # 1. vLLM 加载 sft_model 进行多温度采样
    # 2. Judge 模型打分
    # 3. 构造 chosen/rejected 对
    raise NotImplementedError("TODO(phase-3)：DPO 数据合成")


if __name__ == "__main__":
    main()
