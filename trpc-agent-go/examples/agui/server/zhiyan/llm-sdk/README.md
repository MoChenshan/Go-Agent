# AG-UI 智研上报示例

本示例演示如何在 AG-UI 服务端中集成智研-监控宝-LLM 应用性能分析。示例会启动 AG-UI Server，并将对话、工具调用等数据通过 OpenTelemetry 上报到智研。

## 前置条件

- 拥有智研 API Key、上报地址和应用名。
- 任意 AG-UI 客户端用于发起对话。

在 trpc_go.yaml 中配置 `(telemetry, zhiyan-llm)` 插件。

```yaml
plugins:  # 插件配置
  telemetry:
    zhiyan-llm:
      # YAML overrides env. You can also set env vars:
      # - ZHIYANLLM_API_ENDPOINT
      # - ZHIYANLLM_API_KEY
      # - ZHIYANLLM_APP_NAME
      api_endpoint: ${ZHIYANLLM_API_ENDPOINT}
      api_key: ${ZHIYANLLM_API_KEY}
      app_name: ${ZHIYANLLM_APP_NAME}
```

本示例的插件配置需要结合以下环境变量起作用，请先配置以下环境变量：

```bash
export ZHIYANLLM_API_ENDPOINT="https://trace.zhiyan.tencent-cloud.net:4318"
export ZHIYANLLM_API_KEY="key-xxxx"
export ZHIYANLLM_APP_NAME="llm-trpc-go-server"
```

## 快速开始

1. 进入示例目录并拉起 AG-UI Server：

```bash
cd trpc-agent-go-internal/examples/agui/server/zhiyan/llm-sdk
go run .
```

支持通过参数自定义模型与是否流式返回，例如：

```bash
go run . -model deepseek-chat -stream=true
```

输出日志如下：

```bash
plugin log-default setup succeed, time elapsed: 243.356µs
2025-10-27 21:50:27.869 DEBUG   maxprocs/maxprocs.go:48 maxprocs: Leaving GOMAXPROCS=32: CPU quota undefined
2025-10-27 21:50:27.869 INFO    server/service.go:211   process: 2331205, http_no_protocol service: trpc.test.helloworld.agui launch success, tcp: 127.0.0.1:8080, serving ...
```

启动日志中可看到 tRPC server 监听地址 `127.0.0.1:8080`，服务名 `trpc.test.helloworld.agui`。

2. 使用任意 AG-UI 客户端连接到 `http://127.0.0.1:8080/agui` 并发起对话或工具调用。示例内置了一个函数计算器工具，便于触发工具链路。

## 说明

当客户端发送 AG-UI 请求时，框架会通过 `runOptionResolver` 解析本次运行使用的 Attributes，其中包含智研需要用到的属性，例如 `agentName`、`modelName` 和 `user-message`。span 上下文会通过运行器传播，因此每个流式事件都会共享相同的跟踪信息。

`AfterTranslate` 回调会聚合增量文本内容，并将最终结果记录在 span 属性 `output` 中。这确保了用户提示和最终模型输出在智研LLM应用监控显示。

## 智研监控宝 Trace

![zhiyan](../../../../.resources/agui/server/zhiyan/llm-sdk/img/zhiyan.png)
