#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""perf_report 技能：对性能数据 CSV 做统计 + 突增点检测。

标准库实现，零三方依赖（避免 pandas/numpy），兼容 Python 3.6+。
"""

import argparse
import csv
import json
import math
import sys


def percentile(sorted_values, p):
    if not sorted_values:
        return 0
    k = (len(sorted_values) - 1) * (p / 100.0)
    f = math.floor(k)
    c = math.ceil(k)
    if f == c:
        return sorted_values[int(k)]
    return sorted_values[f] + (sorted_values[c] - sorted_values[f]) * (k - f)


def analyze(path: str, metric: str):
    values = []
    head_sample = []
    col_idx = None

    with open(path, "r", encoding="utf-8-sig", errors="replace", newline="") as f:
        reader = csv.reader(f)
        header = next(reader, None)
        if not header:
            return {"error": "empty csv"}
        for i, name in enumerate(header):
            if name.strip() == metric:
                col_idx = i
                break
        if col_idx is None:
            return {"error": f"metric column '{metric}' not found in header {header}"}
        head_sample.append(",".join(header))
        for row in reader:
            if len(head_sample) < 4:
                head_sample.append(",".join(row))
            if col_idx >= len(row):
                continue
            try:
                values.append(float(row[col_idx]))
            except ValueError:
                continue

    if not values:
        return {"error": "no numeric values"}

    sorted_vals = sorted(values)
    total = sum(values)
    mean = total / len(values)
    stats = {
        "count": len(values),
        "min":   sorted_vals[0],
        "max":   sorted_vals[-1],
        "mean":  round(mean, 4),
        "p50":   round(percentile(sorted_vals, 50), 4),
        "p90":   round(percentile(sorted_vals, 90), 4),
        "p95":   round(percentile(sorted_vals, 95), 4),
        "p99":   round(percentile(sorted_vals, 99), 4),
    }

    spikes = []
    for i in range(1, len(values)):
        prev, cur = values[i - 1], values[i]
        if prev == 0:
            continue
        ratio = cur / prev
        if ratio >= 1.5 or ratio <= 0.5:
            spikes.append({
                "idx":   i,
                "prev":  prev,
                "cur":   cur,
                "ratio": round(ratio, 4),
            })
        if len(spikes) >= 20:
            break

    return {
        "metric":       metric,
        "stats":        stats,
        "spikes":       spikes,
        "sample_head":  head_sample,
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--path",   required=True)
    ap.add_argument("--metric", default="value")
    args = ap.parse_args()

    try:
        result = analyze(args.path, args.metric)
    except FileNotFoundError:
        print(json.dumps({"error": f"file not found: {args.path}"}, ensure_ascii=False))
        sys.exit(1)
    except Exception as e:
        print(json.dumps({"error": str(e)}, ensure_ascii=False))
        sys.exit(1)

    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
