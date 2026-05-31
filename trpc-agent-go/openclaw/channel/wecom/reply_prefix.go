package wecom

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	replyPrefixFieldAssistant = "assistant"
	replyPrefixFieldPersona   = "persona"
	replyPrefixFieldContext   = "context"
	replyPrefixFieldWorkspace = "workspace"
	replyPrefixFieldLinks     = "links"
	replyPrefixFieldCommands  = "commands"
	replyPrefixFieldHint      = "hint"

	replyPrefixEmojiAssistant = "🤖 "
	replyPrefixEmojiPersona   = "🎭 "
	replyPrefixEmojiContext   = "🧠 "
	replyPrefixEmojiWorkspace = "📂 "
	replyPrefixEmojiLink      = "🔗 "
	replyPrefixEmojiCommands  = "⚡ "
	replyPrefixEmojiHint      = "💬 "

	replyPrefixLeadMarker       = "> "
	replyPrefixDefaultEnabled   = false
	replyPrefixWorkspaceDefault = "默认工作区"
	replyPrefixDefaultHint      = "直接发问题、图片或文件给我"
)

var defaultReplyPrefixFields = []string{
	replyPrefixFieldPersona,
	replyPrefixFieldContext,
	replyPrefixFieldCommands,
	replyPrefixFieldHint,
	replyPrefixFieldLinks,
}

var defaultReplyPrefixCommands = []string{
	helpKeyword,
	personaKeyword,
	statusKeyword,
}

type replyPrefixCfg struct {
	Enabled  *bool                `yaml:"enabled,omitempty"`
	Fields   []string             `yaml:"fields,omitempty"`
	Commands []string             `yaml:"commands,omitempty"`
	Hint     string               `yaml:"hint,omitempty"`
	Links    []replyPrefixLinkCfg `yaml:"links,omitempty"`
}

type replyPrefixLinkCfg struct {
	Label string `yaml:"label,omitempty"`
	URL   string `yaml:"url,omitempty"`
}

func validateReplyPrefix(cfg replyPrefixCfg) error {
	for _, raw := range cfg.Fields {
		field := strings.ToLower(strings.TrimSpace(raw))
		if field == "" {
			continue
		}
		if !isSupportedReplyPrefixField(field) {
			return fmt.Errorf(
				"wecom channel: unsupported reply_prefix "+
					"field %q",
				raw,
			)
		}
	}
	for index, link := range cfg.Links {
		label := strings.TrimSpace(link.Label)
		url := strings.TrimSpace(link.URL)
		if label == "" && url == "" {
			continue
		}
		if label == "" {
			return fmt.Errorf(
				"wecom channel: reply_prefix.links[%d] "+
					"label is required",
				index,
			)
		}
		if url == "" {
			return fmt.Errorf(
				"wecom channel: reply_prefix.links[%d] "+
					"url is required",
				index,
			)
		}
	}
	return nil
}

func resolveReplyPrefixEnabled(cfg replyPrefixCfg) bool {
	if cfg.Enabled == nil {
		return replyPrefixDefaultEnabled
	}
	return *cfg.Enabled
}

func isSupportedReplyPrefixField(field string) bool {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case replyPrefixFieldAssistant:
		return true
	case replyPrefixFieldPersona:
		return true
	case replyPrefixFieldContext:
		return true
	case replyPrefixFieldWorkspace:
		return true
	case replyPrefixFieldLinks:
		return true
	case replyPrefixFieldCommands:
		return true
	case replyPrefixFieldHint:
		return true
	default:
		return false
	}
}

