package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/pkg/tlsfingerprint"
	"github.com/tidwall/gjson"
)

type debugWorkbenchSourceStub struct {
	account *Account
	proxy   *Proxy
	gets    int
}

func (s *debugWorkbenchSourceStub) GetAccount(context.Context, int64) (*Account, error) {
	s.gets++
	return s.account, nil
}
func (s *debugWorkbenchSourceStub) GetProxy(context.Context, int64) (*Proxy, error) {
	return s.proxy, nil
}

type debugWorkbenchHTTPStub struct {
	fn func(*http.Request, string, int64) (*http.Response, error)
}

func (s *debugWorkbenchHTTPStub) Do(req *http.Request, proxy string, account int64, _ int) (*http.Response, error) {
	return s.fn(req, proxy, account)
}
func (s *debugWorkbenchHTTPStub) DoWithTLS(req *http.Request, proxy string, account int64, c int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxy, account, c)
}
func debugWorkbenchTestAccount() *Account {
	return &Account{ID: 7, Name: "debug-account", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-debug-secret", "base_url": "https://api.openai.com"}, Extra: map[string]any{"openai_responses_supported": true}}
}
func debugWorkbenchTestService(account *Account, upstream HTTPUpstream) (*DebugWorkbenchService, *debugWorkbenchSourceStub) {
	source := &debugWorkbenchSourceStub{account: account}
	gateway := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	return NewDebugWorkbenchService(gateway, source), source
}
func debugWorkbenchJSONResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"upstream-debug-id"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestDebugWorkbenchRunsFullBodyExactEndpointsAndRealHeaders(t *testing.T) {
	tests := []struct {
		endpoint, body, response, field string
		expected                        any
	}{
		{"responses", `{"model":"gpt-5.4","input":"full input","stream":false,"max_output_tokens":1234,"metadata":{"debug":"kept"}}`, `{"id":"resp_debug","object":"response","status":"completed","model":"gpt-5.4","output":[],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`, "max_output_tokens", int64(1234)},
		{"chat/completions", `{"model":"gpt-5.4","messages":[{"role":"user","content":"full input"}],"stream":false,"temperature":0.25,"seed":42}`, `{"id":"chatcmpl_debug","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`, "seed", int64(42)},
		{"images/generations", `{"model":"gpt-image-1","prompt":"a tree","n":1,"size":"1024x1024","quality":"high","background":"transparent","output_format":"png"}`, `{"created":1,"data":[{"b64_json":"aW1hZ2U="}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`, "quality", "high"},
	}
	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			account := debugWorkbenchTestAccount()
			if tt.endpoint == "chat/completions" {
				account.Extra["openai_responses_supported"] = false
			}
			var sent []byte
			var finalHeaders http.Header
			upstream := &debugWorkbenchHTTPStub{fn: func(req *http.Request, proxy string, id int64) (*http.Response, error) {
				require.Equal(t, "/v1/"+tt.endpoint, req.URL.Path)
				require.Equal(t, int64(7), id)
				require.Empty(t, proxy)
				sent, _ = io.ReadAll(req.Body)
				finalHeaders = req.Header.Clone()
				require.Equal(t, "Bearer sk-debug-secret", req.Header.Get("Authorization"))
				require.Equal(t, "zh-CN", req.Header.Get("Accept-Language"))
				require.Empty(t, req.Header.Get("Cookie"))
				return debugWorkbenchJSONResponse(http.StatusOK, tt.response), nil
			}}
			svc, _ := debugWorkbenchTestService(account, upstream)
			result, err := svc.Run(context.Background(), 23, 7, DebugWorkbenchRequest{Endpoint: tt.endpoint, Body: json.RawMessage(tt.body), Headers: map[string]string{"Authorization": "Bearer browser-admin-secret", "Accept-Language": "zh-CN", "Cookie": "session=private", "X-Custom-Debug": "filtered", "X-Client-Request-Id": "client-debug-id"}, Session: DebugSessionInput{Action: "new_session"}})
			require.NoError(t, err)
			require.True(t, result.Success, result.Error)
			require.Equal(t, "http", result.Transport)
			switch expected := tt.expected.(type) {
			case string:
				require.Equal(t, expected, gjson.GetBytes(sent, tt.field).String())
			case int64:
				require.Equal(t, expected, gjson.GetBytes(sent, tt.field).Int())
			}
			require.JSONEq(t, tt.body, string(result.Inbound.Body))
			if tt.endpoint == "chat/completions" {
				// Raw native Chat Completions intentionally has a narrower gateway
				// allowlist; its actual filtering must remain visible instead of changed.
				require.Empty(t, finalHeaders.Get("X-Client-Request-Id"))
				filtered := false
				for _, change := range result.Attempts[0].HeaderChanges {
					if strings.EqualFold(change.Name, "X-Client-Request-Id") {
						filtered = change.Action == "filtered"
					}
				}
				require.True(t, filtered)
			} else {
				require.NotEmpty(t, finalHeaders.Get("X-Client-Request-Id"))
				require.NotEmpty(t, finalHeaders.Get("Thread-Id"))
			}
			require.Len(t, result.Attempts, 1)
			require.Equal(t, "https://api.openai.com/v1/"+tt.endpoint, result.Attempts[0].Request.URL)
			require.Equal(t, 200, result.Outbound.StatusCode)
			require.Equal(t, 200, result.Attempts[0].Response.StatusCode)
			resultBytes, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(resultBytes), "sk-debug-secret")
			require.NotContains(t, string(resultBytes), "browser-admin-secret")
			require.NotContains(t, string(resultBytes), "session=private")
			require.NotContains(t, account.Extra, "openai_ws_force_http")
		})
	}
}

