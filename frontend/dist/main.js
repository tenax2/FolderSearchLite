/**
 * @fileoverview
 *
 * Folder Search Liteのフロントエンド制御モジュール。
 *
 * Wailsが公開するGo APIを呼び出し、検索条件、結果グリッド、履歴、
 * ブックマークの表示状態を単一ページ内で管理する。
 * 外部フレームワークへ依存せず、DOMを直接更新する。
 */

/**
 * 画面間で共有する可変状態。
 *
 * resultsにはバックエンドから受け取った元配列を保持し、フィルターとソートでは
 * 新しい配列を作る。これにより、条件解除時に再検索せず元の並びへ戻せる。
 * currentHistoryIdは、直近の検索を保存ボタンからブックマークするために使用する。
 *
 * @type {{
 *   activeTab: string,
 *   history: Array<Object>,
 *   bookmarks: Array<Object>,
 *   currentHistoryId: string,
 *   searching: boolean,
 *   results: Array<Object>,
 *   query: string,
 *   caseSensitive: boolean,
 *   includeOfficeDocuments: boolean,
 *   previewGeneration: number,
 *   filters: Record<string, string>,
 *   sort: {key: string, direction: "asc"|"desc"}
 * }}
 */
const state = {
  activeTab: "search",
  history: [],
  bookmarks: [],
  currentHistoryId: "",
  searching: false,
  results: [],
  query: "",
  caseSensitive: false,
  includeOfficeDocuments: false,
  previewGeneration: 0,
  filters: {
    kind: "",
    name: "",
    relativePath: "",
    matchedBy: "",
    size: "",
    modifiedAt: "",
  },
  sort: {
    key: "",
    direction: "asc",
  },
};

/**
 * ファイル名とパスを日本語ロケールかつ自然数順で比較する照合器。
 * numericを有効にするため、file2はfile10より前に並ぶ。
 */
const resultCollator = new Intl.Collator("ja", {
  numeric: true,
  sensitivity: "base",
});

/**
 * 起動時に解決するDOM要素の参照一覧。
 *
 * HTML側のIDとクラスは、このオブジェクトを介してJavaScriptの処理へ接続する。
 * 必須要素が存在することを前提とし、イベント登録と再描画で同じ参照を再利用する。
 */
const elements = {
  statusText: document.querySelector("#statusText"),
  themeOptions: [...document.querySelectorAll(".theme-option")],
  tabButtons: [...document.querySelectorAll(".tab-button")],
  panels: {
    search: document.querySelector("#searchPanel"),
    history: document.querySelector("#historyPanel"),
    bookmarks: document.querySelector("#bookmarksPanel"),
  },
  form: document.querySelector("#searchForm"),
  rootPath: document.querySelector("#rootPath"),
  query: document.querySelector("#query"),
  extensions: document.querySelector("#extensions"),
  includeFileNames: document.querySelector("#includeFileNames"),
  includeFolderNames: document.querySelector("#includeFolderNames"),
  includeContents: document.querySelector("#includeContents"),
  includeOfficeDocuments: document.querySelector("#includeOfficeDocuments"),
  caseSensitive: document.querySelector("#caseSensitive"),
  maxResults: document.querySelector("#maxResults"),
  browseButton: document.querySelector("#browseButton"),
  searchButton: document.querySelector("#searchButton"),
  cancelSearchButton: document.querySelector("#cancelSearchButton"),
  bookmarkCurrentButton: document.querySelector("#bookmarkCurrentButton"),
  resultSummary: document.querySelector("#resultSummary"),
  scanSummary: document.querySelector("#scanSummary"),
  resultsBody: document.querySelector("#resultsBody"),
  sortButtons: [...document.querySelectorAll(".sort-button")],
  filterControls: [...document.querySelectorAll(".column-filter")],
  clearFiltersButton: document.querySelector("#clearFiltersButton"),
  previewDialog: document.querySelector("#previewDialog"),
  previewTitle: document.querySelector("#previewTitle"),
  previewPath: document.querySelector("#previewPath"),
  previewContent: document.querySelector("#previewContent"),
  closePreviewButton: document.querySelector("#closePreviewButton"),
  historyList: document.querySelector("#historyList"),
  bookmarkList: document.querySelector("#bookmarkList"),
  clearHistoryButton: document.querySelector("#clearHistoryButton"),
};

const THEME_STORAGE_KEY = "folder-search-lite-theme";

/**
 * 指定テーマを画面へ反映し、切替ボタンの押下状態を同期する。
 *
 * @param {"light"|"black"} theme 適用するテーマ名。
 * @param {boolean} persist ユーザー設定として保存する場合はtrue。
 * @returns {void}
 */
function applyTheme(theme, persist = true) {
  const selectedTheme = theme === "black" ? "black" : "light";
  document.documentElement.dataset.theme = selectedTheme;

  for (const button of elements.themeOptions) {
    button.setAttribute("aria-pressed", String(button.dataset.themeOption === selectedTheme));
  }

  if (persist) {
    try {
      localStorage.setItem(THEME_STORAGE_KEY, selectedTheme);
    } catch {
      // 保存できない環境でも、現在の画面にはテーマを適用する。
    }
  }
}

