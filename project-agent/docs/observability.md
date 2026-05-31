# GameOps Agent 可观测性手册

> 适用范围：D16（OTel 基建）+ D16.1（Sampler 可配置化 + SSE 事件埋点）之后的所有版本。
> 目标读者：SRE / Oncall / 架构评审委员会。

本手册把散落在代码注释、PROGRESS.md 变更日志、告警规则文件里的"可观测性约定"集中收敛，
作为**唯一事实源**。三类产物的关系：

```
OTel 装配代码 (src/observability/*)
        │  产出
        ▼
指标/Span 约定 (本文档) ──────────► Prometheus 告警规则 (deploy/alerts/*.yaml)
        │  指导                                 │  依赖
        ▼                                       │
OTLP Exporter 接入文档 ◄───────────────────────┘
```

---

## 1. 启停与总开关

所有 OTel 相关行为**默认关闭**（Noop Tracer/Meter），保证本地/CI/离线环境零开销、零依赖。

### 1.1 环境变量

| 变量 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `OTEL_ENABLED` | bool | `false` | 总开关。为 `true` 时才创建真实 Tracer/Meter Provider。 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | string | 空 | OTLP Collector 地址（如 `http://otel-collector:4318`）。空=不接 exporter，但 sampler 仍生效（便于本地 debug 采样策略）。 |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | enum | `http/protobuf` | 可选 `grpc` / `http/protobuf`。 |
| `OTEL_SERVICE_NAME` | string | `gameops-agent` | Resource `service.name`。 |
| `OTEL_SERVICE_VERSION` | string | `dev` | Resource `service.version`。 |
| `OTEL_DEPLOYMENT_ENVIRONMENT` | string | `local` | Resource `deployment.environment`（例 `prod`/`staging`）。 |
| `OTEL_TRACES_SAMPLER` | enum | `parentbased_always_on` | 见 §1.2。 |
| `OTEL_TRACES_SAMPLER_ARG` | float | `1.0` | 采样比，仅对 `*_traceidratio` 系生效。 |

### 1.2 采样策略（D16.1 新增）

| 值 | 语义 | 推荐场景 |
| --- | --- | --- |
| `always_on` | 全采 | 本地 debug / 灰度验证 |
| `always_off` | 全不采 | 应急止损（不推荐长期用） |
| `traceidratio` | 按 TraceID hash 比例采 | 不在意跨服务一致性 |
| `parentbased_always_on` | 有父随父，无父全采 | **默认**；跨服务 trace 最常见 |
| `parentbased_always_off` | 有父随父，无父全丢 | 入口网关已决策时使用 |
| `parentbased_traceidratio` | 有父随父，无父按比例采 | **生产推荐**，配合 `ARG=0.1` |

> 🔒 非法/未知值一律回落到 `parentbased_always_on`（见 `resolveSampler`），启动永不失败。
> 🔒 `OTEL_TRACES_SAMPLER_ARG` 非法/负/>1 一律回落到 `1.0`，宁可采太多也不静默消失。

### 1.3 典型配置

**本地开发**（完全 Noop）：
```bash
# 什么都不设，零开销
go run .
```

**本地对接 Docker Collector**（全采）：
```bash
export OTEL_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export OTEL_TRACES_SAMPLER=always_on
```

**生产**（10% 采样、遵循上游决策）：
```bash
export OTEL_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector.observability:4318
export OTEL_EXPORTER_OTLP_PROTOCOL=grpc
export OTEL_SERVICE_NAME=gameops-agent
export OTEL_SERVICE_VERSION=v1.2.3
export OTEL_DEPLOYMENT_ENVIRONMENT=prod
export OTEL_TRACES_SAMPLER=parentbased_traceidratio
export OTEL_TRACES_SAMPLER_ARG=0.1
```

---

## 2. Metrics 约定

### 2.1 指标清单

| 指标名 | 类型 | 标签 | 来源 |
| --- | --- | --- | --- |
| `gameops.webhook.requests.total` | Counter | `source`, `outcome` | `src/services/webhook/webhook.go` |
| `gameops.guard.redacted.total` | Counter | `rule` | `src/plugin/output_guard.go` |
| `gameops.input_guard.blocked.total` | Counter | `rule` | `src/plugin/input_guard.go` |
| `gameops.agent.llm.calls.total` | Counter | `agent`, `status` | `src/observability/callbacks.go` |
| `gameops.agent.tool.calls.total` | Counter | `agent`, `tool`, `status` | `src/observability/callbacks.go` |
| `gameops.sse.events.total` | Counter | `event` | `src/services/sse/sse.go` |

### 2.2 标签枚举

| 标签 | 枚举值 |
| --- | --- |
| `source` | `bk_alarm` / `tapd_webhook` / 其他接入方自定 |
| `outcome`（webhook） | `accepted` / `rejected` / `signature_failed` / `malformed` |
| `status`（llm/tool） | `ok` / `error` |
| `event`（sse） | `delta` / `tool_call` / `agent_transfer` / `confirmation_required` / `final` / `error` |
| `rule` | 由 input_guard / output_guard 规则配置决定（如 `id_card` / `phone` / `prompt_injection`） |
| `agent` | `coordinator` / `diagnosis` / `knowledge` / `file_analyst` / `repair` |
| `tool` | 工具 FunctionTool 名（如 `bk_alarm_query` / `gongfeng_mr_create`） |

> ⚠️ 新增标签值前必须先更新本文档；未登记的值进入 Prometheus 会导致 **label cardinality 爆炸**，
> 可能压垮 Prometheus TSDB。尤其 `tool` / `rule` 两个维度最易失控。

### 2.3 Prometheus 命名映射

