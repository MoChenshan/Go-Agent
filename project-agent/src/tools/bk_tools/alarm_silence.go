// Package bktools —— bk_alarm_silence（告警静默，D18.3 新增，第一个 bk-write 能力）。
//
// 这是 repair_agent 生态的第 4 个生产级写工具：
//
//	bcs_helm_manage       —— Release 级写（D1-D7）
//	bcs_scale_deployment  —— Deployment 副本伸缩（D18.1）
//	bcs_pod_restart       —— Pod 级重启 / 驱逐 / 滚动重启（D18.2）
//	bk_alarm_silence      —— 告警静默 / 抑制 / 撤销（本文件，D18.3）
//
// 为什么先做静默而不是改 configmap：
//   - on-call 幸福感最强：发布/灰度/已知偶发 3 类场景占 80% 的夜间唤醒
//   - 与修复动作形成闭环：先 silence 止血 → 再 rollback/scale/restart → 最后 unsilence 恢复监控
//   - 风险边界独特：静默不改业务，但"过大静默范围=真故障漏报"，是另一种维度的危险
//
// 四种静默策略（scope）：
//
//   1) by_strategy   按告警策略 ID 静默（最精准，影响面最小）
//   2) by_target     按目标（IP/Pod/集群）静默单个或多个具体对象
//   3) by_dimension  按维度标签静默（类 labelSelector，最灵活也最危险）
//   4) unsilence     撤销指定 silence_id 的静默（反悔动作，鼓励使用）
//
// 三个生产级原则（有别于 BCS 系列）：
//
//   A) 时窗强制：duration 必填，最大 24h 硬上限（避免"永久静默"变成监控黑洞）
//   B) 禁止自动续期：本工具不提供 auto_extend 参数，到期必须重新评估（合规）
//   C) unsilence 极低 Severity：恢复监控是反悔动作，应鼓励而不阻碍
package bktools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bkapi"
	"git.woa.com/trpc-go/gameops-agent/src/tools/hitl"
)

// 架构级硬上限：单次静默最大时窗（秒）。
// 24h 是合规底线 —— 超过 24h 的静默必须人工走 OA 审批。
// 即使 HITL_DISABLE=1 也无法豁免，这是防黑箱监控的最后闸门。
const silenceHardMaxSeconds = 24 * 3600

// 单次 by_target 批量保护软上限：超过此数视为高风险（Severity 升档）。
const silenceTargetsSoftLimit = 5

// 单次 by_target 批量硬上限：超过直接拒绝。
const silenceTargetsHardLimit = 50

// AlarmSilenceInput bk_alarm_silence 工具入参。
type AlarmSilenceInput struct {
	Scope             string            `json:"scope"              description:"静默维度（必填）：by_strategy(按策略ID) / by_target(按IP/Pod/集群) / by_dimension(按标签) / unsilence(撤销)"`
	BKBizID           int               `json:"bk_biz_id"          description:"蓝鲸业务 ID（新建静默必填）"`
	StrategyIDs       []int             `json:"strategy_ids"       description:"告警策略 ID 列表（scope=by_strategy 必填）"`
	Targets           []string          `json:"targets"            description:"目标列表（scope=by_target 必填）；格式 ip=10.x.x.x 或 namespace=ns,pod=xxx；单次 >5 升档，>50 硬拒"`
	Dimensions        map[string]string `json:"dimensions"         description:"维度标签（scope=by_dimension 必填）；如 {\"env\":\"gray\",\"service\":\"game-core\"}；最危险，必 Critical+RequireReason"`
	SilenceID         string            `json:"silence_id"         description:"静默记录 ID（scope=unsilence 必填）"`
	DurationSeconds   int               `json:"duration_seconds"   description:"静默时长（秒），新建必填；最大 86400（24h）；更长请走人工 OA"`
	Reason            string            `json:"reason"             description:"静默原因（合规必填项，尤其 by_dimension 必填）"`
	Confirmed         bool              `json:"confirmed"          description:"是否已获人工确认；写操作必须 true 才真正下发"`
}

