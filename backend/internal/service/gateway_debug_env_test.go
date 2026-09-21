package service

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestParseDebugEnvBool(t *testing.T) {
	t.Run("empty is false", func(t *testing.T) {
		if parseDebugEnvBool("") {
			t.Fatalf("expected false for empty string")
		}
	})

	t.Run("true-like values", func(t *testing.T) {
		for _, value := range []string{"1", "true", "TRUE", "yes", "on"} {
			t.Run(value, func(t *testing.T) {
				if !parseDebugEnvBool(value) {
					t.Fatalf("expected true for %q", value)
				}
			})
		}
	})

	t.Run("false-like values", func(t *testing.T) {
		for _, value := range []string{"0", "false", "off", "debug"} {
			t.Run(value, func(t *testing.T) {
				if parseDebugEnvBool(value) {
					t.Fatalf("expected false for %q", value)
				}
			})
		}
	})
}

func TestDebugLogGatewaySnapshotRedactsCodexTurnState(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "gateway-debug-*.log")
	if err != nil {
		t.Fatalf("create debug log: %v", err)
	}
	defer func() { _ = f.Close() }()

	const secret = "opaque-turn-state-secret-marker"
	svc := &GatewayService{}
	svc.debugGatewayBodyFile.Store(f)
	defer svc.debugGatewayBodyFile.Store(nil)

	headers := make(http.Header)
	headers.Set("X-Codex-Turn-State", secret)
	svc.debugLogGatewaySnapshot("CLIENT_ORIGINAL", headers, []byte(`{"model":"gpt-5"}`), nil)
	if err := f.Sync(); err != nil {
		t.Fatalf("sync debug log: %v", err)
	}
	content, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("read debug log: %v", err)
	}
	text := string(content)
	if strings.Contains(text, secret) {
		t.Fatalf("debug log leaked raw turn state: %s", text)
	}
	if !strings.Contains(text, "X-Codex-Turn-State: [redacted]") {
		t.Fatalf("debug log did not retain the redacted header marker: %s", text)
	}
}
