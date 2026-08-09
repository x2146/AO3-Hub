package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHTMLToPlainText(t *testing.T) {
	cases := map[string]string{
		`<p>Hello <em>world</em></p>`:       "Hello world",
		`<p>&quot;OK&quot; said Lando.</p>`: `"OK" said Lando.`,
		`<p>Line<br/>break</p>`:             "Linebreak",
		`<p>  spaced  </p>`:                 "spaced",
		`<center><p>☂</p></center>`:         "☂",
		`A &amp; B`:                         "A & B",
	}
	for input, want := range cases {
		if got := htmlToPlainText(input); got != want {
			t.Errorf("htmlToPlainText(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestChapterPlainTextSkipsHRAndEmpty(t *testing.T) {
	chapter := Chapter{
		Index: 0,
		Blocks: []Block{
			{ID: "a", Type: BlockP, HTML: "<p>First</p>"},
			{ID: "b", Type: BlockHR, HTML: ""},
			{ID: "c", Type: BlockP, HTML: "<p></p>"},
			{ID: "d", Type: BlockP, HTML: "<p>Last</p>"},
		},
	}
	got := chapterPlainText(chapter)
	want := "First\n\n---\n\nLast"
	if got != want {
		t.Errorf("chapterPlainText = %q, want %q", got, want)
	}
}

func TestAlignChapterSummariesOrdersCompletePermutation(t *testing.T) {
	ctx := TranslationContext{
		ChapterSummaries: []ChapterSummary{
			{Index: 2, Summary: "third"},
			{Index: 0, Summary: "first"},
			{Index: 1, Summary: "second"},
		},
	}
	original := ChapterFile{Chapters: []Chapter{
		{Index: 0, Title: "C0"},
		{Index: 1, Title: "C1"},
		{Index: 2, Title: "C2"},
	}}
	if err := alignChapterSummaries(&ctx, original); err != nil {
		t.Fatal(err)
	}
	if len(ctx.ChapterSummaries) != 3 {
		t.Fatalf("len = %d", len(ctx.ChapterSummaries))
	}
	if ctx.ChapterSummaries[1].Index != 1 || ctx.ChapterSummaries[1].Title != "C1" || ctx.ChapterSummaries[1].Summary != "second" {
		t.Fatalf("ordered = %+v", ctx.ChapterSummaries)
	}
	if ctx.ChapterSummaries[0].Summary != "first" || ctx.ChapterSummaries[2].Summary != "third" {
		t.Fatalf("did not preserve summaries: %+v", ctx.ChapterSummaries)
	}
}

func TestAlignChapterSummariesRejectsInvalidResults(t *testing.T) {
	original := ChapterFile{Chapters: []Chapter{
		{Index: 0, Title: "C0"},
		{Index: 1, Title: "C1"},
	}}
	tests := map[string][]ChapterSummary{
		"missing":      {{Index: 0, Summary: "first"}},
		"duplicate":    {{Index: 0, Summary: "first"}, {Index: 0, Summary: "again"}},
		"out of range": {{Index: 0, Summary: "first"}, {Index: 2, Summary: "third"}},
		"empty":        {{Index: 0, Summary: "first"}, {Index: 1, Summary: " \n\t"}},
	}
	for name, summaries := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := TranslationContext{ChapterSummaries: summaries}
			if err := alignChapterSummaries(&ctx, original); err == nil {
				t.Fatal("expected invalid chapter summaries to fail")
			}
		})
	}
}

func TestParseAnalysisFullResponseStripsFences(t *testing.T) {
	content := "```json\n{\"summary\":\"s\",\"tone\":\"t\",\"chapterSummaries\":[]}\n```"
	got, err := parseAnalysisFullResponse(content)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "s" || got.Tone != "t" {
		t.Fatalf("got = %+v", got)
	}
	if got.Glossary == nil || got.ChapterSummaries == nil || got.Ships == nil || got.Characters == nil {
		t.Fatalf("nil slices/maps not initialized: %+v", got)
	}
}

func TestParseAnalysisResponsesRejectInvalidSchemaAndEmptySummaries(t *testing.T) {
	tests := map[string]string{
		"empty full summary": `{"summary":"  ","chapterSummaries":[]}`,
		"unknown field":      `{"summary":"ok","chapterSummaries":[],"unexpected":true}`,
		"trailing value":     `{"summary":"ok","chapterSummaries":[]} {}`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAnalysisFullResponse(content); err == nil {
				t.Fatal("expected invalid full analysis response to fail")
			}
		})
	}
	if _, err := parseChapterPartial(`{"summary":" \t"}`, 3, "C3"); err == nil {
		t.Fatal("expected empty chapter summary to fail")
	}
}

