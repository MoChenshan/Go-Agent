package taiji

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/taiji/sdk"
	ihttp "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/http"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/internal/taiji"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/internal/telemetry"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	atrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

const (
	defaultChannelBufSize = 256
	defaultMaxEventSize   = 128 * 1024
)

// RunOptionsKey is the key for the run options
const RunOptionsKey = "Taiji-Agent-Run-Options"

// RunOptions represents the options for runner
type RunOptions struct {
	TaijiContext map[string]any
}

// Agent is the struct of taiji agent.
type Agent struct {
	taijiOpts      *sdk.TaijiOption
	name           string
	desc           string
	channelBufSize int
	streaming      bool
	maxEventSize   int

	client *taiji.Client
}

// New creates a new Taiji agent with the provided options.
func New(opts ...Option) (agent.Agent, error) {
	agent := Agent{
		channelBufSize: defaultChannelBufSize,
		maxEventSize:   defaultMaxEventSize,
	}
	for _, opt := range opts {
		opt(&agent)
	}

	if err := agent.validate(); err != nil {
		return nil, err
	}

	var httpClient ihttp.HTTPClient
	if agent.taijiOpts.ClientBuilder != nil {
		httpClient = agent.taijiOpts.ClientBuilder(
			sdk.WithHTTPClientName(agent.taijiOpts.ServiceName),
			sdk.WithHTTPTRPCClientOptions(agent.taijiOpts.TRPCClientOptions...),
		)
	}
	internalTaijiOption := taiji.TaijiOption{
		Token:       agent.taijiOpts.Token,
		ServiceName: agent.taijiOpts.ServiceName,
		URL:         agent.taijiOpts.URL,
		AgentOption: taiji.AgentOption{
			ApplicationID: agent.taijiOpts.ApplicationID,
		},
	}
	agent.client = taiji.NewClient(
		taiji.WithTaijiOption(internalTaijiOption),
		taiji.WithHTTPClient(httpClient),
		taiji.WithMaxEventSize(agent.maxEventSize))
	return &agent, nil
}

// validate validates the agent configuration
func (a *Agent) validate() error {
	if a.taijiOpts == nil {
		return fmt.Errorf("taiji options is required")
	}
	if a.taijiOpts.Token == "" {
		return fmt.Errorf("token is required")
	}
	if a.taijiOpts.URL == "" && a.taijiOpts.ServiceName == "" && a.taijiOpts.TRPCClientOptions == nil {
		return fmt.Errorf("URL / ServiceName / TRPCClientOptions is required")
	}
	if a.channelBufSize <= 0 {
		return fmt.Errorf("channel buffer size must be positive, got %d", a.channelBufSize)
	}
	if a.taijiOpts.ApplicationID == "" {
		return fmt.Errorf("application ID is required")
	}
	return nil
}

