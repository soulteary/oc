"use strict";

(() => {
  const ids = [
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
  ];
  const elements = Object.fromEntries(ids.map(id => [id, document.getElementById(id)]));
  const requests = new Map();
  const state = {
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
  const dateFormatter = new Intl.DateTimeFormat(undefined, { year: "numeric", month: "short", day: "numeric" });

  class APIError extends Error {
    constructor(status, code, message) {
      super(message);
      this.status = status;
      this.code = code;
    }
  }

  function setText(id, value) {
    elements[id].textContent = value;
  }

  function updateBusy() {
    elements["login-submit"].disabled = requests.has("login") || requests.has("session");
    // Refresh is also the way to stop and restart a slow read request.
    elements.refresh.disabled = !state.authenticated || requests.has("logout");
    elements.logout.disabled = requests.has("logout");
    elements["load-more"].disabled = requests.has("objects");
    setText("load-more", requests.has("objects") ? "Loading…" : "Load more");
    const preparing = requests.has("upload-prepare");
    elements["upload-submit"].disabled = preparing;
    elements["confirm-replace"].disabled = preparing;
    elements["confirm-delete"].disabled = !deletePlan || !deletePlan.confirmToken || deletePlan.job.status !== "ready" || requests.has("delete-execute");
    updateSelection();
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
      throw new APIError(response.status, data?.code || "RequestFailed", data?.message || "The request could not be completed.");
    }
    return data;
  }

  function showLogin(message = "") {
    const interrupted = [...tasks.values()].some(task => !task.expired && !terminalStates.has(task.job.status));
    state.epoch += 1;
    abortAll();
    Object.assign(state, {
      authenticated: false, readOnly: true, alias: "", csrfToken: "", buckets: [], bucket: "", prefix: "",
      entries: [], nextCursor: "", account: null, accountMessage: "Permission information is loading…",
    });
    selectedKeys.clear();
    tasks.clear();
    clearedTasks.clear();
    uploadDraft = null;
    uploadLocation = null;
    deletePlan = null;
    jobsMessage = "";
    closeDialogs();
    elements["upload-form"].reset();
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
    renderMode();
    renderLocation();
    refreshAll();
    if (!state.readOnly) loadJobs();
  }

  function renderMode() {
    setText("session-mode", state.readOnly ? "● Read-only" : "● Writes enabled");
    elements["session-mode"].classList.toggle("writes-enabled", !state.readOnly);
    elements["write-toolbar"].hidden = state.readOnly;
    elements["selection-header"].hidden = state.readOnly;
    elements["objects-body"].closest("table").classList.toggle("writable", !state.readOnly);
    setText("workspace-footnote", `${state.readOnly ? "Read-only browsing." : "Writes enabled by the local OC process."} Downloads use your browser’s download manager. Download errors open in a separate tab.`);
  }

  function formatSize(size) {
    if (!Number.isFinite(size) || size < 0) return "—";
    if (size === 0) return "0 B";
    const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"];
    const unit = Math.min(Math.floor(Math.log(size) / Math.log(1024)), units.length - 1);
    return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: unit === 0 ? 0 : 1 }).format(size / (1024 ** unit))} ${units[unit]}`;
  }

  function formatDate(value) {
    const date = new Date(value);
    if (!value || !Number.isFinite(date.getTime()) || date.getUTCFullYear() < 1970) return "—";
    return dateFormatter.format(date);
  }

  function renderBuckets() {
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
      label.textContent = bucket.name;
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
      if (!state.readOnly) {
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
      const name = document.createElement(entry.isPrefix ? "button" : "span");
      name.className = entry.isPrefix ? "entry-name prefix-button" : "entry-name";
      const symbol = document.createElement("span");
      symbol.className = "entry-symbol";
      symbol.setAttribute("aria-hidden", "true");
      symbol.textContent = entry.isPrefix ? "▱" : "·";
      const label = document.createElement("span");
      label.className = "entry-label";
      label.textContent = entry.key.startsWith(state.prefix) ? entry.key.slice(state.prefix.length) || entry.key : entry.key;
      name.append(symbol, label);
      if (entry.isPrefix) {
        name.type = "button";
        name.addEventListener("click", () => selectLocation(state.bucket, entry.key));
      }
      nameCell.append(name);
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
        download.textContent = "Download ↓";
        download.setAttribute("aria-label", `Download ${entry.key}`);
        download.target = "_blank";
        download.rel = "noopener";
        const bucket = state.bucket;
        download.addEventListener("click", event => prepareDownload(event, download, bucket, entry.key));
        actions.append(download);
        if (!state.readOnly) {
          const remove = document.createElement("button");
          remove.type = "button";
          remove.className = "row-delete";
          remove.textContent = "Delete";
          remove.setAttribute("aria-label", `Review deletion of ${entry.key}`);
          remove.addEventListener("click", () => prepareDeletion({ bucket, keys: [entry.key] }));
          actions.append(remove);
        }
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
    elements["select-loaded"].disabled = !writable || keys.length === 0;
    elements["select-loaded"].checked = keys.length > 0 && keys.every(key => selectedKeys.has(key));
    elements["select-loaded"].indeterminate = selectedKeys.size > 0 && !elements["select-loaded"].checked;
  }

  function selectLocation(bucket, prefix) {
    if (!state.authenticated) return;
    cancelRequest("objects");
    state.bucket = bucket;
    state.prefix = prefix;
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
    const request = beginRequest("buckets");
    setText("bucket-status", "Loading buckets…");
    try {
      const result = await api("/api/buckets", {}, request);
      if (!currentRequest("buckets", request)) return;
      if (!result || !Array.isArray(result.buckets) || result.buckets.some(bucket => !bucket || typeof bucket.name !== "string")) {
        throw new APIError(0, "InvalidBuckets", "The local console returned an invalid bucket list.");
      }
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
    if (!state.readOnly) loadJobs();
  }

  function mutationOptions(body) {
    return { method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": state.csrfToken }, body: JSON.stringify(body) };
  }

  function closeDialogs() {
    for (const id of ["upload-dialog", "replace-dialog", "delete-dialog", "exact-delete-dialog"]) {
      if (elements[id].open) elements[id].close();
    }
  }

  let downloadSequence = 0;
  async function prepareDownload(event, link, bucket, key) {
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
      downloadWindow.location.replace(new URL(`/api/download?${parameters.toString()}`, window.location.origin).href);
      setText("objects-status", "Download requested. If the object is unavailable or access is denied, its error appears in the new tab.");
    } catch (error) {
      if (downloadWindow) downloadWindow.close();
      if (!currentRequest(name, request) || error.name === "AbortError") return;
      if (expired(error)) return;
      elements["objects-status"].classList.add("error");
      setText("objects-status", error.message);
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
      scope.textContent = `${job.bucket}\n${job.kind === "upload" ? job.key || "" : job.prefix ? `Prefix: ${job.prefix}` : `${job.count} explicitly selected object keys`}`;
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
          line.textContent = `${item.key} — ${item.status}${item.status === "unknown" ? "; result is unconfirmed, check storage before retrying" : ""}${item.error?.message ? `: ${item.error.message}` : ""}`;
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

  function openUpload() {
    if (state.readOnly) return;
    uploadLocation = { bucket: state.bucket, prefix: state.prefix };
    uploadDraft = null;
    suggestedUploadKey = "";
    elements["upload-form"].reset();
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
      line.textContent = item.key;
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

  elements["login-form"].addEventListener("submit", async event => {
    event.preventDefault();
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

  elements.refresh.addEventListener("click", refreshAll);
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
    if (!file || !uploadLocation) return;
    const input = elements["upload-key"];
    if (input.value === "" || input.value === uploadLocation.prefix || input.value === suggestedUploadKey) {
      suggestedUploadKey = `${uploadLocation.prefix}${file.name}`;
      input.value = suggestedUploadKey;
    }
    setText("upload-status", `${formatSize(file.size)} selected. Maximum file size: ${formatSize(state.maxUploadSize)}.`);
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

  async function restoreSession() {
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

  window.addEventListener("pagehide", () => { pageSuspended = true; abortAll(); });
  window.addEventListener("pageshow", event => {
    pageSuspended = false;
    if (event.persisted) restoreSession();
  });
  restoreSession();
})();