func TestDebugWorkbenchProxySelectionIsPerRun(t *testing.T) {
	proxyID := int64(10)
	account := debugWorkbenchTestAccount()
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{ID: 10, Protocol: "http", Host: "default-proxy.local", Port: 8080}
	var proxies []string
	upstream := &debugWorkbenchHTTPStub{fn: func(req *http.Request, proxy string, _ int64) (*http.Response, error) {
		proxies = append(proxies, proxy)
		return debugWorkbenchJSONResponse(200, `{"created":1,"data":[{"b64_json":"aW1hZ2U="}]}`), nil
	}}
	svc, source := debugWorkbenchTestService(account, upstream)
	source.proxy = &Proxy{ID: 11, Protocol: "socks5", Host: "selected-proxy.local", Port: 1080, Username: "debug-user", Password: "debug-password", Status: StatusActive}
	input := DebugWorkbenchRequest{Endpoint: "images/generations", Body: json.RawMessage(`{"model":"gpt-image-1","prompt":"a tree"}`)}
	result, err := svc.Run(context.Background(), 1, 7, input)
	require.NoError(t, err)
	require.True(t, result.Success, result.Error)
	direct := int64(0)
	input.ProxyID = &direct
	_, err = svc.Run(context.Background(), 1, 7, input)
	require.NoError(t, err)
	selected := int64(11)
	input.ProxyID = &selected
	result, err = svc.Run(context.Background(), 1, 7, input)
	require.NoError(t, err)
	require.Equal(t, []string{"http://default-proxy.local:8080", "", "socks5://debug-user:debug-password@selected-proxy.local:1080"}, proxies)
	serialized, _ := json.Marshal(result)
	require.NotContains(t, string(serialized), "debug-password")
	require.NotContains(t, string(serialized), "debug-user")
	require.Equal(t, int64(10), *account.ProxyID)
	require.Equal(t, "default-proxy.local", account.Proxy.Host)
}

