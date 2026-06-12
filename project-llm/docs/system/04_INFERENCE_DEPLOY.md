# 推理部署架构详解（Inference & Deployment）

> **文档定位**：本文档对 `project-llm` 项目的推理部署层进行完整技术解析，覆盖 7 种部署方案、EAGLE-3 投机解码、PD 分离架构、LoRA 多租户、端侧部署矩阵、容器化编排与性能 Benchmark。
>
> **前置阅读**：`03_QUANTIZATION.md`（量化产物是部署的输入）

---

## 一、部署层全景架构

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         推理部署层（deploy/）                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────────── GPU 服务端 ───────────────────┐                       │
│  │                                                   │                       │
│  │  ┌─────────────┐   ┌─────────────┐               │                       │
│  │  │  vLLM V1    │   │   SGLang    │               │                       │
│  │  │ +EAGLE-3    │   │ +RadixAttn  │               │                       │
│  │  │ +FP8/GPTQ   │   │ +EAGLE-3    │               │                       │
│  │  └──────┬──────┘   └──────┬──────┘               │                       │
│  │         │                  │                       │                       │
│  │         ▼                  ▼                       │                       │
│  │  ┌──────────────────────────────┐                 │                       │
│  │  │  LoRA 多租户热加载            │                 │                       │
│  │  │  (vllm_lora_multi.sh)        │                 │                       │
│  │  └──────────────────────────────┘                 │                       │
│  └───────────────────────────────────────────────────┘                       │
│                                                                             │
│  ┌─────────────────── CPU / 端侧 ──────────────────┐                       │
│  │                                                   │                       │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐       │                       │
│  │  │llama.cpp │  │  Ollama  │  │ExecuTorch│       │                       │
│  │  │ GGUF     │  │Modelfile │  │ XNN/CML  │       │                       │
│  │  └──────────┘  └──────────┘  └──────────┘       │                       │
│  │                                                   │                       │
│  │  ┌──────────┐  ┌──────────┐                      │                       │
│  │  │ QNN HTP  │  │ MLC-LLM  │                      │                       │
│  │  │ NPU INT8 │  │ WebGPU   │                      │                       │
│  │  └──────────┘  └──────────┘                      │                       │
│  └───────────────────────────────────────────────────┘                       │
│                                                                             │
│  ┌─────────────────── 高级架构 ────────────────────┐                        │
│  │  PD 分离（Prefill/Decode Disaggregation）         │                        │
│  │  容器化编排（Docker Compose）                      │                        │
│  │  Prometheus 指标采集                              │                        │
│  └───────────────────────────────────────────────────┘                       │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 二、GPU 服务端部署

### 2.1 vLLM V1 引擎（主力方案）

**文件**：`deploy/vllm_v1_server.sh`

#### 2.1.1 核心特性

| 特性 | 说明 |
|------|------|
| **PagedAttention V2** | KV Cache 分页管理，消除显存碎片，利用率 >95% |
| **Continuous Batching** | 动态批处理，请求级调度，无需等待 batch 填满 |
| **Chunked Prefill** | 长 prompt 分块处理，避免阻塞 decode 请求，降低 TTFT 尾延迟 |
| **Prefix Caching** | 相同 system prompt 的 KV Cache 复用，多轮对话命中率高 |
| **EAGLE-3 投机解码** | 原生支持，吞吐提升 60~75%，精度无损 |
| **多量化 Kernel** | FP8 E4M3 / GPTQ-Marlin / AWQ 原生加速 |

#### 2.1.2 四档启动配置

脚本通过 `PROFILE` 环境变量切换四种部署档位：

```bash
# 环境变量控制
PROFILE="${PROFILE:-fp8_eagle3}"   # 默认最优档
export VLLM_USE_V1=1               # 强制启用 V1 引擎
export VLLM_ATTENTION_BACKEND=FLASH_ATTN  # Hopper 可切 FLASHINFER
```

| 档位 | 模型格式 | 投机解码 | 适用硬件 | 典型吞吐 |
|------|---------|---------|---------|---------|
| `bf16` | BF16 原始 | ❌ | 任意 GPU | 78 tok/s |
| `fp8` | FP8 E4M3 | ❌ | H100/H200/L40S | 102 tok/s |
| `gptq_marlin` | GPTQ INT4 + Marlin | ❌ | A100/4090 | 145 tok/s |
| `fp8_eagle3` ⭐ | FP8 + EAGLE-3 | ✅ | H100/L40S | **165 tok/s** |

#### 2.1.3 关键启动参数解析