// newAlarmSilenceTool 构造 bk_alarm_silence 工具。
func newAlarmSilenceTool(client *bkapi.Client) tool.Tool {
	fn := func(ctx context.Context, in AlarmSilenceInput) (*Result, error) {
		scope := strings.ToLower(strings.TrimSpace(in.Scope))
		if scope == "" {
			return nil, fmt.Errorf("scope 为必填项（by_strategy / by_target / by_dimension / unsilence）")
		}
		switch scope {
		case "by_strategy":
			return doSilenceByStrategy(ctx, client, in)
		case "by_target":
			return doSilenceByTarget(ctx, client, in)
		case "by_dimension":
			return doSilenceByDimension(ctx, client, in)
		case "unsilence":
			return doUnsilence(ctx, client, in)
		default:
			return nil, fmt.Errorf("不支持的 scope: %q（可选：by_strategy / by_target / by_dimension / unsilence）", scope)
		}
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bk_alarm_silence"),
		function.WithDescription(
			"蓝鲸监控告警静默/抑制工具，四种维度：by_strategy(按策略ID最精准) / by_target(按目标IP/Pod) / "+
				"by_dimension(按标签最灵活也最危险) / unsilence(撤销)。"+
				"⚠ 写操作必须先不带 confirmed 获取 Plan，用户确认后再 confirmed=true 重发。"+
				"时窗上限 24h（超出走 OA）；by_dimension 必填 reason 且 Severity=Critical；unsilence 是鼓励使用的反悔动作。",
		),
	)
}

// =============================================================================
// by_strategy：最精准 —— 按告警策略 ID 静默
// =============================================================================

func doSilenceByStrategy(ctx context.Context, client *bkapi.Client, in AlarmSilenceInput) (*Result, error) {
	if in.BKBizID == 0 {
		return nil, fmt.Errorf("by_strategy 必须指定 bk_biz_id")
	}
	if len(in.StrategyIDs) == 0 {
		return nil, fmt.Errorf("by_strategy 必须指定 strategy_ids（至少 1 个）")
	}
	if err := validateDuration(in.DurationSeconds); err != nil {
		rejectSilenceAudit(client, in, "by_strategy", "duration_hard_limit", err)
		return &Result{OK: false, Message: err.Error()}, nil
	}

	severity := classifySilenceSeverity("by_strategy", in.DurationSeconds, len(in.StrategyIDs), false)
	plan := buildStrategySilencePlan(in, severity)
	if pending, need := hitl.Require(in.Confirmed, plan); need {
		return &Result{OK: false, Message: pending.Message, Data: pending}, nil
	}

	reqBody := map[string]any{
		"bk_biz_id":        in.BKBizID,
		"strategy_ids":     in.StrategyIDs,
		"duration":         in.DurationSeconds,
		"description":      firstNonEmptySilence(in.Reason, "silenced-by-gameops-agent"),
		"matcher": map[string]any{
			"type": "strategy",
		},
	}
	return executeSilence(ctx, client, in, "by_strategy", severity, "/api/bk-monitor/prod/alert/silence/", reqBody,
		map[string]any{"strategy_ids": in.StrategyIDs, "duration": in.DurationSeconds})
}

// =============================================================================
// by_target：按 IP/Pod/集群 目标
// =============================================================================

func doSilenceByTarget(ctx context.Context, client *bkapi.Client, in AlarmSilenceInput) (*Result, error) {
	if in.BKBizID == 0 {
		return nil, fmt.Errorf("by_target 必须指定 bk_biz_id")
	}
	if len(in.Targets) == 0 {
		return nil, fmt.Errorf("by_target 必须指定 targets（至少 1 个）")
	}
	if len(in.Targets) > silenceTargetsHardLimit {
		err := fmt.Errorf("targets 数量 %d > 硬上限 %d", len(in.Targets), silenceTargetsHardLimit)
		rejectSilenceAudit(client, in, "by_target", "targets_hard_limit", err)
		return &Result{OK: false, Message: err.Error()}, nil
	}
	if err := validateDuration(in.DurationSeconds); err != nil {
		rejectSilenceAudit(client, in, "by_target", "duration_hard_limit", err)
		return &Result{OK: false, Message: err.Error()}, nil
	}

	severity := classifySilenceSeverity("by_target", in.DurationSeconds, len(in.Targets), false)
	plan := buildTargetSilencePlan(in, severity)
	if pending, need := hitl.Require(in.Confirmed, plan); need {
		return &Result{OK: false, Message: pending.Message, Data: pending}, nil
	}

	reqBody := map[string]any{
		"bk_biz_id":   in.BKBizID,
		"targets":     in.Targets,
		"duration":    in.DurationSeconds,
		"description": firstNonEmptySilence(in.Reason, "silenced-by-gameops-agent"),
		"matcher": map[string]any{
			"type":    "target",
			"targets": in.Targets,
		},
	}
	return executeSilence(ctx, client, in, "by_target", severity, "/api/bk-monitor/prod/alert/silence/", reqBody,
		map[string]any{"targets": in.Targets, "duration": in.DurationSeconds})
}

