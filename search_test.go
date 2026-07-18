package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSearchFindsMatchAfterOneMegabyteInSingleLine(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "large-line.txt")
	content := strings.Repeat("a", 1024*1024+64) + " target"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	response, _, err := RunSearch(SearchRequest{
		RootPath:        root,
		Query:           "target",
		IncludeContents: true,
		MaxResults:      10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 {
		t.Fatalf("expected one result, got %d", len(response.Results))
	}
	if response.Results[0].MatchedBy[0] != "content" {
		t.Fatalf("expected a content match, got %v", response.Results[0].MatchedBy)
	}
	if !strings.HasPrefix(response.Results[0].Preview, "L1: ") {
		t.Fatalf("expected a line preview, got %q", response.Results[0].Preview)
	}
	if !strings.Contains(response.Results[0].Preview, "target") {
		t.Fatalf("expected the preview to include the match, got %q", response.Results[0].Preview)
	}
	if response.UnreadableItems != 0 {
		t.Fatalf("expected no unreadable items, got %d", response.UnreadableItems)
	}
}

func TestRunSearchContextStopsWhenCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := RunSearchContext(ctx, SearchRequest{RootPath: t.TempDir()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestRunSearchCountsUnreadableOfficeDocument(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "broken.docx"), []byte("not a zip file"), 0600); err != nil {
		t.Fatal(err)
	}

	response, _, err := RunSearch(SearchRequest{
		RootPath:               root,
		Query:                  "target",
		IncludeContents:        true,
		IncludeOfficeDocuments: true,
		MaxResults:             10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.UnreadableItems != 1 {
		t.Fatalf("expected one unreadable item, got %d", response.UnreadableItems)
	}
	if len(response.Results) != 0 {
		t.Fatalf("expected no results, got %d", len(response.Results))
	}
}

func TestAppCancelSearchCancelsActiveContext(t *testing.T) {
	t.Parallel()

	app := NewApp()
	ctx, generation := app.beginSearch()
	if !app.CancelSearch() {
		t.Fatal("expected an active search to be canceled")
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("expected canceled context, got %v", ctx.Err())
	}
	if app.CancelSearch() {
		t.Fatal("expected no active search after cancellation")
	}
	app.finishSearch(generation)
}

func TestFinishingOlderSearchDoesNotCancelNewSearch(t *testing.T) {
	t.Parallel()

	app := NewApp()
	firstCtx, firstGeneration := app.beginSearch()
	secondCtx, secondGeneration := app.beginSearch()

	if !errors.Is(firstCtx.Err(), context.Canceled) {
		t.Fatalf("expected the first search to be canceled, got %v", firstCtx.Err())
	}
	app.finishSearch(firstGeneration)
	if secondCtx.Err() != nil {
		t.Fatalf("expected the second search to remain active, got %v", secondCtx.Err())
	}

	app.finishSearch(secondGeneration)
	if !errors.Is(secondCtx.Err(), context.Canceled) {
		t.Fatalf("expected the second search context to be released, got %v", secondCtx.Err())
	}
}
