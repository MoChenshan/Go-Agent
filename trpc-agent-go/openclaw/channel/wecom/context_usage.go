package wecom

import (
	"fmt"
	"strconv"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
)

const (
	statusLabelContext = "上下文占用："

	compactTokenBaseK = 1000
	compactTokenBaseM = 1000000

	compactTokenThresholdPlain = compactTokenBaseK
	compactTokenThresholdK1    = 100 * compactTokenBaseK
	compactTokenThresholdK0    = compactTokenBaseM
	compactTokenThresholdM1    = 100 * compactTokenBaseM

	compactTokenFormatK1 = "%.1fK"
	compactTokenFormatK0 = "%.0fK"
	compactTokenFormatM1 = "%.1fM"
	compactTokenFormatM0 = "%.0fM"

	compactTokenTrimK = ".0K"
	compactTokenTrimM = ".0M"
	compactTokenKeepK = "K"
	compactTokenKeepM = "M"

	contextUsagePercentBase            = 100
	contextUsagePercentThresholdTenths = 10
	contextUsagePercentFormatTenths    = "%.1f%%"
	contextUsagePercentFormatInteger   = "%.0f%%"
	contextUsagePercentTrim            = ".0%"
	contextUsagePercentKeep            = "%"
)

type contextUsageStatus struct {
	usedTokens       int
	promptTokens     int
	completionTokens int
	totalTokens      int
	contextWindow    int
}

func (c *Channel) runtimeContextWindow() int {
	if c == nil {
		return 0
	}
	window, ok := model.LookupModelContextWindow(c.runtimeModelDisplayName())
	if !ok {
		return 0
	}
	return window
}

func (c *Channel) recordContextUsage(
	sessionID string,
	requestID string,
	usage *gwclient.Usage,
) {
	if c == nil || c.runStatus == nil {
		return
	}
	c.runStatus.setUsage(
		sessionID,
		requestID,
		usage,
		c.runtimeContextWindow(),
	)
}

func (c *Channel) refreshReplyDisplayPrefix(
	sessionID string,
	state *replyStreamState,
) string {
	prefix := c.replyContextPrefix(sessionID)
	if state != nil {
		state.displayPrefix = prefix
	}
	return prefix
}

func buildContextUsageStatus(
	usage *gwclient.Usage,
	contextWindow int,
) *contextUsageStatus {
	if usage == nil || contextWindow <= 0 {
		return nil
	}
	promptTokens := max(usage.PromptTokens, 0)
	completionTokens := max(usage.CompletionTokens, 0)
	totalTokens := usage.TotalTokens
	if totalTokens <= 0 {
		totalTokens = promptTokens + completionTokens
	}
	// Prefer LastPromptTokens (the most recent LLM call's prompt_tokens)
	// because PromptTokens/TotalTokens are aggregated across all LLM calls
	// in a request (tool-call loops) and vastly overstate actual context
	// window occupancy.
	usedTokens := usage.LastPromptTokens
	if usedTokens <= 0 {
		usedTokens = promptTokens
	}
	if usedTokens <= 0 {
		usedTokens = totalTokens
	}
	if usedTokens <= 0 {
		return nil
	}
	return &contextUsageStatus{
		usedTokens:       usedTokens,
		promptTokens:     promptTokens,
		completionTokens: completionTokens,
		totalTokens:      totalTokens,
		contextWindow:    contextWindow,
	}
}

func cloneContextUsageStatus(
	status *contextUsageStatus,
) *contextUsageStatus {
	if status == nil {
		return nil
	}
	cloned := *status
	return &cloned
}

func formatContextUsage(status *contextUsageStatus) string {
	if status == nil ||
		status.contextWindow <= 0 ||
		status.usedTokens <= 0 {
		return ""
	}
	display := formatCompactTokenCount(status.usedTokens) +
		" / " +
		formatCompactTokenCount(status.contextWindow)
	percent := formatContextUsagePercent(status)
	if percent == "" {
		return display
	}
	return display + " (" + percent + ")"
}

func formatContextUsagePercent(
	status *contextUsageStatus,
) string {
	if status == nil ||
		status.contextWindow <= 0 ||
		status.usedTokens <= 0 {
		return ""
	}
	percent := float64(status.usedTokens) *
		contextUsagePercentBase /
		float64(status.contextWindow)
	if percent < contextUsagePercentThresholdTenths {
		return trimContextUsagePercent(
			fmt.Sprintf(
				contextUsagePercentFormatTenths,
				percent,
			),
		)
	}
	return fmt.Sprintf(
		contextUsagePercentFormatInteger,
		percent,
	)
}

func formatCompactTokenCount(tokens int) string {
	switch {
	case tokens < compactTokenThresholdPlain:
		return strconv.Itoa(tokens)
	case tokens < compactTokenThresholdK1:
		return trimCompactDecimal(
			fmt.Sprintf(
				compactTokenFormatK1,
				float64(tokens)/compactTokenBaseK,
			),
		)
	case tokens < compactTokenThresholdK0:
		return fmt.Sprintf(
			compactTokenFormatK0,
			float64(tokens)/compactTokenBaseK,
		)
	case tokens < compactTokenThresholdM1:
		return trimCompactDecimal(
			fmt.Sprintf(
				compactTokenFormatM1,
				float64(tokens)/compactTokenBaseM,
			),
		)
	default:
		return fmt.Sprintf(
			compactTokenFormatM0,
			float64(tokens)/compactTokenBaseM,
		)
	}
}

func trimCompactDecimal(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(
		value,
		compactTokenTrimK,
		compactTokenKeepK,
	)
	value = strings.ReplaceAll(
		value,
		compactTokenTrimM,
		compactTokenKeepM,
	)
	return value
}

func trimContextUsagePercent(value string) string {
	value = strings.TrimSpace(value)
	return strings.ReplaceAll(
		value,
		contextUsagePercentTrim,
		contextUsagePercentKeep,
	)
}

func statusContextUsage(
	status *requestRunStatus,
) *contextUsageStatus {
	if status == nil {
		return nil
	}
	return status.contextUsage
}

func replyPrefixContextUsage(
	snapshot sessionRunSnapshot,
) *contextUsageStatus {
	if usage := statusContextUsage(snapshot.active); usage != nil {
		return usage
	}
	if usage := statusContextUsage(snapshot.last); usage != nil {
		return usage
	}
	if usage := statusContextUsage(snapshot.queued); usage != nil {
		return usage
	}
	return nil
}
