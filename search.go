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
	defaultMaxResults = 500
	maxResultLimit    = 5000
)

type SearchRequest struct {
	RootPath               string   `json:"rootPath"`
	Query                  string   `json:"query"`
	Extensions             []string `json:"extensions"`
	IncludeNames           bool     `json:"includeNames"`
	IncludeFileNames       bool     `json:"includeFileNames"`
	IncludeFolderNames     bool     `json:"includeFolderNames"`
	IncludeContents        bool     `json:"includeContents"`
	IncludeOfficeDocuments bool     `json:"includeOfficeDocuments"`
	IncludeDirectories     bool     `json:"includeDirectories"`
	CaseSensitive          bool     `json:"caseSensitive"`
	MaxResults             int      `json:"maxResults"`
}

type SearchResult struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	Path         string   `json:"path"`
	RelativePath string   `json:"relativePath"`
	Extension    string   `json:"extension"`
	Size         int64    `json:"size"`
	ModifiedAt   string   `json:"modifiedAt"`
	MatchedBy    []string `json:"matchedBy"`
	Preview      string   `json:"preview"`
}

type SearchResponse struct {
	HistoryID          string         `json:"historyId"`
	RootPath           string         `json:"rootPath"`
	Query              string         `json:"query"`
	StartedAt          string         `json:"startedAt"`
	DurationMs         int64          `json:"durationMs"`
	TotalVisited       int            `json:"totalVisited"`
	FilesScanned       int            `json:"filesScanned"`
	DirectoriesScanned int            `json:"directoriesScanned"`
	UnreadableItems    int            `json:"unreadableItems"`
	LimitReached       bool           `json:"limitReached"`
	Results            []SearchResult `json:"results"`
}

type HistoryEntry struct {
	ID          string        `json:"id"`
	Label       string        `json:"label"`
	Request     SearchRequest `json:"request"`
	ResultCount int           `json:"resultCount"`
	SearchedAt  string        `json:"searchedAt"`
	DurationMs  int64         `json:"durationMs"`
	Bookmarked  bool          `json:"bookmarked"`
}

func RunSearch(request SearchRequest) (SearchResponse, HistoryEntry, error) {
	return RunSearchContext(context.Background(), request)
}

func RunSearchContext(ctx context.Context, request SearchRequest) (SearchResponse, HistoryEntry, error) {
	started := time.Now()
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

	explicitExtensionSet := makeExtensionSet(request.Extensions)
	targetExtensionSet := makeTargetExtensionSet(request.Extensions, request.IncludeOfficeDocuments)
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

	walkErr := filepath.WalkDir(request.RootPath, func(path string, dirEntry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			response.UnreadableItems++
			if dirEntry != nil && dirEntry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == request.RootPath {
			return nil
		}

		info, err := dirEntry.Info()
		if err != nil {
			response.UnreadableItems++
			return nil
		}

		response.TotalVisited++
		isDirectory := dirEntry.IsDir()
		if isDirectory {
			response.DirectoriesScanned++
		} else {
			response.FilesScanned++
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !isDirectory && len(targetExtensionSet) > 0 && !targetExtensionSet[ext] {
			return nil
		}

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

		if len(response.Results) >= request.MaxResults {
			response.LimitReached = true
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, filepath.SkipAll) {
		return SearchResponse{}, HistoryEntry{}, walkErr
	}

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

func normalizeRequest(request SearchRequest) SearchRequest {
	request.RootPath = filepath.Clean(strings.TrimSpace(request.RootPath))
	request.Query = strings.TrimSpace(request.Query)
	request.Extensions = normalizeExtensions(request.Extensions)

	if request.IncludeNames && !request.IncludeFileNames && !request.IncludeFolderNames {
		request.IncludeFileNames = true
		request.IncludeFolderNames = request.IncludeDirectories
	}
	if !request.IncludeFileNames && !request.IncludeFolderNames && !request.IncludeContents && len(request.Extensions) == 0 {
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

func makeExtensionSet(extensions []string) map[string]bool {
	set := make(map[string]bool, len(extensions))
	for _, extension := range extensions {
		set[extension] = true
	}
	return set
}

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

func matchesQuery(value string, needle string, caseSensitive bool) bool {
	if needle == "" {
		return true
	}
	if !caseSensitive {
		value = strings.ToLower(value)
	}
	return strings.Contains(value, needle)
}

func scanFileContent(ctx context.Context, path string, ext string, needle string, caseSensitive bool, includeOfficeDocuments bool) (bool, string, error) {
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

func compact(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len([]rune(value)) <= limit {
		return value
	}

	runes := []rune(value)
	return string(runes[:limit]) + "..."
}

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

func resultKind(isDirectory bool) string {
	if isDirectory {
		return "folder"
	}
	return "file"
}

func resultSize(info os.FileInfo, isDirectory bool) int64 {
	if isDirectory {
		return 0
	}
	return info.Size()
}

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

func sortResults(results []SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Kind != results[j].Kind {
			return results[i].Kind < results[j].Kind
		}
		return strings.ToLower(results[i].RelativePath) < strings.ToLower(results[j].RelativePath)
	})
}

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
	return strings.Join(parts, " / ")
}

func makeID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
