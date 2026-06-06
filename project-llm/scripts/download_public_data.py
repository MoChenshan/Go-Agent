"""
download_public_data.py —— 下载公开角色扮演数据集，转换为项目 ShareGPT 格式

支持数据集：
    --dataset character   : 中文角色扮演对话（HuggingFace: zyx2233/character_chat）
    --dataset belle       : BelleGroup 中文指令（通用对话）
    --dataset firefly     : 萤火虫中文指令（含角色扮演）
    --dataset alpaca-zh   : 中文 Alpaca 指令

使用：
    python scripts/download_public_data.py --dataset character --output data/processed/npc_dialogues.json --max 2000
    python scripts/download_public_data.py --dataset character --output data/processed/npc_dialogues.json --max 500  # 快速验证
"""
from __future__ import annotations

import argparse
import json
import os
import random
from pathlib import Path

from dotenv import load_dotenv

load_dotenv()

# HuggingFace 镜像
os.environ.setdefault("HF_ENDPOINT", "https://hf-mirror.com")


# NPC 角色系统能力模板（给通用数据注入角色扮演风格）
NPC_SYSTEM_TEMPLATES = [
    "你是{role}，{desc}。请始终保持角色设定，用{style}的语气回答。",
    "你是{role}。{desc}。回答时请体现你的性格特点，使用{style}的表达方式。",
    "角色：{role}。背景：{desc}。请以{style}的风格进行对话。",
]

# 角色映射（将通用对话转化为角色扮演风格）
ROLE_PRESETS = [
    {"role": "铁匠老张", "desc": "镇上的铁匠，40岁退役老兵，豪爽直率", "style": "大嗓门、军队俚语"},
    {"role": "药师小月", "desc": "精灵族混血药师，温柔细心", "style": "轻声细语、花草比喻"},
    {"role": "酒馆老板娘玛莎", "desc": "前冒险者，精明爽朗", "style": "语速快、市井气"},
    {"role": "教书先生柳三元", "desc": "村口教书先生，迂腐认真", "style": "文绉绉、子曰诗云"},
    {"role": "镖局总镖头铁山", "desc": "身材魁梧，话糙理不糙", "style": "直来直去、江湖气"},
    {"role": "御医秦怀谨", "desc": "老御医，谨慎温和", "style": "慢条斯理、医学术语"},
    {"role": "说书人冯长舌", "desc": "爱卖关子的江湖说书人", "style": "抑扬顿挫、悬念感"},
    {"role": "扫地僧静禅", "desc": "少林扫地僧，平静慈悲", "style": "点到为止、禅意"},
]


def download_character_chat(max_samples: int = 0) -> list[dict]:
    """下载中文角色扮演对话数据集"""
    from datasets import load_dataset

    print("[download] 正在下载 zyx2233/character_chat ...")
    try:
        ds = load_dataset("zyx2233/character_chat", split="train", trust_remote_code=True)
    except Exception:
        print("[download] zyx2233/character_chat 下载失败，尝试备用数据集...")
        try:
            ds = load_dataset("BelleGroup/train_1M_CN", split="train", trust_remote_code=True)
        except Exception:
            print("[download] 备用数据集也失败，尝试 firefly...")
            ds = load_dataset("YeYKSS/Chat-RolePlay", split="train", trust_remote_code=True)

    items = []
    for row in ds:
        if max_samples and len(items) >= max_samples:
            break

        # 尝试适配不同数据集的字段名
        conversations = row.get("conversations") or row.get("dialog") or row.get("messages")

        if conversations:
            # 已经是对话格式
            convs = []
            system_text = ""
            for turn in conversations:
                role = turn.get("from", turn.get("role", ""))
                value = turn.get("value", turn.get("content", ""))
                if isinstance(role, str) and isinstance(value, str):
                    if role in ("system",):
                        system_text = value
                    elif role in ("human", "user"):
                        convs.append({"from": "human", "value": value})
                    elif role in ("gpt", "assistant"):
                        convs.append({"from": "gpt", "value": value})

            if len(convs) >= 2:  # 至少一问一答
                item = {"conversations": convs}
                if system_text:
                    item["system"] = system_text
                else:
                    # 注入随机角色设定
                    role_preset = random.choice(ROLE_PRESETS)
                    tpl = random.choice(NPC_SYSTEM_TEMPLATES)
                    item["system"] = tpl.format(**role_preset)
                items.append(item)

        elif row.get("instruction") or row.get("prompt"):
            # Alpaca 格式 → 转 ShareGPT + 注入角色
            instruction = row.get("instruction", "") or row.get("prompt", "")
            output = row.get("output", "") or row.get("response", "")
            if instruction and output:
                role_preset = random.choice(ROLE_PRESETS)
                tpl = random.choice(NPC_SYSTEM_TEMPLATES)
                items.append({
                    "conversations": [
                        {"from": "human", "value": instruction},
                        {"from": "gpt", "value": output},
                    ],
                    "system": tpl.format(**role_preset),
                })

    return items


