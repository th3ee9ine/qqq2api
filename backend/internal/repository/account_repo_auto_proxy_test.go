//go:build unit

package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func singleAutomaticTargets(ids ...int64) []automaticProxyTarget {
	targets := make([]automaticProxyTarget, 0, len(ids))
	for _, id := range ids {
		targets = append(targets, automaticProxyTarget{AccountID: id, MemberIDs: []int64{id}})
	}
	return targets
}

func assignmentProxyIDs(assignments []proxyAccountAssignment) []int64 {
	ids := make([]int64, 0, len(assignments))
	for _, assignment := range assignments {
		ids = append(ids, assignment.ProxyID)
	}
	return ids
}

func TestPlanAutomaticProxyAssignmentsBalancesLeastUsedFirst(t *testing.T) {
	assignments, err := planAutomaticProxyAssignments([]automaticProxyCapacity{
		{ID: 1, MaxAccounts: 10, AccountCount: 2},
		{ID: 2, MaxAccounts: 10, AccountCount: 0},
	}, singleAutomaticTargets(101, 102, 103))

	require.NoError(t, err)
	require.Equal(t, []int64{2, 2, 1}, assignmentProxyIDs(assignments))
}

func TestPlanAutomaticProxyAssignmentsBreaksTiesByProxyID(t *testing.T) {
	assignments, err := planAutomaticProxyAssignments([]automaticProxyCapacity{
		{ID: 20, MaxAccounts: 5, AccountCount: 1},
		{ID: 10, MaxAccounts: 5, AccountCount: 1},
	}, singleAutomaticTargets(101))

	require.NoError(t, err)
	require.Equal(t, []int64{10}, assignmentProxyIDs(assignments))
}

func TestPlanAutomaticProxyAssignmentsHonorsFiniteLimits(t *testing.T) {
	assignments, err := planAutomaticProxyAssignments([]automaticProxyCapacity{
		{ID: 1, MaxAccounts: 2, AccountCount: 1},
		{ID: 2, MaxAccounts: 1, AccountCount: 0},
	}, singleAutomaticTargets(101, 102))

	require.NoError(t, err)
	require.Equal(t, []int64{2, 1}, assignmentProxyIDs(assignments))
}

func TestPlanAutomaticProxyAssignmentsTreatsZeroAsUnlimited(t *testing.T) {
	assignments, err := planAutomaticProxyAssignments([]automaticProxyCapacity{
		{ID: 1, MaxAccounts: 0, AccountCount: 100},
		{ID: 2, MaxAccounts: 1, AccountCount: 0},
	}, singleAutomaticTargets(101, 102))

	require.NoError(t, err)
	require.Equal(t, []int64{2, 1}, assignmentProxyIDs(assignments))
}

func TestPlanAutomaticProxyAssignmentsRejectsInsufficientCapacity(t *testing.T) {
	assignments, err := planAutomaticProxyAssignments([]automaticProxyCapacity{
		{ID: 1, MaxAccounts: 1, AccountCount: 1},
		{ID: 2, MaxAccounts: 2, AccountCount: 1},
	}, singleAutomaticTargets(101, 102))

	require.Nil(t, assignments)
	require.ErrorIs(t, err, service.ErrProxyCapacityInsufficient)
	var appErr *infraerrors.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.Equal(t, "2", appErr.Metadata["required_accounts"])
	require.Equal(t, "1", appErr.Metadata["available_accounts"])
}

func TestPlanAutomaticProxyAssignmentsCountsShadowFamilyWeight(t *testing.T) {
	assignments, err := planAutomaticProxyAssignments([]automaticProxyCapacity{
		{ID: 1, MaxAccounts: 1, AccountCount: 0},
		{ID: 2, MaxAccounts: 3, AccountCount: 1},
	}, []automaticProxyTarget{
		{AccountID: 101, MemberIDs: []int64{101, 201}},
		{AccountID: 102, MemberIDs: []int64{102}},
	})

	require.NoError(t, err)
	require.Equal(t, []proxyAccountAssignment{
		{AccountID: 101, ProxyID: 2},
		{AccountID: 201, ProxyID: 2},
		{AccountID: 102, ProxyID: 1},
	}, assignments)
}

