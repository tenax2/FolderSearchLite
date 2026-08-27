package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestStoreMutationsRollbackStateWhenSaveFails は、各更新APIが永続化エラーを返したときに、
// 保存されていない変更をStoreのメモリ状態に残さないことを確認する。
func TestStoreMutationsRollbackStateWhenSaveFails(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	existingFavoritePath := filepath.Join(root, "existing-favorite")
	newFavoritePath := filepath.Join(root, "new-favorite")
	for _, path := range []string{existingFavoritePath, newFavoritePath} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}

	// 保存先の親パスを通常ファイルにし、MkdirAllを確実に失敗させる。
	blockingPath := filepath.Join(root, "save-blocker")
	if err := os.WriteFile(blockingPath, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(blockingPath, "state.json")

	initialState := func() AppState {
		return AppState{
			History: []HistoryEntry{
				{
					ID:         "history-bookmarked",
					Bookmarked: true,
					Request: SearchRequest{
						Extensions:         []string{"txt"},
						ExcludedFileNames:  []string{"ignored.txt"},
						ExcludedExtensions: []string{"tmp"},
					},
				},
				{ID: "history-plain"},
			},
			Bookmarks: []HistoryEntry{
				{ID: "history-bookmarked", Bookmarked: true},
			},
			FavoriteFolders: []FavoriteFolder{
				{ID: "favorite-existing", Path: existingFavoritePath},
			},
		}
	}

	tests := []struct {
		name   string
		update func(*Store) error
	}{
		{
			name: "add history",
			update: func(store *Store) error {
				return store.AddHistory(HistoryEntry{ID: "history-new"})
			},
		},
		{
			name: "clear history",
			update: func(store *Store) error {
				_, err := store.ClearHistory()
				return err
			},
		},
		{
			name: "bookmark history",
			update: func(store *Store) error {
				_, err := store.BookmarkHistory("history-plain")
				return err
			},
		},
		{
			name: "remove bookmark",
			update: func(store *Store) error {
				_, err := store.RemoveBookmark("history-bookmarked")
				return err
			},
		},
		{
			name: "add favorite folder",
			update: func(store *Store) error {
				_, err := store.AddFavoriteFolder(newFavoritePath)
				return err
			},
		},
		{
			name: "remove favorite folder",
			update: func(store *Store) error {
				_, err := store.RemoveFavoriteFolder("favorite-existing")
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &Store{
				path:   statePath,
				state:  initialState(),
				loaded: true,
			}
			want := initialState()

			if err := test.update(store); err == nil {
				t.Fatal("expected the state save to fail")
			}
			if !reflect.DeepEqual(store.state, want) {
				t.Fatalf("state changed after a failed save\nwant: %#v\n got: %#v", want, store.state)
			}
		})
	}
}

// TestFavoriteFolderLifecyclePersistsAndDeduplicates は、お気に入りフォルダーの
// 登録順、重複排除、再読み込み、解除が一つの状態ファイルで維持されることを確認する。
func TestFavoriteFolderLifecyclePersistsAndDeduplicates(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	firstPath := filepath.Join(root, "first")
	secondPath := filepath.Join(root, "second")
	for _, path := range []string{firstPath, secondPath} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}

	statePath := filepath.Join(root, "state", "state.json")
	store := &Store{path: statePath}

	favorites, err := store.AddFavoriteFolder(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(favorites) != 1 {
		t.Fatalf("expected one favorite folder, got %d", len(favorites))
	}
	firstID := favorites[0].ID
	if firstID == "" || favorites[0].Path != firstPath {
		t.Fatalf("unexpected first favorite: %+v", favorites[0])
	}

	favorites, err = store.AddFavoriteFolder(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(favorites) != 2 || favorites[0].Path != secondPath {
		t.Fatalf("expected the newest folder first, got %+v", favorites)
	}

	favorites, err = store.AddFavoriteFolder(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(favorites) != 2 {
		t.Fatalf("expected duplicate registration to keep two entries, got %d", len(favorites))
	}
	if favorites[0].ID != firstID || favorites[0].Path != firstPath {
		t.Fatalf("expected the existing favorite to move first, got %+v", favorites[0])
	}

	reloaded := &Store{path: statePath}
	favorites, err = reloaded.GetFavoriteFolders()
	if err != nil {
		t.Fatal(err)
	}
	if len(favorites) != 2 || favorites[0].ID != firstID {
		t.Fatalf("expected favorites to survive reload, got %+v", favorites)
	}

	favorites, err = reloaded.RemoveFavoriteFolder(firstID)
	if err != nil {
		t.Fatal(err)
	}
	if len(favorites) != 1 || favorites[0].Path != secondPath {
		t.Fatalf("expected only the second favorite after removal, got %+v", favorites)
	}
}

// TestAddFavoriteFolderRejectsInvalidPaths は、空値、存在しないパス、通常ファイルを
// お気に入りフォルダーとして永続化しないことを確認する。
func TestAddFavoriteFolderRejectsInvalidPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	filePath := filepath.Join(root, "file.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
	}{
		{name: "empty", path: "  "},
		{name: "missing", path: filepath.Join(root, "missing")},
		{name: "file", path: filePath},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			store := &Store{path: filepath.Join(root, test.name, "state.json")}
			if _, err := store.AddFavoriteFolder(test.path); err == nil {
				t.Fatalf("expected %q to be rejected", test.path)
			}
		})
	}
}

// TestGetFavoriteFoldersLoadsLegacyState は、favoriteFoldersを持たない旧状態JSONを
// 空のお気に入り一覧として後方互換に読み込めることを確認する。
func TestGetFavoriteFoldersLoadsLegacyState(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"history":[],"bookmarks":[]}`), 0600); err != nil {
		t.Fatal(err)
	}

	store := &Store{path: statePath}
	favorites, err := store.GetFavoriteFolders()
	if err != nil {
		t.Fatal(err)
	}
	if favorites == nil || len(favorites) != 0 {
		t.Fatalf("expected a non-nil empty favorite list, got %#v", favorites)
	}
}
