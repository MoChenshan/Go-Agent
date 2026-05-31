# Go-Agent 工程「部署运行」交付清单

> 来源：[GuideQ.txt](GuideQ.txt) 三条主诉求
> 范围：[project-agent](project-agent/)（Go / tRPC-Agent-Go）+ [project-llm](project-llm/)（Python / Qwen3 微调推理）
> 最后更新：2026-05-23

---

## 0. 总目标拆解（来自 GuideQ.txt）

- [x] **G1** 检查两个子项目能否正常运行、能否部署到服务器
- [x] **G2** 给出司内 / 司外、Linux / Windows 的运行选型建议
- [x] **G3** 输出不同内外网 / 不同 OS 的完整部署、启动、运行流程

> ✅ 上述三点的方案已在对话中给出（详见本轮回答）。下面是把方案**真正落到仓库**所需的执行项。

---

## 1. project-agent（GameOps Agent）

### 1.1 现状速查

- 入口：[main.go](project-agent/main.go)（HTTP / CLI 双模式）
- 容器化：[Dockerfile](project-agent/Dockerfile)（distroless 多阶段）
- 本地全栈：[docker-compose.yml](project-agent/docker-compose.yml)（agent + redis + pg + otel + jaeger + langfuse + prom + grafana）
- K8s：[deploy/helm](project-agent/deploy/helm)（HPA + PDB + RBAC + NetworkPolicy + ServiceMonitor）
- Mock 兜底：蓝鲸 / BCS / 工蜂 / 蓝盾 / TAPD / iWiki 全部支持
- 自检：`go run ./src/cmd/preflight`

### 1.2 待办

- [x] **A1** 删除仓库内的 `project-agent/gameops-agent.exe`（41MB，构建产物） ✅
- [x] **A2** 在 [project-agent/.gitignore](project-agent/.gitignore) 中追加 `*.exe` / `bin/` / `coverage.*` ✅
- [x] **A3** 新增 [project-agent/.env.example](project-agent/.env.example)（与 [docker-compose.yml](project-agent/docker-compose.yml) 及各平台环境变量对齐） ✅
- [x] **A4** 本机司内环境构建验证 ✅（2026-05-23 Win + Go 1.24.7）
  - `go build ./...`（默认 stub）✅
  - `go build -tags "a2a agui" .`（生产链路）✅
  - `go run ./src/cmd/preflight` 输出全部 Mock→司外场景 OK✅
  - `go test ./src/{audit,async,config,idempotency}/...` 全 PASS ✅
- [ ] **A5** 跑一次 `make up && make smoke` 验证本地 docker compose 全栈（需 Docker Desktop）
- [x] **A6** 输出 [project-agent/DEPLOY.md](project-agent/DEPLOY.md) ✅

### 1.3 部署矩阵（结论）

| 场景 | OS | 方式 |
|---|---|---|
| 开发联调 | Win / Linux | `make run` 或 `make run-cli` |
| 本地全栈 | Linux（Win 需 WSL2/Docker Desktop） | `make up` |
| 司内生产 | Linux K8s | `helm upgrade --install ... ./deploy/helm` |
| 司外演示 | Linux + Docker Compose | `make up` + LLM 接 DeepSeek/OpenAI + 三方全 Mock |

---

## 2. project-llm（Qwen3 微调 + 推理）

### 2.1 现状速查

- 训练：`llamafactory-cli train configs/{knowledge_sft,npc_sft,npc_dpo,npc_grpo}.yaml`
- 量化：`scripts/quantize_{fp8,awq,gguf}.{py,sh}`
- 推理：[deploy/vllm_v1_server.sh](project-llm/deploy/vllm_v1_server.sh) / sglang / llamacpp
- RAG：[deploy/rag_serve.py](project-llm/deploy/rag_serve.py)（FastAPI :8001）
- 容器：[Dockerfile.train](project-llm/Dockerfile.train) + [Dockerfile.infer](project-llm/Dockerfile.infer)
- Compose：[deploy/docker-compose.infer.yml](project-llm/deploy/docker-compose.infer.yml)（vllm + qdrant + rag + prom）

### 2.2 待办

- [x] **B1** 修复 [project-llm/Makefile](project-llm/Makefile) 中错误的 config 路径 ✅
  - `train-sft` / `train-dpo` 原引用的 `configs/qwen3_*.yaml` 不存在，已改为 `configs/$(DOMAIN)_sft.yaml` 等（DOMAIN=npc|knowledge）
  - `train-grpo` 指向 `configs/npc_grpo.yaml`
- [x] **B2** 修复 Makefile 及 [Dockerfile.infer](project-llm/Dockerfile.infer) 中的脚本路径 ✅
  - `quant-gptq` → `scripts/quantize_gptq_marlin.py`
  - `serve-vllm` → `deploy/vllm_v1_server.sh`；新增 `serve-sglang` / `serve-llamacpp` / `quant-gguf`
  - `edge-executorch-android` / `edge-executorch-ios` / `edge-mlc` / `edge-qnn`
  - Dockerfile.infer 删除对不存在的 `scripts/rag_query.py` 的 COPY
