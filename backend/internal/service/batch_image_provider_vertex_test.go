//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVertexBatchDisplayNameUsesNeutralDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input BatchImageInput
		want  string
	}{
		{name: "explicit display name", input: BatchImageInput{DisplayName: " custom-job "}, want: "custom-job"},
		{name: "batch id", input: BatchImageInput{BatchID: "batch-123"}, want: "image-batch-batch-123"},
		{name: "fallback", input: BatchImageInput{}, want: "image-batch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := vertexBatchDisplayName(tt.input)
			require.Equal(t, tt.want, got)
			require.NotContains(t, strings.ToLower(got), "sub2api")
		})
	}
}
