package service

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

// Legacy account-extra settings for the periodic ChatGPT session cleanup job.
// The active scheduler now reads the installation-wide setting; these keys and
// resolvers remain for backwards-compatible account API consumers and upgrades.
const (
	OpenAISessionCleanupEnabledExtraKey         = "openai_session_cleanup_enabled"
	OpenAISessionCleanupIntervalMinutesExtraKey = "openai_session_cleanup_interval_minutes"
	OpenAISessionCleanupStateExtraKey           = "openai_session_cleanup_state"
	OpenAISessionCleanupIntervalExtraKey        = OpenAISessionCleanupIntervalMinutesExtraKey
	// Short aliases mirror the naming used by other account-extra helpers.
	OpenAISessionCleanupEnabledKey         = OpenAISessionCleanupEnabledExtraKey
	OpenAISessionCleanupIntervalMinutesKey = OpenAISessionCleanupIntervalMinutesExtraKey
	OpenAISessionCleanupIntervalKey        = OpenAISessionCleanupIntervalExtraKey
	OpenAISessionCleanupStateKey           = OpenAISessionCleanupStateExtraKey

	// Legacy aliases accepted during migration.  New writes should use the
	// canonical openai_session_cleanup_* keys above.
	OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey         = "auto_revoke_non_current_sessions_enabled"
	OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey = "auto_revoke_non_current_sessions_interval_minutes"
	OpenAIAutoRevokeNonCurrentSessionsIntervalExtraKey        = OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey
	// A short-lived migration alias used by an early build wrote the state under
	// the policy-prefixed key. Keep reading/stripping it so upgrades do not leave
	// stale runtime status behind or lose it during an account edit.
	OpenAIAutoRevokeNonCurrentSessionsLegacyStateExtraKey = "auto_revoke_non_current_sessions_state"
	// Keep the original state constant on the literal legacy key.  New worker
	// writes always converge on OpenAISessionCleanupStateExtraKey, while callers
	// compiled against the earlier auto-revoke naming still address the value
	// they originally persisted.
	OpenAIAutoRevokeNonCurrentSessionsStateExtraKey = OpenAIAutoRevokeNonCurrentSessionsLegacyStateExtraKey
	// Short policy-oriented spellings are kept alongside the *ExtraKey names
	// because account-extra callers in this package historically used both
	// forms.  They intentionally point at the same literals, so aliases never
	// create a second source of truth.
	OpenAIAutoRevokeNonCurrentSessionsEnabledKey         = OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey
	OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesKey = OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey
	OpenAIAutoRevokeNonCurrentSessionsIntervalKey        = OpenAIAutoRevokeNonCurrentSessionsIntervalExtraKey
	OpenAIAutoRevokeNonCurrentSessionsStateKey           = OpenAIAutoRevokeNonCurrentSessionsStateExtraKey

	OpenAIAutoRevokeNonCurrentSessionsDefaultIntervalMinutes = 60
	OpenAIAutoRevokeNonCurrentSessionsMinIntervalMinutes     = 5
	OpenAIAutoRevokeNonCurrentSessionsMaxIntervalMinutes     = 10080 // 7 days
	OpenAISessionCleanupDefaultIntervalMinutes               = OpenAIAutoRevokeNonCurrentSessionsDefaultIntervalMinutes
	OpenAISessionCleanupMinIntervalMinutes                   = OpenAIAutoRevokeNonCurrentSessionsMinIntervalMinutes
	OpenAISessionCleanupMaxIntervalMinutes                   = OpenAIAutoRevokeNonCurrentSessionsMaxIntervalMinutes
	// Legacy constant spellings retained for source compatibility with the
	// original auto-revoke worker proposal.
	OpenAIAutoRevokeNonCurrentSessionsDefaultInterval = OpenAIAutoRevokeNonCurrentSessionsDefaultIntervalMinutes
	OpenAIAutoRevokeNonCurrentSessionsMinInterval     = OpenAIAutoRevokeNonCurrentSessionsMinIntervalMinutes
	OpenAIAutoRevokeNonCurrentSessionsMaxInterval     = OpenAIAutoRevokeNonCurrentSessionsMaxIntervalMinutes
)

