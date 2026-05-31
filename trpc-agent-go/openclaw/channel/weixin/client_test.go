package weixin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testHTTPSBaseURL           = "https://example.com"
	testHTTPSBaseURLWithPath   = "https://example.com/base"
	testHTTPBaseURL            = "http://example.com"
	testEndpointPath           = "ilink/bot/get_bot_qrcode"
	testEndpointQuery          = "bot_type=3"
	testEndpointWithQuery      = testEndpointPath + "?" + testEndpointQuery
	testAbsoluteEndpoint       = "https://attacker.example.com/path"
	testSchemeRelativeEndpoint = "//attacker.example.com/path"
	testFileBaseURL            = "file:///tmp/weixin"
	testEndpointURL            = testHTTPSBaseURL + "/" + testEndpointWithQuery
	testEndpointURLWithPath    = testHTTPSBaseURLWithPath + "/" +
		testEndpointWithQuery
)

func TestBuildEndpointURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		baseURL  string
		endpoint string
		want     string
	}{
		{
			name:     "default base URL",
			baseURL:  "",
			endpoint: endpointGetUpdates,
			want:     defaultBaseURL + "/" + endpointGetUpdates,
		},
		{
			name:     "base URL with path",
			baseURL:  testHTTPSBaseURLWithPath,
			endpoint: testEndpointWithQuery,
			want:     testEndpointURLWithPath,
		},
		{
			name:     "leading slash endpoint",
			baseURL:  testHTTPSBaseURL,
			endpoint: "/" + testEndpointWithQuery,
			want:     testEndpointURL,
		},
		{
			name:     "http base URL",
			baseURL:  testHTTPBaseURL,
			endpoint: endpointGetUpdates,
			want:     testHTTPBaseURL + "/" + endpointGetUpdates,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := buildEndpointURL(tt.baseURL, tt.endpoint)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestBuildEndpointURLRejectsUnsafeInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		baseURL  string
		endpoint string
	}{
		{
			name:     "absolute endpoint",
			baseURL:  testHTTPSBaseURL,
			endpoint: testAbsoluteEndpoint,
		},
		{
			name:     "scheme relative endpoint",
			baseURL:  testHTTPSBaseURL,
			endpoint: testSchemeRelativeEndpoint,
		},
		{
			name:     "missing base URL host",
			baseURL:  urlSchemeHTTPS + ":",
			endpoint: endpointGetUpdates,
		},
		{
			name:     "unsupported base URL scheme",
			baseURL:  testFileBaseURL,
			endpoint: endpointGetUpdates,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := buildEndpointURL(tt.baseURL, tt.endpoint)
			require.Error(t, err)
		})
	}
}
