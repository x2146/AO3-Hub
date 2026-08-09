package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

const (
	analysisCacheSchemaVersion  = 1
	analysisPromptVersion       = 1
	analysisResultSchemaVersion = 1
)

var errAnalysisInputsChanged = errors.New("分析期间故事内容已变更")

const analysisSystemPromptFull = `你是 AO3 同人文资深读者，正在为后续中文翻译做预读分析。
读完整篇英文同人后，仅输出一个 JSON 对象，schema 如下：

{
  "summary": "300-500 字中文全文摘要，含主线、人物动机与情感弧",
  "tone": "1-2 句中文描述风格基调，如 fluff/angst/slow burn/PWP，含 narrative POV",
  "ships": ["主要 ship，原文写法，例如 Lando Norris/Oscar Piastri"],
  "characters": [
    {"name": "原文姓名", "zh": "中文译名（同人圈惯例）", "role": "角色定位与性格关键词"}
  ],
  "glossary": { "原文专有名词": "中文译法" },
  "chapterSummaries": [
    {"index": 0, "title": "原文章节标题，可空", "summary": "100-150 字中文章节摘要"}
  ]
}

规则：
1) chapterSummaries 必须与输入的 chapters 数组一一对应，长度严格相等
2) characters 收录所有具名角色；glossary 同时含角色、地名、文化梗、关键术语
3) 译名遵循中文同人圈惯例，若无惯例使用最自然的音译/意译
4) 仅输出 JSON 对象本身，无 Markdown 围栏、无解释、无前后空行`

const analysisSystemPromptChapter = `你是 AO3 同人文资深读者，正在为分章预读分析做单章贡献。
读完单章英文同人后，仅输出一个 JSON 对象，schema 如下：

{
  "summary": "100-150 字中文章节摘要",
  "tone": "本章风格基调短语",
  "ships": ["本章涉及 ship，原文写法"],
  "characters": [{"name": "原文姓名", "zh": "中文译名", "role": "本章中的定位"}],
  "glossary": { "原文专有名词": "中文译法" }
}

规则：仅输出 JSON 对象本身，无 Markdown 围栏、无解释。`

const analysisSystemPromptMerge = `你是 AO3 同人文资深读者，正在归并多章预读分析为整篇背景。
基于输入 partials 和原始 meta，仅输出一个 JSON 对象，schema 如下：

{
  "summary": "300-500 字中文全文摘要，整合所有分章信息",
  "tone": "1-2 句中文描述全文风格基调",
  "ships": ["主要 ship，原文写法"],
  "characters": [{"name": "原文姓名", "zh": "中文译名", "role": "全文中的角色定位与性格关键词"}],
  "glossary": { "原文专有名词": "中文译法" },
  "chapterSummaries": [{"index": 0, "title": "原文章节标题", "summary": "100-150 字中文章节摘要"}]
}

规则：
1) chapterSummaries 完整对应输入 partials，每章对应一项，按 index 排序
2) characters / glossary 去重合并；同名优先取出现次数最多的中文译名
3) 仅输出 JSON 对象本身`

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

func stripJSONFences(content string) string {
	s := strings.TrimSpace(content)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "```json"))
		s = strings.TrimSpace(strings.TrimPrefix(s, "```"))
		s = strings.TrimSpace(strings.TrimSuffix(s, "```"))
	}
	return s
}

func decodeAnalysisJSON(content string, dst any, label string) error {
	dec := json.NewDecoder(strings.NewReader(stripJSONFences(content)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%s非有效 JSON: %w", label, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%s包含多个 JSON 值", label)
		}
		return fmt.Errorf("%s包含多余内容: %w", label, err)
	}
	return nil
}

