package service

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	codexTurnStateProbeAttemptTimeout  = 12 * time.Second
	codexTurnStateProbeSessionTimeout  = 24 * time.Second
	codexTurnStateProbeTotalTimeout    = 90 * time.Second
	codexTurnStateProbePoolSize        = 3
	codexTurnStateProbe429Fallback     = 5 * time.Minute
	codexTurnStateProbeProxyNamePrefix = "codex-turn-state-probe:"
	codexTurnState1024ProxyHost        = "us.1024proxy.io"
	codexTurnState1024ProxyPort        = 3000
	codexTurnStateProbeSIDLength       = 8

	// These are local diagnostics only. They deliberately contain no status
	// body, account data, proxy URL or Retry-After value.
	codexTurnStateProbe429RetryAfterCode   = "http_429_retry_after"
	codexTurnStateProbe429NoRetryAfterCode = "http_429_no_retry_after"
)

var errInvalidCodexTurnStateProbeProxy = errors.New("invalid dedicated Turn State probe proxy")

type codexTurnStateProbeProxyTemplate struct {
	protocol  string
	host      string
	port      int
	account   string
	region    string
	password  string
	sessionID string
	dynamic   bool
}

func codexTurnStateDedicatedProxy(proxy Proxy) bool {
	name := strings.TrimSpace(proxy.Name)
	if strings.HasPrefix(name, codexTurnStateProbeProxyNamePrefix) {
		return true
	}
	// Older local deployments stored the same explicit purpose in names such as
	// "team3-state-US-<sid>" before the reserved prefix was introduced. Keep
	// those records usable only when they are actually 1024Proxy records; a
	// generic proxy named "state" must never become a dynamic configuration.
	if strings.ToLower(strings.TrimSpace(proxy.Host)) != codexTurnState1024ProxyHost || proxy.Port != codexTurnState1024ProxyPort {
		return false
	}
	for _, token := range strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == ':' || r == ' ' || r == '\t'
	}) {
		if strings.EqualFold(token, "state") || strings.EqualFold(token, "turnstate") {
			return true
		}
	}
	return false
}

// parseCodexTurnStateProbeProxyTemplate accepts the 1024Proxy dashboard's
// host:port:username:password form and ordinary proxy URLs. It never includes
// the input or credentials in returned errors.
func parseCodexTurnStateProbeProxyTemplate(raw string) (codexTurnStateProbeProxyTemplate, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\r\n\x00") {
		return codexTurnStateProbeProxyTemplate{}, errInvalidCodexTurnStateProbeProxy
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.User == nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return codexTurnStateProbeProxyTemplate{}, errInvalidCodexTurnStateProbeProxy
		}
		password, ok := parsed.User.Password()
		if !ok {
			return codexTurnStateProbeProxyTemplate{}, errInvalidCodexTurnStateProbeProxy
		}
		port, err := strconv.Atoi(parsed.Port())
		if err != nil {
			return codexTurnStateProbeProxyTemplate{}, errInvalidCodexTurnStateProbeProxy
		}
		return newCodexTurnStateProbeProxyTemplate(parsed.Scheme, parsed.Hostname(), port, parsed.User.Username(), password)
	}

	parts := strings.SplitN(raw, ":", 4)
	if len(parts) != 4 {
		return codexTurnStateProbeProxyTemplate{}, errInvalidCodexTurnStateProbeProxy
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		return codexTurnStateProbeProxyTemplate{}, errInvalidCodexTurnStateProbeProxy
	}
	return newCodexTurnStateProbeProxyTemplate("socks5", parts[0], port, parts[2], parts[3])
}

func codexTurnStateProbeProxyTemplateFromRecord(proxy Proxy) (codexTurnStateProbeProxyTemplate, error) {
	return newCodexTurnStateProbeProxyTemplate(proxy.Protocol, proxy.Host, proxy.Port, proxy.Username, proxy.Password)
}

