package wecom

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/assistantname"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/promptasset"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/registry"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	AssistantNameToolProviderType = "assistant_name"

	assistantNameToolName = "set_assistant_name"

	assistantNameScopeKey = "scope"
	assistantNameValueKey = "value"

	schemaTypeObject = "object"
	schemaTypeString = "string"

	assistantNameScopeSession = "session"
	assistantNameScopeGlobal  = "global"
)

type setAssistantNameTool struct {
	stateDir string
}

type setAssistantNameInput struct {
	Scope string `json:"scope"`
	Value string `json:"value"`
}

type setAssistantNameResult struct {
	Scope         string `json:"scope,omitempty"`
	Configured    string `json:"configured,omitempty"`
	EffectiveName string `json:"effective_name,omitempty"`
	Cleared       bool   `json:"cleared"`
}

func init() {
	if err := registry.RegisterToolProvider(
		AssistantNameToolProviderType,
		newAssistantNameTools,
	); err != nil {
		panic(err)
	}
}

func newAssistantNameTools(
	deps registry.ToolProviderDeps,
	spec registry.PluginSpec,
) ([]tool.Tool, error) {
	if spec.Config != nil {
		var cfg struct{}
		if err := registry.DecodeStrict(spec.Config, &cfg); err != nil {
			return nil, err
		}
	}
	return []tool.Tool{
		setAssistantNameTool{
			stateDir: strings.TrimSpace(deps.StateDir),
		},
	}, nil
}

func (t setAssistantNameTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name: assistantNameToolName,
		Description: "Set, remember, change, or clear the " +
			"assistant's name. Use this only when the user " +
			"explicitly asks you to set, remember, change, or " +
			"clear your name, or directly assigns you a new " +
			"name in plain language. Use scope=session for the " +
			"current chat name. Use scope=global only when " +
			"the user clearly wants the default name used by " +
			"other chats that have not picked their own name. " +
			"Pass an empty value, off, clear, or reset to clear " +
			"the chosen scope.",
		InputSchema: &tool.Schema{
			Type:     schemaTypeObject,
			Required: []string{assistantNameScopeKey},
			Properties: map[string]*tool.Schema{
				assistantNameScopeKey: {
					Type: schemaTypeString,
					Enum: []any{
						assistantNameScopeSession,
						assistantNameScopeGlobal,
					},
					Description: "Choose session for the current " +
						"chat name or global for the default " +
						"name used by chats that do not have " +
						"their own name yet.",
				},
				assistantNameValueKey: {
					Type: schemaTypeString,
					Description: "Name to persist. Leave empty to " +
						"clear the chosen scope.",
				},
			},
		},
	}
}

func (t setAssistantNameTool) Call(
	ctx context.Context,
	args []byte,
) (any, error) {
	var in setAssistantNameInput
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}

	scope := strings.ToLower(strings.TrimSpace(in.Scope))
	switch scope {
	case assistantNameScopeSession:
		return t.setSessionName(ctx, in.Value)
	case assistantNameScopeGlobal:
		return t.setGlobalName(in.Value)
	default:
		return nil, fmt.Errorf(
			"assistant_name: unsupported scope %q",
			in.Scope,
		)
	}
}

func (t setAssistantNameTool) setSessionName(
	ctx context.Context,
	value string,
) (any, error) {
	inv, ok := agent.InvocationFromContext(ctx)
	if !ok || inv == nil || inv.Session == nil {
		return nil, fmt.Errorf(
			"assistant_name: current session context is unavailable",
		)
	}

	sessionID := strings.TrimSpace(inv.Session.ID)
	if sessionID == "" {
		return nil, fmt.Errorf(
			"assistant_name: current session id is unavailable",
		)
	}
	baseSessionID := baseSessionIDForSession(sessionID)
	tracker := sharedSessionTrackerWithPath(
		sessionTrackerStorePath(t.stateDir),
	)

	configured := assistantname.Normalize(value)
	if assistantname.IsResetToken(value) {
		configured = ""
	}
	info := tracker.setAssistantAlias(baseSessionID, configured)
	return setAssistantNameResult{
		Scope:      assistantNameScopeSession,
		Configured: configured,
		EffectiveName: resolveToolEffectiveAssistantName(
			t.stateDir,
			info,
		),
		Cleared: configured == "",
	}, nil
}

func (t setAssistantNameTool) setGlobalName(value string) (any, error) {
	path := promptasset.DefaultPaths(t.stateDir).IdentityFile
	configured := assistantname.Normalize(value)
	if assistantname.IsResetToken(value) {
		configured = ""
	}
	if err := assistantname.WriteFile(path, configured); err != nil {
		return nil, err
	}
	return setAssistantNameResult{
		Scope:      assistantNameScopeGlobal,
		Configured: configured,
		EffectiveName: resolveToolEffectiveAssistantName(
			t.stateDir,
			nil,
		),
		Cleared: configured == "",
	}, nil
}

func resolveToolEffectiveAssistantName(
	stateDir string,
	info *sessionInfo,
) string {
	if info != nil {
		if alias := normalizeAssistantAlias(info.assistantAlias); alias != "" {
			return alias
		}
	}
	name, err := assistantname.ReadFile(
		promptasset.DefaultPaths(stateDir).IdentityFile,
	)
	if err == nil && name != "" {
		return name
	}
	return defaultAssistantDisplayName
}
