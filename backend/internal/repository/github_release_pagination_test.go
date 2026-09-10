package repository

import (
	"context"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubReleasePageUsesConfiguredTransportAndAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/repos/openai/codex/releases", r.URL.Path)
		require.Equal(t, "4", r.URL.Query().Get("page"))
		require.Equal(t, "30", r.URL.Query().Get("per_page"))
		require.Equal(t, "Bearer test-update-token", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`[{"tag_name":"rust-v0.150.1","published_at":"2026-08-01T00:00:00Z"}]`))
	}))
	defer server.Close()
	client := &githubReleaseClient{httpClient: &http.Client{Transport: &testTransport{testServerURL: server.URL}}, updateGitHubToken: "test-update-token"}
	releases, err := client.FetchReleasesPage(context.Background(), "openai/codex", 4, 30)
	require.NoError(t, err)
	require.Len(t, releases, 1)
	require.Equal(t, "rust-v0.150.1", releases[0].TagName)
	_, err = client.FetchReleasesPage(context.Background(), "openai/codex", 0, 30)
	require.Error(t, err)
}
