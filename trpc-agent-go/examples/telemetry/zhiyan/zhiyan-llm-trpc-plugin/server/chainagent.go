package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	zhiyanllm "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/zhiyan-llm"
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
	maxTokens   = 500
	temperature = 0.7
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

	genConfig := model.GenerationConfig{
		MaxTokens:   intPtr(maxTokens),
		Temperature: floatPtr(temperature),
		Stream:      true,
	}

	planningAgent := llmagent.New(
		"planning-agent",
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("Analyzes user requests and creates structured plans"),
		llmagent.WithInstruction("Create a brief plan (2-3 steps). Be concise."),
		llmagent.WithGenerationConfig(genConfig),
	)

	researchAgent := llmagent.New(
		"research-agent",
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("Gathers information using available tools and resources"),
		llmagent.WithInstruction("Use tools to gather key info. Be concise and fact-based."),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithTools([]tool.Tool{webSearchTool, knowledgeTool}),
	)

	writingAgent := llmagent.New(
		"writing-agent",
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("Composes final responses based on planning and research"),
		llmagent.WithInstruction("Write a brief, well-structured answer. Keep it concise."),
		llmagent.WithGenerationConfig(genConfig),
	)

	chainAgent := chainagent.New(
		"multi-agent-chain",
		chainagent.WithSubAgents([]agent.Agent{planningAgent, researchAgent, writingAgent}),
	)

	appName := "chain-agent-demo"
	c.runner = runner.NewRunner(appName, chainAgent)

	c.userID = "user"
	c.sessionID = fmt.Sprintf("chain-session-%d", time.Now().Unix())
	return c, nil
}

func (c *chainChat) startChat(ctx context.Context, userInput string) (string, error) {
	if userInput == "" {
		return "please say something", nil
	}
	if strings.ToLower(userInput) == "exit" {
		return "goodbye", nil
	}
	return c.processMessage(ctx, userInput)
}

func (c *chainChat) processMessage(ctx context.Context, userMessage string) (string, error) {
	message := model.NewUserMessage(userMessage)
	// Example: attach Zhiyan's business_scenario for this request.
	ctx = zhiyanllm.WithBusinessScenario(ctx, "customer_service")
	eventChan, err := c.runner.Run(ctx, c.userID, c.sessionID, message)
	if err != nil {
		return "", fmt.Errorf("failed to run chain agent: %w", err)
	}
	return c.processStreamingResponse(eventChan)
}

func (c *chainChat) processStreamingResponse(eventChan <-chan *event.Event) (string, error) {
	var result string
	for event := range eventChan {
		if event.Error != nil {
			result += fmt.Sprintf("\nError: %s\n", event.Error.Message)
			continue
		}
		if len(event.Choices) > 0 {
			choice := event.Choices[0]
			if choice.Delta.Content != "" {
				result += choice.Delta.Content
			}
		}
		if event.Done && event.Response != nil && event.Response.Object == model.ObjectTypeRunnerCompletion {
			break
		}
	}
	return result, nil
}

// Tool implementations.
func (c *chainChat) webSearch(_ context.Context, args webSearchArgs) (webSearchResult, error) {
	results := []string{
		fmt.Sprintf("Recent information about '%s' from reliable sources", args.Query),
		"Current trends and developments in the field",
	}
	return webSearchResult{Query: args.Query, Results: results, Count: len(results)}, nil
}

func (c *chainChat) queryKnowledge(_ context.Context, args knowledgeArgs) (knowledgeResult, error) {
	facts := []string{
		fmt.Sprintf("Factual information about '%s'", args.Topic),
		"Historical context and background",
	}
	return knowledgeResult{Topic: args.Topic, Facts: facts, Count: len(facts)}, nil
}

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

func intPtr(i int) *int           { return &i }
func floatPtr(f float64) *float64 { return &f }
