---
name: iwiki
description: "iWiki document operations via MCP: search, read, create, edit documents, manage spaces, smartsheet operations, and glossary lookups. Uses mcporter to call the iWiki MCP server."
metadata:
  {
    "openclaw":
      {
        "emoji": "📝",
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

# iWiki MCP Skill

Interact with iWiki (internal wiki) through its MCP server using `mcporter`.

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

The config file is `${TRPC_CLAW_STATE_DIR}/skills/bundled/iwiki/mcp.json` (mcporter native format).
mcporter reads `bearerTokenEnv` and resolves the token from `process.env` automatically -- no manual token passing needed.

## Workflow

Always try the call first. Only handle auth if it fails with 401/403.

### Available tools (snapshot: 2026-04-01)

If a tool call fails or a tool is not found, refresh the list first:
`mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/iwiki/mcp.json list iwiki --schema --output json`

| Tool | Description |
|------|-------------|
| addComment | Add a comment to a document |
| addDocumentTags | Add tags to documents |
| aiSearchDocument | AI-powered semantic search across documents |
| copyDocument | Copy a document to a new location |
| createDocument | Create a new document (MD/DOC/FOLDER) |
| deleteDocumentTag | Delete a tag from a document |
| getAttachmentDownloadUrl | Get download URL for an attachment |
| getComments | Get comments on a document |
| getDocQuoteList | Get documents that quote a given document |
| getDocQuoteListBy | Get documents quoted by a given document |
| getDocument | Get document content in Markdown |
| getDocumentTags | Get tags of a document |
| getFavoriteSpaces | Get user's favorite spaces |
| getInlineComment | Get inline comments on a document |
| getManageSpaces | Get spaces the user manages |
| getSpaceInfoByKey | Get space info by space key |
| getSpaceInfoByName | Get space info by name |
| getSpacePageTree | Get page tree under a parent |
| glossaryBatchExactSearch | Batch exact search in glossaries |
| glossaryTermExactSearch | Exact match a single glossary term |
| glossaryTermSearch | Fuzzy search glossary terms |
| listImages | List images in a document |
| metadata | Get document metadata |
| moveDocument | Move a document to a new parent |
| renameDocumentTitle | Rename a document title |
| saveDocument | Save/update document content |
| saveDocumentParts | Partial update of a document |
| searchDocument | Full-text search across iWiki |
| smartsheetAddField | Add a field to a smartsheet |
| smartsheetAddRecords | Add records to a smartsheet |
| smartsheetDeleteField | Delete a smartsheet field |
| smartsheetDeleteRecords | Delete smartsheet records |
| smartsheetGetFields | Get smartsheet field definitions |
| smartsheetGetRecords | Query smartsheet records |
| smartsheetGetViews | Get smartsheet views |
| smartsheetUpdateRecords | Update smartsheet records |

### Search documents

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/iwiki/mcp.json call iwiki.searchDocument \
  --args '{"query":"search keywords"}'
```

### Read a document

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/iwiki/mcp.json call iwiki.getDocument \
  --args '{"docid":"<doc_id>"}'
```

### AI-powered search

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/iwiki/mcp.json call iwiki.aiSearchDocument \
  --args '{"query":"semantic question"}'
```

### Create a document

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/iwiki/mcp.json call iwiki.createDocument \
  --args '{"spaceid":<space_id>,"parentid":<parent_id>,"title":"Doc Title","body":"content"}'
```

### Get space info

```bash
mcporter --config ${TRPC_CLAW_STATE_DIR}/skills/bundled/iwiki/mcp.json call iwiki.getSpaceInfoByKey \
  --args '{"spaceKey":"<space_key>"}'
```

## Auth failure handling

If any call returns 401, 403, or "unauthorized", run:

```bash
env_probe MCP_IWIKI_ACCESS_TOKEN
```

If the token is missing or empty, tell the user **both** options below and let them pick whichever fits their setup.

Always show both command blocks so the user can choose.
Provide both options in full in the same reply. For Option 2, include the exact shell command block(s), not just a promise to guide later.
Even if the user says they already restarted openclaw, if authorization or MCP availability is still failing, return the two Option 2 command blocks again for the user to run and verify.
If the error is not a 401, 403, or explicit unauthorized/authentication failure, do not switch to the authorization guidance below. Return the original error to the user directly.

### Option 1: Agui one-click authorization (if using agui-managed deployment)

Open https://agui.woa.com/agent/claw, find the **Taihu (太湖)** authorization entry, and click to authorize. This is the Taihu PAT, which covers all intranet MCP services (TAPD, iWiki, Gongfeng, Rainbow, KM) at once. Do NOT look for a separate "iWiki" entry -- it is all under Taihu.

After authorizing, restart openclaw and retry.

### Option 2: Manual PAT token (for CLI, Docker, or non-agui deployments)

Open https://tai.it.woa.com/user/pat, create or copy a PAT token, then save it.

If the token only applies to iWiki:

```bash
export MCP_TOKEN=<PASTE_TOKEN_HERE>
mkdir -p "${TRPC_CLAW_STATE_DIR}"
if grep -q '^MCP_IWIKI_ACCESS_TOKEN=' "${TRPC_CLAW_STATE_DIR}/.runtime.env" 2>/dev/null; then
  sed -i "s#^MCP_IWIKI_ACCESS_TOKEN=.*#MCP_IWIKI_ACCESS_TOKEN=${MCP_TOKEN}#" "${TRPC_CLAW_STATE_DIR}/.runtime.env"
else
  printf '%s=%s\n' 'MCP_IWIKI_ACCESS_TOKEN' "$MCP_TOKEN" >> "${TRPC_CLAW_STATE_DIR}/.runtime.env"
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

- Config file: `${TRPC_CLAW_STATE_DIR}/skills/bundled/iwiki/mcp.json`
- Prefer `--output json` for structured results.
