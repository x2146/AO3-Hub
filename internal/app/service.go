package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	maxImportChapters         = 2000
	maxImportBlocks           = 50000
	maxImportBlocksPerChapter = 20000
	maxImportBlockBytes       = 2 << 20
	maxImportContentBytes     = 32 << 20
	maxImportTags             = 1000
)

var (
	errImportTooLarge         = errors.New("AO3 work exceeds import limits")
	errInvalidImport          = errors.New("invalid AO3 work document")
	errAO3Upstream            = errors.New("AO3 upstream response failed")
	errInvalidRetrySelection  = errors.New("invalid retry selection")
	errInvalidTranslationMode = errors.New("invalid translation mode")
)

type storySource struct {
	URL         string
	DownloadURL string
	WorkID      string
}

func indexEntryFor(meta Meta, status StoryStatus) IndexEntry {
	now := nowISO()
	return IndexEntry{
		ID:           meta.ID,
		Title:        meta.Title,
		ChineseTitle: meta.ChineseTitle,
		Author:       meta.Author,
		ChapterCount: meta.ChapterCount,
		WordCount:    meta.WordCount,
		Status:       status,
		AddedAt:      now,
		UpdatedAt:    now,
	}
}

func (a *App) persistParseResult(html string, parsed parseResult, source storySource, mode TranslationMode) (Meta, ChapterFile, bool, error) {
	if err := validateParsedImport(parsed); err != nil {
		return Meta{}, ChapterFile{}, false, err
	}
	id := source.WorkID
	if id == "" {
		id = parsed.Meta.WorkIDGuess
	}
	if id == "" {
		id = randomStoryID()
	}
	if err := validateStoryID(id); err != nil {
		return Meta{}, ChapterFile{}, false, err
	}
	url := source.URL
	if url == "" {
		url = parsed.Meta.WorkURLGuess
	}
	if url == "" {
		url = fmt.Sprintf("https://archiveofourown.org/works/%s", id)
	}
	meta := parsed.Meta.Meta
	meta.ID = id
	meta.URL = url
	meta.DownloadURL = source.DownloadURL

	existing, err := a.store.LoadMeta(id)
	if err != nil {
		return Meta{}, ChapterFile{}, false, err
	}
	translated, err := a.store.LoadTranslated(id)
	if err != nil {
		return Meta{}, ChapterFile{}, false, err
	}
	switch {
	case strings.TrimSpace(string(mode)) != "":
		meta.TranslationMode = normalizeTranslationMode(mode)
	case existing != nil:
		meta.TranslationMode = existing.TranslationMode
	}
	meta = normalizeMeta(meta)

	isNew := !a.store.StoryExists(id)
	if err := a.store.SaveSource(id, html); err != nil {
		return Meta{}, ChapterFile{}, false, err
	}
	if err := a.store.SaveMeta(id, meta); err != nil {
		return Meta{}, ChapterFile{}, false, err
	}
	if err := a.store.SaveOriginal(id, parsed.Original); err != nil {
		return Meta{}, ChapterFile{}, false, err
	}

	var nextTranslated ChapterFile
	if translatedMatchesOriginal(translated, parsed.Original) {
		nextTranslated = *translated
	} else {
		nextTranslated = carryOverTranslated(translated, parsed.Original)
	}
	if err := a.store.SaveTranslated(id, nextTranslated); err != nil {
		return Meta{}, ChapterFile{}, false, err
	}

	total := 0
	for _, chapter := range parsed.Original.Chapters {
		for _, block := range chapter.Blocks {
			if isTranslatable(block) {
				total++
			}
		}
	}
	done := 0
	for ci, chapter := range parsed.Original.Chapters {
		for bi, block := range chapter.Blocks {
			if !isTranslatable(block) || ci >= len(nextTranslated.Chapters) || bi >= len(nextTranslated.Chapters[ci].Blocks) {
				continue
			}
			if nextTranslated.Chapters[ci].Blocks[bi].Status == BlockDone {
				done++
			}
		}
	}
	progress := Progress{
		Phase:       PhaseQueued,
		TotalBlocks: total,
		DoneBlocks:  done,
		StartedAt:   nowISO(),
		Errors:      []ProgressError{},
	}
	if err := a.store.SaveProgress(id, progress); err != nil {
		return Meta{}, ChapterFile{}, false, err
	}

	return meta, parsed.Original, isNew, nil
}

