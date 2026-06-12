# 08 可观测性体系详解（Langfuse / OTel / Prometheus / Grafana）

> **文档定位**：深度解析 `project-llm` 可观测性层（Observability Layer）的完整实现，覆盖 LLM 调用链追踪、GenAI 语义约定、指标采集、可视化面板四大维度。
>
> **前置阅读**：[00_INDEX.md](./00_INDEX.md)（项目总览）→ [05_RAG_SYSTEM.md](./05_RAG_SYSTEM.md)（RAG 系统）→ [06_AGENT_INTEGRATION.md](./06_AGENT_INTEGRATION.md)（Agent 集成）

---

## 一、可观测性架构总览

### 1.1 三支柱模型

```mermaid
graph TD
    subgraph "支柱 1: Traces（调用链）"
        T1[Langfuse] --> T2[LLM 调用链追踪]
        T1 --> T3[Session 关联]
        T1 --> T4[在线评估集成]
    end

    subgraph "支柱 2: Metrics（指标）"
        M1[Prometheus] --> M2[RAG QPS / 延迟]
        M1 --> M3[vLLM 吞吐 / KV-Cache]
        M1 --> M4[Qdrant 向量库指标]
    end

    subgraph "支柱 3: Logs（日志 + 语义）"
        L1[OpenTelemetry] --> L2[GenAI Semantic Conv v1.30]
        L1 --> L3[PII 脱敏]
        L1 --> L4[采样策略]
    end

    T2 --> V[Grafana 可视化]
    M2 --> V
    L2 --> V
    V --> A[告警 + 决策]
```

### 1.2 组件矩阵

| 组件 | 路径 | 职责 | 版本要求 |
|------|------|------|---------|
| **Langfuse** | `observability/langfuse_tracing.py` | LLM 调用链追踪 + 在线评估 | ≥2.60.0 |
| **OpenTelemetry** | `observability/otel_genai_config.yaml` | GenAI 语义约定 + OTLP 导出 | SDK ≥1.27.0 |
| **Prometheus** | `observability/prometheus.yml` | 时序指标采集 | v2.54.1 |
| **Grafana** | `observability/grafana_dashboard.json` | 可视化面板 | 11.2.2 |
| **Docker Compose** | `observability/docker-compose.obs.yaml` | 一键编排观测栈 | — |

### 1.3 数据流全景

```mermaid
graph LR
    subgraph "应用层"
        RAG[rag_serve.py<br/>RAG 服务]
        VLLM[vLLM V1<br/>推理引擎]
        AGENT[GameOps Agent<br/>Go 服务]
    end

    subgraph "采集层"
        LF[Langfuse Client<br/>Python SDK]
        OTEL[OTel Collector<br/>OTLP HTTP]
        PROM_C[prometheus_client<br/>Python 库]
    end

    subgraph "存储层"
        LF_DB[(Langfuse<br/>PostgreSQL)]
        PROM_S[(Prometheus<br/>TSDB)]
    end

    subgraph "展示层"
        LF_UI[Langfuse UI<br/>:3000]
        GF[Grafana<br/>:3001]
        PROM_UI[Prometheus UI<br/>:9090]
    end

    RAG -->|trace_scope| LF
    RAG -->|Counter/Histogram| PROM_C
    VLLM -->|/metrics| PROM_S
    AGENT -->|session_id| LF

    LF --> LF_DB
    LF -->|OTLP| OTEL
    PROM_C -->|/metrics 拉取| PROM_S

    LF_DB --> LF_UI
    PROM_S --> GF
    PROM_S --> PROM_UI
```

---

## 二、Langfuse 调用链追踪

### 2.1 设计原则

| 原则 | 实现方式 |
|------|---------|
| **零侵入降级** | 未配置 `LANGFUSE_*` 环境变量时，所有装饰器降级为 no-op，绝不影响主流程 |
| **隐私保护** | `REDACT_PROMPT=1` 时脱敏 prompt 内容，只记录长度 |
| **端到端关联** | 通过 `session_id` 串联 Agent → RAG → LLM 全链路 |
| **OTel 兼容** | Langfuse client 同时上报 OTLP，与 OpenTelemetry 生态打通 |

### 2.2 核心代码走读：`langfuse_tracing.py`

#### 2.2.1 懒初始化模式

```python
_CLIENT: Any = None
_INIT_DONE: bool = False

def init_langfuse() -> Any | None:
    """懒初始化 Langfuse client；未配置环境变量时返回 None。"""
    global _CLIENT, _INIT_DONE
    if _INIT_DONE:
        return _CLIENT
    _INIT_DONE = True

    pk = os.getenv("LANGFUSE_PUBLIC_KEY")
    sk = os.getenv("LANGFUSE_SECRET_KEY")
    if not (pk and sk):
        print("[langfuse] 未配置 LANGFUSE_PUBLIC_KEY/SECRET_KEY，观测降级为 no-op")
        return None

    try:
        from langfuse import Langfuse
    except ImportError:
        print("[langfuse] 未安装 langfuse 包，观测降级为 no-op。pip install langfuse")
        return None

    _CLIENT = Langfuse(
        public_key=pk,
        secret_key=sk,
        host=os.getenv("LANGFUSE_HOST", "https://cloud.langfuse.com"),
    )
    return _CLIENT
```

