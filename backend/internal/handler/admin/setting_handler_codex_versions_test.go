package admin

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
	"net/http"
	"net/http/httptest"
	"testing"
)

type codexVersionManagerStub struct {
	page int
	err  error
}

func (s *codexVersionManagerStub) ListVersions(_ context.Context, page int) (*service.OpenAICodexVersionHistory, error) {
	s.page = page
	if s.err != nil {
		return nil, s.err
	}
	next := page + 1
	return &service.OpenAICodexVersionHistory{LatestVersion: "0.200.1", Versions: []service.OpenAICodexVersion{{Version: "0.150.1", TagName: "rust-v0.150.1"}}, HasMore: true, NextPage: &next}, nil
}
func (s *codexVersionManagerStub) SyncNow(context.Context) (*service.OpenAICodexVersionSyncResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &service.OpenAICodexVersionSyncResult{LatestVersion: "0.200.1", SyncedVersion: "0.200.1", Updated: true, Defaults: service.OpenAICodexVersionSyncDefaults{Originator: "Codex Desktop", UserAgent: "Codex Desktop/0.200.1", ClientVersion: "0.200.1"}}, nil
}
func TestCodexVersionSettingsAPIContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := &codexVersionManagerStub{}
	h := &SettingHandler{codexVersionManager: manager}
	router := gin.New()
	router.GET("/versions", h.GetOpenAICodexVersions)
	router.POST("/sync", h.SyncOpenAICodexVersion)
	for _, tc := range []struct{ method, url string }{{http.MethodGet, "/versions?page=4"}, {http.MethodPost, "/sync"}} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.url, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		var payload struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
		require.JSONEq(t, `"0.200.1"`, string(payload.Data["latest_version"]))
		if tc.method == http.MethodGet {
			require.Equal(t, 4, manager.page)
			require.JSONEq(t, `5`, string(payload.Data["next_page"]))
		} else {
			require.JSONEq(t, `{"originator":"Codex Desktop","user_agent":"Codex Desktop/0.200.1","client_version":"0.200.1"}`, string(payload.Data["defaults"]))
		}
	}
	for _, page := range []string{"0", "-1", "abc", "1001"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/versions?page="+page, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
}
func TestCodexVersionSettingsAPIFailureIsExplicitAndRedacted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &SettingHandler{codexVersionManager: &codexVersionManagerStub{err: errors.New("private-upstream-details")}}
	router := gin.New()
	router.GET("/versions", h.GetOpenAICodexVersions)
	router.POST("/sync", h.SyncOpenAICodexVersion)
	for _, tc := range []struct{ method, url string }{{http.MethodGet, "/versions"}, {http.MethodPost, "/sync"}} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.url, nil))
		require.Equal(t, http.StatusBadGateway, rec.Code)
		require.NotContains(t, rec.Body.String(), "private-upstream-details")
		require.NotContains(t, rec.Body.String(), `"updated":true`)
	}
}
