
// 蓝鲸监控-元数据查询（bk-metadata）。
//
// 对接蓝鲸 CMDB/监控元数据：POST /api/bk-monitor/prod/metadata/query/
//
// 支持查询的元数据类型：host（主机）、module（模块）、service_instance（服务实例）、
// biz（业务）、k8s_workload（K8s 负载）。
package bktools

import (
	"context"
	"errors"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bkapi"
)

// MetadataInput bk_metadata_query 工具入参。
type MetadataInput struct {
	BKBizID  int               `json:"bk_biz_id" description:"蓝鲸业务 ID（必填）"`
	Resource string            `json:"resource"  description:"元数据类型：host / module / service_instance / biz / k8s_workload（必填）"`
	Filter   map[string]string `json:"filter"    description:"过滤条件，如 {\"bk_host_innerip\":\"1.2.3.4\"}"`
	PageSize int               `json:"page_size" description:"每页数量，默认 50"`
}

// newMetadataTool 构造 bk_metadata_query 工具。
func newMetadataTool(client *bkapi.Client) tool.Tool {
	fn := func(ctx context.Context, in MetadataInput) (*Result, error) {
		if in.BKBizID == 0 || in.Resource == "" {
			return nil, fmt.Errorf("bk_biz_id 和 resource 为必填项")
		}
		pageSize := in.PageSize
		if pageSize <= 0 {
			pageSize = 50
		}

		reqBody := map[string]any{
			"bk_biz_id": in.BKBizID,
			"resource":  in.Resource,
			"filter":    in.Filter,
			"page_size": pageSize,
		}

		var respData map[string]any
		err := client.PostJSON(ctx, "/api/bk-monitor/prod/metadata/query/", reqBody, &respData)
		if errors.Is(err, bkapi.ErrMockMode) {
			return mockMetadata(in), nil
		}
		if err != nil {
			return nil, fmt.Errorf("调用蓝鲸元数据查询失败: %w", err)
		}
		return &Result{OK: true, Data: respData}, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bk_metadata_query"),
		function.WithDescription("查询蓝鲸 CMDB/监控元数据：主机列表、模块拓扑、服务实例、K8s 负载等。适用场景：按 IP 反查所属业务/模块、列出某集群的 Pod、定位资源归属。"),
	)
}

// mockMetadata 返回预置元数据样例。
func mockMetadata(in MetadataInput) *Result {
	var items []map[string]any
	switch in.Resource {
	case "host":
		items = []map[string]any{
			{"bk_host_id": 1001, "bk_host_innerip": "10.1.1.100", "bk_os_name": "linux centos", "bk_set_name": "game-prod", "bk_module_name": "game-core"},
			{"bk_host_id": 1002, "bk_host_innerip": "10.1.1.101", "bk_os_name": "linux centos", "bk_set_name": "game-prod", "bk_module_name": "game-core"},
		}
	case "k8s_workload":
		items = []map[string]any{
			{"namespace": "letsgo", "workload": "game-core", "kind": "Deployment", "replicas": 3, "ready": 2},
		}
	case "module":
		items = []map[string]any{
			{"bk_module_id": 201, "bk_module_name": "game-core", "bk_set_name": "game-prod"},
			{"bk_module_id": 202, "bk_module_name": "game-gateway", "bk_set_name": "game-prod"},
		}
	default:
		items = []map[string]any{{"info": "mock metadata for resource=" + in.Resource}}
	}
	return &Result{
		OK:      true,
		Mock:    true,
		Message: "当前为 Mock 模式",
		Data: map[string]any{
			"bk_biz_id": in.BKBizID,
			"resource":  in.Resource,
			"total":     len(items),
			"items":     items,
		},
	}
}
