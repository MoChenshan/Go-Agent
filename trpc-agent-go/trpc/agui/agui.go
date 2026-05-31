// Package agui provides server interface for the agui service.
package agui

import (
	"fmt"
	"net/http"

	thttp "git.code.oa.com/trpc-go/trpc-go/http"
	"git.code.oa.com/trpc-go/trpc-go/server"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	aguiserver "trpc.group/trpc-go/trpc-agent-go/server/agui"
)

// AddAGUIServerToMux adds a single AG-UI server to the supplied HTTP mux.
func AddAGUIServerToMux(mux *http.ServeMux, aguiServer *aguiserver.Server) error {
	if mux == nil {
		return fmt.Errorf("agui: mux cannot be nil")
	}
	if aguiServer == nil {
		return fmt.Errorf("agui: server cannot be nil")
	}
	handler := aguiServer.Handler()
	if handler == nil {
		return fmt.Errorf("agui: handler cannot be nil")
	}
	mux.Handle(aguiServer.BasePath(), handler)
	return nil
}

// RegisterAGUIServerToMux registers an AG-UI server to a specific tRPC service with the given mux.
func RegisterAGUIServerToMux(s *server.Server, mux *http.ServeMux, name string, aguiServer *aguiserver.Server) error {
	if err := AddAGUIServerToMux(mux, aguiServer); err != nil {
		return err
	}
	if s == nil {
		return fmt.Errorf("agui: trpc server cannot be nil")
	}
	service := s.Service(name)
	if service == nil {
		return fmt.Errorf("agui: service %s not found, please check trpc_go.yaml", name)
	}
	thttp.RegisterNoProtocolServiceMux(service, mux)
	return nil
}

// RegisterAGUIServer is a convenience wrapper that registers an AG-UI server to a tRPC service.
func RegisterAGUIServer(s *server.Server, name string, aguiServer *aguiserver.Server) error {
	mux := http.NewServeMux()
	return RegisterAGUIServerToMux(s, mux, name, aguiServer)
}
