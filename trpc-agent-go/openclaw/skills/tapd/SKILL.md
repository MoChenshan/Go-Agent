---
name: tapd
description: "TAPD project management via MCP: query/create/update stories, bugs, tasks, iterations, and more. Uses mcporter to call the TAPD MCP server with dynamic tool discovery."
metadata:
  {
    "openclaw":
      {
        "emoji": "📋",
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

# TAPD MCP Skill

Interact with TAPD (project management) through its MCP server using `mcporter`.

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

The config file is `${TRPC_CLAW_STATE_DIR}/skills/bundled/tapd/mcp.json` (mcporter native format).
mcporter reads `bearerTokenEnv` and resolves the token from `process.env` automatically -- no manual token passing needed.

## Workflow

Always try the call first. Only handle auth if it fails with 401/403.
Follow: **discover -> schema -> execute**.

### Available tools (snapshot: 2026-04-01)

If a tool call fails or a tool is not found, refresh the list first:
`mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/tapd/mcp.json list tapd --schema --output json`

TAPD is a meta-tool MCP: use `lookup_tapd_tool` to find the right tool, `lookup_tool_param_schema` to get its parameters, and `proxy_execute_tool` to run it.

| Tool | Description |
|------|-------------|
| lookup_tapd_tool | Find TAPD tools by task description |
| lookup_tool_param_schema | Get parameter schema for a specific tool |
| proxy_execute_tool | Execute a TAPD tool with arguments |
| tql_syntax_reference | Get TQL query syntax reference |

Commonly used proxy tools (pass via `proxy_execute_tool`):

| Proxy Tool | Description |
|------------|-------------|
| user_todo_bugs_get | Get user's pending bugs |
| user_todo_stories_get | Get user's pending stories |
| user_todo_tasks_get | Get user's pending tasks |
| user_participant_workspace_get | Get user's workspaces |
| bugs_get / bugs_create / bugs_update | Query/create/update bugs |
| stories_get / stories_create / stories_update | Query/create/update stories |
| tasks_get / tasks_create / tasks_update | Query/create/update tasks |
| iterations_get / iterations_create | Query/create iterations |
| bugs_count / stories_count / tasks_count | Count items |
| vector_search | Semantic search across TAPD items |
| wikis_get / wikis_create / wikis_update | Manage TAPD Wiki |
| comments_get / comments_create | Manage comments |
| get_workitem_change_history | Get item change history |
| tapd_id_get | Look up TAPD workspace ID |

### Step 1: Discover tools

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/tapd/mcp.json list tapd --schema --output json
```

### Step 2: Find the right tool for the task

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/tapd/mcp.json call tapd.lookup_tapd_tool \
  --args '{"task_description":"<describe what you want to do>"}'
```

### Step 3: Get parameter schema

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/tapd/mcp.json call tapd.lookup_tool_param_schema \
  --args '{"tool_name":"<tool_name_from_step2>"}'
```

### Step 4: Execute

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/tapd/mcp.json call tapd.proxy_execute_tool \
  --args '{"tool_name":"<tool_name>","tool_args":{...}}'
```

## TQL syntax (for query tools)

Tools like `bugs_get`, `stories_get`, `tasks_get`, `iterations_get` support TQL filters:

- Fuzzy: `name=LIKE<keyword>`
- Exact: `status=EQ<open>`
- Not equal: `status=NOT_EQ<closed>`
- Multi-value: `owner=USER_OR<user1|user2>`
- Date range: `created=2024-01-01~2024-06-30`
- Sort: `order=created desc`

Check TQL syntax reference:

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/tapd/mcp.json call tapd.tql_syntax_reference
```

## Common shortcuts

Query user's pending bugs:

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/tapd/mcp.json call tapd.proxy_execute_tool \
  --args '{"tool_name":"user_todo_bugs_get","tool_args":{}}'
```

Query user's pending stories:

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/tapd/mcp.json call tapd.proxy_execute_tool \
  --args '{"tool_name":"user_todo_stories_get","tool_args":{}}'
```

## Auth failure handling

If any call returns 401, 403, or "unauthorized", run:

```bash
env_probe MCP_TAPD_ACCESS_TOKEN
```

If the token is missing or empty, tell the user **both** options below and let them pick whichever fits their setup.

Always show both command blocks so the user can choose.
Provide both options in full in the same reply. For Option 2, include the exact shell command block(s), not just a promise to guide later.
Even if the user says they already restarted openclaw, if authorization or MCP availability is still failing, return the two Option 2 command blocks again for the user to run and verify.
If the error is not a 401, 403, or explicit unauthorized/authentication failure, do not switch to the authorization guidance below. Return the original error to the user directly.

### Option 1: Agui one-click authorization (if using agui-managed deployment)

Open https://agui.woa.com/agent/claw, find the **Taihu (太湖)** authorization entry, and click to authorize. This is the Taihu PAT, which covers all intranet MCP services (TAPD, iWiki, Gongfeng, Rainbow, KM) at once. Do NOT look for a separate "TAPD" entry -- it is all under Taihu.

After authorizing, restart openclaw and retry.

### Option 2: Manual PAT token (for CLI, Docker, or non-agui deployments)

Open https://tai.it.woa.com/user/pat, create or copy a PAT token, then save it.

If the token only applies to TAPD:

```bash
export MCP_TOKEN=<PASTE_TOKEN_HERE>
mkdir -p "${TRPC_CLAW_STATE_DIR}"
if grep -q '^MCP_TAPD_ACCESS_TOKEN=' "${TRPC_CLAW_STATE_DIR}/.runtime.env" 2>/dev/null; then
  sed -i "s#^MCP_TAPD_ACCESS_TOKEN=.*#MCP_TAPD_ACCESS_TOKEN=${MCP_TOKEN}#" "${TRPC_CLAW_STATE_DIR}/.runtime.env"
else
  printf '%s=%s\n' 'MCP_TAPD_ACCESS_TOKEN' "$MCP_TOKEN" >> "${TRPC_CLAW_STATE_DIR}/.runtime.env"
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

## Other errors

- Tool not found: re-run `lookup_tapd_tool` with a clearer description.
- Parameter error: re-run `lookup_tool_param_schema` for the correct schema.
- For description field searches, use `vector_search` instead of TQL.

## Notes

- Config file: `${TRPC_CLAW_STATE_DIR}/skills/bundled/tapd/mcp.json`
- Prefer `--output json` for structured results.
- Do not hardcode tool lists; always discover dynamically.
