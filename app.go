package main

import (
	"context"
	"errors"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx              context.Context
	store            *Store
	searchMu         sync.Mutex
	searchCancel     context.CancelFunc
	searchGeneration uint64
}

func NewApp() *App {
	return &App{
		store: NewStore(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	_ = a.store.Load()
}

func (a *App) BrowseFolder() (string, error) {
	if a.ctx == nil {
		return "", errors.New("application context is not ready")
	}

	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "検索対象フォルダを選択",
	})
}

func (a *App) Search(request SearchRequest) (SearchResponse, error) {
	ctx, generation := a.beginSearch()
	defer a.finishSearch(generation)

	response, history, err := RunSearchContext(ctx, request)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return SearchResponse{}, errors.New("検索を中断しました")
		}
		return SearchResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return SearchResponse{}, errors.New("検索を中断しました")
	}

	if err := a.store.AddHistory(history); err != nil {
		return SearchResponse{}, err
	}

	response.HistoryID = history.ID
	return response, nil
}

func (a *App) CancelSearch() bool {
	a.searchMu.Lock()
	defer a.searchMu.Unlock()

	if a.searchCancel == nil {
		return false
	}
	a.searchCancel()
	a.searchCancel = nil
	return true
}

func (a *App) beginSearch() (context.Context, uint64) {
	a.searchMu.Lock()
	defer a.searchMu.Unlock()

	if a.searchCancel != nil {
		a.searchCancel()
	}

	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	a.searchGeneration++
	a.searchCancel = cancel
	return ctx, a.searchGeneration
}

func (a *App) finishSearch(generation uint64) {
	a.searchMu.Lock()
	defer a.searchMu.Unlock()

	if generation != a.searchGeneration {
		return
	}
	if a.searchCancel != nil {
		a.searchCancel()
		a.searchCancel = nil
	}
}

func (a *App) GetHistory() ([]HistoryEntry, error) {
	return a.store.GetHistory()
}

func (a *App) ClearHistory() ([]HistoryEntry, error) {
	return a.store.ClearHistory()
}

func (a *App) BookmarkHistory(id string) ([]HistoryEntry, error) {
	return a.store.BookmarkHistory(id)
}

func (a *App) GetBookmarks() ([]HistoryEntry, error) {
	return a.store.GetBookmarks()
}

func (a *App) RemoveBookmark(id string) ([]HistoryEntry, error) {
	return a.store.RemoveBookmark(id)
}
