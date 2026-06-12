# 11. 框架内部原理深度解析

> **文档定位**：本文档深入解析 `project-llm` 核心流程中**由第三方框架/库实现**的底层机制。现有文档（01-10）主要覆盖项目自定义实现部分，本文档补齐框架"黑盒"内部的原理，确保面试中能回答"框架帮你做了什么？底层怎么实现的？"类问题。
>
> **适用场景**：面试深追、技术评审、框架升级决策、性能调优定位。

---

## 一、训练框架内部机制

### 1.1 LLaMAFactory 内部架构

#### 1.1.1 整体调度流程

LLaMAFactory 是一个**声明式训练框架**——用户只写 YAML 配置，框架内部完成从模型加载到训练循环的全部编排：

```mermaid
graph TD
    A[YAML 配置文件] --> B[ConfigParser<br/>解析 + 校验]
    B --> C[ModelLoader<br/>加载基座 + 量化]
    C --> D[AdapterInjector<br/>注入 LoRA/QLoRA]
    D --> E[DatasetBuilder<br/>加载 + 格式化 + 打包]
    E --> F[TrainerFactory<br/>选择 SFT/DPO/GRPO Trainer]
    F --> G[HuggingFace Trainer.train()<br/>训练循环]
    G --> H[Checkpoint + Export]
```

#### 1.1.2 ConfigParser 阶段

LLaMAFactory 将所有配置统一为内部 `ModelArguments` / `DataArguments` / `TrainingArguments` 三组 dataclass：

```python
# LLaMAFactory 内部（简化）
@dataclass
class ModelArguments:
    model_name_or_path: str
    quantization_bit: Optional[int] = None
    quantization_method: str = "bitsandbytes"
    lora_rank: int = 8
    lora_target: str = "all"
    use_rslora: bool = False
    liger_kernel: bool = False
    flash_attn: str = "auto"
    ...

# YAML → dataclass 映射
args = HfArgumentParser((ModelArguments, DataArguments, TrainingArguments))
model_args, data_args, training_args = args.parse_yaml_file(yaml_path)
```

**关键点**：`lora_target: all` 在内部被展开为模型的所有 Linear 层名称列表（通过 `model.named_modules()` 遍历匹配）。

#### 1.1.3 ModelLoader 阶段——量化加载的内部实现

当配置 `quantization_bit: 4` 时，LLaMAFactory 内部调用链：

```python
# 1. 构造 BitsAndBytesConfig
bnb_config = BitsAndBytesConfig(
    load_in_4bit=True,
    bnb_4bit_quant_type="nf4",           # NF4 量化类型
    bnb_4bit_use_double_quant=True,       # 双重量化
    bnb_4bit_compute_dtype=torch.bfloat16 # 计算精度
)

# 2. 加载模型时传入量化配置
model = AutoModelForCausalLM.from_pretrained(
    model_name_or_path,
    quantization_config=bnb_config,
    torch_dtype=torch.bfloat16,
    device_map="auto",                    # accelerate 自动设备映射
    attn_implementation="flash_attention_2"  # FA2
)
```

**BitsAndBytes NF4 量化的底层实现**（见 1.2 节详解）。

#### 1.1.4 AdapterInjector 阶段——LoRA 注入

```python
from peft import get_peft_model, LoraConfig

# LLaMAFactory 内部构造 LoraConfig
lora_config = LoraConfig(
    r=model_args.lora_rank,              # 16
    lora_alpha=model_args.lora_alpha,    # 32
    target_modules=find_all_linear_names(model),  # "all" → 实际模块名列表
    lora_dropout=model_args.lora_dropout,
    bias="none",
    task_type="CAUSAL_LM",
    use_rslora=model_args.use_rslora,    # RSLoRA 缩放
)

# 注入 LoRA adapter
model = get_peft_model(model, lora_config)
model.print_trainable_parameters()
# trainable params: 52,428,800 || all params: 8,030,261,248 || trainable%: 0.65%
```

**`find_all_linear_names()` 的实现**：

```python
def find_all_linear_names(model):
    """遍历模型所有模块，找出所有 Linear 层的名称"""
    linear_names = set()
    for name, module in model.named_modules():
        if isinstance(module, (torch.nn.Linear, bnb.nn.Linear4bit)):
            # 取最后一级名称（如 "q_proj", "gate_proj"）
            parts = name.split(".")
            linear_names.add(parts[-1])
    # 排除 lm_head（输出层通常不加 LoRA）
    linear_names.discard("lm_head")
    return list(linear_names)
    # Qwen3-8B 结果: ["q_proj", "k_proj", "v_proj", "o_proj", "gate_proj", "up_proj", "down_proj"]
```

#### 1.1.5 neat_packing 的内部实现

`neat_packing: true` 启用**无 padding 的样本打包**，核心思想是将多个短样本拼接到一个 `cutoff_len` 的序列中：

```python
# LLaMAFactory 内部打包逻辑（简化）
def pack_sequences(examples, cutoff_len):
    """将多个样本紧凑打包到固定长度序列中"""
    packed_input_ids = []
    packed_labels = []
    current_ids, current_labels = [], []
    
    for ids, labels in zip(examples["input_ids"], examples["labels"]):
        if len(current_ids) + len(ids) <= cutoff_len:
            current_ids.extend(ids)
            current_labels.extend(labels)
        else:
            # 当前序列已满，保存并开始新序列
            packed_input_ids.append(pad_to_length(current_ids, cutoff_len))
            packed_labels.append(pad_to_length(current_labels, cutoff_len))
            current_ids, current_labels = ids[:], labels[:]
    
    return {"input_ids": packed_input_ids, "labels": packed_labels}
```

**与 `attention_mask` 的配合**：打包后不同样本共享一个序列，需要用 **block-diagonal attention mask** 防止跨样本注意力泄漏。LLaMAFactory 通过 `position_ids` 重置实现这一点。

#### 1.1.6 TrainerFactory——SFT/DPO/GRPO 分发

```python
# LLaMAFactory 内部 Trainer 选择逻辑
def create_trainer(stage, model, tokenizer, dataset, training_args):
    if stage == "sft":
        return SFTTrainer(model=model, tokenizer=tokenizer, ...)
    elif stage == "dpo":
        return DPOTrainer(model=model, ref_model=ref_model, ...)
    elif stage == "grpo":
        return GRPOTrainer(model=model, reward_funcs=load_reward_funcs(), ...)
    elif stage == "kto":
        return KTOTrainer(...)
```

---

### 1.2 BitsAndBytes NF4 量化内部原理

#### 1.2.1 NF4 数据类型

NF4（NormalFloat4）是专为正态分布权重设计的 4-bit 数据类型：

```
标准 INT4 的 16 个量化点：均匀分布在 [-8, 7]
NF4 的 16 个量化点：按标准正态分布的分位点分布

NF4 量化表（归一化后）：
[-1.0, -0.6962, -0.5251, -0.3949, -0.2844, -0.1848, -0.0911, 0.0,
  0.0796,  0.1609,  0.2461,  0.3379,  0.4407,  0.5626,  0.7230, 1.0]
```

**为什么 NF4 优于 INT4**：
- 神经网络权重近似服从正态分布（均值 0，方差小）
- NF4 在分布密集区（接近 0）分配更多量化点
- 信息论最优：最小化量化误差的期望值

#### 1.2.2 量化过程

```python
# BitsAndBytes 内部量化流程（简化）
def quantize_nf4(weight_fp32, block_size=64):
    """
    1. 将权重按 block_size 分组
    2. 每组计算 absmax 作为 scale
    3. 归一化到 [-1, 1]
    4. 找最近的 NF4 量化点
    """
    blocks = weight_fp32.reshape(-1, block_size)
    scales = blocks.abs().max(dim=1).values  # 每组的 absmax
    
    # 归一化
    normalized = blocks / scales.unsqueeze(1)
    
    # 量化：找最近的 NF4 值
    nf4_table = torch.tensor(NF4_VALUES)  # 16 个值
    quantized = torch.argmin(
        (normalized.unsqueeze(-1) - nf4_table).abs(), dim=-1
    )  # 每个值用 4-bit index 表示
    
    return quantized, scales  # 4-bit 索引 + FP32 scale
```

#### 1.2.3 双重量化（Double Quantization）

