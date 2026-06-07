# Go-Agent 工程执行规划

> 范围：[project-agent](project-agent/)（Go / tRPC-Agent-Go）+ [project-llm](project-llm/)（Python / Qwen3 微调推理）
> 创建日期：2026-05-24
> 对应 TODO 项：A5、B3-B5、C1、S1-S4

---

## 一、IDE 选型

| 项目 | 技术栈 | 推荐 IDE | 理由 |
|------|--------|---------|------|
| **project-agent** | Go 1.24 + tRPC + Docker | **GoLand**（首选）/ VSCode + Go 扩展 | Go 原生开发，tRPC proto 生成、go mod、debug、refactor 支持最好 |
| **project-llm** | Python 3.10+ + PyTorch + vLLM | **PyCharm Professional**（首选）/ VSCode + Python/Jupyter 扩展 | Python 生态完整，Jupyter notebook + 远程解释器（连 GPU 服务器）是刚需 |

补充：
- **GoLand** 写 `project-agent` 最自然，tRPC Go 的 proto 生成、go mod 支持最好。所有依赖均为公开库（`goproxy.cn` 可直连）。
- **PyCharm Professional** 的「远程解释器」功能对 `project-llm` 极其关键——本地 Windows 写代码，远程 Linux GPU 服务器跑训练，无缝对接。
- **VSCode** 可作为两个项目的轻量备选，分别装 Go 扩展和 Python + Remote-SSH 扩展。
- **IntelliJ IDEA** 装 Go 和 Python 插件也能用，但不如专用 IDE 专业，不推荐作为主力。

---

## 二、部署层级与服务器需求

### project-agent 三种部署层级

| 层级 | 方式 | 需要什么 | 用途 |
|------|------|---------|------|
| **L1 开发联调** | `make run` / `go run . -cli` | 本地 Windows 即可 | 编码、调试、单元测试 |
| **L2 本地全栈** | `make up`（docker compose） | Windows Docker Desktop | 完整业务流程验证（Agent + Redis + Postgres + OTel + Jaeger + Langfuse + Prometheus + Grafana，全部公开镜像） |
| **L3 生产级部署** | `helm install` 到 K8s | **需要 K8s 集群** | 验证 Helm Chart、HPA、PDB、NetworkPolicy、RBAC、ServiceMonitor、滚动升级等生产特性 |

### L3 生产级部署需要 K8s 验证的功能

Helm Chart 中包含的生产级特性，docker compose 上无法验证：

- **HPA 自动伸缩** → 需要 K8s Metrics Server + 多副本调度
- **PDB 故障预算** → 需要 K8s 调度器驱逐 Pod 来验证
- **NetworkPolicy 网络隔离** → 需要 CNI 插件（Calico/Cilium）支持
- **RBAC 最小权限** → 需要 K8s API Server 鉴权
- **ServiceMonitor** → 需要 Prometheus Operator
- **滚动升级（maxSurge:1, maxUnavailable:0）** → 需要 Deployment Controller
- **反亲和 podAntiAffinity** → 需要多节点调度

### project-llm 各场景需求

| 场景 | 是否需要服务器 | 说明 |
|------|---------------|------|
| 数据合成 / 评测 | ❌ 不需要 | CPU 子集，Windows 本地 `pip install` 去掉 GPU 包即可 |
| QLoRA-SFT / DPO / GRPO 训练 | ✅ **必须** | 需要 Linux + NVIDIA GPU（4090 24GB / A100 40GB+） |
| vLLM / SGLang 推理服务 | ✅ **必须** | 需要 Linux + NVIDIA GPU + NVIDIA Container Toolkit |
| 量化（FP8/GPTQ/AWQ） | ✅ **必须** | 同上 |
| 端侧导出（GGUF/MLC） | ⚠️ 可选 | GGUF 跨平台可用，ExecuTorch/QNN 优先 Linux |

---

## 三、GPU 服务器推荐

