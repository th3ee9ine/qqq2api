package service

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestOpenAIPaidCreditsUnmarshalFlexible(t *testing.T) {
	var c OpenAIPaidCredits
	if err := json.Unmarshal([]byte(`{"hasCredits":true,"unlimited":false,"balance":12.50,"overage_limit_reached":false}`), &c); err != nil {
		t.Fatal(err)
	}
	if !c.HasCredits || c.Balance != "12.5" {
		t.Fatalf("decoded: %#v", c)
	}
	var d OpenAIPaidCredits
	if err := json.Unmarshal([]byte(`{"has_credits":false,"balance":"-1"}`), &d); err != nil || d.HasCredits || d.Balance != "-1" {
		t.Fatalf("decoded: %#v err=%v", d, err)
	}
}

func TestOpenAIPaidCreditsSnapshotActiveBoundaries(t *testing.T) {
	now := time.Now()
	mk := func(v map[string]any) map[string]any {
		v["fetched_at"] = now.Unix()
		return map[string]any{openaiQuotaPaidCreditsKey: v}
	}
	cases := []struct {
		name  string
		extra map[string]any
		want  bool
	}{
		{"positive", mk(map[string]any{"has_credits": true, "balance": "1.2"}), true},
		{"has false", mk(map[string]any{"has_credits": false, "balance": "2"}), false},
		{"overage", mk(map[string]any{"has_credits": true, "balance": "2", "overage_limit_reached": true}), false},
		{"zero", mk(map[string]any{"has_credits": true, "balance": "0"}), false},
		{"nan", mk(map[string]any{"has_credits": true, "balance": math.NaN()}), false},
		{"future", map[string]any{openaiQuotaPaidCreditsKey: map[string]any{"has_credits": true, "balance": "2", "fetched_at": now.Add(time.Minute).Unix()}}, false},
		{"missing timestamp", map[string]any{openaiQuotaPaidCreditsKey: map[string]any{"has_credits": true, "balance": "2"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := openAIPaidCreditsSnapshotActive(tc.extra, now); got != tc.want {
				t.Fatalf("got %v", got)
			}
		})
	}
}

func TestBuildOpenAIPaidCreditsExtraUpdatesNilTombstone(t *testing.T) {
	got := buildOpenAIPaidCreditsExtraUpdates(nil, 123)
	snap := got[openaiQuotaPaidCreditsKey].(map[string]any)
	if snap["fetched_at"] != int64(123) {
		t.Fatalf("snapshot=%#v", snap)
	}
	if snap["has_credits"] != false {
		t.Fatalf("snapshot=%#v", snap)
	}
}
