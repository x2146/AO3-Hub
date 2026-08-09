package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func setLLMTestTLSClient(t *testing.T, servers ...*httptest.Server) {
	t.Helper()
	roots := x509.NewCertPool()
	for _, server := range servers {
		roots.AddCert(server.Certificate())
	}
	transport := newLLMHTTPTransport()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	original := llmHTTPClient
	client := &http.Client{Transport: transport, CheckRedirect: original.CheckRedirect}
	llmHTTPClient = client
	t.Cleanup(func() {
		client.CloseIdleConnections()
		llmHTTPClient = original
	})
}

func TestChatOpenAICompatible(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get("authorization") != "Bearer test-key" {
			t.Fatalf("authorization header = %q", r.Header.Get("authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["response_format"] == nil {
			t.Fatal("missing response_format for json mode")
		}
		if body["max_tokens"] != float64(1000) {
			t.Fatalf("max_tokens = %v", body["max_tokens"])
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": `{"ok":true}`}},
			},
			"usage": map[string]any{"total_tokens": 12},
		})
	}))
	defer server.Close()

	result, err := chat(context.Background(), LLMConfig{
		APIType:             LLMAPITypeOpenAICompatible,
		BaseURL:             server.URL,
		APIKey:              "test-key",
		Model:               "test-model",
		Temperature:         0.3,
		MaxTokensPerRequest: 1000,
	}, []ChatMessage{{Role: "user", Content: "ping"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if result.Content != `{"ok":true}` {
		t.Fatalf("content = %q", result.Content)
	}
}

func TestChatClaudeMessages(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get("x-api-key") != "test-key" {
			t.Fatalf("x-api-key header = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Fatalf("anthropic-version header = %q", r.Header.Get("anthropic-version"))
		}
		var body struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
			System    string `json:"system"`
			Messages  []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "claude-sonnet-4-5" {
			t.Fatalf("model = %q", body.Model)
		}
		if body.MaxTokens != 1000 {
			t.Fatalf("max_tokens = %d", body.MaxTokens)
		}
		wantSystem := "system prompt\n\nRespond only with a valid JSON object. Do not wrap it in Markdown code fences or add any explanation."
		if body.System != wantSystem {
			t.Fatalf("system = %q", body.System)
		}
		if len(body.Messages) != 1 || body.Messages[0].Role != "user" || body.Messages[0].Content != "ping" {
			t.Fatalf("messages = %+v", body.Messages)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"content": []map[string]string{
				{"type": "text", "text": `{"ok":true}`},
			},
			"usage": map[string]any{"output_tokens": 8},
		})
	}))
	defer server.Close()

	result, err := chat(context.Background(), LLMConfig{
		APIType:             LLMAPITypeClaudeMessages,
		BaseURL:             server.URL,
		APIKey:              "test-key",
		Model:               "claude-sonnet-4-5",
		Temperature:         0.3,
		MaxTokensPerRequest: 1000,
	}, []ChatMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "ping"},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/messages" {
		t.Fatalf("path = %q", gotPath)
	}
	if result.Content != `{"ok":true}` {
		t.Fatalf("content = %q", result.Content)
	}
}

func TestChatClaudeMessagesCombinesTextBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"content": []map[string]string{
				{"type": "text", "text": "hello"},
				{"type": "thinking", "text": "ignored"},
				{"type": "text", "text": " world"},
			},
		})
	}))
	defer server.Close()

	result, err := chat(context.Background(), LLMConfig{
		APIType:             "anthropic",
		BaseURL:             server.URL,
		APIKey:              "test-key",
		Model:               "claude-sonnet-4-5",
		Temperature:         0.3,
		MaxTokensPerRequest: 1000,
	}, []ChatMessage{{Role: "user", Content: "ping"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "hello world" {
		t.Fatalf("content = %q", result.Content)
	}
}

func TestChatClaudeMessagesRequiresNonSystemMessage(t *testing.T) {
	_, err := chat(context.Background(), LLMConfig{
		APIType:             LLMAPITypeClaudeMessages,
		BaseURL:             "http://example.test",
		APIKey:              "test-key",
		Model:               "claude-sonnet-4-5",
		MaxTokensPerRequest: 1000,
	}, []ChatMessage{{Role: "system", Content: "system prompt"}}, false)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLLMEndpointRejectsCredentialLeaks(t *testing.T) {
	for _, baseURL := range []string{
		"http://provider.example/v1",
		"https://user:secret@provider.example/v1",
		"https://provider.example/v1?token=secret",
	} {
		if _, err := llmEndpoint(baseURL, "/messages"); err == nil {
			t.Fatalf("llmEndpoint(%q) was accepted", baseURL)
		}
	}
	if got, err := llmEndpoint("http://127.0.0.1:8080/v1/", "/messages"); err != nil || got != "http://127.0.0.1:8080/v1/messages" {
		t.Fatalf("loopback endpoint = %q, %v", got, err)
	}
}

func TestValidateConfigRejectsInsecureLLMBaseURL(t *testing.T) {
	cfg := defaultConfig()
	cfg.LLM.BaseURL = "http://provider.example/v1"
	if err := validateConfig(cfg); err == nil || !strings.Contains(err.Error(), "baseURL") {
		t.Fatalf("validation error = %v", err)
	}
}

func TestNormalizeLLMAPIType(t *testing.T) {
	tests := map[string]string{
		"":                   LLMAPITypeOpenAICompatible,
		"openai":             LLMAPITypeOpenAICompatible,
		"chat-completions":   LLMAPITypeOpenAICompatible,
		"anthropic":          LLMAPITypeClaudeMessages,
		"claude":             LLMAPITypeClaudeMessages,
		"anthropic-messages": LLMAPITypeClaudeMessages,
	}
	for input, want := range tests {
		if got := normalizeLLMAPIType(input); got != want {
			t.Fatalf("normalizeLLMAPIType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeConfigUsesClaudeDefaults(t *testing.T) {
	cfg := normalizeConfig(Config{LLM: LLMConfig{APIType: LLMAPITypeClaudeMessages}})
	if cfg.LLM.BaseURL != DefaultClaudeMessagesBaseURL {
		t.Fatalf("baseURL = %q", cfg.LLM.BaseURL)
	}
	if cfg.LLM.Model != DefaultClaudeMessagesModel {
		t.Fatalf("model = %q", cfg.LLM.Model)
	}
}

func TestNormalizeLLMProviderDefaultsOnTypeSwitch(t *testing.T) {
	previous := defaultLLMConfig(LLMAPITypeOpenAICompatible)
	next := previous
	next.APIType = LLMAPITypeClaudeMessages
	normalizeLLMProviderDefaults(&next, previous, map[string]json.RawMessage{
		"apiType": json.RawMessage(`"claude-messages"`),
	})
	if next.BaseURL != DefaultClaudeMessagesBaseURL {
		t.Fatalf("baseURL = %q", next.BaseURL)
	}
	if next.Model != DefaultClaudeMessagesModel {
		t.Fatalf("model = %q", next.Model)
	}
}

func TestNormalizeLLMProviderDefaultsKeepsCustomValues(t *testing.T) {
	previous := defaultLLMConfig(LLMAPITypeOpenAICompatible)
	previous.BaseURL = "https://proxy.example/v1"
	previous.Model = "custom-model"
	next := previous
	next.APIType = LLMAPITypeClaudeMessages
	normalizeLLMProviderDefaults(&next, previous, map[string]json.RawMessage{
		"apiType": json.RawMessage(`"claude-messages"`),
	})
	if next.BaseURL != previous.BaseURL {
		t.Fatalf("baseURL = %q", next.BaseURL)
	}
	if next.Model != previous.Model {
		t.Fatalf("model = %q", next.Model)
	}
}

func TestChatOpenAICompatibleStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["stream"] != true {
			t.Fatalf("expected stream=true, got %v", body["stream"])
		}
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		chunks := []string{
			`{"choices":[{"delta":{"content":"hello"}}]}`,
			`{"choices":[{"delta":{"content":" world"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"total_tokens":7}}`,
			`[DONE]`,
		}
		for _, c := range chunks {
			_, _ = w.Write([]byte("data: " + c + "\n\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer server.Close()

	result, err := chat(context.Background(), LLMConfig{
		APIType:             LLMAPITypeOpenAICompatible,
		BaseURL:             server.URL,
		APIKey:              "test-key",
		Model:               "test-model",
		Temperature:         0.3,
		MaxTokensPerRequest: 1000,
		Stream:              true,
	}, []ChatMessage{{Role: "user", Content: "ping"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "hello world" {
		t.Fatalf("content = %q", result.Content)
	}
	if _, _, total := extractUsage(result.Usage); total != 7 {
		t.Fatalf("usage total = %d, want 7 (raw=%v)", total, result.Usage)
	}
}

func TestChatOpenAICompatibleStreamRequiresDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"partial"}}]}` + "\n\n"))
	}))
	defer server.Close()

	_, err := chat(context.Background(), LLMConfig{
		APIType:             LLMAPITypeOpenAICompatible,
		BaseURL:             server.URL,
		APIKey:              "test-key",
		Model:               "test-model",
		MaxTokensPerRequest: 1000,
		Stream:              true,
	}, []ChatMessage{{Role: "user", Content: "ping"}}, false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "stream ended before [DONE]") {
		t.Fatalf("error = %q", err)
	}
}

func TestChatOpenAICompatibleStreamReturnsErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"partial"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("event: error\n"))
		_, _ = w.Write([]byte(`data: {"error":{"message":"boom"}}` + "\n\n"))
	}))
	defer server.Close()

	_, err := chat(context.Background(), LLMConfig{
		APIType:             LLMAPITypeOpenAICompatible,
		BaseURL:             server.URL,
		APIKey:              "test-key",
		Model:               "test-model",
		MaxTokensPerRequest: 1000,
		Stream:              true,
	}, []ChatMessage{{Role: "user", Content: "ping"}}, false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %q", err)
	}
}

func TestChatClaudeMessagesStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["stream"] != true {
			t.Fatalf("expected stream=true, got %v", body["stream"])
		}
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		events := []struct{ name, data string }{
			{"message_start", `{"type":"message_start","message":{"usage":{"input_tokens":5}}}`},
			{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`},
			{"content_block_stop", `{"type":"content_block_stop","index":0}`},
			{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`},
			{"message_stop", `{"type":"message_stop"}`},
		}
		for _, ev := range events {
			_, _ = w.Write([]byte("event: " + ev.name + "\n"))
			_, _ = w.Write([]byte("data: " + ev.data + "\n\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer server.Close()

	result, err := chat(context.Background(), LLMConfig{
		APIType:             LLMAPITypeClaudeMessages,
		BaseURL:             server.URL,
		APIKey:              "test-key",
		Model:               "claude-sonnet-4-5",
		Temperature:         0.3,
		MaxTokensPerRequest: 1000,
		Stream:              true,
	}, []ChatMessage{{Role: "user", Content: "ping"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "hello world" {
		t.Fatalf("content = %q", result.Content)
	}
	prompt, completion, _ := extractUsage(result.Usage)
	if prompt != 5 || completion != 3 {
		t.Fatalf("usage prompt=%d completion=%d", prompt, completion)
	}
}

func TestChatClaudeMessagesStreamRequiresMessageStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		events := []struct{ name, data string }{
			{"message_start", `{"type":"message_start","message":{"usage":{"input_tokens":5}}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`},
		}
		for _, ev := range events {
			_, _ = w.Write([]byte("event: " + ev.name + "\n"))
			_, _ = w.Write([]byte("data: " + ev.data + "\n\n"))
		}
	}))
	defer server.Close()

	_, err := chat(context.Background(), LLMConfig{
		APIType:             LLMAPITypeClaudeMessages,
		BaseURL:             server.URL,
		APIKey:              "test-key",
		Model:               "claude-sonnet-4-5",
		MaxTokensPerRequest: 1000,
		Stream:              true,
	}, []ChatMessage{{Role: "user", Content: "ping"}}, false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "stream ended before message_stop") {
		t.Fatalf("error = %q", err)
	}
}

func TestChatClaudeMessagesStreamReturnsErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: error\n"))
		_, _ = w.Write([]byte(`data: {"type":"error","error":{"type":"overloaded_error","message":"boom"}}` + "\n\n"))
	}))
	defer server.Close()

	_, err := chat(context.Background(), LLMConfig{
		APIType:             LLMAPITypeClaudeMessages,
		BaseURL:             server.URL,
		APIKey:              "test-key",
		Model:               "claude-sonnet-4-5",
		MaxTokensPerRequest: 1000,
		Stream:              true,
	}, []ChatMessage{{Role: "user", Content: "ping"}}, false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %q", err)
	}
}

func TestClaudeAPIKeyIsNotForwardedOnRedirect(t *testing.T) {
	for _, test := range []struct {
		name string
		tls  bool
	}{
		{name: "cross origin", tls: true},
		{name: "HTTPS downgrade", tls: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			received := make(chan string, 1)
			targetHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received <- r.Header.Get("x-api-key")
				writeJSON(w, http.StatusOK, map[string]any{"content": []map[string]string{{"type": "text", "text": "unexpected"}}})
			})
			var target *httptest.Server
			if test.tls {
				target = httptest.NewTLSServer(targetHandler)
			} else {
				target = httptest.NewServer(targetHandler)
			}
			defer target.Close()

			source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL+"/stolen", http.StatusTemporaryRedirect)
			}))
			defer source.Close()
			if test.tls {
				setLLMTestTLSClient(t, source, target)
			} else {
				setLLMTestTLSClient(t, source)
			}

			_, err := chat(context.Background(), LLMConfig{
				APIType:             LLMAPITypeClaudeMessages,
				BaseURL:             source.URL,
				APIKey:              "secret-key",
				Model:               "test-model",
				MaxTokensPerRequest: 1000,
			}, []ChatMessage{{Role: "user", Content: "ping"}}, false)
			if err == nil || !strings.Contains(err.Error(), "redirects are disabled") {
				t.Fatalf("redirect error = %v", err)
			}
			select {
			case key := <-received:
				t.Fatalf("redirect target received x-api-key %q", key)
			default:
			}
		})
	}
}

func TestChatHonorsContextDeadline(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	}))
	defer server.Close()
	defer close(release)
	setLLMTestTLSClient(t, server)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := chat(ctx, LLMConfig{
		APIType:             LLMAPITypeOpenAICompatible,
		BaseURL:             server.URL,
		APIKey:              "test-key",
		Model:               "test-model",
		MaxTokensPerRequest: 1000,
	}, []ChatMessage{{Role: "user", Content: "ping"}}, false)
	<-started
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
}

func TestChatResponseSizeLimits(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		limit  int64
	}{
		{name: "success", status: http.StatusOK, limit: maxLLMResponseBytes},
		{name: "error", status: http.StatusBadGateway, limit: maxLLMErrorBytes},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "application/json")
				w.Header().Set("content-length", strconv.FormatInt(test.limit+1, 10))
				w.WriteHeader(test.status)
			}))
			defer server.Close()
			setLLMTestTLSClient(t, server)

			_, err := chat(context.Background(), LLMConfig{
				APIType:             LLMAPITypeOpenAICompatible,
				BaseURL:             server.URL,
				APIKey:              "test-key",
				Model:               "test-model",
				MaxTokensPerRequest: 1000,
			}, []ChatMessage{{Role: "user", Content: "ping"}}, false)
			if err == nil || !strings.Contains(err.Error(), "exceeds") {
				t.Fatalf("size-limit error = %v", err)
			}
			if test.status != http.StatusOK {
				var providerErr LLMError
				if !errors.As(err, &providerErr) || providerErr.Status != test.status {
					t.Fatalf("provider error = %#v", err)
				}
			}
		})
	}
}

func TestChatStreamCanBeCanceled(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}))
	defer server.Close()
	setLLMTestTLSClient(t, server)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := chat(ctx, LLMConfig{
		APIType:             LLMAPITypeOpenAICompatible,
		BaseURL:             server.URL,
		APIKey:              "test-key",
		Model:               "test-model",
		MaxTokensPerRequest: 1000,
		Stream:              true,
	}, []ChatMessage{{Role: "user", Content: "ping"}}, false)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stream cancellation error = %v", err)
	}
}

func TestChatStreamRejectsOversizedLine(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", strings.Repeat("x", maxSSELineBytes))
	}))
	defer server.Close()
	setLLMTestTLSClient(t, server)

	_, err := chat(context.Background(), LLMConfig{
		APIType:             LLMAPITypeOpenAICompatible,
		BaseURL:             server.URL,
		APIKey:              "test-key",
		Model:               "test-model",
		MaxTokensPerRequest: 1000,
		Stream:              true,
	}, []ChatMessage{{Role: "user", Content: "ping"}}, false)
	if err == nil || !strings.Contains(err.Error(), "SSE line exceeds") {
		t.Fatalf("oversized line error = %v", err)
	}
}

func TestChatStreamRejectsUnexpectedContentType(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	setLLMTestTLSClient(t, server)

	_, err := chat(context.Background(), LLMConfig{
		APIType:             LLMAPITypeOpenAICompatible,
		BaseURL:             server.URL,
		APIKey:              "test-key",
		Model:               "test-model",
		MaxTokensPerRequest: 1000,
		Stream:              true,
	}, []ChatMessage{{Role: "user", Content: "ping"}}, false)
	if err == nil || !strings.Contains(err.Error(), "content type") {
		t.Fatalf("content-type error = %v", err)
	}
}

func TestScanSSEEnforcesEventAndTotalLimits(t *testing.T) {
	t.Run("event", func(t *testing.T) {
		err := scanSSE(context.Background(), strings.NewReader("data: abc\ndata: def\n\n"), sseLimits{
			lineBytes:  32,
			eventBytes: 5,
			totalBytes: 128,
		}, func(_, _ string) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "SSE event exceeds") {
			t.Fatalf("event-limit error = %v", err)
		}
	})

	t.Run("total", func(t *testing.T) {
		err := scanSSE(context.Background(), strings.NewReader(": keep\n: keep\n"), sseLimits{
			lineBytes:  32,
			eventBytes: 32,
			totalBytes: 10,
		}, func(_, _ string) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "SSE stream exceeds") {
			t.Fatalf("stream-limit error = %v", err)
		}
	})
}

func TestAppendBoundedRejectsCumulativeStreamContent(t *testing.T) {
	var content strings.Builder
	if err := appendBounded(&content, "1234", 5); err != nil {
		t.Fatal(err)
	}
	if err := appendBounded(&content, "56", 5); err == nil || !strings.Contains(err.Error(), "stream content exceeds") {
		t.Fatalf("content-limit error = %v", err)
	}
}
