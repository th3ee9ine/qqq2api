//go:build unit

package handler

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAuthRequestsBindTencentCaptchaProof(t *testing.T) {
	const payload = `{"email":"admin@example.com","password":"secret-123","tencent_captcha_ticket":"ticket-value","tencent_captcha_randstr":"@rand-value"}`

	var req LoginRequest
	require.NoError(t, json.Unmarshal([]byte(payload), &req))
	proof := captchaProof(req.TurnstileToken, req.TencentCaptchaTicket, req.TencentCaptchaRandstr)
	require.Equal(t, service.CaptchaProof{
		TencentTicket:  "ticket-value",
		TencentRandstr: "@rand-value",
	}, proof)
}
