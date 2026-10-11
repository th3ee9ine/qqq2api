package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type ttftGuardAccountRepoStub struct {
	AccountRepository
	mu      sync.Mutex
	pauses  int
	model   string
	next    time.Time
	changed bool
}

func (r *ttftGuardAccountRepoStub) PauseOpenAIOAuthTTFT(_ context.Context, _ int64, model string, _ time.Time, next time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pauses++
	r.model = model
	r.next = next
	return r.changed, nil
}

func (r *ttftGuardAccountRepoStub) ClaimOpenAIOAuthTTFTProbe(context.Context, time.Time, string, time.Time) (*Account, error) {
	return nil, nil
}

func (r *ttftGuardAccountRepoStub) FinishOpenAIOAuthTTFTProbe(context.Context, int64, string, bool, time.Time) (bool, error) {
	return false, nil
}

func TestObserveOpenAIOAuthTTFTPausesAfterFiveStrictlySlowCalls(t *testing.T) {
	repo := &ttftGuardAccountRepoStub{changed: true}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	latency := 15_001
	for i := 0; i < openAIOAuthTTFTSlowStreakThreshold-1; i++ {
		svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.3-codex", &latency)
	}
	repo.mu.Lock()
	require.Zero(t, repo.pauses)
	repo.mu.Unlock()

	svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.3-codex", &latency)
	repo.mu.Lock()
	require.Equal(t, 1, repo.pauses)
	require.Equal(t, "gpt-5.3-codex", repo.model)
	require.True(t, repo.next.After(time.Now()))
	repo.mu.Unlock()
}

func TestObserveOpenAIOAuthTTFTFastCallBreaksStreakAndSkipsUnsupportedAccounts(t *testing.T) {
	repo := &ttftGuardAccountRepoStub{changed: true}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 43, Platform: PlatformOpenAI, Type: AccountTypeSetupToken}
	slow := 15_001
	fast := 15_000
	for i := 0; i < 4; i++ {
		svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.3-codex", &slow)
	}
	svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.3-codex", &fast)
	svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.3-codex", &slow)
	repo.mu.Lock()
	require.Zero(t, repo.pauses)
	repo.mu.Unlock()

	apiKey := &Account{ID: 44, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	for i := 0; i < openAIOAuthTTFTSlowStreakThreshold; i++ {
		svc.ObserveOpenAIOAuthTTFT(apiKey, "gpt-5.3-codex", &slow)
	}
	shadowID := int64(45)
	shadow := &Account{ID: 45, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &shadowID}
	for i := 0; i < openAIOAuthTTFTSlowStreakThreshold; i++ {
		svc.ObserveOpenAIOAuthTTFT(shadow, "gpt-5.3-codex", &slow)
	}
	repo.mu.Lock()
	require.Zero(t, repo.pauses)
	repo.mu.Unlock()
}
