package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// defaultMaxResults は、要求に有効な上限が指定されなかった場合の既定件数である。
	defaultMaxResults = 500
	// maxResultLimit は、過大な結果配列によるメモリ消費を防ぐための絶対上限である。
	maxResultLimit = 5000
)

// SearchRequest は、フロントエンドから受け取る検索条件を表す。
//
// IncludeNamesとIncludeDirectoriesは旧形式の履歴との互換性を保つために残している。
// 新しい要求ではIncludeFileNamesとIncludeFolderNamesを使用する。
type SearchRequest struct {
	// RootPath は、再帰走査を開始する絶対パスまたは相対パスである。
	RootPath string `json:"rootPath"`
	// Query は、名前または本文へ適用する検索文字列である。
	Query string `json:"query"`
	// Extensions は、先頭のピリオドを省略できる拡張子条件である。
	Extensions []string `json:"extensions"`
	// ExcludedFileNames は、検索対象から除外する完全一致のファイル名である。
	// 大文字と小文字は区別しない。
	ExcludedFileNames []string `json:"excludedFileNames"`
	// ExcludedExtensions は、検索対象から除外する拡張子条件である。
	// Extensionsと同様に先頭のピリオドを省略できる。
	ExcludedExtensions []string `json:"excludedExtensions"`
	// IncludeNames は、旧クライアントが名前検索を指定するための互換フィールドである。
	IncludeNames bool `json:"includeNames"`
	// IncludeFileNames は、ファイル名をQueryと照合するかを示す。
	IncludeFileNames bool `json:"includeFileNames"`
	// IncludeFolderNames は、フォルダ名をQueryと照合するかを示す。
	IncludeFolderNames bool `json:"includeFolderNames"`
	// IncludeContents は、ファイル本文をQueryと照合するかを示す。
	IncludeContents bool `json:"includeContents"`
	// IncludeOfficeDocuments は、対応するOffice Open XML文書を本文検索へ含めるかを示す。
	IncludeOfficeDocuments bool `json:"includeOfficeDocuments"`
	// IncludeDirectories は、旧クライアントがフォルダ検索を指定するための互換フィールドである。
	IncludeDirectories bool `json:"includeDirectories"`
	// CaseSensitive は、Queryの大文字と小文字を区別するかを示す。
	CaseSensitive bool `json:"caseSensitive"`
	// MaxResults は、返却する検索結果の最大件数である。
	MaxResults int `json:"maxResults"`
}

// SearchResult は、一つのファイルまたはフォルダに対する検索結果を表す。
type SearchResult struct {
	// ID は、同じ応答内で結果行を識別する一時IDである。
	ID string `json:"id"`
	// Kind は、fileまたはfolderのいずれかである。
	Kind string `json:"kind"`
	// Name は、親ディレクトリを含まない項目名である。
	Name string `json:"name"`
	// Path は、コピー操作に使用する項目のフルパスである。
	Path string `json:"path"`
	// RelativePath は、検索ルートを基準とした表示用パスである。
	RelativePath string `json:"relativePath"`
	// Extension は、小文字へ正規化した拡張子である。フォルダでは空文字になる。
	Extension string `json:"extension"`
	// Size は、ファイルのバイト数である。フォルダでは0になる。
	Size int64 `json:"size"`
	// ModifiedAt は、更新日時をRFC 3339形式で表した文字列である。
	ModifiedAt string `json:"modifiedAt"`
	// MatchedBy は、extension、file-name、folder-name、contentの一致理由を保持する。
	MatchedBy []string `json:"matchedBy"`
	// Preview は、本文一致の周辺を表示する短い抜粋である。
	Preview string `json:"preview"`
}

// SearchResponse は、一回の検索結果と走査統計をフロントエンドへ返す。
type SearchResponse struct {
	// HistoryID は、保存した履歴をブックマークするときに使用するIDである。
	HistoryID string `json:"historyId"`
	// RootPath は、正規化後の検索ルートである。
	RootPath string `json:"rootPath"`
	// Query は、空白を除去した検索文字列である。
	Query string `json:"query"`
	// StartedAt は、検索開始時刻をRFC 3339形式で表した文字列である。
	StartedAt string `json:"startedAt"`
	// DurationMs は、検索開始から結果確定までの経過ミリ秒である。
	DurationMs int64 `json:"durationMs"`
	// TotalVisited は、ルート自身を除いてメタデータを取得できた項目数である。
	TotalVisited int `json:"totalVisited"`
	// FilesScanned は、走査中に確認したファイル数である。
	FilesScanned int `json:"filesScanned"`
	// DirectoriesScanned は、走査中に確認したフォルダ数である。
	DirectoriesScanned int `json:"directoriesScanned"`
	// UnreadableItems は、権限不足、破損、読み取りエラーにより除外した項目数である。
	UnreadableItems int `json:"unreadableItems"`
	// LimitReached は、MaxResultsへ到達して走査を早期終了したかを示す。
	LimitReached bool `json:"limitReached"`
	// Results は、上限、並び順、重複除去を適用した検索結果である。
	Results []SearchResult `json:"results"`
}

