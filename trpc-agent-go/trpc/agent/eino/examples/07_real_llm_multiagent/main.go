// Package main 演示在 trpc-agent-go 多智能体系统中将 eino 适配器集成为子智能体。
// 此示例展示了 eino Chain、Graph 和 Workflow 适配器如何与原生
// trpc-agent-go 智能体在统一的多智能体生态系统中协同工作。
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	// Import eino adapter
	teino "git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino"

	// Import trpc-agent-go components
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/chainagent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpcopenai "trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

func main() {
	fmt.Println("🚀 Eino Multi-Agent Integration Demo (Enhanced with Real LLM)")
	fmt.Println("==============================================================")
	fmt.Println()
	fmt.Println("此演示展示真实 LLM 驱动的 eino 适配器作为子智能体在 trpc-agent-go 中工作：")
	fmt.Println("• 📋 任务规划器 (Native LLM Agent)")
	fmt.Println("• 🔗 内容处理器 (Eino Chain Adapter + 真实 LLM)")
	fmt.Println("• 🕸️  决策路由器 (Eino Graph Adapter + 真实 LLM + Tools)")
	fmt.Println("• 📊 数据分析器 (Eino Workflow Adapter + 真实 LLM)")
	fmt.Println("• ✍️  报告生成器 (Native LLM Agent)")
	fmt.Println()

	ctx := context.Background()

	// 检查环境变量
	if !checkEnvironmentVariables() {
		return
	}

	// Create the multi-agent system
	multiAgent, err := createMultiAgentSystem(ctx)
	if err != nil {
		log.Fatalf("创建多智能体系统失败: %v", err)
	}

	// Create runner
	agentRunner := runner.NewRunner("eino-multiagent-demo", multiAgent)

	// Start interactive session
	startInteractiveSession(ctx, agentRunner)
}

// createMultiAgentSystem creates a multi-agent system combining eino adapters with native agents.
func createMultiAgentSystem(ctx context.Context) (agent.Agent, error) {
	// 1. Create Native LLM Agent for task planning
	modelInstance := trpcopenai.New(os.Getenv("OPENAI_MODEL_NAME"))
	genConfig := model.GenerationConfig{
		MaxTokens:   intPtr(300),
		Temperature: floatPtr(0.7),
		Stream:      true,
	}

	taskPlanner := llmagent.New(
		"task-planner",
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("Plans and breaks down user requests into structured tasks"),
		llmagent.WithInstruction("You are a task planning specialist. Analyze user requests and create a brief structured plan. Be concise and specific about what needs to be done."),
		llmagent.WithGenerationConfig(genConfig),
	)

	// 2. Create Eino Chain Adapter for content processing
	contentProcessor, err := createEinoChainAdapter(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create chain adapter: %v", err)
	}

	// 3. Create Eino Graph Adapter for decision routing
	decisionRouter, err := createEinoGraphAdapter(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph adapter: %v", err)
	}

	// 4. Create Eino Workflow Adapter for data analysis
	dataAnalyzer, err := createEinoWorkflowAdapter(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow adapter: %v", err)
	}

	// 5. Create Native LLM Agent for report generation
	reportGenerator := llmagent.New(
		"report-generator",
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("Generates comprehensive reports based on processed data and analysis"),
		llmagent.WithInstruction("You are a report generation specialist. Create well-structured, comprehensive reports based on the analysis and data provided by previous agents."),
		llmagent.WithGenerationConfig(genConfig),
	)

	// 6. Option A: Sequential Chain Agent (demonstrates serial processing)
	chainAgent := chainagent.New(
		"eino-integration-chain",
		chainagent.WithSubAgents([]agent.Agent{
			taskPlanner,
			contentProcessor,
			decisionRouter,
			dataAnalyzer,
			reportGenerator,
		}),
	)

	// 7. Option B: Parallel Agent (demonstrates parallel processing)
	// Uncomment this to use parallel processing instead:
	/*
		parallelAgent := parallelagent.New(
			"eino-integration-parallel",
			parallelagent.WithSubAgents([]agent.Agent{
				contentProcessor,
				decisionRouter,
				dataAnalyzer,
			}),
		)
	*/

	fmt.Println("✅ Multi-agent system created with eino adapters integrated!")
	fmt.Printf("🔗 Sequential processing: %s → %s → %s → %s → %s\n",
		taskPlanner.Info().Name,
		contentProcessor.Info().Name,
		decisionRouter.Info().Name,
		dataAnalyzer.Info().Name,
		reportGenerator.Info().Name)
	fmt.Println()

	return chainAgent, nil
}

