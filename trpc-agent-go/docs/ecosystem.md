## 内部共建组件

### 共建流程

1. 在 `https://git.woa.com/trpc-go/trpc-agent-go` 创建分支。
2. 在 `trpc/` 目录下对应模块创建组件（如 `trpc/agent/someagent`、`trpc/model/somemodel`、`trpc/tool/sometool`）。
3. 实现相应接口。
4. 编写测试与文档。
5. 提交 Merge Request。

**目录结构示例：**
```
trpc/agent/tencent_agent/
├── tencent_agent.go
├── config.go
├── examples/
├── README.md
└── tencent_agent_test.go
```

### 适合的组件

- 依赖腾讯内部组件的集成。
- 腾讯云服务集成。
- 内部业务工具。
- 公司特有的监控组件。
- 内部协议支持。

### 组件位置

若组件依赖腾讯内部能力，请贡献至内网 `trpc-agent-go` 仓库，并按以下路径归类：

- 内部 Agent 组件：`https://git.woa.com/trpc-go/trpc-agent-go/tree/master/trpc/agent/`。
- 内部 Model 组件：`https://git.woa.com/trpc-go/trpc-agent-go/tree/master/trpc/model/`。
- 内部 Tool 组件：`https://git.woa.com/trpc-go/trpc-agent-go/tree/master/trpc/tool/`。
- 内部 Knowledge 组件：`https://git.woa.com/trpc-go/trpc-agent-go/tree/master/trpc/knowledge/`。
  - 现有实现可参考：[内网对接的 tRAG 知识库](https://git.woa.com/trpc-go/trpc-agent-go/-/merge_requests/54)。
  - **可集成的内网组件示例：**
    - 腾讯云数据库会话存储。
    - 腾讯云 Redis 会话存储。
- 内部 Session 组件：`https://git.woa.com/trpc-go/trpc-agent-go/tree/master/trpc/session/`。
  - **可集成的内网组件示例：**
    - 腾讯云数据库记忆存储。
    - 腾讯云向量数据库记忆存储。
- 内部 Memory 组件：`https://git.woa.com/trpc-go/trpc-agent-go/tree/master/trpc/memory/`。
- 内部 Telemetry 组件：`https://git.woa.com/trpc-go/trpc-agent-go/tree/master/trpc/telemetry/`。