func normalizeReplyPrefixFields(fields []string) []string {
	if len(fields) == 0 {
		return append([]string(nil), defaultReplyPrefixFields...)
	}
	seen := make(map[string]struct{}, len(fields))
	result := make([]string, 0, len(fields))
	for _, raw := range fields {
		field := strings.ToLower(strings.TrimSpace(raw))
		if field == "" || !isSupportedReplyPrefixField(field) {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		result = append(result, field)
	}
	if len(result) == 0 {
		return append([]string(nil), defaultReplyPrefixFields...)
	}
	return result
}

func trimReplyPrefixCommands(commands []string) []string {
	seen := make(map[string]struct{}, len(commands))
	result := make([]string, 0, len(commands))
	for _, raw := range commands {
		command := strings.TrimSpace(raw)
		if command == "" {
			continue
		}
		if _, ok := seen[command]; ok {
			continue
		}
		seen[command] = struct{}{}
		result = append(result, command)
	}
	if len(result) == 0 {
		return append(
			[]string(nil),
			defaultReplyPrefixCommands...,
		)
	}
	return result
}

func trimReplyPrefixLinks(
	links []replyPrefixLinkCfg,
) []replyPrefixLinkCfg {
	result := make([]replyPrefixLinkCfg, 0, len(links))
	for _, link := range links {
		label := strings.TrimSpace(link.Label)
		url := strings.TrimSpace(link.URL)
		if label == "" || url == "" {
			continue
		}
		result = append(result, replyPrefixLinkCfg{
			Label: label,
			URL:   url,
		})
	}
	return result
}

func (c *Channel) replyPrefixLines(sessionID string) []string {
	if c == nil {
		return nil
	}
	info := c.replyPrefixSessionInfo(sessionID)
	fields := normalizeReplyPrefixFields(c.cfg.ReplyPrefix.Fields)
	segments := make([]string, 0, len(fields)+4)
	for _, field := range fields {
		switch field {
		case replyPrefixFieldAssistant:
			if segment := c.replyPrefixAssistantLine(
				sessionID,
			); segment != "" {
				segments = append(segments, segment)
			}
		case replyPrefixFieldPersona:
			if segment := c.replyPrefixPersonaLine(info); segment != "" {
				segments = append(segments, segment)
			}
		case replyPrefixFieldContext:
			if segment := c.replyPrefixContextLine(
				sessionID,
			); segment != "" {
				segments = append(segments, segment)
			}
		case replyPrefixFieldWorkspace:
			if segment := c.replyPrefixWorkspaceLine(info); segment != "" {
				segments = append(segments, segment)
			}
		case replyPrefixFieldLinks:
			segments = append(segments, c.replyPrefixLinkLines()...)
		case replyPrefixFieldCommands:
			if segment := c.replyPrefixCommandLine(); segment != "" {
				segments = append(segments, segment)
			}
		case replyPrefixFieldHint:
			if segment := c.replyPrefixHintLine(); segment != "" {
				segments = append(segments, segment)
			}
		}
	}
	if len(segments) == 0 {
		return nil
	}
	return []string{
		replyPrefixLeadMarker +
			strings.Join(segments, " | "),
	}
}

func (c *Channel) replyPrefixAssistantLine(
	sessionID string,
) string {
	return replyPrefixEmojiAssistant +
		c.assistantDisplayNameForSession(sessionID)
}

func (c *Channel) replyPrefixPersonaLine(
	info *sessionInfo,
) string {
	display := c.replyPrefixPersonaDisplay(info)
	if display == "" {
		return ""
	}
	return replyPrefixEmojiPersona + "人格：" + display
}

func (c *Channel) replyPrefixPersonaDisplay(
	info *sessionInfo,
) string {
	personaID := defaultChatPersonaID
	if info != nil {
		personaID = info.effectivePersonaID()
	}
	if c == nil || c.personas == nil {
		return personaID
	}
	def, ok, err := c.personas.Get(personaID)
	if err == nil && ok {
		return defaultString(def.Name, def.ID)
	}
	return personaID
}

func (c *Channel) replyPrefixContextLine(
	sessionID string,
) string {
	if c == nil || c.runStatus == nil {
		return ""
	}
	display := formatContextUsage(
		replyPrefixContextUsage(
			c.runStatus.snapshot(sessionID),
		),
	)
	if display == "" {
		return ""
	}
	return replyPrefixEmojiContext + "上下文：" + display
}

func (c *Channel) replyPrefixSessionInfo(
	sessionID string,
) *sessionInfo {
	if c == nil || c.sessionTracker == nil {
		return nil
	}
	baseSessionID := baseSessionIDForSession(sessionID)
	return c.sessionTracker.getSession(baseSessionID)
}

func (c *Channel) replyPrefixWorkspaceLine(
	info *sessionInfo,
) string {
	if c == nil {
		return ""
	}
	customPath := workspacePathFromSession(info)
	defaultPath := strings.TrimSpace(c.defaultCodingWorkspace)
	switch {
	case customPath != "":
		return replyPrefixEmojiWorkspace +
			"工作区：" + baseWorkspaceName(customPath)
	case defaultPath != "":
		return replyPrefixEmojiWorkspace +
			"工作区：" + replyPrefixWorkspaceDefault
	default:
		return ""
	}
}

func baseWorkspaceName(path string) string {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "" || cleaned == "." {
		return ""
	}
	name := filepath.Base(cleaned)
	if name == "" ||
		name == "." ||
		name == string(filepath.Separator) {
		return cleaned
	}
	return name
}

func workspacePathFromSession(info *sessionInfo) string {
	if info == nil {
		return ""
	}
	return strings.TrimSpace(info.workspacePath)
}

func (c *Channel) replyPrefixLinkLines() []string {
	if c == nil {
		return nil
	}
	links := trimReplyPrefixLinks(c.cfg.ReplyPrefix.Links)
	lines := make([]string, 0, len(links))
	for _, link := range links {
		lines = append(
			lines,
			replyPrefixEmojiLink+link.Label+": "+link.URL,
		)
	}
	return lines
}

func (c *Channel) replyPrefixCommandLine() string {
	if c == nil {
		return ""
	}
	commands := trimReplyPrefixCommands(
		c.cfg.ReplyPrefix.Commands,
	)
	if len(commands) == 0 {
		return ""
	}
	return replyPrefixEmojiCommands + "常用：" +
		strings.Join(commands, " ")
}

func (c *Channel) replyPrefixHintLine() string {
	hint := strings.TrimSpace(c.cfg.ReplyPrefix.Hint)
	if hint == "" {
		hint = replyPrefixDefaultHint
	}
	if hint == "" {
		return ""
	}
	return replyPrefixEmojiHint + hint
}