func validateParsedImport(parsed parseResult) error {
	if len(parsed.Original.Chapters) > maxImportChapters {
		return fmt.Errorf("%w: more than %d chapters", errImportTooLarge, maxImportChapters)
	}
	meta := parsed.Meta.Meta
	contentBytes := len(meta.Title) + len(meta.ChineseTitle) + len(meta.Author) + len(meta.AuthorURL) + len(meta.Summary) + len(meta.Notes) + len(meta.Endnotes) + len(meta.Language) + len(meta.PublishedAt) + len(meta.UpdatedAt)
	tagGroups := [][]string{meta.Tags.Fandom, meta.Tags.Relationship, meta.Tags.Character, meta.Tags.Additional, meta.Tags.Warnings, meta.Tags.Categories}
	tagCount := 0
	for _, group := range tagGroups {
		tagCount += len(group)
		for _, tag := range group {
			contentBytes += len(tag)
		}
	}
	contentBytes += len(meta.Tags.Rating)
	if tagCount > maxImportTags {
		return fmt.Errorf("%w: more than %d tags", errImportTooLarge, maxImportTags)
	}
	if contentBytes > maxImportContentBytes {
		return fmt.Errorf("%w: extracted content exceeds %d bytes", errImportTooLarge, maxImportContentBytes)
	}
	totalBlocks := 0
	for _, chapter := range parsed.Original.Chapters {
		if len(chapter.Blocks) > maxImportBlocksPerChapter {
			return fmt.Errorf("%w: chapter contains more than %d blocks", errImportTooLarge, maxImportBlocksPerChapter)
		}
		totalBlocks += len(chapter.Blocks)
		if totalBlocks > maxImportBlocks {
			return fmt.Errorf("%w: more than %d blocks", errImportTooLarge, maxImportBlocks)
		}
		contentBytes += len(chapter.Title)
		for _, block := range chapter.Blocks {
			if len(block.HTML) > maxImportBlockBytes {
				return fmt.Errorf("%w: block exceeds %d bytes", errImportTooLarge, maxImportBlockBytes)
			}
			contentBytes += len(block.HTML)
			if contentBytes > maxImportContentBytes {
				return fmt.Errorf("%w: extracted content exceeds %d bytes", errImportTooLarge, maxImportContentBytes)
			}
		}
	}
	return nil
}

func validateRequestedMode(mode TranslationMode) error {
	if mode != "" && !validTranslationMode(mode) {
		return errInvalidTranslationMode
	}
	return nil
}

func (a *App) lifecycleContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) withExclusiveStory(storyID string, cancelQueued bool, fn func() error) error {
	if cancelQueued && a.queue != nil {
		a.queue.CancelAndWait(storyID)
	}
	release, err := a.acquireStory(a.lifecycleContext(), storyID)
	if err != nil {
		return err
	}
	defer release()
	if cancelQueued && a.queue != nil {
		a.queue.CancelAndWait(storyID)
	}
	return fn()
}

func translatedMatchesOriginal(translated *ChapterFile, original ChapterFile) bool {
	if translated == nil || len(translated.Chapters) != len(original.Chapters) {
		return false
	}
	for ci, chapter := range original.Chapters {
		if len(translated.Chapters[ci].Blocks) != len(chapter.Blocks) {
			return false
		}
		for bi, block := range chapter.Blocks {
			tBlock := translated.Chapters[ci].Blocks[bi]
			if tBlock.ID != block.ID || tBlock.Type != block.Type {
				return false
			}
		}
	}
	return true
}

// carryOverTranslated builds a blank translation for original and keeps every
// finished block whose ID and type still exist, so re-parsing a story only
// re-translates the blocks that actually changed.
func carryOverTranslated(previous *ChapterFile, original ChapterFile) ChapterFile {
	next := makeBlankTranslated(original)
	if previous == nil {
		return next
	}
	done := map[string]Block{}
	for _, chapter := range previous.Chapters {
		for _, block := range chapter.Blocks {
			if block.Status == BlockDone {
				done[block.ID] = block
			}
		}
	}
	for ci, chapter := range original.Chapters {
		for bi, block := range chapter.Blocks {
			if !isTranslatable(block) {
				continue
			}
			if prev, ok := done[block.ID]; ok && prev.Type == block.Type {
				next.Chapters[ci].Blocks[bi] = prev
			}
		}
	}
	return next
}

