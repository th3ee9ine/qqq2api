package service

import (
	"github.com/th3ee9ine/qqq2api/internal/pkg/xai"
)

// defaultGrokUpstreamUserAgent is the pinned Grok CLI / workspace UA.
// Grok upstream must not forward Claude Code / Codex / browser client UAs.
func defaultGrokUpstreamUserAgent() string {
	return xai.CLIUserAgent(xai.ResolveCLIVersion())
}