**设计要点**：
- 单例模式 + `_INIT_DONE` 标志位，避免重复初始化
- 三层降级：环境变量缺失 → 包未安装 → 正常初始化
- 延迟 import `langfuse`，不影响未安装该包的环境

#### 2.2.2 通用 Trace 上下文管理器

```python
@contextmanager
def trace_scope(name: str, *, user_id: str | None = None,
                session_id: str | None = None, metadata: dict | None = None):
    """
    创建一个 trace 上下文；未配置 Langfuse 时返回 no-op object。
    """
    client = init_langfuse()
    if client is None:
        class _NoOp:
            def update(self, **_): pass
            def span(self, **kwargs): return _NoOp()
            def generation(self, **kwargs): return _NoOp()
            def end(self, **_): pass
        yield _NoOp()
        return

    trace = client.trace(
        name=name, user_id=user_id, session_id=session_id,
        metadata=metadata or {},
    )
    try:
        yield trace
    finally:
        try:
            client.flush()
        except Exception:
            pass
```

**设计要点**：
- `_NoOp` 类实现鸭子类型，所有方法返回自身或空操作
- `finally` 中 `flush()` 确保 trace 数据不丢失
- 异常安全：flush 失败不影响业务逻辑

#### 2.2.3 RAG 专用装饰器

```python
def observe_rag(fn: Callable) -> Callable:
    """装饰 RAG 主入口函数，自动记录 query/answer/latency/citations。"""
    @functools.wraps(fn)
    async def wrapper(*args, **kwargs):
        query = kwargs.get("query") or (args[0] if args else "")
        session_id = kwargs.get("session_id") or uuid.uuid4().hex[:12]
        t0 = time.perf_counter()
        with trace_scope("rag_query", session_id=session_id,
                         metadata={"query_len": len(query or "")}) as tr:
            try:
                result = await fn(*args, **kwargs)
            except Exception as e:
                tr.update(level="ERROR", status_message=str(e))
                raise
            latency_ms = int((time.perf_counter() - t0) * 1000)
            meta_out = {}
            if isinstance(result, dict):
                meta_out = {
                    "latency_ms": latency_ms,
                    "n_citations": len(result.get("citations") or []),
                    "trace_id": result.get("trace_id"),
                }
            tr.update(
                input=_redact(query),
                output=_redact(str(result.get("answer", ""))),
                metadata=meta_out,
            )
            return result
    return wrapper
```

**记录字段**：

| 字段 | 说明 |
|------|------|
| `input` | 用户 query（受 REDACT_PROMPT 控制脱敏） |
| `output` | 生成答案（截断至 200 字符） |
| `latency_ms` | 端到端延迟 |
| `n_citations` | 引用数量 |
| `trace_id` | 唯一追踪 ID |
| `level` | ERROR 时标记异常 |

#### 2.2.4 训练观测装饰器

```python
def observe_train(stage: str = "sft"):
    """装饰训练 step 函数，记录 loss / lr / step 等指标。"""
    def deco(fn: Callable) -> Callable:
        @functools.wraps(fn)
        def wrapper(*args, **kwargs):
            client = init_langfuse()
            t0 = time.perf_counter()
            result = fn(*args, **kwargs)
            if client and isinstance(result, dict):
                try:
                    client.event(
                        name=f"train_step:{stage}",
                        metadata={
                            "latency_ms": int((time.perf_counter() - t0) * 1000),
                            **{k: v for k, v in result.items()
                                if isinstance(v, (int, float, str))},
                        },
                    )
                except Exception:
                    pass
            return result
        return wrapper
    return deco
```

**使用方式**：
```python
@observe_train(stage="sft")
def step(batch):
    # ... 训练逻辑 ...
    return {"loss": 0.42, "lr": 5e-5, "step": 100}
```

#### 2.2.5 Agent 关联

```python
def link_agent_trace(session_id: str, agent_trace_id: str,
                      extra: dict | None = None) -> None:
    """
    Agent 侧透传 session_id 后，登记关联事件。
    Langfuse UI 通过 session 视图看到 Agent ↔ RAG ↔ 训练 全链路。
    """
    client = init_langfuse()
    if client is None:
        return
    client.event(
        name="agent_trace_link",
        metadata={"agent_trace_id": agent_trace_id, **(extra or {})},
        session_id=session_id,
    )
```

#### 2.2.6 Prompt 脱敏