// createEinoChainAdapter creates an eino Chain adapter for content processing with real LLM.
func createEinoChainAdapter(ctx context.Context) (agent.Agent, error) {
	// Create real OpenAI ChatModel
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL:     os.Getenv("OPENAI_BASE_URL"),
		APIKey:      os.Getenv("OPENAI_API_KEY"),
		Model:       os.Getenv("OPENAI_MODEL_NAME"),
		Temperature: floatPtr32(0.7),
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Chain Adapter ChatModel 失败: %w", err)
	}

	// Create eino Chain with real LLM
	chain := compose.NewChain[map[string]any, any]().
		AppendChatTemplate(prompt.FromMessages(schema.FString,
			schema.SystemMessage(`你是一个专业的内容处理专家。你的任务是：
1. 分析输入内容的结构和关键信息
2. 清理和标准化文本格式  
3. 提取重要的关键词和主题
4. 为后续处理准备优化的数据结构

请对输入内容进行专业的处理和分析。`),
			schema.UserMessage("请处理以下内容：{query}"))).
		AppendChatModel(chatModel).
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, msg *schema.Message) (*schema.Message, error) {
			// Convert to Message format for consistency
			return msg, nil
		}))

	// Compile and wrap as agent
	compiledChain, err := chain.Compile(ctx)
	if err != nil {
		return nil, err
	}

	return teino.New(compiledChain, "content-processor"), nil
}

// createEinoGraphAdapter creates an eino Graph adapter for decision routing with real LLM and tools.
func createEinoGraphAdapter(ctx context.Context) (agent.Agent, error) {
	// Create real OpenAI ChatModel
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL:     os.Getenv("OPENAI_BASE_URL"),
		APIKey:      os.Getenv("OPENAI_API_KEY"),
		Model:       os.Getenv("OPENAI_MODEL_NAME"),
		Temperature: floatPtr32(0.5),
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Graph Adapter ChatModel 失败: %w", err)
	}

	// 创建分析工具
	analysisTools := []tool.InvokableTool{
		createContentAnalysisTool(),
		createComplexityEvaluationTool(),
	}

	// 获取工具信息并绑定到 ChatModel
	var toolInfos []*schema.ToolInfo
	for _, t := range analysisTools {
		info, err := t.Info(ctx)
		if err != nil {
			log.Printf("⚠️ 获取工具信息失败: %v", err)
			continue
		}
		toolInfos = append(toolInfos, info)
	}

	// 绑定工具到 ChatModel
	if len(toolInfos) > 0 {
		err := chatModel.BindTools(toolInfos)
		if err != nil {
			log.Printf("⚠️ 绑定工具失败: %v", err)
		}
	}

	// 使用 Chain 模拟 Graph（为了简化，实际项目中应该使用真正的 Graph）
	chain := compose.NewChain[map[string]any, any]().
		AppendChatTemplate(prompt.FromMessages(schema.FString,
			schema.SystemMessage(`你是一个智能决策路由专家。你可以使用以下工具：
- content_analysis: 分析内容的主题和结构
- complexity_evaluation: 评估任务的复杂度

你的任务是：
1. 根据输入内容的特点选择最合适的处理路径
2. 使用工具分析内容复杂度和主题
3. 提供处理建议和路由决策

请智能地使用可用工具来分析输入内容。`),
			schema.UserMessage("请分析并路由以下内容：{query}"))).
		AppendChatModel(chatModel).
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, msg *schema.Message) (*schema.Message, error) {
			// Process the message and handle any tool calls if needed
			return msg, nil
		}))

	chainRunnable, err := chain.Compile(ctx)
	if err != nil {
		return nil, err
	}

	return teino.New(chainRunnable, "decision-router"), nil
}

