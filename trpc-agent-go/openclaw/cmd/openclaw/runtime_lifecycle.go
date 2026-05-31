package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	tlog "git.code.oa.com/trpc-go/trpc-go/log"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/releaseinfo"
	"git.woa.com/trpc-go/trpc-agent-go/openclaw/runtimectl"
	ocadmin "trpc.group/trpc-go/trpc-agent-go/openclaw/admin"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

const (
	runtimeAdminStatusPath    = "/api/runtime/status"
	runtimeAdminActionsPath   = "/api/runtime/actions"
	runtimeAdminVersionsPath  = "/api/runtime/versions"
	runtimeAdminChangelogPath = "/api/runtime/changelog"
	runtimeControlActionPath  = "/api/runtime/control/action"
	runtimeControlPagePath    = "/runtime-control"

	runtimeActionQueryError   = "error"
	runtimeActionQueryVersion = "version"

	runtimeActionFormKind          = "kind"
	runtimeActionFormMode          = "mode"
	runtimeActionFormTargetVersion = "target_version"
	runtimeActionFormReturnPath    = "return_path"
	runtimeActionFormReturnTo      = "return_to"

	runtimeAdminFetchTimeout    = 10 * time.Second
	runtimeChangelogSummarySize = 5
	runtimeActionCloseDelay     = 1200 * time.Millisecond
	runtimeActionPollInterval   = 2 * time.Second

	runtimeActionTitleDefault = "Runtime action requested"
	runtimeActionTitleRestart = "Restart requested"
	runtimeActionTitleUpgrade = "Upgrade requested"

	runtimeActionTargetRestart = "Keep current version"
	runtimeActionTargetLatest  = "Latest published version"
)

