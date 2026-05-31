// Package repair ——— system_prompt.md 漂移保护单测（D26 新增）。
//
// 为什么要做：见 diagnosis_agent/prompt_test.go 同题说明。
//
// Repair Agent 是写操作重镇，工具多、Severity 规则多、红线多，
// prompt 漂移的代价比 Diagnosis 更大：LLM 错选一个写工具就是生产事故。
//
// 本测试锁定三条约束：
//  1. 所有 D25 以前的 7 个 bcs-write 工具名都必须在 prompt 里
//  2. D26 新加的两大入口章节（工具选择决策树 / 统一生产红线）都在
//  3. "绝不自动合并 MR"等 D6 核心红线关键词不能被重构改丢
package repair

import (
	"strings"
	"testing"
)

// TestSystemPrompt_MentionsAllBCSWriteTools 断言 7 个 BCS 写工具在 prompt 里均出现。
func TestSystemPrompt_MentionsAllBCSWriteTools(t *testing.T) {
	prompt := defaultSystemPrompt
	must := []string{
		"bcs_helm_manage",        // D1-D7
		"bcs_scale_deployment",   // D18.1
		"bcs_pod_restart",        // D18.2
		"bcs_configmap_update",   // D18.4
		"bcs_secret_update",      // D22
		"bcs_hpa_patch",          // D20.2
		"bcs_network_update",     // D25 ← 本轮 D26 新补引用，防漏
	}
	for _, name := range must {
		if !strings.Contains(prompt, name) {
			t.Errorf("repair system_prompt 缺少写工具名 %q —— LLM 对该工具无感知", name)
		}
	}
}

// TestSystemPrompt_HasDecisionTreeAndProdGuardrails 断言 D26 两大新入口章节存在。
func TestSystemPrompt_HasDecisionTreeAndProdGuardrails(t *testing.T) {
	prompt := defaultSystemPrompt

	// 决策树章节关键词
	decisionTreeKeywords := []string{
		"工具选择决策树",     // 章节标题
		"首要阅读",           // 强调语
		"误区自检",           // 子小节标题
	}
	for _, kw := range decisionTreeKeywords {
		if !strings.Contains(prompt, kw) {
			t.Errorf("repair system_prompt 缺少决策树章节关键词 %q", kw)
		}
	}

	// 统一生产红线章节关键词
	prodGuardKeywords := []string{
		"统一生产红线",
		"Critical 自动触发的通用条件",
		"推荐动作",
	}
	for _, kw := range prodGuardKeywords {
		if !strings.Contains(prompt, kw) {
			t.Errorf("repair system_prompt 缺少统一生产红线章节关键词 %q", kw)
		}
	}
}

// TestSystemPrompt_D6RedLinesPreserved 断言 D6 核心红线没有被 D26 重构意外改丢。
//
// 这类"绝不"条款是 Agent 安全契约的基石，任何重构都不能损失它们。
func TestSystemPrompt_D6RedLinesPreserved(t *testing.T) {
	prompt := defaultSystemPrompt
	redlines := []string{
		"两段式确认",
		"绝不自动合并 MR",
		"绝不自动关闭 TAPD 单",
		"绝不执行 force push",
		"绝不修改用户未要求修改的代码",
	}
	for _, line := range redlines {
		if !strings.Contains(prompt, line) {
			t.Errorf("repair system_prompt 丢失 D6 核心红线 %q —— 请立即恢复", line)
		}
	}
}

// TestSystemPrompt_NetworkUpdateChapterExists 断言 D25 bcs_network_update 独立章节存在。
func TestSystemPrompt_NetworkUpdateChapterExists(t *testing.T) {
	prompt := defaultSystemPrompt
	for _, kw := range []string{
		"BCS 网络层统一更新",       // 章节标题
		"set_selector",            // 六 op 之一
		"set_backend",             // 六 op 之一
		"set_tls",                 // 六 op 之一 + Critical 触发点
		"单元素限制",               // 便捷 op 的关键约束
		"expected_resource_version", // 并发守护关键参数
	} {
		if !strings.Contains(prompt, kw) {
			t.Errorf("repair system_prompt 缺少 D25 network_update 章节关键词 %q", kw)
		}
	}
}
