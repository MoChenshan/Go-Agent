"""
generate_npc_data.py —— 游戏 NPC 对话数据合成（方向二：AINPC）

核心流程（阶段三实现）：
    1. 读取 data/raw/npc_profiles/*.json（角色卡：姓名、身份、性格、语气、世界观）
    2. 读取 data/raw/game_content/ 中的世界观/剧情素材
    3. 通过 Kimi-K2（128K 长上下文）为每个 NPC 生成 N 轮对话：
       - 日常对话
       - 情绪切换（EMOTION:sad / angry / happy）
       - 剧情分支（触发关键剧情关键词）
       - Thinking Mode 复杂推理对话（<think>...</think>）
    4. 输出 ShareGPT 格式（system + conversations [human/gpt]）

使用：
    python scripts/generate_npc_data.py \\
        --profiles data/raw/npc_profiles/ \\
        --world data/raw/game_content/ \\
        --output data/processed/npc_dialogues.json \\
        --per_npc 50
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path

from dotenv import load_dotenv

load_dotenv()


def load_npc_profile(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def build_system_prompt(profile: dict) -> str:
    """基于角色卡构造 System Prompt。TODO(phase-3)：完善 prompt 工程"""
    raise NotImplementedError("TODO(phase-3)：System prompt 构造")


def synthesize_dialogues(client, model: str, profile: dict, world: str, n: int) -> list[dict]:
    """调用 Kimi-K2 合成 n 段对话。TODO(phase-3)：Few-shot / 种子话题 / 情绪控制"""
    raise NotImplementedError("TODO(phase-3)：Kimi-K2 多角色对话合成")


def main():
    parser = argparse.ArgumentParser(description="游戏 NPC 对话数据合成")
    parser.add_argument("--profiles", type=str, required=True)
    parser.add_argument("--world", type=str, default="data/raw/game_content/")
    parser.add_argument("--output", type=str, required=True)
    parser.add_argument("--per_npc", type=int, default=50)
    args = parser.parse_args()

    # TODO(phase-3)：遍历 NPC + 合成 + 落盘
    raise NotImplementedError("TODO(phase-3)：NPC 对话合成主流程")


if __name__ == "__main__":
    main()
