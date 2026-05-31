// Package gongfengtools 实现工蜂 Git 相关的 FunctionTool，服务 RepairAgent。
//
// 目前提供 2 个写工具（全部走 HITL 两段式确认）：
//   - gongfeng_mr_create  创建 Merge Request（高危：会进入代码评审流，不会直接合并）
//   - gongfeng_mr_merge   合并 Merge Request（CRITICAL：直接改 master/main 分支代码）
//
// 未来（D8+）补齐：
//   - gongfeng_branch_create / gongfeng_commit_create（提交修复代码）
//   - gongfeng_pipeline_query（查询工蜂集成的流水线）
//
// 凭据：未配置 GONGFENG_TOKEN 时走 Mock，返回预置 MR 对象。
package gongfengtools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/gongfengapi"
	"git.woa.com/trpc-go/gameops-agent/src/tools"
	"git.woa.com/trpc-go/gameops-agent/src/tools/hitl"
)

// isAutoMergeAllowed 仅在 GONGFENG_ALLOW_AUTO_MERGE 被显式打开时才允许
// 通过 Agent 触发真实合并；默认返回 false，保证安全闸门。
func isAutoMergeAllowed() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GONGFENG_ALLOW_AUTO_MERGE"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// Target 本包 FunctionTool 的分组名。
const Target = "gongfeng"

