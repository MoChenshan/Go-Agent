package knotagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"git.code.oa.com/trpc-go/trpc-go/log"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var defaultChannelBufferSize = 256

type Option func(*Options)

// WithDescription sets the description of the agent.
func WithDescription(description string) Option {
	return func(opts *Options) {
		opts.Description = description
	}
}

// WithChannelBufferSize sets the buffer size for event channels.
func WithChannelBufferSize(size int) Option {
	return func(opts *Options) {
		opts.ChannelBufferSize = size
	}
}

// WithKnotApiKey sets the knot api key.
func WithKnotApiKey(token string) Option {
	return func(opts *Options) {
		opts.KnotApiKey = token
	}
}

// WithKnotModel sets the knot model.
func WithKnotModel(model string) Option {
	return func(opts *Options) {
		opts.KnotModel = model
	}
}

// WithKnotApiUrl sets the knot api url.
func WithKnotApiUrl(url string) Option {
	return func(opts *Options) {
		opts.KnotApiUrl = url
	}
}

// WithKnotApiUser sets the knot api user.
func WithKnotApiUser(user string) Option {
	return func(opts *Options) {
		opts.KnotApiUser = user
	}
}

// WithKnotEnableWebSearch sets the knot enable web search.
func WithKnotEnableWebSearch(enable bool) Option {
	return func(opts *Options) {
		opts.KnotEnableWebSearch = enable
	}
}

type Options struct {
	// Name is the name of the agent.
	Name string
	// Description is a description of the agent.
	Description string
	// ChannelBufferSize is the buffer size for event channels (default: 256).
	ChannelBufferSize int
	// KnotModel is the model to use for generating responses.
	KnotModel string
	// KnotApiUrl is the API URL for the Knot service.
	KnotApiUrl string
	// KnotApiKey is the API key for the Knot service.
	KnotApiKey string
	// KnotApiUser is the username for the Knot service.
	KnotApiUser string
	// KnotEnableWebSearch is the flag to enable web search.
	KnotEnableWebSearch bool
}

// KnotAgent is the struct of knot agent.
type KnotAgent struct {
	name                string
	description         string
	inputSchema         map[string]any
	outputSchema        map[string]any
	tools               []tool.Tool
	subAgents           []agent.Agent
	codeExecutor        codeexecutor.CodeExecutor
	channelBufferSize   int
	knotToken           string
	knotModel           string
	knotApiUrl          string
	knotApiUser         string
	knotEnableWebSearch bool
}

// New creates a new KnotAgent instance with the given name and options.
func New(name string, opts ...Option) *KnotAgent {
	var options = Options{
		ChannelBufferSize:   defaultChannelBufferSize,
		KnotEnableWebSearch: false,
	}
	// Apply function options.
	for _, opt := range opts {
		opt(&options)
	}
	// Return the agent instance.
	return &KnotAgent{
		name:                name,
		description:         options.Description,
		channelBufferSize:   options.ChannelBufferSize,
		knotModel:           options.KnotModel,
		knotApiUrl:          options.KnotApiUrl,
		knotApiUser:         options.KnotApiUser,
		knotToken:           options.KnotApiKey,
		knotEnableWebSearch: options.KnotEnableWebSearch,
	}
}

// knotChatResponse is the struct of knot chat response.
type knotChatResponse struct {
	Type      events.EventType `json:"type"`
	Timestamp int              `json:"timestamp"`
	RawEvent  struct {
		MessageId      string `json:"message_id"`
		ConversationId string `json:"conversation_id"`
		StepName       string `json:"step_name"`
		TokenUsage     struct {
			CompletionTokens        int `json:"completion_tokens"`
			PromptTokens            int `json:"prompt_tokens"`
			TotalTokens             int `json:"total_tokens"`
			CompletionTokensDetails struct {
				AcceptedPredictionTokens any `json:"accepted_prediction_tokens"`
				AudioTokens              any `json:"audio_tokens"`
				ReasoningTokens          int `json:"reasoning_tokens"`
				RejectedPredictionTokens any `json:"rejected_prediction_tokens"`
			} `json:"completion_tokens_details"`
			PromptTokensDetails any `json:"prompt_tokens_details"`
		} `json:"token_usage"`
	} `json:"rawEvent"`
	MessageId string `json:"messageId"`
	Role      string `json:"role"`
	Delta     string `json:"delta"`
	StepName  string `json:"stepName"`
	ThreadId  string `json:"threadId"`
	RunId     string `json:"runId"`
}