const runtimeActionAcceptedPage = `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
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
    main {
      width: 100%;
      padding: 32px 28px 40px;
    }
    .page-wrap {
      max-width: 1180px;
      margin: 0 auto;
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
    h1 {
      margin: 0 0 14px;
      font-size: 36px;
    }
    h2 {
      margin: 0 0 14px;
      font-size: 22px;
    }
    p, button, code {
      font-size: 15px;
      line-height: 1.5;
    }
    .subtle {
      color: var(--muted);
      max-width: 860px;
    }
    .notice {
      margin: 18px 0 0;
      padding: 12px 14px;
      border-radius: 14px;
      border: 1px solid var(--line);
      background: var(--panel-strong);
      box-shadow: var(--shadow);
    }
    .notice.ok {
      border-color: rgba(45, 109, 63, 0.3);
    }
    .panels {
      display: grid;
      gap: 18px;
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
    }
    .runtime-meta-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
      gap: 10px 16px;
      margin-top: 12px;
    }
    .runtime-meta-card {
      border-radius: 14px;
      background: rgba(243, 238, 231, 0.62);
      padding: 10px 12px;
    }
    .runtime-meta-label {
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }
    .runtime-meta-value {
      margin-top: 6px;
      line-height: 1.5;
      word-break: break-word;
    }
    .status-heading {
      margin-top: 6px;
      font-weight: 700;
      font-size: 18px;
    }
    .status-pill {
      display: inline-flex;
      align-items: center;
      border-radius: 999px;
      padding: 4px 10px;
      border: 1px solid rgba(45, 109, 63, 0.18);
      background: rgba(45, 109, 63, 0.08);
      color: var(--ok);
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }
    .status-copy {
      margin-top: 14px;
      font-size: 18px;
      font-weight: 600;
    }
    .status-hint {
      margin-top: 10px;
      color: var(--muted);
    }
    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-top: 18px;
    }
    form { margin: 0; }
    .actions button {
      padding: 8px 14px;
      border: 0;
      border-radius: 999px;
      background: var(--accent);
      color: white;
      cursor: pointer;
      font: inherit;
    }
    .actions button.secondary {
      background: #c9bca9;
      color: var(--ink);
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
    .page-refresh-link.disabled {
      opacity: 0.55;
      pointer-events: none;
      box-shadow: none;
    }
    a {
      color: var(--accent);
    }
    code {
      background: rgba(15, 111, 97, 0.08);
      padding: 2px 6px;
      border-radius: 8px;
      word-break: break-all;
    }
    @media (max-width: 760px) {
      main {
        padding: 24px 16px 32px;
      }
      h1 {
        font-size: 30px;
      }
    }
  </style>
</head>
<body>
  <main>
    <div class="page-wrap">
      <header class="page-header">
        <p class="page-kicker">TRPC-CLAW admin</p>
        <h1>{{.Title}}</h1>
        <p class="subtle">{{.Detail}}</p>
      </header>

      <section class="card">
        <h2>Action Summary</h2>
        <p class="subtle">{{.Summary}}</p>
        <div class="runtime-meta-grid">
          <div class="runtime-meta-card">
            <div class="runtime-meta-label">Action</div>
            <div class="runtime-meta-value">{{.ActionLabel}}</div>
          </div>
          <div class="runtime-meta-card">
            <div class="runtime-meta-label">Mode</div>
            <div class="runtime-meta-value">{{.ModeLabel}}</div>
          </div>
          <div class="runtime-meta-card">
            <div class="runtime-meta-label">Target</div>
            <div class="runtime-meta-value">{{.TargetLabel}}</div>
          </div>
        </div>
      </section>

      <div
        class="notice ok"
        id="runtime-action-status"
        data-action-id="{{.ActionID}}"
        data-return-url="{{.ReturnURL}}"
        data-status-url="{{.StatusURL}}"
        data-poll-ms="{{.PollIntervalMillis}}"
      >
        <div class="status-pill">Accepted</div>
        <div class="status-copy" id="runtime-action-progress">
          {{.ProgressLabel}}
        </div>
        <p class="status-hint" id="runtime-action-hint">
          This page will stay open while the runtime drains work,
          restarts, and becomes reachable again.
        </p>
      </div>

      <section class="panels">
        <article class="card">
          <h2>What Happens Next</h2>
          <p class="subtle">
            The confirmation page now stays in the same admin visual
            language instead of jumping to a generic browser error page.
          </p>
          <p class="subtle">
            If you are behind a proxy subpath, the probe keeps using the
            current relative admin path and waits until this lifecycle
            action is no longer pending.
          </p>
        </article>
        <article class="card">
          <h2>Runtime Control</h2>
          <p class="status-heading" id="runtime-action-open-label">
            Runtime Control is not ready yet.
          </p>
          <p class="subtle" id="runtime-action-open-hint">
            The button below will unlock once a newer runtime answers
            without reporting this action as still pending.
          </p>
          <div class="actions">
            <a
              class="page-refresh-link disabled"
              id="runtime-action-open"
              href="{{.ReturnURL}}"
              aria-disabled="true"
              tabindex="-1"
            >Open Runtime Control</a>
            <button
              class="secondary"
              type="button"
              id="runtime-action-retry"
            >Check again</button>
          </div>
        </article>
      </section>
    </div>
  </main>
  <script>
    (function() {
      const statusCard = document.getElementById(
        "runtime-action-status"
      );
      const progressNode = document.getElementById(
        "runtime-action-progress"
      );
      const hintNode = document.getElementById(
        "runtime-action-hint"
      );
      const openLabelNode = document.getElementById(
        "runtime-action-open-label"
      );
      const openHintNode = document.getElementById(
        "runtime-action-open-hint"
      );
      const openLink = document.getElementById(
        "runtime-action-open"
      );
      const retryButton = document.getElementById(
        "runtime-action-retry"
      );
      if (!statusCard || !progressNode || !hintNode) {
        return;
      }

      const actionID = statusCard.dataset.actionId || "";
      const returnURL = statusCard.dataset.returnUrl || "";
      const statusURL = statusCard.dataset.statusUrl || "";
      const pollMS = Number(statusCard.dataset.pollMs) || 2000;
      let timerID = 0;
      let attempt = 0;
      let running = false;
      let ready = false;

      const schedule = () => {
        window.clearTimeout(timerID);
        timerID = window.setTimeout(probe, pollMS);
      };

      const setWaiting = (message, hint) => {
        attempt += 1;
        progressNode.textContent = message;
        hintNode.textContent = hint;
        if (openLabelNode) {
          openLabelNode.textContent =
            "Runtime Control is not ready yet.";
        }
        if (openHintNode) {
          openHintNode.textContent = "Probe " + attempt +
            " is still waiting for the newer runtime to " +
            "answer without this action pending.";
        }
      };

      const setReady = () => {
        ready = true;
        progressNode.textContent =
          "Runtime Control is available again.";
        hintNode.textContent = "This lifecycle action is no " +
          "longer reported as pending. You can reopen " +
          "Runtime Control now.";
        if (openLabelNode) {
          openLabelNode.textContent =
            "Runtime Control is ready.";
        }
        if (openHintNode) {
          openHintNode.textContent = "Use the button below to " +
            "return when you are ready.";
        }
        if (openLink) {
          openLink.classList.remove("disabled");
          openLink.removeAttribute("aria-disabled");
          openLink.setAttribute("tabindex", "0");
        }
      };

      const probe = async () => {
        if (running || statusURL === "" || returnURL === "" || ready) {
          return;
        }
        running = true;
        let shouldRetry = true;
        try {
          const response = await fetch(statusURL, {
            method: "GET",
            cache: "no-store",
            credentials: "same-origin",
            headers: {
              "Accept": "application/json"
            }
          });
          if (response.ok) {
            let payload = {};
            try {
              payload = await response.json();
            } catch (err) {
              payload = {};
              void err;
            }
            const pending = payload && payload.pending ?
              payload.pending : {};
            const pendingID = pending && typeof pending.id === "string" ?
              pending.id.trim() : "";
            if (actionID !== "" && pendingID === actionID) {
              setWaiting(
                "The runtime is still draining this action.",
                "The current runtime can answer probes, but it " +
                  "still reports this lifecycle action as pending.",
              );
            } else {
              setReady();
              shouldRetry = false;
              return;
            }
          } else {
            setWaiting(
              "Waiting for the runtime to restart.",
              "The admin server returned a temporary response. " +
                "This page will keep retrying in the background.",
            );
          }
        } catch (err) {
          void err;
          setWaiting(
            "Waiting for the runtime to restart.",
            "The admin server is not reachable yet. " +
              "This page will keep retrying in the background.",
          );
        } finally {
          running = false;
        }
        if (shouldRetry) {
          schedule();
        }
      };

      if (openLink) {
        openLink.addEventListener("click", function(event) {
          if (openLink.getAttribute("aria-disabled") === "true") {
            event.preventDefault();
          }
        });
      }
      if (retryButton) {
        retryButton.addEventListener("click", function() {
          window.clearTimeout(timerID);
          probe();
        });
      }
      schedule();
    })();
  </script>
</body>
</html>
`