```python
def _redact(text: str, max_len: int = 200) -> str:
    """按 REDACT_PROMPT 开关脱敏"""
    if os.getenv("REDACT_PROMPT") == "1":
        return f"[REDACTED, len={len(text)}]"
    if len(text) > max_len:
        return text[:max_len] + f"...[+{len(text)-max_len}]"
    return text
```

### 2.3 RAG 服务中的 Langfuse 集成

`deploy/rag_serve.py` 中的实际使用：

```python
# 导入（带降级）
try:
    from observability.langfuse_tracing import init_langfuse, trace_scope
except Exception:
    init_langfuse = lambda: None
    @_cm
    def trace_scope(*_a, **_kw):
        class _N:
            def update(self, **_): pass
        yield _N()

# 应用启动时初始化
@asynccontextmanager
async def lifespan(app: FastAPI):
    # ...
    init_langfuse()
    yield

# 路由中使用
@app.post("/rag/query")
async def rag_query(req: RAGRequest):
    with trace_scope("rag_query", session_id=session_id,
                     metadata={"query_len": len(req.query)}) as tr:
        # ... 检索 + 生成 ...
        tr.update(
            input=req.query,
            output=answer[:500],
            metadata={
                "latency_ms": latency_ms,
                "n_citations": len(citations),
                "top_score": citations[0].score,
                "trace_id": trace_id,
            },
        )
```

### 2.4 Langfuse 部署方式

| 方式 | 说明 | 适用场景 |
|------|------|---------|
| **自托管** | `docker-compose.obs.yaml` 中 langfuse + postgres | 内网环境 / 数据合规 |
| **SaaS** | https://cloud.langfuse.com | 快速 Demo / Hobby 免费额度 |

**自托管配置**（`docker-compose.obs.yaml`）：
```yaml
langfuse:
  image: langfuse/langfuse:2
  depends_on:
    langfuse_db:
      condition: service_healthy
  ports:
    - "3000:3000"
  environment:
    DATABASE_URL: postgresql://langfuse:langfuse@langfuse_db:5432/langfuse
    NEXTAUTH_URL: http://localhost:3000
    NEXTAUTH_SECRET: gameops-langfuse-secret
    SALT: gameops-langfuse-salt
    TELEMETRY_ENABLED: "false"
    LANGFUSE_ENABLE_EXPERIMENTAL_FEATURES: "true"
```

### 2.5 OpenAI 兼容 API 接入方式

```python
from langfuse.openai import OpenAI   # 关键：import 自 langfuse.openai

client = OpenAI(
    base_url="http://localhost:8000/v1",   # 指向 vLLM / SGLang
    api_key="sk-xxx",
)

resp = client.chat.completions.create(
    model="knowledge-expert",
    messages=[{"role": "user", "content": "routesvr 的四种路由模式？"}],
    metadata={
        "trace_name": "knowledge-qa",
        "tags": ["online", "gameops"],
        "user_id": "engineer_001",
    },
)
```

> **原理**：`langfuse.openai.OpenAI` 是对 `openai.OpenAI` 的猴子补丁包装，自动拦截所有 API 调用并上报 trace，零代码改动即可接入。

### 2.6 Agentic RAG 的 Trace 设计

```python
from langfuse.decorators import observe

@observe(name="agentic-rag")
def agentic_answer(question: str):
    @observe(name="classify-query")
    def classify(q): ...             # 分类：高频 QA / 长尾检索 / 多跳

    @observe(name="retrieve")
    def retrieve(q, k=5): ...        # 检索工具

    @observe(name="llm-generate")
    def generate(q, ctx): ...        # LLM 生成

    cls = classify(question)
    ctx = retrieve(question) if cls != "direct" else []
    return generate(question, ctx)
```

**Langfuse UI 展示效果**：
```
agentic-rag (trace)
├── classify-query (span, 15ms)
├── retrieve (span, 120ms)
│   ├── dense_search (event)
│   └── rerank (event)
└── llm-generate (generation, 650ms)
    ├── input_tokens: 1200
    └── output_tokens: 350
```

---

## 三、OpenTelemetry GenAI 语义约定

### 3.1 配置文件详解：`otel_genai_config.yaml`

#### 3.1.1 服务标识

```yaml
service:
  name: project-llm
  namespace: finetune
  version: "1.0.0"

resource:
  attributes:
    deployment.environment: "dev"        # dev / staging / prod
    gen_ai.system: "qwen3"
    gen_ai.operation.name: "chat"
```

#### 3.1.2 OTLP 导出配置

```yaml
exporters:
  otlp:
    endpoint: "${OTEL_EXPORTER_OTLP_ENDPOINT:-http://localhost:4318}"
    protocol: http/protobuf
    headers:
      x-service: project-llm
```

- 支持环境变量 `${VAR:-default}` 语法
- 默认使用 HTTP/Protobuf 协议（比 gRPC 更易穿透防火墙）

