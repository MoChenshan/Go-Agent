# OpenAI 服务器示例（tRPC-Go 集成）

本示例展示了如何启动 trpc-agent-go **OpenAI 兼容服务器**，
该服务器使用 `http_no_protocol` 与 tRPC-Go 框架集成。它实现了 OpenAI Chat Completions API 标准。

## 前置要求

- Go 1.21 或更高版本
- OpenAI API 密钥（或您使用的模型的兼容 API 密钥）
- tRPC-Go 框架

## 运行服务器

```bash
# 从此目录
cd examples/openaiserver/trpc

# 使用默认设置启动服务器（模型：deepseek-chat，端口：8080）
go run .

# 使用不同模型启动服务器
go run . -model gpt-4
```

### 命令行选项

- `-model`: 要使用的模型名称（默认："deepseek-chat"）

### 配置

服务器配置在 `trpc_go.yaml` 中定义：

- 服务名称：`trpc.test.openai.server`
- 监听地址：`127.0.0.1:8080`
- 协议：`http_no_protocol`

## 使用 curl 测试

### 非流式请求

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-chat",
    "messages": [
      {"role": "user", "content": "2 + 2 等于多少？"}
    ],
    "stream": false
  }'
```

### 流式请求

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-chat",
    "messages": [
      {"role": "user", "content": "2 + 2 等于多少？"}
    ],
    "stream": true
  }'
```

### 使用工具（函数调用）

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-chat",
    "messages": [
      {"role": "user", "content": "计算 15 * 23"}
    ],
    "stream": false
  }'
```

## 使用 OpenAI SDK 测试

您可以使用任何 OpenAI 兼容的客户端库。以下是 Python 示例：

```python
from openai import OpenAI

# 指向您的本地服务器
client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="not-needed"  # 本地服务器不需要 API 密钥
)

# 非流式
response = client.chat.completions.create(
    model="deepseek-chat",
    messages=[
        {"role": "user", "content": "2 + 2 等于多少？"}
    ]
)
print(response.choices[0].message.content)

# 流式
stream = client.chat.completions.create(
    model="deepseek-chat",
    messages=[
        {"role": "user", "content": "给我讲个故事"}
    ],
    stream=True
)
for chunk in stream:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)
```

## 功能特性

- ✅ OpenAI Chat Completions API 兼容
- ✅ 流式和非流式响应
- ✅ 函数调用（工具）支持
- ✅ 多轮对话
- ✅ 会话管理

## 可用工具

- **calculator**: 执行基本数学运算（加、减、乘、除）
- **current_time**: 获取指定时区的当前时间和日期

---

可以根据需要在 `main.go` 中替换代理逻辑，或在 `tools.go` 中添加更多工具。
