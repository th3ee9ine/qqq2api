package service

import "regexp"

var sensitiveQueryParamRegex = regexp.MustCompile(`(?i)([?&](?:key|client_secret|access_token|refresh_token)=)[^&"\s]+`)

func sanitizeUpstreamErrorMessage(msg string) string {
	if msg == "" {
		return msg
	}
	return sensitiveQueryParamRegex.ReplaceAllString(msg, `$1***`)
}