#### 3.1.3 批处理 + PII 脱敏

```yaml
processors:
  batch:
    timeout: 5s
    send_batch_size: 512
  redact_pii:
    enable: true
    fields:
      - gen_ai.prompt
      - gen_ai.completion
```

- 批量发送减少网络开销
- PII 脱敏处理 prompt 和 completion 字段

### 3.2 GenAI Semantic Convention v1.30 字段规范

| 级别 | 字段 | 说明 |
|------|------|------|
| **必选** | `gen_ai.system` | 模型系统标识（qwen3 / deepseek / moonshot） |
| **必选** | `gen_ai.request.model` | 具体模型名（qwen3-8b / qwen3-4b） |
| **必选** | `gen_ai.operation.name` | 操作类型（chat / completion / embedding） |
| **推荐** | `gen_ai.request.temperature` | 采样温度 |
| **推荐** | `gen_ai.request.top_p` | Top-P 采样 |
| **推荐** | `gen_ai.request.max_tokens` | 最大生成 token 数 |
| **推荐** | `gen_ai.response.id` | 响应唯一 ID |
| **推荐** | `gen_ai.response.finish_reasons` | 结束原因（stop / length / tool_calls） |
| **推荐** | `gen_ai.usage.input_tokens` | 输入 token 数 |
| **推荐** | `gen_ai.usage.output_tokens` | 输出 token 数 |
| **可选** | `gen_ai.prompt` | 完整 prompt（默认关闭，需合规确认） |
| **可选** | `gen_ai.completion` | 完整生成内容 |

### 3.3 Span 关系约定

```yaml
span_kinds:
  rag:
    - name: "rag.pipeline"
      kind: "internal"
      children:
        - name: "rag.retrieve"
          attributes: [rag.query, rag.top_k, rag.reranked]
        - name: "rag.generate"
          attributes: [gen_ai.*]
  agentic:
    - name: "agent.step"
      kind: "internal"
      attributes:
        - agent.tool_name
        - agent.iteration
```

**Span 层级关系**：
```
rag.pipeline (parent)
├── rag.retrieve (child)
│   ├── rag.query = "routesvr 路由模式"
│   ├── rag.top_k = 5
│   └── rag.reranked = true
└── rag.generate (child)
    ├── gen_ai.request.model = "qwen3-8b"
    ├── gen_ai.usage.input_tokens = 1200
    └── gen_ai.usage.output_tokens = 350
```

### 3.4 采样策略

```yaml
sampling:
  default: 1.0                           # 开发阶段 100% 采样
  production:
    ratio: 0.1                           # 生产 10%
    tail_based:
      - condition: "gen_ai.response.finish_reasons=='error'"
        ratio: 1.0                       # 错误请求全采
```

| 环境 | 策略 | 说明 |
|------|------|------|
| **开发** | 100% 采样 | 全量追踪，便于调试 |
| **生产** | 10% 基础 + 尾部采样 | 降低开销，错误请求全采 |

### 3.5 依赖包

```
opentelemetry-sdk>=1.27.0
opentelemetry-instrumentation-openai>=0.35.0  # OTel GenAI Semantic Conv v1.30
```

> **关键**：`opentelemetry-instrumentation-openai` 自动拦截 OpenAI 兼容 API 调用（包括 vLLM / SGLang），无需手动埋点。

---

## 四、Prometheus 指标采集

### 4.1 指标定义（`rag_serve.py`）

```python
from prometheus_client import Counter, Histogram, CONTENT_TYPE_LATEST, generate_latest

# 请求计数器（按端点 + 状态分标签）
RAG_REQ = Counter("rag_requests_total", "RAG requests", ["endpoint", "status"])

# 端到端延迟直方图（自定义 bucket 边界）
RAG_LAT = Histogram(
    "rag_latency_seconds", "RAG end-to-end latency",
    ["endpoint"],
    buckets=(0.1, 0.3, 0.5, 1, 2, 3, 5, 8, 13, 21),
)

# 引用数量直方图
RAG_CIT = Histogram(
    "rag_citation_count", "citations per query",
    buckets=(0, 1, 2, 3, 5, 8, 13),
)

# 检索 chunk 计数器
RAG_RETRIEVED = Counter("rag_retrieved_chunks_total", "chunks retrieved")
```

### 4.2 指标埋点方式

```python
@app.post("/rag/query")
async def rag_query(req: RAGRequest):
    t0 = time.perf_counter()
    try:
        chunks = await asyncio.to_thread(RETRIEVER.search, req.query)
        RAG_RETRIEVED.inc(len(chunks))

        if not chunks:
            RAG_REQ.labels("rag_query", "empty").inc()
            RAG_LAT.labels("rag_query").observe(latency / 1000)
            return ...

        # ... 生成逻辑 ...
        RAG_REQ.labels("rag_query", "ok").inc()
        RAG_LAT.labels("rag_query").observe(latency / 1000)
        RAG_CIT.observe(len(citations))
    except Exception:
        RAG_REQ.labels("rag_query", "error").inc()
        raise
```