func TestDebugWorkbenchErrorsRetainActualResponseAndReleaseSession(t *testing.T) {
	upstream := &debugWorkbenchHTTPStub{fn: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		return debugWorkbenchJSONResponse(400, `{"error":{"type":"invalid_request_error","message":"invalid quality; sk-debug-secret"}}`), nil
	}}
	svc, _ := debugWorkbenchTestService(debugWorkbenchTestAccount(), upstream)
	input := DebugWorkbenchRequest{Endpoint: "images/generations", Body: json.RawMessage(`{"model":"gpt-image-1","prompt":"a tree"}`)}
	result, err := svc.Run(context.Background(), 1, 7, input)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.NotEmpty(t, result.Error)
	require.Len(t, result.Attempts, 1)
	require.Equal(t, 400, result.Attempts[0].Response.StatusCode)
	require.Contains(t, string(result.Attempts[0].Response.Body), "invalid quality")
	serialized, _ := json.Marshal(result)
	require.NotContains(t, string(serialized), "sk-debug-secret")
	input.Session = DebugSessionInput{ID: result.Session.ID, Action: "continue_turn"}
	_, err = svc.Run(context.Background(), 1, 7, input)
	require.NoError(t, err)
}

func TestDebugWorkbenchContextCancellationReachesUpstream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	upstream := &debugWorkbenchHTTPStub{fn: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		cancel()
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(time.Second):
			t.Error("debug upstream detached cancellation")
			return nil, errors.New("did not cancel")
		}
	}}
	svc, _ := debugWorkbenchTestService(debugWorkbenchTestAccount(), upstream)
	result, err := svc.Run(ctx, 1, 7, DebugWorkbenchRequest{Endpoint: "images/generations", Body: json.RawMessage(`{"model":"gpt-image-1","prompt":"a tree"}`)})
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Len(t, result.Attempts, 1)
	require.NotEmpty(t, result.Attempts[0].Error)
}

func TestDebugWorkbenchValidatesBeforeAccountLookup(t *testing.T) {
	source := &debugWorkbenchSourceStub{}
	svc := NewDebugWorkbenchService(&OpenAIGatewayService{}, source)
	valid := DebugWorkbenchRequest{Endpoint: "responses", Body: json.RawMessage(`{"model":"gpt-5.4"}`)}
	tests := map[string]func(*DebugWorkbenchRequest){
		"endpoint":   func(r *DebugWorkbenchRequest) { r.Endpoint = "https://elsewhere.example" },
		"body array": func(r *DebugWorkbenchRequest) { r.Body = json.RawMessage(`[]`) },
		"body null":  func(r *DebugWorkbenchRequest) { r.Body = json.RawMessage(`null`) },
		"body large": func(r *DebugWorkbenchRequest) {
			r.Body = json.RawMessage(`{"x":"` + strings.Repeat("x", DebugWorkbenchMaxBodyBytes) + `"}`)
		},
		"header newline":        func(r *DebugWorkbenchRequest) { r.Headers = map[string]string{"X-Test": "value\r\nInjected: x"} },
		"header invalid name":   func(r *DebugWorkbenchRequest) { r.Headers = map[string]string{"Bad Header": "value"} },
		"header duplicate case": func(r *DebugWorkbenchRequest) { r.Headers = map[string]string{"Accept": "a", "accept": "b"} },
		"header too many": func(r *DebugWorkbenchRequest) {
			r.Headers = map[string]string{}
			for i := 0; i < 65; i++ {
				r.Headers[string(rune('A'+i))+"-Test"] = "x"
			}
		},
		"proxy negative": func(r *DebugWorkbenchRequest) { id := int64(-1); r.ProxyID = &id },
		"session action": func(r *DebugWorkbenchRequest) { r.Session.Action = "unknown" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			_, err := svc.Run(context.Background(), 1, 7, input)
			require.Error(t, err)
		})
	}
	require.Zero(t, source.gets)
	for _, raw := range []string{`{"endpoint":"responses","body":{}} {}`, `{"endpoint":"responses","body":{},"unknown":1}`} {
		_, err := DecodeDebugWorkbenchRequest(strings.NewReader(raw))
		require.Error(t, err)
	}
}