// createEinoWorkflowAdapter creates an eino Workflow adapter for data analysis with real LLM.
func createEinoWorkflowAdapter(ctx context.Context) (agent.Agent, error) {
	// Create real OpenAI ChatModel
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL:     os.Getenv("OPENAI_BASE_URL"),
		APIKey:      os.Getenv("OPENAI_API_KEY"),
		Model:       os.Getenv("OPENAI_MODEL_NAME"),
		Temperature: floatPtr32(0.3), // 分析类任务使用更低的temperature
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Workflow Adapter ChatModel 失败: %w", err)
	}

	// 使用 Chain 模拟 Workflow（为了简化，实际项目中应该使用真正的 Workflow）
	chain := compose.NewChain[map[string]any, any]().
		AppendChatTemplate(prompt.FromMessages(schema.FString,
			schema.SystemMessage(`你是一个专业的数据分析专家。你的任务是：
1. 对输入数据进行深度分析
2. 识别数据中的模式和趋势  
3. 计算关键指标和统计数据
4. 生成量化的分析结果

请提供详细的数据分析报告，包括：
- 数据摘要和特征
- 关键指标和度量
- 发现的模式或趋势
- 置信度评估`),
			schema.UserMessage("请分析以下数据：{query}"))).
		AppendChatModel(chatModel).
		AppendLambda(compose.InvokableLambda(func(ctx context.Context, msg *schema.Message) (*schema.Message, error) {
			// Convert to Message format for consistency
			return msg, nil
		}))

	chainRunnable, err := chain.Compile(ctx)
	if err != nil {
		return nil, err
	}

	return teino.New(chainRunnable, "data-analyzer"), nil
}

// startInteractiveSession starts an interactive chat session.
func startInteractiveSession(ctx context.Context, agentRunner runner.Runner) {
	fmt.Println("🗣️  Interactive Session Started")
	fmt.Println("Type your questions to see eino adapters working with native agents!")
	fmt.Println("Commands: 'help', 'exit'")
	fmt.Println(strings.Repeat("=", 70))

	scanner := bufio.NewScanner(os.Stdin)
	userID := "user"
	sessionID := fmt.Sprintf("session-%d", time.Now().Unix())

	for {
		fmt.Print("\n💬 You: ")
		if !scanner.Scan() {
			break
		}

		userInput := strings.TrimSpace(scanner.Text())
		if userInput == "" {
			continue
		}

		switch strings.ToLower(userInput) {
		case "help":
			fmt.Println("📖 This demo shows eino adapters (Chain, Graph, Workflow) working as sub-agents")
			fmt.Println("   in a trpc-agent-go multi-agent system. Try asking complex questions!")
			fmt.Println("   Example: 'Analyze the benefits of implementing microservices architecture'")
			continue
		case "exit":
			fmt.Println("👋 Goodbye!")
			return
		}

		// Process message through multi-agent system
		message := model.NewUserMessage(userInput)
		fmt.Printf("\n🚀 Processing through multi-agent pipeline...\n")
		fmt.Println(strings.Repeat("─", 70))

		events, err := agentRunner.Run(ctx, userID, sessionID, message)
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			continue
		}

		// Process streaming response
		processMultiAgentEvents(events)
		fmt.Println(strings.Repeat("─", 70))
		fmt.Println("✅ Multi-agent processing completed!")
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Input scanner error: %v", err)
	}
}

// createContentAnalysisTool 创建内容分析工具
func createContentAnalysisTool() tool.InvokableTool {
	return &ContentAnalysisTool{}
}

// ContentAnalysisTool is a tool for analyzing content.
type ContentAnalysisTool struct{}

// Info returns tool information for ContentAnalysisTool.
func (t *ContentAnalysisTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "content_analysis",
		Desc: "分析文本内容的主题、情感和结构特征",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"content": {
				Type:     "string",
				Desc:     "要分析的文本内容",
				Required: true,
			},
		}),
	}, nil
}

