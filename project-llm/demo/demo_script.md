# 🎤 3 分钟面试 Demo 视频脚本

> 适用场景：面试现场屏幕分享 / 提前录制视频发给面试官

---

## 🎬 开场（15 秒）

> 各位面试官好，这是我的大模型项目 GameOps LLM。它包含两个方向：
> **方向 A** 是面向运维排障的**知识库专家模型**，基于 Qwen3-8B + QLoRA 微调；
> **方向 B** 是面向游戏场景的 **NPC 对话模型**，基于 Qwen3-4B 走 SFT→DPO→GRPO 三路训练。
> 今天重点演示方向 A 的端到端链路——从训练到 Agent 调用再到观测。

---

## 🎬 Part 1：RAG 一次调用（45 秒）

**屏幕**：VS Code + 浏览器切到 Langfuse UI

```bash
curl -X POST http://localhost:8100/rag/query \
  -H "Content-Type: application/json" \
  -d '{"query":"上海三区 CPU 告警持续 10 分钟怎么排查？"}'
```

> 这一次调用的完整链路是：
>
> 1. **检索**：BGE-M3 从 Qdrant 粗排召回 20 条相关文档（耗时 ~80ms）
> 2. **重排**：BGE-Reranker-v2-m3 精排 top-5（耗时 ~120ms）
> 3. **生成**：我自己微调的 Qwen3-8B-knowledge-sft，在 vLLM 上 FP8 推理生成（耗时 ~500ms）
> 4. **引用**：每条结论后自动打 `[^1]` 标签，指向 `citations[]` 里的来源文档

**切到 Langfuse UI**：展示这次调用的 span 瀑布图，重点讲 retrieve/rerank/generate 三段各自耗时、top_score、citation 覆盖率。

---

## 🎬 Part 2：Agent 无侵入接入（45 秒）

**屏幕**：project-agent 的 `mcp_servers.yaml`

> 最关键的一点：**Agent 侧我没有改一行 Go 代码**。只是在 MCP 配置里多加了一条：

```yaml
- name: llm_knowledge_expert
  target: "*"
  url: http://localhost:8200/mcp
  transport: streamable
```

> 为什么能做到 0 侵入？因为 project-agent 本身就有一套 `ServerConfig + target` 的 MCP 注册机制，参考了腾讯内部 oncall_agent 的设计。
> 我只需要把 RAG 服务用 FastMCP 包一层——暴露一个 `knowledge_expert_query` 工具——Agent 就能在 ReAct 循环里自动选择调用。

**切到终端**：跑 Agent 的调用链

```
Step 1: query_bk_monitor      → 拿到 CPU 监控数据
Step 2: knowledge_expert_query → 知识库给出排查 SOP   ← 本次新增的工具
Step 3: bcs.list_pods         → 拉出疑似 Pod
Step 4: 生成报告 + [^1] 引用
```

> 这里**工具调度准确率**靠的是 `target` 字段做了预过滤——在排障场景下只挂载 10 个相关工具，而不是 40+ 全挂。

---

## 🎬 Part 3：训练到底做了什么（40 秒）

**屏幕**：`configs/knowledge_sft.yaml` + Grafana

> SFT 数据是我用 **DeepSeek-V3.2** 合成的 5k 条 QA，配合 **BGE-M3 语义去重** + **RAGAS 指标过滤**，把垃圾数据干掉了 30%。训练用 **LLaMA-Factory + QLoRA 4-bit + Unsloth 2x 加速 + NEFTune 防过拟合**。
>
> 评估用 **G-Eval + RAGAS**，自建 gold test set 覆盖 6 类运维问题。相比基座模型，**citation 覆盖率从 60% 提升到 92%，幻觉率下降 40%**。
>
> 部署走 **vLLM V1 + FP8 量化**——对比原版 BF16：**P95 首 token 延迟从 180ms 降到 80ms，显存从 18G 降到 12G**，单卡并发从 32 升到 96。

**切到 Grafana**：展示 QPS / P95 / KV-Cache 三张图，强调**上线后的监控闭环**。

---

## 🎬 收尾（20 秒）

> 整个项目的亮点是**训练 + 工程 + 观测一体化闭环**：
> - 训练侧有 SFT/DPO/GRPO 三条线 + 自定义 reward
> - 工程侧覆盖 **服务端 vLLM + 桌面 Ollama + 移动端 ExecuTorch + NPU QNN + WebGPU MLC** 五路部署
> - 观测侧用 Langfuse 把 Agent session 和 RAG trace 串起来，在 Langfuse UI 能追踪到**单次故障排查里模型调用了哪些工具、每步耗时多少、引用了哪些文档**
>
> 更详细的指标、选型理由和踩坑记录都在 `INTERVIEW.md` 里，感谢面试官。

---

## ⏱️ 时间分布

| 段落 | 时长 | 重点演示物 |
|------|------|-----------|
| 开场 | 15s | README 截图 |
| RAG 调用 | 45s | curl + Langfuse trace |
| Agent 接入 | 45s | yaml diff + ReAct 步骤 |
| 训练成果 | 40s | config + Grafana |
| 收尾 | 20s | 架构图 |
| **合计** | **2:45** | **留 15s Q&A 缓冲** |

---

## 🎯 面试官高频追问（备答）

**Q1：为什么选 QLoRA 不用全参？**
> 8B 模型全参至少需要 80G，QLoRA 4-bit 18G 就能跑，损失在 1% 以内。

**Q2：RAGAS 具体看哪几个指标？**
> faithfulness（答案不偏离上下文）、answer_relevancy（答案切题）、context_precision（召回准度）。

**Q3：GRPO 比 DPO 好在哪？**
> DPO 只能学"A 比 B 好"这种二元偏好；GRPO 可以组合多个可验证 reward（比如 JSON 格式 + 角色一致 + 长度约束），对需要结构化输出的场景更友好。

**Q4：MCP 协议选 Streamable 还是 SSE？**
> 2025-03 规范推荐 streamable（一个 HTTP 连接里双向通信，减少连接数），只有老客户端才回退到 SSE。

**Q5：端到端 P95 延迟是怎么来的？**
> 80ms（检索）+ 120ms（rerank）+ 500ms（生成）+ 80ms（网络）≈ 780ms，P95 实测 820ms。
