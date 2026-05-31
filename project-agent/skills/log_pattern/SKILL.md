---
name: log_pattern
description: 从日志文件中提取错误模式和频率统计。输入一个文本日志文件路径，输出 Top-N 错误关键词、异常堆栈计数、时间分布。适合 gamesvr/gamedb 启动失败/OOM/panic 场景的初筛。
version: 0.1.0
inputs:
  - name: path
    type: string
    required: true
    description: 日志文件路径（支持 workspace://inputs/ 引用）
  - name: top
    type: int
    required: false
    default: 10
    description: 返回频率前 N 的错误模式
---

# log_pattern 技能

## 用途

- 从 Go 服务的 stderr/stdout 日志中提取 `panic:` / `fatal:` / `error` / `runtime error`
  等关键字命中行，统计频率。
- 输出 JSON：`{top: [{pattern, count, sample_line}], total_lines, error_lines}`。
- 供 FileAnalystAgent 在用户上传大日志文件（>1MB）时先跑一次快速摘要，
  再把 Top-N 错误模式喂给 LLM 做深入分析。

## 入参

| 字段 | 类型 | 必填 | 说明 |
|------|-----|------|------|
| path | string | ✅ | 日志文件路径 |
| top  | int    | ❌ | 返回 Top-N，默认 10 |

## 出参

```json
{
  "top": [
    {"pattern": "panic: runtime error: invalid memory address", "count": 12, "sample_line": "..."},
    {"pattern": "fatal: database connection refused",            "count":  7, "sample_line": "..."}
  ],
  "total_lines": 1024,
  "error_lines": 19
}
```

## 运行

```bash
python3 scripts/analyze.py --path <path> --top <n>
```
