# GameOps Agent — 架构总览

> 本文是给**面试官 / 新人 onboarding** 用的简明架构说明。
> 详细的"为什么这样设计"答辩请看 [INTERVIEW.md](./INTERVIEW.md)。

## 一图概览

```mermaid
flowchart TB
  subgraph 入口层 [入口层 / Entry]
    HTTP[HTTP :8080<br/>SSE /v1/agent]
    AGUI[AG-UI Web<br/>/agui]
    A2A[A2A 协议<br/>tRPC RegisterToTRPC]
    Webhook[Webhook<br/>/webhook/bk_alarm<br/>/webhook/tapd]
    CLI[CLI 模式<br/>-cli]
  end

  subgraph 编排层 [编排层 / Orchestration]
    Coordinator[Coordinator Agent<br/>路由 + Plan]
    DiagAgent[Diagnosis Agent<br/>ReAct]
    RepairAgent[Repair Agent<br/>HITL]
    KnowAgent[Knowledge Agent<br/>iWiki RAG]
    FileAgent[File Analyst Agent]
  end

  subgraph 工具层 [工具层 / Tools]
    BCS[bcs_tools<br/>Pod/HPA/Helm/ConfigMap]
    BK[bk_tools<br/>告警/日志/事件/指标]
    DevOps[devops_tools<br/>流水线]
    Files[file_tools]
    Composite[composite_tools<br/>logs_unified]
    Async[async_tools]
  end

  subgraph 横切层 [横切关注 / Cross-Cutting]
    InputGuard[Input Guard<br/>注入检测]
    OutputGuard[Output Guard<br/>PII 脱敏]
    SafetyGuard[Safety Guard<br/>HITL 拦截]
    Audit[HMAC 审计链]
    Cost[Token/成本统计]
    Idem[幂等键<br/>Redis]
    Resil[pkg/resilience<br/>retry/breaker/limit]
    Obs[Observability<br/>OTel + Metrics]
  end

  subgraph 持久 [持久 / Stateful]
    Session[(Session<br/>InMem/Redis)]
    Report[(Report<br/>FileStore)]
    AuditLog[(Audit Log<br/>HMAC 链)]
  end

  subgraph 外部 [外部依赖]
    LLM[LLM API<br/>OpenAI 兼容]
    BCSAPI[BCS Cluster]
    BKAPI[BK Monitor]
    DevOpsAPI[DevOps]
    Gongfeng[Gongfeng / iWiki]
    TAPD[TAPD]
  end

  HTTP --> Coordinator
  AGUI --> Coordinator
  A2A --> Coordinator
  Webhook --> Coordinator
  CLI --> Coordinator

  Coordinator --> DiagAgent
  Coordinator --> RepairAgent
  Coordinator --> KnowAgent
  Coordinator --> FileAgent

  DiagAgent --> BCS & BK & Composite
  RepairAgent --> BCS & DevOps
  KnowAgent --> Gongfeng
  FileAgent --> Files

  BCS -.-> BCSAPI
  BK -.-> BKAPI
  DevOps -.-> DevOpsAPI

  Coordinator -.贯穿.-> InputGuard & OutputGuard & SafetyGuard
  RepairAgent --> Audit
  Coordinator --> Cost
  BCS --> Idem
  BCS --> Resil
  Coordinator --> Obs

  Coordinator --> Session
  Coordinator --> Report
  Audit --> AuditLog

  LLM <-.-> Coordinator
```

## 模块速查表

| 模块 | 路径 | 关键文件 | 职责 |
|---|---|---|---|
| 启动 | `main.go` + `src/app/app.go` | - | flag 解析、依赖装配、graceful shutdown |
| 配置 | `src/config/` | `loader.go` | YAML + env 加载，DefaultConfig + Override |
| 编排 | `src/agents/` | `coordinator/`, `react.go`, `common.go` | Coordinator + 4 子 Agent，prompt 走 system_prompt.md |
| 工具 | `src/tools/` | 6 大工具组 | 与 BCS/BK/DevOps 等外部系统交互 |
| 基础 | `src/infrastructure/` | `bcsapi/`, `bkapi/`, `devopsapi/`, `gongfengapi/`, `tapdapi/` | HTTP Client，含重试/超时/PII 处理 |
| 插件 | `src/plugin/` | `input_guard.go`, `output_guard.go`, `safety_guard.go`, `audit_hook.go` | callback 形式注入到框架的 hook 点 |
| 审计 | `src/audit/` | `hmac.go`（链式）, `remote_sink.go` | 不可篡改审计链，远端 sink 落 ES/对象存储 |
| 异步 | `src/async/` | `runner.go`, `job.go`, `store.go` | 长任务 / 高危任务异步化 |
| 会话 | `src/session/` | `session.go`（+ Redis 实现） | trpc-agent-go Session 适配，支持自动总结 |
| 幂等 | `src/idempotency/` | - | Webhook / 工具调用幂等键 |
| 弹性 | `pkg/resilience/` | `retry.go`, `breaker.go`, `bulkhead.go`, `ratelimit.go` | 通用韧性原语 |
| 知识 | `src/knowledge/` | `builder.go`, `iwiki_tool.go` | iWiki MCP 工具封装 |
| 报告 | `src/report/` | `report.go`, `summarizer.go`, `templates.go` | 故障复盘报告自动生成 |
| 观测 | `src/observability/` | `otel.go`, `metrics_*.go`, `genai_span.go`, `callbacks.go` | OTel + 自定义 Metrics + GenAI 语义约定 |
| 服务 | `src/services/` | `sse/`, `agui/`, `a2a/`, `webhook/` | 对外接入协议 |
| 评测 | `eval/` | `judge.go`, `judge_llm.go`, `judge_tool_selection.go` | LLM-as-Judge + ADK Eval |
| 命令 | `src/cmd/` | `auditverify/`, `preflight/` | 审计验签 CLI、启动前自检 CLI |