| 平台 | 推荐实例 | 参考价格 | 适合场景 | 推荐度 |
|------|---------|---------|---------|--------|
| **AutoDL** | A100 40GB / 4090 24GB | ¥2~4/时 | 训练+SFT+DPO，国内性价比最高，Jupyter 直连 | ⭐⭐⭐⭐⭐ |
| **阿里云 PAI-EAS / ECS** | A10 24GB / A100 40GB | ¥5~15/时 | 推理部署+生产级，稳定性好，按量付费 | ⭐⭐⭐⭐ |
| **恒源云 (GPUSHARE)** | 4090 24GB / A100 | ¥1.5~3/时 | 训练，便宜，AutoDL 的竞品 | ⭐⭐⭐⭐ |
| **趋动云** | A100 / H800 | ¥3~8/时 | 企业级，有免费额度活动 | ⭐⭐⭐ |
| **Lambda Cloud** | A100 / H100 | $1~2/时 | 海外选项，网络可能受限 | ⭐⭐ |

### 具体选型

**方向一（知识库专家 Qwen3-8B QLoRA）：**
- 训练：AutoDL 4090 24GB × 1（QLoRA 4bit，Qwen3-8B 峰值 ~18GB 显存）
- 推理：AutoDL 4090 24GB 跑 vLLM，或训练完切到阿里云部署

**方向二（AINPC Qwen3-4B + 端侧）：**
- 训练：AutoDL 4090 24GB × 1 富余（Qwen3-4B QLoRA 峰值 ~8GB）
- 推理：不需要 GPU 服务器，最终目标是端侧部署（Ollama/ExecuTorch/MLC）

---

## 四、K8s 集群方案

| 方案 | 成本 | 适合场景 | 推荐度 |
|------|------|---------|--------|
| **minikube / kind** | 免费 | Helm 模板渲染验证、基础 K8s 特性测试 | ⭐⭐⭐⭐（开发阶段首选） |
| **阿里云 ACK / 腾讯云 TKE** | ¥100-300/月（按量） | 完整生产级验证、多节点调度 | ⭐⭐⭐ |
| **AutoDL + k3s 自建** | ¥2-4/时 | 临时验证、用完即毁 | ⭐⭐⭐ |

### 方案 A：minikube / kind（推荐先试）

```bash
# Windows 上用 Docker Desktop 自带的 K8s 或装 minikube
minikube start --nodes=3 --memory=8192 --cpus=4

# Helm 部署
helm install gameops-agent ./deploy/helm -n gameops --create-namespace `
  --set replicaCount=2 `
  --set autoscaling.enabled=false `
  --set secrets.openaiApiKey="$env:OPENAI_API_KEY"
```

- 优点：零成本、Windows 开发机上跑
- 缺点：单机模拟多节点，NetworkPolicy/反亲和验证有限；8GB 内存起 3 副本较紧张
- 能验证：Helm 模板渲染、Deployment 滚动升级、Service/Ingress、Secret 注入、RBAC、ServiceMonitor CRD
- 不能完整验证：真实多节点调度、NetworkPolicy（需装 Calico 插件）、HPA 冷启动

### 方案 B：阿里云 ACK（生产级验证）

```bash
# 创建 ACK 托管集群（~¥150/月控制面 + 按量节点）
# 节点选 2× ecs.c7.xlarge (4C8G) ≈ ¥0.8/时/台
# 总成本约 ¥100-200/月（按需开关）
```

- 优点：真实多节点、完整 K8s 特性
- 缺点：持续花钱；需配 kubeconfig、镜像仓库（ACR/DOCKER HUB）
- 注意：镜像推到 ACR/DOCKER HUB 需要配 `docker login`

### 方案 C：AutoDL + k3s（临时验证）

```bash
# 租一台 AutoDL 4090 机器（同时跑 project-llm 推理 + k3s）
curl -sfL https://get.k3s.io | sh -
kubectl get nodes
```

- 优点：和 GPU 服务器复用，一机两用；按小时计费
- 缺点：单节点，部分特性验证不了；AutoDL 网络有防火墙限制

---

## 五、依赖与外部服务

> project-agent 所有 Go 依赖均为公开库（通过 `goproxy.cn` 拉取）。只需 **1 个 LLM API Key（DeepSeek/OpenAI）** 即可完整跑通所有业务流程。

### project-agent 构建与依赖

| 项 | 说明 |
|---|------|
| Go 代理 | `GOPROXY=https://goproxy.cn,direct`，`GOPRIVATE=""` |
| Docker 镜像 | docker-compose.yml 全部使用公开镜像（Redis/Postgres/OTel/Jaeger/Langfuse/Prom/Grafana） |
| LLM | DeepSeek / OpenAI / 自建 vLLM，通过 `OPENAI_BASE_URL` + `MODEL_NAME` 配置 |
| Embedding | OpenAI `text-embedding-3-small` 或本地 BGE-M3（`.env.example` 已支持） |
| 构建 | `make build`（默认启用 `a2a`/`agui`/`iwiki`/`redis` 四个可选 tag）；`make build-minimal`（不带可选 tag） |