// OpenAINonCurrentSessionRevokeConfig is the resolved account-level policy.
// IntervalMinutes is always populated with a valid value, even when Enabled is
// false, which keeps callers from having to special-case a zero duration.
type OpenAINonCurrentSessionRevokeConfig struct {
	Enabled         bool
	IntervalMinutes int
}

// OpenAISessionCleanupConfig is the UI-oriented spelling of the policy type.
// Keep it as an alias so values can be passed between either API without
// conversions or duplicated fields.
type OpenAISessionCleanupConfig = OpenAINonCurrentSessionRevokeConfig

// OpenAIAutoRevokeNonCurrentSessionsConfig is a descriptive alias retained for
// callers that prefer the setting's full name.
type OpenAIAutoRevokeNonCurrentSessionsConfig = OpenAINonCurrentSessionRevokeConfig

// ResolveOpenAINonCurrentSessionRevokeConfig returns the effective policy for
// an account.  Only OpenAI OAuth parent accounts are eligible; missing or
// malformed historical values fail closed (disabled) and use the default
// interval.
func ResolveOpenAINonCurrentSessionRevokeConfig(account *Account) OpenAINonCurrentSessionRevokeConfig {
	defaults := OpenAINonCurrentSessionRevokeConfig{
		IntervalMinutes: OpenAIAutoRevokeNonCurrentSessionsDefaultIntervalMinutes,
	}
	if !isOpenAINonCurrentSessionRevokeAccount(account) || account.Extra == nil {
		return defaults
	}
	result := defaults
	if rawEnabled, present := cleanupValuePresent(account.Extra, OpenAISessionCleanupEnabledExtraKey, OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey); present {
		enabled, ok := rawEnabled.(bool)
		if !ok {
			return defaults
		}
		result.Enabled = enabled
	}
	if rawInterval, present := cleanupValuePresent(account.Extra, OpenAISessionCleanupIntervalMinutesExtraKey, OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey); present {
		value, ok := parseOpenAINonCurrentSessionInterval(rawInterval)
		if !ok || !isValidOpenAINonCurrentSessionInterval(value) {
			return defaults
		}
		result.IntervalMinutes = value
	}
	return result
}

// ResolveOpenAIAutoRevokeNonCurrentSessionsConfig is an alias for the longer
// setting-oriented name.
func ResolveOpenAIAutoRevokeNonCurrentSessionsConfig(account *Account) OpenAIAutoRevokeNonCurrentSessionsConfig {
	return ResolveOpenAINonCurrentSessionRevokeConfig(account)
}

// ResolveOpenAISessionCleanupConfig is the canonical cleanup-oriented
// resolver.  The longer policy aliases above remain available to integrations
// that adopted the initial feature name.
func ResolveOpenAISessionCleanupConfig(account *Account) OpenAISessionCleanupConfig {
	return ResolveOpenAINonCurrentSessionRevokeConfig(account)
}

func isOpenAINonCurrentSessionRevokeAccount(account *Account) bool {
	return account != nil && account.Platform == PlatformOpenAI && account.Type == AccountTypeOAuth && !account.IsShadow()
}

