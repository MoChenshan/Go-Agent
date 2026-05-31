
// Package tools 提供 FunctionTool 的 target 分组与过滤工具。
//
// 设计思路：
//   - MCP 工具通过 mcp_servers.yaml 的 `target` 字段分组，Agent 通过 `focusedTargets` 过滤。
//   - 本地 FunctionTool 也有同样的需求（例如 bcs-helm 只给 RepairAgent），
//     因此引入 TargetedTool 这一轻量结构体，在 app 层按 target 分发。
package tools

import (
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// TargetedTool 带 target 标签的工具。
//
// Target 约定：
//   - "bk-monitor"  蓝鲸监控（诊断用）
//   - "bcs-read"    BCS 容器只读（诊断用：集群/Pod/资源）
//   - "bcs-write"   BCS 容器写操作（修复用：Helm 部署/回滚）
//   - "gongfeng"    工蜂 Git（修复用：分支/MR）
//   - "devops"      蓝盾 CI/CD（修复用：触发/查询流水线）
//   - "tapd"        TAPD（修复用：关联/更新单据）
//   - "*"           所有 Agent 都可见的通用工具（如 util_tools）
type TargetedTool struct {
	Target string
	Tool   tool.Tool
}

// FilterByTargets 从 tools 中筛选 target 命中 targets 集合的工具。
//
// targets 为空：返回空列表。
// targets 含 "*"：返回全部工具。
// 否则：精确匹配 target 字段（包括 "*" 本身也能被当前 Agent 的 focusedTargets 匹配）。
func FilterByTargets(targetedTools []TargetedTool, targets []string) []tool.Tool {
	if len(targetedTools) == 0 || len(targets) == 0 {
		return nil
	}
	wantAll := false
	set := make(map[string]struct{}, len(targets))
	for _, t := range targets {
		if t == "*" {
			wantAll = true
		}
		set[t] = struct{}{}
	}
	out := make([]tool.Tool, 0, len(targetedTools))
	for _, tt := range targetedTools {
		if wantAll {
			out = append(out, tt.Tool)
			continue
		}
		// 工具自身标记为 "*" 或 target 命中
		if tt.Target == "*" {
			out = append(out, tt.Tool)
			continue
		}
		if _, ok := set[tt.Target]; ok {
			out = append(out, tt.Tool)
		}
	}
	return out
}
