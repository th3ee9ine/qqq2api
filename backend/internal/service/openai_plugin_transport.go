package service

import (
	"errors"
	"net/http"

	"github.com/th3ee9ine/qqq2api/internal/pkg/tlsfingerprint"
)

// doOpenAIOAuthTransport is the shared OpenAI OAuth egress path used by live
// account tests and Codex maintenance probes. The plugin gets the first chance
// to handle the request. When it does not handle it, callers can opt into the
// TLS-profile transport used by Sub2's account-test path. The bool result is
// true only when the plugin returned a handled response (or error).
func doOpenAIOAuthTransport(
	request *http.Request,
	proxyURL string,
	account *Account,
	pluginManager *PluginManager,
	httpUpstream HTTPUpstream,
	tlsFPProfileService *TLSFingerprintProfileService,
	useTLSFallback bool,
) (*http.Response, bool, error) {
	return doOpenAIOAuthTransportWithCredentialAccount(
		request,
		proxyURL,
		account,
		account,
		pluginManager,
		httpUpstream,
		tlsFPProfileService,
		useTLSFallback,
	)
}

// doOpenAIOAuthTransportWithCredentialAccount keeps the account that owns the
// scheduled route separate from the account that owns OAuth credentials. This
// matters for credential shadows: the plugin account directory intentionally
// exposes only the parent account, while the fallback transport still needs the
// shadow id/concurrency for connection-pool and observability semantics.
func doOpenAIOAuthTransportWithCredentialAccount(
	request *http.Request,
	proxyURL string,
	transportAccount *Account,
	credentialAccount *Account,
	pluginManager *PluginManager,
	httpUpstream HTTPUpstream,
	tlsFPProfileService *TLSFingerprintProfileService,
	useTLSFallback bool,
) (*http.Response, bool, error) {
	if request == nil {
		return nil, false, errors.New("openai upstream request is nil")
	}
	if transportAccount == nil {
		return nil, false, errors.New("openai upstream account is nil")
	}
	if credentialAccount == nil {
		credentialAccount = transportAccount
	}
	SanitizeOutboundGatewayIdentity(request.Header)
	if pluginManager != nil {
		response, handled, err := pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, credentialAccount)
		if handled {
			return response, true, err
		}
	}
	if httpUpstream == nil {
		return nil, false, errors.New("openai upstream transport is unavailable")
	}
	if useTLSFallback {
		var profile *tlsfingerprint.Profile
		if tlsFPProfileService != nil {
			profile = tlsFPProfileService.ResolveTLSProfile(transportAccount)
		}
		response, err := httpUpstream.DoWithTLS(request, proxyURL, transportAccount.ID, transportAccount.Concurrency, profile)
		return response, false, err
	}
	response, err := httpUpstream.Do(request, proxyURL, transportAccount.ID, transportAccount.Concurrency)
	return response, false, err
}

func (s *OpenAIGatewayService) SetPluginManager(manager *PluginManager) {
	s.pluginManager = manager
}

// SetTLSFingerprintProfileService attaches the account-aware TLS profile
// resolver without changing the long-standing gateway constructor signature.
func (s *OpenAIGatewayService) SetTLSFingerprintProfileService(service *TLSFingerprintProfileService) {
	if s != nil {
		s.tlsFPProfileService = service
	}
}

// doOpenAIUpstream 只在 OpenAI OAuth 能力绑定已启用时把真实请求交给插件。
// 插件返回标准 http.Response，响应解析、错误映射、SSE 和计费仍由现有核心链处理。
func (s *OpenAIGatewayService) doOpenAIUpstream(request *http.Request, proxyURL string, account *Account) (response *http.Response, err error) {
	request, err = s.refreshCodexTurnStateRequest(request, account)
	if err != nil {
		return nil, err
	}
	request = s.stampCodexTurnStateRequest(request, account)
	defer func() {
		if request == nil || response == nil {
			return
		}
		response.Request = request
	}()
	if request != nil {
		request = snapshotOpenAIUpstreamTurnState(request)
		normalizeLegacyOpenAIOutboundRequestBody(request)
		SanitizeOutboundGatewayIdentity(request.Header)
	}
	var capture *DebugWorkbenchAttemptCapture
	if request != nil && account != nil {
		capture = DebugWorkbenchTraceFromContext(request.Context()).StartAttempt(request, proxyURL, account.ID)
	}
	defer func() { capture.Finish(response, err) }()
	response, handled, err := doOpenAIOAuthTransport(
		request,
		proxyURL,
		account,
		s.pluginManager,
		s.httpUpstream,
		s.tlsFPProfileService,
		false,
	)
	if handled {
		capture.MarkPlugin()
	}
	return response, err
}

// doOpenAIAccountTestUpstream 让 OpenAI OAuth 账号测试与真实转发使用同一插件路径。
// API Key 和未命中插件的账号保持各自原有的 HTTPUpstream 行为。
func (s *AccountTestService) doOpenAIAccountTestUpstream(
	request *http.Request,
	proxyURL string,
	account *Account,
	useTLSFallback bool,
) (*http.Response, error) {
	return s.doOpenAIAccountTestUpstreamWithCredentialAccount(request, proxyURL, account, account, useTLSFallback)
}

// doOpenAIAccountTestUpstreamWithCredentialAccount mirrors the live test flow:
// a shadow's parent supplies plugin OAuth identity, while the selected account
// remains the transport/pool owner for the non-plugin fallback.
func (s *AccountTestService) doOpenAIAccountTestUpstreamWithCredentialAccount(
	request *http.Request,
	proxyURL string,
	account *Account,
	credentialAccount *Account,
	useTLSFallback bool,
) (*http.Response, error) {
	if s == nil {
		return nil, errors.New("openai account-test service is unavailable")
	}
	response, _, err := doOpenAIOAuthTransportWithCredentialAccount(
		request,
		proxyURL,
		account,
		credentialAccount,
		s.pluginManager,
		s.httpUpstream,
		s.tlsFPProfileService,
		useTLSFallback,
	)
	return response, err
}
