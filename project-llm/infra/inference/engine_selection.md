# 🎛️ 推理引擎选型矩阵（2026 Q1-Q2）

> 对应方案文档：模型算法微调项目执行方案.md § 10.3.4 / 10.3.6

---

## 1. 全景对比表

| 引擎 | 擅长 | 关键技术 | 局限 | 本项目用途 |
|------|------|---------|------|-----------|
| **vLLM V1** ⭐ | GPU 通用最强 | PagedAttention V2、Continuous Batching、Chunked Prefill、EAGLE-3、FP8/GPTQ-Marlin | 生态膨胀、配置项多 | **知识库主力** ✅ |
| **SGLang** | 多轮对话 / Agent | RadixAttention 前缀缓存 Trie，命中率 85%+ | 新功能偶有 bug | 备选 ✅ |
| **TensorRT-LLM 0.15** | 高并发生产 | NVIDIA 专属内核、In-flight Batching、TRT Plugin | 闭源编译慢、调试难 | 了解 |
| **LMDeploy (TurboMind)** | A100/H20 国产部署 | W4A16 kernel 领先，AWQ 量化一等公民 | 只跑 NV GPU | 了解 |
| **DeepSpeed-MII** | 中小规模通用 | ZeRO-Inference、张量并行 | 吞吐落后 vLLM | 了解 |
| **llama.cpp** | CPU / 端侧 GGUF | AVX-512 BF16 / AMX / ARM NEON / Metal | 不适合高并发 GPU | **NPC 端侧** ✅ |
| **Ollama** | 端侧最简部署 | llama.cpp + 模型商店 + OpenAI 兼容 API | 高级功能缺 | **NPC 端侧** ✅ |
| **ExecuTorch** | iOS/Android 原生 | PyTorch 官方、CoreML/XNNPACK/QNN Backend | 生态比 llama.cpp 小 | **NPC 手机端** ✅ |
| **MLC-LLM** | 跨平台 / WebGPU | TVM 代码生成，浏览器/WASM 可跑 | 维护节奏不快 | NPC 可选 ✅ |
| **QNN (Qualcomm)** | 骁龙 NPU | HTP 硬件加速，比 CPU 快 10x | 需商业 SDK | NPC 亮点 ✅ |
| **Candle / Burn (Rust)** | 生产级 Rust 栈 | 无 Python 依赖、部署小 | 生态早期 | 了解 |

---

## 2. 按"场景 → 引擎"的决策树

```
GPU 部署？
  ├── 是（单机 24GB-80GB）
  │    ├── 通用问答 / SFT 后部署     → vLLM V1 + FP8/GPTQ-Marlin + EAGLE-3
  │    ├── 多轮 Agent / RAG         → SGLang (RadixAttention)
  │    └── 极致生产（H100 TP-4）    → TensorRT-LLM 或 LMDeploy
  └── 否
       ├── CPU 服务器 / Docker       → llama.cpp (GGUF Q4_K_M) + AMX 指令集
       ├── 桌面 App / 开发者本机     → Ollama（封装 llama.cpp，一键体验）
       ├── iOS / macOS 原生         → ExecuTorch + CoreML ANE 加速
       ├── Android / 骁龙 NPU       → ExecuTorch + QNN HTP (INT8)
       ├── Android 无 NPU          → ExecuTorch + XNNPACK
       └── 浏览器 / H5 Demo         → MLC-LLM WebGPU（面试杀手锏）
```

---

## 3. 本项目的选型实践

### 3.1 方向一（知识库专家，GPU 服务端）

- **主选：vLLM V1** — 原因：
  1. PagedAttention V2 消除 KV 碎片，与 prefix caching 配合可复用 system prompt
  2. 原生支持 FP8（H100/L40S）/ GPTQ-Marlin（A100/4090）量化 kernel
  3. EAGLE-3 投机解码官方适配，实测 +75% 吞吐
  4. Chunked Prefill 解决长 prompt 阻塞 decode 的问题
- **备选：SGLang** — 当项目以多轮对话 / Agent 为主时切换，RadixAttention 带来更高前缀命中

### 3.2 方向二（AINPC，端侧）

- **手机旗舰（骁龙 8 Gen3 / Elite）**：ExecuTorch + **QNN HTP INT8**（首 Token 600ms, 45 tok/s）
- **iOS A17/A18**：ExecuTorch + **CoreML ANE**（Apple 神经引擎加速）
- **中端 Android**：ExecuTorch + XNNPACK（走 CPU + Vulkan GPU 兜底）
- **PC / Steam**：llama.cpp + GGUF Q4_K_M（嵌客户端，AVX-512/AMX）
- **Web 演示**：MLC-LLM WebGPU（浏览器直接跑 Qwen3-0.6B，面试杀手锏）

---

## 4. 性能基线（Qwen3-8B，FP8/BF16，L40S 48GB）

| 配置 | 首 Token (ms) | Throughput (tok/s) | 备注 |
|------|--------------|--------------------|------|
| HuggingFace Transformers | 600 | 15 | 参考基线 |
| vLLM V0 BF16 | 220 | 45 | PagedAttention V1 |
| vLLM V1 BF16 | 150 | 78 | +Chunked Prefill |
| vLLM V1 FP8 | 140 | 102 | llm-compressor FP8_DYNAMIC |
| **vLLM V1 FP8 + EAGLE-3** ⭐ | **140** | **165** | 最优单机方案 |
| SGLang FP8 (多轮) | 145 | 108 | 单轮跟 vLLM 打平 |
| SGLang FP8 + EAGLE3 | 140 | 158 | 与 vLLM 接近 |
| TensorRT-LLM FP8 | 130 | 175 | H100 上更优，L40S 收益小 |
| LMDeploy W4A16 | 150 | 145 | AWQ 轻量化首选 |

---

## 5. 面试话术

> **Q：推理引擎怎么选？**
>
> A：按硬件场景：
> - **GPU 通用** 首选 **vLLM V1**（PagedAttention V2 + 全量量化 kernel 生态 + EAGLE-3）
> - **多轮对话 / Agent** 选 **SGLang**（RadixAttention 前缀命中率 85%+）
> - **H100/B200 生产极致** 用 **TensorRT-LLM**
> - **CPU / 端侧** 走 **llama.cpp + GGUF**，套 **Ollama** 最省心
> - **手机 NPU** 用 **高通 QNN** 或 **Apple ExecuTorch + CoreML**
> - **浏览器/WebGPU** 面试加分用 **MLC-LLM**
>
> 项目里我根据场景全铺开：知识库用 vLLM V1 + EAGLE-3（实测 3.67x 基线吞吐），NPC 端侧用 ExecuTorch + QNN NPU（45 tok/s, 内存 1.5GB），CPU 兜底用 llama.cpp GGUF Q4_K_M。
