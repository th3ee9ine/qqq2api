package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestShouldMarkOpenAICodexTurnStateUsageVerification(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const validBody = `{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply with exactly OK."}]}]}`
	for _, tc := range []struct {
		name          string
		path          string
		body          string
		nativeState   string
		apiKeyID      int64
		groupID       int64
		groupObjectID int64
		hydrated      bool
		exclusive     bool
		want          bool
	}{
		{name: "dedicated-key-exact-request", path: "/v1/responses", body: validBody, apiKeyID: 701, groupID: 88, groupObjectID: 88, hydrated: true, exclusive: true, want: true},
		{name: "ordinary-prompt", path: "/v1/responses", body: `{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Explain this code."}]}]}`, apiKeyID: 701, groupID: 88, groupObjectID: 88, hydrated: true, exclusive: true},
		{name: "native-state", path: "/v1/responses", body: validBody, nativeState: "official-client-state", apiKeyID: 701, groupID: 88, groupObjectID: 88, hydrated: true, exclusive: true},
		{name: "compact-endpoint", path: "/v1/responses/compact", body: validBody, apiKeyID: 701, groupID: 88, groupObjectID: 88, hydrated: true, exclusive: true},
		{name: "non-exclusive-group", path: "/v1/responses", body: validBody, apiKeyID: 701, groupID: 88, groupObjectID: 88, hydrated: true},
		{name: "unhydrated-group", path: "/v1/responses", body: validBody, apiKeyID: 701, groupID: 88, groupObjectID: 88, exclusive: true},
		{name: "mismatched-group", path: "/v1/responses", body: validBody, apiKeyID: 701, groupID: 88, groupObjectID: 89, hydrated: true, exclusive: true},
		{name: "missing-api-key-id", path: "/v1/responses", body: validBody, groupID: 88, groupObjectID: 88, hydrated: true, exclusive: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, nil)
			if tc.nativeState != "" {
				c.Request.Header.Set("x-codex-turn-state", tc.nativeState)
			}
			groupID := tc.groupID
			apiKey := &service.APIKey{
				ID:      tc.apiKeyID,
				GroupID: &groupID,
				Group: &service.Group{
					ID:          tc.groupObjectID,
					Hydrated:    tc.hydrated,
					IsExclusive: tc.exclusive,
				},
			}
			require.Equal(t, tc.want, shouldMarkOpenAICodexTurnStateUsageVerification(c, apiKey, []byte(tc.body)))
		})
	}
}
