package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type temporaryTranslationError struct{}

func (temporaryTranslationError) Error() string   { return "temporary network error" }
func (temporaryTranslationError) Timeout() bool   { return false }
func (temporaryTranslationError) Temporary() bool { return true }

type translationTestBlock struct {
	ID   string         `json:"id"`
	Text string         `json:"text"`
	Runs []translateRun `json:"runs"`
}

func translationTestPayload(r *http.Request) ([]translationTestBlock, error) {
	var request struct {
		Messages []ChatMessage `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return nil, err
	}
	if len(request.Messages) == 0 {
		return nil, errors.New("missing messages")
	}
	var payload struct {
		Blocks []translationTestBlock `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(request.Messages[len(request.Messages)-1].Content), &payload); err != nil {
		return nil, err
	}
	return payload.Blocks, nil
}

// translationTestSegments answers like a well-behaved model: one <seg> per
// plain block, one per run for formatted blocks.
func translationTestSegments(blocks []translationTestBlock) string {
	var b strings.Builder
	for _, block := range blocks {
		if len(block.Runs) == 0 {
			fmt.Fprintf(&b, "<seg id=\"%s\">译:%s</seg>\n", block.ID, block.Text)
			continue
		}
		for _, run := range block.Runs {
			fmt.Fprintf(&b, "<seg id=\"%s\">译:%s</seg>\n", run.ID, run.Text)
		}
	}
	return b.String()
}

func translationTestResponse(r *http.Request) (string, error) {
	blocks, err := translationTestPayload(r)
	if err != nil {
		return "", err
	}
	return translationTestSegments(blocks), nil
}

func writeChatContent(w http.ResponseWriter, content string) {
	writeJSON(w, http.StatusOK, map[string]any{
		"choices": []map[string]any{{"message": map[string]string{"content": content}}},
	})
}

func serveTranslationTestResponse(w http.ResponseWriter, r *http.Request) {
	content, err := translationTestResponse(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"choices": []map[string]any{{"message": map[string]string{"content": content}}},
	})
}

func newTranslationTestState(t *testing.T, blocks []Block) (*App, string, *ChapterFile, *ChapterFile) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storyID := "story"
	original := &ChapterFile{Chapters: []Chapter{{Index: 0, Title: "Chapter", Blocks: blocks}}}
	translatedValue := makeBlankTranslated(*original)
	translated := &translatedValue
	if err := store.SaveOriginal(storyID, *original); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTranslated(storyID, *translated); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveProgress(storyID, Progress{Phase: PhaseTranslating, StartedAt: nowISO(), Errors: []ProgressError{}}); err != nil {
		t.Fatal(err)
	}
	app := &App{
		store:    store,
		bus:      NewEventBus(),
		ctx:      context.Background(),
		inflight: map[string]map[string]bool{},
	}
	return app, storyID, original, translated
}

func translationTestConfig(baseURL string) Config {
	cfg := defaultConfig()
	cfg.LLM.APIKey = "test-key"
	cfg.LLM.BaseURL = baseURL
	cfg.LLM.Model = "test-model"
	cfg.LLM.Mode = TranslationModeNormal
	cfg.LLM.Stream = false
	cfg.LLM.BlocksPerRequest = 1
	cfg.LLM.MaxTokensPerRequest = 1000
	cfg.LLM.MaxAutoRetries = 0
	return cfg
}

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

func TestTranslatedHTMLFromRunsRejectsEmptyRun(t *testing.T) {
	input, err := makeTranslateInput(Block{ID: "b1", Type: BlockP, HTML: `<p>Hello.</p>`})
	if err != nil {
		t.Fatal(err)
	}
	_, err = translatedHTMLFromRuns(input, translateOutput{
		ID:   "b1",
		Runs: []translateRun{{ID: "r0", Text: "  "}},
	})
	if err == nil || !strings.Contains(err.Error(), "译文为空") {
		t.Fatalf("empty-run error = %v", err)
	}
}

