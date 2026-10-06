package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRepairModelJSONEscapesInnerQuotes(t *testing.T) {
	// The reply that broke story 59788516: dialogue quoted with ASCII quotes
	// inside a Chinese string value.
	content := `{"summary":"奥斯卡说"我不走"，兰多笑了。","tone":"fluff, "slow burn"","ships":["A/B"],"glossary":{"Oscar":"奥斯卡"}}`
	got, err := decodeLenientJSON[analysisReply](content)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != `奥斯卡说"我不走"，兰多笑了。` || got.Tone != `fluff, "slow burn"` || got.Glossary["Oscar"] != "奥斯卡" {
		t.Fatalf("got = %+v", got)
	}
}

func TestRepairModelJSONKeepsValidJSONUntouched(t *testing.T) {
	valid := `{"a":"x, \"y\"","b":["c","d"],"e":{"f":1}}`
	if got := repairModelJSON(valid); got != valid {
		t.Fatalf("repair changed valid JSON: %s", got)
	}
}

func TestDecodeLenientJSONReportsUnrepairableReply(t *testing.T) {
	if _, err := decodeLenientJSON[analysisReply](`{"summary": 很长的摘要`); err == nil {
		t.Fatal("expected truncated reply to fail")
	}
}

func TestExtractSegments(t *testing.T) {
	content := "好的：\n<seg id=\"1\">他说 \"走吧\" &amp; 笑了</seg>\n<SEG id='2.1'> 我 </SEG>\n<seg id=2.2>不能</seg>\n<seg id=\"1\">重复</seg>\n<seg id=\"3\">没有闭合\n<seg id=\"4\">被截断"
	got := extractSegments(content)
	want := map[string]string{"1": `他说 "走吧" & 笑了`, "2.1": "我", "2.2": "不能"}
	if len(got) != len(want) {
		t.Fatalf("segments = %#v", got)
	}
	for id, text := range want {
		if got[id] != text {
			t.Fatalf("seg %s = %q, want %q", id, got[id], text)
		}
	}
}

func TestParseSegmentResponseIsolatesBrokenBlocks(t *testing.T) {
	plain, err := makeTranslateInput(Block{ID: "plain", Type: BlockP, HTML: `<p>Hello.</p>`})
	if err != nil {
		t.Fatal(err)
	}
	formatted, err := makeTranslateInput(Block{ID: "formatted", Type: BlockP, HTML: `<p>I <em>can't</em> leave.</p>`})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := makeTranslateInput(Block{ID: "empty", Type: BlockP, HTML: `<p>Bye.</p>`})
	if err != nil {
		t.Fatal(err)
	}
	inputs := []translateInput{plain, formatted, empty, formatted}
	content := "<seg id=\"1\">你好。</seg>\n<seg id=\"2.1\">我</seg>\n<seg id=\"2.3\">离开。</seg>\n<seg id=\"3\">  </seg>\n<seg id=\"4.1\">我</seg><seg id=\"4.2\">不能</seg><seg id=\"4.3\">离开。</seg>"
	outs, failures := parseSegmentResponse(content, inputs, false)
	if len(outs) != 2 || outs[0].ID != "plain" || outs[0].HTML != "<p>你好。</p>" || outs[1].HTML != "<p>我<em>不能</em>离开。</p>" {
		t.Fatalf("outs = %+v", outs)
	}
	if len(failures) != 2 || failures[0].ID != "formatted" || failures[1].ID != "empty" {
		t.Fatalf("failures = %+v", failures)
	}
}

