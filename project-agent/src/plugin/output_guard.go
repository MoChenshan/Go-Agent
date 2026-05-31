// Package plugin 中的 output_guard 在 AfterModel 阶段扫描模型回复，
// 命中敏感模式时 *打码（redact）* 而非短路，保留可用性。
//
// 为什么打码而不是短路：
//   - LLM 产出里夹杂内网 IP / Token / 密码关键词往往来自工具回显，
//     直接短路会让 Agent"变哑巴"；打码既保密又保留上下文。
//   - input_guard 已在输入侧做了硬拦截，这里做兜底二道门。
//
// 默认规则覆盖三类 OWASP LLM06（Sensitive Info Disclosure）常见载体：
//  1. 类 JWT/OAuth Token（长 base64 三段式 / sk-xxx / gho_xxx 等）
//  2. 内网 IP（10.x / 172.16-31.x / 192.168.x）
//  3. 密码/密钥字面量（password=xxx / secret=xxx / api_key=xxx）
package plugin

import (
	"context"
	"regexp"
	"sync/atomic"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// OutputRule 输出扫描规则。命中后用 Replacement 替换原文。
type OutputRule struct {
	// Name 规则名。
	Name string
	// Pattern 预编译正则；命中部分会被 Replacement 整体替换。
	Pattern *regexp.Regexp
	// Replacement 打码后的占位，建议保留字段名让 LLM/用户能自然理解。
	Replacement string
}

// OutputGuardConfig 配置。
type OutputGuardConfig struct {
	// Rules 自定义规则；空则使用 DefaultOutputRules()。
	Rules []OutputRule
	// Logger 命中时的日志钩子（可选）。
	Logger func(ruleName string, hits int)
}

// OutputGuard AfterModel 阶段的输出敏感信息打码器。
//
// D17.1：rules 改为 atomic.Pointer，支持 rule_watcher 运行期热换。
type OutputGuard struct {
	rules  atomic.Pointer[[]OutputRule]
	logger func(rule string, hits int)
}

// NewOutputGuard 构造 OutputGuard。
func NewOutputGuard(cfg OutputGuardConfig) *OutputGuard {
	rules := cfg.Rules
	if len(rules) == 0 {
		rules = DefaultOutputRules()
	}
	g := &OutputGuard{logger: cfg.Logger}
	g.rules.Store(&rules)
	return g
}

// ReplaceRules 原子替换整组打码规则，传入空切片降级为默认规则集。
func (g *OutputGuard) ReplaceRules(rules []OutputRule) {
	if len(rules) == 0 {
		rules = DefaultOutputRules()
	}
	g.rules.Store(&rules)
}

// loadRules 原子读取当前规则快照。
func (g *OutputGuard) loadRules() []OutputRule {
	p := g.rules.Load()
	if p == nil {
		return nil
	}
	return *p
}

// Register 挂到 model.Callbacks.AfterModel。
func (g *OutputGuard) Register(cb *model.Callbacks) *model.Callbacks {
	if cb == nil {
		cb = model.NewCallbacks()
	}
	cb.RegisterAfterModel(g.afterModel)
	return cb
}

// Redact 直接对字符串做一次打码（便于单测/外部复用）。
func (g *OutputGuard) Redact(s string) (string, map[string]int) {
	stat := map[string]int{}
	for _, r := range g.loadRules() {
		if r.Pattern == nil {
			continue
		}
		count := 0
		s = r.Pattern.ReplaceAllStringFunc(s, func(_ string) string {
			count++
			return r.Replacement
		})
		if count > 0 {
			stat[r.Name] = count
			if g.logger != nil {
				g.logger(r.Name, count)
			}
		}
	}
	return s, stat
}

// afterModel 实现 AfterModelCallbackStructured。
func (g *OutputGuard) afterModel(_ context.Context,
	args *model.AfterModelArgs) (*model.AfterModelResult, error) {
	if args == nil || args.Response == nil || len(args.Response.Choices) == 0 {
		return nil, nil
	}
	// 克隆响应避免就地改写（框架后续链路可能还要消费原响应）。
	clone := args.Response.Clone()
	changed := false
	for i := range clone.Choices {
		// 同时覆盖 Message.Content（非流式）和 Delta.Content（流式 chunk）。
		if clone.Choices[i].Message.Content != "" {
			redacted, stat := g.Redact(clone.Choices[i].Message.Content)
			if len(stat) > 0 {
				clone.Choices[i].Message.Content = redacted
				changed = true
			}
		}
		if clone.Choices[i].Delta.Content != "" {
			redacted, stat := g.Redact(clone.Choices[i].Delta.Content)
			if len(stat) > 0 {
				clone.Choices[i].Delta.Content = redacted
				changed = true
			}
		}
	}
	if !changed {
		return nil, nil
	}
	return &model.AfterModelResult{CustomResponse: clone}, nil
}

// DefaultOutputRules 默认 3 条打码规则。
func DefaultOutputRules() []OutputRule {
	return []OutputRule{
		{
			Name: "token_like_secret",
			// 覆盖：
			//   - OpenAI key：sk-xxx（≥20 位字母数字）
			//   - GitHub PAT：gho_/ghp_/ghu_/ghs_/ghr_ 开头 36+ 位
			//   - 通用 JWT：三段式 base64.base64.base64
			Pattern: regexp.MustCompile(
				`sk-[A-Za-z0-9]{20,}` +
					`|gh[oupsr]_[A-Za-z0-9]{36,}` +
					`|eyJ[A-Za-z0-9_\-]{10,}\.eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`),
			Replacement: "[REDACTED_TOKEN]",
		},
		{
			Name: "private_ipv4",
			Pattern: regexp.MustCompile(
				`\b(?:10(?:\.\d{1,3}){3}` +
					`|172\.(?:1[6-9]|2\d|3[01])(?:\.\d{1,3}){2}` +
					`|192\.168(?:\.\d{1,3}){2})\b`),
			Replacement: "[REDACTED_PRIVATE_IP]",
		},
		{
			Name: "credential_literal",
			// password=xxx / pwd=xxx / secret=xxx / api_key=xxx（等号后连续非空白 ≥ 6 位）
			Pattern: regexp.MustCompile(
				`(?i)(password|pwd|secret|api[_-]?key)\s*[:=]\s*[^\s'"` + "`" +
					`]{6,}`),
			Replacement: "[REDACTED_CREDENTIAL]",
		},
	}
}
