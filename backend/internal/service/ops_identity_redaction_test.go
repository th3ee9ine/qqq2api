package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type opsIdentityOutputRepo struct {
	OpsRepository
	rules        []*OpsAlertRule
	events       []*OpsAlertEvent
	createdRule  *OpsAlertRule
	createdEvent *OpsAlertEvent
}

func (r *opsIdentityOutputRepo) ListAlertRules(context.Context) ([]*OpsAlertRule, error) {
	return r.rules, nil
}

func (r *opsIdentityOutputRepo) CreateAlertRule(_ context.Context, rule *OpsAlertRule) (*OpsAlertRule, error) {
	r.createdRule = rule
	return rule, nil
}

func (r *opsIdentityOutputRepo) ListAlertEvents(context.Context, *OpsAlertEventFilter) ([]*OpsAlertEvent, error) {
	return r.events, nil
}

func (r *opsIdentityOutputRepo) CreateAlertEvent(_ context.Context, event *OpsAlertEvent) (*OpsAlertEvent, error) {
	r.createdEvent = event
	return event, nil
}

func assertNoOpsUserIdentityKeys(t *testing.T, value any) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			require.NotContains(t, []string{"user_id", "user_email", "user_query", "username"}, key)
			assertNoOpsUserIdentityKeys(t, nested)
		}
	case []any:
		for _, nested := range typed {
			assertNoOpsUserIdentityKeys(t, nested)
		}
	}
}

func marshalOpsDTO(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	return decoded
}

func TestOpsDTOsHideEndUserIdentityAndKeepAuditFields(t *testing.T) {
	userID := int64(7)
	apiKeyID := int64(11)
	resolvedBy := int64(13)

	errorLog := marshalOpsDTO(t, &OpsErrorLog{
		UserID:             &userID,
		APIKeyID:           &apiKeyID,
		UserAgent:          "curl/8.0",
		ResolvedByUserID:   &resolvedBy,
		ResolvedByUserName: "admin@example.com",
	})
	assertNoOpsUserIdentityKeys(t, errorLog)
	require.Equal(t, "curl/8.0", errorLog["user_agent"])
	require.Equal(t, float64(resolvedBy), errorLog["resolved_by_user_id"])
	require.Equal(t, "admin@example.com", errorLog["resolved_by_user_name"])
	require.Equal(t, float64(apiKeyID), errorLog["api_key_id"])

	for name, dto := range map[string]any{
		"system log":     &OpsSystemLog{UserID: &userID, APIKeyID: &apiKeyID},
		"request detail": &OpsRequestDetail{UserID: &userID, APIKeyID: &apiKeyID},
		"ingress reject": &OpsIngressRejectAggregate{UserID: &userID, APIKeyID: &apiKeyID},
	} {
		t.Run(name, func(t *testing.T) {
			decoded := marshalOpsDTO(t, dto)
			assertNoOpsUserIdentityKeys(t, decoded)
			require.Equal(t, float64(apiKeyID), decoded["api_key_id"])
		})
	}
}

func TestOpsSystemLogIdentityRedactionIsRecursiveAndNonMutating(t *testing.T) {
	userID := int64(7)
	extra := map[string]any{
		"user_id":       userID,
		"user_email":    "user@example.com",
		"user_query":    "ordinary-user",
		"username":      "ordinary-user",
		"user_agent":    "curl/8.0",
		"actor_user_id": int64(99),
		"nested": []any{map[string]any{
			"user_id":       int64(8),
			"user_email":    "nested@example.com",
			"username":      "nested-user",
			"operator_id":   int64(99),
			"updated_by_id": int64(99),
		}},
	}
	repo := &opsRepoMock{ListSystemLogsFn: func(context.Context, *OpsSystemLogFilter) (*OpsSystemLogList, error) {
		return &OpsSystemLogList{Logs: []*OpsSystemLog{{UserID: &userID, Extra: extra}}}, nil
	}}
	svc := NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	result, err := svc.ListSystemLogs(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)
	require.Nil(t, result.Logs[0].UserID)
	assertNoOpsUserIdentityKeys(t, result.Logs[0].Extra)
	require.Equal(t, "curl/8.0", result.Logs[0].Extra["user_agent"])
	require.Equal(t, int64(99), result.Logs[0].Extra["actor_user_id"])

	// Sanitizing a response must not mutate the repository-owned snapshot.
	require.Contains(t, extra, "user_id")
	require.Contains(t, extra, "user_email")
	require.Contains(t, extra, "user_query")
	require.Contains(t, extra, "username")
}

func TestOpsAlertPayloadsDropUserIdentityDimensions(t *testing.T) {
	rule := &OpsAlertRule{Filters: map[string]any{
		"platform":   "openai",
		"group_id":   int64(3),
		"region":     "us",
		"user_id":    int64(7),
		"user_email": "user@example.com",
		"user_query": "ordinary-user",
	}}
	event := &OpsAlertEvent{Dimensions: map[string]any{
		"platform":      "openai",
		"group_id":      int64(3),
		"user_id":       int64(7),
		"nested":        map[string]any{"user_email": "user@example.com", "username": "ordinary-user"},
		"actor_user_id": int64(99),
	}}
	repo := &opsIdentityOutputRepo{rules: []*OpsAlertRule{rule}, events: []*OpsAlertEvent{event}}
	svc := NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rules, err := svc.ListAlertRules(context.Background())
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, map[string]any{"platform": "openai", "group_id": int64(3), "region": "us"}, rules[0].Filters)
	require.Contains(t, rule.Filters, "user_id")

	createdRule, err := svc.CreateAlertRule(context.Background(), rule)
	require.NoError(t, err)
	require.Equal(t, rules[0].Filters, repo.createdRule.Filters)
	require.Equal(t, rules[0].Filters, createdRule.Filters)

	events, err := svc.ListAlertEvents(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assertNoOpsUserIdentityKeys(t, events[0].Dimensions)
	require.Equal(t, int64(99), events[0].Dimensions["actor_user_id"])
	require.Contains(t, event.Dimensions, "user_id")

	createdEvent, err := svc.CreateAlertEvent(context.Background(), event)
	require.NoError(t, err)
	assertNoOpsUserIdentityKeys(t, repo.createdEvent.Dimensions)
	assertNoOpsUserIdentityKeys(t, createdEvent.Dimensions)
}

func TestNormalizeOpsRuntimeLogConfigDropsUserIdentityExtra(t *testing.T) {
	defaults := &OpsRuntimeLogConfig{
		Level:           "info",
		SamplingInitial: 100,
		SamplingNext:    100,
		StacktraceLevel: "error",
		RetentionDays:   30,
	}
	cfg := &OpsRuntimeLogConfig{
		Extra: map[string]any{
			"user_id":            int64(7),
			"user_email":         "user@example.com",
			"user_query":         "ordinary-user",
			"username":           "ordinary-user",
			"user_agent":         "curl/8.0",
			"updated_by_user_id": int64(99),
		},
	}

	normalizeOpsRuntimeLogConfig(cfg, defaults)
	assertNoOpsUserIdentityKeys(t, cfg.Extra)
	require.Equal(t, "curl/8.0", cfg.Extra["user_agent"])
	require.Equal(t, int64(99), cfg.Extra["updated_by_user_id"])
}
