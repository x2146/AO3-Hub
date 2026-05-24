package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
)

const (
	maxEventsRetained = 200
	maxSampleBytes    = 16384
)

func (s *Store) LoadStats(id string) (*StatsFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadStatsLocked(id)
}

func (s *Store) loadStatsLocked(id string) (*StatsFile, error) {
	var file StatsFile
	ok, err := s.readJSON(s.path("stories", id, "stats.json"), &file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &StatsFile{
			Stats:   TranslationStats{ByStage: map[LLMCallStage]StageStats{}},
			Events:  []LLMCallEvent{},
			Samples: map[LLMCallStage]RequestSample{},
		}, nil
	}
	if file.Stats.ByStage == nil {
		file.Stats.ByStage = map[LLMCallStage]StageStats{}
	}
	if file.Events == nil {
		file.Events = []LLMCallEvent{}
	}
	if file.Samples == nil {
		file.Samples = map[LLMCallStage]RequestSample{}
	}
	return &file, nil
}

func (s *Store) saveStatsLocked(id string, file StatsFile) error {
	if file.Stats.ByStage == nil {
		file.Stats.ByStage = map[LLMCallStage]StageStats{}
	}
	if file.Events == nil {
		file.Events = []LLMCallEvent{}
	}
	if file.Samples == nil {
		file.Samples = map[LLMCallStage]RequestSample{}
	}
	return s.writeJSON(s.path("stories", id, "stats.json"), file)
}

func (s *Store) AppendStatsEvent(id string, event LLMCallEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadStatsLocked(id)
	if err != nil {
		return err
	}
	mergeEventIntoStats(&file.Stats, event)
	file.Events = append(file.Events, event)
	if len(file.Events) > maxEventsRetained {
		file.Events = file.Events[len(file.Events)-maxEventsRetained:]
	}
	return s.saveStatsLocked(id, *file)
}

func (s *Store) SaveStatsSample(id string, sample RequestSample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadStatsLocked(id)
	if err != nil {
		return err
	}
	sample.SystemPrompt = truncateString(sample.SystemPrompt, maxSampleBytes)
	sample.UserPayload = truncateString(sample.UserPayload, maxSampleBytes)
	sample.ResponsePreview = truncateString(sample.ResponsePreview, maxSampleBytes)
	file.Samples[sample.Stage] = sample
	return s.saveStatsLocked(id, *file)
}

func (s *Store) ResetStats(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.path("stories", id, "stats.json")
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func mergeEventIntoStats(stats *TranslationStats, event LLMCallEvent) {
	if stats.ByStage == nil {
		stats.ByStage = map[LLMCallStage]StageStats{}
	}
	if stats.StartedAt == "" {
		stats.StartedAt = event.StartedAt
	}
	stats.LastCallAt = event.StartedAt

	stage := stats.ByStage[event.Stage]
	stage.Calls++
	stats.Total.Calls++
	if event.Status == LLMCallSuccess {
		stage.Successes++
		stats.Total.Successes++
	} else {
		stage.Failures++
		stats.Total.Failures++
	}
	if event.Attempt > 0 {
		stage.Retries += event.Attempt
		stats.Total.Retries += event.Attempt
	}
	stage.PromptTokens += event.PromptTokens
	stage.CompletionTokens += event.CompletionTokens
	stage.TotalTokens += event.TotalTokens
	stage.DurationMS += event.DurationMS
	stats.Total.PromptTokens += event.PromptTokens
	stats.Total.CompletionTokens += event.CompletionTokens
	stats.Total.TotalTokens += event.TotalTokens
	stats.Total.DurationMS += event.DurationMS
	stats.ByStage[event.Stage] = stage
}

func truncateString(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "\n…(truncated)"
}

func extractUsageInt(usage map[string]any, keys ...string) int {
	for _, key := range keys {
		v, ok := usage[key]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case float64:
			return int(t)
		case int:
			return t
		case int64:
			return int(t)
		}
	}
	return 0
}

