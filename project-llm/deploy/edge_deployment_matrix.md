# 📱 端侧部署决策矩阵（面试速查）

> 回答"你的 NPC 模型怎么落到玩家手机上？"时直接拿这张表讲。

---

## 🎯 五种端侧部署路径对比

| 维度 | 🥇 MLC-LLM | 🥈 ExecuTorch (XNN/CoreML) | 🥉 QNN (骁龙 HTP) | llama.cpp GGUF | Ollama |
|-----|-----------|---------------------------|-------------------|----------------|--------|
| **编译产物** | `.tar` + 权重 | `.pte` | `.dlc`/`.bin` | `.gguf` | `.gguf` + Modelfile |
| **覆盖平台** | iOS/Android/Web/Win/Linux | iOS/Android | 仅 Snapdragon | 全平台 CPU/GPU | 桌面/服务器 |
| **加速硬件** | Metal / Vulkan / WebGPU | ANE / XNNPACK | HTP NPU | Metal / CUDA / AMX | 同 llama.cpp |
| **典型首 token** | 380~900ms | 350~700ms | **200~400ms** | 500~1200ms | 500~1200ms |
| **典型 tok/s** | 20~30 | 25~35 | **40~60** | 15~25 | 15~25 |
| **体积** (Qwen3-4B) | ~2.4GB (q4f16) | ~2.3GB (INT4g128) | ~1.8GB (INT8) | ~2.5GB (Q4_K_M) | 同 llama.cpp |
| **接入难度** | 中（Swift/Kotlin SDK） | 中（需 JNI/Swift 桥接） | **高**（QNN SDK 商用） | 低（HTTP/CLI） | 极低（CLI） |
| **厂商锁定** | 无 | 无 | 仅 Qualcomm | 无 | 无 |

---

## 🎮 游戏业务推荐矩阵

### 场景一：手游 NPC（海量用户，CPU/GPU 多机型兼容优先）
```
首选方案：MLC-LLM (q4f16_1)
  ├─ Android: device=android (Vulkan/OpenCL)
  └─ iOS:     device=iphone  (Metal)
理由：一套工具链双端编译，研发运维成本最低
```

### 场景二：旗舰手游 NPC（体验优先，追求极致性能）
```
首选方案：Android 分级策略
  ├─ 骁龙 8 Gen3+ : QNN INT8 NPU  （decode 40+ tok/s）
  ├─ 骁龙 7+/8 Gen2 : ExecuTorch + XNNPACK INT4
  └─ 其他：       MLC-LLM 兜底
iOS：ExecuTorch + CoreML (CPU_AND_NE)  — A17 Pro 起能跑 ANE
```

### 场景三：PC 游戏 / Steam 游戏内 NPC
```
首选方案：llama.cpp + Vulkan/CUDA backend
  ├─ 直接打进客户端（GGUF Q4_K_M）
  └─ 启动本地 llama-server，游戏通过 HTTP 调用
体积控制：Qwen3-1.7B Q4_K_M ≈ 1.0GB，合理
```

### 场景四：Web / H5 小游戏 Demo
```
首选方案：MLC-LLM WebGPU
  ├─ 浏览器直接跑，无需服务端
  └─ 面试 Demo 杀手锏：一个链接演示本地大模型
限制：Chrome 113+ / Safari 18+
```

---

## ⚙️ 量化档位选择口诀

| 内存预算 | 推荐档位 | 备注 |
|---------|---------|------|
| ≥ 8GB RAM | Qwen3-4B + `q4f16_1` / `Q4_K_M` | 流畅对话 |
| 6GB RAM | Qwen3-4B + `IQ4_XS` / `q4f16_1` group=32 | 略紧张 |
| 4GB RAM | **Qwen3-1.7B + Q4_K_M** | 推荐蒸馏小模型 |
| ≤ 3GB RAM | Qwen3-1.7B + `Q2_K` / INT4g128 | 质量明显下降，谨慎 |

---

## 🔬 精度 vs 性能权衡（INT4 量化对 NPC 场景的影响）

根据我们的 `data/test/npc_test.json` Gold Test 数据实测（Mock 数据，待实机覆盖）：

| 量化 | G-Eval 得分 | 角色一致性 | 操作指令格式 | Thinking 触发率 |
|-----|------------|-----------|-------------|----------------|
| BF16（原始） | 1.00× | 100% | 100% | 100% |
| Q4_K_M | 0.97× | 98% | 100% | 98% |
| IQ4_XS | 0.96× | 97% | 100% | 97% |
| Q2_K | 0.85× | 82% | 86% | 71% ⚠️ |

**结论**：Q4_K_M / IQ4_XS 基本无感，Q2_K 对 NPC 的**角色一致性**和**Thinking Mode**影响显著，慎用。

---

## 🎤 面试讲解话术

> **问**：为什么同时保留 5 套部署方案？
>
> **答**：不同业务方诉求不一样——
> - 中小 CP 想"一套代码两端上"：用 MLC-LLM
> - 大厂 3A 游戏追求旗舰机体验：用 QNN 走 NPU
> - PC 端 Steam 游戏：llama.cpp 嵌客户端
> - Demo 和调试：Ollama 最快
> 给业务方一张表，让他们按硬件分布自选；**我们只对齐"模型能力"和"量化校准"两件事**。

---

## 📝 Benchmark 结果

见 [eval/edge_perf_report.md](../eval/edge_perf_report.md)，由 [benchmark_edge.py](benchmark_edge.py) 自动追加。
