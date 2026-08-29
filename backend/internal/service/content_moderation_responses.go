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
)

// responsesModerationAPIRequest is the small, provider-neutral subset of the
// OpenAI Responses request envelope needed by the moderation classifier.  The
// classifier deliberately uses a non-streaming request so the complete JSON
// decision can be parsed before applying the configured thresholds.
type responsesModerationAPIRequest struct {
	Model           string  `json:"model"`
	Instructions    string  `json:"instructions"`
	Input           any     `json:"input"`
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"max_output_tokens"`
	Store           bool    `json:"store"`
}

type responsesModerationInputMessage struct {
	Type    string                            `json:"type"`
	Role    string                            `json:"role"`
	Content []responsesModerationInputContent `json:"content"`
}

type responsesModerationInputContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// callResponsesModerationOnce sends one content sample through an
// OpenAI-compatible /v1/responses endpoint and converts its output into the
// same moderationAPIResult used by the native Moderations and Chat
// Completions transports.
func (s *ContentModerationService) callResponsesModerationOnce(ctx context.Context, cfg *ContentModerationConfig, apiKey string, input any, httpStatus *int) (*moderationAPIResult, error) {
	if cfg == nil {
		return nil, errors.New("content moderation config is nil")
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	endpoint, err := url.JoinPath(base, "/v1/responses")
	if err != nil {
		return nil, err
	}
	payload := responsesModerationAPIRequest{
		Model:           cfg.Model,
		Instructions:    contentModerationChatSystemPrompt,
		Input:           responsesModerationInput(input),
		Temperature:     0,
		MaxOutputTokens: 256,
		// Moderation payloads can contain sensitive user content. Keep the
		// Responses provider from retaining this classifier request when the
		// endpoint supports the standard `store` flag.
		Store: false,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeDurationMilliseconds(cfg.TimeoutMS))
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
		return nil, fmt.Errorf("responses moderation api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxChatModerationResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(responseBody)) > maxChatModerationResponseBytes {
		return nil, errors.New("responses moderation api response too large")
	}
	content, err := extractResponsesModerationContent(responseBody)
	if err != nil {
		return nil, err
	}
	return parseChatModerationResult(content)
}

// responsesModerationInput converts the multimodal input representation used
// by the native Moderations adapter (text/image_url parts) to the Responses
// input-item representation (input_text/input_image parts). Plain strings are
// left as strings, which is the most portable Responses request shape.
func responsesModerationInput(input any) any {
	switch value := input.(type) {
	case []moderationAPIInputPart:
		content := make([]responsesModerationInputContent, 0, len(value))
		for _, part := range value {
			switch strings.ToLower(strings.TrimSpace(part.Type)) {
			case "text", "input_text":
				if strings.TrimSpace(part.Text) == "" {
					continue
				}
				content = append(content, responsesModerationInputContent{Type: "input_text", Text: part.Text})
			case "image_url", "input_image":
				if part.ImageURL == nil || strings.TrimSpace(part.ImageURL.URL) == "" {
					continue
				}
				content = append(content, responsesModerationInputContent{Type: "input_image", ImageURL: part.ImageURL.URL})
			}
		}
		if len(content) == 0 {
			return ""
		}
		return []responsesModerationInputMessage{{Type: "message", Role: "user", Content: content}}
	case []string:
		// Responses accepts a string or message array, not the Moderations
		// endpoint's string array. Preserve all supplied text in one user item.
		content := make([]responsesModerationInputContent, 0, len(value))
		for _, text := range value {
			if strings.TrimSpace(text) != "" {
				content = append(content, responsesModerationInputContent{Type: "input_text", Text: text})
			}
		}
		if len(content) == 0 {
			return ""
		}
		return []responsesModerationInputMessage{{Type: "message", Role: "user", Content: content}}
	default:
		return input
	}
}

// extractResponsesModerationContent extracts assistant text from both the
// standard Responses envelope (output[].content[].text) and gateways that
// expose the convenience top-level output_text field.  Heterogeneous output
// items are intentionally accepted so reasoning/refusal metadata does not
// make an otherwise valid classifier result unreadable.
func extractResponsesModerationContent(body []byte) (string, error) {
	var envelope struct {
		OutputText json.RawMessage `json:"output_text"`
		Output     json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("responses moderation response envelope invalid: %w", err)
	}
	if text := responsesModerationRawText(envelope.OutputText); strings.TrimSpace(text) != "" {
		return text, nil
	}
	texts := extractResponsesModerationOutputTexts(envelope.Output)
	if len(texts) == 0 {
		// A few gateways expose the Responses route while returning the
		// Chat-Completions envelope. Accept that compatible fallback so a
		// provider migration does not silently disable moderation.
		if chatContent, err := extractChatModerationContent(body); err == nil {
			return chatContent, nil
		}
		return "", errors.New("responses moderation response output text empty")
	}
	return strings.Join(texts, "\n"), nil
}

func responsesModerationRawText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var texts []string
	if json.Unmarshal(raw, &texts) == nil {
		out := make([]string, 0, len(texts))
		for _, item := range texts {
			if strings.TrimSpace(item) != "" {
				out = append(out, item)
			}
		}
		return strings.Join(out, "\n")
	}
	return ""
}

func extractResponsesModerationOutputTexts(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		// A few OpenAI-compatible gateways return a single output item rather
		// than an array. Handle that shape without broad recursive traversal of
		// arbitrary response metadata.
		var item struct {
			Text    json.RawMessage `json:"text"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(raw, &item) == nil && (len(item.Text) > 0 || len(item.Content) > 0) {
			return extractResponsesModerationOutputItem(item.Text, item.Content)
		}
		if text := responsesModerationRawText(raw); strings.TrimSpace(text) != "" {
			return []string{text}
		}
		return nil
	}
	texts := make([]string, 0, len(items))
	for _, itemRaw := range items {
		var item struct {
			Text    json.RawMessage `json:"text"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(itemRaw, &item) != nil {
			// Non-standard but harmless: output may be an array of plain text
			// strings instead of output-item objects.
			if text := responsesModerationRawText(itemRaw); strings.TrimSpace(text) != "" {
				texts = append(texts, text)
			}
			continue
		}
		texts = append(texts, extractResponsesModerationOutputItem(item.Text, item.Content)...)
	}
	return texts
}

func extractResponsesModerationOutputItem(textRaw, contentRaw json.RawMessage) []string {
	texts := make([]string, 0, 2)
	if text := responsesModerationRawText(textRaw); strings.TrimSpace(text) != "" {
		texts = append(texts, text)
	}
	if len(contentRaw) == 0 || string(contentRaw) == "null" {
		return texts
	}
	var parts []json.RawMessage
	if json.Unmarshal(contentRaw, &parts) == nil {
		for _, partRaw := range parts {
			var part struct {
				Text json.RawMessage `json:"text"`
			}
			if json.Unmarshal(partRaw, &part) == nil {
				if text := responsesModerationRawText(part.Text); strings.TrimSpace(text) != "" {
					texts = append(texts, text)
				}
				continue
			}
			if text := responsesModerationRawText(partRaw); strings.TrimSpace(text) != "" {
				texts = append(texts, text)
			}
		}
		return texts
	}
	// Some gateways encode one content part as an object rather than a list.
	var part struct {
		Text json.RawMessage `json:"text"`
	}
	if json.Unmarshal(contentRaw, &part) == nil {
		if text := responsesModerationRawText(part.Text); strings.TrimSpace(text) != "" {
			texts = append(texts, text)
		}
		return texts
	}
	if text := responsesModerationRawText(contentRaw); strings.TrimSpace(text) != "" {
		texts = append(texts, text)
	}
	return texts
}
