# 🔬 深水区补全 · 面经缺口题全覆盖

> 📌 **本文档目标**：覆盖 `markDown1780553553270.md` 面经 + 6 张图片中**现有文档未覆盖或覆盖不足**的高频题。
>
> 配套阅读：
> - [framework_internals.md](framework_internals.md)：框架内部机制
> - [framework_vs_self.md](framework_vs_self.md)：框架 vs 自研拆解
> - [../project-agent/INTERVIEW.md](../project-agent/INTERVIEW.md)：Agent 项目答辩
> - [../project-llm/INTERVIEW.md](../project-llm/INTERVIEW.md)：LLM 项目答辩

---

## 一、Agentic RL（图片第 3 部分 · 完全缺失）

### Q1：给定一个 query，Agentic RL 的完整训练流程是什么？

> **核心区别**：传统 RL（如 math reasoning）一次 rollout 就是模型生成一段文本；Agentic RL 的 rollout 是**多轮交互**——模型输出 → 工具调用 → 环境返回 observation → 模型继续推理 → 再调工具 → ... → 最终答案。

```
完整流程：
1. Prompt Pool 采样 query
2. Policy Model 生成第一轮 response（可能含 tool_call）
3. 环境执行工具，返回 observation
4. 拼接 [query + response_1 + observation_1] 作为新 context
5. Policy Model 继续生成 response_2
6. 重复 3-5 直到模型输出 <end> 或达到 max_turns
7. 对完整轨迹打 reward（outcome reward / process reward）
8. 计算 advantage（GRPO: group 内归一化）
9. PPO/GRPO loss 更新 policy
```

**关键难点**：
- 轨迹长度不固定（3 轮 ~ 30 轮），batch 内 padding 浪费严重
- 工具调用是真实 API，有延迟、有失败、有副作用
- 训练和推理交替进行，GPU 利用率低

### Q2：Agent loop 如何运行：模型输出、工具调用、工具返回、上下文拼接、继续生成分别怎么衔接？

> ```
> context = [system_prompt, user_query]
> for turn in range(max_turns):
>     response = model.generate(context)  # 可能含 <tool_call>...</tool_call>
>     context.append({"role": "assistant", "content": response})
>     
>     if has_tool_call(response):
>         tool_name, args = parse_tool_call(response)
>         observation = env.execute(tool_name, args)  # 真实执行
>         context.append({"role": "tool", "content": observation})
>     elif has_final_answer(response):
>         break
> 
> reward = reward_fn(context, ground_truth)
> ```

**衔接关键**：
- 模型输出和工具返回用**特殊 token 分隔**（如 `<tool_call>` / `<tool_result>`）
- 上下文是**追加式**（append-only），不回溯修改
- 每轮生成都是**独立的 forward pass**，但 KV cache 可以复用前面的

### Q3：Agent 训练中哪些 token 参与 loss，哪些 token 需要 mask 掉？

> **核心原则**：只对**模型自己生成的 token** 计算 loss，环境返回的 observation **必须 mask**。

| Token 来源 | 参与 loss | 原因 |
|---|---|---|
| System prompt | ❌ mask | 固定模板，不需要学 |
| User query | ❌ mask | 输入，不是模型输出 |
| Assistant response（含 tool_call） | ✅ 参与 | 这是模型要学的决策 |
| Tool observation / 环境返回 | ❌ mask | 外部信息，模型不应该"学会生成"环境结果 |
| Final answer | ✅ 参与 | 最终输出质量 |

**实现**：在 tokenize 时给每个 token 打 `loss_mask` 标记，训练时 `loss = loss * loss_mask`。

### Q4：多轮 Agentic RL 里，什么时候停止 rollout？

> 三种停止条件（任一触发）：
> 1. **模型主动结束**：输出 `<end>` / `<final_answer>` 等终止 token
> 2. **达到 max_turns**：硬上限（通常 10-20 轮），防止死循环
> 3. **达到 max_tokens**：总 token 数超限（如 32k），强制截断
>
> **额外保护**：
> - 同一工具同参数连续调用 3 次 → 注入"你已经重复了"提示 → 再重复直接终止
> - 总耗时超过 timeout（如 5 分钟）→ 强制终止，标记为 failed trajectory

### Q5：模型陷入死循环或 turn 数远超预期时，如何处理？

> **训练时**：
> 1. 设 `max_turns` 硬上限，超限直接截断，给 **负 reward**（如 -1）
> 2. 检测重复模式：连续 3 轮输出相似度 > 0.95 → 提前终止 + 负 reward
> 3. **Reward shaping**：加 step penalty `r_step = -0.01 * num_turns`，鼓励高效完成
>
> **推理时**（我项目的做法）：
> - ReAct 配 `max_iterations=10`
> - 同名工具同参数连续调 3 次自动注入提示
> - 全局 `context.WithTimeout` 兜底

### Q6：多轮 Agentic RL 是否遇到过推理耗时和长尾轨迹？有哪些优化方法？

> **问题**：一个 batch 里有的轨迹 3 轮结束，有的 20 轮还没完，**短轨迹 GPU 空等长轨迹**。

> **优化方法**：
> 1. **异步 rollout**：短轨迹完成后立即开始下一个 query，不等同 batch 的长轨迹
> 2. **轨迹截断 + 部分 reward**：超过 P95 长度的轨迹直接截断，给部分 reward
> 3. **动态 batch**：按预估轨迹长度分桶，短的一批、长的一批
> 4. **KV cache 复用**：同一轨迹内多轮生成共享 KV cache，避免重算
> 5. **工具调用并行化**：多个独立工具调用可以并发执行

### Q7：如果某些轨迹 rollout 特别长，应该丢弃、截断、异步处理，还是复用？各有什么代价？

| 策略 | 优点 | 缺点 |
|---|---|---|
| **丢弃** | 简单、不拖慢训练 | 丢失困难样本的学习信号 |
| **截断** | 保留部分信息 | 截断点的 reward 不准确 |
| **异步处理** | 不浪费 GPU 等待 | 实现复杂、off-policy 风险 |
| **复用**（多次更新） | 数据利用率高 | off-policy 偏差累积 |

> **实践推荐**：截断 + step penalty 组合。截断到 max_turns 后给 `reward = partial_reward - length_penalty`，既不丢信号又不拖训练。

### Q8：rollout 很贵时，能否一批 rollout 数据多次更新？这会带来什么 off-policy 问题？

> **可以**，但有代价。PPO/GRPO 本身支持多 epoch 更新（`ppo_epochs=4`），但：
> - 每次更新后 policy 变了，旧 rollout 的 logprob 不再准确
> - **KL 散度**会累积：`KL(π_new || π_old)` 越来越大
> - 超过 clip 范围的 token 越来越多，梯度信号被截断
>
> **缓解**：
> - 严格限制 `ppo_epochs ≤ 4`
> - 监控 `approx_kl`，超过阈值（如 0.02）提前停止当前 batch 的更新
> - 用 importance sampling ratio clip（PPO 的核心机制）

### Q9：训练和推理分离、异步 rollout 会带来什么收益和风险？

