package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

const (
	openAICodexTurnStateProxyPoolMaxEntries = 256
	openAICodexTurnStateProxyURLMaxBytes    = 2048
)

var errInvalidOpenAICodexTurnStateProxyURL = errors.New("invalid Codex Turn-State proxy URL")

// ErrInvalidOpenAICodexTurnStateProxyPool is returned when an administrator
// submits a malformed dedicated collector proxy pool. The error deliberately
// contains no URL, hostname, username, or password so it is safe to expose as
// a validation result at the HTTP boundary.
var ErrInvalidOpenAICodexTurnStateProxyPool = errors.New("invalid Codex Turn-State proxy pool")

// OpenAICodexTurnStateProxySummary is the credential-free representation used
// by the reliability projection. The actual URL, including credentials, stays
// inside the gateway runtime snapshot and is never serialized here.
type OpenAICodexTurnStateProxySummary struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

func normalizeOpenAICodexTurnStateProxyPool(values []string) ([]string, error) {
	if len(values) > openAICodexTurnStateProxyPoolMaxEntries {
		return nil, errors.New("proxy_pool_urls must contain at most 256 entries")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for index, raw := range values {
		canonical, err := normalizeOpenAICodexTurnStateProxyURL(raw)
		if err != nil {
			return nil, errors.New("proxy_pool_urls[" + strconv.Itoa(index) + "] is invalid")
		}
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	return result, nil
}

// NormalizeOpenAICodexTurnStateProxyPool validates, canonicalizes, and stably
// de-duplicates administrator supplied proxy URLs. It is exported for the
// admin handler so malformed input can be rejected with HTTP 400 before a
// settings write is attempted.
func NormalizeOpenAICodexTurnStateProxyPool(values []string) ([]string, error) {
	normalized, err := normalizeOpenAICodexTurnStateProxyPool(values)
	if err != nil {
		return nil, ErrInvalidOpenAICodexTurnStateProxyPool
	}
	return normalized, nil
}

func normalizeOpenAICodexTurnStateProxyURL(raw string) (string, error) {
	if raw == "" || len(raw) > openAICodexTurnStateProxyURLMaxBytes || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\r\n\x00") {
		return "", errInvalidOpenAICodexTurnStateProxyURL
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" || parsed.Host == "" || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" {
		return "", errInvalidOpenAICodexTurnStateProxyURL
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" && scheme != "socks5" {
		return "", errInvalidOpenAICodexTurnStateProxyURL
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" || strings.IndexFunc(host, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return "", errInvalidOpenAICodexTurnStateProxyURL
	}
	portText := parsed.Port()
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", errInvalidOpenAICodexTurnStateProxyURL
	}
	if parsed.User != nil {
		username := parsed.User.Username()
		password, passwordSet := parsed.User.Password()
		if username == "" || !passwordSet || password == "" || hasProxyControl(username) || hasProxyControl(password) {
			return "", errInvalidOpenAICodexTurnStateProxyURL
		}
	}
	canonical := &url.URL{Scheme: scheme, Host: net.JoinHostPort(strings.ToLower(host), strconv.Itoa(port)), User: parsed.User}
	return canonical.String(), nil
}

func hasProxyControl(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0
}

func parseOpenAICodexTurnStateProxyPool(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil || values == nil {
		return nil, fmt.Errorf("%w: proxy_pool must be a JSON array", ErrInvalidOpenAICodexTurnStateProxyPool)
	}
	normalized, err := normalizeOpenAICodexTurnStateProxyPool(values)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidOpenAICodexTurnStateProxyPool, err)
	}
	return normalized, nil
}

func marshalOpenAICodexTurnStateProxyPool(values []string) (string, error) {
	normalized, err := normalizeOpenAICodexTurnStateProxyPool(values)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func summarizeOpenAICodexTurnStateProxyURL(raw string) OpenAICodexTurnStateProxySummary {
	parsed, err := url.Parse(raw)
	if err != nil {
		return OpenAICodexTurnStateProxySummary{}
	}
	port, _ := strconv.Atoi(parsed.Port())
	return OpenAICodexTurnStateProxySummary{
		Protocol: strings.ToLower(parsed.Scheme),
		Host:     strings.ToLower(parsed.Hostname()),
		Port:     port,
	}
}

func summarizeOpenAICodexTurnStateProxyPool(values []string) []OpenAICodexTurnStateProxySummary {
	if len(values) == 0 {
		return nil
	}
	result := make([]OpenAICodexTurnStateProxySummary, 0, len(values))
	for _, value := range values {
		entry := summarizeOpenAICodexTurnStateProxyURL(value)
		if entry.Protocol == "" || entry.Host == "" || entry.Port <= 0 {
			continue
		}
		result = append(result, entry)
	}
	return result
}
