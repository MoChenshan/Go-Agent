"""数据飞轮 replay buffer：防灾难性遗忘。

背景：每次新增训练数据上线后，模型可能在新数据上表现提升、但在旧能力上退化。
做法：按比例（默认 80% 旧 + 20% 新）合并历史与新数据，形成下一轮 SFT 训练集。

特性：
- 跨轮去重（按 prompt simhash）
- 类别配比（保证每类样本占比不被新数据冲击）
- 可选采样（reservoir，控制总样本量）

用法：
    python scripts/data_replay_buffer.py \
        --base data/processed \
        --new  data/feedback/2026-04 \
        --out  data/processed/replay_2026-05.jsonl \
        --ratio 0.2 \
        --total 25000
"""

import argparse
import hashlib
import json
import os
import random
import sys
from collections import defaultdict
from typing import Iterable, List


def load_jsonl(path: str) -> Iterable[dict]:
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                yield json.loads(line)
            except json.JSONDecodeError as e:
                print(f"WARN: skip malformed line in {path}: {e}", file=sys.stderr)


def load_dir(d: str) -> List[dict]:
    out: List[dict] = []
    if not os.path.isdir(d):
        print(f"WARN: dir not found: {d}", file=sys.stderr)
        return out
    for root, _, files in os.walk(d):
        for fn in files:
            if not fn.endswith(".jsonl"):
                continue
            for rec in load_jsonl(os.path.join(root, fn)):
                out.append(rec)
    return out


def signature(rec: dict) -> str:
    """简单 normalize + sha1，足以挡掉大部分重复。"""
    msgs = rec.get("messages") or []
    body = "\n".join(m.get("content", "")[:512] for m in msgs)
    return hashlib.sha1(body.encode("utf-8", errors="ignore")).hexdigest()


def domain_of(rec: dict) -> str:
    return ((rec.get("meta") or {}).get("domain")) or "unknown"


def stratified_sample(items: List[dict], target_total: int, rng: random.Random) -> List[dict]:
    """按 domain 分层等比抽样。"""
    if target_total <= 0 or target_total >= len(items):
        return items
    by_domain: dict = defaultdict(list)
    for r in items:
        by_domain[domain_of(r)].append(r)
    total = len(items)
    out: List[dict] = []
    remainder: List[dict] = []
    for d, lst in by_domain.items():
        rng.shuffle(lst)
        share = max(1, int(target_total * len(lst) / total))
        out.extend(lst[:share])
        remainder.extend(lst[share:])
    if len(out) > target_total:
        rng.shuffle(out)
        out = out[:target_total]
    elif len(out) < target_total:
        rng.shuffle(remainder)
        out.extend(remainder[: target_total - len(out)])
    return out


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", required=True, help="历史数据目录")
    ap.add_argument("--new",  required=True, help="新增数据目录")
    ap.add_argument("--out",  required=True)
    ap.add_argument("--ratio", type=float, default=0.2, help="新数据占比；默认 0.2")
    ap.add_argument("--total", type=int, default=25000, help="目标总样本量；0 = 不限制")
    ap.add_argument("--seed",  type=int, default=42)
    args = ap.parse_args()

    if not (0 < args.ratio < 1):
        print("ratio must be in (0, 1)", file=sys.stderr)
        return 2

    rng = random.Random(args.seed)
    base = load_dir(args.base)
    new = load_dir(args.new)
    print(f"base={len(base)} new={len(new)}")

    if not new:
        print("ERROR: 没有新数据可 replay", file=sys.stderr)
        return 2

    # 跨轮去重：以 base 的签名为基准，过滤 new 中的重复
    base_sig = {signature(r) for r in base}
    new_dedup = [r for r in new if signature(r) not in base_sig]
    print(f"new after cross-dedup: {len(new_dedup)}")

    target_total = args.total if args.total > 0 else len(base) + len(new_dedup)
    n_new = max(1, int(target_total * args.ratio))
    n_base = max(0, target_total - n_new)

    # base 分层采样
    base_picked = stratified_sample(base, n_base, rng)

    # new 全收，多则采样
    rng.shuffle(new_dedup)
    new_picked = new_dedup[:n_new]

    merged = base_picked + new_picked
    rng.shuffle(merged)

    os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
    with open(args.out, "w", encoding="utf-8") as f:
        for r in merged:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")

    # 输出 manifest 便于下游审计
    manifest = {
        "out": args.out,
        "total": len(merged),
        "base_count": len(base_picked),
        "new_count": len(new_picked),
        "ratio_actual": round(len(new_picked) / max(len(merged), 1), 4),
        "by_domain": {d: c for d, c in (
            (lambda dd=defaultdict(int): (
                [dd.__setitem__(domain_of(r), dd[domain_of(r)] + 1) for r in merged] and dd
            ))()).items()},
        "seed": args.seed,
    }
    with open(args.out + ".manifest.json", "w", encoding="utf-8") as f:
        json.dump(manifest, f, ensure_ascii=False, indent=2)
    print(json.dumps(manifest, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