> **收益**：
> - GPU 利用率从 ~30%（同步等工具）提升到 ~80%
> - 训练节点不被推理阻塞，吞吐翻倍
> - 可以用不同硬件：推理用 A10（便宜），训练用 H100（算力强）
>
> **风险**：
> - **Policy lag**：推理用的是旧 policy，训练用新 policy 更新，产生 off-policy 偏差
> - **数据一致性**：rollout 数据的 logprob 是旧 policy 算的，更新时需要 importance correction
> - **工程复杂度**：需要 KV store 传递轨迹数据、版本对齐、故障恢复

### Q10：异步 Agentic RL 中 policy lag / off-policy 问题如何补偿？

> 三种方案：
> 1. **Importance Sampling**：`ratio = π_new(a|s) / π_old(a|s)`，用 ratio 加权 advantage
> 2. **V-trace**（IMPALA 方案）：截断 importance weight `min(c, ratio)`，防止方差爆炸
> 3. **定期同步**：每 N 步把最新 policy 推送到 rollout worker，控制 lag < 阈值
>
> **实践**：大多数框架（VeRL/OpenRLHF）选方案 3——lag 控制在 1-2 个 update step 内，偏差可忽略。

### Q11：Agentic RL 中影响训练效率的因素有哪些？

> 1. **工具调用延迟**：真实 API 100ms-10s 不等，是最大瓶颈
> 2. **轨迹长度方差**：batch 内长短不一导致 GPU 空等
> 3. **环境不稳定**：API 超时/失败需要重试
> 4. **KV cache 显存**：长轨迹占用大量 KV cache
> 5. **Reward 稀疏**：只有最终结果有 reward，中间步骤无信号
> 6. **数据新鲜度**：on-policy 要求每次更新后重新 rollout

### Q12：工具调用、API 请求、沙箱执行成为瓶颈时，可以怎么优化？

> 1. **工具 Mock / 缓存**：相同参数的工具调用缓存结果，训练时不重复执行
> 2. **异步并行**：多个 rollout 的工具调用并发执行
> 3. **沙箱池化**：预热 N 个沙箱实例，避免冷启动
> 4. **工具结果预计算**：对常见 query 预跑工具，存入 replay buffer
> 5. **模拟环境**：用轻量模型模拟工具返回（牺牲真实性换速度）

### Q13：为什么多轮 Agentic RL 会有 credit assignment 问题？

> **问题本质**：最终 reward 只有一个（如"任务成功/失败"），但轨迹有 10+ 步，**哪一步的决策导致了成功/失败？**
>
> 例如：Agent 第 3 步选错了工具但第 7 步纠正了 → 最终成功 → 第 3 步该得正 reward 还是负 reward？
>
> **解决方案**：
> 1. **Process Reward Model (PRM)**：给每一步打分，不只看最终结果
> 2. **Step-level advantage**：用 GAE（Generalized Advantage Estimation）按时间步衰减
> 3. **Outcome + Process 混合**：`reward = 0.7 * outcome + 0.3 * Σ process_rewards`

### Q14：Process Reward 的优势是什么？可以从哪些维度设计？

> **优势**：
> - 解决 credit assignment：每步都有信号，梯度更稳定
> - 加速收敛：不用等到轨迹结束才知道好坏
> - 可解释性：能看到哪一步得分低
>
> **维度设计**（运维 Agent 场景）：
> | 维度 | 评估内容 | 示例 |
> |---|---|---|
> | 工具选择正确性 | 当前步选的工具是否合理 | 排障应该先查监控不是先重启 |
> | 参数合法性 | 工具参数是否正确 | cluster_id 是否存在 |
> | 信息增益 | 这步是否获得了新信息 | 重复查同一个指标 = 0 增益 |
> | 进度推进 | 是否朝目标前进 | 从"不知道原因"到"定位到 OOM" |
> | 安全性 | 是否有危险操作 | 未确认就执行 scale-down = 负分 |

---

## 二、RL 算法补充（DAPO · 图片第 2 部分）

### Q15：DAPO 相比 GRPO 的核心改进是什么？

> DAPO（Dynamic Advantage Policy Optimization，字节 2024）在 GRPO 基础上做了 4 个改进：
>
> 1. **动态采样（Dynamic Sampling）**：
>    - GRPO 固定采样 G 个 response
>    - DAPO 根据 reward 分布动态调整：如果 group 内 reward 方差太小（全对或全错），**跳过这个 prompt**，不浪费算力
>
> 2. **剔除零奖励样本（Zero-Reward Filtering）**：
>    - 全 0 reward 的 group 不参与梯度更新
>    - 避免"全错"的 prompt 产生无意义的归一化 advantage
>
> 3. **Token-level KL penalty**（替代 sequence-level）：
>    - 更细粒度的约束，避免某些 token 偏离太远
>
> 4. **Overlong Filtering**：
>    - 超长 response 直接截断 + 给固定负 reward
>    - 防止模型学会"写得越长分越高"

### Q16：DAPO 的动态采样为什么能提升训练效率？

> **核心洞察**：不是所有 prompt 都有同等训练价值。
> - **全对的 prompt**（G 个 response 全拿满分）：模型已经会了，梯度 ≈ 0，浪费算力
> - **全错的 prompt**（G 个 response 全 0 分）：归一化后 advantage 全为 0，也没梯度
> - **有区分度的 prompt**（有对有错）：这才是模型能学到东西的
>
> DAPO 的动态采样**自动跳过前两类**，把算力集中在"有区分度"的 prompt 上，等效于 **curriculum learning**。

### Q17：DAPO 把零奖励样本剔除后，会不会限制模型学习困难样本？有什么解决思路？

> **确实有风险**：困难 prompt 可能 G 个 response 全错 → 被剔除 → 模型永远学不会。
>
> **解决思路**：
> 1. **增大 G**：采样更多 response，增加"至少有一个对"的概率
> 2. **分阶段训练**：先在简单 prompt 上训，模型变强后再引入困难 prompt
> 3. **Reward shaping**：给部分正确的 response 非零 reward（如"方向对但最终答案错"给 0.3）
> 4. **Replay buffer**：困难 prompt 不丢弃，累积到下一轮重试
> 5. **Expert demonstration**：对全错 prompt 注入一条 expert 轨迹作为 positive sample

### Q18：GRPO / DAPO 是 on-policy 还是 off-policy 思路？

> **On-policy**。每次更新前都用**当前 policy** 重新 rollout 采样，advantage 基于当前 policy 的 logprob 计算。
>
> 但实际工程中有 **off-policy 成分**：
> - `ppo_epochs > 1`：同一批数据更新多次，后几次已经是"旧数据"
> - 异步 rollout：rollout worker 用的 policy 比 trainer 落后 1-2 步
>
> **和 DPO 的区别**：DPO 是 **off-policy**——用固定的 preference 数据集训练，不需要在线采样。这是 DPO 便宜但效果上限低的原因。

### Q19：DPO 和 PPO 的区别、优势和劣势分别是什么？

