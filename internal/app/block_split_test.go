package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func ao3TestDocument(body string) string {
	return `<!doctype html><html><head><title>Split - Archive of Our Own</title></head><body>
<div id="preface"><div class="meta"><h1>Split</h1><div class="byline">by <a rel="author">tester</a></div></div></div>
<div id="chapters"><div class="userstuff">` + body + `</div></div>
</body></html>`
}

func parseTestBlocks(t *testing.T, body string) []Block {
	t.Helper()
	parsed, err := parseAO3HTML(ao3TestDocument(body))
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Original.Chapters[0].Blocks
}

func assertBlocksWithinLimit(t *testing.T, blocks []Block) {
	t.Helper()
	for i, block := range blocks {
		if n := len(htmlToPlainText(block.HTML)); n > maxBlockTextBytes {
			t.Fatalf("block %d has %d bytes of text, limit %d", i, n, maxBlockTextBytes)
		}
	}
}

func joinedBlockText(blocks []Block) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		parts = append(parts, htmlToPlainText(block.HTML))
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func TestExtractBlocksFlattensNestedWrapperDivs(t *testing.T) {
	blocks := parseTestBlocks(t, `<p>Before.</p>
<div><div dir="auto"><div><p>One.</p><hr/><p>Two <em>em</em>.</p><blockquote><p>Quote.</p></blockquote></div></div></div>
<p>After.</p>`)
	want := []struct {
		typ  BlockType
		html string
	}{
		{BlockP, "<p>Before.</p>"},
		{BlockP, "<p>One.</p>"},
		{BlockHR, "<hr/>"},
		{BlockP, "<p>Two <em>em</em>.</p>"},
		{BlockBlockquote, "<blockquote><p>Quote.</p></blockquote>"},
		{BlockP, "<p>After.</p>"},
	}
	if len(blocks) != len(want) {
		t.Fatalf("blocks = %#v", blocks)
	}
	for i, w := range want {
		if blocks[i].Type != w.typ || blocks[i].HTML != w.html {
			t.Fatalf("block %d = %s %q, want %s %q", i, blocks[i].Type, blocks[i].HTML, w.typ, w.html)
		}
	}
}

func TestExtractBlocksKeepsAlignedAndMixedWrappers(t *testing.T) {
	blocks := parseTestBlocks(t, `<div align="center"><p>Centered one.</p><p>Centered two.</p></div>
<div>Bare text <p>and a paragraph.</p></div>`)
	if len(blocks) != 2 {
		t.Fatalf("blocks = %#v", blocks)
	}
	if !strings.HasPrefix(blocks[0].HTML, `<div align="center">`) || !strings.Contains(blocks[0].HTML, "Centered two.") {
		t.Fatalf("aligned wrapper = %q", blocks[0].HTML)
	}
	if !strings.Contains(blocks[1].HTML, "Bare text") {
		t.Fatalf("mixed wrapper = %q", blocks[1].HTML)
	}
}

func TestExtractBlocksSplitsOversizedParagraphAtSentences(t *testing.T) {
	var b strings.Builder
	b.WriteString("<p>")
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&b, "Sentence %d has <em>some emphasis</em> and ends here. ", i)
	}
	b.WriteString("</p>")
	blocks := parseTestBlocks(t, b.String())
	if len(blocks) < 2 {
		t.Fatalf("oversized paragraph was not split: %d blocks", len(blocks))
	}
	assertBlocksWithinLimit(t, blocks)
	for i, block := range blocks {
		if block.Type != BlockP || !strings.HasPrefix(block.HTML, "<p>") || !strings.HasSuffix(block.HTML, "</p>") {
			t.Fatalf("piece %d = %q", i, block.HTML)
		}
		if !strings.Contains(block.HTML, "<em>some emphasis</em>") {
			t.Fatalf("piece %d lost inline formatting: %q", i, block.HTML)
		}
		if text := strings.TrimSpace(htmlToPlainText(block.HTML)); !strings.HasSuffix(text, "ends here.") {
			t.Fatalf("piece %d does not end at a sentence: %q", i, text[max(0, len(text)-40):])
		}
	}
	if got, want := joinedBlockText(blocks), joinedBlockText([]Block{{HTML: b.String()}}); got != want {
		t.Fatal("split pieces do not preserve the original text")
	}
}

func TestExtractBlocksSplitsOversizedEmphasisAndHardCutsUnpunctuatedText(t *testing.T) {
	long := strings.Repeat("word ", 2000)
	blocks := parseTestBlocks(t, "<p><em>"+long+"</em></p><p>"+strings.Repeat("x", 9000)+"</p>")
	if len(blocks) < 5 {
		t.Fatalf("blocks = %d", len(blocks))
	}
	assertBlocksWithinLimit(t, blocks)
	for i, block := range blocks {
		if strings.Contains(block.HTML, "word") && !strings.HasPrefix(block.HTML, "<p><em>") {
			t.Fatalf("piece %d lost its wrappers: %q", i, block.HTML[:40])
		}
	}
}

func TestExtractBlocksSplitsLongListAndContinuesNumbering(t *testing.T) {
	var b strings.Builder
	b.WriteString("<ol>")
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "<li>%s</li>", strings.Repeat("item text ", 12))
	}
	b.WriteString("</ol>")
	blocks := parseTestBlocks(t, b.String())
	if len(blocks) < 2 {
		t.Fatalf("list was not split: %d blocks", len(blocks))
	}
	assertBlocksWithinLimit(t, blocks)
	items := strings.Count(blocks[0].HTML, "<li>")
	if !strings.HasPrefix(blocks[1].HTML, fmt.Sprintf(`<ol start="%d">`, items+1)) {
		t.Fatalf("second list piece = %q", blocks[1].HTML[:30])
	}
}