// Run executes the provided invocation within the given context and returns
// a channel of events that represent the progress and results of the execution.
func (a *Agent) Run(ctx context.Context, invocation *agent.Invocation) (<-chan *event.Event, error) {
	// Create trace span for Taiji agent execution
	ctx, span := atrace.Tracer.Start(ctx, fmt.Sprintf("%s %s", telemetry.OperationChat, a.name))

	req, headers, err := a.buildChatRequest(invocation)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return nil, fmt.Errorf("failed to build chat request: %w", err)
	}

	// Set initial trace attributes
	a.setTraceAttributes(span, invocation, req)

	respChan, err := a.client.AppCreate(ctx, req, headers, a.channelBufSize)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return nil, fmt.Errorf("failed to generate content: %w", err)
	}

	// Convert client response channel to event channel
	eventChan := make(chan *event.Event, a.channelBufSize)
	go func() {
		defer close(eventChan)
		defer span.End() // End span after goroutine completes

		var lastRespErr *model.ResponseError
		var finalResult strings.Builder
		var finalReasoningContent strings.Builder
		var responseID string
		// Use a single message ID for all streaming events
		messageID := uuid.NewString()

		// Initialize timing tracker
		tracker := newTimingTracker(invocation.GetOrCreateTimingInfo())

		for {
			select {
			case resp, ok := <-respChan:
				if !ok {
					// Channel closed, build final response
					finalResponse := a.buildFinalResponse(
						messageID,
						finalResult.String(),
						finalReasoningContent.String(),
						lastRespErr,
					)

					// Attach timing info to final response
					if tracker.timingInfo != nil && finalResponse != nil {
						if finalResponse.Usage == nil {
							finalResponse.Usage = &model.Usage{}
						}
						finalResponse.Usage.TimingInfo = tracker.timingInfo
					}

					// Send final event if streaming
					if a.streaming {
						a.emitFinalEvent(ctx, invocation, eventChan, finalResponse)
					}

					// Set final trace attributes
					a.setFinalTraceAttributes(span, responseID, finalResponse, lastRespErr)
					return
				}

				// Track timing information
				hasContent := resp.Result != "" || resp.ReasoningContent != ""
				tracker.trackFirstToken(hasContent)

				if a.streaming {
					tracker.trackReasoningDuration(resp.ReasoningContent != "", resp.Result != "")
				}

				// Convert client response to event
				evt := a.convertResponseToEvent(resp, invocation.InvocationID, messageID)
				if evt == nil {
					log.Debugf("skipping nil event for invocation %s", invocation.InvocationID)
					continue
				}

				// Attach timing info to each response event
				if tracker.timingInfo != nil && evt.Response != nil {
					if evt.Response.Usage == nil {
						evt.Response.Usage = &model.Usage{}
					}
					evt.Response.Usage.TimingInfo = tracker.timingInfo
				}

				if a.streaming {
					finalResult.WriteString(resp.Result)
					finalReasoningContent.WriteString(resp.ReasoningContent)
				} else {
					// For non-streaming, use the response content directly
					finalResult.WriteString(resp.Result)
					finalReasoningContent.WriteString(resp.ReasoningContent)
				}
				if evt.Response.Error != nil {
					lastRespErr = evt.Response.Error
				}
				if evt.Response.ID != "" {
					responseID = evt.Response.ID
				}
				if err := agent.EmitEvent(ctx, invocation, eventChan, evt); err != nil {
					log.Infof("failed to emit event for invocation %s: %v", invocation.InvocationID, err)
					span.RecordError(err)
					return
				}
			case <-ctx.Done():
				log.Infof("context cancelled for invocation %s", invocation.InvocationID)
				span.SetStatus(codes.Error, "context cancelled")
				return
			}
		}
	}()

	return eventChan, nil
}

// timingTracker tracks timing information during response streaming
type timingTracker struct {
	timingInfo         *model.TimingInfo
	requestStartTime   time.Time
	firstTokenReceived bool
	reasoningStartTime time.Time
	inReasoningPhase   bool
}

// newTimingTracker creates a new timing tracker
func newTimingTracker(timingInfo *model.TimingInfo) *timingTracker {
	tracker := &timingTracker{
		timingInfo: timingInfo,
	}
	if timingInfo != nil {
		tracker.requestStartTime = time.Now()
	}
	return tracker
}

// trackFirstToken records the time to first token
func (t *timingTracker) trackFirstToken(hasContent bool) {
	if t.timingInfo == nil || t.firstTokenReceived || !hasContent {
		return
	}
	t.timingInfo.FirstTokenDuration = time.Since(t.requestStartTime)
	t.firstTokenReceived = true
}

// trackReasoningDuration tracks the reasoning phase duration (streaming mode only)
func (t *timingTracker) trackReasoningDuration(hasReasoning, hasContent bool) {
	if t.timingInfo == nil {
		return
	}

	if hasReasoning && !t.inReasoningPhase {
		// Start of reasoning phase
		t.inReasoningPhase = true
		t.reasoningStartTime = time.Now()
	} else if !hasReasoning && hasContent && t.inReasoningPhase {
		// Transition from reasoning to normal content
		t.inReasoningPhase = false
		t.timingInfo.ReasoningDuration = time.Since(t.reasoningStartTime)
	}
}

// buildFinalResponse builds the final response structure
func (a *Agent) buildFinalResponse(
	messageID string,
	content string,
	reasoningContent string,
	lastErr *model.ResponseError,
) *model.Response {
	now := time.Now()
	response := &model.Response{
		ID:        messageID,
		Done:      true,
		IsPartial: false,
		Choices: []model.Choice{
			{
				Index: 0,
				Message: model.Message{
					Role:             model.RoleAssistant,
					Content:          content,
					ReasoningContent: reasoningContent,
				},
			},
		},
		Object:    model.ObjectTypeChatCompletion,
		Timestamp: now,
		Created:   now.Unix(),
	}

	if lastErr != nil {
		response.Error = lastErr
	}

	return response
}

