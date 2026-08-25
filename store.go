package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// maxHistoryEntries は、状態ファイルへ保持する検索履歴の最大件数である。
// ブックマークはこの上限の対象外であり、履歴から消えた項目も独立して保持する。
const maxHistoryEntries = 100

// AppState は、状態ファイルへJSONとして保存する最上位データを表す。
type AppState struct {
	// History は、新しい検索を先頭に並べた最大maxHistoryEntries件の履歴である。
	History []HistoryEntry `json:"history"`
	// Bookmarks は、ユーザーが明示的に保存した検索条件である。
	Bookmarks []HistoryEntry `json:"bookmarks"`
	// FavoriteFolders は、検索ルートとして再利用するフォルダーパスである。
	FavoriteFolders []FavoriteFolder `json:"favoriteFolders"`
}

// FavoriteFolder は、検索画面から選択できる永続化済みフォルダーを表す。
type FavoriteFolder struct {
	// ID は、パスに依存せず解除対象を識別するための永続IDである。
	ID string `json:"id"`
	// Path は、登録時に絶対パスへ正規化したフォルダーパスである。
	Path string `json:"path"`
}

// Store は、履歴、ブックマーク、お気に入りフォルダーをプロセス内で保持し、JSONファイルへ永続化する。
// すべての公開操作はmuを取得するため、Wailsから並行して呼ばれても状態を直列に更新する。
type Store struct {
	// mu は、path以外の可変状態とファイル読み書きを保護する。
	mu sync.Mutex
	// path は、状態JSONファイルの保存先である。
	path string
	// state は、最後に読み込んだ、または更新したアプリケーション状態である。
	state AppState
	// loaded は、状態ファイルの初回読み込みを試行済みかを示す。
	loaded bool
}

// NewStore は、OSのユーザー設定ディレクトリを保存先とするStoreを生成する。
// 設定ディレクトリを取得できない場合は、カレントディレクトリへフォールバックする。
func NewStore() *Store {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}

	return &Store{
		path: filepath.Join(configDir, "FolderSearchLite", "state.json"),
	}
}

// Load は、状態ファイルをメモリへ読み込む。
// 同じStoreでは初回だけディスクへアクセスし、以後はメモリ上の状態を再利用する。
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loadLocked()
}

// AddHistory は、検索履歴を先頭へ追加し、最大件数を超えた古い履歴を切り捨てる。
// 同じIDがブックマーク済みの場合は、履歴側のBookmarkedもtrueへ同期する。
func (s *Store) AddHistory(entry HistoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return err
	}

	if s.bookmarkIndexLocked(entry.ID) >= 0 {
		entry.Bookmarked = true
	}

	s.state.History = append([]HistoryEntry{entry}, s.state.History...)
	if len(s.state.History) > maxHistoryEntries {
		s.state.History = s.state.History[:maxHistoryEntries]
	}
	return s.saveLocked()
}

// GetHistory は、呼び出し元によるスライス変更から内部状態を守るためコピーを返す。
func (s *Store) GetHistory() ([]HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	return copyHistory(s.state.History), nil
}

// ClearHistory は、履歴だけを空にして永続化する。
// ブックマーク配列は変更せず、戻り値はJSONでnullにならない空スライスとする。
func (s *Store) ClearHistory() ([]HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, err
	}

	s.state.History = nil
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return []HistoryEntry{}, nil
}

// BookmarkHistory は、履歴IDに対応する項目をブックマークへ保存する。
// 同じIDがすでに存在する場合は内容を更新し、存在しない場合は先頭へ追加する。
// 履歴IDが見つからない場合は、保存元を特定できないためエラーを返す。
func (s *Store) BookmarkHistory(id string) ([]HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, err
	}

	index := s.historyIndexLocked(id)
	if index < 0 {
		return nil, errors.New("history entry was not found")
	}

	entry := s.state.History[index]
	entry.Bookmarked = true
	s.state.History[index].Bookmarked = true

	bookmarkIndex := s.bookmarkIndexLocked(id)
	if bookmarkIndex >= 0 {
		s.state.Bookmarks[bookmarkIndex] = entry
	} else {
		s.state.Bookmarks = append([]HistoryEntry{entry}, s.state.Bookmarks...)
	}

	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return copyHistory(s.state.Bookmarks), nil
}

// GetBookmarks は、呼び出し元によるスライス変更から内部状態を守るためコピーを返す。
func (s *Store) GetBookmarks() ([]HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	return copyHistory(s.state.Bookmarks), nil
}

// RemoveBookmark は、指定IDをブックマークから除き、対応する履歴のフラグを解除する。
// IDが存在しない場合も冪等な解除として正常終了する。
func (s *Store) RemoveBookmark(id string) ([]HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, err
	}

	index := s.bookmarkIndexLocked(id)
	if index >= 0 {
		s.state.Bookmarks = append(s.state.Bookmarks[:index], s.state.Bookmarks[index+1:]...)
	}

	if historyIndex := s.historyIndexLocked(id); historyIndex >= 0 {
		s.state.History[historyIndex].Bookmarked = false
	}

	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return copyHistory(s.state.Bookmarks), nil
}