/**
 * Wailsがwindowへ公開したApp APIを取得する。
 * ブラウザーだけでHTMLを開いた場合はundefinedを返す。
 *
 * @returns {Object|undefined} GoのAppメソッドを持つプロキシ。
 */
function appApi() {
  return window.go?.main?.App;
}

/**
 * Wailsバックエンドの指定メソッドを呼び出す。
 * APIまたはメソッドが存在しない場合は、通常のErrorへ変換して呼び出し元へ返す。
 *
 * @param {string} methodName window.go.main.App上のメソッド名。
 * @param {...unknown} args Goメソッドへ順番に渡す引数。
 * @returns {Promise<unknown>} WailsがJSON変換した戻り値。
 * @throws {Error} Wails APIが利用できない場合、またはバックエンドがエラーを返した場合。
 */
async function callBackend(methodName, ...args) {
  const api = appApi();
  if (!api || typeof api[methodName] !== "function") {
    throw new Error("Wails API が見つかりません");
  }
  return api[methodName](...args);
}

/**
 * ヘッダーの状態メッセージを置き換える。
 *
 * @param {string} message ユーザーへ表示する短い状態説明。
 * @returns {void}
 */
function setStatus(message) {
  elements.statusText.textContent = message;
}

/**
 * 検索実行中フラグと関連ボタンの活性状態を同期する。
 * 二重実行を防ぐため検索ボタンと参照ボタンを無効化し、中断ボタンだけを有効化する。
 *
 * @param {boolean} searching 検索処理が進行中ならtrue。
 * @returns {void}
 */
function setSearching(searching) {
  state.searching = searching;
  elements.searchButton.disabled = searching;
  elements.browseButton.disabled = searching;
  elements.cancelSearchButton.disabled = !searching;
  elements.searchButton.textContent = searching ? "検索中" : "検索";
}

/**
 * 任意の例外値から表示可能なエラーメッセージを取り出す。
 * Error以外がthrowされた場合も文字列化し、空値には既定メッセージを使用する。
 *
 * @param {unknown} error 捕捉した例外値。
 * @returns {string} 状態欄へ表示するメッセージ。
 */
function errorMessage(error) {
  return error?.message || String(error || "不明なエラーが発生しました");
}

/**
 * 拡張子入力欄をバックエンドへ渡す文字列配列へ分解する。
 * カンマ、空白、セミコロンを区切りとして扱い、空要素を除く。
 * ピリオド付与、小文字化、重複除去はバックエンドが担当する。
 *
 * @param {string} value 拡張子入力欄の文字列。
 * @returns {string[]} 入力順を保った拡張子配列。
 */
