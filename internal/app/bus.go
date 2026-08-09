package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type EventBus struct {
	mu       sync.Mutex
	channels map[string]map[chan StreamEvent]struct{}
	closed   bool
}

func NewEventBus() *EventBus {
	return &EventBus{channels: map[string]map[chan StreamEvent]struct{}{}}
}

func (b *EventBus) Subscribe(storyID string) (chan StreamEvent, func()) {
	ch := make(chan StreamEvent, 64)
	b.mu.Lock()
	if b.closed {
		close(ch)
		b.mu.Unlock()
		return ch, func() {}
	}
	if b.channels[storyID] == nil {
		b.channels[storyID] = map[chan StreamEvent]struct{}{}
	}
	b.channels[storyID][ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			set := b.channels[storyID]
			if _, ok := set[ch]; !ok {
				return
			}
			delete(set, ch)
			if len(set) == 0 {
				delete(b.channels, storyID)
			}
			close(ch)
		})
	}
	return ch, unsubscribe
}

func (b *EventBus) Emit(storyID string, event StreamEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	for ch := range b.channels[storyID] {
		select {
		case ch <- event:
		default:
		}
	}
}

func (b *EventBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for storyID, set := range b.channels {
		for ch := range set {
			close(ch)
		}
		delete(b.channels, storyID)
	}
}

type Job struct {
	StoryID string
	Type    string
}

type runningJob struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type Queue struct {
	app     *App
	ctx     context.Context
	cancel  context.CancelFunc
	run     func(context.Context, Job) error
	mu      sync.Mutex
	pending []Job
	queued  map[string]struct{}
	running map[string]*runningJob
	pumping bool
	closed  bool
	wg      sync.WaitGroup
}

func NewQueue(app *App) *Queue {
	base := app.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	q := &Queue{
		app:     app,
		ctx:     ctx,
		cancel:  cancel,
		pending: []Job{},
		queued:  map[string]struct{}{},
		running: map[string]*runningJob{},
	}
	q.run = func(ctx context.Context, job Job) error {
		switch job.Type {
		case "translate", "retry":
			return app.runTranslation(ctx, job.StoryID)
		default:
			return nil
		}
	}
	return q
}

func (q *Queue) Enqueue(job Job) {
	q.mu.Lock()
	if q.closed || q.ctx.Err() != nil {
		q.mu.Unlock()
		return
	}
	if _, ok := q.queued[job.StoryID]; ok {
		q.mu.Unlock()
		return
	}
	if _, ok := q.running[job.StoryID]; ok {
		q.mu.Unlock()
		return
	}
	q.pending = append(q.pending, job)
	q.queued[job.StoryID] = struct{}{}
	if !q.pumping {
		q.pumping = true
		q.wg.Add(1)
		go q.pump()
	}
	q.mu.Unlock()
}

func (q *Queue) CancelAndWait(storyID string) {
	q.mu.Lock()
	if _, ok := q.queued[storyID]; ok {
		next := q.pending[:0]
		for _, job := range q.pending {
			if job.StoryID != storyID {
				next = append(next, job)
			}
		}
		q.pending = next
		delete(q.queued, storyID)
	}
	running := q.running[storyID]
	if running != nil {
		running.cancel()
	}
	q.mu.Unlock()
	if running != nil {
		<-running.done
	}
}

func (q *Queue) Close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		q.wg.Wait()
		return
	}
	q.closed = true
	q.cancel()
	q.pending = nil
	clear(q.queued)
	running := make([]*runningJob, 0, len(q.running))
	for _, job := range q.running {
		job.cancel()
		running = append(running, job)
	}
	q.mu.Unlock()
	for _, job := range running {
		<-job.done
	}
	q.wg.Wait()
}

func (q *Queue) pump() {
	defer q.wg.Done()
	for {
		q.mu.Lock()
		if q.closed || q.ctx.Err() != nil || len(q.pending) == 0 {
			q.pumping = false
			q.mu.Unlock()
			return
		}
		job := q.pending[0]
		q.pending = q.pending[1:]
		delete(q.queued, job.StoryID)
		jobCtx, cancel := context.WithCancel(q.ctx)
		running := &runningJob{cancel: cancel, done: make(chan struct{})}
		q.running[job.StoryID] = running
		q.mu.Unlock()

		err := q.runJob(jobCtx, job)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			msg := err.Error()
			if finishErr := q.app.finishStory(job.StoryID, PhaseError, StatusError, msg); finishErr == nil {
				q.app.bus.Emit(job.StoryID, StreamEvent{Type: "phase", Phase: PhaseError, Message: msg})
			}
		}

		q.mu.Lock()
		delete(q.running, job.StoryID)
		cancel()
		close(running.done)
		q.mu.Unlock()
	}
}

func (q *Queue) runJob(ctx context.Context, job Job) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("job panic: %v", recovered)
		}
	}()
	return q.run(ctx, job)
}
