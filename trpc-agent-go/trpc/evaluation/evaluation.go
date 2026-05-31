// Package evaluation provides tRPC registration helpers for the evaluation service.
package evaluation

import (
	"fmt"
	"net/http"
	"strings"

	thttp "git.code.oa.com/trpc-go/trpc-go/http"
	"git.code.oa.com/trpc-go/trpc-go/server"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	evaluationserver "trpc.group/trpc-go/trpc-agent-go/server/evaluation"
)

// AddEvaluationServerToMux adds a single evaluation server to the supplied HTTP mux.
func AddEvaluationServerToMux(mux *http.ServeMux, evaluationServer *evaluationserver.Server) error {
	if mux == nil {
		return fmt.Errorf("evaluation: mux cannot be nil")
	}
	if evaluationServer == nil {
		return fmt.Errorf("evaluation: server cannot be nil")
	}
	handler := evaluationServer.Handler()
	if handler == nil {
		return fmt.Errorf("evaluation: handler cannot be nil")
	}
	addServerToMux(mux, evaluationServer.BasePath(), handler)
	return nil
}

// RegisterEvaluationServerToMux registers an evaluation server to a specific tRPC service with the given mux.
func RegisterEvaluationServerToMux(
	s *server.Server,
	mux *http.ServeMux,
	name string,
	evaluationServer *evaluationserver.Server,
) error {
	if err := AddEvaluationServerToMux(mux, evaluationServer); err != nil {
		return err
	}
	if s == nil {
		return fmt.Errorf("evaluation: trpc server cannot be nil")
	}
	service := s.Service(name)
	if service == nil {
		return fmt.Errorf("evaluation: service %s not found, please check trpc_go.yaml", name)
	}
	thttp.RegisterNoProtocolServiceMux(service, mux)
	return nil
}

// RegisterEvaluationServer is a convenience wrapper that registers an evaluation server to a tRPC service.
func RegisterEvaluationServer(s *server.Server, name string, evaluationServer *evaluationserver.Server) error {
	mux := http.NewServeMux()
	return RegisterEvaluationServerToMux(s, mux, name, evaluationServer)
}

func addServerToMux(mux *http.ServeMux, basePath string, handler http.Handler) {
	mux.Handle(basePath, handler)
	if basePath != "/" && !strings.HasSuffix(basePath, "/") {
		mux.Handle(basePath+"/", handler)
	}
}