// HistoryEntry は、再検索とブックマークに必要な検索条件と結果概要を保持する。
type HistoryEntry struct {
	// ID は、履歴とブックマークを関連付ける永続IDである。
	ID string `json:"id"`
	// Label は、検索語、対象拡張子、除外条件から生成する一覧表示用ラベルである。
	Label string `json:"label"`
	// Request は、再検索時に復元する正規化済み検索条件である。
	Request SearchRequest `json:"request"`
	// ResultCount は、検索時に返した結果件数である。
	ResultCount int `json:"resultCount"`
	// SearchedAt は、検索開始時刻をRFC 3339形式で表した文字列である。
	SearchedAt string `json:"searchedAt"`
	// DurationMs は、検索に要したミリ秒である。
	DurationMs int64 `json:"durationMs"`
	// Bookmarked は、同じIDのブックマークが存在するかを示す。
	Bookmarked bool `json:"bookmarked"`
}

// RunSearch は、キャンセルを必要としない呼び出し向けに検索を実行する。
// 戻り値は検索応答、履歴へ保存する項目、入力または走査に関するエラーである。
func RunSearch(request SearchRequest) (SearchResponse, HistoryEntry, error) {
	return RunSearchContext(context.Background(), request)
}

// RunSearchContext は、ctxによるキャンセルに対応して検索ルートを再帰走査する。
//
// 読み取れない個別項目は検索全体のエラーにせず、UnreadableItemsへ加算して走査を継続する。
// ルートの不正、コンテキストのキャンセル、走査自体を継続できないエラーは呼び出し元へ返す。
// MaxResultsへ到達した場合はfilepath.SkipAllで正常に早期終了する。
func RunSearchContext(ctx context.Context, request SearchRequest) (SearchResponse, HistoryEntry, error) {
	started := time.Now()
	// 走査中の分岐を単純にするため、互換フィールドと入力表記を開始時に確定する。
	request = normalizeRequest(request)
	if err := ctx.Err(); err != nil {
		return SearchResponse{}, HistoryEntry{}, err
	}

	rootInfo, err := os.Stat(request.RootPath)
	if err != nil {
		return SearchResponse{}, HistoryEntry{}, fmt.Errorf("folder path cannot be read: %w", err)
	}
	if !rootInfo.IsDir() {
		return SearchResponse{}, HistoryEntry{}, errors.New("folder path must be a directory")
	}

	// 明示拡張子は一致理由の判定に使い、対象拡張子と除外条件は走査対象の早期判定に使う。
	// Office検索では、明示条件にOffice拡張子を加えた集合が対象拡張子になる。
	explicitExtensionSet := makeExtensionSet(request.Extensions)
	targetExtensionSet := makeTargetExtensionSet(request.Extensions, request.IncludeOfficeDocuments)
	excludedFileNameSet := makeFileNameSet(request.ExcludedFileNames)
	excludedExtensionSet := makeExtensionSet(request.ExcludedExtensions)
	needle := request.Query
	if !request.CaseSensitive {
		needle = strings.ToLower(needle)
	}

	response := SearchResponse{
		RootPath:  request.RootPath,
		Query:     request.Query,
		StartedAt: started.Format(time.RFC3339),
		Results:   make([]SearchResult, 0, minInt(request.MaxResults, 128)),
	}

	// WalkDirのコールバックは逐次実行されるため、responseの集計値を追加のロックなしで更新できる。
	walkErr := filepath.WalkDir(request.RootPath, func(path string, dirEntry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			// 個別項目の失敗は件数へ変換する。読めないディレクトリ配下は走査できないためスキップする。
			response.UnreadableItems++
			if dirEntry != nil && dirEntry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == request.RootPath {
			return nil
		}

		// ファイルの除外条件はメタデータ取得や本文読み取りより前に適用する。
		// フォルダ名が同じ場合は配下の走査を継続する。
		isDirectory := dirEntry.IsDir()
		ext := strings.ToLower(filepath.Ext(path))
		if !isDirectory && (excludedFileNameSet[strings.ToLower(dirEntry.Name())] || excludedExtensionSet[ext]) {
			return nil
		}

		info, err := dirEntry.Info()
		if err != nil {
			response.UnreadableItems++
			return nil
		}

		response.TotalVisited++
		if isDirectory {
			response.DirectoriesScanned++
		} else {
			response.FilesScanned++
		}

		// 対象外拡張子は、名前照合やファイルオープンより前に除外してI/Oを抑える。
		if !isDirectory && len(targetExtensionSet) > 0 && !targetExtensionSet[ext] {
			return nil
		}

		// 一つの項目は複数条件へ一致できるため、理由を蓄積してから採否を決める。
		matchedBy := make([]string, 0, 3)
		preview := ""

		if !isDirectory && len(explicitExtensionSet) > 0 && explicitExtensionSet[ext] {
			matchedBy = append(matchedBy, "extension")
		}

		shouldMatchFileName := !isDirectory && request.IncludeFileNames && (request.Query != "" || len(explicitExtensionSet) == 0)
		if shouldMatchFileName && matchesQuery(info.Name(), needle, request.CaseSensitive) {
			matchedBy = append(matchedBy, "file-name")
		}

		shouldMatchFolderName := isDirectory && request.IncludeFolderNames && (request.Query != "" || len(explicitExtensionSet) == 0)
		if shouldMatchFolderName && matchesQuery(info.Name(), needle, request.CaseSensitive) {
			matchedBy = append(matchedBy, "folder-name")
		}

		if !isDirectory && request.IncludeContents && request.Query != "" {
			matched, contentPreview, scanErr := scanFileContent(ctx, path, ext, needle, request.CaseSensitive, request.IncludeOfficeDocuments)
			if scanErr != nil {
				// キャンセルは検索全体を停止し、ファイル固有エラーは失敗件数へ変換する。
				if errors.Is(scanErr, context.Canceled) {
					return scanErr
				}
				response.UnreadableItems++
			}
			if matched {
				matchedBy = append(matchedBy, "content")
				preview = contentPreview
			}
		}

		if isDirectory && !request.IncludeFolderNames {
			return nil
		}

		if len(matchedBy) == 0 {
			return nil
		}

		relativePath, err := filepath.Rel(request.RootPath, path)
		if err != nil {
			relativePath = info.Name()
		}

		response.Results = append(response.Results, SearchResult{
			ID:           fmt.Sprintf("result-%d", response.TotalVisited),
			Kind:         resultKind(isDirectory),
			Name:         info.Name(),
			Path:         path,
			RelativePath: relativePath,
			Extension:    ext,
			Size:         resultSize(info, isDirectory),
			ModifiedAt:   info.ModTime().Format(time.RFC3339),
			MatchedBy:    uniqueStrings(matchedBy),
			Preview:      preview,
		})

		// 上限到達はエラーではない。SkipAllで残りのディレクトリ走査だけを中止する。
		if len(response.Results) >= request.MaxResults {
			response.LimitReached = true
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, filepath.SkipAll) {
		return SearchResponse{}, HistoryEntry{}, walkErr
	}

	// バックエンドの既定順を確定してから、同じ条件を再現できる履歴項目を生成する。
	response.DurationMs = time.Since(started).Milliseconds()
	sortResults(response.Results)

	history := HistoryEntry{
		ID:          makeID("search"),
		Label:       makeHistoryLabel(request),
		Request:     request,
		ResultCount: len(response.Results),
		SearchedAt:  response.StartedAt,
		DurationMs:  response.DurationMs,
	}

	return response, history, nil
}

// normalizeRequest は、検索条件を走査処理が前提とする形式へ変換する。
// パス、検索語、対象・除外条件、互換フィールド、既定の結果上限を一箇所で確定させる。
func normalizeRequest(request SearchRequest) SearchRequest {
	request.RootPath = filepath.Clean(strings.TrimSpace(request.RootPath))
	request.Query = strings.TrimSpace(request.Query)
	request.Extensions = normalizeExtensions(request.Extensions)
	request.ExcludedFileNames = normalizeFileNames(request.ExcludedFileNames)
	request.ExcludedExtensions = normalizeExtensions(request.ExcludedExtensions)

	if request.IncludeNames && !request.IncludeFileNames && !request.IncludeFolderNames {
		// 個別フラグ導入前に保存された履歴を、現在のファイル名とフォルダ名条件へ移行する。
		request.IncludeFileNames = true
		request.IncludeFolderNames = request.IncludeDirectories
	}
	if !request.IncludeFileNames && !request.IncludeFolderNames && !request.IncludeContents && len(request.Extensions) == 0 {
		// すべての条件が無効な要求でも空結果にせず、既定動作としてファイル名を列挙する。
		request.IncludeFileNames = true
	}
	if request.MaxResults <= 0 {
		request.MaxResults = defaultMaxResults
	}
	if request.MaxResults > maxResultLimit {
		request.MaxResults = maxResultLimit
	}

	return request
}

// normalizeExtensions は、拡張子を小文字かつピリオド付きへ統一する。
// 空要素と重複を除去し、履歴の再現性を保つため辞書順で返す。
func normalizeExtensions(extensions []string) []string {
	seen := map[string]bool{}
	normalized := make([]string, 0, len(extensions))
	for _, extension := range extensions {
		extension = strings.TrimSpace(strings.ToLower(extension))
		if extension == "" {
			continue
		}
		if !strings.HasPrefix(extension, ".") {
			extension = "." + extension
		}
		if !seen[extension] {
			seen[extension] = true
			normalized = append(normalized, extension)
		}
	}
	sort.Strings(normalized)
	return normalized
}

// normalizeFileNames は、除外ファイル名の前後空白と大小文字違いの重複を除く。
// 表示用の表記は最初の入力を保ち、履歴の再現性を保つため大小文字を無視した辞書順で返す。
func normalizeFileNames(fileNames []string) []string {
	seen := map[string]bool{}
	normalized := make([]string, 0, len(fileNames))
	for _, fileName := range fileNames {
		fileName = strings.TrimSpace(fileName)
		key := strings.ToLower(fileName)
		if fileName == "" || seen[key] {
			continue
		}
		seen[key] = true
		normalized = append(normalized, fileName)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return strings.ToLower(normalized[i]) < strings.ToLower(normalized[j])
	})
	return normalized
}

// makeExtensionSet は、拡張子スライスを定数時間で参照できる集合へ変換する。
func makeExtensionSet(extensions []string) map[string]bool {
	set := make(map[string]bool, len(extensions))
	for _, extension := range extensions {
		set[extension] = true
	}
	return set
}

// makeFileNameSet は、正規化済みの除外ファイル名を大小文字を区別しない集合へ変換する。
func makeFileNameSet(fileNames []string) map[string]bool {
	set := make(map[string]bool, len(fileNames))
	for _, fileName := range fileNames {
		set[strings.ToLower(fileName)] = true
	}
	return set
}

// makeTargetExtensionSet は、ファイル走査の対象となる拡張子集合を作る。
//
// extensionsが空の場合は全拡張子を対象にするためnilを返す。
// Office文書検索が有効な場合は、明示条件にOffice文書の拡張子を追加する。
func makeTargetExtensionSet(extensions []string, includeOfficeDocuments bool) map[string]bool {
	if len(extensions) == 0 {
		return nil
	}

	set := makeExtensionSet(extensions)
	if includeOfficeDocuments {
		for _, extension := range officeDocumentExtensions() {
			set[extension] = true
		}
	}
	return set
}

// matchesQuery は、値が正規化済み検索語を含むかを判定する。
// needleが空の場合は、名前だけを列挙する検索を成立させるためtrueを返す。
func matchesQuery(value string, needle string, caseSensitive bool) bool {
	if needle == "" {
		return true
	}
	if !caseSensitive {
		value = strings.ToLower(value)
	}
	return strings.Contains(value, needle)
}

// scanFileContent は、ファイル本文を検索し、一致の有無と表示用プレビューを返す。
//
// 対応するOffice Open XML文書はZIP内XMLへ処理を委譲する。
// 通常ファイルはNUL文字による簡易判定を通過したものだけを行単位で読む。
// bufio.Readerを使うため、bufio.Scannerのトークン上限を超える単一行も検索できる。
// ctxがキャンセルされた場合、読み取り途中でもcontext.Canceledを返す。
func scanFileContent(ctx context.Context, path string, ext string, needle string, caseSensitive bool, includeOfficeDocuments bool) (bool, string, error) {
	// Office Open XMLはバイナリ判定を行わず、ZIPコンテナ内の対象XMLを解析する。
	if includeOfficeDocuments && isModernOfficeExtension(ext) {
		return scanOfficeContent(ctx, path, ext, needle, caseSensitive)
	}

	file, err := os.Open(path)
	if err != nil {
		return false, "", err
	}
	defer file.Close()

	textFile, err := isLikelyText(file)
	if err != nil {
		return false, "", err
	}
	if !textFile {
		return false, "", nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return false, "", err
	}

	// Readerのバッファを超えた行もReadStringが内部で連結するため、行長の上限を設けない。
	reader := bufio.NewReaderSize(file, 64*1024)
	lineNumber := 0
	for {
		if err := ctx.Err(); err != nil {
			return false, "", err
		}

		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			lineNumber++
			candidate := line
			if !caseSensitive {
				candidate = strings.ToLower(candidate)
			}
			if strings.Contains(candidate, needle) {
				return true, fmt.Sprintf("L%d: %s", lineNumber, compactMatch(line, needle, caseSensitive, 220)), nil
			}
		}

		if errors.Is(readErr, io.EOF) {
			return false, "", nil
		}
		if readErr != nil {
			return false, "", readErr
		}
	}
}

