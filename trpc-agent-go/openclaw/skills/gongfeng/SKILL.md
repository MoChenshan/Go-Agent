---
name: gongfeng
description: "Use for Gongfeng/TGit source code work, especially when the user mentions 工蜂, TGit, git.woa.com, merge requests, MR URLs, MR comments, inline comments, code review, repo browsing, branches, commits, or issues. When the user gives a git.woa.com merge request URL or asks to 看/查/review/comment on an MR, trigger this skill immediately instead of treating it as a generic web page first."
metadata:
  {
    "openclaw":
      {
        "emoji": "🔧",
        "install":
          [
            {
              "id": "node",
              "kind": "node",
              "package": "mcporter",
              "bins": ["mcporter"],
              "label": "Install mcporter (node)",
            },
          ],
      },
  }
---

# Gongfeng MCP Skill

Use this skill for Gongfeng / TGit source code work through the
Gongfeng MCP server.

Trigger immediately when the user:

- mentions 工蜂, TGit, or `git.woa.com`
- gives a Gongfeng merge request URL
- asks to 看 / 查 / review / comment on an MR
- asks for inline MR comments, MR notes, MR diff, or MR changes
- asks to inspect repos, branches, commits, issues, or code review data

Do not treat a `git.woa.com/.../merge_requests/...` URL as a generic web
page first. For Gongfeng MR URLs, prefer this skill over browser-based
page opening.

## Prerequisites

Requires `mcporter` CLI. If not installed:

```bash
npm install -g mcporter
```

If `npm install -g mcporter` succeeds but `mcporter` is still not found, check the npm global bin directory and add it to `PATH`:

```bash
npm prefix -g
export PATH="$(npm prefix -g)/bin:$PATH"
$(npm prefix -g)/bin/mcporter --help
```

## Execution

The config file is `${TRPC_CLAW_STATE_DIR}/skills/bundled/gongfeng/mcp.json` (mcporter native format).
mcporter reads `bearerTokenEnv` and resolves the token from `process.env` automatically -- no manual token passing needed.

## Workflow

For Gongfeng MCP, follow: **discover -> schema -> execute**.

- Treat the live MCP `tools/list` result and each tool's `inputSchema`
  as the source of truth for parameter names, optional fields, and
  inline comment capability.
- Treat the static table below as a snapshot only. It can lag behind the
  live server.
- For parameter-sensitive tools, write operations, or first-time usage,
  check the live schema before building args.
- If a call fails because of parameter names or missing required fields,
  refresh the live schema first. Do not keep guessing field names.
- Only handle auth if the call fails with 401/403.

## Live schema discovery

The MCP endpoint in `mcp.json` already supports standard MCP
`initialize` and `tools/list`, so you can inspect the live tool catalog
and each tool's `inputSchema` directly.

Preferred path:

```bash
python3 scripts/live_schema.py --list
python3 scripts/live_schema.py --tool create_merge_request_note
```

This helper reads `mcp.json`, expands the token from the environment,
and talks to the same MCP endpoint directly. It does **not** require
`mcporter`.

Fallback path when you specifically need the `mcporter` CLI view:

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/gongfeng/mcp.json list gongfeng --schema --output json
```

### Available tools (snapshot only, 2026-04-01)

If a tool call fails, a tool is not found, or parameter names look
uncertain, refresh the live schema first with `scripts/live_schema.py`
or the `mcporter ... list --schema` fallback above.

| Tool | Description |
|------|-------------|
| batch_modify_files | Batch add/edit/delete files in a project |
| cherry_pick_commits | Cherry-pick commits to a target branch |
| compare | Compare two commits/branches/tags |
| create_branch | Create a new branch |
| create_issue | Create a new issue |
| create_issue_note | Add a note to an issue |
| create_merge_request | Create a merge request |
| create_merge_request_note | Comment on a merge request |
| create_or_update_file | Create or update a single file |
| create_repository | Create a new project |
| get_blob_content | Get raw file content |
| get_commit_diff | Get diff of a commit |
| get_commit_info | Get commit details |
| get_commits_list | List commits |
| get_current_user | Get current user info |
| get_file_blame | Get file blame/history |
| get_issue_detail | Get issue details |
| get_issue_notes | List issue notes |
| get_merge_request_changes | Get MR code changes |
| get_project_detail | Get project info |
| get_repository_tree | Browse repository file tree |
| get_svn_commits | Get SVN commit history |
| get_svn_repository_tree | Browse SVN repo tree |
| get_tag_list | List tags |
| get_tapd_workitems | Get TAPD items linked to MR/issue |
| get_user_info | Get user info by ID |
| reply_merge_request_note | Reply to a MR comment |
| revert_commit | Revert a commit |
| search_merge_request | Search merge requests |
| search_merge_request_by_user | Search MRs by user role |
| search_merge_request_notes | Get MR comments |
| search_project_issues | Search issues in a project |
| search_projects | Search for projects |
| update_issue | Update an issue |
| update_issue_note | Update an issue note |
| update_merge_request | Update a merge request |

### Search projects

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/gongfeng/mcp.json call gongfeng.search_projects \
  --args '{"search":"project name"}'
```

### Get repository tree

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/gongfeng/mcp.json call gongfeng.get_repository_tree \
  --args '{"project_id":"<id_or_path>"}'
```

### Read file content

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/gongfeng/mcp.json call gongfeng.get_blob_content \
  --args '{"project_id":"<id>","sha":"master","file_path":"README.md"}'
```