// emitFinalEvent emits the final event for streaming mode
func (a *Agent) emitFinalEvent(
	ctx context.Context,
	invocation *agent.Invocation,
	eventChan chan<- *event.Event,
	response *model.Response,
) {
	finalEvt := &event.Event{
		Response:     response,
		InvocationID: invocation.InvocationID,
		Author:       a.name,
		ID:           uuid.NewString(),
		Timestamp:    time.Now(),
	}

	if response.Error != nil {
		// Event embeds model.Response, so evt.Error is same as evt.Response.Error
		// We don't need to manually set it if it's already in the response,
		// but since we created the Event with the response, it's already there.
	}
	agent.EmitEvent(ctx, invocation, eventChan, finalEvt)
}

// convertResponseToEvent converts client response to event
func (a *Agent) convertResponseToEvent(resp *taiji.ChatResp, invocationID string, messageID string) *event.Event {
	if resp == nil {
		return nil
	}

	now := time.Now()
	evt := &event.Event{
		Response: &model.Response{
			ID:        messageID,
			Timestamp: now,
			Created:   now.Unix(),
		},
		InvocationID: invocationID,
		Author:       a.name,
		ID:           uuid.NewString(),
		Timestamp:    now,
	}

	if a.streaming {
		evt.Response.Choices = []model.Choice{
			{
				Index: 0,
				Delta: model.Message{
					Role:             model.RoleAssistant,
					Content:          resp.Result,
					ReasoningContent: resp.ReasoningContent,
				},
			},
		}
		evt.Response.Done = false
		evt.Response.IsPartial = true
		evt.Response.Object = model.ObjectTypeChatCompletionChunk
	} else {
		evt.Response.Choices = []model.Choice{
			{
				Index: 0,
				Message: model.Message{
					Role:             model.RoleAssistant,
					Content:          resp.Result,
					ReasoningContent: resp.ReasoningContent,
				},
			},
		}
		evt.Response.Done = true
		evt.Response.IsPartial = false
		evt.Response.Object = model.ObjectTypeChatCompletion
	}

	// Handle error case, taiji may return Error or ErrorMsg (when framework error)
	if resp.Error != nil {
		evt.Response.Error = &model.ResponseError{
			Type:    resp.Error.Type,
			Message: resp.Error.Message,
		}
	}

	if resp.ErrorMsg != "" || resp.RetCode != 0 {
		errMsg := resp.ErrorMsg
		if errMsg == "" {
			errMsg = resp.Message
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("taiji error with retcode: %d", resp.RetCode)
		}
		if evt.Response.Error == nil {
			evt.Response.Error = &model.ResponseError{
				Type:    "UnknownError",
				Message: errMsg,
			}
		}
	}

	return evt
}

func (a *Agent) buildChatRequest(invocation *agent.Invocation) (*taiji.ChatRequest, *taiji.ChatHeaders, error) {
	req := &taiji.ChatRequest{
		QueryID: invocation.InvocationID,
		Stream:  a.streaming,
	}

	userMsg := a.convertToTaijiMessage(invocation.Message)
	req.Query = userMsg.Content
	if userMsg.Agent != nil {
		req.MultiMedias = userMsg.Agent.MultiMedias
	}

	// Build message history from session events
	messages := a.buildMessageHistory(invocation)
	if len(messages) > 0 {
		req.Messages = make([]taiji.Message, len(messages))
		for i, msg := range messages {
			req.Messages[i] = *msg
		}
	}

	runOptions := invocation.GetCustomAgentConfig(RunOptionsKey)
	if runOptions == nil {
		return req, &taiji.ChatHeaders{Staffname: invocation.Session.UserID}, nil
	}

	opts, ok := runOptions.(*RunOptions)
	if !ok {
		log.Infof("run options is not a RunOptions type")
		return req, &taiji.ChatHeaders{Staffname: invocation.Session.UserID}, nil
	}

	if opts.TaijiContext != nil {
		contextBytes, err := json.Marshal(opts.TaijiContext)
		if err != nil {
			return nil, nil, err
		}
		req.Context = string(contextBytes)
	}

	return req, &taiji.ChatHeaders{Staffname: invocation.Session.UserID}, nil
}