function parseExtensions(value) {
  return value
    .split(/[,\s;]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

/**
 * 現在の検索フォームからバックエンド用の要求オブジェクトを構築する。
 *
 * includeNamesとincludeDirectoriesは古い履歴形式との互換性のため、
 * 新しい個別フラグと同時に送信する。入力要素自体は変更しない。
 *
 * @returns {Object} GoのSearchRequestへJSON変換できる検索条件。
 */
function buildRequest() {
  const includeFileNames = elements.includeFileNames.checked;
  const includeFolderNames = elements.includeFolderNames.checked;

  return {
    rootPath: elements.rootPath.value.trim(),
    query: elements.query.value.trim(),
    extensions: parseExtensions(elements.extensions.value),
    includeNames: includeFileNames || includeFolderNames,
    includeFileNames,
    includeFolderNames,
    includeDirectories: includeFolderNames,
    includeContents: elements.includeContents.checked,
    includeOfficeDocuments: elements.includeOfficeDocuments.checked,
    caseSensitive: elements.caseSensitive.checked,
    maxResults: Number(elements.maxResults.value || 500),
  };
}

/**
 * 履歴またはブックマークの検索条件をフォームへ復元する。
 *
 * 個別の名前検索フラグがない旧データでは、includeNamesと
 * includeDirectoriesをフォールバック値として使用する。
 *
 * @param {Object} request 保存済みの検索要求。
 * @returns {void}
 */
function applyRequest(request) {
  elements.rootPath.value = request.rootPath || "";
  elements.query.value = request.query || "";
  elements.extensions.value = (request.extensions || []).join(", ");
  elements.includeFileNames.checked = request.includeFileNames ?? request.includeNames ?? true;
  elements.includeFolderNames.checked = request.includeFolderNames ?? request.includeDirectories ?? true;
  elements.includeContents.checked = Boolean(request.includeContents);
  elements.includeOfficeDocuments.checked = request.includeOfficeDocuments !== false;
  elements.caseSensitive.checked = Boolean(request.caseSensitive);
  elements.maxResults.value = request.maxResults || 500;
}

/**
 * 履歴とブックマークを並行取得し、両方の一覧を再描画する。
 * 取得に失敗した場合は既存表示を維持し、状態欄へエラーを表示する。
 *
 * @returns {Promise<void>}
 */
async function refreshSavedLists() {
  try {
    const [history, bookmarks] = await Promise.all([
      callBackend("GetHistory"),
      callBackend("GetBookmarks"),
    ]);
    state.history = history || [];
    state.bookmarks = bookmarks || [];
    renderHistory();
    renderBookmarks();
  } catch (error) {
    setStatus(errorMessage(error));
  }
}

/**
 * 検索要求をバックエンドへ送り、結果、履歴、ボタン状態を更新する。
 *
 * フォルダ未入力と二重実行はバックエンドを呼ばず状態メッセージだけを更新する。
 * 成功時は検索結果を描画して履歴を再取得し、直近履歴の保存ボタンを有効化する。
 * finallyで必ず検索中状態を解除するため、例外後も再検索できる。
 *
 * @param {Object} request buildRequestまたは保存済み履歴から得た検索条件。
 * @returns {Promise<void>}
 */
async function runSearch(request) {
  if (state.searching) {
    setStatus("実行中の検索を中断してから再検索してください");
    return;
  }
  if (!request.rootPath) {
    setStatus("フォルダを入力してください");
    return;
  }

  setSearching(true);
  setStatus("検索中");
  elements.bookmarkCurrentButton.disabled = true;

  try {
    const response = await callBackend("Search", request);
    state.currentHistoryId = response.historyId;
    renderResults(response, request);
    await refreshSavedLists();
    elements.bookmarkCurrentButton.disabled = !state.currentHistoryId;
    const details = [];
    if (response.limitReached) {
      details.push("上限到達");
    }
    if (response.unreadableItems > 0) {
      details.push(`読み取り失敗 ${response.unreadableItems} 件`);
    }
    const detailText = details.length ? ` / ${details.join(" / ")}` : "";
    setStatus(`${response.results.length} 件 / ${response.durationMs} ms${detailText}`);
  } catch (error) {
    setStatus(errorMessage(error));
  } finally {
    setSearching(false);
  }
}

/**
 * 新しい検索応答を画面状態へ取り込み、統計と結果行を描画する。
 * フィルターとソート条件は維持されるため、再検索後の結果にも同じ条件を適用する。
 *
 * 検索語と大小文字条件も保持し、一覧と詳細プレビューの強調表示に使用する。
 *
 * @param {Object} response GoのSearchResponseをJSON変換した値。
 * @param {Object} request 応答を生成したSearchRequest互換の条件。
 * @returns {void}
 */
function renderResults(response, request) {
  closeFilePreview();
  state.results = Array.isArray(response.results) ? response.results : [];
  state.query = String(response.query ?? request.query ?? "");
  state.caseSensitive = Boolean(request.caseSensitive);
  state.includeOfficeDocuments = Boolean(request.includeOfficeDocuments);
  elements.scanSummary.textContent = `${response.totalVisited.toLocaleString()} item / ${response.filesScanned.toLocaleString()} files / ${response.directoriesScanned.toLocaleString()} folders`;
  renderResultRows();
}

/**
 * 現在の結果配列へフィルターとソートを適用し、tbodyを再構築する。
 *
 * フィルター中は「表示件数 / 全件数」を表示する。
 * 元結果が存在するのに表示対象が0件の場合は、検索結果なしと
 * フィルター不一致を区別したメッセージを表示する。
 * 動的文字列はescapeHtmlまたはescapeAttributeを通してからinnerHTMLへ渡す。
 *
 * @returns {void}
 */
function renderResultRows() {
  const results = getVisibleResults();
  const hasActiveFilters = Object.values(state.filters).some(Boolean);
  elements.resultSummary.textContent = hasActiveFilters
    ? `${results.length.toLocaleString()} / ${state.results.length.toLocaleString()} 件`
    : `${state.results.length.toLocaleString()} 件`;
  elements.resultsBody.innerHTML = "";

  if (!results.length) {
    const row = document.createElement("tr");
    row.innerHTML = `<td colspan="7" class="empty">${state.results.length ? "フィルター条件に一致する結果なし" : "結果なし"}</td>`;
    elements.resultsBody.append(row);
    return;
  }

  for (const result of results) {
    const row = document.createElement("tr");
    const matchedBy = result.matchedBy || [];
    const matches = matchedBy.map((item) => `<span class="match-pill">${escapeHtml(matchLabel(item))}</span>`).join("");
    const nameMatched = matchedBy.includes("file-name") || matchedBy.includes("folder-name");
    const contentMatched = matchedBy.includes("content") && Boolean(result.preview);
    const name = nameMatched
      ? highlightText(result.name, state.query, state.caseSensitive)
      : escapeHtml(result.name);
    const preview = contentMatched
      ? `<div class="result-preview"><span class="preview-label">内容</span><span>${highlightText(result.preview, state.query, state.caseSensitive)}</span></div>`
      : "";
    const previewButton = contentMatched
      ? `<button class="secondary row-action" type="button" data-preview-id="${escapeAttribute(result.id)}" aria-label="${escapeAttribute(result.name)}の内容をプレビュー">プレビュー</button>`
      : "";
    const openLabel = result.kind === "folder" ? "フォルダを開く" : "ファイルを開く";
    row.innerHTML = `
      <td><span class="kind-pill ${result.kind}">${result.kind === "folder" ? "フォルダ" : "ファイル"}</span></td>
      <td class="name-cell truncate">${name}</td>
      <td class="path-cell truncate">${escapeHtml(result.relativePath)}${preview}</td>
      <td>${matches}</td>
      <td>${result.kind === "folder" ? "-" : formatBytes(result.size)}</td>
      <td>${formatDate(result.modifiedAt)}</td>
      <td><div class="row-actions"><button class="secondary row-action" type="button" data-open-id="${escapeAttribute(result.id)}" aria-label="${escapeAttribute(result.name)}の${openLabel}">開く</button>${previewButton}<button class="secondary row-action" type="button" data-copy="${escapeAttribute(result.path)}">コピー</button></div></td>
    `;
    elements.resultsBody.append(row);
  }
}

/**
 * 元の検索結果から、現在の列フィルターと並び順に一致する配列を作る。
 * state.resultsは変更せず、filterで生成した新しい配列だけをsortする。
 *
 * @returns {Object[]} グリッドへ表示する検索結果。
 */
function getVisibleResults() {
  const results = state.results.filter((result) => {
    if (state.filters.kind && result.kind !== state.filters.kind) {
      return false;
    }
    if (!includesFilter(result.name, state.filters.name)) {
      return false;
    }
    const pathText = `${result.relativePath || ""} ${result.preview || ""}`;
    if (!includesFilter(pathText, state.filters.relativePath)) {
      return false;
    }
    if (state.filters.matchedBy && !(result.matchedBy || []).includes(state.filters.matchedBy)) {
      return false;
    }
    if (!matchesSizeFilter(result, state.filters.size)) {
      return false;
    }
    return includesFilter(formatDate(result.modifiedAt), state.filters.modifiedAt);
  });

  if (!state.sort.key) {
    return results;
  }

  const direction = state.sort.direction === "desc" ? -1 : 1;
  return results.sort((left, right) => compareResults(left, right, state.sort.key) * direction);
}

/**
 * 値が大文字と小文字を区別せずフィルター文字列を含むか判定する。
 * 空フィルターは全件一致として扱い、nullとundefinedは空文字へ変換する。
 *
 * @param {unknown} value 検索対象の値。
 * @param {string} filter 入力済みフィルター。
 * @returns {boolean} 表示条件に一致する場合はtrue。
 */
function includesFilter(value, filter) {
  return !filter || String(value ?? "").toLocaleLowerCase("ja").includes(filter.toLocaleLowerCase("ja"));
}

/**
 * 検索結果のサイズが入力条件に一致するか判定する。
 *
 * 「>= 1 MB」のような比較演算子とB、KB、MB、GB、TBを解釈する。
 * 演算式として解釈できない入力は、formatBytesの表示文字列に対する部分一致へ切り替える。
 * フォルダはサイズを持たないため、「-」または「フォルダ」の文字列検索だけに一致する。
 *
 * @param {Object} result SearchResult互換の結果。
 * @param {string} filter サイズ欄へ入力された条件。
 * @returns {boolean} サイズ条件に一致する場合はtrue。
 */
function matchesSizeFilter(result, filter) {
  if (!filter) {
    return true;
  }
  if (result.kind === "folder") {
    return includesFilter("- フォルダ", filter);
  }

  const comparison = filter.trim().match(/^(<=|>=|<|>|=)?\s*(\d+(?:\.\d+)?)\s*(b|kb|mb|gb|tb)?$/i);
  if (!comparison) {
    return includesFilter(formatBytes(result.size), filter);
  }

  const units = { b: 1, kb: 1024, mb: 1024 ** 2, gb: 1024 ** 3, tb: 1024 ** 4 };
  const operator = comparison[1] || "=";
  const threshold = Number(comparison[2]) * units[(comparison[3] || "b").toLowerCase()];
  return {
    "<": result.size < threshold,
    "<=": result.size <= threshold,
    "=": result.size === threshold,
    ">=": result.size >= threshold,
    ">": result.size > threshold,
  }[operator];
}

/**
 * 二つの検索結果を指定列で比較する。
 * サイズはバイト数、更新日時はUNIX時刻、それ以外は日本語照合器を使用する。
 *
 * @param {Object} left 左辺の検索結果。
 * @param {Object} right 右辺の検索結果。
 * @param {string} key ソート対象列のキー。
 * @returns {number} 負数、0、正数の比較結果。
 */
function compareResults(left, right, key) {
  if (key === "size") {
    return Number(left.size || 0) - Number(right.size || 0);
  }
  if (key === "modifiedAt") {
    return dateValue(left.modifiedAt) - dateValue(right.modifiedAt);
  }

  const leftValue = resultSortValue(left, key);
  const rightValue = resultSortValue(right, key);
  return resultCollator.compare(leftValue, rightValue);
}

/**
 * 文字列列をソート用のユーザー向け表現へ変換する。
 * 種別と一致理由は、内部コードではなく画面に表示する日本語で比較する。
 *
 * @param {Object} result SearchResult互換の結果。
 * @param {string} key ソート対象列のキー。
 * @returns {string} Intl.Collatorへ渡す比較文字列。
 */
function resultSortValue(result, key) {
  if (key === "kind") {
    return result.kind === "folder" ? "フォルダ" : "ファイル";
  }
  if (key === "matchedBy") {
    return (result.matchedBy || []).map(matchLabel).join(" ");
  }
  return String(result[key] ?? "");
}

/**
 * 日時文字列を比較可能なミリ秒値へ変換する。
 * 不正な日時は0とし、有効な日時より前へ並ぶ値として扱う。
 *
 * @param {string} value RFC 3339などDateが解釈できる日時文字列。
 * @returns {number} 1970-01-01T00:00:00Zからのミリ秒。
 */
function dateValue(value) {
  const time = new Date(value).getTime();
  return Number.isNaN(time) ? 0 : time;
}

/**
 * 現在のソート状態を列見出しの記号とaria-sortへ反映する。
 * スクリーンリーダーにはascendingまたはdescendingを伝え、
 * 未選択列にはnoneと双方向矢印を設定する。
 *
 * @returns {void}
 */
function updateSortIndicators() {
  for (const button of elements.sortButtons) {
    const active = button.dataset.sort === state.sort.key;
    const direction = active ? state.sort.direction : "";
    button.closest("th").setAttribute("aria-sort", active ? (direction === "asc" ? "ascending" : "descending") : "none");
    button.querySelector(".sort-indicator").textContent = active ? (direction === "asc" ? "▲" : "▼") : "↕";
  }
}

/**
 * すべての列フィルターを状態と入力要素の両方から解除する。
 * ソート条件は保持し、解除後の結果をただちに再描画する。
 *
 * @returns {void}
 */
function clearResultFilters() {
  for (const key of Object.keys(state.filters)) {
    state.filters[key] = "";
  }
  for (const control of elements.filterControls) {
    control.value = "";
  }
  elements.clearFiltersButton.disabled = true;
  renderResultRows();
}

/**
 * 内容一致したファイルの詳細プレビューを開く。
 *
 * 一覧にある最初の抜粋をただちに表示した後、バックエンドから
 * 追加の一致箇所を遅延取得する。世代番号により、閉じた後や別ファイルを
 * 開いた後に到着した古い応答が画面を上書きしないようにする。
 *
 * @param {Object} result 内容一致を含むSearchResult互換の結果。
 * @returns {Promise<void>}
 */
async function openFilePreview(result) {
  const generation = ++state.previewGeneration;
  elements.previewTitle.textContent = result.name || "内容プレビュー";
  elements.previewPath.textContent = result.path || "";
  elements.previewContent.innerHTML = `
    <p class="preview-loading" role="status">一致箇所を読み込んでいます…</p>
    ${previewExcerptHtml({ location: "最初の一致", text: result.preview })}
  `;

  if (!elements.previewDialog.open) {
    elements.previewDialog.showModal();
  }

  try {
    const preview = await callBackend("PreviewFile", {
      path: result.path,
      query: state.query,
      caseSensitive: state.caseSensitive,
      includeOfficeDocuments: state.includeOfficeDocuments,
    });
    if (generation !== state.previewGeneration || !elements.previewDialog.open) {
      return;
    }
    renderFilePreview(preview);
  } catch (error) {
    if (generation !== state.previewGeneration || !elements.previewDialog.open) {
      return;
    }
    elements.previewContent.innerHTML = `
      <p class="preview-error">${escapeHtml(errorMessage(error))}</p>
      ${previewExcerptHtml({ location: "検索時の一致", text: result.preview })}
    `;
  }
}

/**
 * バックエンドが返した一致箇所をプレビューダイアログへ描画する。
 *
 * @param {Object} preview FilePreview互換の応答。
 * @returns {void}
 */
function renderFilePreview(preview) {
  const excerpts = Array.isArray(preview.excerpts) ? preview.excerpts : [];
  elements.previewTitle.textContent = preview.name || elements.previewTitle.textContent;
  elements.previewPath.textContent = preview.path || elements.previewPath.textContent;

  if (!excerpts.length) {
    elements.previewContent.innerHTML = `<p class="preview-empty">ファイルが更新されたか、現在の内容に検索語がありません。</p>`;
    return;
  }

  const omitted = preview.truncated
    ? `<p class="preview-note">一致箇所が多いため、先頭 ${excerpts.length.toLocaleString()} 件を表示しています。</p>`
    : "";
  elements.previewContent.innerHTML = `${excerpts.map(previewExcerptHtml).join("")}${omitted}`;
}

/**
 * 一つの一致箇所を、位置ラベルと強調済みテキストのHTMLへ変換する。
 *
 * @param {Object} excerpt FilePreviewExcerpt互換の値。
 * @returns {string} プレビュー一件分の安全なHTML。
 */
function previewExcerptHtml(excerpt) {
  return `
    <article class="preview-excerpt">
      <div class="preview-location">${escapeHtml(excerpt.location || "一致箇所")}</div>
      <div class="preview-text">${highlightText(excerpt.text || "", state.query, state.caseSensitive)}</div>
    </article>
  `;
}

/**
 * 開いているプレビューを閉じ、進行中の取得応答を無効化する。
 *
 * @returns {void}
 */
function closeFilePreview() {
  state.previewGeneration++;
  if (elements.previewDialog.open) {
    elements.previewDialog.close();
  }
}

/**
 * 現在の履歴状態を、保存ボタン付きの一覧として描画する。
 *
 * @returns {void}
 */
function renderHistory() {
  renderSavedList(elements.historyList, state.history, {
    emptyText: "履歴なし",
    showBookmark: true,
  });
}

/**
 * 現在のブックマーク状態を、解除ボタン付きの一覧として描画する。
 *
 * @returns {void}
 */
function renderBookmarks() {
  renderSavedList(elements.bookmarkList, state.bookmarks, {
    emptyText: "ブックマークなし",
    showRemove: true,
  });
}

/**
 * 履歴またはブックマークの共通カード一覧を構築する。
 *
 * data-rerun、data-bookmark、data-remove-bookmark属性へIDを設定し、
 * document単位のクリック委譲で後続操作を識別できるようにする。
 * 保存データ由来の文字列はHTMLまたは属性用にエスケープする。
 *
 * @param {HTMLElement} container 一覧カードを追加する親要素。
 * @param {Object[]} entries HistoryEntry互換の配列。
 * @param {{emptyText: string, showBookmark?: boolean, showRemove?: boolean}} options 表示する操作と空表示文言。
 * @returns {void}
 */
function renderSavedList(container, entries, options) {
  container.innerHTML = "";
  if (!entries.length) {
    const empty = document.createElement("div");
    empty.className = "empty";
    empty.textContent = options.emptyText;
    container.append(empty);
    return;
  }

  for (const entry of entries) {
    const item = document.createElement("article");
    item.className = "saved-item";
    item.innerHTML = `
      <div>
        <div class="saved-title">${escapeHtml(entry.label)}</div>
        <div class="saved-meta">${escapeHtml(entry.request.rootPath)} / ${entry.resultCount.toLocaleString()} 件 / ${formatDate(entry.searchedAt)}</div>
      </div>
      <div class="saved-actions">
        <button class="secondary" type="button" data-rerun="${escapeAttribute(entry.id)}">再検索</button>
        ${options.showBookmark ? `<button class="secondary" type="button" data-bookmark="${escapeAttribute(entry.id)}" ${entry.bookmarked ? "disabled" : ""}>保存</button>` : ""}
        ${options.showRemove ? `<button class="secondary danger" type="button" data-remove-bookmark="${escapeAttribute(entry.id)}">解除</button>` : ""}
      </div>
    `;
    container.append(item);
  }
}

/**
 * 表示中のタブとtabpanelを指定名へ切り替える。
 * 見た目のactiveクラスとアクセシビリティ用aria-selectedを同時に更新する。
 *
 * @param {"search"|"history"|"bookmarks"} tabName 表示するタブ名。
 * @returns {void}
 */
function switchTab(tabName) {
  state.activeTab = tabName;
  for (const button of elements.tabButtons) {
    const active = button.dataset.tab === tabName;
    button.classList.toggle("active", active);
    button.setAttribute("aria-selected", String(active));
  }
  for (const [name, panel] of Object.entries(elements.panels)) {
    panel.classList.toggle("active", name === tabName);
  }
}

/**
 * 履歴とブックマークを横断して指定IDの保存項目を探す。
 *
 * @param {string} id HistoryEntryのID。
 * @returns {Object|undefined} 最初に一致した項目。存在しなければundefined。
 */
function findSavedEntry(id) {
  return [...state.history, ...state.bookmarks].find((entry) => entry.id === id);
}

/**
 * 直近の検索履歴をブックマークする。
 * 検索成功後のHistoryIDがない場合は何も行わない。
 *
 * @returns {Promise<void>}
 */
async function bookmarkCurrent() {
  if (!state.currentHistoryId) {
    return;
  }
  await bookmarkHistory(state.currentHistoryId);
}

/**
 * 指定履歴をブックマークし、履歴とブックマークの表示を同期する。
 * 成功後は同じ項目を重複保存できないよう現在の保存ボタンを無効化する。
 *
 * @param {string} id 保存対象の履歴ID。
 * @returns {Promise<void>}
 */
async function bookmarkHistory(id) {
  try {
    state.bookmarks = (await callBackend("BookmarkHistory", id)) || [];
    await refreshSavedLists();
    elements.bookmarkCurrentButton.disabled = true;
    setStatus("保存しました");
  } catch (error) {
    setStatus(errorMessage(error));
  }
}

/**
 * 指定IDのブックマークを解除し、保存済み一覧を再取得する。
 *
 * @param {string} id 解除対象のブックマークID。
 * @returns {Promise<void>}
 */
async function removeBookmark(id) {
  try {
    state.bookmarks = (await callBackend("RemoveBookmark", id)) || [];
    await refreshSavedLists();
    setStatus("解除しました");
  } catch (error) {
    setStatus(errorMessage(error));
  }
}

/**
 * ユーザー確認後に検索履歴をすべて削除する。
 * ブックマークはバックエンド側で保持される。
 * 確認をキャンセルした場合はAPIを呼ばない。
 *
 * @returns {Promise<void>}
 */
async function clearHistory() {
  if (!window.confirm("検索履歴をすべて削除しますか？")) {
    return;
  }
  try {
    state.history = (await callBackend("ClearHistory")) || [];
    renderHistory();
    setStatus("履歴をクリアしました");
  } catch (error) {
    setStatus(errorMessage(error));
  }
}

/**
 * バイト数を最大TBまでの読みやすい単位へ変換する。
 * 10未満のKB以上だけ小数第一位を残し、0以下または非数は0 Bとする。
 *
 * @param {number} value バイト数。
 * @returns {string} 画面表示用サイズ。
 */
function formatBytes(value) {
  if (!Number.isFinite(value) || value <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = value;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex++;
  }
  return `${size.toFixed(size >= 10 || unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`;
}

/**
 * 日時文字列を日本語ロケールの年月日時分へ変換する。
 * 空値はハイフン、不正な日時は元の文字列を返して情報を失わない。
 *
 * @param {string} value Dateが解釈できる日時文字列。
 * @returns {string} グリッドと保存一覧で使用する日時表現。
 */
function formatDate(value) {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString("ja-JP", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/**
 * バックエンドの一致理由コードをユーザー向けラベルへ変換する。
 * 未知のコードは将来拡張の表示を失わないよう、そのまま返す。
 *
 * @param {string} value extensionなどの一致理由コード。
 * @returns {string} 日本語の表示ラベル。
 */
function matchLabel(value) {
  return {
    extension: "拡張子",
    "file-name": "ファイル名",
    "folder-name": "フォルダ名",
    content: "内容",
  }[value] || value;
}

/**
 * テキスト内の検索語をmark要素で囲み、HTMLとして安全な文字列を返す。
 *
 * 検索語を正規表現としてエスケープし、ファイル内の文字列は一致区間ごとに
 * escapeHtmlを適用する。これにより、<や&を含む検索語でも表示と強調を両立する。
 *
 * @param {unknown} value 表示するテキスト。
 * @param {string} query 強調する検索語。
 * @param {boolean} caseSensitive 大文字と小文字を区別する場合はtrue。
 * @returns {string} 一致箇所だけにmark要素を持つHTML。
 */
function highlightText(value, query, caseSensitive) {
  const text = String(value ?? "");
  if (!query) {
    return escapeHtml(text);
  }

  let matcher;
  try {
    matcher = new RegExp(escapeRegExp(query), caseSensitive ? "gu" : "giu");
  } catch {
    return escapeHtml(text);
  }

  let html = "";
  let cursor = 0;
  for (const match of text.matchAll(matcher)) {
    const index = match.index ?? 0;
    html += escapeHtml(text.slice(cursor, index));
    html += `<mark class="search-hit">${escapeHtml(match[0])}</mark>`;
    cursor = index + match[0].length;
  }
  html += escapeHtml(text.slice(cursor));
  return html;
}

/**
 * 任意の文字列を、正規表現のリテラル文字列として扱えるようにエスケープする。
 *
 * @param {string} value エスケープする検索語。
 * @returns {string} RegExpコンストラクタへ安全に渡せる文字列。
 */
function escapeRegExp(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * 動的値をHTMLテキストとして安全に埋め込める文字列へ変換する。
 * アンパサンドを先に置換し、後続のエンティティ表現を再変換しない。
 *
 * @param {unknown} value HTMLへ埋め込む値。
 * @returns {string} &, <, >, 引用符をエスケープした文字列。
 */
function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

/**
 * 動的値をHTML属性値として安全に埋め込める文字列へ変換する。
 * escapeHtmlに加えてテンプレートリテラルで意味を持つバッククォートも置換する。
 *
 * @param {unknown} value data属性へ埋め込む値。
 * @returns {string} 属性値用にエスケープした文字列。
 */
function escapeAttribute(value) {
  return escapeHtml(value).replaceAll("`", "&#096;");
}

/**
 * 文字列をOSのクリップボードへコピーする。
 *
 * Clipboard APIが利用できる環境ではwriteTextを使う。
 * 利用できないWebViewでは、画面外textareaとexecCommandを一時的に使う。
 *
 * @param {string} value コピーするフルパスなどの文字列。
 * @returns {Promise<void>}
 * @throws {Error} ブラウザーがコピー操作を拒否した場合。
 */
async function copyText(value) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }

  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  document.body.append(textarea);
  textarea.select();
  document.execCommand("copy");
  textarea.remove();
}

/*
 * イベント配線
 *
 * 起動時に取得したDOM参照へ操作イベントを登録する。
 * 行や保存カード内の動的ボタンは再描画で置き換わるため、
 * 個別登録せずdocumentのクリックハンドラーへ委譲する。
 */

// 初期テーマを切替ボタンへ同期し、ユーザー操作時は次回起動用に保存する。
applyTheme(document.documentElement.dataset.theme === "black" ? "black" : "light", false);
elements.themeOptions.forEach((button) => {
  button.addEventListener("click", () => applyTheme(button.dataset.themeOption));
});

// 検索フォームの既定送信を止め、現在の入力値から非同期検索を開始する。
elements.form.addEventListener("submit", (event) => {
  event.preventDefault();
  runSearch(buildRequest());
});

// OSのフォルダ選択ダイアログを開き、選択されたパスだけを入力欄へ反映する。
elements.browseButton.addEventListener("click", async () => {
  try {
    const folder = await callBackend("BrowseFolder");
    if (folder) {
      elements.rootPath.value = folder;
      setStatus("フォルダを選択しました");
    }
  } catch (error) {
    setStatus(errorMessage(error));
  }
});

// 直近検索の保存操作は、履歴IDの検証をbookmarkCurrentへ委譲する。
elements.bookmarkCurrentButton.addEventListener("click", bookmarkCurrent);

// 実行中検索へキャンセルを通知し、応答待ちのあいだ中断ボタンを再度押せないようにする。
elements.cancelSearchButton.addEventListener("click", async () => {
  if (!state.searching) {
    return;
  }

  elements.cancelSearchButton.disabled = true;
  setStatus("検索を中断しています");
  try {
    await callBackend("CancelSearch");
  } catch (error) {
    setStatus(errorMessage(error));
  }
});
// 履歴全削除と列フィルター解除は、それぞれの状態更新関数へ委譲する。
elements.clearHistoryButton.addEventListener("click", clearHistory);
elements.clearFiltersButton.addEventListener("click", clearResultFilters);
elements.closePreviewButton.addEventListener("click", closeFilePreview);
elements.previewDialog.addEventListener("close", () => {
  state.previewGeneration++;
});
elements.previewDialog.addEventListener("click", (event) => {
  if (event.target === elements.previewDialog) {
    closeFilePreview();
  }
});

// 列フィルターの入力ごとに対応する状態キーを更新し、バックエンドを呼ばず結果だけを再描画する。
elements.filterControls.forEach((control) => {
  control.addEventListener("input", () => {
    state.filters[control.dataset.filter] = control.value.trim();
    elements.clearFiltersButton.disabled = !Object.values(state.filters).some(Boolean);
    renderResultRows();
  });
});

// 列見出しは、同じ列なら昇順と降順を反転し、別列なら昇順から開始する。
elements.sortButtons.forEach((button) => {
  button.addEventListener("click", () => {
    const key = button.dataset.sort;
    if (state.sort.key === key) {
      state.sort.direction = state.sort.direction === "asc" ? "desc" : "asc";
    } else {
      state.sort.key = key;
      state.sort.direction = "asc";
    }
    updateSortIndicators();
    renderResultRows();
  });
});

// タブボタンのdata-tabを、表示パネルとARIA状態を更新するキーとして使用する。
elements.tabButtons.forEach((button) => {
  button.addEventListener("click", () => switchTab(button.dataset.tab));
});

// 再描画される結果行と保存カード内のボタン操作をdata属性で判別する。
document.addEventListener("click", async (event) => {
  const target = event.target.closest("button");
  if (!target) {
    return;
  }

  const openId = target.dataset.openId;
  if (openId) {
    const result = state.results.find((item) => item.id === openId);
    if (result) {
      try {
        await callBackend("OpenResult", result.path);
        setStatus(result.kind === "folder" ? "フォルダを開きました" : "ファイルを開きました");
      } catch (error) {
        setStatus(errorMessage(error));
      }
    }
    return;
  }

  const previewId = target.dataset.previewId;
  if (previewId) {
    const result = state.results.find((item) => item.id === previewId);
    if (result) {
      openFilePreview(result);
    }
    return;
  }

  const copyPath = target.dataset.copy;
  if (copyPath) {
    try {
      await copyText(copyPath);
      setStatus("コピーしました");
    } catch (error) {
      setStatus(errorMessage(error));
    }
    return;
  }

  const rerunId = target.dataset.rerun;
  if (rerunId) {
    const entry = findSavedEntry(rerunId);
    if (entry) {
      applyRequest(entry.request);
      switchTab("search");
      runSearch(entry.request);
    }
    return;
  }

  const bookmarkId = target.dataset.bookmark;
  if (bookmarkId) {
    bookmarkHistory(bookmarkId);
    return;
  }

  const removeBookmarkId = target.dataset.removeBookmark;
  if (removeBookmarkId) {
    removeBookmark(removeBookmarkId);
  }
});

// 初期表示では検索を実行せず、永続化済みの履歴とブックマークだけを読み込む。
refreshSavedLists();
