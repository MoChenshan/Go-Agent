package wecom

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/releaseinfo"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimectl"
)

const (
	runtimeAdminPolicyInherit   = "inherit"
	runtimeAdminPolicyAllowlist = "allowlist"
	defaultRuntimeAdminPolicy   = runtimeAdminPolicyInherit

	runtimeStatusLineMode    = "当前动作："
	runtimeStatusLineVersion = "当前版本："
	runtimeStatusLineActive  = "运行中请求："
	runtimeStatusLineQueued  = "排队中请求："
	runtimeStatusLineTarget  = "目标版本："
	runtimeStatusLineActor   = "操作人："
	runtimeStatusLineSource  = "来源："
	runtimeStatusLineChanges = "最近变更："
	runtimeLatestLabel       = "最新版本变更摘要："
	runtimeVersionFmt        = "版本 %s 变更摘要："

	runtimeHintStatus = "可发送 " + runtimeKeyword +
		" status 查看运行时状态。"
	runtimeHintHelp = "可发送 " + runtimeKeyword +
		" 查看运行时卡片，或用 " + helpKeyword +
		" all 查看完整命令。"
	runtimeAdmissionRejectPrefix = "当前实例正在进行运行时切换，" +
		"暂不接收新的普通请求。"

	runtimeChangelogSummaryLimit = 5
)

func parseRuntimeAdminPolicy(raw string) (string, error) {
	policy := strings.ToLower(strings.TrimSpace(raw))
	if policy == "" {
		return defaultRuntimeAdminPolicy, nil
	}
	switch policy {
	case runtimeAdminPolicyInherit,
		runtimeAdminPolicyAllowlist:
		return policy, nil
	default:
		return "", fmt.Errorf(
			"wecom channel: unsupported runtime_admin_policy: %s",
			raw,
		)
	}
}

func (c *Channel) SetRuntimeLifecycleController(
	controller *runtimectl.Manager,
) {
	if c == nil {
		return
	}
	c.runtimeLifecycle = controller
}

func (c *Channel) isRuntimeAdmin(userID string) bool {
	if c == nil {
		return false
	}
	switch c.runtimeAdminPolicy {
	case runtimeAdminPolicyAllowlist:
		_, ok := c.runtimeAdminUsers[strings.TrimSpace(userID)]
		return ok
	default:
		return c.isUserAllowed(userID)
	}
}

func (c *Channel) runtimeLifecycleStatus() runtimectl.Status {
	if c == nil || c.runtimeLifecycle == nil {
		return runtimectl.Status{}
	}
	return c.runtimeLifecycle.Status()
}

func (c *Channel) admitRuntimeRequest(
	ctx context.Context,
	requestTag string,
) (*runtimectl.Handle, error) {
	if c == nil || c.runtimeLifecycle == nil {
		return nil, nil
	}
	handle, err := c.runtimeLifecycle.AdmitRequest(
		ctx,
		requestTag,
	)
	if err == nil {
		return handle, nil
	}
	var admissionErr *runtimectl.AdmissionError
	if !errors.As(err, &admissionErr) {
		return nil, err
	}
	return nil, err
}

func (c *Channel) runtimeAdmissionMessage() string {
	if c == nil || c.runtimeLifecycle == nil {
		return runtimeAdmissionRejectPrefix
	}
	status := c.runtimeLifecycle.Status()
	lines := []string{
		runtimeAdmissionRejectPrefix,
		c.formatRuntimeLifecycleStatus(
			context.Background(),
			status,
		),
		runtimeHintStatus,
	}
	return strings.Join(lines, "\n")
}

func (c *Channel) formatRuntimeLifecycleStatus(
	ctx context.Context,
	status runtimectl.Status,
) string {
	return formatRuntimeLifecycleStatusWithActor(
		status,
		c.runtimeActorLabel(ctx, runtimePendingActor(status)),
	)
}

func formatRuntimeLifecycleStatus(
	status runtimectl.Status,
) string {
	return formatRuntimeLifecycleStatusWithActor(
		status,
		runtimePendingActor(status),
	)
}

func formatRuntimeLifecycleStatusWithActor(
	status runtimectl.Status,
	actorLabel string,
) string {
	lines := []string{
		runtimeStatusLineVersion + strings.TrimSpace(
			status.CurrentVersion,
		),
	}
	if status.Pending == nil {
		lines = append(lines, runtimeStatusLineMode+"空闲")
		return strings.Join(lines, "\n")
	}

	lines = append(
		lines,
		runtimeStatusLineMode+runtimePendingActionText(
			status.Pending,
		),
		runtimeStatusLineActive+strconv.Itoa(
			status.RunningRequests,
		),
		runtimeStatusLineQueued+strconv.Itoa(
			status.QueuedRequests,
		),
	)
	if strings.TrimSpace(status.Pending.TargetVersion) != "" {
		lines = append(
			lines,
			runtimeStatusLineTarget+status.Pending.TargetVersion,
		)
	}
	if strings.TrimSpace(actorLabel) != "" {
		lines = append(
			lines,
			runtimeStatusLineActor+actorLabel,
		)
	}
	if strings.TrimSpace(status.Pending.Source) != "" {
		lines = append(
			lines,
			runtimeStatusLineSource+status.Pending.Source,
		)
	}
	if len(status.Pending.Summary) > 0 {
		lines = append(
			lines,
			runtimeStatusLineChanges,
		)
		for _, note := range status.Pending.Summary {
			lines = append(lines, "- "+note)
		}
	}
	return strings.Join(lines, "\n")
}

