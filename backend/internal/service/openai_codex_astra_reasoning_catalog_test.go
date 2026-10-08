package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompleteAPIKeyCodexModelsManifestMetadataRepairsOfficialAstraFalseCapabilities(t *testing.T) {
	t.Parallel()

	input := []byte(`{"models":[{"slug":"gpt-6-astra","supports_reasoning_effort_updates":false,"supports_reasoning_summaries":false}]}`)
	official := newCodexModelsAPIKeyTestAccount("https://api.openai.com/v1")
	custom := newCodexModelsAPIKeyTestAccount("https://relay.example/v1")

	for _, tc := range []struct {
		name    string
		account *Account
		want    bool
	}{
		{name: "official OpenAI", account: official, want: true},
		{name: "custom relay", account: custom, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := &OpenAIModelsResponse{Body: append([]byte(nil), input...)}
			completed, err := completeAPIKeyCodexModelsManifestMetadata(manifest.Body, true, tc.account)
			require.NoError(t, err)
			models := decodeCodexManifestModels(t, completed)
			require.Len(t, models, 1)
			require.Equal(t, tc.want, models[0]["supports_reasoning_effort_updates"])
			require.Equal(t, tc.want, models[0]["supports_reasoning_summaries"])
		})
	}
}
