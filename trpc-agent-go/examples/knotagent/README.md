# Knot Agent 示例

本示例演示如何使用 `KnotAgent` 构建一个基于 [Knot](https://knot.woa.com) 平台的交互式命令行聊天应用。

## Knot API 文档

官方 API 文档：<https://iwiki.woa.com/p/4016457374>

## 前置条件

- 有效的 Knot API Key（通过 Knot 平台申请）
- 你的用户名

## 配置说明

所有参数均可通过 **命令行参数** 或 **环境变量** 设置：

| 命令行参数 | 环境变量 | 默认值 | 说明 |
|------|---------------------|---------|-------------|
| `-api-url` | `KNOT_API_URL` | （空） | Knot API 地址，格式：`http://knot.woa.com/apigw/api/v1/agents/agui/{agent_id}` |
| `-api-key` | `KNOT_API_KEY` | （空） | Knot API 密钥 |
| `-model` | `KNOT_MODEL` | `deepseek-v3` | 模型名称 |
| `-user` | `KNOT_USER` | `your-rtx` | 用户名 |

## 快速开始

### 通过环境变量配置

```bash
export KNOT_API_URL="http://knot.woa.com/apigw/api/v1/agents/agui/{agent_id}"
export KNOT_API_KEY="your-api-key"
export KNOT_MODEL="deepseek-v3"
export KNOT_USER="your-rtx"

go run main.go
```

### 通过命令行参数配置

```bash
go run main.go \
  -api-url="http://knot.woa.com/apigw/api/v1/agents/agui/{agent_id}" \
  -api-key="your-api-key" \
  -model="deepseek-v3" \
  -user="your-rtx"
```

## 使用方式

启动后进入交互式聊天：

```
Interactive Chat with KnotAgent
Model: deepseek-v3
==================================================
Chat ready! Session: session-1740000000

Commands:
   /exit  - End the conversation

You: Hello
Assistant: Hi! How can I help you today?

You: /exit
Goodbye!
```