func hasOversizedBlock(file ChapterFile) bool {
	for _, chapter := range file.Chapters {
		for _, block := range chapter.Blocks {
			if isTranslatable(block) && len(htmlToPlainText(block.HTML)) > maxBlockTextBytes {
				return true
			}
		}
	}
	return false
}

// repairOversizedOriginal re-parses the saved source of a story imported before
// oversized blocks were split, carrying finished translations over by block ID.
// It reports whether the stored original was replaced.
func (a *App) repairOversizedOriginal(id string, original ChapterFile, translated *ChapterFile) (*ChapterFile, *ChapterFile, bool, error) {
	if !hasOversizedBlock(original) {
		return &original, translated, false, nil
	}
	source, ok, err := a.store.LoadSource(id)
	if err != nil || !ok {
		return &original, translated, false, err
	}
	parsed, err := parseAO3HTML(source)
	if err != nil || len(parsed.Original.Chapters) != len(original.Chapters) {
		return &original, translated, false, nil
	}
	next := carryOverTranslated(translated, parsed.Original)
	if err := a.store.SaveOriginal(id, parsed.Original); err != nil {
		return nil, nil, false, err
	}
	if err := a.store.SaveTranslated(id, next); err != nil {
		return nil, nil, false, err
	}
	return &parsed.Original, &next, true, nil
}

func (a *App) prepareEntry(id, title, author string, status StoryStatus) error {
	idx, err := a.store.LoadIndex()
	if err != nil {
		return err
	}
	for _, entry := range idx.Stories {
		if entry.ID == id {
			_, err := a.store.PatchIndex(id, func(e *IndexEntry) {
				e.Status = status
			})
			return err
		}
	}
	return a.store.UpsertIndex(IndexEntry{
		ID:           id,
		Title:        title,
		Author:       author,
		ChapterCount: 0,
		WordCount:    0,
		Status:       status,
		AddedAt:      nowISO(),
		UpdatedAt:    nowISO(),
	})
}

func (a *App) failPreparedStory(id string, cause error) error {
	terminalErr := a.finishStory(id, PhaseError, StatusError, cause.Error())
	if terminalErr == nil {
		a.bus.Emit(id, StreamEvent{Type: "phase", Phase: PhaseError, Message: cause.Error()})
		return cause
	}
	statusErr := a.updateStoryStatus(id, StatusError)
	if statusErr == nil {
		a.bus.Emit(id, StreamEvent{Type: "phase", Phase: PhaseError, Message: cause.Error()})
		if errors.Is(terminalErr, errStoryNotFound) {
			return cause
		}
		return errors.Join(cause, fmt.Errorf("persist story failure state: %w", terminalErr))
	}
	return errors.Join(
		cause,
		fmt.Errorf("persist story terminal state: %w", terminalErr),
		fmt.Errorf("persist story error status: %w", statusErr),
	)
}

func (a *App) enqueueStory(id string) error {
	if err := a.queue.Enqueue(Job{StoryID: id, Type: "translate"}); err != nil {
		stateErr := a.finishStory(id, PhaseError, StatusError, err.Error())
		if stateErr != nil {
			return errors.Join(err, fmt.Errorf("persist queue rejection: %w", stateErr))
		}
		a.bus.Emit(id, StreamEvent{Type: "phase", Phase: PhaseError, Message: err.Error()})
		return err
	}
	return nil
}

func (a *App) CreateFromURL(url string, mode TranslationMode) (map[string]string, error) {
	if err := validateRequestedMode(mode); err != nil {
		return nil, err
	}
	canonicalURL, workID, err := normalizeAO3WorkURL(url)
	if err != nil {
		return nil, err
	}
	var out map[string]string
	err = a.withExclusiveStory(workID, true, func() error {
		if err := a.prepareEntry(workID, "Fetching…", "", StatusFetching); err != nil {
			return err
		}
		a.bus.Emit(workID, StreamEvent{Type: "phase", Phase: PhaseFetching})

		html, err := a.fetchDownloadHTMLContext(a.lifecycleContext(), workID)
		if err != nil {
			return a.failPreparedStory(workID, fmt.Errorf("%w: %w", errAO3Upstream, err))
		}

		parsed, err := parseAO3HTML(html)
		if err != nil {
			return a.failPreparedStory(workID, fmt.Errorf("%w: %w", errAO3Upstream, err))
		}
		meta, _, _, err := a.persistParseResult(html, parsed, storySource{
			URL:         canonicalURL,
			DownloadURL: canonicalURL + "?view_full_work=true",
			WorkID:      workID,
		}, mode)
		if err != nil {
			return a.failPreparedStory(workID, err)
		}
		if err := a.store.UpsertIndex(indexEntryFor(meta, StatusQueued)); err != nil {
			return a.failPreparedStory(workID, err)
		}
		if err := a.enqueueStory(workID); err != nil {
			return err
		}
		out = map[string]string{"id": workID, "status": string(StatusQueued)}
		return nil
	})
	return out, err
}

