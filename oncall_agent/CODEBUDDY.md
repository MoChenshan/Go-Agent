# CODEBUDDY.md This file provides guidance to CodeBuddy when working with code in this repository.

## Build & Run Commands

- **Build the whole repo**: `go build ./...`
- **Run the service**: `go run .`
  - Requires tRPC runtime environment plus Rainbow (七彩石) and Wuji (无极) config access.
- **Run all tests**: `go test ./...`
- **Run one package / one test**: `go test ./domain/tools/traceanalysis/... -run TestFunctionName -v`
- **Regenerate Wire output**: `go generate ./wire.go`
  - Do this after changing `wire.go` providers or constructor wiring.
- **Regenerate mocks**: `go generate ./domain/.../`
  - Agent/tool API files use `mockgen` directives.
- **Deploy to test**: `bash upload2test.sh`
  - Script builds a static binary and publishes with `dtools bpatch`.

## Architecture Overview

This repository is a tRPC-Go microservice for Tencent Video Magic (魔方) oncall AI workflows. It combines:
- `trpc-agent-go` for LLM agents and tools
- `trpc-a2a-go` for A2A protocol exposure
- Google Wire for dependency injection
- Rainbow for static/runtime config bootstrap
- Wuji for dynamic tool / agent configuration

Startup flow:
1. `main.go` creates the tRPC server and calls `rainbow.Init()`.
2. `InitApp()` in `wire.go` builds all infra clients, tool dependencies, agents, and protocol servers.
3. `main.go` registers SSE / A2A / AGUI / Debug services.
4. If enabled, WeCom is started in a background goroutine via `app.WeComServer.Run(context.Background())`.

## Layer Structure

```text
main.go / wire.go          -> bootstrap, dependency wiring, service registration
services/                  -> protocol adapters (SSE, A2A, AGUI, Debug, WeCom)
domain/                    -> business logic and orchestration
  agents/                  -> agent packages and prompts
  tools/                   -> local tool implementations
  interfaces/external/     -> ports for infra dependencies
  model/                   -> shared domain types / planner helpers
infrastructure/            -> concrete adapters for config, HTTP, tRPC, MySQL
utils/                     -> shared utility tools
```

Dependency direction is **outer to inner**: `main/services -> domain -> infrastructure adapters via interfaces`.

## Agent Map

Current agent packages under `domain/agents/`:
- `magiconcall`
  - Unified Magic oncall agent.
  - Handles issue diagnosis, config lookup, and general Magic platform Q&A.
  - **Important naming note**: Go package name is `magiconcall`, but runtime agent name is `magic_agent`.
  - This is the main agent exposed through user-facing protocols.
- `cdkagent`
  - CDKey / order query agent.
  - Exposed only through its SSE endpoint.
- `codeanalysis`
  - Internal sub-agent for code analysis.
  - Combines old repo explanation and span-analysis responsibilities.
  - Used as a tool by the Magic agent, not exposed as a standalone external service.

Removed legacy agent directories should stay treated as deleted history, not active architecture: `magic_agent/`, old `magic_oncall_agent/`, `magic_config_agent/`, `rule_engine_agent/`, `repo_agent/`, `span_analysis_agent/`.

## Protocol Exposure

Actual protocol exposure in `main.go` / `wire.go`:
- **SSE**
  - `trpc.magic.oncall_agent.sse` -> `/v1/agent` -> Magic agent
  - `trpc.magic.oncall_agent.cdkey_sse` -> `/v1/cdkey_agent` -> CDKey agent
- **A2A**
  - `trpc.magic.oncall_agent.a2a` -> Magic agent only
- **AGUI**
  - `trpc.magic.oncall_agent.agui` -> Magic agent only
- **Debug service**
  - Registered from `services/debug`, used mainly for `magictool` inspection
- **WeCom**
  - Optional
  - Built in `services/wecom`
  - Created only when `cfg.WeComEnabled` is true
  - Uses the Magic agent, not the CDKey agent

