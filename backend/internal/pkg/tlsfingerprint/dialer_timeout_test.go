//go:build unit

package tlsfingerprint

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"
)

func TestFingerprintDialerBoundsConnectionContext(t *testing.T) {
	wantErr := errors.New("dial stopped")
	var remaining time.Duration
	dialer := NewDialer(nil, func(ctx context.Context, _, _ string) (net.Conn, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			return nil, errors.New("fingerprint dial context has no deadline")
		}
		remaining = time.Until(deadline)
		return nil, wantErr
	})
	_, err := dialer.DialTLSContext(context.Background(), "tcp", "example.com:443")
	if !errors.Is(err, wantErr) {
		t.Fatalf("unexpected dial error: %v", err)
	}
	if remaining <= 0 || remaining > defaultConnectTimeout {
		t.Fatalf("unexpected connection deadline: %v", remaining)
	}
}

// A peer that accepts TCP but never completes TLS, HTTP CONNECT, or SOCKS5
// used to retain a request slot after its context was canceled. These cases
// exercise the real blocking network exchanges without external endpoints.
func TestFingerprintDialerReleasesStalledConnection(t *testing.T) {
	for _, stage := range []string{"tls", "http_connect", "socks5"} {
		for _, cancellation := range []string{"cancel", "deadline"} {
			t.Run(stage+"/"+cancellation, func(t *testing.T) {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = listener.Close() })
				accepted := make(chan net.Conn, 1)
				go func() {
					conn, err := listener.Accept()
					if err == nil {
						accepted <- conn
					}
				}()

				ctx := context.Background()
				var cancel context.CancelFunc
				if cancellation == "deadline" {
					ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				} else {
					ctx, cancel = context.WithCancel(ctx)
				}
				defer cancel()

				done := make(chan error, 1)
				go func() {
					var conn net.Conn
					var err error
					switch stage {
					case "tls":
						conn, err = NewDialer(nil, nil).DialTLSContext(ctx, "tcp", listener.Addr().String())
					case "http_connect":
						proxyURL := &url.URL{Scheme: "http", Host: listener.Addr().String()}
						conn, err = NewHTTPProxyDialer(nil, proxyURL).DialTLSContext(ctx, "tcp", "example.com:443")
					case "socks5":
						proxyURL := &url.URL{Scheme: "socks5", Host: listener.Addr().String()}
						conn, err = NewSOCKS5ProxyDialer(nil, proxyURL).DialTLSContext(ctx, "tcp", "example.com:443")
					}
					if conn != nil {
						_ = conn.Close()
					}
					done <- err
				}()

				select {
				case conn := <-accepted:
					t.Cleanup(func() { _ = conn.Close() })
				case <-time.After(2 * time.Second):
					t.Fatal("dialer did not connect to local listener")
				}
				if cancellation == "cancel" {
					cancel()
				}
				select {
				case err := <-done:
					if err == nil {
						t.Fatal("stalled establishment unexpectedly succeeded")
					}
				case <-time.After(2 * time.Second):
					t.Fatal("cancellation did not release stalled connection")
				}
			})
		}
	}
}