func analysisTestInputs() (Meta, ChapterFile, Config) {
	meta := Meta{
		ID:      "story",
		Title:   "Work",
		Author:  "Author",
		Summary: "Story summary",
		Tags: Tags{
			Fandom:       []string{"Fandom"},
			Relationship: []string{"A/B"},
			Character:    []string{"A", "B"},
			Additional:   []string{"Alternate Universe"},
			Rating:       "Teen",
			Warnings:     []string{"No Archive Warnings Apply"},
			Categories:   []string{"M/M"},
		},
	}
	original := ChapterFile{Chapters: []Chapter{{
		Index:  0,
		Title:  "Chapter One",
		Blocks: []Block{{ID: "block", Type: BlockP, HTML: "<p>Original text.</p>"}},
	}}}
	cfg := Config{LLM: LLMConfig{
		APIType:                LLMAPITypeOpenAICompatible,
		BaseURL:                "https://example.com/v1",
		APIKey:                 "first-secret",
		Model:                  "model-a",
		Temperature:            0.3,
		MaxTokensPerRequest:    3500,
		AnalysisMaxInputTokens: 60000,
	}}
	return meta, original, cfg
}

func TestAnalysisFingerprintIsDeterministicAndExcludesAPIKey(t *testing.T) {
	meta, original, cfg := analysisTestInputs()
	first, err := analysisFingerprint(meta, original, cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := analysisFingerprint(meta, original, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.HasPrefix(first, "v1:") || len(first) != len("v1:")+64 {
		t.Fatalf("unstable fingerprint: %q != %q", first, second)
	}
	cfg.LLM.APIKey = "different-secret"
	withoutSecret, err := analysisFingerprint(meta, original, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if withoutSecret != first {
		t.Fatal("API key changed analysis fingerprint")
	}

	cfg.LLM.APIType = "openai"
	cfg.LLM.BaseURL = "https://EXAMPLE.com:443/v1/"
	normalized, err := analysisFingerprint(meta, original, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if normalized != first {
		t.Fatalf("equivalent provider settings changed fingerprint: %q != %q", normalized, first)
	}
}

func TestAnalysisFingerprintInvalidatesEveryOutputInput(t *testing.T) {
	meta, original, cfg := analysisTestInputs()
	baseline, err := analysisFingerprint(meta, original, cfg)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*Meta, *ChapterFile, *Config){
		"meta": func(meta *Meta, _ *ChapterFile, _ *Config) {
			meta.Tags.Additional = append(meta.Tags.Additional, "Canon Divergence")
		},
		"original text": func(_ *Meta, original *ChapterFile, _ *Config) {
			original.Chapters[0].Blocks[0].HTML = "<p>Changed text.</p>"
		},
		"chapter title": func(_ *Meta, original *ChapterFile, _ *Config) {
			original.Chapters[0].Title = "Changed title"
		},
		"api type": func(_ *Meta, _ *ChapterFile, cfg *Config) {
			cfg.LLM.APIType = LLMAPITypeClaudeMessages
		},
		"base URL": func(_ *Meta, _ *ChapterFile, cfg *Config) {
			cfg.LLM.BaseURL = "https://other.example/v1"
		},
		"model": func(_ *Meta, _ *ChapterFile, cfg *Config) {
			cfg.LLM.Model = "model-b"
		},
		"temperature": func(_ *Meta, _ *ChapterFile, cfg *Config) {
			cfg.LLM.Temperature = 0.7
		},
		"output token limit": func(_ *Meta, _ *ChapterFile, cfg *Config) {
			cfg.LLM.MaxTokensPerRequest++
		},
		"analysis threshold": func(_ *Meta, _ *ChapterFile, cfg *Config) {
			cfg.LLM.AnalysisMaxInputTokens++
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changedMeta := meta
			changedMeta.Tags = meta.Tags
			changedMeta.Tags.Additional = append([]string(nil), meta.Tags.Additional...)
			changedOriginal := ChapterFile{Chapters: append([]Chapter(nil), original.Chapters...)}
			changedOriginal.Chapters[0].Blocks = append([]Block(nil), original.Chapters[0].Blocks...)
			changedCfg := cfg
			mutate(&changedMeta, &changedOriginal, &changedCfg)
			got, err := analysisFingerprint(changedMeta, changedOriginal, changedCfg)
			if err != nil {
				t.Fatal(err)
			}
			if got == baseline {
				t.Fatalf("%s did not invalidate analysis fingerprint", name)
			}
		})
	}
}

func newAnalysisRunTestApp(t *testing.T, meta Meta, original ChapterFile) *App {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, bus: NewEventBus(), inflight: map[string]map[string]bool{}}
	t.Cleanup(app.bus.Close)
	if err := store.SaveMeta(meta.ID, meta); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveOriginal(meta.ID, original); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveProgress(meta.ID, Progress{Phase: PhaseQueued, Errors: []ProgressError{}}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertIndex(indexEntryFor(meta, StatusQueued)); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestRunAnalysisPropagatesCorruptContext(t *testing.T) {
	meta, original, cfg := analysisTestInputs()
	app := newAnalysisRunTestApp(t, meta, original)
	contextPath := app.store.path("stories", meta.ID, "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"summary":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.runAnalysis(context.Background(), meta.ID, meta, original, cfg, nil); err == nil || !strings.Contains(err.Error(), "context.json") {
		t.Fatalf("expected corrupt context error, got %v", err)
	}
}

func TestRunAnalysisRejectsSemanticallyInvalidMatchingCache(t *testing.T) {
	meta, original, cfg := analysisTestInputs()
	app := newAnalysisRunTestApp(t, meta, original)
	fingerprint, err := analysisFingerprint(meta, original, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveContext(meta.ID, TranslationContext{
		Summary:             "valid full summary",
		ChapterSummaries:    []ChapterSummary{{Index: 0, Summary: " "}},
		AnalysisFingerprint: fingerprint,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.runAnalysis(context.Background(), meta.ID, meta, original, cfg, nil); err == nil || !strings.Contains(err.Error(), "分析缓存无效") {
		t.Fatalf("expected invalid cache error, got %v", err)
	}
}

func TestRunAnalysisDoesNotSaveAfterStoryChanges(t *testing.T) {
	meta, original, cfg := analysisTestInputs()
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": `{"summary":"old full summary","chapterSummaries":[{"index":0,"summary":"old chapter summary"}]}`}}},
		})
	}))
	defer server.Close()
	cfg.LLM.BaseURL = server.URL
	app := newAnalysisRunTestApp(t, meta, original)

	result := make(chan error, 1)
	go func() {
		_, err := app.runAnalysis(context.Background(), meta.ID, meta, original, cfg, nil)
		result <- err
	}()
	<-started
	changed := original
	changed.Chapters = append([]Chapter(nil), original.Chapters...)
	changed.Chapters[0].Blocks = append([]Block(nil), original.Chapters[0].Blocks...)
	changed.Chapters[0].Blocks[0].HTML = "<p>New story text.</p>"
	if err := app.store.SaveOriginal(meta.ID, changed); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-result; !errors.Is(err, errAnalysisInputsChanged) {
		t.Fatalf("expected stale input error, got %v", err)
	}
	cached, err := app.store.LoadContext(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cached != nil {
		t.Fatalf("stale analysis was saved: %+v", cached)
	}
}

func TestRunAnalysisCacheHitAvoidsProviderCall(t *testing.T) {
	meta, original, cfg := analysisTestInputs()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeError(w, http.StatusInternalServerError, "unexpected")
	}))
	defer server.Close()
	cfg.LLM.BaseURL = server.URL
	app := newAnalysisRunTestApp(t, meta, original)
	fingerprint, err := analysisFingerprint(meta, original, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := TranslationContext{
		Summary:             "cached summary",
		ChapterSummaries:    []ChapterSummary{{Index: 0, Summary: "cached chapter"}},
		AnalysisFingerprint: fingerprint,
	}
	if err := app.store.SaveContext(meta.ID, want); err != nil {
		t.Fatal(err)
	}
	got, err := app.runAnalysis(context.Background(), meta.ID, meta, original, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != want.Summary || calls.Load() != 0 {
		t.Fatalf("cache miss: result=%+v calls=%d", got, calls.Load())
	}
}

func TestAnalyzeFullTextSendsExpectedPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Messages) != 2 {
			t.Fatalf("messages = %d", len(body.Messages))
		}
		if body.Messages[0].Role != "system" || !strings.Contains(body.Messages[0].Content, "AO3 同人文资深读者") {
			t.Fatalf("system prompt = %q", body.Messages[0].Content)
		}
		var userPayload struct {
			Meta     map[string]any   `json:"meta"`
			Chapters []map[string]any `json:"chapters"`
		}
		if err := json.Unmarshal([]byte(body.Messages[1].Content), &userPayload); err != nil {
			t.Fatal(err)
		}
		if userPayload.Meta["title"] != "Test Work" {
			t.Fatalf("meta.title = %v", userPayload.Meta["title"])
		}
		if len(userPayload.Chapters) != 2 {
			t.Fatalf("chapters = %d", len(userPayload.Chapters))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": `{"summary":"全文摘要","tone":"fluff","glossary":{"Lando":"兰多"},"chapterSummaries":[{"index":0,"summary":"c0"},{"index":1,"summary":"c1"}]}`}},
			},
		})
	}))
	defer server.Close()

	cfg := Config{LLM: LLMConfig{
		APIType:             LLMAPITypeOpenAICompatible,
		BaseURL:             server.URL,
		APIKey:              "test",
		Model:               "test",
		MaxTokensPerRequest: 1000,
	}}
	meta := Meta{Title: "Test Work", Tags: Tags{Fandom: []string{"FX"}}}
	original := ChapterFile{Chapters: []Chapter{
		{Index: 0, Title: "C0", Blocks: []Block{{Type: BlockP, HTML: "<p>hi</p>"}}},
		{Index: 1, Title: "C1", Blocks: []Block{{Type: BlockP, HTML: "<p>bye</p>"}}},
	}}
	got, err := analyzeFullText(context.Background(), cfg, meta, original, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "全文摘要" || got.Glossary["Lando"] != "兰多" {
		t.Fatalf("got = %+v", got)
	}
	if len(got.ChapterSummaries) != 2 {
		t.Fatalf("chapterSummaries = %d", len(got.ChapterSummaries))
	}
}