### 外部服务对接

所有外部服务客户端均为自研纯 `net/http` 实现，内置 Mock 模式，未配置凭据时自动返回预置样例数据：

| 服务 | 客户端实现 | Mock 开关 | 状态 |
|------|-----------|----------|------|
| 蓝鲸监控 | `net/http` + APIGW 鉴权 | `BK_API_MOCK=1` | ✅ Mock 可完整跑通 |
| BCS 容器平台 | `net/http` + Bearer Token | `BCS_API_MOCK=1` | ✅ Mock 可完整跑通 |
| 工蜂 Git | `net/http` + PRIVATE-TOKEN | `GONGFENG_API_MOCK=1` | ✅ Mock 可完整跑通 |
| 蓝盾 CI/CD | `net/http` + X-DEVOPS-* | `DEVOPS_API_MOCK=1` | ✅ Mock 可完整跑通 |
| TAPD | `net/http` + Basic Auth | `TAPD_API_MOCK=1` | ✅ Mock 可完整跑通 |
| iWiki 知识库 | Build tag 控制（`-tags iwiki`） | 凭据缺失自动降级 | ✅ 默认可用 |

### project-llm 依赖

| 项 | 说明 |
|---|------|
| LLM API | DeepSeek + Moonshot（`.env.example` 已配置） |
| pip 源 | `pypi.org` 或阿里源 |
| HuggingFace | `hf-mirror.com`（已配 `HF_ENDPOINT`） |
| 训练观测 | TensorBoard（本地） |

---

## 六、整体架构流程图

```
┌─────────────────────── Windows 开发机 ───────────────────────┐
│                                                               │
│  ┌─────────────────┐  ┌─────────────────┐  ┌──────────────┐  │
│  │ GoLand           │  │ PyCharm Pro      │  │ minikube     │  │
│  │ project-agent    │  │ project-llm      │  │ Helm Chart   │  │
│  │ Go 编码+调试     │  │ Python 编码+NB   │  │ L3 轻量验证  │  │
│  └────────┬────────┘  └────────┬────────┘  └──────┬───────┘  │
│           │                    │                   │          │
│           │ make up            │ Remote SSH        │          │
│           ▼                    │                   │          │
│  ┌─────────────────────────┐   │                   │          │
│  │ Docker Desktop           │  │                   │          │
│  │ Redis/OTel/Jaeger/       │  │                   │          │
│  │ Langfuse/Prom/Grafana    │  │                   │          │
│  └─────────────────────────┘   │                   │          │
│                                │                   │          │
└────────────────────────────────┼───────────────────┼──────────┘
                                 │                   │
                    ┌────────────┼───────────────────┼────────┐
                    │    GPU 服务器 (AutoDL/阿里云)    │        │
                    │                                 │        │
                    │  ┌──────────────┐  ┌──────────┐│        │
                    │  │ 训练环境      │  │ 推理环境  ││        │
                    │  │ Ubuntu+CUDA   │  │ vLLM/    ││        │
                    │  │ QLoRA/DPO/    │  │ SGLang   ││        │
                    │  │ GRPO          │  │ :8000/v1 ││        │
                    │  └──────┬───────┘  └─────┬────┘│        │
                    │         │                │     │        │
                    │         │ LoRA 权重      │     │        │
                    │         └───────►────────┘     │        │
                    │                          │     │        │
                    │  ┌──────────────────────┐│     │        │
                    │  │ k3s (可选, 和GPU复用) ││     │        │
                    │  └──────────────────────┘│     │        │
                    │                          │     │        │
                    └──────────────────────────┼─────┼────────┘
                                               │     │
                     OPENAI_BASE_URL ◄─────────┘     │
                     http://gpu-server:8000/v1        │
                                                     │
               ┌─────────────────────────────────────┘
               │
               ▼
    ┌──────────────────────┐
    │ 端侧部署             │
    │ Ollama/ExecuTorch/QNN│
    └──────────────────────┘
```

