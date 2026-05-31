---
name: find-skills
description: 在 SkillHub 和 Knot 上检索并安装 skills 到 trpc-claw 的 local skills 目录。适用于“找个 xxx skill”“从市场装一个 skill”“替换当前本地同名 skill”这类请求。
metadata:
  {
    "openclaw":
      {
        "requires": { "bins": ["python3"] }
      }
  }
---

# Find Skills

这个 skill 做两件事：

1. 在多个技能市场里搜索候选 skill
2. 把选中的 skill 安装到本地 `local` skills 目录

设计原则：

- 远程市场只负责“找候选”
- 本地运行时只保留一个同名 skill
- 默认安装到 `local/<skill_name>`
- 如果本地已经有同名 skill，默认直接覆盖

## 当前支持的市场

- `skillhub`
- `knot`

说明：

- `SkillHub` 默认总是可用
- `Knot` 需要环境变量：
  - `KNOT_USERNAME`
  - `KNOT_API_TOKEN` 或 `KNOT_JWT_TOKEN`
- 如果 Knot 缺少鉴权环境，搜索时会自动跳过，并在输出里说明原因

## 运行约定

如果需要直接查看本 skill 自带的脚本或引用文件，
先从 `find-skills` 自己的 skill 根目录读取。

例如：

```bash
python3 scripts/search_skills.py --query "pdf"
python3 scripts/install_skill.py --provider skillhub --remote-id pdf
```

不要再额外拼 `skills/find-skills/...`。

## 搜索 skills

按关键词搜索：

```bash
python3 scripts/search_skills.py --query "pdf"
python3 scripts/search_skills.py --query "excel" --limit 10
```

不带关键词时，会优先列出 SkillHub 热门 skills：

```bash
python3 scripts/search_skills.py --limit 10
```

脚本会：

- 按 provider 优先级搜索
- 输出易读表格
- 在 `=== JSON_OUTPUT_START ===` / `=== JSON_OUTPUT_END ===`
  之间输出结构化 JSON，方便后续安装

## 安装 skills

从 SkillHub 安装：

```bash
python3 scripts/install_skill.py \
  --provider skillhub \
  --remote-id pdf
```

从 Knot 安装：

```bash
python3 scripts/install_skill.py \
  --provider knot \
  --remote-id 16
```

默认行为：

- 安装目录：`${TRPC_CLAW_STATE_DIR}/skills/local/<skill_name>`
- 如果 `TRPC_CLAW_STATE_DIR` 不存在，则回退到：
  `~/.trpc-agent-go/openclaw/skills/local/<skill_name>`
- 如果本地已存在同名 skill，默认直接覆盖
- 如果当前机器上正跑着同一个 `state_dir` 的 OpenClaw admin，
  安装脚本会在落盘后自动触发一次 live refresh，
  这样新 skill 下一轮就能直接被发现

显式保留已有同名 skill：

```bash
python3 scripts/install_skill.py \
  --provider skillhub \
  --remote-id pdf \
  --keep-existing
```

显式指定安装根目录：

```bash
python3 scripts/install_skill.py \
  --provider skillhub \
  --remote-id pdf \
  --install-root /path/to/skills/local
```

## 安装后的来源记录

每个安装好的本地 skill 目录里都会写一个：

```text
_registry.json
```

它只记录：

- 来源 provider
- 远程 ID
- 版本
- 安装时间

这样以后可以知道当前本地 skill 是从哪里来的。

## 建议工作流

1. 先搜索候选 skill
2. 比较 `provider / name / description / version`
3. 选择一个最合适的候选
4. 安装到本地 `local` skills
5. 默认情况下安装脚本会自动 refresh live repo；
   下一轮让 trpc-claw 直接像普通本地 skill 一样使用它
