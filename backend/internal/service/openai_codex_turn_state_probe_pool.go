package service

import (
	"context"
	"math/rand/v2"
	"time"
)

const (
	codexTurnStateProbeAttemptTimeout = 12 * time.Second
	codexTurnStateProbeTotalTimeout   = 90 * time.Second
	codexTurnStateProbePoolRounds     = 3
	codexTurnStateProbePoolSize       = 3
)

func codexTurnStateAccountProxy(account *Account) string {
	if account != nil && account.ProxyID != nil && account.Proxy != nil {
		return account.Proxy.URL()
	}
	return ""
}

func codexTurnStateFresh292(state string, now time.Time) bool {
	issued, blocks, ok := parseCodexTurnState(state)
	return ok && blocks == 10 && !issued.After(now.Add(time.Minute)) && now.Before(issued.Add(codexTurnStateTTL))
}

func codexTurnStateProbeRetryable(err error) bool {
	if err == nil {
		return false
	}
	switch err.Error() {
	case "auth_failed", "account_unavailable", "http_400", "http_401", "http_402", "http_403", "http_404", "http_422", "http_429":
		return false
	default:
		return true
	}
}

// Pool routes are used only for maintenance probes. Account proxy bindings and
// user requests keep their configured route. Never log URLs containing credentials.
func (s *OpenAIGatewayService) codexTurnStatePoolRoutes(ctx context.Context, primary string) []string {
	if s.proxyRepo == nil {
		return nil
	}
	proxies, err := s.proxyRepo.ListActive(ctx)
	if err != nil {
		return nil
	}
	seen := map[string]bool{primary: true}
	urls := make([]string, 0, len(proxies))
	now := time.Now()
	for _, proxy := range proxies {
		if !proxy.IsActive() || proxy.IsExpired(now) || proxy.Host == "" || proxy.Port <= 0 {
			continue
		}
		switch proxy.Protocol {
		case "http", "https", "socks5", "socks5h":
		default:
			continue
		}
		url := proxy.URL()
		if !seen[url] {
			seen[url] = true
			urls = append(urls, url)
		}
	}
	// Spread concurrent accounts over the pool instead of exhausting its first IP.
	rand.Shuffle(len(urls), func(i, j int) { urls[i], urls[j] = urls[j], urls[i] })
	if len(urls) > codexTurnStateProbePoolSize {
		urls = urls[:codexTurnStateProbePoolSize]
	}
	routes := make([]string, 0, len(urls)*codexTurnStateProbePoolRounds)
	for range codexTurnStateProbePoolRounds {
		routes = append(routes, urls...)
	}
	return routes
}
