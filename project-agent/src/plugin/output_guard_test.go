package plugin

import (
	"context"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

func mkResp(content string) *model.AfterModelArgs {
	return &model.AfterModelArgs{
		Response: &model.Response{
			Choices: []model.Choice{{
				Index:   0,
				Message: model.Message{Role: model.RoleAssistant, Content: content},
			}},
		},
	}
}

// TestOutputGuard_NoHitPassThrough 无敏感内容应不改写响应。
func TestOutputGuard_NoHitPassThrough(t *testing.T) {
	g := NewOutputGuard(OutputGuardConfig{})
	ret, err := g.afterModel(context.Background(),
		mkResp("诊断结论：bcs-prod-1 CPU 使用率飙高，建议重启 game-core pod"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ret != nil && ret.CustomResponse != nil {
		t.Fatalf("无敏感内容不应覆写响应，ret=%+v", ret)
	}
}

// TestOutputGuard_RedactsTokens sk-xxx / gho_xxx / JWT 三类 token 应被打码。
func TestOutputGuard_RedactsTokens(t *testing.T) {
	g := NewOutputGuard(OutputGuardConfig{})
	inputs := map[string]string{
		"openai_key":   "这是 key：sk-abc123DEF456GHIjkl789mno",
		"github_token": "复制 gho_abcdefghijklmnopqrstuvwxyz0123456789XYZA",
		"jwt": "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
			"eyJzdWIiOiIxMjM0NSIsIm5hbWUiOiJ0ZXN0In0." +
			"SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
	}
	for name, in := range inputs {
		ret, _ := g.afterModel(context.Background(), mkResp(in))
		if ret == nil || ret.CustomResponse == nil {
			t.Fatalf("[%s] 应被打码：输入=%q", name, in)
		}
		out := ret.CustomResponse.Choices[0].Message.Content
		if !strings.Contains(out, "[REDACTED_TOKEN]") {
			t.Fatalf("[%s] 打码标记缺失：%q", name, out)
		}
	}
}

// TestOutputGuard_RedactsPrivateIP 三类私网 IP 段均应打码，公网 IP 不动。
func TestOutputGuard_RedactsPrivateIP(t *testing.T) {
	g := NewOutputGuard(OutputGuardConfig{})
	in := "节点列表：10.0.1.23, 172.16.5.8, 192.168.100.200, 8.8.8.8"
	ret, _ := g.afterModel(context.Background(), mkResp(in))
	if ret == nil {
		t.Fatalf("私网 IP 应被打码")
	}
	out := ret.CustomResponse.Choices[0].Message.Content
	for _, priv := range []string{"10.0.1.23", "172.16.5.8", "192.168.100.200"} {
		if strings.Contains(out, priv) {
			t.Fatalf("私网 IP %q 未被打码：%q", priv, out)
		}
	}
	if !strings.Contains(out, "8.8.8.8") {
		t.Fatalf("公网 IP 不应误伤：%q", out)
	}
	if strings.Count(out, "[REDACTED_PRIVATE_IP]") != 3 {
		t.Fatalf("私网 IP 应命中 3 次，实际：%q", out)
	}
}

// TestOutputGuard_RedactsCredentialLiteral password=xxx / secret=xxx / api_key=xxx 应打码。
func TestOutputGuard_RedactsCredentialLiteral(t *testing.T) {
	g := NewOutputGuard(OutputGuardConfig{})
	cases := []string{
		"配置是 password=SuperStrong123",
		"PWD: HelloWorld999",
		"secret=deadbeefcafe",
		"api_key=abc123xyz789",
		"API-Key: tokenValue12345",
	}
	for _, c := range cases {
		ret, _ := g.afterModel(context.Background(), mkResp(c))
		if ret == nil {
			t.Fatalf("凭据字面量应被打码：%q", c)
		}
		out := ret.CustomResponse.Choices[0].Message.Content
		if !strings.Contains(out, "[REDACTED_CREDENTIAL]") {
			t.Fatalf("凭据打码标记缺失：输入=%q 输出=%q", c, out)
		}
	}
}

// TestOutputGuard_RedactLoggerFires 自定义 Logger 应按规则命中次数分别调用。
func TestOutputGuard_RedactLoggerFires(t *testing.T) {
	hits := map[string]int{}
	g := NewOutputGuard(OutputGuardConfig{
		Logger: func(rule string, n int) { hits[rule] += n },
	})
	_, _ = g.afterModel(context.Background(), mkResp(
		"ip=10.0.0.1; token=sk-abcdefghijklmnopqrst; password=GoodOne2024"))
	if hits["private_ipv4"] != 1 || hits["token_like_secret"] != 1 ||
		hits["credential_literal"] != 1 {
		t.Fatalf("Logger 命中次数不对：%+v", hits)
	}
}

// TestOutputGuard_RedactDeltaContent 流式 chunk 的 Delta 字段同样打码。
func TestOutputGuard_RedactDeltaContent(t *testing.T) {
	g := NewOutputGuard(OutputGuardConfig{})
	args := &model.AfterModelArgs{
		Response: &model.Response{
			Choices: []model.Choice{{
				Index: 0,
				Delta: model.Message{Role: model.RoleAssistant,
					Content: "节点 192.168.1.1 不可达"},
			}},
		},
	}
	ret, _ := g.afterModel(context.Background(), args)
	if ret == nil {
		t.Fatalf("Delta 内容应被打码")
	}
	if strings.Contains(ret.CustomResponse.Choices[0].Delta.Content,
		"192.168.1.1") {
		t.Fatalf("Delta 私网 IP 未打码")
	}
}

// TestOutputGuard_NilAndEmpty nil / 空响应安全返回。
func TestOutputGuard_NilAndEmpty(t *testing.T) {
	g := NewOutputGuard(OutputGuardConfig{})
	if r, _ := g.afterModel(context.Background(), nil); r != nil {
		t.Fatalf("nil args 应返回 nil")
	}
	if r, _ := g.afterModel(context.Background(),
		&model.AfterModelArgs{}); r != nil {
		t.Fatalf("空 response 应返回 nil")
	}
	if r, _ := g.afterModel(context.Background(),
		&model.AfterModelArgs{Response: &model.Response{}}); r != nil {
		t.Fatalf("无 choices 应返回 nil")
	}
}

// TestOutputGuard_RedactDirect Redact 方法可脱离 callback 使用。
func TestOutputGuard_RedactDirect(t *testing.T) {
	g := NewOutputGuard(OutputGuardConfig{})
	redacted, stat := g.Redact("连 10.0.0.1 用 password=abcdef1")
	if strings.Contains(redacted, "10.0.0.1") ||
		strings.Contains(redacted, "abcdef1") {
		t.Fatalf("Redact 未生效：%q", redacted)
	}
	if stat["private_ipv4"] != 1 || stat["credential_literal"] != 1 {
		t.Fatalf("stat 不对：%+v", stat)
	}
}
