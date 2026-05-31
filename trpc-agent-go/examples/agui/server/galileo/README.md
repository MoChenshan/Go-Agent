# Galileo AG-UI Server

This example extends the default AG-UI SSE server with Galileo observability. It streams AG-UI events while exporting traces, metrics, and logs through the Galileo tRPC telemetry plugin that ships with `tRPC-Agent-Go`.

## Prerequisites

- Access to a Galileo deployment and the corresponding OCP configuration; update `trpc_go.yaml` to match your access point, collector URL, and resource metadata.
- Go 1.24 or newer, plus model credentials. Set `OPENAI_API_KEY` (and optionally `OPENAI_BASE_URL` if you use a non-default DeepSeek endpoint).
- An AG-UI client, such as the CLI client in this repo or the Copilotkit web client under `examples/agui/client/copilotkit`.

## Run

Start the server from this directory so the bundled `trpc_go.yaml` is picked up.

```bash
cd examples/agui/server/galileo
export OPENAI_API_KEY="your-deepseek-key"
# export OPENAI_BASE_URL="https://api.deepseek.com/v1" # optional override
go run . -model deepseek-chat -stream=true
```

The server exposes AG-UI SSE on `http://127.0.0.1:8080/agui` by default (port and host come from `trpc_go.yaml`, path is set in `main.go`).

## What Happens During a Request

- The blank import `_ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo"` installs Galileo tracer and meter exporters via the tRPC plugin system on startup.
- The AG-UI runner uses the built-in telemetry hooks from `tRPC-Agent-Go`, so agent execution, tool calls, and model invocations emit spans and metrics automatically.
- Logs follow the same resource and are forwarded through the Galileo log writer configured in `trpc_go.yaml`.

## Observing the Trace in Galileo

Trigger a run from any AG-UI client; you should see the end-to-end trace, model spans, and tool spans in Galileo.

![galileo](../../../.resources/agui/server/galileo/img/galileo.png)
