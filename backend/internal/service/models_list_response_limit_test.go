package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

func TestResolveModelsListReadLimit(t *testing.T) {
	t.Run("nil config uses default", func(t *testing.T) {
		require.Equal(t, config.DefaultModelsListReadMaxBytes, resolveModelsListReadLimit(nil))
	})

	t.Run("configured limit wins", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Gateway.ModelsListReadMaxBytes = 16 << 20
		require.Equal(t, int64(16<<20), resolveModelsListReadLimit(cfg))
	})

	t.Run("non-positive limit uses default", func(t *testing.T) {
		cfg := &config.Config{}
		require.Equal(t, config.DefaultModelsListReadMaxBytes, resolveModelsListReadLimit(cfg))
	})
}
