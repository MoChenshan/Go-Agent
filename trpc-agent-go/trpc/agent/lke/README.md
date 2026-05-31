# LKE Agent 适配器

`trpc/agent/lke` 用于将 `github.com/tencent-lke/lke-sdk-go` 适配为 `trpc-agent-go` 的 `agent.Agent`。

接入后，你可以把 LKE 侧已经编排好的能力当成一个标准 Agent 使用：

- 同进程：作为 subAgent 被 `runner` / `multi-agent` / `graph` 等编排能力直接调用
- 跨进程：把该 Agent 暴露成 A2A 服务，再由其他服务远程接入（见 `examples/lke/a2a`）

## 关键设计

### 1) 每次 `Run()` 创建一个 LKE client

适配器默认在每次 `Run()`（也就是每次 invocation）里创建一个新的 `lkeClient`，这能自然满足业务方常见诉求：

- 不同用户/会话拥有不同的 `visitorBizID/sessionID`
- 每次请求可动态注入不同的工具、agent 编排、logger、变量等

默认从 `invocation` 派生：

- `visitorBizID`：`invocation.Session.UserID`，为空则使用 `unknown_user`
- `sessionID`：优先 `invocation.Session.ID`，否则 `invocation.InvocationID`

### 2) 事件桥接由适配器强制保证

LKE SDK 的事件是通过 `EventHandler` 回调上来的。为了让 `trpc-agent-go` 的事件语义（流式输出、插件/runner 过滤等）对齐，适配器会：

- 为每次 `Run()` 创建一个内部的桥接 handler（`BypassEventCollector`）
- 在 `Run()` 的最后 **强制** 执行 `lkeClient.SetEventHandler(collector)`，确保事件流一定能输出到 `trpc-agent-go`

因此业务侧不需要（也不建议）自己手动维护“把 LKE 回调写到 eventChan”这件事。

### 3) 业务回调仍可保留（可按请求生成不同实例）

如果业务原本就有自己的回调逻辑（日志/埋点/落库等），可以通过：

- `WithHandler(handler)`：共享一个 handler
- `WithHandlerFactory(factory)`：每次 `Run()` 生成一个 handler（推荐，便于携带 per-request 状态）

适配器会先调用业务 handler，再做桥接事件输出。

## 快速开始（同进程作为 subAgent）

完整示例：

- `examples/lke/basic/main.go`

最小接入代码（重点看 per-invocation 的 setup / options）：

```go
sub := lke.New(
    "your-bot-app-key",
    lke.WithName("lke-sub-agent"),
    lke.WithHandlerFactory(func(ctx context.Context, inv *agent.Invocation) (lkeeventhandler.EventHandler, error) {
        // 每次请求生成一个 handler（可携带请求级状态）
        return &MyBizHandler{}, nil
    }),
    lke.WithClientSetup(func(ctx context.Context, inv *agent.Invocation, client lke.Client) error {
        // 这里是业务的“胶水层”：给本次请求创建出来的 client 注入资产
        // - endpoint / logger
        // - tools / agents / handoff
        // - timeout / mock 等
        return nil
    }),
    lke.WithRunOptionsFactory(func(ctx context.Context, inv *agent.Invocation) (*lkemodel.Options, error) {
        // 每次请求动态构造 run options（例如变量、流控等）
        return &lkemodel.Options{StreamingThrottle: 20}, nil
    }),
)
```

> `WithClientSetup` 的第三个参数是 `lke.Client`（窄接口），不包含 `Run/SetEventHandler` 等能力，避免业务 setup 误破坏适配器的事件桥接语义。

## 作为 A2A 服务暴露

完整示例：

- `examples/lke/a2a/main.go`

思路很简单：先把 LKE 适配成 `agent.Agent`，再交给 `server/a2a` 暴露成服务即可。

## 配置项

`New(botAppKey, opts...)` 支持：

- `WithName(name string)`：Agent 名称，默认 `lke-agent`
- `WithDescription(desc string)`：Agent 描述
- `WithBufferSize(size int)`：事件通道缓冲区，默认 `50`
- `WithMock(enable bool)`：为每次新建的 `lkeClient` 开启 mock
- `WithEventBypass(enable bool)`：是否桥接事件到 `trpc-agent-go`（默认 `true`）
- `WithHandler(handler lkeeventhandler.EventHandler)`：共享业务回调处理器
- `WithHandlerFactory(factory HandlerFactory)`：每次请求创建业务回调处理器（推荐）
- `WithClientBuilder(builder ClientBuilder)`：自定义每次请求如何创建 `lkeClient`（高级用法）
- `WithClientSetup(setup ClientSetup)`：为每次新建的 `lkeClient` 注入注册逻辑（工具/agent/handoff）
- `WithDefaultRunOptions(opts *lkemodel.Options)`：默认 LKE 调用参数（注意不要在多次请求间复用可变 map）
- `WithRunOptionsFactory(factory RunOptionsFactory)`：为每次请求动态构造 LKE 调用参数（推荐）
- `WithVisitorBizIDResolver(fn)` / `WithSessionIDResolver(fn)`：自定义从 `invocation` 派生 user/session 的方式
- `WithLockKey(fn)`：控制并发串行粒度（默认同 session 串行；返回空字符串可关闭）
- `WithDebug(debug bool)`：开启调试日志

## 注意事项

- `botAppKey` 不能为空
- `invocation.Message.Content` 不能为空（为空会返回错误事件）
- `Close()` 只会禁止后续 `Run()`，不维护全局 `lkeClient`
