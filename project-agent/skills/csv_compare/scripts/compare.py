#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""csv_compare 技能：两份 CSV 按 Key 对齐后输出差异。

标准库 csv 实现，零三方依赖（避免 pandas 依赖），兼容 Python 3.6+。
"""

import argparse
import csv
import json
import sys


def load_csv(path: str, key_col: int):
    rows_by_key = {}
    header = None
    with open(path, "r", encoding="utf-8-sig", errors="replace", newline="") as f:
        reader = csv.reader(f)
        for i, row in enumerate(reader):
            if i == 0:
                header = row
                continue
            if not row:
                continue
            if key_col >= len(row):
                continue
            key = row[key_col].strip()
            if key == "":
                continue
            rows_by_key[key] = row
    return header or [], rows_by_key


def diff(before_rows, after_rows):
    added, removed, changed = [], [], []
    unchanged = 0
    b_keys = set(before_rows.keys())
    a_keys = set(after_rows.keys())

    for k in sorted(a_keys - b_keys):
        added.append({"key": k, "row": after_rows[k]})
    for k in sorted(b_keys - a_keys):
        removed.append({"key": k, "row": before_rows[k]})
    for k in sorted(a_keys & b_keys):
        b = before_rows[k]
        a = after_rows[k]
        if b == a:
            unchanged += 1
            continue
        col_diff = {}
        width = max(len(b), len(a))
        for ci in range(width):
            bv = b[ci] if ci < len(b) else ""
            av = a[ci] if ci < len(a) else ""
            if bv != av:
                col_diff[f"col_{ci}"] = [bv, av]
        changed.append({"key": k, "diff": col_diff})

    return {
        "added":   added,
        "removed": removed,
        "changed": changed,
        "summary": {
            "added":     len(added),
            "removed":   len(removed),
            "changed":   len(changed),
            "unchanged": unchanged,
        },
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--before",  required=True)
    ap.add_argument("--after",   required=True)
    ap.add_argument("--key-col", type=int, default=0)
    args = ap.parse_args()

    try:
        _, b = load_csv(args.before, args.key_col)
        _, a = load_csv(args.after,  args.key_col)
        result = diff(b, a)
    except FileNotFoundError as e:
        print(json.dumps({"error": f"file not found: {e.filename}"}, ensure_ascii=False))
        sys.exit(1)
    except Exception as e:
        print(json.dumps({"error": str(e)}, ensure_ascii=False))
        sys.exit(1)

    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