**标签设计**：

| 指标 | 标签 | 值 |
|------|------|-----|
| `rag_requests_total` | endpoint | `rag_query` / `chat_completions` |
| `rag_requests_total` | status | `ok` / `empty` / `error` |
| `rag_latency_seconds` | endpoint | `rag_query` / `chat_completions` |

### 4.3 指标暴露端点

```python
@app.get("/metrics")
async def metrics():
    if not _PROM:
        return Response("prometheus_client not installed", media_type="text/plain")
    return Response(generate_latest(), media_type=CONTENT_TYPE_LATEST)
```

- 路径：`GET /metrics`
- 格式：Prometheus text exposition format
- 降级：未安装 `prometheus-client` 时返回纯文本提示

### 4.4 Prometheus 抓取配置

#### 观测栈配置（`observability/prometheus.yml`）

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s
  external_labels:
    project: "gameops-llm"
    env: "dev"

scrape_configs:
  # RAG 服务（自定义指标）
  - job_name: "rag_serve"
    metrics_path: /metrics
    static_configs:
      - targets: ["rag_serve:8100"]
        labels: { service: "rag" }

  # vLLM（V1 原生暴露 /metrics）
  - job_name: "vllm_expert"
    metrics_path: /metrics
    static_configs:
      - targets: ["vllm_expert:8000"]
        labels: { service: "vllm", model: "qwen3-8b-knowledge-sft" }

  # Qdrant（自带 :6333/metrics）
  - job_name: "qdrant"
    metrics_path: /metrics
    static_configs:
      - targets: ["qdrant:6333"]
        labels: { service: "vector_db" }

  # Node Exporter（可选）
  - job_name: "node"
    static_configs:
      - targets: ["node_exporter:9100"]
        labels: { service: "host" }
```

### 4.5 vLLM 原生指标

vLLM V1 引擎自动暴露以下关键指标（无需额外配置）：

| 指标 | 类型 | 说明 |
|------|------|------|
| `vllm:num_requests_running` | Gauge | 当前运行中的请求数 |
| `vllm:num_requests_waiting` | Gauge | 等待队列中的请求数 |
| `vllm:prompt_tokens_total` | Counter | 累计输入 token 数 |
| `vllm:generation_tokens_total` | Counter | 累计生成 token 数 |
| `vllm:gpu_cache_usage_perc` | Gauge | GPU KV-Cache 使用率 |
| `vllm:cpu_cache_usage_perc` | Gauge | CPU KV-Cache 使用率 |
| `vllm:time_to_first_token_seconds` | Histogram | TTFT 分布 |
| `vllm:time_per_output_token_seconds` | Histogram | TPOT 分布 |

### 4.6 Qdrant 原生指标

Qdrant 在 `:6333/metrics` 暴露：

| 指标 | 说明 |
|------|------|
| `qdrant_collections_total` | 集合总数 |
| `qdrant_points_total` | 向量点总数 |
| `qdrant_search_duration_seconds` | 搜索延迟分布 |
| `qdrant_grpc_responses_total` | gRPC 响应计数 |

---

## 五、Grafana 可视化面板

### 5.1 Dashboard 布局

`observability/grafana_dashboard.json` 定义了 **GameOps LLM Observability** 面板：

```
┌─────────────────────────────────────────────────────────────────────┐
│  Row 1: 核心指标概览（Stat Panels）                                   │
├────────────┬────────────┬────────────┬────────────────────────────────┤
│ RAG QPS    │ P95 延迟   │ Error Rate │ vLLM Running Requests          │
│ (stat)     │ (stat, s)  │ (stat, %)  │ (stat)                         │
├────────────┴────────────┴────────────┴────────────────────────────────┤
│  Row 2: 时序趋势图                                                    │
├──────────────────────────────┬────────────────────────────────────────┤
│ RAG Latency Distribution     │ vLLM Token Throughput (tokens/s)       │
│ P50 / P95 / P99             │ prompt / generation                    │
├──────────────────────────────┼────────────────────────────────────────┤
│ Citations per Query          │ vLLM KV-Cache Usage                    │
│ P50 / P95                   │ GPU cache / CPU cache                  │
└──────────────────────────────┴────────────────────────────────────────┘
```

### 5.2 关键 PromQL 查询

| 面板 | PromQL 表达式 |
|------|--------------|
| **RAG QPS** | `sum(rate(rag_requests_total[1m]))` |
| **P95 延迟** | `histogram_quantile(0.95, sum(rate(rag_latency_seconds_bucket[5m])) by (le))` |
| **Error Rate** | `100 * sum(rate(rag_requests_total{status="error"}[5m])) / clamp_min(sum(rate(rag_requests_total[5m])), 0.001)` |
| **vLLM Running** | `vllm:num_requests_running` |
| **Token 吞吐** | `rate(vllm:prompt_tokens_total[1m])` / `rate(vllm:generation_tokens_total[1m])` |
| **KV-Cache** | `vllm:gpu_cache_usage_perc` / `vllm:cpu_cache_usage_perc` |
| **Citations P50** | `histogram_quantile(0.50, sum(rate(rag_citation_count_bucket[5m])) by (le))` |

### 5.3 告警阈值

| 指标 | 黄色告警 | 红色告警 |
|------|---------|---------|
| RAG P95 延迟 | ≥ 2s | ≥ 5s |
| Error Rate | ≥ 1% | ≥ 5% |
| GPU KV-Cache | ≥ 80% | ≥ 95% |

### 5.4 Grafana Provisioning 配置

#### 数据源自动配置（`grafana_provisioning/datasources/prometheus.yaml`）

```yaml
apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    uid: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: true
```

#### Dashboard 自动加载（`grafana_provisioning/dashboards/default.yaml`）

```yaml
apiVersion: 1
providers:
  - name: "gameops"
    orgId: 1
    folder: ""
    type: file
    disableDeletion: false
    updateIntervalSeconds: 30
    allowUiUpdates: true
    options:
      path: /var/lib/grafana/dashboards
