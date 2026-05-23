package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"
)

const systemPrompt = `你是文学翻译。把英文文学作品翻译为中文，要求：
1) 严格保留输入中的 HTML 内联标签（em/strong/a/i/b/u/s/sup/sub/br/span/code 等），仅翻译文字内容
2) 每个输入段落必须对应一个输出段落，顺序、数量完全一致
3) 译文自然流畅，符合中文小说语感，不增不减
4) 角色名、地名等专有名词在同一作品内保持一致
5) 输入是一个 JSON 数组，输出也必须是相同长度的 JSON 数组
6) 仅输出 JSON 对象 { "blocks": [{ "id": "...", "html": "..." }] }，不要任何解释`

const refinedSystemPrompt = `你是 AO3 同人文翻译专家，专精英文同人作品的中文本地化。要求：
1) 严格保留输入中的 HTML 内联标签（em/strong/a/i/b/u/s/sup/sub/br/span/code 等），仅翻译文字内容
2) 每个输入段落必须对应一个输出段落，顺序、数量完全一致
3) 译文符合中文同人圈语感：自然、有文学性、不机翻味，保留作者的语气与节奏
4) 严格遵守 context 中 glossary 的译名；ship 关系动态与基调参考 ships/tone 字段，使角色对白与心理符合既定动态
5) 同人圈惯用术语（pet name、kink 词汇、ship 行话等）使用圈内通用的中文表达
6) 仅输出 JSON 对象 { "blocks": [{ "id": "...", "html": "..." }] }，不要任何解释`

type translateInput struct {
	ID   string `json:"id"`
	HTML string `json:"html"`
}

type translateOutput struct {
	ID   string `json:"id"`
	HTML string `json:"html"`
}

type batch struct {
	Blocks []Block
}

func approxTokens(text string) int {
	return int(math.Ceil(float64(len(text)) / 3.2))
}

func isTranslatable(block Block) bool {
	return block.Type != BlockHR && strings.TrimSpace(block.HTML) != ""
}

func chunkBlocks(blocks []Block, blocksPerRequest, maxTokens int) []batch {
	batches := []batch{}
	current := []Block{}
	currentTokens := 0
	for _, block := range blocks {
		tokens := approxTokens(block.HTML)
		wouldExceed := len(current) >= blocksPerRequest || (len(current) > 0 && currentTokens+tokens > maxTokens)
		if wouldExceed {
			batches = append(batches, batch{Blocks: current})
			current = []Block{}
			currentTokens = 0
		}
		current = append(current, block)
		currentTokens += tokens
	}
	if len(current) > 0 {
		batches = append(batches, batch{Blocks: current})
	}
	return batches
}

func buildUserPayload(meta Meta, blocks []translateInput, transCtx *TranslationContext, chapterIndex int) (string, error) {
	contextPayload := map[string]any{
		"title":    meta.Title,
		"fandom":   meta.Tags.Fandom,
		"glossary": map[string]string{},
	}
	if transCtx != nil {
		if transCtx.Summary != "" {
			contextPayload["summary"] = transCtx.Summary
		}
		if transCtx.Tone != "" {
			contextPayload["tone"] = transCtx.Tone
		}
		if len(transCtx.Ships) > 0 {
			contextPayload["ships"] = transCtx.Ships
		}
		if len(transCtx.Characters) > 0 {
			contextPayload["characters"] = transCtx.Characters
		}
		if len(transCtx.Glossary) > 0 {
			contextPayload["glossary"] = transCtx.Glossary
		}
		if rating := meta.Tags.Rating; rating != "" {
			contextPayload["rating"] = rating
		}
		if len(meta.Tags.Relationship) > 0 {
			contextPayload["relationships"] = meta.Tags.Relationship
		}
		if len(meta.Tags.Warnings) > 0 {
			contextPayload["warnings"] = meta.Tags.Warnings
		}
		if len(meta.Tags.Additional) > 0 {
			contextPayload["additionalTags"] = meta.Tags.Additional
		}
		for _, summary := range transCtx.ChapterSummaries {
			if summary.Index == chapterIndex {
				contextPayload["currentChapter"] = map[string]any{
					"index":   summary.Index,
					"title":   summary.Title,
					"summary": summary.Summary,
				}
				break
			}
		}
	}
	payload := map[string]any{
		"context": contextPayload,
		"blocks":  blocks,
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func parseJSONResponse(content string) ([]translateOutput, error) {
	s := strings.TrimSpace(content)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "```json"))
		s = strings.TrimSpace(strings.TrimPrefix(s, "```"))
		s = strings.TrimSpace(strings.TrimSuffix(s, "```"))
	}
	var raw struct {
		Blocks  []translateOutput `json:"blocks"`
		Data    []translateOutput `json:"data"`
		Results []translateOutput `json:"results"`
	}
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, err
	}
	out := raw.Blocks
	if out == nil {
		out = raw.Data
	}
	if out == nil {
		out = raw.Results
	}
	if out == nil {
		return nil, errors.New("LLM response missing 'blocks' array")
	}
	return out, nil
}