// normalizeOpenAINonCurrentSessionRevokeExtra validates account-management
// input and fills the default interval when enabling the feature.  A nil map
// remains nil; callers can therefore distinguish an omitted `extra` object
// from an explicit empty object.  Runtime status is service-owned and is
// stripped from account-edit payloads so only the worker can write it.
func normalizeOpenAINonCurrentSessionRevokeExtra(platform, accountType string, isShadow bool, extra map[string]any) (map[string]any, error) {
	if extra == nil {
		return nil, nil
	}
	normalized := cloneOpenAINonCurrentSessionRevokeExtra(extra)
	// The state object is service-owned and can never be supplied by an account
	// edit request.  Keep configuration validation independent from runtime
	// observations so a stale state payload cannot be persisted accidentally.
	delete(normalized, OpenAISessionCleanupStateExtraKey)
	delete(normalized, OpenAIAutoRevokeNonCurrentSessionsLegacyStateExtraKey)
	_, hasEnabled := cleanupValuePresent(normalized, OpenAISessionCleanupEnabledExtraKey, OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey)
	_, hasInterval := cleanupValuePresent(normalized, OpenAISessionCleanupIntervalMinutesExtraKey, OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey)
	if !hasEnabled && !hasInterval {
		return normalized, nil
	}
	if platform != PlatformOpenAI || accountType != AccountTypeOAuth || isShadow {
		return nil, newOpenAINonCurrentSessionRevokeAccountInvalidError()
	}

	enabled := false
	if hasEnabled {
		rawEnabled := resolveCleanupValue(normalized, OpenAISessionCleanupEnabledExtraKey, OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey)
		value, ok := rawEnabled.(bool)
		if !ok {
			return nil, badRequestOpenAINonCurrentSessionRevoke(
				"OPENAI_AUTO_REVOKE_NON_CURRENT_SESSIONS_ENABLED_INVALID",
				"openai_session_cleanup_enabled must be a boolean",
			)
		}
		enabled = value
	}
	if hasInterval {
		rawInterval := resolveCleanupValue(normalized, OpenAISessionCleanupIntervalMinutesExtraKey, OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey)
		value, ok := parseOpenAINonCurrentSessionInterval(rawInterval)
		if !ok || !isValidOpenAINonCurrentSessionInterval(value) {
			return nil, badRequestOpenAINonCurrentSessionRevoke(
				"OPENAI_AUTO_REVOKE_NON_CURRENT_SESSIONS_INTERVAL_INVALID",
				"openai_session_cleanup_interval_minutes must be an integer between 5 and 10080",
			)
		}
		normalized[OpenAISessionCleanupIntervalMinutesExtraKey] = value
	} else if enabled {
		normalized[OpenAISessionCleanupIntervalMinutesExtraKey] = OpenAIAutoRevokeNonCurrentSessionsDefaultIntervalMinutes
	}
	// Canonicalize legacy aliases after validation.  This makes subsequent
	// reads deterministic and lets operators migrate simply by saving an old
	// account from the current UI.
	if raw, ok := normalized[OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey]; ok {
		if _, canonical := normalized[OpenAISessionCleanupEnabledExtraKey]; !canonical {
			normalized[OpenAISessionCleanupEnabledExtraKey] = raw
		}
		delete(normalized, OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey)
	}
	if raw, ok := normalized[OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey]; ok {
		if _, canonical := normalized[OpenAISessionCleanupIntervalMinutesExtraKey]; !canonical {
			normalized[OpenAISessionCleanupIntervalMinutesExtraKey] = raw
		}
		delete(normalized, OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey)
	}
	// An omitted enabled flag means disabled for backwards compatibility.  Do
	// not add an explicit false key, keeping old account JSON compact.
	return normalized, nil
}

// normalizeOpenAINonCurrentSessionRevokePatch applies the same validation to a
// key-level Extra update while retaining an already configured interval when a
// caller only toggles the enabled flag.  The dedicated cleanup endpoint uses a
// partial PATCH-like payload (`{enabled: ...}`), so blindly letting the generic
// normalizer inject the 60-minute default would unexpectedly overwrite a
// user's existing interval.  Invalid historical intervals are ignored and
// normalized to the default by the regular validator.
func normalizeOpenAINonCurrentSessionRevokePatch(account *Account, extra map[string]any) (map[string]any, error) {
	if account == nil {
		return normalizeOpenAINonCurrentSessionRevokeExtra("", "", false, extra)
	}
	if extra == nil {
		return nil, nil
	}

	_, hasEnabled := cleanupValuePresent(extra, OpenAISessionCleanupEnabledExtraKey, OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey)
	_, hasInterval := cleanupValuePresent(extra, OpenAISessionCleanupIntervalMinutesExtraKey, OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey)
	if hasEnabled && !hasInterval && isOpenAINonCurrentSessionRevokeAccount(account) {
		enabled, enabledOK := resolveCleanupValue(extra, OpenAISessionCleanupEnabledExtraKey, OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey).(bool)
		if enabledOK && enabled {
			if rawInterval, present := cleanupValuePresent(account.Extra, OpenAISessionCleanupIntervalMinutesExtraKey, OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey); present {
				if interval, valid := parseOpenAINonCurrentSessionInterval(rawInterval); valid && isValidOpenAINonCurrentSessionInterval(interval) {
					patched := cloneOpenAINonCurrentSessionRevokeExtra(extra)
					patched[OpenAISessionCleanupIntervalMinutesExtraKey] = interval
					extra = patched
				}
			}
		}
	}
	return normalizeOpenAINonCurrentSessionRevokeExtra(account.Platform, account.Type, account.IsShadow(), extra)
}

