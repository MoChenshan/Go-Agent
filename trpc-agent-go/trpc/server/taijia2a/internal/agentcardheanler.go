package internal

import (
	"encoding/json"
	"net/http"

	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/server/taijia2a/config"
	"github.com/go-chi/chi/v5"
)

// MultiAgentCardHandlerImpl implements the agent card HTTP handler
type agentCardHandlerHTTPImpl struct{}

// NewAgentCardHTTPHandler returns a agent card HTTP handler
func NewAgentCardHTTPHandler() http.Handler {
	return &agentCardHandlerHTTPImpl{}
}

// ServeHTTP serves the HTTP request
func (s *agentCardHandlerHTTPImpl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	subPath := chi.URLParam(r, CtxAgentNameValue)
	proxyCfg, ok := config.GetConfiger().GetProxyConfig(subPath)
	if !ok {
		log.ErrorContext(r.Context(), "subPath not found")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(proxyCfg.ProxyAgentCard); err != nil {
		log.ErrorContext(r.Context(), "Failed to encode agent card: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}
