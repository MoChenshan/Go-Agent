package wecom

import (
	"context"
	"errors"
	"fmt"
	"strings"

	publicsubagent "git.woa.com/trpc-go/trpc-agent-go/openclaw/subagent"
)

const (
	subagentActionHelp   = "help"
	subagentActionList   = "list"
	subagentActionGet    = "get"
	subagentActionCancel = "cancel"

	subagentUnavailableMessage = "当前环境未启用 subagent 管理。"
	subagentEmptyMessage       = "当前会话还没有 subagent。"
	subagentNotFoundPrefix     = "当前会话里没有这个 subagent："

	subagentListTitle   = "🧠 当前会话的 subagent："
	subagentDetailTitle = "🧠 subagent 详情"

	subagentLabelID      = "ID："
	subagentLabelStatus  = "状态："
	subagentLabelTask    = "任务："
	subagentLabelSummary = "摘要："
	subagentLabelError   = "错误："

	subagentStatusQueued    = "排队中"
	subagentStatusRunning   = "运行中"
	subagentStatusCompleted = "已完成"
	subagentStatusFailed    = "失败"
	subagentStatusCanceled  = "已取消"
)

func (c *Channel) SetSubagentService(
	svc publicsubagent.Service,
) {
	if c == nil {
		return
	}
	c.subagentService = svc
}

func (c *Channel) handleSubagentsCommand(
	ctx context.Context,
	chatID string,
	userID string,
	baseSessionID string,
	cmd parsedCommand,
	sender messageSender,
) error {
	if c == nil || c.subagentService == nil {
		_ = sendTextReply(
			ctx,
			sender,
			chatID,
			subagentUnavailableMessage+"\n"+subagentsCommandUsage,
		)
		return nil
	}

	action, runID, usage := parseSubagentCommand(cmd.args)
	if usage != "" {
		_ = sendTextReply(ctx, sender, chatID, usage)
		return nil
	}

	sessionInfo := c.sessionTracker.getOrCreateSession(
		baseSessionID,
		0,
	)
	currentSessionID := strings.TrimSpace(sessionInfo.sessionID)

	switch action {
	case subagentActionHelp:
		_ = sendTextReply(
			ctx,
			sender,
			chatID,
			subagentsCommandUsage,
		)
	case subagentActionList:
		runs := c.subagentService.ListForUser(
			userID,
			publicsubagent.ListFilter{
				ParentSessionID: currentSessionID,
			},
		)
		_ = sendTextReply(
			ctx,
			sender,
			chatID,
			formatSubagentList(runs),
		)
	case subagentActionGet:
		run, err := c.currentSessionSubagentRun(
			userID,
			currentSessionID,
			runID,
		)
		if err != nil {
			_ = sendTextReply(ctx, sender, chatID, err.Error())
			return nil
		}
		_ = sendTextReply(
			ctx,
			sender,
			chatID,
			formatSubagentDetail(*run),
		)
	case subagentActionCancel:
		if _, err := c.currentSessionSubagentRun(
			userID,
			currentSessionID,
			runID,
		); err != nil {
			_ = sendTextReply(ctx, sender, chatID, err.Error())
			return nil
		}
		run, _, err := c.subagentService.CancelForUser(userID, runID)
		if err != nil {
			_ = sendTextReply(
				ctx,
				sender,
				chatID,
				subagentNotFoundMessage(runID),
			)
			return nil
		}
		_ = sendTextReply(
			ctx,
			sender,
			chatID,
			formatSubagentDetail(*run),
		)
	default:
		_ = sendTextReply(ctx, sender, chatID, subagentsCommandUsage)
	}
	return nil
}

func parseSubagentCommand(args []string) (
	string,
	string,
	string,
) {
	if len(args) == 0 {
		return subagentActionList, "", ""
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case subagentActionHelp:
		return subagentActionHelp, "", ""
	case subagentActionList:
		if len(args) == 1 {
			return subagentActionList, "", ""
		}
	case subagentActionGet:
		if len(args) == 2 && strings.TrimSpace(args[1]) != "" {
			return subagentActionGet, strings.TrimSpace(args[1]), ""
		}
	case subagentActionCancel:
		if len(args) == 2 && strings.TrimSpace(args[1]) != "" {
			return subagentActionCancel, strings.TrimSpace(args[1]), ""
		}
	}
	return "", "", subagentsCommandUsage
}

func (c *Channel) currentSessionSubagentRun(
	userID string,
	sessionID string,
	runID string,
) (*publicsubagent.Run, error) {
	if c == nil || c.subagentService == nil {
		return nil, errors.New(subagentUnavailableMessage)
	}

	run, err := c.subagentService.GetForUser(userID, runID)
	if err != nil || run == nil {
		return nil, errors.New(subagentNotFoundMessage(runID))
	}
	if strings.TrimSpace(run.ParentSessionID) !=
		strings.TrimSpace(sessionID) {
		return nil, errors.New(subagentNotFoundMessage(runID))
	}
	return run, nil
}

func formatSubagentList(runs []publicsubagent.Run) string {
	if len(runs) == 0 {
		return subagentEmptyMessage + "\n" + subagentsCommandUsage
	}

	lines := []string{subagentListTitle}
	for index, run := range runs {
		lines = append(
			lines,
			fmt.Sprintf(
				"%d. %s %s",
				index+1,
				run.ID,
				formatSubagentStatus(run.Status),
			),
		)
		if detail := subagentPreview(run); detail != "" {
			lines = append(lines, "   "+detail)
		}
	}
	lines = append(lines, "", subagentsCommandUsage)
	return strings.Join(lines, "\n")
}

func formatSubagentDetail(run publicsubagent.Run) string {
	lines := []string{
		subagentDetailTitle,
		subagentLabelID + run.ID,
		subagentLabelStatus + formatSubagentStatus(run.Status),
	}
	if task := strings.TrimSpace(run.Task); task != "" {
		lines = append(lines, subagentLabelTask+task)
	}
	if summary := strings.TrimSpace(run.Summary); summary != "" {
		lines = append(lines, subagentLabelSummary+summary)
	}
	if errText := strings.TrimSpace(run.Error); errText != "" {
		lines = append(lines, subagentLabelError+errText)
	}
	return strings.Join(lines, "\n")
}

func subagentPreview(run publicsubagent.Run) string {
	if summary := strings.TrimSpace(run.Summary); summary != "" {
		return summary
	}
	if errText := strings.TrimSpace(run.Error); errText != "" {
		return errText
	}
	return strings.TrimSpace(run.Task)
}

func formatSubagentStatus(status publicsubagent.Status) string {
	switch status {
	case publicsubagent.StatusQueued:
		return subagentStatusQueued
	case publicsubagent.StatusRunning:
		return subagentStatusRunning
	case publicsubagent.StatusCompleted:
		return subagentStatusCompleted
	case publicsubagent.StatusFailed:
		return subagentStatusFailed
	case publicsubagent.StatusCanceled:
		return subagentStatusCanceled
	default:
		return string(status)
	}
}

func subagentNotFoundMessage(runID string) string {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return subagentNotFoundPrefix
	}
	return subagentNotFoundPrefix + runID
}
