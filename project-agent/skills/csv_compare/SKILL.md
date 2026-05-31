---
name: csv_compare
description: 对比两个 CSV 文件的差异，生成变化摘要。按首列 Key 对齐两份表，输出新增/删除/值变化三类行。适合性能压测前后数据对比、配置差异化校对。
version: 0.1.0
inputs:
  - name: before
    type: string
    required: true
    description: 旧版 CSV 路径（基线）
  - name: after
    type: string
    required: true
    description: 新版 CSV 路径（对比版）
  - name: key_col
    type: int
    required: false
    default: 0
    description: 作为主键的列索引（0-based），默认首列
---

# csv_compare 技能

## 用途

- 两份 CSV 文件按 Key 对齐后，输出差异摘要。
- 典型场景：发版前后的性能压测 QPS/Latency 对比、配置表校对。

## 入参

| 字段 | 类型 | 必填 | 说明 |
|------|-----|------|------|
| before  | string | ✅ | 基线 CSV |
| after   | string | ✅ | 对比 CSV |
| key_col | int    | ❌ | 作为主键的列，0-based，默认 0 |

## 出参

```json
{
  "added":    [{"key": "...", "row": ["..."]}],
  "removed":  [{"key": "...", "row": ["..."]}],
  "changed":  [{"key": "...", "diff": {"col_2": ["before", "after"]}}],
  "summary":  {"added": 3, "removed": 2, "changed": 5, "unchanged": 120}
}
```

## 运行

```bash
python3 scripts/compare.py --before <p1> --after <p2> [--key-col 0]
```
