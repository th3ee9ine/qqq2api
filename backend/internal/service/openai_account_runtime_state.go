package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

const openAIAccountRuntimeStateShadowLookupTimeout = 3 * time.Second

type openAIProxyRuntimeIdentity struct {
	protocol       string
	host           string
	port           int
	username       string
	password       string
	status         string
	hasExpiresAt   bool
	expiresAt      time.Time
	fallbackMode   string
	hasBackupProxy bool
	backupProxyID  int64
}

func snapshotOpenAIProxyRuntimeIdentity(proxy *Proxy) openAIProxyRuntimeIdentity {
	if proxy == nil {
		return openAIProxyRuntimeIdentity{}
	}
	identity := openAIProxyRuntimeIdentity{
		protocol:     proxy.Protocol,
		host:         proxy.Host,
		port:         proxy.Port,
		username:     proxy.Username,
		password:     proxy.Password,
		status:       proxy.Status,
		fallbackMode: proxy.FallbackMode,
	}
	if proxy.ExpiresAt != nil {
		identity.hasExpiresAt = true
		identity.expiresAt = *proxy.ExpiresAt
	}
	if proxy.BackupProxyID != nil {
		identity.hasBackupProxy = true
		identity.backupProxyID = *proxy.BackupProxyID
	}
	return identity
}

func (identity openAIProxyRuntimeIdentity) equal(other openAIProxyRuntimeIdentity) bool {
	return identity.protocol == other.protocol &&
		identity.host == other.host &&
		identity.port == other.port &&
		identity.username == other.username &&
		identity.password == other.password &&
		identity.status == other.status &&
		identity.hasExpiresAt == other.hasExpiresAt &&
		(!identity.hasExpiresAt || identity.expiresAt.Equal(other.expiresAt)) &&
		identity.fallbackMode == other.fallbackMode &&
		identity.hasBackupProxy == other.hasBackupProxy &&
		identity.backupProxyID == other.backupProxyID
}

// snapshotOpenAIProxyRuntimeAccounts deliberately drops request ownership
// scope. Proxy connection identity is process-global, so a change must rotate
// every affected account even when an administrator request is scoped.
func snapshotOpenAIProxyRuntimeAccounts(repo ProxyRepository, proxyID int64) ([]ProxyAccountSummary, error) {
	if repo == nil || proxyID <= 0 {
		return nil, nil
	}
	lookupCtx, cancel := context.WithTimeout(context.Background(), openAIAccountRuntimeStateShadowLookupTimeout)
	defer cancel()
	accounts, err := repo.ListAccountSummariesByProxyID(lookupCtx, proxyID)
	if err != nil {
		return nil, fmt.Errorf("list accounts for proxy runtime invalidation: %w", err)
	}
	return accounts, nil
}

func invalidateOpenAIProxyRuntimeAccounts(blocker AccountRuntimeBlocker, accounts []ProxyAccountSummary) {
	invalidator, ok := blocker.(OpenAIAccountRuntimeStateInvalidator)
	if !ok || invalidator == nil {
		return
	}
	seen := make(map[int64]struct{}, len(accounts))
	for _, account := range accounts {
		if account.ID <= 0 || account.Platform != PlatformOpenAI ||
			(account.Type != AccountTypeOAuth && account.Type != AccountTypeSetupToken) {
			continue
		}
		if _, exists := seen[account.ID]; exists {
			continue
		}
		seen[account.ID] = struct{}{}
		invalidator.InvalidateOpenAIAccountRuntimeState(account.ID)
	}
}

// OpenAIAccountRuntimeStateInvalidator is deliberately narrower than
// AccountRuntimeBlocker so existing schedulers and test doubles do not need to
// know about gateway-owned connection and turn-state caches.
type OpenAIAccountRuntimeStateInvalidator interface {
	InvalidateOpenAIAccountRuntimeState(accountID int64)
}

func invalidateOpenAIAccountRuntimeState(blocker AccountRuntimeBlocker, accountID int64) {
	if blocker == nil || accountID <= 0 {
		return
	}
	if invalidator, ok := blocker.(OpenAIAccountRuntimeStateInvalidator); ok {
		invalidator.InvalidateOpenAIAccountRuntimeState(accountID)
	}
}

// invalidateOpenAIAccountRuntimeStateWithShadows invalidates the credential
// owner and all shadows that delegate to it. A shadow is keyed independently in
// the collector even though its effective OAuth credentials come from the
// parent account.
func invalidateOpenAIAccountRuntimeStateWithShadows(ctx context.Context, blocker AccountRuntimeBlocker, repo AccountRepository, account *Account) {
	if account == nil || account.ID <= 0 || account.Platform != PlatformOpenAI {
		return
	}
	invalidator, ok := blocker.(OpenAIAccountRuntimeStateInvalidator)
	if !ok || invalidator == nil {
		return
	}
	invalidator.InvalidateOpenAIAccountRuntimeState(account.ID)
	if repo == nil || account.IsCredentialShadow() {
		return
	}

	lookupParent := context.Background()
	if ctx != nil {
		lookupParent = context.WithoutCancel(ctx)
	}
	lookupCtx, cancel := context.WithTimeout(lookupParent, openAIAccountRuntimeStateShadowLookupTimeout)
	defer cancel()
	shadows, err := repo.ListShadowsByParent(lookupCtx, account.ID)
	if err != nil {
		slog.Warn("openai.runtime_state_shadow_lookup_failed", "account_id", account.ID, "error", err)
		return
	}
	for _, shadow := range shadows {
		if shadow != nil {
			invalidator.InvalidateOpenAIAccountRuntimeState(shadow.ID)
		}
	}
}

