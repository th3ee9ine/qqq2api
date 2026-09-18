package service

import (
	"context"
	"fmt"
)

// BuildOpenAITestDefaultsForAccount resolves a credential shadow using the same
// repository path as a live test, without fetching a token or calling upstream.
// Payload mapping and proxy routing remain attached to the selected account.
func (s *AccountTestService) BuildOpenAITestDefaultsForAccount(ctx context.Context, account *Account, endpoint, prompt string) (OpenAITestDefaults, error) {
	if account == nil || !account.IsCredentialShadow() {
		return s.BuildOpenAITestDefaults(account, endpoint, prompt), nil
	}
	if s == nil || s.accountRepo == nil {
		return OpenAITestDefaults{}, fmt.Errorf("account repository unavailable for shadow defaults")
	}
	credentialAccount, err := resolveCredentialAccount(ctx, s.accountRepo, account)
	if err != nil {
		return OpenAITestDefaults{}, err
	}
	result := s.BuildOpenAITestDefaults(account, endpoint, prompt)
	credentialDefaults := s.BuildOpenAITestDefaults(credentialAccount, endpoint, prompt)
	result.AccountType = credentialDefaults.AccountType
	result.URL = credentialDefaults.URL
	upstreamBody := result.Body
	if result.UpstreamBody != nil {
		upstreamBody = result.UpstreamBody
	}
	// Authentication comes from the parent; routing must still reflect the
	// selected child's mapped upstream payload, not the parent's default model.
	result.Headers = buildOpenAIAccountTestHeaderDefaults(credentialAccount, result.Endpoint, result.URL, upstreamBody)
	result.HeaderDetails = buildOpenAITestHeaderDetails(result.Headers)
	result.Notes = append(credentialDefaults.Notes, "影子账号的上游请求头来自母账号；模型映射与代理配置保留所选账号设置。")
	return result, nil
}
