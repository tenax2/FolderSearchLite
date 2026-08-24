package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadFilePreviewReturnsMatchingLines は、大小文字を区別しないプレビューが
// 複数の一致行と正しい行番号を返すことを確認する。
func TestLoadFilePreviewReturnsMatchingLines(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sample.txt")
	content := "before\nAlpha TARGET omega\nno match\ntarget again\nafter\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	preview, err := LoadFilePreview(context.Background(), FilePreviewRequest{
		Path:  path,
		Query: "target",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Excerpts) != 2 {
		t.Fatalf("expected two excerpts, got %d", len(preview.Excerpts))
	}
	if preview.Excerpts[0].Location != "L2" || preview.Excerpts[1].Location != "L4" {
		t.Fatalf("expected L2 and L4, got %+v", preview.Excerpts)
	}
	if !strings.Contains(preview.Excerpts[0].Text, "TARGET") {
		t.Fatalf("expected original letter case in preview, got %q", preview.Excerpts[0].Text)
	}
	if preview.Truncated {
		t.Fatal("expected complete preview")
	}
}

// TestLoadFilePreviewHonorsCaseSensitivity は、大小文字を区別する場合に
// 表記が異なる行をプレビューへ含めないことを確認する。
func TestLoadFilePreviewHonorsCaseSensitivity(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "case.txt")
	if err := os.WriteFile(path, []byte("TARGET\ntarget\n"), 0600); err != nil {
		t.Fatal(err)
	}

	preview, err := LoadFilePreview(context.Background(), FilePreviewRequest{
		Path:          path,
		Query:         "target",
		CaseSensitive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Excerpts) != 1 || preview.Excerpts[0].Location != "L2" {
		t.Fatalf("expected only the lowercase match on L2, got %+v", preview.Excerpts)
	}
}

// TestLoadFilePreviewLimitsExcerptCount は、一致が多いファイルで応答件数を制限し、
// 後続があることをTruncatedで通知することを確認する。
func TestLoadFilePreviewLimitsExcerptCount(t *testing.T) {
	t.Parallel()

	var content strings.Builder
	for index := 0; index < filePreviewExcerptLimit+1; index++ {
		fmt.Fprintf(&content, "target %d\n", index+1)
	}
	path := filepath.Join(t.TempDir(), "many.txt")
	if err := os.WriteFile(path, []byte(content.String()), 0600); err != nil {
		t.Fatal(err)
	}

	preview, err := LoadFilePreview(context.Background(), FilePreviewRequest{
		Path:  path,
		Query: "target",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Excerpts) != filePreviewExcerptLimit {
		t.Fatalf("expected %d excerpts, got %d", filePreviewExcerptLimit, len(preview.Excerpts))
	}
	if !preview.Truncated {
		t.Fatal("expected truncated preview")
	}
}

// TestLoadFilePreviewReadsOfficeDocument は、Office Open XML文書の対象パーツから
// 一致周辺とパーツ名を取得できることを確認する。
func TestLoadFilePreviewReadsOfficeDocument(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sample.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	part, err := archive.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, `<w:document xmlns:w="urn:test"><w:body><w:p><w:r><w:t>alpha target omega</w:t></w:r></w:p></w:body></w:document>`); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	preview, err := LoadFilePreview(context.Background(), FilePreviewRequest{
		Path:                   path,
		Query:                  "target",
		IncludeOfficeDocuments: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Excerpts) != 1 {
		t.Fatalf("expected one Office excerpt, got %d", len(preview.Excerpts))
	}
	if preview.Excerpts[0].Location != "Word / document" {
		t.Fatalf("expected Word part label, got %q", preview.Excerpts[0].Location)
	}
	if !strings.Contains(preview.Excerpts[0].Text, "target") {
		t.Fatalf("expected the Office preview to include the match, got %q", preview.Excerpts[0].Text)
	}
}
