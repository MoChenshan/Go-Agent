---
name: perf_report
description: 解析性能数据 CSV，生成趋势分析报告（均值/分位数/变化率/突增点）。适合压测数据、监控导出数据的自动化初筛。
version: 0.1.0
inputs:
  - name: path
    type: string
    required: true
    description: 性能数据 CSV（首行表头，含 ts/value 列）
  - name: metric
    type: string
    required: false
    default: value
    description: 目标指标列名
---

# perf_report 技能

## 用途

- 读取性能数据 CSV，基于单指标列输出趋势摘要：
  `count / min / max / mean / p50 / p90 / p95 / p99`；
- 额外给出"突增点"：相邻两点变化率 > 50% 的索引与值；
- 供 FileAnalystAgent / DiagnosisAgent 对压测、监控导出数据做第一手聚合。

## 入参

| 字段 | 类型 | 必填 | 说明 |
|------|-----|------|------|
| path   | string | ✅ | 性能 CSV 路径 |
| metric | string | ❌ | 指标列名，默认 `value` |

## 出参

```json
{
  "metric": "latency_ms",
  "stats":  {"count": 1000, "min": 1, "max": 999, "mean": 42.5,
             "p50": 30, "p90": 80, "p95": 120, "p99": 500},
  "spikes": [{"idx": 514, "prev": 30, "cur": 120, "ratio": 4.0}],
  "sample_head": ["ts,value", "1700000000,30", "..."]
}
```

## 运行

```bash
python3 scripts/report.py --path <csv> [--metric value]
```
