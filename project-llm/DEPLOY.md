# project-llm — 部署运行手册（DEPLOY.md）

> 本文档把「数据合成 / 训练 / 量化 / 推理 / 端侧导出 / 评测」全链路的运行流程整理清楚。
> 配套清单：[TODO.md](../TODO.md)、入口：[Makefile](Makefile)、镜像：[Dockerfile.train](Dockerfile.train) + [Dockerfile.infer](Dockerfile.infer)、推理 Compose：[deploy/docker-compose.infer.yml](deploy/docker-compose.infer.yml)。

---

## 1. 总览（部署矩阵）

| 场景 | OS / 硬件 | 推荐方式 |
|---|---|---|
| 数据合成 / 评测 / RAG 服务 | CPU 即可，Win / Linux / macOS | `pip install -r requirements.txt`（去掉 vllm/flash-attn/triton） |
| QLoRA-SFT / DPO / GRPO | **Linux + NVIDIA GPU**（4090 24GB / A100 40GB+） | `make train-sft DOMAIN=npc` |
| FP8 / GPTQ / AWQ 量化 | **Linux + NVIDIA GPU** | `make quant-fp8 / quant-awq / quant-gptq` |
| vLLM / SGLang 推理 | **Linux + NVIDIA GPU** | `make up`（docker compose） |
| 端侧导出（ExecuTorch / QNN / MLC / GGUF） | Linux 优先；MLC / GGUF 跨平台 | `make edge-mlc` / `make quant-gguf` |
| Windows 训练 / vLLM | ❌ 原生不支持 | 用 WSL2 + Ubuntu |

> ⚠ Windows 用户**只能**跑「数据合成 + 评测 + RAG 服务」这部分子集；任何涉及 `bitsandbytes` / `flash-attn` / `triton` / `deepspeed` / `vllm` 的命令都需要 Linux + NVIDIA GPU。

---

## 2. 前置依赖

### 2.1 通用

| 依赖 | 版本 |
|---|---|
| Python | ≥ 3.12 |
| pip / wheel | 最新 |
| `.env` 凭据 | 见 [.env.example](.env.example) |

### 2.2 GPU 训练 / 推理（Linux）

| 依赖 | 版本 |
|---|---|
| OS | Ubuntu 22.04 / CentOS 7+ |
| NVIDIA 驱动 | ≥ 560（CUDA 12.8 兼容） |
| CUDA | 12.8 |
| cuDNN | 9 |
| NVIDIA Container Toolkit | 最新 |
| Docker | ≥ 20.10 |

### 2.3 内 / 外网差异

| 项 | 司内 | 司外 |
|---|---|---|
| pip 源 | `https://mirrors.tencent.com/pypi/simple` | `https://pypi.org/simple` 或阿里源 |
| HuggingFace | `HF_ENDPOINT=https://hf-mirror.com`（已写在 .env.example） | 直连 huggingface.co |
| LLM API（合成 / 评测 judge） | 混元 / 内部代理 | DeepSeek / Moonshot / OpenAI |
| Docker 基础镜像 | `mirrors.tencent.com/library/nvidia/cuda:12.8.1-...` | `nvidia/cuda:12.8.1-...`（docker.io） |

---

## 3. 流程 A：CPU 子集（Win / Linux / macOS 通用）

> 适用：数据合成、评测、RAG 服务，**不训练 / 不上 vLLM**。

```bash
# 1) Python 环境
conda create -n project-llm python=3.12 -y
conda activate project-llm

# 2) 装依赖（去掉 GPU-only 包）
pip install -r requirements.txt --extra-index-url https://download.pytorch.org/whl/cpu
# Windows / 无 GPU：以下包大概率装不上，跳过即可
pip uninstall -y vllm flash-attn triton deepspeed bitsandbytes 2>/dev/null || true

# 3) 配 .env
cp .env.example .env
# Windows: copy .env.example .env
# 至少填入 DEEPSEEK_API_KEY / MOONSHOT_API_KEY / OPENAI_API_KEY 之一

# 4) 跑数据合成
python scripts/generate_qa.py --task npc --output data/processed/npc_sft.jsonl --n 100
python scripts/data_quality.py \
    --input data/processed/npc_sft.jsonl \
    --output data/processed/npc_sft_filtered.jsonl

# 5) 跑评测（外接 OpenAI 兼容 API）
python scripts/evaluate.py \
    --golden eval/golden_50.jsonl \
    --report eval/report_local.md

# 6) 跑 RAG 服务（FastAPI :8001）
python deploy/rag_serve.py --host 0.0.0.0 --port 8001
curl http://localhost:8001/healthz
```

---

## 4. 流程 B：Linux + GPU 训练全链路

```bash
# 0) 系统准备（Ubuntu 22.04 为例）
sudo apt update && sudo apt install -y build-essential git curl
nvidia-smi   # 必须能看到 GPU

# 1) Python + Torch（与 requirements.txt 对齐）
conda create -n project-llm python=3.12 -y
conda activate project-llm
pip install torch==2.8.0 torchvision==0.23.0 \
    --index-url https://download.pytorch.org/whl/cu128
pip install -r requirements.txt

# 2) 配 .env
cp .env.example .env

# 3) 一键流水线
bash scripts/run_npc_pipeline.sh           # 完整 SFT → DPO → GRPO → 评测
SMOKE=1 bash scripts/run_npc_pipeline.sh   # 数据链路 smoke test

# 或分步（Makefile 已修正路径，DOMAIN=npc | knowledge）：
make train-sft DOMAIN=npc      # llamafactory-cli train configs/npc_sft.yaml
make train-dpo DOMAIN=npc      # configs/npc_dpo.yaml
make train-grpo                # configs/npc_grpo.yaml（仅 npc 域）

# 4) 量化
make quant-fp8     # H100 / L40S
make quant-awq     # A100 / 4090
make quant-gptq    # GPTQ-Marlin
make quant-gguf    # GGUF（端侧 / CPU）
```