```python
# 第一层量化：权重 FP32 → NF4（scale 是 FP32）
quantized_weights, scales_fp32 = quantize_nf4(weight)
# scales_fp32 形状: [num_blocks]，每个 scale 占 32 bit

# 第二层量化：scale 本身再量化为 FP8
quantized_scales, meta_scale = quantize_fp8(scales_fp32)
# 节省: 每个 block 的 scale 从 32bit → 8bit，省 24bit/block
# 对于 block_size=64: 省 24/64 = 0.375 bit/param
```

**总存储**：
- 权重：4 bit/param
- Scale（双重量化后）：8 bit / 64 params = 0.125 bit/param
- Meta-scale：32 bit / (64×256) params ≈ 0.002 bit/param
- **总计 ≈ 4.13 bit/param**（vs 无双重量化的 4.5 bit/param）

#### 1.2.4 反量化（推理时）

```python
def dequantize_nf4(quantized, scales, meta_scale):
    """反量化：4-bit index → FP16/BF16 用于计算"""
    # 恢复 scale
    scales_fp32 = dequantize_fp8(quantized_scales, meta_scale)
    # 查表 + 乘 scale
    weight_approx = NF4_TABLE[quantized] * scales_fp32.unsqueeze(1)
    return weight_approx.to(torch.bfloat16)
```

**关键**：反量化只在**前向传播时按需执行**（逐层），不会一次性把整个模型反量化到显存中。

---

### 1.3 PEFT / LoRA 内部实现

#### 1.3.1 LoRA 前向传播

```python
# PEFT 内部 LoraLayer 实现（简化）
class LoraLinear(nn.Module):
    def __init__(self, base_layer, r, alpha, dropout):
        self.base_layer = base_layer  # 原始 Linear（冻结/量化）
        self.r = r
        self.scaling = alpha / r      # 标准 LoRA
        # self.scaling = alpha / sqrt(r)  # RSLoRA
        
        # A: (in_features, r) 高斯初始化
        self.lora_A = nn.Linear(in_features, r, bias=False)
        nn.init.kaiming_uniform_(self.lora_A.weight)
        
        # B: (r, out_features) 零初始化 → 训练开始时 ΔW = 0
        self.lora_B = nn.Linear(r, out_features, bias=False)
        nn.init.zeros_(self.lora_B.weight)
        
        self.dropout = nn.Dropout(dropout)
    
    def forward(self, x):
        # 原始路径（冻结权重，可能是 NF4 量化的）
        base_out = self.base_layer(x)
        
        # LoRA 路径
        lora_out = self.lora_B(self.lora_A(self.dropout(x)))
        
        # 合并
        return base_out + lora_out * self.scaling
```

**训练时梯度流**：
- `base_layer` 的参数被冻结（`requires_grad=False`），不计算梯度
- 只有 `lora_A` 和 `lora_B` 的参数接收梯度
- 梯度通过 `lora_out * scaling` 反向传播到 A 和 B

#### 1.3.2 RSLoRA 的数学原理

标准 LoRA 的 scaling = `α/r`，当 rank 增大时：
- LoRA 输出 `BA·x` 的方差 ∝ r（因为 A 的列数增加）
- 乘以 `α/r` 后方差 ∝ `α²/r` → rank 越大，有效信号越弱

RSLoRA 改为 `α/√r`：
- 乘以 `α/√r` 后方差 ∝ `α²` → **与 rank 无关**
- 不同 rank 的实验结果可比性更强
- 高 rank（32/64）时训练更稳定

```python
# PEFT 内部
if use_rslora:
    self.scaling = lora_alpha / math.sqrt(lora_rank)
else:
    self.scaling = lora_alpha / lora_rank
```

#### 1.3.3 merge_and_unload 的实现

```python
# PEFT 内部 merge 逻辑
def merge_and_unload(self):
    """将 LoRA 权重合并回基座，返回普通模型"""
    for name, module in self.model.named_modules():
        if isinstance(module, LoraLinear):
            # ΔW = B × A × scaling
            delta_w = (module.lora_B.weight @ module.lora_A.weight) * module.scaling
            
            # 如果基座是量化的，先反量化
            base_weight = dequantize(module.base_layer.weight)
            
            # 合并: W_merged = W_base + ΔW
            merged_weight = base_weight + delta_w.to(base_weight.dtype)
            
            # 替换为普通 Linear
            new_linear = nn.Linear(in_features, out_features)
            new_linear.weight.data = merged_weight
            replace_module(self.model, name, new_linear)
    
    return self.model
```

**注意**：合并后模型精度取决于 `--dtype` 参数。QLoRA 训练时基座是 NF4，合并时反量化为 BF16/FP16，**合并后的模型是全精度的**。

---

### 1.4 TRL DPOTrainer 内部机制

#### 1.4.1 训练循环核心

```python
# TRL DPOTrainer 内部（简化）
class DPOTrainer(Trainer):
    def compute_loss(self, model, inputs):
        # 1. 用当前 policy 计算 chosen/rejected 的 log prob
        policy_chosen_logps = self.get_batch_logps(model, inputs["chosen_input_ids"])
        policy_rejected_logps = self.get_batch_logps(model, inputs["rejected_input_ids"])
        
        # 2. 用 ref model 计算 chosen/rejected 的 log prob
        with torch.no_grad():
            ref_chosen_logps = self.get_batch_logps(self.ref_model, inputs["chosen_input_ids"])
            ref_rejected_logps = self.get_batch_logps(self.ref_model, inputs["rejected_input_ids"])
        
        # 3. 计算 DPO loss
        chosen_rewards = self.beta * (policy_chosen_logps - ref_chosen_logps)
        rejected_rewards = self.beta * (policy_rejected_logps - ref_rejected_logps)
        
        # L = -log σ(chosen_reward - rejected_reward)
        loss = -F.logsigmoid(chosen_rewards - rejected_rewards).mean()
        
        return loss
    
    def get_batch_logps(self, model, input_ids):
        """计算序列的 per-token log probability 之和"""
        logits = model(input_ids).logits
        # 只计算 response 部分的 log prob（prompt 部分 mask 掉）
        log_probs = F.log_softmax(logits, dim=-1)
        # 取实际 token 对应的 log prob
        per_token_logps = torch.gather(log_probs[:, :-1], 2, 
                                        input_ids[:, 1:].unsqueeze(2)).squeeze(2)
        # 乘以 loss_mask（只算 response token）
        return (per_token_logps * loss_mask).sum(dim=1)
```

#### 1.4.2 Reference Model 的处理

DPO 需要一个**冻结的参考模型**来计算 KL 散度：

```python
# TRL 内部 ref model 策略
if peft_config is not None:
    # 使用 LoRA 时：ref_model = 基座模型（不加 LoRA adapter）
    # 显存优化：共享基座权重，ref 只是不走 LoRA 路径
    self.ref_model = model.base_model  # 共享权重！
else:
    # 全参微调时：需要完整复制一份模型
    self.ref_model = create_reference_model(model)  # 显存翻倍
```

**关键优化**：使用 LoRA 时，ref model 和 policy model **共享基座权重**，ref 只是跳过 LoRA adapter 的前向传播。这就是为什么 QLoRA + DPO 不会显存翻倍。

#### 1.4.3 SimPO 的内部差异

SimPO 完全去掉 ref model，用**序列长度归一化的 log prob** 作为隐式 reward：

```python
# SimPO loss（TRL 内部）
def simpo_loss(policy_chosen_logps, policy_rejected_logps, 
               chosen_len, rejected_len, beta, gamma):
    # 长度归一化
    chosen_rewards = beta * (policy_chosen_logps / chosen_len)
    rejected_rewards = beta * (policy_rejected_logps / rejected_len)
    
    # 加 margin γ
    loss = -F.logsigmoid(chosen_rewards - rejected_rewards - gamma).mean()
    return loss
```

**优势**：无需 ref model → 显存减半 + 速度翻倍。

---

### 1.5 TRL GRPOTrainer 内部机制

#### 1.5.1 GRPO 训练循环

