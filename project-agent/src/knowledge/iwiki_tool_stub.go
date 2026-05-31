//go:build !iwiki
// +build !iwiki

// iwiki_tool_stub.go: 默认构建路径（未启用 -tags iwiki）。
//
// 该文件不引入任何内网 iwiki 依赖，因此可用于：
//   - 开发者本地单元测试 / 外网 CI（不能访问 git.woa.com）
//   - 不需要 iWiki 云检索能力的部署
//
// 当希望启用真实 iWiki RAG，请以 `go build -tags iwiki` 构建，此时本文件
// 会被编译排除，由 iwiki_tool_real.go 提供 buildIWikiTool 的真实实现。
package knowledge

import (
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// buildIWikiTool 默认构建下始终返回 stub。
//
// 语义：
//   - Disabled：返回"已禁用"stub。
//   - 缺凭据：返回"未就绪 / 缺 env"stub。
//   - 凭据齐备但未启用 -tags iwiki：返回"真实后端未编译"stub，提示运维切换构建。
func buildIWikiTool(cfg IWikiConfig) (tool.Tool, bool) {
	if cfg.Disabled {
		return iwikiStubTool("iWiki 知识库已被禁用（IWIKI_DISABLE=1）。"), true
	}
	if cfg.PaasID == "" || cfg.Token == "" {
		return iwikiStubTool(
			"iWiki 知识库未就绪：缺少 IWIKI_PAAS_ID/IWIKI_TOKEN；" +
				"请配置后重启服务，当前请改用 knowledge_search 或基于常识回答。"), true
	}
	return iwikiStubTool(
		"iWiki 真实后端未编译进当前构建（需 -tags iwiki 才能启用内网 iwiki 模块）；" +
			"请走 knowledge_search 或基于常识回答。"), true
}
