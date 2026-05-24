package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatResult struct {
	Content    string         `json:"content"`
	Usage      map[string]any `json:"usage,omitempty"`
	DurationMS int64          `json:"durationMs,omitempty"`
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
	start := time.Now()
	var result ChatResult
	var err error
	switch normalizeLLMAPIType(config.APIType) {
	case LLMAPITypeOpenAICompatible:
		if config.Stream {
			result, err = chatOpenAICompatibleStream(ctx, config, messages, jsonMode)
		} else {
			result, err = chatOpenAICompatible(ctx, config, messages, jsonMode)
		}
	case LLMAPITypeClaudeMessages:
		if config.Stream {
			result, err = chatClaudeMessagesStream(ctx, config, messages, jsonMode)
		} else {
			result, err = chatClaudeMessages(ctx, config, messages, jsonMode)
		}
	default:
		return ChatResult{}, fmt.Errorf("unsupported LLM apiType: %s", config.APIType)
	}
	result.DurationMS = time.Since(start).Milliseconds()
	return result, err
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

func chatOpenAICompatibleStream(ctx context.Context, config LLMConfig, messages []ChatMessage, jsonMode bool) (ChatResult, error) {
	body := map[string]any{
		"model":          config.Model,
		"temperature":    config.Temperature,
		"messages":       messages,
		"stream":         true,
		"stream_options": map[string]bool{"include_usage": true},
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
	req.Header.Set("accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ChatResult{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		textBytes, _ := io.ReadAll(res.Body)
		return ChatResult{}, LLMError{Status: res.StatusCode, Body: string(textBytes)}
	}

	var content strings.Builder
	var usage map[string]any
	err = scanSSE(res.Body, func(event, data string) error {
		if data == "[DONE]" {
			return io.EOF
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage map[string]any `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil
		}
		for _, c := range chunk.Choices {
			content.WriteString(c.Delta.Content)
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return ChatResult{}, err
	}
	out := content.String()
	if strings.TrimSpace(out) == "" {
		return ChatResult{}, LLMError{Status: 500, Body: "empty stream response"}
	}
	return ChatResult{Content: out, Usage: usage}, nil
}

func chatClaudeMessagesStream(ctx context.Context, config LLMConfig, messages []ChatMessage, jsonMode bool) (ChatResult, error) {
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
		"stream":      true,
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
	req.Header.Set("accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ChatResult{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		textBytes, _ := io.ReadAll(res.Body)
		return ChatResult{}, LLMError{Status: res.StatusCode, Body: string(textBytes)}
	}

	var content strings.Builder
	usage := map[string]any{}
	err = scanSSE(res.Body, func(event, data string) error {
		var head struct {
			Type    string `json:"type"`
			Message struct {
				Usage map[string]any `json:"usage"`
			} `json:"message"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			Usage map[string]any `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &head); err != nil {
			return nil
		}
		switch head.Type {
		case "message_start":
			for k, v := range head.Message.Usage {
				usage[k] = v
			}
		case "content_block_delta":
			if head.Delta.Type == "text_delta" {
				content.WriteString(head.Delta.Text)
			}
		case "message_delta":
			for k, v := range head.Usage {
				usage[k] = v
			}
		case "message_stop":
			return io.EOF
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return ChatResult{}, err
	}
	out := strings.TrimSpace(content.String())
	if out == "" {
		return ChatResult{}, LLMError{Status: 500, Body: "empty stream response"}
	}
	var usageOut map[string]any
	if len(usage) > 0 {
		usageOut = usage
	}
	return ChatResult{Content: out, Usage: usageOut}, nil
}

func scanSSE(body io.Reader, handle func(event, data string) error) error {
	reader := bufio.NewReaderSize(body, 64*1024)
	var event string
	var data strings.Builder
	flush := func() error {
		if data.Len() == 0 {
			event = ""
			return nil
		}
		payload := strings.TrimRight(data.String(), "\n")
		data.Reset()
		ev := event
		event = ""
		return handle(ev, payload)
	}
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case trimmed == "":
				if err := flush(); err != nil {
					return err
				}
			case strings.HasPrefix(trimmed, ":"):
				// comment / keep-alive; ignore
			case strings.HasPrefix(trimmed, "event:"):
				event = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			case strings.HasPrefix(trimmed, "data:"):
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				data.WriteString(strings.TrimPrefix(strings.TrimPrefix(trimmed, "data:"), " "))
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if flushErr := flush(); flushErr != nil && !errors.Is(flushErr, io.EOF) {
					return flushErr
				}
				return nil
			}
			return err
		}
	}
}
