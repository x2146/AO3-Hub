package app

import "sync"

type EventBus struct {
	mu       sync.Mutex
	channels map[string]map[chan StreamEvent]struct{}
}

func NewEventBus() *EventBus {
	return &EventBus{channels: map[string]map[chan StreamEvent]struct{}{}}
}

func (b *EventBus) Subscribe(storyID string) (chan StreamEvent, func()) {
	ch := make(chan StreamEvent, 64)
	b.mu.Lock()
	if b.channels[storyID] == nil {
		b.channels[storyID] = map[chan StreamEvent]struct{}{}
	}
	b.channels[storyID][ch] = struct{}{}
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if set := b.channels[storyID]; set != nil {
			delete(set, ch)
			if len(set) == 0 {
				delete(b.channels, storyID)
			}
		}
		close(ch)
	}
	return ch, unsubscribe
}

func (b *EventBus) Emit(storyID string, event StreamEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.channels[storyID] {
		select {
		case ch <- event:
		default:
		}
	}
}

type Job struct {
	StoryID string
	Type    string
}

type Queue struct {
	app      *App
	mu       sync.Mutex
	pending  []Job
	inflight map[string]bool
	pumping  bool
}

func NewQueue(app *App) *Queue {
	return &Queue{
		app:      app,
		pending:  []Job{},
		inflight: map[string]bool{},
	}
}

func (q *Queue) Enqueue(job Job) {
	q.mu.Lock()
	q.pending = append(q.pending, job)
	shouldPump := !q.pumping
	q.mu.Unlock()
	if shouldPump {
		go q.pump()
	}
}

func (q *Queue) pump() {
	q.mu.Lock()
	if q.pumping {
		q.mu.Unlock()
		return
	}
	q.pumping = true
	q.mu.Unlock()
	defer func() {
		q.mu.Lock()
		q.pumping = false
		hasPending := len(q.pending) > 0
		q.mu.Unlock()
		if hasPending {
			go q.pump()
		}
	}()

	for {
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.mu.Unlock()
			return
		}
		idx := -1
		for i, job := range q.pending {
			if !q.inflight[job.StoryID] {
				idx = i
				break
			}
		}
		if idx < 0 {
			q.mu.Unlock()
			return
		}
		job := q.pending[idx]
		q.pending = append(q.pending[:idx], q.pending[idx+1:]...)
		q.inflight[job.StoryID] = true
		q.mu.Unlock()

		if job.Type == "translate" || job.Type == "retry" {
			if err := q.app.runTranslation(q.app.ctx, job.StoryID); err != nil {
				msg := err.Error()
				_, _ = q.app.store.PatchIndex(job.StoryID, func(entry *IndexEntry) {
					entry.Status = StatusError
				})
				q.app.bus.Emit(job.StoryID, StreamEvent{Type: "phase", Phase: PhaseError, Message: msg})
			}
		}

		q.mu.Lock()
		delete(q.inflight, job.StoryID)
		q.mu.Unlock()
	}
}
