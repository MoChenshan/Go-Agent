package main

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	wecomchannel "git.woa.com/trpc-go/trpc-agent-go/openclaw/channel/wecom"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimectl"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

const (
	channelsAdminPagePath   = "/channels"
	channelsAdminStatusPath = "/api/channels/status"
	channelsAdminTimeLayout = "2006-01-02 15:04:05 MST"

	channelsOverviewPath       = "/overview"
	channelsRuntimeControlPath = "/runtime-control"
	channelsPromptsPath        = "/prompts"
	channelsChatsPath          = "/chats"

	channelsNavInjectionMarker = "</nav>"
	channelsStyleMarker        = "</style>"
	channelsBodyMarker         = "</body>"
	channelsNavLabel           = "Channels"

	channelCardStateEnabled         = "enabled"
	channelCardStateDisabled        = "disabled"
	channelCardStateWaitingForEnv   = "paused"
	channelCardStateRestartRequired = "ready"

	wecomConfigBotModeKey        = "bot_mode"
	wecomConfigConnectionModeKey = "connection_mode"

	wecomActivateUserIDPlaceholder = "e.g. T12345678"
	weixinImplicitConfigHint       = "No matching source config " +
		"section was found for this runtime. It may come " +
		"from an implicit runtime default."
)

var channelsAdminPageTemplate = template.Must(
	template.New("channels-admin").Parse(channelsAdminPageHTML),
)

const channelsAdminPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>TRPC-CLAW admin</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f3eee7;
      --panel: rgba(255, 252, 247, 0.92);
      --panel-strong: #fffdf8;
      --line: #d7cfc2;
      --ink: #1d1a16;
      --muted: #5f574d;
      --accent: #0f6f61;
      --warn: #9a2f2f;
      --ok: #2d6d3f;
      --shadow: 0 18px 40px rgba(35, 29, 22, 0.08);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      font-family: "Iowan Old Style", "Palatino Linotype", serif;
      color: var(--ink);
      background:
        radial-gradient(circle at top left, #fff8ef, transparent 38%),
        linear-gradient(180deg, #efe7dc 0%, var(--bg) 100%);
    }
    a { color: var(--accent); }
    code {
      background: rgba(15, 111, 97, 0.08);
      padding: 2px 6px;
      border-radius: 8px;
      word-break: break-all;
    }
    .app-shell {
      display: grid;
      grid-template-columns: 272px minmax(0, 1fr);
      min-height: 100vh;
    }
    .sidebar {
      position: sticky;
      top: 0;
      align-self: start;
      height: 100vh;
      overflow-y: auto;
` + adminSidebarScrollCSS + `
      padding: 24px 18px 22px;
      border-right: 1px solid rgba(215, 207, 194, 0.92);
      background: rgba(255, 250, 244, 0.78);
      backdrop-filter: blur(16px);
    }
    .sidebar-brand {
      display: flex;
      align-items: center;
      gap: 12px;
      margin-bottom: 28px;
    }
    .sidebar-mark {
      width: 42px;
      height: 42px;
      border-radius: 14px;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      background: var(--accent);
      color: white;
      font-weight: 700;
      letter-spacing: 0.04em;
      box-shadow: var(--shadow);
    }
    .sidebar-eyebrow {
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.12em;
    }
    .sidebar-title {
      margin-top: 2px;
      font-size: 26px;
      font-weight: 700;
      line-height: 1.1;
    }
    .sidebar-subtle {
      margin-top: 4px;
      color: var(--muted);
      font-size: 14px;
    }
    .sidebar-nav {
      display: grid;
      gap: 22px;
    }
    .sidebar-section-title {
      margin: 0 0 10px;
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.12em;
    }
    .sidebar-links {
      display: grid;
      gap: 8px;
    }
    .sidebar-link {
      display: flex;
      align-items: center;
      min-height: 42px;
      padding: 10px 14px;
      border-radius: 14px;
      border: 1px solid transparent;
      color: var(--ink);
      text-decoration: none;
      font-weight: 700;
      transition:
        background 120ms ease,
        border-color 120ms ease,
        color 120ms ease;
    }
    .sidebar-link:hover {
      background: rgba(255, 253, 248, 0.88);
      border-color: rgba(215, 207, 194, 0.88);
    }
    .sidebar-link.active {
      background: rgba(15, 111, 97, 0.1);
      border-color: rgba(15, 111, 97, 0.24);
      color: var(--accent);
      box-shadow: var(--shadow);
    }
    main {
      margin: 0;
      width: 100%;
      padding: 32px 28px 40px;
    }
    .page-wrap {
      max-width: 1440px;
    }
    .page-header {
      margin-bottom: 18px;
    }
    .page-kicker {
      margin: 0 0 10px;
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.12em;
    }
    h1, h2 { margin: 0 0 14px; }
    h1 { font-size: 36px; }
    h2 { font-size: 22px; }
    h3, h4 { margin: 0; }
    h3 { font-size: 18px; }
    h4 { font-size: 16px; }
    p, li, td, th, button, code {
      font-size: 15px;
      line-height: 1.5;
    }
    .subtle {
      color: var(--muted);
      max-width: 860px;
    }
    .page-toolbar {
      margin-top: 16px;
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      justify-content: space-between;
      gap: 12px 16px;
    }
    .page-toolbar-copy {
      display: grid;
      gap: 4px;
      min-width: 0;
    }
    .page-toolbar-updated {
      color: var(--muted);
      font-size: 13px;
      font-weight: 700;
      letter-spacing: 0.02em;
    }
    .page-toolbar-note {
      color: var(--muted);
      font-size: 14px;
      max-width: 720px;
    }
    .page-refresh-link {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 40px;
      padding: 8px 14px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: rgba(255, 253, 248, 0.92);
      color: var(--ink);
      text-decoration: none;
      font-weight: 700;
      box-shadow: var(--shadow);
    }
    .page-refresh-link:hover {
      border-color: rgba(15, 111, 97, 0.28);
      color: var(--accent);
    }
    .notice {
      margin: 18px 0 0;
      padding: 12px 14px;
      border-radius: 14px;
      border: 1px solid var(--line);
      background: var(--panel-strong);
      box-shadow: var(--shadow);
    }
    .notice.ok { border-color: rgba(45, 109, 63, 0.3); }
    .notice.err { border-color: rgba(154, 47, 47, 0.3); }
    .config-sections {
      display: grid;
      gap: 18px;
      margin-top: 16px;
    }
    .section-copy {
      margin: 14px 0 0;
    }
    .panels {
      display: grid;
      gap: 16px;
      margin-top: 24px;
      grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
    }
    .card {
      border: 1px solid var(--line);
      border-radius: 20px;
      padding: 20px;
      background: var(--panel);
      box-shadow: var(--shadow);
      backdrop-filter: blur(8px);
      min-width: 0;
    }
    .card-copy {
      margin-top: 14px;
    }
    .meta {
      margin: 0;
      display: grid;
      grid-template-columns: minmax(110px, 160px) 1fr;
      gap: 8px 12px;
    }
    .meta dt {
      color: var(--muted);
      font-weight: 700;
      min-width: 0;
    }
    .meta dd {
      margin: 0;
      min-width: 0;
      overflow-wrap: anywhere;
      word-break: break-word;
    }
    .config-section-card {
      border: 1px solid var(--line);
      border-radius: 18px;
      padding: 18px;
      background: rgba(255, 253, 248, 0.8);
      box-shadow: 0 10px 24px rgba(35, 29, 22, 0.04);
    }
    .config-field-top {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 12px;
    }
    .config-badges {
      display: flex;
      align-items: center;
      gap: 8px;
      flex-wrap: wrap;
    }
    .config-badge {
      border-radius: 999px;
      border: 1px solid rgba(15, 111, 97, 0.18);
      background: rgba(15, 111, 97, 0.08);
      color: var(--accent);
      padding: 4px 10px;
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }
    .config-badge.warn {
      border-color: rgba(154, 47, 47, 0.18);
      background: rgba(154, 47, 47, 0.08);
      color: var(--warn);
    }
    .config-badge.enabled,
    .config-badge.ready {
      border-color: rgba(45, 109, 63, 0.22);
      background: rgba(45, 109, 63, 0.08);
      color: var(--ok);
    }
    .config-badge.disabled,
    .config-badge.cancelled,
    .config-badge.failed,
    .config-badge.missing_token {
      border-color: rgba(154, 47, 47, 0.18);
      background: rgba(154, 47, 47, 0.08);
      color: var(--warn);
    }
    .config-badge.paused {
      border-color: rgba(194, 122, 32, 0.24);
      background: rgba(194, 122, 32, 0.08);
      color: #9b5f12;
    }
    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
    }
    form,
    .inline-form {
      margin: 0;
    }
    .inline-form {
      display: inline-flex;
    }
    button {
      border: 0;
      border-radius: 999px;
      padding: 8px 14px;
      background: var(--accent);
      color: white;
      cursor: pointer;
    }
    button.secondary {
      background: #c9bca9;
      color: var(--ink);
    }
    button.danger,
    button.warn {
      background: var(--warn);
    }
    button:disabled {
      opacity: 0.58;
      cursor: not-allowed;
    }
    .field {
      margin-top: 12px;
    }
    .field label {
      display: block;
      margin-bottom: 6px;
      color: var(--muted);
      font-size: 12px;
      font-weight: 700;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }
    input[type="text"],
    textarea {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 16px;
      padding: 12px 14px;
      font: inherit;
      background: var(--panel-strong);
      color: var(--ink);
    }
    textarea {
      min-height: 96px;
      resize: vertical;
    }
    .stack-form {
      display: grid;
      gap: 12px;
      margin-top: 14px;
    }
    .checkbox-row {
      display: flex;
      align-items: center;
      gap: 8px;
      color: var(--muted);
      font-size: 14px;
    }
    .field-help {
      margin: 6px 0 0;
      color: var(--muted);
      font-size: 14px;
    }
    .config-meta {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
      gap: 10px 16px;
      margin-top: 12px;
    }
    .config-meta-block {
      border-radius: 14px;
      background: rgba(243, 238, 231, 0.62);
      padding: 10px 12px;
    }
    .config-meta-label {
      color: var(--muted);
      font-size: 12px;
      font-weight: 700;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }
    .config-meta-value {
      margin-top: 6px;
      line-height: 1.5;
      word-break: break-word;
    }
    .channel-help {
      margin-top: 12px;
      color: var(--muted);
      max-width: 860px;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      margin-top: 12px;
    }
    th, td {
      text-align: left;
      vertical-align: top;
      min-width: 0;
      padding: 12px 10px;
      border-top: 1px solid var(--line);
    }
    td {
      overflow-wrap: anywhere;
      word-break: break-word;
    }
    th {
      color: var(--muted);
      font-size: 13px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      white-space: nowrap;
      overflow-wrap: normal;
      word-break: normal;
    }
    .account-list {
      display: grid;
      gap: 12px;
      margin-top: 12px;
    }
    .account-card {
      border: 1px solid rgba(215, 207, 194, 0.9);
      border-radius: 16px;
      padding: 16px;
      background: rgba(255, 252, 247, 0.94);
    }
    .account-title {
      margin: 0;
      font-size: 18px;
    }
    .account-title code {
      font-size: inherit;
    }
    .account-note {
      margin-top: 8px;
      color: var(--muted);
    }
    .empty {
      margin-top: 14px;
      color: var(--muted);
    }
    .runtime-row {
      display: grid;
      gap: 16px;
      margin-top: 18px;
    }
    @media (max-width: 760px) {
      .app-shell {
        grid-template-columns: 1fr;
      }
      .sidebar {
        position: static;
        height: auto;
        overflow: visible;
        border-right: 0;
        border-bottom: 1px solid rgba(215, 207, 194, 0.92);
      }
      main {
        padding: 24px 16px 32px;
      }
      h1 { font-size: 30px; }
      .meta {
        grid-template-columns: 1fr;
      }
      table, thead, tbody, th, td, tr {
        display: block;
      }
      thead {
        display: none;
      }
      td {
        padding: 10px 0;
        border-top: 0;
      }
      tr {
        padding: 14px 0;
        border-top: 1px solid var(--line);
      }
    }
  </style>
  {{if .AutoRefresh}}
  <script>
    window.setTimeout(function () {
      window.location.reload();
    }, {{.RefreshIntervalMS}});
  </script>
  {{end}}