func parseAnalysisFullResponse(content string) (TranslationContext, error) {
	var raw struct {
		Summary          string            `json:"summary"`
		Tone             string            `json:"tone"`
		Ships            []string          `json:"ships"`
		Characters       []Character       `json:"characters"`
		Glossary         map[string]string `json:"glossary"`
		ChapterSummaries []ChapterSummary  `json:"chapterSummaries"`
	}
	if err := decodeAnalysisJSON(content, &raw, "分析结果"); err != nil {
		return TranslationContext{}, err
	}
	raw.Summary = strings.TrimSpace(raw.Summary)
	if raw.Summary == "" {
		return TranslationContext{}, errors.New("分析结果缺少全文摘要")
	}
	ctx := TranslationContext{
		Summary:          raw.Summary,
		Tone:             raw.Tone,
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
	return ctx, nil
}

func parseChapterPartial(content string, index int, title string) (chapterPartial, error) {
	var raw struct {
		Summary    string            `json:"summary"`
		Tone       string            `json:"tone"`
		Ships      []string          `json:"ships"`
		Characters []Character       `json:"characters"`
		Glossary   map[string]string `json:"glossary"`
	}
	if err := decodeAnalysisJSON(content, &raw, "分章分析结果"); err != nil {
		return chapterPartial{}, err
	}
	raw.Summary = strings.TrimSpace(raw.Summary)
	if raw.Summary == "" {
		return chapterPartial{}, fmt.Errorf("章 %d 分析结果缺少摘要", index)
	}
	return chapterPartial{
		Index:      index,
		Title:      title,
		Summary:    raw.Summary,
		Tone:       raw.Tone,
		Ships:      raw.Ships,
		Characters: raw.Characters,
		Glossary:   raw.Glossary,
	}, nil
}

func analyzeFullText(ctx context.Context, cfg Config, meta Meta, original ChapterFile, tracker *statsTracker) (TranslationContext, error) {
	payload, err := buildAnalysisFullPayload(meta, original)
	if err != nil {
		return TranslationContext{}, err
	}
	result, err := tracker.trackedChat(ctx, cfg.LLM, []ChatMessage{
		{Role: "system", Content: analysisSystemPromptFull},
		{Role: "user", Content: payload},
	}, true, trackedCallContext{stage: StageAnalysisFull})
	if err != nil {
		return TranslationContext{}, err
	}
	out, err := parseAnalysisFullResponse(result.Content)
	if err != nil {
		return TranslationContext{}, err
	}
	if err := validateAnalysisContext(&out, original); err != nil {
		return TranslationContext{}, err
	}
	return out, nil
}

func analyzeByChapters(ctx context.Context, cfg Config, meta Meta, original ChapterFile, tracker *statsTracker) (TranslationContext, error) {
	chapters := original.Chapters
	if len(chapters) == 0 {
		return TranslationContext{}, errors.New("原文章节为空")
	}
	partials := make([]chapterPartial, len(chapters))
	errs := make([]error, len(chapters))

	concurrency := cfg.LLM.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(chapters) {
		concurrency = len(chapters)
	}

	var cursor int
	var cursorMu sync.Mutex
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				cursorMu.Lock()
				idx := cursor
				cursor++
				cursorMu.Unlock()
				if idx >= len(chapters) {
					return
				}
				chapter := chapters[idx]
				payload, err := buildAnalysisChapterPayload(meta, chapter)
				if err != nil {
					errs[idx] = err
					continue
				}
				partial, callErr := withRetry(ctx, func(attempt int) (chapterPartial, error) {
					result, err := tracker.trackedChat(ctx, cfg.LLM, []ChatMessage{
						{Role: "system", Content: analysisSystemPromptChapter},
						{Role: "user", Content: payload},
					}, true, trackedCallContext{
						stage:        StageAnalysisChapter,
						chapterIndex: intPtr(chapter.Index),
						attempt:      attempt,
					})
					if err != nil {
						return chapterPartial{}, err
					}
					return parseChapterPartial(result.Content, chapter.Index, chapter.Title)
				}, cfg.LLM.MaxAutoRetries)
				if callErr != nil {
					errs[idx] = fmt.Errorf("章 %d 分析失败: %w", chapter.Index, callErr)
					continue
				}
				partials[idx] = partial
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return TranslationContext{}, err
		}
	}

	mergePayload, err := buildAnalysisMergePayload(meta, partials)
	if err != nil {
		return TranslationContext{}, err
	}
	mergeResult, err := tracker.trackedChat(ctx, cfg.LLM, []ChatMessage{
		{Role: "system", Content: analysisSystemPromptMerge},
		{Role: "user", Content: mergePayload},
	}, true, trackedCallContext{stage: StageAnalysisMerge})
	if err != nil {
		return TranslationContext{}, err
	}
	out, err := parseAnalysisFullResponse(mergeResult.Content)
	if err != nil {
		return TranslationContext{}, err
	}
	if err := validateAnalysisContext(&out, original); err != nil {
		return TranslationContext{}, err
	}
	return out, nil
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

	var result TranslationContext
	var analysisErr error
	if tokens <= threshold {
		result, analysisErr = analyzeFullText(ctx, cfg, meta, original, tracker)
		if analysisErr != nil {
			result, analysisErr = analyzeByChapters(ctx, cfg, meta, original, tracker)
		}
	} else {
		result, analysisErr = analyzeByChapters(ctx, cfg, meta, original, tracker)
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
	return &result, nil
}
