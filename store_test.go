package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