</head>
<body>
  <div class="app-shell">
    <aside class="sidebar">
      <div class="sidebar-brand">
        <div class="sidebar-mark">TC</div>
        <div>
          <div class="sidebar-eyebrow">control</div>
          <div class="sidebar-title">TRPC-CLAW</div>
          <div class="sidebar-subtle">
            trpc-claw
          </div>
        </div>
      </div>
      <nav class="sidebar-nav" aria-label="Admin sections">
        <section>
          <div class="sidebar-section-title">Control</div>
          <div class="sidebar-links">
            <a class="sidebar-link" href="overview">Overview</a>
            <a class="sidebar-link" href="config">Config</a>
            <a class="sidebar-link" href="skills">Skills</a>
            <a class="sidebar-link" href="prompts">Prompts</a>
            <a class="sidebar-link" href="identity">Identity</a>
            <a class="sidebar-link" href="personas">Personas</a>
            <a class="sidebar-link" href="chats">Chats</a>
            <a class="sidebar-link" href="memory">Memory</a>
            <a class="sidebar-link" href="automation">Automation</a>
          </div>
        </section>
        <section>
          <div class="sidebar-section-title">Diagnostics</div>
          <div class="sidebar-links">
            <a class="sidebar-link" href="runtime-control">
              Runtime Control
            </a>
            <a class="sidebar-link" href="sessions">Runtime Sessions</a>
            <a class="sidebar-link" href="debug">Debug</a>
            <a class="sidebar-link" href="browser">Browser</a>
          </div>
        </section>
        <section>
          <div class="sidebar-section-title">Admin</div>
          <div class="sidebar-links">
            <a class="sidebar-link" href="{{.ChatLink}}">
              Chat
            </a>
            <a class="sidebar-link active" href="{{.ChannelsLink}}">
              Channels
            </a>
          </div>
        </section>
      </nav>
    </aside>
    <main>
      <div class="page-wrap">
        <header class="page-header">
          <p class="page-kicker">TRPC-CLAW admin</p>
          <h1>Channels</h1>
          <p class="subtle">
            Shared runtime channel management for saved config, QR login,
            and transport-specific status.
          </p>
          <div class="page-toolbar">
            <div class="page-toolbar-copy">
              <div class="page-toolbar-updated">
                Updated {{.GeneratedAt}}
              </div>
              <div class="page-toolbar-note">
                This page watches for newer runtime state without
                interrupting your reading or editing.
              </div>
            </div>
            <a class="page-refresh-link" href="{{.ChannelsLink}}">
              Refresh page
            </a>
          </div>
        </header>

        {{if .Notice}}<div class="notice ok">{{.Notice}}</div>{{end}}
        {{if .Error}}<div class="notice err">{{.Error}}</div>{{end}}

        <div class="config-sections">
          <article class="card">
            <h2>Configured Channels</h2>
            <p class="subtle section-copy">
              Saved config still lives in the main runtime YAML. Use
              Config for field edits and this page for runtime-facing
              channel operations.
            </p>
            <p class="channel-help">
              To switch a channel transport, open its
              <code>Type</code> field in
              <a href="{{.ConfigLink}}">Config</a>, save the new
              value such as
              <code>weixin</code>, then use
              <a href="{{.RuntimeLink}}">
                Runtime Control
              </a>
              to restart.
            </p>
            {{if .ConfigPath}}
            <dl class="meta card-copy">
              <dt>Config Path</dt>
              <dd><code>{{.ConfigPath}}</code></dd>
            </dl>
            <p class="subtle card-copy">
              Open
              <a href="{{.StatusLink}}">JSON status</a>.
            </p>
            {{end}}
            {{if .Configured}}
            <div class="panels">
              {{range .Configured}}
              <article class="config-section-card">
                <div class="config-field-top">
                  <div>
                    <h3>{{.Title}}</h3>
                    {{if .Summary}}
                    <p class="subtle section-copy">{{.Summary}}</p>
                    {{end}}
                  </div>
                  <div class="config-badges">
                    <span class="config-badge {{.StateClass}}">
                      {{.StateLabel}}
                    </span>
                  </div>
                </div>
                <div class="config-meta">
                  <div class="config-meta-block">
                    <div class="config-meta-label">Type</div>
                    <div class="config-meta-value">{{.Type}}</div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">Name</div>
                    <div class="config-meta-value">
                      {{if .Name}}{{.Name}}{{else}}-{{end}}
                    </div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">Runtime</div>
                    <div class="config-meta-value">
                      {{if .RuntimeHint}}{{.RuntimeHint}}{{else}}-{{end}}
                    </div>
                  </div>
                </div>
                <p class="channel-help">
                  Switch this channel in
                  <a href="{{.TypeLink}}">Config → Type</a>.
                  After saving, restart from
                  <a href="{{$.RuntimeLink}}">
                    Runtime Control
                  </a>.
                </p>
                <div class="actions card-copy">
                  <a href="{{.TypeLink}}">Edit Type</a>
                  <a href="{{.ConfigLink}}">Open Config</a>
                  <a href="{{$.RuntimeLink}}">
                    Open Runtime Control
                  </a>
                </div>
              </article>
              {{end}}
            </div>
            {{else}}
            <p class="empty">
              No configured channels were found in the source runtime
              config.
            </p>
            {{end}}
          </article>

          {{if .Weixin}}
          <article class="card">
            <h2>Weixin Runtime</h2>
            <p class="subtle section-copy">
              QR login, account resume or remove, and live account state
              stay on the shared admin surface instead of a separate
              Weixin-only UI.
            </p>
            <div class="runtime-row">
              {{range .Weixin}}
              <section class="config-section-card" id="{{.Anchor}}">
                <h3>{{.Title}}</h3>
                <div class="config-meta">
                  <div class="config-meta-block">
                    <div class="config-meta-label">State Dir</div>
                    <div class="config-meta-value">
                      <code>{{.StateDir}}</code>
                    </div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">Default Base URL</div>
                    <div class="config-meta-value">
                      <code>{{.DefaultBaseURL}}</code>
                    </div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">Accounts</div>
                    <div class="config-meta-value">{{.AccountCount}}</div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">Updated</div>
                    <div class="config-meta-value">{{.GeneratedAt}}</div>
                  </div>
                </div>

                <div class="panels">
                  <article class="config-section-card">
                    <h4>QR Login</h4>
                    <form
                      method="post"
                      action="{{$.WeixinStartAction}}"
                    >
                      <input
                        type="hidden"
                        name="runtime_key"
                        value="{{.Key}}"
                      >
                      <div class="field">
                        <label for="{{.Key}}-base-url">Base URL</label>
                        <input
                          id="{{.Key}}-base-url"
                          name="base_url"
                          type="text"
                          value="{{.DefaultBaseURL}}"
                        >
                      </div>
                      <div class="field">
                        <label for="{{.Key}}-bot-type">Bot Type</label>
                        <input
                          id="{{.Key}}-bot-type"
                          name="bot_type"
                          type="text"
                          value="{{.DefaultBotType}}"
                        >
                      </div>
                      <div class="actions card-copy">
                        <button type="submit">Start QR Login</button>
                        <a
                          href="{{.QREntryLink}}"
                          target="_blank"
                          rel="noreferrer"
                        >
                          Open QR Entry
                        </a>
                        {{if .ConfigSectionLink}}
                        <a href="{{.ConfigSectionLink}}">
                          Open Config
                        </a>
                        {{end}}
                      </div>
                      {{if .ConfigHint}}
                      <p class="field-help">{{.ConfigHint}}</p>
                      {{end}}
                    </form>

                    {{if .Login.Exists}}
                    <div class="card-copy">
                      <div class="config-badges">
                        <span class="config-badge {{.Login.StatusClass}}">
                          {{.Login.StatusLabel}}
                        </span>
                      </div>
                      <dl class="meta" style="margin-top: 12px;">
                        <dt>Started</dt>
                        <dd>{{.Login.StartedAt}}</dd>
                        <dt>Updated</dt>
                        <dd>{{.Login.UpdatedAt}}</dd>
                        {{if .Login.CompletedAt}}
                        <dt>Completed</dt>
                        <dd>{{.Login.CompletedAt}}</dd>
                        {{end}}
                        {{if .Login.BaseURL}}
                        <dt>Base URL</dt>
                        <dd><code>{{.Login.BaseURL}}</code></dd>
                        {{end}}
                        {{if .Login.BotType}}
                        <dt>Bot Type</dt>
                        <dd><code>{{.Login.BotType}}</code></dd>
                        {{end}}
                        {{if .Login.SavedAccountID}}
                        <dt>Saved Account</dt>
                        <dd><code>{{.Login.SavedAccountID}}</code></dd>
                        {{end}}
                      </dl>
                      <div class="actions card-copy">
                        {{if .Login.QRCodeURL}}
                        <a
                          href="{{.Login.QRCodeURL}}"
                          target="_blank"
                          rel="noreferrer"
                        >
                          Open the latest QR page
                        </a>
                        {{end}}
                        {{if .Login.Active}}
                        <form
                          method="post"
                          action="{{$.WeixinCancelAction}}"
                        >
                          <input
                            type="hidden"
                            name="runtime_key"
                            value="{{.Key}}"
                          >
                          <button class="secondary" type="submit">
                            Cancel Login
                          </button>
                        </form>
                        {{end}}
                      </div>
                      {{if .Login.Error}}
                      <div class="notice err">{{.Login.Error}}</div>
                      {{end}}
                    </div>
                    {{end}}
                  </article>

                  <article class="config-section-card">
                    <h4>Accounts</h4>
                    {{if .LoadError}}
                    <div class="notice err">{{.LoadError}}</div>
                    {{else if .Accounts}}
                    {{$runtime := .}}
                    <div class="account-list">
                      {{range .Accounts}}
                      <article class="account-card">
                        <div class="config-field-top">
                          <div>
                            <h5 class="account-title">
                              <code>{{.AccountID}}</code>
                            </h5>
                            <div class="account-note">
                              Peers: {{.ContextPeerCount}}
                            </div>
                          </div>
                          <div class="config-badges">
                            <span class="config-badge {{.StateClass}}">
                              {{.StateLabel}}
                            </span>
                          </div>
                        </div>
                        <div class="config-meta">
                          <div class="config-meta-block">
                            <div class="config-meta-label">User</div>
                            <div class="config-meta-value">
                              {{if .UserID}}
                              <code>{{.UserID}}</code>
                              {{else}}
                              <span class="subtle">Unknown</span>
                              {{end}}
                            </div>
                          </div>
                          <div class="config-meta-block">
                            <div class="config-meta-label">Base URL</div>
                            <div class="config-meta-value">
                              {{if .BaseURL}}
                              <code>{{.BaseURL}}</code>
                              {{else}}
                              <span class="subtle">-</span>
                              {{end}}
                            </div>
                          </div>
                          <div class="config-meta-block">
                            <div class="config-meta-label">Activity</div>
                            <div class="config-meta-value">
                              Inbound: {{.LastInboundAt}}<br>
                              Outbound: {{.LastOutboundAt}}<br>
                              Event: {{.LastEventAt}}
                            </div>
                          </div>
                          <div class="config-meta-block">
                            <div class="config-meta-label">Last Error</div>
                            <div class="config-meta-value">
                              {{if .LastError}}
                              {{.LastError}}
                              {{else}}
                              <span class="subtle">None</span>
                              {{end}}
                            </div>
                          </div>
                          {{if .PauseRemaining}}
                          <div class="config-meta-block">
                            <div class="config-meta-label">
                              Pause Remaining
                            </div>
                            <div class="config-meta-value">
                              {{.PauseRemaining}}
                            </div>
                          </div>
                          {{end}}
                        </div>
                        <div class="actions card-copy">
                          {{if .CanResume}}
                          <form
                            class="inline-form"
                            method="post"
                            action="{{$.WeixinResumeAction}}"
                          >
                            <input
                              type="hidden"
                              name="runtime_key"
                              value="{{$runtime.Key}}"
                            >
                            <input
                              type="hidden"
                              name="account_id"
                              value="{{.AccountID}}"
                            >
                            <button class="secondary" type="submit">
                              Resume Now
                            </button>
                          </form>
                          {{end}}
                          <form
                            class="inline-form"
                            method="post"
                            action="{{$.WeixinRemoveAction}}"
                          >
                            <input
                              type="hidden"
                              name="runtime_key"
                              value="{{$runtime.Key}}"
                            >
                            <input
                              type="hidden"
                              name="account_id"
                              value="{{.AccountID}}"
                            >
                            <button class="danger" type="submit">
                              Remove
                            </button>
                          </form>
                        </div>
                      </article>
                      {{end}}
                    </div>
                    {{else}}
                    <p class="empty">
                      No Weixin accounts are saved under this runtime yet.
                    </p>
                    {{end}}
                  </article>
                </div>
              </section>
              {{end}}
            </div>
          </article>
          {{end}}

          {{if .WeCom}}
          <article class="card">
            <h2>WeCom Runtime</h2>
            <p class="subtle section-copy">
              Shared WeCom transport status is grouped here while deeper
              chat and prompt controls stay on their existing pages.
            </p>
            <div class="panels">
              {{range .WeCom}}
              <article class="config-section-card">
                <div class="config-field-top">
                  <div>
                    <h3 id="{{.Anchor}}">{{.Title}}</h3>
                  </div>
                </div>
                {{if .LoadError}}
                <div class="notice err">{{.LoadError}}</div>
                {{end}}
                <div class="config-meta">
                  <div class="config-meta-block">
                    <div class="config-meta-label">State Dir</div>
                    <div class="config-meta-value">
                      <code>{{.StateDir}}</code>
                    </div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">Bot Mode</div>
                    <div class="config-meta-value">{{.BotMode}}</div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">Transport</div>
                    <div class="config-meta-value">
                      {{.ConnectionMode}}
                    </div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">Callback</div>
                    <div class="config-meta-value">
                      {{.CallbackPath}}
                    </div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">Chat Policy</div>
                    <div class="config-meta-value">{{.ChatPolicy}}</div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">Runtime Admin</div>
                    <div class="config-meta-value">
                      {{.RuntimeAdmin}}
                    </div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">User Label Mode</div>
                    <div class="config-meta-value">
                      {{.UserLabelMode}}
                    </div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">Tracked Chats</div>
                    <div class="config-meta-value">
                      {{.ChatSummary.TrackedChats}}
                    </div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">Direct / Group</div>
                    <div class="config-meta-value">
                      {{.ChatSummary.DirectChats}} /
                      {{.ChatSummary.GroupChats}}
                    </div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">Group User</div>
                    <div class="config-meta-value">
                      {{.ChatSummary.GroupUserChats}}
                    </div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">Workspace Chats</div>
                    <div class="config-meta-value">
                      {{.ChatSummary.WorkspaceChats}}
                    </div>
                  </div>
                  <div class="config-meta-block">
                    <div class="config-meta-label">Last Activity</div>
                    <div class="config-meta-value">
                      {{.ChatSummary.LastActivity}}
                    </div>
                  </div>
                </div>
                {{if .ActivationHint}}
                <p class="field-help">{{.ActivationHint}}</p>
                {{end}}
                {{if .RuntimeKey}}
                <form
                  class="stack-form"
                  method="post"
                  action="{{$.WeComActivateAction}}"
                >
                  <input
                    type="hidden"
                    name="runtime_key"
                    value="{{.RuntimeKey}}"
                  >
                  <input
                    type="hidden"
                    name="scene"
                    value="admin_manual"
                  >
                  <input
                    type="hidden"
                    name="return_path"
                    value="{{.ActivateReturnPath}}"
                  >
                  <div class="field">
                    <label>WeCom User ID</label>
                    <input
                      type="text"
                      name="wecom_user_id"
                      value="{{.DefaultWeComUserID}}"
                      placeholder="{{.ActivatePlaceholder}}"
                      {{if not .Activation.Available}}disabled{{end}}
                    >
                  </div>
                  <div class="actions card-copy">
                    <button
                      type="submit"
                      {{if not .Activation.Available}}disabled{{end}}
                    >
                      Send Activation
                    </button>
                  </div>
                </form>
                {{if or .DebugSend.Supported .DebugSendHint}}
                <form
                  class="stack-form"
                  method="post"
                  action="{{$.WeComDebugSendAction}}"
                >
                  <input
                    type="hidden"
                    name="runtime_key"
                    value="{{.RuntimeKey}}"
                  >
                  <input
                    type="hidden"
                    name="return_path"
                    value="{{.DebugSendReturnPath}}"
                  >
                  <div class="field">
                    <label>Debug Target</label>
                    <input
                      type="text"
                      name="target"
                      value="{{.DebugSendTarget}}"
                      placeholder="{{.DebugSendPlaceholder}}"
                      {{if not .DebugSend.Available}}disabled{{end}}
                    >
                  </div>
                  <div class="field">
                    <label>Text</label>
                    <textarea
                      name="text"
                      {{if not .DebugSend.Available}}disabled{{end}}
                    ></textarea>
                  </div>
                  <div class="field">
                    <label>Local File Path</label>
                    <input
                      type="text"
                      name="file_path"
                      placeholder="/workspace/out/screenshot.png"
                      {{if not .DebugSend.Available}}disabled{{end}}
                    >
                  </div>
                  <div class="field">
                    <label>Uploaded File Name</label>
                    <input
                      type="text"
                      name="file_name"
                      placeholder="optional display name"
                      {{if not .DebugSend.Available}}disabled{{end}}
                    >
                  </div>
                  <label class="checkbox-row">
                    <input
                      type="checkbox"
                      name="as_voice"
                      value="true"
                      {{if not .DebugSend.Available}}disabled{{end}}
                    >
                    Send compatible .amr as voice
                  </label>
                  {{if .DebugSendHint}}
                  <p class="field-help">{{.DebugSendHint}}</p>
                  {{end}}
                  <div class="actions card-copy">
                    <button
                      type="submit"
                      {{if not .DebugSend.Available}}disabled{{end}}
                    >
                      Send Debug Message
                    </button>
                  </div>
                </form>
                {{end}}
                {{end}}
                <div class="actions card-copy">
                  <a href="{{$.ChatsLink}}">Open Chats</a>
                  <a href="{{$.PromptsLink}}">Open Prompts</a>
                  <a href="{{.ConfigSectionLink}}">
                    Open Config
                  </a>
                </div>
              </article>
              {{end}}
            </div>
          </article>
          {{end}}
        </div>
      </div>
    </main>
  </div>
