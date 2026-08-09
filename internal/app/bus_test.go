package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func newLifecycleTestApp(t *testing.T) *App {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	app := &App{
		store:    store,
		bus:      NewEventBus(),
		ctx:      ctx,
		cancel:   cancel,
		inflight: map[string]map[string]bool{},
	}
	app.queue = NewQueue(app)
	t.Cleanup(app.Close)
	return app
}

func TestQueueDeduplicatesPendingAndRunningStory(t *testing.T) {
	app := newLifecycleTestApp(t)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondDone := make(chan struct{})
	var firstCalls atomic.Int32
	var secondCalls atomic.Int32
	app.queue.run = func(_ context.Context, job Job) error {
		switch job.StoryID {
		case "first":
			if firstCalls.Add(1) == 1 {
				close(firstStarted)
			}
			<-releaseFirst
		case "second":
			if secondCalls.Add(1) == 1 {
				close(secondDone)
			}
		}
		return nil
	}

	app.queue.Enqueue(Job{StoryID: "first", Type: "translate"})
	<-firstStarted
	for i := 0; i < 32; i++ {
		app.queue.Enqueue(Job{StoryID: "first", Type: "translate"})
		app.queue.Enqueue(Job{StoryID: "second", Type: "translate"})
	}
	close(releaseFirst)
	<-secondDone
	app.queue.CancelAndWait("second")

	if got := firstCalls.Load(); got != 1 {
		t.Fatalf("first story calls = %d, want 1", got)
	}
	if got := secondCalls.Load(); got != 1 {
		t.Fatalf("second story calls = %d, want 1", got)
	}
}

func TestQueueCancelRemovesPendingAndWaitsForCleanup(t *testing.T) {
	app := newLifecycleTestApp(t)
	started := make(chan struct{})
	canceled := make(chan struct{})
	allowCleanup := make(chan struct{})
	returned := make(chan struct{})
	var pendingCalls atomic.Int32
	app.queue.run = func(ctx context.Context, job Job) error {
		if job.StoryID == "pending" {
			pendingCalls.Add(1)
			return nil
		}
		close(started)
		<-ctx.Done()
		close(canceled)
		<-allowCleanup
		return ctx.Err()
	}

	app.queue.Enqueue(Job{StoryID: "running", Type: "translate"})
	<-started
	app.queue.Enqueue(Job{StoryID: "pending", Type: "translate"})
	app.queue.CancelAndWait("pending")
	go func() {
		app.queue.CancelAndWait("running")
		close(returned)
	}()
	<-canceled
	select {
	case <-returned:
		t.Fatal("CancelAndWait returned before runner cleanup")
	default:
	}
	close(allowCleanup)
	<-returned
	if got := pendingCalls.Load(); got != 0 {
		t.Fatalf("removed pending story ran %d times", got)
	}
}

func TestDeleteStoryWaitsForCanceledJobBeforeRemovingData(t *testing.T) {
	app := newLifecycleTestApp(t)
	const storyID = "story"
	meta := Meta{ID: storyID, Title: "Work"}
	if err := app.store.SaveMeta(storyID, meta); err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveProgress(storyID, Progress{Phase: PhaseTranslating, Errors: []ProgressError{}}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.UpsertIndex(indexEntryFor(meta, StatusTranslating)); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	staleWriteDone := make(chan struct{})
	app.queue.run = func(ctx context.Context, _ Job) error {
		close(started)
		<-ctx.Done()
		if err := app.store.SaveMeta(storyID, Meta{ID: storyID, Title: "stale"}); err != nil {
			t.Errorf("stale runner write: %v", err)
		}
		close(staleWriteDone)
		return ctx.Err()
	}
	app.queue.Enqueue(Job{StoryID: storyID, Type: "translate"})
	<-started

	if err := app.DeleteStory(storyID); err != nil {
		t.Fatal(err)
	}
	<-staleWriteDone
	if app.store.StoryExists(storyID) {
		t.Fatal("canceled job recreated deleted story")
	}
	index, err := app.store.LoadIndex()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range index.Stories {
		if entry.ID == storyID {
			t.Fatalf("deleted story remains in index: %+v", entry)
		}
	}
}

func TestStoryMutationCancelsWorkerBeforeEnteringGate(t *testing.T) {
	app := newLifecycleTestApp(t)
	started := make(chan struct{})
	releaseCleanup := make(chan struct{})
	var active atomic.Int32
	var maxActive atomic.Int32
	recordActive := func() {
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				return
			}
		}
	}
	app.queue.run = func(ctx context.Context, _ Job) error {
		recordActive()
		close(started)
		<-ctx.Done()
		<-releaseCleanup
		active.Add(-1)
		return ctx.Err()
	}
	app.queue.Enqueue(Job{StoryID: "story", Type: "translate"})
	<-started

	mutationDone := make(chan error, 1)
	go func() {
		mutationDone <- app.withExclusiveStory("story", true, func() error {
			recordActive()
			active.Add(-1)
			return nil
		})
	}()
	close(releaseCleanup)
	if err := <-mutationDone; err != nil {
		t.Fatal(err)
	}
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("worker and mutation overlapped; max active = %d", got)
	}
	app.storyMu.Lock()
	gateCount := len(app.storyGates)
	app.storyMu.Unlock()
	if gateCount != 0 {
		t.Fatalf("released story gates retained = %d", gateCount)
	}
}