```python
# TRL GRPOTrainer 内部（简化）
class GRPOTrainer(Trainer):
    def training_step(self, model, inputs):
        prompts = inputs["prompt"]
        
        # 1. 生成 G 个 rollout（每个 prompt 采样 num_generations 个回复）
        with torch.no_grad():
            completions = []
            for _ in range(self.num_generations):  # 默认 8
                output = model.generate(prompts, 
                    do_sample=True, temperature=1.0,
                    max_new_tokens=self.max_new_tokens)
                completions.append(output)
        
        # 2. 计算每个 completion 的 reward
        rewards = []
        for reward_fn in self.reward_funcs:
            r = reward_fn(completions, prompts=prompts, **kwargs)
            rewards.append(r)
        # 加权求和
        total_rewards = weighted_sum(rewards, self.reward_weights)
        
        # 3. 组内归一化计算 advantage
        # 每个 prompt 的 G 个 reward 做 z-score 归一化
        grouped = total_rewards.reshape(-1, self.num_generations)
        mean = grouped.mean(dim=1, keepdim=True)
        std = grouped.std(dim=1, keepdim=True) + 1e-8
        advantages = ((grouped - mean) / std).reshape(-1)
        
        # 4. 计算 policy gradient loss
        # 重新前向传播计算 log prob
        logps = self.get_per_token_logps(model, completions)
        with torch.no_grad():
            ref_logps = self.get_per_token_logps(self.ref_model, completions)
        
        # 5. PPO-clip 风格的 loss（或简化版 REINFORCE）
        ratio = torch.exp(logps - old_logps)
        clipped_ratio = torch.clamp(ratio, 1 - self.clip_eps, 1 + self.clip_eps)
        policy_loss = -torch.min(ratio * advantages, clipped_ratio * advantages).mean()
        
        # 6. KL 惩罚
        kl = (logps - ref_logps).mean()
        loss = policy_loss + self.beta * kl
        
        return loss
```

#### 1.5.2 Reward 函数接口规范

TRL 要求 reward 函数遵循统一接口：

```python
def reward_fn(
    completions: list[str],           # G 个生成结果
    prompts: list[str] | None = None, # 对应的 prompt
    **kwargs                          # 额外上下文（如 npc_profiles）
) -> list[float]:                     # 每个 completion 的 reward 分数
    ...
```

**LLaMAFactory 的扩展**：通过 `reward_funcs` 配置项指定函数名，框架在运行时 `importlib` 动态加载：

```python
# LLaMAFactory 内部加载 reward 函数
reward_funcs = []
for fn_name in config.reward_funcs:
    module = importlib.import_module("scripts.grpo_rewards")
    fn = getattr(module, fn_name)
    reward_funcs.append(fn)
```

#### 1.5.3 GRPO vs PPO 的关键差异

| 维度 | PPO | GRPO |
|------|-----|------|
| Critic/Value Model | 需要（同规模网络） | **不需要** ⭐ |
| Baseline | Value function V(s) | Group mean（组内均值） |
| Advantage | A = R - V(s) | A = (r_i - mean) / std |
| 显存 | Actor + Critic + Ref = 3× | Actor + Ref = 2× |
| Reward Model | 需要训练 | **可用规则替代** ⭐ |
| 适用场景 | 通用 RLHF | 可验证任务（代码/格式/数学） |

---

### 1.6 Liger Kernel 内部原理

#### 1.6.1 融合算子的核心思想

Liger Kernel 将多个连续的 element-wise 操作融合为单个 Triton kernel，减少 HBM 读写次数：

```
朴素实现（4 次 HBM 读写）：
  x → [HBM读] → square → [HBM写] → [HBM读] → mean → [HBM写] → 
  [HBM读] → rsqrt → [HBM写] → [HBM读] → mul_weight → [HBM写] → y

融合实现（1 次 HBM 读 + 1 次 HBM 写）：
  x → [HBM读] → [SRAM内: square→mean→rsqrt→mul] → [HBM写] → y
```

#### 1.6.2 Liger 覆盖的算子

| 算子 | 朴素 kernel 数 | 融合后 | 显存节省 |
|------|--------------|--------|---------|
| **RMSNorm** | 4 | 1 | 中间激活 3× |
| **SwiGLU** (gate·up·silu) | 3 | 1 | gate/up 中间值 |
| **RoPE** | 3 | 1 | sin/cos 表 |
| **CrossEntropyLoss** | 2 | 1 | logits 全量 |
| **FusedLinearCrossEntropy** | matmul+CE | 1 | **最大节省**：不存 logits |

#### 1.6.3 FusedLinearCrossEntropy 的特殊价值

这是 Liger 最重要的优化——将 `lm_head` 的 matmul 和 CrossEntropy 融合：

```python
# 朴素实现：
logits = lm_head(hidden_states)  # [batch, seq, vocab_size=152064]
# logits 占显存: batch × seq × 152064 × 2 bytes
# 对于 batch=4, seq=4096: 4 × 4096 × 152064 × 2 = 4.7 GB !!!
loss = F.cross_entropy(logits.view(-1, vocab_size), labels.view(-1))

# Liger 融合实现：
# 不 materialize 完整 logits，分 chunk 计算
loss = fused_linear_cross_entropy(hidden_states, lm_head.weight, labels)
# 每次只算一个 chunk 的 logits → softmax → CE → 累加
# 峰值显存: chunk_size × vocab_size × 2 bytes ≈ 几十 MB
```

**这就是 Liger Kernel 能省 20% 显存的主要来源**——对于大词表模型（Qwen3 vocab=152064），logits 是训练时最大的中间激活。

#### 1.6.4 LLaMAFactory 集成方式

```python
# LLaMAFactory 内部（一行配置触发）
if model_args.liger_kernel:
    from liger_kernel.transformers import apply_liger_kernel_to_qwen3
    apply_liger_kernel_to_qwen3()  # monkey-patch 替换原始算子
```

`apply_liger_kernel_to_qwen3()` 内部通过 **monkey-patching** 替换 Qwen3 模型的：
- `Qwen3RMSNorm.forward` → `LigerRMSNorm`
- `Qwen3MLP.forward` → `LigerSwiGLU`
- `Qwen3RotaryEmbedding` → `LigerRoPE`
- `CrossEntropyLoss` → `LigerFusedLinearCrossEntropy`

---

## 二、推理框架内部机制

### 2.1 vLLM PagedAttention 详解

#### 2.1.1 传统 KV Cache 的问题

```
传统连续分配：
序列 A (len=100): [████████████████████░░░░░░░░░░░░░░░░░░░░]  预分配 200 token
序列 B (len=50):  [████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░]  预分配 200 token
序列 C (结束):    [                                          ]  空洞！无法利用

问题：
1. 预分配浪费（序列实际长度 < 预分配长度）
2. 碎片化（序列结束后留下空洞）
3. 利用率仅 ~60%
```

#### 2.1.2 PagedAttention 的解决方案

借鉴操作系统虚拟内存的分页机制：

```
PagedAttention：
物理 Block Pool: [B0][B1][B2][B3][B4][B5][B6][B7][B8][B9]...
                  ↑   ↑   ↑   ↑   ↑   ↑
                  │   │   │   │   │   │
Block Table A:   [0] [1] [5]           ← 序列 A 使用 block 0,1,5
Block Table B:   [2] [3]               ← 序列 B 使用 block 2,3
Block Table C:   [4] [6] [7] [8]       ← 序列 C 使用 block 4,6,7,8

每个 Block = 固定 16 token 的 KV cache
分配粒度: 16 token（而非整个 max_seq_len）
```

**核心数据结构**：

```python
# vLLM 内部（简化）
class BlockTable:
    """逻辑序列 → 物理 block 的映射表"""
    logical_to_physical: dict[int, int]  # 逻辑 block ID → 物理 block ID

class BlockAllocator:
    """物理 block 池管理器"""
    free_blocks: list[int]       # 空闲 block 列表
    ref_count: dict[int, int]    # 每个 block 的引用计数（用于 CoW）
    
    def allocate(self) -> int:
        """分配一个空闲 block"""
        return self.free_blocks.pop()
    
    def free(self, block_id: int):
        """释放 block（引用计数为 0 时真正释放）"""
        self.ref_count[block_id] -= 1
        if self.ref_count[block_id] == 0:
            self.free_blocks.append(block_id)
```

#### 2.1.3 Prefix Caching（前缀缓存）

相同 system prompt 的多个请求可以**共享 KV cache block**：

```
请求 1: [system prompt (blocks 0-3)] + [user msg A (blocks 4-5)]
请求 2: [system prompt (blocks 0-3)] + [user msg B (blocks 6-7)]
                         ↑ 共享！Copy-on-Write

Block 0-3 的 ref_count = 2
当请求 1 结束时: ref_count 降为 1，不释放
当请求 2 也结束时: ref_count 降为 0，释放
```

