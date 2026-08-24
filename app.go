package main

import (
	"context"
	"errors"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App は、Wailsがフロントエンドへ公開するアプリケーションAPIを実装する。
// 検索処理と永続化処理を仲介し、同時に存在できる検索を一つに制限する。
type App struct {
	// ctx は、Wailsの起動時に受け取るアプリケーションライフサイクルのコンテキストである。
	ctx context.Context
	// store は、検索履歴とブックマークの永続化を担当する。
	store *Store
	// searchMu は、searchCancelとsearchGenerationを直列化する。
	searchMu sync.Mutex
	// searchCancel は、現在実行中の検索を停止する関数である。検索がなければnilになる。
	searchCancel context.CancelFunc
	// searchGeneration は、古い検索の終了処理が新しい検索を解除しないための世代番号である。
	searchGeneration uint64
}

// NewApp は、空のライフサイクル状態と永続ストアを持つAppを生成する。
// Wailsのコンテキストはstartupが後から設定する。
func NewApp() *App {
	return &App{
		store: NewStore(),
	}
}

// startup は、Wails起動時のコンテキストを保存し、永続状態を先読みする。
// Loadの失敗は後続の各ストア操作でも返されるため、起動自体は継続する。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	_ = a.store.Load()
}

// BrowseFolder は、OS標準のフォルダ選択ダイアログを開く。
// startup前に呼ばれた場合は、ダイアログに必要なコンテキストがないためエラーを返す。
func (a *App) BrowseFolder() (string, error) {
	if a.ctx == nil {
		return "", errors.New("application context is not ready")
	}

	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "検索対象フォルダを選択",
	})
}

// Search は、フロントエンドから受け取った条件で検索し、成功した検索を履歴へ保存する。
//
// 新しい検索を開始すると既存検索をキャンセルする。
// キャンセルはユーザー向けメッセージへ変換し、途中結果を履歴へ保存しない。
// 検索と履歴保存の両方が成功した場合だけHistoryIDを応答へ設定する。
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

// PreviewFile は、内容検索で一致したファイルを再読み込みし、詳細表示用の抜粋を返す。
// 検索結果応答へ本文を大量に含めず、利用者がプレビューを開いたときだけ読み取る。
func (a *App) PreviewFile(request FilePreviewRequest) (FilePreview, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return LoadFilePreview(ctx, request)
}

// CancelSearch は、実行中の検索へキャンセルを通知する。
// キャンセル対象が存在した場合はtrue、検索中でなければfalseを返す。
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

// beginSearch は、既存検索を停止して新しいキャンセル可能コンテキストを作る。
// 戻り値の世代番号は、対応するfinishSearchへ必ず渡す必要がある。
func (a *App) beginSearch() (context.Context, uint64) {
	a.searchMu.Lock()
	defer a.searchMu.Unlock()

	if a.searchCancel != nil {
		// 画面側の二重実行防止だけに依存せず、APIが直接並行呼び出しされても前の検索を停止する。
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

// finishSearch は、指定世代が現在の検索と一致するときだけキャンセル関数を解放する。
// 古い検索のdeferから呼ばれても、新しい検索のコンテキストには影響しない。
func (a *App) finishSearch(generation uint64) {
	a.searchMu.Lock()
	defer a.searchMu.Unlock()

	if generation != a.searchGeneration {
		// 古い検索のdeferは、新しい検索が登録したキャンセル関数を解放してはならない。
		return
	}
	if a.searchCancel != nil {
		a.searchCancel()
		a.searchCancel = nil
	}
}

// GetHistory は、保存済み検索履歴のスナップショットを返す。
func (a *App) GetHistory() ([]HistoryEntry, error) {
	return a.store.GetHistory()
}

// ClearHistory は、ブックマークを残したまま検索履歴を削除する。
func (a *App) ClearHistory() ([]HistoryEntry, error) {
	return a.store.ClearHistory()
}

// BookmarkHistory は、指定した履歴項目をブックマークへ追加または更新する。
func (a *App) BookmarkHistory(id string) ([]HistoryEntry, error) {
	return a.store.BookmarkHistory(id)
}

// GetBookmarks は、保存済みブックマークのスナップショットを返す。
func (a *App) GetBookmarks() ([]HistoryEntry, error) {
	return a.store.GetBookmarks()
}

// RemoveBookmark は、指定IDのブックマークを解除する。
func (a *App) RemoveBookmark(id string) ([]HistoryEntry, error) {
	return a.store.RemoveBookmark(id)
}
