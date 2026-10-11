package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

type accountTTFTHistoryReaderStub struct {
	history map[int64]*service.OpenAIOAuthTTFTHistory
	err     error
	batches [][]int64
	owners  []int64
}

func (r *accountTTFTHistoryReaderStub) GetOpenAIOAuthTTFTHistory(ctx context.Context, accounts []service.Account) (map[int64]*service.OpenAIOAuthTTFTHistory, error) {
	ids := make([]int64, len(accounts))
	for i := range accounts {
		ids[i] = accounts[i].ID
	}
	r.batches = append(r.batches, ids)
	owner, _ := ctxkey.AccountAdminIDFromContext(ctx)
	r.owners = append(r.owners, owner)
	return r.history, r.err
}

func ttftHistoryForHandlerTest() *service.OpenAIOAuthTTFTHistory {
	latencies := []int{16001, 9000, 17000, 15000}
	observations := make([]service.OpenAIOAuthTTFTObservation, 5)
	for i := range observations {
		observations[i] = service.OpenAIOAuthTTFTObservation{
			ObservedAt: time.Date(2026, 10, 11, 12, i, 0, 0, time.UTC),
			Model:      "gpt-5.4",
		}
		if i < len(latencies) {
			observations[i].FirstTokenMs = &latencies[i]
		}
	}
	return &service.OpenAIOAuthTTFTHistory{ThresholdMs: 15000, RecentRequests: observations}
}

func ttftHistoryAccountForHandlerTest(id int64) service.Account {
	return service.Account{
		ID: id, Name: "subscription", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Credentials: map[string]any{"access_token": "secret"},
		GroupIDs: []int64{7},
		Groups:   []*service.Group{{ID: 7, Name: "GPT", Platform: service.PlatformOpenAI}},
	}
}

func ttftHistoryListItems(t *testing.T, recorder *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var payload struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload.Data.Items
}

func TestAccountHandlerTTFTHistoryListJSONAndETag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, query string
		simple      bool
	}{
		{name: "full"},
		{name: "lite", query: "?lite=1"},
		{name: "simple_full", simple: true},
		{name: "simple_lite", query: "?lite=1", simple: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newStubAdminService()
			svc.accounts = []service.Account{ttftHistoryAccountForHandlerTest(501), ttftHistoryAccountForHandlerTest(502), {
				ID: 503, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			}}
			reader := &accountTTFTHistoryReaderStub{history: map[int64]*service.OpenAIOAuthTTFTHistory{
				501: ttftHistoryForHandlerTest(),
				502: {ThresholdMs: 15000, RecentRequests: []service.OpenAIOAuthTTFTObservation{}},
			}}
			h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			h.SetOpenAIOAuthTTFTHistoryReader(reader)
			if tc.simple {
				h.cfg = &config.Config{RunMode: config.RunModeSimple}
			}
			router := gin.New()
			router.GET("/accounts", h.List)
			first := httptest.NewRecorder()
			router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/accounts"+tc.query, nil))
			items := ttftHistoryListItems(t, first)
			require.Len(t, items, 3)
			history := items[0]["openai_oauth_ttft"].(map[string]any)
			require.Equal(t, float64(15000), history["threshold_ms"])
			recent := history["recent_requests"].([]any)
			require.Len(t, recent, 5)
			require.Equal(t, float64(16001), recent[0].(map[string]any)["first_token_ms"])
			require.Equal(t, "2026-10-11T12:00:00Z", recent[0].(map[string]any)["observed_at"])
			require.Equal(t, "gpt-5.4", recent[0].(map[string]any)["model"])
			require.Contains(t, recent[4].(map[string]any), "first_token_ms")
			require.Nil(t, recent[4].(map[string]any)["first_token_ms"])
			require.Equal(t, []any{}, items[1]["openai_oauth_ttft"].(map[string]any)["recent_requests"])
			require.NotContains(t, items[2], "openai_oauth_ttft")
			require.Equal(t, [][]int64{{501, 502, 503}}, reader.batches)
			if tc.query != "" {
				require.NotContains(t, items[0], "groups")
			} else {
				require.Contains(t, items[0], "groups")
			}
			etag := first.Header().Get("ETag")
			require.NotEmpty(t, etag)
			refresh := func() *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodGet, "/accounts"+tc.query, nil)
				req.Header.Set("If-None-Match", etag)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				return rec
			}
			require.Equal(t, http.StatusNotModified, refresh().Code)
			*reader.history[501].RecentRequests[0].FirstTokenMs = 8000
			changed := refresh()
			require.Equal(t, http.StatusOK, changed.Code)
			require.NotEqual(t, etag, changed.Header().Get("ETag"))
			require.Contains(t, changed.Body.String(), `"first_token_ms":8000`)
		})
	}
}

func TestAccountHandlerTTFTHistoryDisabledOrUnavailableOmitsField(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reader *accountTTFTHistoryReaderStub
	}{
		{name: "not_injected"},
		{name: "disabled", reader: &accountTTFTHistoryReaderStub{}},
		{name: "read_error", reader: &accountTTFTHistoryReaderStub{history: map[int64]*service.OpenAIOAuthTTFTHistory{501: ttftHistoryForHandlerTest()}, err: errors.New("history unavailable")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &simpleModeAccountService{stubAdminService: newStubAdminService(), account: ttftHistoryAccountForHandlerTest(501)}
			svc.accounts = []service.Account{svc.account}
			h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			if tc.reader != nil {
				h.SetOpenAIOAuthTTFTHistoryReader(tc.reader)
			}
			router := gin.New()
			router.GET("/accounts", h.List)
			router.GET("/accounts/:id", h.GetByID)
			for _, path := range []string{"/accounts", "/accounts?lite=1", "/accounts/501"} {
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.NotContains(t, rec.Body.String(), "openai_oauth_ttft")
			}
		})
	}
}

