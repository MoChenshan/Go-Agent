# tRPC Agent HTTP Service

使用 tRPC-Go 框架创建的 Agent HTTP 服务示例。

## 功能特性

- **两个 HTTP 端点**: `/agent/run` (非流式) 和 `/agent/stream` (SSE流式)
- **对接 runner**: 直接调用 `runner.Run()` 接口
- **基于 tRPC-Go**: 使用 `http_no_protocol` 服务类型
- **会话支持**: 通过 session_id 维护对话状态
- **计算工具**: 内置简单的四则运算工具

## 🚀 快速开始

### 1. 环境准备

```bash
# 设置 API 凭证
export OPENAI_BASE_URL="https://api.lkeap.cloud.tencent.com/v1"
export OPENAI_API_KEY="your-api-key"
export MODEL_NAME="deepseek-v3-0324"
```

### 2. 启动服务

```bash
# 编译并运行
go mod tidy
go run main.go
```

服务将在 `http://127.0.0.1:8088` 启动。

### 3. 测试接口

**非流式调用**:
```bash
curl -X POST http://localhost:8088/agent/run \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Calculate 15 * 23", 
    "user_id": "user123", 
    "session_id": "session456"
  }'
```

**流式调用**:
```bash
curl -N -X POST http://localhost:8088/agent/stream \
  -H "Content-Type: application/json" \
  -d '{
    "message": "What is 10 + 5?", 
    "user_id": "user123", 
    "session_id": "session456"
  }'
```

## 📡 接口文档

### POST /agent/run (非流式)

**请求格式**:
```json
{
  "message": "用户输入内容",           // 必需：用户消息
  "user_id": "user123",             // 必需：用户ID
  "session_id": "session456"        // 可选：会话ID，不提供会自动生成
}
```

**响应格式**:
```json
{
  "content": "Agent 回复内容",
  "session_id": "session456",
  "done": true,
  "usage": {
    "prompt_tokens": 150,
    "completion_tokens": 50,
    "total_tokens": 200
  }
}
```

### POST /agent/stream (流式)

**请求格式**: 同上

**响应格式** (SSE流):

流式端点会发送结构化事件,包含 `type` 字段标识事件类型:

#### 事件类型

1. **工具调用事件** (`type: "tool_call"`):
```json
data: {
  "type": "tool_call",
  "tool_call": {
    "id": "call_xxx",
    "name": "calculator",
    "arguments": "{\"a\":15,\"b\":23,\"operation\":\"multiply\"}"
  },
  "session_id": "session456",
  "done": false
}
```

2. **工具响应事件** (`type: "tool_response"`):
```json
data: {
  "type": "tool_response",
  "tool_response": {
    "id": "call_xxx",
    "content": "{\"result\":345}"
  },
  "session_id": "session456",
  "done": false
}
```

3. **内容事件** (`type: "content"`):
```json
data: {
  "type": "content",
  "content": "15 乘以 23 的结果是 345。",
  "session_id": "session456",
  "done": false
}
```

4. **最终事件** (带usage):
```json
data: {
  "type": "content",
  "session_id": "session456",
  "done": true,
  "usage": {
    "prompt_tokens": 150,
    "completion_tokens": 50,
    "total_tokens": 200
  }
}
```

#### 客户端处理建议

```javascript
// JavaScript示例: 处理SSE流
const evtSource = new EventSource('/agent/stream');
evtSource.addEventListener('message', (event) => {
  const data = JSON.parse(event.data);
  
  switch(data.type) {
    case 'tool_call':
      console.log(`🔧 调用工具: ${data.tool_call.name}`);
      console.log(`   参数: ${data.tool_call.arguments}`);
      break;
      
    case 'tool_response':
      console.log(`✅ 工具响应: ${data.tool_response.content}`);
      break;
      
    case 'content':
      // 追加增量内容到UI
      appendToUI(data.content);
      if (data.done && data.usage) {
        console.log(`Token使用: ${data.usage.total_tokens}`);
      }
      break;
      
    case 'error':
      console.error(`❌ 错误: ${data.content}`);
      break;
  }
});
```

```python
# Python示例: 处理SSE流
import requests
import json

response = requests.post(
    'http://localhost:8088/agent/stream',
    json={'message': '计算15*23', 'user_id': 'user123'},
    stream=True
)

for line in response.iter_lines():
    if line.startswith(b'data: '):
        data = json.loads(line[6:])
        
        if data.get('type') == 'tool_call':
            print(f"🔧 调用工具: {data['tool_call']['name']}")
        elif data.get('type') == 'tool_response':
            print(f"✅ 工具响应: {data['tool_response']['content']}")
        elif data.get('type') == 'content':
            print(data.get('content', ''), end='', flush=True)
```


## 🔧 配置说明

### trpc_go.yaml 配置

```yaml
global:
  namespace: development
  env_name: test

server:
  service:
    - name: trpc.app.server.agent
      ip: 127.0.0.1
      port: 8088
      network: tcp
      protocol: http_no_protocol
      timeout: 60000
```

### 环境变量

- `OPENAI_BASE_URL`: LLM API 基础URL（OpenAI 客户端自动读取）
- `OPENAI_API_KEY`: LLM API 密钥（OpenAI 客户端自动读取）
- `MODEL_NAME`: 模型名称

**注意**: OpenAI Go 客户端会自动从环境变量读取 API 配置，无需在代码中显式传递。

## 🏗️ 架构设计

### 核心组件

```
HTTP Request → AgentService → runner.Run() → event.Event → HTTP Response
```

### 关键设计

1. **接口对齐**: HTTP 请求直接映射到 `runner.Run(ctx, userID, sessionID, message)`
2. **事件处理**: 从 `<-chan *event.Event` 提取内容和 Usage 信息
3. **错误处理**: 完整的错误传递和HTTP状态码处理
4. **会话管理**: 通过 session_id 维护对话上下文

