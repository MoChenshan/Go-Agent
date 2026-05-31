# tRPC-Go 集成示例

在已有 tRPC-Go 进程中暴露 `/agui` SSE 端点的示例：

1. 	导入 `tagui "git.woa.com/trpc-go/trpc-agent-go/trpc/agui"`
2. `server := trpc.NewServer()` 加载配置文件，创建 `trpc` 服务。
3. `agui.New(runner, agui.WithPath("/agui"))` 设置 HTTP 路由，创建 AG-UI server。
4. `tagui.RegisterAGUIServer(server, "service_name", aguiServer)` 将 AG-UI server 注册到 `trpc` 服务。
5. `server.Serve()` 启动 `trpc` 服务后，即可复用日志、限流、注册中心等能力。

## 运行

```bash
go run .
```

默认监听 `127.0.0.1:8080/agui`，监听地址在配置文件中配置，路由在代码中配置。

日志示例：

```log
plugin log-default setup succeed, time elapsed: 334.678µs
2025-09-26 22:36:52.365	DEBUG	maxprocs/maxprocs.go:48	maxprocs: Leaving GOMAXPROCS=32: CPU quota undefined
2025-09-26 22:36:52.365	INFO	server/service.go:211	process: 3392277, http_no_protocol service: trpc.test.helloworld.agui launch success, tcp: 127.0.0.1:8080, serving ...
2025-09-26 22:38:09.344	DEBUG	processor/basic.go:71	Basic request processor: processing request for agent agui-agent
2025-09-26 22:38:09.344	DEBUG	processor/basic.go:81	Basic request processor: sent preprocessing event
2025-09-26 22:38:09.344	DEBUG	processor/instruction.go:90	Instruction request processor: processing request for agent agui-agent
2025-09-26 22:38:09.344	DEBUG	processor/instruction.go:187	Instruction request processor: added combined system message
2025-09-26 22:38:09.344	DEBUG	processor/instruction.go:220	Instruction request processor: sent preprocessing event
2025-09-26 22:38:09.344	DEBUG	processor/identity.go:73	Identity request processor: processing request for agent agui-agent
2025-09-26 22:38:09.344	DEBUG	processor/identity.go:107	Identity request processor: sent preprocessing event
2025-09-26 22:38:09.344	DEBUG	llmflow/llmflow.go:322	Calling LLM for agent agui-agent
2025-09-26 22:38:19.329	DEBUG	processor/functioncall.go:445	Executing tool calculator with args: {"a": 10, "b": 11, "operation": "add"}
2025-09-26 22:38:19.329	DEBUG	processor/functioncall.go:469	CallableTool calculator executed successfully, result: {"result":21}
2025-09-26 22:38:19.329	DEBUG	processor/basic.go:71	Basic request processor: processing request for agent agui-agent
2025-09-26 22:38:19.329	DEBUG	processor/basic.go:81	Basic request processor: sent preprocessing event
2025-09-26 22:38:19.329	DEBUG	processor/instruction.go:90	Instruction request processor: processing request for agent agui-agent
2025-09-26 22:38:19.329	DEBUG	processor/instruction.go:187	Instruction request processor: added combined system message
2025-09-26 22:38:19.329	DEBUG	processor/instruction.go:220	Instruction request processor: sent preprocessing event
2025-09-26 22:38:19.329	DEBUG	processor/identity.go:73	Identity request processor: processing request for agent agui-agent
2025-09-26 22:38:19.329	DEBUG	processor/identity.go:107	Identity request processor: sent preprocessing event
2025-09-26 22:38:19.329	DEBUG	llmflow/llmflow.go:322	Calling LLM for agent agui-agent
2025-09-26 22:38:24.499	DEBUG	processor/functioncall.go:445	Executing tool calculator with args: {"a": 21, "b": 2, "operation": "multiply"}
2025-09-26 22:38:24.499	DEBUG	processor/functioncall.go:469	CallableTool calculator executed successfully, result: {"result":42}
2025-09-26 22:38:24.499	DEBUG	processor/basic.go:71	Basic request processor: processing request for agent agui-agent
2025-09-26 22:38:24.499	DEBUG	processor/basic.go:81	Basic request processor: sent preprocessing event
2025-09-26 22:38:24.499	DEBUG	processor/instruction.go:90	Instruction request processor: processing request for agent agui-agent
2025-09-26 22:38:24.499	DEBUG	processor/instruction.go:187	Instruction request processor: added combined system message
2025-09-26 22:38:24.500	DEBUG	processor/instruction.go:220	Instruction request processor: sent preprocessing event
2025-09-26 22:38:24.500	DEBUG	processor/identity.go:73	Identity request processor: processing request for agent agui-agent
2025-09-26 22:38:24.500	DEBUG	processor/identity.go:107	Identity request processor: sent preprocessing event
2025-09-26 22:38:24.500	DEBUG	llmflow/llmflow.go:322	Calling LLM for agent agui-agent
2025-09-26 22:40:55.643	INFO	server/service.go:792	process: 3392277, http_no_protocol service: trpc.test.helloworld.agui, closing...
2025-09-26 22:40:55.643	INFO	server/service.go:836	process 3392277 service trpc.test.helloworld.agui remain 1 requests/listeners/conns, wait 1s before closing service
2025-09-26 22:40:55.643	INFO	admin/admin.go:196	process: 3392277, admin server, closed
2025-09-26 22:40:56.745	INFO	server/service.go:822	process: 3392277, http_no_protocol service: trpc.test.helloworld.agui, closed
```