```bash
vllm serve "$MODEL_PATH" \
    --port "$PORT" \
    --tensor-parallel-size "$TP_SIZE" \          # 张量并行（多卡切分）
    --dtype auto \                               # 自动检测模型精度
    --max-model-len "$MAX_MODEL_LEN" \           # 最大上下文长度 32768
    --gpu-memory-utilization "$GPU_UTIL" \       # GPU 显存占用上限 0.90
    --enable-prefix-caching \                    # V1 内置前缀缓存
    --enable-chunked-prefill \                   # 长 prompt 分块（V1 默认开启）
    --served-model-name "$SERVED_NAME" \         # OpenAI 兼容 model name
    --trust-remote-code \
    $EXTRA                                       # 投机解码 / 量化参数
```

#### 2.1.4 EAGLE-3 投机解码配置

```bash
# fp8_eagle3 档位的 EXTRA 参数
EXTRA="--speculative-config {
    \"method\": \"eagle3\",
    \"model\": \"yuhuili/EAGLE3-Qwen3-8B\",
    \"num_speculative_tokens\": 5
}"
```

**工作原理**：

```
传统解码：  [prompt] → target → tok1 → target → tok2 → target → tok3
            每个 token 都要跑一次 target 模型

EAGLE-3：   [prompt] → target → tok1
                    → draft → [tok2, tok3, tok4, tok5, tok6]（一次出 5 个）
                    → target verify → accept k∈[0,5] 个
                    → 下一轮从 tok(1+k) 继续
            draft 模型仅为 target 的 1~3% 参数量
```

**关键指标**：
- 接受率（accept rate）：典型 ~70%，目标 ≥ 0.65
- draft token 数：默认 5，过大浪费 draft 算力
- 验证方式：`curl http://localhost:8000/metrics | grep spec_decode_`

**自训 draft 模型**（当社区 draft 接受率 < 50% 时）：

```bash
git clone https://github.com/SafeAILab/EAGLE.git
python -m eagle.train.main_eagle3 \
    --basepath ./output/knowledge_sft_merged \
    --configpath configs/Qwen3-8B-eagle3.json \
    --datapath data/processed/knowledge_qa.json \
    --bs 4 --lr 3e-5 --gradient_accumulation_steps 16
```

---

### 2.2 SGLang（多轮对话优化）

**文件**：`deploy/sglang_server.sh`

#### 2.2.1 核心优势：RadixAttention

SGLang 的 **RadixAttention** 使用 Radix Tree（基数树）管理 KV Cache 前缀：
- 多轮对话中相同的 system prompt + 历史消息自动命中缓存
- 命中率可达 **85%+**（Agent 场景）
- 相比 vLLM 的 prefix caching，粒度更细、命中率更高

#### 2.2.2 启动配置

```bash
python -m sglang.launch_server \
  --model-path "$MODEL_PATH" \
  --port "$PORT" \
  --tp "$TP_SIZE" \
  --enable-radix-cache \              # 核心：Radix Tree KV 缓存
  --mem-fraction-static 0.85 \        # 静态显存分配比例
  --context-length 32768 \
  --served-model-name knowledge-expert \
  --trust-remote-code \
  # EAGLE-3 投机解码（sglang >= 0.4.0）
  --speculative-algorithm EAGLE3 \
  --speculative-draft-model-path yuhuili/EAGLE3-Qwen2.5-8B \
  --speculative-num-steps 5 \
  --speculative-eagle-topk 8 \
  --speculative-num-draft-tokens 32
```

#### 2.2.3 vLLM vs SGLang 选型

| 维度 | vLLM V1 | SGLang |
|------|---------|--------|
| 单轮吞吐 | ⭐ 略优 | 接近 |
| 多轮命中率 | 中等 | ⭐ 85%+ |
| Agent 场景 | 好 | ⭐ 最优 |
| 生态成熟度 | ⭐ 最大 | 快速追赶 |
| EAGLE-3 支持 | ⭐ 原生 | 原生 |
| 推荐场景 | 通用问答 / RAG | 多轮 Agent / NPC |

---

### 2.3 LoRA 多租户热加载

**文件**：`deploy/vllm_lora_multi.sh`

#### 2.3.1 设计理念

一份基座模型 + 多个 LoRA adapter 同进程加载，按请求参数选择 adapter：
- 显存占用 ≈ 基座(FP16/AWQ) + 每个 adapter（数十 MB）
- 适用场景：多业务共存 / A/B 测试 / 灰度回滚

#### 2.3.2 目录结构

```
/ckpt/loras/
  ├── npc_v2/        adapter_config.json + adapter_model.safetensors
  ├── ops_v1/        运维知识 LoRA
  └── customer_v1/   客服 LoRA
```

#### 2.3.3 关键参数

```bash
ARGS=(
  --model "${BASE_MODEL}"
  --enable-lora                      # 开启 LoRA 支持
  --max-loras "${MAX_LORAS}"         # 同时加载数量上限（默认 8）
  --max-lora-rank "${MAX_LORA_RANK}" # LoRA rank 上限（默认 64）
  --lora-modules "${LORA_MODULES[@]}" # adapter 列表（name=path 格式）
)
```