var runtimeActionAcceptedTmpl = template.Must(
	template.New("runtime_action_accepted").Parse(
		runtimeActionAcceptedPage,
	),
)

type runtimeLifecycleAwareChannel interface {
	SetRuntimeLifecycleController(
		controller *runtimectl.Manager,
	)
}

type runtimeChangelogResponse struct {
	Version   string   `json:"version"`
	Summary   []string `json:"summary,omitempty"`
	Changelog string   `json:"changelog"`
}

type runtimeLifecycleAdminProvider struct {
	manager *runtimectl.Manager
}

func newRuntimeLifecycleManager(
	paths startupPaths,
	onReady func(runtimectl.Intent),
) *runtimectl.Manager {
	return runtimectl.NewManager(runtimectl.Options{
		CurrentVersion: currentVersion(),
		StateDir:       strings.TrimSpace(paths.StateDir),
		ReleaseBaseURL: releaseBaseURL,
		OnReadyToExit:  onReady,
	})
}

func injectRuntimeLifecycleController(
	channels []channel.Channel,
	controller *runtimectl.Manager,
) {
	if controller == nil {
		return
	}
	for _, ch := range channels {
		aware, ok := ch.(runtimeLifecycleAwareChannel)
		if !ok || aware == nil {
			continue
		}
		aware.SetRuntimeLifecycleController(controller)
	}
}

func newRuntimeLifecycleAdminProvider(
	manager *runtimectl.Manager,
) ocadmin.RuntimeLifecycleProvider {
	if manager == nil {
		return nil
	}
	return &runtimeLifecycleAdminProvider{manager: manager}
}