| 维度 | PPO | DPO |
|---|---|---|
| 需要 Reward Model | ✅ 需要训 RM | ❌ 隐式 reward |
| 需要在线采样 | ✅ on-policy rollout | ❌ 离线数据集 |
| 训练成本 | 高（4 个模型：Actor/Critic/RM/Ref） | 低（2 个模型：Policy/Ref） |
| 效果上限 | 高（实时探索） | 中（受限于数据集质量） |
| 稳定性 | 差（reward hacking / KL 爆炸） | 好（loss 简单） |
| 适用 | 有明确 reward 信号的场景 | 有 preference 数据的场景 |

> **我项目选 DPO + GRPO 组合**：DPO 做风格对齐（便宜），GRPO 做结构化输出优化（有可验证 reward）。不用 PPO 因为数据量不够训 RM。

### Q20：GRPO 中 old policy、reference model、policy model 分别起什么作用？

| 模型 | 作用 | 更新频率 |
|---|---|---|
| **Policy Model（Actor）** | 当前正在训练的模型，生成 response | 每步更新 |
| **Old Policy** | 上一步的 policy，用于算 importance ratio | 每个 mini-batch 开始时快照 |
| **Reference Model** | SFT 后的初始模型，用于 KL penalty | 固定不动 |

> **关系**：
> - `ratio = π_policy(a|s) / π_old(a|s)` → PPO clip 用
> - `KL = log(π_policy / π_ref)` → 防止偏离 SFT 太远
> - GRPO 中 old policy 就是 rollout 时的 policy（因为 rollout 后才更新）

### Q21：GRPO 中 KL 或 reference penalty 的作用是什么？

> **防止 reward hacking**：模型可能找到"高 reward 但不自然"的输出模式（如重复某个关键词骗分）。
>
> KL penalty 约束模型不能偏离 reference（SFT 模型）太远：
> ```
> total_reward = task_reward - β * KL(π || π_ref)
> ```
> - β 太大：模型不敢探索，几乎不学习
> - β 太小：模型放飞自我，输出退化
> - 经验值：GRPO β=0.04（比 DPO 的 0.1 小，因为 GRPO reward 本身就稀疏）

### Q22：如果给一个真实 query，让你代码实现 GRPO 全流程，你会如何组织 rollout、reward、logprob、advantage、loss 和 update？

```python
# 伪代码：GRPO 单步更新
def grpo_step(prompts, policy, ref_model, reward_fn, G=8, beta=0.04):
    # 1. Rollout：每个 prompt 采样 G 个 response
    all_responses = []
    all_logprobs = []
    for prompt in prompts:
        responses = policy.generate(prompt, num_return=G, temperature=1.0)
        logprobs = policy.log_prob(prompt, responses)
        all_responses.append(responses)
        all_logprobs.append(logprobs)
    
    # 2. Reward：对每个 response 打分
    rewards = [[reward_fn(p, r) for r in resps] for p, resps in zip(prompts, all_responses)]
    
    # 3. Advantage：group 内归一化
    advantages = []
    for group_rewards in rewards:
        mean_r = mean(group_rewards)
        std_r = std(group_rewards) + 1e-8
        advantages.append([(r - mean_r) / std_r for r in group_rewards])
    
    # 4. KL penalty
    ref_logprobs = [[ref_model.log_prob(p, r) for r in resps] 
                    for p, resps in zip(prompts, all_responses)]
    kl = [[lp - rlp for lp, rlp in zip(lps, rlps)] 
          for lps, rlps in zip(all_logprobs, ref_logprobs)]
    
    # 5. Loss：PPO-clip style
    new_logprobs = policy.log_prob(prompts, all_responses)  # 当前 policy
    ratio = exp(new_logprobs - all_logprobs)  # importance ratio
    clipped_ratio = clip(ratio, 1-eps, 1+eps)
    loss = -min(ratio * advantages, clipped_ratio * advantages) + beta * kl
    
    # 6. Update
    loss.backward()
    optimizer.step()
```

---

## 三、大模型训练补充（图片第 1 部分）

### Q23：后训练中如何融合 math、code、中文、agent 等多个领域或方向的数据？

> **核心挑战**：不同领域数据的分布差异大，简单混合会导致"跷跷板效应"。
>
> **实践方案**：
> 1. **数据配比**：按能力权重混合，不是等比例
>    - 通用对话 40% + 代码 25% + 数学 15% + Agent 工具调用 15% + 安全 5%
> 2. **分阶段训练**：
>    - Phase 1：通用能力（对话 + 知识）
>    - Phase 2：专项能力（代码 + 数学 + Agent）
>    - Phase 3：对齐（安全 + 格式）
> 3. **动态权重调整**：监控各领域 eval 指标，哪个掉了加大该领域数据比例
> 4. **Replay buffer**：每阶段保留 20% 上阶段数据防遗忘

### Q24：多领域数据混训时，如何避免某个能力提升导致其他能力下降？

> **"灾难性遗忘"的变体**——不是忘了旧知识，而是新能力挤占了旧能力的表达空间。
>
> **解决方案**：
> 1. **EWC（Elastic Weight Consolidation）**：对重要权重加正则，限制变化幅度
> 2. **数据 Replay**：每轮训练必带 20% 旧数据（我项目就这么做的）
> 3. **LoRA 分模块**：不同能力用不同 LoRA adapter，推理时按需加载
> 4. **多任务 Loss 加权**：`total_loss = Σ w_i * loss_i`，动态调 w_i
> 5. **Checkpoint 回退**：某能力掉超过 2pp 就回退到上一个 checkpoint，调整配比重训
> 6. **评测驱动**：每 200 步跑全维度 eval，画雷达图看有没有"凹陷"

### Q25：SFT 冷启动和后续 RL 的关系是什么？为什么很多 RL 任务前需要 SFT 冷启动？

> **SFT 冷启动的作用**：
> 1. **格式对齐**：让模型学会"对话格式"（user/assistant 交替）、"工具调用格式"（JSON schema）
> 2. **能力激活**：base model 有潜在能力但不会主动使用，SFT 教它"什么时候该用"
> 3. **探索起点**：RL 需要模型能产生"有区分度"的 response，如果 base model 全输出垃圾，reward 信号为 0，RL 无法启动
>
> **类比**：SFT 是"教会走路"，RL 是"教会跑步"。不会走就直接跑 → 摔倒（训练崩溃）。
>
> **例外**：DeepSeek-R1-Zero 证明了**足够强的 base model + 足够好的 reward** 可以跳过 SFT，但这需要极大算力和精心设计的 reward。

### Q26：Base model 已经具备一定 Agentic 能力时，为什么还需要构造业务 Agentic 数据继续训练？

> **三个原因**：
> 1. **工具 schema 适配**：base model 见过通用工具调用，但没见过你的业务工具（如 `bcs_get_pod_status`），需要 SFT 教它认识新工具
> 2. **决策偏好**：通用模型可能倾向"先搜索再回答"，但运维场景需要"先查监控 → 再查日志 → 最后查配置"的特定顺序
> 3. **安全约束**：业务场景有特殊的安全规则（如"不能直接执行 scale-down"），需要通过训练内化
>
> **我项目的做法**：用 Qwen3-8B（已有 Agent 能力）+ 5k 条运维 QA SFT → citation 覆盖率从 60% 到 92%。

### Q27：业务基模和通用基模有什么区别？业务基模效果应该如何评估？

