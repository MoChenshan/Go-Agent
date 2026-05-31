package trpc

import (
	"net/http"
	"testing"

	ihttp "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/http"
	basemodel "trpc.group/trpc-go/trpc-agent-go/model"
)

func TestDefaultNewHTTPClientUsesCustomTransport(t *testing.T) {
	transport := &http.Transport{}

	client := basemodel.DefaultNewHTTPClient(
		basemodel.WithHTTPClientTransport(transport),
	)

	httpClient, ok := client.(*http.Client)
	if !ok {
		t.Fatalf("expected *http.Client, got %T", client)
	}
	if httpClient.Transport != transport {
		t.Fatalf("expected custom transport to be preserved")
	}
}

func TestDefaultNewHTTPClientUsesTRPCHandler(t *testing.T) {
	client := basemodel.DefaultNewHTTPClient(
		basemodel.WithHTTPClientName("trpc.test.llm.openai"),
	)

	if _, ok := client.(*ihttp.RequestHandler); !ok {
		t.Fatalf("expected *ihttp.RequestHandler, got %T", client)
	}
}
