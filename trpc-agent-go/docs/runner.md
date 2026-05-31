## 🏢 内部接入

通过空白导入即可启用所有内部增强功能：

```go
// 只需一次导入，自动启用内部增强
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc"

// 其余代码与外网版本完全一致
import "trpc.group/trpc-go/trpc-agent-go/runner"
```

**自动获得的能力：**

- ✅ 内网 HTTP 客户端（支持北极星、服务发现）
- ✅ 监控和链路追踪集成
- ✅ 配置文件支持（`trpc_go.yaml`）
- ✅ 太极平台适配（`WithExtraFields`）

**注意**：如果拉取不到外网依赖，可以尝试添加以下环境变量：

```bash
export GONOPROXY="trpc.group,github.com"
export GOPRIVATE="trpc.group" 
export GONOSUMDB="trpc.group,github.com"
```

### 超时控制

- 默认行为：内部 HTTP 适配器对 GET/POST 使用 `client.WithTimeout(0)`，避免 LLM 流式被全局较小超时切断。
- 推荐做法：
  - 在业务侧用 `context.WithTimeout` 控制每次 Agent/LLM 调用的上限（例如 30–120s，视场景而定）。
  - 对 OpenAI 兼容 SDK，使用请求级超时：
    - `openaiopt.WithRequestTimeout(30*time.Second)`（`github.com/openai/openai-go/option`）。

### 事件流消费与取消

- 不要仅以 `event.Done == true` 判定“流程结束”。在工具调用、子 Agent 切换等阶段，可能出现中间事件 `Done=true`，但并未达到最终的 Runner 结束事件。
- 正确做法是仅在收到 Runner 完成事件时结束循环：

```go
for event := range eventChan {
    if event.Error != nil {
        // 记录错误后可选择继续或中止
        log.Printf("error: %s", event.Error.Message)
    }
    // 处理流式增量输出 ...

    if event.Done && event.Response != nil &&
        event.Response.Object == model.ObjectTypeRunnerCompletion {
        break // 仅在 RunnerCompletion 时退出
    }
}
```

- 症状与根因：若在中途 `break` 退出循环，上层 RPC/HTTP 可能率先返回并 `cancel` ctx，Agent 仍在后台运行，随后工具或二次提问阶段会收到 `context canceled`，日志常见：
  - `CallableTool ...: context canceled`
  - `Flow context canceled ... exiting without error`
  - 或 `Timeout waiting for completion of event ...`（等待下游完成，但上游已取消）。

### 服务发现支持

```go
// 自动支持多种服务发现方式
// polaris://service-name  -> 北极星服务发现
// ip://host:port         -> 直连 IP
// dns://domain.com       -> DNS 解析

import (
	"git.code.oa.com/trpc-go/trpc-go/client"
)

sessionService, err := redis.NewService(
    redis.WithRedisClientURL("polaris://redis-service"),
    redis.WithExtraOptions(
        // 通过 extra options 指定对节点的 namespace 等信息
        client.WithNamespace("Development"),
    ),
)
```

### 太极平台适配

```go
// 内网版本专有的 WithExtraFields 选项
taijiModel := openai.New("deepseek-chat",
    openai.WithExtraFields(map[string]interface{}{
        "openai_infer": true,
        "tool_choice":  "auto",
    }))
```