| 维度 | 通用基模 | 业务基模 |
|---|---|---|
| 训练数据 | 互联网通用语料 | 通用 + 领域数据 continue pretrain |
| 评估 | MMLU / HumanEval / GSM8K | **领域 benchmark + 业务指标** |
| 优势 | 泛化好 | 领域术语理解准、格式输出稳 |
| 劣势 | 领域知识浅 | 通用能力可能退化 |

> **业务基模评估方法**：
> 1. **领域 benchmark**：自建 100 条 golden set，按"事实/推理/拒答"分类
> 2. **A/B 对比**：同一 prompt 让通用模型和业务模型各答，LLM-Judge 打分
> 3. **业务指标**：修复成功率、MTTR、用户赞踩比
> 4. **回归测试**：通用能力不能掉超过 2pp（跑 MMLU 子集）

### Q28：MoE 和 Dense 模型在 RL 训练中有什么差异？MoE 做 RL 为什么可能更不稳定？

> **差异**：
> 1. **梯度分布不均**：MoE 中只有被激活的专家收到梯度，其他专家"冻结"
> 2. **负载均衡 loss 冲突**：RL loss 想让模型选"对的专家"，balance loss 想让所有专家均匀使用
> 3. **Router 不稳定**：RL 更新可能让 router 突然改变路由策略，导致训练震荡
>
> **不稳定原因**：
> - Dense 模型：所有参数同步更新，梯度方向一致
> - MoE 模型：不同 token 走不同专家，**梯度方向可能互相矛盾**
> - RL 的 reward 信号本身就有高方差，叠加 MoE 的路由随机性 → 方差²
>
> **缓解**：
> - 降低 RL 学习率（比 Dense 低 2-5x）
> - 增大 batch size 降低方差
> - 冻结 router，只训专家内部参数
> - 用 DPO 替代 PPO（DPO 更稳定）

---

## 四、Infra 补充（图片第 4 部分）

### Q29：VeRL / Ray 这类框架大致如何组织分布式 RL？

> **VeRL（字节 2024）架构**：
> ```
> ┌─────────────────────────────────────────────┐
> │              Controller (调度器)              │
> │  管理 rollout/training 的资源分配和同步       │
> └─────────┬───────────────┬───────────────────┘
>           ▼               ▼
> ┌─────────────────┐  ┌─────────────────────────┐
> │ Rollout Workers  │  │   Training Workers      │
> │ (vLLM 推理引擎)  │  │ (FSDP/Megatron 训练)    │
> │ 生成 response    │  │ 计算 loss + 更新参数     │
> └────────┬────────┘  └────────────┬────────────┘
>          │                        │
>          └──── Shared Storage ────┘
>                (轨迹数据 + 模型权重)
> ```
>
> **核心模块**：
> 1. **Actor**：生成 response（用 vLLM 加速推理）
> 2. **Critic**（PPO 才需要）：估计 value function
> 3. **Reward Model**：打分
> 4. **Reference Model**：计算 KL
> 5. **Trainer**：汇总 advantage + 更新参数
>
> **Ray 的角色**：提供分布式 Actor 调度、资源管理、故障恢复。VeRL 在 Ray 之上封装了 RL 特有的 rollout-train 循环。

### Q30：SGLang 和 vLLM 在推理服务中分别适合什么场景？

| 维度 | vLLM | SGLang |
|---|---|---|
| 核心优势 | PagedAttention + 生态成熟 | RadixAttention + 结构化生成 |
| Prefix cache | ✅ 支持 | ✅ **更强**（Radix Tree 自动共享任意前缀） |
| 结构化输出 | xgrammar（外挂） | **原生 FSM**，速度更快 |
| 多轮对话 | 好 | **更好**（自动复用历史 KV） |
| LoRA 多租户 | ✅ 成熟 | ⚠️ 支持但生态弱 |
| 投机解码 | ✅ EAGLE/Medusa | ⚠️ 有限支持 |
| 适用场景 | **通用生产部署** | **多轮对话 / Agent / 结构化输出** |

> **我项目选 vLLM**：生态成熟 + LoRA 多租户 + EAGLE-3 投机解码。如果未来 Agent 场景多轮对话 KV 复用成为瓶颈，会考虑切 SGLang。

### Q31：训推分离有什么收益？

> **收益**：
> 1. **硬件异构**：训练用 H100（算力强），推理用 A10/L4（便宜）
> 2. **弹性伸缩**：推理按 QPS 弹性扩缩，训练按 schedule 定时跑
> 3. **互不干扰**：训练 OOM 不影响线上推理
> 4. **版本管理**：训练产出 artifact → model registry → 推理拉取，有明确的版本边界
> 5. **成本优化**：训练用 spot instance（便宜 70%），推理用 reserved instance（稳定）

### Q32：fully async 训练会带来什么问题？

> **问题**：
> 1. **Stale gradient**：异步 worker 用旧参数算的梯度，应用到新参数上 → 方向可能错
> 2. **收敛不稳定**：不同 worker 的更新互相覆盖，loss 震荡
> 3. **一致性**：没有全局 barrier，不同 worker 看到的模型版本不同
>
> **缓解**：
> - **Bounded staleness**：允许最多落后 K 步，超过就等待
> - **Gradient compression**：减少通信量，让同步更快
> - **Local SGD**：每个 worker 本地更新 N 步，再同步一次

---

## 五、Agent 框架深水区（图片第 5 部分 + 字节面经图片）

### Q33：Agent 遇到上下文窗口溢出时怎么办？

> **分层策略**：
> 1. **预防层**：
>    - 工具返回结果限长（max 2000 token），超长自动摘要
>    - Session 自动总结（我项目：20 条 events / 4k token / 5min 静默 三档触发）
>    - 工具白名单切片，减少 schema 占用
> 2. **检测层**：
>    - 每次调 LLM 前估算 token 数（`src/cost/cost.go` 的 `EstimateTokens`）
>    - 超过 80% 窗口触发压缩
> 3. **处理层**：
>    - **滑动窗口**：只保留最近 N 轮对话
>    - **摘要替换**：旧对话压缩成 summary 放在 system prompt
>    - **优先级丢弃**：tool observation > old assistant > old user 的优先级保留

### Q34：工具返回结果很长时，应该裁剪、摘要、丢弃还是压缩？

| 策略 | 适用场景 | 优点 | 缺点 |
|---|---|---|---|
| **裁剪（truncate）** | 日志、堆栈 | 简单快速 | 可能丢关键信息 |
| **摘要（summarize）** | 长文档、API 响应 | 保留语义 | 需要额外 LLM 调用 |
| **丢弃** | 明确无用的 | 最省 token | 可能误判 |
| **压缩（结构化提取）** | JSON/表格 | 保留结构 | 需要定制逻辑 |

> **我项目的做法**：
> - BCS API 返回的 Pod 列表：**结构化提取**（只保留 name/status/restart_count）
> - 日志查询结果：**裁剪** top 50 行 + 尾部 10 行
> - 知识库检索结果：**摘要**（reranker 过滤后只保留 top-5 chunk）

### Q35：哪些上下文不能轻易压缩或删除？

