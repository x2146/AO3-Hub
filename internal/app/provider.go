package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	llmCallTimeout                 = 10 * time.Minute
	maxLLMResponseBytes      int64 = 16 << 20
	maxLLMErrorBytes         int64 = 64 << 10
	maxSSELineBytes                = 256 << 10
	maxSSEEventBytes               = 1 << 20
	maxSSEStreamBytes        int64 = 32 << 20
	maxLLMStreamContentBytes       = 16 << 20
)

var llmHTTPClient = &http.Client{
	Transport: newLLMHTTPTransport(),
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return errors.New("LLM provider redirects are disabled")
	},
}

func newLLMHTTPTransport() *http.Transport {
	transport := newExternalHTTPTransport()
	transport.ResponseHeaderTimeout = 5 * time.Minute
	return transport
}

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
	ctx, cancel := context.WithTimeout(ctx, llmCallTimeout)
	defer cancel()
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
		"max_tokens":  config.MaxTokensPerRequest,
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
	endpoint, err := llmEndpoint(config.BaseURL, "/chat/completions")
	if err != nil {
		return ChatResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return ChatResult{}, err
	}
	req.Header.Set("authorization", "Bearer "+config.APIKey)
	req.Header.Set("content-type", "application/json")
	res, err := llmHTTPClient.Do(req)
	if err != nil {
		return ChatResult{}, err
	}
	defer res.Body.Close()
	textBytes, err := readLLMResponse(res)
	if err != nil {
		return ChatResult{}, err
	}
	text := string(textBytes)
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
	endpoint, err := llmEndpoint(config.BaseURL, "/messages")
	if err != nil {
		return ChatResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return ChatResult{}, err
	}
	req.Header.Set("x-api-key", config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")
	res, err := llmHTTPClient.Do(req)
	if err != nil {
		return ChatResult{}, err
	}
	defer res.Body.Close()
	textBytes, err := readLLMResponse(res)
	if err != nil {
		return ChatResult{}, err
	}
	text := string(textBytes)
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

func llmEndpoint(baseURL, suffix string) (string, error) {
	base, err := url.ParseRequestURI(strings.TrimSpace(baseURL))
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return "", errors.New("invalid LLM baseURL")
	}
	switch strings.ToLower(base.Scheme) {
	case "https":
	case "http":
		host := base.Hostname()
		ip := net.ParseIP(host)
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return "", errors.New("LLM baseURL must use HTTPS outside loopback")
		}
	default:
		return "", errors.New("LLM baseURL must use HTTP or HTTPS")
	}
	base.Path = strings.TrimRight(base.Path, "/") + suffix
	base.RawPath = ""
	return base.String(), nil
}

func readLLMResponse(res *http.Response) ([]byte, error) {
	limit := maxLLMResponseBytes
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		limit = maxLLMErrorBytes
	}
	body, err := readBoundedResponse(res, limit, "LLM provider response")
	if err != nil {
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, LLMError{Status: res.StatusCode, Body: err.Error()}
		}
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, LLMError{Status: res.StatusCode, Body: string(body)}
	}
	return body, nil
}

func validateSSEContentType(res *http.Response) error {
	mediaType, _, err := mime.ParseMediaType(res.Header.Get("content-type"))
	if err != nil || mediaType != "text/event-stream" {
		return fmt.Errorf("LLM stream returned unexpected content type %q", res.Header.Get("content-type"))
	}
	return nil
}

func appendBounded(builder *strings.Builder, value string, limit int) error {
	if len(value) > limit-builder.Len() {
		return fmt.Errorf("LLM stream content exceeds %d bytes", limit)
	}
	builder.WriteString(value)
	return nil
}

