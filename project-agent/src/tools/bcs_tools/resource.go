
// BCS 资源查询（bcs-resource）。
//
// 对接 BCS 资源代理：POST /bcsapi/v4/storage/k8s/dynamic/clusters/{cluster_id}/{resource}
// 支持的 resource：pod / deployment / statefulset / service / ingress / event
package bcstools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// ResourceInput bcs_resource_query 工具入参。
type ResourceInput struct {
	ClusterID    string `json:"cluster_id"    description:"集群 ID（必填），如 BCS-K8S-00001"`
	Resource     string `json:"resource"      description:"资源类型（必填）：pod / deployment / statefulset / service / ingress / event"`
	Namespace    string `json:"namespace"     description:"命名空间（可选，空则全集群）"`
	Name         string `json:"name"          description:"资源名称精确匹配（可选）"`
	LabelSelector string `json:"label_selector" description:"标签选择器，如 'app=game-core,env=prod'（可选）"`
	Limit        int    `json:"limit"         description:"返回条数上限，默认 50"`
}

// newResourceTool 构造 bcs_resource_query 工具。
func newResourceTool(client *bcsapi.Client) tool.Tool {
	fn := func(ctx context.Context, in ResourceInput) (*Result, error) {
		if in.ClusterID == "" || in.Resource == "" {
			return nil, fmt.Errorf("cluster_id 和 resource 为必填项")
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 50
		}

		path := fmt.Sprintf("/bcsapi/v4/storage/k8s/dynamic/clusters/%s/%s", in.ClusterID, in.Resource)
		reqBody := map[string]any{
			"namespace":      in.Namespace,
			"name":           in.Name,
			"label_selector": in.LabelSelector,
			"limit":          limit,
		}

		var respData map[string]any
		err := client.PostJSON(ctx, path, reqBody, &respData)
		if errors.Is(err, bcsapi.ErrMockMode) {
			return mockResource(in), nil
		}
		if err != nil {
			return nil, fmt.Errorf("查询 BCS 资源失败: %w", err)
		}
		return &Result{OK: true, Data: respData}, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bcs_resource_query"),
		function.WithDescription("查询 BCS K8s 资源（Pod/Deployment/Service/Event 等）。适用场景：查看 Pod 状态、Deployment 副本健康、最近事件列表。"),
	)
}

func mockResource(in ResourceInput) *Result {
	now := time.Now()
	var items []map[string]any
	switch in.Resource {
	case "pod":
		items = []map[string]any{
			{
				"name":      "game-core-7f9bc5d-abc12",
				"namespace": firstNonEmpty(in.Namespace, "letsgo"),
				"status":    "Running",
				"ready":     "1/1",
				"restarts":  0,
				"nodeName":  "node-10-1-1-100",
				"createdAt": now.Add(-6 * time.Hour).Format(time.RFC3339),
			},
			{
				"name":      "game-core-7f9bc5d-def34",
				"namespace": firstNonEmpty(in.Namespace, "letsgo"),
				"status":    "CrashLoopBackOff",
				"ready":     "0/1",
				"restarts":  7,
				"nodeName":  "node-10-1-1-101",
				"createdAt": now.Add(-6 * time.Hour).Format(time.RFC3339),
			},
		}
	case "deployment":
		items = []map[string]any{
			{
				"name":            "game-core",
				"namespace":       firstNonEmpty(in.Namespace, "letsgo"),
				"replicas":        3,
				"readyReplicas":   2,
				"updatedReplicas": 3,
				"image":           "game-core:v1.2.3",
			},
		}
	case "event":
		items = []map[string]any{
			{
				"type":    "Warning",
				"reason":  "BackOff",
				"object":  "Pod/game-core-7f9bc5d-def34",
				"message": "Back-off restarting failed container (mock)",
				"time":    now.Add(-5 * time.Minute).Format(time.RFC3339),
			},
		}
	default:
		items = []map[string]any{{"info": "mock resource for type=" + in.Resource}}
	}

	return &Result{
		OK:      true,
		Mock:    true,
		Message: "当前为 Mock 模式",
		Data: map[string]any{
			"cluster_id": in.ClusterID,
			"resource":   in.Resource,
			"total":      len(items),
			"items":      items,
		},
	}
}