func TestBuildUserPayloadIncludesContextWhenRefined(t *testing.T) {
	transCtx := &TranslationContext{
		Summary:  "s",
		Tone:     "t",
		Ships:    []string{"A/B"},
		Glossary: map[string]string{"Lando": "兰多"},
		ChapterSummaries: []ChapterSummary{
			{Index: 0, Title: "C0", Summary: "chapter 0 summary"},
			{Index: 1, Title: "C1", Summary: "chapter 1 summary"},
		},
	}
	meta := Meta{
		Title: "Work",
		Tags:  Tags{Fandom: []string{"F"}, Rating: "Explicit", Relationship: []string{"A/B"}, Warnings: []string{"None"}, Additional: []string{"AU"}},
	}
	out, err := buildUserPayload(meta, []translateInput{{ID: "x", Text: "hi", Runs: []translateRun{{ID: "r0", Text: "hi"}}, HTML: "<p>hi</p>"}}, transCtx, 1)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Context map[string]any   `json:"context"`
		Blocks  []map[string]any `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Context["summary"] != "s" || parsed.Context["tone"] != "t" {
		t.Fatalf("context = %+v", parsed.Context)
	}
	if parsed.Context["rating"] != "Explicit" {
		t.Fatalf("rating = %v", parsed.Context["rating"])
	}
	current, ok := parsed.Context["currentChapter"].(map[string]any)
	if !ok {
		t.Fatalf("missing currentChapter: %+v", parsed.Context)
	}
	if current["summary"] != "chapter 1 summary" {
		t.Fatalf("currentChapter.summary = %v", current["summary"])
	}
}

func TestBuildUserPayloadNormalModeOmitsContext(t *testing.T) {
	out, err := buildUserPayload(Meta{Title: "Work", Tags: Tags{Fandom: []string{"F"}}}, []translateInput{{ID: "x", Text: "hi", Runs: []translateRun{{ID: "r0", Text: "hi"}}, HTML: "<p>hi</p>"}}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Context map[string]any `json:"context"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed.Context["summary"]; ok {
		t.Fatalf("normal mode should not include summary: %+v", parsed.Context)
	}
	if _, ok := parsed.Context["currentChapter"]; ok {
		t.Fatalf("normal mode should not include currentChapter: %+v", parsed.Context)
	}
	if parsed.Context["title"] != "Work" {
		t.Fatalf("title = %v", parsed.Context["title"])
	}
}