#### 2.3.4 请求时指定 adapter

```bash
curl http://localhost:8000/v1/chat/completions \
  -d '{
    "model": "npc_v2",
    "messages": [{"role": "user", "content": "你好"}]
  }'
```

---

## 三、CPU / 端侧部署

### 3.1 llama.cpp（CPU 高性能推理）

**文件**：`deploy/llamacpp_server.sh`

#### 3.1.1 适用场景

- 服务器 CPU（含 AMX 指令集）/ 云主机无 GPU
- PC 游戏内嵌 NPC 对话

#### 3.1.2 启动配置

```bash
export GGML_AMX=1  # Intel Sapphire Rapids / Emerald Rapids AMX 加速

"$LLAMA_CPP_DIR/build/bin/llama-server" \
  --model "$MODEL_GGUF" \           # GGUF 量化模型
  --host 0.0.0.0 --port "$PORT" \
  --threads "$THREADS" \            # 建议 = 物理核数
  --ctx-size "$CTX_SIZE" \          # 上下文长度 8192
  --parallel 4 \                    # 并发请求数
  --cont-batching \                 # 连续批处理
  --metrics \                       # Prometheus 指标端点
  --jinja                           # 使用模型自带 chat template（Qwen3）
```

#### 3.1.3 关键技术

| 技术 | 说明 |
|------|------|
| GGUF 格式 | 单文件包含权重 + 元数据 + tokenizer |
| AMX 指令集 | Intel 第四代 Xeon 矩阵加速，BF16 吞吐翻倍 |
| Continuous Batching | 多请求动态调度 |
| Jinja Template | 自动使用 Qwen3 的 `<\|im_start\|>` 格式 |

---

### 3.2 Ollama（最简端侧部署）

**文件**：`deploy/Modelfile`

#### 3.2.1 Modelfile 配置解析

```dockerfile
FROM ./output/npc_gguf/npc-4b-q4_k_m.gguf

# 采样参数
PARAMETER temperature 0.8
PARAMETER top_p 0.9
PARAMETER top_k 40
PARAMETER repeat_penalty 1.05
PARAMETER num_ctx 8192
PARAMETER num_predict 512

# Qwen3 停止符
PARAMETER stop "<|im_end|>"
PARAMETER stop "<|endoftext|>"

# Chat Template（支持 Thinking Mode）
TEMPLATE """{{- if .System }}<|im_start|>system
{{ .System }}<|im_end|>
{{ end -}}
{{- range .Messages }}<|im_start|>{{ .Role }}
{{ .Content }}<|im_end|>
{{ end -}}<|im_start|>assistant
"""

# 默认 System Prompt
SYSTEM """你是游戏世界中的一名 NPC。请始终保持角色设定，用第一人称与玩家对话。
如果玩家提出的问题需要复杂推理或剧情判断，请在回复前用 <think>...</think> 进行内部思考。
"""
```

#### 3.2.2 使用方式

```bash
# 创建模型
ollama create npc-zhang -f deploy/Modelfile

# 运行对话
ollama run npc-zhang

# API 调用（OpenAI 兼容）
curl http://localhost:11434/api/chat -d '{
  "model": "npc-zhang",
  "messages": [{"role": "user", "content": "铁匠铺在哪里？"}]
}'
```

---

### 3.3 ExecuTorch（iOS / Android 原生）

**文件**：`deploy/executorch/export_android_xnn.py`、`deploy/executorch/export_ios_coreml.py`

#### 3.3.1 Android XNNPACK 导出

**核心流程**：HF 模型 → params.json 映射 → ExecuTorch export_llama → `.pte` 产物

```python
def build_model_params_json(hf_dir: Path, out_dir: Path) -> Path:
    """从 HF config.json 映射生成 ExecuTorch 所需的 params.json"""
    cfg = json.loads((hf_dir / "config.json").read_text(encoding="utf-8"))
    params = {
        "dim": cfg["hidden_size"],
        "n_layers": cfg["num_hidden_layers"],
        "n_heads": cfg["num_attention_heads"],
        "n_kv_heads": cfg.get("num_key_value_heads", cfg["num_attention_heads"]),
        "vocab_size": cfg["vocab_size"],
        "norm_eps": cfg.get("rms_norm_eps", 1e-5),
        "max_seq_len": cfg.get("max_position_embeddings", 8192),
        "rope_theta": cfg.get("rope_theta", 1000000.0),
        "use_scaled_rope": False,
    }
    # ...
```

**导出命令**：

```bash
python deploy/executorch/export_android_xnn.py \
    --model_path ./output/npc_merged \
    --output ./output/npc_edge/npc-android-xnn.pte \
    --quant_bits 4 --group_size 128 --seq_len 2048
```

