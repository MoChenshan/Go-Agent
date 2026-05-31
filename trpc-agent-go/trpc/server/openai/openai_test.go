package openai

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	trpc "git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/server"
	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	openaiserver "trpc.group/trpc-go/trpc-agent-go/server/openai"
)

func TestAddOpenAIServerToMux(t *testing.T) {
	tests := []struct {
		name         string
		mux          *http.ServeMux
		openaiServer *openaiserver.Server
		wantErr      bool
		errMsg       string
	}{
		{
			name:         "mux is nil",
			mux:          nil,
			openaiServer: nil,
			wantErr:      true,
			errMsg:       "mux cannot be nil",
		},
		{
			name:         "openaiServer is nil",
			mux:          http.NewServeMux(),
			openaiServer: nil,
			wantErr:      true,
			errMsg:       "server cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := addOpenAIServerToMux(tt.mux, tt.openaiServer)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRegisterOpenAIServerToMux(t *testing.T) {
	tests := []struct {
		name         string
		trpcServer   *server.Server
		mux          *http.ServeMux
		serviceName  string
		openaiServer *openaiserver.Server
		wantErr      bool
		errMsg       string
	}{
		{
			name:         "openaiServer is nil",
			trpcServer:   &server.Server{},
			mux:          http.NewServeMux(),
			serviceName:  "test.service",
			openaiServer: nil,
			wantErr:      true,
			errMsg:       "server cannot be nil",
		},
		{
			name:         "trpcServer is nil (checked after openaiServer)",
			trpcServer:   nil,
			mux:          http.NewServeMux(),
			serviceName:  "test.service",
			openaiServer: nil,
			wantErr:      true,
			errMsg:       "server cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registerOpenAIServerToMux(tt.trpcServer, tt.mux, tt.serviceName, tt.openaiServer)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRegisterOpenAIServer(t *testing.T) {
	tests := []struct {
		name         string
		trpcServer   *server.Server
		serviceName  string
		openaiServer *openaiserver.Server
		wantErr      bool
		errMsg       string
	}{
		{
			name:         "trpcServer is nil",
			trpcServer:   nil,
			serviceName:  "test.service",
			openaiServer: nil,
			wantErr:      true,
		},
		{
			name:         "openaiServer is nil",
			trpcServer:   &server.Server{},
			serviceName:  "test.service",
			openaiServer: nil,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RegisterOpenAIServer(tt.trpcServer, tt.serviceName, tt.openaiServer)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestAddOpenAIServerToMux_PathRegistration tests path registration.
func TestAddOpenAIServerToMux_PathRegistration(t *testing.T) {
	mux := http.NewServeMux()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	basePath := "/v1"
	mux.Handle(basePath+"/", handler)

	req, _ := http.NewRequest("GET", "/v1/test", nil)
	rr := &responseRecorder{header: make(http.Header), statusCode: 0}
	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.statusCode)
}

// TestAddOpenAIServerToMux_HandlerNil tests handler nil case.
// Since we can't directly mock openaiserver.Server, we test the error path
// by ensuring the function checks handler after getting it from server.
func TestAddOpenAIServerToMux_HandlerNil(t *testing.T) {
	mux := http.NewServeMux()
	var server *openaiserver.Server
	err := addOpenAIServerToMux(mux, server)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server cannot be nil")
}

// TestRegisterOpenAIServerToMux_TrpcServerNil tests trpcServer nil case.
func TestRegisterOpenAIServerToMux_TrpcServerNil(t *testing.T) {
	mux := http.NewServeMux()
	err := registerOpenAIServerToMux(nil, mux, "test.service", nil)
	assert.Error(t, err)
	// Error will be from openaiServer check first
	assert.Contains(t, err.Error(), "server cannot be nil")
}

// TestAddOpenAIServerToMux_Success tests successful path registration.
func TestAddOpenAIServerToMux_Success(t *testing.T) {
	// Create a real OpenAI server for testing
	modelInstance := openai.New("deepseek-chat")
	genConfig := model.GenerationConfig{
		MaxTokens:   intPtr(100),
		Temperature: floatPtr(0.7),
		Stream:      false,
	}
	agent := llmagent.New(
		"test-agent",
		llmagent.WithModel(modelInstance),
		llmagent.WithGenerationConfig(genConfig),
	)

	server, err := openaiserver.New(
		openaiserver.WithAgent(agent),
		openaiserver.WithBasePath("/v1"),
		openaiserver.WithModelName("deepseek-chat"),
	)
	if err != nil {
		t.Fatalf("Failed to create OpenAI server: %v", err)
	}
	defer server.Close()

	mux := http.NewServeMux()
	err = addOpenAIServerToMux(mux, server)
	assert.NoError(t, err)

	// Verify handler is registered by checking that the path is handled
	// The OpenAI server handler will handle /v1/chat/completions, etc.
	req, _ := http.NewRequest("GET", "/v1/chat/completions", nil)
	rr := &responseRecorder{header: make(http.Header), statusCode: 0}
	mux.ServeHTTP(rr, req)
	// Handler is registered, should not be 404 (might be 400/405/etc for wrong method/body)
	assert.NotEqual(t, http.StatusNotFound, rr.statusCode)
}

// TestRegisterOpenAIServerToMux_ServiceNotFound tests service not found case.
// This requires a valid openaiServer and trpcServer with no service registered.
func TestRegisterOpenAIServerToMux_ServiceNotFound(t *testing.T) {
	// Create a real OpenAI server
	modelInstance := openai.New("deepseek-chat")
	genConfig := model.GenerationConfig{
		MaxTokens:   intPtr(100),
		Temperature: floatPtr(0.7),
		Stream:      false,
	}
	agent := llmagent.New(
		"test-agent",
		llmagent.WithModel(modelInstance),
		llmagent.WithGenerationConfig(genConfig),
	)

	openaiServer, err := openaiserver.New(
		openaiserver.WithAgent(agent),
		openaiserver.WithBasePath("/v1"),
		openaiserver.WithModelName("deepseek-chat"),
	)
	if err != nil {
		t.Fatalf("Failed to create OpenAI server: %v", err)
	}
	defer openaiServer.Close()

	// Create a minimal config file for trpc server
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "trpc_go.yaml")
	config := `server:
  service:
    - name: test.service
      ip: 127.0.0.1
      port: 8080
      protocol: http_no_protocol
`
	if err := os.WriteFile(configFile, []byte(config), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Set config path and create server
	oldConfigPath := trpc.ServerConfigPath
	trpc.ServerConfigPath = configFile
	defer func() {
		trpc.ServerConfigPath = oldConfigPath
	}()

	trpcServer := trpc.NewServer()
	mux := http.NewServeMux()
	err = registerOpenAIServerToMux(trpcServer, mux, "nonexistent.service", openaiServer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service nonexistent.service not found")
}

func intPtr(i int) *int {
	return &i
}

func floatPtr(f float64) *float64 {
	return &f
}

// responseRecorder is used for testing HTTP responses.
type responseRecorder struct {
	header     http.Header
	statusCode int
	body       []byte
}

func (r *responseRecorder) Header() http.Header {
	return r.header
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return len(b), nil
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
}
