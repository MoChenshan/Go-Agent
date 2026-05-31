
# 智研-监控宝-LLM应用性能分析

trpc-agent-go 使用 OpenTelemetry 采集链路追踪数据，并支持将追踪数据导出到智研-监控宝-LLM应用性能分析。









## Go 代码集成示例

### 第一步：配置智研环境变量

```bash
export ZHIYANLLM_API_ENDPOINT="https://trace.zhiyan.tencent-cloud.net:4318"
export ZHIYANLLM_API_KEY="key-xxxx"
export ZHIYANLLM_APP_NAME="llm-trpc-go-server"
```

### 第二步：编写代码

```go
import (
	"context"
	"log"
	
	zhiyanllm "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan-llm"
)

func main() {
    _, err := zhiyanllm.Start(context.Background())
    if err != nil {
        log.Fatal(err)
	}
	// other code 
}
```


## 运行代码

完整示例代码见 [main.go](./main.go)。执行：

```bash
go run .
```

本示例模拟了一个智能体应用，处理一系列用户消息，演示了多工具任务下的链路追踪与指标采集。

## 查看追踪数据

在监控宝-LLM应用性能分析-链路查询上查看：

![zhiyan-trace-overall](../../../../docs/img/telemetry/zhiyan-sdk-trace-overall.png)

具体的 trace 展示类似下面的效果：
![zhiyan-trace-datil](../../../../docs/img/telemetry/zhiyan-sdk-trace-detail.png)