**vLLM V1 的 prefix caching 实现**：
- 用 **hash(token_ids)** 作为 block 的 key
- 维护一个 `prefix_cache: dict[hash, block_id]`
- 新请求到来时，先查 cache 是否有匹配的 prefix blocks
- 命中则直接复用，未命中则正常分配

#### 2.1.4 Continuous Batching（连续批处理）

传统 static batching vs vLLM continuous batching：

```
Static Batching:
  Batch 1: [A(100tok), B(50tok), C(200tok)]
  等 C 生成完 200 tok 后，整个 batch 才能释放
  A 和 B 早就结束了，GPU 空等

Continuous Batching:
  Step 1: [A, B, C] 同时 decode
  Step 30: B 结束 → 立即插入新请求 D
  Step 50: A 结束 → 立即插入新请求 E
  Step 100: C 结束 → 立即插入新请求 F
  GPU 永远满载！
```

**vLLM Scheduler 的核心逻辑**：

```python
# vLLM 内部调度器（简化）
class Scheduler:
    def schedule(self):
        """每个 step 调用一次，决定哪些序列参与本次 forward"""
        # 1. 优先处理 running 队列（正在 decode 的序列）
        running_batch = self.running_queue[:max_batch_size]
        
        # 2. 如果有空余 GPU 显存，从 waiting 队列拉新请求做 prefill
        remaining_budget = self.get_memory_budget() - self.running_memory()
        while self.waiting_queue and remaining_budget > 0:
            new_seq = self.waiting_queue.pop(0)
            if new_seq.prompt_len * kv_per_token < remaining_budget:
                running_batch.append(new_seq)
                remaining_budget -= new_seq.prompt_len * kv_per_token
        
        # 3. 如果显存不够，preempt（踢出）优先级最低的序列
        while self.over_memory_budget():
            victim = self.select_victim()  # LRU / priority
            self.preempt(victim)  # swap to CPU 或 recompute
        
        return running_batch
```

#### 2.1.5 Chunked Prefill

长 prompt 的 prefill 会阻塞 decode 请求。Chunked Prefill 将长 prompt 分块处理：

```
传统 Prefill:
  Step 1: [Prefill 8000 tokens] ← 耗时 200ms，期间所有 decode 请求等待
  Step 2: [Decode batch]

Chunked Prefill:
  Step 1: [Prefill chunk 1 (512 tok)] + [Decode batch]  ← 混合执行
  Step 2: [Prefill chunk 2 (512 tok)] + [Decode batch]
  ...
  Step 16: [Prefill chunk 16 (512 tok)] + [Decode batch]
  
  Decode 请求不再被阻塞！TTFT P99 大幅降低
```

---

### 2.2 EAGLE-3 投机解码内部原理

#### 2.2.1 投机解码的数学保证

投机解码的核心定理：**验证后的输出分布与直接用 target 模型采样的分布完全一致**。

```python
# 验证算法（Speculative Sampling）
def speculative_verify(draft_probs, target_probs, draft_tokens):
    """
    对每个 draft token，以概率 min(1, target_p/draft_p) 接受
    被拒绝时，从修正分布 (target_p - draft_p)+ 重新采样
    """
    accepted = []
    for i, token in enumerate(draft_tokens):
        p_draft = draft_probs[i][token]
        p_target = target_probs[i][token]
        
        # 接受概率
        accept_prob = min(1.0, p_target / p_draft)
        
        if random.random() < accept_prob:
            accepted.append(token)
        else:
            # 从修正分布采样
            residual = torch.clamp(target_probs[i] - draft_probs[i], min=0)
            residual = residual / residual.sum()
            new_token = torch.multinomial(residual, 1)
            accepted.append(new_token)
            break  # 后续 draft token 全部丢弃
    
    return accepted
```

**数学证明**：接受的 token 序列的联合分布 = target model 直接采样的分布。这意味着**投机解码是精度无损的加速**。

#### 2.2.2 EAGLE-3 的 Draft 模型架构

EAGLE 系列的核心创新：draft 模型**看 target 的 hidden state**，而非只看 logits：

```
Medusa（旧方案）：
  target logits → N 个独立 MLP head → 预测 N 个位置的 token
  问题：每个 head 独立，无法建模 token 间依赖

EAGLE-1/2/3：
  target hidden_state (最后一层) → 轻量 Transformer（1-2 层）→ 预测下一个 token
  优势：
  1. hidden_state 比 logits 信息更丰富
  2. 自回归生成，能建模 token 间依赖
  3. accept_rate 从 ~50% 提升到 ~70-75%
```

**EAGLE-3 的具体架构**：

```python
# EAGLE-3 Draft Model（简化）
class Eagle3DraftModel(nn.Module):
    def __init__(self, hidden_size, num_layers=1):
        # 轻量 Transformer：1-2 层，与 target 共享 embedding
        self.layers = nn.ModuleList([
            TransformerBlock(hidden_size) for _ in range(num_layers)
        ])
        self.lm_head = target_model.lm_head  # 共享输出头！
    
    def forward(self, target_hidden_states, input_ids):
        """
        输入：target 模型最后一层的 hidden state + 已生成的 token
        输出：下一个 token 的 logits
        """
        # 将 target hidden state 作为额外输入
        h = self.embed(input_ids) + target_hidden_states
        for layer in self.layers:
            h = layer(h)
        return self.lm_head(h)
```

**参数量对比**：
- Target (Qwen3-8B): 8B params
- EAGLE-3 Draft: ~100-200M params（target 的 1-3%）
- 推理开销：draft 一次生成 5 个 token 的时间 ≈ target 生成 0.3 个 token 的时间

#### 2.2.3 Tree Attention（树形验证）

EAGLE-3 不是线性生成 5 个 token，而是生成一棵**候选树**：

```
                    tok_0 (已确认)
                   /      \
              tok_1a      tok_1b
             /    \         |
         tok_2a  tok_2b   tok_2c
           |       |
         tok_3a  tok_3b

Target 模型一次 forward 验证整棵树（通过 tree attention mask）
最长被接受的路径 = 实际输出
```

**Tree Attention Mask**：

```python
# 树形 attention mask（简化）
# 每个节点只能 attend 到自己的祖先节点
tree_mask = [
    [1, 0, 0, 0, 0, 0, 0],  # tok_0: 只看自己
    [1, 1, 0, 0, 0, 0, 0],  # tok_1a: 看 tok_0 + 自己
    [1, 0, 1, 0, 0, 0, 0],  # tok_1b: 看 tok_0 + 自己
    [1, 1, 0, 1, 0, 0, 0],  # tok_2a: 看 tok_0 + tok_1a + 自己
    ...
]
```

---

### 2.3 SGLang RadixAttention 内部原理

#### 2.3.1 Radix Tree 数据结构

RadixAttention 用 **Radix Tree（基数树/压缩前缀树）** 管理所有请求的 KV cache：

```
Radix Tree 示例：
                    root
                   /    \
        [system prompt]  [other prefix]
           /       \
    [user: "告警"]  [user: "部署"]
       /    \
  [resp1]  [resp2]

每个节点 = 一段 token 序列的 KV cache
查找时间: O(序列长度)，但实际因为前缀共享，大部分命中在浅层
```

#### 2.3.2 与 vLLM Prefix Caching 的区别

| 维度 | vLLM Prefix Caching | SGLang RadixAttention |
|------|--------------------|-----------------------|
| 数据结构 | Hash Table (block hash → block) | Radix Tree |
| 匹配粒度 | Block 级（16 token） | **Token 级**（任意前缀） |
| 多轮对话 | 只能匹配完整 block | 能匹配任意长度的公共前缀 |
| 命中率 | 中等（~60%） | **高（~85%）** |
| 适用场景 | 通用 | **多轮对话 / Agent** ⭐ |

#### 2.3.3 为什么 Agent 场景命中率更高

Agent 的多轮对话结构：
```
Turn 1: [system(500)] + [user_1(100)] + [assistant_1(200)]
Turn 2: [system(500)] + [user_1(100)] + [assistant_1(200)] + [user_2(80)] + [assistant_2(150)]
Turn 3: [system(500)] + [user_1(100)] + [assistant_1(200)] + [user_2(80)] + [assistant_2(150)] + [user_3(60)]
```

每轮新增的 token 只有最后的 user + assistant，前面的**全部命中 Radix Tree**。

---

### 2.4 vLLM Guided Decoding 内部原理

#### 2.4.1 xgrammar 引擎

vLLM 0.6+ 默认使用 `xgrammar` 作为 guided decoding 后端（比 outlines 快 10×）：