func TestDebugWorkbenchConcurrencyLimit(t *testing.T) {
	svc, _ := debugWorkbenchTestService(debugWorkbenchTestAccount(), nil)
	for i := 0; i < cap(svc.slots); i++ {
		svc.slots <- struct{}{}
	}
	_, err := svc.Run(context.Background(), 1, 7, DebugWorkbenchRequest{Endpoint: "responses", Body: json.RawMessage(`{"model":"gpt-5.4"}`)})
	var inputErr *DebugWorkbenchInputError
	require.ErrorAs(t, err, &inputErr)
	require.Equal(t, http.StatusTooManyRequests, inputErr.StatusCode)
}

func TestDebugWorkbenchSessionHeadersReplayOnlyObservedUpstreamState(t *testing.T) {
	var requests []http.Header
	upstream := &debugWorkbenchHTTPStub{fn: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		requests = append(requests, req.Header.Clone())
		response := debugWorkbenchJSONResponse(200, `{"id":"resp_debug","object":"response","status":"completed","model":"gpt-5.4","output":[],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`)
		response.Header.Set("X-Codex-Turn-State", "observed-upstream-state")
		return response, nil
	}}
	svc, _ := debugWorkbenchTestService(debugWorkbenchTestAccount(), upstream)
	input := DebugWorkbenchRequest{Endpoint: "responses", Body: json.RawMessage(`{"model":"gpt-5.4","input":"hello","stream":false}`), Headers: map[string]string{"X-Codex-Turn-State": "forged-client-state", "Thread-Id": "forged-client-thread"}, Session: DebugSessionInput{Action: "new_session"}}
	first, err := svc.Run(context.Background(), 1, 7, input)
	require.NoError(t, err)
	require.True(t, first.Success, first.Error)
	require.True(t, first.Session.TurnStateAvailable)
	require.Empty(t, requests[0].Get("X-Codex-Turn-State"))
	require.NotEqual(t, "forged-client-thread", requests[0].Get("Thread-Id"))
	input.Session = DebugSessionInput{ID: first.Session.ID, Action: "continue_turn"}
	second, err := svc.Run(context.Background(), 1, 7, input)
	require.NoError(t, err)
	require.True(t, second.Success, second.Error)
	require.Equal(t, "observed-upstream-state", requests[1].Get("X-Codex-Turn-State"))
	require.Equal(t, requests[0].Get("Thread-Id"), requests[1].Get("Thread-Id"))
	require.Equal(t, requests[0].Get("Turn-Id"), requests[1].Get("Turn-Id"))
	require.NotEqual(t, requests[0].Get("X-Client-Request-Id"), requests[1].Get("X-Client-Request-Id"))
	input.Session.Action = "new_turn"
	third, err := svc.Run(context.Background(), 1, 7, input)
	require.NoError(t, err)
	require.True(t, third.Success, third.Error)
	require.Empty(t, requests[2].Get("X-Codex-Turn-State"))
	require.Equal(t, requests[1].Get("Thread-Id"), requests[2].Get("Thread-Id"))
	require.NotEqual(t, requests[1].Get("Turn-Id"), requests[2].Get("Turn-Id"))
	require.Equal(t, 2, third.Session.TurnIndex)
	_, err = svc.Run(context.Background(), 2, 7, input)
	var scopedErr *DebugWorkbenchInputError
	require.ErrorAs(t, err, &scopedErr)
	require.Equal(t, 404, scopedErr.StatusCode)
}

