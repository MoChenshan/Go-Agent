---
name: rainbow-config
description: "Rainbow configuration management via MCP: manage apps, groups, configs, templates, releases, and operation logs. Uses mcporter to call the Rainbow MCP server."
metadata:
  {
    "openclaw":
      {
        "emoji": "🌈",
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

# Rainbow MCP Skill

Interact with Rainbow (configuration management platform) through its MCP server using `mcporter`.

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

The config file is `${TRPC_CLAW_STATE_DIR}/skills/bundled/rainbow-config/mcp.json` (mcporter native format).
mcporter reads `bearerTokenEnv` and resolves the token from `process.env` automatically -- no manual token passing needed.

## Workflow

Always try the call first. Only handle auth if it fails with 401/403.

### Available tools (snapshot: 2026-04-01)

If a tool call fails or a tool is not found, refresh the list first:
`mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/rainbow-config/mcp.json list rainbow --schema --output json`

| Tool | Description |
|------|-------------|
| cancel_group_changes | Discard unpublished config changes |
| create_config_version | Create a new version for multi-version groups |
| detect_app_cluster | Detect which cluster an appid belongs to |
| get_app_info | Get project details, roles, environments |
| get_basic_info | Get user info and accessible projects |
| get_group_client_list | List clients connected to a group |
| get_group_config | Get group configuration data (kv/file/table) |
| get_group_list | List groups in a project |
| get_group_metadata | Get group metadata, quota, schema, versions |
| get_group_release_task | Query release tasks |
| get_op_record | Query data and release operation logs |
| get_template_info | Query template packages, files, versions |
| get_zhiyan_template_info | Query Zhiyan template module files |
| manage_group | Create or update a group |
| manage_group_config | Add/update/delete config data |
| manage_table_schema | Create table or manage columns |
| manage_template | Update or cancel template changes |
| manage_zhiyan_template | Add/update/delete Zhiyan template files |
| release_group_config | Release a config version |

### Get user's projects

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/rainbow-config/mcp.json call rainbow.get_basic_info
```

### Get group config

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/rainbow-config/mcp.json call rainbow.get_group_config \
  --args '{"appid":"<appid>","group":"<group_name>"}'
```

### Release config

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/rainbow-config/mcp.json call rainbow.release_group_config \
  --args '{"appid":"<appid>","group":"<group>"}'
```

## Auth failure handling

If any call returns 401, 403, or "unauthorized", run:

```bash
env_probe MCP_RAINBOW_ACCESS_TOKEN
```

If the token is missing or empty, tell the user **both** options below and let them pick whichever fits their setup.

Always show both command blocks so the user can choose.
Provide both options in full in the same reply. For Option 2, include the exact shell command block(s), not just a promise to guide later.
Even if the user says they already restarted openclaw, if authorization or MCP availability is still failing, return the two Option 2 command blocks again for the user to run and verify.
If the error is not a 401, 403, or explicit unauthorized/authentication failure, do not switch to the authorization guidance below. Return the original error to the user directly.

### Option 1: Agui one-click authorization (if using agui-managed deployment)

Open https://agui.woa.com/agent/claw, find the **Taihu (太湖)** authorization entry, and click to authorize. This is the Taihu PAT, which covers all intranet MCP services (TAPD, iWiki, Gongfeng, Rainbow, KM) at once. Do NOT look for a separate "Rainbow" entry -- it is all under Taihu.

After authorizing, restart openclaw and retry.

### Option 2: Manual PAT token (for CLI, Docker, or non-agui deployments)

Open https://tai.it.woa.com/user/pat, create or copy a PAT token, then save it.

If the token only applies to Rainbow:

```bash
export MCP_TOKEN=<PASTE_TOKEN_HERE>
mkdir -p "${TRPC_CLAW_STATE_DIR}"
if grep -q '^MCP_RAINBOW_ACCESS_TOKEN=' "${TRPC_CLAW_STATE_DIR}/.runtime.env" 2>/dev/null; then
  sed -i "s#^MCP_RAINBOW_ACCESS_TOKEN=.*#MCP_RAINBOW_ACCESS_TOKEN=${MCP_TOKEN}#" "${TRPC_CLAW_STATE_DIR}/.runtime.env"
else
  printf '%s=%s\n' 'MCP_RAINBOW_ACCESS_TOKEN' "$MCP_TOKEN" >> "${TRPC_CLAW_STATE_DIR}/.runtime.env"
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

- Config file: `${TRPC_CLAW_STATE_DIR}/skills/bundled/rainbow-config/mcp.json`
- Only TESTING and DEVELOPMENT environments allow write operations.
- Use `rainbow.detect_app_cluster` if data is not found (may be in Singapore cluster).
- Prefer `--output json` for structured results.
