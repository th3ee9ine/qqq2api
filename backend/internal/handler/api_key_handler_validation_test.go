//go:build unit

package handler

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateAPIKeyCreateRequest(t *testing.T) {
	zero, large, negative, nan, inf := 0.0, 1e100, -1.0, math.NaN(), math.Inf(1)
	zeroConcurrency, positiveConcurrency, negativeConcurrency := 0, 8, -1
	positiveDays, zeroDays, negativeDays := 1, 0, -1
	require.NoError(t, validateAPIKeyCreateRequest(CreateAPIKeyRequest{}))
	require.NoError(t, validateAPIKeyCreateRequest(CreateAPIKeyRequest{Quota: &zero, RateLimit5h: &large, ExpiresInDays: &positiveDays}))
	require.NoError(t, validateAPIKeyCreateRequest(CreateAPIKeyRequest{Concurrency: &zeroConcurrency}))
	require.NoError(t, validateAPIKeyCreateRequest(CreateAPIKeyRequest{Concurrency: &positiveConcurrency}))

	for _, req := range []CreateAPIKeyRequest{
		{Quota: &negative},
		{Quota: &nan},
		{RateLimit5h: &inf},
		{RateLimit1d: &negative},
		{RateLimit7d: &negative},
		{ExpiresInDays: &zeroDays},
		{ExpiresInDays: &negativeDays},
		{Concurrency: &negativeConcurrency},
	} {
		require.Error(t, validateAPIKeyCreateRequest(req))
	}
}

func TestValidateAPIKeyUpdateRequest(t *testing.T) {
	zero, large, negative, nan, inf := 0.0, 1e100, -1.0, math.NaN(), math.Inf(-1)
	zeroConcurrency, positiveConcurrency, negativeConcurrency := 0, 8, -1
	require.NoError(t, validateAPIKeyUpdateRequest(UpdateAPIKeyRequest{Quota: &zero, RateLimit7d: &large}))
	require.NoError(t, validateAPIKeyUpdateRequest(UpdateAPIKeyRequest{Concurrency: &zeroConcurrency}))
	require.NoError(t, validateAPIKeyUpdateRequest(UpdateAPIKeyRequest{Concurrency: &positiveConcurrency}))

	for _, req := range []UpdateAPIKeyRequest{
		{Quota: &negative},
		{RateLimit5h: &nan},
		{RateLimit1d: &inf},
		{RateLimit7d: &negative},
		{Concurrency: &negativeConcurrency},
	} {
		require.Error(t, validateAPIKeyUpdateRequest(req))
	}
}