---

## 七、分阶段执行规划

### 阶段 P0：开发环境搭建（1 天，成本 ¥0）

#### P0-A：project-agent 开发环境

| # | 操作 | 验收标准 |
|---|------|---------|
| 1 | 安装 GoLand，打开 `project-agent` | IDE 识别 Go module，无红色编译错误 |
| 2 | 配置 Go 代理：`go env -w GOPROXY=https://goproxy.cn,direct` | `go env GOPROXY` 输出正确 |
| 3 | 构建项目：`make build` | 生成 `bin/gameops-agent` |
| 4 | 复制 `.env.example → .env`，填入 DeepSeek API Key，所有 `*_API_MOCK=1` | `.env` 文件就位 |
| 5 | 运行 preflight 自检：`go run ./src/cmd/preflight` | 输出显示所有平台为 MOCK 状态 |
| 6 | 启动 Agent：`make run` | 控制台输出 `listening :8080`，无 panic |
| 7 | CLI 模式验证：`make run-cli` | 进入交互式对话，输入 "hello" 能获得 Mock 响应 |

**P0-A 验收门禁**：`make run` 无错启动 + `make run-cli` 对话正常

#### P0-B：project-llm CPU 子集环境

| # | 操作 | 验收标准 |
|---|------|---------|
| 1 | 安装 PyCharm Professional，打开 `project-llm` | 项目正常加载 |
| 2 | conda 创建环境：`conda create -n project-llm python=3.10 -y && conda activate project-llm` | `python --version` 输出 3.10.x |
| 3 | 安装 CPU 子集依赖：`pip install -r requirements.txt` | 无报错 |
| 4 | 卸载 GPU-only 包：`pip uninstall -y vllm flash-attn triton deepspeed bitsandbytes 2>$null` | 卸载完成或提示未安装 |
| 5 | 复制 `.env.example → .env`，填入 `DEEPSEEK_API_KEY` 和 `MOONSHOT_API_KEY` | `.env` 文件就位 |
| 6 | 验证基础导入：`python -c "import datasets, transformers, peft; print('OK')"` | 输出 `OK` |

**P0-B 验收门禁**：Python 环境可用 + 依赖安装无致命错误

---

### 阶段 P1：project-agent 全栈验证（2-3 天，对应 TODO A5，成本 ¥0）

#### P1-A：Docker Desktop + docker compose 全栈启动

| # | 操作 | 验收标准 |
|---|------|---------|
| 1 | 安装 Docker Desktop for Windows，启用 WSL2 后端 | `docker --version` 输出 ≥ 20.10 |
| 2 | Docker Desktop 设置中分配 ≥ 8GB 内存 | 设置界面确认 |
| 3 | 构建并启动：`make up` | 所有容器 Up（agent + redis + postgres + otel-collector + jaeger + langfuse + prometheus + grafana） |
| 4 | Agent 健康检查：`curl http://localhost:8080/healthz` | 返回 `ok` |
| 5 | Jaeger UI：`http://localhost:16686` | 可看到 gameops-agent service |
| 6 | Grafana：`http://localhost:3000`（admin/admin） | 能看到 gameops dashboard |
| 7 | Langfuse：`http://localhost:3001` | 可访问，能看到 Trace 列表（对话后） |
| 8 | Prometheus：`http://localhost:9090` | 查询 `gameops_session_inflight` 有数据 |