// buildMessageHistory builds the message history from session events and current invocation
func (a *Agent) buildMessageHistory(invocation *agent.Invocation) []*taiji.Message {
	var messages []*taiji.Message

	// Process session events first
	for i := range invocation.Session.Events {
		msg := a.convertToTaijiMessageHistory(&invocation.Session.Events[i])
		if msg != nil {
			messages = append(messages, msg)
		}
	}
	return messages
}

// convertToTaijiMessageHistory converts event to taiji message with enhanced type handling
func (a *Agent) convertToTaijiMessageHistory(evt *event.Event) *taiji.Message {
	// Check for nil response first
	if evt.Response == nil && len(evt.Choices) == 0 {
		return nil
	}

	// should not happen
	if a.isToolEvent(evt) {
		return nil
	}

	// Extract message from event response
	if evt.Response != nil && len(evt.Response.Choices) > 0 {
		choice := evt.Response.Choices[0]
		content := choice.Message.Content
		if a.isOtherAgentReply(evt) {
			content = fmt.Sprintf("other agent %s said: %s", evt.Author, content)
		}
		msg := &taiji.Message{
			Role:    string(choice.Message.Role),
			Content: content,
		}
		// Handle multimedia content
		if len(choice.Message.ContentParts) > 0 {
			msg.Agent = a.convertToTaijiMultiMedias(choice.Message.ContentParts)
		}
		return msg
	}
	return nil
}

// isToolEvent checks if an event represents a tool call or response
func (a *Agent) isToolEvent(evt *event.Event) bool {
	// Check response choices for tool-related content
	if evt.Response != nil && len(evt.Response.Choices) > 0 {
		choice := evt.Response.Choices[0]
		return len(choice.Message.ToolCalls) > 0 ||
			choice.Message.ToolID != "" ||
			choice.Message.Role == model.RoleTool
	}
	return false
}

// convertToTaijiMultiMedias converts model content parts to client agent structure
func (a *Agent) convertToTaijiMultiMedias(contentParts []model.ContentPart) *taiji.MessageAgent {
	if len(contentParts) == 0 {
		return nil
	}

	agent := &taiji.MessageAgent{
		MultiMedias: make([]taiji.Multimedia, 0, len(contentParts)),
	}

	for _, part := range contentParts {
		if part.Type == model.ContentTypeFile && part.File != nil {
			urlStr := string(part.File.Data)

			// Validate URL format and scheme
			if !a.validateURL(urlStr) {
				log.Infof("taiji agent only accept url in file content, invalid URL in file %s (ID: %s): %s",
					part.File.Name, part.File.FileID, truncateURLForLog(urlStr))
				continue
			}

			agent.MultiMedias = append(agent.MultiMedias, taiji.Multimedia{
				Type:     part.File.MimeType,
				URL:      urlStr,
				MediaID:  part.File.FileID,
				FileName: part.File.Name,
			})
		}
	}

	if len(agent.MultiMedias) == 0 {
		return nil
	}

	return agent
}

// isOtherAgentReply checks whether the event is a reply from another agent.
func (a *Agent) isOtherAgentReply(evt *event.Event) bool {
	return a.name != "" &&
		evt.Author != a.name &&
		evt.Author != "user" &&
		evt.Author != ""
}

func (a *Agent) convertToTaijiMessage(message model.Message) *taiji.Message {
	msg := taiji.Message{
		Role:    string(message.Role),
		Content: message.Content,
	}
	msg.Agent = a.convertToTaijiMultiMedias(message.ContentParts)
	return &msg
}

// Tools returns the list of tools that this agent has access to and can execute.
// These tools represent the capabilities available to the agent during invocations.
func (a *Agent) Tools() []tool.Tool {
	return []tool.Tool{}
}

// setTraceAttributes sets initial trace attributes for the agent execution
func (a *Agent) setTraceAttributes(span trace.Span, invocation *agent.Invocation, req *taiji.ChatRequest) {
	attrs := []attribute.KeyValue{
		attribute.String(telemetry.KeyGenAISystem, "taiji"),
		attribute.String(telemetry.KeyGenAIOperationName, telemetry.OperationChat),
	}

	// Add Taiji-specific attributes
	if a.taijiOpts != nil {
		attrs = append(attrs, attribute.String(telemetry.KeyTaijiApplicationID, a.taijiOpts.ApplicationID))
		attrs = append(attrs, attribute.String(telemetry.KeyTaijiURL, a.taijiOpts.URL))
	}

	// Add invocation attributes
	attrs = append(attrs, telemetry.BuildInvocationAttributes(invocation)...)

	// Add request attributes
	attrs = append(attrs, a.buildRequestAttributes(invocation, req)...)

	span.SetAttributes(attrs...)
}

