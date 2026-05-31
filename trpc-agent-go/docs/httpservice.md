## tRPC-Go HTTP 服务示例

基于 tRPC-Agent-Go Runner 的轻量 HTTP 暴露方式。它不像 AG-UI / A2A / Debugserver 那样定义了固定协议，而是一个示例级实现，告诉你 Runner 需要哪些入参、会产出怎样的事件流，方便在已有的 tRPC-Go HTTP 服务中自行定制路径、请求体和返回格式。

示例代码目录：[examples/trpchttpservice](https://git.woa.com/trpc-go/trpc-agent-go/tree/master/examples/trpchttpservice)（包含 `main.go` 与 `trpc_go.yaml`）。

### 适用场景
- 你已经有一套 tRPC-Go `http_no_protocol` 服务，想最快把 Agent 能力挂到几个 HTTP 路由上。
- 只需演示 / 验证 Runner 的入参和输出形态，不想引入 UI、A2A 协议或调试器。
- 需要完全自定义路径（如 `/run` / `/runsse`）、请求字段或响应格式，按业务约定来定义。

### 快速跑通示例
```bash
cd examples/trpchttpservice
export OPENAI_BASE_URL="https://api.lkeap.cloud.tencent.com/v1"   
export OPENAI_API_KEY="your-api-key"
export MODEL_NAME="deepseek-v3-0324"                            
go run main.go
```

默认监听 `http://127.0.0.1:8088`（可在 `trpc_go.yaml` 修改 `server.service.port`）。

简单测试：
```bash
# 非流式
curl -X POST http://127.0.0.1:8088/agent/run \
  -H "Content-Type: application/json" \
  -d '{"message":"15*23","user_id":"u1","session_id":"s1"}'

# SSE 流式
curl -N -X POST http://127.0.0.1:8088/agent/stream \
  -H "Content-Type: application/json" \
  -d '{"message":"10+5?","user_id":"u1","session_id":"s2"}'
```

### 接口形态（示例，可按需改）
- **请求体**：`message`（必填，用户输入）、`user_id`（必填，对应 Runner 的 userID）、`session_id`（选填，未传则示例内自动生成）。
- **非流式响应**：`content`、`session_id`、`done:true`、`usage`（tokens 信息）。
- **流式 SSE 事件类型**（`type` 字段）：
  - `tool_call`：模型要求调用工具，携带 `name` 与 `arguments`。
  - `tool_response`：工具回包内容。
  - `content`：增量文本，最终事件附带 `done:true` 与 `usage`。
  - `error`：错误场景。

这些字段只是示例里的定义，你可以根据业务协议自由调整；核心是把请求转换为 Runner 需要的入参，事件再映射回你的返回格式。

示例里的请求/响应结构体定义：
```go
type AgentRunRequest struct {
    Message   string `json:"message"`
    UserID    string `json:"user_id"`
    SessionID string `json:"session_id,omitempty"`
}

type AgentResponse struct {
    Content   string `json:"content"`
    SessionID string `json:"session_id"`
    Done      bool   `json:"done"`
    Usage     *Usage `json:"usage,omitempty"`
}

type StreamResponse struct {
    Type         string      `json:"type,omitempty"`
    Content      string      `json:"content,omitempty"`
    ToolCallInfo *ToolCall   `json:"tool_call,omitempty"`
    ToolResponse *ToolResult `json:"tool_response,omitempty"`
    SessionID    string      `json:"session_id"`
    Done         bool        `json:"done"`
    Usage        *Usage      `json:"usage,omitempty"`
}
```

### Runner 对接要点
- 示例中的 `AgentRunRequest`、`StreamResponse` 仅用于演示，可以重命名或扩展，但需最终调用 `runner.Run(ctx, userID, sessionID, model.NewUserMessage(...))`。
- 事件处理逻辑演示了常见分支：
  - `event.Choices[].Message.ToolCalls` → 转成 `tool_call` 事件。
  - `event.Response.Choices` 中的 Tool 消息 → 转成 `tool_response` 事件。
  - 文本增量取 `choice.Delta.Content`，避免累积重复。
- 结束时使用 `event.IsRunnerCompletion()` + `event.Usage`，避免用 `IsFinalResponse` 过早退出（HTTP handler 结束会取消 ctx，可能打断 runner 的异步收尾如持久化 session）。
- Session 由示例内置的内存实现 `inmemory.NewSessionService()` 维护；可替换为你自己的持久化实现，但保持 `session_id` 的透传。

核心对接逻辑（非流式）：
```go
func (svc *AgentService) handleRun(w http.ResponseWriter, r *http.Request) error {
    var req AgentRunRequest
    json.NewDecoder(r.Body).Decode(&req)
    if req.SessionID == "" { req.SessionID = fmt.Sprintf("session-%d", time.Now().UnixNano()) }

    events, _ := svc.runner.Run(r.Context(), req.UserID, req.SessionID, model.NewUserMessage(req.Message))
    var content strings.Builder
    for e := range events {
        content.WriteString(extractContentFromEventNonStreaming(e)) // 用 Delta 避免重复
    }
    return json.NewEncoder(w).Encode(AgentResponse{Content: content.String(), SessionID: req.SessionID, Done: true})
}
```

核心对接逻辑（SSE）：
```go
func (svc *AgentService) handleStream(w http.ResponseWriter, r *http.Request) error {
    var req AgentRunRequest
    json.NewDecoder(r.Body).Decode(&req)
    if req.SessionID == "" { req.SessionID = fmt.Sprintf("session-%d", time.Now().UnixNano()) }

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    flusher := w.(http.Flusher)

    events, _ := svc.runner.Run(r.Context(), req.UserID, req.SessionID, model.NewUserMessage(req.Message))
    for e := range events {
        switch {
        case len(e.Choices) > 0 && len(e.Choices[0].Message.ToolCalls) > 0:
            // 发送 tool_call
        case e.Response != nil && hasToolResponse(e):
            // 发送 tool_response
        default:
            resp := StreamResponse{Type: "content", Content: extractContentFromEvent(e), SessionID: req.SessionID, Done: e.Done, Usage: copyUsage(e.Usage)}
            data, _ := json.Marshal(resp)
            fmt.Fprintf(w, "data: %s\n\n", data)
            flusher.Flush()
        }
        if e.IsRunnerCompletion() { break }
    }
    return nil
}
```

Agent & Runner 初始化（模型/工具/Session 均可替换）：
```go
func NewAgentService() *AgentService {
    sessionSvc := inmemory.NewSessionService()
    agent := llmagent.New(
        "trpc-agent",
        llmagent.WithModel(openai.New(getEnv("MODEL_NAME", "deepseek-v3-0324"))),
        llmagent.WithTools([]tool.Tool{calculatorTool}),
        llmagent.WithGenerationConfig(model.GenerationConfig{Stream: true}),
    )
    return &AgentService{runner: runner.NewRunner("trpc-agent-service", agent, runner.WithSessionService(sessionSvc))}
}
```

### 工具事件可见性（可选）
- 不希望前端看到工具细节时，可在流式 Handler 内直接跳过工具事件：
```go
if len(e.Choices) > 0 && len(e.Choices[0].Message.ToolCalls) > 0 {
    continue // 不发送 tool_call
}
if e.Response != nil && len(e.Response.Choices) > 0 && hasToolResponse(e) {
    continue // 不发送 tool_response
}
```
- 也可以通过 `showToolEvents` 之类的开关控制；客户端同样可以只处理 `type == "content"` 事件。

### 工具调用事件顺序
- 先收到 `tool_call`（模型要求调用工具，`Done=false`）。
- 再收到 `tool_response`（工具执行结果，通常 `RoleTool`）。
- 最后收到 `content` 增量，最终事件 `Done=true` 且附带 `usage`。
- 非流式端点会等待所有阶段完成，只返回最终内容。

### 融入现有 tRPC-Go HTTP 服务
- 使用 `http_no_protocol` 服务类型，在代码里用 `thttp.HandleFunc` 注册路由，再通过 `thttp.RegisterNoProtocolService(server.Service("xxx"))` 绑定到配置的 service name。
- 你可以直接在现有服务的 mux 里挂载 Handler（如 `/run`、`/runsse`），核心逻辑是从 HTTP 请求构造 Runner 入参，消费 Runner 事件流并写回 HTTP 响应 / SSE。
- 如需鉴权、限流、追踪等，可复用 tRPC-Go 的中间件；示例保留了最小可用路径，便于按需加代码。

路由注册示例：
```go
s := trpc.NewServer()
svc := NewAgentService()
thttp.HandleFunc("/agent/run", svc.handleRun)
thttp.HandleFunc("/agent/stream", svc.handleStream)
thttp.RegisterNoProtocolService(s.Service("trpc.app.server.agent"))
s.Serve()
```

### 注意事项
- 这是一个“示例实现”而非“协议标准”，前后端字段、路径完全由业务自定义，但建议保持 `user_id`、`session_id` 与 Runner 参数的对应关系。
- 若使用 SSE，确保返回头包含 `text/event-stream`、`Cache-Control: no-cache`，并在写入后 `Flush()`。
- 流式体验依赖模型配置 `Stream=true`（示例在 `GenerationConfig` 已开启）。
- 根据生产要求补齐错误处理、超时、重试、日志脱敏等逻辑。
