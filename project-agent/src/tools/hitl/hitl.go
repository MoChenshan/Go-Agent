
// Package hitl 提供统一的 Human-in-the-Loop 确认机制，服务于所有写操作工具。
//
// 设计背景
//   运维 Agent 最大的风险点是 LLM 在没有人类参与的情况下直接执行高危写操作
//  （Helm rollback / 合并 MR / 重跑发布流水线 / 删分支 / 改集群状态...）。
//   Anthropic、OpenAI、Google 的工具调用最佳实践都明确强调这类"破坏性操作"
//   必须强制 Human-in-the-Loop。
//
// 本包实现的模式：两段式确认（plan → confirm → execute）
//
//	1) 未 confirmed → 工具返回 PendingPlan（包含 action/side_effect/impact_scope/
//	   rollback_plan 等），不触达真实 API；prompt 约束 LLM 必须原样向用户展示计划
//	2) 用户回复"确认"/"同意"/"yes" 等 → LLM 带 confirmed=true 重新调用工具
//	3) 工具此时才真正下发请求
//
// 软开关：环境变量 HITL_DISABLE=1 时，所有写操作自动绕过（仅供 CI / 集成测试使用）。
//
// 预期所有写操作工具都复用本包的 Plan / Execute 工厂，避免样板代码散落在各处。
package hitl

import (
	"fmt"
	"os"
	"strings"
)

// Severity 写操作破坏等级。
type Severity string

const (
	// SeverityCritical 不可逆 / 影响生产 / 影响面大。例如：helm uninstall / 回滚生产 release / force push
	SeverityCritical Severity = "critical"
	// SeverityHigh 有副作用但可回滚 / 影响单集群或单服务。例如：helm rollback 到历史版本 / 合并 MR
	SeverityHigh Severity = "high"
	// SeverityMedium 轻度副作用 / 易回滚。例如：重跑流水线 / 取消 Job
	SeverityMedium Severity = "medium"
	// SeverityLow 软写 / 仅登记性质。例如：创建 TAPD 缺陷单 / 评论 MR
	SeverityLow Severity = "low"
)

// Plan 向用户展示的执行计划，核心字段都在一处显式列出，避免散落到 description 里。
type Plan struct {
	Action        string         `json:"action"`                    // 将要执行的动作（如 "bcs.helm.rollback"）
	Severity      Severity       `json:"severity"`                  // 破坏等级
	Target        string         `json:"target"`                    // 作用对象（如 "BCS-K8S-001/letsgo/game-core"）
	SideEffect    string         `json:"side_effect"`               // 副作用一句话描述
	ImpactScope   string         `json:"impact_scope,omitempty"`    // 影响范围（如 "生产环境 3 副本将滚动重启"）
	RollbackPlan  string         `json:"rollback_plan,omitempty"`   // 如果出错如何回滚
	Params        map[string]any `json:"params,omitempty"`          // 本次调用的关键入参（去敏后）
	RequireReason bool           `json:"require_reason,omitempty"`  // 是否要求用户在确认时给出文字原因
}

// PendingResult 是 HITL 第一阶段（未确认）时工具必须返回的结构。
// 工具必须把它作为 Data 返回给 LLM，LLM 负责把 human_prompt 原样展示给用户。
type PendingResult struct {
	OK          bool   `json:"ok"`          // 固定为 false（语义：未执行）
	Status      string `json:"status"`      // 固定为 "awaiting_confirmation"
	Message     string `json:"message"`     // 给 LLM 的指令（描述"为什么不执行"）
	HumanPrompt string `json:"human_prompt"` // 建议展示给用户的原文（含三段式模板）
	Plan        Plan   `json:"plan"`
}

// BuildPending 构造 PendingResult。工具调用方：写操作入口发现 confirmed=false 时调用。
func BuildPending(p Plan) PendingResult {
	return PendingResult{
		OK:          false,
		Status:      "awaiting_confirmation",
		Message:     fmt.Sprintf("写操作 %q 需要人工确认。请【原样】向用户展示 human_prompt，等用户明确回复『确认』后，再以 confirmed=true 重新调用本工具。", p.Action),
		HumanPrompt: renderHumanPrompt(p),
		Plan:        p,
	}
}

// IsDisabled HITL 是否被软关闭（仅 HITL_DISABLE=1/true/yes/on 时成立）。
func IsDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("HITL_DISABLE"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// Require 判定是否需要拦截。
//   - 已 confirmed → 不拦截
//   - HITL_DISABLE=1 → 不拦截（仅测试）
//   - 其他 → 返回 PendingResult 提示上层 return
func Require(confirmed bool, p Plan) (PendingResult, bool) {
	if confirmed || IsDisabled() {
		return PendingResult{}, false
	}
	return BuildPending(p), true
}

// renderHumanPrompt 生成一个人类可读的三段式确认文案，让 LLM 原样展示给用户。
// 采用 Markdown 轻格式，便于前端渲染 & 终端显示都 OK。
func renderHumanPrompt(p Plan) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("⚠ **即将执行写操作：%s**（严重级别：%s）\n\n", p.Action, p.Severity))
	sb.WriteString(fmt.Sprintf("• **作用对象**：%s\n", p.Target))
	if p.SideEffect != "" {
		sb.WriteString(fmt.Sprintf("• **副作用**：%s\n", p.SideEffect))
	}
	if p.ImpactScope != "" {
		sb.WriteString(fmt.Sprintf("• **影响范围**：%s\n", p.ImpactScope))
	}
	if p.RollbackPlan != "" {
		sb.WriteString(fmt.Sprintf("• **回滚预案**：%s\n", p.RollbackPlan))
	}
	if len(p.Params) > 0 {
		sb.WriteString("• **关键参数**：")
		first := true
		for k, v := range p.Params {
			if !first {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s=%v", k, v))
			first = false
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n请回复『**确认**』以继续；或提供不同参数重新发起。")
	if p.RequireReason {
		sb.WriteString("\n（请同时简述本次变更原因，便于审计留痕。）")
	}
	return sb.String()
}
