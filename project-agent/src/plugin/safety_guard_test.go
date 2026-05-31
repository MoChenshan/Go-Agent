package plugin

import (
	"context"
	"encoding/json"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func newArgs(name, raw string) *tool.BeforeToolArgs {
	return &tool.BeforeToolArgs{
		ToolName:  name,
		Arguments: []byte(raw),
	}
}

// TestSafetyGuard_DefaultPasses 正常工具调用不应被拦截。
func TestSafetyGuard_DefaultPasses(t *testing.T) {
	g := NewSafetyGuard(SafetyConfig{})
	ret, err := g.before(context.Background(), newArgs("bk_alarm_query",
		`{"cluster":"bcs-1","period":"1h"}`))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ret != nil {
		t.Fatalf("正常调用不应被拦截，实际 ret=%+v", ret)
	}
}

// TestSafetyGuard_BlocksForcePush 触发 force_push=true 必须拦截。
func TestSafetyGuard_BlocksForcePush(t *testing.T) {
	var logs []string
	g := NewSafetyGuard(SafetyConfig{
		Logger: func(tool, rule, reason string) {
			logs = append(logs, tool+"/"+rule)
		},
	})
	ret, err := g.before(context.Background(), newArgs("gongfeng_push",
		`{"project_id":"a/b","branch":"fix/x","force_push":true}`))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ret == nil || ret.CustomResult == nil {
		t.Fatalf("force push 必须被拦截，ret=%+v", ret)
	}
	m, ok := ret.CustomResult.(map[string]any)
	if !ok || !m["blocked"].(bool) || m["rule"] != "block_force_push" {
		t.Fatalf("拦截返回体结构不对：%+v", ret.CustomResult)
	}
	if len(logs) != 1 {
		t.Fatalf("Logger 应被调用 1 次，实际 %d", len(logs))
	}
}

// TestSafetyGuard_BlocksMergeToMain target_branch=main 的合并 MR 必须拦截。
func TestSafetyGuard_BlocksMergeToMain(t *testing.T) {
	g := NewSafetyGuard(SafetyConfig{})
	for _, branch := range []string{"main", "master", "MASTER", " Main "} {
		raw, _ := json.Marshal(map[string]any{
			"project_id":    "a/b",
			"iid":           123,
			"target_branch": branch,
		})
		ret, _ := g.before(context.Background(),
			newArgs("gongfeng_mr_merge", string(raw)))
		if ret == nil {
			t.Fatalf("branch=%q 应拦截", branch)
		}
	}
	// 合并到非主干分支不应拦截。
	ret, _ := g.before(context.Background(),
		newArgs("gongfeng_mr_merge",
			`{"project_id":"a/b","iid":1,"target_branch":"develop"}`))
	if ret != nil {
		t.Fatalf("合并到 develop 不应拦截：%+v", ret)
	}
}

// TestSafetyGuard_HelmUninstallNeedsReason uninstall 无 reason 拦截，有则放行。
func TestSafetyGuard_HelmUninstallNeedsReason(t *testing.T) {
	g := NewSafetyGuard(SafetyConfig{})

	ret, _ := g.before(context.Background(), newArgs("bcs_helm_manage",
		`{"action":"uninstall","release":"game-core"}`))
	if ret == nil {
		t.Fatalf("uninstall 无 reason 必须拦截")
	}

	ret, _ = g.before(context.Background(), newArgs("bcs_helm_manage",
		`{"action":"uninstall","release":"game-core","reason":"迁移到新集群"}`))
	if ret != nil {
		t.Fatalf("uninstall 附 reason 不应拦截：%+v", ret)
	}

	// 其他 action 不受影响。
	ret, _ = g.before(context.Background(), newArgs("bcs_helm_manage",
		`{"action":"upgrade","release":"game-core"}`))
	if ret != nil {
		t.Fatalf("upgrade 不应受 uninstall 规则影响：%+v", ret)
	}
}

// TestSafetyGuard_PipelineRerunEmptyID 空 pipeline_id 必须拦截。
func TestSafetyGuard_PipelineRerunEmptyID(t *testing.T) {
	g := NewSafetyGuard(SafetyConfig{})
	ret, _ := g.before(context.Background(), newArgs("devops_pipeline_rerun",
		`{"pipeline_id":""}`))
	if ret == nil {
		t.Fatalf("空 pipeline_id 必须拦截")
	}
	ret, _ = g.before(context.Background(), newArgs("devops_pipeline_rerun",
		`{"pipeline_id":"P-123"}`))
	if ret != nil {
		t.Fatalf("非空 pipeline_id 不应拦截：%+v", ret)
	}
}

// TestSafetyGuard_CustomRule 自定义规则可覆盖默认策略。
func TestSafetyGuard_CustomRule(t *testing.T) {
	g := NewSafetyGuard(SafetyConfig{
		Rules: []SafetyRule{
			{
				Name:     "block_all_foo",
				ToolName: "foo",
				Match:    func(_ []byte, _ map[string]any) bool { return true },
				Reason:   "foo 被禁止",
			},
		},
	})
	ret, _ := g.before(context.Background(), newArgs("foo", `{}`))
	if ret == nil {
		t.Fatalf("自定义规则未生效")
	}
	// 默认的 force_push 规则在此 config 下应被覆盖（不生效）。
	ret, _ = g.before(context.Background(), newArgs("bar",
		`{"force_push":true}`))
	if ret != nil {
		t.Fatalf("自定义规则应替换默认规则，force_push 不应再被拦截：%+v", ret)
	}
}