// isLikelyText は、先頭8000バイトにNUL文字があるかでテキストらしさを判定する。
// 呼び出し後のファイル位置は先頭から読み取った分だけ進むため、本文走査前にSeekが必要である。
func isLikelyText(file *os.File) (bool, error) {
	buffer := make([]byte, 8000)
	n, err := file.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	if n == 0 {
		return true, nil
	}
	return bytes.IndexByte(buffer[:n], 0) == -1, nil
}

// compact は、連続する空白を一つへ畳み、指定ルーン数を超える文字列を末尾省略する。
// バイト数ではなくルーン数で切るため、日本語をUTF-8の途中で分断しない。
func compact(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len([]rune(value)) <= limit {
		return value
	}

	runes := []rune(value)
	return string(runes[:limit]) + "..."
}

// compactMatch は、一致箇所がプレビュー内に残るよう前後の文脈を切り出す。
//
// 照合位置はバイトインデックスからルーンインデックスへ変換する。
// 切り出しが原文の途中から始まる、または途中で終わる場合は省略記号を付ける。
// 正規化後に一致位置を再取得できない場合はcompactへフォールバックする。
func compactMatch(value string, needle string, caseSensitive bool, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}

	candidate := value
	if !caseSensitive {
		candidate = strings.ToLower(candidate)
	}
	matchByte := strings.Index(candidate, needle)
	if matchByte < 0 {
		return compact(value, limit)
	}

	matchRune := utf8.RuneCountInString(candidate[:matchByte])
	needleRunes := len([]rune(needle))
	contextRunes := limit - minInt(needleRunes, limit)
	start := matchRune - contextRunes/2
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(runes) {
		end = len(runes)
		start = end - limit
	}

	preview := string(runes[start:end])
	if start > 0 {
		preview = "..." + preview
	}
	if end < len(runes) {
		preview += "..."
	}
	return preview
}

