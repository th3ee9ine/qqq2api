package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type debugWorkbenchVerificationScope struct {
	apiKey       *APIKey
	group        *Group
	subscription *UserSubscription
}

func debugWorkbenchAccountInGroup(account *Account, groupID int64) bool {
	if account == nil {
		return false
	}
	for _, id := range account.GroupIDs {
		if id == groupID {
			return true
		}
	}
	for _, binding := range account.AccountGroups {
		if binding.GroupID == groupID {
			return true
		}
	}
	for _, group := range account.Groups {
		if group != nil && group.ID == groupID {
			return true
		}
	}
	return false
}

func (s *DebugWorkbenchService) resolveDebugWorkbenchVerificationScope(ctx context.Context, account *Account, apiKeyID int64) (*debugWorkbenchVerificationScope, error) {
	if s == nil || s.apiKeys == nil || s.gateway == nil {
		return nil, debugInputError(http.StatusServiceUnavailable, "state verification requires the API key and gateway services")
	}
	source, ok := s.accounts.(debugWorkbenchVerificationSource)
	if !ok {
		return nil, debugInputError(http.StatusServiceUnavailable, "state verification scope lookup is not configured")
	}
	if account == nil || account.ID <= 0 || !account.IsOpenAI() || !account.IsActive() || account.ParentAccountID != nil {
		return nil, debugInputError(http.StatusConflict, "state verification requires the selected active OpenAI account")
	}
	if apiKeyID <= 0 {
		return nil, debugInputError(http.StatusConflict, "state verification requires an explicit dedicated API key ID")
	}
	apiKey, err := s.apiKeys.GetByID(ctx, apiKeyID)
	if err != nil {
		return nil, fmt.Errorf("load dedicated verification API key: %w", err)
	}
	if apiKey == nil || apiKey.ID != apiKeyID || apiKey.User == nil || apiKey.GroupID == nil || *apiKey.GroupID <= 0 ||
		apiKey.UserID <= 0 || apiKey.UserID != apiKey.User.ID || !apiKey.IsActive() || !apiKey.User.IsActive() ||
		apiKey.User.DeletedAt != nil || apiKey.IsExpired() || apiKey.IsQuotaExhausted() || apiKey.Concurrency != 1 {
		return nil, debugInputError(http.StatusConflict, "the dedicated API key must be active, unexpired, quota-eligible and concurrency-1")
	}
	group, err := source.GetGroup(ctx, *apiKey.GroupID)
	if err != nil {
		return nil, fmt.Errorf("load verification group: %w", err)
	}
	if group == nil || group.ID != *apiKey.GroupID || !group.Hydrated || !group.IsActive() || group.Platform != PlatformOpenAI ||
		!group.IsExclusive || !debugWorkbenchAccountInGroup(account, group.ID) || apiKey.Group == nil ||
		apiKey.Group.ID != group.ID || !apiKey.Group.Hydrated {
		return nil, debugInputError(http.StatusConflict, "the dedicated API key must belong to the selected account's active exclusive OpenAI group")
	}
	accounts, totalAccounts, err := source.ListAccounts(ctx, 1, 2, "", "", "", "", group.ID, "", "id", "asc")
	if err != nil {
		return nil, fmt.Errorf("load verification group accounts: %w", err)
	}
	if totalAccounts != 1 || len(accounts) != 1 || accounts[0].ID != account.ID {
		return nil, debugInputError(http.StatusConflict, "the dedicated verification group must contain exactly the selected account")
	}
	keys, totalKeys, err := source.GetGroupAPIKeys(ctx, group.ID, 1, 2)
	if err != nil {
		return nil, fmt.Errorf("load verification group API keys: %w", err)
	}
	if totalKeys != 1 || len(keys) != 1 || keys[0].ID != apiKeyID {
		return nil, debugInputError(http.StatusConflict, "the dedicated verification group must contain exactly the selected API key")
	}
	apiKey.Group = group
	scope := &debugWorkbenchVerificationScope{apiKey: apiKey, group: group}
	if group.IsSubscriptionType() {
		if s.gateway.userSubRepo == nil {
			return nil, debugInputError(http.StatusServiceUnavailable, "state verification subscription lookup is not configured")
		}
		subscription, err := s.gateway.userSubRepo.GetActiveByUserIDAndGroupID(ctx, apiKey.UserID, group.ID)
		if err != nil {
			return nil, fmt.Errorf("load dedicated verification subscription: %w", err)
		}
		if subscription == nil || subscription.UserID != apiKey.UserID || subscription.GroupID != group.ID ||
			!subscription.IsActive() || subscription.StartsAt.After(time.Now()) || subscription.DeletedAt != nil {
			return nil, debugInputError(http.StatusConflict, "the dedicated verification key requires an active matching subscription")
		}
		scope.subscription = subscription
	}
	return scope, nil
}

func (s *DebugWorkbenchService) recordDebugWorkbenchVerificationUsage(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	scope *debugWorkbenchVerificationScope,
	body []byte,
	result *OpenAIForwardResult,
	pricingAt time.Time,
) error {
	if s == nil || s.gateway == nil || s.apiKeys == nil || c == nil || account == nil || scope == nil ||
		scope.apiKey == nil || scope.apiKey.User == nil || result == nil {
		return errors.New("debug verification usage scope is incomplete")
	}
	requestedModel := debugWorkbenchRequestModel(body)
	if requestedModel == "" {
		return errors.New("debug verification requested model is missing")
	}
	upstreamEndpoint := strings.TrimSpace(result.UpstreamEndpoint)
	if upstreamEndpoint == "" {
		upstreamEndpoint = GetActualOpenAIUpstreamEndpoint(c)
	}
	if upstreamEndpoint == "" {
		upstreamEndpoint = codexTurnStateUsageVerificationEndpoint
	}
	usageInput := &OpenAIRecordUsageInput{
		Result:                 result,
		APIKey:                 scope.apiKey,
		User:                   scope.apiKey.User,
		Account:                account,
		Subscription:           scope.subscription,
		InboundEndpoint:        codexTurnStateUsageVerificationEndpoint,
		UpstreamEndpoint:       upstreamEndpoint,
		UserAgent:              c.GetHeader("User-Agent"),
		SessionID:              ExtractClientSessionID(c),
		RequestPayloadHash:     HashUsageRequestPayload(body),
		APIKeyService:          s.apiKeys,
		QuotaPlatform:          PlatformOpenAI,
		PricingAt:              pricingAt,
		RequireDurableUsageLog: true,
		ChannelUsageFields: ChannelUsageFields{
			OriginalModel:      requestedModel,
			ChannelMappedModel: requestedModel,
			BillingModelSource: BillingModelSourceRequested,
		},
	}
	if err := s.gateway.RecordUsage(ctx, usageInput); err != nil {
		return err
	}
	if !usageInput.usageLogInserted {
		return errors.New("debug verification usage log was not newly inserted")
	}
	return nil
}
