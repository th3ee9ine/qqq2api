//go:build unit

package service

import (
	"sync"
	"testing"
)

func TestGetHeaderOverridesConcurrentReadsAndValueCopies(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		credKeyHeaderOverrideEnabled: true,
		credKeyHeaderOverrides: map[string]any{
			"X-App": "configured", "authorization": "blocked", "empty": "",
		},
		"model_mapping": map[string]any{"alias": "gpt-5.5"},
	}}
	const callers = 32
	start := make(chan struct{})
	var done sync.WaitGroup
	done.Add(callers)
	for range callers {
		go func() {
			defer done.Done()
			<-start
			for range 100 {
				shared := account.GetHeaderOverrides()
				if len(shared) != 1 || shared["x-app"] != "configured" {
					t.Error("shared account header override changed")
				}
				shared["x-app"] = "caller-local"
				copy := *account
				overrides := copy.GetHeaderOverrides()
				if len(overrides) != 1 || overrides["x-app"] != "configured" {
					t.Error("copy must retain the configured and filtered headers")
				}
				if account.GetModelMapping()["alias"] != "gpt-5.5" {
					t.Error("concurrent model mapping changed")
				}
			}
		}()
	}
	close(start)
	done.Wait()
	if got := account.Credentials[credKeyHeaderOverrides].(map[string]any)["X-App"]; got != "configured" {
		t.Fatalf("lookup changed source credentials: %v", got)
	}
}