func TestDebugWorkbenchAlignsExistingSessionMetadataWithoutInventingAncestry(t *testing.T) {
	view := DebugSessionView{SessionID: "managed-session", ThreadID: "managed-thread", TurnID: "managed-turn", WindowID: "managed-window"}
	headers := http.Header{}
	headers.Set("X-Codex-Turn-Metadata", `{"thread_id":"stale-thread","turn_id":"stale-turn","parent_thread_id":"parent-thread","parent_turn_id":"parent-turn","ordinal":9223372036854775806,"subagent":"reviewer"}`)
	body := []byte(`{"model":"custom-model","metadata":{"keep":"untouched"},"client_metadata":{"session_id":"old-session","window_id":"old-window","x-codex-turn-metadata":"{\"turn_id\":\"old-turn\",\"root_turn_id\":\"root-turn\"}","custom":42}}`)
	out, changed, err := debugWorkbenchAlignSessionMetadata(headers, body, view)
	require.NoError(t, err)
	require.True(t, changed)
	rawHeader := headers.Get("X-Codex-Turn-Metadata")
	require.Equal(t, "managed-thread", gjson.Get(rawHeader, "thread_id").String())
	require.Equal(t, "managed-turn", gjson.Get(rawHeader, "turn_id").String())
	require.Equal(t, "parent-thread", gjson.Get(rawHeader, "parent_thread_id").String())
	require.Equal(t, "parent-turn", gjson.Get(rawHeader, "parent_turn_id").String())
	require.Equal(t, "9223372036854775806", gjson.Get(rawHeader, "ordinal").Raw)
	require.Equal(t, "reviewer", gjson.Get(rawHeader, "subagent").String())
	require.Equal(t, "managed-session", gjson.GetBytes(out, "client_metadata.session_id").String())
	require.Equal(t, "managed-window", gjson.GetBytes(out, "client_metadata.window_id").String())
	embedded := gjson.GetBytes(out, "client_metadata.x-codex-turn-metadata").String()
	require.Equal(t, "managed-turn", gjson.Get(embedded, "turn_id").String())
	require.Equal(t, "root-turn", gjson.Get(embedded, "root_turn_id").String())
	require.Equal(t, "custom-model", gjson.GetBytes(out, "model").String())
	require.Equal(t, "untouched", gjson.GetBytes(out, "metadata.keep").String())
	unchanged := []byte(`{"model":"custom-model","client_metadata":{"parent_thread_id":"parent-thread","ordinal":9223372036854775806}}`)
	out, changed, err = debugWorkbenchAlignSessionMetadata(http.Header{}, unchanged, view)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, unchanged, out)
	_, _, err = debugWorkbenchAlignSessionMetadata(http.Header{"X-Codex-Turn-Metadata": {"[]"}}, body, view)
	require.Error(t, err)
}

type debugWorkbenchResolverStub struct {
	resolve func(*Account) OpenAIWSProtocolDecision
}

func (s debugWorkbenchResolverStub) Resolve(account *Account) OpenAIWSProtocolDecision {
	return s.resolve(account)
}

func TestDebugWorkbenchForcesHTTPOnPrivateAccountCopy(t *testing.T) {
	account := debugWorkbenchTestAccount()
	account.Extra["openai_ws_force_http"] = false
	upstream := &debugWorkbenchHTTPStub{fn: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		return debugWorkbenchJSONResponse(200, `{"id":"resp_debug","object":"response","status":"completed","model":"gpt-5.4","output":[],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`), nil
	}}
	svc, _ := debugWorkbenchTestService(account, upstream)
	resolved := false
	svc.gateway.openaiWSResolver = debugWorkbenchResolverStub{resolve: func(selected *Account) OpenAIWSProtocolDecision {
		resolved = true
		require.NotSame(t, account, selected)
		require.True(t, selected.IsOpenAIWSForceHTTPEnabled())
		return OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportHTTPSSE, Reason: "test_force_http"}
	}}
	result, err := svc.Run(context.Background(), 1, 7, DebugWorkbenchRequest{Endpoint: "responses", Body: json.RawMessage(`{"model":"gpt-5.4","input":"hello","stream":false}`)})
	require.NoError(t, err)
	require.True(t, result.Success, result.Error)
	require.True(t, resolved)
	require.False(t, account.IsOpenAIWSForceHTTPEnabled())
	require.Equal(t, "http", result.Transport)
}

type debugWorkbenchShadowRepo struct {
	AccountRepository
	parent *Account
}

func (r *debugWorkbenchShadowRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.parent, nil
}