` + adminSidebarRevealScriptHTML + `
</body>
</html>
`

type channelsAdminService struct {
	provider       *runtimeAdminProvider
	weixin         *weixinAdminService
	wecomActivate  *wecomActivateAdminService
	wecomDebugSend *wecomDebugSendAdminService
}

type channelsAdminPageData struct {
	Notice string `json:"notice,omitempty"`
	Error  string `json:"error,omitempty"`

	ConfigPath  string `json:"config_path,omitempty"`
	GeneratedAt string `json:"generated_at,omitempty"`

	AutoRefresh       bool  `json:"auto_refresh"`
	RefreshIntervalMS int64 `json:"refresh_interval_ms"`

	Configured []channelsConfiguredChannelView `json:"configured,omitempty"`
	Weixin     []channelsWeixinRuntimeView     `json:"weixin,omitempty"`
	WeCom      []channelsWeComRuntimeView      `json:"wecom,omitempty"`

	ChannelsLink         string `json:"-"`
	StatusLink           string `json:"-"`
	ConfigLink           string `json:"-"`
	RuntimeLink          string `json:"-"`
	ChatLink             string `json:"-"`
	ChatsLink            string `json:"-"`
	PromptsLink          string `json:"-"`
	WeixinStartAction    string `json:"-"`
	WeixinCancelAction   string `json:"-"`
	WeixinResumeAction   string `json:"-"`
	WeixinRemoveAction   string `json:"-"`
	WeComActivateAction  string `json:"-"`
	WeComDebugSendAction string `json:"-"`
}

type channelsConfiguredChannelView struct {
	SectionKey  string `json:"section_key"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	StateLabel  string `json:"state_label"`
	StateClass  string `json:"state_class"`
	Summary     string `json:"summary,omitempty"`
	RuntimeHint string `json:"runtime_hint,omitempty"`
	ConfigLink  string `json:"config_link"`
	TypeLink    string `json:"type_link"`
}