> 1. **System prompt**：Agent 的"灵魂"，删了行为完全变
> 2. **用户最新消息**：当前任务的输入
> 3. **未完成的 tool_call 结果**：正在进行的推理链依赖这些事实
> 4. **HITL 审批状态**：删了会导致重复执行危险操作
> 5. **关键事实/结论**：之前推理得出的中间结论（如"根因是 OOM"）
>
> **可以压缩的**：
> - 早期的探索性工具调用（已被后续结论覆盖）
> - 重复的 observation（同一工具多次调用）
> - 格式化的寒暄/确认消息

### Q36：OpenCloud / Claude Code 类系统的上下文管理大概怎么做？

> **核心思路**：分层 + 按需加载 + 自动摘要
>
> ```
> ┌─────────────────────────────────────────┐
> │ Layer 0: System Prompt (固定，不压缩)    │
> ├─────────────────────────────────────────┤
> │ Layer 1: Project Context (按需加载)      │
> │   - 文件树摘要                           │
> │   - 最近编辑的文件                       │
> │   - 相关代码片段（语义检索）             │
> ├─────────────────────────────────────────┤
> │ Layer 2: Conversation History (滑动窗口) │
> │   - 最近 N 轮完整保留                    │
> │   - 更早的自动摘要                       │
> ├─────────────────────────────────────────┤
> │ Layer 3: Tool Results (按需/压缩)        │
> │   - 最新的完整保留                       │
> │   - 旧的只保留摘要                       │
> └─────────────────────────────────────────┘
> ```
>
> **关键技术**：
> - **Codebase indexing**：用 embedding 索引整个代码库，按 query 检索相关片段
> - **File diff tracking**：只把变更的部分放入上下文
> - **Conversation compaction**：超过阈值自动调 LLM 做摘要
> - **Tool result caching**：相同参数的工具调用结果缓存，不重复放入上下文

### Q37：MCP 和 Skill 分别是什么，功能定位有什么区别？

| 维度 | MCP (Model Context Protocol) | Skill |
|---|---|---|
| 本质 | **进程间通信协议** | **预定义的多步骤工作流** |
| 粒度 | 单个工具调用 | 多个工具 + 模板 + 后处理的组合 |
| 触发 | LLM 自主决策调用 | LLM 识别意图后整体触发 |
| 状态 | 无状态（每次调用独立） | 有状态（中间结果传递） |
| 示例 | `bcs_get_pods(cluster_id)` | `perf_report`（查指标→分析→生成报告） |
| 扩展性 | 任何语言实现 server | 通常和 Agent 同语言 |

> **我项目的 Skill 体系**（`skills/` 目录）：
> - `perf_report`：查 5 个性能指标 → 对比基线 → 生成 Markdown 报告
> - `log_pattern`：拉日志 → 正则提取 → 聚类 → 输出 top-5 模式
> - `csv_compare`：读两份 CSV → diff → 高亮变化
>
> **关系**：Skill 内部可能调用 MCP 工具，但 Skill 本身不是 MCP——它是更高层的抽象。

### Q38：Function Call 和 Agent 工具调用的关系是什么？

> ```
> Function Calling (模型能力)
>     ↓ 模型输出 tool_call JSON
> Agent Runtime (框架层)
>     ↓ 解析 + 路由 + 执行
> Tool Implementation (实现层)
>     ├── 本地函数 (function.NewFunctionTool)
>     ├── MCP Server (远程 JSON-RPC)
>     └── Skill (多步工作流)
> ```
>
> - **Function Calling** 是模型的能力：模型学会了在适当时候输出结构化的工具调用请求
> - **Agent 工具调用** 是框架的能力：接收模型的请求，找到对应工具，执行，返回结果
> - 两者是**上下游关系**：FC 是"模型说要调什么"，Agent 是"真正去调"

---

## 六、Agent 工程实战（字节面经图片 · 85df7be6）

### Q39：当一个用户请求进来，模型决策到工具调用，这个链路如何设计？

> ```mermaid
> sequenceDiagram
>     participant U as 用户
>     participant GW as API Gateway
>     participant R as Runner
>     participant C as Coordinator
>     participant S as SubAgent
>     participant T as Tool
>     participant LLM as Model
> 
>     U->>GW: HTTP/SSE 请求
>     GW->>R: runner.Run(ctx, userID, sessionID, msg)
>     R->>R: 加载 Session + 构建 Invocation
>     R->>C: coordinator.Run(invocation)
>     C->>LLM: 意图识别 + transfer 决策
>     LLM-->>C: transfer_to_diagnosis
>     C->>S: diagnosis.Run(invocation)
>     S->>LLM: ReAct 推理（带工具列表）
>     LLM-->>S: tool_call: bcs_get_pods
>     S->>T: tool.Call(ctx, args)
>     T-->>S: observation
>     S->>LLM: 带 observation 继续推理
>     LLM-->>S: final_answer
>     S-->>R: events 流
>     R-->>U: SSE 推送
> ```

### Q40：用户确认中断后，resume 从 checkpoint 恢复续跑，从触发中断到掉 resume 这个过程期间 checkpoint 状态如何持久化？

> **我项目的 HITL 实现**：
> 1. **中断时**：
>    - safety_guard 在 pre-tool callback 拦截写操作
>    - 生成 `confirmation_required` 事件，包含 `{invocation_id, step_id, plan, tool_name, args}`
>    - 事件写入 Session（Redis），**这就是 checkpoint**
>    - SSE 推送给前端，等待用户响应
> 2. **等待期间**：
>    - Session 在 Redis 中持久化，进程重启不丢
>    - 前端保持 SSE 连接（断了可重连，通过 `Last-Event-ID` 恢复）
> 3. **Resume 时**：
>    - 用户发送 approve/reject
>    - Runner 从 Session 读取 checkpoint 状态
>    - 如果 approve：带 `confirmed=true` 重新调用同一工具
>    - 如果 reject：注入"用户拒绝了该操作"消息，LLM 重新规划

### Q41：Agent 跑到一半，用户发来新消息，如何处理并发？

> **三种策略**（按场景选）：
> 1. **排队（Queue）**：新消息等当前轮完成后再处理
>    - 适用：大多数场景，保证一致性
>    - 我项目默认用这个
> 2. **中断（Cancel + Restart）**：取消当前执行，用新消息重新开始
>    - 适用：用户明确改变意图（如"算了，帮我查另一个"）
>    - 通过 `context.Cancel()` 实现
> 3. **并行（Parallel）**：两个请求独立执行
>    - 适用：无状态查询
>    - 需要 Session 锁防止写冲突
>
> **实现**：Runner 层用 `sync.Mutex` 保证同一 session 同时只有一个 Run 在执行。新请求来了要么等锁、要么 cancel 旧的。

### Q42：LLM 推理时外层的 time guard 如何设计的（主流程和泄漏 goroutine 解耦）？

