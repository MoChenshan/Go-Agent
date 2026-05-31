# OpenClaw Skills

本目录同步默认 bundled skills。
发布包会把这里的内容打进产物，
安装后默认落到：

- `~/.trpc-agent-go/openclaw/skills/bundled/`

安装脚本也会额外创建一个本地自定义目录：

- `~/.trpc-agent-go/openclaw/skills/local/`

默认 profile 已经开启：

```yaml
skills:
  root: "${TRPC_CLAW_STATE_DIR}/skills/bundled"
  extra_dirs:
    - "${TRPC_CLAW_STATE_DIR}/skills/local"
    - "${HOME}/.codex/skills"
    - "./.agents/skills"
```

建议：

- `bundled/` 里现在包含两组技能：
  - OpenClaw 默认 skills
  - Anthropic 官方 skills 快照
- Anthropic 官方 skills 统一改成 `anthropic-*` 命名，
  例如 `anthropic-docx`、`anthropic-pdf`、
  `anthropic-webapp-testing`。
- 这样做的目的：
  - 避免和已有 OpenClaw skill / 用户 skill 撞名
  - 降低不同 skill pack 在触发时互相干扰
- 官方默认 skills 只从 `bundled/` 读，不直接改这里。
- 你自己的 skill 放到 `local/`，升级时更安全。
- 当你希望机器人长期学会某个工作流、工具/API/MCP 调用方式、
  团队流程或领域规则时，优先把它沉淀成 local skill。
  轻量事实、偏好和简单常驻规则仍然放到 memory。
  `trpc-claw` 代码负责稳定的安全和生命周期边界，
  skill 负责持续演进的触发条件、操作步骤、示例和失败恢复。
- 如果你通过 Codex / skill hub 装技能，
  `~/.codex/skills/` 也会自动加入搜索路径。
- 想一次性看 bundled skills 的依赖规划，直接跑：

```bash
trpc-claw inspect deps --bundled
trpc-claw bootstrap deps --bundled --apply
```

- 这里的 `--bundled` 只会自动规划相对安全的系统包和托管 Python 依赖。
  浏览器 runtime、全局 npm 包、登录态之类仍然保持手动。
- 如果要找参考实现，优先看：
  - `weather/`
  - `github/`
  - `skill-creator/`
  - `anthropic-docx/`
  - `anthropic-pdf/`
