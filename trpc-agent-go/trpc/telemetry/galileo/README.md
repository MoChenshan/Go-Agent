# trpc-agent-go Galileo LLM 可观测集成指南

## 📋 概述

本文档介绍如何将 trpc-agent-go 与 Galileo LLM 可观测系统集成。

### 🔍 当前支持状态
- ✅ **Tracer**: 支持 Galileo Tracer 集成
- ✅ **Meter**: 支持 Galileo Meter 集成
- ✅ **Logger**: 支持 Galileo Logger 集成

## 🚀 集成方法

### 方法 1: tRPC 框架服务集成

适用于基于 tRPC 框架的服务。

#### 1. 前置条件
参考 [Galileo 官方文档 - GO (tRPC) 集成指南](https://iwiki.woa.com/p/4009274553) 完成基础配置。

#### 2. 集成代码

```go
// Import telemetry setup for OpenTelemetry integration, setup will be executed at init function
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo"
```
注意更新  git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo 和 git.woa.com/galileo/trpc-agent-go-galileo 到最新版本

#### 3. 配置文件

确保 `trpc_go.yaml` 包含 Galileo 插件配置：

```yaml
plugins:
  telemetry:
    galileo:
      # Galileo related configuration
      verbose: "error"
      # ... other configuration items
      config: # 本地配置
        opentelemetry_push:
          enable: true
          url: otlp.j.woa.com:80
```

### 方法 2: 非 tRPC 框架服务集成

适用于不基于 tRPC 框架的 Go 服务。

#### 1. 前置条件
参考 [Galileo 官方文档 - GO (通用) 集成指南](https://iwiki.woa.com/p/4013979751) 完成基础配置。

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
    gmetrics "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo/metrics"
    gtrace "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo/trace"
	gevaluation "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo/evaluation"
)

func setupGalileo() error {
    // Resource description, see documentation: https://git.woa.com/galileo/semantic-conventions/blob/toraxie-omp-3.0/semconv/doc/v3.0.0/index.md
	resv3 := modelv3.NewResource(
		"PCG-123.knocknock_test.short_token_proxy", // required, 观测对象的唯一标识 ID
		basemode.Production,                        // required, 命名空间，区分正式环境和测试环境
		"formal",                                   // required, 用户环境
		"127.0.0.1",                                // 本机 IP 地址
		"test.galileo.SDK.sz10010",                 // 容器名
		"set.sz.1",                                 // 服务 set
		"sz",                                       // 部署城市
		"test-v0.1.0",                              //  服务版本
		"",                                         // 框架协议，如 trpc、http、grpc 等
	)
    cfg := basemode.OpenTelemetryPushConfig{
        Enable: true,
        Url:    "otlp.j.woa.com:80", // 伽利略 OpenTelemetry collector 地址。
    }
	local := func(to *ocp.GalileoConfig) error {
		to.Verbose = "error"
		// Modify the access address as needed, refer to: https://iwiki.woa.com/p/4010767585: https://iwiki.woa.com/p/4010767585
		// OCP management address: Mainland China intranet (default)
		to.OcpAddr = "http://gocp.woa.com/ocp/api/v1/get_config"
		// Data access point: Mainland China intranet (default)
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
    if err := gtrace.Setup(tracesConfig); err != nil {
        return err
    }
    if err := gevaluation.Setup(*resv3, ocp.GetUpdater(resv3.Target).GetConfig().OcpAddr); err != nil {
        return err
    }
    return gmetrics.Setup(*resv3, cfg)
}

```

## 🚀 评测插件集成
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

## 🧪 集成验证

集成完成后，您可以通过以下方法进行验证：

1. **检查日志**: 启动时应该看到成功的 Galileo 初始化日志
2. **追踪数据**: 检查 Galileo 平台上是否有追踪数据上报
3. **错误监控**: 确认没有相关的错误日志
4. **LLM 评测**: 确认评测数据是否有上报

## 📚 参考文档

- [Galileo GO (tRPC) 集成指南](https://iwiki.woa.com/p/4009274553)
- [Galileo GO (通用) 集成指南](https://iwiki.woa.com/p/4013979751)
- [Galileo SDK Tracer 实现文档](https://iwiki.woa.com/p/4012224483)

## ⚠️ 重要说明

1. **依赖版本**: 确保 Galileo SDK 和 trpc-agent-go 版本兼容性
2. **网络连接**: 确认服务能够访问 Galileo 端点
3. **配置验证**: 仔细检查 Galileo 相关配置项的正确性

## 🔧 故障排除

### 常见问题

**Q: 没有追踪数据上报**
- 检查 Galileo 配置是否正确
- 确认网络连接正常
- 检查采样率设置

**Q: 编译错误**
- 检查依赖包版本
- 确认导入路径正确

**Q: 运行时错误**
- 查看详细错误日志
- 检查 Galileo 服务状态

