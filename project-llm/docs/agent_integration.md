# 🔗 GameOps Agent × Knowledge Expert 接入指南

> 本文档说明：如何把 `project-llm` 训练出来的 **知识库专家模型**，通过 MCP 协议接入 `project-agent`（GameOps Agent），实现 **Agentic RAG** 闭环。

---

## 🗺️ 端到端架构图

```
┌──────────────────────────────────────────────────────────────────────┐
│                       GameOps Agent (Go)                              │
│                    project-agent/src/agent                            │
│  ┌──────────────────────────────────────────────────────────────┐    │
│  │  ReAct Planner                                               │    │
│  │   ├─ Tool: query_bk_monitor   (MCP bk-monitor)               │    │
│  │   ├─ Tool: query_bcs_cluster  (MCP bcs)                      │    │
│  │   ├─ Tool: knowledge_expert_query  ← 本次新增！              │    │
│  │   └─ Tool: create_gongfeng_issue  (MCP gongfeng)             │    │
│  └─────────────────────────┬────────────────────────────────────┘    │
└────────────────────────────┼─────────────────────────────────────────┘
                             │ MCP Streamable HTTP (2025-03)
                             ▼
┌──────────────────────────────────────────────────────────────────────┐
│           mcp_expert_server.py  :8200  (Python / FastMCP)             │
│    Tool: knowledge_expert_query(question, top_k) → {answer, citations}│
└──────────────────────────────┬───────────────────────────────────────┘
                               │ HTTP
                               ▼
┌──────────────────────────────────────────────────────────────────────┐
│              rag_serve.py  :8100  (FastAPI)                          │
│  Retrieve (BGE-M3 dense)  →  Rerank (BGE-Reranker-v2-m3)             │
│  → Generate (vLLM + Qwen3-8B-knowledge-sft + FP8)                    │
│  → 返回 answer + citations[] + trace_id                              │
└──────┬──────────────────────────────────┬───────────────────────────┘
       │                                  │
       ▼                                  ▼
  Qdrant :6333                 vLLM V1 :8000
  (向量检索)                   (知识库专家模型)
       ▲
       │ 离线索引构建
       │
  scripts/build_index.py
       ▲
       │ 读取
  data/raw/kb/   ← 运维 SOP / 告警手册 / 游戏设计文档
```

---

## 📋 三步接入

### Step 1 —— 启动 RAG 四件套

```bash
# 启动 Qdrant + vLLM + rag_serve + mcp_expert 四个容器
docker compose -f deploy/rag_docker-compose.yaml up -d

# 等待健康检查通过
docker compose -f deploy/rag_docker-compose.yaml ps
```

### Step 2 —— 构建索引

```bash
# 准备运维文档（markdown / jsonl）
mkdir -p data/raw/kb
cp your_kb/*.md data/raw/kb/

# 构建索引（重建模式）
python scripts/build_index.py \
    --config configs/knowledge_rag.yaml \
    --source_dir data/raw/kb \
    --recreate
```

### Step 3 —— 在 project-agent 注册 MCP 工具

在 `project-agent/conf/mcp_servers.yaml`（如不存在则创建）追加：

```yaml
mcp_servers:
  # ---------- 知识库专家（本次阶段 F 新增） ----------
  - name: llm_knowledge_expert
    target: "*"                    # 所有场景可见（排障/答疑都要用）
    url: http://localhost:8200/mcp
    transport: streamable
    timeout: 60
    allowed_tools:
      - knowledge_expert_query
      - knowledge_expert_health
    enabled: true

  # ---------- 现有 MCP（示例，按需保留） ----------
  - name: bk_monitor
    target: bk-monitor
    url: http://bk-monitor-mcp.example.com/mcp
    transport: streamable
    auth_header: X-Bkapi-Authorization
    auth_value: "${BK_API_TOKEN}"
```

重启 project-agent 后，`knowledge_expert_query` 工具即生效。

---

## 🧪 端到端验证

### 直连 MCP（不经过 Agent）

```bash
# 列出工具
curl -X POST http://localhost:8200/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# 调用工具
curl -X POST http://localhost:8200/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc":"2.0","id":2,
    "method":"tools/call",
    "params":{
      "name":"knowledge_expert_query",
      "arguments":{"question":"CPU 告警怎么排查？","top_k":5}
    }
  }'
```

### 通过 Agent 调用

```bash
# 假设 project-agent 已监听 8080
curl -X POST http://localhost:8080/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "query": "上海三区 CPU 告警持续 10 分钟，先看下是怎么回事",
    "session_id": "test-001"
  }'
```

Agent 预期 ReAct 链路：
```
Step 1: query_bk_monitor(metric="cpu_usage", region="sh-3") → 拿到监控数据
Step 2: knowledge_expert_query("CPU 持续高位的常见原因和处置 SOP") → 知识库给出排查思路
Step 3: bcs.list_pods(cluster="sh-3", namespace="gameops") → 拉出疑似 Pod
Step 4: 生成最终报告 + 引用编号 [^1]
```

---

## 🎤 面试讲解话术

> **问**：你是怎么让专家模型在 Agent 里真正起作用的？
>
> **答**：我把 RAG 服务**直接封装成 MCP 工具**，Agent 完全无感接入——只需要在 `mcp_servers.yaml` 加一条配置，不改任何 Go 代码。好处是：
>
> 1. **工具选择器权重可控**：通过 `target` 机制，在故障排查场景下才加载这个工具，避免 40+ MCP 工具污染工具选择准确率（参考 oncall_agent 的 `mcptool` 设计）
> 2. **可独立迭代**：模型升级、RAG 策略调整、prompt 改动都不触达 Go 代码
> 3. **统一观测**：RAG 返回的 `trace_id` 可与 Agent 的 `session_id` 关联，在 Langfuse 里串起端到端链路
> 4. **天然降级**：主模型（vLLM）失败自动 fallback 到 DeepSeek-V3.2，RAG 层直接兜底，Agent 侧完全无感

---

## ⚙️ 配置参数速查

| 配置 | 默认 | 建议 |
|------|------|------|
| `retriever.top_k` | 20 | 粗排召回 |
| `reranker.top_k` | 5 | 精排保留，复杂问题可设 8 |
| `reranker.score_threshold` | 0.3 | 低于则丢弃 |
| `generator.temperature` | 0.1 | 知识问答建议低温度 |
| `generator.max_tokens` | 1024 | 简单问答 256 即可 |
| `service.return_citations` | true | 面试演示时必开 |

---

## 📊 观测接入（阶段 G 预告）

阶段 G 会在 `rag_serve.py` 中接入 Langfuse，每次 RAG 调用会记录：
- 输入 query / top-k 召回片段 / rerank 分数
- LLM 的 prompt / response / token 消耗
- 端到端延迟 / 错误栈

与 Agent 侧的 trace 通过 `trace_id` 关联，形成**单次故障排查的完整可视化时间线**。
