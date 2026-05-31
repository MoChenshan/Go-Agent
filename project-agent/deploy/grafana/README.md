# GameOps Agent · Grafana 面板模板（D19.4）

## 目录结构

```
deploy/grafana/
├── README.md                         ← 本文件：使用说明与设计取舍
├── panels.yaml                       ← 意图级面板声明（SSOT）
└── dashboards/
    └── gameops-overview.json         ← 手工维护的最小 JSON 骨架（可直接 Import）
```

## 为什么是两份：YAML + JSON？

Grafana 面板的 JSON 结构"又长又脆"——一个完整 15 panels 的面板 JSON 常常 2000+ 行，
手写且手改的维护成本极高。但 Grafana 本身只接受 JSON，没法绕开。

所以分两层：

| 文件 | 用途 | 期望读者 |
|---|---|---|
| `panels.yaml` | **设计意图**的 SSOT：哪些面板、每个 panel 用什么 PromQL、阈值多少 | 代码评审人、SRE、研发 |
| `dashboards/*.json` | **运维可直接 Import** 的最小骨架 | 运维环境初始化脚本 |

修改流程：
1. 先改 `panels.yaml` 走评审；
2. 评审通过后再手工同步到对应 `*.json`；
3. （未来 `gen.go` 可自动化第 2 步，本轮不做。）

## 指标真相源（Metrics SSOT）

所有面板引用的指标名在这两个 Go 文件中定义：
- `src/observability/metrics.go` —— D16 基础指标（webhook / guard / llm / tool / sse）
- `src/observability/metrics_more.go` —— D17.x / D19.x 扩展指标（audit_remote / judge / rule_reload / async）

修改指标名时必须同步更新：
- `panels.yaml`（本目录）
- `dashboards/*.json`（本目录）
- `deploy/alerts/prometheus_rules.yaml`（告警规则）

> **编译期保障**：代码里引用 `observability.MetricWebhookRequests` 常量，指标改名时编译器会报错；
> 但面板和告警是**纯文本引用**，没有编译检查——所以请用 `grep -r gameops_xxx_total` 双向同步。

## 部署

### Grafana（v9+）
```bash
# 方式 1：通过 UI Import
# Grafana → Dashboards → New → Import → Upload JSON File

# 方式 2：Grafana API 批量导入
curl -X POST -H "Authorization: Bearer $GRAFANA_TOKEN" \
     -H "Content-Type: application/json" \
     -d @deploy/grafana/dashboards/gameops-overview.json \
     $GRAFANA_URL/api/dashboards/db
```

### Prometheus 告警规则
```yaml
# prometheus.yml
rule_files:
  - /etc/prometheus/rules/gameops-agent.yaml
```
把 `deploy/alerts/prometheus_rules.yaml` 挂到对应路径即可。

## 面板导航

### Dashboard #1: gameops-overview —— 总览
4 行、7 panels，面向日常值班巡检：
- **① 入口压力**：Webhook QPS / outcome 分布
- **② LLM 健康**：调用速率 / 错误率
- **③ 安全审计**：Guard 拦截 / 审计远端 dropped/failed 红线
- **④ 异步任务**：队列水位 / 终态分布

### Dashboard #2: gameops-async-deep —— 深潜（仅 YAML 声明）
3 行专供 async 故障排查：
- **① 延迟分布**：p50 / p95 / p99 + 热力图
- **② 终态构成**：timed_out 比例 / cancelled 区分
- **③ 背压信号**：rejected / dedup_hit

> 当前仅 `panels.yaml` 里声明，未生成对应 JSON。若需要可手工按 overview 的 JSON 模式补齐。

## 关键告警与面板对应关系

| 告警 | 对应面板 | 关键数据源 |
|---|---|---|
| `GameOpsAsyncQueueSaturated` | overview · 异步队列水位 | `gameops_async_jobs_submitted/finished_total` |
| `GameOpsAsyncTimeoutRatioHigh` | async-deep · timed_out 比例 | `gameops_async_jobs_finished_total{status="timed_out"}` |
| `GameOpsAsyncJobDurationP95Regression` | async-deep · p99 热力图 | `gameops_async_jobs_duration_seconds_bucket` |
| `GameOpsAuditRemoteDropped` | overview · 审计远端红线 | `gameops_audit_remote_dropped_total` |
| `GameOpsLLMErrorRatioHigh` | overview · LLM 错误率 | `gameops_agent_llm_calls_total{status="error"}` |
| `GameOpsWebhookSignatureFailures` | overview · Webhook outcome 分布 | `gameops_webhook_requests_total{outcome="signature_failed"}` |
