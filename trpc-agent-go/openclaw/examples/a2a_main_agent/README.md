# Standalone OpenClaw A2A With Primary Agent

This example uses two separate processes:

1. a standalone `trpc-claw` binary that exposes the native OpenClaw
   A2A surface
2. a primary agent process that connects to that OpenClaw instance as
   an A2A sub-agent

The remote OpenClaw process exposes only the bundled `weather` skill,
which makes the example close to the real IDC-to-sandbox deployment
shape.

## Run

From the `openclaw` directory:

```bash
go build -o ./bin/trpc-claw ./cmd/openclaw

./bin/trpc-claw \
  -conf ./examples/a2a_main_agent/trpc_go.yaml \
  -config ./examples/a2a_main_agent/openclaw.yaml
```

In a second terminal, still from the `openclaw` directory:

```bash
go run ./examples/a2a_main_agent \
  -a2a-url http://127.0.0.1:18080/a2a \
  -model gpt-5.2 \
  -question "What's the weather in Shanghai today?" \
  -follow-up "What about tomorrow?"
```

Typical output looks like:

```text
Remote A2A URL: http://127.0.0.1:18080/a2a
Remote Agent: openclaw-sandbox
Remote Skills: 1

Q1: What's the weather in Shanghai today?
Trace: primary-idc-agent -> transfer_to_agent {"agent_name":"openclaw-sandbox",...}
Trace: openclaw-sandbox -> skill_load
Trace: openclaw-sandbox -> skill_run
A1: ...

Q2: What about tomorrow?
Trace: primary-idc-agent -> transfer_to_agent {"agent_name":"openclaw-sandbox",...}
A2: ...
```

## Notes

- The primary agent uses a real model and a real remote A2A sub-agent.
- The two runs share the same session on the primary side, so the
  follow-up turn should continue the same conversation.
- The bundled weather skill calls external weather data, so `curl`
  needs to be available on the host.
