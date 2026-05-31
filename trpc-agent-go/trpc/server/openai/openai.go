// Package openai provides server interface for the OpenAI-compatible service.
package openai

import (
	"errors"
	"fmt"
	"net/http"

	thttp "git.code.oa.com/trpc-go/trpc-go/http"
	"git.code.oa.com/trpc-go/trpc-go/server"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	openaiserver "trpc.group/trpc-go/trpc-agent-go/server/openai"
)

// addOpenAIServerToMux adds a single OpenAI server to the supplied HTTP mux.
func addOpenAIServerToMux(mux *http.ServeMux, openaiServer *openaiserver.Server) error {
	if mux == nil {
		return errors.New("openai: mux cannot be nil")
	}
	if openaiServer == nil {
		return errors.New("openai: server cannot be nil")
	}
	handler := openaiServer.Handler()
	if handler == nil {
		return errors.New("openai: handler cannot be nil")
	}
	basePath := openaiServer.BasePath()
	if basePath != "" && basePath[len(basePath)-1] != '/' {
		basePath += "/"
	}
	mux.Handle(basePath, handler)
	return nil
}

// registerOpenAIServerToMux registers an OpenAI server to a specific tRPC
// service with the given mux.
func registerOpenAIServerToMux(s *server.Server, mux *http.ServeMux, name string, openaiServer *openaiserver.Server) error {
	if err := addOpenAIServerToMux(mux, openaiServer); err != nil {
		return err
	}
	if s == nil {
		return errors.New("openai: trpc server cannot be nil")
	}
	service := s.Service(name)
	if service == nil {
		return fmt.Errorf("openai: service %s not found, please check trpc_go.yaml", name)
	}
	thttp.RegisterNoProtocolServiceMux(service, mux)
	return nil
}

// RegisterOpenAIServer is a convenience wrapper that registers an OpenAI
// server to a tRPC service.
func RegisterOpenAIServer(s *server.Server, name string, openaiServer *openaiserver.Server) error {
	mux := http.NewServeMux()
	return registerOpenAIServerToMux(s, mux, name, openaiServer)
}