type channelsWeixinRuntimeView struct {
	weixinAdminRuntimeView
	ConfigSectionKey  string `json:"config_section_key,omitempty"`
	ConfigHint        string `json:"config_hint,omitempty"`
	ConfigSectionLink string `json:"-"`
	QREntryLink       string `json:"-"`
}

type channelsWeComRuntimeView struct {
	Title  string `json:"title"`
	Anchor string `json:"anchor,omitempty"`

	ConfigSectionKey  string `json:"config_section_key,omitempty"`
	ConfigSectionLink string `json:"-"`

	StateDir       string `json:"state_dir,omitempty"`
	BotMode        string `json:"bot_mode,omitempty"`
	ConnectionMode string `json:"connection_mode,omitempty"`
	CallbackPath   string `json:"callback_path,omitempty"`
	ChatPolicy     string `json:"chat_policy,omitempty"`

	RuntimeAdmin string `json:"runtime_admin_policy,omitempty"`

	UserLabelMode string `json:"user_label_mode,omitempty"`
	LoadError     string `json:"load_error,omitempty"`
	RuntimeKey    string `json:"runtime_key,omitempty"`

	DefaultWeComUserID string `json:"default_wecom_user_id,omitempty"`

	Activation     wecomActivateStatusView `json:"activation"`
	ActivationHint string                  `json:"activation_hint,omitempty"`
	DebugSend      wecomActivateStatusView `json:"debug_send"`
	DebugSendHint  string                  `json:"debug_send_hint,omitempty"`

	ActivateReturnPath   string `json:"-"`
	ActivatePlaceholder  string `json:"-"`
	DebugSendReturnPath  string `json:"-"`
	DebugSendTarget      string `json:"-"`
	DebugSendPlaceholder string `json:"-"`

	ChatSummary channelsWeComChatSummaryView `json:"chat_summary"`
}

