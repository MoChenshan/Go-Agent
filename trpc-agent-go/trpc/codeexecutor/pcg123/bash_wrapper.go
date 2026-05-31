// Package pcg123 is the remote code executor for pcg 123
package pcg123

import (
	"fmt"
	"strings"
)

const noneStr = "None"

// bashExecTemplate is the Python script template for executing bash commands
// on a workspace inside the pcg123 sandbox.
//
// Workspace invariant maintained across every RunProgram: files are
// owned `mqq:pcg123_<gid>` mode 0660, dirs are `mqq:pcg123_<gid>` mode
// 2770. The wrapper enforces this with two symmetric sweeps around the
// bash invocation — a pre-exec `chmod 2770` to repair dir modes the SDK
// staged at 2755, and a post-exec `chown mqq` to reclaim files bash
// wrote under its setuid'd identity. The group stays as the per-session
// gid throughout (setgid-bit inheritance), which is what carries
// cross-session isolation.
const bashExecTemplate = `
import os
import sys
import json
import subprocess
import traceback

SETUID_STUB = (
    "import os, sys, json;"
    "uid=int(sys.argv[1]); gid=int(sys.argv[2]); cwd=sys.argv[3]; env=json.loads(sys.argv[4]); cmd=sys.argv[5];"
    "os.setgroups([]);"
    "os.setgid(gid); os.setuid(uid);"
    "os.umask(0o007);"
    "os.chdir(cwd);"
    "os.execvpe('bash', ['bash', '-c', cmd], env)"
)

def _build_env(workspace_path, custom_env):
    env = dict(os.environ)
    env["WORKSPACE_DIR"] = workspace_path
    env["OUTPUT_DIR"] = os.path.join(workspace_path, "out")
    env["WORK_DIR"] = os.path.join(workspace_path, "work")
    env["SKILLS_DIR"] = os.path.join(workspace_path, "skills")
    # HOME is the workspace itself — the per-session user's /etc/passwd
    # entry already points here, but the SETUID_STUB exec'es bash with an
    # explicit env dict (built from this map), so set it here too to keep
    # the value consistent across both lookup paths.
    env["HOME"] = workspace_path
    if custom_env:
        env.update(custom_env)
    return env

def main():
    workspace_path = %s
    command = %s
    work_dir = %s
    stdin_input = %s
    timeout_sec = %s
    custom_env = %s
    run_as_uid = %s
    run_as_gid = %s

    if not os.path.isdir(workspace_path):
        print("Error: workspace directory not found: " + workspace_path, file=sys.stderr)
        sys.exit(1)

    exec_dir = os.path.join(workspace_path, work_dir) if work_dir else workspace_path

    os.makedirs(os.path.join(workspace_path, "out"), exist_ok=True)
    os.makedirs(exec_dir, exist_ok=True)

    env = _build_env(workspace_path, custom_env)

    if run_as_uid is None or run_as_gid is None:
        # Single-user path: run as the sandbox default user, no isolation.
        os.chdir(exec_dir)
        argv = command
        run_kwargs = {"shell": True, "capture_output": True, "text": True, "env": env}
    else:
        # Isolation path: drop privileges to (run_as_uid, run_as_gid). The
        # workspace is already chgrp'd to run_as_gid with mode 2770, so the
        # bash process has full access via the group while cross-session
        # processes (different per-session gid) are blocked at the dir.
        #
        # Refresh directory modes before exec: SDK staging (mkdir over NFS
        # for skill bundles, .metadata files, etc.) goes through go-billy
        # osfs which calls os.Mkdir with mode 0o755; the kernel-applied
        # umask + setgid-bit-only inheritance leaves those dirs at 2755
        # (group r-x), so the per-session uid cannot write into them. A
        # single 'find -exec chmod 2770' walks them back to 2770 every
        # RunProgram, keeping the workspace mode invariant the wrapper
        # relies on (group rwx, world none, setgid). It runs as mqq via
        # sudo because the dirs are owned by mqq, not run_as_uid.
        refresh = subprocess.run(
            ["sudo", "find", workspace_path, "-type", "d", "-exec", "chmod", "2770", "{}", "+"],
            capture_output=True, text=True,
        )
        if refresh.returncode != 0:
            print("Error refreshing workspace dir modes: " + refresh.stderr, file=sys.stderr)
            sys.exit(1)
        argv = [
            "sudo", "python3", "-c", SETUID_STUB,
            str(int(run_as_uid)),
            str(int(run_as_gid)),
            exec_dir,
            json.dumps(env),
            command,
        ]
        run_kwargs = {"capture_output": True, "text": True}

    if timeout_sec is not None:
        run_kwargs["timeout"] = timeout_sec
    if stdin_input is not None:
        run_kwargs["input"] = stdin_input

    exit_code = 1
    try:
        try:
            result = subprocess.run(argv, **run_kwargs)
            if result.stdout:
                print(result.stdout)
            if result.stderr:
                print(result.stderr, file=sys.stderr)
            exit_code = result.returncode
        except subprocess.TimeoutExpired:
            print("Command execution timed out", file=sys.stderr)
            exit_code = 124
        except Exception as e:
            print("Error executing command: " + str(e), file=sys.stderr)
            traceback.print_exc(file=sys.stderr)
            exit_code = 1
    finally:
        # Normalize ownership back to the sandbox default user. While bash
        # runs setuid'd to run_as_uid, any file or dir it creates is owned
        # by run_as_uid; without this sweep the workspace would drift into
        # a mixed-ownership state across calls. The group is left alone —
        # setgid-bit inheritance has already pinned new entries to
        # run_as_gid, which is what cross-session isolation depends on.
        # The '! -user mqq' filter keeps it idempotent: previously-swept
        # files (already mqq-owned) are skipped, so the cost scales with
        # what bash actually wrote this round, not the whole tree. Runs
        # under finally so timeouts and exceptions still leave the
        # workspace in the canonical mqq:run_as_gid shape for the next
        # SDK touch and the next RunProgram.
        if run_as_uid is not None and run_as_gid is not None:
            subprocess.run(
                ["sudo", "find", workspace_path, "!", "-user", "mqq", "-exec", "chown", "mqq", "{}", "+"],
                capture_output=True, text=True,
            )

    sys.exit(exit_code)

if __name__ == "__main__":
    main()
`

