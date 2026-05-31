package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPClientErrorDetails tests that HTTP error responses include
// the response body.
func TestHTTPClientErrorDetails(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		responseBody  any
		expectedInErr string
	}{
		{
			name:       "JSON error response",
			statusCode: http.StatusBadRequest,
			responseBody: map[string]any{
				"error": map[string]any{
					"message": "Invalid request: missing required field 'model'",
					"type":    "invalid_request_error",
					"code":    "missing_required_parameter",
				},
			},
			expectedInErr: "missing required field",
		},
		{
			name:       "401 Unauthorized",
			statusCode: http.StatusUnauthorized,
			responseBody: map[string]any{
				"error": map[string]any{
					"message": "Invalid API key provided",
					"type":    "invalid_request_error",
					"code":    "invalid_api_key",
				},
			},
			expectedInErr: "Invalid API key",
		},
		{
			name:          "Plain text error",
			statusCode:    http.StatusInternalServerError,
			responseBody:  "Internal server error occurred",
			expectedInErr: "Internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock server
			mockServer := httptest.NewServer(
				http.HandlerFunc(func(
					w http.ResponseWriter,
					r *http.Request,
				) {
					w.WriteHeader(tt.statusCode)
					if str, ok := tt.responseBody.(string); ok {
						w.Write([]byte(str))
					} else {
						json.NewEncoder(w).Encode(tt.responseBody)
					}
				}),
			)
			defer mockServer.Close()

			// Create HTTP client and send request
			handler := NewRequestHandler("test-client")
			req, err := http.NewRequest(
				http.MethodPost,
				mockServer.URL,
				strings.NewReader("{}"),
			)
			require.NoError(t, err)

			resp, err := handler.Do(req)

			// Should have error for non-2xx status
			assert.Error(t, err)
			assert.Nil(t, resp)

			// Error should contain response body
			assert.Contains(t, err.Error(), tt.expectedInErr)
		})
	}
}

// TestHTTPClientSuccessResponse tests that successful responses work
// correctly.
func TestHTTPClientSuccessResponse(t *testing.T) {
	mockServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"id": "chatcmpl-123",
				"choices": []map[string]any{
					{
						"message": map[string]string{
							"content": "Hello",
						},
					},
				},
			})
		}),
	)
	defer mockServer.Close()

	handler := NewRequestHandler("test-client")
	req, _ := http.NewRequest(
		http.MethodPost,
		mockServer.URL,
		strings.NewReader("{}"),
	)
	resp, err := handler.Do(req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

func TestExtractTargetFromURL(t *testing.T) {
	const (
		openAPIPath                 = "/openapi"
		testTRPCClientName          = "trpc.test.llm.openai"
		taijiPolarisService         = "MiniMax-M2.5-Ruying-01_AIDE"
		boundaryPunctuationService  = "_llm-service.name-"
		emptyLabelService           = "llm-service..name"
		unsupportedCharacterService = "llm-service!.name"
	)

	tests := []struct {
		name        string
		rawURL      string
		handlerName string
		wantTarget  string
		wantTLS     bool
		wantErr     string
	}{
		{
			name:       "http uses direct client",
			rawURL:     "http://example.com/v1",
			wantTarget: "",
			wantTLS:    false,
		},
		{
			name:       "dns target is preserved",
			rawURL:     "dns://api.taiji.woa.com:8080/openapi",
			wantTarget: "dns://api.taiji.woa.com:8080",
			wantTLS:    false,
		},
		{
			name:        "polaris target is preserved",
			rawURL:      "polaris://llm-service.name/openapi",
			handlerName: testTRPCClientName,
			wantTarget:  "polaris://llm-service.name",
			wantTLS:     false,
		},
		{
			name: "polaris allows taiji service name",
			rawURL: schemePolaris + targetSep +
				taijiPolarisService + openAPIPath,
			handlerName: testTRPCClientName,
			wantTarget: schemePolaris + targetSep +
				taijiPolarisService,
			wantTLS: false,
		},
		{
			name:        "polaris requires client name",
			rawURL:      "polaris://llm-service.name/openapi",
			wantErr:     errMsgMissingTRPCClientName,
			handlerName: "",
		},
		{
			name:        "polaris rejects ip host",
			rawURL:      "polaris://127.0.0.1/openapi",
			handlerName: testTRPCClientName,
			wantErr:     errMsgInvalidTargetHost,
		},
		{
			name: "polaris allows boundary punctuation",
			rawURL: schemePolaris + targetSep +
				boundaryPunctuationService + openAPIPath,
			handlerName: testTRPCClientName,
			wantTarget: schemePolaris + targetSep +
				boundaryPunctuationService,
			wantTLS: false,
		},
		{
			name: "polaris rejects empty service label",
			rawURL: schemePolaris + targetSep +
				emptyLabelService + openAPIPath,
			handlerName: testTRPCClientName,
			wantErr:     errMsgInvalidTargetHost,
		},
		{
			name: "polaris rejects unsupported host character",
			rawURL: schemePolaris + targetSep +
				unsupportedCharacterService + openAPIPath,
			handlerName: testTRPCClientName,
			wantErr:     errMsgInvalidTargetHost,
		},
		{
			name:        "polaris rejects explicit port",
			rawURL:      "polaris://llm-service.name:8080/openapi",
			handlerName: testTRPCClientName,
			wantErr:     errMsgInvalidTargetPort,
		},
		{
			name:    "ip selector is rejected",
			rawURL:  "ip://127.0.0.1:80",
			wantErr: errMsgUnsupportedTargetScheme,
		},
		{
			name:    "unix selector is rejected",
			rawURL:  "unix:///tmp/socket",
			wantErr: errMsgUnsupportedTargetScheme,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsedURL, err := url.Parse(tt.rawURL)
			require.NoError(t, err)

			target, err := extractTargetFromURL(
				parsedURL,
				tt.handlerName,
			)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantTarget, target.value)
			assert.Equal(t, tt.wantTLS, target.needsTLS)
		})
	}
}