func TestStoreUpdateProgressDoesNotLoseConcurrentAppends(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const storyID = "story"
	if err := store.SaveProgress(storyID, Progress{Phase: PhaseTranslating, Errors: []ProgressError{}}); err != nil {
		t.Fatal(err)
	}

	const updates = 64
	errs := make(chan error, updates)
	var wg sync.WaitGroup
	for i := 0; i < updates; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- store.UpdateProgress(storyID, func(progress Progress) Progress {
				progress.Errors = append(progress.Errors, ProgressError{BlockID: fmt.Sprintf("b%d", i)})
				return progress
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	progress, err := store.LoadProgress(storyID)
	if err != nil {
		t.Fatal(err)
	}
	if progress == nil || len(progress.Errors) != updates {
		t.Fatalf("progress errors = %d, want %d", len(progress.Errors), updates)
	}
}

func TestWithRetryPolicy(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		retries      int
		wantAttempts int
		wantSleeps   int
	}{
		{name: "403", err: LLMError{Status: http.StatusForbidden}, retries: 4, wantAttempts: 1},
		{name: "404", err: LLMError{Status: http.StatusNotFound}, retries: 4, wantAttempts: 1},
		{name: "schema", err: errors.New("schema mismatch"), retries: 4, wantAttempts: 1},
		{name: "408", err: LLMError{Status: http.StatusRequestTimeout}, retries: 2, wantAttempts: 3, wantSleeps: 2},
		{name: "429", err: LLMError{Status: http.StatusTooManyRequests, RetryAfter: 2 * time.Second}, retries: 2, wantAttempts: 3, wantSleeps: 2},
		{name: "500", err: LLMError{Status: http.StatusInternalServerError}, retries: 2, wantAttempts: 3, wantSleeps: 2},
		{name: "pointer 503", err: &LLMError{Status: http.StatusServiceUnavailable}, retries: 2, wantAttempts: 3, wantSleeps: 2},
		{name: "temporary network", err: temporaryTranslationError{}, retries: 2, wantAttempts: 3, wantSleeps: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			sleeps := []time.Duration{}
			_, err := withRetrySleep(context.Background(), func(_ int) (int, error) {
				attempts++
				return 0, test.err
			}, test.retries, func(_ context.Context, delay time.Duration) error {
				sleeps = append(sleeps, delay)
				return nil
			})
			if !errors.Is(err, test.err) && err.Error() != test.err.Error() {
				t.Fatalf("error = %v, want %v", err, test.err)
			}
			if attempts != test.wantAttempts || len(sleeps) != test.wantSleeps {
				t.Fatalf("attempts/sleeps = %d/%d, want %d/%d", attempts, len(sleeps), test.wantAttempts, test.wantSleeps)
			}
			if test.name == "429" {
				for _, delay := range sleeps {
					if delay < 2*time.Second {
						t.Fatalf("Retry-After delay = %s", delay)
					}
				}
			}
		})
	}
}

func TestWithRetryCapsRetryAfter(t *testing.T) {
	var slept time.Duration
	_, err := withRetrySleep(context.Background(), func(_ int) (int, error) {
		return 0, LLMError{Status: http.StatusTooManyRequests, RetryAfter: retryMaxDelay + time.Minute}
	}, 1, func(_ context.Context, delay time.Duration) error {
		slept = delay
		return nil
	})
	if err == nil {
		t.Fatal("expected retry error")
	}
	if slept != retryMaxDelay {
		t.Fatalf("retry delay = %s, want %s", slept, retryMaxDelay)
	}
}

func TestWithRetryBackoffIsCancelable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error, 1)
	var attempts atomic.Int32
	go func() {
		_, err := withRetry(ctx, func(_ int) (int, error) {
			attempts.Add(1)
			close(started)
			return 0, LLMError{Status: http.StatusServiceUnavailable}
		}, 3)
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context cancellation", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("retry backoff did not stop after cancellation")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestTranslatePassConcurrentWorkersPersistAllResults(t *testing.T) {
	const workers = 4
	started := make(chan struct{})
	release := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == workers {
			close(started)
		}
		<-release
		serveTranslationTestResponse(w, r)
	}))
	defer server.Close()

	blocks := make([]Block, workers)
	for i := range blocks {
		blocks[i] = Block{ID: fmt.Sprintf("b%d", i), Type: BlockP, HTML: fmt.Sprintf("<p>text %d</p>", i)}
	}
	app, storyID, original, translated := newTranslationTestState(t, blocks)
	cfg := translationTestConfig(server.URL)
	cfg.LLM.Concurrency = workers
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.translatePass(context.Background(), storyID, cfg, Meta{ID: storyID, Title: "Work"}, original, translated, nil, nil)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("workers did not reach provider concurrently")
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	stored, err := app.store.LoadTranslated(storyID)
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range stored.Chapters[0].Blocks {
		if block.Status != BlockDone || block.HTML == "" {
			t.Fatalf("block not durably translated: %+v", block)
		}
	}
	progress, err := app.store.LoadProgress(storyID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.DoneBlocks != workers || progress.ErrorBlocks != 0 || progress.InflightBlocks != 0 {
		t.Fatalf("progress = %+v", progress)
	}
}

