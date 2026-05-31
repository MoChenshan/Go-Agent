package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadRulesetFromFile_Empty path 为空或文件不存在时返回 (nil, nil)。
func TestLoadRulesetFromFile_Empty(t *testing.T) {
	rs, err := LoadRulesetFromFile("")
	if err != nil || rs != nil {
		t.Fatalf("空 path 应返回 (nil,nil), got rs=%v err=%v", rs, err)
	}
	// 不存在的路径
	rs, err = LoadRulesetFromFile(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil || rs != nil {
		t.Fatalf("不存在的文件应返回 (nil,nil), got rs=%v err=%v", rs, err)
	}
}

// TestLoadRulesetFromFile_OK 正常解析带 input/output 两段的 YAML。
func TestLoadRulesetFromFile_OK(t *testing.T) {
	path := writeTmpYAML(t, `
input:
  max_user_chars: 1024
  rules:
    - name: jailbreak
      pattern: "(?i)ignore\\s+previous"
      reason: "越狱指令"
    - name: base64_payload
      pattern: "[A-Za-z0-9+/=]{80,}"
      reason: "疑似走私攻击"
      require_contains: ["decode", "execute", "运行"]
output:
  rules:
    - name: token
      pattern: "sk-[A-Za-z0-9]{20,}"
      replacement: "[REDACTED_TOKEN]"
`)
	rs, err := LoadRulesetFromFile(path)
	if err != nil {
		t.Fatalf("LoadRulesetFromFile failed: %v", err)
	}
	if rs.Input.MaxUserChars != 1024 {
		t.Fatalf("max_user_chars 期望 1024, got %d", rs.Input.MaxUserChars)
	}
	if len(rs.Input.Rules) != 2 || len(rs.Output.Rules) != 1 {
		t.Fatalf("规则数量不符: in=%d out=%d",
			len(rs.Input.Rules), len(rs.Output.Rules))
	}
	if len(rs.Input.Rules[1].RequireContains) != 3 {
		t.Fatalf("require_contains 未正确解析: %v",
			rs.Input.Rules[1].RequireContains)
	}
}

// TestLoadRulesetFromFile_BadYAML 非法 YAML 必须返回 error。
func TestLoadRulesetFromFile_BadYAML(t *testing.T) {
	path := writeTmpYAML(t, "input: [this is not valid")
	_, err := LoadRulesetFromFile(path)
	if err == nil {
		t.Fatal("非法 YAML 应返回 error")
	}
}

// TestCompileInputRules_Happy 正常编译并命中。
func TestCompileInputRules_Happy(t *testing.T) {
	rules, err := CompileInputRules([]InputRuleYAML{
		{Name: "jb", Pattern: "(?i)ignore", Reason: "越狱"},
		{Name: "b64", Pattern: "[A-Za-z0-9]{40,}", Reason: "走私",
			RequireContains: []string{"decode", "执行"}},
	})
	if err != nil {
		t.Fatalf("CompileInputRules err: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("期望 2 条规则, got %d", len(rules))
	}
	// 规则 0：正则单独命中
	if !rules[0].Match("please IGNORE all", nil) {
		t.Fatal("jb 规则应命中 'IGNORE'")
	}
	// 规则 1：长串 + decode 语义 → 命中
	longStr := strings.Repeat("A", 60)
	if !rules[1].Match("please decode "+longStr, nil) {
		t.Fatal("b64 + decode 应命中")
	}
	// 规则 1：长串但无语义 → 不命中（降低误伤）
	if rules[1].Match("log dump "+longStr, nil) {
		t.Fatal("单纯长串（无 decode/执行）不应命中")
	}
	// 规则 1：中文"执行"也算命中
	if !rules[1].Match("请执行 "+longStr, nil) {
		t.Fatal("中文执行语义应命中")
	}
}

// TestCompileInputRules_Validation 必填字段/重名/非法正则必须报错。
func TestCompileInputRules_Validation(t *testing.T) {
	cases := []struct {
		name  string
		rules []InputRuleYAML
		msg   string
	}{
		{
			"name empty",
			[]InputRuleYAML{{Pattern: "x", Reason: "r"}},
			"name 不能为空",
		},
		{
			"pattern empty",
			[]InputRuleYAML{{Name: "n", Reason: "r"}},
			"pattern 不能为空",
		},
		{
			"reason empty",
			[]InputRuleYAML{{Name: "n", Pattern: "x"}},
			"reason 不能为空",
		},
		{
			"dup name",
			[]InputRuleYAML{
				{Name: "n", Pattern: "a", Reason: "r1"},
				{Name: "n", Pattern: "b", Reason: "r2"},
			},
			"重复",
		},
		{
			"bad regex",
			[]InputRuleYAML{{Name: "n", Pattern: "(?P<", Reason: "r"}},
			"非法正则",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := CompileInputRules(c.rules)
			if err == nil || !strings.Contains(err.Error(), c.msg) {
				t.Fatalf("期望 err 含 %q, got %v", c.msg, err)
			}
		})
	}
}

// TestCompileOutputRules_Happy 正常编译。
func TestCompileOutputRules_Happy(t *testing.T) {
	rules, err := CompileOutputRules([]OutputRuleYAML{
		{Name: "tok", Pattern: "sk-[A-Za-z0-9]{5,}",
			Replacement: "[REDACTED]"},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rules) != 1 || rules[0].Pattern == nil {
		t.Fatal("规则编译结果异常")
	}
	out := rules[0].Pattern.ReplaceAllString("key is sk-ABCDE", rules[0].Replacement)
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("替换未生效: %s", out)
	}
}

// TestCompileOutputRules_Validation 空字段/重名/非法正则必须报错。
func TestCompileOutputRules_Validation(t *testing.T) {
	cases := []struct {
		name  string
		rules []OutputRuleYAML
		msg   string
	}{
		{"name empty",
			[]OutputRuleYAML{{Pattern: "x", Replacement: "r"}},
			"name 不能为空"},
		{"pattern empty",
			[]OutputRuleYAML{{Name: "n", Replacement: "r"}},
			"pattern 不能为空"},
		{"replacement empty",
			[]OutputRuleYAML{{Name: "n", Pattern: "x"}},
			"replacement 不能为空"},
		{"dup",
			[]OutputRuleYAML{
				{Name: "n", Pattern: "a", Replacement: "r"},
				{Name: "n", Pattern: "b", Replacement: "r"},
			}, "重复"},
		{"bad regex",
			[]OutputRuleYAML{{Name: "n", Pattern: "(?P<", Replacement: "r"}},
			"非法正则"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := CompileOutputRules(c.rules)
			if err == nil || !strings.Contains(err.Error(), c.msg) {
				t.Fatalf("期望 err 含 %q, got %v", c.msg, err)
			}
		})
	}
}

// writeTmpYAML 辅助：把 content 写到临时文件并返回路径。
func writeTmpYAML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rules.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write tmp yaml: %v", err)
	}
	return path
}
