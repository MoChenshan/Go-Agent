# 面试索引 — 想看 X 看哪里

> 评审/面试时，把这份当**目录**翻，能在 200+ 文件里 30 秒定位到关键证据。
> 两侧主 INTERVIEW.md 已经写得很厚，本文件只做**反向索引**（按问题找文件）。

---

## 🎯 总览

| 想看 | 直接打开 |
|---|---|
| 项目矩阵 + 一键启动 | [README.md](./README.md) |
| 顶层全栈 compose | [docker-compose.full.yml](./docker-compose.full.yml) |
| 一键 demo（无 GPU） | `make demo-agent` → [project-agent/src/cmd/demo/main.go](./project-agent/src/cmd/demo/main.go) |
| Agent 完整面试问答 | [project-agent/INTERVIEW.md](./project-agent/INTERVIEW.md) |
| LLM 完整面试问答 | [project-llm/INTERVIEW.md](./project-llm/INTERVIEW.md) |

---

## 🅰️ project-agent —— 按问题找代码

### 架构与设计模式
| 问题 | 看哪儿 |
|---|---|
| 多 Agent 如何编排（Coordinator + 子 Agent） | [src/agents/coordinator/](./project-agent/src/agents/coordinator) |
| ReAct 实现 | [src/agents/react.go](./project-agent/src/agents/react.go) |
| 工具系统抽象（function calling / MCP） | [src/tools/](./project-agent/src/tools) + [mcp_servers.yaml](./project-agent/mcp_servers.yaml) |
| 整体架构图 | [ARCHITECTURE.md](./project-agent/ARCHITECTURE.md) |
| 对外契约（OpenAPI） | [api/openapi.yaml](./project-agent/api/openapi.yaml) |
| 对外契约（gRPC/tRPC） | [api/proto/agent.proto](./project-agent/api/proto/agent.proto) |

### 韧性 / 高可用
| 问题 | 看哪儿 |
|---|---|
| 限流 / 熔断 / 重试 / 隔板 | [pkg/resilience/](./project-agent/pkg/resilience) |
| 弹性链组合顺序与 Why | [pkg/resilience/chain.go](./project-agent/pkg/resilience/chain.go) |
| 混沌测试 | [src/integration/chaos_test.go](./project-agent/src/integration/chaos_test.go) |
| 优雅关闭 | [src/app/shutdown.go](./project-agent/src/app/shutdown.go) |
| HITL 重启不中断（Redis Session） | [src/session/](./project-agent/src/session) |
| 异步 Job + 幂等 | [src/async/](./project-agent/src/async) + [src/idempotency/](./project-agent/src/idempotency) |

### 安全 / 合规
| 问题 | 看哪儿 |
|---|---|
| Input/Output/Safety Guard | [src/plugin/](./project-agent/src/plugin) |
| HMAC 审计链 | [src/audit/hmac.go](./project-agent/src/audit/hmac.go) |
| 审计链验证 CLI | [src/cmd/auditverify/main.go](./project-agent/src/cmd/auditverify/main.go) |
| 安全事件响应 | [SECURITY.md](./project-agent/SECURITY.md) |
| K8s 最小权限 RBAC | [deploy/helm/templates/rbac.yaml](./project-agent/deploy/helm/templates/rbac.yaml) |

### 可观测性
| 问题 | 看哪儿 |
|---|---|
| OTel GenAI semconv 接入 | [src/observability/genai_span.go](./project-agent/src/observability/genai_span.go) |
| Metrics 全套 | [src/observability/metrics_more.go](./project-agent/src/observability/metrics_more.go) |
| Prometheus 告警规则 | [deploy/alerts/prometheus_rules.yaml](./project-agent/deploy/alerts/prometheus_rules.yaml) |
| 告警规则单测（promtool） | [deploy/alerts/prometheus_rules_test.yaml](./project-agent/deploy/alerts/prometheus_rules_test.yaml) |
| Grafana 看板 | [deploy/grafana/dashboards/](./project-agent/deploy/grafana/dashboards) |
| 观测最佳实践 | [docs/observability.md](./project-agent/docs/observability.md) |

