---
name: trpc-agent
description: Generate tRPC-Agent-Go project skeleton using trpc agent command.
---

# tRPC Agent Project Generator Skill

Generate Agent project skeleton using `trpc agent` command from trpc-go-cmdline.

## Prerequisites

- trpc-go-cmdline version >= v2.9.3
- Go version >= 1.21
- trpc command installed and available in PATH

## Capabilities

- Generate LLM Agent project with interactive CLI mode
- Generate AGUI service project (HTTP server)
- Generate A2A (Agent-to-Agent) service project
- Generate Debug server project for ADK Web UI
- Generate OpenAI-compatible API service
- Generate Graph-based Agent project for complex workflows
- Auto-generate tools from proto files
- Configure Session/Memory storage backends
- Integrate observability platforms (Galileo, Zhiyan)

## Usage

### Generate Interactive CLI Agent

```bash
trpc agent -o my-agent
cd my-agent
go mod tidy
go run .
```

### Generate AGUI Service

```bash
trpc agent -o my-agui-agent --server agui
cd my-agui-agent
go mod tidy
go run .
```

### Generate A2A Service

```bash
trpc agent -o my-a2a-agent --server a2a
cd my-a2a-agent
go mod tidy
go run .
```

### Generate Debug Server

```bash
trpc agent -o my-debug-agent --server debug
cd my-debug-agent
go mod tidy
go run .
```

### Generate OpenAI Compatible API Service

```bash
trpc agent -o my-openai-agent --server openai
cd my-openai-agent
go mod tidy
go run .
```

### Generate Graph Agent

```bash
trpc agent -o my-graph-agent --agent graph
cd my-graph-agent
go mod tidy
go run .
```

### Generate Agent with Tools from Proto

```bash
trpc agent -o calculator-agent --tool calculator.proto
cd calculator-agent
go mod tidy
go run .
```

### Generate Agent with Observability

```bash
# With Galileo monitoring
trpc agent -o my-agent --opsys galileo

# With Zhiyan monitoring
trpc agent -o my-agent --opsys zhiyan
```

### Configure Session and Memory Storage

```bash
# Use Redis for session and memory
trpc agent -o my-agent --session redis --memory redis

# Use MySQL for persistent storage
trpc agent -o my-agent --session mysql --memory mysql
```

## Parameters

| Option | Short | Default | Description |
|--------|-------|---------|-------------|
| --output | -o | (required) | Output directory |
| --force | -f | false | Overwrite existing directory |
| --agent | | llm | Agent type: llm, graph |
| --server | | none | Server type: none, agui, a2a, debug, openai |
| --model | | openai | Model provider: openai, anthropic |
| --session | | inmemory | Session storage: inmemory, redis, mysql, postgres |
| --memory | | inmemory | Memory storage: inmemory, redis, mysql, postgres |
| --tool | -t | | Proto file path for tool generation |
| --gomod | | (project name) | Go module path |
| --opsys | | | Observability: galileo, zhiyan |

## Generated Project Structure

```
my-agent/
├── agents/
│   ├── agent.go      # Agent creation logic
│   └── tool.go       # Tool definitions (when --tool is used)
├── main.go           # Entry point
├── go.mod            # Go module file
└── trpc_go.yaml      # Service configuration
```

## Installation

```bash
# Install trpc command
go install trpc.tech/trpc-go/trpc-go-cmdline/v2/trpc@latest

# Verify installation
trpc version
```

## Scripts

This skill provides helper scripts under `scripts/`:

- `install.sh` - Install trpc-go-cmdline tool and verify installation
- `generate.sh` - Wrapper for `trpc agent`, passes all arguments directly

## Help

```bash
trpc help agent
```