type channelsWeComChatSummaryView struct {
	TrackedChats   int    `json:"tracked_chats"`
	DirectChats    int    `json:"direct_chats"`
	GroupChats     int    `json:"group_chats"`
	GroupUserChats int    `json:"group_user_chats"`
	WorkspaceChats int    `json:"workspace_chats"`
	LastActivity   string `json:"last_activity,omitempty"`
}

func newChannelsAdminService(
	provider *runtimeAdminProvider,
	weixin *weixinAdminService,
	wecomActivate *wecomActivateAdminService,
	wecomDebugSendOpt ...*wecomDebugSendAdminService,
) *channelsAdminService {
	var wecomDebugSend *wecomDebugSendAdminService
	if len(wecomDebugSendOpt) > 0 {
		wecomDebugSend = wecomDebugSendOpt[0]
	}
	if provider == nil && weixin == nil &&
		wecomActivate == nil && wecomDebugSend == nil {
		return nil
	}
	return &channelsAdminService{
		provider:       provider,
		weixin:         weixin,
		wecomActivate:  wecomActivate,
		wecomDebugSend: wecomDebugSend,
	}
}

func collectRuntimeWeComAdminTargets(
	channels []occhannel.Channel,
) []wecomchannel.AdminTarget {
	targets := make([]wecomchannel.AdminTarget, 0, len(channels))
	seen := make(map[string]struct{}, len(channels))
	for _, ch := range channels {
		provider, ok := ch.(wecomAdminTargetProvider)
		if !ok || provider == nil {
			continue
		}
		target := provider.WeComAdminTarget()
		key := buildWeComRuntimeIdentity(target)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		targets = append(targets, target)
	}
	sort.SliceStable(targets, func(i, j int) bool {
		left := strings.TrimSpace(targets[i].Name)
		right := strings.TrimSpace(targets[j].Name)
		if left == right {
			return targets[i].StateDir < targets[j].StateDir
		}
		return left < right
	})
	return targets
}

