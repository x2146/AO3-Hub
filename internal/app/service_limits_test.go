package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestValidateParsedImportBoundsStructureAndContent(t *testing.T) {
	makeResult := func(chapters []Chapter) parseResult {
		return parseResult{
			Meta:     parsedMeta{Meta: Meta{Title: "Work", Author: "Author"}},
			Original: ChapterFile{Chapters: chapters},
		}
	}
	tests := []struct {
		name   string
		result parseResult
	}{
		{
			name:   "chapters",
			result: makeResult(make([]Chapter, maxImportChapters+1)),
		},
		{
			name: "blocks per chapter",
			result: makeResult([]Chapter{{
				Blocks: make([]Block, maxImportBlocksPerChapter+1),
			}}),
		},
		{
			name: "total blocks",
			result: makeResult([]Chapter{
				{Blocks: make([]Block, maxImportBlocksPerChapter)},
				{Blocks: make([]Block, maxImportBlocksPerChapter)},
				{Blocks: make([]Block, maxImportBlocks-maxImportBlocksPerChapter*2+1)},
			}),
		},
		{
			name: "single block bytes",
			result: makeResult([]Chapter{{Blocks: []Block{{
				HTML: strings.Repeat("x", maxImportBlockBytes+1),
			}}}}),
		},
		{
			name: "tags",
			result: parseResult{
				Meta: parsedMeta{Meta: Meta{Tags: Tags{
					Additional: make([]string, maxImportTags+1),
				}}},
				Original: ChapterFile{Chapters: []Chapter{{}}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateParsedImport(test.result); !errors.Is(err, errImportTooLarge) {
				t.Fatalf("validation error = %v, want %v", err, errImportTooLarge)
			}
		})
	}

	largeBlock := strings.Repeat("x", maxImportBlockBytes)
	blocks := make([]Block, maxImportContentBytes/maxImportBlockBytes+1)
	for i := range blocks {
		blocks[i].HTML = largeBlock
	}
	if err := validateParsedImport(makeResult([]Chapter{{Blocks: blocks}})); !errors.Is(err, errImportTooLarge) {
		t.Fatalf("total content validation error = %v, want %v", err, errImportTooLarge)
	}
}

func TestRetryStoryRejectsUnknownSelectionWithoutMutation(t *testing.T) {
	app := newLifecycleTestApp(t)
	const storyID = "story"
	seedRetryableStory(t, app, storyID)
	if err := app.RetryStory(storyID, []string{"missing"}, nil, TranslationModeRefined); !errors.Is(err, errInvalidRetrySelection) {
		t.Fatalf("retry error = %v, want %v", err, errInvalidRetrySelection)
	}
	meta, err := app.store.LoadMeta(storyID)
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil || meta.TranslationMode != TranslationModeNormal {
		t.Fatalf("invalid retry mutated mode: %+v", meta)
	}
	translated, err := app.store.LoadTranslated(storyID)
	if err != nil {
		t.Fatal(err)
	}
	if translated == nil || translated.Chapters[0].Blocks[0].Status != BlockError {
		t.Fatalf("invalid retry mutated translation: %+v", translated)
	}
}

func TestRetryStoryUsesOriginalBlockIdentity(t *testing.T) {
	app := newLifecycleTestApp(t)
	const storyID = "story"
	seedRetryableStory(t, app, storyID)
	translated, err := app.store.LoadTranslated(storyID)
	if err != nil {
		t.Fatal(err)
	}
	translated.Chapters[0].Blocks[0].ID = "stale-copy-id"
	if err := app.store.SaveTranslated(storyID, *translated); err != nil {
		t.Fatal(err)
	}
	app.queue.run = func(ctx context.Context, _ Job) error {
		<-ctx.Done()
		return ctx.Err()
	}
	if err := app.RetryStory(storyID, []string{"block"}, nil, ""); err != nil {
		t.Fatal(err)
	}
	app.queue.CancelAndWait(storyID)
	translated, err = app.store.LoadTranslated(storyID)
	if err != nil {
		t.Fatal(err)
	}
	block := translated.Chapters[0].Blocks[0]
	if block.ID != "block" || block.Type != BlockP || block.Status != BlockPending || block.HTML != "" || block.Error != "" {
		t.Fatalf("retried block was not repaired from original: %+v", block)
	}
}

func TestCreateFromHTMLRejectsInvalidModeBeforeParsing(t *testing.T) {
	app := newLifecycleTestApp(t)
	if _, err := app.CreateFromHTML("not html", TranslationMode("unknown")); !errors.Is(err, errInvalidTranslationMode) {
		t.Fatalf("create error = %v, want %v", err, errInvalidTranslationMode)
	}
}