func translateBatch(ctx context.Context, cfg Config, meta Meta, inputs []translateInput, transCtx *TranslationContext, chapterIndex int) ([]translateOutput, error) {
	userPayload, err := buildUserPayload(meta, inputs, transCtx, chapterIndex)
	if err != nil {
		return nil, err
	}
	prompt := systemPrompt
	if transCtx != nil {
		prompt = refinedSystemPrompt
	}
	result, err := chat(ctx, cfg.LLM, []ChatMessage{
		{Role: "system", Content: prompt},
		{Role: "user", Content: userPayload},
	}, true)
	if err != nil {
		return nil, err
	}
	out, err := parseJSONResponse(result.Content)
	if err != nil {
		return nil, err
	}
	if len(out) != len(inputs) {
		return nil, fmt.Errorf("段数不匹配: 输入 %d，输出 %d", len(inputs), len(out))
	}
	byID := map[string]translateOutput{}
	for _, item := range out {
		byID[item.ID] = item
	}
	ordered := make([]translateOutput, 0, len(inputs))
	for _, input := range inputs {
		found, ok := byID[input.ID]
		if !ok {
			return nil, fmt.Errorf("缺少段 id=%s 的译文", input.ID)
		}
		ordered = append(ordered, translateOutput{ID: input.ID, HTML: found.HTML})
	}
	return ordered, nil
}

func withRetry[T any](fn func() (T, error), retries int) (T, error) {
	var zero T
	var last error
	for i := 0; i <= retries; i++ {
		out, err := fn()
		if err == nil {
			return out, nil
		}
		last = err
		var llmErr LLMError
		if errors.As(err, &llmErr) && (llmErr.Status == 400 || llmErr.Status == 401) {
			return zero, err
		}
		backoff := time.Duration(600*math.Pow(2, float64(i))) * time.Millisecond
		backoff += time.Duration(rand.Intn(300)) * time.Millisecond
		time.Sleep(backoff)
	}
	return zero, last
}

func makeBlankTranslated(original ChapterFile) ChapterFile {
	chapters := make([]Chapter, len(original.Chapters))
	for i, chapter := range original.Chapters {
		blocks := make([]Block, len(chapter.Blocks))
		for j, block := range chapter.Blocks {
			status := BlockDone
			if isTranslatable(block) {
				status = BlockPending
			}
			blocks[j] = Block{
				ID:     block.ID,
				Type:   block.Type,
				HTML:   "",
				Status: status,
			}
		}
		chapters[i] = Chapter{
			Index:  chapter.Index,
			Title:  chapter.Title,
			Blocks: blocks,
		}
	}
	return ChapterFile{Chapters: chapters}
}

func (a *App) markInflight(storyID string, ids []string) {
	a.inflightMu.Lock()
	defer a.inflightMu.Unlock()
	set, ok := a.inflight[storyID]
	if !ok {
		set = map[string]bool{}
		a.inflight[storyID] = set
	}
	for _, id := range ids {
		set[id] = true
	}
}