func (s *channelsAdminService) snapshot() channelsAdminPageData {
	out := channelsAdminPageData{
		GeneratedAt: time.Now().Format(channelsAdminTimeLayout),
		RefreshIntervalMS: int64(
			weixinAdminRefreshInterval / time.Millisecond,
		),
	}
	if s == nil {
		return out
	}

	runtimeTargets := []runtimeChannelTarget{}
	runtimeSectionKeys := map[int]string{}
	sectionTitles := map[string]string{}
	if s.provider != nil {
		out.ConfigPath = strings.TrimSpace(s.provider.sourceConfigPath)
		runtimeTargets = s.provider.channelTargets()
		entries, err := s.provider.configuredChannelEntries()
		if err != nil {
			out.Error = strings.TrimSpace(err.Error())
		} else {
			matches := matchConfiguredRuntimeChannels(
				entries,
				runtimeTargets,
			)
			out.Configured = buildConfiguredChannelViews(
				entries,
				matches,
			)
			for _, view := range out.Configured {
				sectionKey := strings.TrimSpace(view.SectionKey)
				if sectionKey == "" {
					continue
				}
				sectionTitles[sectionKey] = strings.TrimSpace(
					view.Title,
				)
			}
			runtimeSectionKeys = configuredRuntimeSectionKeys(
				entries,
				runtimeTargets,
			)
		}
	}

	if s.weixin != nil {
		weixinData := s.weixin.snapshot()
		out.AutoRefresh = weixinData.AutoRefresh
		out.Weixin = make(
			[]channelsWeixinRuntimeView,
			0,
			len(weixinData.Runtimes),
		)
		weixinSections := weixinRuntimeConfigSections(
			runtimeTargets,
			runtimeSectionKeys,
		)
		for _, item := range weixinData.Runtimes {
			view := channelsWeixinRuntimeView{
				weixinAdminRuntimeView: item,
			}
			view.ConfigSectionKey = weixinSections[strings.TrimSpace(
				item.StateDir,
			)]
			if title := strings.TrimSpace(
				sectionTitles[view.ConfigSectionKey],
			); title != "" {
				view.Title = title
			}
			if strings.TrimSpace(view.ConfigSectionKey) == "" {
				view.ConfigHint = weixinImplicitConfigHint
			}
			out.Weixin = append(out.Weixin, view)
		}
	}

	out.WeCom = buildWeComRuntimeViews(
		runtimeTargets,
		runtimeSectionKeys,
		sectionTitles,
		s.wecomActivate,
		s.wecomDebugSend,
	)
	return out
}

func configuredRuntimeSectionKeys(
	entries []configuredChannelEntry,
	targets []runtimeChannelTarget,
) map[int]string {
	sections := make(map[int]string, len(entries))
	used := make(map[int]struct{}, len(targets))
	for i := range entries {
		entry := entries[i]
		index := findRuntimeChannelTarget(
			entry,
			targets,
			used,
			true,
		)
		if index < 0 {
			index = findRuntimeChannelTarget(
				entry,
				targets,
				used,
				false,
			)
		}
		if index < 0 {
			continue
		}
		used[index] = struct{}{}
		sections[index] = entry.SectionKey
	}
	return sections
}

func weixinRuntimeConfigSections(
	targets []runtimeChannelTarget,
	sections map[int]string,
) map[string]string {
	out := make(map[string]string, len(sections))
	for index, target := range targets {
		if target.Weixin == nil {
			continue
		}
		stateDir := strings.TrimSpace(target.Weixin.StateDir)
		if stateDir == "" {
			continue
		}
		sectionKey := strings.TrimSpace(sections[index])
		if sectionKey == "" {
			continue
		}
		out[stateDir] = sectionKey
	}
	return out
}

func (p *runtimeAdminProvider) configuredChannelEntries() (
	[]configuredChannelEntry,
	error,
) {
	if p == nil {
		return nil, nil
	}
	rawRoot, _, err := loadPromptConfigRoots(
		p.sourceConfigPath,
		p.args,
		p.stateDir,
	)
	if err != nil {
		return nil, err
	}
	return collectConfiguredChannelEntries(
		rawRoot,
		runtimeConfigEnvLookup(p.stateDir),
	)
}

func buildConfiguredChannelViews(
	entries []configuredChannelEntry,
	matches map[string]*runtimeChannelTarget,
) []channelsConfiguredChannelView {
	views := make([]channelsConfiguredChannelView, 0, len(entries))
	for _, entry := range entries {
		stateLabel, stateClass, runtimeHint :=
			describeConfiguredChannelState(
				entry,
				matches[entry.Key],
			)
		views = append(views, channelsConfiguredChannelView{
			SectionKey: entry.SectionKey,
			Title: channelSectionTitle(
				entry.Index,
				channelTypeLabel(entry.Type),
				entry.Name,
			),
			Type:        entry.Type,
			Name:        entry.Name,
			StateLabel:  stateLabel,
			StateClass:  stateClass,
			Summary:     configuredChannelSummary(entry),
			RuntimeHint: runtimeHint,
			ConfigLink: channelConfigPagePath + "#config-section-" +
				entry.SectionKey,
			TypeLink: channelConfigPagePath + "#config-field-" +
				channelConfigFieldKey(entry.Index, channelTypeKey),
		})
	}
	return views
}

func describeConfiguredChannelState(
	entry configuredChannelEntry,
	target *runtimeChannelTarget,
) (string, string, string) {
	if !entry.Enabled {
		if target != nil {
			return "Disabled", channelCardStateDisabled,
				"A matching channel is still loaded now, but " +
					"config disables it for the next restart."
		}
		return "Disabled", channelCardStateDisabled,
			"This channel will stay out of the next runtime " +
				"start until it is re-enabled."
	}
	if len(entry.MissingEnabledEnv) > 0 {
		missing := strings.Join(entry.MissingEnabledEnv, ", ")
		if target != nil {
			return "Waiting For Env", channelCardStateWaitingForEnv,
				"A matching channel is still loaded now, but " +
					"the next restart will wait for env vars: " +
					missing + "."
		}
		return "Waiting For Env", channelCardStateWaitingForEnv,
			"This channel is configured, but the next " +
				"restart is waiting for env vars: " +
				missing + "."
	}
	if target != nil {
		return "Enabled", channelCardStateEnabled,
			"A matching channel is loaded in the current " +
				"runtime."
	}
	return "Restart Required", channelCardStateRestartRequired,
		"Required env vars are already ready for the next " +
			"restart, but no matching live channel was found."
}