func runtimePendingActor(status runtimectl.Status) string {
	if status.Pending == nil {
		return ""
	}
	return strings.TrimSpace(status.Pending.Actor)
}

func (c *Channel) runtimeActorLabel(
	ctx context.Context,
	userID string,
) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	if c == nil || c.identityResolver == nil {
		return userID
	}
	if strings.TrimSpace(c.userLabelMode) == userLabelModeID {
		return userID
	}
	profiles := c.identityResolver.ResolveUsers(ctx, []string{userID})
	if len(profiles) == 0 {
		return userID
	}
	labels := resolvedIdentityLabels(c.userLabelMode, profiles)
	label := strings.TrimSpace(labels[userID])
	if label == "" {
		return userID
	}
	return label
}

func runtimePendingActionText(
	pending *runtimectl.PendingAction,
) string {
	if pending == nil {
		return "空闲"
	}

	action := "重启"
	if pending.Kind == runtimectl.ActionUpgrade {
		action = "升级"
	}
	mode := "无损"
	if pending.Mode == runtimectl.ModeForce {
		mode = "强制"
	}
	return mode + action
}

func formatRuntimeVersions(index releaseinfo.Index) string {
	if len(index.Versions) == 0 {
		return "当前没有可用版本信息。"
	}

	lines := []string{
		"可用版本：",
	}
	for _, entry := range index.Versions {
		line := "- " + entry.Version
		if entry.Version == index.LatestVersion {
			line += " (latest)"
		}
		lines = append(lines, line)
		for _, note := range entry.Notes {
			note = strings.TrimSpace(note)
			if note == "" {
				continue
			}
			lines = append(lines, "  - "+note)
		}
	}
	if strings.TrimSpace(index.MinSupportedTarget) != "" {
		lines = append(
			lines,
			"指定版本最小要求："+index.MinSupportedTarget,
		)
	}
	return strings.Join(lines, "\n")
}

func formatRuntimeChangelogSummary(
	version string,
	summary []string,
) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = runtimeLatestLabel
	} else {
		version = fmt.Sprintf(runtimeVersionFmt, version)
	}
	if len(summary) == 0 {
		return version + "\n暂无摘要。"
	}

	lines := []string{
		version,
	}
	for _, note := range summary {
		lines = append(lines, "- "+note)
	}
	return strings.Join(lines, "\n")
}

func (c *Channel) formatRuntimeActionResult(
	ctx context.Context,
	result runtimectl.ActionResult,
) string {
	return formatRuntimeActionResultWithStatusText(
		result,
		c.formatRuntimeLifecycleStatus(ctx, result.Status),
	)
}

func formatRuntimeActionResultWithStatusText(
	result runtimectl.ActionResult,
	statusText string,
) string {
	status := result.Status
	if status.Pending == nil {
		return "运行时动作已提交。"
	}
	lines := []string{
		"✅ 已提交：" + runtimePendingActionText(
			status.Pending,
		),
		statusText,
	}
	return strings.Join(lines, "\n\n")
}

func (c *Channel) handleRuntimeLeaseCanceled(
	ctx context.Context,
	msg WebhookMessage,
	sender messageSender,
	replyState *replyStreamState,
	sessionID string,
	requestID string,
	requestCtx context.Context,
) error {
	c.runStatus.cancel(sessionID, requestID)
	runtimeMessage := runtimectl.UserMessageFromContext(
		requestCtx,
	)
	if strings.TrimSpace(runtimeMessage) == "" {
		runtimeMessage = c.cancelOKMessage
	}
	if c.finishReplyHintOnError(
		ctx,
		msg,
		sender,
		replyState,
		runtimeMessage,
	) {
		return nil
	}
	return sender.SendMarkdown(ctx, msg.ChatID, runtimeMessage)
}

func (c *Channel) handleRuntimeAbortDuringRun(
	runCtx context.Context,
	msg WebhookMessage,
	sender messageSender,
	replyState *replyStreamState,
	sessionID string,
	requestID string,
) bool {
	if c == nil {
		return false
	}
	runtimeMessage := runtimectl.UserMessageFromContext(runCtx)
	if strings.TrimSpace(runtimeMessage) == "" {
		return false
	}
	c.runStatus.cancel(sessionID, requestID)
	notifyCtx, cancel := context.WithTimeout(
		context.Background(),
		gatewayCancelTimeout,
	)
	defer cancel()
	if c.finishReplyHintOnError(
		notifyCtx,
		msg,
		sender,
		replyState,
		runtimeMessage,
	) {
		return true
	}
	_ = sender.SendMarkdown(
		notifyCtx,
		msg.ChatID,
		runtimeMessage,
	)
	return true
}
