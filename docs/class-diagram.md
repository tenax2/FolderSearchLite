# Folder Search Lite クラス図

## 図の対象

この図は、Goバックエンドの構造体と主要モジュール、Wailsを介して接続するフロントエンド制御部を示す。

Goの検索処理とOffice文書解析は関数群で実装されている。
クラス図では、責務と依存関係を表すためにモジュールとして記載する。

## クラス図

```mermaid
classDiagram
    direction LR

    class FrontendController {
        <<module>>
        -state
        -elements
        +runSearch(request) Promise
        +renderResults(response) void
        +renderResultRows() void
        +refreshSavedLists() Promise
        +switchTab(tabName) void
    }

    class App {
        -context.Context ctx
        -Store store
        -sync.Mutex searchMu
        -context.CancelFunc searchCancel
        -uint64 searchGeneration
        +BrowseFolder() string
        +Search(request SearchRequest) SearchResponse
        +CancelSearch() bool
        +GetHistory() HistoryEntry[]
        +ClearHistory() HistoryEntry[]
        +BookmarkHistory(id string) HistoryEntry[]
        +GetBookmarks() HistoryEntry[]
        +RemoveBookmark(id string) HistoryEntry[]
        -beginSearch() context.Context
        -finishSearch(generation uint64) void
    }

    class SearchEngine {
        <<module>>
        +RunSearch(request SearchRequest) SearchResponse
        +RunSearchContext(ctx context.Context, request SearchRequest) SearchResponse
        -normalizeRequest(request SearchRequest) SearchRequest
        -scanFileContent(ctx, path, ext, needle, caseSensitive, includeOffice) match
        -compactMatch(value, needle, caseSensitive, limit) string
        -sortResults(results SearchResult[]) void
    }

    class OfficeDocumentScanner {
        <<module>>
        -officeDocumentExtensions() string[]
        -isModernOfficeExtension(ext string) bool
        -scanOfficeContent(ctx, filePath, ext, needle, caseSensitive) match
        -shouldScanOfficePart(ext, name string) bool
        -scanXMLText(ctx, reader, needle, caseSensitive) match
    }

    class Store {
        -sync.Mutex mu
        -string path
        -AppState state
        -bool loaded
        +Load() error
        +AddHistory(entry HistoryEntry) error
        +GetHistory() HistoryEntry[]
        +ClearHistory() HistoryEntry[]
        +BookmarkHistory(id string) HistoryEntry[]
        +GetBookmarks() HistoryEntry[]
        +RemoveBookmark(id string) HistoryEntry[]
        -loadLocked() error
        -saveLocked() error
    }

    class AppState {
        +HistoryEntry[] History
        +HistoryEntry[] Bookmarks
    }

    class SearchRequest {
        +string RootPath
        +string Query
        +string[] Extensions
        +bool IncludeNames
        +bool IncludeFileNames
        +bool IncludeFolderNames
        +bool IncludeContents
        +bool IncludeOfficeDocuments
        +bool IncludeDirectories
        +bool CaseSensitive
        +int MaxResults
    }

    class SearchResponse {
        +string HistoryID
        +string RootPath
        +string Query
        +string StartedAt
        +int64 DurationMs
        +int TotalVisited
        +int FilesScanned
        +int DirectoriesScanned
        +int UnreadableItems
        +bool LimitReached
        +SearchResult[] Results
    }

    class SearchResult {
        +string ID
        +string Kind
        +string Name
        +string Path
        +string RelativePath
        +string Extension
        +int64 Size
        +string ModifiedAt
        +string[] MatchedBy
        +string Preview
    }

    class HistoryEntry {
        +string ID
        +string Label
        +SearchRequest Request
        +int ResultCount
        +string SearchedAt
        +int64 DurationMs
        +bool Bookmarked
    }

    class StateFile {
        <<artifact>>
        +history
        +bookmarks
    }

    class FileSystem {
        <<external>>
        +folders
        +text files
        +Office Open XML files
    }

    FrontendController ..> App : Wails API
    FrontendController ..> SearchRequest : 構築と復元
    FrontendController ..> SearchResponse : 描画
    App *-- Store : 所有
    App ..> SearchEngine : 検索実行
    App ..> SearchRequest
    App ..> SearchResponse
    SearchEngine ..> OfficeDocumentScanner : Office本文解析
    SearchEngine ..> FileSystem : 再帰走査と読取
    SearchEngine ..> SearchRequest
    SearchEngine ..> SearchResponse
    SearchResponse *-- "0..*" SearchResult : Results
    Store *-- AppState : メモリ状態
    AppState o-- "0..*" HistoryEntry : History
    AppState o-- "0..*" HistoryEntry : Bookmarks
    HistoryEntry *-- SearchRequest : Request
    Store ..> StateFile : JSON永続化
```

## 関係の説明

| 起点 | 終点 | 関係 |
|---|---|---|
| `FrontendController` | `App` | `window.go.main.App`を介して公開メソッドを呼び出す。 |
| `App` | `SearchEngine` | 検索コンテキストを作り、検索結果を受け取る。 |
| `App` | `Store` | 成功した検索だけを履歴へ保存し、履歴とブックマークの操作を委譲する。 |
| `SearchEngine` | `OfficeDocumentScanner` | 対応するOffice Open XML形式の本文解析を委譲する。 |
| `Store` | `StateFile` | ユーザー設定ディレクトリのJSONファイルを一時ファイル経由で置換する。 |
| `HistoryEntry` | `SearchRequest` | 再検索できるよう、正規化済み検索条件を保持する。 |

## 多重度

`SearchResponse`は0件以上の`SearchResult`を持つ。

`AppState`は最大100件の履歴を持つ。
ブックマークには実装上の件数上限を設けていない。

`App`が保持する実行中検索は最大1件である。
新しい検索を開始すると、前の検索コンテキストをキャンセルする。