func TestBuildUserPayloadUsesShortAliases(t *testing.T) {
	plain, _ := makeTranslateInput(Block{ID: strings.Repeat("a", 32), Type: BlockP, HTML: `<p>Hello.</p>`})
	formatted, _ := makeTranslateInput(Block{ID: strings.Repeat("b", 32), Type: BlockP, HTML: `<p>I <em>can't</em> leave.</p>`})
	payload, err := buildUserPayload(Meta{Title: "Work"}, []translateInput{plain, formatted}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "aaaa") || strings.Contains(payload, `"r0"`) {
		t.Fatalf("payload leaked internal ids: %s", payload)
	}
	var parsed struct {
		Blocks []promptBlock `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Blocks[0].ID != "1" || parsed.Blocks[0].Runs != nil {
		t.Fatalf("plain block = %+v", parsed.Blocks[0])
	}
	if parsed.Blocks[1].ID != "2" || len(parsed.Blocks[1].Runs) != 3 || parsed.Blocks[1].Runs[2].ID != "2.3" {
		t.Fatalf("formatted block = %+v", parsed.Blocks[1])
	}
}

func TestTranslatePassRetriesOnlyMissingBlocks(t *testing.T) {
	var mu sync.Mutex
	requests := [][]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		blocks, err := translationTestPayload(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		texts := make([]string, len(blocks))
		for i, block := range blocks {
			texts[i] = block.Text
		}
		requests = append(requests, texts)
		first := len(requests) == 1
		mu.Unlock()
		if first {
			// Drop the last block, as a model that stops early would.
			writeChatContent(w, translationTestSegments(blocks[:len(blocks)-1]))
			return
		}
		writeChatContent(w, translationTestSegments(blocks))
	}))
	defer server.Close()

	app, storyID, original, translated := newTranslationTestState(t, []Block{
		{ID: "b1", Type: BlockP, HTML: "<p>one</p>"},
		{ID: "b2", Type: BlockP, HTML: "<p>two</p>"},
		{ID: "b3", Type: BlockP, HTML: "<p>three</p>"},
	})
	cfg := translationTestConfig(server.URL)
	cfg.LLM.BlocksPerRequest = 3
	cfg.LLM.MaxAutoRetries = 1
	if err := app.translatePass(context.Background(), storyID, cfg, Meta{ID: storyID}, original, translated, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || len(requests[1]) != 1 || requests[1][0] != "three" {
		t.Fatalf("requests = %v", requests)
	}
	stored, err := app.store.LoadTranslated(storyID)
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range stored.Chapters[0].Blocks {
		if block.Status != BlockDone || !strings.Contains(block.HTML, "译:") {
			t.Fatalf("block = %+v", block)
		}
	}
}

func TestTranslatePassRetriesUnparseableReplyThenReportsIt(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeChatContent(w, `{"blocks":[{"id":"1","text":"不是 seg 格式"}]}`)
	}))
	defer server.Close()

	app, storyID, original, translated := newTranslationTestState(t, []Block{{ID: "b1", Type: BlockP, HTML: "<p>one</p>"}})
	cfg := translationTestConfig(server.URL)
	cfg.LLM.MaxAutoRetries = 2
	retryBase := retryBaseDelay
	retryBaseDelay = 0
	defer func() { retryBaseDelay = retryBase }()
	if err := app.translatePass(context.Background(), storyID, cfg, Meta{ID: storyID}, original, translated, nil, nil); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 3 {
		t.Fatalf("requests = %d, want 3", requests.Load())
	}
	stored, err := app.store.LoadTranslated(storyID)
	if err != nil {
		t.Fatal(err)
	}
	if block := stored.Chapters[0].Blocks[0]; block.Status != BlockError || !strings.Contains(block.Error, "模型输出格式无效") {
		t.Fatalf("block = %+v", block)
	}
}

type analysisTestRequest struct {
	System   string
	Messages []ChatMessage
	Chapter  *int
}

func decodeAnalysisTestRequest(r *http.Request) (analysisTestRequest, error) {
	var body struct {
		Messages []ChatMessage `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return analysisTestRequest{}, err
	}
	out := analysisTestRequest{Messages: body.Messages, System: body.Messages[0].Content}
	var payload struct {
		Chapter *struct {
			Index int `json:"index"`
		} `json:"chapter"`
	}
	if err := json.Unmarshal([]byte(body.Messages[1].Content), &payload); err == nil && payload.Chapter != nil {
		out.Chapter = &payload.Chapter.Index
	}
	return out, nil
}