// =============================================================================
// by_dimension：最灵活也最危险 —— Critical + RequireReason
// =============================================================================

func doSilenceByDimension(ctx context.Context, client *bkapi.Client, in AlarmSilenceInput) (*Result, error) {
	if in.BKBizID == 0 {
		return nil, fmt.Errorf("by_dimension 必须指定 bk_biz_id")
	}
	if len(in.Dimensions) == 0 {
		return nil, fmt.Errorf("by_dimension 必须指定 dimensions（至少 1 个键值对）")
	}
	if err := validateDuration(in.DurationSeconds); err != nil {
		rejectSilenceAudit(client, in, "by_dimension", "duration_hard_limit", err)
		return &Result{OK: false, Message: err.Error()}, nil
	}
	// by_dimension 是最危险的 —— label selector 可能意外命中整个生产
	// 规则：confirmed 前必须带 reason，否则即便走 Plan 回传也会在 confirmed=true 路径二次拦截
	if in.Confirmed && strings.TrimSpace(in.Reason) == "" {
		return &Result{
			OK: false,
			Message: "规则拦截：by_dimension 静默影响面不可枚举（label selector 可能意外匹配生产核心业务），必须在 reason 字段提供变更原因。",
		}, nil
	}

	severity := classifySilenceSeverity("by_dimension", in.DurationSeconds, 0, false)
	plan := buildDimensionSilencePlan(in, severity)
	if pending, need := hitl.Require(in.Confirmed, plan); need {
		return &Result{OK: false, Message: pending.Message, Data: pending}, nil
	}

	reqBody := map[string]any{
		"bk_biz_id":   in.BKBizID,
		"dimensions":  in.Dimensions,
		"duration":    in.DurationSeconds,
		"description": firstNonEmptySilence(in.Reason, "silenced-by-gameops-agent"),
		"matcher": map[string]any{
			"type":       "dimension",
			"dimensions": in.Dimensions,
		},
	}
	return executeSilence(ctx, client, in, "by_dimension", severity, "/api/bk-monitor/prod/alert/silence/", reqBody,
		map[string]any{"dimensions": in.Dimensions, "duration": in.DurationSeconds})
}

// =============================================================================
// unsilence：撤销静默，Low Severity 鼓励使用
// =============================================================================

func doUnsilence(ctx context.Context, client *bkapi.Client, in AlarmSilenceInput) (*Result, error) {
	if strings.TrimSpace(in.SilenceID) == "" {
		return nil, fmt.Errorf("unsilence 必须指定 silence_id")
	}

	severity := hitl.SeverityLow // 恢复监控是反悔动作，低风险鼓励使用
	plan := buildUnsilencePlan(in, severity)
	if pending, need := hitl.Require(in.Confirmed, plan); need {
		return &Result{OK: false, Message: pending.Message, Data: pending}, nil
	}

	path := fmt.Sprintf("/api/bk-monitor/prod/alert/silence/%s/", in.SilenceID)
	var respData map[string]any
	err := client.DeleteJSON(ctx, path, nil, &respData)

	if errors.Is(err, bkapi.ErrMockMode) {
		emitSilenceAudit(client, in, "unsilence", severity, true, nil,
			map[string]any{"silence_id": in.SilenceID, "mock": true})
		return &Result{
			OK: true, Mock: true,
			Message: fmt.Sprintf("Mock 模式：silence_id=%q 已撤销（未真实下发）", in.SilenceID),
			Data:    map[string]any{"silence_id": in.SilenceID, "status": "REVOKED (mock)"},
		}, nil
	}
	if err != nil {
		emitSilenceAudit(client, in, "unsilence", severity, false, err,
			map[string]any{"silence_id": in.SilenceID})
		return nil, fmt.Errorf("unsilence 失败: %w", err)
	}
	emitSilenceAudit(client, in, "unsilence", severity, true, nil,
		map[string]any{"silence_id": in.SilenceID})
	return &Result{
		OK:   true,
		Data: map[string]any{"silence_id": in.SilenceID, "api_response": respData},
	}, nil
}