func wrapRuntimeAdminHandler(
	base http.Handler,
	manager *runtimectl.Manager,
) http.Handler {
	if manager == nil {
		return base
	}

	mux := http.NewServeMux()
	mux.HandleFunc(
		runtimeAdminStatusPath,
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				writeRuntimeMethodNotAllowed(w, http.MethodGet)
				return
			}
			writeRuntimeJSON(w, http.StatusOK, manager.Status())
		},
	)
	mux.HandleFunc(
		runtimeAdminActionsPath,
		func(w http.ResponseWriter, r *http.Request) {
			handleRuntimeAdminAction(w, r, manager)
		},
	)
	mux.HandleFunc(
		runtimeControlActionPath,
		func(w http.ResponseWriter, r *http.Request) {
			handleRuntimeControlAdminAction(w, r, manager)
		},
	)
	mux.HandleFunc(
		runtimeAdminVersionsPath,
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				writeRuntimeMethodNotAllowed(w, http.MethodGet)
				return
			}
			index, err := manager.ListVersions(r.Context())
			if err != nil {
				writeRuntimeError(
					w,
					http.StatusBadGateway,
					err,
				)
				return
			}
			writeRuntimeJSON(w, http.StatusOK, index)
		},
	)
	mux.HandleFunc(
		runtimeAdminChangelogPath,
		func(w http.ResponseWriter, r *http.Request) {
			handleRuntimeAdminChangelog(w, r, manager)
		},
	)
	if base != nil {
		mux.Handle("/", base)
	}
	return mux
}

type runtimeControlReturnTarget struct {
	Path   string
	Anchor string
}

func handleRuntimeAdminAction(
	w http.ResponseWriter,
	r *http.Request,
	manager *runtimectl.Manager,
) {
	if r.Method != http.MethodPost {
		writeRuntimeMethodNotAllowed(w, http.MethodPost)
		return
	}

	var req runtimectl.ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRuntimeError(w, http.StatusBadRequest, err)
		return
	}

	result, err := manager.RequestAction(r.Context(), req)
	if err == nil {
		writeRuntimeJSON(w, http.StatusOK, result)
		return
	}

	status := http.StatusBadRequest
	if errors.Is(err, runtimectl.ErrActionInProgress) {
		status = http.StatusConflict
	}
	writeRuntimeJSON(w, status, map[string]any{
		"error":  err.Error(),
		"result": result,
	})
}

func handleRuntimeControlAdminAction(
	w http.ResponseWriter,
	r *http.Request,
	manager *runtimectl.Manager,
) {
	if r.Method != http.MethodPost {
		http.Error(
			w,
			http.StatusText(http.StatusMethodNotAllowed),
			http.StatusMethodNotAllowed,
		)
		return
	}
	if manager == nil {
		redirectRuntimeControlAdminMessage(
			w,
			r,
			runtimeActionQueryError,
			"runtime lifecycle provider is not available",
			"",
			runtimeControlReturnTarget{},
		)
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectRuntimeControlAdminMessage(
			w,
			r,
			runtimeActionQueryError,
			err.Error(),
			"",
			runtimeControlReturnTarget{},
		)
		return
	}

	returnTarget := runtimeControlReturnTargetFromForm(r)
	req, err := runtimeLifecycleActionRequestFromAdminForm(r)
	if err != nil {
		redirectRuntimeControlAdminMessage(
			w,
			r,
			runtimeActionQueryError,
			err.Error(),
			"",
			returnTarget,
		)
		return
	}

	ctx, cancel := runtimeLifecycleAdminContext()
	defer cancel()

	result, err := manager.RequestAction(ctx, req)
	if err != nil {
		redirectRuntimeControlAdminMessage(
			w,
			r,
			runtimeActionQueryError,
			err.Error(),
			runtimeLifecycleActionTargetVersion(req, result),
			returnTarget,
		)
		return
	}

	renderRuntimeControlActionAcceptedPage(
		w,
		r,
		req,
		result,
		returnTarget,
	)
}

