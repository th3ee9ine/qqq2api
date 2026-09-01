package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

type opsErrorListCaptureRepo struct {
	service.OpsRepository
	filter *service.OpsErrorLogFilter
}

func (r *opsErrorListCaptureRepo) ListErrorLogs(_ context.Context, filter *service.OpsErrorLogFilter) (*service.OpsErrorLogList, error) {
	r.filter = filter
	return &service.OpsErrorLogList{Errors: []*service.OpsErrorLog{}, Total: 0, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func TestGetErrorLogs_IncludeDetailsIsExplicitOptIn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name  string
		query string
		want  bool
	}{
		{name: "true", query: "include_details=true", want: true},
		{name: "one", query: "include_details=1", want: true},
		{name: "yes", query: "include_details=yes", want: true},
		{name: "false", query: "include_details=false", want: false},
		{name: "unknown", query: "include_details=maybe", want: false},
		{name: "omitted", query: "", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &opsErrorListCaptureRepo{}
			svc := service.NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			h := NewOpsHandler(svc)
			r := gin.New()
			r.GET("/errors", h.GetErrorLogs)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/errors?"+tc.query, nil))
			require.Equal(t, http.StatusOK, w.Code)
			require.NotNil(t, repo.filter)
			require.Equal(t, tc.want, repo.filter.IncludeDetails)
		})
	}
}
