package session

import (
	"os"
	"testing"
)

func TestBackendFromEnv(t *testing.T) {
	t.Setenv("SESSION_BACKEND", "")
	if got := BackendFromEnv(); got != BackendInMem {
		t.Fatalf("expected inmem, got %s", got)
	}

	t.Setenv("SESSION_BACKEND", "inmem")
	if got := BackendFromEnv(); got != BackendInMem {
		t.Fatalf("expected inmem, got %s", got)
	}

	t.Setenv("SESSION_BACKEND", "redis")
	if got := BackendFromEnv(); got != BackendRedis {
		t.Fatalf("expected redis, got %s", got)
	}

	t.Setenv("SESSION_BACKEND", "garbage")
	if got := BackendFromEnv(); got != BackendInMem {
		t.Fatalf("expected inmem fallback for unknown value, got %s", got)
	}
}

func TestNewWithBackend_RedisDegradesWhenNotCompiled(t *testing.T) {
	// 在默认构建下（无 -tags redis），newRedisSession 返回 nil；
	// NewWithBackend 应该自动降级到 inmem
	t.Setenv("SESSION_REDIS_ADDR", "should-not-matter:6379")
	svc := NewWithBackend(DefaultConfig(), nil, BackendRedis)
	if svc == nil {
		t.Fatal("NewWithBackend should never return nil; should fallback to inmem")
	}
	_ = os.Unsetenv("SESSION_REDIS_ADDR")
}
