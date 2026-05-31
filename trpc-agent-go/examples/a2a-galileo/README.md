# A2A with Galileo Observability Integration

This example demonstrates how to integrate the **Galileo observability platform** into Agent-to-Agent (A2A) communication using the tRPC-Agent-Go framework. The example shows how to enable tracing, and metrics collection for multi-agent workflows.

## � Overview

This A2A-Galileo example showcases:
- **Observability**: Full tracing and metrics collection via Galileo platform
- **Multi-Agent System**: Chain of Planning → Research → Writing agents  
- **Telemetry Integration**: Seamless integration of tracing and metrics
- **Production Monitoring**: Real-world observability patterns for agent systems

## 🏗️ Architecture with Observability

```
┌─────────────┐    ┌─────────────────────────────────────┐
│   A2A       │    │           A2A Server                │
│   Client    │───▶│  ┌─────────────────────────────────┐│
│             │    │  │       Chain Agent               ││
└─────────────┘    │  │  ┌─────────┬─────────┬─────────┐││
                   │  │  │Planning │Research │Writing ││││
                   │  │  │ Agent   │ Agent   │ Agent  ││││
                   │  │  └─────────┴─────────┴─────────┘││
                   │  └─────────────────────────────────┘│
                   │           │                         │
                   │           ▼                         │
                   │    [DeepSeek API]                   │
                   └─────────────────┬───────────────────┘
                                     │
                   ┌─────────────────▼───────────────────┐
                   │         Galileo Platform            │
                   │  ┌─────────┬─────────┬─────────────┐│
                   │  │ Traces  │ Metrics │    Logs     ││
                   │  │         │         │     (todo)  ││
                   │  └─────────┴─────────┴─────────────┘│
                   └─────────────────────────────────────┘
```

## 🎯 Key Features

### � **Distributed Tracing**
- End-to-end request tracing across agent chain
- Agent transition visibility  
- Performance bottleneck identification
- Cross-service correlation


## �🚀 Quick Start

### Prerequisites

1. **DeepSeek API Key**: Get your API key from [DeepSeek API](https://api-docs.deepseek.com/)
2. **Galileo Access**: Ensure access to the Galileo observability platform
3. **Go**: Version 1.23.0 or later
4. **tRPC Configuration**: Properly configured `trpc_go.yaml`

### Configuration Setup

1. **Environment Variables**:
   ```bash
   export OPENAI_API_KEY="your-deepseek-api-key"
   export OPENAI_BASE_URL="https://api.deepseek.com/v1"
   ```

2. **Galileo Configuration** (`trpc_go.yaml`):


### Running the Example

1. **Start the A2A Server with Galileo**:
   ```bash
   cd examples/a2a-galileo/server
   go run . -model "deepseek-chat"
   ```

2. **Run the Client**:
   ```bash
   cd examples/a2a-galileo/client  
   go run . -message "write quantum computing summary" -timeout 60s
   ```

## 🔧 tRPC Service and Galileo Integration 


Suitable for services based on the tRPC framework.

#### 1. Prerequisites
Refer to the [Galileo Official Documentation - GO (tRPC) Integration Guide](https://iwiki.woa.com/p/4009274553) to complete basic configuration.

#### 2. Integration Code

```go
// Import telemetry setup for OpenTelemetry integration, setup will be executed at init function
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo"
```

#### 3. Configuration File

Ensure that `trpc_go.yaml` contains the Galileo plugin configuration:

```yaml
plugins:
  telemetry:
    galileo:
      # Galileo related configuration
      verbose: "error"
      # ... other configuration items
```

### Automatic Instrumentation

The framework automatically captures:

1. **Agent Spans**: Individual agent execution traces  
2. **Tool Spans**: Tool invocation and execution
3. **Model Spans**: LLM API call tracing


## 📊 Observability Dashboard

Once running, you can monitor your agents through Galileo dashboards:


### 🔍 **Trace Analysis**

Typical trace structure:
```
A2A Request
├── Planning Agent Execution
│   ├── Model API Call (DeepSeek)
│   └── Response Processing
├── Research Agent Execution  
│   ├── Tool: web_search
│   ├── Tool: knowledge_base
│   └── Model API Call (DeepSeek)
└── Writing Agent Execution
    ├── Model API Call (DeepSeek)
    └── Final Response Generation
```