### Search merge requests

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/gongfeng/mcp.json call gongfeng.search_merge_request \
  --args '{"project_id":"<id>","state":"opened"}'
```

### Show one tool's live schema

```bash
python3 scripts/live_schema.py --tool create_merge_request_note
```

### Create an inline MR comment

Check the live schema first, then call with the canonical
`merge_request_id` and inline location fields:

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/gongfeng/mcp.json call gongfeng.create_merge_request_note \
  --args '{"project_id":"lingshan/agent_builder","merge_request_id":24501876,"body":"wine 的小助手：这里建议补一个空值保护。","path":"internal/app/service/graphdebug/debugtoolguard.go","line":120,"line_type":"new"}'
```

Notes:

- `merge_request_id` is the canonical internal MR id, not the iid.
- Inline comments use `path`, `line`, and `line_type`.
- Do not guess older names such as `filepath` or `linecode`; confirm the
  live schema first.

## Merge request inspection workflow

When the user gives a merge request URL, iid, short number, or other
non-canonical handle and asks you to look at or review that MR, default
to inspecting the code changes and summarizing them.

Use this workflow:

1. Resolve the project from the URL or path.
2. If you already have a canonical `merge_request_id`, use it directly.
3. If a direct MR lookup or changes lookup fails because the handle may
   be an iid, short number, or other non-canonical identifier, search
   the project's merge requests to recover the canonical internal id.
4. Once you recover the canonical internal id, immediately retry
   `get_merge_request_changes` in the same turn.
5. Do not stop after reporting the iid-to-id mapping if the next
   changes lookup is obvious and feasible.
6. Use `search_merge_request_notes` only after you have the changes when
   review comments or discussion context are needed.

Default interpretation of "看看 MR" or "review this MR":

- Pull the code changes or diff first.
- Summarize the main change areas and risks.
- Only stay at metadata level if code changes are genuinely unavailable.

## Auth failure handling

If any call returns 401, 403, or "unauthorized", run:

```bash
env_probe MCP_GONGFENG_ACCESS_TOKEN
```

If the token is missing or empty, tell the user **both** options below and let them pick whichever fits their setup.

Always show both command blocks so the user can choose.
Provide both options in full in the same reply. For Option 2, include the exact shell command block(s), not just a promise to guide later.
Even if the user says they already restarted openclaw, if authorization or MCP availability is still failing, return the two Option 2 command blocks again for the user to run and verify.
If the error is not a 401, 403, or explicit unauthorized/authentication failure, do not switch to the authorization guidance below. Return the original error to the user directly.

### Option 1: Agui one-click authorization (if using agui-managed deployment)

Open https://agui.woa.com/agent/claw, find the **Taihu (太湖)** authorization entry, and click to authorize. This is the Taihu PAT, which covers all intranet MCP services (TAPD, iWiki, Gongfeng, Rainbow, KM) at once. Do NOT look for a separate "Gongfeng" entry -- it is all under Taihu.

After authorizing, restart openclaw and retry.

### Option 2: Manual PAT token (for CLI, Docker, or non-agui deployments)

Open https://tai.it.woa.com/user/pat, create or copy a PAT token, then save it.

If the token only applies to Gongfeng:

```bash
export MCP_TOKEN=<PASTE_TOKEN_HERE>
mkdir -p "${TRPC_CLAW_STATE_DIR}"
if grep -q '^MCP_GONGFENG_ACCESS_TOKEN=' "${TRPC_CLAW_STATE_DIR}/.runtime.env" 2>/dev/null; then
  sed -i "s#^MCP_GONGFENG_ACCESS_TOKEN=.*#MCP_GONGFENG_ACCESS_TOKEN=${MCP_TOKEN}#" "${TRPC_CLAW_STATE_DIR}/.runtime.env"
else
  printf '%s=%s\n' 'MCP_GONGFENG_ACCESS_TOKEN' "$MCP_TOKEN" >> "${TRPC_CLAW_STATE_DIR}/.runtime.env"
fi
```

If one token covers all intranet MCP components (recommended):

```bash
export MCP_TOKEN=<PASTE_TOKEN_HERE>
mkdir -p "${TRPC_CLAW_STATE_DIR}"
for KEY in MCP_IWIKI_ACCESS_TOKEN MCP_GONGFENG_ACCESS_TOKEN MCP_TAPD_ACCESS_TOKEN MCP_RAINBOW_ACCESS_TOKEN MCP_KM_ACCESS_TOKEN; do
  if grep -q "^${KEY}=" "${TRPC_CLAW_STATE_DIR}/.runtime.env" 2>/dev/null; then
    sed -i "s#^${KEY}=.*#${KEY}=${MCP_TOKEN}#" "${TRPC_CLAW_STATE_DIR}/.runtime.env"
  else
    printf '%s=%s\n' "$KEY" "$MCP_TOKEN" >> "${TRPC_CLAW_STATE_DIR}/.runtime.env"
  fi
done
```

If the user says one token covers all components, explicitly tell them to use the all-components command block.

After saving the token, restart openclaw and retry.

## Notes

- Config file: `${TRPC_CLAW_STATE_DIR}/skills/bundled/gongfeng/mcp.json`
- Use `project_id` as either numeric ID or full path (e.g. `group/project`).
- Prefer `--output json` for structured results.
- Do not hardcode tool lists or parameter names; prefer live schema.
