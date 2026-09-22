package service

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeOpenAICodexTurnStateProxyPoolAcceptsSupportedFormats(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "http credentials",
			in:   []string{"http://user:pass@proxy.example:8080"},
			want: []string{"http://user:pass@proxy.example:8080"},
		},
		{
			name: "https ipv6",
			in:   []string{"https://[::1]:8443"},
			want: []string{"https://[::1]:8443"},
		},
		{
			name: "socks5 credentials and ipv6",
			in:   []string{"socks5://user:pass@[2001:DB8::1]:1080"},
			want: []string{"socks5://user:pass@[2001:db8::1]:1080"},
		},
		{
			name: "scheme and host are canonicalized",
			in:   []string{"HTTP://PROXY.EXAMPLE:80"},
			want: []string{"http://proxy.example:80"},
		},
		{
			name: "duplicates are removed stably",
			in: []string{
				"http://PROXY.EXAMPLE:8080",
				"http://proxy.example:8080",
				"https://proxy.example:8443",
				"http://proxy.example:8080",
			},
			want: []string{
				"http://proxy.example:8080",
				"https://proxy.example:8443",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeOpenAICodexTurnStateProxyPool(tt.in)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeOpenAICodexTurnStateProxyPoolRejectsMalformedURLs(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "unsupported scheme", value: "socks4://proxy.example:1080"},
		{name: "missing scheme", value: "proxy.example:8080"},
		{name: "missing port", value: "http://proxy.example"},
		{name: "non numeric port", value: "http://proxy.example:not-a-port"},
		{name: "zero port", value: "http://proxy.example:0"},
		{name: "port above range", value: "http://proxy.example:65536"},
		{name: "path", value: "http://proxy.example:8080/path"},
		{name: "query", value: "http://proxy.example:8080?token=secret"},
		{name: "fragment", value: "http://proxy.example:8080#fragment"},
		{name: "empty username", value: "http://:pass@proxy.example:8080"},
		{name: "missing password", value: "http://user@proxy.example:8080"},
		{name: "empty password", value: "http://user:@proxy.example:8080"},
		{name: "leading whitespace", value: " http://proxy.example:8080"},
		{name: "trailing whitespace", value: "http://proxy.example:8080 "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeOpenAICodexTurnStateProxyPool([]string{tt.value})
			require.ErrorIs(t, err, ErrInvalidOpenAICodexTurnStateProxyPool)
		})
	}
}

func TestNormalizeOpenAICodexTurnStateProxyPoolEnforcesEntryLimit(t *testing.T) {
	pool := make([]string, openAICodexTurnStateProxyPoolMaxEntries+1)
	for index := range pool {
		pool[index] = fmt.Sprintf("http://proxy-%d.example:8080", index)
	}

	_, err := NormalizeOpenAICodexTurnStateProxyPool(pool)
	require.ErrorIs(t, err, ErrInvalidOpenAICodexTurnStateProxyPool)

	pool = pool[:openAICodexTurnStateProxyPoolMaxEntries]
	normalized, err := NormalizeOpenAICodexTurnStateProxyPool(pool)
	require.NoError(t, err)
	require.Len(t, normalized, openAICodexTurnStateProxyPoolMaxEntries)
}
