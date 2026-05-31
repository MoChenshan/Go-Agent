---
name: oncall-agent-code-norm-fixes
overview: 根据 tRPC-Go DDD 代码规范，修复 oncall_agent 项目中的代码规范问题（不涉及目录结构调整）
todos:
  - id: add-package-comments
    content: 补充 19 个缺失包级注释的文件
    status: pending
  - id: extract-sql-template
    content: 将 feedback 模块的 SQL 模板提取到独立文件，使用 go:embed 加载
    status: pending
    dependencies:
      - add-package-comments
  - id: reuse-http-client
    content: lingshan 模块 HTTP Client 改为包级别复用
    status: pending
    dependencies:
      - add-package-comments
  - id: unify-logging-methods
    content: 统一使用 Context 版本的日志方法
    status: pending
    dependencies:
      - add-package-comments
  - id: add-mock-directives
    content: 为 infrastructure 层接口添加 mockgen 生成指令
    status: pending
  - id: run-generate-mock
    content: 运行 go generate 生成 mock 文件
    status: pending
    dependencies:
      - add-mock-directives
  - id: verify-build
    content: 运行 go build 验证编译通过
    status: pending
    dependencies:
      - extract-sql-template
      - reuse-http-client
      - unify-logging-methods
      - run-generate-mock
  - id: run-lint-check
    content: 运行 golangci-lint 检查代码质量
    status: pending
    dependencies:
      - verify-build
---

## 用户需求

用户确认 agent 目录结构保持不变，要求检查项目中除 agent 目录结构之外的其他代码规范问题，并提出重构计划。

## 产品概述

oncall_agent 是一个基于 tRPC-Go 框架的魔方营销平台问题定位 Agent 服务，采用 DDD 架构。需要检查代码质量、注释规范、资源管理等方面的合规性。

## 核心问题

1. **包级注释缺失**：约 20 个 Go 文件缺少 `// Package xxx ...` 注释
2. **SQL 模板硬编码**：feedback 模块将 SQL 语句硬编码在代码中
3. **HTTP Client 未复用**：lingshan 模块每次请求都创建新的 HTTP Client
4. **日志使用不一致**：部分代码使用 `log.Errorf` 而非 `log.ErrorContextf`
5. **缺少 mock 生成指令**：部分 infrastructure 包缺少 `//go:generate mockgen`

## 技术栈

- **框架**: tRPC-Go
- **架构**: DDD（领域驱动设计）
- **依赖注入**: Wire
- **数据库**: MySQL（GORM）
- **代码检查**: golangci-lint

## 实现方案

### 1. 补充包级注释

根据 DDD 规范，每个 Go 文件必须有 `// Package <包名> <描述>` 格式的包级注释。

**需要补充的文件**（共 19 个）：

| 文件 | 包名 |
| --- | --- |
| `infrastructure/config/rainbow/types.go` | rainbow |
| `infrastructure/external/http/lingshan/lingshan_impl.go` | lingshan |
| `infrastructure/external/http/cdkey/cdkey_api.go` | cdkey |
| `infrastructure/external/http/cdkey/cdkey_impl.go` | cdkey |
| `infrastructure/external/http/galileo/galileo_api.go` | galileo |
| `infrastructure/external/http/lingshan/lingshan_api.go` | lingshan |
| `infrastructure/external/http/magiccli/magiccli_api.go` | magiccli |
| `infrastructure/external/http/magiccli/magiccli_impl.go` | magiccli |
| `infrastructure/external/trpc/conditionlog/conditionlog_impl.go` | conditionlog |
| `infrastructure/repo/mysql/magic_config/magic_config_impl.go` | magicconfig |
| `domain/model/cdkey.go` | model |
| `domain/model/galileo.go` | model |
| `domain/model/lingshan.go` | model |
| `domain/model/magic_config.go` | model |
| `domain/interfaces/external/cdkey_api.go` | external |
| `domain/interfaces/external/conditionlog_api.go` | external |
| `domain/interfaces/external/lingshan_api.go` | external |
| `domain/interfaces/external/magiccli_api.go` | external |
| `domain/tools/trace_analysis/trace_analysis_impl.go` | trace_analysis |


### 2. SQL 模板外提

**文件**: `infrastructure/repo/mysql/feedback/feedback_impl.go`

**当前代码**:

```
var storeMySQLCmd string = `
INSERT INTO t_oncall_agent_feedback 
    (sessionID, userID, sessionhistory, isPositive) 
    VALUES (?, ?, ?, ?)`
```

**修改方案**:

1. 创建 `infrastructure/repo/mysql/feedback/sql/store_feedback.sql`
2. 使用 `//go:embed` 加载 SQL 文件

### 3. HTTP Client 复用

**文件**: `infrastructure/external/http/lingshan/lingshan_impl.go`

**当前问题**: 每次请求都创建新的 `http.Client`

**修改方案**:

```
var httpClient = &http.Client{
    Timeout: 30 * time.Second,
}
```

### 4. 日志方法统一

将 `log.Errorf`/`log.Infof` 替换为 `log.ErrorContextf(ctx, ...)`/`log.InfoContextf(ctx, ...)` 以支持链路追踪。

**涉及文件**:

- `infrastructure/config/wuji/magic_impl.go`
- `services/a2a/a2a.go`

### 5. 补充 mock 生成指令

为 infrastructure 层的接口添加 `//go:generate mockgen` 指令：

**涉及文件**:

- `infrastructure/repo/mysql/feedback/feedback.go`
- `infrastructure/external/http/lingshan/lingshan_api.go`

## 目录结构

```
infrastructure/repo/mysql/feedback/
├── feedback.go           # [MODIFY] 添加 //go:generate mockgen
├── feedback_impl.go      # [MODIFY] 添加包注释，SQL 改用 go:embed
├── feedback_mock.go      # [NEW] mockgen 生成
└── sql/
    └── store_feedback.sql # [NEW] SQL 模板文件
```

## 实现注意事项

- SQL 文件使用 `go:embed` 加载，路径相对于 Go 文件
- HTTP Client 应声明为包级别变量，避免重复创建
- 日志方法优先使用 Context 版本，确保链路追踪完整
- mock 文件由 mockgen 自动生成，不要手动编辑

## Agent Extensions

### Skill

- **trpc-ddd-codegen**
- Purpose: 作为 DDD 代码规范参考，验证修改后的代码结构是否符合规范
- Expected outcome: 确保所有文件命名、注释规范符合 DDD 标准