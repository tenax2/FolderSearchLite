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
        +openFilePreview(result) Promise
        +highlightText(value, query, caseSensitive) string
        +refreshSavedLists() Promise
        +renderFavoriteFolders() void
        +renderFavoriteFolderList() void
        +addFavoriteFolder() Promise
        +removeFavoriteFolder(id) Promise
        +useFavoriteFolder(id) void
        +switchTab(tabName) void
    }

    class App {
        -context.Context ctx
        -Store store
        -sync.Mutex searchMu
        -context.CancelFunc searchCancel
        -uint64 searchGeneration
        -openPath(path, isDirectory) error
        +BrowseFolder() string
        +Search(request SearchRequest) SearchResponse
        +PreviewFile(request FilePreviewRequest) FilePreview
        +OpenResult(path string) void
        +CancelSearch() bool
        +GetHistory() HistoryEntry[]
        +ClearHistory() HistoryEntry[]
        +BookmarkHistory(id string) HistoryEntry[]
        +GetBookmarks() HistoryEntry[]
        +RemoveBookmark(id string) HistoryEntry[]
        +AddFavoriteFolder(path string) FavoriteFolder[]
        +GetFavoriteFolders() FavoriteFolder[]
        +RemoveFavoriteFolder(id string) FavoriteFolder[]
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
        -previewOfficeContent(ctx, filePath, ext, needle, caseSensitive, limits) FilePreviewExcerpt[]
    }

    class PreviewEngine {
        <<module>>
        +LoadFilePreview(ctx context.Context, request FilePreviewRequest) FilePreview
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
        +AddFavoriteFolder(path string) FavoriteFolder[]
        +GetFavoriteFolders() FavoriteFolder[]
        +RemoveFavoriteFolder(id string) FavoriteFolder[]
        -loadLocked() error
        -updateAndSaveLocked() error
        -saveLocked() error
    }

    class AppState {
        +HistoryEntry[] History
        +HistoryEntry[] Bookmarks
        +FavoriteFolder[] FavoriteFolders
    }

    class FavoriteFolder {
        +string ID
        +string Path
    }

    class SearchRequest {
        +string RootPath
        +string Query
        +string[] Extensions
        +string[] ExcludedFileNames
        +string[] ExcludedExtensions
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

    class FilePreviewRequest {
        +string Path
        +string Query
        +bool CaseSensitive
        +bool IncludeOfficeDocuments
    }

    class FilePreview {
        +string Name
        +string Path
        +FilePreviewExcerpt[] Excerpts
        +bool Truncated
    }

    class FilePreviewExcerpt {
        +string Location
        +string Text
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
        +favoriteFolders
    }

    class FileSystem {
        <<external>>
        +folders
        +text files
        +Office Open XML files
    }

    class SystemPathOpener {
        <<external>>
        +default application
        +Windows Explorer
    }

    FrontendController ..> App : Wails API
    FrontendController ..> SearchRequest : 構築と復元
    FrontendController ..> SearchResponse : 描画
    FrontendController ..> FilePreviewRequest : 構築
    FrontendController ..> FilePreview : 描画
    FrontendController ..> FavoriteFolder : 選択肢と管理一覧を描画
    App *-- Store : 所有
    App ..> SearchEngine : 検索実行
    App ..> SearchRequest
    App ..> SearchResponse
    App ..> PreviewEngine : 一致抜粋の取得
    App ..> SystemPathOpener : 検索結果を開く
    App ..> FilePreviewRequest
    App ..> FilePreview
    App ..> FavoriteFolder
    SearchEngine ..> OfficeDocumentScanner : Office本文解析
    SearchEngine ..> FileSystem : 再帰走査と読取
    SearchEngine ..> SearchRequest
    SearchEngine ..> SearchResponse
    PreviewEngine ..> OfficeDocumentScanner : Office抜粋の取得
    PreviewEngine ..> FileSystem : ファイルの再読み込み
    PreviewEngine ..> FilePreviewRequest
    PreviewEngine ..> FilePreview
    SearchResponse *-- "0..*" SearchResult : Results
    FilePreview *-- "0..*" FilePreviewExcerpt : Excerpts
    Store *-- AppState : メモリ状態
    AppState o-- "0..*" HistoryEntry : History
    AppState o-- "0..*" HistoryEntry : Bookmarks
    AppState o-- "0..*" FavoriteFolder : FavoriteFolders
    HistoryEntry *-- SearchRequest : Request
    Store ..> StateFile : JSON永続化
```

## 関係の説明

| 起点 | 終点 | 関係 |
|---|---|---|
| `FrontendController` | `App` | `window.go.main.App`を介して公開メソッドを呼び出す。 |
| `App` | `SearchEngine` | 検索コンテキストを作り、検索結果を受け取る。 |
| `App` | `PreviewEngine` | ファイルを再読み込みし、一致抜粋を受け取る。 |
| `App` | `SystemPathOpener` | 存在と種別を確認した検索結果を既定アプリまたはExplorerで開く。 |
| `App` | `Store` | 成功した検索だけを履歴へ保存し、履歴、ブックマーク、お気に入りフォルダーの操作を委譲する。 |
| `SearchEngine` | `OfficeDocumentScanner` | 対応するOffice Open XML形式の本文解析を委譲する。 |
| `Store` | `StateFile` | ユーザー設定ディレクトリのJSONファイルを一時ファイル経由で置換する。 |
| `HistoryEntry` | `SearchRequest` | 再検索できるよう、正規化済み検索条件を保持する。 |

## 多重度

`SearchResponse`は0件以上の`SearchResult`を持つ。

`FilePreview`は0件から12件の`FilePreviewExcerpt`を持つ。

`AppState`は最大100件の履歴を持つ。
ブックマークとお気に入りフォルダーには実装上の件数上限を設けていない。

`App`が保持する実行中検索は最大1件である。
新しい検索を開始すると、前の検索コンテキストをキャンセルする。