**关键参数**：

| 参数 | 说明 | 推荐值 |
|------|------|--------|
| `--quant_bits` | 量化位宽 | 4（INT4g128） |
| `--group_size` | 量化分组大小 | 128 |
| `--seq_len` | 最大序列长度 | 2048 |
| `--use_sdpa_with_kv_cache` | SDPA+KV Cache 加速 | 开启（2-3× 加速） |

**产物**：`.pte` 文件，Android 端通过 JNI + ExecuTorch Runtime 加载。

#### 3.3.2 iOS CoreML 导出

**核心差异**：走 Apple Neural Engine (ANE) 加速，需 macOS 环境。

```bash
python deploy/executorch/export_ios_coreml.py \
    --model_path ./output/npc_merged \
    --output ./output/npc_edge/npc-ios-coreml.pte \
    --quant_bits 4 --compute_unit ALL
```

**compute_unit 选项**：

| 选项 | 说明 | 推荐设备 |
|------|------|---------|
| `CPU_ONLY` | 仅 CPU | 调试用 |
| `CPU_AND_GPU` | CPU + GPU | 旧设备兼容 |
| `CPU_AND_NE` | CPU + ANE | ⭐ A17 Pro+ 推荐 |
| `ALL` | 自动选择 | 默认推荐 |

**关键配置差异**：
- `--group_size 32`：CoreML 推荐（ANE 友好）
- `--dtype-override fp16`：iOS 用 FP16（Android 用 FP32）
- `--minimum_deployment_target iOS17`：A17 Pro 起步

---

### 3.4 QNN（高通 NPU 加速）

**文件**：`deploy/qnn/convert.sh`、`deploy/qnn/quant_config.json`

#### 3.4.1 转换流水线

```mermaid
graph LR
    A[HF 模型] -->|optimum-cli| B[ONNX]
    B -->|qnn-onnx-converter| C[DLC 未量化]
    C -->|calibration + INT8| D[DLC 量化]
    D -->|qnn-context-binary-generator| E[HTP Binary .bin]
```

**四步转换**：

1. **HF → ONNX**（optimum）：
```bash
optimum-cli export onnx \
    --model "$MODEL_HF" \
    --task text-generation-with-past \
    --opset 17 --device cpu --trust-remote-code \
    "$OUT_DIR/onnx_raw/"
```

2. **Calibration 数据准备**：从 SFT 训练数据中抽取 128 条样本，tokenize 后写入 `.raw` 文件

3. **ONNX → DLC + INT8 量化**：
```bash
qnn-onnx-converter \
    --input_network "$MODEL_ONNX" \
    --output_path "$MODEL_DLC_Q" \
    --quantization_overrides "$QUANT_CONFIG" \
    --input_list "$CALIB_RAW" \
    --act_bitwidth 8 --weight_bitwidth 8 --bias_bitwidth 32
```

4. **DLC → HTP Binary**：
```bash
qnn-context-binary-generator \
    --model "$MODEL_DLC_Q" \
    --backend libQnnHtp.so \
    --binary_file "$MODEL_BIN"
```

#### 3.4.2 混合精度量化配置

```json
{
    "activation_encodings": {
        "_default": { "bitwidth": 8, "dtype": "int", "is_symmetric": "False" }
    },
    "param_encodings": {
        "_default": { "bitwidth": 8, "dtype": "int", "is_symmetric": "True" },
        "lm_head.weight": { "bitwidth": 16, "dtype": "int" },
        "model.embed_tokens.weight": { "bitwidth": 16, "dtype": "int" }
    },
    "op_type": {
        "LayerNorm": { "activation_encodings": {"bitwidth": 16} },
        "Softmax": { "activation_encodings": {"bitwidth": 16} }
    }
}
```

**设计思路**：
- 默认 INT8（权重 + 激活）
- `lm_head` 和 `embed_tokens` 保留 INT16（对精度敏感）
- `LayerNorm` 和 `Softmax` 激活用 INT16（数值稳定性）

---

### 3.5 MLC-LLM（跨平台统一部署）

**文件**：`deploy/mlc/compile.sh`、`deploy/mlc/README.md`

#### 3.5.1 核心优势

**唯一能用一套工具链同时编译 iOS / Android / WebGPU / Windows / Linux** 的大模型部署框架。

#### 3.5.2 三步编译流程

```bash
# Step 1：权重转换（HF → MLC 格式，只需一次）
mlc_llm convert_weight "$MODEL_HF" --quantization "$QUANT" -o "$OUT_DIR"

# Step 2：生成 mlc-chat-config（含 conv template）
mlc_llm gen_config "$MODEL_HF" \
    --quantization "$QUANT" \
    --conv-template qwen3 \
    --context-window-size 8192 \
    -o "$OUT_DIR"

# Step 3：编译目标端 library
mlc_llm compile "$OUT_DIR/mlc-chat-config.json" \
    --device "$DEVICE" --host "$HOST" \
    -o "$ARTIFACT"
```

