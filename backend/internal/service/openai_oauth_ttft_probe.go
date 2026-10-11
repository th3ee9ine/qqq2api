package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// The recovery probe is deliberately bounded more tightly than a normal
	// user request.  It is only a health check and must not hold a scheduler
	// recovery worker indefinitely.
	openAIOAuthTTFTProbeTimeout   = 30 * time.Second
	openAIOAuthTTFTProbeMaxMillis = 15_000
)

var (
	errOpenAIOAuthTTFTProbeInvalidAccount = errors.New("openai oauth ttft probe requires a non-shadow oauth account")
	errOpenAIOAuthTTFTProbeNoResult       = errors.New("openai oauth ttft probe returned no result")
	errOpenAIOAuthTTFTProbeNoFirstToken   = errors.New("openai oauth ttft probe returned no first token")
	errOpenAIOAuthTTFTProbeNotTerminal    = errors.New("openai oauth ttft probe did not reach a successful terminal event")
	errOpenAIOAuthTTFTProbeTooSlow        = errors.New("openai oauth ttft probe exceeded the first-token threshold")
)

func validateOpenAIOAuthTTFTProbeResult(result *OpenAIForwardResult) error {
	if result == nil {
		return errOpenAIOAuthTTFTProbeNoResult
	}
	if result.FirstTokenMs == nil || *result.FirstTokenMs < 0 {
		return errOpenAIOAuthTTFTProbeNoFirstToken
	}
	if !result.SucceededForScheduling() {
		return errOpenAIOAuthTTFTProbeNotTerminal
	}
	if *result.FirstTokenMs > openAIOAuthTTFTProbeMaxMillis {
		return errOpenAIOAuthTTFTProbeTooSlow
	}
	return nil
}

// openAIOAuthTTFTProbeResponseWriter deliberately discards the downstream
// response. Forward still needs a complete http.ResponseWriter so the normal
// streaming parser can run, but a recovery probe must never retain response
// bytes in memory or expose them to a client.
type openAIOAuthTTFTProbeResponseWriter struct {
	header http.Header
	status int
	closed chan bool
}

func newOpenAIOAuthTTFTProbeResponseWriter() *openAIOAuthTTFTProbeResponseWriter {
	return &openAIOAuthTTFTProbeResponseWriter{
		header: make(http.Header),
		closed: make(chan bool),
	}
}

func (w *openAIOAuthTTFTProbeResponseWriter) Header() http.Header {
	if w == nil {
		return make(http.Header)
	}
	return w.header
}

func (w *openAIOAuthTTFTProbeResponseWriter) WriteHeader(status int) {
	if w == nil || w.status != 0 {
		return
	}
	w.status = status
}

func (w *openAIOAuthTTFTProbeResponseWriter) Write(p []byte) (int, error) {
	if w == nil {
		return 0, errors.New("nil ttft probe response writer")
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return len(p), nil
}

func (w *openAIOAuthTTFTProbeResponseWriter) Flush() {
	if w != nil {
		w.WriteHeader(http.StatusOK)
	}
}

func (w *openAIOAuthTTFTProbeResponseWriter) CloseNotify() <-chan bool {
	if w == nil {
		return nil
	}
	return w.closed
}

// ProbeOpenAIOAuthTTFT executes the same HTTP Responses forwarding path used
// by a real request, with a minimal streaming prompt and an isolated Gin
// context.  It intentionally calls Forward directly instead of the scheduler:
// the caller uses this method only for an explicit recovery check on an account
// that is currently paused.  No scheduler result or streak observation is
// recorded here.
//
// The returned result is useful to callers for diagnostics.  A nil error means
// the upstream produced a first token within 15 seconds and reached a terminal
// success state; a non-nil error means the probe must not restore the account.
func (s *OpenAIGatewayService) ProbeOpenAIOAuthTTFT(ctx context.Context, account *Account, model string) (*OpenAIForwardResult, error) {
	if account == nil || !account.IsOpenAIOAuthLike() || account.IsShadow() {
		return nil, errOpenAIOAuthTTFTProbeInvalidAccount
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, errors.New("openai oauth ttft probe requires a model")
	}
	if s == nil {
		return nil, errors.New("nil openai gateway service")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, openAIOAuthTTFTProbeTimeout)
	defer cancel()

	payload, err := json.Marshal(map[string]any{
		"model": model,
		"input": []map[string]any{{
			"type": "message",
			"role": "user",
			"content": []map[string]any{{
				"type": "input_text",
				"text": "Reply with OK.",
			}},
		}},
		"stream": true,
		"store":  false,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal openai oauth ttft probe: %w", err)
	}
	request, err := http.NewRequestWithContext(probeCtx, http.MethodPost, "/v1/responses", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build openai oauth ttft probe request: %w", err)
	}
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Content-Type", "application/json")
	// Seed the synthetic request with the account's current outbound identity.
	// Forward then applies the same account-scoped identity and authentication
	// projection as a normal request, including proxy and refreshed token.
	identity := resolveCodexOutboundIdentityForAccount(account)
	if identity.userAgent != "" {
		request.Header.Set("User-Agent", identity.userAgent)
	}
	if identity.originator != "" {
		request.Header.Set("Originator", identity.originator)
	}
	if identity.version != "" {
		request.Header.Set("Version", identity.version)
	}

	writer := newOpenAIOAuthTTFTProbeResponseWriter()
	c, _ := gin.CreateTestContext(writer)
	c.Request = request
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	result, forwardErr := s.Forward(probeCtx, c, account, payload)
	if forwardErr != nil {
		return result, forwardErr
	}
	if err := validateOpenAIOAuthTTFTProbeResult(result); err != nil {
		return result, err
	}
	return result, nil
}