func (a *App) CreateFromHTML(html string, mode TranslationMode) (map[string]string, error) {
	if err := validateRequestedMode(mode); err != nil {
		return nil, err
	}
	parsed, err := parseAO3HTML(html)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidImport, err)
	}
	storyID := parsed.Meta.WorkIDGuess
	if storyID == "" {
		storyID = randomStoryID()
	}
	if err := validateStoryID(storyID); err != nil {
		return nil, err
	}
	var out map[string]string
	err = a.withExclusiveStory(storyID, true, func() error {
		meta, _, _, err := a.persistParseResult(html, parsed, storySource{WorkID: storyID}, mode)
		if err != nil {
			return err
		}
		if err := a.store.UpsertIndex(indexEntryFor(meta, StatusQueued)); err != nil {
			return err
		}
		if err := a.enqueueStory(storyID); err != nil {
			return err
		}
		out = map[string]string{"id": storyID, "status": string(StatusQueued)}
		return nil
	})
	return out, err
}

func (a *App) RetryStory(id string, blockIDs []string, chapterIndex *int, mode TranslationMode) error {
	if err := validateStoryID(id); err != nil {
		return err
	}
	if err := validateRequestedMode(mode); err != nil {
		return err
	}
	return a.withExclusiveStory(id, true, func() error {
		translated, err := a.store.LoadTranslated(id)
		if err != nil {
			return err
		}
		if translated == nil {
			return errStoryNotFound
		}
		original, err := a.store.LoadOriginal(id)
		if err != nil {
			return err
		}
		if original == nil {
			return errStoryNotFound
		}
		if chapterIndex != nil && (*chapterIndex < 0 || *chapterIndex >= len(original.Chapters)) {
			return errInvalidRetrySelection
		}
		idSet := map[string]bool{}
		for _, blockID := range blockIDs {
			idSet[blockID] = true
		}
		original, translated, repaired, err := a.repairOversizedOriginal(id, *original, translated)
		if err != nil {
			return err
		}
		if repaired {
			// The selected blocks may have been split into new ones; the next
			// pass translates every unfinished block anyway.
			idSet = map[string]bool{}
		}
		if len(idSet) > 0 {
			matchedIDs := map[string]bool{}
			for ci, chapter := range original.Chapters {
				if chapterIndex != nil && ci != *chapterIndex {
					continue
				}
				for _, block := range chapter.Blocks {
					if idSet[block.ID] {
						matchedIDs[block.ID] = true
					}
				}
			}
			if len(matchedIDs) != len(idSet) {
				return errInvalidRetrySelection
			}
		}
		if strings.TrimSpace(string(mode)) != "" {
			nextMode := normalizeTranslationMode(mode)
			meta, err := a.store.LoadMeta(id)
			if err != nil {
				return err
			}
			if meta == nil {
				return errStoryNotFound
			}
			if meta.TranslationMode != nextMode {
				meta.TranslationMode = nextMode
				if err := a.store.SaveMeta(id, *meta); err != nil {
					return err
				}
			}
		}
		for ci := range translated.Chapters {
			if chapterIndex != nil && ci != *chapterIndex {
				continue
			}
			if ci >= len(original.Chapters) {
				continue
			}
			for bi := range translated.Chapters[ci].Blocks {
				if bi >= len(original.Chapters[ci].Blocks) {
					continue
				}
				block := translated.Chapters[ci].Blocks[bi]
				ob := original.Chapters[ci].Blocks[bi]
				if len(idSet) > 0 && !idSet[ob.ID] {
					continue
				}
				if len(idSet) == 0 && block.Status != BlockError {
					continue
				}
				if !isTranslatable(ob) {
					translated.Chapters[ci].Blocks[bi] = Block{ID: ob.ID, Type: ob.Type, HTML: ob.HTML, Status: BlockDone}
					continue
				}
				block.ID = ob.ID
				block.Type = ob.Type
				block.Status = BlockPending
				block.HTML = ""
				block.Error = ""
				translated.Chapters[ci].Blocks[bi] = block
			}
		}
		if err := a.store.SaveTranslated(id, *translated); err != nil {
			return err
		}
		if err := a.store.QueueStory(id); err != nil {
			return err
		}
		return a.enqueueStory(id)
	})
}

