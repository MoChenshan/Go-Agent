// judge_tool_selection.go —— D29 新增 Judge 维度：ToolSelectionAccuracy。
//
// # 为什么需要这个维度
//
// 现有 3 个维度（RootCause / Evidence / HelpfulnessSafety）都是基于**答案文本**的关键词
// 打分，不能回答 D26 prompt 工程的核心问题：
//
//	"LLM 看到 DiskPressure 告警时，真的会选 bcs_node_describe 而不是 bcs_pod_restart 吗？"
//
// 这个问题的证据在 **tool_call 轨迹**里，不在答案文本里。D29 新增的
// ToolSelectionAccuracy 维度直接对比：
//
//	实际 tool trace 的工具名序列  VS  golden tool trace 的工具名序列
//
// 命中率越高说明 LLM 越遵循 prompt 的决策树。
//
// # 评分算法
//
// 两个维度的加权：
//  1. 精确命中（exact match）：实际用到的工具完全等于 golden 的工具集合（忽略顺序）
//     —— 权重 0.6
//  2. 顺序命中（order match）：实际调用顺序和 golden 一致（用 LCS 长度 / golden 长度）
//     —— 权重 0.4
//
// 两个分数加权后得到 0~1 的总分。这样设计的好处：
//   - 只"选对工具但顺序乱" → 仍能拿到 0.6 基础分（诊断-修复大框架对）
//   - "先 scale 再 describe"（违反"先看后动"）→ 顺序分会拉低总分
//
// # 数据流
//
//	case.Conversation[i].Tools       → golden trace（执行方案定义的标准路径）
//	JudgeInput.ActualToolCalls       → 实际 LLM 这次调用产生的 trace
//	ScoreToolSelection(golden, actual) → 0~1
package eval

import (
	"strings"
)

// DimensionToolSelectionAccuracy 是 D29 新增的第 4 个维度名。
const DimensionToolSelectionAccuracy = "ToolSelectionAccuracy"

// ToolSelectionAccuracyDimension 返回该维度的默认配置。
//
// 阈值 0.80 比 RootCause 的 0.85 低一点——因为 LLM 可能多调一些工具做交叉验证
// （不一定和 golden 完全一致），只要核心工具对了就算过。
func ToolSelectionAccuracyDimension() JudgeDimension {
	return JudgeDimension{
		Name:      DimensionToolSelectionAccuracy,
		Threshold: 0.80,
		Criterion: "实际工具调用序列与 golden 轨迹的重合度（工具名集合 + 顺序）。关注 LLM 是否按 prompt 决策树选对工具。",
	}
}

// DefaultJudgeDimensionsV2 是 D29 后的 4 维度版本，向后兼容旧的 3 维。
//
// 注：不直接修改 DefaultJudgeDimensions() 以避免 D12~D28 遗留测试被动失败。
// 新调用方建议使用 V2；老调用方继续用 V1 仍然能跑。
func DefaultJudgeDimensionsV2() []JudgeDimension {
	return append(DefaultJudgeDimensions(), ToolSelectionAccuracyDimension())
}

// ScoreToolSelection 对比两条 tool trace，返回 0~1 的打分。
//
//	golden —— 期望的工具名序列（来自 evalset case.Tools）
//	actual —— LLM 本次实际调用的工具名序列
//
// 规则：
//   - golden 为空：返回 1.0（该 case 不关心工具选择，应该在更高层过滤掉）
//   - actual 为空但 golden 非空：返回 0（LLM 一个工具都没调，严重不符合预期）
//   - 其他情况：0.6·集合命中率 + 0.4·顺序命中率
func ScoreToolSelection(golden, actual []string) float64 {
	if len(golden) == 0 {
		return 1.0
	}
	if len(actual) == 0 {
		return 0.0
	}
	setScore := setOverlapScore(golden, actual)
	orderScore := lcsRatio(golden, actual)
	total := 0.6*setScore + 0.4*orderScore
	if total > 1 {
		total = 1
	}
	if total < 0 {
		total = 0
	}
	// 四舍五入到 2 位避免测试抖动
	return float64(int(total*100+0.5)) / 100
}

// setOverlapScore 计算 golden 工具集合在 actual 里的命中率（忽略顺序、忽略重复）。
//
//	hit = |unique(golden) ∩ unique(actual)| / |unique(golden)|
func setOverlapScore(golden, actual []string) float64 {
	if len(golden) == 0 {
		return 1.0
	}
	gset := dedupLower(golden)
	aset := dedupLower(actual)
	aMap := map[string]struct{}{}
	for _, a := range aset {
		aMap[a] = struct{}{}
	}
	hit := 0
	for _, g := range gset {
		if _, ok := aMap[g]; ok {
			hit++
		}
	}
	return float64(hit) / float64(len(gset))
}

// lcsRatio 返回 golden 与 actual 的最长公共子序列长度 / len(golden)。
//
// 用 LCS 而非"逐位相等"是为了容忍 LLM 多调了一两个无害的交叉验证工具
// （例如在正确路径上插入一次 bcs_resource_query）。
//
// 示例：
//	golden = [node_describe, network_update]
//	actual = [node_describe, pod_describe, network_update]  ← 多了 pod_describe
//	LCS = 2，ratio = 2/2 = 1.0（顺序完全对）
//
// 反例：
//	golden = [node_describe, network_update]
//	actual = [network_update, node_describe]  ← 顺序反了
//	LCS = 1，ratio = 0.5
func lcsRatio(golden, actual []string) float64 {
	if len(golden) == 0 {
		return 1.0
	}
	g := toLower(golden)
	a := toLower(actual)
	n, m := len(g), len(a)
	// 标准 LCS DP（O(n·m)，n/m 典型 < 10，够用）
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if g[i-1] == a[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] > dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	return float64(dp[n][m]) / float64(n)
}

// dedupLower 对字符串数组去重并小写化，保持首次出现顺序。
func dedupLower(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// toLower 批量小写化（不去重、保留顺序，供 LCS 使用）。
func toLower(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToLower(strings.TrimSpace(s))
	}
	return out
}

// ExtractGoldenToolNames 从一个 EvalCase 里按对话顺序抽出期望的工具名序列。
//
// 这是喂给 ScoreToolSelection 的 golden 一方。
func ExtractGoldenToolNames(c EvalCase) []string {
	var names []string
	for _, inv := range c.Conversation {
		for _, t := range inv.Tools {
			if t.Name != "" {
				names = append(names, t.Name)
			}
		}
	}
	return names
}