- [ ] **B3** 跑通 CPU 子集（数据合成 + 评测 + RAG），验证 Windows 也能用（需 Python 3.10 + .env API key）
- [ ] **B4** Linux + GPU 机上跑通 `bash scripts/run_npc_pipeline.sh`（SMOKE=1 即可）
- [ ] **B5** Linux + GPU 上 `make docker-infer && make up`，访问 `:8000/v1/models` 验证 vLLM
- [x] **B6** 输出 [project-llm/DEPLOY.md](project-llm/DEPLOY.md) ✅

### 2.3 部署矩阵（结论）

| 场景 | OS / 硬件 | 方式 |
|---|---|---|
| 数据合成 / 评测 / RAG | CPU 即可，Win/Linux/macOS | `pip install -r requirements.txt`（去掉 vllm/flash-attn/triton） |
| QLoRA-SFT/DPO/GRPO | **Linux + NVIDIA GPU**（4090/A100+） | `llamafactory-cli train ...` 或 `make train-sft` |
| vLLM/SGLang 推理 | **Linux + NVIDIA GPU** | `make up` 或 `bash deploy/vllm_v1_server.sh` |
| Windows 训练 / vLLM | ❌ 不支持 | 用 WSL2 + Ubuntu |

---

## 3. 联动启动顺序（两个项目串起来）

```
project-llm 训练 → 量化 → vLLM serve :8000（OpenAI 兼容）
                                    ↓
project-agent  OPENAI_BASE_URL=http://vllm:8000/v1
              OPENAI_API_KEY=any
                                    ↓
           对外暴露 /v1/agent SSE
```

- [ ] **C1** 在同一 Linux GPU 机上做一次端到端联调：vLLM ↑ → agent docker compose ↑ → `make smoke` 通过

---

## 4. 内/外网差异速查

| 项 | 司内 | 司外 |
|---|---|---|
| Go 模块代理 | `GOPROXY=https://goproxy.woa.com,direct` + `GOPRIVATE=git.woa.com,trpc.group` | `GOPROXY=https://goproxy.cn,direct`，外网用 `make build-stub` |
| pip 源 | `https://mirrors.tencent.com/pypi/simple` | `https://pypi.org/simple` 或阿里源 |
| HF 模型 | `HF_ENDPOINT=https://hf-mirror.com` | 直连 huggingface.co |
| LLM 后端 | 混元 `http://hunyuanapi.woa.com/openapi/v1` | DeepSeek / OpenAI |
| 蓝鲸/BCS/工蜂/蓝盾/TAPD/iWiki | 真实凭据 | **必须**全开 `*_API_MOCK=1` |
| Docker 基础镜像 | `mirrors.tencent.com/library/...` | docker.io 或自建镜像 |

---

## 5. 安全 / 合规清单

- [ ] **S1** 所有 token（`OPENAI_API_KEY` / `AUDIT_HMAC_KEY` / `BCS_TOKEN` / `GONGFENG_TOKEN` / `DEVOPS_TOKEN` / `TAPD_TOKEN`）走 K8s Secret 或 Vault，不入库
- [ ] **S2** 生产环境**永远**不要设 `HITL_DISABLE=1`
- [ ] **S3** `GONGFENG_ALLOW_AUTO_MERGE` / `DEVOPS_ALLOW_AUTO_OPS` 默认关，按治理流程开
- [ ] **S4** 上 K8s 前先跑 `go run ./src/cmd/preflight` 看 REAL/MOCK/DISABLED 状态

---

## 6. 进度条

| 阶段 | 状态 | 备注 |
|---|---|---|
| 方案输出 | ✅ Done | 见对话第一轮回答 |
| TODO 落档 | ✅ Done | 本文件 |
| project-agent 清理 + .env.example（A1~A3） | ✅ Done | gameops-agent.exe 已删；.gitignore 已增强；.env.example 已创建 |
| project-llm Makefile + Dockerfile 修复（B1~B2） | ✅ Done | 所有错误路径已修正 |
| project-agent 本机构建+测试验证（A4） | ✅ Done | go build / preflight / 单测 全通过 |
| DEPLOY.md 落档（A6 + B6） | ✅ Done | 两份部署手册已入库 |
| 文档整理（删冗余 + docs/INDEX.md） | ✅ Done | 删除 3 份完全重复的"大模型应用-...-1/2/3.md"（合订本已包含）；新建 [docs/INDEX.md](docs/INDEX.md) 作为反向导航；根 README 添加导航入口 |
| docker compose / GPU 实跑（A5、B3~B5、C1） | ⬜ 待做 | 需要 Docker Desktop、Linux GPU 环境 |

