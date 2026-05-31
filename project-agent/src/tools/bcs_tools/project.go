
// BCS 项目查询（bcs-project）。
//
// 对接 BCS：GET /bcsapi/v4/bcsproject/v1/projects
package bcstools

import (
	"context"
	"errors"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// ProjectInput bcs_project_query 工具入参。
type ProjectInput struct {
	ProjectCode string `json:"project_code" description:"BCS 项目 Code（可选，空则列出全部）"`
	Name        string `json:"name"         description:"项目名模糊匹配"`
	PageSize    int    `json:"page_size"    description:"每页数量，默认 20"`
}

// newProjectTool 构造 bcs_project_query 工具。
func newProjectTool(client *bcsapi.Client) tool.Tool {
	fn := func(ctx context.Context, in ProjectInput) (*Result, error) {
		pageSize := in.PageSize
		if pageSize <= 0 {
			pageSize = 20
		}
		query := map[string]string{
			"limit": itoa(pageSize),
		}
		if in.ProjectCode != "" {
			query["projectCode"] = in.ProjectCode
		}
		if in.Name != "" {
			query["searchName"] = in.Name
		}

		var respData map[string]any
		err := client.Get(ctx, "/bcsapi/v4/bcsproject/v1/projects", query, &respData)
		if errors.Is(err, bcsapi.ErrMockMode) {
			return mockProject(in), nil
		}
		if err != nil {
			return nil, fmt.Errorf("查询 BCS 项目失败: %w", err)
		}
		return &Result{OK: true, Data: respData}, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bcs_project_query"),
		function.WithDescription("查询 BCS 项目列表（按 projectCode 或名称）。适用场景：定位某业务在 BCS 中对应的 project。"),
	)
}

func mockProject(in ProjectInput) *Result {
	projects := []map[string]any{
		{"projectID": "proj-letsgo-001", "projectCode": "letsgo", "name": "LetsGo 游戏", "businessID": "100"},
	}
	if in.ProjectCode != "" && in.ProjectCode != "letsgo" {
		projects = []map[string]any{}
	}
	return &Result{
		OK:      true,
		Mock:    true,
		Message: "当前为 Mock 模式",
		Data:    map[string]any{"total": len(projects), "projects": projects},
	}
}

// itoa 简单 int->string，避免引入 strconv 在头部（保持文件风格一致）。
func itoa(n int) string { return fmt.Sprintf("%d", n) }