func renderRuntimeControlActionAcceptedPage(
	w http.ResponseWriter,
	r *http.Request,
	req runtimectl.ActionRequest,
	result runtimectl.ActionResult,
	returnTarget runtimeControlReturnTarget,
) {
	page := runtimeActionAcceptedPageData{
		Title: runtimeLifecycleActionAcceptedTitle(req),
		Summary: runtimeLifecycleActionAcceptedSummary(
			req,
			result,
		),
		Detail: runtimeLifecycleActionAcceptedDetail(
			req,
			result,
		),
		ReturnURL: runtimeLifecycleActionReturnURL(
			r,
			returnTarget,
			runtimeLifecycleActionTargetVersion(
				req,
				result,
			),
		),
		StatusURL: adminRelativeReference(
			runtimeLifecycleRequestPath(r),
			runtimeAdminStatusPath,
		),
		ActionID:    runtimeLifecycleActionID(result),
		ActionLabel: runtimeLifecycleActionLabel(req),
		ModeLabel:   runtimeLifecycleModeLabel(req),
		TargetLabel: runtimeLifecycleActionTargetLabel(
			req,
			result,
		),
		ProgressLabel: runtimeLifecycleActionProgressLabel(
			req,
		),
		PollIntervalMillis: int(
			runtimeActionPollInterval / time.Millisecond,
		),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
	if err := runtimeActionAcceptedTmpl.Execute(w, page); err != nil {
		tlog.Errorf(
			"runtime action accepted page render failed: %v",
			err,
		)
		return
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

type runtimeActionAcceptedPageData struct {
	Title              string
	Summary            string
	Detail             string
	ReturnURL          string
	StatusURL          string
	ActionID           string
	ActionLabel        string
	ModeLabel          string
	TargetLabel        string
	ProgressLabel      string
	PollIntervalMillis int
}

func redirectRuntimeControlAdminMessage(
	w http.ResponseWriter,
	r *http.Request,
	key string,
	message string,
	version string,
	returnTarget runtimeControlReturnTarget,
) {
	target := url.URL{
		Path:     runtimeControlReturnPath(returnTarget),
		Fragment: strings.TrimSpace(returnTarget.Anchor),
	}
	values := url.Values{}
	key = strings.TrimSpace(key)
	message = strings.TrimSpace(message)
	if key != "" && message != "" {
		values.Set(key, message)
	}
	version = strings.TrimSpace(version)
	if version != "" {
		values.Set(runtimeActionQueryVersion, version)
	}
	target.RawQuery = values.Encode()
	location := adminRelativeReference(
		runtimeLifecycleRequestPath(r),
		target.String(),
	)
	w.Header().Set("Location", location)
	w.WriteHeader(http.StatusSeeOther)
}

func runtimeControlReturnTargetFromForm(
	r *http.Request,
) runtimeControlReturnTarget {
	if r == nil {
		return runtimeControlReturnTarget{
			Path: runtimeControlPagePath,
		}
	}

	pathValue := strings.TrimSpace(
		r.FormValue(runtimeActionFormReturnPath),
	)
	if pathValue == "" {
		pathValue = runtimeControlPagePath
	}
	return runtimeControlReturnTarget{
		Path:   adminCleanURLPath(pathValue),
		Anchor: strings.TrimSpace(r.FormValue(runtimeActionFormReturnTo)),
	}
}

func runtimeLifecycleActionRequestFromAdminForm(
	r *http.Request,
) (runtimectl.ActionRequest, error) {
	if r == nil {
		return runtimectl.ActionRequest{}, fmt.Errorf(
			"request is required",
		)
	}

	kind := normalizeRuntimeLifecycleAdminActionKind(
		r.FormValue(runtimeActionFormKind),
	)
	if kind == "" {
		return runtimectl.ActionRequest{}, fmt.Errorf(
			"runtime action kind is required",
		)
	}

	mode := normalizeRuntimeLifecycleAdminActionMode(
		r.FormValue(runtimeActionFormMode),
	)
	if mode == "" {
		return runtimectl.ActionRequest{}, fmt.Errorf(
			"runtime action mode is required",
		)
	}

	req := runtimectl.ActionRequest{
		Kind:   kind,
		Mode:   mode,
		Source: "admin",
	}
	if kind == runtimectl.ActionUpgrade {
		req.TargetVersion = strings.TrimSpace(
			r.FormValue(runtimeActionFormTargetVersion),
		)
	}
	return req, nil
}

func normalizeRuntimeLifecycleAdminActionKind(
	raw string,
) runtimectl.ActionKind {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(runtimectl.ActionRestart):
		return runtimectl.ActionRestart
	case string(runtimectl.ActionUpgrade):
		return runtimectl.ActionUpgrade
	default:
		return ""
	}
}

func normalizeRuntimeLifecycleAdminActionMode(
	raw string,
) runtimectl.ActionMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(runtimectl.ModeGraceful):
		return runtimectl.ModeGraceful
	case string(runtimectl.ModeForce):
		return runtimectl.ModeForce
	default:
		return ""
	}
}

