package pcg123

import (
	"strings"
	"testing"
)

// TestGenerateBashExecScriptBaseline verifies the wrapper renders without
// session isolation set.
func TestGenerateBashExecScriptBaseline(t *testing.T) {
	got := generateBashExecScript(bashExecParams{
		WorkspacePath: "/usr/local/app/nfs_root/ws_test",
		Command:       "echo hi",
		WorkDir:       "work",
		TimeoutSec:    5,
	})

	mustContain(t, got, `workspace_path = "/usr/local/app/nfs_root/ws_test"`)
	mustContain(t, got, `command = "echo hi"`)
	mustContain(t, got, `work_dir = "work"`)
	mustContain(t, got, `timeout_sec = 5`)
	mustContain(t, got, `run_as_uid = None`)
	mustContain(t, got, `run_as_gid = None`)
	// Without RunAsUID/RunAsGID, the wrapper takes the single-user
	// shell=True path that runs as the sandbox default user.
	mustContain(t, got, `"shell": True`)
}

// TestGenerateBashExecScriptWithUID verifies the setuid+exec branch.
func TestGenerateBashExecScriptWithUID(t *testing.T) {
	uid := uint32(60123)
	gid := uint32(60123)
	got := generateBashExecScript(bashExecParams{
		WorkspacePath: "/usr/local/app/nfs_root/ws_test",
		Command:       "id",
		RunAsUID:      &uid,
		RunAsGID:      &gid,
	})

	mustContain(t, got, `run_as_uid = 60123`)
	mustContain(t, got, `run_as_gid = 60123`)
	// SETUID_STUB constant used to drop privileges. setgid happens before
	// setuid because, after setuid to a non-zero uid, setgid would lose
	// the CAP_SETGID privilege and fail.
	mustContain(t, got, `os.setgroups([])`)
	mustContain(t, got, `os.setgid(gid); os.setuid(uid)`)
	mustContain(t, got, `os.execvpe`)
	// umask 007 in SETUID_STUB so files/dirs the bash session itself
	// creates are group-writable (0660 / 2770), keeping the per-session
	// group consistent for the next RunProgram.
	mustContain(t, got, `os.umask(0o007)`)
	// The wrapper builds a sudo + python3 -c argv when uid/gid are set.
	mustContain(t, got, `"sudo", "python3", "-c", SETUID_STUB`)
	// Pre-exec dir-mode refresh: SDK staging (NFS mkdir at 0o755) leaves
	// child dirs at 2755 because setgid-bit inheritance only carries the
	// group, not the group-write bit. The wrapper sweeps every dir back
	// to 2770 before exec so the per-session bash can mkdir/write inside.
	mustContain(t, got, `"sudo", "find", workspace_path, "-type", "d", "-exec", "chmod", "2770"`)
	// Post-exec ownership reclaim: bash runs setuid'd to run_as_uid so
	// files it creates land owned by run_as_uid; the wrapper sweeps them
	// back to mqq in a finally block so the workspace stays uniformly
	// mqq-owned for SDK reads (collect, metadata) and the next RunProgram.
	mustContain(t, got, `"sudo", "find", workspace_path, "!", "-user", "mqq", "-exec", "chown", "mqq"`)
	mustContain(t, got, "finally:")
}

// TestGenerateBashExecScriptEnvRendering verifies custom env keys flow through.
func TestGenerateBashExecScriptEnvRendering(t *testing.T) {
	got := generateBashExecScript(bashExecParams{
		WorkspacePath: "/ws",
		Command:       "true",
		Env:           map[string]string{"FOO": "bar"},
	})
	mustContain(t, got, `"FOO": "bar"`)
	// HOME is anchored to the workspace so tools that resolve "~" don't
	// touch the (non-existent) per-session home dir.
	mustContain(t, got, `env["HOME"] = workspace_path`)
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("rendered script does not contain %q.\n--- script ---\n%s\n--- end ---", needle, haystack)
	}
}
