package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	xhtml "golang.org/x/net/html"
)

const (
	maxProgressErrors = 100
	retryBaseDelay    = 600 * time.Millisecond
	retryMaxDelay     = 5 * time.Minute
)

const systemPrompt = `你是文学翻译。把英文文学作品翻译为中文，要求：
1) 输入不含 HTML 或格式标签；程序会保留 AO3 原始富文本结构，你只负责翻译纯文本
2) 每个 block 的 text 是整段上下文，runs 是需要翻译的文本片段；结合 text 让 runs 的译文自然连贯
3) 输出 blocks 必须与输入 blocks 数量、id、顺序一致；每个 block.runs 必须与输入 runs 数量、id、顺序一致
4) 仅翻译 runs[].text，不增删 run，不输出 HTML、Markdown 或格式标注
5) 译文自然流畅，符合中文小说语感；角色名、地名等专有名词在同一作品内保持一致
6) 仅输出 JSON 对象 { "blocks": [{ "id": "...", "runs": [{ "id": "...", "text": "..." }] }] }，不要任何解释`

const refinedSystemPrompt = `你是 AO3 同人文翻译专家，专精英文同人作品的中文本地化。要求：
1) 输入不含 HTML 或格式标签；程序会保留 AO3 原始富文本结构，你只负责翻译纯文本
2) 每个 block 的 text 是整段上下文，runs 是需要翻译的文本片段；结合 text、context、glossary 让 runs 的译文自然连贯
3) 输出 blocks 必须与输入 blocks 数量、id、顺序一致；每个 block.runs 必须与输入 runs 数量、id、顺序一致
4) 仅翻译 runs[].text，不增删 run，不输出 HTML、Markdown 或格式标注
5) 译文符合中文同人圈语感，保留作者语气与节奏；严格遵守 context 中 glossary 的译名
6) 仅输出 JSON 对象 { "blocks": [{ "id": "...", "runs": [{ "id": "...", "text": "..." }] }] }，不要任何解释`

type translateRun struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type translateInput struct {
	ID   string         `json:"id"`
	Text string         `json:"text"`
	Runs []translateRun `json:"runs"`
	HTML string         `json:"-"`
}

type translateOutput struct {
	ID   string         `json:"id"`
	Text string         `json:"text,omitempty"`
	Runs []translateRun `json:"runs,omitempty"`
	HTML string         `json:"html,omitempty"`
}

type batch struct {
	Blocks []Block
}

func approxTokens(text string) int {
	return int(math.Ceil(float64(len(text)) / 3.2))
}

func isTranslatable(block Block) bool {
	return block.Type != BlockHR && htmlToPlainText(block.HTML) != ""
}

func parseHTMLBody(raw string) (*xhtml.Node, error) {
	doc, err := xhtml.Parse(strings.NewReader("<!doctype html><html><body>" + raw + "</body></html>"))
	if err != nil {
		return nil, err
	}
	var findBody func(*xhtml.Node) *xhtml.Node
	findBody = func(n *xhtml.Node) *xhtml.Node {
		if n.Type == xhtml.ElementNode && n.Data == "body" {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if found := findBody(c); found != nil {
				return found
			}
		}
		return nil
	}
	body := findBody(doc)
	if body == nil {
		return nil, errors.New("parsed html missing body")
	}
	return body, nil
}

func eachTranslatableTextNode(root *xhtml.Node, fn func(*xhtml.Node)) {
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			if strings.TrimSpace(n.Data) != "" {
				fn(n)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		walk(c)
	}
}