def download_belle(max_samples: int = 0) -> list[dict]:
    """下载 BelleGroup 中文指令数据集"""
    from datasets import load_dataset

    print("[download] 正在下载 BelleGroup/train_2M_CN ...")
    ds = load_dataset("BelleGroup/train_2M_CN", split="train", trust_remote_code=True)

    items = []
    for row in ds:
        if max_samples and len(items) >= max_samples:
            break
        instruction = row.get("instruction", "")
        output = row.get("output", "")
        if instruction and output:
            role_preset = random.choice(ROLE_PRESETS)
            tpl = random.choice(NPC_SYSTEM_TEMPLATES)
            items.append({
                "conversations": [
                    {"from": "human", "value": instruction},
                    {"from": "gpt", "value": output},
                ],
                "system": tpl.format(**role_preset),
            })
    return items


def download_firefly(max_samples: int = 0) -> list[dict]:
    """下载萤火虫中文对话数据集"""
    from datasets import load_dataset

    print("[download] 正在下载 YeYKSS/Chat-RolePlay ...")
    try:
        ds = load_dataset("YeYKSS/Chat-RolePlay", split="train", trust_remote_code=True)
    except Exception:
        print("[download] 备用: FreedomIntelligence/ALLM ...")
        ds = load_dataset("FreedomIntelligence/ALLM", split="train", trust_remote_code=True)

    items = []
    for row in ds:
        if max_samples and len(items) >= max_samples:
            break

        conversations = row.get("conversations") or row.get("messages")
        if conversations:
            convs = []
            system_text = ""
            for turn in conversations:
                role = turn.get("from", turn.get("role", ""))
                value = turn.get("value", turn.get("content", ""))
                if role in ("system",):
                    system_text = value
                elif role in ("human", "user"):
                    convs.append({"from": "human", "value": value})
                elif role in ("gpt", "assistant"):
                    convs.append({"from": "gpt", "value": value})

            if len(convs) >= 2:
                item = {"conversations": convs}
                if not system_text:
                    role_preset = random.choice(ROLE_PRESETS)
                    tpl = random.choice(NPC_SYSTEM_TEMPLATES)
                    system_text = tpl.format(**role_preset)
                item["system"] = system_text
                items.append(item)

    return items


DATASET_LOADERS = {
    "character": download_character_chat,
    "belle": download_belle,
    "firefly": download_firefly,
}


def main():
    parser = argparse.ArgumentParser(description="下载公开数据集并转换为项目格式")
    parser.add_argument("--dataset", type=str, default="character",
                        choices=list(DATASET_LOADERS.keys()),
                        help="数据集名称")
    parser.add_argument("--output", type=str, default="data/processed/npc_dialogues.json",
                        help="输出文件路径")
    parser.add_argument("--max", type=int, default=0,
                        help="最大样本数（0=全部）")
    args = parser.parse_args()

    random.seed(42)

    loader = DATASET_LOADERS[args.dataset]
    items = loader(max_samples=args.max)

    # 保存
    out_path = Path(args.output)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(
        json.dumps(items, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )
    print(f"[done] {len(items)} 条样本 → {args.output}")

    # 统计
    has_system = sum(1 for it in items if it.get("system"))
    multi_turn = sum(1 for it in items if len(it.get("conversations", [])) > 2)
    print(f"[stats] 有 system prompt: {has_system}/{len(items)}")
    print(f"[stats] 多轮对话: {multi_turn}/{len(items)}")


if __name__ == "__main__":
    main()