func seedRetryableStory(t *testing.T, app *App, storyID string) {
	t.Helper()
	meta := Meta{ID: storyID, Title: "Work", TranslationMode: TranslationModeNormal}
	original := ChapterFile{Chapters: []Chapter{{Index: 0, Blocks: []Block{{ID: "block", Type: BlockP, HTML: "<p>source</p>"}}}}}
	translated := ChapterFile{Chapters: []Chapter{{Index: 0, Blocks: []Block{{ID: "block", Type: BlockP, Status: BlockError, Error: "failed"}}}}}
	if err := app.store.SaveMeta(storyID, meta); err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveOriginal(storyID, original); err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveTranslated(storyID, translated); err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveProgress(storyID, Progress{Phase: PhaseError, Errors: []ProgressError{}}); err != nil {
		t.Fatal(err)
	}
	if err := app.store.UpsertIndex(indexEntryFor(meta, StatusError)); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentRetriesCancelSupersededJobsWithoutDeadlock(t *testing.T) {
	app := newLifecycleTestApp(t)
	const storyID = "story"
	seedRetryableStory(t, app, storyID)
	app.queue.run = func(ctx context.Context, _ Job) error {
		<-ctx.Done()
		return ctx.Err()
	}

	const retries = 16
	start := make(chan struct{})
	errs := make(chan error, retries)
	for i := 0; i < retries; i++ {
		go func() {
			<-start
			errs <- app.RetryStory(storyID, nil, nil, "")
		}()
	}
	close(start)
	for i := 0; i < retries; i++ {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent retry deadlocked behind a superseded translation")
		}
	}
	app.queue.CancelAndWait(storyID)
	app.storyMu.Lock()
	gateCount := len(app.storyGates)
	app.storyMu.Unlock()
	if gateCount != 0 {
		t.Fatalf("released retry gates retained = %d", gateCount)
	}
}

func TestConcurrentRetryAndDeleteCannotReviveStory(t *testing.T) {
	app := newLifecycleTestApp(t)
	const storyID = "story"
	seedRetryableStory(t, app, storyID)
	workerStarted := make(chan struct{})
	var startOnce atomic.Bool
	app.queue.run = func(ctx context.Context, _ Job) error {
		if startOnce.CompareAndSwap(false, true) {
			close(workerStarted)
		}
		<-ctx.Done()
		return ctx.Err()
	}
	app.queue.Enqueue(Job{StoryID: storyID, Type: "translate"})
	<-workerStarted

	start := make(chan struct{})
	retryErr := make(chan error, 1)
	deleteErr := make(chan error, 1)
	go func() {
		<-start
		retryErr <- app.RetryStory(storyID, nil, nil, "")
	}()
	go func() {
		<-start
		deleteErr <- app.DeleteStory(storyID)
	}()
	close(start)
	select {
	case err := <-deleteErr:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delete deadlocked with retry")
	}
	select {
	case err := <-retryErr:
		if err != nil && !errors.Is(err, errStoryNotFound) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("retry deadlocked with delete")
	}
	app.queue.CancelAndWait(storyID)
	if app.store.StoryExists(storyID) {
		t.Fatal("retry revived the deleted story")
	}
}

func TestRetryPersistsRestartableQueuedState(t *testing.T) {
	app := newLifecycleTestApp(t)
	const storyID = "story"
	seedRetryableStory(t, app, storyID)
	app.queue.run = func(ctx context.Context, _ Job) error {
		<-ctx.Done()
		return ctx.Err()
	}
	if err := app.RetryStory(storyID, nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	progress, err := app.store.LoadProgress(storyID)
	if err != nil {
		t.Fatal(err)
	}
	if progress == nil || progress.Phase != PhaseQueued || progress.FinishedAt != "" || progress.Message != "" {
		t.Fatalf("retry progress is not restartable: %+v", progress)
	}
	index, err := app.store.LoadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Stories) != 1 || index.Stories[0].Status != StatusQueued {
		t.Fatalf("retry index state = %+v", index)
	}
	app.queue.CancelAndWait(storyID)
}