#### 3.5.3 四端编译目标

```bash
# 通过 TARGET 环境变量一键切换
TARGET=android  bash deploy/mlc/compile.sh   # Vulkan/OpenCL
TARGET=iphone   bash deploy/mlc/compile.sh   # Metal
TARGET=webgpu   bash deploy/mlc/compile.sh   # 浏览器 WebGPU
TARGET=windows  bash deploy/mlc/compile.sh   # Vulkan 桌面端
```

#### 3.5.4 量化档位

| 量化 | 含义 | 适用 | 体积 (Qwen3-4B) |
|------|------|------|-----------------|
| `q4f16_1` | 权重 INT4 / 激活 FP16 / group=32 | ⭐ 推荐端侧 | ~2.4 GB |
| `q4f32_1` | 权重 INT4 / 激活 FP32 / group=32 | 数值稳定 | ~2.5 GB |
| `q0f16` | 无量化 FP16 | Web Demo | ~8 GB |

---

## 四、端侧部署决策矩阵

### 4.1 五种方案对比

| 维度 | MLC-LLM | ExecuTorch | QNN HTP | llama.cpp | Ollama |
|-----|---------|-----------|---------|-----------|--------|
| **编译产物** | `.tar` + 权重 | `.pte` | `.dlc`/`.bin` | `.gguf` | `.gguf` + Modelfile |
| **覆盖平台** | iOS/Android/Web/Win | iOS/Android | 仅 Snapdragon | 全平台 | 桌面/服务器 |
| **加速硬件** | Metal/Vulkan/WebGPU | ANE/XNNPACK | HTP NPU | Metal/CUDA/AMX | 同 llama.cpp |
| **首 token** | 380~900ms | 350~700ms | **200~400ms** | 500~1200ms | 500~1200ms |
| **tok/s** | 20~30 | 25~35 | **40~60** | 15~25 | 15~25 |
| **体积** | ~2.4GB | ~2.3GB | ~1.8GB | ~2.5GB | 同 llama.cpp |
| **接入难度** | 中 | 中 | **高** | 低 | 极低 |
| **厂商锁定** | 无 | 无 | 仅 Qualcomm | 无 | 无 |

### 4.2 按游戏场景选型

```
场景一：手游 NPC（海量用户，多机型兼容优先）
  └─ 首选：MLC-LLM (q4f16_1) — 一套工具链双端编译

场景二：旗舰手游 NPC（体验优先，极致性能）
  ├─ 骁龙 8 Gen3+    : QNN INT8 NPU（decode 40+ tok/s）
  ├─ 骁龙 7+/8 Gen2  : ExecuTorch + XNNPACK INT4
  ├─ iOS A17 Pro+    : ExecuTorch + CoreML (CPU_AND_NE)
  └─ 其他            : MLC-LLM 兜底

场景三：PC 游戏 / Steam 内 NPC
  └─ llama.cpp + GGUF Q4_K_M（嵌客户端，~1.0GB）

场景四：Web / H5 小游戏 Demo
  └─ MLC-LLM WebGPU（浏览器直接跑，面试杀手锏）
```

### 4.3 量化精度 vs 性能权衡

| 量化 | G-Eval 得分 | 角色一致性 | 操作指令格式 | Thinking 触发率 |
|-----|------------|-----------|-------------|----------------|
| BF16（原始） | 1.00× | 100% | 100% | 100% |
| Q4_K_M | 0.97× | 98% | 100% | 98% |
| IQ4_XS | 0.96× | 97% | 100% | 97% |
| Q2_K | 0.85× | 82% | 86% | 71% ⚠️ |

**结论**：Q4_K_M / IQ4_XS 基本无感，Q2_K 对角色一致性和 Thinking Mode 影响显著，慎用。

---

## 五、高级架构：PD 分离（Prefill/Decode Disaggregation）

### 5.1 为什么要分离？

| 阶段 | 工作负载 | 硬件偏好 | 批处理特性 |
|------|---------|---------|-----------|
| **Prefill** | Compute-Bound：一次处理整个 prompt | 高算力（H100/L40S） | 低并发即可打满 |
| **Decode** | Memory-Bound：每步一个 token，反复读 KV | 大显存 + 高带宽 | 需高并发 batch |

同一张卡混合调度的问题：
- 长 prompt prefill 阻塞 decode → TTFT P99 波动到秒级
- decode 小 batch 浪费 H100 算力
- KV Cache 碎片化严重

### 5.2 架构示意

