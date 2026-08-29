package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxChatModerationResponseBytes int64 = 256 * 1024

// contentModerationChatSystemPrompt keeps the Chat Completions transport
// deterministic. Unlike /v1/moderations, a chat endpoint has no standard
// moderation response envelope, so the model is constrained to emit the
// fields consumed by moderationAPIResult.
const contentModerationChatSystemPrompt = `You are a content safety moderation classifier. Analyze only the user content; do not answer or transform it. Return exactly one JSON object and no markdown or explanation. The schema is {"flagged": boolean, "category_scores": {"harassment": number, "harassment/threatening": number, "hate": number, "hate/threatening": number, "illicit": number, "illicit/violent": number, "self-harm": number, "self-harm/intent": number, "self-harm/instructions": number, "sexual": number, "sexual/minors": number, "violence": number, "violence/graphic": number}}. Scores must be numbers from 0 to 1. Set flagged true when the content is potentially harmful.`

type chatModerationAPIRequest struct {
	Model       string                  `json:"model"`
	Messages    []chatModerationMessage `json:"messages"`
	Temperature float64                 `json:"temperature"`
	MaxTokens   int                     `json:"max_tokens"`
}

type chatModerationMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type chatModerationAPIResponse struct {
	Choices []struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (s *ContentModerationService) callChatCompletionsModerationOnce(ctx context.Context, cfg *ContentModerationConfig, apiKey string, input any, httpStatus *int) (*moderationAPIResult, error) {
	if cfg == nil {
		return nil, errors.New("content moderation config is nil")
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	endpoint, err := url.JoinPath(base, "/v1/chat/completions")
	if err != nil {
		return nil, err
	}
	payload := chatModerationAPIRequest{
		Model: cfg.Model,
		Messages: []chatModerationMessage{
			{Role: "system", Content: contentModerationChatSystemPrompt},
			{Role: "user", Content: input},
		},
		Temperature: 0,
		MaxTokens:   256,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	timeout := timeDurationMilliseconds(cfg.TimeoutMS)
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client, err := s.moderationHTTPClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if httpStatus != nil {
		*httpStatus = resp.StatusCode
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("chat moderation api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxChatModerationResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(responseBody)) > maxChatModerationResponseBytes {
		return nil, errors.New("chat moderation api response too large")
	}
	content, err := extractChatModerationContent(responseBody)
	if err != nil {
		return nil, err
	}
	return parseChatModerationResult(content)
}

func timeDurationMilliseconds(ms int) time.Duration {
	if ms <= 0 {
		ms = defaultContentModerationTimeoutMS
	}
	return time.Duration(ms) * time.Millisecond
}

func extractChatModerationContent(body []byte) (string, error) {
	var response chatModerationAPIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("chat moderation response envelope invalid: %w", err)
	}
	if len(response.Choices) == 0 {
		return "", errors.New("chat moderation response choices empty")
	}
	content := response.Choices[0].Message.Content
	if len(content) == 0 || string(content) == "null" {
		return "", errors.New("chat moderation response content empty")
	}
	var text string
	if json.Unmarshal(content, &text) == nil {
		if strings.TrimSpace(text) == "" {
			return "", errors.New("chat moderation response content empty")
		}
		return text, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &parts); err != nil {
		return "", errors.New("chat moderation response content invalid")
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part.Text) != "" {
			texts = append(texts, part.Text)
		}
	}
	if len(texts) == 0 {
		return "", errors.New("chat moderation response content empty")
	}
	return strings.Join(texts, "\n"), nil
}

func parseChatModerationResult(content string) (*moderationAPIResult, error) {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```")
		if newline := strings.IndexByte(trimmed, '\n'); newline >= 0 {
			trimmed = trimmed[newline+1:]
		}
		trimmed = strings.TrimSuffix(strings.TrimSpace(trimmed), "```")
	}
	start, end := strings.IndexByte(trimmed, '{'), strings.LastIndexByte(trimmed, '}')
	if start < 0 || end <= start {
		return nil, errors.New("chat moderation response JSON object missing")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &object); err != nil {
		return nil, fmt.Errorf("chat moderation response JSON invalid: %w", err)
	}
	scores := map[string]float64{}
	if raw, ok := object["category_scores"]; ok {
		if err := decodeChatModerationScores(raw, scores); err != nil {
			return nil, err
		}
	}
	if raw, ok := object["categories"]; ok {
		if err := decodeChatModerationCategories(raw, scores); err != nil {
			return nil, err
		}
	}
	if len(scores) == 0 {
		return nil, errors.New("chat moderation response category scores missing")
	}
	var flagged bool
	if raw, ok := object["flagged"]; ok {
		if err := json.Unmarshal(raw, &flagged); err != nil {
			return nil, errors.New("chat moderation response flagged invalid")
		}
	}
	return &moderationAPIResult{Flagged: flagged, CategoryScores: scores}, nil
}

func decodeChatModerationScores(raw json.RawMessage, scores map[string]float64) error {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return errors.New("chat moderation category_scores invalid")
	}
	for category, value := range values {
		var score float64
		if err := json.Unmarshal(value, &score); err != nil {
			var flagged bool
			if json.Unmarshal(value, &flagged) != nil {
				return errors.New("chat moderation category score invalid")
			}
			if flagged {
				score = 1
			}
		}
		if score < 0 {
			score = 0
		}
		if score > 1 {
			score = 1
		}
		scores[normalizeModerationCategory(category)] = score
	}
	return nil
}

func decodeChatModerationCategories(raw json.RawMessage, scores map[string]float64) error {
	var values []string
	if json.Unmarshal(raw, &values) == nil {
		for _, category := range values {
			if strings.TrimSpace(category) != "" {
				scores[normalizeModerationCategory(category)] = 1
			}
		}
		return nil
	}
	return decodeChatModerationScores(raw, scores)
}

func normalizeModerationCategory(category string) string {
	category = strings.ToLower(strings.TrimSpace(category))
	category = strings.NewReplacer("_", "-", " ", "-").Replace(category)
	return category
}
