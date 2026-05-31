package vedas

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// SSEType is the type of sse.
type SSEType string

const (
	SSETypeCreatePlan          SSEType = "create_plan"          // Plan creation
	SSETypeUserPrompt          SSEType = "user_prompt"          // User prompt
	SSETypeIntention           SSEType = "intention"            // Intention recognition
	SSETypeInitAttachment      SSEType = "init_attachment"      // Attachment initialization
	SSETypeAttachment          SSEType = "attachment"           // Attachment processing
	SSETypeUserInteraction     SSEType = "user_interaction"     // User interaction
	SSETypeAgentResponse       SSEType = "agent_response"       // Agent response - requires attention
	SSETypeIntentionTransition SSEType = "intention_transition" // Intention transition
	SSETypeInitStage           SSEType = "init_stage"           // Stage initialization
	SSETypeStageUpdate         SSEType = "stage_update"         // Stage update
	SSETypeToolCall            SSEType = "tool_call"            // Tool call - requires attention
	SSETypeTerminate           SSEType = "terminate"            // Task termination
	SSETypeComplete            SSEType = "complete"             // Task completion
	SSETypeFailure             SSEType = "failure"              // Task failure
)

// SSEPlanResponse represents a parsed SSE plan response.
type SSEPlanResponse struct {
	Type       SSEType
	ProjectID  string
	PlanID     string
	Thoughts   string
	ToolName   string
	Content    string
	ErrCode    int
	ErrMessage string
}

// SSEEvent represents a parsed SSE event.
type SSEEvent struct {
	EventID    int        `json:"event_id"`
	StepID     string     `json:"step_id"`
	Type       SSEType    `json:"type"`
	ProjectID  string     `json:"project_id"`
	TaskID     string     `json:"task_id"`
	PlanID     string     `json:"plan_id"`
	StageIndex *int       `json:"stage_index"`
	Stages     []sseStage `json:"stages"`
	Message    *string    `json:"message"`
	Data       sseData    `json:"data"`
	ErrCode    int        `json:"-"`
	ErrMessage string     `json:"-"`
}

type sseData struct {
	ID               string         `json:"id"`
	Type             SSEType        `json:"type"`
	Status           PlanStatus     `json:"status"`
	Description      string         `json:"description"`
	ActiveStageIndex *int           `json:"active_stage_index"`
	CreatedAt        string         `json:"created_at"`
	EventID          int            `json:"event_id"`
	Thoughts         string         `json:"thoughts"`
	Intention        ForceIntention `json:"intention"`
	ToolCalls        []toolCall     `json:"tool_calls,omitempty"`
	AgentResponse    []responseInfo `json:"response"`
}

type sseStage struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Notes       string `json:"notes"`
	PlanID      string `json:"plan_id"`
	Index       int    `json:"index"`
}

// toolCall represents a tool call struct only select some fields.
type toolCall struct {
	ToolName  string     `json:"tool_name"`
	ToolAlias *string    `json:"tool_alias"` // tool alias, can be nil
	Status    PlanStatus `json:"status"`     // tool call status
}

// responseInfo represents a response info struct only select some fields.
type responseInfo struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

const (
	sseDataPrefix  = "data:"
	sseEventPrefix = "event:"
)

// ProcessPlanStream processes a vedas plan stream.
func (c *Client) ProcessPlanStream(ctx context.Context, req CreatePlanResponse, channelBufSize int) (<-chan *SSEPlanResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.APISSEURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	respChan := make(chan *SSEPlanResponse, channelBufSize)

	go func() {
		defer func() {
			resp.Body.Close()
			close(respChan)
		}()
		scanner := bufio.NewScanner(resp.Body)
		// Increase the scanner token limit beyond the default 64KB to handle larger SSE lines.
		buf := make([]byte, c.maxEventSize)
		scanner.Buffer(buf, c.maxEventSize)

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}
			line := strings.TrimSpace(scanner.Text())
			// Ignore empty lines and lines that don't start with "data:"
			if line == "" || !strings.HasPrefix(line, sseDataPrefix) {
				continue
			}
			info := strings.TrimSpace(strings.TrimPrefix(line, sseDataPrefix))
			c.processSSEEvent(info, respChan)
		}
		if err := scanner.Err(); err != nil {
			c.sendErrorEvent("failed to read response", -1, respChan)
			return
		}
	}()
	return respChan, nil
}

// processSSEEvent processes accumulated SSE event data and sends it to the response channel.
func (c *Client) processSSEEvent(line string, respChan chan *SSEPlanResponse) {
	resp := &SSEEvent{}
	if err := json.Unmarshal([]byte(line), resp); err != nil {
		c.sendErrorEvent("failed to decode response", -1, respChan)
		return
	}

	respChan <- c.convertSSEEvent2Response(resp)
}

func (c *Client) convertSSEEvent2Response(in *SSEEvent) *SSEPlanResponse {
	var toolName string
	var content string
	if len(in.Data.ToolCalls) > 0 {
		toolName = in.Data.ToolCalls[0].ToolName
		if in.Data.ToolCalls[0].ToolAlias != nil {
			toolName = *in.Data.ToolCalls[0].ToolAlias
		}
	}
	if len(in.Data.AgentResponse) > 0 {
		content = in.Data.AgentResponse[0].Text
	}
	out := &SSEPlanResponse{
		Type:      in.Type,
		ProjectID: in.ProjectID,
		PlanID:    in.PlanID,
		Thoughts:  in.Data.Thoughts,
		Content:   content,
		ToolName:  toolName,
	}
	return out
}

func (c *Client) sendErrorEvent(msg string, retCode int, respChan chan *SSEPlanResponse) {
	resp := &SSEPlanResponse{
		ErrCode:    retCode,
		ErrMessage: msg,
	}
	select {
	case respChan <- resp:
	default:
		log.Warnf("failed to send error event, channel may be full or closed: %s", msg)
	}
}