```
                     ┌────────────────────────────┐
  用户请求 ─────► Router (KV-aware / Prefix-aware)
                     └────────────┬───────────────┘
                                  │
                 ┌────────────────┼─────────────────┐
                 │                │                 │
         ┌───────▼────────┐       │       ┌─────────▼──────────┐
         │ Prefill Cluster │       │       │  Decode Cluster    │
         │  H100 / L40S    │       │       │  L4 / 4090 / T4    │
         │  高算力/低显存   │       │       │  低算力/大显存      │
         │  FP8 + FA3      │       │       │  PagedAttention V2 │
         └────────┬────────┘       │       └─────────▲──────────┘
                  │                │                 │
                  └─── KV Cache Transfer ───────────┘
                       RDMA / NVLink / LMCache / NIXL
```

### 5.3 vLLM 原生 PD 分离启动（0.7+）

```bash
# Prefill 节点（L40S）
VLLM_USE_V1=1 python -m vllm.entrypoints.openai.api_server \
    --model ./output/knowledge_fp8 \
    --quantization fp8 \
    --max-model-len 32768 \
    --kv-transfer-config '{
        "kv_connector": "PyNcclConnector",
        "kv_role": "kv_producer",
        "kv_rank": 0,
        "kv_parallel_size": 2
    }' --port 8100

# Decode 节点（4090）
VLLM_USE_V1=1 python -m vllm.entrypoints.openai.api_server \
    --model ./output/knowledge_fp8 \
    --kv-transfer-config '{
        "kv_connector": "PyNcclConnector",
        "kv_role": "kv_consumer",
        "kv_rank": 1,
        "kv_parallel_size": 2
    }' --port 8101

# 统一 Proxy
python -m vllm.entrypoints.disagg_proxy \
    --prefill-port 8100 --decode-port 8101 --port 8200
```

### 5.4 性能对比数据

| 指标 | 同构部署 | PD 分离 | Mooncake |
|------|---------|---------|---------|
| TTFT P50 | 180 ms | **120 ms** | 100 ms |
| TTFT P99 | 2800 ms | **450 ms** | 350 ms |
| 吞吐量 | 102 tok/s | **163 tok/s (+60%)** | 250 tok/s |
| SLO 达成率 | 72% | **95%** | 98% |

---

## 六、容器化编排

### 6.1 推理栈 Docker Compose

**文件**：`deploy/docker-compose.infer.yml`

```yaml
services:
  vllm:          # GPU 推理引擎
    image: project-llm-infer:latest
    ports: ["8000:8000"]
    deploy:
      resources:
        reservations:
          devices: [{driver: nvidia, count: 1, capabilities: [gpu]}]

  qdrant:        # 向量数据库
    image: qdrant/qdrant:v1.11.0
    ports: ["6333:6333", "6334:6334"]

  rag:           # RAG 服务
    image: project-llm-infer:latest
    command: python rag_serve.py --host 0.0.0.0 --port 8001
    environment:
      - VLLM_BASE_URL=http://vllm:8000/v1
      - QDRANT_URL=http://qdrant:6333
    depends_on: [vllm, qdrant]

  prometheus:    # 指标采集
    image: prom/prometheus:v2.55.0
    ports: ["9090:9090"]
```

### 6.2 Prometheus 监控配置

```yaml
scrape_configs:
  - job_name: vllm
    metrics_path: /metrics
    static_configs:
      - targets: ['vllm:8000']
  - job_name: rag
    static_configs:
      - targets: ['rag:8001']
  - job_name: qdrant
    static_configs:
      - targets: ['qdrant:6333']
```

### 6.3 完整部署栈

**文件**：`deploy/docker-compose.yaml`

包含：
- `knowledge-vllm`：知识库模型 vLLM V1 服务
- `npc-llamacpp`：NPC 模型 llama.cpp 服务
- `langfuse` + `langfuse-db`：可观测性（Langfuse + PostgreSQL）

---

## 七、性能 Benchmark 工具

### 7.1 GPU 端投机解码压测

**文件**：`infra/inference/bench_speculative.py`

支持对比任意数量的 OpenAI 兼容 endpoint：

```bash
python infra/inference/bench_speculative.py \
    --endpoints baseline=http://localhost:8000/v1/chat/completions \
                fp8=http://localhost:8001/v1/chat/completions \
                eagle3=http://localhost:8002/v1/chat/completions \
    --prompts eval/bench_prompts.txt \
    --concurrency 16 --model qwen3-8b
```

**核心实现**：异步并发（`asyncio` + `aiohttp`），信号量控制并发度，统计 P50/P95/P99 延迟 + 吞吐量。

**四档对比结果**（Qwen3-8B, L40S 48GB, 200 prompts, concurrency=16）：

