# Folder Search Lite

Go + Wails で作る軽量フォルダ検索デスクトップアプリの基盤です。

## 機能

- フォルダパス指定
- 配下のファイル・フォルダを再帰検索
- 文字列、拡張子、ファイル名、フォルダ名、ファイル内容の検索
- Word、Excel、PowerPoint の内容検索
- 結果一覧表示
- 検索履歴保存
- ブックマーク保存

## セットアップ

このプロジェクトで解決されている Wails v2.13.0 は Go 1.25.0 以上を要求します。Wails CLI の導入後に `wails doctor` で環境を確認してください。

```powershell
go version
npm --version
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
wails doctor
```

このプロジェクトはフロントエンドを `frontend/dist` に直接配置しているため、React/Vue/Vite などの追加依存はありません。

## 起動

```powershell
cd C:\Users\tenas\Documents\Codex\2026-07-08\new-chat\outputs\folder-search-lite
go mod tidy
wails dev
```

## ビルド

```powershell
wails build
```

生成物は通常 `build/bin` 配下に出力されます。

## 保存先

履歴とブックマークは OS のユーザー設定フォルダ配下に JSON で保存します。

Windows 例:

```text
%AppData%\FolderSearchLite\state.json
```

## Office 文書検索

内容検索で対応している Office 文書は、Office Open XML 形式の以下です。

- Word: `.docx`, `.docm`, `.dotx`, `.dotm`
- Excel: `.xlsx`, `.xlsm`, `.xltx`, `.xltm`
- PowerPoint: `.pptx`, `.pptm`, `.ppsx`, `.ppsm`, `.potx`, `.potm`

古いバイナリ形式の `.doc`, `.xls`, `.ppt` などは、Office 文書の対象拡張子としては扱いますが、内容検索は対象外です。

## 内容検索サイズ制限について

現在は内容検索時のファイルサイズ制限を設けていません。

メリット:

- 大きいログ、CSV、仕様書、Office 文書も検索対象から漏れにくくなります。
- 利用者がサイズ上限を意識せずに検索できます。
- 「存在するはずなのに見つからない」という取りこぼしを減らせます。

デメリット:

- 巨大ファイルや大量の Office 文書があるフォルダでは検索時間が長くなります。
- ZIP/XML 展開を伴う Office 文書では CPU とメモリ使用量が増える場合があります。
- ネットワークドライブやクラウド同期フォルダでは、読み取り待ちで UI が重く感じられることがあります。
- 破損ファイルや特殊な形式のファイルは読み飛ばされるため、完全な全文検索エンジンほどの網羅性はありません。

## 補足

プレーンテキストは行単位で読み、バイナリらしいファイルは読み飛ばします。Office Open XML 文書は ZIP 内の XML テキストだけを読み取ります。