func plainTextFromRuns(runs []translateRun) string {
	parts := make([]string, 0, len(runs))
	for _, run := range runs {
		text := strings.TrimSpace(whitespaceRE.ReplaceAllString(run.Text, " "))
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

func makeTranslateInput(block Block) (translateInput, error) {
	body, err := parseHTMLBody(block.HTML)
	if err != nil {
		return translateInput{}, err
	}
	runs := []translateRun{}
	eachTranslatableTextNode(body, func(n *xhtml.Node) {
		runs = append(runs, translateRun{
			ID:   fmt.Sprintf("r%d", len(runs)),
			Text: n.Data,
		})
	})
	if len(runs) == 0 {
		return translateInput{}, fmt.Errorf("block %s has no translatable text", block.ID)
	}
	return translateInput{
		ID:   block.ID,
		Text: plainTextFromRuns(runs),
		Runs: runs,
		HTML: block.HTML,
	}, nil
}

func renderHTMLBodyContents(body *xhtml.Node) (string, error) {
	var b strings.Builder
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		if err := xhtml.Render(&b, c); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

func outputRunsForInput(input translateInput, output translateOutput) ([]translateRun, error) {
	if len(output.Runs) == 0 {
		if len(input.Runs) == 1 && output.Text != "" {
			return []translateRun{{ID: input.Runs[0].ID, Text: output.Text}}, nil
		}
		return nil, fmt.Errorf("段 id=%s 缺少 runs 译文", input.ID)
	}
	if len(output.Runs) != len(input.Runs) {
		return nil, fmt.Errorf("段 id=%s run 数不匹配: 输入 %d，输出 %d", input.ID, len(input.Runs), len(output.Runs))
	}
	byID := map[string]translateRun{}
	for _, run := range output.Runs {
		byID[run.ID] = run
	}
	ordered := make([]translateRun, 0, len(input.Runs))
	for i, inputRun := range input.Runs {
		run, ok := byID[inputRun.ID]
		if !ok {
			if output.Runs[i].ID == "" {
				run = translateRun{ID: inputRun.ID, Text: output.Runs[i].Text}
			} else {
				return nil, fmt.Errorf("段 id=%s 缺少 run id=%s 的译文", input.ID, inputRun.ID)
			}
		}
		if strings.TrimSpace(run.Text) == "" {
			return nil, fmt.Errorf("段 id=%s 的 run id=%s 译文为空", input.ID, inputRun.ID)
		}
		ordered = append(ordered, translateRun{ID: inputRun.ID, Text: run.Text})
	}
	return ordered, nil
}

func translatedHTMLFromRuns(input translateInput, output translateOutput) (string, error) {
	if output.HTML != "" && len(output.Runs) == 0 && output.Text == "" {
		return "", fmt.Errorf("段 id=%s 返回了 html 字段；当前翻译接口只接受纯文本 runs", input.ID)
	}
	runs, err := outputRunsForInput(input, output)
	if err != nil {
		return "", err
	}
	body, err := parseHTMLBody(input.HTML)
	if err != nil {
		return "", err
	}
	i := 0
	eachTranslatableTextNode(body, func(n *xhtml.Node) {
		if i < len(runs) {
			n.Data = runs[i].Text
			i++
		}
	})
	if i != len(runs) {
		return "", fmt.Errorf("段 id=%s 回填 run 数不匹配", input.ID)
	}
	rendered, err := renderHTMLBodyContents(body)
	if err != nil {
		return "", err
	}
	return sanitizeHTMLFragment(rendered), nil
}

func chunkBlocks(blocks []Block, blocksPerRequest, maxTokens int) []batch {
	batches := []batch{}
	current := []Block{}
	currentTokens := 0
	for _, block := range blocks {
		tokens := approxTokens(htmlToPlainText(block.HTML))
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

func translateBatch(ctx context.Context, cfg Config, meta Meta, inputs []translateInput, transCtx *TranslationContext, chapterIndex int, tracker *statsTracker, attempt int) ([]translateOutput, error) {
	userPayload, err := buildUserPayload(meta, inputs, transCtx, chapterIndex)
	if err != nil {
		return nil, err
	}
	prompt := systemPrompt
	if transCtx != nil {
		prompt = refinedSystemPrompt
	}
	ids := make([]string, len(inputs))
	for i, input := range inputs {
		ids[i] = input.ID
	}
	result, err := tracker.trackedChat(ctx, cfg.LLM, []ChatMessage{
		{Role: "system", Content: prompt},
		{Role: "user", Content: userPayload},
	}, true, trackedCallContext{
		stage:        StageTranslateBatch,
		chapterIndex: intPtr(chapterIndex),
		blockIDs:     ids,
		attempt:      attempt,
	})
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
		html, err := translatedHTMLFromRuns(input, found)
		if err != nil {
			return nil, err
		}
		ordered = append(ordered, translateOutput{ID: input.ID, HTML: html})
	}
	return ordered, nil
}

func withRetry[T any](ctx context.Context, fn func(attempt int) (T, error), retries int) (T, error) {
	return withRetrySleep(ctx, fn, retries, sleepWithContext)
}

func withRetrySleep[T any](ctx context.Context, fn func(attempt int) (T, error), retries int, sleep func(context.Context, time.Duration) error) (T, error) {
	var zero T
	if retries < 0 {
		retries = 0
	}
	for i := 0; i <= retries; i++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		out, err := fn(i)
		if err == nil {
			return out, nil
		}
		retryAfter, retryable := retryableLLMError(err)
		if !retryable || i == retries {
			return zero, err
		}
		delay := retryBaseDelay
		for n := 0; n < i && delay < retryMaxDelay; n++ {
			delay *= 2
			if delay > retryMaxDelay {
				delay = retryMaxDelay
			}
		}
		if retryAfter > delay {
			delay = retryAfter
		}
		if delay > retryMaxDelay {
			delay = retryMaxDelay
		}
		if err := sleep(ctx, delay); err != nil {
			return zero, err
		}
	}
	return zero, errors.New("retry loop exhausted")
}

func retryableLLMError(err error) (time.Duration, bool) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 0, false
	}
	var llmErr LLMError
	if errors.As(err, &llmErr) {
		return llmErr.RetryAfter, llmErr.Status == http.StatusRequestTimeout || llmErr.Status == http.StatusTooManyRequests || llmErr.Status >= 500
	}
	var llmErrPtr *LLMError
	if errors.As(err, &llmErrPtr) && llmErrPtr != nil {
		return llmErrPtr.RetryAfter, llmErrPtr.Status == http.StatusRequestTimeout || llmErrPtr.Status == http.StatusTooManyRequests || llmErrPtr.Status >= 500
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return 0, networkError.Timeout() || networkError.Temporary()
	}
	return 0, false
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func makeBlankTranslated(original ChapterFile) ChapterFile {
	chapters := make([]Chapter, len(original.Chapters))
	for i, chapter := range original.Chapters {
		blocks := make([]Block, len(chapter.Blocks))
		for j, block := range chapter.Blocks {
			status := BlockDone
			html := block.HTML
			if isTranslatable(block) {
				status = BlockPending
				html = ""
			}
			blocks[j] = Block{
				ID:     block.ID,
				Type:   block.Type,
				HTML:   html,
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

func (a *App) emitProgress(storyID string, translated ChapterFile, original ChapterFile, progressErrors ...ProgressError) error {
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
		p.Errors = append(p.Errors, progressErrors...)
		if len(p.Errors) > maxProgressErrors {
			p.Errors = p.Errors[len(p.Errors)-maxProgressErrors:]
		}
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
	return a.store.UpdateProgress(storyID, mutator)
}

func (a *App) updateStoryStatus(storyID string, status StoryStatus) error {
	entry, err := a.store.PatchIndex(storyID, func(entry *IndexEntry) {
		entry.Status = status
	})
	if err != nil {
		return err
	}
	if entry == nil {
		return errStoryNotFound
	}
	return nil
}

func (a *App) finishStory(storyID string, phase ProgressPhase, status StoryStatus, message string) error {
	return a.store.FinishStory(storyID, phase, status, message)
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
		if err := a.finishStory(storyID, PhaseError, StatusError, msg); err != nil {
			return err
		}
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

	tracker := newStatsTracker(a, storyID)

	effectiveMode := meta.TranslationMode
	if effectiveMode == "" {
		effectiveMode = cfg.LLM.Mode
	}
	effectiveMode = normalizeTranslationMode(effectiveMode)

	var transCtx *TranslationContext
	if effectiveMode == TranslationModeRefined {
		analysed, err := a.runAnalysis(ctx, storyID, *meta, *original, cfg, tracker)
		if err != nil {
			msg := "精翻预读分析失败: " + err.Error()
			if err := a.finishStory(storyID, PhaseError, StatusError, msg); err != nil {
				return err
			}
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

	if err := a.translatePass(ctx, storyID, cfg, *meta, original, translated, transCtx, tracker); err != nil {
		return err
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
	if err := a.finishStory(storyID, phase, status, ""); err != nil {
		return err
	}
	a.bus.Emit(storyID, StreamEvent{Type: "phase", Phase: phase})
	return nil
}

func (a *App) translatePass(ctx context.Context, storyID string, cfg Config, meta Meta, original *ChapterFile, translated *ChapterFile, transCtx *TranslationContext, tracker *statsTracker) error {
	for chIdx := 0; chIdx < len(original.Chapters); chIdx++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		orig := original.Chapters[chIdx]
		if chIdx >= len(translated.Chapters) {
			translated.Chapters = append(translated.Chapters, Chapter{Index: chIdx, Title: orig.Title})
		}
		transCh := &translated.Chapters[chIdx]
		for len(transCh.Blocks) < len(orig.Blocks) {
			ob := orig.Blocks[len(transCh.Blocks)]
			status := BlockDone
			html := ob.HTML
			if isTranslatable(ob) {
				status = BlockPending
				html = ""
			}
			transCh.Blocks = append(transCh.Blocks, Block{ID: ob.ID, Type: ob.Type, HTML: html, Status: status})
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
				if tb.Status != BlockDone || tb.HTML != ob.HTML || tb.Type != ob.Type {
					transCh.Blocks[bi] = Block{ID: ob.ID, Type: ob.Type, HTML: ob.HTML, Status: BlockDone}
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

		passCtx, cancel := context.WithCancel(ctx)
		var cursor int
		var cursorMu sync.Mutex
		var stateMu sync.Mutex
		var firstErr error
		setFirstErrorLocked := func(err error) {
			if err != nil && firstErr == nil {
				firstErr = err
				cancel()
			}
		}
		var wg sync.WaitGroup
		for worker := 0; worker < concurrency; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					if err := passCtx.Err(); err != nil {
						return
					}
					stateMu.Lock()
					stopped := firstErr != nil
					stateMu.Unlock()
					if stopped {
						return
					}
					cursorMu.Lock()
					i := cursor
					cursor++
					cursorMu.Unlock()
					if i >= len(batches) {
						return
					}

					b := batches[i]
					inputs := make([]translateInput, 0, len(b.Blocks))
					failures := make([]ProgressError, 0)
					for _, block := range b.Blocks {
						input, err := makeTranslateInput(block)
						if err == nil && cfg.LLM.MaxTokensPerRequest > 0 {
							tokens := approxTokens(input.Text)
							if tokens > cfg.LLM.MaxTokensPerRequest {
								err = fmt.Errorf("段 id=%s 预计 %d tokens，超过单次请求上限 %d", block.ID, tokens, cfg.LLM.MaxTokensPerRequest)
							}
						}
						if err != nil {
							failures = append(failures, ProgressError{ChapterIndex: chIdx, BlockID: block.ID, Message: err.Error(), At: nowISO()})
							continue
						}
						inputs = append(inputs, input)
					}

					if len(failures) > 0 {
						stateMu.Lock()
						persisted := firstErr == nil
						if persisted {
							for _, failure := range failures {
								bi := findBlockIndex(transCh.Blocks, failure.BlockID)
								if bi >= 0 {
									block := transCh.Blocks[bi]
									block.Status = BlockError
									block.Error = failure.Message
									transCh.Blocks[bi] = block
								}
							}
							if err := a.store.SaveTranslated(storyID, *translated); err != nil {
								setFirstErrorLocked(err)
								persisted = false
							} else if err := a.emitProgress(storyID, *translated, *original, failures...); err != nil {
								setFirstErrorLocked(err)
								persisted = false
							}
						}
						stateMu.Unlock()
						if !persisted {
							return
						}
						for _, failure := range failures {
							a.bus.Emit(storyID, StreamEvent{Type: "block-error", ChapterIndex: chIdx, BlockID: failure.BlockID, Message: failure.Message})
						}
					}

					if len(inputs) == 0 {
						continue
					}
					ids := make([]string, len(inputs))
					for i, input := range inputs {
						ids[i] = input.ID
					}
					a.markInflight(storyID, ids)
					stateMu.Lock()
					if firstErr != nil {
						a.clearInflight(storyID, ids)
						stateMu.Unlock()
						return
					}
					if err := a.emitProgress(storyID, *translated, *original); err != nil {
						a.clearInflight(storyID, ids)
						setFirstErrorLocked(err)
						stateMu.Unlock()
						return
					}
					stateMu.Unlock()

					outs, callErr := withRetry(passCtx, func(attempt int) ([]translateOutput, error) {
						return translateBatch(passCtx, cfg, meta, inputs, transCtx, chIdx, tracker, attempt)
					}, cfg.LLM.MaxAutoRetries)

					stateMu.Lock()
					a.clearInflight(storyID, ids)
					if firstErr != nil || (callErr != nil && passCtx.Err() != nil) {
						stateMu.Unlock()
						return
					}
					events := make([]StreamEvent, 0, len(inputs))
					progressErrors := make([]ProgressError, 0)
					if callErr != nil {
						msg := callErr.Error()
						for _, input := range inputs {
							bi := findBlockIndex(transCh.Blocks, input.ID)
							if bi < 0 {
								continue
							}
							block := transCh.Blocks[bi]
							block.Status = BlockError
							block.Error = msg
							transCh.Blocks[bi] = block
							progressErrors = append(progressErrors, ProgressError{ChapterIndex: chIdx, BlockID: input.ID, Message: msg, At: nowISO()})
							events = append(events, StreamEvent{Type: "block-error", ChapterIndex: chIdx, BlockID: input.ID, Message: msg})
						}
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
							events = append(events, StreamEvent{Type: "block-done", ChapterIndex: chIdx, BlockID: out.ID})
						}
					}
					if err := a.store.SaveTranslated(storyID, *translated); err != nil {
						setFirstErrorLocked(err)
						stateMu.Unlock()
						return
					}
					if err := a.emitProgress(storyID, *translated, *original, progressErrors...); err != nil {
						setFirstErrorLocked(err)
						stateMu.Unlock()
						return
					}
					stateMu.Unlock()
					for _, event := range events {
						a.bus.Emit(storyID, event)
					}
				}
			}()
		}
		wg.Wait()
		cancel()
		stateMu.Lock()
		persistErr := firstErr
		stateMu.Unlock()
		if persistErr != nil {
			return persistErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
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