func (a *App) clearInflight(storyID string, ids []string) {
	a.inflightMu.Lock()
	defer a.inflightMu.Unlock()
	set, ok := a.inflight[storyID]
	if !ok {
		return
	}
	for _, id := range ids {
		delete(set, id)
	}
	if len(set) == 0 {
		delete(a.inflight, storyID)
	}
}

func (a *App) resetInflight(storyID string) {
	a.inflightMu.Lock()
	defer a.inflightMu.Unlock()
	delete(a.inflight, storyID)
}

func (a *App) inflightCount(storyID string) int {
	a.inflightMu.RLock()
	defer a.inflightMu.RUnlock()
	return len(a.inflight[storyID])
}

func (a *App) emitProgress(storyID string, translated ChapterFile, original ChapterFile) error {
	total := 0
	done := 0
	errs := 0
	for i := range original.Chapters {
		for j := range original.Chapters[i].Blocks {
			if !isTranslatable(original.Chapters[i].Blocks[j]) {
				continue
			}
			total++
			if i >= len(translated.Chapters) || j >= len(translated.Chapters[i].Blocks) {
				continue
			}
			switch translated.Chapters[i].Blocks[j].Status {
			case BlockDone:
				done++
			case BlockError:
				errs++
			}
		}
	}
	inflight := a.inflightCount(storyID)
	if err := a.setProgress(storyID, func(p Progress) Progress {
		p.TotalBlocks = total
		p.DoneBlocks = done
		p.ErrorBlocks = errs
		p.InflightBlocks = inflight
		return p
	}); err != nil {
		return err
	}
	a.bus.Emit(storyID, StreamEvent{
		Type:           "progress",
		DoneBlocks:     done,
		TotalBlocks:    total,
		ErrorBlocks:    errs,
		InflightBlocks: inflight,
		Phase:          PhaseTranslating,
	})
	return nil
}

func (a *App) setProgress(storyID string, mutator func(Progress) Progress) error {
	current, err := a.store.LoadProgress(storyID)
	if err != nil {
		return err
	}
	if current == nil {
		return errors.New("progress not found")
	}
	next := mutator(*current)
	return a.store.SaveProgress(storyID, next)
}

func (a *App) updateStoryStatus(storyID string, status StoryStatus) error {
	_, err := a.store.PatchIndex(storyID, func(entry *IndexEntry) {
		entry.Status = status
	})
	return err
}

