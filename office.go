package main

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strings"
)

// officePreviewWindow は、XMLトークンをまたぐ一致を検出するために保持する最大ルーン数である。
// 上限を超えた場合も後半を残すため、次の文字データとの境界にある検索語を検出できる。
const officePreviewWindow = 3000

// officeDocumentExtensions は、UIでOffice文書として扱う拡張子を返す。
// 古いバイナリ形式も一覧には含むが、本文解析できるのはisModernOfficeExtensionがtrueを返す形式だけである。
func officeDocumentExtensions() []string {
	return []string{
		".doc", ".dot", ".docx", ".docm", ".dotx", ".dotm",
		".xls", ".xlt", ".xlsx", ".xlsm", ".xltx", ".xltm",
		".ppt", ".pps", ".pot", ".pptx", ".pptm", ".ppsx", ".ppsm", ".potx", ".potm",
	}
}

// isModernOfficeExtension は、ZIPとXMLで構成されるOffice Open XML形式かを判定する。
// 拡張子は呼び出し元の正規化状態に依存しないよう、関数内でも小文字へ変換する。
func isModernOfficeExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".docx", ".docm", ".dotx", ".dotm",
		".xlsx", ".xlsm", ".xltx", ".xltm",
		".pptx", ".pptm", ".ppsx", ".ppsm", ".potx", ".potm":
		return true
	default:
		return false
	}
}

// scanOfficeContent は、Office Open XMLコンテナ内の検索対象パーツを順に検索する。
//
// 戻り値は一致の有無、文書種別を付けたプレビュー、解析エラーである。
// 一つのパーツを開けなくても残りを検索し、最初のエラーだけを保持する。
// 一致を発見した場合は、それ以前の非致命的エラーより一致結果を優先してnilエラーを返す。
// ctxがキャンセルされた場合は、後続パーツを開かず直ちに終了する。
func scanOfficeContent(ctx context.Context, filePath string, ext string, needle string, caseSensitive bool) (bool, string, error) {
	reader, err := zip.OpenReader(filePath)
	if err != nil {
		return false, "", err
	}
	defer reader.Close()

	// 一部パーツの破損を理由に、正常な別パーツの一致を取りこぼさない。
	var firstErr error
	for _, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return false, "", err
		}

		name := path.Clean(strings.ReplaceAll(file.Name, "\\", "/"))
		if !shouldScanOfficePart(ext, name) {
			continue
		}

		part, err := file.Open()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		matched, preview, scanErr := scanXMLText(ctx, part, needle, caseSensitive)
		_ = part.Close()
		if matched {
			return true, fmt.Sprintf("%s: %s", officePartLabel(name), preview), nil
		}
		if scanErr != nil && firstErr == nil {
			firstErr = scanErr
		}
	}

	return false, "", firstErr
}

// shouldScanOfficePart は、拡張子とZIP内パスから本文検索に必要なXMLかを判定する。
//
// Wordでは本文、ヘッダー、フッター、コメント、脚注を対象にする。
// Excelでは共有文字列、ワークシート、コメント、描画を対象にする。
// PowerPointではスライド、ノート、コメントを対象にする。
// 書式、テーマ、リレーション定義は表示テキストではないため除外する。
func shouldScanOfficePart(ext string, name string) bool {
	if !strings.HasSuffix(name, ".xml") {
		return false
	}

	switch strings.ToLower(ext) {
	case ".docx", ".docm", ".dotx", ".dotm":
		return name == "word/document.xml" ||
			strings.HasPrefix(name, "word/header") ||
			strings.HasPrefix(name, "word/footer") ||
			strings.HasPrefix(name, "word/comments") ||
			name == "word/footnotes.xml" ||
			name == "word/endnotes.xml"
	case ".xlsx", ".xlsm", ".xltx", ".xltm":
		return name == "xl/sharedStrings.xml" ||
			strings.HasPrefix(name, "xl/worksheets/") ||
			strings.HasPrefix(name, "xl/comments") ||
			strings.HasPrefix(name, "xl/threadedComments") ||
			strings.HasPrefix(name, "xl/drawings/")
	case ".pptx", ".pptm", ".ppsx", ".ppsm", ".potx", ".potm":
		return strings.HasPrefix(name, "ppt/slides/") ||
			strings.HasPrefix(name, "ppt/notesSlides/") ||
			strings.HasPrefix(name, "ppt/comments/")
	default:
		return false
	}
}

