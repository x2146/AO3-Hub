package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const (
	analysisCacheSchemaVersion  = 1
	analysisPromptVersion       = 2
	analysisResultSchemaVersion = 1
)

var errAnalysisInputsChanged = errors.New("分析期间故事内容已变更")

// analysisJSONRules is shared by every analysis prompt. Unescaped ASCII
// quotes inside Chinese strings are by far the most common way a reply
// breaks, so the rule is spelled out instead of left to "valid JSON".
const analysisJSONRules = `输出要求（程序按此解析）：
- 只输出这一个 JSON 对象，不要 Markdown 围栏、解释或前后缀
- 字符串内引用对话或称呼时用中文引号“”或「」，不要直接写英文双引号 "；确需英文双引号必须写成 \"
- 不要尾随逗号，不要注释`

const analysisSystemPromptFull = `你是 AO3 同人文资深读者，正在为后续中文翻译做预读分析。
读完整篇英文同人后，输出一个 JSON 对象，schema 如下：

{
  "summary": "300-500 字中文全文摘要，含主线、人物动机与情感弧",
  "tone": "1-2 句中文描述风格基调，如 fluff/angst/slow burn/PWP，含 narrative POV",
  "ships": ["主要 ship，原文写法，例如 Lando Norris/Oscar Piastri"],
  "characters": [
    {"name": "原文姓名", "zh": "中文译名（同人圈惯例）", "role": "角色定位与性格关键词"}
  ],
  "glossary": { "原文专有名词": "中文译法" },
  "chapterSummaries": [
    {"index": 0, "summary": "100-150 字中文章节摘要"}
  ]
}

规则：
1) chapterSummaries 与输入的 chapters 一一对应：每章一项，index 取输入中的 index
2) characters 收录所有具名角色；glossary 同时含角色、地名、文化梗、关键术语
3) 译名遵循中文同人圈惯例，若无惯例使用最自然的音译/意译

` + analysisJSONRules

const analysisSystemPromptChapter = `你是 AO3 同人文资深读者，正在为分章预读分析做单章贡献。
读完单章英文同人后，输出一个 JSON 对象，schema 如下：

{
  "summary": "100-150 字中文章节摘要",
  "tone": "本章风格基调短语",
  "ships": ["本章涉及 ship，原文写法"],
  "characters": [{"name": "原文姓名", "zh": "中文译名", "role": "本章中的定位"}],
  "glossary": { "原文专有名词": "中文译法" }
}

` + analysisJSONRules

const analysisSystemPromptMerge = `你是 AO3 同人文资深读者，正在把多章预读分析归并为整篇背景。
基于输入的 partials（按章顺序的分章分析）和原始 meta，输出一个 JSON 对象，schema 如下：

{
  "summary": "300-500 字中文全文摘要，整合所有分章信息",
  "tone": "1-2 句中文描述全文风格基调",
  "ships": ["主要 ship，原文写法"],
  "characters": [{"name": "原文姓名", "zh": "中文译名", "role": "全文中的角色定位与性格关键词"}],
  "glossary": { "原文专有名词": "中文译法" }
}

规则：
1) 分章摘要由程序直接沿用，不要重复输出
2) characters / glossary 去重合并；同一原名出现不同译名时取出现次数最多的一个

` + analysisJSONRules

const analysisRetryInstruction = "请重新输出完整的 JSON 对象：字符串内的引号用中文引号“”或写成 \\\"，不要输出 JSON 以外的任何内容。"

var htmlTagRE = regexp.MustCompile(`<[^>]+>`)

func htmlToPlainText(s string) string {
	stripped := htmlTagRE.ReplaceAllString(s, "")
	stripped = html.UnescapeString(stripped)
	return strings.TrimSpace(stripped)
}

