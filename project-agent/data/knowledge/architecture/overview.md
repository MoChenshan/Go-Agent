
# GameOps Agent 架构概览

## 5 Agent 分工

GameOps Agent 由 1 个协调者 + 4 个专家构成：

- **Coordinator**：意图路由，将用户请求分发给合适的专家
- **DiagnosisAgent**：调用蓝鲸监控 + BCS 容器工具定位问题根因
- **KnowledgeAgent**：基于运维文档、FAQ、Runbook 回答概念/流程性问题
- **FileAnalystAgent**：分析用户上传的日志 / JSON / YAML
- **RepairAgent**：执行 Helm 回滚、CI/CD 重跑，写操作必经 HITL 人工确认

## 典型排障链路

1. 用户贴出告警 → Coordinator → Diagnosis
2. Diagnosis 先调用 `bk_alarm_query` 找告警详情
3. 再调用 `bcs_resource_query` 看 Pod 状态、Event
4. 最后调用 `bk_log_query` 抓关键日志
5. 给出根因结论 + 修复建议
6. 如需修复，Coordinator transfer 给 Repair，Repair 走 HITL

## 工具分组（target）

- `bk-monitor` — 蓝鲸监控（Diagnosis）
- `bcs-read`   — BCS 容器只读（Diagnosis）
- `bcs-write`  — BCS 容器写操作（Repair，必须 HITL）
- `gongfeng`   — 工蜂 Git（Repair）
- `devops`     — 蓝盾 CI/CD（Repair）
- `tapd`       — TAPD 单据（Repair）
- `*`          — 通用工具（所有 Agent）
