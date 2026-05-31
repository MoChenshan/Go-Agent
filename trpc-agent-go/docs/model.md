## 内部平台接入

以下是各平台的配置示例，分为环境变量配置和代码配置两种方式：

**环境变量配置**

```bash
# 腾讯云平台
export OPENAI_BASE_URL="https://api.lkeap.cloud.tencent.com/v1"
export OPENAI_API_KEY="your-api-key"
cd examples/runner
go run main.go -model hunyuan-lite

# Venus平台
export OPENAI_BASE_URL="http://v2.open.venus.oa.com/llmproxy"
export OPENAI_API_KEY="your-venus-api-key"
cd examples/runner
go run main.go -model claude-3-opus

# 混元模型
export OPENAI_BASE_URL="http://hunyuanapi.woa.com/openapi/v1"
export OPENAI_API_KEY="your-hunyuan-api-key"
cd examples/runner
go run main.go -model hunyuan-lite

# 使用无极平台
export OPENAI_BASE_URL="http://wujipt.woa.com/api/v1"
export OPENAI_API_KEY="your-wuji-api-key"
cd examples/runner
go run main.go -model deepseek-chat

# 太极平台（需要额外字段支持工具调用）
# 注意：太极平台有两个 API 版本：
#   - /openapi     — 适用于大部分模型（如 DeepSeek 系列、GLM 系列等）
#   - /openapi/v2  — 适用于 hy3-preview 等新一代混元模型
# 如果调用 hy3-preview 等模型报错，请切换到 /openapi/v2。
export OPENAI_BASE_URL="http://api.taiji.woa.com/openapi"
export OPENAI_API_KEY="your-taiji-api-key"
# 以下域名，仅供测试环境调试使用，不保证稳定性，太极表示已不再维护
# 详见 https://iwiki.woa.com/p/4013654342
# export OPENAI_BASE_URL="http://taiji-stream-server-online-openapi.turbotke.production.polaris:8080/openapi" # 办公网
# export OPENAI_BASE_URL="https://taiji-stream-server-online-openapi.turbotke.production.polaris:80" # 办公网，注意是https
# export OPENAI_BASE_URL="http://taiji-stream-server-online-openapi.turbotke.production.polaris:81" # IDC 可用
# export OPENAI_BASE_URL="http://taiji-stream-server-online-openapi.turbotke.production.polaris:1081" # DevCloud 可用
cd examples/runner
go run main.go -model deepseek-chat -platform taiji  # runner 示例中使用platform参数自动添加额外字段

# 太极平台（hy3-preview 等新一代混元模型，需使用 /openapi/v2）
export OPENAI_BASE_URL="http://api.taiji.woa.com/openapi/v2"
export OPENAI_API_KEY="your-taiji-api-key"
cd examples/runner
go run main.go -model hy3-preview -platform taiji

# 太极平台（手动配置额外字段方式）
export OPENAI_BASE_URL="http://api.taiji.woa.com/openapi"
export OPENAI_API_KEY="your-taiji-api-key"
# 实际在代码中添加：
# openai.WithExtraFields(map[string]any{
#     "openai_infer": true,
#     "tool_choice":  "auto",
# })
```

**代码配置方式**

在自己的代码中直接使用 Model 时的配置方式。

**推荐方式：使用内部平台模型适配包**

