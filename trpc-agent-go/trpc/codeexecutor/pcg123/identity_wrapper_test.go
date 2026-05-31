package pcg123

import (
	"os/exec"
	"strings"
	"testing"
)

// parsePython parses src as Python via `python3 -c 'ast.parse(...)'`. Used
// to catch templating mistakes (unbalanced quotes, stray `%` from
// format-string mishaps, etc.) in scripts we render and ship to the
// sandbox. Skipped when python3 is not on PATH so CI without Python still
// passes.
func parsePython(t *testing.T, label, src string) {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skipf("python3 not found, skipping %s syntax check: %v", label, err)
	}
	cmd := exec.Command(py, "-c", "import ast,sys; ast.parse(sys.stdin.read())")
	cmd.Stdin = strings.NewReader(src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rendered %s not valid Python: %v\n--- python output ---\n%s\n--- script ---\n%s",
			label, err, out, src)
	}
}

// TestGenerateBashExecScriptIsValidPython parses every shape the bash
// wrapper template can render — baseline (no isolation), with uid+gid,
// and with quote-heavy commands.
func TestGenerateBashExecScriptIsValidPython(t *testing.T) {
	uid := uint32(60123)
	gid := uint32(60123)
	cases := []struct {
		name   string
		params bashExecParams
	}{
		{
			name: "baseline",
			params: bashExecParams{
				WorkspacePath: "/usr/local/app/nfs_root/ws_test",
				Command:       "echo hi",
				WorkDir:       "work",
				TimeoutSec:    5,
			},
		},
		{
			name: "with_uid_gid",
			params: bashExecParams{
				WorkspacePath: "/usr/local/app/nfs_root/ws_test",
				Command:       "id; whoami",
				WorkDir:       "work/sub",
				TimeoutSec:    10,
				Stdin:         "stdin payload\n",
				Env:           map[string]string{"FOO": "bar", "BAZ": "qux"},
				RunAsUID:      &uid,
				RunAsGID:      &gid,
			},
		},
		{
			name: "tricky_quotes_in_command",
			params: bashExecParams{
				WorkspacePath: "/ws",
				Command:       `echo "hello $world" 'a\b' 100%done`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsePython(t, "bash exec", generateBashExecScript(tc.params))
		})
	}
}

// TestGenerateSessionIdentityScriptIsValidPython parses the rendered
// session-identity Python — paths and usernames flow through %q, so a
// botched template would corrupt argv at runtime.
func TestGenerateSessionIdentityScriptIsValidPython(t *testing.T) {
	cases := []struct {
		name   string
		params sessionIdentityParams
	}{
		{
			name: "baseline",
			params: sessionIdentityParams{
				WorkspacePath: "/usr/local/app/nfs_root/ws_test",
				UID:           60123,
				Username:      "pcg123_60123",
			},
		},
		{
			name: "path_with_spaces_and_quotes",
			params: sessionIdentityParams{
				WorkspacePath: `/usr/local/app/nfs_root/ws "weird" name`,
				UID:           60500,
				Username:      "pcg123_60500",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsePython(t, "session identity", generateSessionIdentityScript(tc.params))
		})
	}
}

// TestGenerateSessionIdentityScriptContent asserts the rendered Python
// carries every step we depend on — defends against accidental drops
// during future refactors.
func TestGenerateSessionIdentityScriptContent(t *testing.T) {
	got := generateSessionIdentityScript(sessionIdentityParams{
		WorkspacePath: "/usr/local/app/nfs_root/ws_test",
		UID:           60123,
		Username:      "pcg123_60123",
	})

	wants := []string{
		// Step 1+2: user/group provisioning, gated on getent.
		`"sudo", "groupadd", "--gid", str(uid), name`,
		`"sudo", "useradd"`,
		`"--home-dir", ws`,
		`"--no-create-home"`,
		// Step 3+4: workspace ownership and dir mode.
		`"sudo", "chgrp", "-R", str(uid), ws`,
		`"sudo", "find", ws, "-type", "d", "-exec", "chmod", "2770"`,
		// Step 5: .bashrc dropped at workspace root, mqq:gid mode 0660.
		`/usr/local/app/.bashrc`,
		`"sudo", "install", "-m", "0660", "-o", "mqq", "-g", name,`,
		`ws + "/.bashrc"`,
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("rendered identity script missing %q.\n--- script ---\n%s\n--- end ---", want, got)
		}
	}
}
