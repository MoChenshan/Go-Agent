// Package vedas is vedas sdk configs
package vedas

import (
	"git.woa.com/trpc-go/trpc-agent-go/trpc/internal/vedas"
)

// ConfigOption is an option for the VedasConfigOption.
type ConfigOption func(*Configs)

const (
	// defaultVedasMode is the default safe mode for the vedas.
	defaultVedasMode = vedas.PlanModeSafe
	// defaultVedasIntention is the default (empty) intention:
	// when not passed, the agent will judge the task intention automatically.
	defaultVedasIntention = vedas.ForceIntentionAuto
)

// Configs options
type Configs struct {
	// Attachments is the attachments for the vedas.
	Attachments []string
	// MCPInstances is the mcp instances for the vedas.
	// refer https://iwiki.woa.com/p/4014325318
	MCPInstances []string
	// Mode is the plan mode for the vedas.
	// safe: only inner api will be called. & smart: will call external api.
	// refer https://iwiki.woa.com/p/4014325318
	Mode vedas.PlanMode
	// Intention is the force intention for the vedas.
	// empty (default): auto detect by agent, omit the field in request.
	// one_stage_task: multi-step task (e.g. query weather then send notification), short time, no result file, generates conclusion.
	// multi_stage_research: planning/research task, longer time, generates result files.
	// refer https://iwiki.woa.com/p/4014325318
	Intention vedas.ForceIntention // force intention
}

// NewConfigs creates a new vedasOption with functional options.
func NewConfigs(opts ...ConfigOption) *Configs {
	opt := &Configs{
		Mode:         defaultVedasMode,
		Intention:    defaultVedasIntention,
		MCPInstances: []string{},
	}
	for _, o := range opts {
		o(opt)
	}
	return opt
}

// WithMCPInstances sets the vedas mcp instances.
func WithMCPInstances(instances []string) ConfigOption {
	return func(opt *Configs) {
		opt.MCPInstances = instances
	}
}

// WithAttachments sets the vedas attachments.
func WithAttachments(attachments []string) ConfigOption {
	return func(opt *Configs) {
		opt.Attachments = attachments
	}
}

// WithMultiStageTask sets the vedas plan mode to multi_stage_research.
// if not set, the plan mode will be auto detected by agent (empty intention).
func WithMultiStageTask() ConfigOption {
	return func(opt *Configs) {
		opt.Intention = vedas.ForceIntentionMulti
	}
}

// WithUnSafeSmartMode sets the vedas plan mode to smart which will call external api.
// if not set, the plan mode will be safe mode: only inner api will be called.
func WithUnSafeSmartMode() ConfigOption {
	return func(opt *Configs) {
		opt.Mode = vedas.PlanModeSmart
	}
}