func chapterPlainText(chapter Chapter) string {
	parts := []string{}
	for _, block := range chapter.Blocks {
		if block.Type == BlockHR {
			parts = append(parts, "---")
			continue
		}
		text := htmlToPlainText(block.HTML)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n")
}

func metaSeed(meta Meta) map[string]any {
	return map[string]any{
		"title":         meta.Title,
		"author":        meta.Author,
		"summary":       meta.Summary,
		"fandom":        meta.Tags.Fandom,
		"relationships": meta.Tags.Relationship,
		"characters":    meta.Tags.Character,
		"additional":    meta.Tags.Additional,
		"rating":        meta.Tags.Rating,
		"warnings":      meta.Tags.Warnings,
		"categories":    meta.Tags.Categories,
	}
}

func buildAnalysisFullPayload(meta Meta, original ChapterFile) (string, error) {
	chapters := make([]map[string]any, 0, len(original.Chapters))
	for _, chapter := range original.Chapters {
		chapters = append(chapters, map[string]any{
			"index": chapter.Index,
			"title": chapter.Title,
			"text":  chapterPlainText(chapter),
		})
	}
	payload := map[string]any{
		"meta":     metaSeed(meta),
		"chapters": chapters,
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func buildAnalysisChapterPayload(meta Meta, chapter Chapter) (string, error) {
	payload := map[string]any{
		"meta": metaSeed(meta),
		"chapter": map[string]any{
			"index": chapter.Index,
			"title": chapter.Title,
			"text":  chapterPlainText(chapter),
		},
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

type chapterPartial struct {
	Index      int               `json:"index"`
	Title      string            `json:"title,omitempty"`
	Summary    string            `json:"summary"`
	Tone       string            `json:"tone,omitempty"`
	Ships      []string          `json:"ships,omitempty"`
	Characters []Character       `json:"characters,omitempty"`
	Glossary   map[string]string `json:"glossary,omitempty"`
}

func buildAnalysisMergePayload(meta Meta, partials []chapterPartial) (string, error) {
	payload := map[string]any{
		"meta":     metaSeed(meta),
		"partials": partials,
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

type analysisReply struct {
	Summary          string            `json:"summary"`
	Tone             string            `json:"tone"`
	Ships            []string          `json:"ships"`
	Characters       []Character       `json:"characters"`
	Glossary         map[string]string `json:"glossary"`
	ChapterSummaries []ChapterSummary  `json:"chapterSummaries"`
}

func decodeAnalysisReply(content, label string) (analysisReply, error) {
	raw, err := decodeLenientJSON[analysisReply](content)
	if err != nil {
		return analysisReply{}, fmt.Errorf("%s非有效 JSON: %w", label, err)
	}
	raw.Summary = strings.TrimSpace(raw.Summary)
	return raw, nil
}

func contextFromReply(raw analysisReply) TranslationContext {
	ctx := TranslationContext{
		Summary:          raw.Summary,
		Tone:             strings.TrimSpace(raw.Tone),
		Ships:            raw.Ships,
		Characters:       raw.Characters,
		Glossary:         raw.Glossary,
		ChapterSummaries: raw.ChapterSummaries,
	}
	if ctx.Glossary == nil {
		ctx.Glossary = map[string]string{}
	}
	if ctx.Ships == nil {
		ctx.Ships = []string{}
	}
	if ctx.Characters == nil {
		ctx.Characters = []Character{}
	}
	if ctx.ChapterSummaries == nil {
		ctx.ChapterSummaries = []ChapterSummary{}
	}
	return ctx
}

func parseAnalysisFullResponse(content string) (TranslationContext, error) {
	raw, err := decodeAnalysisReply(content, "分析结果")
	if err != nil {
		return TranslationContext{}, err
	}
	if raw.Summary == "" {
		return TranslationContext{}, errors.New("分析结果缺少全文摘要")
	}
	return contextFromReply(raw), nil
}

// parseAnalysisMergeResponse reads the merge reply. Chapter summaries come
// from the partials, never from the model, so the merge cannot drop or
// renumber a chapter.
func parseAnalysisMergeResponse(content string, partials []chapterPartial) (TranslationContext, error) {
	raw, err := decodeAnalysisReply(content, "归并结果")
	if err != nil {
		return TranslationContext{}, err
	}
	if raw.Summary == "" {
		return TranslationContext{}, errors.New("归并结果缺少全文摘要")
	}
	raw.ChapterSummaries = make([]ChapterSummary, len(partials))
	for i, partial := range partials {
		raw.ChapterSummaries[i] = ChapterSummary{Index: partial.Index, Title: partial.Title, Summary: partial.Summary}
	}
	return contextFromReply(raw), nil
}

func parseChapterPartial(content string, index int, title string) (chapterPartial, error) {
	raw, err := decodeAnalysisReply(content, "分章分析结果")
	if err != nil {
		return chapterPartial{}, err
	}
	if raw.Summary == "" {
		return chapterPartial{}, fmt.Errorf("章 %d 分析结果缺少摘要", index)
	}
	return chapterPartial{
		Index:      index,
		Title:      title,
		Summary:    raw.Summary,
		Tone:       strings.TrimSpace(raw.Tone),
		Ships:      raw.Ships,
		Characters: raw.Characters,
		Glossary:   raw.Glossary,
	}, nil
}

// analysisCall runs one analysis request with retries. parse is applied
// inside the tracked call, so a reply that cannot be used is recorded as a
// failed call with its raw output, and the retry replays that output together
// with the parse error instead of asking blind.
func analysisCall[T any](ctx context.Context, cfg Config, tracker *statsTracker, messages []ChatMessage, callCtx trackedCallContext, retries int, parse func(content string) (T, error)) (T, error) {
	var lastContent string
	var lastErr error
	return withRetry(ctx, func(attempt int) (T, error) {
		request := messages
		if isOutputFormatError(lastErr) {
			request = withFormatFeedback(messages, lastContent, errors.Unwrap(lastErr), analysisRetryInstruction)
		}
		call := callCtx
		call.attempt = attempt
		var out T
		result, err := tracker.trackedChat(ctx, cfg.LLM, request, true, call, func(content string) error {
			parsed, err := parse(content)
			out = parsed
			return err
		})
		// Keep the last unusable reply across transport failures, so a 5xx
		// between two bad replies does not cost the next retry its feedback.
		if err == nil || isOutputFormatError(err) {
			lastContent, lastErr = result.Content, err
		}
		return out, err
	}, retries)
}

func analyzeFullText(ctx context.Context, cfg Config, meta Meta, original ChapterFile, tracker *statsTracker) (TranslationContext, error) {
	payload, err := buildAnalysisFullPayload(meta, original)
	if err != nil {
		return TranslationContext{}, err
	}
	// The whole work is the largest prompt this app sends, and the chaptered
	// path is a working fallback, so allow one corrective retry at most.
	retries := cfg.LLM.MaxAutoRetries
	if retries > 1 {
		retries = 1
	}
	return analysisCall(ctx, cfg, tracker, []ChatMessage{
		{Role: "system", Content: analysisSystemPromptFull},
		{Role: "user", Content: payload},
	}, trackedCallContext{stage: StageAnalysisFull}, retries, func(content string) (TranslationContext, error) {
		out, err := parseAnalysisFullResponse(content)
		if err != nil {
			return TranslationContext{}, err
		}
		if err := validateAnalysisContext(&out, original); err != nil {
			return TranslationContext{}, err
		}
		return out, nil
	})
}

// chapterPartialCache lets a chaptered analysis resume: chapters found in
// known are not requested again, and save is called (concurrently) for every
// chapter that finishes. Either may be nil.
type chapterPartialCache struct {
	known map[int]chapterPartial
	save  func(chapterPartial) error
}

func analyzeByChapters(ctx context.Context, cfg Config, meta Meta, original ChapterFile, tracker *statsTracker, cache chapterPartialCache) (TranslationContext, error) {
	chapters := original.Chapters
	if len(chapters) == 0 {
		return TranslationContext{}, errors.New("原文章节为空")
	}
	partials := make([]chapterPartial, len(chapters))
	errs := make([]error, len(chapters))
	todo := make([]int, 0, len(chapters))
	for i, chapter := range chapters {
		if partial, ok := cache.known[chapter.Index]; ok && strings.TrimSpace(partial.Summary) != "" {
			partial.Title = chapter.Title
			partials[i] = partial
			continue
		}
		todo = append(todo, i)
	}

	concurrency := cfg.LLM.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(todo) {
		concurrency = len(todo)
	}

	var cursor int
	var cursorMu sync.Mutex
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				cursorMu.Lock()
				next := cursor
				cursor++
				cursorMu.Unlock()
				if next >= len(todo) {
					return
				}
				idx := todo[next]
				chapter := chapters[idx]
				payload, err := buildAnalysisChapterPayload(meta, chapter)
				if err != nil {
					errs[idx] = err
					continue
				}
				partial, err := analysisCall(ctx, cfg, tracker, []ChatMessage{
					{Role: "system", Content: analysisSystemPromptChapter},
					{Role: "user", Content: payload},
				}, trackedCallContext{
					stage:        StageAnalysisChapter,
					chapterIndex: intPtr(chapter.Index),
				}, cfg.LLM.MaxAutoRetries, func(content string) (chapterPartial, error) {
					return parseChapterPartial(content, chapter.Index, chapter.Title)
				})
				if err != nil {
					errs[idx] = err
					continue
				}
				partials[idx] = partial
				if cache.save != nil {
					// Only a resume aid: losing it costs a re-request later,
					// not this analysis.
					_ = cache.save(partial)
				}
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return TranslationContext{}, err
	}
	if err := joinChapterErrors(chapters, errs); err != nil {
		return TranslationContext{}, err
	}

	mergePayload, err := buildAnalysisMergePayload(meta, partials)
	if err != nil {
		return TranslationContext{}, err
	}
	return analysisCall(ctx, cfg, tracker, []ChatMessage{
		{Role: "system", Content: analysisSystemPromptMerge},
		{Role: "user", Content: mergePayload},
	}, trackedCallContext{stage: StageAnalysisMerge}, cfg.LLM.MaxAutoRetries, func(content string) (TranslationContext, error) {
		out, err := parseAnalysisMergeResponse(content, partials)
		if err != nil {
			return TranslationContext{}, err
		}
		if err := validateAnalysisContext(&out, original); err != nil {
			return TranslationContext{}, err
		}
		return out, nil
	})
}

// joinChapterErrors reports every failed chapter, not just the first, so the
// user can see whether one chapter or the provider as a whole is the problem.
func joinChapterErrors(chapters []Chapter, errs []error) error {
	const shown = 3
	failed := 0
	details := []string{}
	var first error
	for i, err := range errs {
		if err == nil {
			continue
		}
		failed++
		if first == nil {
			first = err
		}
		if len(details) < shown {
			details = append(details, fmt.Sprintf("章 %d: %v", chapters[i].Index, err))
		}
	}
	if failed == 0 {
		return nil
	}
	if failed > shown {
		details = append(details, fmt.Sprintf("另有 %d 章", failed-shown))
	}
	return fmt.Errorf("%d/%d 章分析失败（已完成的章节已缓存，重试时跳过）: %s: %w", failed, len(chapters), strings.Join(details, "；"), errAnalysisChapters{first})
}

// errAnalysisChapters keeps the first chapter error reachable through
// errors.Is/As without repeating it in the message.
type errAnalysisChapters struct{ err error }

func (e errAnalysisChapters) Error() string { return "分章分析未完成" }
func (e errAnalysisChapters) Unwrap() error { return e.err }

// shouldFallBackToChapters reports whether a failed whole-work analysis is
// worth retrying chapter by chapter. Auth, permission and cancellation
// failures would only fail again once per chapter.
func shouldFallBackToChapters(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var llmErr LLMError
	if errors.As(err, &llmErr) {
		return llmErr.Status != http.StatusUnauthorized && llmErr.Status != http.StatusForbidden
	}
	var llmErrPtr *LLMError
	if errors.As(err, &llmErrPtr) && llmErrPtr != nil {
		return llmErrPtr.Status != http.StatusUnauthorized && llmErrPtr.Status != http.StatusForbidden
	}
	return true
}

func alignChapterSummaries(ctx *TranslationContext, original ChapterFile) error {
	want := len(original.Chapters)
	if want == 0 {
		return errors.New("原文章节为空")
	}
	if got := len(ctx.ChapterSummaries); got != want {
		return fmt.Errorf("章节摘要数量不匹配: 期望 %d，实际 %d", want, got)
	}

	expected := make(map[int]int, want)
	for position, chapter := range original.Chapters {
		if _, exists := expected[chapter.Index]; exists {
			return fmt.Errorf("原文章节索引重复: %d", chapter.Index)
		}
		expected[chapter.Index] = position
	}
	ordered := make([]ChapterSummary, want)
	seen := make(map[int]struct{}, want)
	for _, summary := range ctx.ChapterSummaries {
		position, ok := expected[summary.Index]
		if !ok {
			return fmt.Errorf("章节摘要索引越界: %d", summary.Index)
		}
		if _, exists := seen[summary.Index]; exists {
			return fmt.Errorf("章节摘要索引重复: %d", summary.Index)
		}
		summary.Summary = strings.TrimSpace(summary.Summary)
		if summary.Summary == "" {
			return fmt.Errorf("章节 %d 摘要为空", summary.Index)
		}
		summary.Title = original.Chapters[position].Title
		ordered[position] = summary
		seen[summary.Index] = struct{}{}
	}
	ctx.ChapterSummaries = ordered
	return nil
}

func validateAnalysisContext(ctx *TranslationContext, original ChapterFile) error {
	ctx.Summary = strings.TrimSpace(ctx.Summary)
	if ctx.Summary == "" {
		return errors.New("分析结果缺少全文摘要")
	}
	return alignChapterSummaries(ctx, original)
}

func normalizedAnalysisBaseURL(raw string) (string, error) {
	normalized, err := llmEndpoint(raw, "")
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", err
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	} else {
		parsed.Host = hostname
	}
	return parsed.String(), nil
}

func analysisFingerprint(meta Meta, original ChapterFile, cfg Config) (string, error) {
	baseURL, err := normalizedAnalysisBaseURL(cfg.LLM.BaseURL)
	if err != nil {
		return "", fmt.Errorf("规范化 LLM baseURL: %w", err)
	}
	threshold := cfg.LLM.AnalysisMaxInputTokens
	if threshold <= 0 {
		threshold = 60000
	}
	payload := struct {
		CacheSchemaVersion  int            `json:"cacheSchemaVersion"`
		PromptVersion       int            `json:"promptVersion"`
		ResultSchemaVersion int            `json:"resultSchemaVersion"`
		Prompts             [3]string      `json:"prompts"`
		Meta                map[string]any `json:"meta"`
		Original            ChapterFile    `json:"original"`
		Provider            struct {
			APIType             string  `json:"apiType"`
			BaseURL             string  `json:"baseURL"`
			Model               string  `json:"model"`
			Temperature         float64 `json:"temperature"`
			MaxOutputTokens     int     `json:"maxOutputTokens"`
			AnalysisInputTokens int     `json:"analysisInputTokens"`
		} `json:"provider"`
	}{
		CacheSchemaVersion:  analysisCacheSchemaVersion,
		PromptVersion:       analysisPromptVersion,
		ResultSchemaVersion: analysisResultSchemaVersion,
		Prompts:             [3]string{analysisSystemPromptFull, analysisSystemPromptChapter, analysisSystemPromptMerge},
		Meta:                metaSeed(meta),
		Original:            original,
	}
	payload.Provider.APIType = normalizeLLMAPIType(cfg.LLM.APIType)
	payload.Provider.BaseURL = baseURL
	payload.Provider.Model = cfg.LLM.Model
	payload.Provider.Temperature = cfg.LLM.Temperature
	payload.Provider.MaxOutputTokens = cfg.LLM.MaxTokensPerRequest
	payload.Provider.AnalysisInputTokens = threshold
	digest := sha256.New()
	if err := json.NewEncoder(digest).Encode(payload); err != nil {
		return "", fmt.Errorf("编码分析缓存指纹: %w", err)
	}
	return fmt.Sprintf("v%d:%x", analysisCacheSchemaVersion, digest.Sum(nil)), nil
}

func estimateAnalysisInputTokens(meta Meta, original ChapterFile) int {
	total := 0
	for _, chapter := range original.Chapters {
		total += approxTokens(chapter.Title)
		for _, block := range chapter.Blocks {
			total += approxTokens(block.HTML)
		}
	}
	total += approxTokens(meta.Title)
	total += approxTokens(meta.Summary)
	for _, t := range meta.Tags.Fandom {
		total += approxTokens(t)
	}
	for _, t := range meta.Tags.Relationship {
		total += approxTokens(t)
	}
	for _, t := range meta.Tags.Character {
		total += approxTokens(t)
	}
	for _, t := range meta.Tags.Additional {
		total += approxTokens(t)
	}
	return total
}

func (a *App) runAnalysis(ctx context.Context, storyID string, meta Meta, original ChapterFile, cfg Config, tracker *statsTracker) (*TranslationContext, error) {
	fingerprint, err := analysisFingerprint(meta, original, cfg)
	if err != nil {
		return nil, err
	}
	existing, err := a.store.LoadContext(storyID)
	if err != nil {
		return nil, fmt.Errorf("读取分析缓存: %w", err)
	}
	if existing != nil && existing.AnalysisFingerprint == fingerprint {
		if err := validateAnalysisContext(existing, original); err != nil {
			return nil, fmt.Errorf("分析缓存无效: %w", err)
		}
		return existing, nil
	}

	if err := a.updateStoryStatus(storyID, StatusAnalyzing); err != nil {
		return nil, err
	}
	if err := a.setProgress(storyID, func(p Progress) Progress {
		p.Phase = PhaseAnalyzing
		p.Message = ""
		return p
	}); err != nil {
		return nil, err
	}
	a.bus.Emit(storyID, StreamEvent{Type: "phase", Phase: PhaseAnalyzing})

	threshold := cfg.LLM.AnalysisMaxInputTokens
	if threshold <= 0 {
		threshold = 60000
	}
	tokens := estimateAnalysisInputTokens(meta, original)

	byChapters := func() (TranslationContext, error) {
		return analyzeByChapters(ctx, cfg, meta, original, tracker, a.chapterPartialCache(storyID, fingerprint))
	}
	var result TranslationContext
	var analysisErr error
	if tokens <= threshold {
		result, analysisErr = analyzeFullText(ctx, cfg, meta, original, tracker)
		if fullErr := analysisErr; fullErr != nil && shouldFallBackToChapters(fullErr) {
			result, analysisErr = byChapters()
			if analysisErr != nil && ctx.Err() == nil {
				analysisErr = fmt.Errorf("整篇分析失败（%v），改用分章分析也失败: %w", fullErr, analysisErr)
			}
		}
	} else {
		result, analysisErr = byChapters()
	}
	if analysisErr != nil {
		return nil, analysisErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	currentMeta, err := a.store.LoadMeta(storyID)
	if err != nil {
		return nil, fmt.Errorf("保存分析缓存前读取元数据: %w", err)
	}
	currentOriginal, err := a.store.LoadOriginal(storyID)
	if err != nil {
		return nil, fmt.Errorf("保存分析缓存前读取原文: %w", err)
	}
	if currentMeta == nil || currentOriginal == nil {
		return nil, errAnalysisInputsChanged
	}
	currentFingerprint, err := analysisFingerprint(*currentMeta, *currentOriginal, cfg)
	if err != nil {
		return nil, err
	}
	if currentFingerprint != fingerprint {
		return nil, errAnalysisInputsChanged
	}

	result.GeneratedAt = nowISO()
	result.ChapterCount = len(original.Chapters)
	result.AnalysisFingerprint = fingerprint
	if err := a.store.SaveContext(storyID, result); err != nil {
		return nil, err
	}
	// The partials only exist to resume this analysis; a leftover file would
	// at worst be ignored by fingerprint, so a failed delete is not fatal.
	_ = a.store.DeleteAnalysisPartials(storyID)
	return &result, nil
}

// chapterPartialCache backs a chaptered analysis with analysis-partials.json.
// A file from a different fingerprint, or one that cannot be read, is simply
// treated as empty and overwritten by the first finished chapter.
func (a *App) chapterPartialCache(storyID, fingerprint string) chapterPartialCache {
	known := map[int]chapterPartial{}
	if stored, err := a.store.LoadAnalysisPartials(storyID); err == nil && stored != nil && stored.Fingerprint == fingerprint {
		for _, partial := range stored.Chapters {
			known[partial.Index] = partial
		}
	}
	var mu sync.Mutex
	saved := make(map[int]chapterPartial, len(known))
	for index, partial := range known {
		saved[index] = partial
	}
	return chapterPartialCache{
		known: known,
		save: func(partial chapterPartial) error {
			mu.Lock()
			defer mu.Unlock()
			saved[partial.Index] = partial
			chapters := make([]chapterPartial, 0, len(saved))
			for _, p := range saved {
				chapters = append(chapters, p)
			}
			sort.Slice(chapters, func(i, j int) bool { return chapters[i].Index < chapters[j].Index })
			return a.store.SaveAnalysisPartials(storyID, analysisPartials{Fingerprint: fingerprint, Chapters: chapters})
		},
	}
}
