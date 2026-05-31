package eval

import (
	"path/filepath"
	"testing"
)

// knownTools 枚举当前 App 所有已注册的 FunctionTool 名字。
//
// 只要 golden set 里引用了这个列表之外的工具名，TestGoldenSet_ToolsExist 就会红灯；
// 新增工具时同步追加此处即可——这比在运行时 load 整个 App 要轻量，适合 CI。
//
// 来源：src/tools/ 下各 tool.go 中的 function.WithName(...)。
var knownTools = map[string]struct{}{
	// bk_tools
	"bk_alarm_query":    {},
	"bk_event_query":    {},
	"bk_log_query":      {},
	"bk_metadata_query": {},
	"bk_metrics_query":  {},
	"bk_tracing_query":  {},
	// bcs_tools（D12 基线：project/cluster/resource/helm_manage）
	"bcs_project_query":  {},
	"bcs_cluster_query":  {},
	"bcs_resource_query": {},
	"bcs_helm_manage":    {},
	// bcs_tools（D21~D28 扩展：诊断深度 + 网络层 + 写操作）
	"bcs_pod_logs_tail":    {}, // D21：Pod 日志拉取
	"bcs_pod_describe":     {}, // D21.1：Pod 深度诊断
	"bcs_node_describe":    {}, // D24：Node 深度诊断（Scenario F Step1）
	"bcs_scale_deployment": {}, // D19/D26：扩缩容（HITL）
	"bcs_pod_restart":      {}, // D20：Pod 重启
	"bcs_configmap_update": {}, // D20.1：ConfigMap 热更
	"bcs_secret_update":    {}, // D22：Secret 热更
	"bcs_hpa_patch":        {}, // D20.2：HPA 防抖
	"bcs_network_update":   {}, // D25：网络层（Scenario F Step2~4）
	// file_tools
	"file_detect":     {},
	"file_read_slice": {},
	"json_query":      {},
	"log_analyze":     {},
	// gongfeng_tools
	"gongfeng_mr_create": {},
	"gongfeng_mr_merge":  {},
	// devops_tools
	"devops_pipeline_rerun": {},
	"devops_build_cancel":   {},
	// tapd_tools
	"tapd_bug_query":  {},
	"tapd_bug_create": {},
	// knowledge
	"knowledge_search": {},
	"iwiki_search":     {},
}

// 默认数据目录（测试运行目录是 eval/ 本身，因此用相对路径 data）
const testDataDir = "data"

func goldenPath(t *testing.T, kind string) string {
	t.Helper()
	return filepath.Join(testDataDir, DefaultEvalSetID, DefaultEvalSetID+"."+kind+".json")
}

func TestGoldenSet_LoadOK(t *testing.T) {
	set, err := LoadEvalSet(goldenPath(t, "evalset"))
	if err != nil {
		t.Fatalf("LoadEvalSet: %v", err)
	}
	if set.EvalSetID != DefaultEvalSetID {
		t.Errorf("evalSetId = %q, want %q", set.EvalSetID, DefaultEvalSetID)
	}
	if got := len(set.EvalCases); got < 3 {
		t.Errorf("evalCases = %d, want >= 3 (覆盖 diagnose/repair/knowledge 至少 3 类)", got)
	}
}

func TestGoldenSet_MetricsLoadOK(t *testing.T) {
	metrics, err := LoadMetrics(goldenPath(t, "metrics"))
	if err != nil {
		t.Fatalf("LoadMetrics: %v", err)
	}
	// 至少要有 tool_trajectory_avg_score（D12 最小可用指标）
	var hasTraj bool
	for _, m := range metrics {
		if m.MetricName == "tool_trajectory_avg_score" {
			hasTraj = true
			if m.Threshold <= 0 || m.Threshold > 1 {
				t.Errorf("tool_trajectory_avg_score threshold = %v, want (0,1]", m.Threshold)
			}
		}
	}
	if !hasTraj {
		t.Error("metrics 缺少 tool_trajectory_avg_score（D12 基础指标）")
	}
}

func TestGoldenSet_AppNameConsistent(t *testing.T) {
	set, err := LoadEvalSet(goldenPath(t, "evalset"))
	if err != nil {
		t.Fatalf("LoadEvalSet: %v", err)
	}
	if bad := set.ValidateAppName(DefaultAppName); len(bad) > 0 {
		t.Errorf("appName 不一致（期望 %q）: %v", DefaultAppName, bad)
	}
}

// TestGoldenSet_ToolsExist 是 D12 最关键的保护：
// 防止 golden set 引用已被删除或改名的工具，导致 tool_trajectory_avg_score 全军覆没。
func TestGoldenSet_ToolsExist(t *testing.T) {
	set, err := LoadEvalSet(goldenPath(t, "evalset"))
	if err != nil {
		t.Fatalf("LoadEvalSet: %v", err)
	}
	missing := set.ValidateAgainstToolRegistry(knownTools)
	if len(missing) > 0 {
		t.Errorf("golden set 引用了未注册的工具，请同步 knownTools 或修正 golden：%v", missing)
	}
}

// TestGoldenSet_SummaryShape 校验统计摘要的合理性，兼作文档展示。
func TestGoldenSet_SummaryShape(t *testing.T) {
	set, err := LoadEvalSet(goldenPath(t, "evalset"))
	if err != nil {
		t.Fatalf("LoadEvalSet: %v", err)
	}
	sum := set.Summarize()
	if sum.CaseCount == 0 || sum.InvCount == 0 || sum.ToolCalls == 0 {
		t.Errorf("summary 不应为空: %+v", sum)
	}
	if len(sum.ToolNames) == 0 {
		t.Error("summary.ToolNames 为空")
	}
	t.Logf("golden summary: cases=%d invocations=%d tool_calls=%d tools=%v",
		sum.CaseCount, sum.InvCount, sum.ToolCalls, sum.ToolNames)
}

// TestGoldenSet_CasesHaveUserAndFinal 每个 case 至少一轮且有用户输入 + 期望回复。
func TestGoldenSet_CasesHaveUserAndFinal(t *testing.T) {
	set, err := LoadEvalSet(goldenPath(t, "evalset"))
	if err != nil {
		t.Fatalf("LoadEvalSet: %v", err)
	}
	for _, c := range set.EvalCases {
		if len(c.Conversation) == 0 {
			t.Errorf("case %s: conversation 为空", c.EvalID)
			continue
		}
		for i, inv := range c.Conversation {
			if inv.UserContent.Content == "" {
				t.Errorf("case %s.inv[%d]: userContent 为空", c.EvalID, i)
			}
			if inv.FinalResponse.Content == "" {
				t.Errorf("case %s.inv[%d]: finalResponse 为空", c.EvalID, i)
			}
		}
	}
}
