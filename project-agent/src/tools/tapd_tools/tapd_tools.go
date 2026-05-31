// Package tapdtools 实现 TAPD 相关 FunctionTool，服务 RepairAgent。
//
// 当前 D6 交付 2 个工具：
//   - tapd_bug_query   查询缺陷（只读，属于 tapd-read，Diagnosis 也能用）
//   - tapd_bug_create  登记缺陷（软写；只在 TAPD 落单据，不影响生产；仍走 HITL）
//
// 为了最小破坏面：
//   - 不提供 tapd_bug_close 或 tapd_bug_update，Agent 绝不自动关闭/状态流转单据
//   - 若将来需要评论 bug，新增 tapd_bug_comment，同样走 HITL
package tapdtools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/tapdapi"
	"git.woa.com/trpc-go/gameops-agent/src/tools"
	"git.woa.com/trpc-go/gameops-agent/src/tools/hitl"
)

const (
	// TargetRead  只读 TAPD 工具分组名（查询缺陷）。
	TargetRead = "tapd-read"
	// TargetWrite 写 TAPD 工具分组名（登记/评论）。
	TargetWrite = "tapd"
)

// Result 统一返回结构。
type Result struct {
	OK      bool   `json:"ok"`
	Mock    bool   `json:"mock,omitempty"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// NewAllTargeted 返回所有 TAPD TargetedTool。
func NewAllTargeted(client *tapdapi.Client) []tools.TargetedTool {
	if client == nil {
		client = tapdapi.NewClient()
	}
	return []tools.TargetedTool{
		{Target: TargetRead, Tool: newBugQueryTool(client)},
		{Target: TargetWrite, Tool: newBugCreateTool(client)},
	}
}

// BugQueryInput tapd_bug_query 入参。
type BugQueryInput struct {
	WorkspaceID string `json:"workspace_id" description:"TAPD 工作区 ID（可选；不填则用 TAPD_WORKSPACE_ID 环境变量）"`
	Keyword     string `json:"keyword"      description:"标题或描述模糊关键字（可选）"`
	Status      string `json:"status"       description:"状态过滤（可选），如 new/in_progress/resolved"`
	Owner       string `json:"owner"        description:"处理人（可选）"`
	Limit       int    `json:"limit"        description:"返回条数上限，默认 10，最大 50"`
}

func newBugQueryTool(c *tapdapi.Client) tool.Tool {
	fn := func(_ context.Context, in BugQueryInput) (*Result, error) {
		if in.Limit <= 0 {
			in.Limit = 10
		}
		if in.Limit > 50 {
			in.Limit = 50
		}
		if !c.IsMock() {
			bugs, err := c.QueryBugs(context.Background(), tapdapi.QueryBugsInput{
				WorkspaceID: in.WorkspaceID,
				Keyword:     in.Keyword,
				Status:      in.Status,
				Owner:       in.Owner,
				Limit:       in.Limit,
			})
			if err == nil {
				items := make([]map[string]any, 0, len(bugs))
				for _, b := range bugs {
					items = append(items, map[string]any{
						"id":       b.ID,
						"title":    b.Title,
						"status":   b.Status,
						"owner":    b.Owner,
						"priority": b.Priority,
						"severity": b.Severity,
						"created":  b.Created,
					})
				}
				return &Result{OK: true, Data: map[string]any{"items": items, "total": len(items)}}, nil
			}
			if !errors.Is(err, tapdapi.ErrMockMode) {
				return &Result{OK: false, Message: fmt.Sprintf("tapd api error: %v", err)}, nil
			}
		}
		return mockBugQuery(in), nil
	}
	return function.NewFunctionTool(
		fn,
		function.WithName("tapd_bug_query"),
		function.WithDescription(
			"查询 TAPD 缺陷单（只读）。适用场景：排障时参考历史同类问题、统计某类错误是否已有单据。"),
	)
}

func mockBugQuery(in BugQueryInput) *Result {
	n := 2
	if strings.Contains(strings.ToLower(in.Keyword), "oom") {
		n = 3
	}
	items := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, map[string]any{
			"id":       10000 + i,
			"title":    fmt.Sprintf("[mock] 样例缺陷 %d (kw=%s)", i+1, in.Keyword),
			"status":   firstNonEmpty(in.Status, "in_progress"),
			"owner":    firstNonEmpty(in.Owner, "oncall-sre"),
			"priority": "high",
			"created":  time.Now().Add(-time.Duration(i+1) * 24 * time.Hour).Format(time.RFC3339),
		})
	}
	return &Result{
		OK: true, Mock: true,
		Message: "Mock 模式：返回样例缺陷",
		Data:    map[string]any{"items": items, "total": len(items)},
	}
}

// BugCreateInput tapd_bug_create 入参。
type BugCreateInput struct {
	WorkspaceID string `json:"workspace_id" description:"工作区 ID（可选；默认用环境变量）"`
	Title       string `json:"title"        description:"缺陷标题（必填）"`
	Description string `json:"description"  description:"缺陷描述，建议包含：现象/复现步骤/关联诊断/建议处理人"`
	Owner       string `json:"owner"        description:"建议处理人（工号）"`
	Priority    string `json:"priority"     description:"优先级：low / middle / high / urgent"`
	Severity    string `json:"severity"     description:"严重程度：trivial / minor / major / critical"`
	Confirmed   bool   `json:"confirmed"    description:"是否已获用户确认（尽管是软写，仍要求 HITL 以避免误触发）"`
}

func newBugCreateTool(c *tapdapi.Client) tool.Tool {
	fn := func(_ context.Context, in BugCreateInput) (*Result, error) {
		if strings.TrimSpace(in.Title) == "" {
			return nil, fmt.Errorf("title 为必填项")
		}
		plan := hitl.Plan{
			Action:      "tapd.bug.create",
			Severity:    hitl.SeverityLow,
			Target:      fmt.Sprintf("workspace=%s", firstNonEmpty(in.WorkspaceID, c.WorkspaceID)),
			SideEffect:  "在 TAPD 登记一条缺陷单，不影响生产",
			ImpactScope: "缺陷单会通知处理人，进入团队待办",
			Params: map[string]any{
				"title":    in.Title,
				"owner":    in.Owner,
				"priority": in.Priority,
				"severity": in.Severity,
			},
		}
		if pending, need := hitl.Require(in.Confirmed, plan); need {
			return &Result{OK: false, Message: pending.Message, Data: pending}, nil
		}
		var result *Result
		var apiErr error
		if !c.IsMock() {
			bug, err := c.CreateBug(context.Background(), tapdapi.CreateBugInput{
				WorkspaceID: in.WorkspaceID,
				Title:       in.Title,
				Description: in.Description,
				Owner:       in.Owner,
				Priority:    in.Priority,
				Severity:    in.Severity,
			})
			if err == nil {
				result = &Result{OK: true, Data: bug}
			} else if !errors.Is(err, tapdapi.ErrMockMode) {
				apiErr = err
				result = &Result{OK: false, Message: fmt.Sprintf("tapd api error: %v", err)}
			}
		}
		if result == nil {
			result = mockBugCreate(in)
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
		function.WithName("tapd_bug_create"),
		function.WithDescription(
			"在 TAPD 登记一条缺陷单（软写）。尽管不影响生产，仍走 HITL 两段式确认，避免误触发。"+
				"建议 description 包含：现象、复现步骤、关联的监控链接、建议处理人。"),
	)
}

func mockBugCreate(in BugCreateInput) *Result {
	id := 20000 + int(time.Now().Unix()%10000)
	return &Result{
		OK: true, Mock: true,
		Message: "Mock 模式：未真正创建 TAPD 缺陷",
		Data: map[string]any{
			"id":         id,
			"title":      in.Title,
			"owner":      firstNonEmpty(in.Owner, "oncall-sre"),
			"priority":   firstNonEmpty(in.Priority, "high"),
			"severity":   firstNonEmpty(in.Severity, "major"),
			"status":     "new",
			"created_at": time.Now().Format(time.RFC3339),
		},
	}
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
