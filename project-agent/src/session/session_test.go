package session

import (
	"testing"
	"time"
)

// TestDefaultConfig_NoEnv 验证默认值（无 env）。
func TestDefaultConfig_NoEnv(t *testing.T) {
	t.Setenv("SESSION_EVENT_THRESHOLD", "")
	t.Setenv("SESSION_TOKEN_THRESHOLD", "")
	t.Setenv("SESSION_TIME_THRESHOLD_MIN", "")
	cfg := DefaultConfig()
	if cfg.EventThreshold != 20 {
		t.Errorf("EventThreshold: want 20, got %d", cfg.EventThreshold)
	}
	if cfg.TokenThreshold != 6000 {
		t.Errorf("TokenThreshold: want 6000, got %d", cfg.TokenThreshold)
	}
	if cfg.TimeThreshold != 10*time.Minute {
		t.Errorf("TimeThreshold: want 10min, got %s", cfg.TimeThreshold)
	}
	if cfg.EventLimit != 40 {
		t.Errorf("EventLimit: want 40, got %d", cfg.EventLimit)
	}
}

// TestDefaultConfig_EnvDriven 验证 env 覆盖。
func TestDefaultConfig_EnvDriven(t *testing.T) {
	t.Setenv("SESSION_EVENT_THRESHOLD", "30")
	t.Setenv("SESSION_TOKEN_THRESHOLD", "8000")
	t.Setenv("SESSION_TIME_THRESHOLD_MIN", "5")
	cfg := DefaultConfig()
	if cfg.EventThreshold != 30 {
		t.Errorf("EventThreshold: want 30, got %d", cfg.EventThreshold)
	}
	if cfg.TokenThreshold != 8000 {
		t.Errorf("TokenThreshold: want 8000, got %d", cfg.TokenThreshold)
	}
	if cfg.TimeThreshold != 5*time.Minute {
		t.Errorf("TimeThreshold: want 5min, got %s", cfg.TimeThreshold)
	}
}

// TestNew_NoModel_Fallback 验证 model==nil 时降级为纯内存 session。
func TestNew_NoModel_Fallback(t *testing.T) {
	svc := New(DefaultConfig(), nil)
	if svc == nil {
		t.Fatal("expected non-nil service even without model")
	}
}

// TestNew_ZeroConfig_UseDefaults 验证零值 Config 被自动补全。
func TestNew_ZeroConfig_UseDefaults(t *testing.T) {
	svc := New(Config{}, nil)
	if svc == nil {
		t.Fatal("expected non-nil service with zero config")
	}
}

// TestEnvInt_Invalid 验证 env 无效时回退默认值。
func TestEnvInt_Invalid(t *testing.T) {
	t.Setenv("TEST_SESSION_X", "not-a-number")
	got := envInt("TEST_SESSION_X", 42)
	if got != 42 {
		t.Errorf("envInt invalid: want 42, got %d", got)
	}

	t.Setenv("TEST_SESSION_Y", "-5")
	got = envInt("TEST_SESSION_Y", 42)
	if got != 42 {
		t.Errorf("envInt negative: want 42, got %d", got)
	}
}
