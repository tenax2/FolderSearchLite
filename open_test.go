package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenResultUsesDefaultApplicationForFile は、通常ファイルの絶対パスと
// ファイル種別がOSオープナーへ渡されることを確認する。
func TestOpenResultUsesDefaultApplicationForFile(t *testing.T) {
	t.Parallel()

	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	var openedPath string
	var openedAsDirectory bool
	app.openPath = func(path string, isDirectory bool) error {
		openedPath = path
		openedAsDirectory = isDirectory
		return nil
	}

	if err := app.OpenResult(filePath); err != nil {
		t.Fatal(err)
	}
	if openedPath != filePath {
		t.Fatalf("expected %q to be opened, got %q", filePath, openedPath)
	}
	if openedAsDirectory {
		t.Fatal("expected the result to be opened as a file")
	}
}

// TestOpenResultUsesExplorerForFolder は、フォルダの絶対パスとフォルダ種別が
// OSオープナーへ渡されることを確認する。
func TestOpenResultUsesExplorerForFolder(t *testing.T) {
	t.Parallel()

	folderPath := t.TempDir()
	app := NewApp()
	var openedPath string
	var openedAsDirectory bool
	app.openPath = func(path string, isDirectory bool) error {
		openedPath = path
		openedAsDirectory = isDirectory
		return nil
	}

	if err := app.OpenResult(folderPath); err != nil {
		t.Fatal(err)
	}
	if openedPath != folderPath {
		t.Fatalf("expected %q to be opened, got %q", folderPath, openedPath)
	}
	if !openedAsDirectory {
		t.Fatal("expected the result to be opened as a directory")
	}
}

// TestOpenResultRejectsMissingPath は、検索後に削除された項目をOSへ渡さないことを確認する。
func TestOpenResultRejectsMissingPath(t *testing.T) {
	t.Parallel()

	app := NewApp()
	opened := false
	app.openPath = func(string, bool) error {
		opened = true
		return nil
	}

	err := app.OpenResult(filepath.Join(t.TempDir(), "missing.txt"))
	if err == nil {
		t.Fatal("expected a missing-path error")
	}
	if opened {
		t.Fatal("expected the OS opener not to be called")
	}
}

// TestOpenResultReturnsOpenerError は、OS側の起動失敗を利用者へ返すことを確認する。
func TestOpenResultReturnsOpenerError(t *testing.T) {
	t.Parallel()

	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, nil, 0600); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.openPath = func(string, bool) error {
		return errors.New("no file association")
	}

	err := app.OpenResult(filePath)
	if err == nil || !strings.Contains(err.Error(), "no file association") {
		t.Fatalf("expected the opener error, got %v", err)
	}
}
