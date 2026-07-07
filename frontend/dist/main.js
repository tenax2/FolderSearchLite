const state = {
  activeTab: "search",
  history: [],
  bookmarks: [],
  currentHistoryId: "",
  searching: false,
};

const elements = {
  statusText: document.querySelector("#statusText"),
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
  bookmarkCurrentButton: document.querySelector("#bookmarkCurrentButton"),
  resultSummary: document.querySelector("#resultSummary"),
  scanSummary: document.querySelector("#scanSummary"),
  resultsBody: document.querySelector("#resultsBody"),
  historyList: document.querySelector("#historyList"),
  bookmarkList: document.querySelector("#bookmarkList"),
  clearHistoryButton: document.querySelector("#clearHistoryButton"),
};

function appApi() {
  return window.go?.main?.App;
}

async function callBackend(methodName, ...args) {
  const api = appApi();
  if (!api || typeof api[methodName] !== "function") {
    throw new Error("Wails API が見つかりません");
  }
  return api[methodName](...args);
}

function setStatus(message) {
  elements.statusText.textContent = message;
}

function setSearching(searching) {
  state.searching = searching;
  elements.searchButton.disabled = searching;
  elements.browseButton.disabled = searching;
  elements.searchButton.textContent = searching ? "検索中" : "検索";
}

function parseExtensions(value) {
  return value
    .split(/[,\s;]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

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
    setStatus(error.message);
  }
}

async function runSearch(request) {
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
    renderResults(response);
    await refreshSavedLists();
    elements.bookmarkCurrentButton.disabled = !state.currentHistoryId;
    const limitText = response.limitReached ? " / 上限到達" : "";
    setStatus(`${response.results.length} 件 / ${response.durationMs} ms${limitText}`);
  } catch (error) {
    setStatus(error.message);
  } finally {
    setSearching(false);
  }
}

function renderResults(response) {
  elements.resultSummary.textContent = `${response.results.length.toLocaleString()} 件`;
  elements.scanSummary.textContent = `${response.totalVisited.toLocaleString()} item / ${response.filesScanned.toLocaleString()} files / ${response.directoriesScanned.toLocaleString()} folders`;
  elements.resultsBody.innerHTML = "";

  if (!response.results.length) {
    const row = document.createElement("tr");
    row.innerHTML = `<td colspan="7" class="empty">結果なし</td>`;
    elements.resultsBody.append(row);
    return;
  }

  for (const result of response.results) {
    const row = document.createElement("tr");
    const matches = result.matchedBy.map((item) => `<span class="match-pill">${escapeHtml(matchLabel(item))}</span>`).join("");
    row.innerHTML = `
      <td><span class="kind-pill ${result.kind}">${result.kind === "folder" ? "フォルダ" : "ファイル"}</span></td>
      <td class="name-cell truncate">${escapeHtml(result.name)}</td>
      <td class="path-cell truncate">${escapeHtml(result.relativePath)}${result.preview ? `<div class="preview">${escapeHtml(result.preview)}</div>` : ""}</td>
      <td>${matches}</td>
      <td>${result.kind === "folder" ? "-" : formatBytes(result.size)}</td>
      <td>${formatDate(result.modifiedAt)}</td>
      <td><button class="secondary row-action" type="button" data-copy="${escapeAttribute(result.path)}">コピー</button></td>
    `;
    elements.resultsBody.append(row);
  }
}

function renderHistory() {
  renderSavedList(elements.historyList, state.history, {
    emptyText: "履歴なし",
    showBookmark: true,
  });
}

function renderBookmarks() {
  renderSavedList(elements.bookmarkList, state.bookmarks, {
    emptyText: "ブックマークなし",
    showRemove: true,
  });
}

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

function switchTab(tabName) {
  state.activeTab = tabName;
  for (const button of elements.tabButtons) {
    button.classList.toggle("active", button.dataset.tab === tabName);
  }
  for (const [name, panel] of Object.entries(elements.panels)) {
    panel.classList.toggle("active", name === tabName);
  }
}

function findSavedEntry(id) {
  return [...state.history, ...state.bookmarks].find((entry) => entry.id === id);
}

async function bookmarkCurrent() {
  if (!state.currentHistoryId) {
    return;
  }
  await bookmarkHistory(state.currentHistoryId);
}

async function bookmarkHistory(id) {
  try {
    state.bookmarks = (await callBackend("BookmarkHistory", id)) || [];
    await refreshSavedLists();
    elements.bookmarkCurrentButton.disabled = true;
    setStatus("保存しました");
  } catch (error) {
    setStatus(error.message);
  }
}

async function removeBookmark(id) {
  try {
    state.bookmarks = (await callBackend("RemoveBookmark", id)) || [];
    await refreshSavedLists();
    setStatus("解除しました");
  } catch (error) {
    setStatus(error.message);
  }
}

async function clearHistory() {
  try {
    state.history = (await callBackend("ClearHistory")) || [];
    renderHistory();
    setStatus("履歴をクリアしました");
  } catch (error) {
    setStatus(error.message);
  }
}

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

function matchLabel(value) {
  return {
    extension: "拡張子",
    "file-name": "ファイル名",
    "folder-name": "フォルダ名",
    content: "内容",
  }[value] || value;
}

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function escapeAttribute(value) {
  return escapeHtml(value).replaceAll("`", "&#096;");
}

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

elements.form.addEventListener("submit", (event) => {
  event.preventDefault();
  runSearch(buildRequest());
});

elements.browseButton.addEventListener("click", async () => {
  try {
    const folder = await callBackend("BrowseFolder");
    if (folder) {
      elements.rootPath.value = folder;
      setStatus("フォルダを選択しました");
    }
  } catch (error) {
    setStatus(error.message);
  }
});

elements.bookmarkCurrentButton.addEventListener("click", bookmarkCurrent);
elements.clearHistoryButton.addEventListener("click", clearHistory);

elements.tabButtons.forEach((button) => {
  button.addEventListener("click", () => switchTab(button.dataset.tab));
});

document.addEventListener("click", async (event) => {
  const target = event.target.closest("button");
  if (!target) {
    return;
  }

  const copyPath = target.dataset.copy;
  if (copyPath) {
    try {
      await copyText(copyPath);
      setStatus("コピーしました");
    } catch (error) {
      setStatus(error.message);
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

refreshSavedLists();
