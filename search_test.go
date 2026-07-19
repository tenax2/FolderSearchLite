package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunSearchFindsMatchAfterOneMegabyteInSingleLine は、Scannerの既定上限を超える
// 単一行でも末尾の検索語を検出し、一致箇所をプレビューへ残せることを確認する。
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

// TestRunSearchContextStopsWhenCanceled は、走査開始前にキャンセルされた要求が
// context.Canceledを失わず呼び出し元へ返すことを確認する。
func TestRunSearchContextStopsWhenCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := RunSearchContext(ctx, SearchRequest{RootPath: t.TempDir()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

// TestRunSearchCountsUnreadableOfficeDocument は、ZIPとして壊れたOffice文書を
// 検索全体の失敗にせず、読み取り失敗件数として報告することを確認する。
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

// TestAppCancelSearchCancelsActiveContext は、CancelSearchが実行中コンテキストを停止し、
// 同じ検索へ二度目のキャンセルを行わないことを確認する。
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

// TestFinishingOlderSearchDoesNotCancelNewSearch は、世代番号が古い終了処理によって
// 後から開始した検索のコンテキストが解除されないことを確認する。
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