`codeanalysis` is deliberately **not** registered as its own SSE/A2A/AGUI service.

## Tool System

Tools come from three places:
- **Local Go tools** in `domain/tools/`
  - Examples: `traceanalysis`, `logquery`, `lingshanquery`, `cdkeyquery`, `magictool`, `mcptool`
- **MCP tool sets** loaded dynamically from Wuji config
- **Sub-agent tools** created by wrapping another agent as a tool
  - Example: `codeanalysis`

Important wiring rule:
- `tool.Tool` instances are **not** Wire providers.
- Each `provide*AgentDep` function constructs its own tools inline.
- This avoids Wire ambiguity because many constructors return the same `tool.Tool` type.

Important Magic-agent behavior:
- The Magic agent intentionally exposes **read-only** config lookup.
- `get_magic_mod_type_info` and `get_magic_act_info` are wired.
- `propose_config_change` is intentionally **not** included.

## Configuration Sources

### Rainbow (`infrastructure/config/rainbow`)
Rainbow provides the app bootstrap config, including:
- OpenAI model settings
- Galileo / Lingshan credentials
- trace analysis limits
- session summarization thresholds
- CDKey query credentials
- WeCom config (`wecom_enabled`, bot id/secret, websocket url, stream flag)

### Wuji (`infrastructure/config/wuji`)
Wuji provides dynamic runtime configuration for:
- **MCP tools**: table `mcp_tool`, filtered by `valid=1`- **Agent config**: table `agent_config`, filtered by `is_valid=1`
- **Local tool config**: table `local_tool`

Each agent checks Wuji at init time and can override:
- description
- system prompt
- input schema
- local tool description

## Wire / Dependency Injection Rules

`wire.go` is the source of truth for application assembly.

Key patterns:
- Infra providers return unique interface types, which keeps Wire resolution unambiguous.
- Agent constructors return the shared `agent.Agent` interface, so they are **not** directly registered as Wire providers.
- Instead, Wire builds each unique `Dep` struct, and `provideApp()` manually calls each agent package's `New()`.
- After editing `wire.go`, regenerate `wire_gen.go` with `go generate ./wire.go`.

## Practical Repo Conventions

- **Follow surrounding import style**: this repo currently mixes `trpc.group/...`, `git.woa.com/...`, and `git.code.oa.com/...` imports depending on dependency source. Keep edits consistent with nearby files instead of normalizing unrelated imports.
- **Do not re-introduce removed agents**: `repo_agent` and `span_analysis_agent` were merged into `code_analysis_agent`.
- **Be careful with naming differences**:
  - package path may differ from runtime agent name
  - example: package `magiconcall`, runtime agent name `magic_agent`, SSE session label still uses `"magic_oncall_agent"`
- **When changing prompt or schema behavior**, check both:
  - embedded defaults in the agent package
  - Wuji dynamic overrides
- **Forked dependency** (`git.woa.com/youngjin/trpc-agent-go`):
  - Fork of `git.woa.com/trpc-go/trpc-agent-go`, referenced via `replace` in `go.mod`.
  - **Patch**: `trpc/server/wecom/server.go` and `options.go` — added `WithShowToolCalls` option to display tool call names (e.g. "*🔧 Calling tool: tool_name*") in the WeCom reply stream.
  - When upgrading the upstream version, re-apply this patch or check if the feature has been merged upstream.

## Files Worth Checking First

When debugging or extending behavior, start here:
- `main.go` - protocol registration and process startup
- `wire.go` - all dependency assembly and service exposure
- `domain/agents/magiconcall/` - main Magic agent
- `domain/agents/codeanalysis/` - internal code-analysis sub-agent
- `domain/tools/magictool/` - Magic config read tools
- `services/sse/` - SSE streaming behavior
- `services/wecom/` - WeCom bot bridge
- `infrastructure/config/rainbow/types.go` - Rainbow config schema
- `README.md` - operational notes and troubleshooting context
