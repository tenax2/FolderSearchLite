package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// filePreviewExcerptLimit は、一回のプレビューで返す一致箇所の上限である。
	filePreviewExcerptLimit = 12
	// filePreviewRuneLimit は、一つの一致箇所に含める最大ルーン数である。
	filePreviewRuneLimit = 600
)

// FilePreviewRequest は、検索結果のファイルから一致箇所を再取得する条件を表す。
type FilePreviewRequest struct {
	// Path は、プレビュー対象ファイルのフルパスである。
	Path string `json:"path"`
	// Query は、プレビュー内で探す検索語である。
	Query string `json:"query"`
	// CaseSensitive は、Queryの大文字と小文字を区別するかを示す。
	CaseSensitive bool `json:"caseSensitive"`
	// IncludeOfficeDocuments は、Office Open XML文書の抽出テキストを対象にするかを示す。
	IncludeOfficeDocuments bool `json:"includeOfficeDocuments"`
}

// FilePreviewExcerpt は、ファイル内の一つの一致箇所と表示位置を表す。
type FilePreviewExcerpt struct {
	// Location は、L10やWord / documentなどの表示用位置である。
	Location string `json:"location"`
	// Text は、一致語の周辺を切り出したプレーンテキストである。
	Text string `json:"text"`
}

// FilePreview は、プレビュー対象の基本情報と一致箇所の一覧を表す。
type FilePreview struct {
	// Name は、親ディレクトリを含まないファイル名である。
	Name string `json:"name"`
	// Path は、読み取り時点で正規化したフルパスである。
	Path string `json:"path"`
	// Excerpts は、ファイル内で見つかった一致箇所である。
	Excerpts []FilePreviewExcerpt `json:"excerpts"`
	// Truncated は、上限を超える一致箇所があり、後続を省略したことを示す。
	Truncated bool `json:"truncated"`
}

// LoadFilePreview は、指定ファイルを再読み込みし、Queryに一致する箇所を抽出する。
//
// プレーンテキストは一行ごとに一致を収集する。Office Open XML文書は、
// ZIP内の検索対象パーツごとに最初の一致を収集する。
// 応答サイズを制御するため、上限を超えるとその時点で読み取りを終了する。
func LoadFilePreview(ctx context.Context, request FilePreviewRequest) (FilePreview, error) {
	request.Path = strings.TrimSpace(request.Path)
	request.Query = strings.TrimSpace(request.Query)
	if request.Path == "" {
		return FilePreview{}, errors.New("プレビュー対象のファイルが指定されていません")
	}
	if request.Query == "" {
		return FilePreview{}, errors.New("プレビューする検索語が指定されていません")
	}
	if err := ctx.Err(); err != nil {
		return FilePreview{}, err
	}

	cleanPath := filepath.Clean(request.Path)
	info, err := os.Stat(cleanPath)
	if err != nil {
		return FilePreview{}, fmt.Errorf("プレビュー対象を読み取れません: %w", err)
	}
	if !info.Mode().IsRegular() {
		return FilePreview{}, errors.New("プレビュー対象は通常ファイルである必要があります")
	}

	preview := FilePreview{
		Name:     info.Name(),
		Path:     cleanPath,
		Excerpts: make([]FilePreviewExcerpt, 0, filePreviewExcerptLimit),
	}

	needle := request.Query
	if !request.CaseSensitive {
		needle = strings.ToLower(needle)
	}
	ext := strings.ToLower(filepath.Ext(cleanPath))
	if request.IncludeOfficeDocuments && isModernOfficeExtension(ext) {
		preview.Excerpts, preview.Truncated, err = previewOfficeContent(
			ctx,
			cleanPath,
			ext,
			needle,
			request.CaseSensitive,
			filePreviewExcerptLimit,
			filePreviewRuneLimit,
		)
		return preview, err
	}

	file, err := os.Open(cleanPath)
	if err != nil {
		return FilePreview{}, fmt.Errorf("プレビュー対象を開けません: %w", err)
	}
	defer file.Close()

	textFile, err := isLikelyText(file)
	if err != nil {
		return FilePreview{}, fmt.Errorf("ファイル形式を判定できません: %w", err)
	}
	if !textFile {
		return FilePreview{}, errors.New("バイナリファイルはプレビューできません")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return FilePreview{}, fmt.Errorf("プレビュー対象を読み直せません: %w", err)
	}

	reader := bufio.NewReaderSize(file, 64*1024)
	lineNumber := 0
	for {
		if err := ctx.Err(); err != nil {
			return FilePreview{}, err
		}

		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			lineNumber++
			candidate := line
			if !request.CaseSensitive {
				candidate = strings.ToLower(candidate)
			}
			if strings.Contains(candidate, needle) {
				if len(preview.Excerpts) >= filePreviewExcerptLimit {
					preview.Truncated = true
					return preview, nil
				}
				preview.Excerpts = append(preview.Excerpts, FilePreviewExcerpt{
					Location: fmt.Sprintf("L%d", lineNumber),
					Text:     compactMatch(line, needle, request.CaseSensitive, filePreviewRuneLimit),
				})
			}
		}

		if errors.Is(readErr, io.EOF) {
			return preview, nil
		}
		if readErr != nil {
			return FilePreview{}, fmt.Errorf("プレビュー対象を読み取れません: %w", readErr)
		}
	}
}
