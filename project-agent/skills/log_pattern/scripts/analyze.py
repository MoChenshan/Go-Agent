#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""log_pattern 技能：从文本日志中提取错误模式和频率。

标准库实现，零三方依赖，兼容 Python 3.6+。
"""

import argparse
import json
import re
import sys
from collections import Counter


ERROR_PATTERNS = [
    (re.compile(r"panic:\s*(.+)"),                     "panic"),
    (re.compile(r"fatal(?:\s+error)?:\s*(.+)", re.I),  "fatal"),
    (re.compile(r"runtime\s+error:\s*(.+)", re.I),     "runtime_error"),
    (re.compile(r"\[error\]\s*(.+)", re.I),            "error_tag"),
    (re.compile(r"ERROR\s+(.+)"),                      "error"),
    (re.compile(r"\bexception\b[:\s]+(.+)", re.I),     "exception"),
    (re.compile(r"failed\s+to\s+(.+)", re.I),          "failed_to"),
    (re.compile(r"cannot\s+(.+)", re.I),               "cannot"),
    (re.compile(r"connection\s+(?:refused|reset|timeout)", re.I), "conn_err"),
    (re.compile(r"OOM|out\s+of\s+memory", re.I),       "oom"),
]


def normalize(line: str) -> str:
    """把行里的数字、十六进制、时间戳归一化，便于聚合同类错误。"""
    s = line.strip()
    s = re.sub(r"0x[0-9a-fA-F]+",         "<hex>",   s)
    s = re.sub(r"\b\d{10,}\b",            "<ts>",    s)
    s = re.sub(r"\b\d+(\.\d+)?\b",        "<n>",     s)
    s = re.sub(r"\s+",                    " ",       s)
    return s[:200]


def match_error(line: str):
    for pat, tag in ERROR_PATTERNS:
        m = pat.search(line)
        if m:
            return tag, normalize(line)
    return None, None


def analyze(path: str, top: int):
    total = 0
    err = 0
    counter = Counter()
    samples = {}

    with open(path, "r", encoding="utf-8", errors="replace") as f:
        for line in f:
            total += 1
            tag, key = match_error(line)
            if tag is None:
                continue
            err += 1
            fullkey = f"{tag}::{key}"
            counter[fullkey] += 1
            if fullkey not in samples:
                samples[fullkey] = line.rstrip("\n")[:500]

    out_top = []
    for fullkey, cnt in counter.most_common(top):
        tag, pattern = fullkey.split("::", 1)
        out_top.append({
            "tag":         tag,
            "pattern":     pattern,
            "count":       cnt,
            "sample_line": samples.get(fullkey, ""),
        })
    return {
        "top":         out_top,
        "total_lines": total,
        "error_lines": err,
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--path", required=True, help="日志文件路径")
    ap.add_argument("--top",  type=int, default=10, help="Top-N")
    args = ap.parse_args()

    try:
        result = analyze(args.path, args.top)
    except FileNotFoundError:
        print(json.dumps({"error": f"file not found: {args.path}"}, ensure_ascii=False))
        sys.exit(1)
    except Exception as e:
        print(json.dumps({"error": str(e)}, ensure_ascii=False))
        sys.exit(1)

    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
