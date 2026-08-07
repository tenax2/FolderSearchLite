# Folder Search Lite

Folder Search Lite は、Go と Wails で作る軽量なフォルダ検索デスクトップアプリです。

## 機能

- 指定したフォルダ配下を再帰検索する
- 文字列、拡張子、ファイル名、フォルダ名、ファイル内容で絞り込む
- Word、Excel、PowerPoint の内容を検索する
- 実行中の検索を中断する
- 検索結果と読み取りに失敗した項目数を表示する
- 検索履歴とブックマークを保存する

## 設計文書

- [クラス図](./docs/class-diagram.md)
- [基本設計書](./docs/basic-design.md)
- [詳細設計書](./docs/detailed-design.md)

## セットアップ

このプロジェクトが使用する Wails v2.13.0 は、Go 1.25.0 以上を要求します。
Wails CLI の導入後に `wails doctor` で環境を確認してください。

```powershell
go version
npm --version
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
wails doctor
```

フロントエンドは `frontend/dist` に直接配置しています。
React、Vue、Vite などの追加依存はありません。

## 起動

```powershell
cd FolderSearchLite
go mod tidy
wails dev
```

## ビルド

```powershell
wails build
```

生成物は通常 `build/bin` 配下に出力されます。

## テスト

```powershell
go test ./...
go vet ./...
node --check frontend/dist/main.js
```

## 保存先

履歴とブックマークは、OS のユーザー設定フォルダ配下に JSON で保存します。

Windows での保存先は次のとおりです。

```text
%AppData%\FolderSearchLite\state.json
```

## Office 文書検索

内容検索では、次の Office Open XML 形式に対応しています。

- **Word**：`.docx`, `.docm`, `.dotx`, `.dotm`
- **Excel**：`.xlsx`, `.xlsm`, `.xltx`, `.xltm`
- **PowerPoint**：`.pptx`, `.pptm`, `.ppsx`, `.ppsm`, `.potx`, `.potm`

古いバイナリ形式の `.doc`, `.xls`, `.ppt` などは、Office 文書の対象拡張子として扱います。
ただし、これらの形式の内容は検索しません。

## 内容検索の制約

内容検索はファイルサイズと一行の長さに上限を設けていません。
この仕様により、大きいログ、CSV、仕様書、Office 文書も検索対象にできます。

一方で、巨大なファイルや大量の Office 文書を検索すると、処理時間とメモリ使用量が増えます。
ネットワークドライブやクラウド同期フォルダでは、読み取り待ちが長くなる場合もあります。
長時間かかる検索は、画面の「中断」ボタンから停止できます。

権限不足、破損、読み取りエラーが発生した項目は検索を止めずに除外し、その件数を画面に表示します。
プレーンテキストかどうかは、先頭部分に NUL 文字が含まれるかで判定します。
Office Open XML 文書は、ZIP 内の検索対象 XML だけを読み取ります。