func extractUsage(usage map[string]any) (prompt, completion, total int) {
	if usage == nil {
		return 0, 0, 0
	}
	prompt = extractUsageInt(usage, "prompt_tokens", "input_tokens")
	completion = extractUsageInt(usage, "completion_tokens", "output_tokens")
	total = extractUsageInt(usage, "total_tokens")
	if total == 0 {
		total = prompt + completion
	}
	return prompt, completion, total
}

type statsTracker struct {
	app          *App
	storyID      string
	mu           sync.Mutex
	eventCounter int
}

func newStatsTracker(app *App, storyID string) *statsTracker {
	return &statsTracker{app: app, storyID: storyID}
}

func (t *statsTracker) nextID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.eventCounter++
	id, err := randomHex(4)
	if err != nil {
		return strings.TrimSpace(nowISO())
	}
	return id
}

func (t *statsTracker) record(event LLMCallEvent) {
	if t == nil || t.app == nil {
		return
	}
	if event.ID == "" {
		event.ID = t.nextID()
	}
	if err := t.app.store.AppendStatsEvent(t.storyID, event); err != nil {
		return
	}
	t.app.bus.Emit(t.storyID, StreamEvent{
		Type:         "llm-call",
		Phase:        PhaseTranslating,
		Message:      string(event.Stage) + ":" + string(event.Status),
		ChapterIndex: derefIntOrZero(event.ChapterIndex),
	})
}

func (t *statsTracker) saveSample(sample RequestSample) {
	if t == nil || t.app == nil {
		return
	}
	if sample.CapturedAt == "" {
		sample.CapturedAt = nowISO()
	}
	_ = t.app.store.SaveStatsSample(t.storyID, sample)
}

type trackedCallContext struct {
	stage        LLMCallStage
	chapterIndex *int
	blockIDs     []string
	attempt      int
}

func (t *statsTracker) trackedChat(ctx context.Context, cfg LLMConfig, messages []ChatMessage, jsonMode bool, callCtx trackedCallContext) (ChatResult, error) {
	startedAt := nowISO()
	result, err := chat(ctx, cfg, messages, jsonMode)
	event := LLMCallEvent{
		Stage:        callCtx.stage,
		Model:        cfg.Model,
		StartedAt:    startedAt,
		DurationMS:   result.DurationMS,
		Attempt:      callCtx.attempt,
		ChapterIndex: callCtx.chapterIndex,
		BlockIDs:     callCtx.blockIDs,
	}
	if err != nil {
		event.Status = LLMCallError
		event.ErrorMessage = err.Error()
		var llmErr LLMError
		if errors.As(err, &llmErr) {
			event.ErrorStatus = llmErr.Status
		}
	} else {
		event.Status = LLMCallSuccess
		prompt, completion, total := extractUsage(result.Usage)
		event.PromptTokens = prompt
		event.CompletionTokens = completion
		event.TotalTokens = total
	}
	if t != nil {
		t.record(event)
		if shouldSaveSample(callCtx.stage, err) {
			sample := RequestSample{
				Stage:        callCtx.stage,
				CapturedAt:   startedAt,
				Model:        cfg.Model,
				SystemPrompt: joinSystem(messages),
				UserPayload:  joinUser(messages),
				ChapterIndex: callCtx.chapterIndex,
				BlockIDs:     callCtx.blockIDs,
			}
			if err == nil {
				sample.ResponsePreview = result.Content
			} else {
				sample.ResponsePreview = err.Error()
			}
			t.saveSample(sample)
		}
	}
	return result, err
}

func shouldSaveSample(stage LLMCallStage, err error) bool {
	if err != nil {
		return true
	}
	switch stage {
	case StageAnalysisChapter, StageAnalysisMerge, StageAnalysisFull, StageTranslateBatch:
		return true
	}
	return false
}

func joinSystem(messages []ChatMessage) string {
	parts := []string{}
	for _, m := range messages {
		if strings.EqualFold(strings.TrimSpace(m.Role), "system") {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, "\n\n")
}

func joinUser(messages []ChatMessage) string {
	parts := []string{}
	for _, m := range messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role == "system" {
			continue
		}
		parts = append(parts, m.Content)
	}
	return strings.Join(parts, "\n\n")
}

func derefIntOrZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func intPtr(v int) *int {
	return &v
}