func channelTypeLabel(typeName string) string {
	switch strings.TrimSpace(typeName) {
	case channelTypeWeixin:
		return "Weixin"
	case channelTypeWeCom:
		return "WeCom"
	default:
		return strings.TrimSpace(typeName)
	}
}

func configuredChannelSummary(entry configuredChannelEntry) string {
	switch strings.TrimSpace(entry.Type) {
	case channelTypeWeixin:
		baseURL := firstNonEmptyString(
			mappingStringValue(entry.ConfigNode, channelFieldBaseURL),
			weixinDefaultBaseURLConfigValue,
		)
		return "Base URL " + baseURL
	case channelTypeWeCom:
		botMode := firstNonEmptyString(
			mappingStringValue(entry.ConfigNode, wecomConfigBotModeKey),
			"notification",
		)
		connectionMode := firstNonEmptyString(
			mappingStringValue(
				entry.ConfigNode,
				wecomConfigConnectionModeKey,
			),
			"webhook",
		)
		callbackPath := firstNonEmptyString(
			mappingStringValue(entry.ConfigNode, channelFieldCallbackPath),
			wecomDefaultCallbackPathConfigValue,
		)
		return fmt.Sprintf(
			"bot_mode=%s, connection=%s, callback=%s",
			botMode,
			connectionMode,
			callbackPath,
		)
	default:
		return ""
	}
}

func buildWeComRuntimeViews(
	targets []runtimeChannelTarget,
	sections map[int]string,
	sectionTitles map[string]string,
	activation *wecomActivateAdminService,
	debugSend *wecomDebugSendAdminService,
) []channelsWeComRuntimeView {
	views := make([]channelsWeComRuntimeView, 0, len(targets))
	for index, target := range targets {
		if target.WeCom == nil {
			continue
		}
		runtimeIdentity := buildWeComRuntimeIdentity(*target.WeCom)
		view := channelsWeComRuntimeView{
			Title: channelRuntimeTitle(
				"WeCom Runtime",
				target.Name,
			),
			Anchor:           wecomActivateAnchor(runtimeIdentity),
			ConfigSectionKey: strings.TrimSpace(sections[index]),
			StateDir:         strings.TrimSpace(target.WeCom.StateDir),
			BotMode:          strings.TrimSpace(target.WeCom.BotMode),
			ConnectionMode:   strings.TrimSpace(target.WeCom.ConnectionMode),
			CallbackPath:     strings.TrimSpace(target.WeCom.CallbackPath),
			ChatPolicy:       strings.TrimSpace(target.WeCom.ChatPolicy),
			RuntimeAdmin: strings.TrimSpace(
				target.WeCom.RuntimeAdminPolicy,
			),
			UserLabelMode:       strings.TrimSpace(target.WeCom.UserLabelMode),
			ActivatePlaceholder: wecomActivateUserIDPlaceholder,
			DebugSendPlaceholder: "single:T12345678 or " +
				"group:chatid",
		}
		if activationView, ok := activation.runtimeViewByIdentity(
			runtimeIdentity,
		); ok {
			view.RuntimeKey = strings.TrimSpace(
				activationView.RuntimeKey,
			)
			view.DefaultWeComUserID = strings.TrimSpace(
				activationView.DefaultWeComUserID,
			)
			view.Anchor = wecomActivateAnchor(view.RuntimeKey)
			view.Activation = activationView.Activation
			view.ActivationHint = describeWeComActivateStatus(
				activationView.Activation,
			)
			view.ActivateReturnPath = channelsAdminPagePath +
				"#" + view.Anchor
		}
		if debugView, ok := debugSend.runtimeViewByIdentity(
			runtimeIdentity,
		); ok {
			view.DebugSend = debugView.Send
			view.DebugSendTarget = strings.TrimSpace(
				debugView.DefaultTarget,
			)
			view.DebugSendReturnPath = channelsAdminPagePath +
				"#" + view.Anchor
			view.DebugSendHint = describeWeComDebugSendStatus(
				debugView.Send,
			)
		}
		if title := strings.TrimSpace(
			sectionTitles[view.ConfigSectionKey],
		); title != "" {
			view.Title = title
		}
		summary, err := wecomchannel.BuildAdminChatSummary(
			target.WeCom.StateDir,
		)
		if err != nil {
			view.LoadError = strings.TrimSpace(err.Error())
		} else {
			view.ChatSummary = channelsWeComChatSummaryView{
				TrackedChats:   summary.TrackedChats,
				DirectChats:    summary.DirectChats,
				GroupChats:     summary.GroupChats,
				GroupUserChats: summary.GroupUserChats,
				WorkspaceChats: summary.WorkspaceChats,
				LastActivity: formatAdminTimePtr(
					summary.LastActivity,
				),
			}
		}
		views = append(views, view)
	}
	return views
}

func describeWeComDebugSendStatus(
	status wecomActivateStatusView,
) string {
	if status.Available {
		return ""
	}
	switch strings.TrimSpace(status.Reason) {
	case "":
		return ""
	case wecomActivateReasonAIModeRequired:
		return "Debug send requires `bot_mode=ai`."
	case wecomActivateReasonWebSocketMode:
		return "Debug send requires `connection_mode=websocket`."
	case wecomActivateReasonNotConnected:
		return "Debug send requires a live WeCom websocket " +
			"connection."
	default:
		return "Debug send is not available for the current " +
			"WeCom runtime."
	}
}

func channelRuntimeTitle(prefix string, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return prefix
	}
	return prefix + " · " + name
}

func wrapChannelsAdminHandler(
	base http.Handler,
	service *channelsAdminService,
) http.Handler {
	if service == nil {
		return base
	}

	mux := http.NewServeMux()
	mux.HandleFunc(
		channelsAdminPagePath,
		service.handlePage,
	)
	mux.HandleFunc(
		channelsAdminStatusPath,
		service.handleStatusJSON,
	)
	if base != nil {
		mux.Handle("/", injectChannelsAdminNav(base))
	}
	return mux
}

func (s *channelsAdminService) handlePage(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data := s.snapshot()
	data.Notice = strings.TrimSpace(
		r.URL.Query().Get(weixinAdminQueryNotice),
	)
	if data.Error == "" {
		data.Error = strings.TrimSpace(
			r.URL.Query().Get(weixinAdminQueryError),
		)
	}
	data.applyRelativeAdminLinks(r.URL.Path)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := channelsAdminPageTemplate.Execute(w, data); err != nil {
		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
	}
}

func (s *channelsAdminService) handleStatusJSON(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeRuntimeJSON(w, http.StatusOK, s.snapshot())
}

func wrapOpenClawAdminHandler(
	base http.Handler,
	manager *runtimectl.Manager,
	adminChat *adminWebChatService,
	channels *channelsAdminService,
	weixin *weixinAdminService,
	wecomActivate *wecomActivateAdminService,
	wecomDebugSend *wecomDebugSendAdminService,
) http.Handler {
	handler := wrapAdminWebChatHandler(
		wrapWeComDebugSendAdminHandler(
			wrapWeComActivateAdminHandler(
				wrapChannelsAdminHandler(
					wrapWeixinAdminHandler(
						wrapRuntimeAdminHandler(base, manager),
						weixin,
					),
					channels,
				),
				wecomActivate,
			),
			wecomDebugSend,
		),
		adminChat,
	)
	return handler
}

