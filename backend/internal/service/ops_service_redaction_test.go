package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIsSensitiveKey_TokenBudgetKeysNotRedacted(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		"max_tokens",
		"max_output_tokens",
		"max_input_tokens",
		"max_completion_tokens",
		"max_tokens_to_sample",
		"budget_tokens",
		"prompt_tokens",
		"completion_tokens",
		"input_tokens",
		"output_tokens",
		"total_tokens",
		"token_count",
	} {
		if isSensitiveKey(key) {
			t.Fatalf("expected key %q to NOT be treated as sensitive", key)
		}
	}

	for _, key := range []string{
		"authorization",
		"Authorization",
		"access_token",
		"refresh_token",
		"id_token",
		"session_token",
		"token",
		"client_secret",
		"private_key",
		"signature",
	} {
		if !isSensitiveKey(key) {
			t.Fatalf("expected key %q to be treated as sensitive", key)
		}
	}
}

func TestSanitizeAndTrimJSONPayload_PreservesTokenBudgetFields(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"model":"claude-3","max_tokens":123,"thinking":{"type":"enabled","budget_tokens":456},"access_token":"abc","messages":[{"role":"user","content":"hi"}]}`)
	out, _, _ := sanitizeAndTrimJSONPayload(raw, 10*1024)
	if out == "" {
		t.Fatalf("expected non-empty sanitized output")
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("unmarshal sanitized output: %v", err)
	}

	if got, ok := decoded["max_tokens"].(float64); !ok || got != 123 {
		t.Fatalf("expected max_tokens=123, got %#v", decoded["max_tokens"])
	}

	thinking, ok := decoded["thinking"].(map[string]any)
	if !ok || thinking == nil {
		t.Fatalf("expected thinking object to be preserved, got %#v", decoded["thinking"])
	}
	if got, ok := thinking["budget_tokens"].(float64); !ok || got != 456 {
		t.Fatalf("expected thinking.budget_tokens=456, got %#v", thinking["budget_tokens"])
	}

	if got := decoded["access_token"]; got != "[REDACTED]" {
		t.Fatalf("expected access_token to be redacted, got %#v", got)
	}
}

func TestShrinkToEssentials_IncludesThinking(t *testing.T) {
	t.Parallel()

	root := map[string]any{
		"model":      "claude-3",
		"max_tokens": 100,
		"thinking": map[string]any{
			"type":          "enabled",
			"budget_tokens": 200,
		},
		"messages": []any{
			map[string]any{"role": "user", "content": "first"},
			map[string]any{"role": "user", "content": "last"},
		},
	}

	out := shrinkToEssentials(root)
	if _, ok := out["thinking"]; !ok {
		t.Fatalf("expected thinking to be included in essentials: %#v", out)
	}
}

func TestIsSensitiveOpsFieldNameNormalizesCommonSeparators(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		"api-key",
		"apiKey",
		"privateKey",
		"x-api-key",
		"authorization",
		"client.secret",
	} {
		if !IsSensitiveOpsFieldName(key) {
			t.Fatalf("expected field %q to be classified as sensitive", key)
		}
	}
	for _, key := range []string{"max-output-tokens", "budget_tokens", "prompt_tokens", "model"} {
		if IsSensitiveOpsFieldName(key) {
			t.Fatalf("expected diagnostic field %q to remain visible", key)
		}
	}
}

func TestSanitizeOpsRequestDetailsForQueuePreservesNestedSecrets(t *testing.T) {
	t.Parallel()

	raw := `{"method":"POST","path":"/v1/responses","body":{"model":"gpt-test","prompt":"keep me","apiKey":"body-secret","nested":{"privateKey":"pem-secret"},"max_output_tokens":256},"query":{"trace":"fixture"}}`
	out, changed := SanitizeOpsRequestDetailsForQueue(raw)
	if changed {
		t.Fatal("a bounded request snapshot should be returned unchanged")
	}
	if len(out) > OpsErrorLogRequestDetailsQueueMaxBytes {
		t.Fatalf("sanitized details exceed queue bound: %d", len(out))
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("sanitized details are not valid JSON: %v", err)
	}
	body, ok := decoded["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected body object, got %#v", decoded["body"])
	}
	if body["apiKey"] != "body-secret" {
		t.Fatalf("apiKey was not preserved: %#v", body["apiKey"])
	}
	nested, ok := body["nested"].(map[string]any)
	if !ok || nested["privateKey"] != "pem-secret" {
		t.Fatalf("privateKey was not preserved: %#v", body["nested"])
	}
	if body["prompt"] != "keep me" {
		t.Fatalf("non-sensitive prompt was not preserved: %#v", body["prompt"])
	}
	if body["max_output_tokens"] != float64(256) {
		t.Fatalf("token budget was unexpectedly redacted: %#v", body["max_output_tokens"])
	}
}

func TestSanitizeOpsRequestDetailsInvalidJSONUsesValidMarker(t *testing.T) {
	t.Parallel()

	out, changed := SanitizeOpsRequestDetailsForQueue(`{"method":`)
	if !changed {
		t.Fatal("invalid JSON should be reported as changed")
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("omission marker is not valid JSON: %q", out)
	}
	var marker map[string]any
	if err := json.Unmarshal([]byte(out), &marker); err != nil {
		t.Fatal(err)
	}
	if marker["payload_omitted"] != true || marker["reason"] != "invalid_json" {
		t.Fatalf("unexpected omission marker: %#v", marker)
	}
}

func TestSanitizeOpsRequestDetailsCompactsOversizedBody(t *testing.T) {
	t.Parallel()

	message := strings.Repeat("message-", 400)
	raw := `{"method":"POST","path":"/v1/responses","body":{"model":"gpt-test","messages":[{"role":"user","content":"` + message + `"},{"role":"assistant","content":"last"}]}}`
	out, changed := sanitizeOpsRequestDetails(raw, 1024)
	if !changed {
		t.Fatal("oversized details should be compacted")
	}
	if len(out) > 1024 {
		t.Fatalf("compacted details exceed requested bound: %d", len(out))
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("compacted details are not valid JSON: %q", out)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["method"] != "POST" || decoded["path"] != "/v1/responses" {
		t.Fatalf("route metadata was lost during compaction: %#v", decoded)
	}
}

func TestSanitizeOpsRequestDetailsInputLimit(t *testing.T) {
	t.Parallel()

	raw := `{"body":"` + strings.Repeat("x", opsMaxRequestDetailsInputBytes) + `"}`
	out, changed := SanitizeOpsRequestDetailsForQueue(raw)
	if !changed {
		t.Fatal("input over the defensive ceiling should be marked changed")
	}
	if len(out) > OpsErrorLogRequestDetailsQueueMaxBytes {
		t.Fatalf("input-limit marker exceeds queue bound: %d", len(out))
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("input-limit marker is not valid JSON: %q", out)
	}
}
