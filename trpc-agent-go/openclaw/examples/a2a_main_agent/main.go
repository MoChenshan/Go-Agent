// Package main runs a primary agent that delegates to a remote
// OpenClaw A2A sub-agent.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/a2aagent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

const (
	defaultA2AURL = "http://127.0.0.1:18080/a2a"
	defaultModel  = "gpt-5.2"

	defaultQuestion = "What's the weather in Shanghai today?"
	defaultFollowUp = "What about tomorrow?"

	demoAppName    = "openclaw-a2a-main-agent"
	demoUserID     = "demo-user"
	demoSessionID  = "demo-session"
	primaryAgentID = "primary-idc-agent"

	transferToolName = "transfer_to_agent"
	requestTimeout   = 180 * time.Second
)

const primaryDescription = "" +
	"Primary IDC agent that delegates sandbox-dependent work to " +
	"OpenClaw over A2A."

const primaryInstruction = "" +
	"You are the primary IDC agent. " +
	"For live weather, forecast, file-processing, or sandbox-dependent " +
	"tasks, you must delegate to the OpenClaw sub-agent with " +
	"transfer_to_agent instead of answering from memory. " +
	"After the sub-agent returns, provide a concise final answer. " +
	"Never repeat raw JSON, tool envelopes, or fields such as stdout, " +
	"stderr, exit_code, or duration_ms. " +
	"Extract the useful facts and answer in plain language only. " +
	"Do not invent current conditions."

var (
	a2aURLFlag = flag.String(
		"a2a-url",
		defaultA2AURL,
		"Remote OpenClaw A2A base URL",
	)
	modelFlag = flag.String(
		"model",
		envOrDefault("MODEL_NAME", defaultModel),
		"OpenAI-compatible model name",
	)
	baseURLFlag = flag.String(
		"base-url",
		os.Getenv("OPENAI_BASE_URL"),
		"OpenAI-compatible base URL",
	)
	apiKeyFlag = flag.String(
		"api-key",
		os.Getenv("OPENAI_API_KEY"),
		"OpenAI-compatible API key",
	)
	questionFlag = flag.String(
		"question",
		defaultQuestion,
		"First user question",
	)
	followUpFlag = flag.String(
		"follow-up",
		defaultFollowUp,
		"Optional follow-up question in the same session",
	)
	streamingFlag = flag.Bool(
		"streaming",
		true,
		"Enable streaming when the primary agent calls the sub-agent",
	)
)

func main() {
	log.SetFlags(0)
	flag.Parse()

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	a2aURL := strings.TrimSpace(*a2aURLFlag)
	if a2aURL == "" {
		return errors.New("a2a-url is required")
	}

	subAgent, err := a2aagent.New(
		a2aagent.WithAgentCardURL(a2aURL),
		a2aagent.WithEnableStreaming(*streamingFlag),
	)
	if err != nil {
		return fmt.Errorf("create a2a sub-agent failed: %w", err)
	}

	card := subAgent.GetAgentCard()
	if card == nil {
		return errors.New("resolved a2a agent card is nil")
	}

	primaryAgent := buildPrimaryAgent(subAgent)
	sessionSvc := inmemory.NewSessionService()
	procRunner := runner.NewRunner(
		demoAppName,
		primaryAgent,
		runner.WithSessionService(sessionSvc),
	)
	defer procRunner.Close()

	fmt.Printf("Remote A2A URL: %s\n", a2aURL)
	fmt.Printf("Remote Agent: %s\n", card.Name)
	fmt.Printf("Remote Skills: %d\n\n", len(card.Skills))

	for idx, prompt := range prompts() {
		result, err := ask(procRunner, prompt)
		if err != nil {
			return err
		}

		fmt.Printf("Q%d: %s\n", idx+1, prompt)
		for _, trace := range result.ToolTrace {
			fmt.Printf("Trace: %s\n", trace)
		}
		fmt.Printf("A%d: %s\n\n", idx+1, result.Answer)
	}
	return nil
}