func (a *App) DeleteStory(id string) error {
	if err := validateStoryID(id); err != nil {
		return err
	}
	return a.withExclusiveStory(id, true, func() error {
		return a.store.RemoveStoryAndIndex(id)
	})
}

func (a *App) ReanalyzeStory(id string) error {
	if err := validateStoryID(id); err != nil {
		return err
	}
	return a.withExclusiveStory(id, true, func() error {
		meta, err := a.store.LoadMeta(id)
		if err != nil {
			return err
		}
		if meta == nil {
			return errStoryNotFound
		}
		if err := a.store.DeleteContext(id); err != nil {
			return err
		}
		meta.TranslationMode = TranslationModeRefined
		if err := a.store.SaveMeta(id, *meta); err != nil {
			return err
		}
		if err := a.store.QueueStory(id); err != nil {
			return err
		}
		return a.enqueueStory(id)
	})
}

func (a *App) ResumeOnStartup() error {
	idx, err := a.store.LoadIndex()
	if err != nil {
		return err
	}
	for _, entry := range idx.Stories {
		if !a.store.StoryExists(entry.ID) {
			if err := a.store.RemoveIndexEntry(entry.ID); err != nil {
				return fmt.Errorf("remove stale story %s: %w", entry.ID, err)
			}
			continue
		}
		progress, err := a.store.LoadProgress(entry.ID)
		if err != nil || progress == nil {
			message := "启动恢复失败：progress.json 缺失"
			if err != nil {
				message = "启动恢复失败：" + err.Error()
			}
			failed := Progress{
				Phase:      PhaseError,
				StartedAt:  nowISO(),
				FinishedAt: nowISO(),
				Message:    message,
				Errors:     []ProgressError{},
			}
			if stateErr := a.store.ReconcileStoryState(entry.ID, failed, StatusError); stateErr != nil {
				return fmt.Errorf("repair unrecoverable story %s: %w", entry.ID, stateErr)
			}
			continue
		}
		status, valid := storyStatusForPhase(progress.Phase)
		if !valid {
			invalidPhase := progress.Phase
			progress.Phase = PhaseError
			progress.CurrentChapter = nil
			progress.InflightBlocks = 0
			progress.FinishedAt = nowISO()
			progress.Message = fmt.Sprintf("启动恢复失败：未知进度阶段 %q", invalidPhase)
			status = StatusError
		}
		if entry.Status != status || !valid {
			if stateErr := a.store.ReconcileStoryState(entry.ID, *progress, status); stateErr != nil {
				return fmt.Errorf("reconcile story %s: %w", entry.ID, stateErr)
			}
		}
		if status == StatusReady || status == StatusError {
			continue
		}
		if err := a.queue.Enqueue(Job{StoryID: entry.ID, Type: "translate"}); err != nil {
			if errors.Is(err, errQueueFull) {
				if stateErr := a.finishStory(entry.ID, PhaseError, StatusError, err.Error()); stateErr != nil {
					return fmt.Errorf("mark unresumed story %s: %w", entry.ID, stateErr)
				}
				continue
			}
			return fmt.Errorf("resume story %s: %w", entry.ID, err)
		}
	}
	return nil
}

func storyStatusForPhase(phase ProgressPhase) (StoryStatus, bool) {
	switch phase {
	case PhaseQueued:
		return StatusQueued, true
	case PhaseFetching:
		return StatusFetching, true
	case PhaseParsing:
		return StatusParsing, true
	case PhaseAnalyzing:
		return StatusAnalyzing, true
	case PhaseTranslating:
		return StatusTranslating, true
	case PhaseReady:
		return StatusReady, true
	case PhaseError:
		return StatusError, true
	default:
		return StatusError, false
	}
}
