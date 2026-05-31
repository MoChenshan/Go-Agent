# MLC-LLM 跨平台部署（iOS / Android / Web / WASM）

> MLC-LLM 是 **唯一能用一套工具链同时编译 iOS / Android / WebGPU / Windows / Linux** 的大模型部署框架。
> 适合需要"一次训练、多端落地"的游戏业务。

---

## 🧩 三大档位

| 量化 | 含义 | 适用 | 典型体积 (Qwen3-4B) |
|------|------|------|------------------|
| `q4f16_1` | 权重 INT4 / 激活 FP16 / group_size=32 | ⭐ **推荐端侧** | ~2.4 GB |
| `q4f32_1` | 权重 INT4 / 激活 FP32 / group_size=32 | 数值稳定 | ~2.5 GB |
| `q0f16`   | 无量化 FP16 | Web/WASM Demo | ~8 GB |

---

## 🚀 一键编译（四端）

直接使用 [compile.sh](compile.sh)：

```bash
# 编译 Android
TARGET=android bash deploy/mlc/compile.sh

# 编译 iOS（需 macOS）
TARGET=iphone bash deploy/mlc/compile.sh

# 编译 WebGPU（浏览器 Demo）
TARGET=webgpu bash deploy/mlc/compile.sh

# 编译 Windows（桌面端）
TARGET=windows bash deploy/mlc/compile.sh
```

---

## 📋 手动分步（了解原理）

```bash
# 1. 安装 MLC-LLM
pip install --pre --force-reinstall mlc-ai-nightly mlc-llm-nightly \
    -f https://mlc.ai/wheels

# 2. 权重转换（HF → MLC 格式）
mlc_llm convert_weight ./output/npc_merged/ \
    --quantization q4f16_1 \
    -o ./output/npc_mlc/

# 3. 生成 chat-config
mlc_llm gen_config ./output/npc_merged/ \
    --quantization q4f16_1 \
    --conv-template qwen3 \
    --context-window-size 8192 \
    -o ./output/npc_mlc/

# 4. 编译运行时 library（各端不同）
mlc_llm compile ./output/npc_mlc/mlc-chat-config.json \
    --device android \
    --host android \
    -o ./output/npc_mlc/npc-android.tar
```

---

## 📱 Android 集成

1. 参考 [mlc-llm/android 官方 demo](https://github.com/mlc-ai/mlc-llm/tree/main/android/MLCChat)
2. 把 `npc-android.tar` + 权重目录打进 APK assets
3. Kotlin 调用（JNI 包装）：
   ```kotlin
   val engine = MLCEngine()
   engine.reload("npc-zhang", "/sdcard/npc-android.tar", "npc_mlc/")
   val resp = engine.chatCompletion(messages)
   ```

## 🍎 iOS 集成

1. 编译产出 `npc-iphone.tar`（包含 Metal shader）
2. 将 `tar` + `mlc-chat-config.json` + 权重放入 iOS bundle
3. Swift 通过 `MLCSwift` package 集成（官方已包装）

## 🌐 WebGPU Demo（浏览器直接跑）

```bash
TARGET=webgpu bash deploy/mlc/compile.sh
# 产物： npc-webgpu.wasm + npc-params/
```

浏览器要求：Chrome 113+ / Safari 18+ / Edge 113+。
作为**面试 Demo** 特别有爆点：一个链接演示大模型在浏览器本地跑。

---

## 🧪 Benchmark

```bash
python deploy/benchmark_edge.py \
    --backend mlc \
    --model_path ./output/npc_mlc \
    --prompts data/test/npc_test.json
```

预期（骁龙 8 Gen3 / A17 Pro）：
```
Qwen3-4B q4f16_1  Android  : 首 token ~450ms, 解码 ~22 tok/s
Qwen3-4B q4f16_1  iOS ANE  : 首 token ~380ms, 解码 ~28 tok/s
Qwen3-4B q4f16_1  WebGPU   : 首 token ~900ms, 解码 ~12 tok/s
```

---

## ⚠️ 常见问题

| 问题 | 原因 | 解决 |
|------|------|------|
| 编译卡在 `TVM JIT` | TVM 缓存路径权限 | 清理 `~/.cache/tvm` 重试 |
| 运行时 `No WebGPU adapter` | 浏览器未开 WebGPU | Chrome 开 `chrome://flags/#enable-unsafe-webgpu` |
| Android APK 启动闪退 | 权重文件没打进 assets 或路径错 | 用 `adb push` 把权重单独推到 `/sdcard/` |
