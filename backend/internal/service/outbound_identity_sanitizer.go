package service

import (
	"net/http"
	"strings"
)

// internalGatewayBrandTokens identify accidental product metadata. They are
// not used to rewrite or imitate any provider identity.
var internalGatewayBrandTokens = [...]string{"sub2api", "qqq2api"}

// outboundIdentityHeaders are metadata fields whose values identify the
// calling application. Credential and provider-specific headers remain opaque.
var outboundIdentityHeaders = map[string]struct{}{
	"user-agent":         {},
	"client-info":        {},
	"originator":         {},
	"origin":             {},
	"referer":            {},
	"http-referer":       {},
	"via":                {},
	"x-app":              {},
	"x-application":      {},
	"x-client":           {},
	"x-client-name":      {},
	"x-client-info":      {},
	"x-gateway":          {},
	"x-goog-api-client":  {},
	"x-openrouter-title": {},
	"x-origin":           {},
	"x-powered-by":       {},
	"x-product":          {},
	"x-requested-with":   {},
	"x-source":           {},
	"x-service":          {},
	"x-title":            {},
	"x-user-agent":       {},
}

func containsInternalGatewayBrand(value string) bool {
	value = strings.ToLower(value)
	for _, token := range internalGatewayBrandTokens {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}

func isOutboundIdentityHeader(name string) bool {
	if _, ok := outboundIdentityHeaders[name]; ok {
		return true
	}
	for _, prefix := range []string{"x-app-", "x-application-", "x-client-", "x-stainless-"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// SanitizeOutboundGatewayIdentity strips accidental product-identifying HTTP
// metadata. It runs after account-level header overrides so custom settings
// cannot reintroduce an internal marker on the wire.
func SanitizeOutboundGatewayIdentity(headers http.Header) {
	if headers == nil {
		return
	}
	for name, values := range headers {
		normalizedName := strings.ToLower(strings.TrimSpace(name))
		if containsInternalGatewayBrand(normalizedName) {
			delete(headers, name)
			continue
		}
		if !isOutboundIdentityHeader(normalizedName) {
			continue
		}

		cleanValues := values[:0]
		for _, value := range values {
			if !containsInternalGatewayBrand(value) {
				cleanValues = append(cleanValues, value)
			}
		}
		if len(cleanValues) == 0 {
			delete(headers, name)
			continue
		}
		headers[name] = cleanValues
	}
}