优先推荐使用 `trpc/model` 内部平台模型适配包，它会自动处理平台特殊字段，避免手写 `extra_fields`。详细说明请参考 [推荐用法：使用 `trpc/model` 内部平台模型适配包](#推荐用法使用-trpcmodel-内部平台模型适配包)。

```go
// 太极平台（推荐使用内部平台模型适配包）
import "git.woa.com/trpc-go/trpc-agent-go/trpc/model/taiji"

deepseekModel := taiji.NewOpenAI("DeepSeek-V3_2-Online-32k",
    taiji.WithBaseURL("http://api.taiji.woa.com/openapi"),
    taiji.WithAPIKey("your-api-key"),
    taiji.WithHTTPClientName("trpc.test.llm.openai"),
    taiji.WithOpenAIInfer(true),   // 可选：启用 openai_infer 模式
    taiji.WithToolChoice(),        // 可选：启用工具调用
    taiji.WithThinking(true),      // 可选：启用 DeepSeek V3.1/V3.2 思考模式
)

// 太极平台 hy3-preview（注意：需使用 /openapi/v2）
hy3Model := taiji.NewOpenAI("hy3-preview",
    taiji.WithBaseURL("http://api.taiji.woa.com/openapi/v2"),
    taiji.WithAPIKey("your-api-key"),
    taiji.WithHTTPClientName("trpc.test.llm.openai"),
)

// 混元平台（推荐使用内部平台模型适配包）
import "git.woa.com/trpc-go/trpc-agent-go/trpc/model/hunyuan"

hunyuanModel := hunyuan.NewOpenAI("hunyuan-a13b",
    hunyuan.WithBaseURL("http://hunyuanapi.woa.com/openapi/v1"),
    hunyuan.WithAPIKey("your-api-key"),
    hunyuan.WithHTTPClientName("trpc.test.llm.hunyuan"),
    hunyuan.WithThinking(), // 启用思考模式（可选）.
)

hunyuanSearchModel := hunyuan.NewOpenAI("hunyuan-2.0-instruct-20251111",
    hunyuan.WithBaseURL("http://hunyuanapi.woa.com/openapi/v1"),
    hunyuan.WithAPIKey("your-api-key"),
    hunyuan.WithHTTPClientName("trpc.test.llm.hunyuan"),
    hunyuan.WithEnableEnhancement(true),
    // 启用搜索增强（可选）.
    hunyuan.WithSearchScene(hunyuan.SearchSceneSafe),
    // 使用 safe 搜索场景（可选）.
)
```

对于太极平台的以下 GLM 模型：`GLM-5-FP8-Online-128K`、`GLM-5-FP8-Online-32K`，思考模式底层仍然通过 `chat_template_kwargs.enable_thinking` 控制，但在 `trpc/model/taiji` 中已经支持根据模型名自动映射，因此可直接继续使用 `taiji.WithThinking(...)`：

```go
import "git.woa.com/trpc-go/trpc-agent-go/trpc/model/taiji"

glmModel := taiji.NewOpenAI("GLM-5-FP8-Online-128K",
    taiji.WithBaseURL("http://api.taiji.woa.com/openapi"),
    taiji.WithAPIKey("your-api-key"),
    taiji.WithHTTPClientName("trpc.test.llm.openai"),
    taiji.WithOpenAIInfer(true),
    taiji.WithThinking(false), // 自动转换为 chat_template_kwargs.enable_thinking=false
)
```

**手动配置方式**

如果你需要手动配置额外字段，可以使用 `openai.WithExtraFields`。

当目标平台是太极时，建议同时加上 `taiji.WithOpenAIErrorCompat()`。

```go
// 太极平台（手动配置额外字段方式，工具调用）
toolModel := openai.New("deepseek-chat",
    openai.WithBaseURL("http://api.taiji.woa.com/openapi"),
    openai.WithAPIKey("your-taiji-api-key"),
    taiji.WithOpenAIErrorCompat(),
    openai.WithExtraFields(map[string]any{
        "openai_infer": true,
        "tool_choice":  "auto",
    }),
)

// 太极平台（DeepSeek V3.1/V3.2 思考模式）
deepseekThinkingModel := openai.New("DeepSeek-V3_2-Online-32k",
    openai.WithBaseURL("http://api.taiji.woa.com/openapi"),
    openai.WithAPIKey("your-taiji-api-key"),
    taiji.WithOpenAIErrorCompat(),
    openai.WithExtraFields(map[string]any{
        "thinking": true,
    }),
)

// 太极平台（GLM 模型思考模式）
glmModel := openai.New("glm-4.6",
    openai.WithBaseURL("http://api.taiji.woa.com/openapi"),
    openai.WithAPIKey("your-taiji-api-key"),
    taiji.WithOpenAIErrorCompat(),
    openai.WithExtraFields(map[string]any{
        "chat_template_kwargs": map[string]any{
            "enable_thinking": false, // false 关闭思考，true 开启思考
        },
    }),
)

// 其他平台配置类似，只需修改模型名称、BaseURL 和 APIKey，无需额外字段
defaultModel := openai.New("your-model-name",
    openai.WithBaseURL("your-base-url"),
    openai.WithAPIKey("your-api-key"),
)
```

#### 太极平台特殊参数说明

太极平台支持多种特殊参数，用于控制模型的行为。详细文档请参考：[太极公共模型服务调用 API 文档](https://iwiki.woa.com/p/4015530156)。

**API 版本说明**

太极平台提供了两个 API 版本的 Base URL：

- **`http://api.taiji.woa.com/openapi`**：适用于大部分模型（如 DeepSeek 系列、GLM 系列等）
- **`http://api.taiji.woa.com/openapi/v2`**：适用于 `hy3-preview` 等新一代混元模型

如果使用 `hy3-preview` 等模型时收到错误提示"该模型需要使用 /openapi/v2 接口"，请将 Base URL 切换为 `/openapi/v2`。

**`openai_infer` 参数**

- **功能**：控制请求处理方式
- **类型**：`bool`
- **设为 `true`**：请求直接透传到推理引擎，接入层不做参数转换。这种模式下，部分支持原生思考的模型（如 DeepSeek-R1-Online）会将思考过程的内容放置在 `Content` 字段中，而不是标准的 `ReasoningContent` 字段
- **设为 `false`**：系统会进行相应的参数转换和字段映射，以确保思考内容正确放置在 `ReasoningContent` 字段中

**`tool_choice` 参数**

- **功能**：控制模型工具调用
- **类型**：`string`
- **常用值**：`"auto"`
- **使用前提**：仅在为 agent 提供了工具（tools 数组）时才应设置
- **重要提醒**：如果设置了 `tool_choice: "auto"` 但没有提供任何工具，会导致 API 调用失败。当使用 `tool_choice` 参数时，必须同时提供对应的 `tools` 数组

**`thinking` 参数**

- **功能**：控制 DeepSeek V3.1/V3.2 系列模型的思考模式
- **类型**：`bool`
- **默认值**：`false`
- **支持的模型**：
  - DeepSeek-V3_1 系列：`DeepSeek-V3_1-Online-16k`、`DeepSeek-V3_1-Online-32k`、`DeepSeek-V3_1-Online-64k`、`DeepSeek-V3_1-Online-128k`
  - DeepSeek-V3_2 系列：`DeepSeek-V3_2-Online-16k`、`DeepSeek-V3_2-Online-32k`、`DeepSeek-V3_2-Online-64k`、`DeepSeek-V3_2-Online-128k`
- **说明**：与 DeepSeek-R1-Online 等原生支持思考的模型不同，V3.1/V3.2 系列需要显式开启思考模式。开启后模型会在回复前进行深度思考，可能会增加响应时间

**`query_id` 参数**

- **功能**：请求标识，部分自部署模型要求必填
- **类型**：`string`
- **适用场景**：太极平台部分自部署模型（非公共模型服务）
- **错误提示**：如果缺少该字段，可能返回错误 `"should include query_id && model && message"`
- **推荐用法**：使用 `taiji.WithQueryID("your-query-id")` 设置

**`chat_template_kwargs` 参数（GLM 模型）**

- **功能**：请求 Body 中的高级参数配置项，主要用于控制聊天模板的动态行为
- **类型**：`object`
- **GLM 思考开关**：通过 `chat_template_kwargs.enable_thinking` 控制是否开启思考
- **`enable_thinking` 类型**：`bool`
- **设为 `false`**：关闭思考模式
- **设为 `true`**：开启思考模式
- **适用范围**：太极平台上的 GLM 系列模型
- **示例字段**：`"chat_template_kwargs": {"enable_thinking": false}`

**使用示例**

```go
// 太极平台基础配置（工具调用）
toolModel := openai.New("deepseek-chat",
    openai.WithBaseURL("http://api.taiji.woa.com/openapi"),
    openai.WithAPIKey("your-taiji-api-key"),
    taiji.WithOpenAIErrorCompat(),
    openai.WithExtraFields(map[string]any{
        "openai_infer": true,
        "tool_choice":  "auto",
    }),
)

// DeepSeek V3.1/V3.2 系列开启思考模式
deepseekModel := openai.New("DeepSeek-V3_2-Online-32k",
    openai.WithBaseURL("http://api.taiji.woa.com/openapi"),
    openai.WithAPIKey("your-taiji-api-key"),
    taiji.WithOpenAIErrorCompat(),
    openai.WithExtraFields(map[string]any{
        "thinking": true,
    }),
)

// GLM 模型关闭思考模式
glmModel := openai.New("glm-4.6",
    openai.WithBaseURL("http://api.taiji.woa.com/openapi"),
    openai.WithAPIKey("your-taiji-api-key"),
    taiji.WithOpenAIErrorCompat(),
    openai.WithExtraFields(map[string]any{
        "chat_template_kwargs": map[string]any{
            "enable_thinking": false,
        },
    }),
)

// 自部署模型（需要 query_id）
queryIDModel := openai.New("your-model-name",
    openai.WithBaseURL("http://your-taiji-endpoint/openapi"),
    openai.WithAPIKey("your-api-key"),
    taiji.WithOpenAIErrorCompat(),
    openai.WithExtraFields(map[string]any{
        "query_id": "your-query-id",
    }),
)
```

完整示例见 [examples/taijiopenai](../examples/taijiopenai)。

### 混元模型思考模式配置

混元模型的思考模式通过 `enable_thinking` 参数控制，该参数是混元模型的私有字段。详细文档请参考：[混元文生文&多模态 OpenAPI](https://iwiki.woa.com/p/4010715517)。

**参数说明**

- **参数名**：`enable_thinking`
- **类型**：`bool`
- **默认值**：`true`
- **支持的模型**：当前仅 `hunyuan-a13b`、`hunyuan-0.5b`、`hunyuan-1.8b`、`hunyuan-4b`、`hunyuan-7b` 模型支持
- **功能**：控制模型是否开启思考模式

**使用方式**

推荐优先使用 `trpc/model/hunyuan` 适配包，它已经把 `enable_thinking`
封装成显式 option：

```go
import "git.woa.com/trpc-go/trpc-agent-go/trpc/model/hunyuan"

// 关闭混元模型思考模式
modelInstance := hunyuan.NewOpenAI("hunyuan-a13b",
    hunyuan.WithDisableThinking(),
)

// 或者显式开启思考模式
modelInstance := hunyuan.NewOpenAI("hunyuan-a13b",
    hunyuan.WithThinking(),
)
```

如果你使用通用 `openai.New(...)`，仍然可以通过
`openai.WithExtraFields` 直接设置：

```go
// 关闭混元模型思考模式
modelInstance := openai.New("hunyuan-a13b",
    openai.WithExtraFields(map[string]any{
        "enable_thinking": false, // 注意：不是 "enable thinking" 或者 "enabled_thinking"
    }),
)

// 或者开启思考模式（通常不需要设置，对于思考模型默认为 true）
modelInstance := openai.New("hunyuan-a13b",
    openai.WithExtraFields(map[string]any{
        "enable_thinking": true,
    }),
)
```

**注意事项**

1. 混元模型的 `enable_thinking` 参数是混元平台特有的私有字段
2. 默认情况下思考模式是开启的（`enable_thinking: true`）
3. 如果不需要模型的思考过程输出，建议显式设置 `enable_thinking: false` 以获得更直接的响应

**与其他模型思考模式的区别**

- `thinking_enabled`：这是本框架提供的参数，用于控制 Claude 和 Gemini 等模型通过 OpenAI API 启用思考模式
- `enable_thinking`：这是混元平台的私有参数，仅用于混元模型系列

两种参数针对不同的模型和平台，请根据实际情况选择使用。

### 混元模型搜索增强配置

混元模型的搜索增强通过 `enable_enhancement`、
`force_search_enhancement` 和 `search_scene` 参数控制。详细文档请参考：
[混元文生文&多模态 OpenAPI](https://iwiki.woa.com/p/4010715517)。

**参数说明**

- **`enable_enhancement`**：`bool` 类型，默认值为 `false`。
  开启后，模型会自行判断是否触发搜索增强。
  在 `trpc/model/hunyuan` 中可通过 `hunyuan.WithEnableEnhancement(...)`
  设置。
- **`force_search_enhancement`**：`bool` 类型，默认值为 `false`。
  设置为 `true` 时会强制命中搜索链路；在 `trpc/model/hunyuan` 中可通过
  `hunyuan.WithForceSearchEnhancement(true)` 设置，封装层会自动补齐
  `enable_enhancement=true`。
- **`search_scene`**：`string` 类型；当值为 `"safe"` 时会屏蔽 Bing
  数据源。在 `trpc/model/hunyuan` 中可通过
  `hunyuan.WithSearchScene(hunyuan.SearchSceneSafe)` 设置。

**使用方式**

```go
import "git.woa.com/trpc-go/trpc-agent-go/trpc/model/hunyuan"

searchModel := hunyuan.NewOpenAI("hunyuan-2.0-instruct-20251111",
    hunyuan.WithBaseURL("http://hunyuanapi.woa.com/openapi/v1"),
    hunyuan.WithAPIKey("your-api-key"),
    hunyuan.WithHTTPClientName("trpc.test.llm.hunyuan"),
    hunyuan.WithEnableEnhancement(true),
    hunyuan.WithSearchScene(hunyuan.SearchSceneSafe),
)

forcedSearchModel := hunyuan.NewOpenAI("hunyuan-2.0-thinking-20251109",
    hunyuan.WithBaseURL("http://hunyuanapi.woa.com/openapi/v1"),
    hunyuan.WithAPIKey("your-api-key"),
    hunyuan.WithHTTPClientName("trpc.test.llm.hunyuan"),
    hunyuan.WithForceSearchEnhancement(true),
    // 会自动补齐 enable_enhancement=true.
)
```

如果你使用通用 `openai.New(...)`，也可以继续直接透传额外字段：

```go
searchModel := openai.New("hunyuan-2.0-instruct-20251111",
    openai.WithBaseURL("http://hunyuanapi.woa.com/openapi/v1"),
    openai.WithAPIKey("your-api-key"),
    openai.WithExtraFields(map[string]any{
        "enable_enhancement":       true,
        "force_search_enhancement": true,
        "search_scene":             "safe",
    }),
)
```

**注意事项**

1. 搜索增强属于混元平台私有参数，只有混元兼容接口会识别这些字段。
2. 根据官方文档，混元 2.0 模型的 AI 搜索链路暂不支持天气、汇率、地图、日历插件。
3. `search_scene="safe"` 仅影响搜索数据源选择，不会单独触发搜索。

### tRPC 注入与北极星

通过匿名导入 `trpc` 包，OpenAI 兼容 SDK 会把默认 HTTP
客户端替换为内部请求处理器；在显式传入自定义 `Transport`
时，仍会保留标准 `http.Client`。这样既能复用 tRPC-Go
的寻址、限流、拦截器、监控等生态能力（包括 `trpc_go.yaml`
配置），又不会拦截显式自定义的原生 HTTP 传输。

- 关键代码路径：`trpc/model.go` 在 `init()` 中覆盖了
  `openai.DefaultNewHTTPClient`，默认返回内部请求处理器；
  当显式传入 `Transport` 时返回标准 `http.Client`。
- 寻址行为（由 `WithBaseURL` 的 scheme 决定）：
  - BaseURL 为**纯路径**（如 `"/llmproxy"`）时，不设置寻址 target，由 YAML `target` 接管（推荐内网使用）；
  - BaseURL 为 `http/https` 时，保持原生 HTTP 请求语义，自动推导 `dns://host` 作为 target；
  - BaseURL 为 `dns://host[:port]` 时，会显式使用 DNS selector 寻址；
  - BaseURL 为 `polaris://service` 时，直接使用北极星寻址。

使用方式（二选一）：

**方式一：纯路径 BaseURL + YAML target（推荐）**

`WithBaseURL` 只填路径，寻址由 `trpc_go.yaml` 的 `target` 控制。
不改代码即可切换路由，也能使用 YAML 中的全部北极星参数。

```go
import (
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"
    "trpc.group/trpc-go/trpc-agent-go/model/openai"
)

model := openai.New("deepseek-chat",
    openai.WithBaseURL("/llmproxy"),  // 只填路径，不带 scheme 和 host
    openai.WithAPIKey("your-api-key"),
    openai.WithHTTPClientOptions(
        openai.WithHTTPClientName("llm_client"), // 绑定 trpc_go.yaml 的 client.service.name
    ),
)
```

对应 `trpc_go.yaml`：

```yaml
client:
  service:
    - name: llm_client
      target: polaris://llm-service.name  # 寻址由 YAML 控制
      protocol: http
```

> 原理：当 BaseURL 是纯路径时，请求 URL 没有 host，框架不会生成
> per-request target，YAML 中的 `target` 自然生效。

**方式二：代码中显式写 `polaris://`**

在代码层面直接确定寻址方式。此方式下代码中的 target 优先级高于 YAML 的 `target` 字段，
YAML 中的其他配置（过滤器、连接池等）仍然生效。

```go
import (
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"
    "trpc.group/trpc-go/trpc-agent-go/model/openai"
)

model := openai.New("deepseek-chat",
    openai.WithBaseURL("polaris://llm-service.name"),
    openai.WithAPIKey("your-api-key"),
    openai.WithHTTPClientOptions(
        openai.WithHTTPClientName("llm_client"), // 对应 trpc_go.yaml 的 client.service.name
    ),
)
```

#### BaseURL 带有 path 的两种情况

有些自建模型服务会挂在网关路径下（例如 `/qpilothub/v2/...`）。此时 **path 应该通过 `WithBaseURL(...)` 的 URL Path 传入**，它不会影响北极星寻址（tRPC 只使用 `polaris://{service}` 的 host 作为 target）。

##### 1）endpoint 与 OpenAI 接口一致（推荐）

如果你的服务仍然提供 OpenAI 兼容接口（例如 ChatCompletions 的 endpoint 为 `chat/completions`），只需要把网关前缀写进 BaseURL：

```go
model := openai.New("deepseek-chat",
    // 只写到网关前缀，不要把 /chat/completions 写进去（避免重复拼接）
    openai.WithBaseURL("polaris://llm-service.name/qpilothub/v2"),
    openai.WithAPIKey("your-api-key"),
    openai.WithHTTPClientOptions(
        openai.WithHTTPClientName("llm_client"),
    ),
)
```

最终请求路径会变成：`/qpilothub/v2/chat/completions`（SDK 会把 `chat/completions` 作为相对路径拼到 BaseURL 的 path 后面）。

##### 2）endpoint 与 OpenAI 接口不一致（可选）

如果你的服务端 endpoint 不是 OpenAI 的 `chat/completions`（例如是 `/qpilothub/v2/xxx/yyy`），有两种做法：

- **做法 A（推荐）**：在网关/服务端做兼容映射
  - 把 `POST /qpilothub/v2/chat/completions` 转发到你们真实的 endpoint；
  - 这样 `trpc-agent-go`/`openai-go` 无需改动，且兼容性最好。

- **做法 B（高级）**：用 `openai-go` middleware 重写请求 path
  - 仅当你们的请求/响应体仍兼容 OpenAI ChatCompletions 时可用；
  - 示例：把 SDK 默认的 `/.../chat/completions` 重写为你们的 `/.../xxx/yyy`。

```go
import (
    "net/http"
    "strings"

    openaiopt "github.com/openai/openai-go/option"
    "trpc.group/trpc-go/trpc-agent-go/model/openai"
)

model := openai.New("deepseek-chat",
    openai.WithBaseURL("polaris://llm-service.name"),
    openai.WithAPIKey("your-api-key"),
    openai.WithHTTPClientOptions(
        openai.WithHTTPClientName("llm_client"),
    ),
    openai.WithOpenAIOptions(openaiopt.WithMiddleware(
        func(req *http.Request, next openaiopt.MiddlewareNext) (*http.Response, error) {
            if strings.HasSuffix(req.URL.Path, "/chat/completions") {
                // 替换为你们的真实 endpoint
                req.URL.Path = strings.TrimSuffix(req.URL.Path, "/chat/completions") + "/qpilothub/v2/xxx/yyy"
            }
            return next(req)
        },
    )),
)
```

> 注意：如果你们的协议/字段并非 OpenAI 兼容（不满足 ChatCompletions 的请求与响应结构），则不建议走 `openai.New(...).GenerateContent(...)` 这条链路，需要自定义适配层或直接使用你们自己的 SDK。

#### 腾讯云 Token Hub Kimi 使用说明

当同时满足以下条件时：

- 使用 Kimi 模型
- 开启 thinking 模式
- 进行工具调用并把 assistant tool-call message 回放到下一轮请求

建议显式开启 `openai.WithReasoningContentBackfill(true)`。这个选项会在
`reasoning_content` 为空但 provider 仍要求字段存在时，回放空字符串，
避免部分 OpenAI-compatible provider 在多轮 tool call 场景下返回 400。

```go
import (
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"

    "trpc.group/trpc-go/trpc-agent-go/model/openai"
)

llm := openai.New("kimi-k2.5",
    openai.WithBaseURL("http://tokenhub.tencentmaas.com/v1"),
    openai.WithAPIKey("your-api-key"),
    openai.WithHTTPClientOptions(
        openai.WithHTTPClientName("trpc.test.llm.openai"),
    ),
    openai.WithExtraFields(map[string]any{
        "thinking": true,
    }),
    openai.WithReasoningContentBackfill(true),
)
```

如果没有开启 thinking，或者当前对话不会发生 tool call replay，则不需
要设置这个选项。

#### 推荐用法：使用 `trpc/model` 内部平台模型适配包

如果你在内网使用 tRPC-Go 生态（例如北极星寻址、监控、过滤器、连接池配置等），建议直接使用 `git.woa.com/trpc-go/trpc-agent-go` 提供的 `trpc/model` 内部平台模型适配包。

- **版本要求**：`git.woa.com/trpc-go/trpc-agent-go` 版本需不低于 `v1.2.0`。
- **它们解决的问题**：
  - 避免忘记匿名导入 `_ "git.woa.com/trpc-go/trpc-agent-go/trpc"`（内部平台模型适配包已处理）。
  - 把平台私有字段（例如太极的 `openai_infer`、`tool_choice`、DeepSeek 场景下的 `thinking`，混元的 `enable_thinking`、`enable_enhancement`、`force_search_enhancement`、`search_scene`）封装成显式的 Option，减少手写 `openai.WithExtraFields(...)`。
  - 对齐 OpenAI 兼容 SDK 的命名：`WithHTTPClientName`、`WithHTTPClientTransport`。

**太极平台示例**：

```go
import (
    "git.woa.com/trpc-go/trpc-agent-go/trpc/model"
    "git.woa.com/trpc-go/trpc-agent-go/trpc/model/taiji"
)

llm := taiji.NewOpenAI("DeepSeek-V3_2-Online-32k",
    taiji.WithAPIKey("your-api-key"),
    taiji.WithBaseURL("http://api.taiji.woa.com/openapi"),
    taiji.WithHTTPClientName("trpc.test.llm.openai"),
    // 示例名称，需与 trpc_go.yaml 的 client.service.name 一致。
    taiji.WithOpenAIInfer(true),
    // 启用 openai_infer 模式，用于太极平台大多数模型。
    taiji.WithToolChoice(),
    // 启用 tool_choice: "auto"，用于模型工具调用（需同时提供 tools）.
    taiji.WithThinking(true),
    // 启用 thinking 模式，用于 DeepSeek V3.1/V3.2 系列模型。
    taiji.WithQueryID("your-query-id"),
    // 设置 query_id，部分自部署模型需要。
)

var _ model.Model = llm

// hy3-preview 等新一代混元模型需要使用 /openapi/v2
hy3 := taiji.NewOpenAI("hy3-preview",
    taiji.WithAPIKey("your-api-key"),
    taiji.WithBaseURL("http://api.taiji.woa.com/openapi/v2"),
    taiji.WithHTTPClientName("trpc.test.llm.openai"),
)

var _ model.Model = hy3
```

如果使用太极平台的 `GLM-5-FP8-Online-128K` 或 `GLM-5-FP8-Online-32K`，`taiji.WithThinking(...)` 会自动把参数写入 `chat_template_kwargs.enable_thinking`：

```go
import "git.woa.com/trpc-go/trpc-agent-go/trpc/model/taiji"

llm := taiji.NewOpenAI("GLM-5-FP8-Online-128K",
    taiji.WithAPIKey("your-api-key"),
    taiji.WithBaseURL("http://api.taiji.woa.com/openapi"),
    taiji.WithHTTPClientName("trpc.test.llm.openai"),
    taiji.WithOpenAIInfer(true),
    taiji.WithThinking(false),
)
```

上述选项对应的参数含义，详见 [太极平台特殊参数说明](#太极平台特殊参数说明)。

**混元平台示例**：

```go
import "git.woa.com/trpc-go/trpc-agent-go/trpc/model/hunyuan"

llm := hunyuan.NewOpenAI("hunyuan-a13b",
    hunyuan.WithAPIKey("your-api-key"),
    hunyuan.WithBaseURL("http://hunyuanapi.woa.com/openapi/v1"),
    hunyuan.WithHTTPClientName("trpc.test.llm.hunyuan"),
    // 示例名称，需与 trpc_go.yaml 的 client.service.name 一致。
    hunyuan.WithThinking(),
    // 启用 enable_thinking: true，用于混元思考模式。
)
```

上述选项对应的参数含义，详见 [混元模型思考模式配置](#混元模型思考模式配置)。

命令行示例：

```bash
cd examples/runner
export OPENAI_BASE_URL="polaris://llm-service.name"
export OPENAI_API_KEY="your-api-key"
go run main.go -model deepseek-chat
```

示例 `trpc_go.yaml`（client 侧）：

```yaml
client:
  namespace: Production
  service:
    - name: llm_client # 与 WithHTTPClientName 保持一致
      target: polaris://llm-service.name  # 仅在 BaseURL 为纯路径时生效
      protocol: http # thttp 客户端
      timeout: 0 # 不设置请求超时，交由 ctx 控制
      conn_type: httppool # HTTP 连接池设置
      httppool:
        idle_conn_timeout: 50s # 客户端空闲连接超时（默认 50s）
        max_idle_conns_per_host: 100
```

### 代码可选项（优先级与用法）

- `openai.WithBaseURL(url)`：
  - **纯路径**（如 `"/llmproxy"`）：不设置寻址 target，由 YAML `target` 接管（推荐内网使用）；
  - `polaris://svc` 或 `polaris://svc/path`：在代码层面直接使用北极星寻址；
  - `http/https`：自动推导 `dns://host` 作为 target（直接 DNS 解析）；
  - `dns://host[:port]`：显式使用 DNS selector 寻址。
- `openai.WithHTTPClientOptions(openai.WithHTTPClientName("llm_client"))`：
  - 将 HTTP 客户端名称绑定到 `trpc_go.yaml` 的 `client.service.name`；
  - 配合 YAML 中的过滤器、拦截器、连接池等能力；
  - 配合**纯路径 BaseURL** 时，YAML 的 `target` 字段生效，可实现北极星寻址；
  - 当 BaseURL 带有 scheme + host 时，YAML 的 `target` **不生效**（被 URL 推导的 target 覆盖）。
- `openaiopt.WithRequestTimeout(d)`：
  - 来自 `github.com/openai/openai-go/option`，为每次请求设置请求级超时；
  - 与调用的 `ctx` 共同决定端到端时限（取更小者）。

优先级规则（HTTP 层）

- 注入的 thttp 处理器会在 GET/POST 附加 `client.WithTimeout(0)`（不强制 tRPC 请求超时）。
- 请求时限生效顺序：`min(调用 ctx deadline, openaiopt.WithRequestTimeout)`。
- 寻址生效顺序：
  - BaseURL 带有 scheme + host → 从 URL 推导 target（per-request），**覆盖** YAML `target`；
  - BaseURL 为纯路径（无 scheme、无 host）→ 不设置 per-request target，**YAML `target` 生效**。

示例（结合 ctx + SDK 请求级超时）：

```go
import openaiopt "github.com/openai/openai-go/option"

ctx, cancel := context.WithTimeout(ctx, 60*time.Second) // 端到端时限
defer cancel()

llm := openai.New("gpt-4o-mini",
    openai.WithBaseURL("polaris://llm-service.name"),
    openai.WithHTTPClientOptions(openai.WithHTTPClientName("llm_client")),
    openaiopt.WithRequestTimeout(30*time.Second), // 单次请求超时
)
```

### 超时控制（LLM/流式调用）

- 自动注入的 thttp 客户端（见上文）在 OpenAI 侧会对 GET/POST 追加 `client.WithTimeout(0)`，即不设置 tRPC 的请求级超时，避免流式/长轮询被提前截断。
- 连接空闲超时（IdleTimeout）：
  - 客户端：HTTP 传输的 `IdleConnTimeout` 默认 50s；
  - 服务端：`server.service.idletime` 默认 60s（按 tRPC-Go 默认值），仅对“空闲连接”生效，活跃流式连接不会触发。
- 建议使用调用的 `context` 精确控制端到端超时，或在 `openai-go` 侧配置请求级超时。

```go
// 1) 用 context 限制本次调用的最大时长
ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
defer cancel()

// 2) 对 OpenAI 兼容 SDK 使用请求级超时
import openaiopt "github.com/openai/openai-go/option"

llm := openai.New("gpt-4o-mini",
    openaiopt.WithRequestTimeout(30*time.Second), // 每次请求的超时
)

// 你的调用代码会沿用传入的 ctx 截止时间
rspCh, err := llm.GenerateContent(ctx, &model.Request{ /* ... */ })
```

- Agent/Runner 会沿用你传入的 `ctx`：

```go
// 以 Runner 为例
respCh, err := runner.Run(ctx, userID, sessionID,
    llmagent.WithModel(llm),
    llmagent.WithGenerationConfig(model.GenerationConfig{Stream: true}),
)
```

如需在多处统一管理时限，建议封装一层创建带超时 `ctx` 的帮助函数，避免在底层传输上设置“硬截断”。
