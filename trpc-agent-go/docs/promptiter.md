## PromptIter 内网集成指南

### 结合 tRPC-Go 启动 PromptIter 服务

结合 `trpc-go.yaml` 配置文件使用 `trpc-go` 启动 PromptIter HTTP 服务，即可复用 `trpc-go` 框架的能力。

代码：

```go
import (
	"git.code.oa.com/trpc-go/trpc-go"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	tpromptiter "git.woa.com/trpc-go/trpc-agent-go/trpc/promptiter"
	"trpc.group/trpc-go/trpc-agent-go/server/promptiter"
)

server := trpc.NewServer()
promptIterServer, err := promptiter.New(
	promptiter.WithAppName(appName),
	promptiter.WithBasePath("/promptiter/v1/apps"),
	promptiter.WithEngine(promptIterEngine),
	promptiter.WithManager(promptIterManager),
)
if err != nil {
	log.Fatalf("failed to create PromptIter server: %v", err)
}
if err := tpromptiter.RegisterPromptIterServer(server, "trpc.test.promptiter.server", promptIterServer); err != nil {
	log.Fatalf("failed to register PromptIter server: %v", err)
}
if err := server.Serve(); err != nil {
	log.Fatalf("server stopped with error: %v", err)
}
```

配置文件：

```yaml
server:
  service:
    - name: trpc.test.promptiter.server
      ip: 127.0.0.1
      port: 8080
      protocol: http_no_protocol
```

完整代码参见 [examples/evaluation/promptiter/trpc](../examples/evaluation/promptiter/trpc/)。