// resultKind は、ディレクトリ判定をフロントエンドの種別値へ変換する。
func resultKind(isDirectory bool) string {
	if isDirectory {
		return "folder"
	}
	return "file"
}

// resultSize は、ファイルのバイト数を返す。
// フォルダのサイズはOSごとの意味が一定しないため0へ正規化する。
func resultSize(info os.FileInfo, isDirectory bool) int64 {
	if isDirectory {
		return 0
	}
	return info.Size()
}

// uniqueStrings は、空文字と重複を除去し、最初に現れた順序を保って返す。
// 一つの項目が同じ理由で複数回一致しても、UIの一致ラベルは一つだけになる。
func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	return unique
}

// sortResults は、結果スライスを種別、相対パスの順で安定ソートする。
// 種別が同じ項目では大文字と小文字を区別せずにパスを比較する。
func sortResults(results []SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Kind != results[j].Kind {
			return results[i].Kind < results[j].Kind
		}
		return strings.ToLower(results[i].RelativePath) < strings.ToLower(results[j].RelativePath)
	})
}

// makeHistoryLabel は、履歴一覧で検索条件を識別できる短いラベルを作る。
// 検索語も拡張子もない場合はallを使用し、除外条件はその後ろへ付加する。
func makeHistoryLabel(request SearchRequest) string {
	parts := []string{}
	if request.Query != "" {
		parts = append(parts, request.Query)
	}
	if len(request.Extensions) > 0 {
		parts = append(parts, strings.Join(request.Extensions, ", "))
	}
	if len(parts) == 0 {
		parts = append(parts, "all")
	}
	if len(request.ExcludedFileNames) > 0 {
		parts = append(parts, "除外名: "+strings.Join(request.ExcludedFileNames, ", "))
	}
	if len(request.ExcludedExtensions) > 0 {
		parts = append(parts, "除外拡張子: "+strings.Join(request.ExcludedExtensions, ", "))
	}
	return strings.Join(parts, " / ")
}

// makeID は、用途を示す接頭辞と現在時刻のナノ秒値からIDを作る。
// 永続的な暗号学的識別子ではなく、ローカル履歴の関連付けだけを目的とする。
func makeID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// minInt は、二つの整数の小さい方を返す。
func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
