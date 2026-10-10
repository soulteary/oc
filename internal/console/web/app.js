"use strict";

(() => {
  const ids = [
    "rename-dialog", "rename-form", "rename-title", "rename-source", "rename-name", "rename-status", "rename-submit", "close-rename",
    "setting-versioning-option", "preferences-status", "image-preview-dialog", "image-preview-title", "close-image-preview", "image-preview-status", "image-preview", "preview-zoom-out", "preview-zoom-in", "preview-zoom-reset", "preview-zoom-label", "object-info-type", "refresh-object-info", "info-preview", "info-share",
    "nav-favorites", "favorites-page", "favorite-list", "object-info-dialog", "object-info-title", "close-object-info", "object-info-bucket", "object-info-key", "object-info-size", "object-info-modified", "object-info-etag", "object-local-link", "object-copy-status", "copy-object-link",
    "upload-selection", "upload-selected-name", "upload-selected-size", "remove-upload-file",
    "bucket-directory", "bucket-detail-navigation", "directory-create", "directory-account", "bucket-search", "directory-refresh", "directory-status", "directory-body", "directory-count", "back-to-buckets", "bucket-files-tab", "bucket-settings-tab", "bucket-refresh", "upload-file-summary",
    "console-shell", "console-navigation", "toggle-navigation", "nav-overview", "nav-buckets", "nav-tasks", "nav-account", "page-title", "overview-panel", "storage-browser", "task-page", "task-empty", "overview-capacity", "overview-buckets", "overview-tasks", "overview-status", "overview-connection", "recent-buckets", "overview-browse",
    "login-panel", "login-form", "login-code", "login-submit", "login-status",
    "workspace", "session-actions", "alias-name", "refresh", "logout",
    "bucket-list", "bucket-count", "bucket-status", "objects-title", "bucket-created",
    "parent-prefix", "prefix-form", "prefix-input", "prefix-submit", "root-prefix",
    "objects-status", "objects-empty", "empty-title", "empty-description", "objects-retry",
    "objects-table-wrap", "objects-body", "pagination", "object-count", "load-more", "account-status",
    "session-mode", "workspace-footnote", "write-toolbar", "open-upload", "open-exact-delete", "delete-selected", "delete-prefix",
    "selection-count", "selection-header", "select-loaded", "tasks-panel", "tasks-list", "tasks-status", "clear-finished",
    "upload-dialog", "upload-form", "close-upload", "upload-bucket", "upload-file", "upload-key", "upload-overwrite",
    "upload-submit", "upload-status", "replace-dialog", "close-replace", "replace-bucket", "replace-key", "replace-status", "confirm-replace",
    "delete-dialog", "close-delete", "delete-bucket", "delete-scope", "delete-count", "delete-items", "delete-status", "confirm-delete",
    "exact-delete-dialog", "exact-delete-form", "close-exact-delete", "exact-delete-bucket", "exact-delete-key",
    "open-settings", "settings-dialog", "close-settings", "settings-load-form", "settings-bucket", "settings-kind", "load-setting",
    "settings-status", "settings-editor", "settings-scope", "settings-help", "settings-document", "save-setting", "remove-setting",
    "settings-review-dialog", "back-settings", "settings-review-bucket", "settings-review-kind", "settings-review-warning",
    "settings-review-document", "settings-review-status", "confirm-setting",
    "settings-read-dialog", "back-setting-read", "settings-read-current", "settings-read-next", "confirm-setting-read",
    "open-account", "account-dialog", "close-account", "self-account-status", "self-account-summary", "account-buckets",
    "self-secret-help", "self-secret-form", "new-secret", "self-secret-status", "review-secret", "secret-confirmation", "cancel-secret", "confirm-secret",
    "open-create-bucket", "open-delete-bucket", "bucket-dialog", "bucket-action-form", "bucket-action-title", "bucket-action-help", "close-bucket-action", "bucket-action-name", "bucket-confirm-field", "bucket-action-confirm", "bucket-action-status", "submit-bucket-action",
    "open-versions", "versions-dialog", "close-versions", "versions-form", "versions-bucket", "versions-key", "load-versions", "versions-status", "versions-list", "more-versions",
    "share-dialog", "share-form", "close-share", "share-scope", "share-expiry", "share-name", "share-status", "share-result", "share-url", "share-expires", "copy-share", "create-share",
    "read-toolbar", "open-current-archive", "archive-selected", "archive-prefix", "archive-selection-count", "archive-dialog", "close-archive", "archive-scope", "archive-limit-help", "archive-items", "archive-status", "archive-progress", "retry-archive-status", "cancel-archive", "start-archive", "download-archive",
    "iam-binding-controls", "load-iam-bindings", "iam-binding-scope", "iam-binding-status", "iam-binding-read-confirmation", "iam-binding-read-warning", "cancel-iam-bindings-read", "confirm-iam-bindings-read",
    "open-iam", "iam-dialog", "close-iam", "iam-list-form", "iam-kind", "iam-owner-field", "iam-owner", "refresh-iam", "iam-status", "iam-list", "iam-action-form", "iam-action", "iam-action-help", "iam-fields", "iam-confirm", "iam-action-status", "submit-iam-action", "iam-secret-result", "iam-result-access", "iam-result-secret", "reveal-iam-secret", "dismiss-iam-secret",
    "stopped-credential-dialog", "close-stopped-credential", "stopped-result-access", "stopped-result-secret", "reveal-stopped-secret", "dismiss-stopped-secret",
  ];
  const elements = Object.fromEntries(ids.map(id => [id, document.getElementById(id)]));
  const requests = new Map();
  const state = {
    capabilities: {},
    authenticated: false,
    readOnly: true,
    maxUploadSize: 1024 ** 3,
    alias: "",
    csrfToken: "",
    buckets: [],
    bucket: "",
    prefix: "",
    entries: [],
    nextCursor: "",
    account: null,
    accountMessage: "Permission information is loading…",
    epoch: 0,
  };
  let infoObject = null;
  let previewURL = "";
  let previewZoom = 1;
  const imageTypes = { jpg: "image/jpeg", jpeg: "image/jpeg", png: "image/png", gif: "image/gif", webp: "image/webp" };
  function imageType(key) { return imageTypes[key.split(".").pop().toLowerCase()] || ""; }
  function clearPreview() {
    cancelRequest("image-preview");
    elements["image-preview"].hidden = true;
    elements["image-preview"].removeAttribute("src");
    if (previewURL) URL.revokeObjectURL(previewURL);
    previewURL = "";
  }
  function zoomPreview(value) {
    previewZoom = Math.min(4, Math.max(.25, value));
    elements["image-preview"].style.transform = `scale(${previewZoom})`;
    setText("preview-zoom-label", `${Math.round(previewZoom * 100)}%`);
  }
  async function openImagePreview(bucket, entry) {
    closeDialogs(); clearPreview(); zoomPreview(1);
    setText("image-preview-title", entry.key);
    setText("image-preview-status", "正在读取图片，最大支持 20 MiB。");
    elements["image-preview-dialog"].showModal();
    const request = beginRequest("image-preview");
    let reader;
    try {
      const type = imageType(entry.key);
      if (!type || entry.size > 20 * 1024 ** 2) throw new Error("仅支持不超过 20 MiB 的 JPEG、PNG、GIF 和 WebP 图片。");
      const result = await fetch(`/api/download?${new URLSearchParams({ bucket, key: entry.key })}`, { credentials: "same-origin", redirect: "error", signal: request.controller.signal });
      if (!currentRequest("image-preview", request)) return;
      if (!result.ok) {
        if (result.status === 401) { showLogin("Your session has expired."); return; }
        throw new Error("图片读取失败，请确认读取权限及文件是否存在。");
      }
      reader = result.body.getReader();
      const chunks = []; let size = 0;
      while (true) {
        const part = await reader.read();
        if (!currentRequest("image-preview", request)) return;
        if (part.done) break;
        size += part.value.byteLength;
        if (size > 20 * 1024 ** 2) throw new Error("图片超过 20 MiB 预览限制，请使用下载。");
        chunks.push(part.value);
      }
      if (!currentRequest("image-preview", request)) return;
      previewURL = URL.createObjectURL(new Blob(chunks, { type }));
      elements["image-preview"].src = previewURL;
      elements["image-preview"].alt = entry.key;
      elements["image-preview"].hidden = false;
      setText("image-preview-status", "");
    } catch (error) {
      if (currentRequest("image-preview", request) && error.name !== "AbortError") setText("image-preview-status", error.message);
    } finally {
      if (reader) { try { await reader.cancel(); } catch (_) {} }
      finishRequest("image-preview", request);
    }
  }
  async function refreshObjectInfo() {
    if (!infoObject) return;
    const scope = infoObject;
    const request = beginRequest("object-info");
    setText("object-copy-status", "正在读取对象信息…");
    try {
      const info = await api(`/api/object-info?${new URLSearchParams({ bucket: scope.bucket, key: scope.entry.key })}`, {}, request);
      if (!currentRequest("object-info", request) || infoObject !== scope) return;
      if (!info || !Number.isSafeInteger(info.size) || info.size < 0 || typeof info.etag !== "string") throw new Error("对象信息响应无效。");
      setText("object-info-size", formatSize(info.size)); setText("object-info-etag", info.etag || "—");
      setText("object-info-modified", formatDate(info.modified)); setText("object-info-type", info.contentType || "—");
      setText("object-copy-status", "已读取服务端对象信息。");
    } catch (error) {
      if (currentRequest("object-info", request) && !expired(error) && error.name !== "AbortError") setText("object-copy-status", error.message);
    } finally { finishRequest("object-info", request); }
  }
  const favoriteObjects = new Map();
  let bucketsLoaded = false;
  let currentPage = "overview";
  let recentBuckets = [];
  function showPage(page) {
    currentPage = page;
    elements["favorites-page"].hidden = page !== "favorites";
    renderFavorites();
    elements["overview-panel"].hidden = page !== "overview";
    elements["storage-browser"].hidden = page !== "objects";
    elements["bucket-directory"].hidden = page !== "buckets";
    elements["bucket-detail-navigation"].hidden = page !== "objects";
    elements["task-page"].hidden = page !== "tasks";
    setText("page-title", { overview: "概览", buckets: "存储桶列表", tasks: "任务中心", objects: "文件列表", favorites: "收藏路径" }[page]);
    for (const name of ["overview", "buckets", "tasks", "favorites"]) {
      elements[`nav-${name}`].setAttribute("aria-current", (name === page || (name === "buckets" && page === "objects")) ? "page" : "false");
    }
  }
  let preferencesReady = Promise.resolve();
  let preferenceQueue = Promise.resolve();
  let preferenceSequence = 0;
  function applyPreferences(data) {
    if (!data || !Array.isArray(data.favorites) || !Array.isArray(data.recent)) throw new Error("Invalid preferences response");
    favoriteObjects.clear();
    for (const item of data.favorites) if (typeof item.bucket === "string" && typeof item.key === "string") favoriteObjects.set(favoriteID(item.bucket, item.key), item);
    recentBuckets = data.recent.filter(item => typeof item === "string");
    if (window.OCI18n) window.OCI18n.setLanguage(data.language);
    renderFavorites(); renderOverview(); renderEntries();
  }
  async function loadPreferences() {
    const request = beginRequest("preferences-load");
    try {
      const data = await api("/api/preferences", {}, request);
      if (currentRequest("preferences-load", request)) applyPreferences(data);
    } catch (error) {
      if (currentRequest("preferences-load", request) && error.name !== "AbortError") setText("preferences-status", "Unable to load saved preferences. Refresh to retry.");
    } finally { finishRequest("preferences-load", request); }
  }
  function savePreference(operation) {
    const sequence = ++preferenceSequence;
    const ready = preferencesReady;
    const epoch = state.epoch;
    preferenceQueue = preferenceQueue.catch(() => {}).then(async () => {
      await ready;
      if (!state.authenticated || state.epoch !== epoch) return;
      const request = beginRequest("preferences-save");
      try {
        const data = await api("/api/preferences", { method: "PUT", headers: { "Content-Type": "application/json", "X-CSRF-Token": state.csrfToken }, body: JSON.stringify(operation) }, request);
        if (currentRequest("preferences-save", request)) {
          if (sequence === preferenceSequence) applyPreferences(data);
          setText("preferences-status", "");
        }
      } catch (error) {
        if (currentRequest("preferences-save", request) && error.name !== "AbortError") {
          if (expired(error)) return;
          setText("preferences-status", "Unable to confirm preferences were saved. Refresh to check.");
        }
      } finally { finishRequest("preferences-save", request); }
    });
  }
  function favoriteID(bucket, key) { return JSON.stringify([bucket, key]); }
  function renderFavorites() {
    elements["favorite-list"].replaceChildren();
    for (const item of favoriteObjects.values()) {
      const row = document.createElement("div"); row.className = "favorite-row";
      row.append(actionButton(`${item.bucket} / ${item.key}`, () => {
        showPage("objects");
        const slash = item.key.lastIndexOf("/");
        selectLocation(item.bucket, slash < 0 ? "" : item.key.slice(0, slash + 1));
      }, `打开收藏 ${item.key}`));
      row.append(actionButton("移除", () => { favoriteObjects.delete(favoriteID(item.bucket, item.key)); savePreference({ action: "favorite-remove", bucket: item.bucket, key: item.key }); renderFavorites(); renderEntries(); }, `移除收藏 ${item.key}`));
      elements["favorite-list"].append(row);
    }
    if (!favoriteObjects.size) elements["favorite-list"].textContent = "尚未收藏对象。可通过文件名旁的星标添加。";
  }
  function openObjectInfo(bucket, entry) {
    closeDialogs();
    infoObject = { bucket, entry };
    elements["info-preview"].hidden = !imageType(entry.key);
    elements["info-share"].hidden = !featureEnabled("sharing");
    setText("object-info-type", "—");
    setText("object-info-bucket", bucket);
    setText("object-info-key", entry.key);
    setText("object-info-size", formatSize(entry.size));
    setText("object-info-modified", formatDate(entry.modified));
    setText("object-info-etag", entry.etag || "—");
    elements["object-local-link"].value = new URL(`/api/download?${new URLSearchParams({ bucket, key: entry.key })}`, window.location.origin).href;
    setText("object-copy-status", "");
    elements["object-info-dialog"].showModal();
    elements["close-object-info"].focus();
  }
  let renameTarget = null;
  function openRename(bucket, entry) {
    if (state.readOnly || !featureEnabled("rename")) return;
    closeDialogs();
    renameTarget = { bucket, key: entry.key, etag: entry.etag };
    setText("rename-source", entry.key);
    elements["rename-name"].value = entry.key.slice(entry.key.lastIndexOf("/") + 1);
    setText("rename-status", "");
    elements["rename-submit"].disabled = false;
    elements["rename-dialog"].showModal();
    elements["rename-name"].focus();
  }
  elements["close-rename"].addEventListener("click", () => { if (!requests.has("rename")) elements["rename-dialog"].close(); });
  elements["rename-form"].addEventListener("submit", async event => {
    event.preventDefault();
    if (!renameTarget || state.readOnly || !featureEnabled("rename") || requests.has("rename")) return;
    const target = { ...renameTarget };
    const name = elements["rename-name"].value;
    if (!name || name.includes("/") || name.includes("\\")) { setText("rename-status", "请输入不含路径分隔符的文件名。"); return; }
    const newKey = target.key.slice(0, target.key.lastIndexOf("/") + 1) + name;
    if (newKey === target.key) { setText("rename-status", "请输入不同的文件名。"); return; }
    const request = beginRequest("rename");
    elements["rename-submit"].disabled = true;
    setText("rename-status", "正在重命名…");
    try {
      await api("/api/objects/rename", mutationOptions({ ...target, newKey }), request);
      if (!currentRequest("rename", request)) return;
      elements["rename-dialog"].close();
      selectedKeys.delete(target.key);
      if (state.bucket === target.bucket) loadObjects(false);
    } catch (error) {
      if (currentRequest("rename", request) && !expired(error) && error.name !== "AbortError") {
        setText("rename-status", error.message);
        if (["rename_partial", "outcome_unknown"].includes(error.code)) renameTarget = null;
      }
    } finally { finishRequest("rename", request); elements["rename-submit"].disabled = !renameTarget; }
  });

  async function copyObjectLink() {
    const epoch = state.epoch;
    const value = elements["object-local-link"].value;
    if (!state.authenticated || !value) return;
    try {
      if (typeof navigator === "undefined" || !navigator.clipboard?.writeText) throw new Error("Clipboard unavailable");
      await navigator.clipboard.writeText(value);
      if (state.epoch === epoch && elements["object-local-link"].value === value) setText("object-copy-status", "已复制本机会话下载链接。");
    } catch (_) {
      if (state.epoch !== epoch || elements["object-local-link"].value !== value) return;
      elements["object-local-link"].focus(); elements["object-local-link"].select();
      setText("object-copy-status", "请复制上方已选中的链接。");
    }
  }
  function renderDirectory() {
    elements["directory-create"].hidden = elements["open-create-bucket"].hidden;
    setText("directory-status", elements["bucket-status"].textContent);
    elements["directory-body"].replaceChildren();
    const query = elements["bucket-search"].value.toLowerCase();
    const buckets = state.buckets.filter(bucket => bucket.name.toLowerCase().includes(query));
    for (const bucket of buckets) {
      const row = document.createElement("tr");
      const name = document.createElement("td");
      const link = document.createElement("button");
      link.className = "row-action";
      link.type = "button";
      link.setAttribute("data-no-i18n", ""); link.textContent = bucket.name;
      link.addEventListener("click", () => { showPage("objects"); selectLocation(bucket.name, ""); });
      name.append(link);
      const account = state.account?.buckets.find(item => item.name === bucket.name);
      row.append(name);
      for (const value of [account ? `${account.read ? "可读" : "未标明读取"} · ${account.write ? "可写" : "未标明写入"}` : "摘要不可用", account ? formatSize(account.size) : "—", formatDate(bucket.created)]) {
        const cell = document.createElement("td"); cell.textContent = value; row.append(cell);
      }
      const actions = document.createElement("td");
      const settings = document.createElement("button"); settings.type = "button"; settings.className = "row-action"; settings.textContent = "配置管理";
      settings.addEventListener("click", () => { showPage("objects"); selectLocation(bucket.name, ""); openSettings(); });
      actions.append(settings); row.append(actions); elements["directory-body"].append(row);
    }
    setText("directory-count", `显示 ${buckets.length} 个桶 / 共 ${state.buckets.length} 个${!buckets.length ? " · 未找到匹配的存储桶" : ""}`);
  }
  function renderOverview() {
    renderDirectory();
    setText("overview-buckets", bucketsLoaded ? String(state.buckets.length) : "—");
    const sizes = state.account?.buckets.map(bucket => bucket.size);
    const total = sizes?.reduce((sum, size) => sum + size, 0);
    const valid = sizes && sizes.every(size => Number.isSafeInteger(size) && size >= 0) && Number.isSafeInteger(total);
    setText("overview-capacity", valid ? formatSize(total) : "—");
    setText("overview-status", state.account ? (valid ? "容量仅覆盖账户摘要返回的桶，统计刷新周期由服务端决定。" : "服务端容量摘要不可用。") : state.accountMessage);
    setText("overview-tasks", String(tasks.size));
    setText("overview-connection", `${state.alias} · ${state.readOnly ? "只读模式" : "已开启写操作"}`);
    elements["task-empty"].hidden = tasks.size > 0 || Boolean(jobsMessage);
    elements["recent-buckets"].replaceChildren();
    for (const bucket of recentBuckets) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "recent-bucket";
      button.setAttribute("data-no-i18n", ""); button.textContent = bucket;
      button.addEventListener("click", () => { showPage("objects"); selectLocation(bucket, ""); });
      elements["recent-buckets"].append(button);
    }
    if (!recentBuckets.length) elements["recent-buckets"].textContent = "浏览存储桶后显示访问记录。";
  }
  const selectedKeys = new Set();
  const tasks = new Map();
  const clearedTasks = new Set();
  const terminalStates = new Set(["succeeded", "failed", "partial", "canceled"]);
  const jobStates = new Set(["waiting", "planning", "ready", "running", ...terminalStates]);
  let pollTimer = null;
  let uploadDraft = null;
  let uploadLocation = null;
  let suggestedUploadKey = "";
  let deletePlan = null;
  let jobsMessage = "";
  let pageSuspended = false;
  let loadedSetting = null;
  let settingDraft = null;
  let pendingSettingRead = null;
  let selfAccount = null;
  let consoleStopped = false;
  let rotationInFlight = false;
  let bucketAction = "create";
  let versionLocation = null;
  let versionEntries = [];
  let versionCursor = "";
  let shareRef = null;
  let archiveDraft = null;
  let archiveJob = null;
  let archiveTimer = null;
  let iamInputs = {};
  let iamRecord = null;
  let iamGroups = [];
  let iamBinding = null;
  let iamBindingNeedsRead = false;
  let pendingBindingRead = null;
  const settingLabels = { policy: "Bucket policy", versioning: "Versioning", lifecycle: "Lifecycle" };
  function uiLocale() { return window.OCI18n?.language() === "en" ? "en-US" : "zh-CN"; }

  class APIError extends Error {
    constructor(status, code, message, restartRequired) {
      super(message);
      this.status = status;
      this.code = code;
      if (typeof restartRequired === "boolean") this.restartRequired = restartRequired;
    }
  }

  function setText(id, value) {
    elements[id].textContent = value;
  }

  function updateBusy() {
    elements["login-submit"].disabled = consoleStopped || requests.has("login") || requests.has("session");
    elements["login-code"].disabled = consoleStopped;
    // Refresh is also the way to stop and restart a slow read request.
    elements.refresh.disabled = !state.authenticated || requests.has("logout");
    elements.logout.disabled = requests.has("logout");
    elements["load-more"].disabled = requests.has("objects");
    setText("load-more", requests.has("objects") ? "Loading…" : "Load more");
    const preparing = requests.has("upload-prepare");
    elements["upload-submit"].disabled = preparing;
    elements["confirm-replace"].disabled = preparing;
    elements["confirm-delete"].disabled = !deletePlan || !deletePlan.confirmToken || deletePlan.job.status !== "ready" || requests.has("delete-execute");
    elements["open-settings"].disabled = !state.authenticated;
    elements["open-account"].disabled = !state.authenticated;
    const settingBusy = requests.has("setting-read") || requests.has("setting-write");
    elements["load-setting"].disabled = !state.authenticated || settingBusy;
    elements["settings-bucket"].disabled = settingBusy;
    elements["settings-kind"].disabled = settingBusy;
    elements["settings-document"].readOnly = !canWriteSetting() || settingBusy;
    elements["save-setting"].disabled = !canWriteSetting() || settingBusy;
    elements["remove-setting"].disabled = !canWriteSetting() || !loadedSetting?.exists || loadedSetting?.kind === "versioning" || settingBusy;
    elements["confirm-setting"].disabled = !settingDraft || !canWriteSetting() || settingBusy;
    elements["confirm-setting-read"].disabled = !pendingSettingRead || settingBusy;
    const secretBusy = requests.has("secret-write");
    elements["new-secret"].disabled = secretBusy || !elements["secret-confirmation"].hidden;
    elements["review-secret"].disabled = !canRotateSecret() || secretBusy;
    elements["confirm-secret"].disabled = !canRotateSecret() || secretBusy;
    elements["cancel-secret"].disabled = secretBusy;
    elements["close-account"].disabled = secretBusy;
    updateSelection();
    updateFeatures();
  }

  function cancelRequest(name) {
    const request = requests.get(name);
    if (request) {
      request.controller.abort();
      requests.delete(name);
    }
  }

  function beginRequest(name) {
    cancelRequest(name);
    const request = { controller: new AbortController(), epoch: state.epoch };
    requests.set(name, request);
    updateBusy();
    return request;
  }

  function currentRequest(name, request) {
    return requests.get(name) === request && !request.controller.signal.aborted && request.epoch === state.epoch;
  }

  function finishRequest(name, request) {
    if (requests.get(name) === request) {
      requests.delete(name);
      updateBusy();
    }
  }

  function abortAll() {
    clearTimeout(pollTimer);
    clearTimeout(archiveTimer);
    archiveTimer = null;
    pollTimer = null;
    for (const request of requests.values()) request.controller.abort();
    requests.clear();
    for (const task of tasks.values()) if (task.xhr) task.xhr.abort();
    updateBusy();
  }

  async function api(path, options, request) {
    let response;
    try {
      response = await fetch(path, {
        credentials: "same-origin",
        cache: "no-store",
        ...options,
        redirect: "error",
        signal: request.controller.signal,
      });
    } catch (error) {
      if (error.name === "AbortError") throw error;
      throw new APIError(0, "NetworkError", "Unable to reach the local OC console. Check that OC is still running.");
    }
    if (response.status === 204) return null;
    let data;
    try {
      data = await response.json();
    } catch (error) {
      if (error.name === "AbortError") throw error;
      throw new APIError(response.status, "InvalidResponse", "The local console returned an unexpected response. Try refreshing.");
    }
    if (!response.ok) {
      throw new APIError(response.status, data?.code || "RequestFailed", data?.message || "The request could not be completed.", data?.restartRequired);
    }
    return data;
  }

  function showLogin(message = "") {
    if (rotationInFlight) {
      consoleStopped = true;
      rotationInFlight = false;
      message = "The secret rotation response was interrupted. Verify which secret works in your terminal, update the alias, and restart OC console. This request will not be repeated.";
    }
    const interrupted = [...tasks.values()].some(task => !task.expired && !terminalStates.has(task.job.status));
    state.epoch += 1;
    abortAll();
    Object.assign(state, {
      capabilities: {}, authenticated: false, readOnly: true, alias: "", csrfToken: "", buckets: [], bucket: "", prefix: "",
      entries: [], nextCursor: "", account: null, accountMessage: "Permission information is loading…",
    });
    infoObject = null;
    clearPreview();
    favoriteObjects.clear();
    renderFavorites();
    elements["object-local-link"].value = "";
    for (const id of ["object-info-bucket", "object-info-key", "object-info-size", "object-info-modified", "object-info-etag", "object-copy-status"]) setText(id, "");
    bucketsLoaded = false;
    elements["bucket-search"].value = "";
    recentBuckets = [];
    elements["console-navigation"].hidden = true;
    elements["toggle-navigation"].hidden = true;
    elements["console-shell"].classList.remove("connected");
    selectedKeys.clear();
    tasks.clear();
    clearedTasks.clear();
    uploadDraft = null;
    uploadLocation = null;
    deletePlan = null;
    loadedSetting = null;
    settingDraft = null;
    pendingSettingRead = null;
    selfAccount = null;
    jobsMessage = "";
    closeDialogs();
    clearArchive();
    versionEntries = [];
    versionLocation = null;
    versionCursor = "";
    elements["versions-list"].replaceChildren();
    elements["versions-key"].value = "";
    elements["versions-bucket"].value = "";
    elements["upload-form"].reset();
    renderUploadSelection();
    clearSecret();
    elements["settings-document"].value = "";
    elements["settings-bucket"].value = "";
    for (const id of ["settings-review-document", "settings-read-current", "settings-read-next", "settings-scope", "settings-status", "settings-help", "self-account-summary", "self-account-status", "self-secret-help", "self-secret-status"]) setText(id, "");
    elements["account-buckets"].replaceChildren();
    elements["settings-editor"].hidden = true;
    renderTasks();
    renderMode();
    elements["workspace"].hidden = true;
    elements["session-actions"].hidden = true;
    elements["login-panel"].hidden = false;
    elements["login-code"].value = "";
    elements["prefix-input"].value = "";
    elements["bucket-list"].replaceChildren();
    elements["objects-body"].replaceChildren();
    setText("alias-name", "");
    setText("login-status", `${message}${interrupted ? " An active task was interrupted. Completed writes are not rolled back; check the affected objects before retrying." : ""}`.trim());
    updateBusy();
    if (consoleStopped) elements["login-status"].focus();
  }

  function expired(error) {
    if (error.status !== 401 && !(error.status === 403 && error.code === "invalid_csrf")) return false;
    showLogin(error.code === "invalid_csrf" ? "Your local session has changed. Enter the login code from your OC terminal to reconnect." : "Your session has expired. Enter the login code from your OC terminal to reconnect.");
    elements["login-code"].focus();
    return true;
  }

  function acceptSession(session) {
    if (!session || typeof session.alias !== "string" || typeof session.csrfToken !== "string" || typeof session.readOnly !== "boolean") {
      throw new APIError(0, "InvalidSession", "The local console returned an invalid session.");
    }
    state.epoch += 1;
    abortAll();
    state.capabilities = {};
    state.authenticated = true;
    state.readOnly = session.readOnly;
    state.maxUploadSize = Number.isSafeInteger(session.maxUploadSize) && session.maxUploadSize > 0 ? session.maxUploadSize : 1024 ** 3;
    state.alias = session.alias;
    state.csrfToken = session.csrfToken;
    elements["login-code"].value = "";
    elements["login-panel"].hidden = true;
    elements["workspace"].hidden = false;
    elements["session-actions"].hidden = false;
    setText("alias-name", state.alias);
    setText("login-status", "");
    elements["console-navigation"].hidden = false;
    elements["toggle-navigation"].hidden = false;
    elements["console-shell"].classList.add("connected");
    showPage("overview");
    renderMode();
    renderLocation();
    preferencesReady = loadPreferences();
    refreshAll();
  }

  function renderMode() {
    setText("session-mode", state.readOnly ? "● Read-only" : "● Writes enabled");
    elements["session-mode"].classList.toggle("writes-enabled", !state.readOnly);
    elements["write-toolbar"].hidden = state.readOnly;
    elements["selection-header"].hidden = !canSelectObjects();
    elements["objects-body"].closest("table").classList.toggle("writable", canSelectObjects());
    updateFeatures();
    setText("workspace-footnote", `${state.readOnly ? "Read-only browsing." : "Writes enabled by the local OC process."} Downloads use your browser’s download manager. Download errors open in a separate tab.`);
  }

  function formatSize(size) {
    if (!Number.isFinite(size) || size < 0) return "—";
    if (size === 0) return "0 B";
    const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"];
    const unit = Math.min(Math.floor(Math.log(size) / Math.log(1024)), units.length - 1);
    return `${new Intl.NumberFormat(uiLocale(), { maximumFractionDigits: unit === 0 ? 0 : 1 }).format(size / (1024 ** unit))} ${units[unit]}`;
  }

  function formatDate(value) {
    const date = new Date(value);
    if (!value || !Number.isFinite(date.getTime()) || date.getUTCFullYear() < 1970) return "—";
    return new Intl.DateTimeFormat(uiLocale(), { year: "numeric", month: "short", day: "numeric" }).format(date);
  }

  function renderBuckets() {
    renderOverview();
    elements["bucket-list"].replaceChildren();
    setText("bucket-count", String(state.buckets.length));
    for (const bucket of state.buckets) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "bucket-button";
      button.setAttribute("aria-current", bucket.name === state.bucket ? "true" : "false");
      const symbol = document.createElement("span");
      symbol.className = "bucket-symbol";
      symbol.setAttribute("aria-hidden", "true");
      symbol.textContent = "▤";
      const label = document.createElement("span");
      label.className = "bucket-name";
      label.setAttribute("data-no-i18n", ""); label.textContent = bucket.name;
      button.append(symbol, label);
      button.addEventListener("click", () => selectLocation(bucket.name, ""));
      elements["bucket-list"].append(button);
    }
  }

  function parentPrefix(prefix) {
    const withoutTrailingSlash = prefix.endsWith("/") ? prefix.slice(0, -1) : prefix;
    const slash = withoutTrailingSlash.lastIndexOf("/");
    return slash < 0 ? "" : withoutTrailingSlash.slice(0, slash + 1);
  }

  function renderLocation() {
    const bucket = state.buckets.find(item => item.name === state.bucket);
    if (state.bucket) elements["objects-title"].setAttribute("data-no-i18n", ""); else elements["objects-title"].removeAttribute("data-no-i18n");
    setText("objects-title", state.bucket || "Choose a bucket");
    const created = bucket ? formatDate(bucket.created) : "—";
    setText("bucket-created", created === "—" ? "" : `Created ${created}`);
    elements["prefix-input"].value = state.prefix;
    elements["prefix-input"].disabled = !state.bucket;
    elements["prefix-submit"].disabled = !state.bucket;
    elements["parent-prefix"].disabled = !state.bucket || state.prefix === "";
    elements["root-prefix"].disabled = !state.bucket || state.prefix === "";
    renderAccount();
    updateSelection();
  }

  function renderAccount() {
    renderOverview();
    renderAccountBuckets();
    if (!state.bucket) {
      setText("account-status", "Select a bucket to view available permission information.");
      return;
    }
    if (!state.account) {
      setText("account-status", state.accountMessage);
      return;
    }
    const bucket = state.account.buckets.find(item => item.name === state.bucket);
    if (!bucket) {
      setText("account-status", "No permission summary is available for this bucket. Browsing remains available.");
      return;
    }
    const read = bucket.read ? "Read indicated" : "read not indicated";
    const write = bucket.write ? "write indicated" : "write not indicated";
    setText("account-status", `${read} · ${write} · ${formatSize(bucket.size)}. This bucket summary does not determine access to individual objects.`);
  }

  function showEmpty(title, description, retry = false) {
    setText("empty-title", title);
    setText("empty-description", description);
    elements["objects-empty"].hidden = false;
    elements["objects-retry"].hidden = !retry;
    elements["objects-table-wrap"].hidden = true;
    elements["pagination"].hidden = true;
  }

  function downloadURL(key) {
    const parameters = new URLSearchParams({ bucket: state.bucket, key });
    return `/api/download?${parameters.toString()}`;
  }

  function renderEntries() {
    elements["objects-body"].replaceChildren();
    if (state.entries.length === 0) {
      showEmpty("No objects here", state.prefix ? "This prefix is empty. Try another prefix or return to the root." : "This bucket is empty.");
      // A provider may return an empty page with a continuation cursor.
      elements["pagination"].hidden = !state.nextCursor;
    } else {
      elements["objects-empty"].hidden = true;
      elements["objects-table-wrap"].hidden = false;
      elements["pagination"].hidden = false;
    }
    for (const entry of state.entries) {
      const row = document.createElement("tr");
      if (canSelectObjects()) {
        const selection = document.createElement("td");
        selection.className = "selection-cell";
        if (!entry.isPrefix) {
          const checkbox = document.createElement("input");
          checkbox.type = "checkbox";
          checkbox.checked = selectedKeys.has(entry.key);
          checkbox.setAttribute("aria-label", `Select ${entry.key}`);
          checkbox.addEventListener("change", () => {
            if (checkbox.checked) selectedKeys.add(entry.key);
            else selectedKeys.delete(entry.key);
            updateSelection();
          });
          selection.append(checkbox);
        }
        row.append(selection);
      }
      const nameCell = document.createElement("td");
      nameCell.className = "file-name-cell";
      const name = document.createElement(entry.isPrefix ? "button" : "span");
      name.className = entry.isPrefix ? "entry-name prefix-button" : "entry-name";
      const symbol = document.createElement("span");
      symbol.className = "entry-symbol";
      symbol.setAttribute("aria-hidden", "true");
      symbol.textContent = entry.isPrefix ? "▱" : "·";
      const label = document.createElement("span");
      label.className = "entry-label";
      label.setAttribute("title", entry.key);
      label.setAttribute("data-no-i18n", ""); label.textContent = entry.key.startsWith(state.prefix) ? entry.key.slice(state.prefix.length) || entry.key : entry.key;
      name.append(symbol, label);
      if (entry.isPrefix) {
        name.type = "button";
        name.addEventListener("click", () => selectLocation(state.bucket, entry.key));
      }
      nameCell.append(name);
      if (!entry.isPrefix) {
        const bucket = state.bucket;
        const shortcuts = document.createElement("span"); shortcuts.className = "file-shortcuts";
        const copy = actionButton("⧉", () => { openObjectInfo(bucket, entry); copyObjectLink(); }, `复制文件链接 ${entry.key}`);
        copy.setAttribute("title", "复制本机会话下载链接");
        const favorite = actionButton(favoriteObjects.has(favoriteID(bucket, entry.key)) ? "★" : "☆", () => {
          const id = favoriteID(bucket, entry.key);
          if (favoriteObjects.has(id)) favoriteObjects.delete(id); else favoriteObjects.set(id, { bucket, key: entry.key });
          const saved = favoriteObjects.has(id);
          savePreference({ action: saved ? "favorite-add" : "favorite-remove", bucket, key: entry.key });
          favorite.textContent = saved ? "★" : "☆"; favorite.setAttribute("aria-pressed", String(saved)); renderFavorites();
        }, `收藏 ${entry.key}`);
        favorite.setAttribute("title", "收藏 / 取消收藏"); favorite.setAttribute("aria-pressed", String(favoriteObjects.has(favoriteID(bucket, entry.key))));
        const rename = actionButton("✎", () => openRename(bucket, entry), `重命名 ${entry.key}`);
        rename.setAttribute("title", "重命名对象");
        rename.disabled = state.readOnly || !featureEnabled("rename");
        shortcuts.append(rename, copy, favorite); nameCell.append(shortcuts);
      }
      const size = document.createElement("td");
      size.textContent = entry.isPrefix ? "—" : formatSize(entry.size);
      const modified = document.createElement("td");
      modified.className = "modified-cell";
      modified.textContent = entry.isPrefix ? "—" : formatDate(entry.modified);
      const action = document.createElement("td");
      action.className = "row-actions-cell";
      if (!entry.isPrefix) {
        const actions = document.createElement("div");
        actions.className = "row-actions";
        const download = document.createElement("a");
        download.className = "download-link";
        download.href = downloadURL(entry.key);
        download.textContent = "下载";
        download.setAttribute("aria-label", `Download ${entry.key}`);
        download.target = "_blank";
        download.rel = "noopener";
        const bucket = state.bucket;
        download.addEventListener("click", event => prepareDownload(event, download, bucket, entry.key));
        if (imageType(entry.key)) actions.append(actionButton("预览", () => openImagePreview(bucket, entry), `预览图片 ${entry.key}`));
        actions.append(actionButton("详情", () => openObjectInfo(bucket, entry), `文件详情 ${entry.key}`), download);
        const more = document.createElement("details"); more.className = "file-more";
        const summary = document.createElement("summary"); summary.textContent = "更多";
        const menu = document.createElement("div"); menu.className = "file-more-menu";
        more.append(summary, menu);
        more.addEventListener("toggle", () => {
          if (!more.open) return;
          const rect = summary.getBoundingClientRect();
          menu.style.left = `${Math.max(8, Math.min(rect.right - 176, window.innerWidth - 184))}px`;
          menu.style.top = `${rect.bottom + 6}px`;
          const height = menu.getBoundingClientRect().height;
          if (rect.bottom + 6 + height > window.innerHeight) menu.style.top = `${Math.max(8, rect.top - height - 6)}px`;
        });
        more.addEventListener("keydown", event => { if (event.key === "Escape") { more.open = false; summary.focus(); } });
        menu.addEventListener("click", () => { more.open = false; });

        actions.append(more);
        if (featureEnabled("versions")) menu.append(actionButton("历史版本", () => openVersions(bucket, entry.key), `Read versions of ${entry.key}`));
        if (featureEnabled("sharing")) menu.append(actionButton("分享链接", () => openShare({ bucket, key: entry.key }), `Share ${entry.key}`));
        if (!state.readOnly) {
          const remove = document.createElement("button");
          remove.type = "button";
          remove.className = "row-delete";
          remove.textContent = "删除";
          remove.setAttribute("aria-label", `Review deletion of ${entry.key}`);
          remove.addEventListener("click", () => prepareDeletion({ bucket, keys: [entry.key] }));
          if (menu.children.length) menu.append(remove);
          else { remove.className = "row-action row-delete"; actions.append(remove); }
        }
        if (!menu.children.length) more.hidden = true;
        action.append(actions);
      }
      row.append(nameCell, size, modified, action);
      elements["objects-body"].append(row);
    }
    setText("object-count", `${state.entries.length} ${state.entries.length === 1 ? "entry" : "entries"} loaded`);
    elements["load-more"].hidden = !state.nextCursor;
    updateBusy();
  }

  function updateSelection() {
    const writable = state.authenticated && !state.readOnly;
    elements["open-upload"].disabled = !writable;
    elements["open-exact-delete"].disabled = !writable || requests.has("delete-plan");
    elements["delete-selected"].disabled = !writable || selectedKeys.size === 0 || requests.has("delete-plan");
    elements["delete-prefix"].disabled = !writable || !state.prefix.endsWith("/") || requests.has("delete-plan");
    setText("selection-count", selectedKeys.size ? `${selectedKeys.size} loaded ${selectedKeys.size === 1 ? "object" : "objects"} selected` : "No objects selected");
    const keys = state.entries.filter(entry => !entry.isPrefix).map(entry => entry.key);
    elements["select-loaded"].disabled = !canSelectObjects() || keys.length === 0;
    elements["select-loaded"].checked = keys.length > 0 && keys.every(key => selectedKeys.has(key));
    elements["select-loaded"].indeterminate = selectedKeys.size > 0 && !elements["select-loaded"].checked;
    updateFeatures();
  }

  function selectLocation(bucket, prefix) {
    if (!state.authenticated) return;
    cancelRequest("objects");
    state.bucket = bucket;
    state.prefix = prefix;
    if (currentPage === "objects") { recentBuckets = [bucket, ...recentBuckets.filter(name => name !== bucket)].slice(0, 20); savePreference({ action: "visit", bucket }); }
    state.capabilities.versions = false; state.capabilities.versioning = false;
    loadCapabilities(bucket);
    state.entries = [];
    state.nextCursor = "";
    selectedKeys.clear();
    renderBuckets();
    renderLocation();
    loadObjects(false);
  }

  async function loadObjects(append) {
    if (!state.authenticated || !state.bucket) return;
    const bucket = state.bucket;
    const prefix = state.prefix;
    const cursor = append ? state.nextCursor : "";
    if (append && !cursor) return;
    const request = beginRequest("objects");
    elements["objects-status"].classList.remove("error");
    setText("objects-status", append ? "Loading more objects…" : "Loading objects…");
    elements["objects-retry"].hidden = true;
    if (!append) {
      selectedKeys.clear();
      updateSelection();
      state.entries = [];
      state.nextCursor = "";
      elements["objects-body"].replaceChildren();
      showEmpty("Loading objects…", "You can choose another bucket or prefix while this request is running.");
    }
    const parameters = new URLSearchParams({ bucket, prefix, limit: "100" });
    if (cursor) parameters.set("cursor", cursor);
    try {
      const page = await api(`/api/objects?${parameters.toString()}`, {}, request);
      if (!currentRequest("objects", request) || state.bucket !== bucket || state.prefix !== prefix) return;
      if (!page || !Array.isArray(page.entries) || page.entries.some(entry => !entry || typeof entry.key !== "string")) {
        throw new APIError(0, "InvalidPage", "The local console returned an invalid object page.");
      }
      const combined = append ? state.entries.concat(page.entries) : page.entries;
      state.entries = [...new Map(combined.map(entry => [`${entry.isPrefix ? "prefix" : "object"}:${entry.key}`, entry])).values()];
      state.nextCursor = typeof page.nextCursor === "string" ? page.nextCursor : "";
      if (state.nextCursor && state.nextCursor === cursor) {
        state.nextCursor = "";
        setText("objects-status", "The server returned a repeated page cursor. Refresh to retry browsing.");
      } else {
        setText("objects-status", "");
      }
      renderEntries();
    } catch (error) {
      if (!currentRequest("objects", request) || error.name === "AbortError") return;
      if (expired(error)) return;
      elements["objects-status"].classList.add("error");
      setText("objects-status", error.message);
      if (!append) {
        showEmpty(error.status === 403 ? "Access restricted" : "Objects unavailable", error.status === 403 ? "You do not have permission to list this location. Try a permitted prefix." : "Check the local OC process and try again.", true);
      }
    } finally {
      finishRequest("objects", request);
    }
  }

  async function loadBuckets() {
    bucketsLoaded = false;
    renderOverview();
    const request = beginRequest("buckets");
    setText("bucket-status", "Loading buckets…");
    try {
      const result = await api("/api/buckets", {}, request);
      if (!currentRequest("buckets", request)) return;
      if (!result || !Array.isArray(result.buckets) || result.buckets.some(bucket => !bucket || typeof bucket.name !== "string")) {
        throw new APIError(0, "InvalidBuckets", "The local console returned an invalid bucket list.");
      }
      bucketsLoaded = true;
      state.buckets = result.buckets;
      setText("bucket-status", state.buckets.length ? "" : "No buckets are available to this identity.");
      const selected = state.buckets.find(bucket => bucket.name === state.bucket);
      const nextBucket = selected ? selected.name : state.buckets[0]?.name || "";
      const nextPrefix = selected ? state.prefix : "";
      if (nextBucket) {
        selectLocation(nextBucket, nextPrefix);
      } else {
        cancelRequest("objects");
        state.bucket = "";
        state.prefix = "";
        state.entries = [];
        state.nextCursor = "";
        selectedKeys.clear();
        renderBuckets();
        renderLocation();
        setText("objects-status", "");
        showEmpty("No buckets available", "No buckets were returned for the selected alias and identity.");
      }
    } catch (error) {
      if (!currentRequest("buckets", request) || error.name === "AbortError") return;
      if (expired(error)) return;
      setText("bucket-status", error.message);
      if (state.bucket) {
        loadObjects(false);
      } else {
        showEmpty(error.status === 403 ? "Access restricted" : "Buckets unavailable", "Use Refresh to retry the bucket list. OC uses the identity configured for this alias.");
      }
    } finally {
      if (currentRequest("buckets", request)) renderDirectory();
      finishRequest("buckets", request);
    }
  }

  async function loadAccount() {
    const request = beginRequest("account");
    state.account = null;
    state.accountMessage = "Permission information is loading…";
    renderAccount();
    try {
      const account = await api("/api/account", {}, request);
      if (!currentRequest("account", request)) return;
      if (!account || !Array.isArray(account.buckets)) throw new APIError(0, "InvalidAccount", "Permission summary unavailable.");
      state.account = account;
    } catch (error) {
      if (!currentRequest("account", request) || error.name === "AbortError") return;
      if (expired(error)) return;
      state.accountMessage = "Permission summary unavailable. Bucket browsing and downloads remain available.";
    } finally {
      if (currentRequest("account", request)) renderAccount();
      finishRequest("account", request);
    }
  }

  function refreshAll() {
    if (!state.authenticated) return;
    cancelRequest("objects");
    loadBuckets();
    loadAccount();
    loadCapabilities();
    if (!state.readOnly) loadJobs();
  }

  function mutationOptions(body) {
    return { method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": state.csrfToken }, body: JSON.stringify(body) };
  }

  function closeDialogs() {
    clearPreview();
    cancelRequest("object-info");
    clearSecret();
    clearShare();
    clearIAM();
    clearStoppedCredential();
    cancelRequest("versions");
    for (const id of ["rename-dialog", "image-preview-dialog", "object-info-dialog", "upload-dialog", "replace-dialog", "delete-dialog", "exact-delete-dialog", "settings-dialog", "settings-read-dialog", "settings-review-dialog", "account-dialog", "bucket-dialog", "versions-dialog", "share-dialog", "archive-dialog", "iam-dialog", "stopped-credential-dialog"]) {
      if (elements[id].open) elements[id].close();
    }
  }

  let downloadSequence = 0;
  async function prepareDownload(event, link, bucket, key, versionId, downloadPath) {
    event.preventDefault();
    if (!state.authenticated) return;
    // Open during the user gesture; waiting for fetch first would trigger popup blockers.
    const downloadWindow = window.open("about:blank", "_blank");
    if (downloadWindow) downloadWindow.opener = null;
    const name = `download-${++downloadSequence}`;
    const request = beginRequest(name);
    try {
      const session = await api("/api/session", {}, request);
      if (!currentRequest(name, request)) {
        if (downloadWindow) downloadWindow.close();
        return;
      }
      if (!session || session.alias !== state.alias || session.csrfToken !== state.csrfToken) {
        throw new APIError(401, "SessionChanged", "The local session has changed.");
      }
      if (!downloadWindow) {
        throw new APIError(0, "PopupBlocked", "Allow pop-ups for this local console, then try the download again.");
      }
      const parameters = new URLSearchParams({ bucket, key });
      if (versionId !== undefined) parameters.set("versionId", versionId);
      downloadWindow.location.replace(new URL(downloadPath || `/api/download?${parameters.toString()}`, window.location.origin).href);
      if (downloadPath && archiveJob) {
        archiveJob.status = "downloading";
        renderArchive();
        clearTimeout(archiveTimer);
        archiveTimer = setTimeout(pollArchive, 500);
      }
      setText("objects-status", "Download requested. If the object is unavailable or access is denied, its error appears in the new tab.");
    } catch (error) {
      if (downloadWindow) downloadWindow.close();
      if (!currentRequest(name, request) || error.name === "AbortError") return;
      if (expired(error)) return;
      elements["objects-status"].classList.add("error");
      setText(downloadPath ? "archive-status" : "objects-status", error.message);
    } finally {
      finishRequest(name, request);
    }
  }

  function validateJob(job) {
    const itemStates = new Set(["pending", "succeeded", "failed", "unknown"]);
    if (!job || typeof job.id !== "string" || !job.id || !["upload", "delete"].includes(job.kind) ||
      !jobStates.has(job.status) || typeof job.bucket !== "string" ||
      (job.items !== undefined && (!Array.isArray(job.items) || job.items.some(item => !item || typeof item.key !== "string" || !itemStates.has(item.status))))) {
      throw new APIError(0, "InvalidJob", "The console returned an invalid task status. Check the object list before retrying a write.");
    }
    return job.kind === "delete" && job.count === 0 && job.items === undefined ? { ...job, items: [] } : job;
  }

  function rememberJob(rawJob) {
    const validated = validateJob(rawJob);
    // Confirmation capabilities never enter task snapshots or recovered status.
    const { confirmToken, ...job } = validated;
    const existing = tasks.get(job.id);
    // A recovered/expired record cannot be revived by an older in-flight reply.
    if (existing?.expired) return existing;
    // An older status request may finish after the final upload response.
    if (existing && terminalStates.has(existing.job.status) && !terminalStates.has(job.status)) return existing;
    const becameTerminal = existing && !terminalStates.has(existing.job.status) && terminalStates.has(job.status);
    const task = existing || { job, xhr: null, sendProgress: null, notice: "", canceling: false, refreshing: false, expanded: false, expired: false };
    task.job = job;
    tasks.set(job.id, task);
    if (deletePlan?.job.id === job.id) deletePlan.job = job;
    renderTasks();
    schedulePoll();
    if (becameTerminal && state.authenticated && state.bucket === job.bucket) loadObjects(false);
    return task;
  }

  function schedulePoll() {
    clearTimeout(pollTimer);
    pollTimer = null;
    if (!pageSuspended && state.authenticated && !state.readOnly && [...tasks.values()].some(task => !task.expired && !terminalStates.has(task.job.status))) {
      pollTimer = setTimeout(loadJobs, 1500);
    }
  }

  function expireTask(task) {
    if (!task || task.expired) return;
    // This is a client lifecycle flag, not a new storage task outcome.
    task.expired = true;
    task.notice = "The task record has expired or was reclaimed. Its final outcome is unavailable here; check storage before retrying any write.";
    if (task.xhr) task.xhr.abort();
    if (deletePlan?.job.id === task.job.id) {
      deletePlan.confirmToken = "";
      renderDeletePlan();
    }
    renderTasks();
    schedulePoll();
  }

  async function loadJobs() {
    if (!state.authenticated || state.readOnly || requests.has("jobs")) return;
    // A response captured before concurrent creation cannot retire the new task.
    const knownIDs = [...tasks.keys()];
    const request = beginRequest("jobs");
    try {
      const result = await api("/api/jobs", {}, request);
      if (!currentRequest("jobs", request)) return;
      if (!result || !Array.isArray(result.jobs)) throw new APIError(0, "InvalidJobs", "Task status could not be read.");
      const returnedJobs = result.jobs.map(validateJob);
      const returnedIDs = new Set(returnedJobs.map(job => job.id));
      jobsMessage = "";
      for (const job of returnedJobs) if (!clearedTasks.has(job.id)) rememberJob(job);
      for (const id of knownIDs) if (!returnedIDs.has(id)) expireTask(tasks.get(id));
      renderTasks();
      if (deletePlan) renderDeletePlan();
    } catch (error) {
      if (!currentRequest("jobs", request) || error.name === "AbortError") return;
      if (expired(error)) return;
      jobsMessage = "Task status is temporarily unavailable. Use Refresh or Check status to retry; no write will be retried automatically.";
      for (const task of tasks.values()) {
        if (!task.expired && !terminalStates.has(task.job.status)) task.notice = "Task status is temporarily unavailable. No write will be retried automatically.";
      }
      renderTasks();
    } finally {
      finishRequest("jobs", request);
      schedulePoll();
    }
  }

  function statusLabel(task) {
    if (task.expired) return "Task record expired · outcome unavailable";
    const labels = { waiting: "Waiting for file", planning: "Building deletion plan", ready: "Awaiting confirmation", running: "Running", succeeded: "Succeeded", failed: "Failed", partial: "Partially completed", canceled: "Canceled" };
    if (task.xhr) return task.sendProgress === 100 ? "File sent · awaiting storage result" : "Sending file to OC";
    return labels[task.job.status] || "Unknown status";
  }

  function renderTasks() {
    renderOverview();
    const focusedAction = document.activeElement?.dataset?.taskAction;
    const focusedTask = document.activeElement?.dataset?.taskId;
    let restoreFocus = null;
    elements["tasks-panel"].hidden = tasks.size === 0 && !jobsMessage;
    setText("tasks-status", jobsMessage);
    elements["tasks-list"].replaceChildren();
    elements["clear-finished"].disabled = ![...tasks.values()].some(task => (task.expired || terminalStates.has(task.job.status)) && !task.xhr && !task.canceling && !task.refreshing);
    for (const task of [...tasks.values()].reverse()) {
      const job = task.job;
      const card = document.createElement("article");
      card.className = `task-card${task.expired || ["failed", "partial", "canceled"].includes(job.status) ? " issue" : ""}`;
      const heading = document.createElement("div");
      heading.className = "task-heading";
      const label = document.createElement("p");
      label.className = "task-label";
      label.textContent = job.kind === "upload" ? "File upload" : "Object deletion";
      const actions = document.createElement("div");
      actions.className = "task-actions";
      if (!task.expired && (!terminalStates.has(job.status) || task.xhr)) {
        const cancel = document.createElement("button");
        cancel.className = "button quiet";
        cancel.type = "button";
        cancel.textContent = task.canceling ? "Canceling…" : "Cancel task";
        cancel.disabled = task.canceling;
        cancel.dataset.taskId = job.id;
        cancel.dataset.taskAction = "cancel";
        if (focusedTask === job.id && focusedAction === "cancel") restoreFocus = cancel;
        cancel.addEventListener("click", () => cancelJob(job.id));
        actions.append(cancel);
      }
      const check = document.createElement("button");
      check.className = "button quiet";
      check.type = "button";
      check.textContent = task.refreshing ? "Checking…" : "Check status";
      check.disabled = task.refreshing || task.expired;
      check.dataset.taskId = job.id;
      check.dataset.taskAction = "check";
      if (focusedTask === job.id && focusedAction === "check") restoreFocus = check;
      check.addEventListener("click", () => refreshJob(job.id));
      actions.append(check);
      heading.append(label, actions);
      const scope = document.createElement("p");
      scope.className = "task-scope";
      scope.setAttribute("data-no-i18n", ""); scope.textContent = `${job.bucket}\n${job.kind === "upload" ? job.key || "" : job.prefix ? `Prefix: ${job.prefix}` : `${job.count} explicitly selected object keys`}`;
      const status = document.createElement("span");
      status.className = "task-state";
      status.textContent = statusLabel(task);
      card.append(heading, scope, status);
      if (!task.expired && job.kind === "upload" && task.sendProgress !== null) {
        const progress = document.createElement("progress");
        progress.className = "task-progress";
        progress.max = 100;
        progress.value = task.sendProgress;
        progress.setAttribute("aria-label", "File transfer from browser to OC; storage completion is shown separately");
        card.append(progress);
      }
      const message = document.createElement("p");
      message.className = "task-message";
      const details = [];
      if (task.expired) details.push(`Last known server status: ${job.status}.`);
      if (job.kind === "upload") {
        details.push(`${formatSize(job.size || 0)} · ${job.overwrite ? "Replacement allowed" : "Create only"}`);
        if (task.sendProgress !== null) details.push(`${task.sendProgress}% sent from this browser; this alone does not confirm storage success.`);
        if (!task.expired && job.status === "waiting" && !task.xhr) details.push("This page has no active file transfer for this task. Cancel it before preparing a new upload.");
        if (["failed", "canceled"].includes(job.status)) details.push("The object may already have been saved. Refresh and check it before retrying.");
      } else {
        const attempted = (job.items || []).filter(item => item.status !== "pending").length;
        details.push(`${attempted} of ${job.count || 0} objects attempted; ${job.completed || 0} succeeded.`);
        if (!task.expired && job.status === "ready" && (deletePlan?.job.id !== job.id || !deletePlan.confirmToken)) details.push("The original confirmation is unavailable on this page. Cancel this plan and create a fresh one to review its scope.");
        if (job.status === "canceled") details.push("Completed deletions were not undone. Pending objects may remain.");
      }
      if (job.error?.message) details.push(job.error.message);
      if (task.notice) details.push(task.notice);
      message.textContent = details.join(" ");
      card.append(message);
      if (Array.isArray(job.items) && job.items.length) {
        const detailsElement = document.createElement("details");
        detailsElement.className = "task-items";
        detailsElement.open = task.expanded;
        detailsElement.addEventListener("toggle", () => { task.expanded = detailsElement.open; });
        const summary = document.createElement("summary");
        summary.dataset.taskId = job.id;
        summary.dataset.taskAction = "details";
        if (focusedTask === job.id && focusedAction === "details") restoreFocus = summary;
        summary.textContent = `${task.expired ? "Last known object results" : "Object results"} (${job.items.length})`;
        const list = document.createElement("ul");
        for (const item of job.items) {
          const line = document.createElement("li");
          line.setAttribute("data-no-i18n", ""); line.textContent = `${item.key} — ${item.status}${item.status === "unknown" ? "; result is unconfirmed, check storage before retrying" : ""}${item.error?.message ? `: ${item.error.message}` : ""}`;
          list.append(line);
        }
        detailsElement.append(summary, list);
        card.append(detailsElement);
      }
      elements["tasks-list"].append(card);
    }
    if (restoreFocus && !restoreFocus.disabled) restoreFocus.focus({ preventScroll: true });
  }

  async function refreshJob(id) {
    const task = tasks.get(id);
    if (!task || task.expired || !state.authenticated || task.refreshing) return;
    task.refreshing = true;
    renderTasks();
    const name = `job-${id}`;
    const request = beginRequest(name);
    try {
      const job = await api(`/api/jobs/${encodeURIComponent(id)}`, {}, request);
      if (!currentRequest(name, request)) return;
      if (task.expired) return;
      if (terminalStates.has(job.status)) task.notice = "";
      rememberJob(job);
      if (deletePlan?.job.id === id) renderDeletePlan();
    } catch (error) {
      if (!currentRequest(name, request) || error.name === "AbortError") return;
      if (expired(error)) return;
      if (error.status === 404) {
        expireTask(task);
        return;
      }
      task.notice = error.message;
    } finally {
      task.refreshing = false;
      finishRequest(name, request);
      if (state.authenticated) renderTasks();
    }
  }

  async function cancelJob(id) {
    const task = tasks.get(id);
    if (!task || task.expired || !state.authenticated || state.readOnly || task.canceling) return;
    task.canceling = true;
    task.notice = "Cancellation requested. Completed work is not rolled back; an in-flight result may be unconfirmed.";
    if (task.xhr) task.xhr.abort();
    if (deletePlan?.job.id === id) {
      deletePlan.confirmToken = "";
      renderDeletePlan();
    }
    renderTasks();
    const name = `cancel-${id}`;
    const request = beginRequest(name);
    try {
      const job = await api(`/api/jobs/${encodeURIComponent(id)}/cancel`, mutationOptions({}), request);
      if (!currentRequest(name, request)) return;
      rememberJob(job);
    } catch (error) {
      if (!currentRequest(name, request) || error.name === "AbortError") return;
      if (expired(error)) return;
      if (error.status === 404) {
        expireTask(task);
        return;
      }
      task.notice = `Cancellation could not be confirmed. ${error.message} Check task status and storage before retrying.`;
    } finally {
      task.canceling = false;
      finishRequest(name, request);
      if (state.authenticated) renderTasks();
      schedulePoll();
    }
  }

  function renderUploadSelection() {
    const file = elements["upload-file"].files[0];
    elements["upload-selection"].hidden = !file;
    setText("upload-selected-name", file ? file.name : "");
    setText("upload-selected-size", file ? formatSize(file.size) : "");
    setText("upload-file-summary", file ? "已选择文件，可重新选择或从下方列表移除。" : "选择一个文件，然后确认目标桶与对象路径。");
  }

  function openUpload() {
    if (state.readOnly) return;
    uploadLocation = { bucket: state.bucket, prefix: state.prefix };
    uploadDraft = null;
    suggestedUploadKey = "";
    setText("upload-file-summary", "选择一个文件，然后确认目标桶与对象路径。");
    elements["upload-form"].reset();
    renderUploadSelection();
    elements["upload-bucket"].value = uploadLocation.bucket;
    setText("upload-status", `Maximum file size: ${formatSize(state.maxUploadSize)}.`);
    elements["upload-key"].value = uploadLocation.prefix;
    elements["upload-dialog"].showModal();
    elements["upload-file"].focus();
  }

  function closeUpload() {
    cancelRequest("upload-prepare");
    uploadDraft = null;
    uploadLocation = null;
    elements["upload-form"].reset();
    if (elements["upload-dialog"].open) elements["upload-dialog"].close();
    if (elements["replace-dialog"].open) elements["replace-dialog"].close();
    updateBusy();
  }

  async function startUpload(draft) {
    if (!state.authenticated || state.readOnly || requests.has("upload-prepare")) return;
    const request = beginRequest("upload-prepare");
    setText("upload-status", "Preparing upload… No file bytes have been sent yet.");
    setText("replace-status", "Preparing upload…");
    try {
      const job = validateJob(await api("/api/uploads", mutationOptions({ bucket: draft.bucket, key: draft.key, size: draft.file.size, overwrite: draft.overwrite }), request));
      if (!currentRequest("upload-prepare", request)) return;
      const task = rememberJob(job);
      if (job.kind !== "upload" || job.status !== "waiting" || job.bucket !== draft.bucket || job.key !== draft.key || !!job.overwrite !== draft.overwrite || (job.size || 0) !== draft.file.size) {
        throw new APIError(0, "InvalidUpload", "Upload preparation did not match the requested destination. No file bytes were sent.");
      }
      closeDialogs();
      elements["upload-form"].reset();
      uploadDraft = null;
      uploadLocation = null;
      sendFile(task, draft.file);
    } catch (error) {
      if (!currentRequest("upload-prepare", request) || error.name === "AbortError") return;
      if (expired(error)) return;
      setText("upload-status", error.message);
      setText("replace-status", error.message);
    } finally {
      finishRequest("upload-prepare", request);
    }
  }

  function sendFile(task, file) {
    const epoch = state.epoch;
    const xhr = new XMLHttpRequest();
    task.xhr = xhr;
    task.sendProgress = 0;
    task.notice = "";
    xhr.open("PUT", `/api/uploads/${encodeURIComponent(task.job.id)}`);
    xhr.withCredentials = true;
    xhr.setRequestHeader("X-CSRF-Token", state.csrfToken);
    xhr.setRequestHeader("Content-Type", "application/octet-stream");
    const active = () => !pageSuspended && !task.expired && state.authenticated && state.epoch === epoch && tasks.get(task.job.id) === task;
    xhr.upload.addEventListener("progress", event => {
      if (!active()) return;
      if (event.lengthComputable) task.sendProgress = event.total > 0 ? Math.min(100, Math.floor(100 * event.loaded / event.total)) : 100;
      renderTasks();
    });
    xhr.addEventListener("load", () => {
      if (!active()) return;
      task.xhr = null;
      let result;
      try { result = JSON.parse(xhr.responseText); } catch (_) { result = null; }
      if (xhr.status === 401 || (xhr.status === 403 && result?.code === "invalid_csrf")) {
        expired(new APIError(xhr.status, result?.code || "SessionExpired", "Your session has expired or changed."));
        return;
      }
      try {
        if (result?.id === task.job.id) {
          task.notice = "";
          rememberJob(result);
        } else {
          throw new APIError(xhr.status, result?.code || "UploadUnconfirmed", result?.message || "The upload result could not be read.");
        }
      } catch (error) {
        task.notice = `${error.message} The object may have been saved. Check task status and refresh the object list before retrying.`;
      }
      renderTasks();
      schedulePoll();
    });
    for (const type of ["error", "abort", "timeout"]) {
      xhr.addEventListener(type, () => {
        task.xhr = null;
        if (!active()) return;
        task.notice = "File transfer was interrupted. The storage result may be unconfirmed; check task status and the object before retrying.";
        renderTasks();
        schedulePoll();
      });
    }
    renderTasks();
    try {
      xhr.send(file);
    } catch (_) {
      task.xhr = null;
      task.notice = "This browser could not start the file transfer. Cancel this waiting task before preparing a new upload.";
      renderTasks();
      schedulePoll();
    }
  }

  async function prepareDeletion(scope) {
    if (!state.authenticated || state.readOnly || requests.has("delete-plan")) return;
    deletePlan = null;
    setText("delete-bucket", scope.bucket);
    setText("delete-scope", scope.prefix ? `Exact prefix: ${scope.prefix}` : `${scope.keys.length} explicitly selected object keys`);
    setText("delete-count", "");
    elements["delete-items"].replaceChildren();
    setText("delete-status", "Building a fixed object list… Nothing is being deleted.");
    elements["delete-dialog"].showModal();
    const request = beginRequest("delete-plan");
    try {
      const rawJob = validateJob(await api("/api/deletions/plan", mutationOptions(scope), request));
      if (!currentRequest("delete-plan", request)) return;
      const task = rememberJob(rawJob);
      if (rawJob.kind !== "delete" || rawJob.bucket !== scope.bucket || rawJob.status !== "ready" || typeof rawJob.confirmToken !== "string" || !rawJob.confirmToken || !Array.isArray(rawJob.items) || rawJob.items.length !== rawJob.count) {
        throw new APIError(0, "InvalidPlan", rawJob.error?.message || "The server did not return a complete deletion plan. Nothing was executed. Cancel the task and create a fresh plan.");
      }
      const plannedKeys = new Set(rawJob.items.map(item => item.key));
      const scopeMatches = scope.prefix !== undefined
        ? rawJob.prefix === scope.prefix && rawJob.items.every(item => item.key.startsWith(scope.prefix))
        : !rawJob.prefix && plannedKeys.size === new Set(scope.keys).size && scope.keys.every(key => plannedKeys.has(key));
      if (!scopeMatches || plannedKeys.size !== rawJob.count) {
        throw new APIError(0, "InvalidScope", "The deletion plan does not match the requested scope. Nothing was executed. Cancel the task and create a fresh plan.");
      }
      deletePlan = { job: task.job, confirmToken: rawJob.confirmToken };
      renderDeletePlan();
    } catch (error) {
      if (!currentRequest("delete-plan", request) || error.name === "AbortError") return;
      if (expired(error)) return;
      setText("delete-status", error.message);
    } finally {
      finishRequest("delete-plan", request);
    }
  }

  function renderDeletePlan() {
    if (!deletePlan) return;
    const job = deletePlan.job;
    const expires = new Date(job.expires);
    const expiredPlan = tasks.get(job.id)?.expired || !Number.isFinite(expires.getTime()) || expires.getTime() <= Date.now();
    setText("delete-bucket", job.bucket);
    setText("delete-scope", job.prefix ? `Exact prefix: ${job.prefix}` : "Explicitly selected object keys");
    setText("delete-count", `${job.count} ${job.count === 1 ? "object" : "objects"} in this plan. Expires ${Number.isFinite(expires.getTime()) ? expires.toLocaleTimeString() : "at an unknown time"}.`);
    elements["delete-items"].replaceChildren();
    for (const item of job.items || []) {
      const line = document.createElement("li");
      line.setAttribute("data-no-i18n", ""); line.textContent = item.key;
      elements["delete-items"].append(line);
    }
    if (expiredPlan) {
      deletePlan.confirmToken = "";
      setText("delete-status", "This plan has expired. Close it and create a fresh plan to review the current scope.");
    } else if (job.status !== "ready" || !deletePlan.confirmToken) {
      setText("delete-status", "This plan can no longer be executed here. Check its task status or create a fresh plan.");
    } else {
      setText("delete-status", job.count ? "Review every listed key. Confirming deletes their current objects; this cannot be undone here." : "The plan contains no objects. Confirming completes an empty task without deleting anything.");
    }
    setText("confirm-delete", job.count ? `Delete ${job.count} planned ${job.count === 1 ? "object" : "objects"}` : "Complete empty plan");
    updateBusy();
  }

  function closeDelete() {
    cancelRequest("delete-plan");
    const id = deletePlan?.job.id;
    const cancelReady = !!deletePlan?.confirmToken && deletePlan.job.status === "ready" && !requests.has("delete-execute");
    if (deletePlan) deletePlan.confirmToken = "";
    deletePlan = null;
    if (elements["delete-dialog"].open) elements["delete-dialog"].close();
    if (id && cancelReady) cancelJob(id);
    updateBusy();
  }

  async function executeDeletion() {
    if (!deletePlan || requests.has("delete-execute") || state.readOnly) return;
    renderDeletePlan();
    if (!deletePlan.confirmToken || deletePlan.job.status !== "ready") return;
    const id = deletePlan.job.id;
    const confirmToken = deletePlan.confirmToken;
    deletePlan.confirmToken = "";
    const request = beginRequest("delete-execute");
    setText("delete-status", "Submitting this exact plan… Check the task panel for per-object results.");
    try {
      const job = await api(`/api/deletions/${encodeURIComponent(id)}/execute`, mutationOptions({ confirmToken }), request);
      if (!currentRequest("delete-execute", request)) return;
      const returned = validateJob(job);
      if (returned.id !== id) throw new APIError(0, "InvalidTask", "The server returned a different deletion task. Check task status before doing anything else.");
      rememberJob(returned);
      deletePlan = null;
      elements["delete-dialog"].close();
      if (state.bucket === job.bucket) loadObjects(false);
    } catch (error) {
      if (!currentRequest("delete-execute", request) || error.name === "AbortError") return;
      if (expired(error)) return;
      const task = tasks.get(id);
      // This explicit server rejection happens before consuming the capability.
      // It is the one safe case where a user may confirm the same plan again.
      if (error.status === 429 && error.code === "busy" && deletePlan?.job.id === id && !task?.expired) {
        deletePlan.confirmToken = confirmToken;
        if (task) task.notice = "Execution was rejected because other write tasks are active. This plan has not started.";
        renderDeletePlan();
        setText("delete-status", "Other write tasks are active. This plan has not started. Wait for a task to finish, then confirm this same plan again.");
        renderTasks();
        return;
      }
      if (error.status === 404) {
        expireTask(task);
        setText("delete-status", "The task record has expired or was reclaimed. Its outcome is unavailable; check storage before preparing another deletion.");
        return;
      }
      if (task) task.notice = `Execution result is unconfirmed. ${error.message} Check task status; do not create a new plan merely to retry this request.`;
      setText("delete-status", `Execution may already be running. ${error.message} Check task status. This confirmation cannot be reused.`);
      renderTasks();
      refreshJob(id);
    } finally {
      finishRequest("delete-execute", request);
      schedulePoll();
    }
  }

  function canWriteSetting() {
    return state.authenticated && !state.readOnly && loadedSetting?.conditional === true && !loadedSetting.blocked && typeof loadedSetting.revision === "string" && /^[a-f0-9]{64}$/.test(loadedSetting.revision);
  }

  function canRotateSecret() {
    return state.authenticated && !state.readOnly && selfAccount?.kind === "iam" && selfAccount.status === "enabled" && selfAccount.canRotateSecret === true;
  }

  function renderAccountBuckets() {
    elements["account-buckets"].replaceChildren();
    if (!state.account) {
      const item = document.createElement("li");
      item.textContent = state.accountMessage;
      elements["account-buckets"].append(item);
      return;
    }
    for (const bucket of state.account.buckets) {
      const item = document.createElement("li");
      item.textContent = `${bucket.name} · ${formatSize(bucket.size)} · ${bucket.read ? "read indicated" : "read not indicated"} · ${bucket.write ? "write indicated" : "write not indicated"}`;
      elements["account-buckets"].append(item);
    }
    if (!state.account.buckets.length) {
      const item = document.createElement("li");
      item.textContent = "No bucket summary is available for this identity. Exact operations may still be permitted.";
      elements["account-buckets"].append(item);
    }
  }

  function openSettings() {
    if (!state.authenticated) return;
    closeSettings();
    elements["settings-bucket"].value = state.bucket;
    elements["settings-kind"].value = "policy";
    elements["settings-editor"].hidden = true;
    loadedSetting = null;
    settingDraft = null;
    pendingSettingRead = null;
    setText("settings-status", "Enter a bucket and choose a setting to read. Each setting has its own storage permission.");
    elements["settings-dialog"].showModal();
    elements[state.bucket ? "load-setting" : "settings-bucket"].focus();
    updateBusy();
  }

  function closeSettings() {
    cancelRequest("setting-read");
    cancelRequest("setting-write");
    loadedSetting = null;
    settingDraft = null;
    pendingSettingRead = null;
    elements["settings-document"].value = "";
    elements["settings-review-document"].textContent = "";
    for (const id of ["settings-dialog", "settings-read-dialog", "settings-review-dialog"]) if (elements[id].open) elements[id].close();
    updateBusy();
  }

  async function readSetting(approvedTarget = null) {
    if (!state.authenticated || !elements["settings-dialog"].open || pendingSettingRead || requests.has("setting-read") || requests.has("setting-write")) return;
    const bucket = approvedTarget?.bucket || elements["settings-bucket"].value;
    const kind = approvedTarget?.kind || elements["settings-kind"].value;
    if (!bucket || !Object.hasOwn(settingLabels, kind) || (kind === "versioning" && state.capabilities.versioning !== true)) return;
    if (!approvedTarget && loadedSetting && elements["settings-document"].value !== loadedSetting.document) {
      pendingSettingRead = { bucket, kind };
      setText("settings-read-current", `${loadedSetting.bucket} · ${settingLabels[loadedSetting.kind]}`);
      setText("settings-read-next", `${bucket} · ${settingLabels[kind]}`);
      elements["settings-dialog"].close();
      elements["settings-read-dialog"].showModal();
      elements["back-setting-read"].focus();
      updateBusy();
      return;
    }
    const request = beginRequest("setting-read");
    settingDraft = null;
    if (!loadedSetting) elements["settings-editor"].hidden = true;
    setText("settings-status", `Reading ${settingLabels[kind].toLowerCase()} for ${bucket}…`);
    updateBusy();
    try {
      const parameters = new URLSearchParams({ bucket, kind });
      const setting = await api(`/api/bucket-settings?${parameters}`, {}, request);
      if (!currentRequest("setting-read", request) || !elements["settings-dialog"].open) return;
      if (!setting || setting.bucket !== bucket || setting.kind !== kind || !["json", "xml"].includes(setting.format) || typeof setting.document !== "string" || typeof setting.exists !== "boolean") {
        throw new APIError(0, "InvalidSetting", "The local console returned an invalid setting. No change can be submitted.");
      }
      loadedSetting = { ...setting, blocked: false };
      elements["settings-document"].value = setting.document;
      elements["settings-editor"].hidden = false;
      setText("settings-scope", `${settingLabels[kind]} · ${bucket} · ${setting.format.toUpperCase()} · ${setting.exists ? "configuration present" : "no stored configuration"}`);
      const viewOnly = state.readOnly ? "This console is read-only." : !canWriteSetting() ? "The server did not provide conditional update support and a valid revision. Changes are unavailable." : "Saving requires this exact configuration revision; a concurrent change is rejected.";
      const versionHelp = kind === "versioning" ? " Versioning suspension keeps existing versions; it does not return the bucket to its never-enabled state." : "";
      const absentHelp = !setting.exists ? ` No stored configuration is present. Enter the complete ${setting.format.toUpperCase()} document to create one.` : "";
      setText("settings-help", `${viewOnly}${versionHelp}${absentHelp}`);
      setText("settings-status", "");
    } catch (error) {
      if (!currentRequest("setting-read", request) || error.name === "AbortError") return;
      if (expired(error)) return;
      setText("settings-status", `${settingLabels[kind]} for ${bucket}: ${error.message}${loadedSetting ? " The previous configuration and draft are retained." : ""} Other settings can be read independently.`);
    } finally {
      finishRequest("setting-read", request);
    }
  }

  function reviewSetting(remove) {
    if (!elements["settings-dialog"].open || !canWriteSetting() || requests.has("setting-read") || requests.has("setting-write")) return;
    if (remove && (!loadedSetting.exists || loadedSetting.kind === "versioning")) return;
    const document = elements["settings-document"].value;
    if (!remove && !document.trim()) {
      setText("settings-status", "Enter the complete configuration, or use Review removal for a stored policy or lifecycle.");
      return;
    }
    if (!remove && loadedSetting.kind === "policy") {
      try {
        const policy = JSON.parse(document);
        const statements = Array.isArray(policy.Statement) ? policy.Statement : [policy.Statement];
        const mismatch = statements.some(statement => {
          const resources = Array.isArray(statement?.Resource) ? statement.Resource : [statement?.Resource];
          return resources.some(resource => {
            if (typeof resource !== "string" || !resource.startsWith("arn:aws:s3:::")) return false;
            const bucket = resource.slice(13).split("/")[0];
            return bucket !== loadedSetting.bucket && !/[?*]/.test(bucket);
          });
        });
        if (mismatch) { setText("settings-status", "The policy Resource refers to a different bucket. Use the target bucket name in every S3 resource ARN."); return; }
      } catch { setText("settings-status", "Enter a valid JSON policy document."); return; }
    }
    settingDraft = { bucket: loadedSetting.bucket, kind: loadedSetting.kind, document: remove ? "" : document, revision: loadedSetting.revision, remove, confirm: true };
    setText("settings-review-bucket", settingDraft.bucket);
    setText("settings-review-kind", settingLabels[settingDraft.kind]);
    const risks = {
      policy: "A bucket policy can expose objects publicly or restrict future access. Review every statement and condition before applying it.",
      versioning: "Versioning affects future writes and deletes. Enabling it can increase storage use; suspension keeps previous versions and cannot disable object-lock requirements.",
      lifecycle: "Lifecycle rules can expire current objects and permanently delete historical versions later. Removing rules stops those rules from scheduling future work; it cannot restore objects already removed.",
    };
    setText("settings-review-warning", `${remove ? "Remove the complete stored configuration" : "Replace the complete configuration"} for this exact bucket. ${risks[settingDraft.kind]}`);
    setText("settings-review-document", remove ? "Remove this configuration." : document);
    setText("settings-review-status", "");
    setText("confirm-setting", remove ? "Remove this setting" : "Apply this setting");
    elements["settings-dialog"].close();
    elements["settings-review-dialog"].showModal();
    elements["back-settings"].focus();
    updateBusy();
  }

  function backSettings() {
    if (!state.authenticated) return;
    if (requests.has("setting-write")) {
      cancelRequest("setting-write");
      if (loadedSetting) loadedSetting.blocked = true;
      setText("settings-status", "The save response was interrupted. Its outcome is unknown; read the setting before making another change.");
    }
    settingDraft = null;
    elements["settings-review-document"].textContent = "";
    if (elements["settings-review-dialog"].open) elements["settings-review-dialog"].close();
    elements["settings-dialog"].showModal();
    elements["load-setting"].focus();
    updateBusy();
  }

  async function saveSetting() {
    if (!elements["settings-review-dialog"].open || !settingDraft || !canWriteSetting() || requests.has("setting-write")) return;
    const draft = settingDraft;
    const request = beginRequest("setting-write");
    setText("settings-review-status", "Applying the reviewed setting…");
    try {
      const setting = await api("/api/bucket-settings", mutationOptions(draft), request);
      if (!currentRequest("setting-write", request) || settingDraft !== draft) return;
      if (!setting || setting.bucket !== draft.bucket || setting.kind !== draft.kind ||
        setting.format !== (draft.kind === "policy" ? "json" : "xml") || typeof setting.document !== "string" ||
        typeof setting.exists !== "boolean" || setting.exists === draft.remove || setting.conditional !== true || typeof setting.revision !== "string" || !/^[a-f0-9]{64}$/.test(setting.revision)) {
        throw new APIError(0, "InvalidSetting", "The local console returned an unexpected update response. Its outcome is unknown.");
      }
      loadedSetting = { ...setting, blocked: false };
      elements["settings-document"].value = setting.document;
      setText("settings-scope", `${settingLabels[draft.kind]} · ${draft.bucket} · ${loadedSetting.format?.toUpperCase() || "configuration"} · ${setting.exists ? "configuration present" : "no stored configuration"}`);
      setText("settings-status", "The storage server confirmed this change. Read again before editing if the configuration changed elsewhere.");
      settingDraft = null;
      elements["settings-review-document"].textContent = "";
      elements["settings-review-dialog"].close();
      elements["settings-dialog"].showModal();
    } catch (error) {
      if (!currentRequest("setting-write", request) || error.name === "AbortError") return;
      if (expired(error)) return;
      const unknown = error.code === "outcome_unknown" || !error.status || error.status >= 500 || (error.status >= 300 && error.status < 400) || (error.code === "InvalidResponse" && error.status >= 200 && error.status < 300);
      const conflict = error.status === 412 || error.code === "setting_conflict";
      if (conflict || unknown) {
        loadedSetting.blocked = true;
        settingDraft = null;
        elements["settings-review-document"].textContent = "";
        elements["settings-review-dialog"].close();
        elements["settings-dialog"].showModal();
        setText("settings-status", conflict ? "The configuration changed since it was read. Your draft is retained. Copy any edits you want to keep, then read the current setting and review a fresh change." : "The update outcome is unknown. Your draft is retained. Read the setting to check what storage accepted before making another change; this write will not be replayed.");
      } else {
        setText("settings-review-status", error.message);
      }
    } finally {
      finishRequest("setting-write", request);
      if (state.authenticated && elements["settings-dialog"].open && loadedSetting?.blocked) elements["load-setting"].focus();
    }
  }

  function clearSecret() {
    elements["new-secret"].value = "";
    elements["new-secret"].disabled = false;
    elements["secret-confirmation"].hidden = true;
    elements["review-secret"].hidden = false;
  }

  function closeAccount() {
    if (requests.has("secret-write")) return;
    cancelRequest("self-account");
    selfAccount = null;
    clearSecret();
    elements["self-secret-form"].hidden = true;
    if (elements["account-dialog"].open) elements["account-dialog"].close();
    updateBusy();
  }

  async function openAccount() {
    if (!state.authenticated || requests.has("secret-write")) return;
    closeAccount();
    renderAccountBuckets();
    setText("self-account-status", "Loading the current identity's account information…");
    setText("self-account-summary", "");
    setText("self-secret-help", "");
    setText("self-secret-status", "");
    elements["account-dialog"].showModal();
    await loadSelfAccount();
  }

  async function loadSelfAccount(messageAfter = "") {
    if (!state.authenticated || !elements["account-dialog"].open) return;
    selfAccount = null;
    clearSecret();
    elements["self-secret-form"].hidden = true;
    const request = beginRequest("self-account");
    let discoveryFailed = false;
    try {
      const account = await api("/api/self-account", {}, request);
      if (!currentRequest("self-account", request) || !elements["account-dialog"].open) return;
      if (!account || !["root", "iam", "sts", "service", "directory", "unknown"].includes(account.kind) || !["enabled", "disabled", "unknown"].includes(account.status) || typeof account.canRotateSecret !== "boolean") {
        throw new APIError(0, "InvalidAccount", "Current identity information is unavailable. Secret rotation is disabled.");
      }
      selfAccount = { kind: account.kind, status: account.status, canRotateSecret: account.canRotateSecret };
      const labels = { root: "Root account", iam: "IAM user", sts: "Temporary STS identity", service: "Service account", directory: "Directory identity", unknown: "Identity type unavailable" };
      setText("self-account-summary", `${labels[account.kind]} · ${account.status === "unknown" ? "status unavailable" : account.status}`);
      setText("self-account-status", "");
      elements["self-secret-form"].hidden = !canRotateSecret();
      const message = state.readOnly ? "This local console is read-only. Secret rotation is disabled." : account.kind === "unknown" || account.status === "unknown" ? "This server does not provide safe current-identity discovery. Secret rotation is unavailable; bucket browsing remains independent." : account.kind !== "iam" ? "This identity cannot rotate its secret here. Manage root, temporary, service, and directory credentials through their respective owner or identity provider." : !canRotateSecret() ? "The storage server does not permit secret rotation for this identity." : "Secret rotation changes only this IAM user's secret and preserves its account status. OC does not write the new secret to your alias file.";
      setText("self-secret-help", message);
    } catch (error) {
      if (!currentRequest("self-account", request) || error.name === "AbortError") return;
      if (expired(error)) return;
      discoveryFailed = true;
      setText("self-account-status", error.message);
      setText("self-secret-help", "Account information is unavailable. Bucket browsing remains independent; secret rotation is disabled.");
    } finally {
      if (messageAfter && currentRequest("self-account", request) && elements["account-dialog"].open) {
        setText("self-account-status", `${messageAfter}${discoveryFailed ? " Current identity information could not be refreshed; secret rotation is disabled." : ""}`);
      }
      finishRequest("self-account", request);
    }
  }

  function stopAfterRotation(confirmed) {
    consoleStopped = true;
    rotationInFlight = false;
    showLogin(confirmed ? "The storage server confirmed secret rotation. All local sessions have ended. Update this alias's secret in your terminal and restart OC console." : "The secret rotation outcome is unknown. This console has stopped using the old identity. Verify which secret works in your terminal, update the alias, and restart OC console. Do not repeat this request automatically.");
    setText("session-mode", "● Restart required");
    updateBusy();
  }

  function validNewSecret() {
    const length = new TextEncoder().encode(elements["new-secret"].value).length;
    if (length < 8 || length > 128 || /[\x00\r\n]/.test(elements["new-secret"].value)) {
      setText("self-secret-status", "Enter a new secret containing 8–128 UTF-8 bytes.");
      return false;
    }
    return true;
  }

  async function rotateSecret(event) {
    event.preventDefault();
    if (!canRotateSecret() || elements["secret-confirmation"].hidden || requests.has("secret-write") || !validNewSecret()) return;
    const options = mutationOptions({ newSecret: elements["new-secret"].value, confirm: true });
    clearSecret();
    const request = beginRequest("secret-write");
    rotationInFlight = true;
    setText("self-secret-status", "Rotating the secret and stopping this console…");
    elements["self-secret-status"].focus();
    try {
      const result = await api("/api/self-secret", options, request);
      if (!currentRequest("secret-write", request)) return;
      if (!result || result.restartRequired !== true || result.outcome !== "confirmed") {
        stopAfterRotation(false);
      } else {
        stopAfterRotation(true);
      }
    } catch (error) {
      if (!currentRequest("secret-write", request)) return;
      if (error.restartRequired === true || error.code === "outcome_unknown" || !error.status ||
        (error.code === "InvalidResponse" && error.status >= 200 && error.status < 300) ||
        (error.status >= 300 && error.status < 400) ||
        (error.status >= 500 && error.restartRequired !== false)) {
        stopAfterRotation(false);
        return;
      }
      rotationInFlight = false;
      if (expired(error)) return;
      const message = error.status === 409 && error.code === "active_tasks" ? "Stop active transfers and deletions before rotating the secret. Enter the new secret again when they finish." : `${error.message} Enter the new secret again to make a new request.`;
      setText("self-secret-status", message);
      if (error.restartRequired === false) {
        finishRequest("secret-write", request);
        await loadSelfAccount(message);
      }
    } finally {
      rotationInFlight = false;
      finishRequest("secret-write", request);
      if (state.authenticated && elements["account-dialog"].open && !requests.has("secret-write")) elements[canRotateSecret() ? "new-secret" : "close-account"].focus();
    }
  }

  let capabilityBucket = "";
  function featureEnabled(name) {
    return state.authenticated && state.capabilities[name] === true;
  }

  function canSelectObjects() {
    return state.authenticated && (!state.readOnly || featureEnabled("archives"));
  }

  async function loadCapabilities(bucket = state.bucket) {
    if (!state.authenticated || (requests.has("capabilities") && bucket === capabilityBucket)) return;
    capabilityBucket = bucket;
    const request = beginRequest("capabilities");
    try {
      const result = await api(bucket ? `/api/capabilities?${new URLSearchParams({ bucket })}` : "/api/capabilities", {}, request);
      if (!currentRequest("capabilities", request)) return;
      state.capabilities = result && typeof result === "object" ? { ...result } : {};
      renderMode();
      renderEntries();
      if (featureEnabled("archives")) loadArchiveList();
    } catch (error) {
      if (!currentRequest("capabilities", request) || error.name === "AbortError") return;
      if (expired(error)) return;
      state.capabilities = { versions: false, versioning: false };
      renderMode();
    } finally {
      finishRequest("capabilities", request);
    }
  }

  function updateFeatures() {
    const writable = state.authenticated && !state.readOnly;
    elements["open-create-bucket"].hidden = !featureEnabled("bucketManagement") || !writable;
    elements["directory-create"].hidden = elements["open-create-bucket"].hidden;
    elements["open-delete-bucket"].hidden = !featureEnabled("bucketManagement") || !writable;
    elements["open-delete-bucket"].disabled = !featureEnabled("bucketManagement") || !writable;
    elements["open-versions"].hidden = !featureEnabled("versions");
    elements["open-versions"].disabled = !featureEnabled("versions");
    const versionOption = elements["setting-versioning-option"];
    if (versionOption) { versionOption.hidden = state.capabilities.versioning !== true; versionOption.disabled = state.capabilities.versioning !== true; }
    if (state.capabilities.versioning !== true && elements["settings-kind"].value === "versioning") elements["settings-kind"].value = "policy";
    if (!featureEnabled("versions")) { cancelRequest("versions"); if (elements["versions-dialog"].open) elements["versions-dialog"].close(); versionEntries = []; versionCursor = ""; }

    elements["open-iam"].hidden = !featureEnabled("iam");
    elements["read-toolbar"].hidden = !featureEnabled("archives");
    const archiveBytes = Number.isSafeInteger(state.capabilities.maxArchiveSize) && state.capabilities.maxArchiveSize > 0 ? state.capabilities.maxArchiveSize : 5 * 1024 ** 3;
    const archiveCount = Number.isSafeInteger(state.capabilities.maxArchiveObjects) && state.capabilities.maxArchiveObjects > 0 ? state.capabilities.maxArchiveObjects : 1000;
    setText("archive-limit-help", `Up to ${archiveCount.toLocaleString()} objects and ${formatSize(archiveBytes)} are supported.`);
    elements["archive-selected"].disabled = !featureEnabled("archives") || !selectedKeys.size;
    elements["archive-prefix"].disabled = !featureEnabled("archives") || !state.bucket;
    elements["open-current-archive"].hidden = !archiveJob && !requests.has("archive-create");
    setText("archive-selection-count", selectedKeys.size ? `${selectedKeys.size} objects selected` : "No objects selected");
    const bucketBusy = requests.has("bucket-action");
    elements["submit-bucket-action"].disabled = !writable || bucketBusy;
    elements["close-bucket-action"].disabled = bucketBusy;
    elements["bucket-action-name"].disabled = bucketBusy;
    elements["bucket-action-confirm"].disabled = bucketBusy;
    elements["load-versions"].disabled = requests.has("versions");
    elements["versions-bucket"].disabled = requests.has("versions");
    elements["versions-key"].disabled = requests.has("versions");
    elements["more-versions"].disabled = requests.has("versions");
    elements["create-share"].disabled = !featureEnabled("sharing") || requests.has("share");
    elements["copy-share"].disabled = !elements["share-url"].value;
    elements["start-archive"].disabled = !archiveDraft || !!archiveJob || requests.has("archive-create") || requests.has("archives-list");
    elements["cancel-archive"].disabled = requests.has("archive-cancel");
    elements["retry-archive-status"].disabled = requests.has("archive-status");
    elements["refresh-iam"].disabled = requests.has("iam-list");
    elements["submit-iam-action"].disabled = !writable || requests.has("iam-action") || !elements["iam-secret-result"].hidden || (bindingAction() && (!bindingReady() || requests.has("iam-bindings") || !!pendingBindingRead));
    elements["close-iam"].disabled = requests.has("iam-action");
    const iamLocked = requests.has("iam-action") || requests.has("iam-bindings") || !!pendingBindingRead || !elements["iam-secret-result"].hidden;
    elements["iam-kind"].disabled = iamLocked;
    elements["iam-action"].disabled = iamLocked;
    elements["iam-owner"].disabled = iamLocked;
    elements["iam-confirm"].disabled = iamLocked;
    for (const input of Object.values(iamInputs)) input.disabled = iamLocked;
    if (iamInputs.policies && bindingAction()) iamInputs.policies.readOnly = !bindingReady() || iamLocked;
    elements["load-iam-bindings"].disabled = iamLocked;
    elements["confirm-iam-bindings-read"].disabled = requests.has("iam-bindings");
  }

  function actionButton(label, callback, ariaLabel = label) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "row-action";
    button.textContent = label;
    if (label.includes("/") || label === infoObject?.key || [...favoriteObjects.values()].some(item => item.key === label)) button.setAttribute("data-no-i18n", "");
    button.setAttribute("aria-label", ariaLabel);
    button.addEventListener("click", callback);
    return button;
  }

  function openBucketAction(remove) {
    if (!featureEnabled("bucketManagement") || state.readOnly) return;
    closeDialogs();
    bucketAction = remove ? "delete" : "create";
    elements["bucket-action-form"].reset();
    elements["bucket-action-name"].value = remove ? state.bucket : "";
    elements["bucket-action-name"].readOnly = false;
    elements["bucket-confirm-field"].hidden = !remove;
    elements["bucket-action-confirm"].required = remove;
    setText("bucket-action-title", remove ? "Delete empty bucket" : "Create bucket");
    setText("bucket-action-help", remove ? "Enter an exact known bucket name, even if listing buckets is not allowed. Only an empty bucket can be deleted. Objects, historical versions, and delete markers must be removed separately. OC never empties the bucket automatically." : "A new bucket starts private. Use 3–63 lowercase letters, digits, dots, or hyphens; begin and end with a letter or digit.");
    setText("bucket-action-status", "");
    setText("submit-bucket-action", remove ? "Delete this empty bucket" : "Create private bucket");
    elements["submit-bucket-action"].className = `button ${remove ? "danger" : "primary"}`;
    updateBusy();
    elements["bucket-dialog"].showModal();
    elements[remove && state.bucket ? "bucket-action-confirm" : "bucket-action-name"].focus();
  }

  async function submitBucketAction(event) {
    event.preventDefault();
    if (!featureEnabled("bucketManagement") || state.readOnly || requests.has("bucket-action")) return;
    const bucket = elements["bucket-action-name"].value;
    const removing = bucketAction === "delete";
    const confirmBucket = elements["bucket-action-confirm"].value;
    if (removing && confirmBucket !== bucket) {
      setText("bucket-action-status", "Enter the exact bucket name to confirm deletion.");
      return;
    }
    const request = beginRequest("bucket-action");
    setText("bucket-action-status", removing ? "Deleting empty bucket…" : "Creating bucket…");
    try {
      await api(`/api/buckets/${removing ? "delete" : "create"}`, mutationOptions(removing ? { bucket, confirmBucket } : { bucket }), request);
      if (!currentRequest("bucket-action", request)) return;
      elements["bucket-dialog"].close();
      state.bucket = removing ? "" : bucket;
      state.prefix = "";
      selectedKeys.clear();
      loadBuckets();
    } catch (error) {
      if (!currentRequest("bucket-action", request) || error.name === "AbortError") return;
      if (!expired(error)) setText("bucket-action-status", error.message);
    } finally {
      finishRequest("bucket-action", request);
    }
  }

  function openVersions(bucket, key = "") {
    if (!featureEnabled("versions")) return;
    closeDialogs();
    versionLocation = null;
    versionEntries = [];
    versionCursor = "";
    elements["versions-bucket"].value = bucket;
    elements["versions-key"].value = key;
    elements["versions-list"].replaceChildren();
    elements["more-versions"].hidden = true;
    setText("versions-status", "");
    elements["versions-dialog"].showModal();
    if (key) readVersions(false);
    else elements[bucket ? "versions-key" : "versions-bucket"].focus();
  }

  async function readVersions(append) {
    const bucket = append ? versionLocation?.bucket : elements["versions-bucket"].value;
    const key = append ? versionLocation?.key : elements["versions-key"].value;
    if (!featureEnabled("versions") || !bucket || !key || (append && !versionCursor)) return;
    const cursor = append ? versionCursor : "";
    const request = beginRequest("versions");
    setText("versions-status", "Loading versions…");
    if (!append) {
      versionEntries = [];
      versionCursor = "";
      versionLocation = { bucket, key };
      renderVersions();
    }
    const parameters = new URLSearchParams({ bucket, key, limit: "100" });
    if (cursor) parameters.set("cursor", cursor);
    try {
      const result = await api(`/api/versions?${parameters}`, {}, request);
      if (!currentRequest("versions", request)) return;
      if (!result || !Array.isArray(result.entries) || result.entries.some(entry => !entry || typeof entry.versionId !== "string" || typeof entry.key !== "string")) throw new APIError(0, "InvalidVersions", "The console returned an invalid version list.");
      versionEntries = [...new Map((append ? [...versionEntries, ...result.entries] : result.entries).map(entry => [`${entry.key}:${entry.versionId}`, entry])).values()];
      versionCursor = typeof result.nextCursor === "string" && result.nextCursor !== cursor ? result.nextCursor : "";
      setText("versions-status", versionEntries.length ? "" : "No versions were returned for this exact object.");
      renderVersions();
    } catch (error) {
      if (!currentRequest("versions", request) || error.name === "AbortError") return;
      if (error.code === "versions_unsupported" || error.status === 501) { state.capabilities.versions = false; state.capabilities.versioning = false; updateFeatures(); renderEntries(); }
      else if (!expired(error)) setText("versions-status", error.message);
    } finally { finishRequest("versions", request); }
  }

  function renderVersions() {
    elements["versions-list"].replaceChildren();
    for (const entry of versionEntries) {
      const card = document.createElement("article");
      card.className = "record-card";
      const title = document.createElement("p");
      title.className = "scope-key";
      title.setAttribute("data-no-i18n", ""); title.textContent = entry.versionId || "Unversioned object";
      const detail = document.createElement("p");
      detail.className = "subtle";
      detail.textContent = `${entry.latest ? "Latest · " : ""}${entry.deleteMarker ? "Delete marker" : formatSize(entry.size)} · ${formatDate(entry.modified)}`;
      card.append(title, detail);
      if (!entry.deleteMarker && versionLocation) {
        const ref = { bucket: versionLocation.bucket, key: entry.key, versionId: entry.versionId };
        const actions = document.createElement("div");
        actions.className = "record-actions";
        const download = document.createElement("a");
        download.className = "download-link";
        const parameters = new URLSearchParams(ref);
        download.href = `/api/download?${parameters}`;
        download.textContent = "Download this version ↓";
        download.target = "_blank";
        download.rel = "noopener";
        download.addEventListener("click", event => prepareDownload(event, download, ref.bucket, ref.key, ref.versionId));
        actions.append(download);
        if (featureEnabled("sharing")) actions.append(actionButton("Share this version", () => openShare(ref)));
        if (featureEnabled("archives")) actions.append(actionButton("ZIP this version", () => openArchive({ refs: [ref] })));
        card.append(actions);
      }
      elements["versions-list"].append(card);
    }
    elements["more-versions"].hidden = !versionCursor;
  }

  function clearShare() {
    cancelRequest("share");
    shareRef = null;
    elements["share-url"].value = "";
    elements["share-name"].value = "";
    elements["share-result"].hidden = true;
    setText("share-scope", "");
    setText("share-status", "");
    setText("share-expires", "");
  }

  function openShare(ref) {
    if (!featureEnabled("sharing")) return;
    closeDialogs();
    shareRef = { ...ref };
    elements["share-expiry"].value = "3600";
    setText("share-scope", `${ref.bucket}/${ref.key}${ref.versionId !== undefined ? `\nVersion ${ref.versionId || "null"}` : "\nCurrent object"}`);
    elements["share-dialog"].showModal();
    updateBusy();
  }

  async function createShare(event) {
    event.preventDefault();
    if (!featureEnabled("sharing") || !shareRef) return;
    const request = beginRequest("share");
    elements["share-url"].value = "";
    elements["share-result"].hidden = true;
    setText("share-status", "Creating a download link…");
    const body = { ...shareRef, expiresSeconds: Number(elements["share-expiry"].value), downloadName: elements["share-name"].value };
    try {
      const result = await api("/api/shares", mutationOptions(body), request);
      if (!currentRequest("share", request)) return;
      if (!result || typeof result.url !== "string" || !/^https?:\/\//.test(result.url)) throw new APIError(0, "InvalidShare", "The console returned an invalid share link.");
      elements["share-url"].value = result.url;
      elements["share-result"].hidden = false;
      setText("share-expires", `Expires ${new Date(result.expiresAt).toLocaleString()}. Temporary credentials may expire sooner.`);
      setText("share-status", "Link created. Copy it before closing this window.");
    } catch (error) {
      if (!currentRequest("share", request) || error.name === "AbortError") return;
      if (!expired(error)) setText("share-status", error.message);
    } finally { finishRequest("share", request); }
  }

  async function copyShare() {
    const value = elements["share-url"].value;
    if (!value) return;
    try {
      if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(value);
        setText("share-status", "Link copied.");
        return;
      }
    } catch (_) { /* Manual selection also works when clipboard permission is denied. */ }
    elements["share-url"].focus();
    elements["share-url"].select();
    setText("share-status", "Link selected. Copy it using your browser or keyboard.");
  }

  function clearArchive() {
    clearTimeout(archiveTimer);
    archiveTimer = null;
    cancelRequest("archive-status");
    archiveDraft = null;
    archiveJob = null;
    elements["archive-items"].replaceChildren();
    elements["archive-progress"].hidden = true;
    elements["retry-archive-status"].hidden = true;
    elements["download-archive"].hidden = true;
    elements["download-archive"].removeAttribute("href");
    elements["cancel-archive"].hidden = true;
    elements["start-archive"].hidden = false;
    setText("archive-scope", "");
    setText("archive-status", "");
  }

  function openArchive(draft) {
    if (!featureEnabled("archives")) return;
    closeDialogs();
    if ((archiveJob && ["planning", "running", "ready", "downloading"].includes(archiveJob.status)) || requests.has("archive-create")) {
      elements["archive-dialog"].showModal();
      return;
    }
    clearArchive();
    archiveDraft = draft.refs ? { refs: draft.refs.map(ref => ({ ...ref })) } : { bucket: draft.bucket, prefix: draft.prefix };
    setText("archive-scope", draft.refs ? `${draft.refs.length} selected ${draft.refs.length === 1 ? "object" : "objects"}` : `Bucket ${draft.bucket}\nPrefix ${draft.prefix || "(all objects)"}`);
    for (const ref of draft.refs || []) {
      const item = document.createElement("li");
      item.setAttribute("data-no-i18n", ""); item.textContent = `${ref.bucket}/${ref.key}${ref.versionId !== undefined ? ` · version ${ref.versionId || "null"}` : " · current object"}`;
      elements["archive-items"].append(item);
    }
    setText("archive-status", draft.refs ? "The selected keys are fixed. Current objects are resolved during preparation." : "Preparation lists this prefix once and fixes the archive manifest before reading object data.");
    elements["archive-dialog"].showModal();
    updateBusy();
  }

  function renderArchive() {
    if (!archiveJob) return;
    const { status, count, size, transferred, error } = archiveJob;
    elements["retry-archive-status"].hidden = true;
    const labels = { planning: "Planning archive…", running: "Preparing ZIP…", ready: "ZIP ready to download", downloading: "Downloading ZIP…", succeeded: "ZIP download completed", failed: "Archive preparation failed", canceled: "Archive canceled" };
    setText("archive-status", `${labels[status] || status} · ${count || 0} objects · ${formatSize(size)}${error ? ` · ${typeof error === "object" ? error.message || "Archive preparation failed." : error}` : ""}`);
    if (Array.isArray(archiveJob.entries)) {
      elements["archive-items"].replaceChildren();
      for (const entry of archiveJob.entries) {
        const item = document.createElement("li");
        item.setAttribute("data-no-i18n", ""); item.textContent = `${entry.bucket}/${entry.key} · ${formatSize(entry.size)} · ${entry.versionId ? `version ${entry.versionId}` : "current object (fixed content)"} → ${entry.archivePath}`;
        elements["archive-items"].append(item);
      }
    }
    elements["start-archive"].hidden = true;
    elements["cancel-archive"].hidden = !["planning", "running", "ready"].includes(status);
    elements["cancel-archive"].textContent = status === "ready" ? "Discard ZIP" : "Cancel preparation";
    elements["download-archive"].hidden = status !== "ready";
    if (status === "ready") elements["download-archive"].href = `/api/archives/${encodeURIComponent(archiveJob.id)}/download`;
    elements["archive-progress"].hidden = status !== "running";
    elements["archive-progress"].max = size || 1;
    elements["archive-progress"].value = transferred || 0;
    updateBusy();
  }

  function acceptArchive(result) {
    if (!result || typeof result.id !== "string" || !["planning", "running", "ready", "downloading", "succeeded", "failed", "canceled"].includes(result.status)) throw new APIError(0, "InvalidArchive", "The console returned an invalid archive status.");
    archiveJob = result;
    renderArchive();
    clearTimeout(archiveTimer);
    if (["planning", "running", "downloading", "ready"].includes(result.status) && !pageSuspended) archiveTimer = setTimeout(pollArchive, result.status === "ready" ? 5000 : 1000);
  }

  async function loadArchiveList() {
    if (!featureEnabled("archives") || requests.has("archive-create")) return;
    const request = beginRequest("archives-list");
    try {
      const result = await api("/api/archives", {}, request);
      if (!currentRequest("archives-list", request)) return;
      const states = new Set(["planning", "running", "ready", "downloading", "succeeded", "failed", "canceled"]);
      if (!result || !Array.isArray(result.archives) || result.archives.length > 16 || result.archives.some(task => !task || typeof task.id !== "string" || !states.has(task.status))) throw new APIError(0, "InvalidArchives", "The console returned an invalid ZIP task list.");
      const active = result.archives.find(task => ["planning", "running", "ready", "downloading"].includes(task.status));
      const recovered = active || (archiveJob && result.archives.find(task => task.id === archiveJob.id));
      if (recovered) {
        if (archiveJob?.id !== recovered.id) {
          archiveDraft = null;
          elements["archive-items"].replaceChildren();
          setText("archive-scope", `Recovered ZIP task${recovered.created ? ` · created ${new Date(recovered.created).toLocaleString()}` : ""}`);
        }
        acceptArchive(recovered);
      } else if (archiveJob) {
        archiveJob.status = "failed";
        archiveJob.error = { message: "Archive expired or is no longer available." };
        renderArchive();
      }
    } catch (error) {
      if (!currentRequest("archives-list", request) || error.name === "AbortError") return;
      if (!expired(error)) setText("archive-status", `${error.message} Check ZIP status before preparing another archive.`);
    } finally { finishRequest("archives-list", request); }
  }

  async function startArchive() {
    if (!featureEnabled("archives") || !archiveDraft || archiveJob || requests.has("archives-list")) return;
    cancelRequest("archives-list");
    const request = beginRequest("archive-create");
    let recover = false;
    setText("archive-status", "Planning ZIP…");
    try {
      const result = await api("/api/archives", mutationOptions(archiveDraft), request);
      if (currentRequest("archive-create", request)) acceptArchive(result);
    } catch (error) {
      if (!currentRequest("archive-create", request) || error.name === "AbortError") return;
      if (!expired(error)) {
        setText("archive-status", error.message);
        recover = error.status === 0 || error.status === 429;
      }
    } finally {
      finishRequest("archive-create", request);
      if (recover) loadArchiveList();
    }
  }

  async function pollArchive() {
    if (!archiveJob || !state.authenticated || pageSuspended) return;
    const id = archiveJob.id;
    const request = beginRequest("archive-status");
    try {
      const result = await api(`/api/archives/${encodeURIComponent(id)}`, {}, request);
      if (currentRequest("archive-status", request) && archiveJob?.id === id) acceptArchive(result);
    } catch (error) {
      if (!currentRequest("archive-status", request) || error.name === "AbortError") return;
      if (!expired(error)) {
        if ([404, 410].includes(error.status)) {
          archiveJob.status = "failed";
          archiveJob.error = "Archive expired or is no longer available.";
          renderArchive();
          setText("archive-status", "Archive expired or is no longer available. Close this window and prepare a new ZIP.");
        } else {
          setText("archive-status", `${error.message} Check ZIP status to retry this read. Preparation is not repeated.`);
          elements["retry-archive-status"].hidden = false;
          elements["download-archive"].hidden = true;
        }
      }
    } finally { finishRequest("archive-status", request); }
  }

  async function cancelArchive() {
    if (!archiveJob || !state.authenticated || requests.has("archive-cancel")) return;
    clearTimeout(archiveTimer);
    cancelRequest("archive-status");
    cancelRequest("archives-list");
    const request = beginRequest("archive-cancel");
    let recover = false;
    try {
      const result = await api(`/api/archives/${encodeURIComponent(archiveJob.id)}/cancel`, mutationOptions({}), request);
      if (currentRequest("archive-cancel", request)) acceptArchive(result);
    } catch (error) {
      if (!currentRequest("archive-cancel", request) || error.name === "AbortError") return;
      if (!expired(error)) {
        setText("archive-status", error.message);
        recover = error.status === 0 || error.status === 404 || error.status === 410;
      }
    } finally {
      finishRequest("archive-cancel", request);
      if (recover) loadArchiveList();
    }
  }

  const iamActions = {
    users: ["user.create", "user.enable", "user.disable", "user.rotate", "user.policies", "user.delete"],
    groups: ["group.create", "group.add-members", "group.remove-members", "group.enable", "group.disable", "group.policies", "group.delete"],
    "service-accounts": ["service-account.create", "service-account.enable", "service-account.disable", "service-account.rotate", "service-account.policy", "service-account.delete"],
    policies: [],
  };
  const iamLabels = {
    "user.policies": "Change user policy bindings", "group.policies": "Change group policy bindings",
    "user.create": "Create user", "user.enable": "Enable user", "user.disable": "Disable user", "user.rotate": "Change user secret key", "user.delete": "Delete user",
    "group.create": "Create empty group", "group.add-members": "Add group members", "group.remove-members": "Remove group members", "group.enable": "Enable group", "group.disable": "Disable group", "group.delete": "Delete empty group",
    "service-account.create": "Create service account", "service-account.enable": "Enable service account", "service-account.disable": "Disable service account", "service-account.rotate": "Change service account secret key", "service-account.policy": "Change service account restrictions", "service-account.delete": "Delete service account",
  };

  function clearIAMSecret() {
    elements["iam-result-access"].value = "";
    elements["iam-result-secret"].value = "";
    elements["iam-result-secret"].type = "password";
    elements["iam-secret-result"].hidden = true;
    setText("reveal-iam-secret", "Show secret");
    if (iamInputs.secretKey) iamInputs.secretKey.value = "";
  }

  function clearStoppedCredential() {
    elements["stopped-result-access"].value = "";
    elements["stopped-result-secret"].value = "";
    elements["stopped-result-secret"].type = "password";
    setText("reveal-stopped-secret", "Show secret");
  }

  function dismissStoppedCredential() {
    clearStoppedCredential();
    if (elements["stopped-credential-dialog"].open) elements["stopped-credential-dialog"].close();
  }

  function showStoppedCredential(credentials) {
    if (!credentials || typeof credentials.accessKey !== "string" || !credentials.accessKey || typeof credentials.secretKey !== "string" || !credentials.secretKey) return;
    elements["stopped-result-access"].value = credentials.accessKey;
    elements["stopped-result-secret"].value = credentials.secretKey;
    elements["stopped-credential-dialog"].showModal();
    elements["stopped-result-secret"].focus();
  }

  function clearIAM() {
    clearIAMSecret();
    cancelRequest("iam-list");
    resetBindings();
    iamInputs = {};
    iamRecord = null;
    iamGroups = [];
    elements["iam-fields"].replaceChildren();
    elements["iam-list"].replaceChildren();
    elements["iam-confirm"].value = "";
    elements["iam-owner"].value = "";
    setText("iam-action-status", "");
    setText("iam-status", "");
  }

  function openIAM() {
    if (!featureEnabled("iam")) return;
    closeDialogs();
    elements["iam-kind"].value = "users";
    configureIAMView();
    elements["iam-dialog"].showModal();
    loadIAM();
  }

  function configureIAMView() {
    clearIAMSecret();
    iamRecord = null;
    const kind = elements["iam-kind"].value;
    elements["iam-owner-field"].hidden = kind !== "service-accounts";
    elements["iam-action"].replaceChildren();
    for (const action of iamActions[kind] || []) {
      const option = document.createElement("option");
      option.value = action;
      option.textContent = iamLabels[action];
      elements["iam-action"].append(option);
    }
    elements["iam-action"].value = iamActions[kind]?.[0] || "";
    elements["iam-action-form"].hidden = state.readOnly || !(iamActions[kind]?.length);
    elements["iam-list"].replaceChildren();
    setText("iam-action-status", "");
    configureIAMAction();
  }

  function addIAMField(name, labelText, options = {}) {
    const wrapper = document.createElement("div");
    const label = document.createElement("label");
    const input = document.createElement(options.multiline ? "textarea" : "input");
    input.id = `iam-field-${name}`;
    if (!options.multiline) input.type = options.secret ? "password" : "text";
    input.autocomplete = options.secret ? "new-password" : "off";
    input.spellcheck = false;
    input.required = options.required !== false;
    if (options.secret) { input.minLength = 8; input.maxLength = 128; input.setAttribute("placeholder", "At least 8 characters; leave blank to generate when optional"); }
    input.value = options.value || "";
    if (options.multiline) input.rows = 5;
    label.setAttribute("for", input.id);
    label.textContent = labelText;
    wrapper.append(label, input);
    elements["iam-fields"].append(wrapper);
    input.addEventListener("input", updateBusy);
    iamInputs[name] = input;
  }

  function configureIAMAction() {
    resetBindings();
    if (iamInputs.secretKey) iamInputs.secretKey.value = "";
    iamInputs = {};
    elements["iam-fields"].replaceChildren();
    elements["iam-confirm"].value = "";
    setText("iam-action-status", "");
    const action = elements["iam-action"].value;
    elements["iam-binding-controls"].hidden = !bindingAction();
    if (!action) return;
    if (action.startsWith("user.")) {
      addIAMField("user", "User access key", { value: iamRecord?.accessKey });
      if (["user.create", "user.rotate"].includes(action)) addIAMField("secretKey", action === "user.create" ? "New secret key (optional; generated if empty)" : "New secret key", { secret: true, required: action !== "user.create" });
    } else if (action.startsWith("group.")) {
      addIAMField("group", "Group name", { value: iamRecord?.name });
      if (action.endsWith("members")) addIAMField("members", "Member access keys (one per line)", { multiline: true });
    } else if (action === "service-account.create") {
      addIAMField("user", "Parent user access key", { value: elements["iam-owner"].value });
      addIAMField("accessKey", "New access key (optional)", { required: false });
      addIAMField("secretKey", "New secret key (optional; generated if empty)", { secret: true, required: false });
      addIAMField("policy", "Restriction policy JSON (optional)", { multiline: true, required: false });
    } else {
      addIAMField("accessKey", "Service account access key", { value: iamRecord?.accessKey });
      if (action === "service-account.rotate") addIAMField("secretKey", "New secret key", { secret: true });
      if (action === "service-account.policy") addIAMField("policy", "Restriction policy JSON", { multiline: true, value: typeof iamRecord?.policy === "string" ? iamRecord.policy : iamRecord?.policy ? JSON.stringify(iamRecord.policy, null, 2) : "" });
    }
    if (bindingAction()) addIAMField("policies", "Complete policy bindings (one policy name per line; empty removes all bindings)", { multiline: true, required: false });
    let help = "Confirm the exact target before applying. Changes are never retried automatically. Policy binding changes require a fresh read and support for safe concurrent updates.";
    if (action === "user.delete") help += " Remove listed service accounts and group memberships first. Current membership is checked again before deletion. Deletion revokes temporary credentials and direct policy bindings, and may revoke service credentials or memberships created concurrently.";
    if (action === "group.delete") help += " Only an empty group can be deleted.";
    if (action === "service-account.create") help += " Confirm the parent user's access key. Supply an access key when you supply a secret key; leave both blank to generate a credential. Save the returned credential before closing.";
    if (action.endsWith("rotate")) help += " Existing clients must update their credentials after the change.";
    setText("iam-action-help", help);
    updateBusy();
  }

  function iamMembershipSummary(record) {
    if (record.memberOfKnown === true && Array.isArray(record.memberOf)) return `Groups: ${record.memberOf.join(", ") || "none"}`;
    const listedGroups = iamGroups.filter(group => Array.isArray(group.members) && group.members.includes(record.accessKey)).map(group => group.name);
    return [listedGroups.length ? `Listed groups: ${listedGroups.join(", ")}` : "", "Group membership: unknown"].filter(Boolean).join(" · ");
  }

  async function loadIAM() {
    if (!featureEnabled("iam")) return;
    const kind = elements["iam-kind"].value;
    const user = elements["iam-owner"].value;
    if (kind === "groups") iamGroups = [];
    if (kind === "service-accounts" && !user) {
      setText("iam-status", "Enter an exact parent user to list its service accounts.");
      return;
    }
    const request = beginRequest("iam-list");
    setText("iam-status", "Loading access records…");
    const path = `/api/iam/${kind}${kind === "service-accounts" ? `?${new URLSearchParams({ user })}` : ""}`;
    try {
      const result = await api(path, {}, request);
      if (!currentRequest("iam-list", request)) return;
      const items = result?.[kind === "service-accounts" ? "serviceAccounts" : kind];
      if (!Array.isArray(items)) throw new APIError(0, "InvalidIAM", "The console returned invalid access records.");
      if (kind === "groups") iamGroups = items;
      elements["iam-list"].replaceChildren();
      for (const record of items) {
        const card = document.createElement("article");
        card.className = "record-card";
        const title = document.createElement("h3");
        title.setAttribute("data-no-i18n", ""); title.textContent = record.name || record.accessKey || "Unnamed record";
        card.append(title);
        const detail = document.createElement("p");
        detail.className = "subtle";
        detail.textContent = [record.status, record.parentUser ? `Parent: ${record.parentUser}` : "", record.policies?.length ? `Policies: ${record.policies.join(", ")}` : "", record.members ? `Members: ${record.members.join(", ") || "none"}` : "", kind === "users" ? iamMembershipSummary(record) : ""].filter(Boolean).join(" · ");
        card.append(detail);
        if (record.document || record.policy) {
          const policy = document.createElement("pre");
          policy.className = "scope-key setting-preview";
          const value = record.document || record.policy;
          policy.setAttribute("data-no-i18n", ""); policy.textContent = typeof value === "string" ? value : JSON.stringify(value, null, 2);
          card.append(policy);
        }
        if (!state.readOnly && kind !== "policies") card.append(actionButton("Choose for change", () => {
          iamRecord = record;
          elements["iam-action"].value = iamActions[kind][1] || iamActions[kind][0];
          configureIAMAction();
          elements["iam-action"].focus();
        }, `Choose ${title.textContent} for a change`));
        elements["iam-list"].append(card);
      }
      setText("iam-status", `${items.length} ${kind.replaceAll("-", " ")} returned. Policy bindings can be inspected through the policy binding action.`);
    } catch (error) {
      if (!currentRequest("iam-list", request) || error.name === "AbortError") return;
      if (!expired(error)) setText("iam-status", error.message);
    } finally { finishRequest("iam-list", request); }
  }

  async function applyIAMAction(event) {
    event.preventDefault();
    if (!featureEnabled("iam") || state.readOnly || requests.has("iam-action") || !elements["iam-secret-result"].hidden || !bindingReady() || pendingBindingRead) return;
    const action = elements["iam-action"].value;
    const body = { action, confirmTarget: elements["iam-confirm"].value };
    for (const [name, input] of Object.entries(iamInputs)) {
      if (name === "policies") body.policies = [...new Set(input.value.split(/\r?\n/).map(value => value.trim()).filter(Boolean))];
      else if (name === "members") body.members = input.value.split(/\r?\n/).map(value => value.trim()).filter(Boolean);
      else if (input.value || input.required) body[name] = input.value;
    }
    if (bindingAction()) body.revision = iamBinding.revision;
    if (action === "service-account.create" && body.secretKey && !body.accessKey) {
      setText("iam-action-status", "Enter an access key when supplying a secret key, or leave both blank to generate the credential.");
      return;
    }
    if (body.secretKey && (body.secretKey.length < 8 || body.secretKey.length > 128)) {
      setText("iam-action-status", "Secret key must contain 8–128 characters. Leave it blank to generate one when optional.");
      return;
    }
    const target = action === "service-account.create" ? body.user : action.startsWith("user.") ? body.user : action.startsWith("group.") ? body.group : body.accessKey;
    if (!target || body.confirmTarget !== target) {
      setText("iam-action-status", "Enter the exact target name or access key to confirm this change.");
      return;
    }
    const request = beginRequest("iam-action");
    if (iamInputs.secretKey) iamInputs.secretKey.value = "";
    setText("iam-action-status", "Applying the change…");
    try {
      const result = await api("/api/iam/actions", mutationOptions(body), request);
      if (!currentRequest("iam-action", request)) return;
      if (result?.outcome !== "confirmed") throw new APIError(0, "UnknownIAMOutcome", "The change was not confirmed. Verify storage before retrying.");
      if (result?.restartRequired) {
        consoleStopped = true;
        showLogin("The current credentials changed. Update the alias in your terminal and restart OC console before continuing.");
        showStoppedCredential(result.credentials);
        return;
      }
      if (iamInputs.secretKey) iamInputs.secretKey.value = "";
      elements["iam-confirm"].value = "";
      setText("iam-action-status", "Change confirmed by storage.");
      iamGroups = [];
      if (bindingAction()) {
        iamBindingNeedsRead = true;
        iamBinding = { ...iamBinding, policies: body.policies };
        iamInputs.policies.value = body.policies.join("\n");
        setText("iam-binding-status", "Change confirmed. Read current bindings before making another change.");
      }
      if (result.credentials?.accessKey && result.credentials?.secretKey) {
        elements["iam-result-access"].value = result.credentials.accessKey;
        elements["iam-result-secret"].value = result.credentials.secretKey;
        elements["iam-secret-result"].hidden = false;
        elements["iam-result-secret"].focus();
      }
      loadIAM();
    } catch (error) {
      if (iamInputs.secretKey) iamInputs.secretKey.value = "";
      if (!currentRequest("iam-action", request) || error.name === "AbortError") return;
      if (error.restartRequired || error.status === 0 || error.code === "outcome_unknown") {
        consoleStopped = true;
        showLogin("The credential change outcome is unconfirmed. Verify which secret works, update the alias, and restart OC console. This action will not be repeated.");
        return;
      }
      if (!expired(error)) {
        setText("iam-action-status", error.message);
        if (bindingAction()) {
          iamBindingNeedsRead = true;
          setText("iam-binding-status", "Read current bindings before another change. Your policy draft is preserved; no conflicting change is retried automatically.");
        }
      }
    } finally { finishRequest("iam-action", request); }
  }

  function bindingAction() {
    return ["user.policies", "group.policies"].includes(elements["iam-action"].value);
  }

  function bindingTarget() {
    const kind = elements["iam-action"].value.startsWith("user.") ? "user" : "group";
    return { kind, target: iamInputs[kind]?.value || "" };
  }

  function bindingReady() {
    if (!bindingAction()) return true;
    const { kind, target } = bindingTarget();
    return !!iamBinding && !iamBindingNeedsRead && iamBinding.conditional === true && /^[a-f0-9]{64}$/.test(iamBinding.revision || "") && iamBinding.kind === kind && iamBinding.target === target;
  }

  function dirtyBindings() {
    return !!iamBinding && !!iamInputs.policies && iamInputs.policies.value !== iamBinding.policies.join("\n");
  }

  function resetBindings() {
    cancelRequest("iam-bindings");
    iamBinding = null;
    pendingBindingRead = null;
    iamBindingNeedsRead = false;
    elements["iam-binding-read-confirmation"].hidden = true;
    setText("iam-binding-scope", "");
    setText("iam-binding-status", "");
  }

  function reviewBindingRead() {
    if (!bindingAction() || requests.has("iam-bindings")) return;
    const scope = bindingTarget();
    if (!scope.target) {
      setText("iam-binding-status", "Enter an exact user or group before reading its policy bindings.");
      return;
    }
    if (dirtyBindings()) {
      pendingBindingRead = scope;
      elements["iam-binding-read-confirmation"].hidden = false;
      setText("iam-binding-read-warning", `Read bindings for ${scope.kind} ${scope.target}? A successful read will replace the unsaved policy list. A failed read keeps the draft.`);
      elements["cancel-iam-bindings-read"].focus();
      updateBusy();
      return;
    }
    readIAMBindings(scope);
  }

  async function readIAMBindings(scope) {
    if (!bindingAction() || !scope.target || requests.has("iam-bindings")) return;
    pendingBindingRead = null;
    elements["iam-binding-read-confirmation"].hidden = true;
    const request = beginRequest("iam-bindings");
    setText("iam-binding-status", "Reading current policy bindings…");
    try {
      const result = await api(`/api/iam/bindings?${new URLSearchParams(scope)}`, {}, request);
      if (!currentRequest("iam-bindings", request)) return;
      if (!result || result.kind !== scope.kind || result.target !== scope.target || !Array.isArray(result.policies) || result.policies.some(policy => typeof policy !== "string") || typeof result.conditional !== "boolean" || typeof result.revision !== "string") throw new APIError(0, "InvalidBindings", "The console returned invalid policy bindings.");
      iamBinding = result;
      iamBindingNeedsRead = false;
      iamInputs.policies.value = result.policies.join("\n");
      setText("iam-binding-scope", `Loaded ${result.kind} ${result.target}`);
      setText("iam-binding-status", result.conditional && /^[a-f0-9]{64}$/.test(result.revision) ? "Edit the complete list. Saving checks that these bindings have not changed since this read." : "Current storage permits reading these bindings. Editing requires support for safe concurrent changes.");
    } catch (error) {
      if (!currentRequest("iam-bindings", request) || error.name === "AbortError") return;
      if (!expired(error)) setText("iam-binding-status", error.message);
    } finally { finishRequest("iam-bindings", request); }
  }

  elements["load-iam-bindings"].addEventListener("click", reviewBindingRead);
  elements["cancel-iam-bindings-read"].addEventListener("click", () => {
    pendingBindingRead = null;
    elements["iam-binding-read-confirmation"].hidden = true;
    updateBusy();
  });
  elements["confirm-iam-bindings-read"].addEventListener("click", () => { if (pendingBindingRead) readIAMBindings({ ...pendingBindingRead }); });

  elements["open-create-bucket"].addEventListener("click", () => openBucketAction(false));
  elements["open-delete-bucket"].addEventListener("click", () => openBucketAction(true));
  elements["bucket-action-form"].addEventListener("submit", submitBucketAction);
  elements["close-bucket-action"].addEventListener("click", () => elements["bucket-dialog"].close());
  elements["bucket-dialog"].addEventListener("cancel", event => { if (requests.has("bucket-action")) event.preventDefault(); });
  elements["open-versions"].addEventListener("click", () => openVersions(state.bucket));
  elements["close-versions"].addEventListener("click", () => { cancelRequest("versions"); elements["versions-dialog"].close(); });
  elements["versions-form"].addEventListener("submit", event => { event.preventDefault(); readVersions(false); });
  elements["more-versions"].addEventListener("click", () => readVersions(true));
  elements["share-form"].addEventListener("submit", createShare);
  elements["copy-share"].addEventListener("click", copyShare);
  elements["close-share"].addEventListener("click", () => { clearShare(); elements["share-dialog"].close(); });
  elements["share-dialog"].addEventListener("cancel", event => { event.preventDefault(); clearShare(); elements["share-dialog"].close(); });
  elements["archive-selected"].addEventListener("click", () => openArchive({ refs: [...selectedKeys].map(key => ({ bucket: state.bucket, key })) }));
  elements["archive-prefix"].addEventListener("click", () => openArchive({ bucket: state.bucket, prefix: state.prefix }));
  elements["start-archive"].addEventListener("click", startArchive);
  elements["retry-archive-status"].addEventListener("click", pollArchive);
  elements["cancel-archive"].addEventListener("click", cancelArchive);
  elements["download-archive"].addEventListener("click", event => { if (archiveJob?.status === "ready") prepareDownload(event, elements["download-archive"], "", "", undefined, `/api/archives/${encodeURIComponent(archiveJob.id)}/download`); else event.preventDefault(); });
  elements["open-current-archive"].addEventListener("click", () => { closeDialogs(); elements["archive-dialog"].showModal(); });
  elements["close-archive"].addEventListener("click", () => elements["archive-dialog"].close());
  elements["archive-dialog"].addEventListener("cancel", event => { event.preventDefault(); elements["archive-dialog"].close(); });
  elements["open-iam"].addEventListener("click", openIAM);
  elements["close-iam"].addEventListener("click", () => { clearIAM(); elements["iam-dialog"].close(); });
  elements["iam-dialog"].addEventListener("cancel", event => { event.preventDefault(); if (!requests.has("iam-action")) { clearIAM(); elements["iam-dialog"].close(); } });
  elements["iam-kind"].addEventListener("change", () => { cancelRequest("iam-list"); configureIAMView(); loadIAM(); });
  elements["iam-action"].addEventListener("change", configureIAMAction);
  elements["iam-list-form"].addEventListener("submit", event => { event.preventDefault(); loadIAM(); });
  elements["iam-action-form"].addEventListener("submit", applyIAMAction);
  elements["dismiss-iam-secret"].addEventListener("click", () => { clearIAMSecret(); updateBusy(); });
  elements["reveal-iam-secret"].addEventListener("click", () => {
    elements["iam-result-secret"].type = elements["iam-result-secret"].type === "password" ? "text" : "password";
    setText("reveal-iam-secret", elements["iam-result-secret"].type === "password" ? "Show secret" : "Hide secret");
  });
  elements["close-stopped-credential"].addEventListener("click", dismissStoppedCredential);
  elements["dismiss-stopped-secret"].addEventListener("click", dismissStoppedCredential);
  elements["stopped-credential-dialog"].addEventListener("cancel", event => { event.preventDefault(); dismissStoppedCredential(); });
  elements["stopped-credential-dialog"].addEventListener("close", clearStoppedCredential);
  elements["reveal-stopped-secret"].addEventListener("click", () => {
    elements["stopped-result-secret"].type = elements["stopped-result-secret"].type === "password" ? "text" : "password";
    setText("reveal-stopped-secret", elements["stopped-result-secret"].type === "password" ? "Show secret" : "Hide secret");
  });

  elements["settings-bucket"].addEventListener("change", () => loadCapabilities(elements["settings-bucket"].value));
  elements["open-settings"].addEventListener("click", openSettings);
  elements["close-settings"].addEventListener("click", closeSettings);
  elements["settings-dialog"].addEventListener("cancel", event => { event.preventDefault(); closeSettings(); });
  elements["settings-load-form"].addEventListener("submit", event => { event.preventDefault(); readSetting(); });
  function backSettingRead() {
    pendingSettingRead = null;
    if (elements["settings-read-dialog"].open) elements["settings-read-dialog"].close();
    if (!state.authenticated) return;
    elements["settings-dialog"].showModal();
    elements["settings-document"].focus();
    updateBusy();
  }
  elements["back-setting-read"].addEventListener("click", backSettingRead);
  elements["settings-read-dialog"].addEventListener("cancel", event => { event.preventDefault(); backSettingRead(); });
  elements["confirm-setting-read"].addEventListener("click", () => {
    if (!pendingSettingRead || !state.authenticated || requests.has("setting-read")) return;
    const target = pendingSettingRead;
    pendingSettingRead = null;
    elements["settings-read-dialog"].close();
    elements["settings-dialog"].showModal();
    readSetting(target);
  });
  elements["save-setting"].addEventListener("click", () => reviewSetting(false));
  elements["remove-setting"].addEventListener("click", () => reviewSetting(true));
  elements["back-settings"].addEventListener("click", backSettings);
  elements["settings-review-dialog"].addEventListener("cancel", event => { event.preventDefault(); backSettings(); });
  elements["confirm-setting"].addEventListener("click", saveSetting);
  elements["open-account"].addEventListener("click", openAccount);
  elements["close-account"].addEventListener("click", closeAccount);
  elements["account-dialog"].addEventListener("cancel", event => { event.preventDefault(); closeAccount(); });
  elements["account-dialog"].addEventListener("close", clearSecret);
  elements["review-secret"].addEventListener("click", () => {
    if (!canRotateSecret() || !validNewSecret()) return;
    setText("self-secret-status", "");
    elements["secret-confirmation"].hidden = false;
    elements["new-secret"].disabled = true;
    elements["review-secret"].hidden = true;
    elements["cancel-secret"].focus();
  });
  elements["cancel-secret"].addEventListener("click", () => {
    elements["secret-confirmation"].hidden = true;
    elements["new-secret"].disabled = false;
    elements["review-secret"].hidden = false;
    elements["new-secret"].focus();
  });
  elements["self-secret-form"].addEventListener("submit", rotateSecret);

  elements["login-form"].addEventListener("submit", async event => {
    event.preventDefault();
    if (consoleStopped) return;
    const code = elements["login-code"].value.trim();
    if (!code) return;
    const request = beginRequest("login");
    elements["login-code"].value = "";
    setText("login-status", "Connecting…");
    try {
      const session = await api("/api/login", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ code }) }, request);
      if (currentRequest("login", request)) acceptSession(session);
    } catch (error) {
      if (!currentRequest("login", request) || error.name === "AbortError") return;
      setText("login-status", error.message);
      elements["login-code"].focus();
    } finally {
      finishRequest("login", request);
    }
  });

  elements.logout.addEventListener("click", async () => {
    clearSecret();
    dismissStoppedCredential();
    if (consoleStopped) return;
    const request = beginRequest("logout");
    try {
      await api("/api/logout", { method: "POST", headers: { "X-CSRF-Token": state.csrfToken } }, request);
      if (!currentRequest("logout", request)) return;
      showLogin("You are logged out. Use the code from your OC terminal to reconnect.");
      elements["login-code"].focus();
    } catch (error) {
      if (!currentRequest("logout", request) || error.name === "AbortError") return;
      if (expired(error)) return;
      elements["objects-status"].classList.add("error");
      setText("objects-status", error.message);
    } finally {
      finishRequest("logout", request);
    }
  });

  elements.refresh.addEventListener("click", () => { if (!state.authenticated || consoleStopped) return; preferencesReady = preferenceQueue.catch(() => {}).then(loadPreferences); refreshAll(); });
  elements["prefix-form"].addEventListener("submit", event => {
    event.preventDefault();
    selectLocation(state.bucket, elements["prefix-input"].value);
  });
  elements["parent-prefix"].addEventListener("click", () => selectLocation(state.bucket, parentPrefix(state.prefix)));
  elements["root-prefix"].addEventListener("click", () => selectLocation(state.bucket, ""));
  elements["load-more"].addEventListener("click", () => loadObjects(true));
  elements["objects-retry"].addEventListener("click", () => loadObjects(false));
  elements["open-upload"].addEventListener("click", openUpload);
  elements["close-upload"].addEventListener("click", closeUpload);
  elements["upload-dialog"].addEventListener("cancel", event => { event.preventDefault(); closeUpload(); });
  elements["upload-file"].addEventListener("change", () => {
    const file = elements["upload-file"].files[0];
    renderUploadSelection();
    if (!file || !uploadLocation) return;
    const input = elements["upload-key"];
    if (input.value === "" || input.value === uploadLocation.prefix || input.value === suggestedUploadKey) {
      suggestedUploadKey = `${uploadLocation.prefix}${file.name}`;
      input.value = suggestedUploadKey;
    }
    setText("upload-status", `${formatSize(file.size)} selected. Maximum file size: ${formatSize(state.maxUploadSize)}.`);
  });
  elements["remove-upload-file"].addEventListener("click", () => {
    if (requests.has("upload-prepare")) return;
    elements["upload-file"].value = "";
    if (elements["upload-key"].value === suggestedUploadKey) elements["upload-key"].value = uploadLocation?.prefix || "";
    suggestedUploadKey = "";
    renderUploadSelection();
    setText("upload-status", "已移除待上传文件，请重新选择。");
    elements["upload-file"].focus();
  });
  elements["upload-form"].addEventListener("submit", event => {
    event.preventDefault();
    if (state.readOnly || requests.has("upload-prepare")) return;
    const file = elements["upload-file"].files[0];
    const bucket = elements["upload-bucket"].value;
    const key = elements["upload-key"].value;
    if (!file || !bucket || !key) {
      setText("upload-status", "Choose a file and enter an exact bucket and full object key.");
      return;
    }
    if (file.size > state.maxUploadSize) {
      setText("upload-status", `The selected file exceeds this console’s ${formatSize(state.maxUploadSize)} upload limit.`);
      return;
    }
    uploadDraft = { bucket, key, file, overwrite: elements["upload-overwrite"].checked };
    if (uploadDraft.overwrite) {
      setText("replace-bucket", bucket);
      setText("replace-key", key);
      setText("replace-status", "");
      elements["upload-dialog"].close();
      elements["replace-dialog"].showModal();
      elements["close-replace"].focus();
    } else {
      startUpload(uploadDraft);
    }
  });
  function backToUpload() {
    if (requests.has("upload-prepare")) {
      cancelRequest("upload-prepare");
      setText("upload-status", "Upload preparation was stopped before any file bytes were sent.");
    }
    uploadDraft = null;
    elements["replace-dialog"].close();
    elements["upload-dialog"].showModal();
    elements["upload-submit"].focus();
    updateBusy();
  }
  elements["close-replace"].addEventListener("click", backToUpload);
  elements["replace-dialog"].addEventListener("cancel", event => { event.preventDefault(); backToUpload(); });
  elements["confirm-replace"].addEventListener("click", () => { if (uploadDraft) startUpload(uploadDraft); });
  elements["select-loaded"].addEventListener("change", () => {
    selectedKeys.clear();
    if (elements["select-loaded"].checked) {
      for (const entry of state.entries) if (!entry.isPrefix) selectedKeys.add(entry.key);
    }
    renderEntries();
  });
  elements["delete-selected"].addEventListener("click", () => {
    if (selectedKeys.size) prepareDeletion({ bucket: state.bucket, keys: [...selectedKeys] });
  });
  elements["delete-prefix"].addEventListener("click", () => {
    if (state.bucket && state.prefix) prepareDeletion({ bucket: state.bucket, prefix: state.prefix });
  });
  elements["open-exact-delete"].addEventListener("click", () => {
    if (state.readOnly) return;
    elements["exact-delete-form"].reset();
    elements["exact-delete-bucket"].value = state.bucket;
    elements["exact-delete-dialog"].showModal();
    elements[state.bucket ? "exact-delete-key" : "exact-delete-bucket"].focus();
  });
  elements["close-exact-delete"].addEventListener("click", () => elements["exact-delete-dialog"].close());
  elements["exact-delete-form"].addEventListener("submit", event => {
    event.preventDefault();
    const bucket = elements["exact-delete-bucket"].value;
    const key = elements["exact-delete-key"].value;
    if (!bucket || !key || state.readOnly) return;
    elements["exact-delete-dialog"].close();
    prepareDeletion({ bucket, keys: [key] });
  });
  elements["close-delete"].addEventListener("click", closeDelete);
  elements["delete-dialog"].addEventListener("cancel", event => { event.preventDefault(); closeDelete(); });
  elements["confirm-delete"].addEventListener("click", executeDeletion);
  elements["clear-finished"].addEventListener("click", () => {
    for (const [id, task] of tasks) {
      if ((task.expired || terminalStates.has(task.job.status)) && !task.xhr && !task.canceling && !task.refreshing) {
        tasks.delete(id);
        clearedTasks.add(id);
      }
    }
    renderTasks();
    schedulePoll();
  });

  elements["refresh-object-info"].addEventListener("click", refreshObjectInfo);
  elements["info-preview"].addEventListener("click", () => { if (infoObject) openImagePreview(infoObject.bucket, infoObject.entry); });
  elements["info-share"].addEventListener("click", () => { if (infoObject) openShare({ bucket: infoObject.bucket, key: infoObject.entry.key }); });
  const closeImagePreview = () => { clearPreview(); elements["image-preview-dialog"].close(); };
  elements["close-image-preview"].addEventListener("click", closeImagePreview);
  elements["image-preview-dialog"].addEventListener("cancel", event => { event.preventDefault(); closeImagePreview(); });
  elements["image-preview"].addEventListener("error", () => { if (previewURL) { clearPreview(); setText("image-preview-status", "图片格式无法解码，请下载查看。"); } });
  elements["preview-zoom-in"].addEventListener("click", () => zoomPreview(previewZoom + .25));
  elements["preview-zoom-out"].addEventListener("click", () => zoomPreview(previewZoom - .25));
  elements["preview-zoom-reset"].addEventListener("click", () => zoomPreview(1));
  window.addEventListener("oc-language-change", event => { if (state.authenticated) { renderOverview(); renderEntries(); renderLocation(); savePreference({ action: "language", language: event.detail }); } });
  elements["nav-favorites"].addEventListener("click", () => showPage("favorites"));
  elements["close-object-info"].addEventListener("click", () => { cancelRequest("object-info"); elements["object-info-dialog"].close(); });
  elements["copy-object-link"].addEventListener("click", copyObjectLink);
  elements["bucket-search"].addEventListener("input", renderDirectory);
  elements["directory-refresh"].addEventListener("click", refreshAll);
  elements["directory-account"].addEventListener("click", openAccount);
  elements["directory-create"].addEventListener("click", () => openBucketAction(false));
  elements["back-to-buckets"].addEventListener("click", () => showPage("buckets"));
  elements["bucket-files-tab"].addEventListener("click", () => showPage("objects"));
  elements["bucket-settings-tab"].addEventListener("click", openSettings);
  elements["bucket-refresh"].addEventListener("click", () => loadObjects(false));
  elements["nav-overview"].addEventListener("click", () => showPage("overview"));
  const browsePage = () => {
    showPage("buckets");
    renderOverview();
  };
  elements["nav-buckets"].addEventListener("click", browsePage);
  elements["overview-browse"].addEventListener("click", browsePage);
  elements["nav-tasks"].addEventListener("click", () => showPage("tasks"));
  elements["nav-account"].addEventListener("click", openAccount);
  elements["toggle-navigation"].addEventListener("click", () => {
    const collapsed = elements["console-shell"].classList.toggle("navigation-collapsed");
    elements["toggle-navigation"].setAttribute("aria-expanded", String(!collapsed));
  });

  async function restoreSession() {
    if (consoleStopped) return;
    showLogin();
    const request = beginRequest("session");
    try {
      const session = await api("/api/session", {}, request);
      if (currentRequest("session", request)) acceptSession(session);
    } catch (error) {
      if (!currentRequest("session", request) || error.name === "AbortError") return;
      if (error.status !== 401) setText("login-status", error.message);
    } finally {
      finishRequest("session", request);
    }
  }

  window.addEventListener("pagehide", () => {
    pageSuspended = true;
    clearSecret();
    clearShare();
    clearIAMSecret();
    dismissStoppedCredential();
    if (rotationInFlight) stopAfterRotation(false);
    else abortAll();
  });
  window.addEventListener("pageshow", event => {
    pageSuspended = false;
    if (event.persisted) restoreSession();
  });
  restoreSession();
})();