// buildKnotChatRequest builds the Knot chat request.
func (a *KnotAgent) buildKnotChatRequest(ctx context.Context, invocation *agent.Invocation) (*http.Request, error) {
	// Build request body.
	reqBody := map[string]interface{}{
		"input": map[string]interface{}{
			"message":           invocation.Message.Content,
			"conversation_id":   "", // Conversation state is not wired through yet.
			"model":             a.knotModel,
			"stream":            true,
			"enable_web_search": false,
		},
	}
	reqBodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize knot request: %w", err)
	}
	// Build request.
	req, err := http.NewRequestWithContext(ctx, "POST", a.knotApiUrl, bytes.NewBuffer(reqBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create knot request: %w", err)
	}
	// Set HTTP headers.
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-knot-api-token", a.knotToken)
	req.Header.Set("x-knot-api-user", a.knotApiUser)
	req.Header.Set("Accept", "text/event-stream")
	return req, nil
}

// formatEvent returns a TRPC event based on the knot chat response
func (a *KnotAgent) formatEvent(e *knotChatResponse, invocationID, author string) *event.Event {
	now := time.Now()
	// Base event structure.
	trpcEvt := &event.Event{
		InvocationID: invocationID,
		Author:       author,
		ID:           uuid.NewString(),
		Timestamp:    now,
		Response: &model.Response{
			Model:     a.knotModel,
			ID:        invocationID,
			Timestamp: now,
			Created:   now.Unix(),
			IsPartial: true,
			Done:      false,
			Choices: []model.Choice{{
				Index: 0,
				Delta: model.Message{
					Role: model.RoleAssistant,
				},
			}},
		},
	}
	switch e.Type {
	case events.EventTypeRunStarted:
		trpcEvt.Response.Object = model.ObjectTypeRunnerCompletion
		return trpcEvt
	case events.EventTypeRunFinished:
		trpcEvt.Response.Done = true
		trpcEvt.Response.IsPartial = false
		trpcEvt.Response.Object = model.ObjectTypeRunnerCompletion
		return trpcEvt
	case events.EventTypeThinkingStart:
		trpcEvt.Response.Object = model.ObjectTypePreprocessingPlanning
		return trpcEvt
	case events.EventTypeThinkingTextMessageStart:
		//trpcEvt.Response.Object = model.ObjectTypePreprocessingPlanning
		//trpcEvt.Response.Object = model.ObjectTypeChatCompletionChunk
		//trpcEvt.Response.Choices[0].Delta.Content = "\n<think>"
		return trpcEvt
	case events.EventTypeThinkingTextMessageContent:
		//trpcEvt.Response.Object = model.ObjectTypePreprocessingContent
		trpcEvt.Response.Object = model.ObjectTypeChatCompletionChunk
		trpcEvt.Response.Choices[0].Delta.Content = e.Delta
		return trpcEvt
	case events.EventTypeThinkingTextMessageEnd:
		//trpcEvt.Response.Object = model.ObjectTypePreprocessingPlanning
		//trpcEvt.Response.Object = model.ObjectTypeChatCompletionChunk
		//trpcEvt.Response.Choices[0].Delta.Content = "</think>"
		return trpcEvt
	case events.EventTypeThinkingEnd:
		trpcEvt.Response.Object = model.ObjectTypePostprocessingPlanning
		return trpcEvt
	case events.EventTypeTextMessageStart:
		trpcEvt.Response.Object = model.ObjectTypeChatCompletion
		return trpcEvt
	case events.EventTypeTextMessageContent:
		trpcEvt.Response.Object = model.ObjectTypeChatCompletionChunk
		if strings.TrimSpace(e.Delta) == "" {
			return nil
		}
		trpcEvt.Response.Choices[0].Delta.Content = e.Delta
		return trpcEvt
	case events.EventTypeTextMessageEnd:
		trpcEvt.Response.Object = model.ObjectTypeChatCompletion
		return trpcEvt
	case events.EventTypeToolCallStart:
		trpcEvt.Response.Object = model.ObjectTypeToolResponse
		return trpcEvt
	case events.EventTypeToolCallChunk:
		trpcEvt.Response.Object = model.ObjectTypeToolResponse
		return trpcEvt
	case events.EventTypeToolCallArgs:
		trpcEvt.Response.Object = model.ObjectTypeToolResponse
		trpcEvt.Response.Choices[0].Delta.Content = e.Delta
		return trpcEvt
	case events.EventTypeToolCallEnd:
		trpcEvt.Response.Object = model.ObjectTypeToolResponse
		return trpcEvt
	case events.EventTypeToolCallResult:
		trpcEvt.Response.Object = model.ObjectTypeToolResponse
		return trpcEvt
	case events.EventTypeStepStarted:
		trpcEvt.Response.Object = model.ObjectTypePreprocessingPlanning
		return trpcEvt
	case events.EventTypeStepFinished:
		trpcEvt.Response.Object = model.ObjectTypePreprocessingPlanning
		return trpcEvt
	default:
		//log.Infof("formatEvent default: %s", label)
		return trpcEvt
	}
}

