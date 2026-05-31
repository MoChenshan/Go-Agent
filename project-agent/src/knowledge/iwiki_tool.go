// Package knowledge（D10）：iWiki 云端知识库工具。
//
// 设计要点：
//   - 自行实现 iWiki RAG OpenAPI 客户端（Rio 签名 + HTTP POST），无内网包依赖
//   - 与本地 BuiltinKnowledge 并存（本地优先，iWiki 兜底）
//   - 凭据缺失自动降级为 stub，不阻塞启动
//
// 构建标签约定：
//   - 默认构建：只提供 stub 实现（方便离线单测 / 不需要 iWiki 的场景）
//   - `-tags iwiki`：启用真实 iWiki 客户端（见 iwiki_tool_real.go）
//
// 凭据（真实模式下生效）：
//   - IWIKI_PAAS_ID   / IWIKI_TOKEN      （太湖平台）
//   - IWIKI_URL       （默认 prod 环境）
//   - IWIKI_SPACE_IDS （可选，逗号分隔的 space id 列表）
//   - IWIKI_DISABLE=1 强制禁用
package knowledge

import (
	"context"
	"os"
	"strconv"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// IWikiToolName 注入到 Agent 的工具名。
const IWikiToolName = "iwiki_search"

// IWikiConfig iWiki 工具配置。
type IWikiConfig struct {
	URL      string
	PaasID   string
	Token    string
	SpaceIDs []int
	// MaxResults 默认 5
	MaxResults int
	// Disabled 强制禁用（IWIKI_DISABLE=1）
	Disabled bool
}

// DefaultIWikiConfig 从环境变量补齐。
func DefaultIWikiConfig() IWikiConfig {
	c := IWikiConfig{
		URL:        getenvDefault("IWIKI_URL", "http://api-idc.sgw.woa.com/ebus/iwiki/prod"),
		PaasID:     os.Getenv("IWIKI_PAAS_ID"),
		Token:      os.Getenv("IWIKI_TOKEN"),
		MaxResults: 5,
	}
	if v := os.Getenv("IWIKI_MAX_RESULTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 20 {
			c.MaxResults = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("IWIKI_SPACE_IDS")); v != "" {
		for _, p := range strings.Split(v, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && n > 0 {
				c.SpaceIDs = append(c.SpaceIDs, n)
			}
		}
	}
	if isTruthy(os.Getenv("IWIKI_DISABLE")) {
		c.Disabled = true
	}
	return c
}

// BuildIWikiTool 根据凭据情况构造 iwiki_search 工具。
//
// 本函数对外暴露统一入口，内部通过 buildIWikiTool(cfg) 分发：
//   - 默认构建（无 iwiki tag）：走 iwiki_tool_stub.go 里的实现，始终返回 stub。
//   - `-tags iwiki`：走 iwiki_tool_real.go 里的实现，自行实现 iWiki HTTP 客户端。
//
// 两个 build tag 文件通过互斥的 //go:build 约束保证同时只有一份被编译，
// 由它们各自提供 buildIWikiTool 函数。
//
// 第二返回值 isStub 便于上层决定是否在日志中提示降级。
func BuildIWikiTool(cfg IWikiConfig) (tool.Tool, bool) {
	return buildIWikiTool(cfg)
}

// iwikiStubTool 生成降级占位工具。
func iwikiStubTool(msg string) tool.Tool {
	type stubIn struct {
		Query string `json:"query" description:"要检索的问题"`
	}
	type stubOut struct {
		OK      bool   `json:"ok"`
		Stub    bool   `json:"stub"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
	}
	fn := func(_ context.Context, _ stubIn) (*stubOut, error) {
		return &stubOut{
			OK:      false,
			Stub:    true,
			Message: msg,
			Hint:    "iWiki 工具不可用，请优先使用 knowledge_search；若仍无结果，请基于模型常识回答并提示用户。",
		}, nil
	}
	return function.NewFunctionTool(
		fn,
		function.WithName(IWikiToolName),
		function.WithDescription("【占位工具】iWiki 云端知识库未就绪，调用后返回提示。"),
	)
}

// snippet 截断文本，避免 token 溢出。
func snippet(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	// 按 rune 截断避免切字符
	rs := []rune(s)
	if len(rs) <= max {
		return string(rs)
	}
	return string(rs[:max]) + "…"
}