func TestAnalyzeFullTextFeedsParseErrorBackOnRetry(t *testing.T) {
	var calls atomic.Int32
	var retry analysisTestRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeAnalysisTestRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if calls.Add(1) == 1 {
			writeChatContent(w, `{"summary":"截断的摘要`)
			return
		}
		retry = req
		writeChatContent(w, `{"summary":"全文","chapterSummaries":[{"index":0,"summary":"c0"}]}`)
	}))
	defer server.Close()
	meta, original, cfg := analysisTestInputs()
	cfg.LLM.BaseURL = server.URL
	cfg.LLM.MaxAutoRetries = 2
	retryBase := retryBaseDelay
	retryBaseDelay = 0
	defer func() { retryBaseDelay = retryBase }()

	got, err := analyzeFullText(context.Background(), cfg, meta, original, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "全文" || calls.Load() != 2 {
		t.Fatalf("got = %+v, calls = %d", got, calls.Load())
	}
	if len(retry.Messages) != 4 || retry.Messages[2].Role != "assistant" || retry.Messages[2].Content != `{"summary":"截断的摘要` || !strings.Contains(retry.Messages[3].Content, "无法被程序解析") {
		t.Fatalf("retry messages = %+v", retry.Messages)
	}
}

func TestRunAnalysisResumesChapteredAnalysisFromCachedPartials(t *testing.T) {
	meta, original, cfg := analysisTestInputs()
	original.Chapters = []Chapter{
		{Index: 0, Title: "C0", Blocks: []Block{{ID: "a", Type: BlockP, HTML: "<p>zero</p>"}}},
		{Index: 1, Title: "C1", Blocks: []Block{{ID: "b", Type: BlockP, HTML: "<p>one</p>"}}},
	}
	var mu sync.Mutex
	chapterCalls := map[int]int{}
	failChapterOne := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeAnalysisTestRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch {
		case req.Chapter != nil:
			chapterCalls[*req.Chapter]++
			if *req.Chapter == 1 && failChapterOne {
				writeChatContent(w, "抱歉，我无法输出 JSON。")
				return
			}
			writeChatContent(w, fmt.Sprintf(`{"summary":"章%d摘要","glossary":{"Name%d":"名%d"}}`, *req.Chapter, *req.Chapter, *req.Chapter))
		case strings.Contains(req.System, "归并"):
			writeChatContent(w, `{"summary":"归并全文","glossary":{"Name0":"名0"}}`)
		default:
			http.Error(w, "unexpected full-text request", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	cfg.LLM.BaseURL = server.URL
	cfg.LLM.AnalysisMaxInputTokens = 1
	cfg.LLM.MaxAutoRetries = 0
	app := newAnalysisRunTestApp(t, meta, original)
	if err := app.store.SaveOriginal(meta.ID, original); err != nil {
		t.Fatal(err)
	}

	_, err := app.runAnalysis(context.Background(), meta.ID, meta, original, cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "1/2 章分析失败") || !isOutputFormatError(err) {
		t.Fatalf("first run error = %v", err)
	}
	partials, err := app.store.LoadAnalysisPartials(meta.ID)
	if err != nil || partials == nil || len(partials.Chapters) != 1 || partials.Chapters[0].Index != 0 {
		t.Fatalf("partials = %+v, err = %v", partials, err)
	}

	mu.Lock()
	failChapterOne = false
	mu.Unlock()
	got, err := app.runAnalysis(context.Background(), meta.ID, meta, original, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if chapterCalls[0] != 1 || chapterCalls[1] != 2 {
		t.Fatalf("chapter calls = %v", chapterCalls)
	}
	if got.Summary != "归并全文" || len(got.ChapterSummaries) != 2 || got.ChapterSummaries[1].Summary != "章1摘要" || got.ChapterSummaries[1].Title != "C1" {
		t.Fatalf("context = %+v", got)
	}
	if left, err := app.store.LoadAnalysisPartials(meta.ID); err != nil || left != nil {
		t.Fatalf("partials left behind: %+v, %v", left, err)
	}
}

func TestShouldFallBackToChapters(t *testing.T) {
	if shouldFallBackToChapters(LLMError{Status: http.StatusUnauthorized}) || shouldFallBackToChapters(context.Canceled) {
		t.Fatal("auth and cancellation must not fan out per chapter")
	}
	if !shouldFallBackToChapters(asOutputFormatError(errors.New("bad"))) || !shouldFallBackToChapters(LLMError{Status: http.StatusBadRequest}) {
		t.Fatal("format and context-length failures should fall back")
	}
}

func TestDecodeLenientJSONEdgeCases(t *testing.T) {
	// A raw line break inside a long summary.
	got, err := decodeLenientJSON[analysisReply]("{\"summary\":\"第一段\n第二段\",\"tone\":\"t\"}")
	if err != nil || got.Summary != "第一段\n第二段" || got.Tone != "t" {
		t.Fatalf("newline: %+v, %v", got, err)
	}
	// A lead-in sentence with its own brace.
	got, err = decodeLenientJSON[analysisReply]("以下是结果 {注意}:\n{\"summary\":\"s\"}")
	if err != nil || got.Summary != "s" {
		t.Fatalf("lead-in brace: %+v, %v", got, err)
	}
	// A repair that would cut the object short must fail, not drop fields.
	if got, err := decodeLenientJSON[analysisReply](`{"summary":"他说"好"}, 然后","tone":"x"}`); err == nil {
		t.Fatalf("expected misrepaired reply to fail, got %+v", got)
	}
	// Syntax errors read as text, not as a stray byte of a Chinese character.
	_, err = decodeLenientJSON[analysisReply](`{"summary": 甲乙}`)
	if err == nil || strings.Contains(err.Error(), "ç") || !strings.Contains(err.Error(), "附近") {
		t.Fatalf("error = %v", err)
	}
	if _, err := decodeLenientJSON[analysisReply](`{"summary":"截断`); err == nil || !strings.Contains(err.Error(), "不完整") {
		t.Fatalf("truncated error = %v", err)
	}
}

func TestExtractSegmentsToleratesTagVariants(t *testing.T) {
	got := extractSegments("<seg id=\" 1 \">一</seg>\n<seg id=“2”>二</seg>\n<seg id=\\\"3\\\">三<\\/seg>")
	if got["1"] != "一" || got["2"] != "二" || got["3"] != "三" {
		t.Fatalf("segments = %#v", got)
	}
}

func TestParseSegmentResponseKeepsSpacesBetweenLatinRuns(t *testing.T) {
	input, err := makeTranslateInput(Block{ID: "b", Type: BlockP, HTML: `<p><em>Draco</em> Malfoy said <b>no</b>.</p>`})
	if err != nil {
		t.Fatal(err)
	}
	outs, failures := parseSegmentResponse("<seg id=\"1.1\">Draco</seg><seg id=\"1.2\">Malfoy 说</seg><seg id=\"1.3\">不</seg><seg id=\"1.4\">。</seg>", []translateInput{input}, false)
	if len(failures) != 0 || outs[0].HTML != "<p><em>Draco</em> Malfoy 说<b>不</b>。</p>" {
		t.Fatalf("outs = %+v, failures = %+v", outs, failures)
	}
}

func TestParseSegmentResponseFlattensOnFinalAttempt(t *testing.T) {
	input, err := makeTranslateInput(Block{ID: "b", Type: BlockP, HTML: `<p class="x">I <em>can't</em> leave.</p>`})
	if err != nil {
		t.Fatal(err)
	}
	reply := "<seg id=\"1\">我不能离开。</seg>"
	if outs, failures := parseSegmentResponse(reply, []translateInput{input}, false); len(outs) != 0 || len(failures) != 1 {
		t.Fatalf("non-final attempt accepted a whole-block seg: %+v", outs)
	}
	outs, failures := parseSegmentResponse(reply, []translateInput{input}, true)
	if len(failures) != 0 || outs[0].HTML != `<p>我不能离开。<em></em></p>` {
		t.Fatalf("outs = %+v, failures = %+v", outs, failures)
	}
}

func TestDecodeLenientJSONNeverPicksNestedObject(t *testing.T) {
	if got, err := decodeLenientJSON[analysisReply](`{"summary": 坏, "glossary":{"A":"甲"}}`); err == nil {
		t.Fatalf("decoded nested object as the reply: %+v", got)
	}
}