// Run runs the agent and processes the invocation.
func (a *KnotAgent) Run(ctx context.Context, invocation *agent.Invocation) (<-chan *event.Event, error) {
	// Convert the upstream response stream into agent events.
	eventChan := make(chan *event.Event)
	req, err := a.buildKnotChatRequest(ctx, invocation)
	if err != nil {
		return nil, err
	}
	// Send request.
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send knot request: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		statusErr := fmt.Sprintf("knot API request failed: status=%d", resp.StatusCode)
		if body := strings.TrimSpace(string(bodyBytes)); body != "" {
			statusErr = fmt.Sprintf("%s, body=%s", statusErr, body)
		}
		go func() {
			defer close(eventChan)
			eventChan <- event.NewErrorEvent(invocation.InvocationID, a.name, model.ErrorTypeAPIError, statusErr)
		}()
		return eventChan, nil
	}

	go func() {
		defer resp.Body.Close()
		defer close(eventChan)
		contentType := resp.Header.Get("Content-Type")
		if contentType != "" && !strings.Contains(strings.ToLower(contentType), "text/event-stream") {
			eventChan <- event.NewErrorEvent(
				invocation.InvocationID,
				a.name,
				model.ErrorTypeAPIError,
				fmt.Sprintf("unexpected content type from knot API: %s", contentType),
			)
			return
		}

		var receivedEvent bool
		var terminalErrEmitted bool
		// Read the streaming response with an enlarged scanner buffer.
		scanner := bufio.NewScanner(resp.Body)
		buf := make([]byte, 0, 64*1024) // Initial 64KB.
		scanner.Buffer(buf, 1024*1024)  // Max 1MB.
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				// Parse Knot SSE payload.
				dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if dataStr == "[DONE]" {
					break
				}
				var tData *knotChatResponse
				if err := json.Unmarshal([]byte(dataStr), &tData); err != nil {
					log.Errorf("failed to parse Knot SSE payload: %v, data: %s", err, dataStr)
					eventChan <- event.NewErrorEvent(
						invocation.InvocationID,
						a.name,
						model.ErrorTypeStreamError,
						fmt.Sprintf("failed to parse knot stream payload: %v", err),
					)
					terminalErrEmitted = true
					return
				}
				receivedEvent = true
				// Convert upstream payload to an internal event.
				trpcEvt := a.formatEvent(tData, invocation.InvocationID, a.name)
				if trpcEvt != nil {
					eventChan <- trpcEvt
				}
			}
		}
		if err := scanner.Err(); err != nil {
			log.Errorf("failed to read knot response stream: %v", err)
			eventChan <- event.NewErrorEvent(
				invocation.InvocationID,
				a.name,
				model.ErrorTypeStreamError,
				fmt.Sprintf("failed to read knot response stream: %v", err),
			)
			return
		}
		if !receivedEvent && !terminalErrEmitted {
			eventChan <- event.NewErrorEvent(
				invocation.InvocationID,
				a.name,
				model.ErrorTypeStreamError,
				"knot response stream ended without any events",
			)
		}
	}()

	return eventChan, nil
}

// Tools return the tools of the agent
func (a *KnotAgent) Tools() []tool.Tool {
	return a.tools
}

// Info return the info of the agent
func (a *KnotAgent) Info() agent.Info {
	return agent.Info{
		Name:         a.name,
		Description:  a.description,
		InputSchema:  a.inputSchema,
		OutputSchema: a.outputSchema,
	}
}

// SubAgents return the sub agents of the agent
func (a *KnotAgent) SubAgents() []agent.Agent {
	return a.subAgents
}

// FindSubAgent return the sub agent by name
func (a *KnotAgent) FindSubAgent(name string) agent.Agent {
	for _, subAgent := range a.subAgents {
		if subAgent.Info().Name == name {
			return subAgent
		}
	}
	return nil
}

// CodeExecutor return the code executor of the agent
func (a *KnotAgent) CodeExecutor() codeexecutor.CodeExecutor {
	return a.codeExecutor
}

// KnotAgent implements the agent.Agent interface
var _ agent.Agent = (*KnotAgent)(nil)
var _ agent.CodeExecutor = (*KnotAgent)(nil)
