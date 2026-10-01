package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/th3ee9ine/qqq2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	middleware2 "github.com/th3ee9ine/qqq2api/internal/server/middleware"
)

// 通配条目在 /v1/models 中展开为候选来源中所有匹配项，保持来源顺序。
func TestGatewayModels_ModelAllowlistWildcardExpandsAgainstSource(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(31)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformOpenAI,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"gpt-5.4":       "gpt-5.4",
								"gpt-5.5-codex": "gpt-5.5-codex",
								"gpt-5.5-mini":  "gpt-5.5-mini",
								"other-foo":     "other-foo",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformOpenAI,
			ModelAllowlist: service.GroupModelAllowlist{
				Enabled: true,
				Models:  []string{"gpt-5.5-*", "gpt-5.4"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"gpt-5.5-codex", "gpt-5.5-mini", "gpt-5.4"}, modelIDsForTest(got.Data))
}

// geminiAllowlistAccountRepoStub 在 gatewayModelsAccountRepoStub 之上补充
// Gemini 兼容层用到的按平台过滤查询（分组内无任何账号，触发 fallback 列表）。
type geminiAllowlistAccountRepoStub struct {
	gatewayModelsAccountRepoStub
}

func (s *geminiAllowlistAccountRepoStub) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]service.Account, error) {
	allowed := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		allowed[platform] = struct{}{}
	}
	accounts := s.byGroup[groupID]
	filtered := make([]service.Account, 0, len(accounts))
	for _, account := range accounts {
		if _, ok := allowed[account.Platform]; ok {
			filtered = append(filtered, account)
		}
	}
	return filtered, nil
}