type accountTTFTHistoryScopedService struct {
	*stubAdminService
}

func (s *accountTTFTHistoryScopedService) ListAccounts(ctx context.Context, page, pageSize int, platform, accountType, status, search string, groupID int64, privacyMode, sortBy, sortOrder string) ([]service.Account, int64, error) {
	owner, _ := ctxkey.AccountAdminIDFromContext(ctx)
	owned := make([]service.Account, 0)
	for _, account := range s.accounts {
		if account.AccountAdminID != nil && *account.AccountAdminID == owner {
			owned = append(owned, account)
		}
	}
	filtered := newStubAdminService()
	filtered.accounts = owned
	return filtered.ListAccounts(ctx, page, pageSize, platform, accountType, status, search, groupID, privacyMode, sortBy, sortOrder)
}

func TestAccountHandlerTTFTHistoryReadsOnlyScopedCurrentPage(t *testing.T) {
	owner, foreignOwner := int64(41), int64(73)
	svc := &accountTTFTHistoryScopedService{stubAdminService: newStubAdminService()}
	svc.accounts = nil
	for _, id := range []int64{101, 102, 103, 104, 999} {
		account := ttftHistoryAccountForHandlerTest(id)
		account.AccountAdminID = &owner
		if id == 999 {
			account.AccountAdminID = &foreignOwner
		}
		svc.accounts = append(svc.accounts, account)
	}
	reader := &accountTTFTHistoryReaderStub{history: map[int64]*service.OpenAIOAuthTTFTHistory{999: ttftHistoryForHandlerTest()}}
	h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h.SetOpenAIOAuthTTFTHistoryReader(reader)
	router := gin.New()
	router.GET("/accounts", h.List)
	req := httptest.NewRequest(http.MethodGet, "/accounts?page=2&page_size=2&account_ids=999", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.AccountAdminID, owner))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	items := ttftHistoryListItems(t, rec)
	require.Len(t, items, 2)
	require.Equal(t, float64(103), items[0]["id"])
	require.Equal(t, float64(104), items[1]["id"])
	require.Equal(t, [][]int64{{103, 104}}, reader.batches)
	require.Equal(t, []int64{owner}, reader.owners)
	require.NotContains(t, rec.Body.String(), "openai_oauth_ttft")

	reader.batches = nil
	empty := httptest.NewRecorder()
	router.ServeHTTP(empty, httptest.NewRequest(http.MethodGet, "/accounts?page=9&page_size=2", nil).WithContext(req.Context()))
	require.Empty(t, ttftHistoryListItems(t, empty))
	require.Empty(t, reader.batches, "empty pages must not read history")
}

type accountTTFTHistoryRuntimeService struct {
	*simpleModeAccountService
}

func (s *accountTTFTHistoryRuntimeService) SetAccountSchedulable(_ context.Context, _ int64, schedulable bool) (*service.Account, error) {
	s.account.Schedulable = schedulable
	return &s.account, nil
}

func TestAccountHandlerTTFTHistoryDetailAndUpdateRefresh(t *testing.T) {
	for _, simple := range []bool{false, true} {
		svc := &accountTTFTHistoryRuntimeService{simpleModeAccountService: &simpleModeAccountService{
			stubAdminService: newStubAdminService(), account: ttftHistoryAccountForHandlerTest(501),
		}}
		reader := &accountTTFTHistoryReaderStub{history: map[int64]*service.OpenAIOAuthTTFTHistory{501: ttftHistoryForHandlerTest()}}
		h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
		h.SetOpenAIOAuthTTFTHistoryReader(reader)
		if simple {
			h.cfg = &config.Config{RunMode: config.RunModeSimple}
		}
		router := gin.New()
		router.GET("/accounts/:id", h.GetByID)
		router.PUT("/accounts/:id", h.Update)
		router.POST("/accounts/:id/schedulable", h.SetSchedulable)
		for _, request := range []struct{ method, path, body string }{
			{http.MethodGet, "/accounts/501", ""},
			{http.MethodPut, "/accounts/501", `{"name":"subscription"}`},
			{http.MethodPost, "/accounts/501/schedulable", `{"schedulable":true}`},
		} {
			req := httptest.NewRequest(request.method, request.path, strings.NewReader(request.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			var payload struct {
				Data struct {
					History *service.OpenAIOAuthTTFTHistory `json:"openai_oauth_ttft"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
			require.NotNil(t, payload.Data.History)
			require.Equal(t, 15000, payload.Data.History.ThresholdMs)
			require.Len(t, payload.Data.History.RecentRequests, 5)
		}
		require.Equal(t, [][]int64{{501}, {501}, {501}}, reader.batches)
	}
}

func TestAccountHandlerTTFTHistoryDoesNotReadUnauthorizedDetail(t *testing.T) {
	owner, foreignOwner := int64(41), int64(73)
	svc := &accountOwnershipScopeAdminService{
		stubAdminService: newStubAdminService(),
		getAccountResult: &service.Account{ID: 999, AccountAdminID: &foreignOwner},
	}
	reader := &accountTTFTHistoryReaderStub{}
	h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h.SetOpenAIOAuthTTFTHistoryReader(reader)
	router := gin.New()
	router.GET("/accounts/:id", h.AccountOwnershipScope(), h.GetByID)
	req := httptest.NewRequest(http.MethodGet, "/accounts/999", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.AccountAdminID, owner))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Empty(t, reader.batches)
}