```python
# xgrammar 内部工作原理（简化）
class GrammarConstraint:
    def __init__(self, json_schema):
        # 1. JSON Schema → Context-Free Grammar (CFG)
        self.grammar = schema_to_cfg(json_schema)
        
        # 2. CFG → 确定性有限自动机 (DFA)
        #    预编译所有合法的 token 转移
        self.dfa = compile_to_dfa(self.grammar)
        
        # 3. 预计算每个 DFA 状态的合法 token mask
        #    vocab_size = 152064 (Qwen3)
        self.token_masks = precompute_masks(self.dfa, tokenizer)
    
    def get_allowed_tokens(self, state) -> torch.Tensor:
        """返回当前状态下合法的 token mask"""
        return self.token_masks[state]  # O(1) 查表
```

#### 2.4.2 在 logits 上的应用

```python
# vLLM 内部 guided decoding 流程
def apply_guided_decoding(logits, grammar_state):
    """在采样前 mask 掉不合法的 token"""
    allowed_mask = grammar.get_allowed_tokens(grammar_state)
    
    # 将不合法 token 的 logit 设为 -inf
    logits[~allowed_mask] = float('-inf')
    
    # 正常采样（softmax + multinomial）
    probs = F.softmax(logits, dim=-1)
    token = torch.multinomial(probs, 1)
    
    # 更新 DFA 状态
    new_state = grammar.transition(grammar_state, token)
    
    return token, new_state
```

**性能开销**：
- 预编译（首次请求）：~50ms
- 每步 mask 查表：~0.1ms（可忽略）
- 总体影响：首 token +5-15ms，吞吐 -3%

---

## 三、检索框架内部机制

### 3.1 BGE-M3 多任务架构

#### 3.1.1 模型架构

BGE-M3 基于 XLM-RoBERTa-large（568M params），通过**多任务训练**同时学习三种表示：

```
Input: "如何排查 OOM 告警"
  ↓
[XLM-RoBERTa Encoder] (12 层 Transformer)
  ↓
Hidden States: [h_CLS, h_1, h_2, ..., h_N]
  ↓
┌─────────────────────────────────────────────────┐
│ Dense Head:  h_CLS → Linear(768→1024) → L2Norm │ → 1024-dim vector
│ Sparse Head: h_i → Linear(768→1) → ReLU        │ → {token_id: weight}
│ ColBERT Head: h_i → Linear(768→1024) → L2Norm  │ → N × 1024 matrix
└─────────────────────────────────────────────────┘
```

#### 3.1.2 Sparse Head 的工作原理（学习型 BM25）

```python
# BGE-M3 Sparse Head 内部（简化）
class SparseHead(nn.Module):
    def forward(self, hidden_states, input_ids):
        """
        为每个 token 位置计算一个"重要性权重"
        输出格式: {token_id: weight}（类似 BM25 的 term frequency）
        """
        # 每个 token 的重要性分数
        weights = self.linear(hidden_states).squeeze(-1)  # [batch, seq_len]
        weights = F.relu(weights)  # 只保留正值（重要的 token）
        
        # 聚合同一 token 的权重（一个 token 可能出现多次）
        lexical_weights = {}
        for pos, token_id in enumerate(input_ids):
            if weights[pos] > 0:
                token_id_str = str(token_id.item())
                if token_id_str in lexical_weights:
                    lexical_weights[token_id_str] = max(
                        lexical_weights[token_id_str], weights[pos].item()
                    )
                else:
                    lexical_weights[token_id_str] = weights[pos].item()
        
        return lexical_weights
```

**与 BM25 的对比**：
| 维度 | BM25 | BGE-M3 Sparse |
|------|------|---------------|
| 权重来源 | TF-IDF 统计 | 神经网络学习 |
| 语义理解 | 无（纯词频） | 有（Transformer 上下文） |
| 同义词 | 不识别 | 能识别（上下文编码） |
| 训练 | 无需训练 | 需要对比学习 |
| 效果 | 精确匹配强 | 精确匹配 + 语义理解 |

#### 3.1.3 为什么一次前向能同时产出三种表示

BGE-M3 的训练目标是**多任务联合训练**：

```python
# BGE-M3 训练 loss（简化）
total_loss = (
    dense_contrastive_loss(dense_vecs_q, dense_vecs_d)      # InfoNCE
    + sparse_contrastive_loss(sparse_vecs_q, sparse_vecs_d)  # 稀疏向量对比
    + colbert_loss(colbert_vecs_q, colbert_vecs_d)           # token 级对比
    + distillation_loss(teacher_scores, student_scores)       # 知识蒸馏
)
```

三个 head 共享 Transformer backbone，各自有独立的投影层。推理时一次前向传播就能同时获得三种表示。

---

### 3.2 Qdrant 向量索引内部原理

#### 3.2.1 HNSW 索引结构

Qdrant 使用 **HNSW（Hierarchical Navigable Small World）** 作为核心索引：

```
Layer 3 (最稀疏):  [A] ─────────────────── [B]
                    │                        │
Layer 2:           [A] ── [C] ── [D] ── [B]
                    │      │      │      │
Layer 1:           [A]─[E]─[C]─[F]─[D]─[G]─[B]
                    │  │  │  │  │  │  │  │
Layer 0 (最密集):  [A][E][H][C][I][F][J][D][K][G][L][B]
                   (所有向量都在 Layer 0)
```

**搜索过程**：
1. 从最高层的入口点开始
2. 在当前层贪心搜索最近邻
3. 找到局部最优后，下降到下一层
4. 在下一层继续贪心搜索（起点是上层的结果）
5. 重复直到 Layer 0，返回 top-k

**复杂度**：O(log N) 搜索，O(N·M·log N) 构建（M = 每层连接数）

#### 3.2.2 Qdrant 的 Sparse Vector 索引

Qdrant 对稀疏向量使用**倒排索引**（类似搜索引擎）：

```
倒排索引结构：
token_id_1234 → [(doc_1, 0.8), (doc_5, 0.3), (doc_9, 0.6)]
token_id_5678 → [(doc_2, 0.9), (doc_5, 0.4)]
token_id_9012 → [(doc_1, 0.2), (doc_3, 0.7), (doc_8, 0.5)]

查询 sparse_vec = {1234: 0.7, 5678: 0.5}:
1. 查 token_1234 的 posting list → 候选: doc_1, doc_5, doc_9
2. 查 token_5678 的 posting list → 候选: doc_2, doc_5
3. 计算内积: doc_5 = 0.7×0.3 + 0.5×0.4 = 0.41（最高）
```

#### 3.2.3 Collection 配置与本项目的对应

```python
# build_index.py 创建 collection 时的配置
client.create_collection(
    collection_name="gameops_kb",
    vectors_config=models.VectorParams(
        size=1024,                    # BGE-M3 dense 维度
        distance=models.Distance.COSINE,
    ),
    sparse_vectors_config={
        "sparse": models.SparseVectorParams(
            index=models.SparseIndexParams(
                on_disk=False,        # 内存索引（快）
            )
        )
    },
    # HNSW 参数
    hnsw_config=models.HnswConfigDiff(
        m=16,                         # 每层连接数
        ef_construct=100,             # 构建时搜索宽度
    ),
)
```

---

### 3.3 BGE-Reranker-v2-m3 内部原理

#### 3.3.1 Cross-Encoder 架构

与 Bi-Encoder（分别编码 query 和 doc）不同，Cross-Encoder **同时编码 query+doc**：

```
Bi-Encoder (BGE-M3 检索):
  query → Encoder → vec_q ─┐
                            ├─ cosine(vec_q, vec_d) → score
  doc   → Encoder → vec_d ─┘
  优点: doc 可以离线编码，检索时只需算 query
  缺点: query 和 doc 无法交互注意力

Cross-Encoder (BGE-Reranker 精排):
  [CLS] query [SEP] doc [SEP] → Encoder → h_CLS → Linear → score
  优点: query 和 doc 的每个 token 都能互相 attend
  缺点: 每对 (query, doc) 都要重新编码，不能离线
```

#### 3.3.2 为什么精排比粗排准

Cross-Encoder 能捕捉**细粒度的语义交互**：

