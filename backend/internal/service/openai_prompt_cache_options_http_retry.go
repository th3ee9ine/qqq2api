package service

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// doOpenAIUpstreamWithPromptCacheOptionsRetry preserves explicit caching on the
// first request. The Chat Completions pipelines do not use the Responses retry
// loop, so bridge only this exact capability rejection here, before any output
// is committed. Do not apply unrelated Responses-only repairs to native Chat
// Completions bodies.
func (s *OpenAIGatewayService) doOpenAIUpstreamWithPromptCacheOptionsRetry(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	request *http.Request,
	proxyURL string,
	body []byte,
) (*http.Response, error) {
	resp, err := s.doOpenAIUpstream(request, proxyURL, account)
	if err != nil || resp == nil || resp.StatusCode != http.StatusBadRequest ||
		account == nil || account.Platform != PlatformOpenAI ||
		!gjson.GetBytes(body, "prompt_cache_options").Exists() ||
		(c != nil && (IsResponseCommitted(c) || (c.Writer != nil && c.Writer.Written()))) ||
		ctx.Err() != nil || request.Context().Err() != nil {
		return resp, err
	}

	responseBody, _ := s.readOpenAIUpstreamError(resp)
	retryBody, reason, changed, retryErr := normalizeOpenAIResponsesRejectedFieldRetryBody(resp.StatusCode, body, responseBody)
	if retryErr != nil {
		logger.L().Warn("openai chat_completions: prompt cache options compatibility retry skipped", zap.Error(retryErr))
		return resp, nil
	}
	if !changed || reason != "prompt_cache_options parameter rejection" ||
		ctx.Err() != nil || request.Context().Err() != nil ||
		!openAIResponsesRejectedFieldRetryStateForRequest(c, body).Allow(retryBody) {
		return resp, nil
	}

	// Clone the fully built request so authentication, account identity, session
	// headers, proxy routing, and transport context stay identical on the retry.
	retryRequest := request.Clone(request.Context())
	retryRequest.Body = io.NopCloser(bytes.NewReader(retryBody))
	retryRequest.ContentLength = int64(len(retryBody))
	retryRequest.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(retryBody)), nil
	}
	_ = resp.Body.Close()
	logger.L().Info("openai chat_completions: retrying explicitly rejected prompt cache options",
		zap.Int64("account_id", account.ID),
		zap.String("reason", reason),
	)
	return s.doOpenAIUpstream(retryRequest, proxyURL, account)
}
