//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package promptengine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ConfigHandler returns an http.Handler that serves the sampler's
// control-plane API: GET, PUT and DELETE on a path whose prefix is chosen
// by the caller (see mux.Handle in the package documentation).
//
// The handler dispatches exclusively on r.Method and the ?app= query
// parameter and therefore works under any prefix. It does not own or
// validate URL paths beyond what the enclosing ServeMux delivered.
//
// ConfigHandler does not authenticate requests. Production deployments should
// mount it on an internal admin endpoint or wrap it in caller-owned middleware.
func (s *Sampler) ConfigHandler() http.Handler {
	return &configHandler{sampler: s}
}

// configHandler is the unexported implementation returned by ConfigHandler.
// It is deliberately stateless with respect to URL prefixes: the enclosing
// ServeMux handles path routing.
type configHandler struct {
	sampler *Sampler
}

// ServeHTTP implements http.Handler by dispatching on r.Method.
func (h *configHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleGet(w, r)
	case http.MethodPut:
		h.handlePut(w, r)
	case http.MethodDelete:
		h.handleDelete(w, r)
	default:
		w.Header().Set("Allow", strings.Join(
			[]string{http.MethodGet, http.MethodPut, http.MethodDelete},
			", ",
		))
		h.writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// GET handlers are defined below.

// handleGet returns either the full snapshot (default + apps) when ?app is
// absent, or a single {"config": ..., "source": "override|default"} when
// ?app is supplied.
func (h *configHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	app, hasApp := readAppParam(r)
	if !hasApp {
		snapshot := h.sampler.getConfig()
		appsOverride := h.sampler.listAppConfigs()
		body := map[string]any{
			"config": snapshot,
			"apps":   appsOverride,
		}
		h.writeJSON(w, r, http.StatusOK, body)
		return
	}
	// Empty app value (?app=) is treated as "use default", mirroring the
	// PUT semantics. We surface that as source=default so operators can
	// tell their query hit the default branch.
	cfg, isOverride := h.sampler.getAppConfig(app)
	source := "default"
	if isOverride {
		source = "override"
	}
	h.writeJSON(w, r, http.StatusOK, map[string]any{
		"config": cfg,
		"source": source,
	})
}

// PUT handlers are defined below.

// configEnvelope is the shared on-the-wire shape for PUT requests and GET
// single-config responses. The outer wrapper keeps forward compatibility
// with platforms that already speak the historical contract.
type configEnvelope struct {
	Config *runtimeConfig `json:"config"`
}

func (h *configHandler) handlePut(w http.ResponseWriter, r *http.Request) {
	cfg, err := decodeConfigBody(r)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := cfg.validate(); err != nil {
		h.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	app, hasApp := readAppParam(r)
	if !hasApp || app == "" {
		if err := h.sampler.setConfig(cfg); err != nil {
			h.writeError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		// Respond with the latest snapshot so callers can confirm.
		h.writeJSON(w, r, http.StatusOK, map[string]any{
			"config": h.sampler.getConfig(),
			"apps":   h.sampler.listAppConfigs(),
		})
		return
	}
	if err := h.sampler.setAppConfig(app, cfg); err != nil {
		h.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	effective, isOverride := h.sampler.getAppConfig(app)
	source := "default"
	if isOverride {
		source = "override"
	}
	h.writeJSON(w, r, http.StatusOK, map[string]any{
		"config": effective,
		"source": source,
	})
}

// decodeConfigBody parses the PUT body into a runtimeConfig, enforcing the
// outer "config" wrapper and guarding against multi-document bodies.
func decodeConfigBody(r *http.Request) (*runtimeConfig, error) {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var envelope configEnvelope
	if err := dec.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("invalid request body: %v", err)
	}
	if envelope.Config == nil {
		return nil, errors.New("request body must contain a 'config' field")
	}
	// Reject trailing tokens so that clients can't smuggle extra config
	// objects past the JSON decoder by concatenating JSON documents.
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err == nil {
		return nil, errors.New("invalid request body: request must contain a single JSON object")
	}
	return envelope.Config, nil
}

// DELETE handlers are defined below.

func (h *configHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	app, hasApp := readAppParam(r)
	if !hasApp || app == "" {
		w.Header().Set("Allow", strings.Join(
			[]string{http.MethodGet, http.MethodPut, http.MethodDelete},
			", ",
		))
		h.writeError(w, r, http.StatusMethodNotAllowed,
			"default config cannot be deleted, use PUT to reset it")
		return
	}
	if removed := h.sampler.deleteAppConfig(app); !removed {
		h.writeError(w, r, http.StatusNotFound, "app override not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Helper functions are defined below.

// readAppParam returns the ?app= value from the request. The second return
// value indicates whether the query parameter was present at all (as
// opposed to present-but-empty).
func readAppParam(r *http.Request) (app string, hasApp bool) {
	q := r.URL.Query()
	if _, ok := q["app"]; !ok {
		return "", false
	}
	return q.Get("app"), true
}

// writeJSON writes a JSON response body using the common Content-Type and
// status handling expected by the spec.
func (h *configHandler) writeJSON(
	w http.ResponseWriter, r *http.Request, status int, body any,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.ErrorfContext(r.Context(),
			"[promptengine] ConfigHandler: write json failed: method=%s path=%s err=%v",
			r.Method, r.URL.Path, err,
		)
	}
}

// writeError writes a {"error": msg} JSON response. It is the single exit
// point for all failure paths, which keeps the wire format consistent and
// makes sure client code can rely on the shape.
func (h *configHandler) writeError(
	w http.ResponseWriter, r *http.Request, status int, msg string,
) {
	h.writeJSON(w, r, status, map[string]string{"error": msg})
}
