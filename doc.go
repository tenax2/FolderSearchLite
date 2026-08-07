// Package main は、Folder Search Lite デスクトップアプリケーションを実装する。
//
// アプリケーションは、次の責務に分かれる。
//
//   - App はWailsから呼び出される公開APIと検索キャンセルのライフサイクルを管理する。
//   - RunSearchContext はファイルシステムを走査し、名前、拡張子、本文を照合する。
//   - Office文書解析はOffice Open XMLコンテナ内の検索対象XMLだけを読み取る。
//   - Store は検索履歴とブックマークをユーザー設定ディレクトリへ永続化する。
//
// フロントエンドとはWailsのJSONバインディングを介して通信する。
// JSONタグが付いた構造体は、フロントエンドとのデータ契約を兼ねる。
package main
