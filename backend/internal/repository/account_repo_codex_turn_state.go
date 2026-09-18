package repository

import (
	"context"

	dbaccount "github.com/th3ee9ine/qqq2api/ent/account"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

var _ service.CodexTurnStateSourceRepository = (*accountRepository)(nil)

// Refresh just the selected account's managed states, without credentials,
// proxies or group joins. The normal soft-delete filter still applies.
func (r *accountRepository) GetCodexTurnStateSource(ctx context.Context, id int64) (*service.Account, error) {
	row, err := r.client.Account.Query().Where(dbaccount.IDEQ(id)).Select(dbaccount.FieldID, dbaccount.FieldPlatform, dbaccount.FieldType, dbaccount.FieldExtra).Only(ctx)
	if err != nil {
		return nil, err
	}
	return &service.Account{ID: row.ID, Platform: row.Platform, Type: row.Type, Extra: row.Extra}, nil
}