#### P1-B：冒烟测试 + 业务流程验证

| # | 操作 | 验收标准 |
|---|------|---------|
| 1 | 运行 `make smoke` | `/healthz` ✓ + `/v1/agent` SSE 有事件输出 |
| 2 | CLI 模式完整对话：`make run-cli` → 输入"查一下告警" | KnowledgeAgent/DiagnosisAgent 被路由，返回 Mock 告警数据 |
| 3 | 输入"帮我重启 pod xxx" | 触发 RepairAgent + HITL 确认流程 |
| 4 | 输入"分析这个日志" + 文件路径 | FileAnalystAgent 响应 |
| 5 | 查 Jaeger Trace | 能看到完整 Agent 链路（Coordinator → SubAgent → Tool → Response） |
| 6 | 查 Langfuse | 能看到 LLM 调用详情（token 用量、延迟） |
| 7 | 运行 `make down` 停栈 | 所有容器正常退出 |

#### P1-C：Helm Chart 本地验证（minikube）

| # | 操作 | 验收标准 |
|---|------|---------|
| 1 | 安装 minikube：`choco install minikube` | `minikube version` 正常 |
| 2 | 启动：`minikube start --memory=8192 --cpus=4` | `kubectl get nodes` Ready |
| 3 | 构建镜像推入 minikube：`eval $(minikube docker-env) && make docker` | 镜像在 minikube 内可见 |
| 4 | 安装：`helm install gameops-agent ./deploy/helm -n gameops --create-namespace --set secrets.openaiApiKey=$env:OPENAI_API_KEY --set autoscaling.enabled=false --set replicaCount=1` | Pod Running |
| 5 | 验证：`kubectl -n gameops port-forward svc/gameops-agent 8080:8080` + `curl localhost:8080/healthz` | 返回 `ok` |
| 6 | 验证 RBAC：`kubectl -n gameops get role,rolebinding,serviceaccount` | 资源存在且配置正确 |
| 7 | 验证 Secret：`kubectl -n gameops get secret` | Secret 存在 |
| 8 | 卸载：`helm uninstall gameops-agent -n gameops` | 清理干净 |

**P1 验收门禁**：
- ✅ `make up && make smoke` 通过（TODO A5 闭环）
- ✅ 四种 Agent 路由均 Mock 正常
- ✅ 可观测性全链路可见（Jaeger + Langfuse + Grafana）
- ✅ Helm Chart 在 minikube 可部署

---

### 阶段 P2：project-llm 数据链路验证（2 天，对应 TODO B3，成本 ¥0-5）

> 在 Windows 本地 CPU 环境完成，不需要 GPU。

#### P2-A：数据合成

| # | 操作 | 验收标准 |
|---|------|---------|
| 1 | 准备 NPC 数据：创建 `data/raw/npc_profiles.json` + `data/raw/world_setting.md`（用样例） | 文件存在且格式正确 |
| 2 | 跑 NPC 对话合成（smoke 模式）：`python scripts/generate_dialogue.py --n 10 --output data/processed/npc_sft.jsonl` | 生成 ≥ 10 条对话，JSONL 格式正确 |
| 3 | 跑 QA 合成（smoke 模式）：`python scripts/generate_qa.py --task npc --output data/processed/qa_test.jsonl --n 10` | 生成 ≥ 10 条 QA 对 |
| 4 | 数据质量检查：`python scripts/data_quality.py --input data/processed/npc_sft.jsonl --output data/processed/npc_sft_filtered.jsonl` | 输出质量报告，过滤比例合理 |

#### P2-B：评测脚本验证