func TestSplitOversizedBlockHTMLLeavesSmallBlocksUntouched(t *testing.T) {
	raw := `<p dir="auto">Short <em>text</em>.</p>`
	if got := splitOversizedBlockHTML(raw); len(got) != 1 || got[0] != raw {
		t.Fatalf("split = %#v", got)
	}
}

func TestCarryOverTranslatedKeepsFinishedBlocksByID(t *testing.T) {
	original := ChapterFile{Chapters: []Chapter{{Blocks: []Block{
		{ID: "a", Type: BlockP, HTML: "<p>A</p>"},
		{ID: "b1", Type: BlockP, HTML: "<p>B1</p>"},
		{ID: "b2", Type: BlockP, HTML: "<p>B2</p>"},
		{ID: "c", Type: BlockP, HTML: "<p>C</p>"},
	}}}}
	previous := &ChapterFile{Chapters: []Chapter{{Blocks: []Block{
		{ID: "a", Type: BlockP, HTML: "<p>甲</p>", Status: BlockDone},
		{ID: "b", Type: BlockP, Status: BlockError, Error: "stream error"},
		{ID: "c", Type: BlockP, Status: BlockError, Error: "stream error"},
	}}}}
	next := carryOverTranslated(previous, original)
	got := next.Chapters[0].Blocks
	if got[0].Status != BlockDone || got[0].HTML != "<p>甲</p>" {
		t.Fatalf("finished block not carried over: %#v", got[0])
	}
	for _, block := range got[1:] {
		if block.Status != BlockPending || block.HTML != "" || block.Error != "" {
			t.Fatalf("unfinished block = %#v", block)
		}
	}
}

func TestRepairOversizedOriginalReparsesSourceAndKeepsTranslations(t *testing.T) {
	var giant strings.Builder
	giant.WriteString(`<div><div dir="auto">`)
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&giant, "<p>Paragraph number %d of the wrapped tail.</p>", i)
	}
	giant.WriteString(`</div></div>`)
	source := ao3TestDocument(`<p>Kept.</p>` + giant.String())

	// Simulate a story stored by the old parser: the wrapper is one block.
	app, storyID, _, _ := newTranslationTestState(t, nil)
	keptHTML := "<p>Kept.</p>"
	wrapperHTML := strings.TrimSpace(sanitizeHTMLFragment(giant.String()))
	stale := ChapterFile{Chapters: []Chapter{{Index: 0, Title: "Split", Blocks: []Block{
		{ID: blockID(0, keptHTML), Type: BlockP, HTML: keptHTML},
		{ID: blockID(0, wrapperHTML), Type: BlockP, HTML: wrapperHTML},
	}}}}
	translated := makeBlankTranslated(stale)
	translated.Chapters[0].Blocks[0].Status = BlockDone
	translated.Chapters[0].Blocks[0].HTML = "<p>保留。</p>"
	translated.Chapters[0].Blocks[1].Status = BlockError
	if err := app.store.SaveSource(storyID, source); err != nil {
		t.Fatal(err)
	}

	original, next, repaired, err := app.repairOversizedOriginal(storyID, stale, &translated)
	if err != nil || !repaired {
		t.Fatalf("repaired = %v, err = %v", repaired, err)
	}
	blocks := original.Chapters[0].Blocks
	if len(blocks) != 301 {
		t.Fatalf("blocks = %d", len(blocks))
	}
	if got := next.Chapters[0].Blocks[0]; got.Status != BlockDone || got.HTML != "<p>保留。</p>" {
		t.Fatalf("translation lost: %#v", got)
	}
	if got := next.Chapters[0].Blocks[1]; got.Status != BlockPending {
		t.Fatalf("new block status = %s", got.Status)
	}
	stored, err := app.store.LoadOriginal(storyID)
	if err != nil || stored == nil || len(stored.Chapters[0].Blocks) != 301 {
		t.Fatalf("stored original not replaced: %v", err)
	}

	_, _, repairedAgain, err := app.repairOversizedOriginal(storyID, *original, next)
	if err != nil || repairedAgain {
		t.Fatalf("second repair = %v, err = %v", repairedAgain, err)
	}
}

func TestRetryableLLMErrorCoversResetHTTP2Streams(t *testing.T) {
	err := fmt.Errorf("post: %w", errors.New("stream error: stream ID 3; INTERNAL_ERROR; received from peer"))
	if _, ok := retryableLLMError(err); !ok {
		t.Fatal("HTTP/2 stream reset should be retryable")
	}
}

func TestExtractBlocksSplitsCJKTextWithoutSpaces(t *testing.T) {
	blocks := parseTestBlocks(t, "<p>"+strings.Repeat("这是一个没有空格的中文句子。", 200)+"</p>")
	if len(blocks) < 2 {
		t.Fatalf("CJK paragraph was not split: %d blocks", len(blocks))
	}
	assertBlocksWithinLimit(t, blocks)
	for i, block := range blocks {
		if text := htmlToPlainText(block.HTML); !strings.HasSuffix(strings.TrimSpace(text), "。") {
			t.Fatalf("piece %d does not end at a sentence", i)
		}
	}
}
