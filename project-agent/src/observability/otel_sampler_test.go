package observability

import (
	"context"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestResolveSampler_Env 覆盖 OTEL_TRACES_SAMPLER 所有支持值。
func TestResolveSampler_Env(t *testing.T) {
	cases := []struct {
		name    string
		env     string
		arg     string
		wantStr string // Description() 子串，用来区分采样器
	}{
		{"default_empty", "", "", "ParentBased"},
		{"always_on", "always_on", "", "AlwaysOnSampler"},
		{"always_off", "always_off", "", "AlwaysOffSampler"},
		{"traceidratio_half", "traceidratio", "0.5", "TraceIDRatioBased"},
		{"parentbased_ratio", "parentbased_traceidratio", "0.1", "ParentBased"},
		{"unknown_fallback", "weird_value", "", "ParentBased"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_TRACES_SAMPLER", tc.env)
			t.Setenv("OTEL_TRACES_SAMPLER_ARG", tc.arg)
			s := resolveSampler()
			if s == nil {
				t.Fatalf("sampler is nil")
			}
			desc := s.Description()
			if desc == "" {
				t.Fatalf("sampler Description() empty")
			}
			if !strings.Contains(desc, tc.wantStr) {
				t.Errorf("sampler Description()=%q want substring %q", desc, tc.wantStr)
			}
		})
	}

	// 兜底：返回类型必须实现 sdktrace.Sampler 接口
	var _ sdktrace.Sampler = resolveSampler()
}

// TestParseRatio 覆盖 OTEL_TRACES_SAMPLER_ARG 的解析健壮性。
func TestParseRatio(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"", 1.0},
		{"0", 0.0},
		{"0.5", 0.5},
		{"1", 1.0},
		{"1.5", 1.0},  // 超过 1 回落
		{"-0.3", 1.0}, // 负数回落
		{"abc", 1.0},  // 非法回落
	}
	for _, tc := range cases {
		got := parseRatio(tc.in)
		if got != tc.want {
			t.Errorf("parseRatio(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

// TestIncSSEEvent_NoPanic 只验证埋点 API 在 Noop Meter 下不 panic。
func TestIncSSEEvent_NoPanic(t *testing.T) {
	ResetMetricsForTest()
	ctx := context.Background()
	for _, ev := range []string{"", "delta", "tool_call", "agent_transfer", "confirmation_required", "final", "error"} {
		IncSSEEvent(ctx, ev)
	}
}

// TestDescribeSampler 覆盖启动日志用的采样器描述函数，
// 重点验证：① 规范化名稱、② ratio 字符串对 ratio 系/非 ratio 系的分叉、③ fallback 标记。
func TestDescribeSampler(t *testing.T) {
	cases := []struct {
		name         string
		env          string
		arg          string
		wantName     string
		wantRatio    string
		wantFallback bool
	}{
		{"default_empty", "", "", "parentbased_always_on", "n/a", false},
		{"always_on", "always_on", "", "always_on", "n/a", false},
		{"always_off", "always_off", "", "always_off", "n/a", false},
		{"traceidratio_half", "traceidratio", "0.5", "traceidratio", "0.50", false},
		{"parentbased_ratio_10pct", "parentbased_traceidratio", "0.1", "parentbased_traceidratio", "0.10", false},
		{"parentbased_always_off", "parentbased_always_off", "", "parentbased_always_off", "n/a", false},

		// fallback 场景
		{"unknown_sampler", "weird_value", "", "parentbased_always_on", "n/a", true},
		{"illegal_arg_negative", "traceidratio", "-0.3", "traceidratio", "1.00", true},
		{"illegal_arg_abc", "parentbased_traceidratio", "abc", "parentbased_traceidratio", "1.00", true},
		{"illegal_arg_gt1", "traceidratio", "2.5", "traceidratio", "1.00", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_TRACES_SAMPLER", tc.env)
			t.Setenv("OTEL_TRACES_SAMPLER_ARG", tc.arg)
			name, ratio, fb := describeSampler()
			if name != tc.wantName {
				t.Errorf("name=%q want %q", name, tc.wantName)
			}
			if ratio != tc.wantRatio {
				t.Errorf("ratio=%q want %q", ratio, tc.wantRatio)
			}
			if fb != tc.wantFallback {
				t.Errorf("fallback=%v want %v", fb, tc.wantFallback)
			}
		})
	}
}