// =============================================================================
// 公共执行 / 分级 / Plan / 审计
// =============================================================================

// executeSilence 新建静默的公共执行路径（by_strategy / by_target / by_dimension 复用）。
func executeSilence(ctx context.Context, client *bkapi.Client, in AlarmSilenceInput,
	scope string, severity hitl.Severity, path string, reqBody map[string]any,
	extraAudit map[string]any) (*Result, error) {
	var respData map[string]any
	err := client.PostJSON(ctx, path, reqBody, &respData)

	// from/to 对齐 D17.6 审计语义：from=当前（创建前无 silence_id），to=创建后
	auditExtra := map[string]any{"scope": scope, "from": nil}
	for k, v := range extraAudit {
		auditExtra[k] = v
	}

	if errors.Is(err, bkapi.ErrMockMode) {
		mockID := mockSilenceID(scope, in)
		auditExtra["to"] = mockID
		emitSilenceAudit(client, in, scope, severity, true, nil, auditExtra)
		return &Result{
			OK: true, Mock: true,
			Message: fmt.Sprintf("Mock 模式：已创建 silence_id=%q（未真实下发），将在 %ds 后自动到期。",
				mockID, in.DurationSeconds),
			Data: map[string]any{
				"silence_id":       mockID,
				"scope":            scope,
				"duration_seconds": in.DurationSeconds,
				"expires_at":       time.Now().Add(time.Duration(in.DurationSeconds) * time.Second).Format(time.RFC3339),
				"status":           "CREATED (mock)",
			},
		}, nil
	}
	if err != nil {
		emitSilenceAudit(client, in, scope, severity, false, err, auditExtra)
		return nil, fmt.Errorf("创建静默失败: %w", err)
	}
	silenceID, _ := respData["silence_id"].(string)
	auditExtra["to"] = silenceID
	emitSilenceAudit(client, in, scope, severity, true, nil, auditExtra)
	return &Result{
		OK: true,
		Data: map[string]any{
			"silence_id":   silenceID,
			"scope":        scope,
			"api_response": respData,
		},
	}, nil
}

// validateDuration 校验静默时窗。
// 0 或负数：必填项缺失；>24h：架构级硬拒。
func validateDuration(sec int) error {
	if sec <= 0 {
		return fmt.Errorf("duration_seconds 必填且必须 > 0")
	}
	if sec > silenceHardMaxSeconds {
		return fmt.Errorf(
			"duration_seconds=%d 超过硬上限 %d（24h），请走 OA 审批。长期静默会造成监控黑洞",
			sec, silenceHardMaxSeconds,
		)
	}
	return nil
}

// classifySilenceSeverity 按 scope / 时长 / 批量 决定 Severity。
//
// 决策原则：
//   - by_strategy 最精准，Severity 最低
//   - by_target 受批量规模影响
//   - by_dimension 始终 Critical（label selector 不可预知影响面）
//   - 时长 >1h 一律至少 Medium
func classifySilenceSeverity(scope string, durationSec, batchSize int, _ bool) hitl.Severity {
	switch scope {
	case "by_strategy":
		if durationSec <= 3600 {
			return hitl.SeverityLow
		}
		return hitl.SeverityMedium
	case "by_target":
		if batchSize >= silenceTargetsSoftLimit {
			return hitl.SeverityHigh
		}
		if durationSec <= 3600 {
			return hitl.SeverityMedium
		}
		return hitl.SeverityHigh
	case "by_dimension":
		return hitl.SeverityCritical // label selector 永远是 Critical
	}
	return hitl.SeverityMedium
}

