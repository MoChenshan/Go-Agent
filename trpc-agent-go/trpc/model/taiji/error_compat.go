package taiji

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	openaiopt "github.com/openai/openai-go/option"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

const taijiCompatibleErrorStatusCode = http.StatusBadRequest

// WithOpenAIErrorCompat normalizes Taiji error responses for OpenAI-compatible callers.
func WithOpenAIErrorCompat() openai.Option {
	return openai.WithOpenAIOptions(openaiopt.WithMiddleware(
		func(req *http.Request, next openaiopt.MiddlewareNext) (*http.Response, error) {
			resp, err := next(req)
			if err != nil || resp == nil {
				return resp, err
			}
			return normalizeTaijiErrorResponse(resp)
		},
	))
}

func normalizeTaijiErrorResponse(resp *http.Response) (*http.Response, error) {
	if resp.Body == nil || !isJSONResponse(resp.Header) {
		return resp, nil
	}
	body, err := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	normalizedBody, ok := normalizeTaijiErrorBody(body)
	if !ok {
		setResponseBody(resp, body)
		return resp, nil
	}
	setResponseBody(resp, normalizedBody)
	if resp.StatusCode < taijiCompatibleErrorStatusCode {
		resp.StatusCode = taijiCompatibleErrorStatusCode
		resp.Status = fmt.Sprintf(
			"%d %s",
			taijiCompatibleErrorStatusCode,
			http.StatusText(taijiCompatibleErrorStatusCode),
		)
	}
	return resp, nil
}

func normalizeTaijiErrorBody(body []byte) ([]byte, bool) {
	var envelope struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Error) == 0 {
		return nil, false
	}
	if envelope.Error["message"] == nil && envelope.Error["code"] != nil {
		envelope.Error["message"] = fmt.Sprint(envelope.Error["code"])
	}
	if envelope.Error["code"] == nil && envelope.Error["ret_code"] != nil {
		envelope.Error["code"] = fmt.Sprint(envelope.Error["ret_code"])
	}
	if envelope.Error["type"] == nil {
		envelope.Error["type"] = "TaijiAPIError"
	}
	if envelope.Error["param"] == nil {
		envelope.Error["param"] = ""
	}
	normalizedBody, err := json.Marshal(map[string]any{"error": envelope.Error})
	if err != nil {
		return nil, false
	}
	return normalizedBody, true
}

func isJSONResponse(header http.Header) bool {
	contentType := header.Get("Content-Type")
	if contentType == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return mediaType == "application/json" || (len(mediaType) > 5 && mediaType[len(mediaType)-5:] == "+json")
}

func setResponseBody(resp *http.Response, body []byte) {
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
}
