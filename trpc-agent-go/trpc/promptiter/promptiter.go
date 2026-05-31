// Package promptiter provides tRPC registration helpers for the PromptIter service.
package promptiter

import (
	"fmt"
	"net/http"
	"strings"

	thttp "git.code.oa.com/trpc-go/trpc-go/http"
	"git.code.oa.com/trpc-go/trpc-go/server"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	promptiterserver "trpc.group/trpc-go/trpc-agent-go/server/promptiter"
)

// AddPromptIterServerToMux adds a single PromptIter server to the supplied HTTP mux.
func AddPromptIterServerToMux(mux *http.ServeMux, promptIterServer *promptiterserver.Server) error {
	if mux == nil {
		return fmt.Errorf("promptiter: mux cannot be nil")
	}
	if promptIterServer == nil {
		return fmt.Errorf("promptiter: server cannot be nil")
	}
	handler := promptIterServer.Handler()
	if handler == nil {
		return fmt.Errorf("promptiter: handler cannot be nil")
	}
	addServerToMux(mux, promptIterServer.BasePath(), handler)
	return nil
}

// RegisterPromptIterServerToMux registers a PromptIter server to a specific tRPC service with the given mux.
func RegisterPromptIterServerToMux(
	s *server.Server,
	mux *http.ServeMux,
	name string,
	promptIterServer *promptiterserver.Server,
) error {
	if err := AddPromptIterServerToMux(mux, promptIterServer); err != nil {
		return err
	}
	if s == nil {
		return fmt.Errorf("promptiter: trpc server cannot be nil")
	}
	service := s.Service(name)
	if service == nil {
		return fmt.Errorf("promptiter: service %s not found, please check trpc_go.yaml", name)
	}
	thttp.RegisterNoProtocolServiceMux(service, mux)
	return nil
}

// RegisterPromptIterServer is a convenience wrapper that registers a PromptIter server to a tRPC service.
func RegisterPromptIterServer(s *server.Server, name string, promptIterServer *promptiterserver.Server) error {
	mux := http.NewServeMux()
	return RegisterPromptIterServerToMux(s, mux, name, promptIterServer)
}

func addServerToMux(mux *http.ServeMux, basePath string, handler http.Handler) {
	mux.Handle(basePath, handler)
	if basePath != "/" && !strings.HasSuffix(basePath, "/") {
		mux.Handle(basePath+"/", handler)
	}
}
