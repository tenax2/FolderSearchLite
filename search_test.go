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

// TestRunSearchExcludesFileNamesAndExtensions は、除外条件が大小文字を区別せず、
// 名前照合や本文読み取りより先に適用されることを確認する。
func TestRunSearchExcludesFileNamesAndExtensions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	files := map[string]string{
		"keep.txt":   "target",
		"SECRET.txt": "target",
		"debug.LOG":  "target",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	response, history, err := RunSearch(SearchRequest{
		RootPath: root,
		Query:    "target",
		Extensions: []string{
			"txt",
			"log",
		},
		ExcludedFileNames: []string{" secret.TXT ", "SECRET.txt"},
		ExcludedExtensions: []string{
			"log",
			".LOG",
		},
		IncludeContents: true,
		MaxResults:      10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].Name != "keep.txt" {
		t.Fatalf("expected only keep.txt, got %#v", response.Results)
	}
	if response.FilesScanned != 1 {
		t.Fatalf("expected excluded files not to be scanned, got %d scanned files", response.FilesScanned)
	}
	if len(history.Request.ExcludedFileNames) != 1 || history.Request.ExcludedFileNames[0] != "secret.TXT" {
		t.Fatalf("unexpected normalized excluded file names: %v", history.Request.ExcludedFileNames)
	}
	if len(history.Request.ExcludedExtensions) != 1 || history.Request.ExcludedExtensions[0] != ".log" {
		t.Fatalf("unexpected normalized excluded extensions: %v", history.Request.ExcludedExtensions)
	}
	if !strings.Contains(history.Label, "除外名: secret.TXT") || !strings.Contains(history.Label, "除外拡張子: .log") {
		t.Fatalf("expected exclusion conditions in history label, got %q", history.Label)
	}
}

// TestRunSearchFileNameExclusionDoesNotSkipFolder は、同名フォルダを除外せず、
// 配下の検索も継続することを確認する。
func TestRunSearchFileNameExclusionDoesNotSkipFolder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	directory := filepath.Join(root, "secret.txt")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "keep.txt"), []byte("target"), 0600); err != nil {
		t.Fatal(err)
	}

	response, _, err := RunSearch(SearchRequest{
		RootPath:          root,
		Query:             "target",
		ExcludedFileNames: []string{"secret.txt"},
		IncludeContents:   true,
		MaxResults:        10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].Name != "keep.txt" {
		t.Fatalf("expected search to continue below the same-named folder, got %#v", response.Results)
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
