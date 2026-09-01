package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// opsErrorListRowValues mirrors the projection order in ListErrorLogs. The
// optional diagnostic projection is appended after the lightweight columns so
// the default query and its scan contract remain unchanged.
func opsErrorListRowValues(includeDetails bool) []driver.Value {
	createdAt := time.Date(2026, 9, 1, 1, 2, 3, 0, time.UTC)
	values := []driver.Value{
		int64(42), createdAt, "request", "invalid_request_error", "client", "client_request",
		"P2", int64(400), "openai", "gpt-test", false, nil, nil, "",
		"client-42", "request-42", "invalid request", nil, int64(7), int64(8),
		"fixture-account", int64(9), "fixture-group", "203.0.113.7", "/v1/chat/completions",
		false, "/v1/chat/completions", "/v1/responses", "gpt-test", "gpt-test", "fixture-client/1.0",
		nil, "fixture-key", nil,
	}
	if includeDetails {
		values = append(values,
			`{"error":"bad request"}`,
			`{"method":"POST","path":"/v1/chat/completions"}`,
			int64(503),
			"upstream failed",
			`{"type":"server_error"}`,
			`[{"upstream_status_code":503}]`,
			false,
			int64(4), int64(5), int64(6), int64(7), int64(8),
			"sk-prefix",
		)
	}
	return values
}

func TestListErrorLogs_InlineDetailsProjection(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &opsRepository{db: db}

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM ops_error_logs`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery(`(?s)SELECT.*request_details::text.*FROM ops_error_logs.*LIMIT \$1 OFFSET \$2`).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows(make([]string, 47)).AddRow(opsErrorListRowValues(true)...))

	result, err := repo.ListErrorLogs(context.Background(), &service.OpsErrorLogFilter{
		Page: 1, PageSize: 20, IncludeDetails: true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Len(t, result.Errors, 1)
	item := result.Errors[0]
	require.True(t, item.DetailsIncluded)
	require.Equal(t, `{"error":"bad request"}`, item.ErrorBody)
	require.Equal(t, `{"method":"POST","path":"/v1/chat/completions"}`, item.RequestDetails)
	require.NotNil(t, item.UpstreamStatusCode)
	require.Equal(t, 503, *item.UpstreamStatusCode)
	require.Equal(t, "upstream failed", item.UpstreamErrorMessage)
	require.Equal(t, `[{"upstream_status_code":503}]`, item.UpstreamErrors)
	require.NotNil(t, item.AuthLatencyMs)
	require.EqualValues(t, 4, *item.AuthLatencyMs)
	require.Equal(t, "sk-prefix", item.APIKeyPrefix)
	encoded, err := json.Marshal(item)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"details_included":true`)
	require.Contains(t, string(encoded), `"request_details":"{\"method\":\"POST\",\"path\":\"/v1/chat/completions\"}"`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListErrorLogs_DefaultProjectionStaysLightweight(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &opsRepository{db: db}

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM ops_error_logs`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery(`(?s)SELECT.*FROM ops_error_logs.*LIMIT \$1 OFFSET \$2`).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows(make([]string, 34)).AddRow(opsErrorListRowValues(false)...))

	result, err := repo.ListErrorLogs(context.Background(), &service.OpsErrorLogFilter{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, result.Errors, 1)
	require.False(t, result.Errors[0].DetailsIncluded)
	require.Empty(t, result.Errors[0].ErrorBody)
	require.Empty(t, result.Errors[0].RequestDetails)
	require.NoError(t, mock.ExpectationsWereMet())
}
