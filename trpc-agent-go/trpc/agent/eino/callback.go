package eino

import (
	icallbacks "git.woa.com/trpc-go/trpc-agent-go/trpc/agent/eino/internal/callbacks"
	"github.com/cloudwego/eino/callbacks"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// CallbackConfig provides configuration options for callback adapters.
type CallbackConfig struct {
	// NodeFilter specifies which Eino nodes to handle (empty = handle all)
	NodeFilter map[string]bool
}

// CallbackOption configures callback adapters.
type CallbackOption func(*CallbackConfig)

// WithCallbackNodeFilter sets which Eino nodes to handle in callbacks.
// If empty, all nodes will be handled.
func WithCallbackNodeFilter(nodes ...string) CallbackOption {
	return func(config *CallbackConfig) {
		config.NodeFilter = make(map[string]bool)
		for _, node := range nodes {
			config.NodeFilter[node] = true
		}
	}
}

// NewToolCallbacks converts an Eino callback handler to tRPC tool callbacks.
// This allows users to reuse their existing Eino callback logic when using tRPC native agents.
//
// Example:
//
//	einoHandler := &MyEinoToolHandler{...}
//	trpcCallbacks := teino.NewToolCallbacks(einoHandler)
//
//	agent := llmagent.New("service",
//	    llmagent.WithModel(model),
//	    llmagent.WithToolCallbacks(trpcCallbacks),
//	)
func NewToolCallbacks(einoHandler callbacks.Handler, options ...CallbackOption) *tool.Callbacks {
	config := &CallbackConfig{}
	for _, opt := range options {
		opt(config)
	}

	internalConfig := &icallbacks.CallbackConfig{
		NodeFilter: config.NodeFilter,
	}

	return icallbacks.NewToolCallbackAdapter(einoHandler, internalConfig)
}

// NewModelCallbacks converts an Eino callback handler to tRPC model callbacks.
// This allows users to reuse their existing Eino callback logic when using tRPC native agents.
//
// Example:
//
//	einoHandler := &MyEinoModelHandler{...}
//	trpcCallbacks := teino.NewModelCallbacks(einoHandler)
//
//	agent := llmagent.New("service",
//	    llmagent.WithModel(model),
//	    llmagent.WithModelCallbacks(trpcCallbacks),
//	)
func NewModelCallbacks(einoHandler callbacks.Handler, options ...CallbackOption) *model.Callbacks {
	config := &CallbackConfig{}
	for _, opt := range options {
		opt(config)
	}

	internalConfig := &icallbacks.CallbackConfig{
		NodeFilter: config.NodeFilter,
	}

	return icallbacks.NewModelCallbackAdapter(einoHandler, internalConfig)
}