func newCodexTurnStateProbeProxyTemplate(protocol, host string, port int, username, password string) (codexTurnStateProbeProxyTemplate, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	host = strings.ToLower(strings.TrimSpace(host))
	if protocol != "http" && protocol != "https" && protocol != "socks5" && protocol != "socks5h" ||
		host != codexTurnState1024ProxyHost || port != codexTurnState1024ProxyPort ||
		len(username) > 100 || !codexTurnStateProbeCredentialSafe(password) {
		return codexTurnStateProbeProxyTemplate{}, errInvalidCodexTurnStateProbeProxy
	}

	account, region, sessionID, dynamic, ok := parseCodexTurnState1024ProxyUsername(username)
	canonicalSID := sessionID
	if dynamic {
		canonicalSID = strings.Repeat("x", codexTurnStateProbeSIDLength)
	}
	if !ok || len(account+"-region-"+region+"-sid-"+canonicalSID+"-t-5") > 100 {
		return codexTurnStateProbeProxyTemplate{}, errInvalidCodexTurnStateProbeProxy
	}
	return codexTurnStateProbeProxyTemplate{
		protocol: protocol, host: host, port: port, account: account, region: region,
		password: password, sessionID: sessionID, dynamic: dynamic,
	}, nil
}

func parseCodexTurnState1024ProxyUsername(username string) (account, region, sessionID string, dynamic, ok bool) {
	const (
		regionMarker  = "-region-"
		sessionMarker = "-sid-"
		stickySuffix  = "-t-5"
	)
	if strings.TrimSpace(username) != username || username == "" {
		return "", "", "", false, false
	}
	regionIndex := strings.LastIndex(username, regionMarker)
	if regionIndex <= 0 {
		return "", "", "", false, false
	}
	account = username[:regionIndex]
	remainder := username[regionIndex+len(regionMarker):]
	if strings.HasSuffix(remainder, stickySuffix) {
		remainder = strings.TrimSuffix(remainder, stickySuffix)
		sessionIndex := strings.LastIndex(remainder, sessionMarker)
		if sessionIndex <= 0 {
			return "", "", "", false, false
		}
		region = remainder[:sessionIndex]
		sessionID = remainder[sessionIndex+len(sessionMarker):]
		if codexTurnStateProbeAccountSafe(account) && codexTurnStateProbeRegionSafe(region) && codexTurnStateProbeSIDSafe(sessionID) {
			return account, region, sessionID, false, true
		}
		return "", "", "", false, false
	}
	region = remainder
	if codexTurnStateProbeAccountSafe(account) && codexTurnStateProbeRegionSafe(region) {
		return account, region, "", true, true
	}
	return "", "", "", false, false
}

func codexTurnStateProbeAccountSafe(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

func codexTurnStateProbeRegionSafe(value string) bool {
	// A Rand route can change countries when a new SID is created. State
	// acceptance requires every collection attempt to stay in one explicit
	// country, so only a two-letter country selector is eligible here.
	return len(value) == 2 && value[0] >= 'A' && value[0] <= 'Z' && value[1] >= 'A' && value[1] <= 'Z'
}

func codexTurnStateProbeSIDSafe(value string) bool {
	if len(value) < codexTurnStateProbeSIDLength || len(value) > 32 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9') {
			return false
		}
	}
	return true
}

func codexTurnStateProbeCredentialSafe(value string) bool {
	if value == "" || len(value) > 100 {
		return false
	}
	for _, ch := range []byte(value) {
		if ch < 0x21 || ch > 0x7e {
			return false
		}
	}
	return true
}

func codexTurnStateProbeRandomSID(reader io.Reader) (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	const unbiasedLimit = 248 // largest multiple of len(alphabet) below 256
	result := make([]byte, 0, codexTurnStateProbeSIDLength)
	buffer := []byte{0}
	for len(result) < codexTurnStateProbeSIDLength {
		if _, err := io.ReadFull(reader, buffer); err != nil {
			return "", errInvalidCodexTurnStateProbeProxy
		}
		if buffer[0] >= unbiasedLimit {
			continue
		}
		result = append(result, alphabet[int(buffer[0])%len(alphabet)])
	}
	return string(result), nil
}

func (template codexTurnStateProbeProxyTemplate) route(sessionID string) string {
	username := template.account + "-region-" + template.region + "-sid-" + sessionID + "-t-5"
	result := &url.URL{Scheme: template.protocol, Host: net.JoinHostPort(template.host, strconv.Itoa(template.port))}
	result.User = url.UserPassword(username, template.password)
	return result.String()
}

