package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/chainagent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	defaultChannelBufferSize = 256
	maxTokens                = 500 // Reduced for faster, more concise responses
	temperature              = 0.7
)

// chainChat manages the multi-agent conversation.
type chainChat struct {
	modelName string
	runner    runner.Runner
	userID    string
	sessionID string
}

func newChainChat(ctx context.Context, modelName string) (*chainChat, error) {
	c := &chainChat{modelName: modelName}
	// Create OpenAI model.
	modelInstance := openai.New(c.modelName)

	// Create shared tools for research agent.
	webSearchTool := function.NewFunctionTool(
		c.webSearch,
		function.WithName("web_search"),
		function.WithDescription("Search the web for current information on any topic"),
	)
	knowledgeTool := function.NewFunctionTool(
		c.queryKnowledge,
		function.WithName("knowledge_base"),
		function.WithDescription("Query internal knowledge base for factual information"),
	)

	// Create generation config.
	genConfig := model.GenerationConfig{
		MaxTokens:   intPtr(maxTokens),
		Temperature: floatPtr(temperature),
		Stream:      true,
	}

	// Create Planning Agent.
	planningAgent := llmagent.New(
		"planning-agent",
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("Analyzes user requests and creates structured plans"),
		llmagent.WithInstruction("You are a planning specialist. Analyze the user's request and create a brief, structured plan (2-3 steps max). Be concise and specific about what needs to be done. Keep your response under 100 words."),
		llmagent.WithGenerationConfig(genConfig),
	)

	// Create Research Agent with tools.
	researchAgent := llmagent.New(
		"research-agent",
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("Gathers information using available tools and resources"),
		llmagent.WithInstruction("You are a research specialist. Use the available tools to gather key information. Be concise and fact-based. Keep your response under 150 words."),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithTools([]tool.Tool{webSearchTool, knowledgeTool}),
	)

	// Create Writing Agent.
	writingAgent := llmagent.New(
		"writing-agent",
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("Composes final responses based on planning and research"),
		llmagent.WithInstruction("You are a writing specialist. Create a brief, well-structured response based on the plan and research from previous agents. Be clear and concise. Keep your response under 200 words."),
		llmagent.WithGenerationConfig(genConfig),
	)

	// Create Chain Agent with sub-agents.
	chainAgent := chainagent.New("multi-agent-chain",
		chainagent.WithSubAgents([]agent.Agent{planningAgent, researchAgent, writingAgent}),
	)

	// Create runner with the chain agent.
	appName := "chain-agent-demo"
	c.runner = runner.NewRunner(appName, chainAgent)

	// Setup identifiers.
	c.userID = "user"
	c.sessionID = fmt.Sprintf("chain-session-%d", time.Now().Unix())

	fmt.Printf("✅ Chain ready! Session: %s\n", c.sessionID)
	fmt.Printf("📝 Agents: %s → %s → %s\n\n",
		planningAgent.Info().Name,
		researchAgent.Info().Name,
		writingAgent.Info().Name)
	return c, nil
}

// startChat runs the interactive conversation loop.
func (c *chainChat) startChat(ctx context.Context, userInput string) (string, error) {
	//
	//result := fmt.Sprint("👤 You: \n")

	if userInput == "" {
		return "please say something", nil
	}

	// Handle exit command.
	if strings.ToLower(userInput) == "exit" {
		return fmt.Sprint("👋 Goodbye!"), nil
	}

	// Process the user message.
	return c.processMessage(ctx, userInput)
}

// processMessage handles a single message exchange through the agent chain.
func (c *chainChat) processMessage(ctx context.Context, userMessage string) (string, error) {
	message := model.NewUserMessage(userMessage)

	// Run the chain agent through the runner.
	eventChan, err := c.runner.Run(ctx, c.userID, c.sessionID, message)
	if err != nil {
		return "", fmt.Errorf("failed to run chain agent: %w", err)
	}

	// Process streaming response.
	return c.processStreamingResponse(eventChan)
}

