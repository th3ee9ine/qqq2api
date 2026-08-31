package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMapUserErrorCategory(t *testing.T) {
	cases := []struct {
		phase, etype, want string
	}{
		{"auth", "authentication_error", "auth"},
		{"request", "rate_limit_error", "rate_limit"},
		{"request", "billing_error", "quota"},
		{"request", "subscription_error", "quota"},
		{"request", "invalid_request_error", "invalid_request"},
		{"routing", "api_error", "service_unavailable"},
		{"account_auth", "upstream_error", "upstream"},
		{"upstream", "upstream_error", "upstream"},
		{"network", "api_error", "upstream"},
		{"internal", "api_error", "internal"},
		{"weird", "weird", "other"},
	}
	for _, c := range cases {
		if got := MapUserErrorCategory(c.phase, c.etype); got != c.want {
			t.Errorf("MapUserErrorCategory(%q,%q)=%q want %q", c.phase, c.etype, got, c.want)
		}
	}
}

func TestCategoryToFilter(t *testing.T) {
	phases, types := CategoryToFilter("rate_limit")
	if len(types) != 1 || types[0] != "rate_limit_error" || len(phases) != 0 {
		t.Fatalf("rate_limit => phases=%v types=%v", phases, types)
	}
	phases, types = CategoryToFilter("auth")
	if len(phases) != 1 || phases[0] != "auth" || len(types) != 0 {
		t.Fatalf("auth => phases=%v types=%v", phases, types)
	}
	phases, types = CategoryToFilter("service_unavailable")
	if len(phases) != 1 || phases[0] != "routing" || len(types) != 0 {
		t.Fatalf("service_unavailable => phases=%v types=%v", phases, types)
	}
	phases, types = CategoryToFilter("upstream")
	if len(phases) != 3 || phases[0] != "account_auth" || phases[1] != "upstream" || phases[2] != "network" || len(types) != 0 {
		t.Fatalf("upstream => phases=%v types=%v", phases, types)
	}
	phases, types = CategoryToFilter("internal")
	if len(phases) != 1 || phases[0] != "internal" || len(types) != 0 {
		t.Fatalf("internal => phases=%v types=%v", phases, types)
	}
	phases, types = CategoryToFilter("quota")
	if len(types) != 2 || types[0] != "billing_error" || types[1] != "subscription_error" || len(phases) != 0 {
		t.Fatalf("quota => phases=%v types=%v", phases, types)
	}
	phases, types = CategoryToFilter("invalid_request")
	if len(types) != 1 || types[0] != "invalid_request_error" || len(phases) != 0 {
		t.Fatalf("invalid_request => phases=%v types=%v", phases, types)
	}
	phases, types = CategoryToFilter("other")
	if len(phases) != 0 || len(types) != 0 {
		t.Fatalf("other => phases=%v types=%v", phases, types)
	}
}

func TestToUserErrorRequest_RedactsSensitiveFields(t *testing.T) {
	src := &OpsErrorLog{
		ID:              123,
		CreatedAt:       time.Unix(0, 0).UTC(),
		Model:           "m",
		RequestedModel:  "rm",
		InboundEndpoint: "/v1/chat/completions",
		StatusCode:      429,
		Platform:        "openai",
		Phase:           "request",
		Type:            "rate_limit_error",
		Message:         "rate limit exceeded",
		APIKeyName:      "my-key",
		APIKeyDeleted:   true,
	}
	out := ToUserErrorRequest(src)
	if out.ID != 123 {
		t.Errorf("want ID=123, got %d", out.ID)
	}
	if out.Model != "rm" {
		t.Errorf("want requested_model preferred, got %q", out.Model)
	}
	if out.Category != "rate_limit" {
		t.Errorf("category=%q", out.Category)
	}
	if out.StatusCode != 429 || out.InboundEndpoint != "/v1/chat/completions" || out.Platform != "openai" {
		t.Errorf("basic fields wrong: %+v", out)
	}
	if out.Message != "rate limit exceeded" {
		t.Errorf("want message=%q, got %q", "rate limit exceeded", out.Message)
	}
	if out.KeyName != "my-key" {
		t.Errorf("want key_name=my-key, got %q", out.KeyName)
	}
	if !out.KeyDeleted {
		t.Error("want key_deleted=true")
	}
}

