---
name: km
description: "KM (Knowledge Management) operations via MCP: search articles, knowledge bases, groups, questions, events, manage followings, and view hot content. Uses mcporter to call the KM MCP server."
metadata:
  {
    "openclaw":
      {
        "emoji": "📚",
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

# KM MCP Skill

Interact with KM (Knowledge Management platform) through its MCP server using `mcporter`.

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

The config file is `${TRPC_CLAW_STATE_DIR}/skills/bundled/km/mcp.json` (mcporter native format).
mcporter reads `bearerTokenEnv` and resolves the token from `process.env` automatically -- no manual token passing needed.

## Workflow

Always try the call first. Only handle auth if it fails with 401/403.

### Available tools (snapshot: 2026-04-01)

If a tool call fails or a tool is not found, refresh the list first:
`mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/km/mcp.json list km --schema --output json`

| Tool | Description |
|------|-------------|
| follow-user | Follow a user |
| get-user | Get user profile and top articles |
| hot-articles | Get hot articles (Top N) |
| list-articles | Search/list articles with filters |
| list-events | List tech events and meetups |
| list-followers | List user's followers |
| list-followings | List who a user follows |
| list-groups | Search K groups by keyword/code |
| list-knowledges | Search knowledge bases |
| list-mutual-followings | Get mutual follows with a user |
| list-questions | Search Q&A questions |
| list-space-visitors | List recent space visitors |
| selected-questions | Get featured/selected questions |
| show-article | Get full article by ID or URL |
| show-knowledge | Get knowledge base details and tree |
| unfollow-user | Unfollow a user |

### Search articles by keyword

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/km/mcp.json call km.list-articles \
  --args '{"keywords":["Go","microservice"],"sort":"relevance"}'
```

### Show article by ID or URL

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/km/mcp.json call km.show-article \
  --args '{"article":"<id_or_url>"}'
```

### Hot articles

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/km/mcp.json call km.hot-articles \
  --args '{"period":7,"limit":10}'
```

## Auth failure handling

If any call returns 401, 403, or "unauthorized", run:

```bash
env_probe MCP_KM_ACCESS_TOKEN
```

If the token is missing or empty, tell the user **both** options below and let them pick whichever fits their setup.

Always show both command blocks so the user can choose.
Provide both options in full in the same reply. For Option 2, include the exact shell command block(s), not just a promise to guide later.
Even if the user says they already restarted openclaw, if authorization or MCP availability is still failing, return the two Option 2 command blocks again for the user to run and verify.
If the error is not a 401, 403, or explicit unauthorized/authentication failure, do not switch to the authorization guidance below. Return the original error to the user directly.

### Option 1: Agui one-click authorization (if using agui-managed deployment)

Open https://agui.woa.com/agent/claw, find the **Taihu (太湖)** authorization entry, and click to authorize. This is the Taihu PAT, which covers all intranet MCP services (TAPD, iWiki, Gongfeng, Rainbow, KM) at once. Do NOT look for a separate "KM" entry -- it is all under Taihu.

After authorizing, restart openclaw and retry.

### Option 2: Manual PAT token (for CLI, Docker, or non-agui deployments)

Open https://tai.it.woa.com/user/pat, create or copy a PAT token, then save it.

If the token only applies to KM:

```bash
export MCP_TOKEN=<PASTE_TOKEN_HERE>
mkdir -p "${TRPC_CLAW_STATE_DIR}"
if grep -q '^MCP_KM_ACCESS_TOKEN=' "${TRPC_CLAW_STATE_DIR}/.runtime.env" 2>/dev/null; then
  sed -i "s#^MCP_KM_ACCESS_TOKEN=.*#MCP_KM_ACCESS_TOKEN=${MCP_TOKEN}#" "${TRPC_CLAW_STATE_DIR}/.runtime.env"
else
  printf '%s=%s\n' 'MCP_KM_ACCESS_TOKEN' "$MCP_TOKEN" >> "${TRPC_CLAW_STATE_DIR}/.runtime.env"
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

- Config file: `${TRPC_CLAW_STATE_DIR}/skills/bundled/km/mcp.json`
- For keyword search, provide 2-5 concise terms; use `sort=relevance` for best results.
- Pagination: use `token` field from `next_token` in previous response.
- Prefer `--output json` for structured results.