func TestDebugWorkbenchOAuthUsesNativeGatewayAndParentCredentials(t *testing.T) {
	for _, shadow := range []bool{false, true} {
		name := "oauth"
		if shadow {
			name = "shadow"
		}
		t.Run(name, func(t *testing.T) {
			parent := &Account{ID: 71, Name: "oauth-parent", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1, Credentials: map[string]any{"access_token": "oauth-private-access-token", "chatgpt_account_id": "private-chatgpt-account"}}
			account := parent
			if shadow {
				id := parent.ID
				account = &Account{ID: 72, Name: "oauth-shadow", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1, ParentAccountID: &id, Credentials: map[string]any{}}
			}
			var actual http.Header
			var actualBody []byte
			upstream := &debugWorkbenchHTTPStub{fn: func(req *http.Request, _ string, selectedID int64) (*http.Response, error) {
				require.Equal(t, account.ID, selectedID)
				require.Equal(t, chatgptCodexURL, req.URL.String())
				require.Equal(t, "chatgpt.com", req.Host)
				require.Equal(t, "Bearer oauth-private-access-token", req.Header.Get("Authorization"))
				require.Equal(t, "private-chatgpt-account", req.Header.Get("Chatgpt-Account-Id"))
				require.Empty(t, req.Header.Get("Cookie"))
				actual = req.Header.Clone()
				actualBody, _ = io.ReadAll(req.Body)
				response := debugWorkbenchJSONResponse(200, "data: "+`{"type":"response.completed","response":{"id":"resp_oauth_debug","object":"response","status":"completed","model":"gpt-5.4","output":[{"type":"message","id":"msg_debug","role":"assistant","content":[{"type":"output_text","text":"native ok"}]}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`+"\n\ndata: [DONE]\n\n")
				response.Header.Set("Content-Type", "text/event-stream")
				response.Header.Set("X-Codex-Turn-State", "native-observed-state")
				return response, nil
			}}
			svc, _ := debugWorkbenchTestService(account, upstream)
			if shadow {
				svc.gateway.accountRepo = &debugWorkbenchShadowRepo{parent: parent}
			}
			input := DebugWorkbenchRequest{Endpoint: "responses", Body: json.RawMessage(`{"model":"gpt-5.4","input":[{"role":"user","content":[{"type":"input_text","text":"complete custom prompt"}]}],"stream":false,"service_tier":"fast","prompt_cache_key":"explicit-cache-key","reasoning":{"effort":"high"},"text":{"verbosity":"low"}}`), Headers: map[string]string{"User-Agent": "Browser/1.0", "Originator": "browser", "Version": "0.0.1", "Accept-Language": "zh-CN", "Authorization": "Bearer injected-admin-secret", "Chatgpt-Account-Id": "injected-account", "Cookie": "private-browser-cookie", "X-Codex-Routing-Hint": "model=spoofed;tier=flex"}, Session: DebugSessionInput{Action: "new_session"}}
			if shadow {
				input.Body = json.RawMessage(strings.Replace(string(input.Body), `,"prompt_cache_key":"explicit-cache-key"`, "", 1))
			}
			result, err := svc.Run(context.Background(), 23, account.ID, input)
			require.NoError(t, err)
			require.True(t, result.Success, result.Error)
			require.Len(t, result.Attempts, 1)
			require.Equal(t, "text/event-stream", actual.Get("Accept"))
			require.Equal(t, "application/json", actual.Get("Content-Type"))
			require.Equal(t, "zh-CN", actual.Get("Accept-Language"))
			require.Equal(t, CodexCanonicalClientVersion(), actual.Get("Version"))
			require.NotEqual(t, "Browser/1.0", actual.Get("User-Agent"))
			require.NotEmpty(t, actual.Get("User-Agent"))
			require.NotEqual(t, "browser", actual.Get("Originator"))
			require.NotEmpty(t, actual.Get("Originator"))
			require.Equal(t, openAIRemoteCompactionV2Feature, actual.Get("X-Codex-Beta-Features"))
			require.Equal(t, "gpt-5.4", gjson.GetBytes(actualBody, "model").String())
			require.Equal(t, "priority", gjson.GetBytes(actualBody, "service_tier").String())
			require.Equal(t, "model=gpt-5.4;tier=priority", actual.Get("X-Codex-Routing-Hint"))
			require.Equal(t, "high", gjson.GetBytes(actualBody, "reasoning.effort").String())
			require.Equal(t, "low", gjson.GetBytes(actualBody, "text.verbosity").String())
			require.Contains(t, string(actualBody), "complete custom prompt")
			require.NotEmpty(t, actual.Get("session_id"))
			cacheKey := "explicit-cache-key"
			if shadow {
				cacheKey = result.Session.SessionID
				require.False(t, gjson.GetBytes(result.Inbound.Body, "prompt_cache_key").Exists())
				require.Contains(t, strings.Join(result.Warnings, " "), "Body 未指定 prompt_cache_key")
			} else {
				require.Equal(t, "explicit-cache-key", gjson.GetBytes(result.Inbound.Body, "prompt_cache_key").String())
			}
			require.Equal(t, isolateOpenAIUpstreamSessionID(-23, parent, cacheKey), actual.Get("session_id"))
			require.Equal(t, scopeCodexAccountIdentityValue(parent, -23, "prompt-cache", cacheKey), gjson.GetBytes(actualBody, "prompt_cache_key").String())
			require.Equal(t, scopeCodexAccountIdentityValue(parent, -23, "thread", result.Session.ThreadID), actual.Get("Thread-Id"))
			require.NotEmpty(t, actual.Get("Thread-Id"))
			require.True(t, result.Session.TurnStateAvailable)
			serialized, _ := json.Marshal(result)
			for _, secret := range []string{"oauth-private-access-token", "private-chatgpt-account", "injected-admin-secret", "private-browser-cookie"} {
				require.NotContains(t, string(serialized), secret)
			}
			require.Contains(t, string(result.Outbound.Body), "native ok")
			require.Contains(t, result.Attempts[0].Response.BodyText, "response.completed")
		})
	}
}