## ⚠️ 重要：正确判断 Runner 结束

### Context 取消时序问题

**tRPC HTTP 服务会在请求结束后立即取消 context**，这会导致一个关键的时序问题：

- 如果使用 `event.IsFinalResponse()` 或直接判断 `event.Object == model.ObjectTypeChatCompletion` 来结束请求
- HTTP handler 会提前退出，context 被取消
- 而 runner 可能还在执行异步流程（如持久化 runner.completion 事件到 session）
- 导致这些异步操作因 context 取消而失败

### 正确的结束判断

**必须使用 `event.IsRunnerCompletion()` 来判断 runner 是否真正结束**：

```go
for event := range eventChan {
    // ... 处理事件 ...
    
    // ❌ 错误：使用 IsFinalResponse 或判断 chat.completion
    // if event.IsFinalResponse() || event.Object == model.ObjectTypeChatCompletion {
    //     break  // 这会导致 runner 的异步流程被中断
    // }
    
    // ✅ 正确：使用 IsRunnerCompletion
    if event.IsRunnerCompletion() {
        log.Infof("Runner completed")
        break  // 此时 runner 所有流程都已完成，可以安全退出
    }
}
```

### 两者的区别

| 方法 | 含义 | Runner 状态 | 适用场景 |
|------|------|------------|---------|
| `IsFinalResponse()` | 收到了最后一个 LLM 响应事件 | 可能还在执行异步流程<br>（emit completion、append session） | 判断内容是否完整 |
| `IsRunnerCompletion()` | Runner 流程完全结束 | 所有流程（包括异步持久化）都已完成 | 判断何时可以安全退出请求 |

### 时序问题说明

简单来说，执行顺序是这样的：

1. LLM 返回最后一个响应并发送到 channel
2. Runner 开始执行后续清理工作：创建 `runner.completion` 事件并持久化到 session（这可能是一个 Redis I/O 操作）
3. 持久化完成后，发送 `runner.completion` 事件到 channel

**问题**：如果服务端在第 1 步就 break 退出，HTTP 请求结束，context 被取消，导致第 2 步的持久化操作失败。

**解决**：等到收到 `runner.completion` 事件（第 3 步）再退出，此时所有异步操作都已完成。

### 流式端点最佳实践

```go
for event := range eventChan {
    // ... 处理和发送事件 ...
    
    if event.IsRunnerCompletion() {
        // 此时 runner 所有流程都已完成，包括：
        // - LLM 响应已完整
        // - 工具调用已完成
        // - Runner completion 事件已持久化到 session
        log.Infof("Completed streaming request")
        break
    }
}
```

## 🎯 高级用法

### 过滤工具调用事件

如果你的场景不需要向客户端暴露工具调用细节,可以在服务端过滤这些事件。

#### 方案1: 修改流式端点 - 过滤工具事件

```go
// 在 handleStream 函数中,注释掉工具事件的处理
for event := range eventChan {
    // ... 错误处理 ...
    
    /*
    // 注释掉工具调用事件
    if len(event.Choices) > 0 && len(event.Choices[0].Message.ToolCalls) > 0 {
        // 不发送工具调用事件
        continue
    }
    
    // 注释掉工具响应事件
    if event.Response != nil && len(event.Response.Choices) > 0 {
        // 不发送工具响应事件
        continue
    }
    */
    
    // 只发送内容事件
    content := extractContentFromEvent(event)
    if content != "" {
        streamResp := StreamResponse{
            Type:      "content",
            Content:   content,
            SessionID: req.SessionID,
            Done:      event.Done,
        }
        // ... 发送SSE事件 ...
    }
}
```

#### 方案2: 客户端过滤

客户端可以选择性忽略工具事件,只处理内容事件:

```javascript
evtSource.addEventListener('message', (event) => {
  const data = JSON.parse(event.data);
  
  // 只处理内容事件,忽略工具事件
  if (data.type === 'content') {
    appendToUI(data.content);
  }
});
```

#### 方案3: 添加配置开关

```go
type AgentService struct {
    runner             runner.Runner
    showToolEvents     bool  // 是否显示工具事件
}

func NewAgentService(showToolEvents bool) *AgentService {
    // ...
    return &AgentService{
        runner:         agentRunner,
        showToolEvents: showToolEvents,
    }
}

// 在 handleStream 中使用配置
if svc.showToolEvents && len(event.Choices) > 0 && len(event.Choices[0].Message.ToolCalls) > 0 {
    // 发送工具调用事件
}
```

### 工具调用流程说明

当 Agent 需要调用工具时,事件流顺序为:

1. **工具调用阶段**: `event.Choices[0].Message.ToolCalls` 包含工具调用信息
   - 此时 `Done=false`
   - 流式端点发送 `tool_call` 事件

2. **工具执行阶段**: 服务端执行工具,生成工具响应
   - 响应通过 `event.Response.Choices[]` 返回
   - `Role=RoleTool` 标识这是工具响应
   - 流式端点发送 `tool_response` 事件

3. **最终回复阶段**: LLM 根据工具结果生成回复
   - `Delta.Content` 包含增量内容
   - 流式端点发送 `content` 事件
   - 最后一个事件 `Done=true` 并带 `usage`

**非流式端点行为**: 等待所有阶段完成,只返回最终的回复内容,不包含中间的工具调用细节。

## 🔌 tRPC-Go 集成

### 调用示例

```go
// 使用 tRPC HTTP 客户端
client := thttp.NewClientProxy("trpc.app.server.agent")
response, err := client.Post(ctx, "/agent/run", request, response)
```