func chatOpenAICompatibleStream(ctx context.Context, config LLMConfig, messages []ChatMessage, jsonMode bool) (ChatResult, error) {
	body := map[string]any{
		"model":          config.Model,
		"max_tokens":     config.MaxTokensPerRequest,
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
	endpoint, err := llmEndpoint(config.BaseURL, "/chat/completions")
	if err != nil {
		return ChatResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return ChatResult{}, err
	}
	req.Header.Set("authorization", "Bearer "+config.APIKey)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "text/event-stream")
	res, err := llmHTTPClient.Do(req)
	if err != nil {
		return ChatResult{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_, err := readLLMResponse(res)
		return ChatResult{}, err
	}
	if err := validateSSEContentType(res); err != nil {
		return ChatResult{}, err
	}

	var content strings.Builder
	var usage map[string]any
	done := false
	err = scanSSE(ctx, res.Body, defaultSSELimits(), func(event, data string) error {
		if data == "[DONE]" {
			done = true
			return io.EOF
		}
		if event == "error" {
			return LLMError{Status: 500, Body: "stream error: " + truncate(data, 200)}
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage map[string]any  `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return LLMError{Status: 500, Body: "non-json stream event: " + truncate(data, 200)}
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return LLMError{Status: 500, Body: "stream error: " + truncate(string(chunk.Error), 200)}
		}
		for _, c := range chunk.Choices {
			if err := appendBounded(&content, c.Delta.Content, maxLLMStreamContentBytes); err != nil {
				return err
			}
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return ChatResult{}, err
	}
	if !done {
		return ChatResult{}, LLMError{Status: 500, Body: "stream ended before [DONE]"}
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
	endpoint, err := llmEndpoint(config.BaseURL, "/messages")
	if err != nil {
		return ChatResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return ChatResult{}, err
	}
	req.Header.Set("x-api-key", config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "text/event-stream")
	res, err := llmHTTPClient.Do(req)
	if err != nil {
		return ChatResult{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_, err := readLLMResponse(res)
		return ChatResult{}, err
	}
	if err := validateSSEContentType(res); err != nil {
		return ChatResult{}, err
	}

	var content strings.Builder
	usage := map[string]any{}
	done := false
	err = scanSSE(ctx, res.Body, defaultSSELimits(), func(event, data string) error {
		if event == "error" {
			return LLMError{Status: 500, Body: "stream error: " + truncate(data, 200)}
		}
		var head struct {
			Type    string `json:"type"`
			Message struct {
				Usage map[string]any `json:"usage"`
			} `json:"message"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			Usage map[string]any  `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &head); err != nil {
			return LLMError{Status: 500, Body: "non-json stream event: " + truncate(data, 200)}
		}
		switch head.Type {
		case "message_start":
			for k, v := range head.Message.Usage {
				usage[k] = v
			}
		case "content_block_delta":
			if head.Delta.Type == "text_delta" {
				if err := appendBounded(&content, head.Delta.Text, maxLLMStreamContentBytes); err != nil {
					return err
				}
			}
		case "message_delta":
			for k, v := range head.Usage {
				usage[k] = v
			}
		case "error":
			body := data
			if len(head.Error) > 0 && string(head.Error) != "null" {
				body = string(head.Error)
			}
			return LLMError{Status: 500, Body: "stream error: " + truncate(body, 200)}
		case "message_stop":
			done = true
			return io.EOF
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return ChatResult{}, err
	}
	if !done {
		return ChatResult{}, LLMError{Status: 500, Body: "stream ended before message_stop"}
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

type sseLimits struct {
	lineBytes  int
	eventBytes int
	totalBytes int64
}

func defaultSSELimits() sseLimits {
	return sseLimits{
		lineBytes:  maxSSELineBytes,
		eventBytes: maxSSEEventBytes,
		totalBytes: maxSSEStreamBytes,
	}
}

func scanSSE(ctx context.Context, body io.Reader, limits sseLimits, handle func(event, data string) error) error {
	if limits.lineBytes <= 0 || limits.eventBytes <= 0 || limits.totalBytes <= 0 {
		return errors.New("invalid SSE limits")
	}
	reader := bufio.NewReaderSize(body, limits.lineBytes+1)
	var event string
	var data strings.Builder
	var totalBytes int64
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
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := reader.ReadSlice('\n')
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		if errors.Is(err, bufio.ErrBufferFull) || len(line) > limits.lineBytes {
			return fmt.Errorf("SSE line exceeds %d bytes", limits.lineBytes)
		}
		totalBytes += int64(len(line))
		if totalBytes > limits.totalBytes {
			return fmt.Errorf("SSE stream exceeds %d bytes", limits.totalBytes)
		}
		if len(line) > 0 {
			trimmed := strings.TrimRight(string(line), "\r\n")
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
				value := strings.TrimPrefix(strings.TrimPrefix(trimmed, "data:"), " ")
				additional := len(value)
				if data.Len() > 0 {
					additional++
				}
				if additional > limits.eventBytes-data.Len() {
					return fmt.Errorf("SSE event exceeds %d bytes", limits.eventBytes)
				}
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				data.WriteString(value)
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