func runtimeLifecycleActionAcceptedSummary(
	req runtimectl.ActionRequest,
	result runtimectl.ActionResult,
) string {
	mode := "graceful"
	if req.Mode == runtimectl.ModeForce {
		mode = "force"
	}

	switch req.Kind {
	case runtimectl.ActionRestart:
		return "Requested " + mode + " restart."
	case runtimectl.ActionUpgrade:
		target := runtimeLifecycleActionTargetVersion(
			req,
			result,
		)
		if target == "" {
			return "Requested " + mode + " upgrade to latest."
		}
		return "Requested " + mode + " switch to " + target + "."
	default:
		return "Requested runtime action."
	}
}

func runtimeLifecycleActionAcceptedTitle(
	req runtimectl.ActionRequest,
) string {
	switch req.Kind {
	case runtimectl.ActionRestart:
		return runtimeActionTitleRestart
	case runtimectl.ActionUpgrade:
		return runtimeActionTitleUpgrade
	default:
		return runtimeActionTitleDefault
	}
}

func runtimeLifecycleActionAcceptedDetail(
	req runtimectl.ActionRequest,
	result runtimectl.ActionResult,
) string {
	target := runtimeLifecycleActionTargetVersion(req, result)
	switch req.Kind {
	case runtimectl.ActionRestart:
		if req.Mode == runtimectl.ModeForce {
			return "The runtime may interrupt in-flight work when " +
				"the forced shutdown window expires."
		}
		return "The runtime is draining in-flight work before it " +
			"exits."
	case runtimectl.ActionUpgrade:
		if req.Mode == runtimectl.ModeForce {
			if target == "" {
				return "The runtime may interrupt in-flight work " +
					"when the forced shutdown window expires."
			}
			return "The runtime may interrupt in-flight work " +
				"when the forced shutdown window expires, " +
				"then switch to " + target + "."
		}
		if target == "" {
			return "The runtime is draining in-flight work " +
				"before it switches versions."
		}
		return "The runtime is draining in-flight work before " +
			"it switches to " + target + "."
	default:
		return "The runtime is applying the requested action."
	}
}

func runtimeLifecycleActionLabel(
	req runtimectl.ActionRequest,
) string {
	switch req.Kind {
	case runtimectl.ActionRestart:
		return "Restart"
	case runtimectl.ActionUpgrade:
		return "Upgrade"
	default:
		return "Runtime action"
	}
}

func runtimeLifecycleModeLabel(
	req runtimectl.ActionRequest,
) string {
	switch req.Mode {
	case runtimectl.ModeForce:
		return "Force"
	case runtimectl.ModeGraceful:
		return "Graceful"
	default:
		return "Unknown"
	}
}

func runtimeLifecycleActionTargetLabel(
	req runtimectl.ActionRequest,
	result runtimectl.ActionResult,
) string {
	switch req.Kind {
	case runtimectl.ActionRestart:
		return runtimeActionTargetRestart
	case runtimectl.ActionUpgrade:
		target := runtimeLifecycleActionTargetVersion(
			req,
			result,
		)
		if target != "" {
			return target
		}
		return runtimeActionTargetLatest
	default:
		return "-"
	}
}

func runtimeLifecycleActionProgressLabel(
	req runtimectl.ActionRequest,
) string {
	switch req.Kind {
	case runtimectl.ActionRestart:
		return "The restart request has been accepted."
	case runtimectl.ActionUpgrade:
		return "The upgrade request has been accepted."
	default:
		return "The runtime action has been accepted."
	}
}

func runtimeLifecycleActionID(
	result runtimectl.ActionResult,
) string {
	if result.Status.Pending == nil {
		return ""
	}
	return strings.TrimSpace(result.Status.Pending.ID)
}

func runtimeLifecycleActionTargetVersion(
	req runtimectl.ActionRequest,
	result runtimectl.ActionResult,
) string {
	if result.Status.Pending != nil {
		target := strings.TrimSpace(
			result.Status.Pending.TargetVersion,
		)
		if target != "" {
			return target
		}
	}
	return strings.TrimSpace(req.TargetVersion)
}

