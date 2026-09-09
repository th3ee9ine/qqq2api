package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func paidCreditsSchedulingTestQuery() (string, []any) {
	table := entsql.Table("paid_credits_scheduler_fixtures")
	s := entsql.Dialect(dialect.Postgres).Select(table.C("name")).From(table)
	tempUnschedulablePredicate()(s)
	return s.OrderBy(table.C("name")).Query()
}

func TestTempUnschedulablePredicate_PaidCreditsSQL(t *testing.T) {
	query, args := paidCreditsSchedulingTestQuery()
	require.Equal(t, []any{service.PlatformOpenAI, service.AccountTypeOAuth, `{"source":"account_scheduling_threshold"%`}, args)
	require.Contains(t, query, `"parent_account_id" IS NULL`)
	require.Contains(t, query, "CASE WHEN jsonb_typeof(")
	require.Contains(t, query, "strict $.codex_paid_credits_snapshot.fetched_at.double().floor()")
	require.Contains(t, query, "strict $.codex_paid_credits_snapshot.balance.double()")
	require.Contains(t, query, "'{}'::jsonb, true)")
	require.NotContains(t, query, "::bigint")
	require.NotContains(t, query, "::numeric")
	require.NotContains(t, query, "fetched_at}' <= EXTRACT", "JSON text must not be compared directly to a numeric timestamp")
}