```
Query: "K8s pod OOM 怎么排查"
Doc A: "当 Pod 出现 OOMKilled 状态时，检查 resources.limits.memory 配置"
Doc B: "OOM (Out of Memory) 是操作系统层面的内存不足错误"

Bi-Encoder: 两个 doc 都包含 "OOM"，cosine 相似度接近
Cross-Encoder: 
  - Doc A 中 "Pod" 与 query 中 "K8s pod" 强交互 → 高分
  - Doc B 中 "操作系统" 与 query 中 "K8s" 不匹配 → 低分
```

#### 3.3.3 Reranker 的 normalize 参数

```python
# FlagReranker.compute_score 内部
def compute_score(self, pairs, normalize=True):
    """
    pairs: [["query", "doc1"], ["query", "doc2"], ...]
    normalize=True: 用 sigmoid 将分数映射到 [0, 1]
    normalize=False: 返回原始 logit（可能是负数）
    """
    # 编码
    inputs = self.tokenizer(pairs, padding=True, truncation=True, return_tensors="pt")
    # 前向
    logits = self.model(**inputs).logits.squeeze(-1)  # [num_pairs]
    
    if normalize:
        scores = torch.sigmoid(logits)  # → [0, 1]
    else:
        scores = logits
    
    return scores.tolist()
```

**本项目使用 `normalize=True`**，所以 `score_threshold=0.3` 的含义是：sigmoid 后低于 0.3 的候选被丢弃。

---

## 四、可观测性框架内部机制

### 4.1 Langfuse Trace 传播机制

#### 4.1.1 Trace 层次结构

```
Trace (一次完整请求)
├── Span: "retrieval" (检索阶段)
│   ├── Event: "dense_search" 
│   ├── Event: "sparse_search"
│   └── Event: "rerank"
├── Generation: "llm_call" (LLM 生成)
│   ├── input: messages
│   ├── output: response
│   ├── model: "qwen3-8b-knowledge-sft"
│   └── usage: {prompt_tokens: 1200, completion_tokens: 300}
└── Score: "user_feedback" (用户反馈)
```

#### 4.1.2 Session 关联机制

Langfuse 通过 `session_id` 将多个 trace 关联为一个会话：

```python
# Agent 侧（Go）
session_id = "agent-session-abc123"
# 通过 MCP 参数透传
mcp_call(tool="knowledge_expert_query", 
         params={"question": "...", "session_id": session_id})

# RAG 侧（Python）
with trace_scope("rag_query", session_id=session_id) as tr:
    # 这个 trace 自动归属到 session "agent-session-abc123"
    ...

# Langfuse UI 中：
# Session "agent-session-abc123"
#   ├── Trace: agent_planning (Go 侧上报)
#   ├── Trace: rag_query (Python 侧上报)  ← 通过 session_id 关联
#   └── Trace: tool_execution (Go 侧上报)
```

#### 4.1.3 异步上报与 Flush 机制

```python
# Langfuse SDK 内部（简化）
class LangfuseClient:
    def __init__(self):
        self._queue = Queue(maxsize=1000)  # 内存队列
        self._worker = Thread(target=self._flush_worker, daemon=True)
        self._worker.start()
    
    def trace(self, **kwargs):
        """非阻塞：放入队列即返回"""
        self._queue.put(("trace", kwargs))
        return TraceObject(...)
    
    def _flush_worker(self):
        """后台线程批量上报"""
        while True:
            batch = []
            # 攒够 batch_size 或等待 flush_interval
            while len(batch) < 100:
                try:
                    item = self._queue.get(timeout=1.0)
                    batch.append(item)
                except Empty:
                    break
            if batch:
                self._send_batch(batch)  # HTTP POST 到 Langfuse server
    
    def flush(self):
        """强制刷新队列（请求结束时调用）"""
        self._queue.join()
```

**关键设计**：
- 非阻塞：`trace()` / `span()` 调用不等待网络
- 批量上报：减少 HTTP 请求数
- `flush()` 在 `trace_scope` 的 `finally` 中调用，确保数据不丢

---

### 4.2 Prometheus 指标采集原理

#### 4.2.1 vLLM 暴露的关键指标

vLLM 内置 Prometheus exporter，暴露在 `/metrics` 端点：

```python
# vLLM 内部指标定义（简化）
from prometheus_client import Counter, Histogram, Gauge

# 请求级指标
vllm_request_success = Counter("vllm_request_success_total", "成功请求数")
vllm_request_duration = Histogram("vllm_e2e_request_latency_seconds", "端到端延迟",
    buckets=[0.1, 0.3, 0.5, 1, 2, 5, 10, 30])

# Token 级指标
vllm_prompt_tokens = Counter("vllm_prompt_tokens_total", "输入 token 总数")
vllm_generation_tokens = Counter("vllm_generation_tokens_total", "生成 token 总数")

# KV Cache 指标
vllm_gpu_cache_usage = Gauge("vllm_gpu_cache_usage_perc", "GPU KV cache 使用率")
vllm_cpu_cache_usage = Gauge("vllm_cpu_cache_usage_perc", "CPU swap 使用率")

# 投机解码指标
vllm_spec_decode_draft_acceptance = Gauge(
    "vllm_spec_decode_draft_acceptance_rate", "Draft 接受率")

# 调度指标
vllm_num_requests_running = Gauge("vllm_num_requests_running", "正在处理的请求数")
vllm_num_requests_waiting = Gauge("vllm_num_requests_waiting", "等待队列长度")
```

#### 4.2.2 Pull 模型 vs Push 模型

Prometheus 使用 **Pull 模型**：

```
Prometheus Server ──(每 15s)──> GET /metrics ──> vLLM
                  ──(每 15s)──> GET /metrics ──> RAG Service
                  ──(每 15s)──> GET /metrics ──> Qdrant

优势：
1. 服务端无需知道 Prometheus 地址
2. 服务挂了 Prometheus 能检测到（scrape 失败 = down）
3. 不会因为 push 风暴压垮监控系统
```

---

## 五、分布式训练框架内部机制

### 5.1 DeepSpeed ZeRO 通信原语

#### 5.1.1 ZeRO-1/2/3 切分策略

```
全参训练的显存组成（以 Adam 为例）：
  参数 W:        2 bytes/param (BF16)
  梯度 G:        2 bytes/param (BF16)
  优化器状态 O:  12 bytes/param (FP32 master + momentum + variance)
  总计:          16 bytes/param

8B 模型 = 128 GB 显存（单卡放不下）

ZeRO-1: 只切 O → 每卡 2+2+12/N bytes/param
ZeRO-2: 切 O+G → 每卡 2+(2+12)/N bytes/param  
ZeRO-3: 切 O+G+W → 每卡 (2+2+12)/N bytes/param = 16/N bytes/param
```

#### 5.1.2 ZeRO-2 的通信模式

```python
# ZeRO-2 训练一步的通信（简化）
def zero2_training_step(model, batch, optimizer):
    # 1. Forward: 每卡有完整参数 W，正常前向
    loss = model(batch)
    
    # 2. Backward: 每卡计算完整梯度 G
    loss.backward()
    
    # 3. Reduce-Scatter: 梯度分片
    #    每卡只保留自己负责的那 1/N 梯度
    #    通信量: 2 × model_size × (N-1)/N ≈ 2 × model_size
    reduce_scatter(gradients)
    
    # 4. 优化器更新: 每卡只更新自己负责的 1/N 参数
    optimizer.step()  # 只更新 local shard
    
    # 5. All-Gather: 收集更新后的参数
    #    通信量: model_size × (N-1)/N ≈ model_size
    all_gather(parameters)
```

#### 5.1.3 ZeRO-3 + CPU Offload

```python
# ZeRO-3 的关键区别：参数也被切分
def zero3_forward(model, batch):
    for layer in model.layers:
        # 前向时按需 All-Gather 当前层的参数
        all_gather(layer.parameters())  # GPU ← 其他 GPU
        
        output = layer(input)
        
        # 用完立即释放（不保留在 GPU）
        partition(layer.parameters())  # 只保留 1/N
    
    return output

# CPU Offload: 优化器状态放 CPU
# GPU 只在 optimizer.step() 时把梯度传给 CPU
# CPU 更新后把新参数传回 GPU
```

**本项目配置**（`infra/distributed/ds_zero3.json`）：

```json
{
    "zero_optimization": {
        "stage": 3,
        "offload_optimizer": {"device": "cpu", "pin_memory": true},
        "offload_param": {"device": "cpu", "pin_memory": true},
        "overlap_comm": true,
        "contiguous_gradients": true,
        "reduce_bucket_size": 5e7
    }
}
```