// mergeOpenAINonCurrentSessionRevokeExistingConfig preserves an account's
// already configured policy when a full account edit omits the two policy
// fields.  UpdateAccount accepts a JSON object that is also used for unrelated
// settings; replacing that object wholesale should not silently disable a
// periodic security job merely because an older client did not know about the
// new keys.  Explicit values in extra always win, including an explicit
// `enabled: false` or an invalid value (which is subsequently rejected by the
// normalizer).
func mergeOpenAINonCurrentSessionRevokeExistingConfig(account *Account, effectiveType string, extra map[string]any) map[string]any {
	if extra == nil || account == nil || account.Platform != PlatformOpenAI || effectiveType != AccountTypeOAuth || account.IsShadow() {
		return extra
	}
	merged := cloneOpenAINonCurrentSessionRevokeExtra(extra)
	if _, present := cleanupValuePresent(merged, OpenAISessionCleanupEnabledExtraKey, OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey); !present {
		if raw, exists := cleanupValuePresent(account.Extra, OpenAISessionCleanupEnabledExtraKey, OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey); exists {
			if enabled, ok := raw.(bool); ok {
				merged[OpenAISessionCleanupEnabledExtraKey] = enabled
			}
		}
	}
	if _, present := cleanupValuePresent(merged, OpenAISessionCleanupIntervalMinutesExtraKey, OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey); !present {
		if raw, exists := cleanupValuePresent(account.Extra, OpenAISessionCleanupIntervalMinutesExtraKey, OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey); exists {
			if interval, ok := parseOpenAINonCurrentSessionInterval(raw); ok && isValidOpenAINonCurrentSessionInterval(interval) {
				merged[OpenAISessionCleanupIntervalMinutesExtraKey] = interval
			}
		}
	}
	return merged
}

// NormalizeOpenAINonCurrentSessionRevokeExtra exposes the validator to import
// and integration callers while preserving the lower-case helper used by the
// account service implementation.
func NormalizeOpenAINonCurrentSessionRevokeExtra(platform, accountType string, isShadow bool, extra map[string]any) (map[string]any, error) {
	return normalizeOpenAINonCurrentSessionRevokeExtra(platform, accountType, isShadow, extra)
}

// NormalizeOpenAISessionCleanupExtra is an alias for callers that expose the
// canonical cleanup terminology in their import/sync pipelines.
func NormalizeOpenAISessionCleanupExtra(platform, accountType string, isShadow bool, extra map[string]any) (map[string]any, error) {
	return normalizeOpenAINonCurrentSessionRevokeExtra(platform, accountType, isShadow, extra)
}

// NormalizeOpenAIAutoRevokeNonCurrentSessionsExtra is the legacy exported
// spelling retained for callers that use the original feature name.
func NormalizeOpenAIAutoRevokeNonCurrentSessionsExtra(platform, accountType string, isShadow bool, extra map[string]any) (map[string]any, error) {
	return normalizeOpenAINonCurrentSessionRevokeExtra(platform, accountType, isShadow, extra)
}