// InvalidateOpenAIAccountRuntimeState synchronously crosses every in-process
// ownership boundary that can retain an account's upstream identity. The
// collector invalidation runs first so late responses and probes are rejected
// before connection and session bindings are removed.
func (s *OpenAIGatewayService) InvalidateOpenAIAccountRuntimeState(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	if s.codexTurnStateCollector != nil {
		s.codexTurnStateCollector.DeleteAccount(accountID)
	}
	s.openaiCodexTurnStateOrigins.Range(func(key, value any) bool {
		origin, ok := value.(openAICodexTurnStateOrigin)
		if !ok || origin.accountID == accountID {
			s.openaiCodexTurnStateOrigins.Delete(key)
		}
		return true
	})
	if stateStore := s.getOpenAIWSStateStore(); stateStore != nil {
		if cleaner, ok := stateStore.(interface{ DeleteAccountTurnStates(int64) }); ok {
			cleaner.DeleteAccountTurnStates(accountID)
		}
	}
	if pool := s.getOpenAIWSConnPool(); pool != nil {
		pool.ClearAccount(accountID)
	}
}

// codexTurnStateRequestGenerationCurrent rejects state published by a request
// that began before account runtime invalidation. Requests without a collector
// binding retain the legacy provenance behavior.
func (s *OpenAIGatewayService) codexTurnStateRequestGenerationCurrent(c *gin.Context, accountID int64) (uint64, bool) {
	if s == nil || s.codexTurnStateCollector == nil || accountID <= 0 {
		return 0, true
	}
	if c != nil {
		if raw, ok := c.Get(openAICodexTurnStateContextKey); ok {
			if binding, ok := raw.(openAICodexTurnStateRequestBinding); ok && binding.key.AccountID == accountID {
				return binding.key.generation, s.codexTurnStateCollector.IsCurrentKey(binding.key)
			}
		}
	}
	key := s.codexTurnStateCollector.BindKey(OpenAICodexTurnStateKey{AccountID: accountID})
	return key.generation, true
}

type openAIAccountRuntimeIdentity struct {
	platform        string
	accountType     string
	hasProxy        bool
	proxyID         int64
	credentialsHash [sha256.Size]byte
	extraHash       [sha256.Size]byte
}

func snapshotOpenAIAccountRuntimeIdentity(account *Account) openAIAccountRuntimeIdentity {
	if account == nil {
		return openAIAccountRuntimeIdentity{}
	}
	identity := openAIAccountRuntimeIdentity{
		platform:        account.Platform,
		accountType:     account.Type,
		credentialsHash: hashOpenAIAccountRuntimeDocument(account.Credentials),
		extraHash:       hashOpenAIAccountRuntimeDocument(account.Extra),
	}
	if account.ProxyID != nil {
		identity.hasProxy = true
		identity.proxyID = *account.ProxyID
	}
	return identity
}

func hashOpenAIAccountRuntimeDocument(value map[string]any) [sha256.Size]byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		// Persisted account documents are JSON-compatible. Treat an unexpected
		// value as changed rather than retaining state under an uncertain identity.
		return sha256.Sum256([]byte("invalid-runtime-identity-document"))
	}
	return sha256.Sum256(encoded)
}

func updatesOpenAIAccountRuntimeIdentityExtra(updates map[string]any) bool {
	for _, key := range []string{
		codexFingerprintModeExtraKey,
		codexFingerprintSeedExtraKey,
		"openai_device_id",
		"openai_session_id",
		OpenAILocalDeviceUserAgentExtraKey,
		OpenAILocalDeviceOriginatorExtraKey,
		OpenAILocalDeviceVersionExtraKey,
		"openai_device_user_agent",
		"openai_session_user_agent",
		"local_device_user_agent",
		"openai_local_device_ua",
		"openai_device_ua",
		"openai_device_originator",
		"openai_session_originator",
		"local_device_originator",
		"openai_originator",
		"originator",
		"openai_device_version",
		"openai_session_version",
		"local_device_version",
		"openai_local_device_client_version",
		"openai_device_client_version",
		"openai_session_client_version",
		"local_device_client_version",
		"openai_local_device_session",
		"openai_device_session",
		"local_device_session",
		"openai_current_device_session",
		"openai_local_device",
		"openai_device",
		"current_device_session",
	} {
		if _, changed := updates[key]; changed {
			return true
		}
	}
	return false
}
