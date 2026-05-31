// Package ingress defines optional HTTP ingress hooks for OpenClaw
// channels.
//
// Some channels need an HTTP callback endpoint (for example, webhooks or
// WeCom callbacks). Instead of starting a separate net/http server, such
// channels can implement HTTPIngress so the OpenClaw distribution can
// mount their handlers into a tRPC-Go HTTP service (ports/protocol are
// configured in trpc_go.yaml).
package ingress

import "net/http"

const (
	// DefaultHTTPServiceName is the shared tRPC HTTP service used by
	// OpenClaw's gateway and HTTP-based channels.
	DefaultHTTPServiceName = "trpc.openclaw.gateway"
)

// HTTPIngress is an optional interface implemented by channels that want
// to expose HTTP endpoints.
type HTTPIngress interface {
	// HTTPServiceName returns the tRPC service name to mount onto.
	//
	// If empty, the distribution should mount the handlers onto the
	// gateway HTTP service.
	HTTPServiceName() string

	// HTTPPatterns returns the HTTP path patterns registered by MountHTTP.
	// It is intended for logging and troubleshooting.
	HTTPPatterns() []string

	// MountHTTP registers HTTP handlers onto mux.
	MountHTTP(mux *http.ServeMux) error
}

// RequireMethod returns a handler that only allows the provided HTTP
// method.
func RequireMethod(method string, next http.Handler) http.Handler {
	if next == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}
