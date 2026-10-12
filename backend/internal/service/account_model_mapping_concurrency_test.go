//go:build unit

package service

import (
	"fmt"
	"sync"
	"testing"
)

func TestAccountGetModelMappingConcurrentReadsAndValueCopies(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Credentials: map[string]any{
		"model_mapping": map[string]any{"gpt-*": "gpt-5.5", "alias": "gpt-5.4"},
	}}
	const callers = 32
	start := make(chan struct{})
	var done sync.WaitGroup
	done.Add(callers)
	for i := range callers {
		go func() {
			defer done.Done()
			<-start
			for range 100 {
				shared := account.GetModelMapping()
				if shared["gpt-*"] != "gpt-5.5" {
					t.Error("shared account mapping changed")
				}
				copy := *account
				mapping := copy.GetModelMapping()
				if mapping["alias"] != "gpt-5.4" {
					t.Errorf("copy %d mapping changed", i)
				}
			}
		}()
	}
	close(start)
	done.Wait()
}

func TestAccountGetModelMappingReturnsIndependentMapping(t *testing.T) {
	account := &Account{Credentials: map[string]any{
		"model_mapping": map[string]any{"alias": "original"},
	}}
	first := account.GetModelMapping()
	first["alias"] = "caller-local"
	if got := account.GetModelMapping()["alias"]; got != "original" {
		t.Fatalf("caller mutation leaked into the account mapping: %s", got)
	}
}

func BenchmarkAccountGetModelMapping(b *testing.B) {
	for _, size := range []int{0, 4, 32} {
		b.Run(fmt.Sprintf("entries_%d", size), func(b *testing.B) {
			raw := make(map[string]any, size)
			for i := range size {
				raw[fmt.Sprintf("alias-%d", i)] = fmt.Sprintf("model-%d", i)
			}
			account := &Account{Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": raw}}
			account.GetModelMapping()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				mapping := account.GetModelMapping()
				if len(mapping) != size {
					b.Fatal("unexpected mapping size")
				}
			}
		})
	}
}
