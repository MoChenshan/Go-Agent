## Evaluation 内网集成指南

### 结合 tRPC-Go 启动 Evaluation 服务

结合 `trpc-go.yaml` 配置文件使用 `trpc-go` 启动 Evaluation HTTP 服务，即可复用 `trpc-go` 框架的能力。

代码：

```go
import (
	"git.code.oa.com/trpc-go/trpc-go"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	tevaluation "git.woa.com/trpc-go/trpc-agent-go/trpc/evaluation"
	"trpc.group/trpc-go/trpc-agent-go/server/evaluation"
)

server := trpc.NewServer()
evaluationServer, err := evaluation.New(
	evaluation.WithAppName(appName),
	evaluation.WithBasePath("/evaluation"),
	evaluation.WithAgentEvaluator(agentEvaluator),
	evaluation.WithEvalSetManager(evalSetManager),
	evaluation.WithMetricManager(metricManager),
	evaluation.WithEvalResultManager(evalResultManager),
)
if err != nil {
	log.Fatalf("failed to create Evaluation server: %v", err)
}
if err := tevaluation.RegisterEvaluationServer(server, "trpc.test.evaluation.server", evaluationServer); err != nil {
	log.Fatalf("failed to register Evaluation server: %v", err)
}
if err := server.Serve(); err != nil {
	log.Fatalf("server stopped with error: %v", err)
}
```

配置文件：

```yaml
server:
  service:
    - name: trpc.test.evaluation.server
      ip: 127.0.0.1
      port: 8080
      protocol: http_no_protocol
```

完整代码参见 [examples/evaluation/trpc](../examples/evaluation/trpc/)。

### 伽利略评测插件集成

具体参考 [伽利略评测插件集成](https://iwiki.woa.com/p/4015773654#%E4%BC%BD%E5%88%A9%E7%95%A5galileo%E5%B9%B3%E5%8F%B0)。
