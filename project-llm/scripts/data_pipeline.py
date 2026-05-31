"""data_pipeline.py — 原始数据 → 标准化 SFT 数据集。

职责：
  1. 扫描 data/raw 下所有原始数据（NPC profiles / wiki docs / 自由对话）
  2. 调用现有 generate_npc_data.py / generate_qa.py 的产物（若存在则直接读）
  3. 去重（按 prompt 文本 hash）
  4. PII 过滤（手机号 / 身份证 / 邮箱）
  5. 长度过滤（cutoff_len）
  6. 输出 data/processed/sft_demo.jsonl —— LlamaFactory ShareGPT 格式

用法：
    python scripts/data_pipeline.py --in data/raw --out data/processed
"""
from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from pathlib import Path
from typing import Iterable

# ---------- PII 正则 ----------
PII_PATTERNS: list[tuple[str, re.Pattern[str]]] = [
    ("phone", re.compile(r"1[3-9]\d{9}")),
    ("id_card", re.compile(r"[1-9]\d{5}(19|20)\d{2}(0[1-9]|1[0-2])(0[1-9]|[12]\d|3[01])\d{3}[\dXx]")),
    ("email", re.compile(r"[A-Za-z0-9_.+-]+@[A-Za-z0-9-]+\.[A-Za-z0-9-.]+")),
]


def strip_pii(text: str) -> tuple[str, list[str]]:
    hit: list[str] = []
    for tag, pat in PII_PATTERNS:
        if pat.search(text):
            hit.append(tag)
            text = pat.sub(f"[REDACTED_{tag.upper()}]", text)
    return text, hit


def hash_key(s: str) -> str:
    return hashlib.md5(s.strip().encode("utf-8")).hexdigest()


def load_npc_profiles(p: Path) -> Iterable[dict]:
    """从 npc_profiles.json 衍生最小的 SFT 样本（system + greeting）。"""
    if not p.exists():
        return
    data = json.loads(p.read_text(encoding="utf-8"))
    npcs = data if isinstance(data, list) else data.get("npcs", [])
    for npc in npcs:
        name = npc.get("name", "无名 NPC")
        persona = npc.get("persona") or npc.get("description") or ""
        greeting = npc.get("greeting") or f"我是{name}，有什么可以帮你？"
        if not persona:
            continue
        yield {
            "conversations": [
                {"from": "system", "value": f"你是{name}。{persona}"},
                {"from": "human", "value": "你好，介绍下你自己"},
                {"from": "gpt", "value": greeting},
            ],
            "_source": "npc_profile",
        }


def load_wiki_docs(folder: Path) -> Iterable[dict]:
    """把 wiki_docs/*.md 转成"问知识 → 答内容首段"。"""
    if not folder.exists():
        return
    for md in folder.glob("*.md"):
        text = md.read_text(encoding="utf-8").strip()
        if len(text) < 50:
            continue
        first_para = next((p for p in text.split("\n\n") if p.strip()), text)
        yield {
            "conversations": [
                {"from": "system", "value": "你是一个游戏后端文档专家。"},
                {"from": "human", "value": f"请简介一下 {md.stem} 模块"},
                {"from": "gpt", "value": first_para[:600]},
            ],
            "_source": f"wiki:{md.name}",
        }


def load_existing_jsonl(folder: Path) -> Iterable[dict]:
    """如已用 generate_*.py 生成过 jsonl，则一并合并。"""
    if not folder.exists():
        return
    for jl in folder.glob("*.jsonl"):
        for line in jl.read_text(encoding="utf-8").splitlines():
            line = line.strip()
            if not line:
                continue
            try:
                yield json.loads(line)
            except json.JSONDecodeError:
                continue


def normalize(item: dict) -> dict | None:
    """统一为 LlamaFactory ShareGPT。返回 None 视为丢弃。"""
    if "conversations" in item:
        conv = item["conversations"]
    elif "messages" in item:
        conv = []
        for m in item["messages"]:
            role_map = {"user": "human", "assistant": "gpt", "system": "system"}
            conv.append({"from": role_map.get(m["role"], m["role"]), "value": m["content"]})
    elif "instruction" in item and "output" in item:
        conv = [
            {"from": "human", "value": item["instruction"]},
            {"from": "gpt", "value": item["output"]},
        ]
    else:
        return None

    pii_tags: set[str] = set()
    for turn in conv:
        v, hits = strip_pii(turn.get("value", ""))
        turn["value"] = v
        pii_tags.update(hits)

    text_total = sum(len(t.get("value", "")) for t in conv)
    if text_total < 8 or text_total > 8000:
        return None
    return {"conversations": conv, "_pii_redacted": sorted(pii_tags)}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--in", dest="src", default="data/raw")
    ap.add_argument("--out", dest="dst", default="data/processed")
    ap.add_argument("--name", default="sft_demo.jsonl")
    args = ap.parse_args()

    src = Path(args.src)
    dst = Path(args.dst)
    dst.mkdir(parents=True, exist_ok=True)

    raw: list[dict] = []
    raw.extend(load_npc_profiles(src / "npc_profiles.json"))
    raw.extend(load_wiki_docs(src / "wiki_docs"))
    raw.extend(load_existing_jsonl(src))

    seen: set[str] = set()
    out_path = dst / args.name
    n_in = n_out = n_dup = n_drop = 0
    pii_hit = 0
    with out_path.open("w", encoding="utf-8") as f:
        for item in raw:
            n_in += 1
            norm = normalize(item)
            if norm is None:
                n_drop += 1
                continue
            key = hash_key(json.dumps(norm["conversations"], ensure_ascii=False))
            if key in seen:
                n_dup += 1
                continue
            seen.add(key)
            if norm.get("_pii_redacted"):
                pii_hit += 1
            f.write(json.dumps(norm, ensure_ascii=False) + "\n")
            n_out += 1

    summary = {
        "input": n_in,
        "output": n_out,
        "dup_dropped": n_dup,
        "format_dropped": n_drop,
        "pii_redacted": pii_hit,
        "out": str(out_path),
    }
    print(json.dumps(summary, ensure_ascii=False, indent=2))

    if n_out == 0:
        print("ERROR: 未产出任何样本，请检查 data/raw 内容", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