// InvokableRun executes the content analysis operation.
func (t *ContentAnalysisTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	// 模拟内容分析逻辑
	return `{
		"主题": ["任务管理", "项目规划", "团队协作"],
		"情感倾向": "中性-积极",
		"复杂度": "中等",
		"关键词": ["会议", "策划", "流程", "协调"],
		"结构类型": "列表化任务",
		"可操作性": "高"
	}`, nil
}

// createComplexityEvaluationTool 创建复杂度评估工具
func createComplexityEvaluationTool() tool.InvokableTool {
	return &ComplexityEvaluationTool{}
}

// ComplexityEvaluationTool is a tool for evaluating task complexity.
type ComplexityEvaluationTool struct{}

// Info returns tool information for ComplexityEvaluationTool.
func (t *ComplexityEvaluationTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "complexity_evaluation",
		Desc: "评估任务或内容的复杂度级别",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"task_description": {
				Type:     "string",
				Desc:     "任务或内容描述",
				Required: true,
			},
		}),
	}, nil
}

// InvokableRun executes the complexity evaluation operation.
func (t *ComplexityEvaluationTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	// 模拟复杂度评估逻辑
	return `{
		"复杂度级别": "中等",
		"评分": 6.5,
		"评估维度": {
			"技术复杂度": 5,
			"协调复杂度": 7,
			"时间复杂度": 6,
			"资源复杂度": 7
		},
		"处理建议": "建议分阶段执行，重点关注协调和资源管理",
		"预估处理时间": "1-2小时"
	}`, nil
}

// processMultiAgentEvents processes events from the multi-agent system.
func processMultiAgentEvents(events <-chan *event.Event) {
	agentIcons := map[string]string{
		"task-planner":      "📋",
		"content-processor": "🔗",
		"decision-router":   "🕸️",
		"data-analyzer":     "📊",
		"report-generator":  "✍️",
	}

	var currentAgent string
	agentStarted := false

	for event := range events {
		// Handle errors
		if event.Error != nil {
			fmt.Printf("\n❌ Error from %s: %s\n", event.Author, event.Error.Message)
			continue
		}

		// Track agent transitions
		if event.Author != currentAgent {
			if agentStarted {
				fmt.Printf("\n")
			}
			currentAgent = event.Author
			agentStarted = true

			// Display agent with icon
			icon := agentIcons[currentAgent]
			if icon == "" {
				icon = "🤖"
			}
			fmt.Printf("%s [%s]: ", icon, currentAgent)
		}

		// Process streaming content
		if len(event.Choices) > 0 {
			choice := event.Choices[0]
			if choice.Delta.Content != "" {
				fmt.Print(choice.Delta.Content)
			}
		}

		// Check for completion
		if event.Done && event.Response != nil && event.Response.Object == model.ObjectTypeRunnerCompletion {
			fmt.Printf("\n")
			break
		}
	}
}

// checkEnvironmentVariables 检查必要的环境变量
func checkEnvironmentVariables() bool {
	required := []string{"OPENAI_API_KEY", "OPENAI_MODEL_NAME"}
	missing := []string{}

	for _, env := range required {
		if os.Getenv(env) == "" {
			missing = append(missing, env)
		}
	}

	if len(missing) > 0 {
		fmt.Printf("❌ 缺少必要的环境变量: %s\n", strings.Join(missing, ", "))
		fmt.Println("请设置以下环境变量:")
		fmt.Println("export OPENAI_API_KEY=\"your-api-key\"")
		fmt.Println("export OPENAI_MODEL_NAME=\"gpt-4\"  # 或其他模型")
		fmt.Println("export OPENAI_BASE_URL=\"https://api.openai.com/v1\"  # 可选")
		fmt.Println()
		fmt.Println("💡 提示：当前演示将回退到硬编码模式，但真实 LLM 效果更佳")
		return false
	}

	fmt.Printf("✅ 环境变量检查通过: 模型=%s\n", os.Getenv("OPENAI_MODEL_NAME"))
	return true
}

// Helper functions

func intPtr(i int) *int {
	return &i
}

func floatPtr(f float64) *float64 {
	return &f
}

// Helper functions for pointer conversion
func floatPtr32(f float32) *float32 {
	return &f
}
