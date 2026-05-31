# data/ 数据目录说明

本目录存放训练/评估数据，具体数据文件**不进仓库**（见 `.gitignore`）。

## 📁 子目录用途

| 目录 | 用途 | 示例文件 |
|------|------|---------|
| `raw/wiki_docs/` | 原始 Wiki / 运维手册 Markdown 文档 | `routesvr.md` / `oncall_sop.md` |
| `raw/game_content/` | 游戏剧情 / 对话 / 世界观设定 | `world_setting.md` / `story_chapter_1.md` |
| `raw/npc_profiles/` | NPC 角色卡（JSON） | `blacksmith_zhang.json` / `alchemist_yue.json` |
| `processed/` | 经脚本处理后的训练数据 | `knowledge_qa.json` / `npc_dialogues.json` / `npc_dpo.json` / `npc_grpo_prompts.json` |
| `test/` | 独立评估集（G-Eval / RAGAS gold set） | `knowledge_test.json` / `npc_test.json` |

## 📝 数据集注册

所有 LLaMAFactory 训练用到的数据集都要在 `dataset_info.json` 中注册。
新增数据集步骤：
1. 在 `processed/` 下生成 JSON 文件
2. 在 `dataset_info.json` 中添加一条记录（指定 file_name / formatting / columns）
3. 在 `configs/*.yaml` 的 `dataset:` 字段引用数据集名

## 🔄 数据生成流程

```
raw/wiki_docs/           ──►  scripts/generate_qa.py      ──►  processed/knowledge_qa.json
raw/npc_profiles/        ──►  scripts/generate_npc_data.py ──►  processed/npc_dialogues.json
processed/npc_dialogues  ──►  scripts/generate_dpo_data.py ──►  processed/npc_dpo.json
raw/game_content/        ──►  scripts/generate_grpo_prompts.py ──► processed/npc_grpo_prompts.json
```

## ⚠️ 隐私与合规

- 原始运维文档 / 用户对话数据**必须**经过 PII 脱敏（见 `scripts/data_quality.py` 中的 presidio 流程）
- 合成数据涉及外部 API（DeepSeek / Kimi / GPT），调用前请确认数据可用于外发
