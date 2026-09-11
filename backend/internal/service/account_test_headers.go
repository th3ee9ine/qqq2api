package service

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const (
	openAITestRedactedHeader = "••••••••"
	openAITestDynamicHeader  = "<generated-per-request>"
)

// applyOpenAIAccountTestHeaders is shared by the live connectivity probes and
// their workbench templates. Authentication is supplied separately: generating
// Agent Identity assertions may register a task and must never run in a preview.
// endpoint is the upstream protocol, not necessarily the caller's API endpoint.
func applyOpenAIAccountTestHeaders(req *http.Request, account *Account, endpoint string, body []byte) {
	if req == nil {
		return
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Content-Type", "application/json")
	// This is a per-request diagnostic ID, not a conversation or device ID.
	req.Header.Set("X-Client-Request-Id", uuid.NewString())
	isOAuth := account != nil && account.IsOAuth()
	if isOAuth {
		req.Host = "chatgpt.com"
		req.Header.Set("Accept", "text/event-stream")
		canonical := resolveCodexOutboundIdentity("")
		req.Header.Set("Originator", canonical.originator)
		if customUA := strings.TrimSpace(account.GetOpenAICodexUserAgent()); customUA != "" {
			req.Header.Set("User-Agent", customUA)
		} else {
			req.Header.Set("User-Agent", canonical.userAgent)
		}
		setOpenAIChatGPTAccountHeaders(req.Header, account)
		enforceCodexIdentityHeadersWithAccount(req.Header, account)
	} else {
		switch endpoint {
		case "responses":
			applyOpenAICodexProbeHeaders(req.Header)
			req.Header.Set("Accept", "text/event-stream")
		case "chat/completions":
			req.Header.Set("Accept", "text/event-stream")
		}
	}
	account.ApplyHeaderOverrides(req.Header)
	// Use the gateway's own capability and final-model routing rules. Keep
	// explicit feature overrides, and never trust a static routing override.
	applyOpenAICodexBetaFeatures(nil, account, req.Header)
	setOpenAICodexRoutingHintFromBody(req.Header, account, body)
	if isOAuth {
		stripOpenAILegacyResponsesBeta(req.Header)
	}
	SanitizeOutboundGatewayIdentity(req.Header)
}

// buildOpenAIAccountTestHeaderDefaults snapshots only headers this probe sets.
// Host is carried by http.Request.Host rather than Header but is part of the
// request on the wire. Other transport-derived fields are explained in Notes.
func buildOpenAIAccountTestHeaderDefaults(account *Account, endpoint, upstreamURL string, body map[string]any) map[string]string {
	req, err := http.NewRequest(http.MethodPost, upstreamURL, nil)
	if err != nil {
		return map[string]string{}
	}
	if account != nil && account.IsOpenAIAgentIdentity() {
		req.Header.Set("Authorization", "AgentAssertion "+openAITestDynamicHeader)
	} else {
		req.Header.Set("Authorization", "Bearer "+openAITestRedactedHeader)
	}
	bodyBytes, _ := json.Marshal(body)
	applyOpenAIAccountTestHeaders(req, account, endpoint, bodyBytes)

	result := make(map[string]string, len(req.Header)+1)
	overrides := account.GetHeaderOverrides()
	for name, values := range req.Header {
		value := strings.Join(values, ", ")
		// All custom values are credential material from the account record.
		// Unknown/private header names may carry secrets too, so a denylist of
		// token-like names alone is not sufficient for this read-only endpoint.
		if _, overridden := overrides[strings.ToLower(name)]; overridden && !strings.EqualFold(name, openAICodexRoutingHintHeader) {
			value = openAITestRedactedHeader
		} else if strings.EqualFold(name, "Chatgpt-Account-Id") {
			value = openAITestRedactedHeader
		} else if strings.EqualFold(name, "X-Codex-Window-Id") || strings.EqualFold(name, "X-Client-Request-Id") {
			value = openAITestDynamicHeader
		}
		result[http.CanonicalHeaderKey(name)] = value
	}
	result["Host"] = req.Host
	return result
}