> **两层超时 + goroutine 生命周期管理**：
>
> ```go
> func (r *Runner) Run(ctx context.Context, ...) {
>     // 第一层：全局超时
>     ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
>     defer cancel()
>     
>     // 第二层：单次 LLM 请求超时
>     // 在 model.Generate 内部用 WithRequestTimeout(30s)
>     
>     // goroutine 泄漏防护
>     done := make(chan struct{})
>     go func() {
>         defer close(done)
>         // 实际执行 Agent 逻辑
>         agent.Run(ctx, invocation)
>     }()
>     
>     select {
>     case <-done:
>         // 正常完成
>     case <-ctx.Done():
>         // 超时，但不能直接 kill goroutine
>         // 通过 ctx 传播取消信号，Agent 内部检查 ctx.Err()
>         // 等待 goroutine 自行退出（最多再等 5s）
>         select {
>         case <-done:
>         case <-time.After(5 * time.Second):
>             log.Warn("goroutine leaked, force abandon")
>         }
>     }
> }
> ```
>
> **关键设计**：
> - 主流程通过 `ctx.Done()` 感知超时，不阻塞
> - Agent 内部每个循环检查 `ctx.Err()`，及时退出
> - 即使 goroutine 泄漏（如 HTTP 连接卡住），主流程也能返回

### Q43：Agent 在跑的时候，用户发一个取消信号，这个信号广播到 agent 所在节点，如果这个节点刚好重启，subscriber 还没起来，那怎么处理？

> **问题本质**：取消信号是瞬时的，如果接收方不在线就丢了。
>
> **解决方案**：
> 1. **持久化取消状态**：取消信号不只是 pub/sub，还要写入 Redis/DB
>    ```
>    SET cancel:{session_id}:{invocation_id} = "cancelled" EX 300
>    ```
> 2. **启动时检查**：subscriber 重启后，先检查是否有未处理的取消信号
>    ```go
>    func (r *Runner) Resume(sessionID string) {
>        if isCancelled(sessionID) {
>            // 不恢复执行，直接标记为 cancelled
>            return
>        }
>        // 正常恢复
>    }
>    ```
> 3. **幂等取消**：取消操作可以重复发送，接收方幂等处理
> 4. **超时兜底**：即使取消信号丢了，全局 timeout 也会最终终止执行

### Q44：Agent 卡在一个长时间推理，期间没有 tool call 没有 yield 点，用户点了取消要跑完才能感知到，这个场景怎么做的？

> **问题**：LLM 推理是一个阻塞的 HTTP 调用，中间没有检查点。
>
> **解决方案**：
> 1. **流式推理 + 逐 chunk 检查**：
>    ```go
>    for chunk := range stream {
>        select {
>        case <-ctx.Done():
>            // 用户取消了，立即停止消费 stream
>            stream.Close()  // 关闭 HTTP 连接
>            return ctx.Err()
>        default:
>            processChunk(chunk)
>        }
>    }
>    ```
> 2. **HTTP 连接级取消**：`ctx` 传递到 HTTP client，`ctx.Done()` 触发时底层 TCP 连接被关闭，服务端感知到断开停止生成
> 3. **非流式场景**：用 `http.Request.WithContext(ctx)`，超时/取消时 Go 的 net/http 会自动关闭连接
>
> **关键**：必须用**流式推理**（`stream=true`），否则要等整个 response 生成完才能感知取消。我项目所有 LLM 调用都是流式的。

### Q45：对 Agent 可观测性的理解？

> **三个层次**：
>
> | 层次 | 看什么 | 工具 | 我项目实现 |
> |---|---|---|---|
> | **Traces** | 单次请求的完整链路 | OTel + Langfuse | `src/observability/genai_span.go` |
> | **Metrics** | 聚合指标趋势 | Prometheus + Grafana | `src/observability/metrics_more.go` |
> | **Logs** | 详细执行日志 | 结构化日志 | tRPC 日志框架 |
>
> **Agent 特有的观测维度**：
> 1. **决策链路**：哪个 Agent 做了什么决策、为什么 transfer
> 2. **工具调用**：调了什么工具、参数是什么、耗时多少、成功/失败
> 3. **Token 消耗**：每轮用了多少 token、成本多少
> 4. **质量指标**：工具选择准确率、用户满意度、HITL 通过率
> 5. **异常检测**：死循环、重复调用、超时、幻觉
>
> **GenAI Semantic Convention v1.30**（我项目遵循的标准）：
> - `gen_ai.system`：模型提供商
> - `gen_ai.request.model`：模型名
> - `gen_ai.usage.input_tokens` / `output_tokens`：token 用量
> - `gen_ai.response.finish_reasons`：结束原因

---

## 七、多 Agent 协作深水区（markdown 面经补充）

### Q46：如何防止 multi-agent 互相 A2A 停不下来？

> **5 层防护**（我项目全部实现）：
>
> | 层 | 措施 | 代码 |
> |---|---|---|
> | 1. 框架层 | `WithEndInvocationAfterTransfer(true)` | Agent 配置 |
> | 2. 拓扑层 | transfer 严格单向（Coordinator → Sub），子 Agent 不互相 transfer | 架构设计 |
> | 3. 深度限制 | `max_transfer_depth=3`，超过直接返回 | Coordinator prompt |
> | 4. Prompt 层 | system prompt 明令"单轮最多一次 transfer" | 各 Agent prompt |
> | 5. 监控层 | `agent.transfer.depth` metric，超过阈值告警 | Prometheus rules |
>
> **根本原因**：A2A 死循环通常是因为两个 Agent 互相认为"这个问题应该交给对方"。解决方案是**明确职责边界 + 单向拓扑**。

### Q47：主子模式下子 Agent 产生幻觉怎么办？

> **三层兜底**：
> 1. **工具事实约束**：子 Agent 必须基于工具返回的事实回答，prompt 里写"不要编造工具没返回的信息"
> 2. **Output Guard**：`src/plugin/output_guard.go` 检查输出是否包含未在 context 中出现的"事实性声明"
> 3. **LLM Judge 评测**：`EvidenceSufficiency` 维度评估"每个事实是否有 citation 支撑"
>
> **具体措施**：
> - ReAct prompt 强制要求"引用工具返回的原文"
> - 如果模型输出包含数字/ID/时间但 context 里没有 → 标记为可疑
> - HITL 写操作前，Plan 里的每个字段都必须能追溯到某个 tool observation

### Q48：State 管理和 Checkpoint 机制怎么实现的？多个 agent 同时跑的时候，状态竞争怎么避免？

> **State 管理**：
> - Session 的 `State map[string]any` 存储全局状态
> - 每个 Agent 通过 `invocation.Session.State` 读写
> - 状态变更通过 events 追踪（append-only）
>
> **Checkpoint**：
> - 每个 event 写入 Session 就是一个隐式 checkpoint
> - HITL 中断时显式保存 `{step_id, tool_name, args, plan}`
> - 恢复时从最后一个 checkpoint 继续
>
> **状态竞争避免**：
> 1. **串行执行**：同一 Session 同时只有一个 Agent 在跑（Runner 层 Mutex）
> 2. **Transfer 语义**：Coordinator transfer 给子 Agent 后自己结束，不并行
> 3. **State 写入原子性**：Redis 用 MULTI/EXEC 保证多字段原子更新
> 4. **乐观锁**：State 带 version 字段，CAS 更新失败则重试

### Q49：子任务如果失败了，比如数据抓回来是空的，工作流怎么重试或降级？

