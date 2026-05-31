# promptengine

`promptengine` 是一个 trpc-agent-go 采样插件，用于采集一次 Runner 任务执行的完整轨迹，并通过 tRPC 调用 `log_collector` 服务上报。

**Module 路径**：`git.woa.com/trpc-go/trpc-agent-go/trpc/plugin/promptengine`

## 功能

- root Agent `AfterAgent` 触发时上报一次，sub-agent / model / tool 步骤会合并到同一次 root invocation。
- 采样开关、采样率、业务隔离 token 支持进程内 API 与 HTTP 控制面热更新。
- 支持按 `appName` 下发 per-app 覆盖配置。
- 内置 tRPC writer，默认上报到 `polaris://trpc.trs.prompt_log_collector.LogCollector`。
- 上报失败只打日志，不影响 Runner 主流程。

## 快速开始

```go
import (
    "git.woa.com/trpc-go/trpc-agent-go/trpc/plugin/promptengine"
    "trpc.group/trpc-go/trpc-agent-go/runner"
)

sampler := promptengine.New(
    promptengine.WithSampleRate(0),
    promptengine.WithTRPCWriter(),
    promptengine.WithAsyncWrite(100),
)

r := runner.NewRunner(
    "myapp",
    myAgent,
    runner.WithPlugins(sampler.NonClosable()),
)
```

`WithTRPCWriter()` 会从 `trpc.GlobalConfig().Server.Service[0].Name` 自动读取 caller。宿主进程仍需要在 `trpc_go.yaml` 中配置 tRPC 服务，并引入 Polaris selector / registry 相关插件配置，否则默认 Polaris target 无法解析。

## 公共 API

| API | 作用 | 默认 |
| --- | --- | --- |
| `New(opts...)` | 创建 sampler | - |
| `WithName(string)` | 设置插件名 | `promptengine` |
| `WithEnabled(bool)` | 设置采样总开关 | `true` |
| `WithSampleRate(float64)` | 设置采样率，范围 `[0,1]` | `0` |
| `WithSamplerToken(string)` | 设置默认业务隔离 token | `""` |
| `WithTRPCWriter(opts...)` | 使用内置 tRPC writer 上报 `log_collector` | 日志 writer |
| `WithTRPCCaller(string)` | 覆盖上报 caller | 自动读取 tRPC server service name |
| `WithTRPCTarget(string)` | 覆盖上报 target | `polaris://trpc.trs.prompt_log_collector.LogCollector` |
| `WithTRPCTimeout(time.Duration)` | 设置单次上报超时 | `3s` |
| `WithMaxSteps(int)` | 限制单次 trace step 数量 | `1000` |
| `WithAsyncWrite(int)` | 开启异步上报队列 | 同步上报 |
| `WithStructureID(string)` | 设置默认 structure ID | root agent name |
| `ConfigHandler()` | 暴露 HTTP 控制面 | - |

`Trace` 结构、自定义 writer 接口和 writer option 类型不作为公共 API 暴露。业务侧只依赖 sampler 配置入口，trace payload 是 sampler 与 `log_collector` 之间的内部协议。

## 多 Runner 单例

如果一个进程内有多个 Runner，或者按请求创建 Runner，并且这些 Runner 共享同一个 sampler，应使用 `sampler.NonClosable()`：

```go
var sampler = promptengine.New(
    promptengine.WithSampleRate(0),
    promptengine.WithTRPCWriter(),
    promptengine.WithAsyncWrite(100),
)

r := runner.NewRunner(
    "myapp",
    myAgent,
    runner.WithPlugins(sampler.NonClosable()),
)
```

`*Sampler` 实现了 `plugin.Closer`，Runner 关闭时会关闭异步 writer。`NonClosable()` 返回的 wrapper 只实现 `plugin.Plugin`，适合挂到多个 Runner 上共享同一个 sampler core。

## HTTP 控制面

构造期配置使用 `Option`。运行时动态调整采样开关、采样率和 per-app token 时，使用 `ConfigHandler()` 暴露的 HTTP 控制面；控制面的 JSON body 是内部 wire contract，不作为 Go 公共类型导出。

