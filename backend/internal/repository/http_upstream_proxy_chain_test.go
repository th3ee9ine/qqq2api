package repository

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

func TestBuildUpstreamTransportUsesConfiguredProxyChain(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.ProxyChain.PreProxyURL = "socks5h://host.docker.internal:7890"
	cfg.Gateway.ProxyChain.ForceHTTPProxy = true
	target, err := url.Parse("socks5h://target-user:target-pass@us.1024proxy.io:3000")
	require.NoError(t, err)

	transport, err := buildUpstreamTransport(defaultPoolSettings(cfg), target, upstreamProtocolModeDefault)
	require.NoError(t, err)
	t.Cleanup(transport.CloseIdleConnections)
	require.NotNil(t, transport.DialContext)
	require.NotNil(t, transport.Proxy)

	configured, err := transport.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "chatgpt.com"}})
	require.NoError(t, err)
	require.Equal(t, "http", configured.Scheme)
	require.Equal(t, target.Host, configured.Host)
	require.Equal(t, target.User.String(), configured.User.String())
}

func TestBuildUpstreamTransportProxyChainDoesNotAffectDirectRoute(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.ProxyChain.PreProxyURL = "socks5h://host.docker.internal:7890"
	cfg.Gateway.ProxyChain.ForceHTTPProxy = true

	transport, err := buildUpstreamTransport(defaultPoolSettings(cfg), nil, upstreamProtocolModeDefault)
	require.NoError(t, err)
	t.Cleanup(transport.CloseIdleConnections)
	require.Nil(t, transport.Proxy)
}

func TestBuildPoolKeyDoesNotExposeProxyChainCredentials(t *testing.T) {
	settings := defaultPoolSettings(nil)
	settings.proxyChainPreProxyURL = "socks5h://private-user:private-password@127.0.0.1:7890"

	key := buildPoolKey(settings, upstreamProtocolModeDefault)

	require.Contains(t, key, "proxy_chain:")
	require.False(t, strings.Contains(key, "private-user"))
	require.False(t, strings.Contains(key, "private-password"))
}
