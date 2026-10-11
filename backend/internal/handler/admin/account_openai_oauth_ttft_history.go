package admin

import (
	"context"
	"log/slog"

	"github.com/th3ee9ine/qqq2api/internal/service"
)

// OpenAIOAuthTTFTHistoryReader provides recent actual-call latency for accounts
// already authorized by the account management service. It owns enabled and
// account-eligibility checks; handlers do not perform additional account reads.
type OpenAIOAuthTTFTHistoryReader interface {
	GetOpenAIOAuthTTFTHistory(context.Context, []service.Account) (map[int64]*service.OpenAIOAuthTTFTHistory, error)
}

// SetOpenAIOAuthTTFTHistoryReader attaches optional account latency history.
func (h *AccountHandler) SetOpenAIOAuthTTFTHistoryReader(reader OpenAIOAuthTTFTHistoryReader) {
	h.openAIOAuthTTFTHistory = reader
}

func (h *AccountHandler) readOpenAIOAuthTTFTHistory(ctx context.Context, accounts []service.Account) map[int64]*service.OpenAIOAuthTTFTHistory {
	if h == nil || h.openAIOAuthTTFTHistory == nil || len(accounts) == 0 {
		return nil
	}
	history, err := h.openAIOAuthTTFTHistory.GetOpenAIOAuthTTFTHistory(ctx, accounts)
	if err != nil {
		slog.WarnContext(ctx, "account_openai_oauth_ttft_history_failed", "error", err)
		return nil
	}
	return history
}