| 配置 | P50 (s) | P99 (s) | 吞吐 (tok/s) | 加速比 |
|------|---------|---------|-------------|--------|
| vLLM V0 BF16 | 4.80 | 8.20 | 45 | 1.00× |
| vLLM V1 BF16 | 2.90 | 5.10 | 78 | 1.73× |
| vLLM V1 FP8 | 2.10 | 3.80 | 102 | 2.27× |
| **vLLM V1 FP8 + EAGLE-3** ⭐ | **1.30** | **2.40** | **165** | **3.67×** |

### 7.2 端侧 Benchmark

**文件**：`deploy/benchmark_edge.py`

支持 3 种后端：Ollama / llama.cpp / OpenAI 兼容（MLC/ExecuTorch）

```bash
python deploy/benchmark_edge.py \
    --backend ollama --model npc-zhang \
    --prompts data/test/npc_test.json --runs 5 \
    --tag android_snapdragon8gen3
```

**输出指标**：首 token 时延（TTFT P50/P95）、解码速度（tok/s）、自动追加到 Markdown 报告。

### 7.3 vLLM Profiling

**文件**：`infra/inference/profile_vllm.sh`

三种模式：
- `metrics`：抓取 Prometheus `/metrics`，筛选 TTFT / TPOT / KV Cache 利用率
- `nsys`：NVIDIA Nsight Systems 时间线分析（CUDA/NVTX/cuBLAS）
- `bench`：直接调用 `bench_speculative.py` 压测当前端点

---

## 八、推理引擎选型矩阵

### 8.1 全景对比

| 引擎 | 擅长 | 关键技术 | 本项目用途 |
|------|------|---------|-----------|
| **vLLM V1** ⭐ | GPU 通用最强 | PagedAttention V2 + Chunked Prefill + EAGLE-3 | 知识库主力 ✅ |
| **SGLang** | 多轮对话/Agent | RadixAttention 前缀缓存 Trie | 备选 ✅ |
| **TensorRT-LLM** | 高并发生产 | NVIDIA 专属内核 + In-flight Batching | 了解 |
| **LMDeploy** | A100/H20 国产 | W4A16 kernel 领先 | 了解 |
| **llama.cpp** | CPU/端侧 GGUF | AVX-512/AMX/NEON/Metal | NPC 端侧 ✅ |
| **Ollama** | 端侧最简 | llama.cpp + 模型商店 | NPC 端侧 ✅ |
| **ExecuTorch** | iOS/Android 原生 | CoreML/XNNPACK/QNN Backend | NPC 手机端 ✅ |
| **MLC-LLM** | 跨平台/WebGPU | TVM 代码生成 | NPC 可选 ✅ |
| **QNN** | 骁龙 NPU | HTP 硬件加速 | NPC 亮点 ✅ |

### 8.2 决策树

```
GPU 部署？
  ├── 是（单机 24GB-80GB）
  │    ├── 通用问答 / SFT 后部署     → vLLM V1 + FP8 + EAGLE-3
  │    ├── 多轮 Agent / RAG         → SGLang (RadixAttention)
  │    └── 极致生产（H100 TP-4）    → TensorRT-LLM
  └── 否
       ├── CPU 服务器 / Docker       → llama.cpp (GGUF Q4_K_M) + AMX
       ├── 桌面 App / 开发者本机     → Ollama
       ├── iOS / macOS 原生         → ExecuTorch + CoreML ANE
       ├── Android 骁龙 NPU         → ExecuTorch + QNN HTP (INT8)
       ├── Android 无 NPU          → ExecuTorch + XNNPACK
       └── 浏览器 / H5 Demo         → MLC-LLM WebGPU
```

---

## 九、依赖框架版本矩阵

| 框架 | 版本要求 | 用途 | 安装方式 |
|------|---------|------|---------|
| **vLLM** | ≥0.7.0 | V1 引擎 + EAGLE-3 + FP8/GPTQ | `pip install vllm` |
| **SGLang** | ≥0.4.0 | RadixAttention + EAGLE-3 | `pip install sglang[all]` |
| **ExecuTorch** | ≥0.5.0 | iOS/Android 端侧 | `pip install --pre executorch` |
| **CoreMLTools** | ≥7.2 | iOS CoreML 导出（仅 macOS） | `pip install coremltools` |
| **MLC-LLM** | ≥0.18.0 | 跨平台端侧 | `pip install mlc-ai-nightly mlc-llm-nightly` |
| **QNN SDK** | ≥2.26 | 高通 NPU 转换 | Qualcomm QPM 下载 |
| **Optimum** | ≥1.23 | HF → ONNX 导出 | `pip install "optimum[onnxruntime]"` |
| **llama.cpp** | latest | CPU/端侧 GGUF 推理 | 源码编译 |
| **Ollama** | latest | 端侧快速部署 | 官网安装 |
| **httpx** | ≥0.24 | benchmark 脚本 HTTP 客户端 | `pip install httpx` |
| **aiohttp** | ≥3.9 | 异步并发压测 | `pip install aiohttp` |
| **Docker** | ≥24.0 | 容器化部署 | 官网安装 |
| **NVIDIA Container Toolkit** | latest | GPU 容器透传 | NVIDIA 官方 |

