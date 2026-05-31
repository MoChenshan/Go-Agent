# Changelog

All notable changes to the trpc/telemetry/zhiyan-llm module will be documented in this file.

## [Unreleased]

## [v1.8.2] - 2026-04-20

### Bug Fixes

- **exporter**: Align tool attributes to match zhiyan SDK semantics
  - Updated exporter to build SDK-style `input.value` and `output.value`
  - Extract `gen_ai.request.functions.*` from tool definitions or request payloads
  - Fixed missing or misrendering tool-related fields in LLM spans
  - Improved tool and tool-call metadata visibility in Zhiyan page

### Features

- **tests**: Added focused tests for tool attribute extraction
- **tests**: Enhanced test coverage for tool-calling scenarios

## [v1.8.1] - 2026-04-15

### Features

- telemetry/zhiyan-llm: align exported prompt/completion attributes with `llm_go_sdk` semantics
- telemetry/zhiyan-llm: add exporter tests for prompt/completion and tool call extraction

## [v1.8.0] - 2026-04-07

### Features

- Release v1.8.0 (!585)

### Dependencies

- `trpc.group/trpc-go/trpc-agent-go`: Updated to v1.8.0
- `trpc.group/trpc-go/trpc-mcp-go`: Updated to v0.0.14 (indirect)

## [v1.7.1] - 2026-03-26

### Features

- zhiyan-llm: add `WithBusinessScenario` helper for request-scoped Zhiyan context attributes

## [v1.6.1] - 2026-02-27

### Features

- telemetry/zhiyan-llm: map TTFT and tool name to semantic conventions (!450)

## [v1.6.0] - 2026-02-26

### Features

- Release v1.6.0 (!445)

### Dependencies

- `trpc.group/trpc-go/trpc-agent-go`: Updated to v1.6.0
- Remove unused indirect dependency `github.com/spaolacci/murmur3`

## [v1.5.0] - 2026-02-02

### Features

- Release v1.5.0 (!410)

### Dependencies

- `trpc.group/trpc-go/trpc-agent-go`: Updated to v1.5.0
- `git.woa.com/trpc-go/trpc-mcp-go`: Updated to v0.0.13 (indirect)
- `trpc.group/trpc-go/trpc-mcp-go`: Updated to v0.0.12 (indirect)

## [v1.2.1] - 2026-01-28

### Documentation

- zhiyan-llm: add README.md (!396)

## [v1.2.0] - 2026-01-14

### Features

- Release v1.2.0 (!367)

### Dependencies

- `trpc.group/trpc-go/trpc-agent-go`: Updated to v1.2.0
- `git.woa.com/trpc-go/trpc-mcp-go`: Updated to v0.0.11 (indirect)

## [v1.1.4] - 2026-01-27

### Bug fixes

- telemetry/zhiyan-llm: skip i/o token attributes in span transformation !394

## [v1.1.3] - 2026-01-27

### Bug fixes

- telemetry/zhiyan-llm: remove redundant i/o token attributes (!393)

## [v1.1.2] - 2026-01-14

### Features

- **plugin**: Add Zhiyan LLM tRPC plugin (!357)
- **deps**: Go mod tidy prepare for internal release test (!360)

### Dependencies

- `git.code.oa.com/trpc-go/trpc-go`: Declared as direct dependency (v0.19.3)

## [v1.1.1] - 2025-12-31

### Features

- Release v1.1.1 (!327)

### Dependencies

- `trpc.group/trpc-go/trpc-agent-go`: Updated to v1.1.1

## [v1.1.0] - 2025-12-29

### Features

- Release v1.1.0 (!324)

### Dependencies

- `trpc.group/trpc-go/trpc-agent-go`: Updated to v1.1.0
- `trpc.group/trpc-go/trpc-mcp-go`: Updated to v0.0.11 (indirect)

## [v0.8.0] - 2025-12-18

### Features

- Release v0.8.0 (!310)

### Dependencies

- `trpc.group/trpc-go/trpc-agent-go`: Updated to v0.8.0
- `git.woa.com/trpc-go/trpc-agent-go`: Updated to v0.7.0
- Various indirect dependency updates

## [v0.7.0] - 2025-12-04

### Features

- Release v0.7.0 (!288)

### Dependencies

- `trpc.group/trpc-go/trpc-agent-go`: Updated to v0.7.0

## [v0.6.0] - 2025-11-25

### Features

- Release v0.6.0 (!266)

### Dependencies

- `trpc.group/trpc-go/trpc-agent-go`: Updated to v0.6.0

## [v0.5.1] - 2025-11-21

### Bug Fixes

- **exporter**: Fixed duplicate import of `semconvtrace` package
- **exporter**: Set proper span kind for invoke agent operations (#102, !253)
  - Changed span kind from UNSPECIFIED/INTERNAL to SERVER for agent invocations
  - Ensures correct span classification in distributed tracing
- **exporter**: Fix tool span input/output attributes (!252)
  - Improved attribute extraction for tool execution spans
  - Better handling of tool arguments and results
- **exporter**: Fix chat span attribute transformation (!250)
  - Enhanced LLM request/response attribute processing
  - Improved session ID and user ID handling

## [v0.5.0] - 2025-11-13

### Features

- Release v0.5.0 (!243)
- Updated trpc-agent-go dependency to v0.4.0 (!238)

### Dependencies

- `git.woa.com/trpc-go/trpc-agent-go`: Updated to v0.4.0

## [v0.4.0] - 2025-10-28

### Bug Fixes

- **exporter**: Fix session ID and user ID reporting (!213)
  - Corrected session ID attribute mapping
  - Fixed user ID propagation in telemetry data

### Features

- Release v0.4.0 (!216)

## [v0.3.0] - 2025-10-14

### Features

- Initial release of zhiyan-llm telemetry exporter
- Support for LLM span transformation
- Support for tool execution span transformation
- Support for agent invocation span transformation
- Integration with OpenTelemetry OTLP HTTP exporter