func buildCodexTurnStateProbeRoutes(templates []codexTurnStateProbeProxyTemplate, primary string, random io.Reader) []string {
	if len(templates) == 0 || random == nil {
		return nil
	}
	base := templates[0]
	sessions := make(map[string]struct{}, codexTurnStateProbePoolSize)
	dynamic := false
	for _, template := range templates {
		if template.protocol != base.protocol || template.host != base.host || template.port != base.port ||
			template.account != base.account || template.region != base.region || template.password != base.password {
			return nil
		}
		if template.dynamic {
			dynamic = true
			continue
		}
		sessions[template.sessionID] = struct{}{}
	}

	sessionIDs := make([]string, 0, len(sessions))
	for sessionID := range sessions {
		sessionIDs = append(sessionIDs, sessionID)
	}
	rand.Shuffle(len(sessionIDs), func(i, j int) { sessionIDs[i], sessionIDs[j] = sessionIDs[j], sessionIDs[i] })

	// A bare fixed-country template fills the remaining slots with fresh sticky
	// sessions. Explicit values retain their exact supplied SID. The region in
	// the authenticated username, not the proxy hostname, is the country boundary.
	for attempts := 0; dynamic && len(sessionIDs) < codexTurnStateProbePoolSize && attempts < codexTurnStateProbePoolSize*8; attempts++ {
		sessionID, err := codexTurnStateProbeRandomSID(random)
		if err != nil {
			return nil
		}
		if _, exists := sessions[sessionID]; exists {
			continue
		}
		sessions[sessionID] = struct{}{}
		sessionIDs = append(sessionIDs, sessionID)
	}

	seenRoutes := map[string]struct{}{primary: {}}
	routes := make([]string, 0, codexTurnStateProbePoolSize)
	for _, sessionID := range sessionIDs {
		route := base.route(sessionID)
		if _, exists := seenRoutes[route]; exists {
			continue
		}
		seenRoutes[route] = struct{}{}
		routes = append(routes, route)
		if len(routes) == codexTurnStateProbePoolSize {
			break
		}
	}
	return routes
}

func codexTurnStateAccountProxy(account *Account) string {
	if account != nil && account.ProxyID != nil && account.Proxy != nil {
		return account.Proxy.URL()
	}
	return ""
}

func codexTurnStateFreshNormal(state string, now time.Time) bool {
	issued, _, ok := parseCodexTurnState(state)
	// Envelope size is diagnostic only. Eligibility is established by raw
	// response-model validation and bounded local age, not 292/312/332/356.
	return ok && !issued.After(now.Add(time.Minute)) && now.Before(issued.Add(codexTurnStateTTL))
}

func codexTurnStateProbeRetryable(err error) bool {
	if err == nil {
		return false
	}
	switch err.Error() {
	case "auth_failed", "account_unavailable", "probe_boundary_refresh_failed", "http_400", "http_401", "http_402", "http_403", "http_404", "http_422", "http_429",
		codexTurnStateProbe429RetryAfterCode, codexTurnStateProbe429NoRetryAfterCode:
		return false
	default:
		return true
	}
}