// bashExecParams holds the parameters for generating the bash execution script.
type bashExecParams struct {
	WorkspacePath string            // workspace, e.g., "/usr/local/app/nfs_root/ws_xxx"
	Command       string            // Shell command to execute
	WorkDir       string            // Working directory relative to workspace root
	Stdin         string            // Optional stdin input for the command
	TimeoutSec    int               // Execution timeout in seconds, 0 means no timeout
	Env           map[string]string // Additional environment variables
	// RunAsUID / RunAsGID are the per-session uid and gid to drop privileges
	// to before exec'ing bash. Both must be set together; nil disables uid
	// switching and the wrapper runs as the sandbox default user.
	RunAsUID *uint32
	RunAsGID *uint32
}

// generateBashExecScript renders a Python script that executes a command
// in the workspace.
func generateBashExecScript(params bashExecParams) string {
	workspacePathRepr := fmt.Sprintf("%q", params.WorkspacePath)
	cmdRepr := fmt.Sprintf("%q", params.Command)

	workDirRepr := noneStr
	if params.WorkDir != "" {
		workDirRepr = fmt.Sprintf("%q", params.WorkDir)
	}

	stdinRepr := noneStr
	if params.Stdin != "" {
		stdinRepr = fmt.Sprintf("%q", params.Stdin)
	}

	timeoutRepr := noneStr
	if params.TimeoutSec != 0 {
		timeoutRepr = fmt.Sprintf("%d", params.TimeoutSec)
	}

	envRepr := noneStr
	if len(params.Env) > 0 {
		envRepr = pythonDictRepr(params.Env)
	}

	runAsUIDRepr := noneStr
	if params.RunAsUID != nil {
		runAsUIDRepr = fmt.Sprintf("%d", *params.RunAsUID)
	}

	runAsGIDRepr := noneStr
	if params.RunAsGID != nil {
		runAsGIDRepr = fmt.Sprintf("%d", *params.RunAsGID)
	}

	return fmt.Sprintf(bashExecTemplate,
		workspacePathRepr,
		cmdRepr,
		workDirRepr,
		stdinRepr,
		timeoutRepr,
		envRepr,
		runAsUIDRepr,
		runAsGIDRepr,
	)
}

// pythonDictRepr renders a map as a Python dict literal using Go's %q for
// each key and value. Iteration order is non-deterministic; the produced
// Python is a literal, so order does not matter for execution.
func pythonDictRepr(m map[string]string) string {
	var sb strings.Builder
	sb.WriteString("{")
	first := true
	for k, v := range m {
		if !first {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%q: %q", k, v)
		first = false
	}
	sb.WriteString("}")
	return sb.String()
}