// scanXMLText は、XMLの文字データを連結し、検索語を含む区間を探す。
//
// 要素境界で分割された語句も検出できるよう、xml.CharDataを空白区切りでバッファへ蓄積する。
// バッファはofficePreviewWindowを超えると後半だけを残し、文書全体の保持を避ける。
// XMLが不正な場合とctxがキャンセルされた場合は、原因となったエラーを返す。
func scanXMLText(ctx context.Context, reader io.Reader, needle string, caseSensitive bool) (bool, string, error) {
	return scanXMLTextWithLimit(ctx, reader, needle, caseSensitive, 220)
}

// scanXMLTextWithLimit は、抽出する一致周辺の最大ルーン数を指定できるXML本文検索である。
// 通常の検索結果は短い抜粋、詳細プレビューは長い抜粋を同じ解析処理から生成する。
func scanXMLTextWithLimit(ctx context.Context, reader io.Reader, needle string, caseSensitive bool, previewRuneLimit int) (bool, string, error) {
	decoder := xml.NewDecoder(reader)
	var buffer strings.Builder

	for {
		if err := ctx.Err(); err != nil {
			return false, "", err
		}

		token, err := decoder.Token()
		if err == io.EOF {
			return false, "", nil
		}
		if err != nil {
			return false, "", err
		}

		charData, ok := token.(xml.CharData)
		if !ok {
			continue
		}

		text := strings.Join(strings.Fields(string(charData)), " ")
		if text == "" {
			continue
		}
		if buffer.Len() > 0 {
			buffer.WriteByte(' ')
		}
		buffer.WriteString(text)

		// XML要素境界をまたぐ検索語へ対応するため、現在までの文字データ全体を照合する。
		segment := buffer.String()
		candidate := segment
		if !caseSensitive {
			candidate = strings.ToLower(candidate)
		}
		if strings.Contains(candidate, needle) {
			return true, compactMatch(segment, needle, caseSensitive, previewRuneLimit), nil
		}
		if len([]rune(segment)) > officePreviewWindow {
			// 無制限なメモリ増加を避けつつ、次のトークンとの境界に必要な末尾を保持する。
			buffer.Reset()
			buffer.WriteString(tailRunes(segment, officePreviewWindow/2))
		}
	}
}

// previewOfficeContent は、Office Open XML文書の各検索対象パーツから最初の一致を収集する。
// 一致数がexcerptLimitを超えた場合は、上限分と省略フラグを返す。
func previewOfficeContent(
	ctx context.Context,
	filePath string,
	ext string,
	needle string,
	caseSensitive bool,
	excerptLimit int,
	previewRuneLimit int,
) ([]FilePreviewExcerpt, bool, error) {
	reader, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, false, err
	}
	defer reader.Close()

	excerpts := make([]FilePreviewExcerpt, 0, excerptLimit)
	var firstErr error
	for _, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}

		name := path.Clean(strings.ReplaceAll(file.Name, "\\", "/"))
		if !shouldScanOfficePart(ext, name) {
			continue
		}

		part, err := file.Open()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		matched, preview, scanErr := scanXMLTextWithLimit(ctx, part, needle, caseSensitive, previewRuneLimit)
		_ = part.Close()
		if matched {
			if len(excerpts) >= excerptLimit {
				return excerpts, true, nil
			}
			excerpts = append(excerpts, FilePreviewExcerpt{
				Location: fmt.Sprintf("%s / %s", officePartLabel(name), strings.TrimSuffix(path.Base(name), path.Ext(name))),
				Text:     preview,
			})
		}
		if scanErr != nil && firstErr == nil {
			firstErr = scanErr
		}
	}

	if len(excerpts) > 0 {
		// 一部パーツが壊れていても、表示できる一致があればそれを優先する。
		return excerpts, false, nil
	}
	return excerpts, false, firstErr
}

// tailRunes は、文字列の末尾から最大limitルーンを返す。
// UTF-8のマルチバイト文字を分断しないため、バイト単位のスライスは使用しない。
func tailRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[len(runes)-limit:])
}

// officePartLabel は、ZIP内パスをユーザー向けのOffice製品名へ変換する。
// 未知のパスはOfficeと表示し、プレビュー自体は失わない。
func officePartLabel(name string) string {
	switch {
	case strings.HasPrefix(name, "word/"):
		return "Word"
	case strings.HasPrefix(name, "xl/"):
		return "Excel"
	case strings.HasPrefix(name, "ppt/"):
		return "PowerPoint"
	default:
		return "Office"
	}
}