---

### 5.2 FSDP（Fully Sharded Data Parallel）内部原理

#### 5.2.1 FSDP vs ZeRO-3

FSDP 是 PyTorch 原生的 ZeRO-3 实现，核心思想完全相同：

```python
# FSDP 内部（简化）
class FullyShardedDataParallel(nn.Module):
    def __init__(self, module, sharding_strategy):
        # 将参数 flatten + 切分到各 rank
        self._shard_parameters()
    
    def forward(self, *args):
        # 前向前：All-Gather 收集完整参数
        self._all_gather_params()
        
        output = self.module(*args)
        
        # 前向后：释放非本 rank 的参数
        self._free_full_params()
        
        return output
    
    def _post_backward_hook(self):
        # 反向后：Reduce-Scatter 梯度
        self._reduce_scatter_grads()
```

#### 5.2.2 FSDP 的 Sharding Strategy

| 策略 | 等价 ZeRO | 切分内容 | 通信量 |
|------|-----------|---------|--------|
| `FULL_SHARD` | ZeRO-3 | 参数+梯度+优化器 | 最大 |
| `SHARD_GRAD_OP` | ZeRO-2 | 梯度+优化器 | 中等 |
| `NO_SHARD` | DDP | 不切分 | 最小 |

---

### 5.3 FlashAttention 内部原理

#### 5.3.1 朴素 Attention 的显存问题

```python
# 朴素实现
S = Q @ K.T          # [N, N] 矩阵，N=4096 时占 128MB (FP32)
P = softmax(S)       # [N, N] 又一个 128MB
O = P @ V            # [N, d]

# 总中间显存: 2 × N² × sizeof(float) = 2 × 4096² × 4 = 128 MB
# 如果 N=32768 (长上下文): 2 × 32768² × 4 = 8 GB !!!
```

#### 5.3.2 FlashAttention 的 Tiling 策略

```python
# FlashAttention 核心思想（简化伪代码）
def flash_attention(Q, K, V, block_size=256):
    """
    不 materialize N×N 的 S 矩阵
    分块计算，中间结果留在 SRAM
    """
    N, d = Q.shape
    O = torch.zeros(N, d)
    
    # 外层循环：遍历 K/V 的 block
    for j in range(0, N, block_size):
        Kj = K[j:j+block_size]  # 加载一个 block 到 SRAM
        Vj = V[j:j+block_size]
        
        # 内层循环：遍历 Q 的 block
        for i in range(0, N, block_size):
            Qi = Q[i:i+block_size]  # 加载一个 block 到 SRAM
            
            # 在 SRAM 内计算局部 attention
            Sij = Qi @ Kj.T  # [block, block] 在 SRAM 内
            
            # Online Softmax：增量更新 max 和 sum
            # 不需要看完整行就能算 softmax！
            m_new = max(m_old, Sij.max())
            P_ij = exp(Sij - m_new)
            l_new = l_old * exp(m_old - m_new) + P_ij.sum()
            
            # 累积输出
            O[i:i+block_size] = (O[i:i+block_size] * l_old * exp(m_old - m_new) 
                                 + P_ij @ Vj) / l_new
            
            m_old, l_old = m_new, l_new
    
    return O
```

#### 5.3.3 Online Softmax 的数学推导

标准 softmax 需要两遍扫描：
1. 第一遍：找 max（数值稳定性）
2. 第二遍：计算 exp 和 sum

Online Softmax 只需一遍：

```
维护两个状态：m (running max), l (running sum of exp)

当新 block 到来时：
  m_new = max(m_old, block_max)
  
  # 修正旧的 sum（因为 max 变了）
  l_new = l_old × exp(m_old - m_new) + sum(exp(block - m_new))
  
  # 修正旧的输出（因为 softmax 分母变了）
  O_new = O_old × (l_old/l_new) × exp(m_old - m_new) + (P_block @ V_block) / l_new
```

**结果**：HBM I/O 从 O(N²) 降到 O(N²/M)，其中 M = SRAM 大小。实际效果：**显存 O(N) + 速度 2-4×**。

---

## 六、Web 框架内部机制

### 6.1 FastAPI 异步调度

#### 6.1.1 ASGI 事件循环

```python
# FastAPI + Uvicorn 的内部架构
#
# Uvicorn (ASGI Server)
#   └── asyncio event loop (单线程)
#       ├── 接收 HTTP 连接
#       ├── 路由到 FastAPI handler
#       └── 并发处理多个请求（协程切换）

# RAG 服务中的异步处理
@app.post("/rag/query")
async def rag_query(req: RAGRequest):
    # 1. 检索（CPU 密集）→ 放到线程池避免阻塞事件循环
    chunks = await asyncio.to_thread(RETRIEVER.search, req.query)
    
    # 2. LLM 生成（网络 I/O）→ 原生异步
    answer = await GENERATOR.complete(messages, stream=False)
    
    return RAGResponse(answer=answer, ...)
```

#### 6.1.2 为什么检索用 `asyncio.to_thread`

```python
# BGE-M3 编码是 CPU/GPU 密集操作（PyTorch forward）
# 如果直接在 async handler 中调用，会阻塞整个事件循环
# 其他请求全部等待！

# asyncio.to_thread 将同步函数放到线程池执行
# 事件循环不被阻塞，可以继续处理其他请求
chunks = await asyncio.to_thread(RETRIEVER.search, req.query)
# 等价于：
loop = asyncio.get_event_loop()
chunks = await loop.run_in_executor(None, RETRIEVER.search, req.query)
```

#### 6.1.3 流式响应（SSE）

```python
# FastAPI 流式响应实现
from fastapi.responses import StreamingResponse

@app.post("/rag/query")
async def rag_query(req: RAGRequest):
    if req.stream:
        return StreamingResponse(
            stream_generator(req),
            media_type="text/event-stream"
        )

async def stream_generator(req):
    """SSE 格式的流式生成"""
    # 先返回检索结果
    yield f"data: {json.dumps({'type': 'citations', 'data': citations})}\n\n"
    
    # 流式返回 LLM 生成
    async for chunk in GENERATOR.stream(messages):
        yield f"data: {json.dumps({'type': 'token', 'data': chunk})}\n\n"
    
    yield "data: [DONE]\n\n"
```

---

### 6.2 MCP 协议传输层

#### 6.2.1 Streamable HTTP 传输

MCP Streamable HTTP 是基于 HTTP 的双向通信协议：

```
Client (Agent)                          Server (MCP Expert)
    │                                        │
    │── POST /mcp ──────────────────────────>│  (JSON-RPC request)
    │   Content-Type: application/json       │
    │   Body: {"method": "tools/call",       │
    │          "params": {"name": "...",     │
    │                     "arguments": {}}}  │
    │                                        │
    │<── 200 OK ─────────────────────────────│  (JSON-RPC response)
    │   Content-Type: text/event-stream      │  (可以是流式！)
    │   data: {"result": {...}}              │
    │                                        │
    │── POST /mcp (with Mcp-Session-Id) ────>│  (同 session 后续请求)
    │                                        │
```

#### 6.2.2 Session 管理

```python
# FastMCP 内部 session 管理（简化）
class MCPServer:
    def __init__(self):
        self.sessions: dict[str, Session] = {}
    
    async def handle_request(self, request):
        session_id = request.headers.get("Mcp-Session-Id")
        
        if session_id and session_id in self.sessions:
            session = self.sessions[session_id]
        else:
            session = Session()
            session_id = str(uuid4())
            self.sessions[session_id] = session
        
        # 处理 JSON-RPC 请求
        result = await self.dispatch(request.json(), session)
        
        # 返回时带上 session_id
        return Response(
            content=json.dumps(result),
            headers={"Mcp-Session-Id": session_id}
        )
```

#### 6.2.3 重连机制

本项目 Agent 侧配置 `WithSessionReconnect(3)`：

```go
// project-agent 侧（Go）
client := mcp.NewStreamableHTTPClient(
    mcp.WithURL("http://localhost:8200/mcp"),
    mcp.WithSessionReconnect(3),  // 最多重连 3 次
    mcp.WithTimeout(60 * time.Second),
)
```

重连时携带 `Mcp-Session-Id`，服务端恢复 session 上下文。

---

## 七、量化框架内部机制

### 7.1 LLMCompressor FP8 量化

#### 7.1.1 FP8 数据格式