## 启动时序

```mermaid
sequenceDiagram
  autonumber
  participant U as User/Operator
  participant M as main.go
  participant C as config.Load
  participant O as observability.Init
  participant A as app.Init
  participant R as async.Runner
  participant H as http.Server

  U->>M: ./gameops-agent -addr=:8080
  M->>C: 读取 YAML + env，校验
  C-->>M: *Config
  M->>O: OTel TracerProvider/MeterProvider
  O-->>M: shutdownFn
  M->>A: 构造 Agents/Tools/Plugins/Audit/Session/Cost
  A->>R: 启动 Async Worker Pool
  A-->>M: *App
  M->>H: 注册路由 (sse/agui/webhook/healthz)
  H->>H: ListenAndServe
  Note over M,H: SIGINT/SIGTERM
  M->>H: srv.Shutdown(ctx 30s)
  M->>R: Runner.Stop（等待 inflight）
  M->>O: shutdownFn（flush traces/metrics）
  M-->>U: exit 0
```

## 单次请求时序

```mermaid
sequenceDiagram
  autonumber
  participant U as User
  participant SSE as /v1/agent (SSE)
  participant Sess as Session
  participant IG as InputGuard
  participant Co as Coordinator
  participant Sub as Sub Agent (Diag/Repair/Know/File)
  participant T as Tool
  participant SG as SafetyGuard (HITL)
  participant Ad as Audit
  participant OG as OutputGuard
  participant Ob as OTel

  U->>SSE: POST 用户问题
  SSE->>Sess: 拉历史/写新事件
  SSE->>IG: pre-model callback
  IG-->>SSE: 通过/拒绝
  SSE->>Co: runner.Run(...)
  Co->>Ob: span "agent.coordinator"
  Co->>Sub: hand off
  Sub->>T: tool call
  T->>SG: pre-tool callback（高危→HITL 暂停）
  SG-->>T: 通过 / 写入 pending_approval
  T-->>Sub: ToolResult
  Sub->>Ad: append HMAC chain
  Sub-->>Co: result
  Co->>OG: post-model callback（PII 脱敏）
  OG-->>SSE: 流式 chunk
  SSE-->>U: SSE event
  Co->>Ob: span End
```

## 部署拓扑（生产）

```mermaid
flowchart LR
  subgraph K8s [Kubernetes Cluster]
    subgraph NS [namespace: gameops]
      Ingress[Ingress / Gateway]
      Svc[Service ClusterIP]
      D[Deployment<br/>replicas=3<br/>HPA 1-10]
      P1[Pod 1]
      P2[Pod 2]
      P3[Pod 3]
      Ingress --> Svc --> D
      D --> P1 & P2 & P3
    end
    subgraph DEPS [namespace: gameops-deps]
      Redis[(Redis<br/>StatefulSet)]
      PG[(Postgres<br/>StatefulSet)]
      OTC[OTel Collector<br/>DaemonSet]
    end
  end
  subgraph EXT [外部托管]
    LF[Langfuse Cloud]
    JG[Jaeger / APM]
    PR[Prometheus / Grafana]
    KMS[(KMS<br/>HMAC Key)]
  end
  P1 & P2 & P3 -.-> Redis
  P1 & P2 & P3 -.-> OTC
  OTC --> LF & JG & PR
  P1 & P2 & P3 -.HMAC Key.-> KMS
```

## 关键设计决策（决策日志）

| # | 决策 | 替代方案 | 选用理由 |
|---|---|---|---|
| 1 | 使用 trpc-agent-go 而非自研 | LangGraph / 自研 | 公司基础设施一等公民，原生 OTel + A2A + AG-UI |
| 2 | Coordinator + 子 Agent 而非单 Agent | 大 prompt 单 Agent | 子 Agent 各自 prompt 短，便于评测与回归 |
| 3 | HITL 用 pending state + 异步等待 | 同步阻塞工具调用 | 不阻塞 worker，支持长审批时间 |
| 4 | HMAC 链式审计 | 普通 append-only | 防篡改，可单点验签 |
| 5 | Session 默认 in-mem，Redis 可选 | 强制 Redis | 本地零依赖可跑，生产可切换 |
| 6 | Plugin 走 callback 而非 middleware | middleware | 框架原生扩展点，与 OTel callback 共享生命周期 |
| 7 | 工具白名单 + 集群级 RBAC | 仅工具级 | 多 cluster 环境必需 |
| 8 | A2A/AG-UI 用 build tag stub/real | 永远依赖 | 外网 / 离线 CI 可编译 |