// processStreamingResponse handles the streaming response from the agent chain.
func (c *chainChat) processStreamingResponse(eventChan <-chan *event.Event) (string, error) {
	var result string
	var (
		currentAgent    string
		agentStarted    bool
		toolCallsActive bool
	)

	for event := range eventChan {
		// Handle errors.
		if event.Error != nil {
			result += fmt.Sprintf("\n❌ Error: %s\n", event.Error.Message)
			continue
		}

		// Track which agent is currently active.
		if event.Author != currentAgent {
			if agentStarted {
				result += fmt.Sprintf("\n")
			}
			currentAgent = event.Author
			agentStarted = true
			toolCallsActive = false

			// Display agent transition.
			switch currentAgent {
			case "planning-agent":
				result += fmt.Sprintf("📋 Planning Agent: ")
			case "research-agent":
				result += fmt.Sprintf("🔍 Research Agent: ")
			case "writing-agent":
				result += fmt.Sprintf("✍️  Writing Agent: ")
			default:
				result += fmt.Sprintf("🤖 %s: ", currentAgent)
			}
		}

		// Detect and display tool calls.
		if len(event.Choices) > 0 && len(event.Choices[0].Message.ToolCalls) > 0 {
			if !toolCallsActive {
				toolCallsActive = true
				result += fmt.Sprintf("\n🔧 Using tools:\n")
				for _, toolCall := range event.Choices[0].Message.ToolCalls {
					result += fmt.Sprintf("   • %s (ID: %s)\n", toolCall.Function.Name, toolCall.ID)
					if len(toolCall.Function.Arguments) > 0 {
						result += fmt.Sprintf("     Args: %s\n", string(toolCall.Function.Arguments))
					}
				}
				result += fmt.Sprintf("🔄 Executing...\n")
			}
		}

		// Detect tool responses.
		if event.Response != nil && len(event.Response.Choices) > 0 {
			for _, choice := range event.Response.Choices {
				if choice.Message.Role == model.RoleTool && choice.Message.ToolID != "" {
					result += fmt.Sprintf("✅ Tool result (ID: %s): %s\n",
						choice.Message.ToolID,
						strings.TrimSpace(choice.Message.Content))
				}
			}
		}

		// Process streaming content.
		if len(event.Choices) > 0 {
			choice := event.Choices[0]
			if choice.Delta.Content != "" {
				if toolCallsActive {
					toolCallsActive = false
					result += fmt.Sprintf("\n%s (continued): ", c.getAgentEmoji(currentAgent))
				}
				result += choice.Delta.Content
			}
		}

		// Check if this is the final runner completion event.
		if event.Done && event.Response != nil && event.Response.Object == model.ObjectTypeRunnerCompletion {
			result += fmt.Sprintf("\n")
			break
		}
	}

	return result, nil
}

// getAgentEmoji returns the appropriate emoji for the agent.
func (c *chainChat) getAgentEmoji(agentName string) string {
	switch agentName {
	case "planning-agent":
		return "📋 Planning Agent"
	case "research-agent":
		return "🔍 Research Agent"
	case "writing-agent":
		return "✍️  Writing Agent"
	default:
		return "🤖 " + agentName
	}
}

// Tool implementations.

// webSearch simulates a web search tool.
func (c *chainChat) webSearch(_ context.Context, args webSearchArgs) (webSearchResult, error) {
	// Simulate web search with relevant information.
	results := []string{
		fmt.Sprintf("Recent information about '%s' from reliable sources", args.Query),
		"Current trends and developments in the field",
		"Expert opinions and analysis from industry leaders",
	}

	return webSearchResult{
		Query:   args.Query,
		Results: results,
		Count:   len(results),
	}, nil
}

// queryKnowledge simulates a knowledge base query.
func (c *chainChat) queryKnowledge(_ context.Context, args knowledgeArgs) (knowledgeResult, error) {
	// Simulate knowledge base query.
	facts := []string{
		fmt.Sprintf("Factual information about '%s'", args.Topic),
		"Historical context and background",
		"Technical specifications and details",
	}

	return knowledgeResult{
		Topic: args.Topic,
		Facts: facts,
		Count: len(facts),
	}, nil
}

// Tool argument and result types.

type webSearchArgs struct {
	Query string `json:"query" description:"Search query for web search"`
}

type webSearchResult struct {
	Query   string   `json:"query"`
	Results []string `json:"results"`
	Count   int      `json:"count"`
}

type knowledgeArgs struct {
	Topic string `json:"topic" description:"Topic to query in knowledge base"`
}

type knowledgeResult struct {
	Topic string   `json:"topic"`
	Facts []string `json:"facts"`
	Count int      `json:"count"`
}

// Helper functions.

func intPtr(i int) *int {
	return &i
}

func floatPtr(f float64) *float64 {
	return &f
}
