package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// assets は、配布バイナリへ組み込むHTML、CSS、JavaScriptを保持する。
// 開発用サーバーがなくても画面を起動できるよう、frontend/dist配下を再帰的に埋め込む。
//
//go:embed all:frontend/dist
var assets embed.FS

// main は、依存オブジェクトとWailsウィンドウを構成してイベントループを開始する。
//
// 初期サイズは検索条件を一行で表示できる寸法とし、最小サイズはCSSの
// レスポンシブレイアウトが操作可能な下限に合わせる。
// AppをBindへ渡すことで、公開メソッドがフロントエンドのwindow.go.main.Appへ公開される。
func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "Folder Search Lite",
		Width:     1440,
		Height:    900,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.startup,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
