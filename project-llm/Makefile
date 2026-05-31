# ============================================================
# project-llm — Makefile
# 训练 / 量化 / 推理 / 评测 / 部署 全链路一键命令
# ============================================================

SHELL := /bin/bash
.DEFAULT_GOAL := help

PYTHON ?= python
TAG    ?= latest
TRAIN_IMG ?= project-llm-train:$(TAG)
INFER_IMG ?= project-llm-infer:$(TAG)

##@ Help

.PHONY: help
help: ## 列出所有可用目标
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Setup

.PHONY: install
install: ## 安装 Python 依赖（推荐先建 venv）
	$(PYTHON) -m pip install --upgrade pip wheel
	$(PYTHON) -m pip install -r requirements.txt

.PHONY: docker-train
docker-train: ## 构建训练镜像
	docker build -f Dockerfile.train -t $(TRAIN_IMG) .

.PHONY: docker-infer
docker-infer: ## 构建推理镜像
	docker build -f Dockerfile.infer -t $(INFER_IMG) .

##@ Data

.PHONY: data-gen
data-gen: ## 合成 SFT/DPO/GRPO 数据（默认 NPC 域）
	$(PYTHON) scripts/generate_qa.py --task npc --output data/processed/npc_sft.jsonl --n 5000
	$(PYTHON) scripts/generate_qa.py --task ops --output data/processed/ops_sft.jsonl --n 3000

.PHONY: data-replay
data-replay: ## 数据飞轮回放（防遗忘）
	$(PYTHON) scripts/data_replay_buffer.py --base data/processed --new data/feedback --out data/processed/replay.jsonl

##@ Train

# DOMAIN 用于切换训练域：npc（默认，AINPC 对话）/ knowledge（知识库专家）
DOMAIN ?= npc

.PHONY: train-sft
train-sft: ## SFT 训练（LoRA + Liger-Kernel）；DOMAIN=npc|knowledge
	llamafactory-cli train configs/$(DOMAIN)_sft.yaml

.PHONY: train-dpo
train-dpo: ## DPO 偏好对齐；DOMAIN=npc|knowledge
	llamafactory-cli train configs/$(DOMAIN)_dpo.yaml

.PHONY: train-grpo
train-grpo: ## GRPO 强化学习（仅 npc 域提供 config）
	llamafactory-cli train configs/npc_grpo.yaml

##@ Quantize

.PHONY: quant-fp8
quant-fp8: ## FP8 量化（H100/H200）
	$(PYTHON) scripts/quantize_fp8.py --model ckpt/sft_merged --out ckpt/serve_fp8

.PHONY: quant-awq
quant-awq: ## AWQ-W4A16 量化（A10/L20 友好）
	$(PYTHON) scripts/quantize_awq.py --model ckpt/sft_merged --out ckpt/serve_awq

.PHONY: quant-gptq
quant-gptq: ## GPTQ-Marlin 量化
	$(PYTHON) scripts/quantize_gptq_marlin.py --model ckpt/sft_merged --out ckpt/serve_gptq

.PHONY: quant-gguf
quant-gguf: ## GGUF 多精度量化（端侧 / CPU 推理）
	bash scripts/quantize_gguf.sh

##@ Serve

.PHONY: serve-vllm
serve-vllm: ## vLLM v1 启动（含 EAGLE-3，PROFILE=bf16|fp8|gptq_marlin|fp8_eagle3）
	bash deploy/vllm_v1_server.sh

.PHONY: serve-sglang
serve-sglang: ## SGLang 启动（多轮对话优化）
	bash deploy/sglang_server.sh

.PHONY: serve-llamacpp
serve-llamacpp: ## llama.cpp 启动（CPU/端侧）
	bash deploy/llamacpp_server.sh

.PHONY: serve-vllm-multi
serve-vllm-multi: ## vLLM LoRA 多租户启动
	bash deploy/vllm_lora_multi.sh

.PHONY: serve-rag
serve-rag: ## RAG 服务启动（FastAPI :8001）
	$(PYTHON) deploy/rag_serve.py --host 0.0.0.0 --port 8001

.PHONY: up
up: docker-infer ## 一键 docker compose 起 vllm + rag + qdrant
	docker compose -f deploy/docker-compose.infer.yml up -d

.PHONY: down
down: ## 关停推理栈
	docker compose -f deploy/docker-compose.infer.yml down

##@ Eval

.PHONY: eval
eval: ## 跑全套评测（精确指标 + LLM Judge + RAGAS）
	$(PYTHON) scripts/evaluate.py --golden eval/golden_50.jsonl --report eval/report_$(shell date +%Y%m%d).md

.PHONY: eval-ragas
eval-ragas: ## 单独跑 RAGAS 评测
	$(PYTHON) scripts/ragas_eval.py --golden eval/golden_50.jsonl

.PHONY: eval-redteam
eval-redteam: ## 越狱 / 注入红队评测（安全门禁）
	$(PYTHON) scripts/red_team_eval.py --golden eval/red_team.jsonl --threshold 0.95

.PHONY: bench-speculative
bench-speculative: ## 投机解码加速比基准
	$(PYTHON) infra/inference/bench_speculative.py --concurrency 1,4,16,64

##@ Edge

.PHONY: edge-executorch-android
edge-executorch-android: ## 导出 ExecuTorch (Android XNNPACK)
	$(PYTHON) deploy/executorch/export_android_xnn.py

.PHONY: edge-executorch-ios
edge-executorch-ios: ## 导出 ExecuTorch (iOS CoreML)
	$(PYTHON) deploy/executorch/export_ios_coreml.py

.PHONY: edge-mlc
edge-mlc: ## 编译 MLC LLM（跨平台端侧）
	bash deploy/mlc/compile.sh

.PHONY: edge-qnn
edge-qnn: ## 转换 QNN（高通 NPU）
	bash deploy/qnn/convert.sh

##@ Clean

.PHONY: clean
clean: ## 清理临时产物（不含 ckpt / data）
	rm -rf .pytest_cache __pycache__ */__pycache__
	find . -name "*.pyc" -delete