> **三级策略**：
> 1. **自动重试**：`pkg/resilience/retry.go` 指数退避重试 3 次
> 2. **降级**：
>    - API 不可用 → 用缓存数据（标记为"非实时"）
>    - 主模型不可用 → 切备用模型
>    - MCP server 不可用 → 跳过该工具，告知 LLM "该信息暂时无法获取"
> 3. **LLM 自主决策**：ReAct prompt 里写"工具返回空/失败时，可以换工具或直接告诉用户当前无法获取"
>
> **我项目的实际做法**：
> ```go
> result, err := tool.Call(ctx, args)
> if err != nil || result == nil {
>     // 不直接报错，而是把失败信息作为 observation 喂给 LLM
>     observation = fmt.Sprintf("工具 %s 调用失败: %v，请换个思路", toolName, err)
>     // LLM 看到这个 observation 后会自主决定下一步
> }
> ```

### Q50：一次会话的 token 大概有多少？有没有超限？

> **我项目实测数据**：
> - 简单查询（1 轮工具调用）：~2k token
> - 中等排障（3-5 轮 ReAct）：~8k token
> - 复杂修复（含 HITL）：~15k token
> - 极端 case（长日志 + 多工具）：~25k token
>
> **超限处理**：
> - 模型窗口 32k，预留 4k 给输出 → 输入上限 28k
> - `src/cost/cost.go` 的 `EstimateTokens` 每次调 LLM 前估算
> - 超过 80%（22k）触发 Session 自动总结
> - 工具返回结果限长 2000 token，超长自动截断

---

## 八、Self-Attention 原理（markdown 面经 Q17）

### Q51：简单讲一下 Self-Attention 的实现原理，为什么要分成 Q、K、V 三个向量？

> **实现原理**：
> ```
> Attention(Q, K, V) = softmax(QK^T / √d_k) · V
> ```
> 1. 输入 X 通过三个线性变换得到 Q、K、V：`Q = XW_Q, K = XW_K, V = XW_V`
> 2. Q 和 K 做点积得到注意力分数：`scores = QK^T / √d_k`
> 3. softmax 归一化得到注意力权重：`weights = softmax(scores)`
> 4. 用权重加权 V 得到输出：`output = weights · V`
>
> **为什么分 Q、K、V？**
> - **Q（Query）**：当前 token "想要找什么信息"
> - **K（Key）**：每个 token "能提供什么信息"的标签
> - **V（Value）**：每个 token "实际携带的信息内容"
>
> **类比**：图书馆检索
> - Q = 你的搜索关键词
> - K = 每本书的索引标签
> - V = 书的实际内容
> - Q·K = 计算你的搜索和每本书的相关度
> - softmax(Q·K)·V = 按相关度加权读取内容
>
> **为什么不能只用一个矩阵？**
> - 如果 Q=K=V=X，那 `softmax(XX^T)X` 只能做"自相似加权平均"
> - 分开后，模型可以学到**不对称的关系**：token A 关注 token B，不代表 B 也关注 A
> - Q 和 K 的空间可以不同于 V 的空间，**检索和内容解耦**

### Q52：同一个 token 在不同位置的向量是一样的吗？

> **不一样**。原因：
> 1. **位置编码**：RoPE 给每个位置的 Q/K 施加不同的旋转，同一个词在位置 0 和位置 100 的 Q/K 向量不同
> 2. **上下文依赖**：经过多层 attention 后，每个 token 的表示融合了上下文信息
> 3. **因果 mask**：位置 5 的 token 只能看到位置 0-5，位置 100 的同一 token 能看到 0-100，信息量不同
>
> **只有 embedding 层**（最底层）同一个 token 的初始向量是一样的，加上位置编码后就不同了。

---

## 九、Go 语言八股补充（markdown 面经 Q16 + 图片）

### Q53：讲一讲 GMP（Goroutine-Machine-Processor）模型

> **三个核心概念**：
> - **G（Goroutine）**：用户态协程，初始栈 2KB，可动态增长
> - **M（Machine）**：OS 线程，真正执行代码的载体
> - **P（Processor）**：逻辑处理器，持有本地运行队列，数量 = GOMAXPROCS
>
> **调度流程**：
> ```
> G 创建 → 放入 P 的本地队列 → P 绑定 M → M 从 P 的队列取 G 执行
>                                              ↓ 队列空
>                                         从全局队列偷 / 从其他 P 偷（work stealing）
> ```
>
> **关键机制**：
> 1. **Work Stealing**：P 本地队列空时，从其他 P 偷一半 G
> 2. **Handoff**：G 阻塞在 syscall 时，M 和 P 解绑，P 找新 M 继续跑其他 G
> 3. **Preemption**：Go 1.14+ 基于信号的抢占，防止 G 长时间占用 M
> 4. **Netpoller**：网络 I/O 不阻塞 M，G 挂到 netpoller 等待事件

### Q54：Python 有没有真正的多线程？为什么要有 GIL？

> **没有真正的并行多线程**（CPython）。GIL（Global Interpreter Lock）保证同一时刻只有一个线程执行 Python 字节码。
>
> **为什么要 GIL**：
> 1. **引用计数安全**：CPython 用引用计数做 GC，多线程并发修改 refcount 会 race condition
> 2. **C 扩展兼容**：大量 C 扩展假设单线程环境，去掉 GIL 会破坏兼容性
> 3. **历史包袱**：Python 1.x 时代设计的，现在改动成本极高
>
> **绕过方案**：
> - CPU-bound：`multiprocessing`（多进程）/ `concurrent.futures.ProcessPoolExecutor`
> - I/O-bound：`asyncio` / `threading`（GIL 在 I/O 等待时释放）
> - 计算密集：NumPy/PyTorch（C/CUDA 层释放 GIL）
>
> **Python 3.13**：实验性 free-threaded build（PEP 703），去掉 GIL，但性能和兼容性还在验证中。

### Q55：多线程中的 Lock 和 RLock 有什么区别？

| 维度 | Lock（Mutex） | RLock（Reentrant Lock） |
|---|---|---|
| 同一线程重复加锁 | ❌ 死锁 | ✅ 可以，内部计数 |
| 释放要求 | 加一次释放一次 | 加 N 次释放 N 次 |
| 性能 | 略快 | 略慢（维护 owner + count） |
| 适用 | 简单互斥 | 递归调用场景 |

> **Go 的对应**：
> - `sync.Mutex` = Lock（非递归，同一 goroutine 重复 Lock 会死锁）
> - Go **没有内置 RLock**，需要自己实现或用 `sync.RWMutex`（读写锁，不是递归锁）

---

## 十、产品/架构决策类（markdown 面经二面）

### Q56：说说你觉得最突出的两个架构设计，why？

> **设计 1：TargetedTool 工具白名单**
> - Why：所有工具都暴露给 LLM → schema 太长（prefix-cache 命中率掉）+ 越权风险
> - How：每个工具声明 `Targets`，按 Agent 切片，物理隔离
> - 效果：schema 从 40 个工具缩到 10 个，prefix-cache 命中率 +20pp，且杜绝越权
>
> **设计 2：HITL 两段式确认**
> - Why：写操作（Helm 回滚/MR merge）一旦 LLM 误判，爆炸半径大
> - How：写工具第一次调用只返回 Plan，SSE 推给前端等审批，approve 后才真执行
> - 效果：上线 3 个月零误操作事故，用户信任度从"不敢用"到"日常依赖"

