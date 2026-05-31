# Go-Agent — 端到端 AI 工程作品集

> 一份覆盖 **训练/推理/Agent/SRE 全链路** 的可运行工程实践，用于支撑 LLM 算法 + AI 系统岗的简历项目。

## 项目矩阵

| 项目 | 定位 | 主语言 | 关键能力 |
|---|---|---|---|
| [project-llm](./project-llm) | 模型训练 + 推理 + RAG + 端侧部署 | Python | SFT/DPO/GRPO 全链路、vLLM v1 + EAGLE-3、AWQ/FP8 量化、ExecuTorch/MLC/QNN 端侧、RAGAS 评测 |
| [project-agent](./project-agent) | 生产级 SRE Agent（Game Ops） | Go 1.24 | trpc-agent-go、Coordinator + 4 子 Agent、HITL、A2A/AG-UI/MCP、HMAC 审计链、OTel + Langfuse、ReAct |

两个项目各自独立可运行，也可通过 [project-llm](./project-llm) 训练出的模型 + RAG 服务接入 [project-agent](./project-agent) 实现"私有模型 + 私有知识 + 业务 Agent"端到端闭环。

## 整体架构

```mermaid
flowchart LR
  subgraph 训练侧 [project-llm]
    DataGen[数据合成<br/>generate_qa.py] --> SFT[SFT/DPO/GRPO<br/>llamafactory + trl]
    SFT --> Quant[量化<br/>AWQ/GPTQ/FP8]
    Quant --> Infer[vLLM v1 / SGLang<br/>EAGLE-3]
    DataGen --> RAG[RAG Service<br/>BGE-M3 + Reranker]
  end

  subgraph 推理服务
    Infer --> OAI[(OpenAI 兼容 API)]
    RAG --> RAGAPI[(/v1/rag/query)]
  end

  subgraph Agent侧 [project-agent]
    OAI --> Coordinator[Coordinator]
    RAGAPI --> KnowledgeAgent[Knowledge Agent]
    Coordinator --> DiagnosisAgent[Diagnosis Agent]
    Coordinator --> RepairAgent[Repair Agent<br/>HITL]
    Coordinator --> FileAnalyst[File Analyst]
    Coordinator --> KnowledgeAgent
  end

  subgraph 业务接入
    BKAlarm[蓝鲸告警] --> Webhook[/webhook/bk_alarm]
    TAPDBug[TAPD 单] --> Webhook
    Webhook --> Coordinator
    Coordinator --> SSE[SSE 流式]
    Coordinator --> AGUI[AG-UI Web]
    Coordinator --> A2A[A2A 协议]
  end

  subgraph 可观测 [Observability]
    Coordinator -.OTel GenAI.-> Otel[OTel Collector]
    Otel --> Langfuse[Langfuse]
    Otel --> Jaeger[Jaeger]
    Coordinator -.metrics.-> Prom[Prometheus]
    Prom --> Grafana[Grafana]
  end
```

## 一键启动

```bash
# 1. 启动推理 + RAG（project-llm 侧）
cd project-llm
make up                    # docker compose up -d vllm + rag-server + qdrant

# 2. 启动 Agent + 全套观测栈（project-agent 侧）
cd ../project-agent
make up                    # docker compose up -d agent + redis + postgres + otel-collector + jaeger + langfuse + prometheus + grafana
make smoke                 # 跑端到端 smoke 测试

# 3. 打开 Web 控制台
open http://localhost:8080/agui      # AG-UI 前端
open http://localhost:3000           # Grafana
open http://localhost:16686          # Jaeger
open http://localhost:3001           # Langfuse
```

## 仓库结构

```
Go-Agent/
├── README.md                 # ← 本文件
├── LICENSE                   # Apache-2.0
├── project-llm/              # 模型训练 / 推理 / RAG / 端侧
│   ├── Makefile
│   ├── Dockerfile.train
│   ├── Dockerfile.infer
│   ├── INTERVIEW.md          # 面试问答详解
│   ├── MODEL_CARD.md
│   ├── DATASET_CARD.md
│   ├── configs/              # SFT/DPO/GRPO/RAG/Quant 配置
│   ├── scripts/              # 数据合成、量化、评测、Pipeline
│   ├── infra/                # CUDA 算子 / 分布式训练 / 推理优化
│   ├── deploy/               # vLLM/SGLang/llama.cpp/ExecuTorch/MLC/QNN
│   ├── eval/                 # 评测脚本与金标
│   └── observability/        # OTel + Langfuse + Prometheus
└── project-agent/            # 生产级 SRE Agent
    ├── Makefile
    ├── Dockerfile
    ├── docker-compose.yml
    ├── INTERVIEW.md
    ├── ARCHITECTURE.md
    ├── api/openapi.yaml      # 对外契约
    ├── deploy/helm/          # K8s 部署
    ├── pkg/resilience/       # 限流/熔断/重试/隔板
    ├── src/
    │   ├── agents/           # Coordinator + 4 子 Agent
    │   ├── tools/            # BCS/BK/DevOps/File/Composite/Async
    │   ├── plugin/           # Input/Output/Safety Guard
    │   ├── audit/            # HMAC 链审计
    │   ├── observability/    # OTel + GenAI Span + Metrics
    │   ├── session/          # 会话（in-mem + Redis）
    │   ├── idempotency/      # 幂等键
    │   ├── async/            # 异步 Job
    │   └── services/         # SSE/AG-UI/A2A/Webhook
    └── eval/                 # ADK Eval + LLM-as-Judge
```

## 自评指标速览

| 维度 | project-llm | project-agent |
|---|---|---|
| 代码量 | ~12k LOC Python | ~30k LOC Go |
| 单测覆盖 | 关键模块 ≥70% | 核心模块 ≥75%（含 60+ 测试文件） |
| 端到端测试 | RAG/Edge Pipeline shell | 6 个 e2e + chaos test |
| 文档 | INTERVIEW 748 行 + Model/Dataset Card | INTERVIEW 933 行 + ARCHITECTURE + OpenAPI |
| 部署形态 | Docker + ExecuTorch/MLC/QNN | Docker Compose + Helm Chart |
| 观测 | Langfuse + OTel + Prometheus | OTel GenAI v1.30 + Langfuse + 完整 Grafana 看板 |

## 📚 文档导航

仓库内的 markdown 文档比较多，按主题查找请走 → **[docs/INDEX.md](./docs/INDEX.md)**

常用入口：
- 部署运行：[project-agent/DEPLOY.md](./project-agent/DEPLOY.md) · [project-llm/DEPLOY.md](./project-llm/DEPLOY.md)
- 进度追踪：[TODO.md](./TODO.md)
- 面试反向索引：[INTERVIEW_INDEX.md](./INTERVIEW_INDEX.md)

## License

Apache License 2.0 — 详见 [LICENSE](./LICENSE)