```

---

## 六、Docker Compose 服务编排

### 6.1 完整编排文件：`docker-compose.obs.yaml`

```yaml
version: "3.9"

services:
  # Langfuse PostgreSQL
  langfuse_db:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: langfuse
      POSTGRES_PASSWORD: langfuse
      POSTGRES_DB: langfuse
    volumes:
      - ../output/obs/langfuse_db:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U langfuse"]
      interval: 5s
      retries: 10

  # Langfuse Server（自托管 v2）
  langfuse:
    image: langfuse/langfuse:2
    depends_on:
      langfuse_db: { condition: service_healthy }
    ports: ["3000:3000"]
    environment:
      DATABASE_URL: postgresql://langfuse:langfuse@langfuse_db:5432/langfuse
      NEXTAUTH_URL: http://localhost:3000
      NEXTAUTH_SECRET: gameops-langfuse-secret

  # Prometheus
  prometheus:
    image: prom/prometheus:v2.54.1
    ports: ["9090:9090"]
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - ../output/obs/prom_data:/prometheus
    command:
      - "--config.file=/etc/prometheus/prometheus.yml"
      - "--storage.tsdb.retention.time=7d"

  # Grafana
  grafana:
    image: grafana/grafana:11.2.2
    ports: ["3001:3000"]
    environment:
      GF_SECURITY_ADMIN_USER: admin
      GF_SECURITY_ADMIN_PASSWORD: admin
    volumes:
      - ../output/obs/grafana_data:/var/lib/grafana
      - ./grafana_provisioning:/etc/grafana/provisioning:ro
      - ./grafana_dashboard.json:/var/lib/grafana/dashboards/gameops.json:ro
    depends_on: [prometheus]

networks:
  default:
    name: gameops-obs
```

### 6.2 服务访问入口

| 服务 | 端口 | 默认凭据 |
|------|------|---------|
| Langfuse UI | http://localhost:3000 | 首次注册即 admin |
| Grafana | http://localhost:3001 | admin / admin |
| Prometheus | http://localhost:9090 | 无需认证 |

### 6.3 一键启动脚本

```bash
#!/usr/bin/env bash
# scripts/run_observability.sh

# [1/3] 启动观测栈
docker compose -f observability/docker-compose.obs.yaml up -d

# [2/3] 等待服务就绪
for i in 1 2 3 4 5 6; do
    if curl -sf http://localhost:9090/-/ready >/dev/null 2>&1 \
       && curl -sf http://localhost:3001/api/health >/dev/null 2>&1; then
        echo "✅ 就绪"
        break
    fi
    echo "  waiting... ($i/6)"; sleep 5
done

# [3/3] 输出访问入口
echo "🧠 Langfuse    → http://localhost:3000"
echo "📊 Grafana     → http://localhost:3001"
echo "📈 Prometheus  → http://localhost:9090"
```

---

## 七、端到端链路关联

### 7.1 全链路追踪架构

```mermaid
sequenceDiagram
    participant Agent as GameOps Agent (Go)
    participant RAG as rag_serve.py
    participant LLM as vLLM / Qwen3-8B
    participant LF as Langfuse
    participant Prom as Prometheus

    Agent->>RAG: POST /rag/query<br/>{session_id: "abc123"}
    activate RAG

    RAG->>LF: trace_scope("rag_query", session_id="abc123")
    RAG->>RAG: Retriever.search() → chunks
    RAG->>Prom: RAG_RETRIEVED.inc(5)
    RAG->>LLM: chat.completions.create()
    LLM-->>RAG: answer + tokens
    RAG->>Prom: RAG_REQ.labels("ok").inc()
    RAG->>Prom: RAG_LAT.observe(0.8)
    RAG->>LF: tr.update(input, output, metadata)

    deactivate RAG
    RAG-->>Agent: {answer, citations, trace_id}

    Agent->>LF: link_agent_trace(session_id, agent_trace_id)

    Note over LF: Session 视图：<br/>Agent → RAG → LLM 全链路