> 训练脚本 `run_*_pipeline.sh` 的 `SMOKE=1` 模式用最小数据量跑通流程，适合首次环境验证。

---

## 5. 流程 C：Linux + GPU 推理服务

### 5.1 Docker Compose 一键起（推荐）

```bash
# 0) NVIDIA Container Toolkit
distribution=$(. /etc/os-release;echo $ID$VERSION_ID)
curl -s -L https://nvidia.github.io/libnvidia-container/$distribution/libnvidia-container.list \
    | sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list
sudo apt update && sudo apt install -y nvidia-container-toolkit
sudo systemctl restart docker

# 1) 把训练产物放到 ./ckpt/serve/
ls ckpt/serve/   # 应能看到 config.json + safetensors

# 2) 构建推理镜像
make docker-infer

# 3) 起 vllm + qdrant + rag + prometheus
make up

# 4) 验证
curl http://localhost:8000/v1/models     # vLLM
curl http://localhost:8001/healthz       # RAG
curl http://localhost:6333/collections   # Qdrant
curl http://localhost:9090/-/healthy     # Prometheus

# 5) 关停
make down
```

### 5.2 裸跑 vLLM

```bash
export VLLM_USE_V1=1
PROFILE=fp8_eagle3 \
    MODEL_PATH=./output/knowledge_fp8 \
    bash deploy/vllm_v1_server.sh
# PROFILE: bf16 | fp8 | gptq_marlin | fp8_eagle3
```

或通过 Makefile：

```bash
make serve-vllm        # 默认 fp8_eagle3
make serve-sglang      # SGLang
make serve-llamacpp    # llama.cpp（CPU/端侧）
```

---

## 6. 端侧部署

| 平台 | 命令 | OS |
|---|---|---|
| Android (XNNPACK) | `make edge-executorch-android` | Linux |
| iOS (CoreML) | `make edge-executorch-ios` | macOS |
| 高通 NPU (QNN) | `make edge-qnn` | Linux + QAIRT SDK |
| MLC（跨平台） | `make edge-mlc` | Linux/Win/macOS |
| GGUF（llama.cpp） | `make quant-gguf` + `make serve-llamacpp` | 全平台（CPU 也行） |

---

## 7. 与 project-agent 联动

让 [project-agent](../project-agent) 用本项目训出的模型作为 LLM 后端：

```bash
# 在 GPU 机上
cd project-llm
make up                                          # vLLM :8000

# 在同机或另一台
cd ../project-agent
export OPENAI_BASE_URL=http://<gpu-host>:8000/v1
export OPENAI_API_KEY=any                        # vLLM 不校验
export MODEL_NAME=knowledge-expert               # 与 vllm --served-model-name 对齐
make up
```

vLLM 暴露的是 OpenAI 兼容协议，agent 端零改动即可对接。

---

## 8. 环境变量速查（详见 [.env.example](.env.example)）

| 类别 | 关键变量 |
|---|---|
| 数据合成 LLM | `DEEPSEEK_API_KEY` / `MOONSHOT_API_KEY` / `OPENAI_API_KEY` |
| HuggingFace 镜像 | `HF_ENDPOINT=https://hf-mirror.com`（推荐内网） |
| Embedding 模型 | `EMBED_MODEL=BAAI/bge-m3` |
| 训练观测 | `WANDB_API_KEY` / `LANGFUSE_PUBLIC_KEY` / `LANGFUSE_SECRET_KEY` |
| 可观测性 | `OTEL_EXPORTER_OTLP_ENDPOINT` / `OTEL_SERVICE_NAME` |
| GPU | `CUDA_VISIBLE_DEVICES` / `USE_FA3`（Hopper 才开） |

---

## 9. 故障排查

| 现象 | 可能原因 | 处理 |
|---|---|---|
| `pip install` 卡在 `flash-attn` | 没有 GPU 或 CUDA 不匹配 | CPU 子集场景：`pip uninstall flash-attn`；GPU 场景：检查 `nvidia-smi` 与 CUDA 12.8 |
| `llamafactory-cli train` 报 `No module named 'bitsandbytes'` | bnb 在 Windows 装不上 | 必须 Linux + GPU |
| vLLM 起来但 `--served-model-name` 不一致 | agent 调用时 `MODEL_NAME` 对不上 | 二者保持完全一致 |
| Docker compose 找不到 GPU | NVIDIA Container Toolkit 没装 / 没重启 docker | `sudo systemctl restart docker` |
| HuggingFace 下载超时 | 没设 `HF_ENDPOINT` | 内网必须设 `https://hf-mirror.com` |
| `make train-sft` 抱怨 config 不存在 | 早期 Makefile 用错路径（已修） | `git pull` 拉最新；或显式 `llamafactory-cli train configs/npc_sft.yaml` |

---

## 10. 安全 / 合规

- ❌ `.env` 不入库（已在 `.gitignore`）
- ❌ HuggingFace token、API key 不提交到仓库
- ✅ 训练产物（`ckpt/`、`output/`）按公司模型管理规范存放，**不**入 git
- ✅ Langfuse / WanDB 用最小可见域；OTel exporter 默认 `localhost`
