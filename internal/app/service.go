package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

func (a *App) persistParsed(html string, source storySource, mode TranslationMode) (Meta, ChapterFile, bool, error) {
	parsed, err := parseAO3HTML(html)
	if err != nil {
		return Meta{}, ChapterFile{}, false, err
	}
	return a.persistParseResult(html, parsed, source, mode)
}

func (a *App) persistParseResult(html string, parsed parseResult, source storySource, mode TranslationMode) (Meta, ChapterFile, bool, error) {
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

	existing, _ := a.store.LoadMeta(id)
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

	translated, _ := a.store.LoadTranslated(id)
	var nextTranslated ChapterFile
	if translatedMatchesOriginal(translated, parsed.Original) {
		nextTranslated = *translated
	} else {
		nextTranslated = makeBlankTranslated(parsed.Original)
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

func (a *App) CreateFromURL(url string, mode TranslationMode) (map[string]string, error) {
	workID := extractWorkID(url)
	if workID == "" {
		return nil, errors.New("无法从 URL 提取 work id")
	}
	var out map[string]string
	err := a.withExclusiveStory(workID, true, func() error {
		if err := a.prepareEntry(workID, "Fetching…", "", StatusFetching); err != nil {
			return err
		}
		a.bus.Emit(workID, StreamEvent{Type: "phase", Phase: PhaseFetching})

		html, err := a.fetchDownloadHTMLContext(a.lifecycleContext(), workID)
		if err != nil {
			return a.failPreparedStory(workID, err)
		}

		meta, _, _, err := a.persistParsed(html, storySource{
			URL:         url,
			DownloadURL: fmt.Sprintf("https://archiveofourown.org/works/%s?view_full_work=true", workID),
			WorkID:      workID,
		}, mode)
		if err != nil {
			return a.failPreparedStory(workID, err)
		}
		if err := a.store.UpsertIndex(indexEntryFor(meta, StatusQueued)); err != nil {
			return a.failPreparedStory(workID, err)
		}
		a.queue.Enqueue(Job{StoryID: workID, Type: "translate"})
		out = map[string]string{"id": workID, "status": string(StatusQueued)}
		return nil
	})
	return out, err
}

func (a *App) CreateFromHTML(html string, mode TranslationMode) (map[string]string, error) {
	parsed, err := parseAO3HTML(html)
	if err != nil {
		return nil, err
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
		a.queue.Enqueue(Job{StoryID: storyID, Type: "translate"})
		out = map[string]string{"id": storyID, "status": string(StatusQueued)}
		return nil
	})
	return out, err
}

func (a *App) RetryStory(id string, blockIDs []string, chapterIndex *int, mode TranslationMode) error {
	if err := validateStoryID(id); err != nil {
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
		idSet := map[string]bool{}
		if len(blockIDs) > 0 {
			for _, blockID := range blockIDs {
				idSet[blockID] = true
			}
		}
		for ci := range translated.Chapters {
			if chapterIndex != nil && ci != *chapterIndex {
				continue
			}
			for bi := range translated.Chapters[ci].Blocks {
				block := translated.Chapters[ci].Blocks[bi]
				if len(idSet) > 0 && !idSet[block.ID] {
					continue
				}
				if len(idSet) == 0 && block.Status != BlockError {
					continue
				}
				if ci >= len(original.Chapters) || bi >= len(original.Chapters[ci].Blocks) {
					continue
				}
				ob := original.Chapters[ci].Blocks[bi]
				if !isTranslatable(ob) {
					block.Status = BlockDone
					block.HTML = ob.HTML
					block.Error = ""
					translated.Chapters[ci].Blocks[bi] = block
					continue
				}
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
		a.queue.Enqueue(Job{StoryID: id, Type: "translate"})
		return nil
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
		a.queue.Enqueue(Job{StoryID: id, Type: "translate"})
		return nil
	})
}

func (a *App) ResumeOnStartup() error {
	idx, err := a.store.LoadIndex()
	if err != nil {
		return err
	}
	for _, entry := range idx.Stories {
		if entry.Status == StatusReady || entry.Status == StatusError {
			continue
		}
		progress, err := a.store.LoadProgress(entry.ID)
		if err != nil || progress == nil {
			continue
		}
		if progress.Phase == PhaseReady || progress.Phase == PhaseError {
			continue
		}
		a.queue.Enqueue(Job{StoryID: entry.ID, Type: "translate"})
	}
	return nil
}