func buildPrimaryAgent(subAgent agent.Agent) agent.Agent {
	modelInstance := openai.New(*modelFlag, buildModelOptions()...)
	genConfig := model.GenerationConfig{
		MaxTokens:   intPtr(2000),
		Temperature: floatPtr(0.2),
		Stream:      true,
	}

	return llmagent.New(
		primaryAgentID,
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription(primaryDescription),
		llmagent.WithInstruction(primaryInstruction),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithSubAgents([]agent.Agent{subAgent}),
		llmagent.WithEndInvocationAfterTransfer(false),
	)
}

func buildModelOptions() []openai.Option {
	options := make([]openai.Option, 0, 2)
	if baseURL := strings.TrimSpace(*baseURLFlag); baseURL != "" {
		options = append(options, openai.WithBaseURL(baseURL))
	}
	if apiKey := strings.TrimSpace(*apiKeyFlag); apiKey != "" {
		options = append(options, openai.WithAPIKey(apiKey))
	}
	return options
}

func prompts() []string {
	out := []string{strings.TrimSpace(*questionFlag)}
	if followUp := strings.TrimSpace(*followUpFlag); followUp != "" {
		out = append(out, followUp)
	}
	return out
}

type runResult struct {
	Answer    string
	ToolTrace []string
}

func ask(procRunner runner.Runner, prompt string) (runResult, error) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		requestTimeout,
	)
	defer cancel()

	events, err := procRunner.Run(
		ctx,
		demoUserID,
		demoSessionID,
		model.NewUserMessage(prompt),
	)
	if err != nil {
		return runResult{}, fmt.Errorf("run primary agent failed: %w", err)
	}

	result := runResult{}
	seenToolCalls := make(map[string]struct{})
	sawDelta := false

	for evt := range events {
		if evt.Error != nil {
			return runResult{}, errors.New(evt.Error.Message)
		}
		if evt.Response == nil {
			continue
		}
		for _, choice := range evt.Response.Choices {
			result.ToolTrace = appendToolCalls(
				result.ToolTrace,
				evt.Author,
				choice.Message.ToolCalls,
				seenToolCalls,
			)
			result.ToolTrace = appendToolCalls(
				result.ToolTrace,
				evt.Author,
				choice.Delta.ToolCalls,
				seenToolCalls,
			)
			if choice.Delta.Content != "" {
				sawDelta = true
				result.Answer += choice.Delta.Content
				continue
			}
			if !sawDelta && choice.Message.Content != "" {
				result.Answer = choice.Message.Content
			}
		}
	}

	result.Answer = strings.TrimSpace(result.Answer)
	result.Answer = trimLeadingJSONEnvelope(result.Answer)
	if result.Answer == "" {
		return runResult{}, errors.New("primary agent returned empty answer")
	}
	return result, nil
}

func appendToolCalls(
	trace []string,
	author string,
	calls []model.ToolCall,
	seen map[string]struct{},
) []string {
	for _, call := range calls {
		key := toolCallKey(author, call)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		trace = append(trace, formatToolCall(author, call))
	}
	return trace
}

func toolCallKey(author string, call model.ToolCall) string {
	return strings.Join(
		[]string{
			author,
			call.ID,
			call.Function.Name,
			string(call.Function.Arguments),
		},
		"|",
	)
}

func formatToolCall(author string, call model.ToolCall) string {
	prefix := fmt.Sprintf("%s -> %s", author, call.Function.Name)
	if call.Function.Name != transferToolName ||
		len(call.Function.Arguments) == 0 {
		return prefix
	}
	return fmt.Sprintf("%s %s", prefix, string(call.Function.Arguments))
}

func trimLeadingJSONEnvelope(answer string) string {
	answer = strings.TrimSpace(answer)
	if answer == "" || answer[0] != '{' {
		return answer
	}

	end := findLeadingJSONObjectEnd(answer)
	if end < 0 {
		return answer
	}
	rest := strings.TrimSpace(answer[end+1:])
	if rest == "" {
		return answer
	}
	return rest
}

func findLeadingJSONObjectEnd(answer string) int {
	depth := 0
	inString := false
	escaped := false

	for idx := 0; idx < len(answer); idx++ {
		ch := answer[idx]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return idx
			}
		}
	}
	return -1
}

func envOrDefault(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value != "" {
		return value
	}
	return fallback
}

func intPtr(v int) *int {
	return &v
}

func floatPtr(v float64) *float64 {
	return &v
}