```
FP8 E4M3（用于 forward / 权重）：
  1 bit sign + 4 bit exponent + 3 bit mantissa
  范围: [-448, 448]，精度: ~0.1%
  
FP8 E5M2（用于 backward / 梯度）：
  1 bit sign + 5 bit exponent + 2 bit mantissa
  范围: [-57344, 57344]，精度: ~0.4%
  
对比 FP16：
  1 bit sign + 5 bit exponent + 10 bit mantissa
  范围: [-65504, 65504]，精度: ~0.001%
```

#### 7.1.2 Dynamic Quantization（动态量化）

```python
# LLMCompressor FP8 动态量化内部（简化）
def quantize_fp8_dynamic(tensor):
    """
    动态量化：每次推理时根据实际数据范围计算 scale
    优点：精度高（适应每个 batch 的数据分布）
    缺点：每次都要算 scale（微小开销）
    """
    # 1. 计算当前 tensor 的 absmax
    absmax = tensor.abs().max()
    
    # 2. 计算 scale（将数据映射到 FP8 范围）
    fp8_max = 448.0  # E4M3 的最大值
    scale = absmax / fp8_max
    
    # 3. 量化
    quantized = (tensor / scale).to(torch.float8_e4m3fn)
    
    return quantized, scale

# vs Static Quantization：
# scale 在 calibration 时固定，推理时不再计算
# 优点：零开销
# 缺点：如果推理数据分布与 calibration 不同，精度下降
```

**本项目选择 `activation_scheme=dynamic`**：动态量化精度更高（MMLU 只掉 0.3pp vs static 掉 0.5pp），开销可忽略。

#### 7.1.3 Per-Tensor vs Per-Channel vs Per-Token

| 粒度 | Scale 数量 | 精度 | 开销 |
|------|-----------|------|------|
| Per-Tensor | 1 per tensor | 低 | 最小 |
| Per-Channel | 1 per output channel | 中 | 小 |
| Per-Token | 1 per token position | **高** ⭐ | 中 |

vLLM FP8 默认使用 **Per-Tensor dynamic**（权重）+ **Per-Token dynamic**（激活）。

---

### 7.2 GPTQ 量化内部原理

#### 7.2.1 核心思想：逐列量化 + 误差补偿

GPTQ 基于 OBQ（Optimal Brain Quantization），核心是**量化一列权重时，用 Hessian 信息补偿到其他列**：

```python
# GPTQ 量化算法（简化）
def gptq_quantize(W, H, bits=4, group_size=128):
    """
    W: 权重矩阵 [out_features, in_features]
    H: Hessian 矩阵 (X^T X)，反映每个权重的重要性
    """
    Q = torch.zeros_like(W)  # 量化后的权重
    
    for col in range(W.shape[1]):
        # 1. 量化当前列
        q = quantize_to_grid(W[:, col], bits, group_size)
        Q[:, col] = q
        
        # 2. 计算量化误差
        error = (W[:, col] - q) / H[col, col]
        
        # 3. 将误差补偿到后续列（Hessian 加权）
        W[:, col+1:] -= error.unsqueeze(1) * H[col, col+1:].unsqueeze(0)
    
    return Q
```

**为什么需要 Hessian**：
- Hessian `H = X^T X` 反映了每个权重对输出的影响程度
- 重要的权重（H 对角线大）量化误差影响更大
- GPTQ 优先保证重要权重的精度

#### 7.2.2 Marlin Kernel 加速

GPTQ 量化后的 INT4 权重需要特殊 kernel 才能高效推理。**Marlin** 是 NVIDIA 优化的 INT4×FP16 GEMM kernel：

```
标准 FP16 GEMM: A(FP16) × W(FP16) → C(FP16)
Marlin GEMM:    A(FP16) × W(INT4, packed) → C(FP16)

Marlin 的优化：
1. INT4 权重 4 个一组 pack 到 INT16，减少显存带宽
2. 反量化融合到 GEMM kernel 内部（不单独反量化）
3. 利用 Tensor Core 的 INT8 指令做部分计算
4. 结果：比朴素 FP16 GEMM 快 2-3×（因为权重带宽减半）
```

---

## 八、面试高频问题速查

### 8.1 框架原理类

**Q：LLaMAFactory 的 `neat_packing` 和普通 padding 有什么区别？**
> neat_packing 将多个短样本拼接到一个 `cutoff_len` 序列中，通过 block-diagonal attention mask 防止跨样本注意力泄漏。相比 padding，GPU 利用率从 ~60% 提升到 ~95%，等效吞吐量 +30%。

**Q：BitsAndBytes 的 NF4 为什么比 INT4 好？**
> 神经网络权重近似正态分布，NF4 的 16 个量化点按正态分布分位点分布，在分布密集区（接近 0）分配更多量化点，信息论上是最优的。实测 NF4 比 INT4 均方误差低 ~30%。

**Q：LoRA 的 merge_and_unload 后精度会变吗？**
> 如果基座是 NF4 量化的，merge 时会先反量化为 BF16，再加上 LoRA 的 ΔW。合并后的模型是全精度 BF16，**精度不会因为合并操作本身下降**。但 NF4→BF16 的反量化有微小误差（~0.1%）。

**Q：vLLM 的 Continuous Batching 和 Static Batching 的本质区别？**
> Static Batching 等最慢的序列结束才释放整个 batch；Continuous Batching 在每个 decode step 都可以插入新请求/释放已完成请求。本质是从"batch 级调度"变为"step 级调度"，GPU 利用率从 ~40% 提升到 ~90%。

**Q：FlashAttention 为什么能同时省显存又加速？**
> 省显存：不 materialize N×N 的 attention 矩阵，显存从 O(N²) 降到 O(N)。加速：减少 HBM 读写次数（从 4 次降到 1 次），对于 memory-bound 的 attention 操作，减少 I/O = 加速。

**Q：EAGLE-3 投机解码为什么是精度无损的？**
> 数学保证：验证算法以概率 min(1, p_target/p_draft) 接受 draft token，被拒绝时从修正分布 (p_target - p_draft)+ 重新采样。可以证明最终输出的联合分布与直接用 target 采样完全一致。

**Q：Qdrant 的 HNSW 索引为什么比暴力搜索快？**
> HNSW 是多层跳表结构，搜索从最稀疏层开始贪心下降，每层只需检查 O(log N) 个节点。总复杂度 O(log N) vs 暴力搜索 O(N)。代价是构建时间 O(N·M·log N) 和额外的图存储空间。

**Q：BGE-M3 的 Sparse Head 和 BM25 有什么本质区别？**
> BM25 的权重来自统计（TF-IDF），无法理解语义；BGE-M3 的 Sparse Head 是神经网络学习的，能通过 Transformer 上下文理解同义词和语义关系。例如 "OOM" 和 "内存不足" 在 BM25 中完全无关，但 BGE-M3 Sparse 能给它们相似的权重。

### 8.2 工程决策类

**Q：为什么 DPO 用 LoRA 时不需要额外的 ref model 显存？**
> PEFT 的实现中，ref model 和 policy model 共享基座权重。ref 的前向传播跳过 LoRA adapter（等价于只用基座），policy 的前向传播走 LoRA adapter。两者共享同一份 NF4 量化的基座参数，不需要额外显存。

**Q：Liger Kernel 的 FusedLinearCrossEntropy 为什么是最大的显存优化？**
> Qwen3-8B 的 vocab_size=152064，对于 batch=4, seq=4096 的训练，logits 矩阵占 4×4096×152064×2=4.7GB。FusedLinearCrossEntropy 分 chunk 计算，每次只 materialize 一小块 logits，峰值显存从 4.7GB 降到几十 MB。

**Q：RRF 为什么比归一化加权更好？**
> Dense 检索的 cosine 分数 ∈ [-1,1]，Sparse 检索的内积分数无上界，两者尺度完全不同。归一化需要校准（min-max 或 z-score），对数据分布敏感。RRF 只看排名不看绝对分数，对分数分布免疫，是 TREC 多年验证的稳定方案。

---

> **配套文档**：
> - [02_TRAINING_SYSTEM.md](./02_TRAINING_SYSTEM.md)：训练配置与自定义实现
> - [04_INFERENCE_DEPLOY.md](./04_INFERENCE_DEPLOY.md)：部署方案与性能数据
> - [05_RAG_SYSTEM.md](./05_RAG_SYSTEM.md)：RAG 自定义实现
> - [09_AI_INFRA.md](./09_AI_INFRA.md)：CUDA/分布式底层实现
