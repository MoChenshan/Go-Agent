package wecom

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"git.woa.com/trpc-go/trpc-agent-go/openclaw/releaseinfo"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimectl"
)

const (
	runtimeActionStatus    = "status"
	runtimeActionRestart   = "restart"
	runtimeActionUpgrade   = "upgrade"
	runtimeActionVersions  = "versions"
	runtimeActionChangelog = "changelog"
	runtimeActionBundle    = "bundle"
	runtimeActionForce     = "force"
	runtimeActionFull      = "full"

	runtimePermissionDenied = "您没有权限执行运行时升级或重启。"
	runtimeBundleUsage      = "用法：" + runtimeKeyword + " " +
		runtimeActionBundle + " [" + runtimeActionFull +
		" [总上限]]"
	runtimeBundleLimitHint = "总上限支持 80、80mb、1gb；" +
		"不写单位时默认按 MB 处理。"
)

func (c *Channel) handleRuntimeCommand(
	ctx context.Context,
	chatID string,
	baseSessionID string,
	fromID string,
	responseURL string,
	cmd parsedCommand,
	sender messageSender,
) error {
	args := normalizedRuntimeArgs(cmd.args)
	if len(args) > 0 && args[0] == runtimeActionBundle {
		if !c.isRuntimeAdmin(fromID) {
			_ = sendTextReply(
				ctx,
				sender,
				chatID,
				runtimePermissionDenied,
			)
			return nil
		}
		req, err := parseRuntimeDebugBundleRequest(args[1:])
		if err != nil {
			_ = sendTextReply(
				ctx,
				sender,
				chatID,
				runtimeBundleUsage+"\n"+runtimeBundleLimitHint,
			)
			return nil
		}
		if err := c.sendRuntimeDebugBundle(
			ctx,
			chatID,
			sender,
			req,
		); err != nil {
			_ = sendTextReply(
				ctx,
				sender,
				chatID,
				"打包调试资料失败："+err.Error(),
			)
		}
		return nil
	}

	if c.runtimeLifecycle == nil {
		_ = sendTextReply(
			ctx,
			sender,
			chatID,
			"当前环境未启用运行时控制。\n"+runtimeCommandUsage,
		)
		return nil
	}

	if len(args) == 0 {
		if c.sendRuntimeControlCard(
			ctx,
			chatID,
			baseSessionID,
			sender,
			"",
		) {
			return nil
		}
		_ = sendTextReply(
			ctx,
			sender,
			chatID,
			c.formatRuntimeLifecycleStatus(
				ctx,
				c.runtimeLifecycle.Status(),
			)+"\n\n"+runtimeCommandUsage,
		)
		return nil
	}

	switch args[0] {
	case runtimeActionStatus:
		if c.sendRuntimeControlCard(
			ctx,
			chatID,
			baseSessionID,
			sender,
			"",
		) {
			return nil
		}
		_ = sendTextReply(
			ctx,
			sender,
			chatID,
			c.formatRuntimeLifecycleStatus(
				ctx,
				c.runtimeLifecycle.Status(),
			),
		)
		return nil
	case runtimeActionVersions:
		index, err := c.runtimeLifecycle.ListVersions(ctx)
		if err != nil {
			_ = sendTextReply(
				ctx,
				sender,
				chatID,
				"读取版本列表失败："+err.Error(),
			)
			return nil
		}
		_ = sendTextReply(
			ctx,
			sender,
			chatID,
			formatRuntimeVersions(index),
		)
		return nil
	case runtimeActionChangelog:
		version := ""
		if len(args) > 1 {
			version = args[1]
		}
		summary, err := c.runtimeLifecycle.FetchChangeSummary(
			ctx,
			version,
			runtimeChangelogSummaryLimit,
		)
		if err != nil {
			_ = sendTextReply(
				ctx,
				sender,
				chatID,
				"读取变更摘要失败："+err.Error(),
			)
			return nil
		}
		_ = sendTextReply(
			ctx,
			sender,
			chatID,
			formatRuntimeChangelogSummary(version, summary),
		)
		return nil
	case runtimeActionRestart, runtimeActionUpgrade:
		if !c.isRuntimeAdmin(fromID) {
			_ = sendTextReply(
				ctx,
				sender,
				chatID,
				runtimePermissionDenied,
			)
			return nil
		}
	default:
		_ = sendTextReply(
			ctx,
			sender,
			chatID,
			runtimeCommandUsage,
		)
		return nil
	}

	req, err := parseRuntimeActionRequest(args)
	if err != nil {
		_ = sendTextReply(
			ctx,
			sender,
			chatID,
			runtimeCommandUsage,
		)
		return nil
	}
	req.Actor = fromID
	req.Source = "slash"

	result, actionErr := c.runtimeLifecycle.RequestAction(ctx, req)
	if actionErr != nil && !result.Started {
		reply := actionErr.Error()
		if strings.TrimSpace(result.Status.CurrentVersion) != "" {
			reply = c.formatRuntimeLifecycleStatus(
				ctx,
				result.Status,
			)
		}
		_ = sendTextReply(
			ctx,
			sender,
			chatID,
			reply,
		)
		return nil
	}
	c.stageRuntimeCompletionNotice(
		result,
		chatID,
		fromID,
		responseURL,
		runtimeCompletionReplyReqID(sender),
	)

	if c.sendRuntimeControlCard(
		ctx,
		chatID,
		baseSessionID,
		sender,
		c.formatRuntimeActionResult(ctx, result),
	) {
		return nil
	}
	_ = sendTextReply(
		ctx,
		sender,
		chatID,
		c.formatRuntimeActionResult(ctx, result),
	)
	return nil
}