OpenTelemetry → Prometheus 导出时按官方规则：
- `.` → `_`
- 单位后缀/`_total` 尾缀由 Exporter 保证。

因此查询时写：`gameops_sse_events_total`，而不是 `gameops.sse.events.total`。

---

## 3. Traces（GenAI Semantic Conv v1.30）

### 3.1 Span 层级

```
HTTP/SSE 入口 (自动, http server instrumentation)
  └── invoke <agent-name>                (业务 span, agent.go)
        ├── chat <model-name>            (LLM span, observability/genai_span.go)
        │     attributes:
        │       gen_ai.system = "openai-compatible"
        │       gen_ai.request.model = "hunyuan-turbo-s"
        │       gen_ai.request.temperature / top_p / max_tokens
        │       gen_ai.usage.input_tokens / output_tokens
        │     events:
        │       gen_ai.user.message / gen_ai.assistant.message / gen_ai.tool.message
        └── execute_tool <tool-name>     (Tool span, callbacks.go)
              attributes:
                gen_ai.tool.name
                gen_ai.tool.call.id
```

### 3.2 与 Langfuse / 其他 LLM 观测平台对接

Langfuse 从 v2.50 起原生支持 OTel GenAI Semantic Conv，无需 SDK 层改造：

1. 部署 Langfuse 自托管实例（或使用 cloud）。
2. 配置 Collector 的 OTLP endpoint 指向 Langfuse，或直接把 `OTEL_EXPORTER_OTLP_ENDPOINT` 指到 Langfuse 的 OTLP 入口：
   ```
   OTEL_EXPORTER_OTLP_ENDPOINT=https://cloud.langfuse.com/api/public/otel
   OTEL_EXPORTER_OTLP_HEADERS=authorization=Basic <base64(pk:sk)>
   ```
3. 即可在 Langfuse UI 看到按 `invoke diagnosis` 分组的完整会话，每条 `chat hunyuan-turbo-s` 为一轮 LLM 调用，`execute_tool bk_alarm_query` 为一次工具调用。

### 3.3 伽利略 / 智研（腾讯内网）

目前以**构建标签隔离**的方式规划（`-tags galileo`），代码未落地。落地时需：
- 新建 `src/observability/galileo.go`（构建约束 `//go:build galileo`）；
- 把 `Init` 里对 Langfuse-compatible endpoint 的配置替换为伽利略特定的 exporter；
- CI 按需 `go build -tags galileo`。

---

## 4. 告警规则

生产就绪告警规则集放在 [deploy/alerts/prometheus_rules.yaml](../deploy/alerts/prometheus_rules.yaml)，
共 5 组 10 条，覆盖：

| 组 | 关注点 | 严重度分布 |
| --- | --- | --- |
| `gameops-agent.guard` | input/output guard 触发频率 | warning / info |
| `gameops-agent.webhook` | 签名失败、拒绝率、malformed | critical / warning |
| `gameops-agent.llm` | LLM 失败率、调用归零 | critical / warning |
| `gameops-agent.tools` | 工具失败率、写工具突增 | warning |
| `gameops-agent.sse` | error 事件激增、HITL 占比异常 | warning / info |

**调优准则**（接真实流量前**务必**按以下步骤校准）：

1. 让服务跑 7 天基线（不开告警），导出各指标 p50/p95/p99；
2. 将告警阈值设为 `max(绝对阈值, p99 × 3)`，避免白噪音告警；
3. `for:` 时间窗口不低于 2 个 scrape interval（默认 30s × 2 = 1m）；
4. critical 级别必须挂 runbook_url，warning 级别至少挂 description。

---

## 5. 本地联调 Cheatsheet

### 5.1 起一个本地 OTel Collector

```bash
docker run -p 4317:4317 -p 4318:4318 \
  -v $(pwd)/otel-config.yaml:/etc/otelcol/config.yaml \
  otel/opentelemetry-collector-contrib:latest
```

最小 `otel-config.yaml`（只打印 stdout，不外发）：

```yaml
receivers:
  otlp:
    protocols:
      http:
      grpc:
exporters:
  debug:
    verbosity: detailed
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [debug]
    metrics:
      receivers: [otlp]
      exporters: [debug]
```

### 5.2 冒烟验证

```bash
export OTEL_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export OTEL_TRACES_SAMPLER=always_on
go run .
```

发一个带签名的 webhook，观察 Collector 日志：
- 应看到 `chat hunyuan-turbo-s` span 带 `gen_ai.*` 属性；
- 应看到 `execute_tool bk_alarm_query` span；
- 应看到 `gameops.webhook.requests.total` / `gameops.sse.events.total` 等 Counter 数据点。

---

## 6. D16 / D16.1 遗留 TODO

| 条目 | 状态 | 所属阶段 |
| --- | --- | --- |
| OTLP Metric Exporter 真实接入 | TODO | D17+ |
| Langfuse 对接（文档已就绪，仅缺运行环境验证） | 可立即做 | D17+ |
| 伽利略 / 智研 build tag 隔离 | TODO | D17+ |
| `Init` 阶段日志打印生效的采样器和 ratio | TODO | 轻量可即刻做 |
| SSE 事件延迟 Histogram `gameops.sse.event.duration` | TODO | 体验优化类 |
| `writeSSE` ctx 透传（替换 `context.Background()`） | TODO | 小重构 |
| AlertManager 规则模板 | ✅ D16.2 已完成（本文档 §4） | —— |
| 可观测性手册集中化 | ✅ D16.2 已完成（即本文档） | —— |

---

## 7. 版本变更

| 版本 | 日期 | 变更 |
| --- | --- | --- |
| v1 | 2026-04-21 | 初版（D16.2）：收敛 D16 + D16.1 所有环境变量 / 指标 / Span / 告警约定 |