| # | 操作 | 验收标准 |
|---|------|---------|
| 1 | 准备评测集：创建 `eval/golden_50.jsonl`（用 5 条样例即可） | 文件存在 |
| 2 | 跑评测（外接 DeepSeek 作为 Judge）：`python scripts/evaluate.py --golden eval/golden_50.jsonl --report eval/test_report.md` | 生成评测报告 markdown |
| 3 | 检查 `eval/test_report.md` | 包含 G-Eval 分数、工具选择准确率 |

#### P2-C：RAG 服务本地验证

| # | 操作 | 验收标准 |
|---|------|---------|
| 1 | 下载 BGE-M3 embedding 模型到本地（`HF_ENDPOINT=https://hf-mirror.com`） | 模型可加载 |
| 2 | 构建 Qdrant 索引：`python scripts/build_index.py --docs data/raw/wiki_docs --output data/index` | 索引文件生成 |
| 3 | 启动 RAG 服务：`python deploy/rag_serve.py --host 0.0.0.0 --port 8001` | 服务启动，`curl localhost:8001/healthz` 返回 ok |
| 4 | 测试 RAG 查询：`curl -X POST localhost:8001/query -d '{"query":"如何重启pod"}'` | 返回相关文档片段 |

**P2 验收门禁**：
- ✅ 数据合成、质量过滤、格式化全链路跑通（TODO B3 闭环）
- ✅ 评测脚本可执行
- ✅ RAG 服务本地可启动

---

### 阶段 P3：GPU 训练（3-5 天，对应 TODO B4，成本 ¥30-80）

> 需要租 GPU 服务器，推荐 AutoDL。

#### P3-A：服务器准备

| # | 操作 | 验收标准 |
|---|------|---------|
| 1 | 注册 AutoDL 账号，充值 ¥50 | 余额充足 |
| 2 | 租用实例：Ubuntu 22.04 + 4090 24GB + 10GB 系统盘 + 50GB 数据盘 | 实例 Running |
| 3 | PyCharm 配置 Remote SSH：Tools → Deployment → Configuration → 添加 AutoDL SSH 连接 | PyCharm 能编辑远程文件 |
| 4 | PyCharm 配置 Remote Interpreter：Settings → Project → Python Interpreter → SSH Interpreter | 远程 Python 环境可用 |
| 5 | 在远程服务器 clone 代码，安装完整依赖：`pip install -r requirements.txt` | 无致命错误 |
| 6 | 验证 CUDA：`python -c "import torch; print(torch.cuda.is_available(), torch.cuda.get_device_name())"` | `True, NVIDIA RTX 4090` |
| 7 | 验证 vLLM：`python -c "import vllm; print(vllm.__version__)"` | 版本 ≥ 0.7.0 |

#### P3-B：方向二 NPC SFT 训练（Qwen3-4B，优先做，快）

| # | 操作 | 验收标准 |
|---|------|---------|
| 1 | 上传 P2 阶段生成的训练数据到服务器 `data/processed/` | 数据就位 |
| 2 | 下载基座模型 Qwen3-4B（通过 `HF_ENDPOINT=https://hf-mirror.com`） | 模型文件就位 |
| 3 | Smoke 跑通：`SMOKE=1 bash scripts/run_npc_pipeline.sh` | 全链路无错退出 |
| 4 | 正式 SFT 训练：`llamafactory-cli train configs/npc_sft.yaml` | 训练 loss 下降，无 OOM |
| 5 | 合并 LoRA：`llamafactory-cli export --model_name_or_path Qwen/Qwen3-4B --adapter_name_or_path output/npc_sft --output_dir output/npc_sft_merged` | 合并后模型完整 |
| 6 | 快速评估：`python scripts/evaluate.py --model output/npc_sft_merged --golden data/test/npc_test.json --report eval/npc_sft_report.md` | 评测报告生成 |

#### P3-C：方向一 Knowledge SFT 训练（Qwen3-8B，选做）