func parseRuntimeActionRequest(
	args []string,
) (runtimectl.ActionRequest, error) {
	if len(args) == 0 {
		return runtimectl.ActionRequest{}, nil
	}

	switch args[0] {
	case runtimeActionRestart:
		req := runtimectl.ActionRequest{
			Kind: runtimectl.ActionRestart,
			Mode: runtimectl.ModeGraceful,
		}
		if hasRuntimeForce(args[1:]) {
			req.Mode = runtimectl.ModeForce
		}
		return req, nil
	case runtimeActionUpgrade:
		req := runtimectl.ActionRequest{
			Kind: runtimectl.ActionUpgrade,
			Mode: runtimectl.ModeGraceful,
		}
		if err := applyRuntimeUpgradeArgs(&req, args[1:]); err != nil {
			return runtimectl.ActionRequest{}, err
		}
		return req, nil
	default:
		return runtimectl.ActionRequest{}, nil
	}
}

func applyRuntimeUpgradeArgs(
	req *runtimectl.ActionRequest,
	args []string,
) error {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if arg == runtimeActionForce {
			req.Mode = runtimectl.ModeForce
			continue
		}
		if arg == releaseinfo.ChannelPreview {
			if err := setRuntimeUpgradeChannel(
				req,
				releaseinfo.ChannelPreview,
			); err != nil {
				return err
			}
			continue
		}
		if strings.TrimSpace(req.TargetChannel) != "" {
			return fmt.Errorf("runtime upgrade target is ambiguous")
		}
		req.TargetVersion = arg
	}
	return nil
}

func setRuntimeUpgradeChannel(
	req *runtimectl.ActionRequest,
	channel string,
) error {
	if strings.TrimSpace(req.TargetVersion) != "" ||
		strings.TrimSpace(req.TargetChannel) != "" {
		return fmt.Errorf("runtime upgrade target is ambiguous")
	}
	req.TargetChannel = channel
	return nil
}

func hasRuntimeForce(args []string) bool {
	for _, arg := range args {
		if strings.TrimSpace(arg) == runtimeActionForce {
			return true
		}
	}
	return false
}

func normalizedRuntimeArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.ToLower(strings.TrimSpace(arg))
		if arg == "" {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func parseRuntimeDebugBundleRequest(
	args []string,
) (runtimeDebugBundleRequest, error) {
	if len(args) == 0 {
		return runtimeDebugBundleRequest{}, nil
	}
	if args[0] != runtimeActionFull {
		return runtimeDebugBundleRequest{}, fmt.Errorf(
			"unknown runtime bundle mode %q",
			args[0],
		)
	}

	req := runtimeDebugBundleRequest{
		Full:            true,
		TotalLimitBytes: runtimeBundleFullDefaultTotalBytes,
	}
	if len(args) == 1 {
		return req, nil
	}
	if len(args) > 2 {
		return runtimeDebugBundleRequest{}, fmt.Errorf(
			"too many runtime bundle args",
		)
	}

	totalLimitBytes, err := parseRuntimeBundleTotalLimit(args[1])
	if err != nil {
		return runtimeDebugBundleRequest{}, err
	}
	req.TotalLimitBytes = totalLimitBytes
	return req, nil
}

func parseRuntimeBundleTotalLimit(raw string) (int64, error) {
	token := strings.TrimSpace(strings.ToLower(raw))
	if token == "" {
		return 0, fmt.Errorf("empty runtime bundle total limit")
	}

	valueEnd := 0
	for valueEnd < len(token) {
		ch := token[valueEnd]
		if ch < '0' || ch > '9' {
			break
		}
		valueEnd++
	}
	if valueEnd == 0 {
		return 0, fmt.Errorf("invalid runtime bundle total limit")
	}

	value, err := strconv.ParseInt(token[:valueEnd], 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid runtime bundle total limit")
	}

	unit := token[valueEnd:]
	multiplier, ok := runtimeBundleSizeMultiplier(unit)
	if !ok {
		return 0, fmt.Errorf("invalid runtime bundle total limit")
	}
	if value > runtimeBundleFullMaxTotalBytes/multiplier {
		return 0, fmt.Errorf("runtime bundle total limit too large")
	}
	return value * multiplier, nil
}

func runtimeBundleSizeMultiplier(unit string) (int64, bool) {
	switch strings.TrimSpace(unit) {
	case "", "m", "mb":
		return 1024 * 1024, true
	case "b":
		return 1, true
	case "k", "kb":
		return 1024, true
	case "g", "gb":
		return 1024 * 1024 * 1024, true
	default:
		return 0, false
	}
}
