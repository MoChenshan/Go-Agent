package preflight

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// clearAllEnv 清理所有影响 preflight 的环境变量，避免本机/CI 干扰测试。
func clearAllEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"OPENAI_API_KEY", "OPENAI_BASE_URL",
		"AUDIT_DISABLE", "AUDIT_SINK",
		"BK_APP_CODE", "BK_APP_SECRET", "BK_APIGW_BASE_URL",
		"BCS_TOKEN", "BCS_GATEWAY_URL",
		"GONGFENG_TOKEN", "GONGFENG_BASE_URL", "GONGFENG_ALLOW_AUTO_MERGE",
		"DEVOPS_TOKEN", "DEVOPS_BASE_URL", "DEVOPS_UID", "DEVOPS_ALLOW_AUTO_OPS",
		"TAPD_USER", "TAPD_TOKEN", "TAPD_WORKSPACE_ID",
		"IWIKI_PAAS_ID", "IWIKI_TOKEN", "IWIKI_DISABLE",
	}
	for _, k := range keys {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
}

func TestRun_AllMockWhenNoEnv(t *testing.T) {
	clearAllEnv(t)
	rpt := Run()
	if rpt.Model.Ready() {
		t.Errorf("model should not be ready without OPENAI_API_KEY")
	}
	if rpt.Strict() {
		t.Errorf("strict should be false when everything is mock")
	}
	// 至少应当包含 6 个平台
	if len(rpt.Platforms) < 6 {
		t.Errorf("want >=6 platforms, got %d", len(rpt.Platforms))
	}
}

func TestRun_PartialReal(t *testing.T) {
	clearAllEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-xxx")
	t.Setenv("BK_APP_CODE", "xxx")
	t.Setenv("BK_APP_SECRET", "yyy")
	// bkapi.NewClient 会在 baseURL 缺失时也强制切到 Mock 模式，
	// 因此测试 REAL 路径必须同时设置 BK_APIGW_BASE_URL。
	t.Setenv("BK_APIGW_BASE_URL", "https://bkapi.example.com")

	rpt := Run()
	if !rpt.Model.Ready() {
		t.Errorf("model should be ready with OPENAI_API_KEY")
	}
	// BK 应为 REAL
	var bk *Platform
	for i := range rpt.Platforms {
		if rpt.Platforms[i].Name == "bk-monitor" {
			bk = &rpt.Platforms[i]
			break
		}
	}
	if bk == nil || bk.Mode != ModeReal {
		t.Errorf("bk-monitor should be REAL, got %+v", bk)
	}
	// strict 仍为 false（其他平台仍 mock）
	if rpt.Strict() {
		t.Errorf("strict must remain false when some platforms still mock")
	}
}

func TestRun_IWikiDisabled(t *testing.T) {
	clearAllEnv(t)
	t.Setenv("IWIKI_DISABLE", "1")
	rpt := Run()
	var iw *Platform
	for i := range rpt.Platforms {
		if rpt.Platforms[i].Name == "iwiki" {
			iw = &rpt.Platforms[i]
			break
		}
	}
	if iw == nil || iw.Mode != ModeDisabled {
		t.Errorf("iwiki should be DISABLED, got %+v", iw)
	}
}

func TestPrint_ContainsAllPlatforms(t *testing.T) {
	clearAllEnv(t)
	rpt := Run()
	var buf bytes.Buffer
	rpt.Print(&buf)
	out := buf.String()
	for _, title := range []string{"LLM 模型", "蓝鲸监控", "BCS 容器", "工蜂 Git", "蓝盾 CI/CD", "TAPD", "iWiki"} {
		if !strings.Contains(out, title) {
			t.Errorf("output missing %q: %s", title, out)
		}
	}
}
