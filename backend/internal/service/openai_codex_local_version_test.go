package service

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountLocalCodexVersionPriority(t *testing.T) {
	oldLocalEnabled := codexAccountLocalDeviceIdentityEnabled.Load()
	oldEnforcement := codexIdentityEnforcement.Load()
	SetCodexAccountLocalDeviceIdentityEnabled(true)
	SetCodexCanonicalUserAgentResolver(func() string { return "codex-tui/0.300.0 (Linux; x86_64)" })
	SetCodexCanonicalOriginatorResolver(func() string { return "global-client" })
	SetCodexCanonicalResponsesVersionResolver(func() string { return "0.300.0" })
	t.Cleanup(func() {
		SetCodexAccountLocalDeviceIdentityEnabled(oldLocalEnabled)
		SetCodexIdentityEnforcementEnabled(oldEnforcement)
		SetCodexCanonicalUserAgentResolver(nil)
		SetCodexCanonicalOriginatorResolver(nil)
		SetCodexCanonicalResponsesVersionResolver(nil)
	})

	globalIdentity := resolveCodexOutboundIdentity("")
	const localUA = "Codex Desktop/0.146.0 (Mac OS 26.2.0; arm64) unknown (Codex Desktop; 26.820.60940)"
	for _, tt := range []struct {
		name        string
		version     string
		wantVersion string
		wantGlobal  bool
	}{
		{name: "missing version derives engine instead of Desktop host build", wantVersion: "0.146.0"},
		{name: "explicit local version wins over newer global version", version: "0.149.0", wantVersion: "0.149.0"},
		{name: "explicit local version above global", version: "0.301.0", wantVersion: "0.301.0"},
		{name: "explicit local version is not raised to built in floor", version: "0.125.0", wantVersion: "0.125.0"},
		{name: "local prerelease stays a local prerelease", version: "0.147.0-alpha.4", wantVersion: "0.147.0-alpha.4"},
		{name: "surrounding ASCII spaces are normalized", version: " 0.149.0 ", wantVersion: "0.149.0"},
		{name: "malformed explicit version invalidates whole identity", version: "latest", wantGlobal: true},
		{name: "header injection", version: "0.149.0\r\nX-Injected: true", wantGlobal: true},
		{name: "trailing newline", version: "0.149.0\n", wantGlobal: true},
		{name: "leading tab", version: "\t0.149.0", wantGlobal: true},
		{name: "non ASCII whitespace", version: "\u00a00.149.0", wantGlobal: true},
		{name: "whitespace only explicit version", version: " ", wantGlobal: true},
		{name: "overlong explicit version", version: "0.149.0" + strings.Repeat(" ", codexClientVersionMaxLen), wantGlobal: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Extra: map[string]any{
					OpenAILocalDeviceUserAgentExtraKey:  localUA,
					OpenAILocalDeviceOriginatorExtraKey: "Codex Desktop",
					"openai_local_device_version":       tt.version,
				},
			}
			want := globalIdentity
			if !tt.wantGlobal {
				want = codexOutboundIdentity{
					originator: "Codex Desktop",
					userAgent:  strings.Replace(localUA, "/0.146.0 ", "/"+tt.wantVersion+" ", 1),
					version:    tt.wantVersion,
				}
			}
			require.Equal(t, want, resolveCodexOutboundIdentityForAccount(account))

			// Local identity priority is independent of the global enforcement
			// switch. Invalid local values still fall back as one complete tuple.
			for _, enabled := range []bool{true, false} {
				SetCodexIdentityEnforcementEnabled(enabled)
				headers := http.Header{
					"Originator": {"inbound-client"},
					"User-Agent": {"inbound-client/9.9.9"},
					"Version":    {"9.9.9"},
				}
				enforceCodexIdentityHeadersWithAccount(headers, account)
				require.Equal(t, want.originator, headers.Get("Originator"))
				require.Equal(t, want.userAgent, headers.Get("User-Agent"))
				require.Equal(t, want.version, headers.Get("Version"))
			}
		})
	}

	t.Run("disabled local identity keeps the complete global tuple", func(t *testing.T) {
		SetCodexAccountLocalDeviceIdentityEnabled(false)
		defer SetCodexAccountLocalDeviceIdentityEnabled(true)
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Extra: map[string]any{
				OpenAILocalDeviceUserAgentExtraKey:  localUA,
				OpenAILocalDeviceOriginatorExtraKey: "Codex Desktop",
				"openai_local_device_version":       "0.149.0",
			},
		}
		require.Equal(t, globalIdentity, resolveCodexOutboundIdentityForAccount(account))
	})

	t.Run("invalid Originator pair does not retain local Version", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Extra: map[string]any{
				OpenAILocalDeviceUserAgentExtraKey:  localUA,
				OpenAILocalDeviceOriginatorExtraKey: "codex-tui",
				"openai_local_device_version":       "0.149.0",
			},
		}
		require.Equal(t, globalIdentity, resolveCodexOutboundIdentityForAccount(account))
	})

	t.Run("legacy credential identity retains its engine version with enforcement disabled", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Credentials: map[string]any{
				"user_agent": "codex-tui/0.146.0 (Linux; arm64)",
				"originator": "codex-tui",
				"version":    "0.149.0",
			},
		}
		SetCodexIdentityEnforcementEnabled(false)
		headers := http.Header{"Originator": {"inbound-client"}}
		enforceCodexIdentityHeadersWithAccount(headers, account)
		require.Equal(t, "codex-tui", headers.Get("Originator"))
		require.Equal(t, "codex-tui/0.149.0 (Linux; arm64)", headers.Get("User-Agent"))
		require.Equal(t, "0.149.0", headers.Get("Version"))
	})
}
