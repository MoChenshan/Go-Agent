// Package vedas provides vedas agent.
package vedas

import (
	"context"
	"fmt"
	"strings"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/internal/vedas"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	defaultChannelBufSize = 256
)

// Agent is the struct of vedas agent.
type Agent struct {
	name           string
	desc           string
	channelBufSize int
	maxEventSize   int

	configs *Configs
	client  *vedas.Client
}

// validate validates the agent configuration
func (a *Agent) validate() error {
	if a.channelBufSize <= 0 {
		return fmt.Errorf("channel buffer size must be positive, got %d", a.channelBufSize)
	}
	return nil
}

// Run executes the provided invocation within the given context and returns
// a channel of events that represent the progress and results of the execution.
func (a *Agent) Run(ctx context.Context, invocation *agent.Invocation) (<-chan *event.Event, error) {
	req, err := a.buildPlanRequest(invocation)
	if err != nil {
		return nil, err
	}

	resp, err := a.client.CreatePlan(ctx, req)
	if err != nil {
		return nil, err
	}
	// plan ID is a single ID for all streaming events
	if resp.Data.PlanID == "" {
		return nil, fmt.Errorf("failed to create plan")
	}
	respChan, err := a.client.ProcessPlanStream(ctx, resp.Data, a.channelBufSize)
	if err != nil {
		return nil, err
	}
	// Convert client response channel to event channel
	eventChan := make(chan *event.Event, a.channelBufSize)

	go a.processPlanStream(ctx, invocation, resp.Data.PlanID, resp.Data.ProjectID, respChan, eventChan)
	return eventChan, nil
}

type latestResponse struct {
	respErr *model.ResponseError
	result  string
	thought string
}

func (a *Agent) processPlanStream(
	ctx context.Context,
	invocation *agent.Invocation,
	planID, projectID string,
	respChan <-chan *vedas.SSEPlanResponse,
	eventChan chan<- *event.Event,
) {
	defer close(eventChan)

	latestResp := latestResponse{}
	for {
		select {
		case resp, ok := <-respChan:
			if !ok {
				// Channel closed, send final event
				a.sendFinalEvent(ctx, invocation, eventChan, latestResp, planID, projectID)
				return
			}

			// Convert client response to event
			evt := a.convertResponseToEvent(resp, invocation.InvocationID, latestResp)
			if evt == nil {
				log.Debugf("skipping nil event for invocation %s", invocation.InvocationID)
				return
			}

			latestResp.result = resp.Content
			latestResp.thought = resp.Thoughts

			if evt.Response.Error != nil {
				latestResp.respErr = evt.Response.Error
			}

			if err := agent.EmitEvent(ctx, invocation, eventChan, evt); err != nil {
				log.Infof("failed to emit event for invocation %s: %v", invocation.InvocationID, err)
				return
			}

		case <-ctx.Done():
			log.Infof("context cancelled for invocation %s", invocation.InvocationID)
			return
		}
	}
}

func (a *Agent) buildPlanRequest(invocation *agent.Invocation) (*vedas.CreatePlanRequest, error) {
	req := &vedas.CreatePlanRequest{
		ProjectName: invocation.AgentName,
		Prompt:      invocation.Message.Content,
		ProjectID:   invocation.RunOptions.RequestID,
		ExtraParams: &vedas.ExtraParams{
			Mode:           a.configs.Mode,
			ForceIntention: a.configs.Intention,
			TriggerType:    vedas.TriggerTypeAPI,
		},
		McpInstances:  a.configs.MCPInstances,
		AttachmentIDs: a.configs.Attachments,
	}
	return req, nil
}

// convertResponseToEvent converts client response to event
func (a *Agent) convertResponseToEvent(
	resp *vedas.SSEPlanResponse,
	invocationID string,
	latestResp latestResponse,
) *event.Event {
	if resp == nil {
		return nil
	}
	now := time.Now()
	evt := &event.Event{
		RequestID: resp.ProjectID,
		Response: &model.Response{
			ID:        resp.PlanID,
			Timestamp: now,
			Created:   now.Unix(),
			Done:      false,
			IsPartial: true,
			Object:    model.ObjectTypeChatCompletionChunk,
		},
		InvocationID: invocationID,
		Author:       a.name,
		ID:           resp.ProjectID,
		Timestamp:    now,
		FilterKey:    resp.ProjectID,
	}
	content := strings.TrimPrefix(resp.Content, latestResp.result)
	evt.Response.Choices = []model.Choice{
		{
			Index: 0,
			Delta: model.Message{
				Role:    model.RoleAssistant,
				Content: content,
				//ReasoningContent: resp.Thoughts,
				ToolName: resp.ToolName,
			},
		},
	}

	if resp.ErrMessage != "" {
		evt.Response.Error = &model.ResponseError{
			Type:    "UnknownError",
			Message: resp.ErrMessage,
		}
	}
	return evt
}

// sendFinalEvent sends the final event for streaming mode
func (a *Agent) sendFinalEvent(
	ctx context.Context,
	invocation *agent.Invocation,
	eventChan chan<- *event.Event,
	latestResp latestResponse,
	planID, projectID string,
) {
	now := time.Now()
	finalEvt := &event.Event{
		Response: &model.Response{
			ID:        planID,
			Done:      true,
			IsPartial: false,
			Choices: []model.Choice{
				{
					Index: 0,
					Message: model.Message{
						Role:             model.RoleAssistant,
						Content:          latestResp.result,
						ReasoningContent: latestResp.thought,
					},
				},
			},
			Object:    model.ObjectTypeChatCompletion,
			Timestamp: now,
			Created:   now.Unix(),
		},
		InvocationID: invocation.InvocationID,
		Author:       a.name,
		ID:           projectID,
		Timestamp:    now,
	}

	if latestResp.respErr != nil {
		finalEvt.Response.Error = latestResp.respErr
	}
	agent.EmitEvent(ctx, invocation, eventChan, finalEvt)
}

// Tools returns the list of tools that this agent has access to and can execute.
// These tools represent the capabilities available to the agent during invocations.
func (a *Agent) Tools() []tool.Tool {
	return []tool.Tool{}
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
