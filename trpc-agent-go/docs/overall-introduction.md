# tRPC-Agent-Go框架介绍

## 作者：jessemjchen，wineguo，amdahliu，homerpan，nanjianyang，junevanlong，hackerli

## 导语
 tRPC-Go 团队在之前推出了[mcp开发框架](https://git.woa.com/trpc-go/trpc-mcp-go)，以及[A2A开发框架](https://git.woa.com/trpc-go/trpc-a2a-go) ，在公司内外部得到广泛的应用。现在推出[tRPC-Agent-Go框架](https://git.woa.com/trpc-go/trpc-agent-go)，进一步实现tRPC在AI开发框架生态的闭环。

## 背景和技术选型

### 开发背景

随着LLM能力快速提升，Agent开发框架成为连接AI能力与业务应用的重要基础设施。当前框架在技术路线上存在分化，Go语言生态有较大发展空间。

#### 业界框架技术路线分析

目前AI Agent 应用开发框架主要分为两大技术路线：**自主多Agent框架**和**编排式框架**。

**自主多Agent框架**

自主多Agent框架体现了真正的Agent（Autonomous Agent）理念，每个Agent都具备环境感知、自主决策和动作执行能力。多个Agent通过消息传递和协商机制实现分布式协作，能够根据环境变化动态调整策略，展现出智能涌现特性。

- **AutoGen（Microsoft）**: 多Agent协作系统，支持Agent角色专业化和动态协商
- **ADK（Google Agent Development Kit）**: 提供完整的Agent生命周期管理和多Agent编排能力
- **CrewAI**: 面向任务的多Agent协作平台，强调角色定义和责任链模式
- **Agno**: 轻量级高性能Agent框架，专注于多模态能力和团队协作

**编排式框架**

编排式框架采用工作流思维，通过预定义的流程图或状态机来组织LLM调用和组件交互。虽然整个系统表现出"智能"特征，但其执行路径是确定性的，更像是"智能化的工作流"而非真正的自主Agent。

- **LangChain**: 基于Chain抽象的组件编排框架，通过预定义执行链路构建LLM应用
- **LangGraph**: 有向无环图（DAG）状态机框架，提供确定性状态转换和条件分支
- **Eino（字节跳动）**: LLM应用编排框架，基于Pipeline和Graph模式进行流程管理

#### 两种框架类型技术对比

| 对比维度 | 自主多Agent框架 | 编排式框架 |
|---------|----------------|----------------|
| **控制模式** | 分布式自治决策，Agent间协商 | 集中式流程编排，确定性执行 |
| **适用场景** | 开放域问题求解、创造性任务、多专业协作 | 结构化业务流程、数据处理管道、标准化作业 |
| **扩展方式** | 水平扩展Agent角色，垂直增强Agent能力 | 节点扩展和流程图复杂化 |
| **执行可预测性** | 涌现行为，结果多样性高 | 确定性执行，结果可复现 |
| **系统复杂度** | Agent交互复杂，调试困难 | 流程清晰，易于调试和监控 |
| **技术实现** | 基于消息传递和对话协议 | 基于状态机和有向图执行 |

#### 自主多Agent框架的技术特征

现代LLM在复杂推理、动态决策方面能力显著提升，自主多Agent框架相比编排式框架具有以下特征：

- **自适应性**：Agent基于上下文动态调整决策策略和执行路径
- **协作涌现**：多Agent通过消息传递实现去中心化协商和任务分解  
- **认知集成**：深度整合LLM的推理、规划、反思能力形成智能决策链路

#### tRPC-Agent-Go技术定位

**行业与生态现状**：随着LLM能力的持续突破，Agent开发框架正成为AI应用开发的重要趋势。当前主流的自主多Agent框架（如AutoGen、CrewAI、ADK、Agno等）主要基于Python生态构建，为Python开发者提供了丰富的选择。然而，Go语言凭借其卓越的并发性能、内存安全和部署便利性，在微服务架构中占据重要地位。目前较为成熟的Go语言AI开发框架Eino（CloudWeGo）专注于编排式架构，主要适用于结构化业务流程，而自主多Agent框架在Go生态中相对较少，存在发展机会。

基于此现状，tRPC-Agent-Go定位于为Go生态提供自主多Agent框架开发能力：

- **架构特性**: 采用自主多Agent架构模式，充分发挥Go语言并发、高性能等优势
- **生态融合**: 深度集成tRPC微服务生态，复用服务治理、可观测性等基础设施
- **应用适配**: 满足复杂业务场景的智能化改造和部署需求

## tRPC-Agent-Go框架总览

[tRPC-Agent-Go](https://git.woa.com/trpc-go/trpc-agent-go) 框架集成了LLM、智能规划器、会话管理、可观测性和丰富的工具生态系统。支持创建自主Agent和半自主Agent，具备推理能力、工具调用、子Agent协作和长期状态保持能力，为开发者提供构建智能应用的完整技术栈。

### 核心技术特性

- **多样化Agent系统**：提供LLM、Chain、Parallel、Cycle等多种Agent执行模式
- **丰富工具生态**：内置常用工具集，支持自定义扩展和MCP协议标准化集成
- **监控能力**：集成OpenTelemetry标准，支持全链路追踪和性能监控
- **智能会话管理**：支持Session状态持久化，memory记忆管理和知识库集成
- **模块化架构**：清晰的分层设计，便于扩展和定制开发

## 核心模块详解

### Model模块 - 大语言模型抽象层

Model模块提供了统一的LLM接口抽象，支持OpenAI兼容的API调用。通过标准化的接口设计，开发者可以灵活切换不同的模型提供商，实现模型的无缝集成和调用。该模块主要支持了OpenAI like接口的兼容性，已验证公司内外大多数接口。

#### 核心接口设计

```go
// Model是所有语言模型必须实现的接口
type Model interface {
    // 生成内容，支持流式响应
    GenerateContent(ctx context.Context, request *Request) (<-chan *Response, error)
    
    // 返回模型基本信息
    Info() Info
}

// 模型信息结构
type Info struct {
    Name string // 模型名称
}
```

#### OpenAI兼容实现

框架提供了完整的OpenAI兼容实现，支持连接各种OpenAI-like接口：

```go
// 创建OpenAI模型
model := openai.New("gpt-4o-mini",
    openai.WithAPIKey("your-api-key"),
    openai.WithBaseURL("https://api.openai.com/v1"), // 可自定义BaseURL
)

// 支持自定义配置
model := openai.New("custom-model",
    openai.WithAPIKey("your-api-key"),
    openai.WithBaseURL("https://your-custom-endpoint.com/v1"),
    openai.WithChannelBufferSize(512),
    openai.WithExtraFields(map[string]interface{}{
        "custom_param": "value",
    }),
)
```

#### 支持的模型平台

当前框架支持所有提供OpenAI兼容API的模型平台，包括但不限于：

**外部平台**
- **OpenAI** - GPT-4o、GPT-4、GPT-3.5等系列模型
- **腾讯云** - Deeseek,hunyuan系列
- **其他云厂商** - 提供OpenAI兼容接口的各类模型，如deepseek，qwen等

**内部平台**
- **TaiJi** - 太极平台模型服务,deepseek,hunyuan等
- **HunYuan** - 混元
- **Venus** - Venus平台的模型服务，deepseek,claude,openai等

Model 模块的详细介绍请参阅 [Modle](https://iwiki.woa.com/p/4015773853)

### Agent模块 - Agent执行引擎

Agent模块是tRPC-Agent-Go的核心组件，提供智能推理引擎和任务编排能力。该模块具备以下核心功能：

- **多样化Agent类型**：支持LLM、Chain、Parallel、Cycle、Graph等不同执行模式
- **工具调用与集成**：提供丰富的外部能力扩展机制
- **事件驱动架构**：实现流式处理和实时监控  
- **层次化组合**：支持子Agent协作和复杂流程编排
- **状态管理**：确保长对话和会话持久化

Agent模块通过统一接口标准实现高度模块化，为开发者提供从智能对话助手到复杂任务自动化的完整技术支持。

#### 核心接口设计

```go
type Agent interface {
    // 执行Agent调用，返回事件流
    Run(ctx context.Context, invocation *Invocation) (<-chan *event.Event, error)
    
    // 返回Agent可用的工具列表
    Tools() []tool.Tool
    
    // 返回Agent的基本信息
    Info() Info
    
    // 返回子Agent列表，支持层次化组合
    SubAgents() []Agent
    
    // 根据名称查找子Agent
    FindSubAgent(name string) Agent
}
```

#### 多种Agent类型

**LLMAgent - 基础智能Agent**

**核心特点**: 基于LLM的智能Agent，支持工具调用、流式输出和会话管理。

- **执行方式**: 直接与LLM交互，支持单轮对话和多轮会话
- **适用场景**: 智能客服、内容创作、代码助手、数据分析、问答系统
- **优势**: 简单直接、响应快速、配置灵活、易于扩展

```go
agent := llmagent.New(
    "assistant",
    llmagent.WithModel(openai.New("gpt-4o-mini")),
    llmagent.WithInstruction("你是一个专业的AI助手"),
    llmagent.WithTools([]tool.Tool{calculatorTool, searchTool}),
)
```

**ChainAgent - 链式处理Agent**

**核心特点**: 流水线模式，多个Agent按顺序执行，前一个的输出成为后一个的输入。

- **执行方式**: Agent1 → Agent2 → Agent3 顺序执行
- **适用场景**: 文档处理流水线、数据ETL、内容审核链条
- **技术优势**: 专业分工、流程清晰、易于调试

```go
chain := chainagent.New(
    "content-pipeline",
    chainagent.WithSubAgents([]agent.Agent{
        planningAgent,   // 第一步：制定计划
        researchAgent,   // 第二步：收集信息  
        writingAgent,    // 第三步：创作内容
    }),
)
```

**ParallelAgent - 并行处理Agent**

**核心特点**: 并发模式，多个Agent同时执行相同任务，然后合并结果。

- **执行方式**: Agent1 + Agent2 + Agent3 同时执行
- **适用场景**: 多专家评估、多维度分析、决策支持
- **技术优势**: 并发执行、多角度分析、容错性强

```go
parallel := parallelagent.New(
    "multi-expert-evaluation",
    parallelagent.WithSubAgents([]agent.Agent{
        marketAgent,      // 市场分析专家
        technicalAgent,   // 技术评估专家
        financeAgent,     // 财务分析专家
    }),
)
```

**CycleAgent - 循环迭代Agent**

**核心特点**: 迭代模式，通过多轮"执行→评估→改进"循环，不断优化结果。

- **执行方式**: 循环执行直到满足条件或达到最大轮次
- **适用场景**: 复杂问题求解、内容优化、自动调试
- **技术优势**: 自我改进、质量提升、智能停止

```go
cycle := cycleagent.New(
    "problem-solver",
    cycleagent.WithSubAgents([]agent.Agent{
        generatorAgent,  // 生成解决方案
        reviewerAgent,   // 评估质量
    }),
    // 设置最大迭代次数为5，防止无限循环
    cycleagent.WithMaxIterations(5),
)
```

**GraphAgent - 图工作流Agent**

**核心特点**: 基于图的工作流模式，支持条件路由和多节点协作的复杂任务处理。

**设计目的**: 为了满足和兼容腾讯内部之前大多数的AI Agent应用是基于图编排框架进行开发的，方便存量用户迁移，保留已有的开发习惯。

- **执行方式**: 按图结构执行，支持LLM节点、工具节点、条件分支和状态管理
- **适用场景**: 复杂决策流程、多步骤任务协作、动态路由处理、存量图编排应用迁移
- **技术优势**: 灵活路由、状态共享、可视化流程、兼容现有开发模式

```go
// 创建文档处理工作流
stateGraph := graph.NewStateGraph(graph.MessagesStateSchema())

// 创建分析工具
complexityTool := function.NewFunctionTool(
    analyzeComplexity,
    function.WithName("analyze_complexity"),
    function.WithDescription("分析文档复杂度"),
)
tools := map[string]tool.Tool{"analyze_complexity": complexityTool}

// 构建工作流图
g, err := stateGraph.
    AddNode("preprocess", preprocessDocument).          // 预处理节点
    AddLLMNode("analyze", model, 
        "分析文档复杂度，使用analyze_complexity工具", tools). // LLM分析节点
    AddToolsNode("tools", tools).                       // 工具节点
    AddNode("route_complexity", routeComplexity).       // 路由决策节点
    AddLLMNode("summarize", model, "总结复杂文档", nil).  // LLM总结节点
    AddLLMNode("enhance", model, "提升简单文档质量", nil). // LLM增强节点
    AddNode("format_output", formatOutput).             // 格式化节点
    SetEntryPoint("preprocess").                        // 设置入口
    SetFinishPoint("format_output").                    // 设置出口
    AddEdge("preprocess", "analyze").                   // 连接节点
    AddToolsConditionalEdges("analyze", "tools", "route_complexity").
    AddConditionalEdges("route_complexity", complexityCondition, map[string]string{
        "simple":  "enhance",
        "complex": "summarize",
    }).
    AddEdge("enhance", "format_output").
    AddEdge("summarize", "format_output").
    Compile()

// 创建GraphAgent并运行
graphAgent, err := graphagent.New("document-processor", g,
    graphagent.WithDescription("文档处理工作流"),
    graphagent.WithInitialState(graph.State{}),
)

runner := runner.NewRunner("doc-workflow", graphAgent)
events, _ := runner.Run(ctx, userID, sessionID, 
    model.NewUserMessage("处理这个文档内容"))
```

Agent 模块的详细介绍请参阅 [Agent](https://iwiki.woa.com/p/4015773536)、[Multi-Agent](https://iwiki.woa.com/p/4015773672) 和 [Graph](https://iwiki.woa.com/p/4015792386)

### Event模块 - 事件驱动系统

Event模块是tRPC-Agent-Go的事件系统核心，负责Agent执行过程中的状态传递和实时通信。通过统一的事件模型，实现Agent间解耦通信和透明执行监控。

#### 核心特性

- **异步通信**：Agent通过事件流进行非阻塞通信，支持高并发执行
- **实时监控**：所有执行状态通过事件实时传递，支持流式处理
- **统一抽象**：不同类型Agent通过相同事件接口交互
- **多Agent协作**：支持分支事件过滤和状态追踪

#### 核心接口

```go
// Event代表Agent执行过程中的一个事件
type Event struct {
    *model.Response      // 嵌入LLM响应的所有字段
    InvocationID string  // 本次调用的唯一标识
    Author       string  // 事件发起者（Agent名称）
    ID           string  // 事件唯一标识
    Timestamp    time.Time // 事件时间戳
    Branch       string  // 分支标识（多Agent协作）
}
```

#### 主要事件类型

- **`chat.completion`** - LLM对话完成事件
- **`chat.completion.chunk`** - 流式对话事件
- **`tool.response`** - 工具响应事件
- **`agent.transfer`** - Agent转移事件
- **`error`** - 错误事件

#### Agent.Run()与事件处理

所有Agent都通过`Run()`方法返回事件流，实现统一的执行接口：

```go
import (
    "trpc.group/trpc-go/trpc-agent-go/runner"
    "trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
    "trpc.group/trpc-go/trpc-agent-go/model"
)

// Agent接口定义
type Agent interface {
    Run(ctx context.Context, invocation *Invocation) (<-chan *event.Event, error)
}

// 创建Agent并使用Runner执行
agent := llmagent.New("assistant", 
    llmagent.WithModel(model),
    llmagent.WithTools(tools))

// 使用Runner执行Agent（推荐方式）
runner := runner.NewRunner("calculator-app", agent)
events, err := runner.Run(ctx, "user-001", "session-001", 
    model.NewUserMessage("计算 2+3 等于多少"))

// 实时处理事件流
for event := range events {
    switch event.Object {
    case "chat.completion.chunk":
        fmt.Print(event.Choices[0].Delta.Content)
    case "tool.response":
        fmt.Printf("\n[%s] 工具执行完成\n", event.Author)
    case "chat.completion":
        if event.Done && event.Response != nil && 
           event.Response.Object == model.ObjectTypeRunnerCompletion {
            fmt.Printf("\n[%s] 最终答案: %s\n", 
                event.Author, event.Choices[0].Message.Content)
        }
    case "error":
        fmt.Printf("错误: %s\n", event.Error.Message)
        return event.Error
    }
    if event.Done && event.Response != nil && 
       event.Response.Object == model.ObjectTypeRunnerCompletion { break }
}
```

#### 多Agent协作中的事件流

```go
chainAgent := chainagent.New("chain", 
    chainagent.WithSubAgents([]agent.Agent{
        analysisAgent, solutionAgent,
    }))

events, err := chainAgent.Run(ctx, invocation)
if err != nil {
    return err
}

for event := range events {
    switch event.Object {
    case "chat.completion.chunk":
        fmt.Print(event.Choices[0].Delta.Content)
    case "chat.completion":
        if event.Done && event.Response != nil && 
           event.Response.Object == model.ObjectTypeRunnerCompletion {
            fmt.Printf("[%s] 完成: %s\n", event.Author, 
                event.Choices[0].Message.Content)
        }
    case "tool.response":
        fmt.Printf("[%s] 工具执行完成\n", event.Author)
    case "error":
        fmt.Printf("[%s] 错误: %s\n", event.Author, event.Error.Message)
    }
}
```


#### Multi-Agent System - 多Agent协作系统

tRPC-Agent-Go采用SubAgent机制构建多Agent系统，支持多个Agent协作处理复杂任务。

```go
// 创建专业领域Agent
marketAnalyst := llmagent.New("market-analyst",
    llmagent.WithModel(model),
    llmagent.WithInstruction("你是市场分析专家"),
    llmagent.WithTools([]tool.Tool{marketDataTool}))

techArchitect := llmagent.New("tech-architect", 
    llmagent.WithModel(model),
    llmagent.WithInstruction("你是技术架构专家"),
    llmagent.WithTools([]tool.Tool{techAnalysisTool}))

// 串行协作：市场分析 → 技术评估
planningChain := chainagent.New("product-planning",
    chainagent.WithSubAgents([]agent.Agent{
        marketAnalyst, techArchitect,
    }))

// 并行协作：多专家同时评估
expertPanel := parallelagent.New("expert-panel",
    parallelagent.WithSubAgents([]agent.Agent{
        marketAnalyst, techArchitect,
    }))

// 执行多Agent协作
runner := runner.NewRunner("expert-panel-app", masterAgent)
events, err := runner.Run(ctx, "user-001", "session-001", 
    model.NewUserMessage("分析市场，设计产品方案"))
```

Event 模块的详细介绍请参阅 [Event](https://iwiki.woa.com/p/4015814247)

### Planner模块 - 智能规划引擎

Planner模块为Agent提供智能规划能力，通过不同的规划策略增强Agent的推理和决策能力。支持内置思考模型、React结构化规划和自定义显式规划指导三种模式，使Agent能够更好地分解复杂任务和制定执行计划。其中React模式通过"思考-行动"循环和结构化标签，为普通模型提供显式的推理指导，确保Agent能够系统性地处理复杂任务。

#### 核心接口设计

```go
// Planner接口定义了所有规划器必须实现的方法
type Planner interface {
    // 构建规划指令，为LLM请求添加规划相关的系统指令
    BuildPlanningInstruction(
        ctx context.Context,
        invocation *agent.Invocation,
        llmRequest *model.Request,
    ) string
    
    // 处理规划响应，对LLM的响应进行后处理和结构化
    ProcessPlanningResponse(
        ctx context.Context,
        invocation *agent.Invocation,
        response *model.Response,
    ) *model.Response
}
```

#### 内置规划策略

**Builtin Planner - 内置思考规划器**

适用于具有原生思考能力的模型，通过配置模型参数启用内部推理机制：

```go
// 为OpenAI o系列模型配置推理强度
builtinPlanner := builtin.New(builtin.Options{
    ReasoningEffort: stringPtr("medium"), // "low", "medium", "high"
})

// 为Claude/Gemini模型启用思考模式
builtinPlanner := builtin.New(builtin.Options{
    ThinkingEnabled: boolPtr(true),
    ThinkingTokens:  intPtr(1000),
})
```

**React Planner - 结构化规划器**

React（Reasoning and Acting）Planner是一种AI推理模式，通过结构化标签引导模型进行"思考-行动"循环。它将复杂问题分解为四个标准化阶段：制定计划、推理分析、执行行动、提供答案。这种显式的推理过程让Agent能够系统性地处理复杂任务，同时提高决策的可解释性和错误检测能力。


#### 集成到Agent

React Planner可以无缝集成到任何LLMAgent中，为Agent提供结构化的思考能力。集成后，Agent会自动按照React模式的四个阶段来处理用户请求，确保每个复杂任务都能得到系统性的处理。

```go
// 创建带规划能力的Agent
agent := llmagent.New(
    "planning-assistant",
    llmagent.WithModel(openai.New("gpt-4o")),
    llmagent.WithPlanner(reactPlanner), // 集成规划器
    llmagent.WithInstruction("你是一个善于规划的智能助手"),
)

// Agent将自动使用规划器来：
// 1. 为复杂任务制定步骤化计划（PLANNING阶段）
// 2. 在执行过程中进行推理分析（REASONING阶段）
// 3. 调用相应工具执行具体操作（ACTION阶段）
// 4. 整合所有信息提供完整答案（FINAL_ANSWER阶段）
```

**实际应用效果**：
使用React Planner的Agent在处理复杂查询时，会展现出明显的结构化思考特征。例如，当用户询问"帮我制定一个旅行计划"时，Agent会首先分析需求（PLANNING），然后推理最佳路线（REASONING），接着查询具体信息（ACTION），最后提供完整的旅行建议（FINAL_ANSWER）。这种方式不仅提高了回答质量，还让用户能够清楚地看到Agent的思考过程。

#### 自定义规划器

开发者可以实现自定义规划器来满足特定需求：

```go
// 自定义Reflection规划器示例
type ReflectionPlanner struct {
    maxIterations int
}

func (p *ReflectionPlanner) BuildPlanningInstruction(
    ctx context.Context,
    invocation *agent.Invocation,
    llmRequest *model.Request,
) string {
    return `请按以下步骤进行反思式规划：
1. 分析问题并制定初始计划
2. 执行计划并收集结果
3. 反思执行过程，识别问题和改进点
4. 基于反思优化计划并重新执行
5. 重复反思-优化过程直到达到满意结果`
}

func (p *ReflectionPlanner) ProcessPlanningResponse(
    ctx context.Context,
    invocation *agent.Invocation,
    response *model.Response,
) *model.Response {
    // 处理反思内容，提取改进建议
    // 实现反思逻辑...
    return response
}

// 使用自定义规划器
reflectionPlanner := &ReflectionPlanner{maxIterations: 3}
agent := llmagent.New(
    "reflection-agent",
    llmagent.WithModel(model),
    llmagent.WithPlanner(reflectionPlanner), // 使用自定义规划器
)
```

Planner 模块的详细介绍请参阅 [Planner](https://iwiki.woa.com/p/4015773688)

### Tool模块 - 工具调用框架

Tool模块提供了标准化的工具定义、注册和执行机制，使Agent能够与外部世界进行交互。支持同步调用（CallableTool）和流式调用（StreamableTool）两种模式，满足不同场景的技术需求。

#### 核心接口设计

```go
// 基础工具接口
type Tool interface {
    Declaration() *Declaration  // 返回工具元数据
}

// 同步调用工具接口
type CallableTool interface {
    Call(ctx context.Context, jsonArgs []byte) (any, error)
    Tool
}

// 流式工具接口
type StreamableTool interface {
    StreamableCall(ctx context.Context, jsonArgs []byte) (*StreamReader, error)
    Tool
}

```

#### 工具创建示例

```go
// 计算器工具
calculatorTool := function.NewFunctionTool(
    func(ctx context.Context, input struct {
        Operation string  `json:"operation"`
        A         float64 `json:"a"`
        B         float64 `json:"b"`
    }) (struct {
        Result float64 `json:"result"`
    }, error) {
        var result float64
        switch input.Operation {
        case "add":
            result = input.A + input.B
        case "multiply":
            result = input.A * input.B
        case "subtract":
            result = input.A - input.B
        case "divide":
            if input.B != 0 {
                result = input.A / input.B
            } else {
                return struct{Result float64}{}, fmt.Errorf("division by zero")
            }
        default:
            return struct{Result float64}{}, fmt.Errorf("unsupported operation: %s", input.Operation)
        }
        return struct{Result float64}{result}, nil
    },
    function.WithName("calculator"),
    function.WithDescription("执行数学计算"),
)

// 流式日志查询工具类型定义
type logInput struct {
    Query string `json:"query"`
}

type logOutput struct {
    Log string `json:"log"`
}

// 流式日志查询工具
logStreamTool := function.NewStreamableFunctionTool[logInput, logOutput](
    func(input logInput) *tool.StreamReader {
        stream := tool.NewStream(10)
        go func() {
            defer stream.Writer.Close()
            for i := 0; i < 5; i++ {
                chunk := tool.StreamChunk{
                    Content: logOutput{
                        Log: fmt.Sprintf("日志 %d: %s", i+1, input.Query),
                    },
                }
                if stream.Writer.Send(chunk, nil) {
                    return // stream closed
                }
                time.Sleep(50 * time.Millisecond)
            }
        }()
        return stream.Reader
    },
    function.WithName("log_stream"),
    function.WithDescription("流式查询日志"),
)

// 创建多工具Agent
agent := llmagent.New(
    "multi-tool-assistant",
    llmagent.WithModel(model),
    llmagent.WithTools([]tool.Tool{
        calculatorTool,
        logStreamTool,
        duckduckgo.NewTool(),
    }),
)
```

#### MCP工具集成

框架支持各种MCP工具调用，提供多种连接方式。所有MCP工具都通过统一的 `NewMCPToolSet` 函数创建：

```go
// SSE连接的MCP工具集
sseToolSet := mcp.NewMCPToolSet(
    mcp.ConnectionConfig{
        Transport: "sse",
        ServerURL: "https://api.example.com/mcp/sse",
        Headers: map[string]string{
            "Authorization": "Bearer your-token",
        },
        Timeout: 10 * time.Second,
    },
)

// Streamable HTTP连接的MCP工具集
streamableToolSet := mcp.NewMCPToolSet(
    mcp.ConnectionConfig{
        Transport: "streamable_http",
        ServerURL: "https://api.example.com/mcp",
        Timeout: 10 * time.Second,
    },
)

// StdIO连接的MCP工具集
stdioToolSet := mcp.NewMCPToolSet(
    mcp.ConnectionConfig{
        Transport: "stdio",
        Command: "python",
        Args:    []string{"-m", "my_mcp_server"},
        Timeout: 10 * time.Second,
    },
)

agent := llmagent.New(
    "mcp-agent",
    llmagent.WithModel(model),
    llmagent.WithToolSets([]tool.ToolSet{sseToolSet, streamableToolSet, stdioToolSet}),
)
```

Tool 模块的详细介绍请参阅 [Tools](https://iwiki.woa.com/p/4015773568)

### CodeExecutor模块 - 代码执行引擎

CodeExecutor模块为Agent提供代码执行能力，支持在本地环境或Docker容器中执行Python、Bash代码，使Agent具备数据分析、科学计算、脚本自动化等实际工作能力。

#### 核心接口设计

```go
// CodeExecutor是代码执行的核心接口
type CodeExecutor interface {
    ExecuteCode(context.Context, CodeExecutionInput) (CodeExecutionResult, error)
    CodeBlockDelimiter() CodeBlockDelimiter
}

// 代码执行输入和结果
type CodeExecutionInput struct {
    CodeBlocks  []CodeBlock
    ExecutionID string
}

type CodeExecutionResult struct {
    Output      string  // 执行输出
    OutputFiles []File  // 生成的文件
}
```

#### 两种执行器实现

**LocalCodeExecutor - 本地执行器**

直接在本地环境执行代码，适用于开发测试和可信环境：

```go
// 创建本地执行器
localExecutor := local.New(
    local.WithWorkDir("/tmp/code-execution"),
    local.WithTimeout(30*time.Second),
    local.WithCleanTempFiles(true),
)

// 集成到Agent
agent := llmagent.New(
    "data-analyst",
    llmagent.WithModel(model),
    llmagent.WithCodeExecutor(localExecutor), // 集成代码执行器
    llmagent.WithInstruction("你是数据分析师，可以执行Python代码"),
)
```

**ContainerCodeExecutor - 容器执行器**

在隔离的Docker容器中执行代码，提供更高安全性，适用于生产环境：

```go
// 创建容器执行器
containerExecutor, err := container.New(
    container.WithContainerConfig(container.Config{
        Image: "python:3.11-slim",
    }),
    container.WithHostConfig(container.HostConfig{
        AutoRemove:  true,
        NetworkMode: "none",  // 网络隔离
        Resources: container.Resources{
            Memory: 128 * 1024 * 1024,  // 内存限制
        },
    }),
)

agent := llmagent.New(
    "secure-analyst",
    llmagent.WithModel(model),
    llmagent.WithCodeExecutor(containerExecutor), // 使用容器执行器
)
```

#### 自动代码块识别

框架自动从Agent回复中提取markdown代码块并执行：

```go
// Agent回复包含代码块时会自动执行：
// ```python
// import statistics
// data = [1, 2, 3, 4, 5]
// print(f"平均值: {statistics.mean(data)}")
// ```
//
// 支持Python和Bash代码：
// ```bash
// echo "当前时间: $(date)"
// ```
```

#### 使用示例

```go
// 数据分析Agent
dataAgent := llmagent.New(
    "data-scientist",
    llmagent.WithModel(model),
    llmagent.WithCodeExecutor(local.New()),
    llmagent.WithInstruction("你是数据科学家，使用Python标准库进行数据分析"),
)

// 用户提问，Agent自动生成并执行代码
runner := runner.NewRunner("analysis", dataAgent)
events, _ := runner.Run(ctx, userID, sessionID, 
    model.NewUserMessage("分析数据: 23, 45, 12, 67, 34, 89"))

// Agent自动：
// 1. 生成Python分析代码
// 2. 执行代码获取结果  
// 3. 解读分析结果
```

CodeExecutor模块使Agent从纯对话升级为具备实际计算能力的智能助手，支持数据分析、脚本自动化、科学计算等应用场景。

### Runner模块 - Agent执行器

Runner模块是Agent的执行器和运行环境，负责Agent的生命周期管理、会话状态维护和事件流处理。

#### 核心接口

```go
type Runner interface {
    Run(
        ctx context.Context,
        userID string,           // 用户标识
        sessionID string,        // 会话标识  
        message model.Message,   // 输入消息
        runOpts ...agent.RunOptions, // 运行选项
    ) (<-chan *event.Event, error)   // 返回事件流
}
```

#### 使用示例

```go
// 步骤1: 创建Agent
agent := llmagent.New(
    "customer-service-agent",
    llmagent.WithModel(openai.New("gpt-4o-mini")),
    llmagent.WithInstruction("你是专业的客服助手"),
)

// 步骤2: 创建Runner并绑定Agent
runner := runner.NewRunner(
    "customer-service-app",  // 应用名称
    agent,                   // 绑定Agent
)

// 步骤3: 执行对话
events, err := runner.Run(
    context.Background(),
    "user-001",             // 用户ID
    "session-001",          // 会话ID
    model.NewUserMessage("你好，我想咨询产品信息"),
)

// 步骤4: 处理事件流
for event := range events {
    if event.Object == "agent.message" && len(event.Choices) > 0 {
        fmt.Printf("Agent: %s\n", event.Choices[0].Message.Content)
    }
}
```

Runner 模块的详细介绍请参阅 [Runner](https://iwiki.woa.com/p/4015773576)

### Invocation - Agent执行上下文

Invocation是Agent执行的核心上下文对象，封装了单次调用所需的所有信息和状态。它作为Agent.Run()方法的参数，支持事件追踪、状态管理和Agent间协作。

#### 核心结构

```go
type Invocation struct {
	Agent             Agent                    // 要调用的Agent实例
	AgentName         string                   // Agent名称
	InvocationID      string                   // 调用唯一标识
	Branch            string                   // 分支标识符（多Agent协作）
	EndInvocation     bool                     // 是否结束调用
	Session           *session.Session         // 会话状态
	Model             model.Model              // 语言模型
	Message           model.Message            // 用户消息
	EventCompletionCh <-chan string            // 事件完成信号
	RunOptions        RunOptions               // 运行选项
	TransferInfo      *TransferInfo            // Agent转移信息
	AgentCallbacks    *Callbacks               // Agent回调
	ModelCallbacks    *model.Callbacks         // 模型回调
	ToolCallbacks     *tool.Callbacks          // 工具回调
}

type TransferInfo struct {
	TargetAgentName string // 目标Agent名称
	Message         string // 转移消息
	EndInvocation   bool   // 转移后是否结束
}
```

#### 主要功能

- **执行上下文**：Agent标识、调用追踪、分支控制
- **状态管理**：会话历史、模型配置、消息传递
- **事件控制**：异步通信、执行选项
- **Agent协作**：控制权转移、回调机制

#### 使用示例

```go
// 基础调用
invocation := &agent.Invocation{
    AgentName:    "assistant",
    InvocationID: "inv-001",
    Model:        openai.New("gpt-4o-mini"),
    Message:      model.NewUserMessage("你好"),
    Session:      &session.Session{ID: "session-001"},
}
events, err := agent.Run(ctx, invocation)

// Runner自动创建（推荐）
runner := runner.NewRunner("my-app", agent)
events, err := runner.Run(ctx, userID, sessionID, userMessage)

// 上下文获取
invocation, ok := agent.InvocationFromContext(ctx)
```

#### 最佳实践

- 优先使用Runner自动创建Invocation
- 框架会自动填充Model、Callbacks等字段
- 使用transfer工具实现Agent转移，避免直接设置TransferInfo

### Memory模块 - 智能记忆系统

Memory模块为Agent提供持久化的记忆能力，使Agent能够跨会话记住和检索用户信息，提供个性化的交互体验。

#### 工作原理

Agent通过内置的记忆工具自动识别和存储重要信息，支持主题标签分类管理，并在需要时智能检索相关记忆。通过 AppName+UserID 实现多租户隔离，确保用户数据安全。

#### 应用场景

适用于个人助手、客服机器人、教育辅导、项目协作等需要跨会话记忆用户信息的场景，如记住用户偏好、追踪问题解决进度、保存学习计划等。

#### 核心接口

```go
type Service interface {
    // 添加新记忆
    AddMemory(ctx context.Context, userKey UserKey, memory string, topics []string) error
    // 更新现有记忆
    UpdateMemory(ctx context.Context, memoryKey Key, memory string, topics []string) error
    // 删除指定记忆
    DeleteMemory(ctx context.Context, memoryKey Key) error
    // 清空用户所有记忆
    ClearMemories(ctx context.Context, userKey UserKey) error
    // 读取最近记忆
    ReadMemories(ctx context.Context, userKey UserKey, limit int) ([]*Entry, error)
    // 搜索记忆
    SearchMemories(ctx context.Context, userKey UserKey, query string) ([]*Entry, error)
    // 获取记忆工具
    Tools() []tool.Tool
}

// 数据结构
type Entry struct {
    ID        string    `json:"id"`
    AppName   string    `json:"app_name"`
    UserID    string    `json:"user_id"`
    Memory    *Memory   `json:"memory"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type Memory struct {
    Memory      string     `json:"memory"`
    Topics      []string   `json:"topics,omitempty"`
    LastUpdated *time.Time `json:"last_updated,omitempty"`
}
```

#### 快速集成

```go
import (
    "trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
    "trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
)

// 创建记忆服务
memoryService := inmemory.NewMemoryService()

// 创建具备记忆能力的Agent
agent := llmagent.New(
    "memory-bot",
    llmagent.WithModel(model),
    llmagent.WithMemory(memoryService), // 自动注册记忆工具
)
```

#### 内置记忆工具

| 工具名称 | 默认状态 | 功能描述 |
|---------|----------|----------|
| `memory_add` | ✅ 启用 | 添加新的记忆条目 |
| `memory_update` | ✅ 启用 | 更新现有记忆内容 |
| `memory_search` | ✅ 启用 | 根据关键词搜索记忆 |
| `memory_load` | ✅ 启用 | 加载最近的记忆记录 |
| `memory_delete` | ❌ 禁用 | 删除指定记忆条目 |
| `memory_clear` | ❌ 禁用 | 清空用户所有记忆 |

#### 使用示例

```go
// Agent会自动调用记忆工具：

// 记录信息: "我叫张三，住在北京"
// → memory_add("张三住在北京", ["个人信息"])

// 查询信息: "我住在哪里？"  
// → memory_search("住址") → 返回相关记忆

// 更新信息: "我搬到上海了"
// → memory_update(id, "张三住在上海", ["个人信息"])
```

Memory 模块的详细介绍请参阅 [Memory](https://iwiki.woa.com/p/4015773627)

### Session模块 - 会话管理系统

Session模块提供了强大的会话（Session）管理功能，用于维护 Agent 与用户交互过程中的对话历史和上下文信息。通过自动持久化对话记录、智能摘要压缩和灵活的存储后端，会话管理为构建有状态的智能 Agent 提供了完整的基础设施。

#### 定位

Session 用于管理当前会话的上下文，隔离维度为 `<AppName, UserID, SessionID>`，保存这一段对话里的用户消息、Agent 回复、工具调用结果以及基于这些内容生成的简要摘要，用于支撑多轮问答场景。

在同一条对话中，它让多轮问答之间能够自然承接，避免用户在每一轮都重新描述同一个问题或提供相同参数。

#### 核心特性

- **上下文管理**：自动加载历史对话，实现真正的多轮对话
- **会话摘要**：使用 LLM 自动压缩长对话历史，在保留关键上下文的同时降低 token 消耗
- **多存储后端**：支持内存、Redis、PostgreSQL、MySQL 存储
- **自动管理**：集成 Runner 后自动处理会话创建、加载和更新

#### 存储后端

支持内存、Redis、PostgreSQL、MySQL 存储后端。

#### 会话层次结构

```
Application (应用)
├── User Sessions (用户会话)
│   ├── Session 1 (会话1)
│   │   ├── Session Data (会话数据)
│   │   └── Events (事件列表)
│   └── Session 2 (会话2)
│       ├── Session Data (会话数据)
│       └── Events (事件列表)
└── App Data (应用数据)
```

#### 核心接口

```go
// Service定义会话服务的核心接口
type Service interface {
	// CreateSession creates a new session.
	CreateSession(ctx context.Context, key Key, state StateMap, options ...Option) (*Session, error)

	// GetSession gets a session.
	GetSession(ctx context.Context, key Key, options ...Option) (*Session, error)

	// ListSessions lists all sessions by user scope of session key.
	ListSessions(ctx context.Context, userKey UserKey, options ...Option) ([]*Session, error)

	// DeleteSession deletes a session.
	DeleteSession(ctx context.Context, key Key, options ...Option) error

	// AppendEvent appends an event to a session.
	AppendEvent(ctx context.Context, session *Session, event *event.Event, options ...Option) error

	// Close closes the service.
	Close() error

    ...
}
```

#### 存储后端配置示例

```go
import (
    "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
    "trpc.group/trpc-go/trpc-agent-go/session/redis"
    "trpc.group/trpc-go/trpc-agent-go/session/postgres"
    "trpc.group/trpc-go/trpc-agent-go/session/mysql"
)

// 内存存储
sessionService := inmemory.NewSessionService(...)

// Redis存储
sessionService, err := redis.NewService(...)

// PostgreSQL存储
sessionService, err := postgres.NewService(...)

// MySQL存储
sessionService, err := mysql.NewService(...)
```

#### 会话摘要功能

随着对话持续增长，维护完整的事件历史可能会占用大量内存，并可能超出 LLM 的上下文窗口限制。会话摘要功能使用 LLM 自动将历史对话压缩为简洁的摘要：

```go
// 创建摘要器
summarizer := summary.NewSummarizer(
    summaryModel,
    summary.WithChecksAny(                         // 任一条件满足即触发
        summary.CheckEventThreshold(20),           // 超过 20 个事件后触发
        summary.CheckTokenThreshold(4000),         // 超过 4000 个 token 后触发
        summary.CheckTimeThreshold(5*time.Minute), // 5 分钟无活动后触发
    ),
    summary.WithMaxSummaryWords(200),              // 限制摘要在 200 字以内
)

// 配置会话服务
sessionService := inmemory.NewSessionService(
    inmemory.WithSummarizer(summarizer),
)

// 启用摘要注入到 Agent
llmAgent := llmagent.New(
    "my-agent",
    llmagent.WithAddSessionSummary(true),          // 启用摘要注入
)
```

#### 与Runner集成

```go
// 创建Runner并配置会话服务
runner := runner.NewRunner(
    "my-agent",
    llmAgent,
    runner.WithSessionService(sessionService), // 集成会话管理
)

// 使用Runner进行多轮对话
eventChan, err := runner.Run(ctx, userID, sessionID, userMessage)

```

Session 模块的详细介绍请参阅 [Session](https://iwiki.woa.com/p/4015773606)

### Knowledge模块 - 知识管理系统

Knowledge模块是trpc-agent-go中的知识管理核心组件，它实现了完整的RAG（检索增强生成）能力。该模块不仅提供了基础的知识存储和检索功能，还支持多种高级特性：

1. **知识源管理**
   - 支持多种格式的本地文件（Markdown、PDF、TXT等）
   - 支持目录批量导入，自动处理子目录
   - 支持网页抓取，可直接从URL加载内容
   - 智能识别输入类型，自动选择合适的处理器

2. **向量存储**
   - 内存存储：适用于开发和小规模测试
   - PostgreSQL + pgvector：适用于生产环境，支持持久化
   - TcVector：云原生解决方案，适合大规模部署

3. **Embedding**
   - 默认集成OpenAI Embedding模型
   - 支持自定义Embedding模型接入
   - 异步批处理优化性能

4. **智能检索**
   - 基于语义的相似度搜索
   - 支持多轮对话历史上下文
   - 结果重排序提升相关性

#### 核心接口设计

```go
// Knowledge是知识管理的主要接口
type Knowledge interface {
    // Search执行语义搜索并返回相关结果
    Search(ctx context.Context, req *SearchRequest) (*SearchResult, error)
}

// SearchRequest代表带上下文的搜索请求
type SearchRequest struct {
    Query     string                  // 搜索查询文本
    History   []ConversationMessage   // 对话历史用于上下文
    UserID    string                 // 用户标识
    SessionID string                 // 会话标识
}

// SearchResult代表知识搜索的结果
type SearchResult struct {
    Document *document.Document // 匹配的文档
    Score    float64           // 相关性分数
    Text     string            // 文档内容
}
```

#### 与Agent集成

```go
// 创建知识库
kb := knowledge.New(
    knowledge.WithVectorStore(inmemory.New()),
    knowledge.WithEmbedder(openai.New()),
    knowledge.WithSources([]source.Source{
        file.New([]string{"./docs/llm.md"}),
        url.New([]string{"https://wikipedia.org/wiki/LLM"}),
    }),
)

// 加载知识库
kb.Load(ctx)

// 创建带知识库的Agent
agent := llmagent.New(
    "knowledge-assistant",
    llmagent.WithModel(model),
    llmagent.WithKnowledge(kb), // 自动添加knowledge_search工具
    llmagent.WithInstruction("使用knowledge_search工具搜索相关资料来回答问题"),
)
```

Knowledge 模块的详细介绍请参阅 [Knowledge](https://iwiki.woa.com/p/4015773696)

### Observability模块 - 可观测性系统

Observability模块集成OpenTelemetry标准，在Agent执行过程中**自动记录**详细的telemetry数据，支持全链路追踪和性能监控。框架复用OpenTelemetry标准接口，无自定义抽象层。

#### 快速启动

```go
import (
    agentmetric "trpc.group/trpc-go/trpc-agent-go/telemetry/metric"
    agenttrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

func main() {
    ctx := context.Background()
    
    // 启动telemetry收集
    cleanupTrace, _ := agenttrace.Start(ctx)  // 默认localhost:4317
    cleanupMetric, _ := agentmetric.Start(ctx) // 默认localhost:4318
    defer cleanupTrace()
    defer cleanupMetric()
    
    // Agent执行过程将自动记录telemetry数据
    agent := llmagent.New("assistant", 
        llmagent.WithModel(openai.New("gpt-4o-mini")))
    
    runner := runner.NewRunner("app", agent)
    events, _ := runner.Run(ctx, "user-001", "session-001", 
        model.NewUserMessage("你好"))
}
```

#### 自动记录的Trace链路

框架自动创建以下Span层次结构：

```
invocation                              # 对话顶层span
├── call_llm                           # LLM API调用
├── execute_tool calculator            # 工具调用
├── execute_tool search                # 工具调用
└── execute_tool (merged)              # 并行工具调用合并

# GraphAgent执行链路
invocation
└── execute_graph
    ├── execute_node preprocess
    ├── execute_node analyze
    │   └── run_model
    └── execute_node format
```

#### 主要Span属性

- **通用属性**：`invocation_id`, `session_id`, `event_id`
- **LLM调用**：`gen_ai.request.model`, `llm_request/response` JSON
- **工具调用**：`gen_ai.tool.name`, `tool_call_args`, `tool_response` JSON  
- **Graph节点**：`node_id`, `node_name`, `node_description`

#### 配置选项

**自定义端点配置**
```go
cleanupTrace, _ := agenttrace.Start(ctx,
    agenttrace.WithEndpoint("otel-collector:4317"))
```

**自定义Metrics**
```go
counter, _ := metric.Meter.Int64Counter("agent.requests.total")
counter.Add(ctx, 1, metric.WithAttributes(
    attribute.String("agent.name", "assistant")))
```

Observability 模块的详细介绍请参阅 [Observability](https://iwiki.woa.com/p/4015773654)

### Debug Server - ADK Web调试服务器

Debug Server提供HTTP调试服务，兼容ADK Web UI，支持Agent执行的可视化调试和实时监控。

#### 快速启动

```go
// 步骤1: 准备Agent实例
agents := map[string]agent.Agent{
    "chat-assistant": llmagent.New(
        "chat-assistant",
        llmagent.WithModel(openai.New("gpt-4o-mini")),
        llmagent.WithInstruction("你是一个智能助手"),
    ),
}

// 步骤2: 创建Debug Server（包路径：git.woa.com/trpc-go/trpc-agent-go/trpc/server/debug）
debugServer := debug.New(agents)

// 步骤3: 启动HTTP服务器
http.Handle("/", debugServer.Handler())
log.Fatal(http.ListenAndServe(":8080", nil))
```

#### 配置选项

```go
// 可选配置（包路径：git.woa.com/trpc-go/trpc-agent-go/trpc/server/debug）
debugServer := debug.New(agents,
    debug.WithSessionService(redisSessionService), // 自定义会话存储
    debug.WithRunnerOptions(                       // Runner额外配置
        runner.WithObserver(observer),
    ),
)
```

Debug Server 的详细介绍请参阅 [Debug](https://iwiki.woa.com/p/4015773678)

### Callbacks模块 - 回调机制

Callbacks模块提供了一套完整的回调机制，允许在Agent执行、模型推理和工具调用的关键节点进行拦截和处理。通过回调机制，可以实现日志记录、性能监控、内容审核等功能。

#### 回调类型

1. **ModelCallbacks（模型回调）**
   ```go
   // 创建模型回调
   modelCallbacks := model.NewCallbacks().
       RegisterBeforeModel(func(ctx context.Context, req *model.Request) (*model.Response, error) {
           // 模型调用前的处理
           fmt.Printf("🔵 BeforeModel: model=%s, query=%s\n", 
               req.Model, req.LastUserMessage())
           return nil, nil
       }).
       RegisterAfterModel(func(ctx context.Context, req *model.Request, 
           resp *model.Response, err error) (*model.Response, error) {
           // 模型调用后的处理
           fmt.Printf("🟣 AfterModel: model=%s completed\n", req.Model)
           return nil, nil
       })
   ```
   - BeforeModel：模型推理前触发，可用于输入拦截、日志记录
   - AfterModel：每个输出块后触发，可用于内容审核、结果处理

2. **ToolCallbacks（工具回调）**
   ```go
   // 创建工具回调
   toolCallbacks := tool.NewCallbacks().
       RegisterBeforeTool(func(ctx context.Context, name string, 
           decl *tool.Declaration, args []byte) (any, error) {
           // 工具调用前的处理
           fmt.Printf("🟠 BeforeTool: tool=%s, args=%s\n", name, args)
           return nil, nil
       }).
       RegisterAfterTool(func(ctx context.Context, name string,
           decl *tool.Declaration, args []byte, 
           result any, err error) (any, error) {
           // 工具调用后的处理
           fmt.Printf("🟤 AfterTool: tool=%s completed\n", name)
           return nil, nil
       })
   ```
   - BeforeTool：工具调用前触发，可用于参数验证、结果模拟
   - AfterTool：工具调用后触发，可用于结果处理、日志记录

3. **AgentCallbacks（Agent回调）**
   ```go
   // 创建Agent回调
   agentCallbacks := agent.NewCallbacks().
       RegisterBeforeAgent(func(ctx context.Context, 
           inv *agent.Invocation) (*model.Response, error) {
           // Agent执行前的处理
           fmt.Printf("🟢 BeforeAgent: agent=%s starting\n", 
               inv.AgentName)
           return nil, nil
       }).
       RegisterAfterAgent(func(ctx context.Context,
           inv *agent.Invocation, err error) (*model.Response, error) {
           // Agent执行后的处理
           fmt.Printf("🟡 AfterAgent: agent=%s completed\n", 
               inv.AgentName)
           return nil, nil
       })
   ```
   - BeforeAgent：Agent执行前触发，可用于权限检查、输入验证
   - AfterAgent：Agent执行后触发，可用于结果处理、错误处理

#### 使用场景

1. **监控和日志**：记录模型调用、工具使用和Agent执行过程
2. **性能优化**：监控响应时间和资源使用情况
3. **安全和审核**：过滤输入内容，审核输出内容
4. **自定义处理**：格式化结果，重试错误，增强内容

#### 集成示例

```go
// 创建带回调的Agent
agent := llmagent.New(
    "callback-demo",
    llmagent.WithModel(model),
    llmagent.WithModelCallbacks(modelCallbacks),
    llmagent.WithToolCallbacks(toolCallbacks),
    llmagent.WithAgentCallbacks(agentCallbacks),
)

// 创建Runner并执行
runner := runner.NewRunner(
    "callback-app",
    agent,
    runner.WithSessionService(sessionService),
)

// 执行对话
events, err := runner.Run(ctx, userID, sessionID, 
    model.NewUserMessage("Hello"))
```

Callbacks模块通过提供灵活的回调机制，使得Agent的行为更可控、更透明，同时为监控、审核、定制化等需求提供了强大的支持。

Callbacks的详细介绍请参阅 [Callback](https://iwiki.woa.com/p/4015773637)

### A2A集成 - Agent间通信

A2A (Agent-to-Agent) 模块提供Agent间通信能力，支持将tRPC-Agent-Go的Agent快速集成到A2A协议中，实现多Agent协作以及对外暴露能力。

#### 快速启动

```go
// 步骤1: 创建Agent
agent := llmagent.New(
    "my-agent",
    llmagent.WithModel(openai.New("gpt-4o-mini")),
    llmagent.WithInstruction("你是一个智能助手"),
)

// 步骤2: 创建A2A服务器
a2aServer, err := a2a.New(
    a2a.WithAgent(agent),           // 绑定Agent
    a2a.WithHost("localhost:8080"), // 设置监听地址
)
if err != nil {
    log.Fatal(err)
}

// 步骤3: 启动服务器
ctx := context.Background()
if err := a2aServer.Start(ctx); err != nil {
    log.Fatal(err)
}

log.Println("A2A服务器已启动: localhost:8080")
```

#### 内部生态接入

将Agent转化为A2A服务之后既可对外暴露A2A的能力，如果想要接入tRPC内部生态监控A2A请求，这里可以通过内部版本的[trpc-a2a-go](https://git.woa.com/trpc-go/trpc-a2a-go)快速接入tRPC内部生态，实现服务治理、监控、日志等功能。


A2A 集成的详细介绍请参阅 [A2A](https://iwiki.woa.com/p/4015812269)

## 与tRPC生态深度结合

### 内外网版本
tRPC-Agent-Go的主体都在[github版本](https://github.com/trpc-group/trpc-agent-go)开发，内部版本做了tRPC的集成，方便利用tPRC生态和服务治理。

内网版本提供了与GitHub版本完全一致的API，只需在项目中添加一次空白导入：

```go
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc"
```

即可和tRPC以及tRPC生态深度结合，自动启用内网监控和统计等增强功能，其余代码保持不变。

#### 自动替换核心组件

添加空白导入后，GitHub版本的 `model/openai` 包默认 HTTP
客户端会自动替换为内网请求处理器，支持复用
`trpc_go.yaml` 的客户端配置以及北极星等能力：

```go
import "trpc.group/trpc-go/trpc-agent-go/model/openai"

// 支持多种连接模式：
// http://some-domain.com/openai - 保持原生 HTTP 请求语义
// https://some-domain.com/openai - 保持原生 HTTPS 请求语义
// dns://some-domain.com:80/openai - 显式使用 DNS selector
httpModel := openai.New("deepseek-chat")

// polaris://some-service-name/openai 需要配合 http client name
// 使用 trpc_go.yaml 中的客户端配置
polarisModel := openai.New("deepseek-chat",
    openai.WithBaseURL("polaris://some-service-name/openai"),
    openai.WithHTTPClientOptions(
        openai.WithHTTPClientName("some-http-client-name")))
```

注意：`ip://`、`unix://`、`passthrough://` 这类直连 selector
不再通过 `WithBaseURL(...)` 自动映射；如确需使用，请在受信任代码路径中显式传入
`client.WithTarget(...)`。


#### 数据库组件集成

如果使用了依赖数据库的组件（如 knowledge、memory、session 等），只需额外引入相应的包即可自动接入 tRPC 数据库插件生态，支持 trpc_go.yaml 配置、服务发现、监控等能力。

目前支持的数据库组件：

| 数据库 | 导入路径 | 适用组件 | trpc-database 插件 |
|--------|----------|----------|-------------------|
| Redis | `git.woa.com/trpc-go/trpc-agent-go/trpc/storage/redis` | session、memory | [trpc-database/goredis](https://git.woa.com/trpc-go/trpc-database/tree/master/goredis) |
| PostgreSQL | `git.woa.com/trpc-go/trpc-agent-go/trpc/storage/postgres` | session、memory、knowledge (pgvector) | [trpc-database/postgres](https://git.woa.com/trpc-go/trpc-database/tree/master/postgres) |
| MySQL | `git.woa.com/trpc-go/trpc-agent-go/trpc/storage/mysql` | session、memory | [trpc-database/mysql](https://git.woa.com/trpc-go/trpc-database/tree/master/mysql) |
| Elasticsearch | `git.woa.com/trpc-go/trpc-agent-go/trpc/storage/goes` | knowledge | [trpc-database/goes](https://git.woa.com/trpc-go/trpc-database/tree/master/goes) |
| TCVectorDB | `git.woa.com/trpc-go/trpc-agent-go/trpc/storage/tcvector` | knowledge | [trpc-database/tcvectordb](https://git.woa.com/trpc-go/trpc-database/tree/master/tcvectordb) |

```go
// Redis (session/memory)
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/redis"

// PostgreSQL (session/memory/pgvector)
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/postgres"

// MySQL (session/memory)
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/mysql"

// Elasticsearch (knowledge)
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/goes"

// TCVectorDB (knowledge)
import _ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/tcvector"
```

相关示例参考：
+ knowledge 组件接入 tRPC 生态示例: [examples/knowledge/trpc/vectorstores](./examples/knowledge/trpc/vectorstores)
+ session 组件接入 tRPC 生态示例(redis session): [examples/session/trpc](./examples/session/trpc)

### 超时与连接（LLM / MCP / A2A）

- 自动注入 thttp
  - 匿名导入 `_ "git.woa.com/trpc-go/trpc-agent-go/trpc"` 后：
    - OpenAI 模型客户端：默认 HTTP 客户端被替换为 tRPC `thttp`；
    - MCP 客户端：`mcp.NewHTTPReqHandler` 被替换为 tRPC 版本；
    - A2A 客户端：可通过 `a2a-trpc` 的 `NewA2ATRPCHTTPReqHandler` 显式接入（见 a2a.md）。
  - 由此可以直接使用 `trpc_go.yaml` 的 client 配置、北极星寻址、拦截器等。

- 请求级超时策略
  - OpenAI/常规 HTTP：注入的适配器会对 GET/POST 附加 `client.WithTimeout(0)`（不强制 tRPC 请求超时）；
  - MCP：对 GET（SSE）明确附加 `client.WithTimeout(0)`；
  - A2A：`NewA2ATRPCHTTPReqHandler` 可传入 `client.WithTimeout(...)`；
  - 优先级与合并规则：`client.WithTimeout`（代码 Option）会覆盖 `trpc_go.yaml` 的 client/service/method 超时；最终生效为 `min(调用 ctx 的 deadline, Option WithTimeout)`，忽略为 0 的项；若未设置 Option，则为 `min(调用 ctx 的 deadline, YAML 超时)`。
  - 建议：仍以调用的 `context` 统一控制端到端时限。

- 连接空闲超时（IdleTimeout）默认值
  - 客户端：HTTP 传输 `IdleConnTimeout` 默认为 50s；
  - 服务端：`server.service.idletime` 默认为 60s；
  - 说明：Idle 仅在“连接空闲”时生效，活跃的 SSE/流式连接不会触发。

#### MCP 注入与配置示例

```go
import (
    _ "git.woa.com/trpc-go/trpc-agent-go/trpc"          // 启用 tRPC 注入
    mcp "trpc.group/trpc-go/trpc-mcp-go"
)

cli, err := mcp.NewClient(
    "polaris://trpc.mcp.server/mcp",                 // 可用 polaris/dns/ip 等
    mcp.Implementation{Name: "cli", Version: "1.0"},
    mcp.WithServiceName("mcp_client"),               // 对应 trpc_go.yaml 的 client.service.name
    mcp.WithHTTPHeaders(http.Header{"Authorization": []string{"Bearer xxx"}}),
    mcp.WithClientPath("/mcp"),                      // streamable；SSE 则传 "/sse"
    mcp.WithClientGetSSEEnabled(true),               // 启用 GET SSE（兼容旧规）
    mcp.WithSimpleRetry(3),                          // 简单重试；或 mcp.WithRetry(mcp.RetryConfig{...})
)
```

示例 `trpc_go.yaml`：

```yaml
client:
  service:
    - name: mcp_client
      protocol: http
      timeout: 0
      conn_type: httppool
      httppool:
        idle_conn_timeout: 50s
```


### 错误标准化与互转

框架在事件中使用统一的 `ResponseError` 表达错误信息，便于在
不同模块之间传递。为兼容 tRPC 体系里的 `errs.Error`（整型错误
码），提供了互转工具，见
[trpc/errs/convert.go](trpc/errs/convert.go)。

要点：

- `ResponseError.Code` 为字符串；通过互转方法，可安全获取/设置
  tRPC 所需的整型错误码。
- 当 `Code` 非法或缺失时，`FromResponseError` 会回退为
  `trpcerrs.RetUnknown`，保证调用方逻辑稳定。

常见用法：

```go
import (
    trpcerrs "git.code.oa.com/trpc-go/trpc-go/errs"
    agenterrs "git.woa.com/trpc-go/trpc-agent-go/trpc/errs"
)

// 1) ResponseError → tRPC errs
if ev.Error != nil {
    terr := agenterrs.FromResponseError(ev.Error)
    code := trpcerrs.Code(terr) // int
    msg := trpcerrs.Msg(terr)
    _ = code; _ = msg
}

// 2) tRPC errs → ResponseError
src := trpcerrs.New(404, "not found")
re := agenterrs.ToResponseError(src)

// 3) 直接读取整型错误码（带 ok 判定）
if code, ok := agenterrs.CodeFromResponseError(re); ok {
    _ = code
}
```

更多用法见测试：
- [trpc/errs/convert_test.go](trpc/errs/convert_test.go)

## 写在最后

### 致谢

感谢组内同学辛勤开发，感谢提前体验使用trpc-agent-go的业务同学，为trpc-agent-go的顺利推出做出贡献。感谢余老师和钟叔在框架设计过程中的悉心指导和宝贵建议。感谢trpc-python团队与我们一起深入讨论框架设计理念，他们的专业见解为tRPC-Agent-Go的架构设计提供了重要参考。

### 后续规划

tRPC-Agent-Go将持续演进，计划在以下方向进行扩展：

- **Artifacts支持**：集成结构化数据展示和交互能力，支持图表、表格、代码等多种数据格式的可视化展示
- **多模态流式处理**：扩展对音频、图像、视频等多模态数据的流式处理能力，实现更丰富的交互体验
- **多Agent模式扩展**：增加更多Agent协作模式，如竞争式、投票式、层次化决策等高级协作策略
- **生态集成**：深化与tRPC生态的集成，提供更多的组件生态，如Knowledge，memory，tools等等

### 使用与交流

欢迎大家使用tRPC-Agent-Go框架！如需详细的使用文档和示例，请访问：
**使用文档**：https://iwiki.woa.com/p/4015773479

我们建立了技术交流群，欢迎加入讨论框架使用经验、分享最佳实践、提出改进建议。让我们一起推动Go语言在AI Agent领域的发展！
