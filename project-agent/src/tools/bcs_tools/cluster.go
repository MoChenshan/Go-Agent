
// BCS 集群查询（bcs-cluster）。
//
// 对接 BCS：GET /bcsapi/v4/clustermanager/v1/cluster
package bcstools

import (
	"context"
	"errors"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// ClusterInput bcs_cluster_query 工具入参。
type ClusterInput struct {
	ProjectID string `json:"project_id" description:"BCS 项目 ID（必填）"`
	ClusterID string `json:"cluster_id" description:"集群 ID（可选，空则列出项目下全部集群）"`
	Status    string `json:"status"     description:"状态过滤：RUNNING / CREATING / DELETING"`
	Env       string `json:"env"        description:"环境过滤：prod / test / debug"`
}

// newClusterTool 构造 bcs_cluster_query 工具。
func newClusterTool(client *bcsapi.Client) tool.Tool {
	fn := func(ctx context.Context, in ClusterInput) (*Result, error) {
		if in.ProjectID == "" {
			return nil, fmt.Errorf("project_id 为必填项")
		}
		query := map[string]string{"projectID": in.ProjectID}
		if in.ClusterID != "" {
			query["clusterID"] = in.ClusterID
		}
		if in.Status != "" {
			query["status"] = in.Status
		}
		if in.Env != "" {
			query["environment"] = in.Env
		}

		var respData map[string]any
		err := client.Get(ctx, "/bcsapi/v4/clustermanager/v1/cluster", query, &respData)
		if errors.Is(err, bcsapi.ErrMockMode) {
			return mockCluster(in), nil
		}
		if err != nil {
			return nil, fmt.Errorf("查询 BCS 集群失败: %w", err)
		}
		return &Result{OK: true, Data: respData}, nil
	}

	return function.NewFunctionTool(
		fn,
		function.WithName("bcs_cluster_query"),
		function.WithDescription("查询 BCS 集群信息（状态/节点数/K8s 版本）。适用场景：判断集群是否健康、列出项目下的集群列表。"),
	)
}

func mockCluster(in ClusterInput) *Result {
	clusters := []map[string]any{
		{
			"clusterID":   "BCS-K8S-00001",
			"clusterName": "letsgo-prod",
			"status":      "RUNNING",
			"environment": "prod",
			"k8sVersion":  "1.24.8",
			"nodeCount":   24,
			"readyNodes":  23,
			"projectID":   in.ProjectID,
		},
		{
			"clusterID":   "BCS-K8S-00002",
			"clusterName": "letsgo-test",
			"status":      "RUNNING",
			"environment": "test",
			"k8sVersion":  "1.24.8",
			"nodeCount":   6,
			"readyNodes":  6,
			"projectID":   in.ProjectID,
		},
	}
	if in.ClusterID != "" {
		filtered := clusters[:0]
		for _, c := range clusters {
			if c["clusterID"] == in.ClusterID {
				filtered = append(filtered, c)
			}
		}
		clusters = filtered
	}
	return &Result{
		OK:      true,
		Mock:    true,
		Message: "当前为 Mock 模式",
		Data:    map[string]any{"total": len(clusters), "clusters": clusters},
	}
}
