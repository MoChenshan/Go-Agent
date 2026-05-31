# LKE Basic 示例（主 Agent + 子 Agent）

这个示例演示在同一进程里完成 LKE 接入：

- 通过 `lke.New(botAppKey, ...)` 把 LKE SDK 适配成 `agent.Agent` 子 Agent（每次 `Run` 创建一次 `lkeClient`）
- 将子 Agent 挂到主 Agent，并通过 `runner.NewRunner(...)` 统一运行

## 运行方式

在仓库根目录执行：

```bash
cd examples/lke
go run ./basic
```

## 示例要点

- `lke_original.go`：模拟业务侧已经存在的 LKE 代码（回调处理 + 工具）。
- `lke_adapter.go`：模拟业务侧补的“适配/胶水层”，把这些代码挂到 `lke.WithClientSetup(...)` 里，并通过 `lke.New(...)` 产出一个标准 `agent.Agent`。
- `main.go`：展示“转成子 Agent 后”，如何被主 Agent 作为 subAgent 调用（`runner.NewRunner(...).Run(...)`）。
- 示例工具名与参数为通用占位符（例如 `local_action`），不承载任何业务含义。