`ConfigHandler()` 返回一个 `http.Handler`，业务方自行决定挂载路径：

```go
mux := http.NewServeMux()
configHandler := sampler.ConfigHandler()
handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Header.Get("Authorization") != "Bearer "+os.Getenv("PROMPTENGINE_CONFIG_TOKEN") {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusUnauthorized)
        _, _ = w.Write([]byte(`{"error":"unauthorized"}` + "\n"))
        return
    }
    configHandler.ServeHTTP(w, r)
})
mux.Handle(
    "/promptiter/v1/plugins/trace_reporter/config",
    handler,
)
```

`ConfigHandler()` 不内置鉴权，默认放行所有请求。生产环境应挂在内网 admin 端口，或通过外层 middleware 加鉴权。

`sampler_token` 是上报给 `log_collector` 的业务隔离标签，不是 HTTP 控制面的鉴权凭证。per-app 配置会在 root invocation 开始时生效，该 invocation 上报时使用对应 app 的 token。

### GET

```bash
curl http://localhost:9090/promptiter/v1/plugins/trace_reporter/config
curl "http://localhost:9090/promptiter/v1/plugins/trace_reporter/config?app=my-app"
```

不带 `app` 时返回默认配置与所有 app 覆盖：

```json
{
  "config": {"enabled": true, "sample_rate": 0.1, "sampler_token": "default-token"},
  "apps": {
    "my-app": {"enabled": true, "sample_rate": 1.0, "sampler_token": "app-token"}
  }
}
```

带 `app` 时返回该 app 的生效配置：

```json
{
  "config": {"enabled": true, "sample_rate": 1.0, "sampler_token": "app-token"},
  "source": "override"
}
```

未命中 override 时 `source` 为 `default`。

### PUT

```bash
curl -X PUT -H "Content-Type: application/json" \
  -d '{"config":{"enabled":true,"sample_rate":0.5,"sampler_token":"default-token"}}' \
  http://localhost:9090/promptiter/v1/plugins/trace_reporter/config

curl -X PUT -H "Content-Type: application/json" \
  -d '{"config":{"enabled":true,"sample_rate":1.0,"sampler_token":"app-token"}}' \
  "http://localhost:9090/promptiter/v1/plugins/trace_reporter/config?app=my-app"
```

PUT 是完整覆盖语义，不做字段级 merge。`sample_rate` 必须在 `[0,1]` 内。

### DELETE

```bash
curl -X DELETE \
  "http://localhost:9090/promptiter/v1/plugins/trace_reporter/config?app=my-app"
```

DELETE 只支持删除 app override。删除默认配置会返回 `405 Method Not Allowed`。

错误响应统一为：

```json
{"error":"<message>"}
```

## log_collector 协议

本目录的 `internal/proto/log_collector.proto` 是 LogCollector 服务协议的源文件。该 proto 包只供 `promptengine` 内部 writer 使用，不作为公共 API 暴露。修改协议时：

1. 修改 `trpc-agent-go/trpc/plugin/promptengine/internal/proto/log_collector.proto`。
2. 使用本仓 tRPC CLI 重新生成 `log_collector.pb.go` 和 `log_collector.trpc.go`。
3. 将 `.proto` 同步到 `log_collector` 服务仓，仅按服务仓需要调整 `go_package`。
4. 在 `log_collector` 服务仓用其工具链重新生成对应代码。

`log_collector` 服务名为 `trpc.trs.prompt_log_collector.LogCollector`，方法为 `ReportTrace(ReportTraceRequest) returns (ReportTraceResponse)`。

## 注意事项

- `SampleRate=0` 或 `Enabled=false` 时不会采样。
- `WithMaxSteps` 只限制单次 trace step 数量，超限后的 step 会被丢弃。
- `WithAsyncWrite` 队列满时会丢弃 trace 并记录错误日志。
- 内置 writer 使用 `context.WithoutCancel`，Runner 返回后仍会尝试完成上报，单次 RPC 仍受 `WithTRPCTimeout` 控制。
