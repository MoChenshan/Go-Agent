## 大模型流式 chunk token usage 格式说明

`trpc.group/trpc-go/trpc-agent-go/model/openai` 在流式情况下，调用 openai 接口设置 stream_options 中的 [include_usage](https://platform.openai.com/docs/api-reference/completions/create#completions_create-stream_options-include_usage) 字段为 true，该字段的含义如下：

```
include_usage
boolean

Optional
If set, an additional chunk will be streamed before the data: [DONE] message. The usage field on this chunk shows the token usage statistics for the entire request, and the choices field will always be an empty array.

All other chunks will also include a usage field, but with a null value. NOTE: If the stream is interrupted, you may not receive the final usage chunk which contains the total token usage for the request.
```

在外网 openai 模型返回结果中，只有最后一个 chunk 有 streaming chunk token usage，其他 chunk 的 token usage 为空值。
trpc-agent-go 默认是采取对 streaming chunk token usage 进行求和累积，能够正确统计到 token usage。

 但是已经测试的内网的模型返回结果中，token usage 有三种种格式：

1. 出现在最后一个 chunk：该格式符合上述 openai api 规范
2. 累积出现在 chunk 里面： prompt_tokens 出现且保持不变，completion_tokens 和 total_tokens 累积
3. prompt_tokens 重复出现: prompt_tokens 重复出现，total_tokens 和 completion_tokens 出现在最后一个 chunk里面

为了兼容不同格式，需要用户自行设置 `openai.WithAccumulateChunkTokenUsage` 来进行格式转换。

```go
// 需要设置 openai.WithAccumulateChunkTokenUsage 的情况：
// 1. 累积出现在 chunk 里面: prompt_tokens 出现且保持不变；completion_tokens 和 total_tokens 累积
// 2. prompt_tokens 重复出现: prompt_tokens 重复出现，total_tokens 和 completion_tokens 出现在最后一个 chunk里面
import (
    "trpc.group/trpc-go/trpc-agent-go/model/openai"
    "trpc.group/trpc-go/trpc-agent-go/model"
)

modelName := "gpt-5"
model := openai.New(modelName, openai.WithAccumulateChunkTokenUsage(func(u model.Usage, delta model.Usage) {
    return model.Usage{
        PromptTokens:  delta.PromptTokens,
        CompletionTokens: delta.CompletionTokens,
        TotalTokens: delta.TotalTokens,
    }
}))
```

### 不同模型服务商流式响应 Token Usage 格式汇总表



[测试代码](https://git.woa.com/awesome-ai-practice/openai-go/tree/main/teststreming)

#### Venus`http://v2.open.venus.oa.com/llmproxy`）

| OPENAI_MODEL | streaming_chunck_token_usage_format |
|--------------|-------------------------------------|
| gpt-5-chat | 出现在最后一个 chunk |
| gpt-5-mini | 出现在最后一个 chunk |
| gpt-5-nano | 出现在最后一个 chunk |
| gpt-5 | 出现在最后一个 chunk |
| gemini-2.5-flash | 出现在最后一个 chunk |
| gemini-2.5-pro | 出现在最后一个 chunk |
| gemini-2.0-flash | 出现在最后一个 chunk |
| claude-opus-4-5-20251101 |出现在最后一个 chunk |
| claude-4-5-sonnet-20250929 |出现在最后一个 chunk |
| claude-4-sonnet-20250514 | 出现在最后一个 chunk |
| claude-3-7-sonnet-20250219 | prompt_tokens 重复出现|
| o4-mini | 出现在最后一个 chunk |
| o3 | 出现在最后一个 chunk |
| gpt-4.1 | 出现在最后一个 chunk |
| gpt-4o-2024-11-20 | 出现在最后一个 chunk |
| o3-mini | 出现在最后一个 chunk |
| deepseek-r1-local-II | 出现在最后一个 chunk |
| deepseek-v3-local-II | 出现在最后一个 chunk |
| deepseek-v3-local-III | 出现在最后一个 chunk |
| deepseek-v3.1-terminus | 出现在最后一个 chunk|
| deepseek-v3.2 | 出现在最后一个 chunk|
| qwen3-32b-fp8 | 出现在最后一个 chunk |
| qwen3-235b-a22b-2507-fp8 | 出现在最后一个 chunk|
| kimi-k2-instruct-local | 出现在最后一个 chunk |
| kimi-k2-instruct-0905-local | 出现在最后一个 chunk|
| glm-4.5-fp8 | 累积出现在 chunk 里面|
| glm-4.6-fp8| 出现在最后一个 chunk|
| glm-4.7 | 出现在最后一个 chunk|

#### 混元（`http://hunyuanapi.woa.com/openapi/v1`）

| OPENAI_MODEL | streaming_chunck_token_usage_format |
|--------------|-------------------------------------|
| hunyuan-2.0-instruct-20251111 | 累积出现在 chunk 里面 |
| hunyuan-2.0-thinking-20251109 | 累积出现在 chunk 里面 |
| hunyuan-turbos-latest | 累积出现在 chunk 里面 |
| hunyuan-turbo | 累积出现在 chunk 里面 |
| hunyuan-t1-latest | 累积出现在 chunk 里面 |
| huanyuan-large | 未测试 |
| hunyuan-standard| 累积出现在 chunk 里面 |
| hunyuan-standard-70b| 累积出现在 chunk 里面 |
| hunyuan-standard-256K| 累积出现在 chunk 里面 |
| hunyuan-funcall| 累积出现在 chunk 里面 |

#### 太极（`http://api.taiji.woa.com/openapi`）

> 太极平台的流式返回结果受 `openai_infer` 参数影响，以下测试结果按 `openai_infer=false` 记录。

| OPENAI_MODEL | openai_infer | streaming_chunck_token_usage_format |
|--------------|-------------------|-------------------------------------|
| DeepSeek-R1-Online-128K| - | 未测试|
| DeepSeek-R1-Online-64K| - | 未测试|
| DeepSeek-R1-Online-32K | false | 出现在最后一个 chunk |
| DeepSeek-R1-Online-16K| - | 未测试|
| DeepSeek-V3-Online-16K| false |  累积出现在 chunk 里面 |
| DeepSeek-V3-Online-16K| true |  累积出现在 chunk 里面 |
| DeepSeek-V3_1-Online-32k| false |  出现在最后一个 chunk |
| DeepSeek-V3_1-Online-32k| true |  累积出现在 chunk 里面|
| DeepSeek-V3_1-Online-64k| false |  出现在最后一个 chunk |
| DeepSeek-V3_1-Online-64k| true |  累积出现在 chunk 里面 |
| DeepSeek-V3_1-Online-128k| - | 未测试|
| DeepSeek-V3_2-Online-16k| false | 出现在最后一个 chunk |
| DeepSeek-V3_2-Online-16k| true | 累积出现在 chunk 里面 |
| DeepSeek-V3_2-Online-32k| false | 出现在最后一个 chunk |
| DeepSeek-V3_2-Online-32k| true | 累积出现在 chunk 里面 |


### 各类 Token Usage 格式详细示例

#### 出现在最后一个 chunk 

```
Chunk 0: {"completion_tokens":0,"prompt_tokens":0,"total_tokens":0,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 1: {"completion_tokens":0,"prompt_tokens":0,"total_tokens":0,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 2: {"completion_tokens":0,"prompt_tokens":0,"total_tokens":0,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 3: {"completion_tokens":0,"prompt_tokens":0,"total_tokens":0,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 4: {"completion_tokens":0,"prompt_tokens":0,"total_tokens":0,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 5: {"completion_tokens":0,"prompt_tokens":0,"total_tokens":0,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 6: {"completion_tokens":0,"prompt_tokens":0,"total_tokens":0,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 7: {"completion_tokens":0,"prompt_tokens":0,"total_tokens":0,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 8: {"completion_tokens":0,"prompt_tokens":0,"total_tokens":0,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 9: {"completion_tokens":0,"prompt_tokens":0,"total_tokens":0,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 10: {"completion_tokens":8,"prompt_tokens":14,"total_tokens":22,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
```


### 累积出现在 chunk 里面 

#### prompt_tokens 出现且保持不变；completion_tokens 和 total_tokens 累积
```
Chunk 0: {"completion_tokens":1,"prompt_tokens":48,"total_tokens":49,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 1: {"completion_tokens":2,"prompt_tokens":48,"total_tokens":50,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 2: {"completion_tokens":3,"prompt_tokens":48,"total_tokens":51,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 3: {"completion_tokens":4,"prompt_tokens":48,"total_tokens":52,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 4: {"completion_tokens":5,"prompt_tokens":48,"total_tokens":53,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 5: {"completion_tokens":6,"prompt_tokens":48,"total_tokens":54,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 6: {"completion_tokens":7,"prompt_tokens":48,"total_tokens":55,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 7: {"completion_tokens":8,"prompt_tokens":48,"total_tokens":56,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 8: {"completion_tokens":9,"prompt_tokens":48,"total_tokens":57,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 9: {"completion_tokens":10,"prompt_tokens":48,"total_tokens":58,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
```

#### prompt_tokens 重复出现

prompt_tokens 重复出现，total_tokens 和 completion_tokens 出现在最后一个 chunk里面
```
Chunk 102: {"completion_tokens":0,"prompt_tokens":17,"total_tokens":0,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 103: {"completion_tokens":0,"prompt_tokens":17,"total_tokens":0,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 104: {"completion_tokens":0,"prompt_tokens":17,"total_tokens":0,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 105: {"completion_tokens":0,"prompt_tokens":17,"total_tokens":0,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 106: {"completion_tokens":0,"prompt_tokens":17,"total_tokens":0,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
Chunk 107: {"completion_tokens":324,"prompt_tokens":17,"total_tokens":341,"completion_tokens_details":{"accepted_prediction_tokens":0,"audio_tokens":0,"reasoning_tokens":0,"rejected_prediction_tokens":0},"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":0}}
```