func runtimeLifecycleActionReturnURL(
	r *http.Request,
	returnTarget runtimeControlReturnTarget,
	version string,
) string {
	target := url.URL{
		Path:     runtimeControlReturnPath(returnTarget),
		Fragment: strings.TrimSpace(returnTarget.Anchor),
	}
	version = strings.TrimSpace(version)
	if version != "" {
		values := url.Values{}
		values.Set(runtimeActionQueryVersion, version)
		target.RawQuery = values.Encode()
	}
	return adminRelativeReference(
		runtimeLifecycleRequestPath(r),
		target.String(),
	)
}

func runtimeControlReturnPath(
	returnTarget runtimeControlReturnTarget,
) string {
	pathValue := adminCleanURLPath(returnTarget.Path)
	if pathValue == "/" {
		return runtimeControlPagePath
	}
	return pathValue
}

func runtimeLifecycleRequestPath(r *http.Request) string {
	if r == nil || r.URL == nil {
		return runtimeControlActionPath
	}
	return r.URL.Path
}

func handleRuntimeAdminChangelog(
	w http.ResponseWriter,
	r *http.Request,
	manager *runtimectl.Manager,
) {
	if r.Method != http.MethodGet {
		writeRuntimeMethodNotAllowed(w, http.MethodGet)
		return
	}
	version := strings.TrimSpace(
		r.URL.Query().Get("version"),
	)
	payload, err := runtimeChangelogPayload(
		r.Context(),
		manager,
		version,
	)
	if err != nil {
		writeRuntimeError(w, http.StatusBadGateway, err)
		return
	}
	writeRuntimeJSON(w, http.StatusOK, payload)
}

func (p *runtimeLifecycleAdminProvider) RuntimeLifecycleStatus() (
	ocadmin.RuntimeLifecycleStatus,
	error,
) {
	if p == nil || p.manager == nil {
		return ocadmin.RuntimeLifecycleStatus{}, nil
	}
	return runtimeLifecycleStatusView(p.manager.Status()), nil
}

func (p *runtimeLifecycleAdminProvider) RuntimeLifecycleVersions() (
	ocadmin.RuntimeLifecycleVersionIndex,
	error,
) {
	if p == nil || p.manager == nil {
		return ocadmin.RuntimeLifecycleVersionIndex{}, nil
	}

	ctx, cancel := runtimeLifecycleAdminContext()
	defer cancel()

	index, err := p.manager.ListVersions(ctx)
	if err != nil {
		return ocadmin.RuntimeLifecycleVersionIndex{}, err
	}
	return runtimeLifecycleVersionIndexView(index), nil
}

func (p *runtimeLifecycleAdminProvider) RuntimeLifecycleChangelog(
	version string,
) (ocadmin.RuntimeLifecycleChangelog, error) {
	if p == nil || p.manager == nil {
		return ocadmin.RuntimeLifecycleChangelog{}, nil
	}

	ctx, cancel := runtimeLifecycleAdminContext()
	defer cancel()

	payload, err := runtimeChangelogPayload(
		ctx,
		p.manager,
		version,
	)
	if err != nil {
		return ocadmin.RuntimeLifecycleChangelog{}, err
	}
	return ocadmin.RuntimeLifecycleChangelog{
		Version:   strings.TrimSpace(payload.Version),
		Summary:   append([]string(nil), payload.Summary...),
		Changelog: payload.Changelog,
	}, nil
}

func (p *runtimeLifecycleAdminProvider) RequestRuntimeLifecycleAction(
	req ocadmin.RuntimeLifecycleActionRequest,
) (ocadmin.RuntimeLifecycleActionResult, error) {
	if p == nil || p.manager == nil {
		return ocadmin.RuntimeLifecycleActionResult{}, nil
	}

	ctx, cancel := runtimeLifecycleAdminContext()
	defer cancel()

	result, err := p.manager.RequestAction(
		ctx,
		runtimectl.ActionRequest{
			Kind: runtimectl.ActionKind(strings.TrimSpace(req.Kind)),
			Mode: runtimectl.ActionMode(strings.TrimSpace(req.Mode)),
			TargetVersion: strings.TrimSpace(
				req.TargetVersion,
			),
			Source: "admin",
		},
	)
	return ocadmin.RuntimeLifecycleActionResult{
		Status:  runtimeLifecycleStatusView(result.Status),
		Started: result.Started,
	}, err
}

