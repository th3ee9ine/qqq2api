package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

type openAICodexTurnStateTicketContextKey struct{}

var openAICodexTurnStateForeignIdentityHeaders = [...]string{
	"session-id",
	"conversation_id",
	"installation_id",
	"x-codex-installation-id",
	"thread_id",
	"thread-id",
	"turn_id",
	"turn-id",
	"window_id",
	"x-codex-window-id",
	"x-client-request-id",
	"x-codex-turn-metadata",
}

var openAICodexTurnStateForeignBodyKeys = [...]string{
	"prompt_cache_key",
	"client_metadata",
	"device_id",
}

func bindOpenAICodexTurnStateTicketRequest(req *http.Request, snapshot OpenAICodexTurnStateSnapshot) *http.Request {
	if req == nil || snapshot.Route == "client" || snapshot.Token.Value == "" ||
		(!snapshot.EgressPinned && snapshot.HarvestSessionID == "" && len(snapshot.HarvestCookies) == 0) {
		return req
	}
	ctx := context.WithValue(req.Context(), openAICodexTurnStateTicketContextKey{}, cloneOpenAICodexTurnStateSnapshot(snapshot))
	return req.WithContext(ctx)
}

func openAICodexTurnStateTicketFromContext(ctx context.Context) (OpenAICodexTurnStateSnapshot, bool) {
	if ctx == nil {
		return OpenAICodexTurnStateSnapshot{}, false
	}
	snapshot, ok := ctx.Value(openAICodexTurnStateTicketContextKey{}).(OpenAICodexTurnStateSnapshot)
	if !ok || snapshot.Route == "client" || snapshot.Token.Value == "" {
		return OpenAICodexTurnStateSnapshot{}, false
	}
	return cloneOpenAICodexTurnStateSnapshot(snapshot), true
}

func (s *OpenAIGatewayService) pinOpenAICodexTurnStateWSIdentity(
	c interface{ Get(string) (any, bool) },
	account *Account,
	model string,
	headers http.Header,
	defaultProxyURL string,
) string {
	if s == nil || c == nil || account == nil || headers == nil {
		return defaultProxyURL
	}
	raw, ok := c.Get(openAICodexTurnStateContextKey)
	if !ok {
		return defaultProxyURL
	}
	binding, ok := raw.(openAICodexTurnStateRequestBinding)
	if !ok || !binding.used || binding.key.AccountID != account.ID ||
		!codexTurnStateModelIdentitiesMatch(binding.model, model) || binding.snapshot.Route == "client" {
		return defaultProxyURL
	}
	applyOpenAICodexTurnStateTicketHeaders(headers, binding.snapshot, time.Now())
	if binding.snapshot.EgressPinned {
		return binding.snapshot.EgressProxyURL
	}
	return defaultProxyURL
}

func responseOpenAICodexTurnStateCookies(headers http.Header) []OpenAICodexTurnStateCookie {
	if headers == nil || len(headers.Values("Set-Cookie")) == 0 {
		return nil
	}
	response := &http.Response{Header: headers}
	cookies := response.Cookies()
	result := make([]OpenAICodexTurnStateCookie, 0, min(len(cookies), openAICodexTurnStateMaxCookies))
	for _, cookie := range cookies {
		if cookie == nil || strings.TrimSpace(cookie.Name) == "" {
			continue
		}
		result = append(result, OpenAICodexTurnStateCookie{Name: cookie.Name, Value: cookie.Value})
		if len(result) >= openAICodexTurnStateMaxCookies {
			break
		}
	}
	return mergeOpenAICodexTurnStateCookies(nil, result)
}

func applyOpenAICodexTurnStateTicketHeaders(headers http.Header, snapshot OpenAICodexTurnStateSnapshot, now time.Time) {
	if headers == nil {
		return
	}
	for _, name := range openAICodexTurnStateForeignIdentityHeaders {
		headers.Del(name)
	}
	if snapshot.HarvestSessionID != "" {
		headers.Set("session_id", snapshot.HarvestSessionID)
	}
	if !snapshot.cookiesFresh(now) {
		headers.Del("Cookie")
		return
	}
	pairs := make([]string, 0, len(snapshot.HarvestCookies))
	for _, pair := range snapshot.HarvestCookies {
		cookie := (&http.Cookie{Name: pair.Name, Value: pair.Value}).String()
		if cookie != "" {
			pairs = append(pairs, cookie)
		}
	}
	if len(pairs) == 0 {
		headers.Del("Cookie")
		return
	}
	headers.Set("Cookie", strings.Join(pairs, "; "))
}

func pinOpenAICodexTurnStateTicketRequest(req *http.Request, snapshot OpenAICodexTurnStateSnapshot) {
	if req == nil {
		return
	}
	applyOpenAICodexTurnStateTicketHeaders(req.Header, snapshot, time.Now())
	if snapshot.HarvestSessionID == "" {
		return
	}
	raw, err := snapshotOpenAICodexTurnStateRequestBody(req)
	if err != nil || len(raw) == 0 {
		return
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || body == nil {
		return
	}
	changed := false
	for _, key := range openAICodexTurnStateForeignBodyKeys {
		if _, exists := body[key]; exists {
			delete(body, key)
			changed = true
		}
	}
	if !changed {
		return
	}
	next, err := json.Marshal(body)
	if err != nil {
		return
	}
	replaceOpenAICodexTurnStateRequestBody(req, next)
}

func snapshotOpenAICodexTurnStateRequestBody(req *http.Request) ([]byte, error) {
	if req == nil {
		return nil, nil
	}
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		defer body.Close()
		return io.ReadAll(body)
	}
	if req.Body == nil {
		return nil, nil
	}
	raw, err := io.ReadAll(req.Body)
	replaceOpenAICodexTurnStateRequestBody(req, raw)
	return raw, err
}

func replaceOpenAICodexTurnStateRequestBody(req *http.Request, body []byte) {
	if req == nil {
		return
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
}