func TestDebugWorkbenchSessionPromptCacheOnlyDefaultsOAuthCarrier(t *testing.T) {
	oauth := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	for _, endpoint := range []string{"responses", "chat/completions"} {
		body, key, note := debugWorkbenchSessionPromptCache(oauth, endpoint, []byte(`{"model":"custom-model","input":"complete","custom":{"ordinal":9223372036854775806}}`), "managed-session")
		require.Equal(t, "managed-session", key)
		require.NotEmpty(t, note)
		require.Equal(t, "managed-session", gjson.GetBytes(body, "prompt_cache_key").String())
		require.Equal(t, "9223372036854775806", gjson.GetBytes(body, "custom.ordinal").Raw)
		for _, explicit := range []string{`{"prompt_cache_key":"user-chosen"}`, `{"prompt_cache_key":""}`, `{"prompt_cache_key":null}`} {
			output, _, note := debugWorkbenchSessionPromptCache(oauth, endpoint, []byte(explicit), "managed-session")
			require.Equal(t, []byte(explicit), output)
			require.Contains(t, note, "显式")
		}
	}
	image := []byte(`{"model":"gpt-image-1","prompt":"tree"}`)
	output, key, note := debugWorkbenchSessionPromptCache(oauth, "images/generations", image, "managed-session")
	require.Equal(t, image, output)
	require.Empty(t, key)
	require.Contains(t, note, "Images→Responses")
	body := []byte(`{"model":"custom-model","input":"complete"}`)
	output, key, note = debugWorkbenchSessionPromptCache(debugWorkbenchTestAccount(), "responses", body, "managed-session")
	require.Equal(t, body, output)
	require.Empty(t, key)
	require.Empty(t, note)
}

func TestDebugWorkbenchSkipsPublicResponseAffinity(t *testing.T) {
	gateway := &OpenAIGatewayService{}
	trace := NewDebugWorkbenchTrace(nil, nil)
	require.NoError(t, gateway.bindHTTPResponseAccount(trace.Context(context.Background()), nil, nil, "resp_debug"))
	require.Error(t, gateway.bindHTTPResponseAccount(context.Background(), nil, nil, "resp_debug"))
}