type adminCaptureResponseWriter struct {
	header      http.Header
	body        bytes.Buffer
	statusCode  int
	wroteHeader bool
}

func newAdminCaptureResponseWriter() *adminCaptureResponseWriter {
	return &adminCaptureResponseWriter{
		header: make(http.Header),
	}
}

func (w *adminCaptureResponseWriter) Header() http.Header {
	return w.header
}

func (w *adminCaptureResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.statusCode = statusCode
	w.wroteHeader = true
}

func (w *adminCaptureResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(data)
}

func injectChannelsAdminNav(base http.Handler) http.Handler {
	if base == nil {
		return nil
	}
	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		capture := newAdminCaptureResponseWriter()
		base.ServeHTTP(capture, r)
		if !shouldInjectChannelsNav(capture) {
			writeCapturedAdminResponse(w, capture, nil)
			return
		}
		body := injectChannelsNavLink(
			capture.body.Bytes(),
			r.URL.Path,
		)
		writeCapturedAdminResponse(w, capture, body)
	})
}

func shouldInjectChannelsNav(
	capture *adminCaptureResponseWriter,
) bool {
	if capture == nil {
		return false
	}
	if capture.statusCode < http.StatusOK ||
		capture.statusCode >= http.StatusMultipleChoices {
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(
		capture.header.Get("Content-Type"),
	))
	if !strings.Contains(contentType, "text/html") {
		return false
	}
	return bytes.Contains(
		capture.body.Bytes(),
		[]byte(channelsNavInjectionMarker),
	)
}

func writeCapturedAdminResponse(
	w http.ResponseWriter,
	capture *adminCaptureResponseWriter,
	body []byte,
) {
	if capture == nil {
		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
		return
	}
	if body == nil {
		body = capture.body.Bytes()
	}
	for key, values := range capture.header {
		if strings.EqualFold(key, "Content-Length") {
			continue
		}
		dst := w.Header()
		for _, value := range values {
			dst.Add(key, value)
		}
	}
	statusCode := capture.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

func injectChannelsNavLink(
	body []byte,
	currentPath string,
) []byte {
	if len(body) == 0 {
		return body
	}
	body = injectChannelsNavSection(body, currentPath)
	body = injectChannelsAdminSidebarStyle(body)
	body = injectChannelsAdminSidebarScript(body)
	return body
}

func injectChannelsNavSection(
	body []byte,
	currentPath string,
) []byte {
	if bytes.Contains(
		body,
		[]byte(">"+channelsNavLabel+"</a>"),
	) {
		return body
	}
	channelsLink := template.HTMLEscapeString(
		adminRelativeReference(currentPath, channelsAdminPagePath),
	)
	chatLink := template.HTMLEscapeString(
		adminRelativeReference(currentPath, adminWebChatPagePath),
	)
	snippet := `<section>
          <div class="sidebar-section-title">Admin</div>
          <div class="sidebar-links">
            <a class="sidebar-link" href="` + chatLink + `">
              Chat
            </a>
            <a class="sidebar-link" href="` + channelsLink + `">
              Channels
            </a>
          </div>
        </section>
      </nav>`
	return bytes.Replace(
		body,
		[]byte(channelsNavInjectionMarker),
		[]byte(snippet),
		1,
	)
}

func injectChannelsAdminSidebarStyle(body []byte) []byte {
	if bytes.Contains(body, []byte("scrollbar-gutter: stable")) {
		return body
	}
	return bytes.Replace(
		body,
		[]byte(channelsStyleMarker),
		[]byte(adminSidebarInjectedStyleHTML+"\n  "+channelsStyleMarker),
		1,
	)
}

func injectChannelsAdminSidebarScript(body []byte) []byte {
	if bytes.Contains(body, []byte("openclaw.admin.pendingScroll")) {
		return body
	}
	return bytes.Replace(
		body,
		[]byte(channelsBodyMarker),
		[]byte(adminSidebarRevealScriptHTML+"\n"+channelsBodyMarker),
		1,
	)
}

func (d *channelsAdminPageData) applyRelativeAdminLinks(
	currentPath string,
) {
	if d == nil {
		return
	}

	d.ChannelsLink = adminRelativeReference(
		currentPath,
		channelsAdminPagePath,
	)
	d.StatusLink = adminRelativeReference(
		currentPath,
		channelsAdminStatusPath,
	)
	d.ConfigLink = adminRelativeReference(
		currentPath,
		channelConfigPagePath,
	)
	d.RuntimeLink = adminRelativeReference(
		currentPath,
		channelsRuntimeControlPath,
	)
	d.ChatLink = adminRelativeReference(
		currentPath,
		adminWebChatPagePath,
	)
	d.ChatsLink = adminRelativeReference(
		currentPath,
		channelsChatsPath,
	)
	d.PromptsLink = adminRelativeReference(
		currentPath,
		channelsPromptsPath,
	)
	d.WeixinStartAction = adminRelativeReference(
		currentPath,
		weixinAdminLoginStartPath,
	)
	d.WeixinCancelAction = adminRelativeReference(
		currentPath,
		weixinAdminLoginCancelPath,
	)
	d.WeixinResumeAction = adminRelativeReference(
		currentPath,
		weixinAdminAccountResumePath,
	)
	d.WeixinRemoveAction = adminRelativeReference(
		currentPath,
		weixinAdminAccountRemovePath,
	)
	d.WeComActivateAction = adminRelativeReference(
		currentPath,
		wecomActivateActionPath,
	)
	d.WeComDebugSendAction = adminRelativeReference(
		currentPath,
		wecomDebugSendActionPath,
	)

	for i := range d.Configured {
		d.Configured[i].ConfigLink = adminRelativeReference(
			currentPath,
			d.Configured[i].ConfigLink,
		)
		d.Configured[i].TypeLink = adminRelativeReference(
			currentPath,
			d.Configured[i].TypeLink,
		)
	}
	for i := range d.Weixin {
		sectionKey := strings.TrimSpace(d.Weixin[i].ConfigSectionKey)
		if sectionKey != "" {
			d.Weixin[i].ConfigSectionLink = adminRelativeReference(
				currentPath,
				channelConfigPagePath+"#config-section-"+sectionKey,
			)
		}
		d.Weixin[i].QREntryLink = weixinAdminQREntryLink(
			currentPath,
			d.Weixin[i].Key,
		)
	}
	for i := range d.WeCom {
		d.WeCom[i].ConfigSectionLink = adminRelativeReference(
			currentPath,
			channelConfigPagePath+"#config-section-"+
				d.WeCom[i].ConfigSectionKey,
		)
	}
}

func weixinAdminQREntryLink(
	currentPath string,
	runtimeKey string,
) string {
	target := weixinAdminQREntryPath
	runtimeKey = strings.TrimSpace(runtimeKey)
	if runtimeKey != "" {
		values := url.Values{}
		values.Set(weixinAdminFormRuntimeKey, runtimeKey)
		target += "?" + values.Encode()
	}
	return adminRelativeReference(currentPath, target)
}
