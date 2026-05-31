//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package promptengine

import "time"

// traceStatus represents the completion status of a single execution trace.
type traceStatus string

// trace status constants.
const (
	// traceStatusCompleted indicates the trace completed successfully.
	traceStatusCompleted traceStatus = "completed"
	// traceStatusIncomplete indicates the trace did not complete normally.
	traceStatusIncomplete traceStatus = "incomplete"
	// traceStatusFailed indicates the trace failed with an error.
	traceStatusFailed traceStatus = "failed"
)

// stepType indicates the type of a trace step.
type stepType string

// Step type constants.
const (
	// stepTypeModel indicates a model/LLM call step.
	stepTypeModel stepType = "model"
	// stepTypeTool indicates a tool execution step.
	stepTypeTool stepType = "tool"
	// stepTypeAgent indicates an agent invocation step.
	stepTypeAgent stepType = "agent"
)

// nodeKind indicates the kind of node in the static structure.
// It aligns with the static graph's node "kind" field so that dynamic traces
// can be correlated with the structure snapshot.
type nodeKind string

// Node kind constants.
const (
	// nodeKindCoordinator indicates a coordinator (root) agent node.
	nodeKindCoordinator nodeKind = "coordinator"
	// nodeKindMember indicates a member (sub) agent node.
	nodeKindMember nodeKind = "member"
	// nodeKindTool indicates a tool node.
	nodeKindTool nodeKind = "tool"
)

// trace describes a step-DAG of a single Runner execution.
//
// One trace is produced per root invocation; sub-agent steps are merged into
// the root's trace to keep correlation simple.
type trace struct {
	// StructureID identifies the static structure/schema used for this execution.
	StructureID string `json:"structure_id"`
	// InvocationID is the unique identifier of the root invocation.
	InvocationID string `json:"invocation_id"`
	// AgentName is the name of the root agent.
	AgentName string `json:"agent_name"`
	// Status indicates the completion status of this trace.
	Status traceStatus `json:"status"`
	// Input contains the root invocation input.
	Input *traceInput `json:"input,omitempty"`
	// FinalOutput contains the final textual output of the execution.
	FinalOutput *traceOutput `json:"final_output,omitempty"`
	// Steps contains all steps recorded during execution, in completion order.
	Steps []traceStep `json:"steps"`
	// StartTime is when the root invocation started.
	StartTime time.Time `json:"start_time"`
	// EndTime is when the root invocation ended.
	EndTime time.Time `json:"end_time"`
	// Duration is the total execution duration.
	Duration time.Duration `json:"duration"`
	// Error contains the error message if Status is traceStatusFailed.
	Error string `json:"error,omitempty"`
}

// traceStep describes a single node visit in the execution trace.
type traceStep struct {
	// StepID is a unique identifier within the trace.
	StepID string `json:"step_id"`
	// NodeID identifies the corresponding static node (agent name or tool name).
	NodeID string `json:"node_id"`
	// stepType indicates the kind of step (model, tool, agent).
	StepType stepType `json:"step_type"`
	// nodeKind indicates the kind of node in the static structure.
	NodeKind nodeKind `json:"node_kind,omitempty"`
	// PredecessorStepIDs contains the IDs of direct predecessors that were hit.
	PredecessorStepIDs []string `json:"predecessor_step_ids,omitempty"`
	// AppliedSurfaceIDs contains the IDs of surfaces that took effect here.
	AppliedSurfaceIDs []string `json:"applied_surface_ids,omitempty"`
	// Input describes the input to this step.
	Input *traceInput `json:"input,omitempty"`
	// Output describes the output produced by this step.
	Output *traceOutput `json:"output,omitempty"`
	// Error contains the error message if this step failed.
	Error string `json:"error,omitempty"`
	// StartTime is when this step started.
	StartTime time.Time `json:"start_time"`
	// EndTime is when this step ended.
	EndTime time.Time `json:"end_time"`
	// Duration is how long this step took.
	Duration time.Duration `json:"duration"`
}

// traceInput describes the input of a step.
type traceInput struct {
	// Text is a textual representation of the input.
	Text string `json:"text"`
	// ToolName is set when this is a tool input.
	ToolName string `json:"tool_name,omitempty"`
	// ToolArguments contains the raw tool arguments (JSON) if applicable.
	ToolArguments string `json:"tool_arguments,omitempty"`
	// MessageCount is the number of messages in a model input.
	MessageCount int `json:"message_count,omitempty"`
}

// traceOutput describes the output of a step.
type traceOutput struct {
	// Text is a textual representation of the output.
	Text string `json:"text"`
	// ToolResult contains the tool result (serialized) if applicable.
	ToolResult string `json:"tool_result,omitempty"`
	// tokenUsage contains LLM token usage for model calls.
	//
	// Note: this field is the *LLM token accounting* (prompt / completion
	// tokens). It is unrelated to the business isolation Token in
	// runtimeConfig, which identifies the calling tenant.
	TokenUsage *tokenUsage `json:"token_usage,omitempty"`
}

// tokenUsage contains LLM token usage statistics for a model step.
type tokenUsage struct {
	// PromptTokens is the number of tokens in the prompt.
	PromptTokens int `json:"prompt_tokens"`
	// CompletionTokens is the number of tokens in the completion.
	CompletionTokens int `json:"completion_tokens"`
	// TotalTokens is the total number of tokens used.
	TotalTokens int `json:"total_tokens"`
}
