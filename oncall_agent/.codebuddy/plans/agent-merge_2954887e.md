---
name: agent-merge
overview: 合并魔方相关 agent 为统一入口：将 magic_agent 重命名为 magic_oncall_agent，删除废弃的 magic_config_agent、rule_engine_agent 和旧 magic_oncall_agent 目录，更新 wire.go 和 main.go 引用。
todos:
  - id: delete-stale-agents
    content: 删除废弃的旧 magic_oncall_agent、magic_config_agent、rule_engine_agent 目录
    status: completed
  - id: rename-magic-agent
    content: 将 magic_agent 目录重命名为 magic_oncall_agent，文件名和包名同步更新
    status: completed
    dependencies:
      - delete-stale-agents
  - id: update-wire-references
    content: 更新 wire.go 中 import、类型引用和 SSE agentName 参数
    status: completed
    dependencies:
      - rename-magic-agent
  - id: cleanup-main-constants
    content: 删除 main.go 中废弃的 ruleEngineSSEServiceName、magicConfigSSEServiceName 及对应 pathMap 条目
    status: completed
  - id: regenerate-wire
    content: 删除 wire_gen.go 并运行 go generate 重新生成
    status: completed
    dependencies:
      - update-wire-references
      - cleanup-main-constants
  - id: verify-build
    content: 运行 go build 验证编译通过
    status: completed
    dependencies:
      - regenerate-wire
  - id: update-docs
    content: 更新 CODEBUDDY.md 文档中 agent 架构描述
    status: completed
    dependencies:
      - verify-build
---

## 用户需求

将当前 `magic_agent` 目录重命名为 `magic_oncall_agent`，作为魔方相关功能的唯一入口 agent。删除废弃的 `magic_oncall_agent`、`magic_config_agent`、`rule_engine_agent` 目录。更新所有引用。

## 产品概述

oncall_agent 服务中的魔方统一入口 agent 重命名：`magic_agent` → `magic_oncall_agent`，确保命名与业务语义一致，同时清理废弃代码。

## 核心功能

1. 将 `domain/agents/magic_agent/` 重命名为 `domain/agents/magic_oncall_agent/`
2. 包名从 `magic_agent` 改为 `magic_oncall_agent`，agentName 改为 `magic_oncall_agent`
3. 更新 `wire.go` 中所有 import 和类型引用
4. 删除三个废弃 agent 目录
5. 清理 `main.go` 中的废弃常量和 pathMap 条目
6. 更新 `CODEBUDDY.md` 文档
7. 重新生成 `wire_gen.go` 并验证构建

## 技术栈

- **框架**: tRPC-Go
- **架构**: DDD（领域驱动设计）
- **依赖注入**: Wire（Google Wire）
- **语言**: Go

## 实现方案

### 策略

将 `magic_agent` 目录内容迁移到 `magic_oncall_agent`（先删除旧的废弃目录），更新包名和所有引用，然后通过 Wire 重新生成依赖注入代码。

### 关键技术决策

1. **先删旧目录再重命名**：当前磁盘上已有旧的 `magic_oncall_agent/` 目录（废弃的、未合并的版本），需先删除，然后将 `magic_agent/` 重命名为 `magic_oncall_agent/`。避免目录冲突。
2. **文件名也需更新**：`magic_agent_api.go` → `magic_oncall_agent_api.go`，`magic_agent_impl.go` → `magic_oncall_agent_impl.go`，保持 Go 文件名与包名一致。
3. **agentName 变更影响 Wuji 配置**：agentName 从 `"magic_agent"` 改为 `"magic_oncall_agent"`，影响 `WujiCli.GetAgentConfig(agentName)` 的查询 key。需确保 Wuji 配置平台同步更新，或在代码中添加 fallback 逻辑。
4. **SSE agentName 参数**：`sse.NewSSEService(sessionSvc, magicAgt, mysqlCli, "magic_agent", cfg.Debug)` 中的 `"magic_agent"` 也需改为 `"magic_oncall_agent"`。
5. **ProposeConfigChange 不在本次范围**：`magic_tool` 中的 `NewProposeConfigChangeTool` 已不在 wire.go 中使用（agent 只读），本次不做修改。

### 目录结构

```
domain/agents/
├── magic_oncall_agent/              # [RENAME from magic_agent] 魔方统一入口 agent
│   ├── magic_oncall_agent_api.go    # [RENAME + MODIFY] 包名→magic_oncall_agent, 注释更新
│   ├── magic_oncall_agent_impl.go   # [RENAME + MODIFY] 包名→magic_oncall_agent, agentName→magic_oncall_agent
│   └── system_prompt.md             # [KEEP] 已合并的 system prompt，无需修改
├── cdk_agent/                       # [KEEP] CDKey agent
├── span_analysis_agent/             # [KEEP] 子代理
└── repo_agent/                      # [KEEP] 子代理

# 删除的目录
├── magic_oncall_agent/ (旧)         # [DELETE] 废弃的旧 oncall agent
├── magic_config_agent/              # [DELETE] 废弃的 config agent
└── rule_engine_agent/               # [DELETE] 废弃的规则引擎 agent

# 修改的文件
wire.go                              # [MODIFY] import alias, 类型引用, SSE agentName
wire_gen.go                          # [REGENERATE] go generate ./wire.go
main.go                              # [MODIFY] 删除 ruleEngineSSEServiceName, magicConfigSSEServiceName 及对应 pathMap
CODEBUDDY.md                         # [MODIFY] 更新文档描述
```

## 实现注意事项

- Wire 重新生成时必须先删除旧的 `wire_gen.go`，否则 Wire 可能不会重新生成
- `agentName` 变更会影响 Wuji 配置查询，需确认 Wuji 平台侧配置是否需要同步
- 删除旧目录前确认没有其他代码引用这些包（已验证：wire.go 不引用）
- `magic_tool/magic_tool_api.go` 中的 `Operator` 字段硬编码为 `"magic_agent"`，考虑是否需要同步修改（影响 ProposeConfigChange 功能，但该功能已不使用，可暂不改）