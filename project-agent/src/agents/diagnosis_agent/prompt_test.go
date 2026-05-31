// Package diagnosis ——— system_prompt.md 漂移保护单测（D26 新增）。
//
// 为什么要做：
// Prompt 是 LLM 的行为宪法。随着工具迭代（D21→D21.1→D24→...），prompt 极易因
// 迁移忘更新而出现"工具改名却没同步 prompt"的漂移。一旦漂移，LLM 会以为
// 旧工具名还存在，调用失败后还会反复重试——在生产环境是灾难。
//
// 本测试对 prompt 里关键工具名与关键方法论关键词做断言：
//   - 实际装配的本地工具名必须在 prompt 里至少出现 1 次（否则 LLM 不知道它的存在）
//   - 陈旧工具名（被下沉/改名的）必须**不再出现**
//
// 测试通过 = LLM 视野与代码装配**对齐**的可机械验证证据。
package diagnosis

import (
	"strings"
	"testing"
)

// TestSystemPrompt_MentionsExpectedLocalTools 断言 D20+ 全部本地 BCS 读工具都在 prompt 里。
func TestSystemPrompt_MentionsExpectedLocalTools(t *testing.T) {
	prompt := defaultSystemPrompt
	must := []string{
		"bcs_project_query",  // 基础三件套
		"bcs_cluster_query",
		"bcs_resource_query",
		"bcs_pod_logs_tail",  // D21
		"bcs_pod_describe",   // D21.1
		"bcs_node_describe",  // D24 ← 本轮 D26 新增引用，防漏
		"logs_unified_query", // D23'
	}
	for _, name := range must {
		if !strings.Contains(prompt, name) {
			t.Errorf("diagnosis system_prompt 缺少工具名 %q —— LLM 无法感知该工具存在", name)
		}
	}
}

// TestSystemPrompt_DoesNotMentionDeprecatedMCPNames 断言过时 MCP 工具名已清理。
//
// D20+ 之后，bcs-project / bcs-cluster / bcs-resource 已从 MCP 下沉为本地工具，
// 若 prompt 里还留着这三个名字，会让 LLM 去找一个**不存在的 MCP target**。
func TestSystemPrompt_DoesNotMentionDeprecatedMCPNames(t *testing.T) {
	prompt := defaultSystemPrompt
	// 注：`bcs-helm` 仍是 MCP 所以不在此列。
	forbidden := []string{
		"bcs-project.",    // 函数调用形式
		"bcs-cluster.",
		"bcs-resource.",
		"`bcs-resource`",  // markdown 反引号包裹
		"`bcs-cluster`",
		"`bcs-project`",
	}
	for _, bad := range forbidden {
		if strings.Contains(prompt, bad) {
			t.Errorf("diagnosis system_prompt 仍包含过时 MCP 名 %q —— D20+ 这些已下沉为本地工具 bcs_*_query", bad)
		}
	}
}

// TestSystemPrompt_NodeDescribeChapterExists 断言 D26 新补的 Node 深度诊断章节存在。
func TestSystemPrompt_NodeDescribeChapterExists(t *testing.T) {
	prompt := defaultSystemPrompt
	// 章节标题 + 节点层样板关键词都必须同时出现
	for _, keyword := range []string{
		"Node 深度诊断",                // 章节标题
		"MemoryPressure",             // Conditions 关键字
		"DiskPressure",
		"Taints",                     // Taints 章节
		"节点层诊断样板",              // 样板小标题
	} {
		if !strings.Contains(prompt, keyword) {
			t.Errorf("diagnosis system_prompt 缺少 Node 诊断关键信息 %q", keyword)
		}
	}
}
