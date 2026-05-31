# tRPC LLM Client

使用 trpc-agent-go 和 tRPC-Go 框架创建的多平台 LLM 客户端示例，支持 Tool Calling。

## 功能特性

- **多平台支持**：支持 Taiji、Hunyuan 和外部开源 OpenAI 兼容平台
- **tRPC HTTP 客户端集成**：通过 tRPC 配置管理 HTTP 连接，支持 Polaris 服务发现
- **直接 LLM 调用**：使用 trpc-agent-go 的 model 接口
- **流式响应**：支持实时流式输出
- **Tool Calling**：内置 calculator 工具示例，演示完整的 tool-call 循环
- **命令行参数**：通过 `-model` 指定模型名称
- **配置管理**：使用 `trpc_go.yaml` 进行配置

## 支持的平台

### 1. Taiji 平台（推荐用于 DeepSeek 模型）

- 自动 tRPC HTTP 客户端配置
- 支持 `openai_infer`、`tool_choice`、`thinking` 等平台特性
- 完整的 Polaris 服务发现和监控支持

### 2. Hunyuan 平台（推荐用于 Hunyuan 模型）

- 相同的 tRPC 生态优势
- 支持平台特定的 thinking 模式

### 3. 外部开源 OpenAI 兼容包

- 直接使用 GitHub 开源包
- 手动配置 BaseURL 和 API Key
- 适合外部服务集成

## 快速开始

### 1. 环境变量

```bash
export OPENAI_API_KEY="your-api-key"
export OPENAI_BASE_URL="https://your-llm-service.com/v1"  # 可选
```

### 2. 运行

```bash
cd examples/trpchttpclient
go run . -model deepseek-v3.2
```

支持的命令行参数：

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-model` | `deepseek-chat` | 模型名称 |

### 3. 使用

启动后输入消息进行对话，支持普通对话和工具调用：

```
🚀 tRPC LLM Client starting...
🤖 Model: deepseek-v3.2
🔄 Streaming mode: enabled
💬 Enter your message (or 'quit' to exit):

> 计算 123 * 321
🤖 Calling LLM model: deepseek-v3.2
📝 Message: 计算 123 * 321

🤖 我将帮您计算 123 * 321。

📊 Tokens: 516
🔧 Tool calls: 1
   • calculator({"a": 123, "b": 321, "operation": "multiply"})
⚡ Executing calculator ...
✅ Result: {"result":39483}

🤖 123 × 321 = 39,483
📊 Tokens: 548

> 你是谁
🤖 我是DeepSeek，由深度求索公司创造的AI助手。
📊 Tokens: 461
```

## 配置

### trpc_go.yaml

```yaml
client:
  service:
    - name: trpc.test.llm.openai
      target: ""
      timeout: 60000
      protocol: http
```

### 平台配置

修改 `newModel()` 函数中的代码来选择不同的平台：

```go
// 使用 Taiji 平台
taiji.NewOpenAI("DeepSeek-V3_1-Online-32k",
    taiji.WithHTTPClientName("trpc.test.llm.openai"),
    taiji.WithOpenAIInfer(true),
    taiji.WithToolChoice(),
    taiji.WithThinking(true),
)

// 使用 Hunyuan 平台
hunyuan.NewOpenAI("hunyuan-a13b",
    hunyuan.WithHTTPClientName("trpc.test.llm.hunyuan"),
    hunyuan.WithThinking(),
)

// 使用外部开源包（当前默认）
openai.New(*flagModel,
    openai.WithHTTPClientOptions(
        openai.WithHTTPClientName("trpc.test.llm.openai"),
    ),
)
```

## 架构说明

### 核心组件

- **`newModel()`**：演示不同平台的模型创建方式
- **`buildTools()`**：使用 `function.NewFunctionTool` 构建工具，通过结构体 tag 自动生成 JSON Schema
- **`callLLM()`**：执行 LLM 调用，包含完整的 tool-call 循环（最多 10 轮）
- **`collectStreamResponse()`**：收集流式响应，组装 assistant 消息（含文本和 tool calls）
- **`executeTool()`**：查找并执行工具，将结果序列化后返回给模型

### Tool Calling 流程

```
User Message + Tools
        |
        v
    LLM Model  ──(text response)──> Done
        |
   (tool_calls)
        |
        v
  Execute Tools
        |
        v
  Tool Results ──> Append to messages ──> LLM Model (next round)
```

### 自定义工具

通过具体的输入/输出结构体类型创建工具，框架自动生成 JSON Schema：

```go
type myInput struct {
    Param string `json:"param" jsonschema:"description=..."`
}
type myOutput struct {
    Result string `json:"result"`
}

myTool := function.NewFunctionTool(
    func(ctx context.Context, in myInput) (myOutput, error) {
        // ...
    },
    function.WithName("my_tool"),
    function.WithDescription("My tool description."),
)
```

注意：函数签名必须使用具体结构体类型，不能使用 `[]byte` / `any`，否则会导致反射生成 schema 时 panic。
