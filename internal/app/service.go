package app

import (
	"errors"
	"fmt"
	"strings"
)

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

func (a *App) persistParsed(html string, source struct {
	URL         string
	DownloadURL string
	WorkID      string
}, mode TranslationMode) (Meta, ChapterFile, bool, error) {
	parsed, err := parseAO3HTML(html)
	if err != nil {
		return Meta{}, ChapterFile{}, false, err
	}
	id := source.WorkID
	if id == "" {
		id = parsed.Meta.WorkIDGuess
	}
	if id == "" {
		id = randomStoryID()
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

func (a *App) CreateFromURL(url string, mode TranslationMode) (map[string]string, error) {
	workID := extractWorkID(url)
	if workID == "" {
		return nil, errors.New("无法从 URL 提取 work id")
	}
	if err := a.prepareEntry(workID, "Fetching…", "", StatusFetching); err != nil {
		return nil, err
	}
	a.bus.Emit(workID, StreamEvent{Type: "phase", Phase: PhaseFetching})

	html, err := a.fetchDownloadHTML(workID)
	if err != nil {
		_, _ = a.store.PatchIndex(workID, func(entry *IndexEntry) {
			entry.Status = StatusError
		})
		a.bus.Emit(workID, StreamEvent{Type: "phase", Phase: PhaseError, Message: err.Error()})
		return nil, err
	}

	meta, _, _, err := a.persistParsed(html, struct {
		URL         string
		DownloadURL string
		WorkID      string
	}{
		URL:         url,
		DownloadURL: fmt.Sprintf("https://archiveofourown.org/works/%s?view_full_work=true", workID),
		WorkID:      workID,
	}, mode)
	if err != nil {
		return nil, err
	}
	if err := a.store.UpsertIndex(indexEntryFor(meta, StatusQueued)); err != nil {
		return nil, err
	}
	a.queue.Enqueue(Job{StoryID: workID, Type: "translate"})
	return map[string]string{"id": workID, "status": string(StatusQueued)}, nil
}

func (a *App) CreateFromHTML(html string, mode TranslationMode) (map[string]string, error) {
	meta, _, _, err := a.persistParsed(html, struct {
		URL         string
		DownloadURL string
		WorkID      string
	}{}, mode)
	if err != nil {
		return nil, err
	}
	if err := a.store.UpsertIndex(indexEntryFor(meta, StatusQueued)); err != nil {
		return nil, err
	}
	a.queue.Enqueue(Job{StoryID: meta.ID, Type: "translate"})
	return map[string]string{"id": meta.ID, "status": string(StatusQueued)}, nil
}

func (a *App) RetryStory(id string, blockIDs []string, chapterIndex *int, mode TranslationMode) error {
	translated, err := a.store.LoadTranslated(id)
	if err != nil || translated == nil {
		return errors.New("story not found")
	}
	original, err := a.store.LoadOriginal(id)
	if err != nil || original == nil {
		return errors.New("story not found")
	}
	if strings.TrimSpace(string(mode)) != "" {
		nextMode := normalizeTranslationMode(mode)
		if meta, _ := a.store.LoadMeta(id); meta != nil && meta.TranslationMode != nextMode {
			meta.TranslationMode = nextMode
			if err := a.store.SaveMeta(id, *meta); err != nil {
				return err
			}
		}
	}
	idSet := map[string]bool{}
	if len(blockIDs) > 0 {
		for _, id := range blockIDs {
			idSet[id] = true
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
	if err := a.updateStoryStatus(id, StatusTranslating); err != nil {
		return err
	}
	a.queue.Enqueue(Job{StoryID: id, Type: "translate"})
	return nil
}

func (a *App) DeleteStory(id string) error {
	idx, err := a.store.LoadIndex()
	if err != nil {
		return err
	}
	next := idx.Stories[:0]
	for _, entry := range idx.Stories {
		if entry.ID != id {
			next = append(next, entry)
		}
	}
	idx.Stories = next
	if err := a.store.SaveIndex(idx); err != nil {
		return err
	}
	return a.store.RemoveStory(id)
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