### 评测
| 问题 | 看哪儿 |
|---|---|
| LLM-as-Judge | [eval/judge_llm.go](./project-agent/eval/judge_llm.go) |
| 工具选择评测 | [eval/judge_tool_selection.go](./project-agent/eval/judge_tool_selection.go) |
| Golden 数据集 | [eval/data/](./project-agent/eval/data) |
| CI 集成 judge 摘要回写 PR | [scripts/ci/comment-judge-summary.sh](./project-agent/scripts/ci/comment-judge-summary.sh) |

### 测试覆盖
| 问题 | 看哪儿 |
|---|---|
| 端到端 BCS 全链路 | [src/integration/bcs_full_flow_test.go](./project-agent/src/integration/bcs_full_flow_test.go) |
| Webhook → Agent 全链路 | [src/integration/webhook_integration_test.go](./project-agent/src/integration/webhook_integration_test.go) |
| 多 Agent A2A | [src/services/a2a/multi_agent_e2e_test.go](./project-agent/src/services/a2a/multi_agent_e2e_test.go) |
| 修复执行流（含 HITL） | [src/integration/repair_flow_test.go](./project-agent/src/integration/repair_flow_test.go) |

---

## 🅱️ project-llm —— 按问题找文件

### 训练
| 问题 | 看哪儿 |
|---|---|
| SFT 训练（LlamaFactory 主） | [configs/npc_sft.yaml](./project-llm/configs/npc_sft.yaml) |
| SFT 训练（裸 trl 备选） | [scripts/train_sft.py](./project-llm/scripts/train_sft.py) |
| DPO / GRPO | [configs/npc_dpo.yaml](./project-llm/configs/npc_dpo.yaml) / [configs/npc_grpo.yaml](./project-llm/configs/npc_grpo.yaml) |
| GRPO 奖励函数 | [scripts/grpo_rewards.py](./project-llm/scripts/grpo_rewards.py) |
| 数据飞轮 / replay buffer | [scripts/data_replay_buffer.py](./project-llm/scripts/data_replay_buffer.py) |
| LoRA 合并 | [scripts/merge_lora.py](./project-llm/scripts/merge_lora.py) |

### 数据
| 问题 | 看哪儿 |
|---|---|
| 原始数据 → 标准化 pipeline | [scripts/data_pipeline.py](./project-llm/scripts/data_pipeline.py) |
| 数据合成（NPC 对话 / QA） | [scripts/generate_npc_data.py](./project-llm/scripts/generate_npc_data.py) / [scripts/generate_qa.py](./project-llm/scripts/generate_qa.py) |
| 数据质量校验 | [scripts/data_quality.py](./project-llm/scripts/data_quality.py) |
| Demo 样本（20 条 ShareGPT） | [data/processed/sft_demo.jsonl](./project-llm/data/processed/sft_demo.jsonl) |

### 推理 / 部署
| 问题 | 看哪儿 |
|---|---|
| vLLM v1 启动脚本 | [deploy/vllm_v1_server.sh](./project-llm/deploy/vllm_v1_server.sh) |
| 多租户 LoRA | [deploy/vllm_lora_multi.sh](./project-llm/deploy/vllm_lora_multi.sh) |
| EAGLE-3 推测解码 | [deploy/eagle3_draft.md](./project-llm/deploy/eagle3_draft.md) |
| 端侧（ExecuTorch / MLC / QNN） | [deploy/executorch/](./project-llm/deploy/executorch) / [deploy/mlc/](./project-llm/deploy/mlc) / [deploy/qnn/](./project-llm/deploy/qnn) |
| RAG 服务 | [deploy/rag_serve.py](./project-llm/deploy/rag_serve.py) |
| Guided decoding 演示 | [infra/inference/guided_decoding_demo.py](./project-llm/infra/inference/guided_decoding_demo.py) |

