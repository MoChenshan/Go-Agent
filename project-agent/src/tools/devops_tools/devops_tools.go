// Package devopstools 实现蓝盾（BK-CI）相关 FunctionTool，服务 RepairAgent。
//
// 当前 D6 交付：
//   - devops_pipeline_rerun  重跑流水线（写操作，走 HITL）
//   - devops_build_cancel    取消正在运行的构建（写操作，走 HITL）
//
// 凭据：DEVOPS_TOKEN 未配置时走 Mock。
package devopstools

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
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/devopsapi"
	"git.woa.com/trpc-go/gameops-agent/src/tools"
	"git.woa.com/trpc-go/gameops-agent/src/tools/hitl"
)

// isAutoOpsAllowed 仅在 DEVOPS_ALLOW_AUTO_OPS 显式打开时才允许
// 通过 Agent 触发真实的重跑/取消操作；默认 false，保障安全闸门。
// 即便用户 confirmed=true，未开这个开关仍走 Mock 软提示。
func isAutoOpsAllowed() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DEVOPS_ALLOW_AUTO_OPS"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// Target 本包 FunctionTool 的分组名。
const Target = "devops"

// Result 统一返回结构。
type Result struct {
	OK      bool   `json:"ok"`
	Mock    bool   `json:"mock,omitempty"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// NewAllTargeted 返回所有蓝盾 TargetedTool。
func NewAllTargeted(client *devopsapi.Client) []tools.TargetedTool {
	if client == nil {
		client = devopsapi.NewClient()
	}
	return []tools.TargetedTool{
		{Target: Target, Tool: newPipelineRerunTool(client)},
		{Target: Target, Tool: newBuildCancelTool(client)},
	}
}

// PipelineRerunInput devops_pipeline_rerun 入参。
type PipelineRerunInput struct {
	ProjectID  string            `json:"project_id"  description:"蓝盾项目 ID（必填）"`
	PipelineID string            `json:"pipeline_id" description:"流水线 ID（必填）"`
	BuildID    string            `json:"build_id"    description:"重跑的基准 build ID（可选；留空则以最新一次为基准）"`
	Params     map[string]string `json:"params"      description:"构建入参（可选），key-value 形式"`
	Reason     string            `json:"reason"      description:"重跑原因，便于审计（建议填写）"`
	Confirmed  bool              `json:"confirmed"   description:"是否已获用户确认"`
}

func newPipelineRerunTool(c *devopsapi.Client) tool.Tool {
	fn := func(_ context.Context, in PipelineRerunInput) (*Result, error) {
		if in.ProjectID == "" || in.PipelineID == "" {
			return nil, fmt.Errorf("project_id 与 pipeline_id 为必填项")
		}
		plan := hitl.Plan{
			Action:       "devops.pipeline.rerun",
			Severity:     hitl.SeverityMedium,
			Target:       fmt.Sprintf("%s / %s", in.ProjectID, in.PipelineID),
			SideEffect:   "重跑一次流水线（可能再次触发构建/发布/部署动作）",
			ImpactScope:  "取决于流水线定义；若包含发布步骤会影响线上服务",
			RollbackPlan: "若结果异常，取消本次构建或回退到上一次成功版本",
			Params: map[string]any{
				"project_id":  in.ProjectID,
				"pipeline_id": in.PipelineID,
				"build_id":    in.BuildID,
				"reason":      in.Reason,
			},
		}
		if pending, need := hitl.Require(in.Confirmed, plan); need {
			return &Result{OK: false, Message: pending.Message, Data: pending}, nil
		}
		var result *Result
		var apiErr error
		if !c.IsMock() && isAutoOpsAllowed() {
			paramsCopy := in.Params
			res, err := c.PipelineStart(context.Background(), devopsapi.PipelineStartInput{
				ProjectID:  in.ProjectID,
				PipelineID: in.PipelineID,
				BuildID:    in.BuildID,
				Params:     paramsCopy,
			})
			if err == nil {
				result = &Result{OK: true, Data: res}
			} else if !errors.Is(err, devopsapi.ErrMockMode) {
				apiErr = err
				result = &Result{OK: false, Message: fmt.Sprintf("devops api error: %v", err)}
			}
		}
		if result == nil {
			result = mockRerun(in)
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
			Mock:     c.IsMock() || !isAutoOpsAllowed() || result.Mock,
		})
		return result, nil
	}
	return function.NewFunctionTool(
		fn,
		function.WithName("devops_pipeline_rerun"),
		function.WithDescription(
			"重跑蓝盾流水线（写操作）。未携带 confirmed=true 时仅返回执行计划 Plan，需原样展示给用户并获得确认后才执行。"+
				"若流水线包含发布步骤，severity 会被上层标注为高。"),
	)
}

func mockRerun(in PipelineRerunInput) *Result {
	buildNo := fmt.Sprintf("b-%s-%d", strings.ToLower(in.PipelineID), time.Now().Unix()%10000)
	return &Result{
		OK:      true,
		Mock:    true,
		Message: "Mock 模式：未真正触发流水线，仅返回样例",
		Data: map[string]any{
			"build_no":   buildNo,
			"status":     "RUNNING (mock)",
			"started_at": time.Now().Format(time.RFC3339),
			"base":       in.BuildID,
			"reason":     in.Reason,
		},
	}
}

// BuildCancelInput devops_build_cancel 入参。
type BuildCancelInput struct {
	ProjectID  string `json:"project_id"  description:"蓝盾项目 ID（必填）"`
	PipelineID string `json:"pipeline_id" description:"流水线 ID（必填）"`
	BuildID    string `json:"build_id"    description:"要取消的 build ID（必填）"`
	Reason     string `json:"reason"      description:"取消原因（必填，用于审计）"`
	Confirmed  bool   `json:"confirmed"   description:"是否已获用户确认"`
}

func newBuildCancelTool(c *devopsapi.Client) tool.Tool {
	fn := func(_ context.Context, in BuildCancelInput) (*Result, error) {
		if in.ProjectID == "" || in.PipelineID == "" || in.BuildID == "" {
			return nil, fmt.Errorf("project_id / pipeline_id / build_id 均为必填项")
		}
		if strings.TrimSpace(in.Reason) == "" {
			return nil, fmt.Errorf("取消构建必须提供 reason 字段用于审计")
		}
		plan := hitl.Plan{
			Action:        "devops.build.cancel",
			Severity:      hitl.SeverityMedium,
			Target:        fmt.Sprintf("%s / %s / %s", in.ProjectID, in.PipelineID, in.BuildID),
			SideEffect:    "中断正在运行的构建，已执行的步骤不会回滚（例如已部署的服务不会自动撤回）",
			ImpactScope:   "该 build 关联的部署步骤可能处于半完成状态，需另行确认线上效果",
			RollbackPlan:  "重新触发一次完整构建即可恢复到正常发布流程",
			Params:        map[string]any{"project_id": in.ProjectID, "pipeline_id": in.PipelineID, "build_id": in.BuildID, "reason": in.Reason},
			RequireReason: true,
		}
		if pending, need := hitl.Require(in.Confirmed, plan); need {
			return &Result{OK: false, Message: pending.Message, Data: pending}, nil
		}
		var result *Result
		var apiErr error
		if !c.IsMock() && isAutoOpsAllowed() {
			if err := c.BuildCancel(context.Background(), in.ProjectID, in.PipelineID, in.BuildID); err == nil {
				result = &Result{OK: true, Data: map[string]any{
					"build_id": in.BuildID,
					"status":   "CANCELLED",
					"reason":   in.Reason,
				}}
			} else if !errors.Is(err, devopsapi.ErrMockMode) {
				apiErr = err
				result = &Result{OK: false, Message: fmt.Sprintf("devops api error: %v", err)}
			}
		}
		if result == nil {
			result = mockCancel(in)
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
			Mock:     c.IsMock() || !isAutoOpsAllowed() || result.Mock,
		})
		return result, nil
	}
	return function.NewFunctionTool(
		fn,
		function.WithName("devops_build_cancel"),
		function.WithDescription(
			"取消蓝盾正在运行的构建。未 confirmed 时返回 Plan；必须提供 reason 用于审计留痕。"),
	)
}

func mockCancel(in BuildCancelInput) *Result {
	return &Result{
		OK:      true,
		Mock:    true,
		Message: "Mock 模式：未真正取消",
		Data:    map[string]any{"build_id": in.BuildID, "status": "CANCELLED (mock)", "reason": in.Reason},
	}
}