func (a *App) runTranslation(ctx context.Context, storyID string) error {
	meta, err := a.store.LoadMeta(storyID)
	if err != nil || meta == nil {
		return fmt.Errorf("missing meta for %s", storyID)
	}
	original, err := a.store.LoadOriginal(storyID)
	if err != nil || original == nil {
		return fmt.Errorf("missing original for %s", storyID)
	}
	translated, err := a.store.LoadTranslated(storyID)
	if err != nil {
		return err
	}
	if translated == nil {
		blank := makeBlankTranslated(*original)
		translated = &blank
		if err := a.store.SaveTranslated(storyID, *translated); err != nil {
			return err
		}
	}

	cfg, err := a.store.LoadConfig()
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.LLM.APIKey) == "" {
		msg := "未配置 LLM apiKey，请到 Settings 填好后再 retry"
		_ = a.setProgress(storyID, func(p Progress) Progress {
			p.Phase = PhaseError
			p.Message = msg
			p.FinishedAt = nowISO()
			return p
		})
		_ = a.updateStoryStatus(storyID, StatusError)
		a.bus.Emit(storyID, StreamEvent{Type: "phase", Phase: PhaseError, Message: msg})
		return nil
	}

	if err := a.updateStoryStatus(storyID, StatusTranslating); err != nil {
		return err
	}
	if err := a.setProgress(storyID, func(p Progress) Progress {
		p.Phase = PhaseTranslating
		return p
	}); err != nil {
		return err
	}
	a.bus.Emit(storyID, StreamEvent{Type: "phase", Phase: PhaseTranslating})

	a.resetInflight(storyID)
	defer a.resetInflight(storyID)

	effectiveMode := meta.TranslationMode
	if effectiveMode == "" {
		effectiveMode = cfg.LLM.Mode
	}
	effectiveMode = normalizeTranslationMode(effectiveMode)

	var transCtx *TranslationContext
	if effectiveMode == TranslationModeRefined {
		analysed, err := a.runAnalysis(ctx, storyID, *meta, *original, cfg)
		if err != nil {
			msg := "精翻预读分析失败: " + err.Error()
			_ = a.setProgress(storyID, func(p Progress) Progress {
				p.Phase = PhaseError
				p.Message = msg
				p.FinishedAt = nowISO()
				return p
			})
			_ = a.updateStoryStatus(storyID, StatusError)
			a.bus.Emit(storyID, StreamEvent{Type: "phase", Phase: PhaseError, Message: msg})
			return nil
		}
		transCtx = analysed
		if err := a.updateStoryStatus(storyID, StatusTranslating); err != nil {
			return err
		}
		if err := a.setProgress(storyID, func(p Progress) Progress {
			p.Phase = PhaseTranslating
			return p
		}); err != nil {
			return err
		}
		a.bus.Emit(storyID, StreamEvent{Type: "phase", Phase: PhaseTranslating})
	}

	maxRounds := cfg.LLM.MaxAutoRetries
	if maxRounds < 0 {
		maxRounds = 0
	}
	for round := 0; round <= maxRounds; round++ {
		if round > 0 {
			if !resetErrorsToPending(translated) {
				break
			}
			if err := a.store.SaveTranslated(storyID, *translated); err != nil {
				return err
			}
			if err := a.emitProgress(storyID, *translated, *original); err != nil {
				return err
			}
			a.bus.Emit(storyID, StreamEvent{Type: "phase", Phase: PhaseTranslating, Message: fmt.Sprintf("自动重试第 %d/%d 轮", round, maxRounds)})
		}
		if err := a.translatePass(ctx, storyID, cfg, *meta, original, translated, transCtx); err != nil {
			return err
		}
	}

	hasErrors := false
	for _, chapter := range translated.Chapters {
		for _, block := range chapter.Blocks {
			if block.Status == BlockError {
				hasErrors = true
				break
			}
		}
	}
	phase := PhaseReady
	status := StatusReady
	if hasErrors {
		phase = PhaseError
		status = StatusError
	}
	if err := a.setProgress(storyID, func(p Progress) Progress {
		p.Phase = phase
		p.CurrentChapter = nil
		p.FinishedAt = nowISO()
		p.InflightBlocks = 0
		return p
	}); err != nil {
		return err
	}
	if err := a.updateStoryStatus(storyID, status); err != nil {
		return err
	}
	a.bus.Emit(storyID, StreamEvent{Type: "phase", Phase: phase})
	return nil
}

func resetErrorsToPending(translated *ChapterFile) bool {
	reset := false
	for ci := range translated.Chapters {
		for bi := range translated.Chapters[ci].Blocks {
			if translated.Chapters[ci].Blocks[bi].Status == BlockError {
				translated.Chapters[ci].Blocks[bi].Status = BlockPending
				translated.Chapters[ci].Blocks[bi].HTML = ""
				translated.Chapters[ci].Blocks[bi].Error = ""
				reset = true
			}
		}
	}
	return reset
}