// AddFavoriteFolder は、存在するフォルダーをお気に入りの先頭へ登録する。
// すでに同じパスが存在する場合は重複を作らず、対象を先頭へ移動する。
func (s *Store) AddFavoriteFolder(path string) ([]FavoriteFolder, error) {
	normalizedPath, err := normalizeFavoriteFolderPath(path)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, err
	}

	entry := FavoriteFolder{
		ID:   makeID("folder"),
		Path: normalizedPath,
	}
	if index := s.favoriteFolderIndexByPathLocked(normalizedPath); index >= 0 {
		entry = s.state.FavoriteFolders[index]
		s.state.FavoriteFolders = append(s.state.FavoriteFolders[:index], s.state.FavoriteFolders[index+1:]...)
	}
	s.state.FavoriteFolders = append([]FavoriteFolder{entry}, s.state.FavoriteFolders...)

	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return copyFavoriteFolders(s.state.FavoriteFolders), nil
}

// GetFavoriteFolders は、お気に入りフォルダーのスナップショットを返す。
func (s *Store) GetFavoriteFolders() ([]FavoriteFolder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	return copyFavoriteFolders(s.state.FavoriteFolders), nil
}

// RemoveFavoriteFolder は、指定IDのお気に入りフォルダーを解除する。
// IDが存在しない場合も冪等な解除として正常終了する。
func (s *Store) RemoveFavoriteFolder(id string) ([]FavoriteFolder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, err
	}

	if index := s.favoriteFolderIndexByIDLocked(id); index >= 0 {
		s.state.FavoriteFolders = append(s.state.FavoriteFolders[:index], s.state.FavoriteFolders[index+1:]...)
	}

	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return copyFavoriteFolders(s.state.FavoriteFolders), nil
}

// loadLocked は、未読み込みの場合だけ状態ファイルを読み込む。
// 呼び出し元はmuを保持していなければならない。
// ファイルが存在しない、または空の場合は空状態として正常に扱う。
func (s *Store) loadLocked() error {
	if s.loaded {
		return nil
	}

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) == 0 {
		s.loaded = true
		return nil
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return err
	}

	s.loaded = true
	return nil
}

// saveLocked は、現在の状態を一時ファイルへ書き、Renameで保存先を置き換える。
// 呼び出し元はmuを保持していなければならない。
// ディレクトリとファイルの権限は、他ユーザーから履歴を読み取りにくい値へ制限する。
func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}

	// 直接上書きせず、一時ファイルの書き込み完了後に置換して途中状態を残しにくくする。
	tempPath := s.path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tempPath, s.path)
}

// historyIndexLocked は、履歴内で指定IDが最初に現れる位置を返す。
// 呼び出し元はmuを保持していなければならず、見つからない場合は-1を返す。
func (s *Store) historyIndexLocked(id string) int {
	for index, entry := range s.state.History {
		if entry.ID == id {
			return index
		}
	}
	return -1
}

// bookmarkIndexLocked は、ブックマーク内で指定IDが最初に現れる位置を返す。
// 呼び出し元はmuを保持していなければならず、見つからない場合は-1を返す。
func (s *Store) bookmarkIndexLocked(id string) int {
	for index, entry := range s.state.Bookmarks {
		if entry.ID == id {
			return index
		}
	}
	return -1
}

// favoriteFolderIndexByIDLocked は、お気に入りフォルダー内で指定IDが最初に現れる位置を返す。
// 呼び出し元はmuを保持していなければならず、見つからない場合は-1を返す。
func (s *Store) favoriteFolderIndexByIDLocked(id string) int {
	for index, entry := range s.state.FavoriteFolders {
		if entry.ID == id {
			return index
		}
	}
	return -1
}

// favoriteFolderIndexByPathLocked は、OSのパス比較規則で同じフォルダーを探す。
// Windowsではパスの大文字と小文字を区別せず、それ以外のOSでは区別する。
func (s *Store) favoriteFolderIndexByPathLocked(path string) int {
	for index, entry := range s.state.FavoriteFolders {
		if sameFavoriteFolderPath(entry.Path, path) {
			return index
		}
	}
	return -1
}

// copyHistory は、HistoryEntryスライスの浅いコピーを作る。
// 現在のHistoryEntryが参照型フィールドを変更しない前提で、Storeの配列境界を保護する。
func copyHistory(entries []HistoryEntry) []HistoryEntry {
	copied := make([]HistoryEntry, len(entries))
	copy(copied, entries)
	return copied
}

// copyFavoriteFolders は、お気に入りフォルダースライスのコピーを作る。
func copyFavoriteFolders(entries []FavoriteFolder) []FavoriteFolder {
	copied := make([]FavoriteFolder, len(entries))
	copy(copied, entries)
	return copied
}

// normalizeFavoriteFolderPath は、入力パスを検証して絶対パスへ正規化する。
func normalizeFavoriteFolderPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("お気に入りへ登録するフォルダーを指定してください")
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("お気に入りフォルダーのパスを解決できません: %w", err)
	}
	absolutePath = filepath.Clean(absolutePath)

	info, err := os.Stat(absolutePath)
	if err != nil {
		return "", fmt.Errorf("お気に入りフォルダーを確認できません: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("お気に入りへ登録できるのはフォルダーだけです")
	}
	return absolutePath, nil
}

// sameFavoriteFolderPath は、実行OSのファイルシステムで同一表記となるパスかを判定する。
func sameFavoriteFolderPath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
