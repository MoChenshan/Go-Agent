package skillkit

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoad_DisabledByDefault 默认未启用（SKILLS_ENABLE 不设），应该降级。
func TestLoad_DisabledByDefault(t *testing.T) {
	t.Setenv("SKILLS_ENABLE", "")
	b := Load(DefaultConfig())
	if b.Enabled() {
		t.Fatalf("默认应降级，实际却 Enabled")
	}
	if b.SkipReason != "disabled-by-env" {
		t.Fatalf("SkipReason 期望 disabled-by-env，实际 %q", b.SkipReason)
	}
}

// TestLoad_NoSkillsDir 目录不存在时降级，不报错。
func TestLoad_NoSkillsDir(t *testing.T) {
	t.Setenv("SKILLS_ENABLE", "1")
	tmp := t.TempDir()
	cfg := Config{
		SkillsDir:    filepath.Join(tmp, "no-such"),
		WorkspaceDir: filepath.Join(tmp, "ws"),
		EnableEnv:    "SKILLS_ENABLE",
	}
	b := Load(cfg)
	if b.Enabled() {
		t.Fatalf("目录不存在应降级")
	}
	if b.SkipReason != "no-skills-dir" {
		t.Fatalf("SkipReason 期望 no-skills-dir，实际 %q", b.SkipReason)
	}
}

// TestLoad_DirExists 目录存在且启用；若本地无 Python，允许 no-python 降级；
// 若有 Python 则必须 Enabled。
func TestLoad_DirExists(t *testing.T) {
	t.Setenv("SKILLS_ENABLE", "1")
	tmp := t.TempDir()
	skillsDir := filepath.Join(tmp, "skills")
	if err := os.MkdirAll(filepath.Join(skillsDir, "demo"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// 放一个最小的 SKILL.md，FSRepository 扫描时不会报错。
	content := "---\nname: demo\ndescription: demo skill\n---\n# demo\n"
	if err := os.WriteFile(filepath.Join(skillsDir, "demo", "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	b := Load(Config{
		SkillsDir:    skillsDir,
		WorkspaceDir: filepath.Join(tmp, "ws"),
		EnableEnv:    "SKILLS_ENABLE",
	})

	if hasPython() {
		if !b.Enabled() {
			t.Fatalf("本地有 python 但未 Enabled: skip=%q", b.SkipReason)
		}
		if b.Repo == nil || b.Executor == nil {
			t.Fatalf("Repo / Executor 不应为空")
		}
	} else {
		if b.SkipReason != "no-python" {
			t.Fatalf("本地无 python 应降级，实际 %q", b.SkipReason)
		}
	}
}

// TestIsEnabled 核验环境变量解析的大小写兼容。
func TestIsEnabled(t *testing.T) {
	cases := map[string]bool{
		"":      false,
		"0":     false,
		"false": false,
		"1":     true,
		"true":  true,
		"TRUE":  true,
		" on ":  true,
		"Yes":   true,
	}
	for v, want := range cases {
		t.Setenv("__TEST_KEY__", v)
		if got := isEnabled("__TEST_KEY__"); got != want {
			t.Errorf("isEnabled(%q) = %v, want %v", v, got, want)
		}
	}
}
