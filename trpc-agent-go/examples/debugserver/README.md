# Debug Server Example

This example demonstrates how to create a standalone HTTP server that's compatible with [ADK Web UI](https://github.com/google/adk-web). It showcases the integration of trpc-agent-go with trpc-go framework and fully aligns with the API interface expected by ADK Web.

## API Specification

The debug server implements the core endpoints required by ADK Web for basic agent interactions, following the [OpenAPI specification](../../trpc/server/debug/openapi.json). Note that some advanced features like image generation, detailed tracing, and extended metadata may not be fully implemented.

## Prerequisites

- Go 1.21 or later
- NodeJS & npm (for running ADK Web UI)

## Features

- **HTTP Server**: Compatible with ADK Web UI for manual testing
- **LLM Agent**: Uses DeepSeek Chat model with calculator and time tools
- **trpc-go Integration**: Leverages trpc-go framework for HTTP service
- **CORS Support**: Built-in CORS middleware for web compatibility

## Configuration

The server uses `trpc_go.yaml` for configuration:

```yaml
server:
  service:
    - name: trpc.test.debug.stdhttp
      ip: 127.0.0.1
      port: 8000
      protocol: http_no_protocol
      timeout: 0 # 0 means no timeout for LLM calls
```

This example uses internal packages. Import the required packages:

```go
import (
	// Import as a side effect to automatically use the internal utilities.
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	// Import the trpc-go package to create a new tRPC server.
	"git.code.oa.com/trpc-go/trpc-go"
	// Import the trpc-go/http package to register the server handler for HTTP.
	thttp "git.code.oa.com/trpc-go/trpc-go/http"
)
```

## Running the Server

```bash
# From repository root
cd examples/debugserver

# Start the server (uses trpc_go.yaml configuration)
go run .

# The server will start on the IP:port specified in trpc_go.yaml
# Default: http://127.0.0.1:8000
```

## Running ADK Web UI

Clone and serve the front-end (once):

```bash
git clone https://github.com/google/adk-web.git
cd adk-web
npm install

# Point the UI to our Go backend
npm run serve --backend=http://localhost:8000 -- --port=4200 --host=localhost
```

Open <http://localhost:4200> in your browser. 
The `--port` and `--host` options define the address and port that ADK Web will listen on.
In the left sidebar choose the `assistant` application, create a new session and start chatting. Messages will be
sent to the Go server which streams responses in real-time via the `/run_sse`
endpoint.

## API Endpoints

The server exposes the following core endpoints for ADK Web:

- `GET /list-apps` - List available agents
- `POST /run` - Execute agent with tools
- `POST /run_sse` - Execute agent with Server-Sent Events streaming
- `GET /apps/{appName}/users/{userId}/sessions` - List sessions
- `POST /apps/{appName}/users/{userId}/sessions` - Create session
- `GET /apps/{appName}/users/{userId}/sessions/{sessionId}` - Get session

Note: This implementation focuses on basic agent functionality. Advanced features like image generation, detailed execution traces, and comprehensive metadata may require additional implementation.

## Tools

The example agent includes two tools:

1. **Calculator**: Perform basic mathematical operations
2. **Time**: Get current time and date for different timezones

## Notes

- The server automatically loads configuration from `trpc_go.yaml`
- CORS is enabled for web browser compatibility
- LLM calls have no timeout (timeout: 0) to accommodate long model response times

---

Feel free to replace the agent logic or add more tools in `main.go` as needed.
