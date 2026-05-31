package pcg123

import "fmt"

// sessionIdentityTemplate provisions the per-session Linux user/group and
// binds a workspace tree to it. Run once per workspace at CreateWorkspace
// time via runCodeBlock; the bash wrapper later setuid's into this same
// (uid, gid) so the workspace is reachable to the session and opaque to
// other sessions.
//
// Steps:
//  1. Ensure a Linux group exists with gid == uid and name == name.
//  2. Ensure a Linux user exists with that uid/gid/name. Home directory
//     in /etc/passwd points at the workspace itself (--home-dir ws,
//     --no-create-home — the dir was already created by NFS MkdirAll
//     above this layer). Idempotent via getent: concurrent
//     CreateWorkspace calls hashing to the same uid race on useradd, and
//     "already exists" is the desired outcome, not a failure.
//  3. chgrp -R the workspace tree to the group.
//  4. chmod 2770 on every directory (rwxrwx--- + setgid bit). Files are
//     left untouched: at this point in CreateWorkspace they don't exist
//     yet — they appear later from SDK staging or bash output and inherit
//     the group via the setgid bit on their parent.
//  5. Drop a one-line <ws>/.bashrc that sources the sandbox image's
//     shared rc (/usr/local/app/.bashrc), so interactive `su
//     pcg123_<uid>` debugging gets the same alias / prompt set the
//     default mqq user has. Owned mqq:<gid> mode 0660 to fit the
//     workspace-shape invariant the bash wrapper maintains.
//
// HOME pointing at the workspace gives us a single, uniform home concept
// across both entry points: skill execution (the bash wrapper sets
// HOME=workspace_path explicitly in env) and `su` debugging (which reads
// HOME from /etc/passwd) land in the same place. There is no separate
// /home/pcg123_<uid> to keep in sync.
//
// Dispatched via runCodeBlock because the NFS protocol client (SDK side)
// can neither manage local user accounts nor set group/mode on
// server-side files; the sandbox-side Python holds passwordless sudo.
const sessionIdentityTemplate = `
import os
import subprocess
import sys
import tempfile

def run(cmd):
    r = subprocess.run(cmd, capture_output=True, text=True)
    if r.returncode != 0:
        print("session identity: %%s failed: %%s" %% (" ".join(cmd), r.stderr), file=sys.stderr)
        sys.exit(r.returncode or 1)

def exists(database, value):
    return subprocess.run(
        ["getent", database, str(value)],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    ).returncode == 0

uid = %d
name = %q
ws = %q

if not exists("group", uid):
    run(["sudo", "groupadd", "--gid", str(uid), name])
if not exists("passwd", uid):
    run(["sudo", "useradd",
         "--uid", str(uid),
         "--gid", str(uid),
         "--home-dir", ws,
         "--no-create-home",
         "--shell", "/bin/bash",
         "--comment", "pcg123 session",
         name])

run(["sudo", "chgrp", "-R", str(uid), ws])
run(["sudo", "find", ws, "-type", "d", "-exec", "chmod", "2770", "{}", "+"])

# Step 5: minimal .bashrc at workspace root. The [-r ...] guard keeps it
# benign if /usr/local/app/.bashrc moves or becomes unreadable in a
# future image. Written to a tempfile owned by mqq, then sudo install
# drops it at <ws>/.bashrc with mqq:<gid> mode 0660 — owner kept as mqq
# so it matches the workspace-shape invariant the bash wrapper
# maintains, group set to the per-session gid so the per-session bash
# (which runs in that gid) can read it via group-rw, and the post-exec
# chown sweep in bash_wrapper.go will leave it alone (filter is
# ! -user mqq).
with tempfile.NamedTemporaryFile("w", delete=False) as f:
    f.write("[ -r /usr/local/app/.bashrc ] && source /usr/local/app/.bashrc\n")
    bashrc_src = f.name
try:
    run(["sudo", "install", "-m", "0660", "-o", "mqq", "-g", name,
         bashrc_src, ws + "/.bashrc"])
finally:
    os.unlink(bashrc_src)
`

// sessionIdentityParams holds the parameters for the session-identity
// provisioning script. UID is used for both the uid and gid of the
// per-session user (we draw from a range above any real system id, so
// reusing the value for both keeps the data plane simple).
type sessionIdentityParams struct {
	WorkspacePath string
	UID           uint32
	Username      string
}

// generateSessionIdentityScript renders the Python script that creates
// the per-session user/group and binds the workspace tree to them.
func generateSessionIdentityScript(params sessionIdentityParams) string {
	return fmt.Sprintf(sessionIdentityTemplate,
		params.UID,
		params.Username,
		params.WorkspacePath,
	)
}
