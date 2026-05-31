# Langfuse 在线 Trace 接入文档

## 部署方式
- **自托管**：使用 `deploy/docker-compose.yaml` 中的 `langfuse + postgres` 服务
- **SaaS**：直接使用 https://cloud.langfuse.com（Hobby 免费额度足够做 Demo）

## 接入步骤

### 1. 配置环境变量

```bash
# .env
LANGFUSE_PUBLIC_KEY=pk-lf-xxx
LANGFUSE_SECRET_KEY=sk-lf-xxx
LANGFUSE_HOST=https://cloud.langfuse.com   # 或自托管 http://localhost:3000
```

### 2. 代码接入（OpenAI 兼容 API）

```python
from langfuse.openai import OpenAI   # 关键：import 自 langfuse.openai

client = OpenAI(
    base_url="http://localhost:8000/v1",   # 指向 vLLM / SGLang 服务
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

### 3. Agentic RAG 的 Trace 设计

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

## 面试可讲的 Dashboard 截图
- **Traces 视图**：展示 Agentic RAG 的多步调用链（classify → retrieve → generate）
- **Latency 分布**：微调模型直答（P50=180ms）vs RAG 路径（P50=800ms）
- **Token 成本**：在线评估 G-Eval / Faithfulness / Relevance 分数
- **错误率**：按 user / tag / session 筛选失败 trace