| # | 操作 | 验收标准 |
|---|------|---------|
| 1 | 下载 Qwen3-8B 基座模型 | 模型就位 |
| 2 | 跑 knowledge pipeline：`SMOKE=1 bash scripts/run_knowledge_pipeline.sh` | Smoke 通过 |
| 3 | 正式 SFT：`llamafactory-cli train configs/knowledge_sft.yaml` | 训练完成 |
| 4 | 合并 LoRA + 评估 | 合并模型 + 评测报告 |

#### P3-D：DPO/GRPO 对齐（进阶，可选）

| # | 操作 | 验收标准 |
|---|------|---------|
| 1 | NPC DPO：`llamafactory-cli train configs/npc_dpo.yaml` | 训练完成 |
| 2 | NPC GRPO（对比实验）：`llamafactory-cli train configs/npc_grpo.yaml` | 训练完成 |
| 3 | 三模型对比评估 | 生成对比报告 |

**P3 验收门禁**：
- ✅ Smoke pipeline 全链路通过（TODO B4 闭环）
- ✅ 至少 1 个方向 SFT 训练完成 + 评估报告生成
- ✅ 训练产物（LoRA 权重 / merged 模型）保存在 `output/` 目录

---

### 阶段 P4：推理部署 + Agent 联调（2 天，对应 TODO B5 + C1，成本 ¥10-20）

#### P4-A：vLLM 推理服务启动

| # | 操作 | 验收标准 |
|---|------|---------|
| 1 | 在 GPU 服务器上启动 vLLM：`bash deploy/vllm_v1_server.sh`（或 `make serve-vllm`） | 服务启动，日志无 OOM |
| 2 | 验证模型列表：`curl http://localhost:8000/v1/models` | 返回模型 ID |
| 3 | 测试补全：`curl -X POST http://localhost:8000/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"npc-sft","messages":[{"role":"user","content":"你好"}]}'` | 返回正常 JSON 响应 |

#### P4-B：docker compose 推理栈（可选，更完整）

| # | 操作 | 验收标准 |
|---|------|---------|
| 1 | 构建推理镜像：`make docker-infer` | 镜像构建成功 |
| 2 | 启动推理栈：`docker compose -f deploy/docker-compose.infer.yml up -d` | vllm + qdrant + rag + prometheus 全 Up |
| 3 | 验证：`curl http://localhost:8000/v1/models` | 返回模型列表 |
| 4 | 验证 RAG：`curl http://localhost:8001/healthz` | 返回 ok |

#### P4-C：Agent + 自研模型联调（核心目标）

| # | 操作 | 验收标准 |
|---|------|---------|
| 1 | 在 GPU 服务器上保持 vLLM 运行（:8000） | vLLM 服务正常 |
| 2 | 在 GPU 服务器上 clone project-agent，修改 `.env`：`OPENAI_BASE_URL=http://localhost:8000/v1`、`OPENAI_API_KEY=any`、`MODEL_NAME=npc-sft` | 配置就位 |
| 3 | `make up` 启动 Agent 全栈 | Agent 连上 vLLM，无 "connection refused" |
| 4 | `make smoke` 冒烟测试 | SSE 响应中返回自研模型生成的内容 |
| 5 | CLI 深度对话：`make run-cli` → 输入角色扮演问题 | NPC 角色人格正常 |
| 6 | 从 Windows 本地远程访问 GPU 服务器上的 Agent（如 AutoDL 端口映射到本地） | 本地浏览器/curl 可访问 |

**P4 验收门禁**：
- ✅ `curl http://gpu-server:8000/v1/models` 返回模型列表（TODO B5 闭环）
- ✅ Agent 通过自研 vLLM 模型完成对话（TODO C1 闭环）
- ✅ Jaeger/Langfuse 可观测自研模型调用链路

---

### 阶段 P5：持续维护与迭代（持续）

#### 日常维护清单

