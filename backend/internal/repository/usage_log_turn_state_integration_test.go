//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestUsageLog_TurnStatePersistence(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := newUsageLogRepositoryWithSQL(client, integrationDB)
	user := mustCreateUser(t, client, &service.User{Email: "turn-state-" + uuid.NewString() + "@example.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-test-" + uuid.NewString(), Name: "turn-state"})
	account := mustCreateAccount(t, client, &service.Account{Name: "turn-state-" + uuid.NewString()})
	empty, state := "", "test-outbound-state"
	for _, mode := range []string{"single", "batch", "best-effort", "fallback"} {
		for _, value := range []*string{nil, &empty, &state} {
			log := &service.UsageLog{
				UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(),
				Model: "gpt-5.5", UpstreamTurnState: value, CreatedAt: time.Now().UTC(),
			}
			var err error
			switch mode {
			case "single":
				_, err = repo.createSingle(ctx, integrationDB, log)
			case "batch":
				_, err = repo.Create(ctx, log)
			case "best-effort":
				err = repo.CreateBestEffort(ctx, log)
			case "fallback":
				err = execUsageLogInsertNoResult(ctx, integrationDB, prepareUsageLogInsert(log))
			}
			require.NoError(t, err, mode)
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT id FROM usage_logs WHERE request_id = $1 AND api_key_id = $2", log.RequestID, key.ID).Scan(&log.ID))
			got, err := repo.GetByID(ctx, log.ID)
			require.NoError(t, err, mode)
			require.Equal(t, value, got.UpstreamTurnState, mode)
		}
	}
}
