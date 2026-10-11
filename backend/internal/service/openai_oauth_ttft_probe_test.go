package service

import (
	"errors"
	"testing"
)

func TestValidateOpenAIOAuthTTFTProbeResult(t *testing.T) {
	fast := 14999
	boundary := openAIOAuthTTFTProbeMaxMillis
	slow := openAIOAuthTTFTProbeMaxMillis + 1
	tests := []struct {
		name    string
		result  *OpenAIForwardResult
		wantErr error
	}{
		{name: "nil result", wantErr: errOpenAIOAuthTTFTProbeNoResult},
		{name: "missing first token", result: &OpenAIForwardResult{}, wantErr: errOpenAIOAuthTTFTProbeNoFirstToken},
		{name: "negative first token", result: &OpenAIForwardResult{FirstTokenMs: func() *int { v := -1; return &v }()}, wantErr: errOpenAIOAuthTTFTProbeNoFirstToken},
		{name: "failed websocket terminal", result: &OpenAIForwardResult{OpenAIWSMode: true, UpstreamTerminalEvent: "response.failed", FirstTokenMs: &fast}, wantErr: errOpenAIOAuthTTFTProbeNotTerminal},
		{name: "too slow", result: &OpenAIForwardResult{FirstTokenMs: &slow}, wantErr: errOpenAIOAuthTTFTProbeTooSlow},
		{name: "threshold boundary", result: &OpenAIForwardResult{FirstTokenMs: &boundary}},
		{name: "fast HTTP", result: &OpenAIForwardResult{FirstTokenMs: &fast}},
		{name: "successful websocket terminal", result: &OpenAIForwardResult{OpenAIWSMode: true, UpstreamTerminalEvent: "response.completed", FirstTokenMs: &fast}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOpenAIOAuthTTFTProbeResult(tt.result)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("validate result: unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validate result error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