### Q57：Memory 为什么这样决策？和 RAG 那些的区别？

| 维度 | Memory | RAG | Session |
|---|---|---|---|
| 存什么 | 用户偏好/习惯/事实 | 领域知识文档 | 当前对话历史 |
| 生命周期 | 永久（跨 session） | 永久（知识库更新） | 单次会话 |
| 检索方式 | 关键词/向量 | 向量+重排 | 按时间序 |
| 写入时机 | LLM 主动调 memory_save | 离线索引 | 每轮自动追加 |
| 示例 | "用户习惯查 letsgo 集群" | "OOM 排查 Runbook" | "刚才查了 Pod 状态" |

> **我的决策**：Memory 用 Agentic 模式（LLM 显式调工具存取），不用 Auto 模式。原因：运维场景的"记忆"是明确的（"这个用户负责 X 集群"），不需要模型自己猜该记什么。

### Q58：A2A 为什么不用谷歌的那个协议自己写一套设施？

> **实际上我用的就是 A2A 协议**（`trpc-a2a-go v0.2.5`），不是自己写的。
>
> 但做了**适配层**：
> 1. **build tag 双实现**：`+build a2a_real` 走真实 A2A，`+build a2a_stub` 走 mock（CI 用）
> 2. **状态透传**：通过 `WithTransferStateKey` 把业务上下文传给远端 Agent
> 3. **Agent Card 定制**：`/.well-known/agent.json` 里只暴露必要的 capabilities
>
> **为什么不完全自研**：
> - A2A 协议已经标准化（Google 2024），自研没有生态优势
> - 框架已经封装好了，我只需要配置
> - 但如果需要**跨公司/跨框架**互通，标准协议是唯一选择

### Q59：Shared-state、Agent team、主子结构和你这个到底有什么区别？

| 模式 | 通信方式 | 状态管理 | 适用场景 | 我项目 |
|---|---|---|---|---|
| **Shared-state** | 共享黑板/数据库 | 全局可见 | 松耦合、异步协作 | ❌ |
| **Agent team** | 平等协商 | 各自维护 | 创意任务、头脑风暴 | ❌ |
| **主子结构** | 主 Agent 分发 + 收集 | 主 Agent 汇总 | 明确分工、可控 | ✅ |
| **Workflow/DAG** | 固定流程编排 | 按节点传递 | 确定性流程 | ❌ |

> **我选主子结构的原因**：
> 1. 运维场景职责明确（排障/修复/知识/文件分析），不需要"协商"
> 2. 主 Agent（Coordinator）做路由决策，可观测性好
> 3. 子 Agent 之间不直接通信，避免复杂度爆炸
> 4. 通过 Session 共享事实（Diagnosis 查到的告警，Repair 能看到），不需要额外的 shared-state 机制

---

## 十一、额外高频补充题

### Q60：如何看待 Manus/OpenClaw 里的技术落地？有没有真的去看源码？

> **Manus 核心技术**：
> 1. **Computer Use**：通过截图 + 坐标点击操作桌面环境
> 2. **沙箱隔离**：每个任务一个 Docker 容器，安全执行
> 3. **多模态 Agent**：视觉理解 + 文本推理 + 工具调用
>
> **和我项目的关系**：
> - 我项目是**API-first**（通过 MCP 调 API），Manus 是**GUI-first**（通过截图操作界面）
> - 两者互补：API 不可用时可以 fallback 到 GUI 操作
> - 我项目的 HITL 机制可以直接复用到 Computer Use 场景（高危操作前确认）

### Q61：如果我负责豆包，用户习惯性只在一个 session 使用，如何保证上下文的连贯？用户可能不是上一秒问游戏下一秒问经济，而是问了原神然后问崩铁，这样的消息要如何处理？

> **核心挑战**：同一 session 内话题切换，需要区分"延续"和"转换"。
>
> **方案**：
> 1. **话题检测**：每轮用轻量分类器判断是否切换话题
>    - 相似度 < 0.3 → 新话题
>    - 相似度 > 0.7 → 延续
>    - 中间 → 模糊，保留上下文但降低权重
> 2. **分段摘要**：不同话题的历史分别摘要，标记话题标签
> 3. **选择性注入**：新消息来时，只注入**相关话题**的历史摘要
> 4. **Memory 持久化**：跨话题的用户偏好存 Memory（如"用户喜欢原神的雷神"）

### Q62：生成图片和视频可能需要长时间，有什么技术/方案能够减少用户的等待？

> **技术方案**：
> 1. **异步任务 + 进度推送**：提交后立即返回 task_id，通过 SSE/WebSocket 推送进度
> 2. **渐进式生成**：先出低分辨率预览，再逐步增强（Progressive JPEG 思路）
> 3. **预生成 + 缓存**：热门 prompt 预生成，命中直接返回
> 4. **队列优先级**：VIP 用户优先、短任务优先
> 5. **分布式推理**：多 GPU 并行生成，缩短单任务耗时
> 6. **用户体验优化**：等待时展示"正在创作中"动画 + 预估时间 + 中间产物预览

---

## 📌 覆盖检查清单

| 来源 | 题目 | 本文覆盖 | 其他文档覆盖 |
|---|---|---|---|
| 图片1-大模型训练 | 训练阶段/RL vs SFT | Q23-Q28 | project-llm INTERVIEW |
| 图片1-大模型训练 | MoE做RL不稳定 | Q28 | ❌ 之前缺失 |
| 图片2-RL算法 | DAPO/GRPO/DPO对比 | Q15-Q22 | project-llm 部分覆盖 |
| 图片3-Agentic RL | 完整训练流程 | Q1-Q14 | ❌ 之前完全缺失 |
| 图片4-Infra | VeRL/SGLang/训推分离 | Q29-Q32 | project-llm 部分覆盖 |
| 图片5-Agent框架 | 上下文管理/MCP vs Skill | Q33-Q38 | framework_internals 部分 |
| 字节面经图片 | 链路设计/checkpoint/取消 | Q39-Q45 | ❌ 之前缺失 |
| markdown面经 | 多Agent/State/幻觉 | Q46-Q50 | project-agent 部分覆盖 |
| markdown面经 | Self-Attention/GMP | Q51-Q55 | ❌ 之前缺失 |
| markdown面经二面 | 架构决策/Memory/A2A | Q56-Q59 | framework_vs_self 部分 |
| markdown补充 | 豆包/Manus/长等待 | Q60-Q62 | ❌ 之前缺失 |

---

## ✅ 使用建议

1. **Agentic RL 被问到**：从 Q1 开始，讲完整流程 → token mask → 停止条件 → credit assignment
2. **DAPO 被问到**：Q15-Q18，重点讲动态采样和零奖励过滤
3. **Agent 工程被问到**：Q39-Q44，重点讲 time guard 和取消信号处理
4. **架构决策被问到**：Q56-Q59，重点讲 TargetedTool 和 HITL
5. **八股被问到**：Q51-Q55，Self-Attention 和 GMP 必背
