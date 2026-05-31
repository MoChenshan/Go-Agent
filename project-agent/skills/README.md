# Skills 技能库

D13 起落地，复用框架 `skill.FSRepository` + `codeexecutor/local` 能力。
每个技能目录由 `SKILL.md`（元数据 + 说明）和 `scripts/` 脚本组成。

## 已落地

| 技能 | 入口脚本 | 功能 | 触发场景 |
|------|---------|------|---------|
| `log_pattern` | `scripts/analyze.py` | 正则提取错误模式 + 频率统计 | 大段日志初筛（>1MB） |
| `csv_compare` | `scripts/compare.py`  | 两份 CSV 按 Key 对齐后输出差异 | 性能压测前后对比、配置校对 |
| `perf_report` | `scripts/report.py`   | 统计摘要 + 突增点检测 | 性能数据/监控导出分析 |

所有脚本均为 **标准库实现**（Python 3.6+，零三方依赖），避免 pandas/numpy 带来的部署负担。

## 规划（未来轮次）

| 技能 | 功能 | 触发场景 |
|------|------|---------|
| `container_diagnose` | K8s Pod 诊断辅助 | CrashLoopBackOff / OOMKilled |
| `code_review`        | 修复代码 Review 辅助 | RepairAgent 修复前自审 |

## 启用方式

默认 **未启用**（避免在生产意外 fork Python 子进程）：

```bash
export SKILLS_ENABLE=1
```

项目启动时 `src/skillkit` 负责：
1. 检测 `./skills/` 目录存在性；
2. 检测 `python3` / `python` 可用性；
3. 构造 `skill.FSRepository` + `localexec`，挂到 FileAnalystAgent。

三项任一不满足都会 **优雅降级**：启动日志打印 `skills disabled: <reason>`，不阻塞服务启动。

## 本地直接跑脚本

```bash
# log 模式分析
python3 skills/log_pattern/scripts/analyze.py --path /tmp/gamesvr.log --top 10

# CSV 对比
python3 skills/csv_compare/scripts/compare.py \
    --before /tmp/baseline.csv --after /tmp/new.csv --key-col 0

# 性能报告
python3 skills/perf_report/scripts/report.py --path /tmp/qps.csv --metric qps
```
