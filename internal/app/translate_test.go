package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildUserPayloadSendsTextRunsWithoutHTML(t *testing.T) {
	input, err := makeTranslateInput(Block{
		ID:   "b1",
		Type: BlockP,
		HTML: `<p style="text-align: right">I <em>can't</em> leave.</p>`,
	})
	if err != nil {
		t.Fatal(err)
	}

	payload, err := buildUserPayload(Meta{Title: "Work", Tags: Tags{Fandom: []string{"F"}}}, []translateInput{input}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "<p") || strings.Contains(payload, "text-align") {
		t.Fatalf("payload leaked html markup: %s", payload)
	}

	var parsed struct {
		Blocks []map[string]any `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Blocks) != 1 {
		t.Fatalf("blocks = %d", len(parsed.Blocks))
	}
	if _, ok := parsed.Blocks[0]["html"]; ok {
		t.Fatalf("payload block leaked html field: %+v", parsed.Blocks[0])
	}
	if parsed.Blocks[0]["text"] != "I can't leave." {
		t.Fatalf("block text = %v", parsed.Blocks[0]["text"])
	}
}

func TestTranslatedHTMLFromRunsPreservesOriginalSkeleton(t *testing.T) {
	input, err := makeTranslateInput(Block{
		ID:   "b1",
		Type: BlockP,
		HTML: `<p style="text-align: right">I <em>can't</em> leave.</p>`,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := translatedHTMLFromRuns(input, translateOutput{
		ID: "b1",
		Runs: []translateRun{
			{ID: "r0", Text: "我"},
			{ID: "r1", Text: "不能"},
			{ID: "r2", Text: "离开。"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `<p style="text-align: right">我<em>不能</em>离开。</p>`
	if got != want {
		t.Fatalf("translatedHTMLFromRuns() = %q, want %q", got, want)
	}
}

func TestTranslatedHTMLFromRunsRejectsHTMLResponse(t *testing.T) {
	input, err := makeTranslateInput(Block{
		ID:   "b1",
		Type: BlockP,
		HTML: `<p>Hello.</p>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := translatedHTMLFromRuns(input, translateOutput{ID: "b1", HTML: `<p>你好。</p>`}); err == nil {
		t.Fatal("expected html response to be rejected")
	}
}