func (a *App) translatePass(ctx context.Context, storyID string, cfg Config, meta Meta, original *ChapterFile, translated *ChapterFile, transCtx *TranslationContext) error {
	for chIdx := 0; chIdx < len(original.Chapters); chIdx++ {
		orig := original.Chapters[chIdx]
		if chIdx >= len(translated.Chapters) {
			translated.Chapters = append(translated.Chapters, Chapter{Index: chIdx, Title: orig.Title})
		}
		transCh := &translated.Chapters[chIdx]
		for len(transCh.Blocks) < len(orig.Blocks) {
			ob := orig.Blocks[len(transCh.Blocks)]
			status := BlockDone
			if isTranslatable(ob) {
				status = BlockPending
			}
			transCh.Blocks = append(transCh.Blocks, Block{ID: ob.ID, Type: ob.Type, Status: status})
		}

		currentChapter := chIdx
		if err := a.setProgress(storyID, func(p Progress) Progress {
			p.CurrentChapter = &currentChapter
			return p
		}); err != nil {
			return err
		}

		pending := []Block{}
		for bi, ob := range orig.Blocks {
			tb := transCh.Blocks[bi]
			if !isTranslatable(ob) {
				if tb.Status != BlockDone {
					transCh.Blocks[bi] = Block{ID: tb.ID, Type: tb.Type, HTML: ob.HTML, Status: BlockDone}
				}
				continue
			}
			if tb.Status == BlockDone {
				continue
			}
			pending = append(pending, ob)
		}
		if err := a.store.SaveTranslated(storyID, *translated); err != nil {
			return err
		}
		if err := a.emitProgress(storyID, *translated, *original); err != nil {
			return err
		}

		if len(pending) == 0 {
			a.bus.Emit(storyID, StreamEvent{Type: "chapter-done", ChapterIndex: chIdx})
			continue
		}

		batches := chunkBlocks(pending, cfg.LLM.BlocksPerRequest, cfg.LLM.MaxTokensPerRequest)
		concurrency := cfg.LLM.Concurrency
		if concurrency < 1 {
			concurrency = 1
		}
		if concurrency > len(batches) {
			concurrency = len(batches)
		}

		var cursor int
		var cursorMu sync.Mutex
		var transMu sync.Mutex
		var wg sync.WaitGroup
		for worker := 0; worker < concurrency; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					cursorMu.Lock()
					i := cursor
					cursor++
					cursorMu.Unlock()
					if i >= len(batches) {
						return
					}
					b := batches[i]
					inputs := make([]translateInput, len(b.Blocks))
					ids := make([]string, len(b.Blocks))
					for j, block := range b.Blocks {
						inputs[j] = translateInput{ID: block.ID, HTML: block.HTML}
						ids[j] = block.ID
					}
					a.markInflight(storyID, ids)
					_ = a.emitProgress(storyID, *translated, *original)
					outs, err := withRetry(func() ([]translateOutput, error) {
						return translateBatch(ctx, cfg, meta, inputs, transCtx, chIdx)
					}, 2)

					transMu.Lock()
					if err != nil {
						msg := err.Error()
						for _, input := range inputs {
							bi := findBlockIndex(transCh.Blocks, input.ID)
							if bi < 0 {
								continue
							}
							block := transCh.Blocks[bi]
							block.Status = BlockError
							block.Error = msg
							transCh.Blocks[bi] = block
							a.bus.Emit(storyID, StreamEvent{Type: "block-error", ChapterIndex: chIdx, BlockID: input.ID, Message: msg})
						}
						_ = a.setProgress(storyID, func(p Progress) Progress {
							for _, input := range inputs {
								p.Errors = append(p.Errors, ProgressError{ChapterIndex: chIdx, BlockID: input.ID, Message: msg, At: nowISO()})
							}
							return p
						})
					} else {
						for _, out := range outs {
							bi := findBlockIndex(transCh.Blocks, out.ID)
							if bi < 0 {
								continue
							}
							block := transCh.Blocks[bi]
							block.HTML = out.HTML
							block.Status = BlockDone
							block.Error = ""
							transCh.Blocks[bi] = block
							a.bus.Emit(storyID, StreamEvent{Type: "block-done", ChapterIndex: chIdx, BlockID: out.ID})
						}
					}
					_ = a.store.SaveTranslated(storyID, *translated)
					a.clearInflight(storyID, ids)
					_ = a.emitProgress(storyID, *translated, *original)
					transMu.Unlock()
				}
			}()
		}
		wg.Wait()
		a.bus.Emit(storyID, StreamEvent{Type: "chapter-done", ChapterIndex: chIdx})
	}
	return nil
}

func findBlockIndex(blocks []Block, id string) int {
	for i, block := range blocks {
		if block.ID == id {
			return i
		}
	}
	return -1
}
