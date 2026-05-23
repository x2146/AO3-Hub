package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTMLToPlainText(t *testing.T) {
	cases := map[string]string{
		`<p>Hello <em>world</em></p>`:           "Hello world",
		`<p>&quot;OK&quot; said Lando.</p>`:     `"OK" said Lando.`,
		`<p>Line<br/>break</p>`:                 "Linebreak",
		`<p>  spaced  </p>`:                     "spaced",
		`<center><p>☂</p></center>`:             "☂",
		`A &amp; B`:                             "A & B",
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

func TestAlignChapterSummariesFillsMissing(t *testing.T) {
	ctx := TranslationContext{
		ChapterSummaries: []ChapterSummary{
			{Index: 0, Summary: "first"},
			{Index: 2, Summary: "third"},
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
	if ctx.ChapterSummaries[1].Index != 1 || ctx.ChapterSummaries[1].Title != "C1" {
		t.Fatalf("filled = %+v", ctx.ChapterSummaries[1])
	}
	if ctx.ChapterSummaries[0].Summary != "first" || ctx.ChapterSummaries[2].Summary != "third" {
		t.Fatalf("did not preserve existing: %+v", ctx.ChapterSummaries)
	}
}

func TestAlignChapterSummariesErrorsOnExcess(t *testing.T) {
	ctx := TranslationContext{
		ChapterSummaries: []ChapterSummary{
			{Index: 0}, {Index: 1}, {Index: 2},
		},
	}
	original := ChapterFile{Chapters: []Chapter{{Index: 0}}}
	if err := alignChapterSummaries(&ctx, original); err == nil {
		t.Fatal("expected error for too many summaries")
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
	got, err := analyzeFullText(context.Background(), cfg, meta, original)
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
	out, err := buildUserPayload(meta, []translateInput{{ID: "x", HTML: "<p>hi</p>"}}, transCtx, 1)
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
	out, err := buildUserPayload(Meta{Title: "Work", Tags: Tags{Fandom: []string{"F"}}}, []translateInput{{ID: "x", HTML: "<p>hi</p>"}}, nil, 0)
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
