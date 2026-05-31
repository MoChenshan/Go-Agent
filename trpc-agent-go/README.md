# tRPC-Agent-Go - 内网版本

这是 tRPC-Agent-Go 的内网版本，专为内网腾讯内部用户提供，所有内部用户都建议使用该版本.
提供了内网的生态如 tRAG，Taiji RAG，Venus， Taiji，hunyuan等内部平台提供的模型接口，Contex Window的适配，使用内网版本不容易踩坑。PCG123 的代码执行器，伽利略和智研监控上报，trpc server 基于 yaml 配置启动，各种数据库组件的北极星寻址功能等等。
欢迎给[github 版本](https://github.com/trpc-group/trpc-agent-go) 点 star！

## 核心组件

- **Agent**: 核心执行单元，负责处理用户输入并生成响应，支持LLMAgent,ChainAgent、ParallelAgent、CycleAgent，GraphAgent(对标langgraph)等
- **Runner**: Agent 的执行器，负责管理执行流程，串联 Session/Memory Service 等能力
- **Model**: 支持多种 LLM 模型（OpenAI、DeepSeek 等）
- **Tool**: 提供各种工具能力（Function、MCP、DuckDuckGo 等）
- **Session**: 管理用户会话状态和事件
- **Memory**: 记录用户的长期记忆和个性化信息
- **Knowledge**: 实现 RAG 知识检索能力
- **Skills**: 提供Cluade Skills能力 
- **Planner**: 提供 Agent 的计划和推理能力
- **Observability**: 提供监控、追踪和指标等可观测性能力

tRPC-Agent-Go的完整生态和功能请参考[功能生态列表](https://doc.weixin.qq.com/sheet/e3_AGkAxgZOAFMCNYHQ005CkSviP0PpE?scode=AJEAIQdfAAo4uRPJwPAO8AowbdAFw&tab=BB08J2)



## 可视化编排页面

- Graph编排对标Langgraph，可基于代码编排复杂AI工作流，参照 [Graph 流程编排](https://iwiki.woa.com/p/4015792386) 
- 如果你想快速基于页面编排，可以使用[Agent Builder](https://agui.woa.com/)，域名：https://agui.woa.com ，支持拖拽式编排多Agent，支持knowledge/Skill/Session/Memory/CodeExecutor等，
可以快速托管部署运营，也可以编排一键导出代码，方便快速产品原型验证。
- 在平台上可以部署tRPC-Claw，支持WebIDE，内网多种Skill集成。

## 概述

内网版本提供了与 GitHub 版本 (`trpc.group/trpc-go/trpc-agent-go`) 完全一致的 API。
你只需在项目中添加一次空白导入：

```go
// 务必导入该模块
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc"
```

## 环境配置

首先请点击以下链接，进行 go proxy 和 go sumdb 的设置（具体操作是把链接中的小眼睛点开，然后复制到 `~/.bashrc` 中，完成后记得执行 `source ~/.bashrc` 命令）:

[Goproxy for Tencent](https://goproxy.woa.com/)

接下来请执行 `go env` 命令查看输出结果，重点看以下 key 的值：

- `GOPROXY`: 要保证这个值是 [Goproxy for Tencent](https://goproxy.woa.com/) 网站上设置的值
- `GOSUMDB`: 要保证这个值是 [Goproxy for Tencent](https://goproxy.woa.com/) 网站上设置的值
- `GONOPROXY`: 要保证这个值里面不含 `git.code.oa.com`
- `GOPRIVATE`: 要保证这个值里面不含 `git.code.oa.com`
- `GONOSUMDB`: 要保证这个值里面不含 `git.code.oa.com`

假如以上有部分不相符，那么说明存在对应的系统环境变量覆盖了 `go env` 本身设置的值，可以考虑在 `~/.bashrc` 中这样写：

```shell
export GOPROXY=""
export GOSUMDB=""
export GONOPROXY=""
export GOPRIVATE=""
export GONOSUMDB=""

# 以下两行换成你自己访问 https://goproxy.woa.com/ 点开小眼睛后看到的三行
go env -w GOPROXY="https://goproxy.woa.com,direct"
go env -w GOSUMDB="sum.woa.com+643d7a06+Ac5f5VOC4N8NUXdmhbm8pZSXIWfhek5JSmWdWrq7pLX4"
# 绕过公司内网 goproxy 缓存，确保始终拉取最新依赖
go env -w GOPRIVATE="trpc.group,trpc.tech"
go env -w GONOSUMDB="trpc.group,trpc.tech"
go env -w GONOPROXY="trpc.group,trpc.tech"
```

执行 `source ~/.bashrc` 后再次执行 `go env` 命令，保证显示的值符合之前提到的要求。

## 环境变量配置

在使用 tRPC-Agent-Go 时，需要配置相关的环境变量来连接外部服务。建议将敏感信息（如 API Key）通过环境变量配置，而不是硬编码在代码中。

- `OPENAI_API_KEY`: OpenAI 或兼容 API 的 API 密钥
- `OPENAI_BASE_URL`: API 服务的基础 URL（可选，默认为 OpenAI 官方地址）

**环境变量配置**

```bash
export OPENAI_API_KEY="your-api-key"
export OPENAI_BASE_URL="https://api.deepseek.com"  # 可选，默认 https://api.openai.com/v1
```

除了环境变量，你也可以在代码中直接指定配置：

```go
modelInstance := openai.New("your-model-name",
    openai.WithAPIKey("your-api-key"),
    openai.WithBaseURL("your-base-url"),
)
```

## 安装

```bash
go get git.woa.com/trpc-go/trpc-agent-go
```

## 快速开始

### 使用trpc-go启动 AG-UI 服务（推荐）

AG-UI 提供了开箱即用的 Web 界面，支持流式对话、工具调用等功能，可直接集成到现有的 tRPC-Go 服务中。

#### 0. 配置环境变量

```bash
export OPENAI_API_KEY="your-api-key"
export OPENAI_BASE_URL="https://api.deepseek.com"  # 可选，默认 https://api.openai.com/v1
```

#### 1. 创建 main.go

```go
package main

import (
    "context"
    "fmt"
    "log"
    "math"

    "git.code.oa.com/trpc-go/trpc-go"
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"
    tagui "git.woa.com/trpc-go/trpc-agent-go/trpc/agui"
    "trpc.group/trpc-go/trpc-agent-go/agent"
    "trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
    "trpc.group/trpc-go/trpc-agent-go/model"
    "trpc.group/trpc-go/trpc-agent-go/model/openai"
    "trpc.group/trpc-go/trpc-agent-go/runner"
    "trpc.group/trpc-go/trpc-agent-go/server/agui"
    "trpc.group/trpc-go/trpc-agent-go/tool"
    "trpc.group/trpc-go/trpc-agent-go/tool/function"
)

func main() {
    agent := newAgent()
    runner := runner.NewRunner(agent.Info().Name, agent)
    
    // 加载配置文件，创建 trpc 服务
    server := trpc.NewServer()
    
    // 创建 AG-UI 服务
    aguiServer, err := agui.New(runner, agui.WithPath("/agui"))
    if err != nil {
        log.Fatalf("failed to create AG-UI server: %v", err)
    }
    
    // 将 AG-UI server 注册到 trpc service
    if err := tagui.RegisterAGUIServer(server, "trpc.test.helloworld.agui", aguiServer); err != nil {
        log.Fatalf("failed to register AG-UI server: %v", err)
    }
    
    // 启动 trpc 服务
    if err := server.Serve(); err != nil {
        log.Fatalf("server stopped with error: %v", err)
    }
}

func newAgent() agent.Agent {
    modelInstance := openai.New("deepseek-chat")
    generationConfig := model.GenerationConfig{
        MaxTokens:   intPtr(512),
        Temperature: floatPtr(0.7),
        Stream:      true,
    }
    calculatorTool := function.NewFunctionTool(
        calculator,
        function.WithName("calculator"),
        function.WithDescription("A calculator tool for basic arithmetic operations"),
    )
    agent := llmagent.New(
        "agui-agent",
        llmagent.WithTools([]tool.Tool{calculatorTool}),
        llmagent.WithModel(modelInstance),
        llmagent.WithGenerationConfig(generationConfig),
        llmagent.WithInstruction("You are a helpful assistant."),
    )
    return agent
}

func calculator(ctx context.Context, args calculatorArgs) (calculatorResult, error) {
    var result float64
    switch args.Operation {
    case "add", "+":
        result = args.A + args.B
    case "subtract", "-":
        result = args.A - args.B
    case "multiply", "*":
        result = args.A * args.B
    case "divide", "/":
        result = args.A / args.B
    case "power", "^":
        result = math.Pow(args.A, args.B)
    default:
        return calculatorResult{}, fmt.Errorf("invalid operation: %s", args.Operation)
    }
    return calculatorResult{Result: result}, nil
}

type calculatorArgs struct {
    Operation string  `json:"operation" description:"add, subtract, multiply, divide, power"`
    A         float64 `json:"a" description:"First number"`
    B         float64 `json:"b" description:"Second number"`
}

type calculatorResult struct {
    Result float64 `json:"result"`
}

func intPtr(i int) *int       { return &i }
func floatPtr(f float64) *float64 { return &f }
```

#### 2. 创建 trpc_go.yaml 配置文件

```yaml
global:
  namespace: Development
  env_name: test

server:
  app: test
  server: helloworld
  service:
    - name: trpc.test.helloworld.agui
      ip: 127.0.0.1
      port: 8080
      network: tcp
      protocol: http_no_protocol

plugins:
  log:
    default:
      - writer: console
        level: debug
```

#### 3. 运行服务

```bash
go run .
```

服务将在 `http://127.0.0.1:8080/agui` 提供 AG-UI HTTP 服务，示例请参考 [examples/agui/server/default](./examples/agui/server/default/)。

你可以使用 AG-UI 客户端 [examples/agui/client](./examples/agui/client) 访问 AG-UI 服务。

其他对外接入方式请参考：
[A2A Server](./examples/a2aagent/trpc/polaris/)

[OpenAI Server](./examples/openaiserver/trpc/)

[WeCom AI Bot Server](./examples/wecom/)

详细说明见 [docs/wecom.md](./docs/wecom.md)。

#### 4. 使用 trpc agent 命令快速生成项目（推荐）

如果你已安装 `trpc-go-cmdline`（版本不低于 `v2.9.3`），可以使用 `trpc agent` 命令快速生成 Agent 工程骨架，无需手动编写上述代码。

**安装 trpc 命令**

请参考 [trpc agent 命令行工具](https://iwiki.woa.com/p/4017202408) 配置环境并安装 `trpc` 命令。

**生成 AGUI 服务工程**

```bash
trpc agent -o my-agui-agent --server agui
cd my-agui-agent
go mod tidy
go run .
```

**生成带工具的 Agent 工程**

```bash
trpc agent -o calculator-agent --tool calculator.proto --server agui
cd calculator-agent
go mod tidy
go run .
```

**生成带运维体系的工程（可观测性集成）**

```bash
# 使用 PCG123 运维体系（伽利略监控平台）
trpc agent -o my-agent --server agui --opsys pcg123

# 使用智研运维体系
trpc agent -o my-agent --server agui --opsys zhiyan
```

更多选项请执行 `trpc help agent` 查看，或参考 [trpc agent 文档](https://git.woa.com/trpc-go/trpc-go-cmdline/blob/master/README_AGENT.md)。

#### 5. 接入 LLM 监控（强烈建议）

为了更好地监控 Agent 应用的运行状态，建议接入以下 LLM 可观测平台。这些平台可以自动采集 LLM 调用的关键指标，如调用次数、延迟、Token 消耗、错误率等，帮助你及时发现和解决问题。

##### 伽利略监控

**1) 在 main.go 中添加导入：**

```go
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo"
```

**2) 在 `trpc_go.yaml` 中添加伽利略配置：**

**3) 详细[example](./examples/agui/server/galileo/)：**

##### 智研监控

**1) 在 main.go 中添加导入：**

```go
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan"
```

**2) 在`trpc_go.yaml` 中添加智研配置：**


**3) [智研trpc-go插件使用方式](./examples/telemetry/zhiyan/trpc-plugin/)**

更多 LLM 监控例子请参考：
- [可观测性文档](./docs/observability.md)


### 使用其他模型体验

如果你没有 DeepSeek 官方的 API Key，可以使用内网 LLM 平台，例如[腾讯太极机器学习平台](https://taiji.woa.com/)。平台包 `trpc/model/taiji` 封装了平台专用的配置选项，避免手动设置 `extra_fields`。

以下示例展示如何使用 `taiji.NewOpenAI` 创建模型并构建一个支持工具调用的 Agent。

```go
package main

import (
    "context"
    "fmt"
    "log"
    "math"

    // 启用内网增强，务必导入该模块
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"

    // 导入太极平台包
    "git.woa.com/trpc-go/trpc-agent-go/trpc/model/taiji"

    // 其余依赖仍使用 GitHub 版本
    "trpc.group/trpc-go/trpc-agent-go/agent"
    "trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
    "trpc.group/trpc-go/trpc-agent-go/model"
    "trpc.group/trpc-go/trpc-agent-go/runner"
    "trpc.group/trpc-go/trpc-agent-go/tool"
    "trpc.group/trpc-go/trpc-agent-go/tool/function"
)

func main() {
    // 创建太极平台模型，开启 openai_infer 和 tool_choice 功能
    modelInstance := taiji.NewOpenAI("DeepSeek-V3_2-Online-32k",
        taiji.WithHTTPClientName("trpc.test.llm.openai"), // 对应 trpc_go.yaml 客户端配置
        taiji.WithOpenAIInfer(true),                     // 可选：启用 openai_infer
        taiji.WithToolChoice(),                          // 可选：启用工具调用，注意启用此选项必须给 Agent 传 tools
        taiji.WithThinking(true),                        // 可选：开启思考模式
    )

    // 创建计算工具
    calculatorTool := function.NewFunctionTool(
        calculator,
        function.WithName("calculator"),
        function.WithDescription("A calculator tool for basic arithmetic operations"),
    )

    // 构建 LLM Agent
    agent := llmagent.New(
        "taiji-agent",
        llmagent.WithTools([]tool.Tool{calculatorTool}),
        llmagent.WithModel(modelInstance),
        llmagent.WithGenerationConfig(model.GenerationConfig{
            MaxTokens:   intPtr(512),
            Temperature: floatPtr(0.7),
            Stream:      true,
        }),
        llmagent.WithInstruction("You are a helpful assistant."),
    )

    // 创建 Runner 并执行对话
    runner := runner.NewRunner(agent.Info().Name, agent)
    ctx := context.Background()

    // 示例对话
    result, err := runner.Run(ctx, "What is 15 plus 27?")
    if err != nil {
        log.Fatalf("Run failed: %v", err)
    }
    fmt.Printf("Assistant: %s\n", result.Text)
}

// 工具函数实现
func calculator(ctx context.Context, args calculatorArgs) (calculatorResult, error) {
    var result float64
    switch args.Operation {
    case "add", "+":
        result = args.A + args.B
    case "subtract", "-":
        result = args.A - args.B
    case "multiply", "*":
        result = args.A * args.B
    case "divide", "/":
        result = args.A / args.B
    case "power", "^":
        result = math.Pow(args.A, args.B)
    default:
        return calculatorResult{}, fmt.Errorf("invalid operation: %s", args.Operation)
    }
    return calculatorResult{Result: result}, nil
}

type calculatorArgs struct {
    Operation string  `json:"operation" description:"add, subtract, multiply, divide, power"`
    A         float64 `json:"a" description:"First number"`
    B         float64 `json:"b" description:"Second number"`
}

type calculatorResult struct {
    Result float64 `json:"result"`
}

func intPtr(i int) *int       { return &i }
func floatPtr(f float64) *float64 { return &f }
```

该示例展示了如何：
1. 使用 `taiji.NewOpenAI` 创建已配置好的模型实例。
2. 启用太极平台所需的 `openai_infer` 和 `tool_choice` 选项。
3. 绑定 tRPC HTTP 客户端配置，以利用北极星寻址、监控等治理能力。
4. 构建支持工具调用的 Agent 并运行对话。

更多平台参数与用法示例请参考 [docs/model.md](./docs/model.md)。

### 与 GitHub 版本的对比

提供了内网的生态如 tRAG，Taiji RAG，PCG123 的代码执行器，伽利略和智研监控上报，trpc server 基于 yaml 配置启动，各种数据库组件北极星寻址功能。

## 特性

- ✅ **API 兼容性**：与 GitHub 版本 100% 兼容
- ✅ **内网优化**：集成内网监控和统计上报
- ✅ **无缝迁移**：仅需添加一次空白导入即可启用
- ✅ **完整功能**：支持所有 GitHub 版本的功能和工具

## 示例

完整的使用示例请参考 [examples/runner](./examples/runner/main.go)，该示例展示了：

- 多轮对话
- 工具调用
- 流式输出
- 会话管理

基于 Graph 的流程编排示例请参考 [examples/graph](./examples/graph/main.go)

### 推荐用法：使用 `trpc/model` 平台包（太极 / 混元）

当你需要对接内网 LLM 平台（例如太极、混元）时，推荐使用 `git.woa.com/trpc-go/trpc-agent-go/trpc/model/...` 平台包。
平台包内部已包含 tRPC 注入，同时把平台私有字段封装成 Option，避免业务方手写 `extra_fields`。
不同平台通过不同的 BaseURL 进行区分。例如，太极平台使用 `http://api.taiji.woa.com/openapi`，
混元平台使用 `http://hunyuanapi.woa.com/openapi/v1`，Venus平台使用 `http://v2.open.venus.oa.com/llmproxy` 等。

- **版本要求**：`git.woa.com/trpc-go/trpc-agent-go` 版本需不低于 `v1.2.0`。
- **优点**：平台包内部已包含注入，同时把平台私有字段封装成 Option（例如太极的 `openai_infer`、`tool_choice`，混元的 `enable_thinking`）。
- **可用平台包**：`trpc/model/taiji`、`trpc/model/hunyuan`。
- **HTTP 配置**：`WithHTTPClientName` 用于绑定 `trpc_go.yaml` 的 `client.service.name`；`WithHTTPClientTransport` 可注入自定义 transport。

完整的使用示例请参考上一节 [使用其他模型体验](#使用其他模型体验)，其中展示了如何通过 `taiji.NewOpenAI` 创建模型并构建支持工具调用的 Agent。

以下为混元平台的简单示例：

```go
import (
    // 启用内网增强，务必导入该模块
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"

    // 导入混元平台包
    "git.woa.com/trpc-go/trpc-agent-go/trpc/model/hunyuan"
)

llm := hunyuan.NewOpenAI("hunyuan-a13b",
    hunyuan.WithBaseURL("http://hunyuanapi.woa.com/openapi/v1"),
    hunyuan.WithHTTPClientName("trpc.test.llm.hunyuan"),
    hunyuan.WithDisableThinking(), // 可选：关闭思考模式
)
```

更多平台参数与用法示例见 [docs/model.md](./docs/model.md)。

### LLM 客户端与服务端集成

如果你想在业务进程里直接调用 LLM，同时复用 tRPC-Go 的治理能力（超时、重试、连接池、拦截器、观测等），可以参考：

- [examples/trpchttpclient](./examples/trpchttpclient)：展示如何通过 `model/openai` 直接调用 LLM，并通过 tRPC-Go 的 HTTP 客户端（`thttp.Client`）发送请求，配合 `trpc_go.yaml` 的 `client.service` 配置实现统一治理。
  - 支持流式与非流式调用
  - 通过 `openai.WithHTTPClientName()` 绑定 tRPC 客户端配置
  - 适合"裸用 model、借力 tRPC 治理"的场景

如果你想把 Agent 封装成 HTTP 服务，对外提供标准的 Agent 接口（非流式 + SSE 流式），可以参考：

- [examples/trpchttpservice](./examples/trpchttpservice)：展示如何基于 tRPC-Go 框架启动 HTTP 服务，提供 `/agent/run` 和 `/agent/stream` 两个端点，内部对接 `runner.Run()` 接口。
  - 提供标准 HTTP 接口（非流式 + SSE 流式）
  - 支持会话管理（`session_id`）
  - 内置工具调用示例（计算工具）
  - 适合"把 Agent 作为独立服务提供出去"的场景

## 从 GitHub 示例迁移到内网版本

如果你的项目已经使用 GitHub 版本，只需执行下面一步即可完成迁移：

```go
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc"
```

添加后无需修改任何其他代码，即可自动启用内网增强组件。

比如 github 的 `model/openai` 包中使用的 HTTP 客户端会自动替换为内网 trpc 的 thttp 客户端，自动支持使用 `trpc_go.yaml` 客户端配置以及北极星等能力，比如：

```go

import _ "git.woa.com/trpc-go/trpc-agent-go/trpc"
import "trpc.group/trpc-go/trpc-agent-go/model/openai"

// 假设 OPENAI_BASE_URL 配置为 http://some-domain.com/openai
// 以下代码会自动使用 trpc 的 thttp 客户端：
model := openai.New("deepseek-chat")
// 假设 OPENAI_BASE_URL 配置为 https://some-domain.com/openai
// 以下代码会自动使用 trpc 的 thttp 客户端，自动启用 TLS：
model := openai.New("deepseek-chat")
// 假设 OPENAI_BASE_URL 配置为 ip://some-domain.com/openai
// 以下代码会自动使用 trpc 的 thttp 客户端，使用指定 ip 的模式：
model := openai.New("deepseek-chat")
// 假设 OPENAI_BASE_URL 为 polaris://some-service-name
// 以下代码会自动使用北极星 WithTarget 模式的 trpc thttp 客户端：
model := openai.New("deepseek-chat")
// 通过指定 http client name 可以自动使用该 name 来使用 `trpc_go.yaml` 中的客户端配置,
// 自动使用 trpc_go.yaml 中的 some-http-client-name 配置:
model := openai.New("deepseek-chat", openai.WithHTTPClientOptions(openai.WithHTTPClientName("some-http-client-name")))
```

如果使用了依赖数据库的组件（如 knowledge、memory、session 等），只需额外引入相应的包即可自动接入 tRPC 数据库插件生态，支持 trpc_go.yaml 配置、监控等能力。

目前支持的数据库组件：

| 数据库 | 导入路径 | 适用组件 | trpc-database 插件 |
|--------|----------|----------|-------------------|
| Redis | `git.woa.com/trpc-go/trpc-agent-go/trpc/storage/redis` | session、memory | [trpc-database/goredis](https://git.woa.com/trpc-go/trpc-database/tree/master/goredis) |
| PostgreSQL | `git.woa.com/trpc-go/trpc-agent-go/trpc/storage/postgres` | session、memory、knowledge (pgvector) | [trpc-database/postgres](https://git.woa.com/trpc-go/trpc-database/tree/master/postgres) |
| MySQL | `git.woa.com/trpc-go/trpc-agent-go/trpc/storage/mysql` | session、memory | [trpc-database/mysql](https://git.woa.com/trpc-go/trpc-database/tree/master/mysql) |
| Elasticsearch | `git.woa.com/trpc-go/trpc-agent-go/trpc/storage/goes` | knowledge | [trpc-database/goes](https://git.woa.com/trpc-go/trpc-database/tree/master/goes) |
| TCVectorDB | `git.woa.com/trpc-go/trpc-agent-go/trpc/storage/tcvector` | knowledge | [trpc-database/tcvectordb](https://git.woa.com/trpc-go/trpc-database/tree/master/tcvectordb) |

```go
// Redis (session/memory)
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/redis"

// PostgreSQL (session/memory/pgvector)
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/postgres"

// MySQL (session/memory)
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/mysql"

// Elasticsearch (knowledge)
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/goes"

// TCVectorDB (knowledge)
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/tcvector"
```

相关示例参考：
+ knowledge 组件接入 tRPC 生态示例: [examples/knowledge/trpc/vectorstores](./examples/knowledge/trpc/vectorstores)
+ session 组件接入 tRPC 生态示例(redis session): [examples/session/trpc](./examples/session/trpc)


## 依赖关系

```
内网版本：git.woa.com/trpc-go/trpc-agent-go
    └── GitHub 版本：trpc.group/trpc-go/trpc-agent-go
        ├── Agent 组件
        ├── Model 组件
        ├── Tool 组件
        ├── Session 组件
        └── Knowledge 组件

```

## 注意事项

1. **仅需空白导入**：在 import 块加上
   `_ "git.woa.com/trpc-go/trpc-agent-go/trpc"` 即可启用内网增强。
2. **功能无差异**：内网版本提供与 GitHub 版本完全相同的功能。
3. **内网增强**：提供北极星，Taiji，Venus，Hunyuan等内网平台集成功能

## 整体架构

tRPC-Agent-Go 是 tRPC 的 Agent 实现，用于在 tRPC 框架中实现 Agent 功能。

组件关系图：

![overview2](./docs/img/overview2.svg)

其中 Runner 是运行 Agent 的核心组件，建议以 Runner 为入口进行 Agent 的执行。

原始 drawio 见 [docs/img/overview2.drawio.xml](./docs/img/overview2.drawio.xml)

时序图：

![exec_flow](./docs/img/exec_flow.png)

原始 PlantUML 见 [docs/img/exec_flow.puml](./docs/img/exec_flow.puml)

## 详细文档

### 核心组件

- [Agent 使用指南](https://iwiki.woa.com/p/4015773536) - Agent 的创建、配置和执行
- [Runner 使用指南](https://iwiki.woa.com/p/4015773576) - 推荐的使用方式，集成 Session/Memory 等服务
- [Model 模型系统](https://iwiki.woa.com/p/4015773853) - 支持多种 LLM 模型（OpenAI、DeepSeek 等）
- [Tool 系统](https://iwiki.woa.com/p/4015773568) - 各种工具的使用和自定义
- [Session 管理](https://iwiki.woa.com/p/4015773606) - 会话状态管理和事件记录
- [Memory 服务](https://iwiki.woa.com/p/4015773627) - 用户记忆和个性化信息管理
- [Knowledge 知识库](https://iwiki.woa.com/p/4015773696) - RAG 知识检索实现
- [Skill 技能系统](https://iwiki.woa.com/p/4016637290) - Claude Skills的实现

### 高级功能

- [多 Agent 系统](https://iwiki.woa.com/p/4015773672) - 多 Agent 协同工作
- [Graph 流程编排](https://iwiki.woa.com/p/4015792386) - 可视化工作流设计和执行
- [Planner 规划器](https://iwiki.woa.com/p/4015773688) - Agent 的计划和推理能力
- [Event 事件系统](https://iwiki.woa.com/p/4015814247) - 事件流处理和实时响应

### 部署和调试

- [调试服务](https://iwiki.woa.com/p/4015773678) - Agent 调试和前端对接
- [A2A 集成](https://iwiki.woa.com/p/4015812269) - Agent-to-Agent 协议支持
- [WeCom AI Bot 接入](./docs/wecom.md) - 企业微信 AI Bot
  websocket 接入与示例说明
- [可观测性](https://iwiki.woa.com/p/4015773654) - 监控、追踪和指标

### 架构和生态

- [整体架构介绍](./docs/overall-introduction.md) - 框架整体设计和架构说明
- [生态建设](https://iwiki.woa.com/p/4015825174) - 组件生态和自定义实现

## 更多资源

- [GitHub 版本文档](https://github.com/trpc-group/trpc-agent-go)
- [示例代码](./examples/)
- [API 参考](https://pkg.go.dev/trpc.group/trpc-go/trpc-agent-go)