// Run against an isolated PostgreSQL fixture, for example:
// QQQ2API_TEST_POSTGRES_DSN='postgres://postgres:fixture@127.0.0.1:PORT/fixture?sslmode=disable' go test ./internal/repository -run TestTempUnschedulablePredicate
// Only a transaction-local TEMP table is created, and every mutation is rolled back.
func TestTempUnschedulablePredicate_Postgres(t *testing.T) {
	dsn := os.Getenv("QQQ2API_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set QQQ2API_TEST_POSTGRES_DSN to an isolated PostgreSQL fixture to exercise generated SQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE paid_credits_scheduler_fixtures (
		name text PRIMARY KEY, platform text, type text, parent_account_id bigint,
		temp_unschedulable_until timestamptz, temp_unschedulable_reason text, extra jsonb
	) ON COMMIT DROP`)
	require.NoError(t, err)
	var now time.Time
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT NOW()").Scan(&now))

	fresh := now.Add(-time.Minute).Unix()
	threshold := `{"source":"account_scheduling_threshold","message":"quota threshold"}`
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	parent := int64(1)
	type fixture struct {
		name        string
		snapshot    any
		want        bool
		platform    string
		accountType string
		parent      *int64
		reason      string
		until       *time.Time
	}
	snapshot := func(balance any) map[string]any {
		return map[string]any{"fetched_at": fresh, "balance": balance}
	}
	flagged := func(key string, value any) map[string]any {
		s := snapshot("1.25")
		s[key] = value
		return s
	}
	fixtures := []fixture{
		{name: "positive integer", snapshot: snapshot(10), want: true},
		{name: "positive decimal", snapshot: snapshot(1.25), want: true},
		{name: "decimal string", snapshot: snapshot("1.25"), want: true},
		{name: "decimal string without integer", snapshot: snapshot(".25"), want: true},
		{name: "decimal string trailing dot", snapshot: snapshot("2."), want: true},
		{name: "scientific string", snapshot: snapshot("1.25e+2"), want: true},
		{name: "whitespace string", snapshot: snapshot(" \t+1.25 \n"), want: true},
		{name: "small positive", snapshot: snapshot("3e-324"), want: true},
		{name: "max finite float", snapshot: snapshot("1.7976931348623157e308"), want: true},
		{name: "missing has credits", snapshot: snapshot("5"), want: true},
		{name: "has credits", snapshot: flagged("has_credits", true), want: true},
		{name: "has credits false", snapshot: flagged("has_credits", false)},
		{name: "has credits string false", snapshot: flagged("has_credits", "false"), want: true},
		{name: "overage", snapshot: flagged("overage_limit_reached", true)},
		{name: "overage string true", snapshot: flagged("overage_limit_reached", "true"), want: true},
		{name: "unlimited", snapshot: map[string]any{"fetched_at": fresh, "unlimited": true}, want: true},
		{name: "unlimited string", snapshot: map[string]any{"fetched_at": fresh, "unlimited": "true"}},
		{name: "unlimited no credits", snapshot: map[string]any{"fetched_at": fresh, "unlimited": true, "has_credits": false}},
		{name: "unlimited overage", snapshot: map[string]any{"fetched_at": fresh, "unlimited": true, "overage_limit_reached": true}},
		{name: "zero", snapshot: snapshot(0)},
		{name: "negative", snapshot: snapshot(-1)},
		{name: "NaN", snapshot: snapshot("NaN")},
		{name: "infinity", snapshot: snapshot("Infinity")},
		{name: "overflow", snapshot: snapshot("1e9999999999999999999999")},
		{name: "underflow", snapshot: snapshot("1e-9999999999999999999999")},
		{name: "underflow near zero", snapshot: snapshot("1e-324")},
		{name: "malformed balance", snapshot: snapshot("not-a-number")},
		{name: "empty balance", snapshot: snapshot("")},
		{name: "null balance", snapshot: snapshot(nil)},
		{name: "boolean balance", snapshot: snapshot(true)},
		{name: "array balance", snapshot: snapshot([]any{1})},
		{name: "object balance", snapshot: snapshot(map[string]any{"value": 1})},
		{name: "missing timestamp", snapshot: map[string]any{"balance": 1}},
		{name: "null timestamp", snapshot: flagged("fetched_at", nil)},
		{name: "malformed timestamp", snapshot: flagged("fetched_at", "broken")},
		{name: "overflow timestamp", snapshot: flagged("fetched_at", strings.Repeat("9", 1000))},
		{name: "array timestamp", snapshot: flagged("fetched_at", []any{fresh})},
		{name: "object timestamp", snapshot: flagged("fetched_at", map[string]any{"value": fresh})},
		{name: "boolean timestamp", snapshot: flagged("fetched_at", true)},
		{name: "future timestamp", snapshot: flagged("fetched_at", now.Add(time.Minute).Unix())},
		{name: "stale timestamp", snapshot: flagged("fetched_at", now.Add(-2*time.Hour).Unix())},
		{name: "freshness near boundary", snapshot: flagged("fetched_at", now.Add(-119*time.Minute).Unix()), want: true},
		{name: "JSON number timestamp", snapshot: flagged("fetched_at", json.Number(strconv.FormatInt(fresh, 10))), want: true},
		{name: "integer string timestamp", snapshot: flagged("fetched_at", strconv.FormatInt(fresh, 10)), want: true},
		{name: "whitespace timestamp", snapshot: flagged("fetched_at", " \t+"+strconv.FormatInt(fresh, 10)+" \n"), want: true},
		{name: "fractional numeric timestamp", snapshot: flagged("fetched_at", float64(fresh)+0.5), want: true},
		{name: "fractional string timestamp", snapshot: flagged("fetched_at", strconv.FormatInt(fresh, 10)+".5")},
		{name: "null snapshot"},
		{name: "string snapshot", snapshot: "malformed"},
		{name: "array snapshot", snapshot: []any{snapshot(10)}},
		{name: "no pause no snapshot", until: &past, want: true},
		{name: "no pause malformed snapshot", until: &past, snapshot: flagged("fetched_at", "broken"), want: true},
		{name: "other platform threshold", platform: service.PlatformAnthropic, snapshot: snapshot(10)},
		{name: "other platform healthy", platform: service.PlatformAnthropic, until: &past, want: true},
		{name: "API key threshold", accountType: service.AccountTypeAPIKey, snapshot: snapshot(10)},
		{name: "shadow threshold", parent: &parent, snapshot: snapshot(10)},
		{name: "authentication pause", reason: "oauth refresh failed", snapshot: snapshot(10)},
		{name: "custom pause", reason: `{"source":"custom","message":"quota threshold"}`, snapshot: snapshot(10)},
	}
	goSelected := make(map[string]bool)
	for _, f := range fixtures {
		if f.platform == "" {
			f.platform = service.PlatformOpenAI
		}
		if f.accountType == "" {
			f.accountType = service.AccountTypeOAuth
		}
		if f.reason == "" {
			f.reason = threshold
		}
		if f.until == nil {
			f.until = &future
		}
		extra, err := json.Marshal(map[string]any{"codex_paid_credits_snapshot": f.snapshot})
		require.NoError(t, err, f.name)
		var decodedExtra map[string]any
		require.NoError(t, json.Unmarshal(extra, &decodedExtra))
		account := &service.Account{
			Platform: f.platform, Type: f.accountType, Status: service.StatusActive, Schedulable: true,
			ParentAccountID: f.parent, TempUnschedulableUntil: f.until, TempUnschedulableReason: f.reason,
			Extra: decodedExtra,
		}
		goSelected[f.name] = account.IsSchedulable()
		_, err = tx.ExecContext(ctx, `INSERT INTO paid_credits_scheduler_fixtures VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)`, f.name, f.platform, f.accountType, f.parent, f.until, f.reason, extra)
		require.NoError(t, err, f.name)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO paid_credits_scheduler_fixtures VALUES ('never paused', 'anthropic', 'oauth', NULL, NULL, NULL, NULL)`)
	require.NoError(t, err)
	query, args := paidCreditsSchedulingTestQuery()
	rows, err := tx.QueryContext(ctx, query, args...)
	require.NoError(t, err, "the complete Ent selector must compile and execute even for healthy/non-OpenAI rows")
	defer rows.Close()
	selected := make(map[string]bool)
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		selected[name] = true
	}
	require.NoError(t, rows.Err())
	require.True(t, selected["never paused"])
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			require.Equal(t, f.want, selected[f.name])
			require.Equal(t, goSelected[f.name], selected[f.name], "database and in-memory scheduler decisions must agree")
		})
	}
}
