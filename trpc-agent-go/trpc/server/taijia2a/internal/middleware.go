package internal

import (
	"context"
	"net/http"

	"git.code.oa.com/trpc-go/trpc-go/log"
	"github.com/go-chi/chi/v5"
)

const (
	// CtxAgentNameKey agentName by context.
	CtxAgentNameKey = "ContextAgentName"

	// CtxAgentNameValue agentName
	CtxAgentNameValue = "agentName"
)

// MiddleWare middleware.
type MiddleWare struct{}

// NewMiddleWare create new middleware
func NewMiddleWare() *MiddleWare {
	return &MiddleWare{}
}

// Wrap handler
func (m *MiddleWare) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, CtxAgentNameKey, chi.URLParam(r, CtxAgentNameValue))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetSubPath get sub path
func GetSubPath(ctx context.Context) string {
	agentName := ctx.Value(CtxAgentNameKey)
	if agentName == nil {
		log.ErrorContextf(ctx, "agentName is nil")
		return ""
	}
	if s, ok := agentName.(string); ok {
		return s
	}
	log.ErrorContextf(ctx, "agentName is not string type")
	return ""
}
