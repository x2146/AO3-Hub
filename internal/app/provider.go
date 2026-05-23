package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatResult struct {
	Content string         `json:"content"`
	Usage   map[string]any `json:"usage,omitempty"`
}

type LLMError struct {
	Status int
	Body   string
}

func (e LLMError) Error() string {
	body := e.Body
	if len(body) > 200 {
		body = body[:200]
	}
	return fmt.Sprintf("LLM provider error %d: %s", e.Status, body)
}

func chat(ctx context.Context, config LLMConfig, messages []ChatMessage, jsonMode bool) (ChatResult, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return ChatResult{}, errors.New("LLM apiKey not configured")
	}
	if strings.TrimSpace(config.BaseURL) == "" {
		return ChatResult{}, errors.New("LLM baseURL not configured")
	}
	switch normalizeLLMAPIType(config.APIType) {
	case LLMAPITypeOpenAICompatible:
		return chatOpenAICompatible(ctx, config, messages, jsonMode)
	case LLMAPITypeClaudeMessages:
		return chatClaudeMessages(ctx, config, messages, jsonMode)
	default:
		return ChatResult{}, fmt.Errorf("unsupported LLM apiType: %s", config.APIType)
	}
}

func chatOpenAICompatible(ctx context.Context, config LLMConfig, messages []ChatMessage, jsonMode bool) (ChatResult, error) {
	body := map[string]any{
		"model":       config.Model,
		"temperature": config.Temperature,
		"messages":    messages,
	}
	if jsonMode {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return ChatResult{}, err
	}
	url := strings.TrimRight(config.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return ChatResult{}, err
	}
	req.Header.Set("authorization", "Bearer "+config.APIKey)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ChatResult{}, err
	}
	defer res.Body.Close()
	textBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return ChatResult{}, err
	}
	text := string(textBytes)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ChatResult{}, LLMError{Status: res.StatusCode, Body: text}
	}
	var raw struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage map[string]any `json:"usage"`
	}
	if err := json.Unmarshal(textBytes, &raw); err != nil {
		return ChatResult{}, LLMError{Status: 500, Body: "non-json response: " + truncate(text, 200)}
	}
	if len(raw.Choices) == 0 || raw.Choices[0].Message.Content == "" {
		return ChatResult{}, LLMError{Status: 500, Body: "missing message.content: " + truncate(text, 200)}
	}
	return ChatResult{Content: raw.Choices[0].Message.Content, Usage: raw.Usage}, nil
}

func chatClaudeMessages(ctx context.Context, config LLMConfig, messages []ChatMessage, jsonMode bool) (ChatResult, error) {
	system, claudeMessages := splitClaudeMessages(messages)
	if len(claudeMessages) == 0 {
		return ChatResult{}, errors.New("Claude Messages requires at least one user or assistant message")
	}
	if jsonMode {
		system = appendSystemInstruction(system, "Respond only with a valid JSON object. Do not wrap it in Markdown code fences or add any explanation.")
	}
	body := map[string]any{
		"model":       config.Model,
		"max_tokens":  config.MaxTokensPerRequest,
		"temperature": config.Temperature,
		"messages":    claudeMessages,
	}
	if system != "" {
		body["system"] = system
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return ChatResult{}, err
	}
	url := strings.TrimRight(config.BaseURL, "/") + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return ChatResult{}, err
	}
	req.Header.Set("x-api-key", config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ChatResult{}, err
	}
	defer res.Body.Close()
	textBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return ChatResult{}, err
	}
	text := string(textBytes)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ChatResult{}, LLMError{Status: res.StatusCode, Body: text}
	}
	var raw struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage map[string]any `json:"usage"`
	}
	if err := json.Unmarshal(textBytes, &raw); err != nil {
		return ChatResult{}, LLMError{Status: 500, Body: "non-json response: " + truncate(text, 200)}
	}
	parts := []string{}
	for _, block := range raw.Content {
		if block.Type == "" || block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}
	content := strings.TrimSpace(strings.Join(parts, ""))
	if content == "" {
		return ChatResult{}, LLMError{Status: 500, Body: "missing content text: " + truncate(text, 200)}
	}
	return ChatResult{Content: content, Usage: raw.Usage}, nil
}

func appendSystemInstruction(system, instruction string) string {
	if strings.TrimSpace(system) == "" {
		return instruction
	}
	return system + "\n\n" + instruction
}

func splitClaudeMessages(messages []ChatMessage) (string, []map[string]string) {
	systemParts := []string{}
	out := []map[string]string{}
	for _, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		content := message.Content
		switch role {
		case "system":
			if strings.TrimSpace(content) != "" {
				systemParts = append(systemParts, content)
			}
		case "assistant":
			out = append(out, map[string]string{"role": "assistant", "content": content})
		default:
			out = append(out, map[string]string{"role": "user", "content": content})
		}
	}
	return strings.Join(systemParts, "\n\n"), out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