func TestTranslatePassRejectsOversizedSingleBlock(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		serveTranslationTestResponse(w, r)
	}))
	defer server.Close()

	app, storyID, original, translated := newTranslationTestState(t, []Block{{ID: "large", Type: BlockP, HTML: `<p>This block is intentionally much too large.</p>`}})
	cfg := translationTestConfig(server.URL)
	cfg.LLM.MaxTokensPerRequest = 1
	if err := app.translatePass(context.Background(), storyID, cfg, Meta{ID: storyID}, original, translated, nil, nil); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatalf("provider requests = %d, want 0", requests.Load())
	}
	stored, err := app.store.LoadTranslated(storyID)
	if err != nil {
		t.Fatal(err)
	}
	block := stored.Chapters[0].Blocks[0]
	if block.Status != BlockError || !strings.Contains(block.Error, "超过单次请求上限") {
		t.Fatalf("oversized block = %+v", block)
	}
}

func TestRunTranslationPersistenceFailureNeverPublishesReady(t *testing.T) {
	for _, failedFile := range []string{"translated.json", "progress.json"} {
		t.Run(failedFile, func(t *testing.T) {
			app, storyID, original, _ := newTranslationTestState(t, []Block{{ID: "b1", Type: BlockP, HTML: `<p>Hello.</p>`}})
			var once sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				once.Do(func() {
					tmpPath := filepath.Join(app.store.dir, "stories", storyID, failedFile+".tmp")
					if err := os.Mkdir(tmpPath, 0o700); err != nil {
						t.Errorf("create persistence failure: %v", err)
					}
				})
				serveTranslationTestResponse(w, r)
			}))
			defer server.Close()

			cfg := translationTestConfig(server.URL)
			if err := app.store.SaveConfig(cfg); err != nil {
				t.Fatal(err)
			}
			meta := Meta{ID: storyID, URL: "https://archiveofourown.org/works/1", Title: "Work", Author: "Author", ChapterCount: 1, TranslationMode: TranslationModeNormal}
			if err := app.store.SaveMeta(storyID, meta); err != nil {
				t.Fatal(err)
			}
			if err := app.store.UpsertIndex(indexEntryFor(meta, StatusQueued)); err != nil {
				t.Fatal(err)
			}
			if err := app.store.SaveOriginal(storyID, *original); err != nil {
				t.Fatal(err)
			}
			events, unsubscribe := app.bus.Subscribe(storyID)
			defer unsubscribe()

			if err := app.runTranslation(context.Background(), storyID); err == nil {
				t.Fatal("expected persistence failure")
			}
			progress, err := app.store.LoadProgress(storyID)
			if err != nil {
				t.Fatal(err)
			}
			if progress.Phase == PhaseReady {
				t.Fatalf("progress reached Ready after %s failure", failedFile)
			}
			index, err := app.store.LoadIndex()
			if err != nil {
				t.Fatal(err)
			}
			if len(index.Stories) != 1 || index.Stories[0].Status == StatusReady {
				t.Fatalf("index reached Ready after %s failure: %+v", failedFile, index)
			}
			for {
				select {
				case event := <-events:
					if event.Type == "block-done" || (event.Type == "phase" && event.Phase == PhaseReady) {
						t.Fatalf("published success after %s failure: %+v", failedFile, event)
					}
				default:
					return
				}
			}
		})
	}
}