func TestToUserErrorRequestDetail_WhitelistAndRedacts(t *testing.T) {
	uid := int64(42)
	upstreamStatus := 503
	src := &OpsErrorLogDetail{
		OpsErrorLog: OpsErrorLog{
			ID:               999,
			CreatedAt:        time.Unix(1000, 0).UTC(),
			Model:            "gpt-4",
			RequestedModel:   "gpt-4-turbo",
			InboundEndpoint:  "/v1/chat/completions",
			StatusCode:       502,
			Platform:         "openai",
			Phase:            "upstream",
			Type:             "api_error",
			Message:          "upstream error",
			UserID:           &uid,
			ClientIP:         func() *string { s := "1.2.3.4"; return &s }(),
			UpstreamEndpoint: "https://api.openai.com/v1/chat/completions",
			UserAgent:        "codex_cli_rs/0.125.0",
			GroupName:        "grp-a",
			Stream:           true,
		},
		ErrorBody:          `{"error":{"message":"upstream failed","type":"server_error"}}`,
		RequestDetails:     `{"method":"POST","path":"/v1/chat/completions","body":{"model":"gpt-4-turbo","messages":[{"role":"user","content":"hello"}]}}`,
		UpstreamStatusCode: &upstreamStatus,
	}

	out := ToUserErrorRequestDetail(src)
	if out == nil {
		t.Fatal("expected non-nil detail")
	}

	// 基础字段正确映射
	if out.ID != 999 {
		t.Errorf("want ID=999, got %d", out.ID)
	}
	if out.Message != "upstream error" {
		t.Errorf("want message=%q, got %q", "upstream error", out.Message)
	}
	if out.ErrorBody != src.ErrorBody {
		t.Errorf("ErrorBody mismatch")
	}
	if out.RequestDetails != src.RequestDetails {
		t.Errorf("RequestDetails formatting/value mismatch: want %q, got %q", src.RequestDetails, out.RequestDetails)
	}
	if out.UpstreamStatusCode == nil || *out.UpstreamStatusCode != 503 {
		t.Errorf("UpstreamStatusCode mismatch")
	}

	// client_ip / user_agent / group_name / stream 经产品决策开放（与用量明细口径对齐）
	if out.ClientIP != "1.2.3.4" {
		t.Errorf("want client_ip=1.2.3.4, got %q", out.ClientIP)
	}
	if out.UserAgent != "codex_cli_rs/0.125.0" {
		t.Errorf("want user_agent=codex_cli_rs/0.125.0, got %q", out.UserAgent)
	}
	if out.GroupName != "grp-a" {
		t.Errorf("want group_name=grp-a, got %q", out.GroupName)
	}
	if !out.Stream {
		t.Errorf("want stream=true")
	}

	// 序列化后不含敏感字段
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	raw := string(b)
	for _, forbidden := range []string{"user_email", "upstream_endpoint"} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("sensitive field %q leaked in JSON output: %s", forbidden, raw)
		}
	}
}

func TestToUserErrorRequestDetail_Nil(t *testing.T) {
	if out := ToUserErrorRequestDetail(nil); out != nil {
		t.Errorf("expected nil for nil input, got %+v", out)
	}
}

func TestToUserErrorRequestDetail_DefensivelySanitizesRequestDetails(t *testing.T) {
	uid := int64(7)
	src := &OpsErrorLogDetail{
		OpsErrorLog:    OpsErrorLog{UserID: &uid},
		RequestDetails: `{"method":"POST","path":"/v1/responses","headers":{"authorization":["Bearer secret"]},"body":{"prompt":"hello","api_key":"secret"}}`,
	}
	out := ToUserErrorRequestDetail(src)
	if out == nil || out.RequestDetails == "" {
		t.Fatal("expected request details")
	}
	if strings.Contains(out.RequestDetails, "secret") {
		t.Fatalf("request details leaked sensitive value: %s", out.RequestDetails)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out.RequestDetails), &decoded); err != nil {
		t.Fatalf("request details are not valid JSON: %v", err)
	}
	headers, ok := decoded["headers"].(map[string]any)
	if !ok {
		t.Fatalf("expected headers object, got %#v", decoded["headers"])
	}
	// The recursive redactor replaces a sensitive value with a scalar marker,
	// even when the captured header representation is an array.
	if got := headers["authorization"]; got != "[REDACTED]" {
		t.Fatalf("authorization was not redacted: %#v", got)
	}
}