func TestConcurrentReanalyzeKeepsOneRestartableJob(t *testing.T) {
	app := newLifecycleTestApp(t)
	const storyID = "story"
	seedRetryableStory(t, app, storyID)
	app.queue.run = func(ctx context.Context, _ Job) error {
		<-ctx.Done()
		return ctx.Err()
	}

	const requests = 16
	start := make(chan struct{})
	errs := make(chan error, requests)
	for i := 0; i < requests; i++ {
		go func() {
			<-start
			errs <- app.ReanalyzeStory(storyID)
		}()
	}
	close(start)
	for i := 0; i < requests; i++ {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent reanalysis deadlocked")
		}
	}
	progress, err := app.store.LoadProgress(storyID)
	if err != nil {
		t.Fatal(err)
	}
	if progress == nil || progress.Phase != PhaseQueued {
		t.Fatalf("reanalysis progress = %+v", progress)
	}
	meta, err := app.store.LoadMeta(storyID)
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil || meta.TranslationMode != TranslationModeRefined {
		t.Fatalf("reanalysis metadata = %+v", meta)
	}
	app.queue.CancelAndWait(storyID)
}

func TestCreateFromURLFailureLeavesTerminalErrorStatus(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	setAO3TestServers(t, server)
	app := newLifecycleTestApp(t)

	if _, err := app.CreateFromURL(server.URL+"/works/12345", ""); err == nil {
		t.Fatal("expected AO3 fetch failure")
	}
	index, err := app.store.LoadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Stories) != 1 || index.Stories[0].ID != "12345" || index.Stories[0].Status != StatusError {
		t.Fatalf("failed import index state = %+v", index)
	}
}

func TestAppCloseCancelsURLImport(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	setAO3TestServers(t, server)
	app := newLifecycleTestApp(t)
	done := make(chan error, 1)
	go func() {
		_, err := app.CreateFromURL(server.URL+"/works/12345", "")
		done <- err
	}()
	<-started
	app.Close()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("import error = %v, want context cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("App.Close did not cancel URL import")
	}
}

func TestStoreFinishStoryRollsBackProgressWhenIndexWriteFails(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const storyID = "story"
	meta := Meta{ID: storyID, Title: "Work"}
	if err := store.SaveMeta(storyID, meta); err != nil {
		t.Fatal(err)
	}
	previousProgress := Progress{Phase: PhaseTranslating, StartedAt: nowISO(), Errors: []ProgressError{}}
	if err := store.SaveProgress(storyID, previousProgress); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertIndex(indexEntryFor(meta, StatusTranslating)); err != nil {
		t.Fatal(err)
	}

	indexTemp := filepath.Join(store.dir, "index.json.tmp")
	if err := os.Mkdir(indexTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishStory(storyID, PhaseReady, StatusReady, ""); err == nil {
		t.Fatal("expected terminal index write failure")
	}
	if err := os.Remove(indexTemp); err != nil {
		t.Fatal(err)
	}

	progress, err := store.LoadProgress(storyID)
	if err != nil {
		t.Fatal(err)
	}
	if progress == nil || progress.Phase != PhaseTranslating || progress.FinishedAt != "" {
		t.Fatalf("progress was not rolled back: %+v", progress)
	}
	index, err := store.LoadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Stories) != 1 || index.Stories[0].Status != StatusTranslating {
		t.Fatalf("index changed after failed terminal update: %+v", index)
	}
}

func TestEventBusCloseAndUnsubscribeAreIdempotent(t *testing.T) {
	bus := NewEventBus()
	ch, unsubscribe := bus.Subscribe("story")
	unsubscribe()
	unsubscribe()
	if _, ok := <-ch; ok {
		t.Fatal("unsubscribed channel remains open")
	}

	ch, unsubscribe = bus.Subscribe("other")
	bus.Close()
	bus.Close()
	unsubscribe()
	if _, ok := <-ch; ok {
		t.Fatal("closed bus left subscriber open")
	}
	closed, unsubscribeClosed := bus.Subscribe("closed")
	defer unsubscribeClosed()
	if _, ok := <-closed; ok {
		t.Fatal("subscription on closed bus remains open")
	}
}

func TestQueueCloseCancelsRunningJob(t *testing.T) {
	app := newLifecycleTestApp(t)
	started := make(chan struct{})
	app.queue.run = func(ctx context.Context, _ Job) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	app.queue.Enqueue(Job{StoryID: "story", Type: "translate"})
	<-started
	app.Close()
	if !errors.Is(app.ctx.Err(), context.Canceled) {
		t.Fatalf("app context error = %v", app.ctx.Err())
	}
}

func TestQueueRunnerPanicDoesNotWedgeLifecycle(t *testing.T) {
	app := newLifecycleTestApp(t)
	panicked := make(chan struct{})
	nextDone := make(chan struct{})
	app.queue.run = func(_ context.Context, job Job) error {
		if job.StoryID == "panic" {
			close(panicked)
			panic("test panic")
		}
		close(nextDone)
		return nil
	}
	app.queue.Enqueue(Job{StoryID: "panic", Type: "translate"})
	<-panicked
	app.queue.Enqueue(Job{StoryID: "next", Type: "translate"})
	select {
	case <-nextDone:
	case <-time.After(2 * time.Second):
		t.Fatal("queue stopped after runner panic")
	}
	app.queue.CancelAndWait("panic")
}
