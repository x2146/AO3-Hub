package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