```

### 7.2 Session 关联机制

| 层级 | 标识 | 传递方式 |
|------|------|---------|
| Agent | `agent_trace_id` | Go Agent 内部生成 |
| RAG | `session_id` | 请求参数透传 |
| LLM | `trace_id` | Langfuse 自动关联 |

**关联代码**：
```python
# Agent 侧调用 RAG 后，回调关联
link_agent_trace(
    session_id="abc123",
    agent_trace_id="go-agent-trace-xyz",
    extra={"tool": "knowledge_query", "iteration": 2}
)
```

### 7.3 Langfuse Session 视图效果

```
Session: abc123
├── [Agent] agent_trace_link
│   └── agent_trace_id: go-agent-trace-xyz
│       tool: knowledge_query
├── [RAG] rag_query (800ms)
│   ├── input: "routesvr 的四种路由模式？"
│   ├── output: "routesvr 支持..."
│   ├── n_citations: 3
│   └── top_score: 0.87
└── [LLM] generation (650ms)
    ├── model: qwen3-8b-knowledge-sft
    ├── input_tokens: 1200
    └── output_tokens: 350
```

---

## 八、依赖框架详解

### 8.1 核心依赖矩阵

| 包名 | 版本要求 | 用途 | 是否必须 |
|------|---------|------|---------|
| `langfuse` | ≥2.60.0 | LLM 调用链追踪 + 在线评估 | 可选（降级为 no-op） |
| `opentelemetry-sdk` | ≥1.27.0 | OTel 核心 SDK | 可选 |
| `opentelemetry-instrumentation-openai` | ≥0.35.0 | GenAI Semantic Conv v1.30 自动埋点 | 可选 |
| `prometheus-client` | ≥0.21.0 | Python Prometheus 指标库 | 可选（降级为无指标） |
| `prom/prometheus` | v2.54.1 | 时序数据库 + 抓取引擎 | Docker 镜像 |
| `grafana/grafana` | 11.2.2 | 可视化面板 | Docker 镜像 |
| `langfuse/langfuse` | v2 | 自托管 Trace 服务 | Docker 镜像 |
| `postgres` | 16-alpine | Langfuse 后端存储 | Docker 镜像 |

### 8.2 Langfuse SDK 核心 API

| API | 说明 |
|-----|------|
| `Langfuse(public_key, secret_key, host)` | 初始化客户端 |
| `client.trace(name, session_id, user_id, metadata)` | 创建 trace |
| `trace.span(name, metadata)` | 创建子 span |
| `trace.generation(name, model, input, output, usage)` | 记录 LLM 生成 |
| `trace.update(input, output, metadata, level)` | 更新 trace 属性 |
| `client.event(name, metadata, session_id)` | 记录独立事件 |
| `client.flush()` | 强制刷新缓冲区 |

### 8.3 prometheus-client 核心 API

| API | 说明 |
|-----|------|
| `Counter(name, doc, labels)` | 只增计数器 |
| `counter.labels(...).inc(n)` | 按标签递增 |
| `Histogram(name, doc, labels, buckets)` | 直方图（分布） |
| `histogram.labels(...).observe(value)` | 记录观测值 |
| `Gauge(name, doc, labels)` | 可增可减仪表 |
| `generate_latest()` | 生成 Prometheus 格式文本 |

### 8.4 OpenTelemetry 核心概念

| 概念 | 说明 |
|------|------|
| **Resource** | 服务标识（name / version / environment） |
| **Tracer** | 创建 Span 的工厂 |
| **Span** | 一次操作的时间段（含属性、事件、状态） |
| **Exporter** | 数据导出目标（OTLP / Jaeger / Console） |
| **Processor** | 数据处理管道（Batch / PII Redact） |
| **Sampler** | 采样策略（Always / Ratio / TailBased） |

---

## 九、环境变量参考

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `LANGFUSE_PUBLIC_KEY` | — | Langfuse 公钥（未设置则降级） |
| `LANGFUSE_SECRET_KEY` | — | Langfuse 私钥（未设置则降级） |
| `LANGFUSE_HOST` | `https://cloud.langfuse.com` | Langfuse 服务地址 |
| `LANGFUSE_PROJECT` | `default` | 项目名称标识 |
| `REDACT_PROMPT` | `0` | `1` 时脱敏 prompt 内容 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` | OTel 导出端点 |

---

## 十、最佳实践与注意事项

### 10.1 生产环境清单

| 项目 | 开发环境 | 生产环境 |
|------|---------|---------|
| Langfuse 部署 | SaaS / 本地 Docker | 自托管 + 独立 PostgreSQL |
| 采样率 | 100% | 10% + 尾部采样 |
| Prompt 脱敏 | 关闭 | `REDACT_PROMPT=1` |
| Prometheus 保留 | 7d | 30d + 远程存储 |
| Grafana 告警 | 关闭 | 接入 PagerDuty / 企微 |
| 网络隔离 | 同一 Docker 网络 | 独立 VPC + mTLS |

### 10.2 性能影响评估

| 组件 | 额外延迟 | 额外内存 | 说明 |
|------|---------|---------|------|
| Langfuse SDK | < 1ms（异步） | ~10MB | 后台批量上报 |
| prometheus-client | < 0.1ms | ~5MB | 内存中累加 |
| OTel SDK | < 2ms | ~15MB | 批处理 + 异步导出 |

### 10.3 故障降级策略

```mermaid
graph TD
    A[观测组件异常] --> B{哪个组件?}
    B -->|Langfuse| C[trace_scope 返回 NoOp<br/>业务零影响]
    B -->|Prometheus| D[/metrics 返回纯文本提示<br/>Grafana 面板空白]
    B -->|Grafana| E[不影响数据采集<br/>仅可视化不可用]
    B -->|OTel Collector| F[SDK 本地缓存<br/>恢复后重传]
