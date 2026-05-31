
package hitl

import (
	"strings"
	"testing"
)

func TestRequire_NotConfirmed_ShouldIntercept(t *testing.T) {
	// 默认环境下（未设 HITL_DISABLE），confirmed=false 必须拦截
	t.Setenv("HITL_DISABLE", "")

	plan := Plan{
		Action:   "bcs.helm.rollback",
		Severity: SeverityHigh,
		Target:   "BCS-K8S-001/letsgo/game-core",
	}
	pending, need := Require(false, plan)
	if !need {
		t.Fatalf("confirmed=false 必须触发拦截")
	}
	if pending.Status != "awaiting_confirmation" {
		t.Fatalf("status 必须为 awaiting_confirmation，实际：%s", pending.Status)
	}
	if pending.OK {
		t.Fatalf("pending.OK 应为 false（语义：未执行）")
	}
	if !strings.Contains(pending.HumanPrompt, "bcs.helm.rollback") {
		t.Fatalf("human_prompt 必须包含 action，实际：%s", pending.HumanPrompt)
	}
}

func TestRequire_Confirmed_ShouldPass(t *testing.T) {
	t.Setenv("HITL_DISABLE", "")
	plan := Plan{Action: "x", Severity: SeverityHigh, Target: "t"}
	_, need := Require(true, plan)
	if need {
		t.Fatalf("confirmed=true 不应拦截")
	}
}

func TestRequire_Disabled_ShouldBypass(t *testing.T) {
	t.Setenv("HITL_DISABLE", "1")
	plan := Plan{Action: "x", Severity: SeverityCritical, Target: "t"}
	_, need := Require(false, plan)
	if need {
		t.Fatalf("HITL_DISABLE=1 应完全绕过，即便 confirmed=false")
	}
}

func TestIsDisabled_VariousTruthy(t *testing.T) {
	cases := map[string]bool{
		"":      false,
		"0":     false,
		"false": false,
		"no":    false,
		"off":   false,
		"1":     true,
		"true":  true,
		"TRUE":  true,
		"Yes":   true,
		"on":    true,
	}
	for v, want := range cases {
		t.Setenv("HITL_DISABLE", v)
		if got := IsDisabled(); got != want {
			t.Errorf("HITL_DISABLE=%q want %v got %v", v, want, got)
		}
	}
}

func TestRenderHumanPrompt_ContainsAllFields(t *testing.T) {
	p := Plan{
		Action:       "devops.build.cancel",
		Severity:     SeverityMedium,
		Target:       "proj/pipe/build-001",
		SideEffect:   "中断构建",
		ImpactScope:  "半完成状态",
		RollbackPlan: "重新跑一次",
		Params:       map[string]any{"reason": "误触发"},
	}
	s := renderHumanPrompt(p)
	for _, kw := range []string{
		"devops.build.cancel",
		"medium",
		"proj/pipe/build-001",
		"中断构建",
		"半完成状态",
		"重新跑一次",
		"reason=",
		"确认",
	} {
		if !strings.Contains(s, kw) {
			t.Errorf("human_prompt 缺少关键字 %q。全文：\n%s", kw, s)
		}
	}
}

func TestPlan_RequireReason_HintInPrompt(t *testing.T) {
	p := Plan{Action: "x", Severity: SeverityHigh, Target: "t", RequireReason: true}
	s := renderHumanPrompt(p)
	if !strings.Contains(s, "变更原因") {
		t.Fatalf("RequireReason=true 时 human_prompt 应包含『变更原因』提示。实际：\n%s", s)
	}
}
