package main

import (
	"context"
	"errors"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx   context.Context
	store *Store
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
	response, history, err := RunSearch(request)
	if err != nil {
		return SearchResponse{}, err
	}

	if err := a.store.AddHistory(history); err != nil {
		return SearchResponse{}, err
	}

	response.HistoryID = history.ID
	return response, nil
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
