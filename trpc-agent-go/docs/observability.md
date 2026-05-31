## 内部平台接入

### 伽利略(Galileo)平台

### 方式一：tRPC 服务集成

适用于基于 tRPC 框架的服务。 
可以参考 [examples/a2a-galileo](../examples/a2a-galileo) 的代码示例。

#### 1. 前置条件

请参考 [Galileo 官方文档 - GO (tRPC) 集成指南](https://iwiki.woa.com/p/4009274553) 完成基础配置。

#### 2. 集成代码

```go
// 导入伽利略可观测初始化，init 函数自动执行集成
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo"
```

#### 3. 配置文件

确保 `trpc_go.yaml` 包含伽利略插件配置：

```yaml
plugins:
  telemetry:
    galileo:
      verbose: "error"
      # ... 其他配置项
```

### 方式二：通用 Go 服务集成

适用于非 tRPC 框架的 Go 服务。

#### 1. 前置条件

请参考 [伽利略官方文档 - GO (通用) 集成指南](https://iwiki.woa.com/p/4013979751) 完成基础配置。

#### 2. 集成代码

```go
import (
    "git.woa.com/galileo/eco/go/sdk/base/configs/ocp"
    traceconf "git.woa.com/galileo/eco/go/sdk/base/configs/traces"
    "git.woa.com/galileo/eco/go/sdk/base/lib/logs"
    basemode "git.woa.com/galileo/eco/go/sdk/base/model"
    "git.woa.com/galileo/eco/go/sdk/base/self"
    "git.woa.com/galileo/eco/go/sdk/base/semconv"
    modelv3 "git.woa.com/galileo/eco/go/sdk/base/v3/model"
    gtrace "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo/trace"
)

func setupGalileo() error {
    // 资源描述，见文档 https://git.woa.com/galileo/semantic-conventions/blob/toraxie-omp-3.0/semconv/doc/v3.0.0/index.md
    resv3 := modelv3.NewResource(
        "PCG-123.knocknock_test.short_token_proxy",
        basemode.Production,
        "formal",
        "127.0.0.1",
        "test.galileo.SDK.sz10010",
        "set.sz.1",
        "sz",
        "test-v0.1.0",
        "",
    )
    local := func(to *ocp.GalileoConfig) error {
        to.Verbose = "error"
		// 接入地址请根据需要修改，参考：https://iwiki.woa.com/p/4010767585
		// ocp 管控地址：中国大陆内网（默认）
        to.OcpAddr = "http://gocp.woa.com/ocp/api/v1/get_config"
        // 数据接入点：中国大陆内网（默认）
        to.Config.AccessPoint = basemode.AccessPoint_ACCESS_POINT_CN_PRIVATE
		return nil
    }
    _ = ocp.RegisterResource(
        resv3, ocp.WithLocalDecoder(ocp.DecodeFunc(local)),
        ocp.WithDuration(time.Minute),
    )
    // Initialize self-monitoring reporting, settings required for non-default configurations such as overseas access.
    config := ocp.GetUpdater(resv3.Target).GetConfig().Config
    self.SetupObserver(resv3, logs.DefaultWrapper(), config.SelfMonitor, config.ConfigServer)
    tracesConfig := traceconf.NewConfig(
        resv3,
        traceconf.WithSchemaURL(semconv.SchemaURL),
    )
    return gtrace.Setup(tracesConfig)
}
```

### 伽利略(Galileo)评测插件集成
```go
import (
    "context"
    "flag"
    "fmt"
    "log"
    
    "git.code.oa.com/trpc-go/trpc-go"
    gevaluation "git.woa.com/galileo/trpc-agent-go-galileo/evaluation"
    
    "trpc.group/trpc-go/trpc-agent-go/evaluation"
    "trpc.group/trpc-go/trpc-agent-go/evaluation/evaluator/registry"
    "trpc.group/trpc-go/trpc-agent-go/evaluation/metric"
    metriclocal "trpc.group/trpc-go/trpc-agent-go/evaluation/metric/local"
    "trpc.group/trpc-go/trpc-agent-go/runner"
    
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo"
)
// 评测代码集成 只需要使用伽利略的 DatasetManager ResultManager 以及 callbacks，就可以从伽利略拉取数据集进行评测，并将结果上报到伽利略
func eval(){
    evalSetManager := gevaluation.NewDatasetManager()
    evalResultManager := gevaluation.NewResultManager()
    metricManager := metriclocal.New(metric.WithBaseDir(*dataDir))
    registry := registry.New()
    taskManager := gevaluation.NewTaskManager()
    callbacks := gevaluation.NewCallbacks(taskManager, evalResultManager)
    agentEvaluator, err := evaluation.New(
        appName,
        runner,
        evaluation.WithEvalSetManager(evalSetManager),
        evaluation.WithMetricManager(metricManager),
        evaluation.WithEvalResultManager(evalResultManager),
        evaluation.WithRegistry(registry),
        evaluation.WithCallbacks(callbacks),
        evaluation.WithEvalCaseParallelInferenceEnabled(true),
        evaluation.WithEvalCaseParallelism(10),
    )
    if err != nil {
        log.Fatalf("create evaluator: %v", err)
    }
    result, err := agentEvaluator.Evaluate(ctx, *evalSetID)
    if err != nil {
        log.Fatalf("evaluate: %v", err)
    }
}

```

#### agui 的 galileo 上报可参考 [agui 文档](agui.md)

### 智研监控宝

### 方式一：使用 tRPC-Go 插件 telemetry.zhiyan-llm，通过 trpc_go.yaml 配置，具体例子 [examples/telemetry/zhiyan/zhiyan-llm-trpc-plugin](../examples/telemetry/zhiyan/zhiyan-llm-trpc-plugin/README.md)

### 方式二：使用智研监控宝提供的 llm_go_sdk 具体例子 [examples/telemetry/zhiyan/llm-sdk](../examples/telemetry/zhiyan/llm-sdk/READM.md)

### 方式三：使用配置智研提供的 oteltrpc 插件，复用 trpc_go.yaml 配置，具体例子 [examples/telemetry/zhiyan/trpc-plugin](../examples/telemetry/zhiyan/trpc-plugin/README.md)


#### #### agui 的智研监控宝上报可参考 [agui 文档](agui.md)

### 参考资源

- [伽利略 GO (tRPC) 集成指南](https://iwiki.woa.com/p/4009274553)
- [伽利略 GO (通用) 集成指南](https://iwiki.woa.com/p/4013979751)
- [tRPC-Agent-Go Telemetry 示例](../examples/telemetry/)
- [A2A 伽利略集成示例](../examples/a2a-galileo/)

通过合理使用可观测功能，你可以建立完善的 Agent 应用监控体系，及时发现和解决问题，持续优化系统性能。