// buildRequestAttributes builds request-related attributes
func (a *Agent) buildRequestAttributes(invocation *agent.Invocation, req *taiji.ChatRequest) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.Int(telemetry.KeyGenAIRequestChoiceCount, 1),
	}

	// Add stream attribute only when it's true
	if a.streaming {
		attrs = append(attrs, attribute.Bool(telemetry.KeyGenAIRequestIsStream, true))
	}

	// Build input messages from invocation.Message
	// Format: array of messages with role and content
	if invocation != nil && invocation.Message.Content != "" {
		inputMessages := []map[string]any{
			{
				"role":    string(invocation.Message.Role),
				"content": invocation.Message.Content,
			},
		}
		if bts, err := json.Marshal(inputMessages); err == nil {
			attrs = append(attrs, attribute.String(telemetry.KeyGenAIInputMessages, string(bts)))
		}
	}

	// Add request body (taiji-specific)
	if req != nil {
		if bts, err := json.Marshal(req); err == nil {
			attrs = append(attrs, attribute.String(telemetry.KeyLLMRequest, string(bts)))
		} else {
			attrs = append(attrs, attribute.String(telemetry.KeyLLMRequest, "<not json serializable>"))
		}
	}

	return attrs
}

// setFinalTraceAttributes sets final trace attributes after agent execution completes
func (a *Agent) setFinalTraceAttributes(
	span trace.Span, responseID string, response *model.Response, lastErr *model.ResponseError,
) {
	attrs := a.buildResponseAttributes(responseID, response)

	// Set error status if there was an error
	if lastErr != nil {
		span.SetStatus(codes.Error, lastErr.Message)
		attrs = append(attrs,
			attribute.String(telemetry.KeyErrorType, lastErr.Type),
			attribute.String(telemetry.KeyErrorMessage, lastErr.Message),
		)
	} else {
		span.SetStatus(codes.Ok, "success")
	}

	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
}

// buildResponseAttributes builds response-related attributes from final response
func (a *Agent) buildResponseAttributes(responseID string, response *model.Response) []attribute.KeyValue {
	var attrs []attribute.KeyValue

	// Add response ID if available
	if responseID != "" {
		attrs = append(attrs, attribute.String(telemetry.KeyGenAIResponseID, responseID))
	}

	// If no response, return early
	if response == nil {
		return attrs
	}

	// Add error type if present
	if e := response.Error; e != nil {
		attrs = append(attrs,
			attribute.String(telemetry.KeyErrorType, e.Type),
			attribute.String(telemetry.KeyErrorMessage, e.Message),
		)
	}

	// Add choices attributes (gen_ai.output.messages)
	if len(response.Choices) > 0 {
		if bts, err := json.Marshal(response.Choices); err == nil {
			attrs = append(attrs, attribute.String(telemetry.KeyGenAIOutputMessages, string(bts)))
		}
	}

	// Add response body (trpc.go.agent.llm_response)
	if bts, err := json.Marshal(response); err == nil {
		attrs = append(attrs, attribute.String(telemetry.KeyLLMResponse, string(bts)))
	}

	return attrs
}

// Info returns the basic information about this agent.
func (a *Agent) Info() agent.Info {
	return agent.Info{
		Name:        a.name,
		Description: a.desc,
	}
}

// SubAgents returns the list of sub-agents available to this agent.
// Returns empty slice if no sub-agents are available.
func (a *Agent) SubAgents() []agent.Agent {
	return []agent.Agent{}
}

// FindSubAgent finds a sub-agent by name.
// Returns nil if no sub-agent with the given name is found.
func (a *Agent) FindSubAgent(name string) agent.Agent {
	return nil
}

// validateURL validates if the URL string is a valid HTTP/HTTPS URL
func (a *Agent) validateURL(urlStr string) bool {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	return parsedURL.Scheme == "http" || parsedURL.Scheme == "https"
}

// truncateURLForLog truncates URL for logging to prevent excessive output
func truncateURLForLog(urlStr string) string {
	if len(urlStr) > 100 {
		return urlStr[:100] + "..."
	}
	return urlStr
}