// buildStrategySilencePlan 构造 by_strategy 的 HITL Plan。
func buildStrategySilencePlan(in AlarmSilenceInput, severity hitl.Severity) hitl.Plan {
	return hitl.Plan{
		Action:   "bk.alarm.silence",
		Severity: severity,
		Target:   fmt.Sprintf("biz=%d / strategies=%v", in.BKBizID, in.StrategyIDs),
		SideEffect: fmt.Sprintf(
			"静默 %d 个告警策略共 %s（%ds）。期间这些策略触发的告警不会通知 on-call。",
			len(in.StrategyIDs), formatDuration(in.DurationSeconds), in.DurationSeconds,
		),
		ImpactScope:  fmt.Sprintf("业务 %d：仅 strategy_ids=%v 精确命中的告警被静默，影响面最小。", in.BKBizID, in.StrategyIDs),
		RollbackPlan: "可用 scope=unsilence + silence_id 即时恢复（低风险鼓励使用）；或静待到期后自动恢复。",
		Params: map[string]any{
			"scope":            "by_strategy",
			"bk_biz_id":        in.BKBizID,
			"strategy_ids":     in.StrategyIDs,
			"duration_seconds": in.DurationSeconds,
		},
	}
}

// buildTargetSilencePlan 构造 by_target 的 HITL Plan。
func buildTargetSilencePlan(in AlarmSilenceInput, severity hitl.Severity) hitl.Plan {
	side := fmt.Sprintf(
		"静默 %d 个目标对象共 %s（%ds）的所有告警。",
		len(in.Targets), formatDuration(in.DurationSeconds), in.DurationSeconds,
	)
	if len(in.Targets) >= silenceTargetsSoftLimit {
		side += fmt.Sprintf(" 批量 ≥%d 属于高风险，请仔细核对目标清单。", silenceTargetsSoftLimit)
	}
	return hitl.Plan{
		Action:       "bk.alarm.silence",
		Severity:     severity,
		Target:       fmt.Sprintf("biz=%d / targets(%d)=%v", in.BKBizID, len(in.Targets), truncateTargets(in.Targets, 3)),
		SideEffect:   side,
		ImpactScope:  fmt.Sprintf("业务 %d：这 %d 个目标的全部告警策略都将静默。", in.BKBizID, len(in.Targets)),
		RollbackPlan: "可用 scope=unsilence + silence_id 即时恢复；或静待到期。",
		Params: map[string]any{
			"scope":            "by_target",
			"bk_biz_id":        in.BKBizID,
			"targets":          in.Targets,
			"duration_seconds": in.DurationSeconds,
		},
	}
}

// buildDimensionSilencePlan 构造 by_dimension 的 HITL Plan（必 RequireReason）。
func buildDimensionSilencePlan(in AlarmSilenceInput, severity hitl.Severity) hitl.Plan {
	return hitl.Plan{
		Action:   "bk.alarm.silence",
		Severity: severity,
		Target:   fmt.Sprintf("biz=%d / dimensions=%v", in.BKBizID, in.Dimensions),
		SideEffect: fmt.Sprintf(
			"按 label selector %v 静默全部匹配的告警，时长 %s（%ds）。"+
				"⚠ 该模式影响面不可枚举，可能意外命中生产核心业务。",
			in.Dimensions, formatDuration(in.DurationSeconds), in.DurationSeconds,
		),
		ImpactScope:  fmt.Sprintf("业务 %d：所有标签匹配 %v 的告警都将静默；请在 reason 说明理由。", in.BKBizID, in.Dimensions),
		RollbackPlan: "可用 scope=unsilence + silence_id 即时恢复；或静待到期。一旦发现过度匹配请立即撤销。",
		Params: map[string]any{
			"scope":            "by_dimension",
			"bk_biz_id":        in.BKBizID,
			"dimensions":       in.Dimensions,
			"duration_seconds": in.DurationSeconds,
		},
		RequireReason: true, // by_dimension 永远需要理由
	}
}

