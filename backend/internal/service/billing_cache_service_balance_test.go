//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

type balanceEligibilityCacheStub struct {
	billingCacheWorkerStub
	balance float64
}

func (s *balanceEligibilityCacheStub) GetUserBalance(context.Context, int64) (float64, error) {
	return s.balance, nil
}

func (s *balanceEligibilityCacheStub) DeductUserBalance(context.Context, int64, float64) error {
	return nil
}

func (s *balanceEligibilityCacheStub) InvalidateUserBalance(context.Context, int64) error { return nil }

func TestCheckBillingEligibility_RejectsBalanceBelowMinimumReserve(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0.005}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, nil, nil, "")
	require.ErrorIs(t, err, ErrInsufficientBalance)
}

func TestCheckBillingEligibility_AllowsBalanceAtMinimumReserve(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0.01}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, nil, nil, "")
	require.NoError(t, err)
}