func TestPlanAutomaticProxyAssignmentsRejectsWhenNoActiveCapacityProvided(t *testing.T) {
	assignments, err := planAutomaticProxyAssignments(nil, singleAutomaticTargets(101))

	require.Nil(t, assignments)
	require.ErrorIs(t, err, service.ErrProxyCapacityInsufficient)
}

func TestBulkAutoProxyCapacityFailurePerformsNoUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`(?s)SELECT id, max_accounts.*status = \$1.*\(expires_at IS NULL OR expires_at > NOW\(\)\).*FOR NO KEY UPDATE`).
		WithArgs(service.StatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"id", "max_accounts"}).AddRow(7, 1))
	mock.ExpectQuery(`(?s)SELECT id, parent_account_id.*\(id = ANY\(\$1\) OR parent_account_id = ANY\(\$1\)\).*FOR UPDATE`).
		WithArgs(pq.Array([]int64{11})).
		WillReturnRows(sqlmock.NewRows([]string{"id", "parent_account_id"}).AddRow(11, nil))
	mock.ExpectQuery(`(?s)SELECT proxy_id, COUNT\(\*\).*AND NOT \(id = ANY\(\$2\)\).*GROUP BY proxy_id`).
		WithArgs(pq.Array([]int64{7}), pq.Array([]int64{11})).
		WillReturnRows(sqlmock.NewRows([]string{"proxy_id", "count"}).AddRow(7, 1))

	repo := newAccountRepositoryWithSQL(nil, db, nil)
	rows, err := repo.BulkUpdate(context.Background(), []int64{11}, service.AccountBulkUpdate{AutoAssignProxy: true})

	require.Zero(t, rows)
	require.ErrorIs(t, err, service.ErrProxyCapacityInsufficient)
	require.NoError(t, mock.ExpectationsWereMet(), "capacity failure must happen before any UPDATE/Exec")
}

func TestBulkAutoProxyRejectsConflictingAssignmentModes(t *testing.T) {
	proxyID := int64(7)
	rows, err := (&accountRepository{}).BulkUpdate(context.Background(), []int64{11}, service.AccountBulkUpdate{
		ProxyID:         &proxyID,
		AutoAssignProxy: true,
	})

	require.Zero(t, rows)
	require.ErrorIs(t, err, service.ErrProxyAssignmentModeConflict)
	require.Equal(t, "PROXY_ASSIGNMENT_MODE_CONFLICT", infraerrors.Reason(err))
}

func TestAutomaticProxyBaselineExcludesReassignedFamily(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	excluded := []int64{11, 21}
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT proxy_id, COUNT(*)
		FROM accounts
		WHERE deleted_at IS NULL
		  AND proxy_id = ANY($1)
		  AND NOT (id = ANY($2))
		GROUP BY proxy_id
	`)).
		WithArgs(pq.Array([]int64{7}), pq.Array(excluded)).
		WillReturnRows(sqlmock.NewRows([]string{"proxy_id", "count"}))

	capacities := []automaticProxyCapacity{{ID: 7, MaxAccounts: 2, AccountCount: 99}}
	err = loadAutomaticProxyBaselineCounts(context.Background(), db, capacities, excluded)

	require.NoError(t, err)
	require.Zero(t, capacities[0].AccountCount, "selected parent and shadow must be removed from the initial load")
	assignments, err := planAutomaticProxyAssignments(capacities, []automaticProxyTarget{{AccountID: 11, MemberIDs: excluded}})
	require.NoError(t, err)
	require.Len(t, assignments, 2)
	require.NoError(t, mock.ExpectationsWereMet())
}
