# Zhiyan LLM (telemetry.zhiyan-llm) tRPC plugin example

This example demonstrates enabling **Zhiyan LLM APM** export in **tRPC-Agent-Go** via **tRPC-Go plugin configuration** (`trpc_go.yaml`).

## Prerequisites

- Go >= 1.21
- A valid Zhiyan LLM API key

## Configure

Edit [`server/trpc_go.yaml`](./server/trpc_go.yaml):

- `plugins.telemetry.zhiyan-llm.api_endpoint`
- `plugins.telemetry.zhiyan-llm.api_key`
- `plugins.telemetry.zhiyan-llm.app_name`

Tip: `trpc_go.yaml` supports environment-variable expansion, so you can keep secrets in env:

```bash
export ZHIYANLLM_API_KEY="key-xxxx"
```

## Optional: set `business_scenario` per request

This example already sets Zhiyan's `business_scenario` to `customer_service` in [`server/chainagent.go`](./server/chainagent.go) before calling `runner.Run(...)`. Replace that hard-coded value with your own request-specific logic if needed.

## Run

### Start server

```bash
cd server
go run . -model "deepseek-chat"
```

### Start client

```bash
cd ../client
go run . -message "write large language model summary" -timeout 60s
```