```

---

## 十一、面试要点

### 11.1 高频问题

| 问题 | 要点 |
|------|------|
| **为什么选 Langfuse 而不是 LangSmith？** | 开源自托管、数据合规、支持 OpenAI 兼容 API 自动埋点、与 OTel 生态兼容 |
| **如何保证观测不影响主流程？** | 三层降级（env 缺失 → 包未装 → 异常捕获）、异步上报、NoOp 模式 |
| **GenAI Semantic Convention 是什么？** | OTel 社区定义的 LLM 可观测性标准字段（v1.30），统一 gen_ai.* 命名空间 |
| **Prometheus 的 Histogram bucket 如何设计？** | 基于业务 SLA（P95 < 3s），使用斐波那契数列式 bucket：0.1/0.3/0.5/1/2/3/5/8/13/21 |
| **如何实现端到端链路追踪？** | session_id 透传 + link_agent_trace 关联 Agent/RAG/LLM 三层 |

### 11.2 架构设计亮点

1. **零侵入设计**：所有观测代码通过装饰器 / 上下文管理器注入，业务代码无感知
2. **优雅降级**：任何观测组件故障都不影响核心业务
3. **标准化**：遵循 OTel GenAI Semantic Convention v1.30，与业界生态兼容
4. **全链路**：Agent → RAG → LLM 通过 session_id 串联，Langfuse 一站式查看
5. **自动化**：Grafana Provisioning 自动加载数据源 + Dashboard，无需手动配置

### 11.3 可扩展方向

| 方向 | 说明 |
|------|------|
| 接入 Jaeger | OTel Collector 多后端导出 |
| 成本追踪 | 按 token 计费 + 按用户/团队聚合 |
| A/B 实验 | 通过 Langfuse tag 区分实验组 |
| 自动告警 | Grafana Alerting → 企微/飞书 |
| 长期存储 | Prometheus Remote Write → Thanos / Mimir |

---

## 十二、文件清单

| 文件 | 路径 | 说明 |
|------|------|------|
| Langfuse 埋点库 | `observability/langfuse_tracing.py` | 统一 Langfuse 埋点工具（222 行） |
| OTel 配置 | `observability/otel_genai_config.yaml` | GenAI Semantic Conv v1.30 配置 |
| Prometheus 配置 | `observability/prometheus.yml` | 抓取 RAG/vLLM/Qdrant 指标 |
| Grafana Dashboard | `observability/grafana_dashboard.json` | 8 面板可视化 |
| Docker Compose | `observability/docker-compose.obs.yaml` | 观测栈一键编排 |
| Grafana 数据源 | `observability/grafana_provisioning/datasources/prometheus.yaml` | 自动配置 Prometheus |
| Grafana Dashboard 加载 | `observability/grafana_provisioning/dashboards/default.yaml` | 自动加载 JSON |
| Langfuse 接入文档 | `observability/langfuse_setup.md` | 部署 + 代码接入指南 |
| 启动脚本 | `scripts/run_observability.sh` | 一键启动 + 健康检查 |

---

> **下一篇**：[09_AI_INFRA.md](./09_AI_INFRA.md) — AI Infra 深度解析（CUDA 算子 / 分布式训练 / 推理优化）