// buildUnsilencePlan 构造 unsilence 的 HITL Plan（Low + 简短）。
func buildUnsilencePlan(in AlarmSilenceInput, severity hitl.Severity) hitl.Plan {
	return hitl.Plan{
		Action:       "bk.alarm.unsilence",
		Severity:     severity,
		Target:       fmt.Sprintf("silence_id=%s", in.SilenceID),
		SideEffect:   fmt.Sprintf("撤销 silence_id=%q 的静默，立即恢复相关告警通知。", in.SilenceID),
		ImpactScope:  "仅影响此条静默记录，不涉及业务状态。",
		RollbackPlan: "若误撤销，可用 scope=by_strategy/by_target/by_dimension 重新创建静默。",
		Params: map[string]any{
			"scope":      "unsilence",
			"silence_id": in.SilenceID,
		},
	}
}

// emitSilenceAudit 统一审计入账。
func emitSilenceAudit(client *bkapi.Client, in AlarmSilenceInput, scope string,
	severity hitl.Severity, ok bool, err error, extra map[string]any) {
	params := map[string]any{
		"scope":     scope,
		"bk_biz_id": in.BKBizID,
	}
	if in.DurationSeconds > 0 {
		params["duration_seconds"] = in.DurationSeconds
	}
	if in.Reason != "" {
		params["reason"] = in.Reason
	}
	for k, v := range extra {
		params[k] = v
	}
	action := "bk.alarm.silence"
	if scope == "unsilence" {
		action = "bk.alarm.unsilence"
	}
	audit.Emit(audit.Event{
		Agent:    "repair_agent",
		Action:   action,
		Severity: string(severity),
		Target:   fmt.Sprintf("biz=%d / scope=%s", in.BKBizID, scope),
		Params:   params,
		Success:  ok,
		Err:      err,
		Mock:     client.IsMock(),
	})
}

// rejectSilenceAudit 记录在正式下发前就被 Guard 拒绝的事件。
func rejectSilenceAudit(client *bkapi.Client, in AlarmSilenceInput, scope, reason string, err error) {
	audit.Emit(audit.Event{
		Agent:    "repair_agent",
		Action:   "bk.alarm.silence",
		Severity: string(hitl.SeverityCritical),
		Target:   fmt.Sprintf("biz=%d / scope=%s", in.BKBizID, scope),
		Params: map[string]any{
			"scope":            scope,
			"rejected_by":      reason,
			"duration_seconds": in.DurationSeconds,
		},
		Success: false,
		Err:     err,
		Mock:    client.IsMock(),
	})
}

// =============================================================================
// 辅助
// =============================================================================

// mockSilenceID 为 Mock 模式生成可读的 silence_id。
func mockSilenceID(scope string, in AlarmSilenceInput) string {
	return fmt.Sprintf("MOCK-SILENCE-%s-%d-%d", strings.ToUpper(scope), in.BKBizID, time.Now().Unix())
}

// formatDuration 人类可读时长（"30m" / "2h15m"）。
//
// 实现细节：直接用 d.String() 会得到形如 "2h0m0s"——尾部的零段需要拆掉，
// 但**不能用 TrimSuffix("0m")**，否则 "30m" 也会被误砍成 "3"。
// 所以这里用按段拆解：先按时间段累加，再丢掉零段。
func formatDuration(sec int) string {
	if sec <= 0 {
		return "0s"
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	var b strings.Builder
	if h > 0 {
		fmt.Fprintf(&b, "%dh", h)
	}
	if m > 0 {
		fmt.Fprintf(&b, "%dm", m)
	}
	if s > 0 {
		fmt.Fprintf(&b, "%ds", s)
	}
	if b.Len() == 0 {
		return "0s"
	}
	return b.String()
}

// truncateTargets 截断目标列表用于 Plan.Target 的精简展示。
func truncateTargets(targets []string, maxShow int) []string {
	if len(targets) <= maxShow {
		return targets
	}
	out := make([]string, 0, maxShow+1)
	out = append(out, targets[:maxShow]...)
	out = append(out, fmt.Sprintf("...(+%d more)", len(targets)-maxShow))
	return out
}

// firstNonEmptySilence 返回首个非空字符串（本包内使用，避免和其他 helper 冲突）。
func firstNonEmptySilence(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