// Result 统一返回结构。
type Result struct {
	OK      bool   `json:"ok"`
	Mock    bool   `json:"mock,omitempty"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// NewAllTargeted 返回所有工蜂相关 TargetedTool。
func NewAllTargeted(client *gongfengapi.Client) []tools.TargetedTool {
	if client == nil {
		client = gongfengapi.NewClient()
	}
	return []tools.TargetedTool{
		{Target: Target, Tool: newMRCreateTool(client)},
		{Target: Target, Tool: newMRMergeTool(client)},
	}
}

// -----------------------------------------------------------------------------
// gongfeng_mr_create
// -----------------------------------------------------------------------------

// MRCreateInput gongfeng_mr_create 入参。
type MRCreateInput struct {
	ProjectID    string `json:"project_id"    description:"工蜂项目 ID 或 namespace/repo 形式（必填），如 123 或 video/game-core"`
	SourceBranch string `json:"source_branch" description:"源分支（必填），如 fix/oom-cache-20260420"`
	TargetBranch string `json:"target_branch" description:"目标分支（必填），通常为 master / main / release/*"`
	Title        string `json:"title"         description:"MR 标题（必填）"`
	Description  string `json:"description"   description:"MR 描述：修复内容、验证方式、回滚预案"`
	Reviewers    string `json:"reviewers"     description:"评审人列表（逗号分隔，可选）"`
	Confirmed    bool   `json:"confirmed"     description:"是否已获用户确认；未确认时仅返回 Plan，不调用真实 API"`
}

func newMRCreateTool(c *gongfengapi.Client) tool.Tool {
	fn := func(_ context.Context, in MRCreateInput) (*Result, error) {
		if in.ProjectID == "" || in.SourceBranch == "" || in.TargetBranch == "" || in.Title == "" {
			return nil, fmt.Errorf("project_id / source_branch / target_branch / title 均为必填项")
		}
		plan := hitl.Plan{
			Action:       "gongfeng.mr.create",
			Severity:     hitl.SeverityMedium,
			Target:       fmt.Sprintf("%s  %s → %s", in.ProjectID, in.SourceBranch, in.TargetBranch),
			SideEffect:   "创建 Merge Request（不会自动合并，进入人工评审流）",
			ImpactScope:  fmt.Sprintf("目标分支 %s 下即将收到新的 MR，评审人将收到通知", in.TargetBranch),
			RollbackPlan: "若创建有误，可在工蜂前端关闭该 MR，不影响代码",
			Params: map[string]any{
				"project_id":    in.ProjectID,
				"source_branch": in.SourceBranch,
				"target_branch": in.TargetBranch,
				"title":         in.Title,
				"reviewers":     in.Reviewers,
			},
		}
		if pending, need := hitl.Require(in.Confirmed, plan); need {
			return &Result{OK: false, Message: pending.Message, Data: pending}, nil
		}

		// 真实调用（D8）；Mock 模式下走兜底
		var result *Result
		var apiErr error
		if !c.IsMock() {
			mr, err := c.CreateMR(context.Background(), gongfengapi.CreateMRInput{
				ProjectID:    in.ProjectID,
				SourceBranch: in.SourceBranch,
				TargetBranch: in.TargetBranch,
				Title:        in.Title,
				Description:  in.Description,
				Reviewers:    splitList(in.Reviewers),
			})
			if err == nil {
				result = &Result{OK: true, Data: mr}
			} else if !errors.Is(err, gongfengapi.ErrMockMode) {
				apiErr = err
				result = &Result{OK: false, Message: fmt.Sprintf("gongfeng api error: %v", err)}
			}
		}
		if result == nil {
			result = mockMRCreate(in)
		}
		audit.Emit(audit.Event{
			Agent:    "repair_agent",
			Action:   plan.Action,
			Severity: string(plan.Severity),
			Target:   plan.Target,
			Params:   plan.Params,
			Success:  result.OK,
			Err:      apiErr,
			Mock:     c.IsMock() || result.Mock,
		})
		return result, nil
	}
	return function.NewFunctionTool(
		fn,
		function.WithName("gongfeng_mr_create"),
		function.WithDescription(
			"创建工蜂 Merge Request。此工具不会合并代码，仅发起评审请求；"+
				"即便如此仍是写操作，必须先让用户确认（未带 confirmed=true 时会返回结构化 Plan，"+
				"请原样展示给用户并等待其『确认』）。建议 description 字段写清：修复问题、验证方式、回滚预案。"),
	)
}

func mockMRCreate(in MRCreateInput) *Result {
	iid := 42 + int(time.Now().Unix()%100)
	return &Result{
		OK:      true,
		Mock:    true,
		Message: "Mock 模式：未真正调用工蜂 API，仅返回预置样例",
		Data: map[string]any{
			"mr_iid":        iid,
			"web_url":       fmt.Sprintf("https://git.woa.com/%s/merge_requests/%d", in.ProjectID, iid),
			"title":         in.Title,
			"source_branch": in.SourceBranch,
			"target_branch": in.TargetBranch,
			"state":         "opened",
			"reviewers":     splitList(in.Reviewers),
			"created_at":    time.Now().Format(time.RFC3339),
		},
	}
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// gongfeng_mr_merge
// -----------------------------------------------------------------------------

// MRMergeInput gongfeng_mr_merge 入参。
type MRMergeInput struct {
	ProjectID string `json:"project_id"  description:"工蜂项目 ID（必填）"`
	MRIid     int    `json:"mr_iid"      description:"MR 编号（必填）"`
	Reason    string `json:"reason"      description:"合并原因（必填，用于审计）"`
	Confirmed bool   `json:"confirmed"   description:"是否已获用户确认"`
}

func newMRMergeTool(c *gongfengapi.Client) tool.Tool {
	fn := func(_ context.Context, in MRMergeInput) (*Result, error) {
		if in.ProjectID == "" || in.MRIid <= 0 {
			return nil, fmt.Errorf("project_id 与 mr_iid 均为必填项")
		}
		if strings.TrimSpace(in.Reason) == "" {
			return nil, fmt.Errorf("合并 MR 必须提供 reason 字段用于审计")
		}
		plan := hitl.Plan{
			Action:        "gongfeng.mr.merge",
			Severity:      hitl.SeverityCritical,
			Target:        fmt.Sprintf("%s  MR!%d", in.ProjectID, in.MRIid),
			SideEffect:    "直接将 MR 合并进目标分支，可能触发下游流水线",
			ImpactScope:   "代码仓库（长期留存 git 历史）+ 关联发布流水线",
			RollbackPlan:  "合并后需另提 revert MR 才能回退",
			Params:        map[string]any{"project_id": in.ProjectID, "mr_iid": in.MRIid, "reason": in.Reason},
			RequireReason: true,
		}
		if pending, need := hitl.Require(in.Confirmed, plan); need {
			return &Result{OK: false, Message: pending.Message, Data: pending}, nil
		}

		// 团队政策：合并 MR 属于最高危动作，默认依旧不真实下发，仅输出软提示。
		// 仅在显式打开 GONGFENG_ALLOW_AUTO_MERGE=1 时才会调用真实 API（留足安全闸门）。
		var result *Result
		var apiErr error
		if !c.IsMock() && isAutoMergeAllowed() {
			mr, err := c.MergeMR(context.Background(), gongfengapi.MergeMRInput{
				ProjectID:    in.ProjectID,
				MRIid:        in.MRIid,
				MergeMessage: in.Reason,
			})
			if err == nil {
				result = &Result{OK: true, Data: mr}
			} else if !errors.Is(err, gongfengapi.ErrMockMode) {
				apiErr = err
				result = &Result{OK: false, Message: fmt.Sprintf("gongfeng api error: %v", err)}
			}
		}
		if result == nil {
			result = mockMRMerge(in)
		}
		audit.Emit(audit.Event{
			Agent:    "repair_agent",
			Action:   plan.Action,
			Severity: string(plan.Severity),
			Target:   plan.Target,
			Params:   plan.Params,
			Reason:   in.Reason,
			Success:  result.OK,
			Err:      apiErr,
			Mock:     c.IsMock() || !isAutoMergeAllowed() || result.Mock,
		})
		return result, nil
	}
	return function.NewFunctionTool(
		fn,
		function.WithName("gongfeng_mr_merge"),
		function.WithDescription(
			"合并工蜂 Merge Request（最高危险级别）。"+
				"必须先让用户明确确认，且要求携带 reason 字段用于审计。"+
				"团队政策：即便此工具存在，也应优先引导用户在工蜂页面手动合并，而非 Agent 自动合并。"),
	)
}

func mockMRMerge(in MRMergeInput) *Result {
	return &Result{
		OK:      true,
		Mock:    true,
		Message: "Mock 模式：未真正合并，仅返回样例；团队政策要求优先引导用户手动合并",
		Data: map[string]any{
			"mr_iid":    in.MRIid,
			"state":     "merged (mock)",
			"merged_at": time.Now().Format(time.RFC3339),
			"reason":    in.Reason,
		},
	}
}