### CUDA / 分布式
| 问题 | 看哪儿 |
|---|---|
| 自写 Triton RMSNorm | [infra/cuda/triton_rmsnorm.py](./project-llm/infra/cuda/triton_rmsnorm.py) |
| FlashAttention 基准 | [infra/cuda/flash_attn_bench.py](./project-llm/infra/cuda/flash_attn_bench.py) |
| DDP / FSDP / TP | [infra/distributed/](./project-llm/infra/distributed) |
| 分布式内存账本 | [infra/reports/distributed_mem.md](./project-llm/infra/reports/distributed_mem.md) |

### 量化
| 问题 | 看哪儿 |
|---|---|
| AWQ-W4A16 | [scripts/quantize_awq.py](./project-llm/scripts/quantize_awq.py) |
| FP8 / GPTQ-Marlin / GGUF | [scripts/quantize_fp8.py](./project-llm/scripts/quantize_fp8.py) / [scripts/quantize_gptq_marlin.py](./project-llm/scripts/quantize_gptq_marlin.py) / [scripts/quantize_gguf.sh](./project-llm/scripts/quantize_gguf.sh) |
| 量化整体配置 | [configs/quantize.yaml](./project-llm/configs/quantize.yaml) |

### 评测
| 问题 | 看哪儿 |
|---|---|
| 通用评测（G-Eval + RAGAS + Langfuse） | [scripts/evaluate.py](./project-llm/scripts/evaluate.py) |
| 红队 / 越狱评测 | [scripts/red_team_eval.py](./project-llm/scripts/red_team_eval.py) + [eval/red_team.jsonl](./project-llm/eval/red_team.jsonl) |
| Golden 50 | [eval/golden_50.jsonl](./project-llm/eval/golden_50.jsonl) |
| RAGAS 单跑 | [scripts/ragas_eval.py](./project-llm/scripts/ragas_eval.py) |
| 性能基准 | [scripts/benchmark_serving.py](./project-llm/scripts/benchmark_serving.py) |

### 模型/数据卡
| 问题 | 看哪儿 |
|---|---|
| Model Card | [MODEL_CARD.md](./project-llm/MODEL_CARD.md) |
| Dataset Card | [DATASET_CARD.md](./project-llm/DATASET_CARD.md) |

---

## 🅲 跨项目联动

| 想看 | 看哪儿 |
|---|---|
| 一条命令拉起全栈 | `make up` → [docker-compose.full.yml](./docker-compose.full.yml) |
| LLM × Agent 端到端 demo | [project-llm/demo/end_to_end.sh](./project-llm/demo/end_to_end.sh) |
| Agent 接入 LLM 的契约 | [project-llm/docs/agent_integration.md](./project-llm/docs/agent_integration.md) |
| Smoke 测试 | [scripts/smoke_agent.sh](./scripts/smoke_agent.sh) / [scripts/smoke_llm.sh](./scripts/smoke_llm.sh) |

---

## 🎤 推荐演示路径（10 分钟版）

1. **30 秒**：`make demo-agent` → 浏览器打开 `:8090/healthz`，看到 demo 服务起来
2. **1 分钟**：`curl POST :8090/demo/alarm` × 5，再 `GET :8090/demo/audit/last`，讲弹性链 + 审计的真实组合
3. **2 分钟**：打开 [pkg/resilience/chain.go](./project-agent/pkg/resilience/chain.go)，讲限流→隔板→熔断→重试为什么是这个顺序
4. **2 分钟**：打开 [src/audit/hmac.go](./project-agent/src/audit/hmac.go) + 跑 `auditverify`，讲审计链不可篡改
5. **2 分钟**：打开 [project-llm/INTERVIEW.md](./project-llm/INTERVIEW.md) 翻到"训练 / 推理 / 数据飞轮"对应章节
6. **1 分钟**：打开 [INTERVIEW.md](./project-agent/INTERVIEW.md) 翻到"OTel GenAI / Langfuse"段落
7. **1 分钟**：`make up` → 展示全栈 docker compose（视环境跳过实际启动）