// codexTurnStateProbeRetryAfter parses the upstream Retry-After instruction
// without accepting a zero/negative/expired value as a retry window. The
// boolean distinguishes a valid future instruction from an absent or malformed
// header, so callers can report a bounded 429 decision without guessing.
//
// A delta is interpreted as seconds per RFC 9110. An HTTP-date is evaluated
// against now, which is injected by callers/tests to keep the decision
// deterministic. Very large deltas are rejected instead of overflowing a
// time.Duration.
func codexTurnStateProbeRetryAfter(header http.Header, now time.Time) (time.Duration, bool) {
	if header == nil {
		return 0, false
	}
	raw := strings.TrimSpace(header.Get("Retry-After"))
	if raw == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if seconds <= 0 || seconds > int64((time.Duration(1<<63-1))/time.Second) {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	at, err := http.ParseTime(raw)
	if err != nil {
		return 0, false
	}
	delay := at.Sub(now)
	if delay <= 0 {
		return 0, false
	}
	return delay, true
}

// codexTurnStateProbe429Diagnostic classifies a 429 without exposing
// upstream/proxy details. A 429 is never a route-failover signal: the caller
// must stop rotating identities. Missing or malformed Retry-After values use a
// finite local backoff so a busy worker cannot immediately retry the limit.
func codexTurnStateProbe429Diagnostic(statusCode int, header http.Header, now time.Time) (code string, retryAfter time.Duration) {
	if statusCode != http.StatusTooManyRequests {
		return "", 0
	}
	if delay, ok := codexTurnStateProbeRetryAfter(header, now); ok {
		return codexTurnStateProbe429RetryAfterCode, delay
	}
	return codexTurnStateProbe429NoRetryAfterCode, codexTurnStateProbe429Fallback
}

func codexTurnStateProxyUsable(proxy Proxy, now time.Time) bool {
	if !proxy.IsActive() || proxy.IsExpired(now) || strings.TrimSpace(proxy.Host) == "" || strings.TrimSpace(proxy.Host) != proxy.Host || proxy.Port <= 0 || proxy.Port > 65535 {
		return false
	}
	if strings.TrimSpace(proxy.Protocol) != proxy.Protocol || strings.ContainsAny(proxy.Host, "\r\n\x00") || strings.ContainsAny(proxy.Protocol, "\r\n\x00") {
		return false
	}
	switch strings.ToLower(proxy.Protocol) {
	case "http", "https", "socks5", "socks5h":
		return true
	default:
		return false
	}
}

func codexTurnStateProxyRoute(proxy Proxy) string {
	proxy.Protocol = strings.ToLower(strings.TrimSpace(proxy.Protocol))
	proxy.Host = strings.TrimSpace(proxy.Host)
	return proxy.URL()
}

// codexTurnStateIPPoolRoutes returns a bounded, de-duplicated snapshot of the
// ordinary active proxy pool. This is a request-level route override only: it
// never changes the account's persisted proxy binding. The primary route is
// excluded because a pool fallback is meant to provide a fresh exit.
func codexTurnStateIPPoolRoutes(proxies []Proxy, primary string, now time.Time) []string {
	seen := make(map[string]struct{}, len(proxies)+1)
	if primary != "" {
		seen[primary] = struct{}{}
	}
	routes := make([]string, 0, min(len(proxies), codexTurnStateProbePoolSize))
	for _, proxy := range proxies {
		if !codexTurnStateProxyUsable(proxy, now) {
			continue
		}
		route := codexTurnStateProxyRoute(proxy)
		if route == "" {
			continue
		}
		if _, exists := seen[route]; exists {
			continue
		}
		seen[route] = struct{}{}
		routes = append(routes, route)
	}
	// Spread concurrent accounts over the pool without repeating an exit in the
	// same maintenance run. The worker itself performs at most one collection
	// plus same-route replay per returned route.
	rand.Shuffle(len(routes), func(i, j int) { routes[i], routes[j] = routes[j], routes[i] })
	if len(routes) > codexTurnStateProbePoolSize {
		routes = routes[:codexTurnStateProbePoolSize]
	}
	return routes
}

// Pool routes are used only for maintenance probes. Explicitly marked dynamic
// records take precedence and are validated as one fixed-country 1024Proxy
// credential set. When no dynamic record is configured, the regular active IP
// proxy pool is used as a bounded fallback. Account bindings and user requests
// keep their configured route. Never log returned URLs.
func (s *OpenAIGatewayService) codexTurnStatePoolRoutes(ctx context.Context, primary string) []string {
	if s.proxyRepo == nil {
		return nil
	}
	proxies, err := s.proxyRepo.ListActive(ctx)
	if err != nil {
		return nil
	}
	now := time.Now()
	templates := make([]codexTurnStateProbeProxyTemplate, 0, len(proxies))
	dynamicConfigured := false
	for _, proxy := range proxies {
		if !codexTurnStateDedicatedProxy(proxy) || !proxy.IsActive() || proxy.IsExpired(now) {
			continue
		}
		dynamicConfigured = true
		if !codexTurnStateProxyUsable(proxy, now) {
			// An active, explicitly dedicated record is configuration, even when
			// its protocol/host fields are malformed. Do not silently borrow a
			// different identity in that case.
			return nil
		}
		template, err := codexTurnStateProbeProxyTemplateFromRecord(proxy)
		if err != nil {
			// A malformed explicitly dedicated record is a configuration error.
			// Fail closed instead of silently using another identity.
			return nil
		}
		templates = append(templates, template)
	}
	if dynamicConfigured {
		// A maintenance run gets at most one attempt per fresh route. Repeating a
		// route across rounds can turn a bounded probe into an IP-rotation loop and
		// can evade an upstream 429 boundary. A malformed/mixed dynamic set stays
		// fail-closed rather than silently switching identity pools.
		return buildCodexTurnStateProbeRoutes(templates, primary, cryptorand.Reader)
	}
	return codexTurnStateIPPoolRoutes(proxies, primary, now)
}