| 频率 | 事项 | 命令/操作 |
|------|------|---------|
| 每次改代码 | 跑单测 | `make test`（agent）/ `pytest scripts/ -x`（llm） |
| 每次改代码 | 代码质量 | `make lint && make vet`（agent）/ `ruff check .`（llm） |
| 每次改 Prompt | 跑 eval | `make eval`（agent）/ `make eval`（llm） |
| 每周 | 更新依赖 | `go mod tidy`（agent）/ `pip install -U -r requirements.txt`（llm） |
| 每周 | 检查 PROGRESS.md 是否同步 | 对比 git log 更新进度 |
| 每月 | 安全审计 | 检查 `.env` 无明文密钥入库；检查 `HITL_DISABLE` 不为 1 |
| 按需 | GPU 训练/推理 | AutoDL 按需租用，用完即释放 |

#### 版本发布检查清单

```
□ make test       全 PASS
□ make lint       0 error
□ make build      二进制生成
□ make docker     镜像构建
□ make up && make smoke   全栈冒烟通过
□ helm upgrade --install   K8s 部署无错
□ go run ./src/cmd/preflight   全平台状态正确
□ PROGRESS.md 更新
□ CHANGELOG.md 更新
□ git tag 打版本号
```

#### 安全合规检查（对应 TODO S1-S4）

| # | 事项 | 状态 |
|---|------|------|
| S1 | 所有 token（`OPENAI_API_KEY` / `AUDIT_HMAC_KEY` 等）走 K8s Secret 或 Vault，不入库 | ⬜ |
| S2 | 生产环境**永远**不要设 `HITL_DISABLE=1` | ⬜ |
| S3 | `GONGFENG_ALLOW_AUTO_MERGE` / `DEVOPS_ALLOW_AUTO_OPS` 默认关，按治理流程开（Mock 模式下无实际效果） | ⬜ |
| S4 | 上 K8s 前先跑 `go run ./src/cmd/preflight` 看 REAL/MOCK/DISABLED 状态 | ⬜ |

---

## 八、TODO 项 → 阶段映射

| TODO 项 | 内容 | 对应阶段 | 状态 |
|---------|------|---------|------|
| A5 | `make up && make smoke` | P1 | ⬜ 待做 |
| B3 | CPU 子集（数据合成+评测+RAG） | P2 | ✅ Smoke 通过（对话合成+DPO偏好对，AutoDL 4090） |
| B4 | Linux GPU 跑通 `run_npc_pipeline.sh` | P3 | 🔄 进行中（环境就绪，即将 SFT 训练） |
| B5 | GPU 上 vLLM `:8000/v1/models` | P4 | ⬜ 待做 |
| C1 | 端到端联调 vLLM → Agent | P4 | ⬜ 待做 |
| S1-S4 | 安全合规 | P5 持续 | ⬜ 待做 |

---

## 九、总预算估算

| 阶段 | 预计费用 | 说明 |
|------|---------|------|
| P0 | ¥0 | 本地环境搭建 |
| P1 | ¥0 | Docker Desktop + minikube |
| P2 | ¥0-5 | 仅 API 调用费（DeepSeek/Moonshot） |
| P3 | ¥30-80 | AutoDL 4090 租用 |
| P4 | ¥10-20 | 延续 GPU 服务器 |
| P5 | ¥0-50/月 | 按需租用 |
| **合计** | **¥40-155** | 首轮全部跑通 |

---

## 十、联动启动顺序

```
project-llm 训练 → 量化 → vLLM serve :8000（OpenAI 兼容）
                                    ↓
project-agent  OPENAI_BASE_URL=http://vllm:8000/v1
              OPENAI_API_KEY=any
                                    ↓
           对外暴露 /v1/agent SSE
```

---

## 十一、甘特图总览

```
P0 环境搭建        Day 1
│████████████████████████████████████████│

P1 Agent 全栈验证  Day 2-4
│          ████████████████████████████████████████████████████│

P2 LLM 数据链路    Day 5-6
│                                  ████████████████████████████│

P3 GPU 训练        Day 7-11
│                                              ████████████████████████████████████████████████████│

P4 推理联调        Day 12-13
│                                                                                ████████████████████│

P5 持续维护        Day 14+
│                                                                                          ▶▶▶▶▶▶▶▶▶▶
```