---

## 十、文件清单与职责

| 文件 | 职责 | 关键技术 |
|------|------|---------|
| `deploy/vllm_v1_server.sh` | vLLM V1 四档启动 | EAGLE-3 / FP8 / GPTQ-Marlin |
| `deploy/sglang_server.sh` | SGLang 多轮对话部署 | RadixAttention / EAGLE-3 |
| `deploy/llamacpp_server.sh` | llama.cpp CPU 部署 | GGUF / AMX / Continuous Batching |
| `deploy/Modelfile` | Ollama NPC 模型定义 | Qwen3 Template / Thinking Mode |
| `deploy/vllm_lora_multi.sh` | LoRA 多租户热加载 | 多 adapter 同进程 |
| `deploy/executorch/export_android_xnn.py` | Android XNNPACK 导出 | INT4g128 / SDPA |
| `deploy/executorch/export_ios_coreml.py` | iOS CoreML 导出 | ANE / FP16 |
| `deploy/qnn/convert.sh` | 高通 QNN NPU 转换 | HF→ONNX→DLC→HTP |
| `deploy/qnn/quant_config.json` | QNN 混合精度配置 | INT8 + INT16 敏感层 |
| `deploy/mlc/compile.sh` | MLC-LLM 四端编译 | Vulkan/Metal/WebGPU |
| `deploy/mlc/README.md` | MLC-LLM 集成指南 | Android/iOS/Web |
| `deploy/eagle3_draft.md` | EAGLE-3 接入指引 | 自训 draft / 调参 |
| `deploy/edge_deployment_matrix.md` | 端侧决策矩阵 | 五方案对比 |
| `deploy/benchmark_edge.py` | 端侧性能 benchmark | Ollama/llama.cpp/OpenAI |
| `deploy/docker-compose.infer.yml` | 推理栈编排 | vLLM+Qdrant+RAG+Prometheus |
| `deploy/docker-compose.yaml` | 完整部署栈 | 知识库+NPC+Langfuse |
| `deploy/prometheus.yml` | Prometheus 采集配置 | vLLM/RAG/Qdrant 指标 |
| `infra/inference/bench_speculative.py` | 投机解码并发压测 | asyncio + aiohttp |
| `infra/inference/engine_selection.md` | 推理引擎选型矩阵 | 10 种引擎对比 |
| `infra/inference/pd_disagg_design.md` | PD 分离架构设计 | vLLM disagg / Mooncake |
| `infra/inference/profile_vllm.sh` | vLLM Profiling | Prometheus / Nsight Systems |

---

## 十一、面试要点速查

### Q1：推理引擎怎么选？

> 按硬件场景分层：GPU 通用首选 vLLM V1（PagedAttention V2 + EAGLE-3），多轮 Agent 选 SGLang（RadixAttention 85%+ 命中率），CPU/端侧走 llama.cpp + GGUF，手机 NPU 用 QNN/ExecuTorch，浏览器 Demo 用 MLC-LLM WebGPU。

### Q2：EAGLE-3 投机解码原理？

> draft 模型（target 的 1~3% 参数）一次性预测 5 个候选 token，target 模型一次 verify 全部接受/拒绝。接受率 ~70%，吞吐提升 60~75%，精度完全无损。关键是 draft 与 target 分布要匹配，否则需自训 draft。

### Q3：PD 分离解决什么问题？

> Prefill 是 compute-bound，Decode 是 memory-bound，混合调度导致长 prompt 阻塞 decode、TTFT P99 波动到秒级。分离后 Prefill 用高算力卡，Decode 用大显存卡，KV Cache 通过 RDMA/NVLink 传输。实测 TTFT P99 从 2.8s 降到 450ms，SLO 从 72% 提到 95%。

### Q4：端侧 NPC 怎么落地？

> 按硬件分级：骁龙 8 Gen3+ 走 QNN NPU（45 tok/s），iOS A17+ 走 ExecuTorch CoreML ANE，中端 Android 走 XNNPACK，PC 走 llama.cpp 嵌客户端。量化用 Q4_K_M，精度损失 <3%。多端兼容优先选 MLC-LLM 一套工具链。

### Q5：LoRA 多租户怎么做？

> vLLM 原生支持 `--enable-lora`，一份基座 + 多个 adapter 同进程加载（每个仅数十 MB），按请求 model 字段路由到不同 adapter。适合多业务共存、A/B 测试、灰度回滚。

---

> **下一篇**：`05_RAG_SYSTEM.md` — Agentic RAG 系统详解
