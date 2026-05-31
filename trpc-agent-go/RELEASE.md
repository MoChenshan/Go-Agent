# 发版说明

发版 MR 必须同步更新 `trpc/stat.go` 中的 `version` 常量；[发版流水线](https://devops.woa.com/console/pipeline/pcgtrpcproject/p-089e4d10e8a74fc1bfe38ddf51196504) 以 `trpc/stat.go` 的版本变更作为触发条件。

发版流水线会跑三类测试：

- 单元测试：检查各模块的基础逻辑和边界行为是否符合预期。
- e2e 测试：`test/` 检查位于 Agent 上层的集成能力，例如 AG-UI server 的响应内容和事件序列是否符合预期。
- example 测试：`examples/` 检查 Agent 行为、响应内容、工具调用是否符合预期，以及各类 option 是否在端到端级别生效。
