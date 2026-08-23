package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllPlatformsIncludesOnlyActivePlatforms(t *testing.T) {
	require.ElementsMatch(t, []string{
		"anthropic",
		"openai",
	}, AllPlatforms())
}

func TestErrorPassthroughRuleRejectsRetiredPlatform(t *testing.T) {
	rule := &ErrorPassthroughRule{
		Name:            "legacy",
		MatchMode:       MatchModeAny,
		ErrorCodes:      []int{429},
		Platforms:       []string{" GLM "},
		PassthroughCode: true,
		PassthroughBody: true,
	}
	require.Error(t, rule.Validate())
}
