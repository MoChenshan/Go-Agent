package releaseinfo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCompareVersions(t *testing.T) {
	t.Parallel()

	require.Greater(t, CompareVersions("v0.0.47", "v0.0.46"), 0)
	require.Less(t, CompareVersions("v0.0.46", "v0.0.47"), 0)
	require.Zero(t, CompareVersions("v0.0.47", "0.0.47"))
	require.Greater(t, CompareVersions("v0.1.0", "v0.0.99"), 0)
	require.Greater(
		t,
		CompareVersions("v0.0.91-preview.2", "v0.0.90"),
		0,
	)
	require.Less(
		t,
		CompareVersions("v0.0.91-preview.2", "v0.0.91"),
		0,
	)
	require.Greater(
		t,
		CompareVersions("v0.0.91-preview.10", "v0.0.91-preview.2"),
		0,
	)
}

func TestExtractReleaseChanges(t *testing.T) {
	t.Parallel()

	markdown := "" +
		"## v0.0.48 (2026-03-30)\n" +
		"- add graceful runtime restart\n" +
		"- add force runtime upgrade\n" +
		"  with start.sh handoff\n" +
		"- fix card regression\n" +
		"\n" +
		"## v0.0.47 (2026-03-30)\n" +
		"- older\n"

	changes := ExtractReleaseChanges(markdown, "v0.0.48", 3)
	require.Equal(
		t,
		[]string{
			"add graceful runtime restart",
			"add force runtime upgrade with start.sh handoff",
			"fix card regression",
		},
		changes,
	)
}

func TestFetchIndexFallsBackToLatest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/latest/releases.json":
				http.NotFound(w, r)
			case "/latest/VERSION":
				_, _ = w.Write([]byte("v0.0.48"))
			case "/releases/v0.0.48/CHANGELOG.md":
				_, _ = w.Write([]byte(
					"## v0.0.48 (2026-03-30)\n- one\n- two\n",
				))
			default:
				http.NotFound(w, r)
			}
		},
	))
	defer server.Close()

	client := Client{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}

	index, err := client.FetchIndex(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v0.0.48", index.LatestVersion)
	require.Len(t, index.Versions, 1)
	require.Equal(t, "v0.0.48", index.Versions[0].Version)
	require.Equal(t, []string{"one", "two"}, index.Versions[0].Notes)
}

func TestFetchChannelVersionUsesPreview(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/preview/VERSION":
				_, _ = w.Write([]byte("v0.0.91-preview.1"))
			default:
				http.NotFound(w, r)
			}
		},
	))
	defer server.Close()

	client := Client{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}

	version, err := client.FetchChannelVersion(
		context.Background(),
		ChannelPreview,
	)
	require.NoError(t, err)
	require.Equal(t, "v0.0.91-preview.1", version)
}

func TestFetchIndexParsesAndNormalizes(t *testing.T) {
	t.Parallel()

	publishedAt := time.Now().UTC().Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/latest/releases.json":
				_, _ = w.Write([]byte(
					`{
  "latest_version": "v0.0.49",
  "versions": [
    {
      "version": "v0.0.48",
      "published_at": "` + publishedAt + `"
    },
    {
      "version": "v0.0.49"
    },
    {
      "version": "v0.0.49"
    }
  ]
}`,
				))
			default:
				http.NotFound(w, r)
			}
		},
	))
	defer server.Close()

	client := Client{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}

	index, err := client.FetchIndex(context.Background())
	require.NoError(t, err)
	require.Len(t, index.Versions, 2)
	require.Equal(t, "v0.0.49", index.Versions[0].Version)
	require.Equal(t, "v0.0.48", index.Versions[1].Version)
}

func TestFetchChangeSummaryUsesLatestWhenVersionEmpty(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/latest/VERSION":
				_, _ = w.Write([]byte("v0.0.52"))
			case "/releases/v0.0.52/CHANGELOG.md":
				_, _ = w.Write([]byte(
					"## v0.0.52 (2026-03-30)\n- one\n- two\n",
				))
			default:
				http.NotFound(w, r)
			}
		},
	))
	defer server.Close()

	client := Client{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}

	summary, err := client.FetchChangeSummary(
		context.Background(),
		"",
		2,
	)
	require.NoError(t, err)
	require.Equal(t, []string{"one", "two"}, summary)
}
