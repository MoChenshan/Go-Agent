# Dataset Card — project-llm 训练数据

> 参考 HuggingFace Dataset Card / Datasheet for Datasets。

## 1. 数据集组成

| Split | 样本数 | 用途 | 文件 |
|---|---|---|---|
| sft_npc_train | 18,000 | NPC 对话 SFT | `data/processed/npc_sft.jsonl` |
| sft_ops_train | 10,000 | 运维 QA SFT | `data/processed/ops_sft.jsonl` |
| dpo_npc | 4,000 (pairs) | NPC DPO 对齐 | `data/processed/npc_dpo.jsonl` |
| grpo_npc_prompts | 1,200 | GRPO 强化学习 | `data/processed/npc_grpo_prompts.json` |
| eval_golden | 50 | 评测金标 | `eval/golden_50.jsonl` |
| eval_red_team | 200 | 红队/越狱评测 | `eval/red_team.jsonl` |

## 2. 数据来源

| 来源 | 说明 | 数量 | 许可 |
|---|---|---|---|
| **合成数据（DeepSeek-V3 教师模型）** | NPC 对话 + 运维 QA Self-Instruct | 18k | 自有 |
| **公开 SFT 子集**（OpenHermes-2.5-zh / Wizard-LM-zh） | 通用对话保底 | 10k | Apache-2.0 / Llama-2 |
| **iWiki 历史告警工单（脱敏）** | 运维 QA 真实问题 | 1.5k | 内部授权 |
| **DeepSeek 偏好对** | DPO 训练对 | 4k | 自有合成 |

## 3. 预处理流程

```
原始 → 去重(simhash) → PII 过滤(Presidio + 自定义正则)
     → 内容审核(OpenAI moderation + 中文敏感词)
     → 与基座预训练交叉去重(simhash 阈值 0.85)
     → 长度过滤(< 4k tokens)
     → 格式标准化(ChatML)
```

完整流程见 `scripts/data_pipeline.py`。

## 4. 数据 schema

```json
{
  "id": "npc_001234",
  "messages": [
    {"role": "system", "content": "你是名为'晨曦'的精灵游侠..."},
    {"role": "user",   "content": "你最厉害的武器是什么？"},
    {"role": "assistant", "content": "我的最爱是这把..."}
  ],
  "meta": {
    "source": "synthetic_v2",
    "domain": "npc",
    "char_id": "morning_elf_002",
    "quality_score": 0.92,
    "verified_by": "human|llm_judge",
    "license": "Apache-2.0"
  }
}
```

DPO 格式额外多 `chosen` / `rejected` 字段。

## 5. 数据质量

| 指标 | 值 |
|---|---|
| 平均 token 长度 | 487 |
| 中文占比 | 96.3% |
| LLM Judge 平均分（5 分） | 4.21 |
| 人工抽样合格率（500 样本） | 92.4% |
| simhash 重复率 | 0.7%（已去重后残留） |

## 6. 已知偏差与限制

- 角色覆盖偏中世纪/玄幻 NPC（武侠类样本相对较少）
- 运维 QA 来源以 BCS / 蓝鲸 / TAPD 为主，其他云厂商场景覆盖弱
- 合成数据带教师模型的"老好人"倾向（已通过 DPO 偏好对部分缓解）

## 7. 隐私与合规

- 已对所有内部告警/工单做完整脱敏（IP/邮箱/工号/集群名替换为 placeholder）
- 不包含任何用户聊天记录
- 不包含 PII：邮箱 / 手机 / 身份证 / 信用卡 / 内网 IP
- 已做内部数据安全评审（DSEC-2026-Q1 通过）

## 8. 维护与更新

- 数据飞轮：线上 RAG faithfulness < 0.85 的真实 case，每周回流，经人工 review 后纳入下一轮 SFT
- replay 机制：每次新增数据时按 8:2 混入历史，防灾难性遗忘
- 详见 `scripts/data_replay_buffer.py`

## 9. 引用与许可

合成数据 + 衍生处理：Apache-2.0；公开数据子集请遵循其原始许可。