func runtimeLifecycleAdminContext() (
	context.Context,
	context.CancelFunc,
) {
	return context.WithTimeout(
		context.Background(),
		runtimeAdminFetchTimeout,
	)
}

func runtimeLifecycleStatusView(
	status runtimectl.Status,
) ocadmin.RuntimeLifecycleStatus {
	return ocadmin.RuntimeLifecycleStatus{
		State:           string(status.State),
		CurrentVersion:  strings.TrimSpace(status.CurrentVersion),
		ActiveRequests:  status.ActiveRequests,
		RunningRequests: status.RunningRequests,
		QueuedRequests:  status.QueuedRequests,
		Pending:         runtimeLifecyclePendingActionView(status.Pending),
		UpdatedAt:       status.UpdatedAt,
		ExitCode:        status.ExitCode,
	}
}

func runtimeLifecyclePendingActionView(
	action *runtimectl.PendingAction,
) *ocadmin.RuntimeLifecyclePendingAction {
	if action == nil {
		return nil
	}
	return &ocadmin.RuntimeLifecyclePendingAction{
		ID:             strings.TrimSpace(action.ID),
		Kind:           string(action.Kind),
		Mode:           string(action.Mode),
		TargetVersion:  strings.TrimSpace(action.TargetVersion),
		Actor:          strings.TrimSpace(action.Actor),
		Source:         strings.TrimSpace(action.Source),
		RequestedAt:    action.RequestedAt,
		CurrentVersion: strings.TrimSpace(action.CurrentVersion),
		Summary:        append([]string(nil), action.Summary...),
	}
}

func runtimeLifecycleVersionIndexView(
	index releaseinfo.Index,
) ocadmin.RuntimeLifecycleVersionIndex {
	view := ocadmin.RuntimeLifecycleVersionIndex{
		LatestVersion: strings.TrimSpace(index.LatestVersion),
		MinSupportedTarget: strings.TrimSpace(
			index.MinSupportedTarget,
		),
		Versions: make(
			[]ocadmin.RuntimeLifecycleVersion,
			0,
			len(index.Versions),
		),
	}
	for _, item := range index.Versions {
		view.Versions = append(
			view.Versions,
			ocadmin.RuntimeLifecycleVersion{
				Version:     strings.TrimSpace(item.Version),
				PublishedAt: item.PublishedAt,
				InstallURL:  strings.TrimSpace(item.InstallURL),
				ChangelogURL: strings.TrimSpace(
					item.ChangelogURL,
				),
				Notes: append([]string(nil), item.Notes...),
			},
		)
	}
	return view
}

func runtimeChangelogPayload(
	ctx context.Context,
	manager *runtimectl.Manager,
	version string,
) (runtimeChangelogResponse, error) {
	if manager == nil {
		return runtimeChangelogResponse{}, nil
	}

	selected := strings.TrimSpace(version)
	changelog, err := manager.FetchChangelog(ctx, selected)
	if err != nil {
		return runtimeChangelogResponse{}, err
	}

	if selected == "" {
		selected = manager.Status().CurrentVersion
		index, idxErr := manager.ListVersions(ctx)
		if idxErr == nil &&
			strings.TrimSpace(index.LatestVersion) != "" {
			selected = index.LatestVersion
		}
	}
	return runtimeChangelogResponse{
		Version: selected,
		Summary: releaseinfo.ExtractReleaseChanges(
			changelog,
			selected,
			runtimeChangelogSummarySize,
		),
		Changelog: changelog,
	}, nil
}

func writeRuntimeMethodNotAllowed(
	w http.ResponseWriter,
	method string,
) {
	w.Header().Set("Allow", method)
	writeRuntimeError(
		w,
		http.StatusMethodNotAllowed,
		fmt.Errorf("method %s not allowed", method),
	)
}

func writeRuntimeError(
	w http.ResponseWriter,
	status int,
	err error,
) {
	if err == nil {
		err = fmt.Errorf("runtime request failed")
	}
	writeRuntimeJSON(w, status, map[string]string{
		"error": err.Error(),
	})
}

func writeRuntimeJSON(
	w http.ResponseWriter,
	status int,
	value any,
) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		tlog.Errorf("runtime admin json marshal failed: %v", err)
		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
		return
	}
	data = append(data, '\n')
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