// normalizeOpenAIAutoRevokeNonCurrentSessionsExtra is the package-local
// counterpart used by older service tests and integrations.
func normalizeOpenAIAutoRevokeNonCurrentSessionsExtra(platform, accountType string, isShadow bool, extra map[string]any) (map[string]any, error) {
	return normalizeOpenAINonCurrentSessionRevokeExtra(platform, accountType, isShadow, extra)
}

func normalizeOpenAISessionCleanupExtra(platform, accountType string, isShadow bool, extra map[string]any) (map[string]any, error) {
	return normalizeOpenAINonCurrentSessionRevokeExtra(platform, accountType, isShadow, extra)
}

// stripOpenAINonCurrentSessionRevokeManagedExtra removes settings from generic
// runtime-extra writes.  Account edit/bulk paths validate these keys through
// normalizeOpenAINonCurrentSessionRevokeExtra; UpdateExtra is reserved for
// service-owned state and must not let a caller bypass those guards.
func stripOpenAINonCurrentSessionRevokeManagedExtra(extra map[string]any, stripConfig bool) map[string]any {
	if extra == nil {
		return nil
	}
	delete(extra, OpenAISessionCleanupStateExtraKey)
	delete(extra, OpenAIAutoRevokeNonCurrentSessionsLegacyStateExtraKey)
	if stripConfig {
		delete(extra, OpenAISessionCleanupEnabledExtraKey)
		delete(extra, OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey)
		delete(extra, OpenAISessionCleanupIntervalMinutesExtraKey)
		delete(extra, OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey)
	}
	return extra
}

func cloneOpenAINonCurrentSessionRevokeExtra(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func parseOpenAINonCurrentSessionInterval(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		if int64(int(typed)) != typed {
			return 0, false
		}
		return int(typed), true
	case uint:
		if uint(int(typed)) != typed {
			return 0, false
		}
		return int(typed), true
	case uint8:
		return int(typed), true
	case uint16:
		return int(typed), true
	case uint32:
		if uint32(int(typed)) != typed {
			return 0, false
		}
		return int(typed), true
	case uint64:
		if uint64(int(typed)) != typed {
			return 0, false
		}
		return int(typed), true
	case uintptr:
		if uintptr(int(typed)) != typed {
			return 0, false
		}
		return int(typed), true
	case float32:
		value := float64(typed)
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value {
			return 0, false
		}
		return int(value), true
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || math.Trunc(typed) != typed {
			return 0, false
		}
		return int(typed), true
	case json.Number:
		parsed, err := strconv.ParseFloat(string(typed), 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || math.Trunc(parsed) != parsed {
			return 0, false
		}
		return int(parsed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		return 0, false
	}
}

func isValidOpenAINonCurrentSessionInterval(value int) bool {
	return value >= OpenAIAutoRevokeNonCurrentSessionsMinIntervalMinutes && value <= OpenAIAutoRevokeNonCurrentSessionsMaxIntervalMinutes
}

func newOpenAINonCurrentSessionRevokeAccountInvalidError() error {
	return badRequestOpenAINonCurrentSessionRevoke(
		"OPENAI_AUTO_REVOKE_NON_CURRENT_SESSIONS_ACCOUNT_INVALID",
		"automatic non-current session revocation is only supported for OpenAI OAuth parent accounts",
	)
}

func badRequestOpenAINonCurrentSessionRevoke(code, message string) error {
	return infraerrors.BadRequest(code, message)
}

func resolveCleanupValue(extra map[string]any, canonical, legacy string) any {
	if value, ok := extra[canonical]; ok {
		return value
	}
	return extra[legacy]
}

func cleanupValuePresent(extra map[string]any, canonical, legacy string) (any, bool) {
	if value, ok := extra[canonical]; ok {
		return value, true
	}
	value, ok := extra[legacy]
	return value, ok
}

func resolveCleanupBool(extra map[string]any) (bool, bool) {
	value, ok := cleanupValuePresent(extra, OpenAISessionCleanupEnabledExtraKey, OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey)
	if !ok {
		return false, false
	}
	parsed, ok := value.(bool)
	return parsed, ok
}
