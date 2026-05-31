
# A2A 与 伽利略 可观测性集成示例

本示例演示如何在 tRPC-Agent-Go 框架下，非 tRPC 服务如何将 ** 伽利略-可观测平台** 集成到 Agent-to-Agent (A2A) 通信中，实现多智能体工作流的链路追踪与指标采集。

## Go 代码集成示例

### 配置说明
1. **环境变量**：
   ```bash
   export OPENAI_API_KEY="your-deepseek-api-key"
   export OPENAI_BASE_URL="https://api.deepseek.com/v1"
   `
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
)

// 设置初始化参数
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
    return gmetrics.Setup(*resv3, cfg)
}
```   

## 🧪 集成验证

集成完成后，您可以通过以下方法进行验证：

1. **检查日志**: 启动时应该看到成功的 Galileo 初始化日志
2. **追踪数据**: 检查 Galileo 平台上是否有追踪数据上报
3. **错误监控**: 确认没有相关的错误日志

## 📚 参考文档

- [Galileo GO (tRPC) 集成指南](https://iwiki.woa.com/p/4009274553)
- [Galileo GO (通用) 集成指南](https://iwiki.woa.com/p/4013979751)
- [Galileo SDK Tracer 实现文档](https://iwiki.woa.com/p/4012224483)
