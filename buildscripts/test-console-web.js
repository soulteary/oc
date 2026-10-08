#!/usr/bin/env node
// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
// Observable DOM/fetch regressions for the embedded console. This harness uses
// Node built-ins only; real browser layout and storage contracts run separately.
"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { TextEncoder } = require("node:util");

const web = path.join(__dirname, "../internal/console/web");
const html = fs.readFileSync(path.join(web, "index.html"), "utf8");
const source = fs.readFileSync(path.join(web, "app.js"), "utf8");
const tests = [];
const test = (name, run) => tests.push({ name, run });

function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function response(data, status = 200) {
  return { status, ok: status >= 200 && status < 300, json: async () => data };
}

function setting(bucket, kind, options = {}) {
  return {
    bucket, kind, format: kind === "policy" ? "json" : "xml",
    document: kind === "policy" ? '{"Version":"2012-10-17","Statement":[]}' : kind === "versioning" ? "<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>" : "<LifecycleConfiguration><Rule><ID>existing</ID></Rule></LifecycleConfiguration>",
    revision: "a".repeat(64), exists: true, conditional: true, ...options,
  };
}

async function settle() {
  for (let i = 0; i < 8; i++) await new Promise(resolve => setImmediate(resolve));
}

function createHarness(options = {}) {
  const elements = new Map();
  const calls = [];
  const transfers = [];
  const routes = [];
  const timers = new Map();
  const windowListeners = new Map();
  const asynchronousErrors = [];
  let nextTimer = 0;
  let document;
  class Element {
    constructor(tagName, id = "") {
      this.tagName = tagName;
      this.id = id;
      this.children = [];
      this.listeners = new Map();
      this.dataset = {};
      this.attributes = new Map();
      this.hidden = false;
      this.disabled = false;
      this.readOnly = false;
      this.open = false;
      this.value = "";
      this.files = [];
      this.checked = false;
      this._text = "";
      const classes = new Set();
      this.classList = {
        add: (...items) => items.forEach(item => classes.add(item)),
        remove: (...items) => items.forEach(item => classes.delete(item)),
        toggle: (item, force) => {
          const enabled = force === undefined ? !classes.has(item) : force;
          if (enabled) classes.add(item); else classes.delete(item);
          return enabled;
        },
        contains: item => classes.has(item),
      };
    }
    set textContent(value) { this._text = String(value); this.children = []; }
    get textContent() { return this._text + this.children.map(child => typeof child === "string" ? child : child.textContent).join(""); }
    append(...children) { this.children.push(...children); }
    replaceChildren(...children) { this._text = ""; this.children = [...children]; }
    addEventListener(type, callback) {
      if (!this.listeners.has(type)) this.listeners.set(type, []);
      this.listeners.get(type).push(callback);
    }
    setAttribute(name, value) { this.attributes.set(name, String(value)); }
    getAttribute(name) { return this.attributes.get(name) ?? null; }
    closest() { return table; }
    focus() { if (!this.disabled) document.activeElement = this; }
    showModal() {
      assert.equal(this.open, false, `dialog ${this.id} reopened while already open`);
      assert.equal([...elements.values()].filter(element => element.tagName === "dialog" && element.open).length, 0, `another modal is open when ${this.id} opens`);
      this.open = true;
    }
    close() { this.open = false; }
    reset() {
      const match = html.match(new RegExp(`<form[^>]*id="${this.id}"[\\s\\S]*?<\\/form>`));
      for (const id of match?.[0].matchAll(/id="([^"]+)"/g) || []) {
        const field = elements.get(id[1]);
        if (field && field !== this) { field.value = ""; field.checked = false; field.files = []; }
      }
    }
  }
  const table = new Element("table");
  for (const match of html.matchAll(/<([a-z][a-z0-9]*)\b([^>]*\bid="([^"]+)"[^>]*)>/g)) {
    assert.equal(elements.has(match[3]), false, `duplicate HTML id ${match[3]}`);
    const element = new Element(match[1], match[3]);
    element.hidden = /\bhidden(?:\s|>|$)/.test(match[2]);
    element.disabled = /\bdisabled(?:\s|>|$)/.test(match[2]);
    elements.set(match[3], element);
  }
  elements.get("settings-kind").value = "policy";
  document = {
    activeElement: null,
    getElementById(id) { assert.ok(elements.has(id), `script references missing HTML id ${id}`); return elements.get(id); },
    createElement(tagName) { return new Element(tagName); },
  };
  const window = {
    addEventListener(type, callback) {
      if (!windowListeners.has(type)) windowListeners.set(type, []);
      windowListeners.get(type).push(callback);
    },
    open() { return { close() {}, location: "", opener: null }; },
  };
  class XMLHttpRequest {
    constructor() {
      this.listeners = new Map();
      this.upload = new Element("upload");
      this.headers = {};
      transfers.push(this);
    }
    open(method, url) { this.method = method; this.url = url; }
    setRequestHeader(name, value) { this.headers[name] = value; }
    addEventListener(type, callback) {
      if (!this.listeners.has(type)) this.listeners.set(type, []);
      this.listeners.get(type).push(callback);
    }
    send(body) { this.body = body; }
    emit(type, event = {}) { for (const callback of this.listeners.get(type) || []) callback(event); }
    abort() { this.aborted = true; this.emit("abort"); }
    progress(loaded, total) { for (const callback of this.upload.listeners.get("progress") || []) callback({ lengthComputable: true, loaded, total }); }
  }
  const context = vm.createContext({
    document, window, URLSearchParams, AbortController, TextEncoder, XMLHttpRequest,
    setTimeout(callback) { timers.set(++nextTimer, callback); return nextTimer; },
    clearTimeout(id) { timers.delete(id); },
    fetch: async (url, init = {}) => {
      const call = { url: String(url), method: init.method || "GET", body: init.body, headers: init.headers, signal: init.signal, redirect: init.redirect };
      calls.push(call);
      for (const route of [...routes].reverse()) {
        if (route.method === call.method && route.match(call.url)) return await route.handle(call);
      }
      if (url === "/api/session") return response({ alias: "selected", csrfToken: "csrf-only", readOnly: !!options.readOnly, maxUploadSize: 1024 ** 3 });
      if (url === "/api/buckets") return options.listDenied ? response({ code: "AccessDenied", message: "Bucket listing denied." }, 403) : response({ buckets: [{ name: "listed-bucket", created: "2026-10-08T00:00:00Z" }] });
      if (String(url).startsWith("/api/objects?")) return response({ entries: [] });
      if (url === "/api/account") return response({ buckets: [{ name: "listed-bucket", size: 42, read: false, write: false }] });
      if (url === "/api/jobs") return response({ jobs: [] });
      if (url === "/api/self-account") return response({ kind: "iam", status: "enabled", canRotateSecret: true, ...options.selfAccount });
      if (url === "/api/logout") return response(null, 204);
      if (String(url).startsWith("/api/bucket-settings?")) {
        const parameters = new URLSearchParams(String(url).split("?")[1]);
        return response(setting(parameters.get("bucket"), parameters.get("kind")));
      }
      throw new Error(`unexpected fetch ${call.method} ${url}`);
    },
  });
  // A storage call is a regression: the app must not use persistence for any
  // credential, login code, CSRF token, draft, or confirmation capability.
  Object.defineProperty(context, "localStorage", { get() { throw new Error("localStorage access is forbidden"); } });
  Object.defineProperty(context, "sessionStorage", { get() { throw new Error("sessionStorage access is forbidden"); } });
  vm.runInContext(source, context, { filename: "app.js" });
  return {
    elements, calls, timers, transfers,
    element(id) { assert.ok(elements.has(id)); return elements.get(id); },
    activeElement() { return document.activeElement; },
    route(method, match, handle) { routes.push({ method, match: typeof match === "string" ? url => url === match : match, handle }); },
    async fire(id, type = "click", { force = false } = {}) {
      const element = elements.get(id);
      assert.ok(element, id);
      if (!force) assert.equal(element.disabled, false, `disabled element ${id} invoked`);
      for (const callback of element.listeners.get(type) || []) {
        const result = callback({ preventDefault() {} });
        if (result?.then) result.catch(error => asynchronousErrors.push(error));
      }
      await settle();
      assert.deepEqual(asynchronousErrors, []);
    },
    async windowEvent(type, data = {}) {
      for (const callback of windowListeners.get(type) || []) await callback(data);
      await settle();
    },
    async ready() { await settle(); assert.equal(elements.get("workspace").hidden, false); },
  };
}

async function readSetting(harness, bucket = "known-bucket", kind = "policy") {
  if (!harness.element("settings-dialog").open) await harness.fire("open-settings");
  harness.element("settings-bucket").value = bucket;
  harness.element("settings-kind").value = kind;
  await harness.fire("settings-load-form", "submit");
  if (harness.element("settings-read-dialog").open) await harness.fire("confirm-setting-read");
}

async function reviewSecret(harness, secret = "new-owned-secret") {
  await harness.fire("open-account");
  harness.element("new-secret").value = secret;
  await harness.fire("review-secret");
}

function job(kind, options = {}) {
  return { id: "owned-task", kind, status: kind === "upload" ? "waiting" : "ready", bucket: "listed-bucket", key: "exact/目标 +%.txt", size: 19, transferred: 0, overwrite: false, created: new Date().toISOString(), expires: new Date(Date.now() + 600000).toISOString(), count: 0, completed: 0, ...options };
}

test("P2 raw file upload sends original File and 100% progress does not confirm storage", async () => {
  const h = createHarness();
  h.route("POST", "/api/uploads", call => response(job("upload", JSON.parse(call.body))));
  await h.ready();
  await h.fire("open-upload");
  const file = { name: "fixture.txt", size: 19, type: "text/plain" };
  h.element("upload-file").files = [file];
  h.element("upload-key").value = "exact/目标 +%.txt";
  await h.fire("upload-form", "submit");
  assert.equal(h.transfers.length, 1);
  const xhr = h.transfers[0];
  assert.equal(xhr.body, file);
  assert.equal(xhr.headers["Content-Type"], "application/octet-stream");
  assert.equal(xhr.headers["X-CSRF-Token"], "csrf-only");
  xhr.progress(19, 19);
  assert.match(h.element("tasks-list").textContent, /awaiting storage result/);
  assert.doesNotMatch(h.element("tasks-list").textContent, /Succeeded/);
  xhr.status = 502;
  xhr.responseText = JSON.stringify({ code: "outcome_unknown", message: "Storage outcome is unknown." });
  xhr.emit("load");
  assert.match(h.element("tasks-list").textContent, /may have been saved/);
  assert.equal(h.calls.filter(call => call.url === "/api/uploads").length, 1);
});

test("P2 empty prefix plans are reviewable and complete without object deletion", async () => {
  const h = createHarness();
  const plan = job("delete", { prefix: "empty/", key: undefined, size: 0, confirmToken: "one-use-token", items: undefined });
  h.route("POST", "/api/deletions/plan", () => response(plan));
  h.route("POST", "/api/deletions/owned-task/execute", () => response({ ...plan, status: "succeeded", confirmToken: undefined }));
  await h.ready();
  h.element("prefix-input").value = "empty/";
  await h.fire("prefix-form", "submit");
  await h.fire("delete-prefix");
  assert.match(h.element("confirm-delete").textContent, /Complete empty plan/);
  assert.equal(h.element("confirm-delete").disabled, false);
  assert.match(h.element("delete-status").textContent, /without deleting anything/);
  await h.fire("confirm-delete");
  assert.match(h.element("tasks-list").textContent, /Succeeded/);
});

test("P2 only explicit busy rejection permits reuse of a deletion confirmation", async () => {
  for (const [status, code] of [[429, "busy"], [502, "outcome_unknown"]]) {
    const h = createHarness();
    const plan = job("delete", { count: 1, key: undefined, size: 0, confirmToken: "one-use-token", items: [{ key: "exact-key", status: "pending" }] });
    h.route("POST", "/api/deletions/plan", () => response(plan));
    h.route("POST", "/api/deletions/owned-task/execute", () => response({ code, message: "Safe error." }, status));
    h.route("GET", "/api/jobs/owned-task", () => response({ ...plan, confirmToken: undefined, status: "running" }));
    await h.ready();
    await h.fire("open-exact-delete");
    h.element("exact-delete-key").value = "exact-key";
    await h.fire("exact-delete-form", "submit");
    await h.fire("confirm-delete");
    assert.equal(h.element("confirm-delete").disabled, code !== "busy");
    await h.fire("confirm-delete", "click", { force: true });
    assert.equal(h.calls.filter(call => call.url === "/api/deletions/owned-task/execute").length, code === "busy" ? 2 : 1);
  }
});

test("P2 expired recovered tasks stop polling without inventing a final outcome", async () => {
  const h = createHarness();
  h.route("GET", "/api/jobs", () => response({ jobs: [job("upload", { status: "running" })] }));
  await h.ready();
  assert.ok(h.timers.size > 0);
  h.route("GET", "/api/jobs", () => response({ jobs: [] }));
  await h.fire("refresh");
  assert.match(h.element("tasks-list").textContent, /Task record expired · outcome unavailable/);
  assert.match(h.element("tasks-list").textContent, /Last known server status: running/);
  assert.equal(h.timers.size, 0);
});

test("known bucket settings work without bucket listing and root summaries do not gate writes", async () => {
  const h = createHarness({ listDenied: true });
  await h.ready();
  assert.match(h.element("bucket-status").textContent, /denied/);
  await readSetting(h, "unlisted-target", "lifecycle");
  assert.match(h.element("settings-scope").textContent, /unlisted-target/);
  assert.equal(h.element("save-setting").disabled, false);
  assert.equal(h.calls.some(call => call.url === "/api/bucket-settings?bucket=unlisted-target&kind=lifecycle"), true);
  assert.equal(h.calls.some(call => /admin|user-info|accessKey/i.test(call.url)), false);
});

test("a denied setting does not prevent another kind from loading", async () => {
  const h = createHarness();
  h.route("GET", url => url.startsWith("/api/bucket-settings?") && url.endsWith("kind=policy"), () => response({ code: "AccessDenied", message: "Policy denied." }, 403));
  await h.ready();
  await readSetting(h);
  assert.equal(h.element("settings-editor").hidden, true);
  assert.match(h.element("settings-status").textContent, /Other settings/);
  await readSetting(h, "known-bucket", "versioning");
  assert.equal(h.element("settings-editor").hidden, false);
  assert.match(h.element("settings-document").value, /VersioningConfiguration/);
  assert.equal(h.element("remove-setting").disabled, true);
});

test("readonly, missing conditional support, and invalid revisions cannot submit writes", async () => {
  for (const options of [{ readOnly: true }, { conditional: false }, { revision: "" }, { revision: "not-a-revision" }, { revision: ["a".repeat(64)] }]) {
    const h = createHarness(options);
    h.route("GET", url => url.startsWith("/api/bucket-settings?"), () => response(setting("known-bucket", "policy", options)));
    await h.ready();
    await readSetting(h);
    assert.equal(h.element("settings-document").readOnly, true);
    assert.equal(h.element("save-setting").disabled, true);
    await h.fire("save-setting", "click", { force: true });
    assert.equal(h.element("settings-review-dialog").open, false);
    assert.equal(h.calls.filter(call => call.method === "POST" && call.url === "/api/bucket-settings").length, 0);
  }
});

test("review and save use immutable loaded scope, complete bytes, CSRF and revision", async () => {
  const h = createHarness();
  let saved;
  h.route("POST", "/api/bucket-settings", call => {
    saved = JSON.parse(call.body);
    return response(setting(saved.bucket, saved.kind, { document: saved.document, revision: "b".repeat(64) }));
  });
  await h.ready();
  await readSetting(h, "exact-目标", "policy");
  const document = '{\n  "Version":"2012-10-17", "Statement":[], "untouched":{"future":true}\n}';
  h.element("settings-document").value = document;
  h.element("settings-bucket").value = "other-unread-bucket";
  await h.fire("save-setting");
  assert.equal(h.element("settings-review-bucket").textContent, "exact-目标");
  assert.equal(h.element("settings-review-document").textContent, document);
  assert.match(h.element("settings-review-warning").textContent, /expose objects publicly/);
  assert.equal(saved, undefined);
  await h.fire("confirm-setting");
  assert.deepEqual(saved, { bucket: "exact-目标", kind: "policy", document, revision: "a".repeat(64), remove: false, confirm: true });
  const call = h.calls.find(item => item.method === "POST" && item.url === "/api/bucket-settings");
  assert.equal(call.headers["X-CSRF-Token"], "csrf-only");
  assert.equal(h.element("settings-document").value, document);
  assert.match(h.element("settings-status").textContent, /confirmed/);
});

test("removal requires review and retains exact bucket/kind without fake version removal", async () => {
  const h = createHarness();
  const writes = [];
  h.route("POST", "/api/bucket-settings", call => {
    const draft = JSON.parse(call.body); writes.push(draft);
    return response(setting(draft.bucket, draft.kind, { document: "", exists: false, revision: "b".repeat(64) }));
  });
  await h.ready();
  await readSetting(h, "remove-target", "lifecycle");
  await h.fire("remove-setting");
  assert.match(h.element("settings-review-warning").textContent, /permanently delete historical versions/);
  await h.fire("back-settings");
  assert.equal(writes.length, 0);
  await h.fire("remove-setting");
  await h.fire("confirm-setting");
  assert.deepEqual(writes[0], { bucket: "remove-target", kind: "lifecycle", document: "", revision: "a".repeat(64), remove: true, confirm: true });
  assert.equal(h.element("remove-setting").disabled, true);
  await readSetting(h, "remove-target", "versioning");
  await h.fire("remove-setting", "click", { force: true });
  assert.equal(h.element("settings-review-dialog").open, false);
});

test("412 and unknown setting outcomes preserve drafts and require fresh reads without replay", async () => {
  for (const [status, code] of [[412, "revision_conflict"], [409, "setting_conflict"], [502, "outcome_unknown"]]) {
    const h = createHarness();
    h.route("POST", "/api/bucket-settings", () => response({ code, message: "Safe error." }, status));
    await h.ready();
    await readSetting(h);
    const draft = '{"Statement":[{"edited":true}]}';
    h.element("settings-document").value = draft;
    await h.fire("save-setting");
    await h.fire("confirm-setting");
    assert.equal(h.element("settings-dialog").open, true);
    assert.equal(h.element("settings-document").value, draft);
    assert.equal(h.element("save-setting").disabled, true);
    assert.match(h.element("settings-status").textContent, /draft is retained/);
    await h.fire("confirm-setting", "click", { force: true });
    assert.equal(h.calls.filter(call => call.method === "POST" && call.url === "/api/bucket-settings").length, 1);
    await readSetting(h);
    assert.equal(h.element("save-setting").disabled, false);
  }
});

test("closed, canceled, and superseded setting reads cannot restore stale editor content", async () => {
  const h = createHarness();
  const old = deferred();
  h.route("GET", url => url.includes("bucket=old-target"), () => old.promise);
  await h.ready();
  await readSetting(h, "old-target");
  await h.fire("close-settings");
  assert.equal(h.calls.find(call => call.url.includes("bucket=old-target")).signal.aborted, true);
  await readSetting(h, "new-target", "lifecycle");
  old.resolve(response(setting("old-target", "policy", { document: "stale draft" })));
  await settle();
  assert.match(h.element("settings-scope").textContent, /new-target/);
  assert.doesNotMatch(h.element("settings-document").value, /stale/);
});

test("an interrupted save requires reading storage before another write and ignores late acknowledgement", async () => {
  const h = createHarness();
  const old = deferred();
  h.route("POST", "/api/bucket-settings", () => old.promise);
  await h.ready();
  await readSetting(h);
  h.element("settings-document").value = '{"draft":true}';
  await h.fire("save-setting");
  await h.fire("confirm-setting");
  await h.fire("back-settings");
  assert.equal(h.element("save-setting").disabled, true);
  assert.match(h.element("settings-status").textContent, /outcome is unknown/);
  old.resolve(response(setting("known-bucket", "policy", { document: "late ack" })));
  await settle();
  assert.equal(h.element("settings-document").value, '{"draft":true}');
  assert.equal(h.element("save-setting").disabled, true);
});

test("secret rotation is available only for writable enabled IAM identity", async () => {
  for (const options of [{ readOnly: true }, { selfAccount: { kind: "root" } }, { selfAccount: { kind: "sts" } }, { selfAccount: { kind: "service" } }, { selfAccount: { kind: "directory" } }, { selfAccount: { kind: "unknown", status: "unknown", canRotateSecret: false } }, { selfAccount: { status: "disabled" } }, { selfAccount: { canRotateSecret: false } }]) {
    const h = createHarness(options);
    await h.ready();
    await h.fire("open-account");
    assert.equal(h.element("self-secret-form").hidden, true);
    h.element("new-secret").value = "unsubmitted-secret";
    await h.fire("review-secret", "click", { force: true });
    assert.equal(h.element("secret-confirmation").hidden, true);
    assert.equal(h.calls.filter(call => call.url === "/api/self-secret").length, 0);
    await h.fire("close-account");
    assert.equal(h.element("new-secret").value, "");
  }
});

test("account errors and late responses cannot enable rotation or disturb browsing", async () => {
  const h = createHarness();
  const old = deferred();
  h.route("GET", "/api/self-account", () => old.promise);
  await h.ready();
  await h.fire("open-account");
  await h.fire("close-account");
  old.resolve(response({ kind: "iam", status: "enabled", canRotateSecret: true }));
  await settle();
  assert.equal(h.element("account-dialog").open, false);
  assert.equal(h.element("self-secret-form").hidden, true);
  assert.equal(h.element("workspace").hidden, false);
});

test("secret byte limits are checked and cancel/close/logout erase input without submitting", async () => {
  const h = createHarness();
  await h.ready();
  await h.fire("open-account");
  for (const secret of ["short", "字".repeat(43)]) {
    h.element("new-secret").value = secret;
    await h.fire("review-secret");
    assert.equal(h.element("secret-confirmation").hidden, true);
    assert.match(h.element("self-secret-status").textContent, /8–128/);
  }
  h.element("new-secret").value = "valid-secret";
  await h.fire("review-secret");
  assert.equal(h.element("new-secret").disabled, true);
  await h.fire("cancel-secret");
  assert.equal(h.element("new-secret").disabled, false);
  await h.fire("close-account");
  assert.equal(h.element("new-secret").value, "");
  await h.fire("open-account");
  assert.equal(h.element("review-secret").hidden, false);
  h.element("new-secret").value = "another-unsubmitted-secret";
  await h.fire("logout");
  assert.equal(h.element("new-secret").value, "");
  assert.equal(h.element("account-dialog").open, false);
  assert.equal(h.calls.filter(call => call.url === "/api/self-secret").length, 0);
});

test("confirmed rotation clears secret immediately and stops all session actions", async () => {
  const h = createHarness();
  const result = deferred();
  h.route("POST", "/api/self-secret", () => result.promise);
  await h.ready();
  await reviewSecret(h, "synthetic-secret");
  await h.fire("self-secret-form", "submit");
  assert.equal(h.element("new-secret").value, "");
  assert.equal(h.element("close-account").disabled, true);
  const call = h.calls.find(item => item.url === "/api/self-secret");
  assert.deepEqual(JSON.parse(call.body), { newSecret: "synthetic-secret", confirm: true });
  assert.equal(call.headers["X-CSRF-Token"], "csrf-only");
  result.resolve(response({ restartRequired: true, outcome: "confirmed" }));
  await settle();
  assert.equal(h.element("workspace").hidden, true);
  assert.equal(h.element("account-dialog").open, false);
  assert.equal(h.element("login-submit").disabled, true);
  assert.match(h.element("login-status").textContent, /confirmed secret rotation/);
  await h.windowEvent("pageshow", { persisted: true });
  assert.equal(h.calls.filter(item => item.url === "/api/session").length, 1);
});

test("unknown rotation, transport loss and page suspension stop the UI without replay", async () => {
  for (const kind of ["unknown", "network", "suspend"]) {
    const h = createHarness();
    const held = deferred();
    h.route("POST", "/api/self-secret", () => kind === "unknown" ? response({ code: "outcome_unknown", message: "Unknown." }, 502) : kind === "network" ? Promise.reject(new TypeError("lost response")) : held.promise);
    await h.ready();
    await reviewSecret(h);
    await h.fire("self-secret-form", "submit");
    if (kind === "suspend") {
      await h.windowEvent("pagehide");
      held.resolve(response({ restartRequired: true, outcome: "confirmed" }));
      await settle();
    }
    assert.equal(h.element("new-secret").value, "");
    assert.equal(h.element("login-submit").disabled, true);
    assert.match(h.element("login-status").textContent, /unknown/);
    await h.fire("self-secret-form", "submit", { force: true });
    assert.equal(h.calls.filter(item => item.url === "/api/self-secret").length, 1);
  }
});

test("explicit zero-write 501/502/403 rotation failures retain session and refresh identity", async () => {
  for (const [status, code] of [[501, "secret_rotation_unsupported"], [502, "UpstreamError"], [403, "AccessDenied"]]) {
    const h = createHarness();
    let reads = 0;
    h.route("GET", "/api/self-account", () => {
      reads++;
      return response({ kind: "iam", status: "enabled", canRotateSecret: reads === 1 || status === 502 });
    });
    h.route("POST", "/api/self-secret", () => response({ code, message: "No secret write was attempted.", restartRequired: false }, status));
    await h.ready();
    await reviewSecret(h);
    await h.fire("self-secret-form", "submit");
    assert.equal(h.element("workspace").hidden, false);
    assert.equal(h.element("account-dialog").open, true);
    assert.equal(h.element("new-secret").value, "");
    assert.equal(h.element("secret-confirmation").hidden, true);
    assert.equal(reads, 2);
    assert.equal(h.element("self-secret-form").hidden, status !== 502);
    assert.match(h.element("self-account-status").textContent, /No secret write was attempted/);
    await h.fire("self-secret-form", "submit", { force: true });
    assert.equal(h.calls.filter(call => call.url === "/api/self-secret").length, 1);
  }
});

test("ambiguous 5xx, explicit retirement and malformed success must still stop rotation UI", async () => {
  for (const [status, data] of [
    [500, { code: "UpstreamError", message: "No definitive acknowledgement." }],
    [409, { code: "CredentialConflict", message: "Selected identity no longer valid.", restartRequired: true }],
    [502, { code: "outcome_unknown", message: "Unknown.", restartRequired: false }],
    [200, { restartRequired: false, outcome: "confirmed" }],
    [200, "malformed-json"],
  ]) {
    const h = createHarness();
    h.route("POST", "/api/self-secret", () => data === "malformed-json" ? { status: 200, ok: true, json: async () => { throw new SyntaxError("broken JSON acknowledgement"); } } : response(data, status));
    await h.ready();
    await reviewSecret(h);
    await h.fire("self-secret-form", "submit");
    assert.equal(h.element("workspace").hidden, true);
    assert.equal(h.element("login-submit").disabled, true);
    assert.match(h.element("login-status").textContent, /unknown/);
    assert.equal(h.element("new-secret").value, "");
  }
});

test("setting save displays server canonical document and uses its fresh revision for the next review", async () => {
  const h = createHarness();
  const canonical = '{"Version":"2012-10-17","Statement":[]}';
  const writes = [];
  h.route("POST", "/api/bucket-settings", call => {
    writes.push(JSON.parse(call.body));
    return response(setting("known-bucket", "policy", { document: canonical, revision: "b".repeat(64) }));
  });
  await h.ready();
  await readSetting(h);
  h.element("settings-document").value = ' {\n "Version" : "2012-10-17",\n"Statement": []\n}';
  await h.fire("save-setting");
  await h.fire("confirm-setting");
  assert.equal(h.element("settings-document").value, canonical);
  assert.notEqual(writes[0].document, canonical);
  await h.fire("save-setting");
  await h.fire("confirm-setting");
  assert.equal(writes[1].revision, "b".repeat(64));
  assert.equal(writes[1].document, canonical);
});

test("active-task rejection clears secret and requires a new human review", async () => {
  const h = createHarness();
  h.route("POST", "/api/self-secret", () => response({ code: "active_tasks", message: "Active tasks." }, 409));
  await h.ready();
  await reviewSecret(h);
  await h.fire("self-secret-form", "submit");
  assert.equal(h.element("new-secret").value, "");
  assert.equal(h.element("workspace").hidden, false);
  assert.equal(h.element("secret-confirmation").hidden, true);
  assert.match(h.element("self-secret-status").textContent, /Stop active/);
  await h.fire("self-secret-form", "submit", { force: true });
  assert.equal(h.calls.filter(item => item.url === "/api/self-secret").length, 1);
});

test("an invalid CSRF response clears all configuration and credential drafts", async () => {
  const h = createHarness();
  h.route("POST", "/api/bucket-settings", () => response({ code: "invalid_csrf", message: "Session changed." }, 403));
  await h.ready();
  await readSetting(h);
  h.element("settings-document").value = '{"draft":true}';
  await h.fire("save-setting");
  await h.fire("confirm-setting");
  assert.equal(h.element("settings-document").value, "");
  assert.equal(h.element("settings-review-dialog").open, false);
  assert.equal(h.element("workspace").hidden, true);
});

test("reading another setting cannot discard a dirty draft without confirmation or on failure", async () => {
  const h = createHarness();
  await h.ready();
  await readSetting(h);
  const draft = '{"draft":"keep me"}';
  h.element("settings-document").value = draft;
  h.route("GET", url => url.startsWith("/api/bucket-settings?"), () => response({ code: "AccessDenied", message: "New read denied." }, 403));
  await h.fire("settings-load-form", "submit");
  assert.equal(h.element("settings-document").value, draft);
  assert.equal(h.element("settings-read-dialog").open, true);
  assert.equal(h.calls.filter(call => call.url.startsWith("/api/bucket-settings?")).length, 1);
  await h.fire("back-setting-read");
  assert.equal(h.element("settings-document").value, draft);
  await h.fire("settings-load-form", "submit");
  await h.fire("confirm-setting-read");
  assert.equal(h.element("settings-document").value, draft);
  assert.match(h.element("settings-status").textContent, /retained/);
});

test("malformed successful setting acknowledgement blocks another confirmation and retains draft", async () => {
  const h = createHarness();
  h.route("POST", "/api/bucket-settings", () => ({ status: 200, ok: true, json: async () => { throw new SyntaxError("broken JSON acknowledgement"); } }));
  await h.ready();
  await readSetting(h);
  const draft = '{"draft":true}';
  h.element("settings-document").value = draft;
  await h.fire("save-setting");
  await h.fire("confirm-setting");
  assert.equal(h.element("settings-dialog").open, true);
  assert.equal(h.element("settings-document").value, draft);
  assert.equal(h.element("save-setting").disabled, true);
  await h.fire("confirm-setting", "click", { force: true });
  assert.equal(h.calls.filter(call => call.url === "/api/bucket-settings" && call.method === "POST").length, 1);
});

test("redirects and incomplete successful setting DTOs require fresh reads without another write", async () => {
  const cases = [
    response({ code: "RedirectDisabled", message: "Redirected." }, 302),
    response(setting("known-bucket", "policy", { conditional: false })),
    response(setting("known-bucket", "policy", { revision: "" })),
    response(setting("known-bucket", "policy", { revision: ["a".repeat(64)] })),
    response(setting("known-bucket", "policy", { exists: false })),
    response(setting("known-bucket", "policy", { format: "xml" })),
    response(setting("different-bucket", "policy")),
  ];
  for (const returned of cases) {
    const h = createHarness();
    h.route("POST", "/api/bucket-settings", () => returned);
    await h.ready();
    await readSetting(h);
    h.element("settings-document").value = '{"draft":true}';
    await h.fire("save-setting");
    await h.fire("confirm-setting");
    assert.equal(h.element("settings-dialog").open, true);
    assert.equal(h.element("save-setting").disabled, true);
    assert.match(h.element("settings-status").textContent, /unknown/);
    await h.fire("confirm-setting", "click", { force: true });
    assert.equal(h.calls.filter(call => call.url === "/api/bucket-settings" && call.method === "POST").length, 1);
    assert.equal(h.calls.every(call => call.redirect === "error"), true);
  }
});

test("logout during secret submission reports an unknown outcome and ignores late confirmation", async () => {
  const h = createHarness();
  const held = deferred();
  h.route("POST", "/api/self-secret", () => held.promise);
  await h.ready();
  await reviewSecret(h);
  await h.fire("self-secret-form", "submit");
  await h.fire("logout");
  assert.equal(h.element("workspace").hidden, true);
  assert.equal(h.element("new-secret").value, "");
  assert.match(h.element("login-status").textContent, /interrupted|unknown/);
  assert.equal(h.element("login-submit").disabled, true);
  held.resolve(response({ restartRequired: true, outcome: "confirmed" }));
  await settle();
  assert.doesNotMatch(h.element("login-status").textContent, /confirmed secret rotation/);
});

test("logout clears complete configuration review text as well as editor and secret inputs", async () => {
  const h = createHarness();
  await h.ready();
  await readSetting(h);
  h.element("settings-document").value = '{"private-policy-draft":true}';
  await h.fire("save-setting");
  await h.fire("logout");
  assert.equal(h.element("settings-document").value, "");
  assert.equal(h.element("settings-review-document").textContent, "");
  assert.equal(h.element("settings-read-dialog").open, false);
});

test("repeated confirmation cannot send concurrent settings writes or secret rotations", async () => {
  const h = createHarness();
  const save = deferred();
  h.route("POST", "/api/bucket-settings", () => save.promise);
  await h.ready();
  await readSetting(h);
  await h.fire("save-setting");
  await h.fire("save-setting", "click", { force: true });
  await h.fire("confirm-setting");
  await h.fire("confirm-setting", "click", { force: true });
  assert.equal(h.calls.filter(call => call.url === "/api/bucket-settings" && call.method === "POST").length, 1);
  save.resolve(response(setting("known-bucket", "policy", { revision: "b".repeat(64) })));
  await settle();
  await h.fire("close-settings");
  const rotation = deferred();
  h.route("POST", "/api/self-secret", () => rotation.promise);
  await reviewSecret(h);
  h.element("confirm-secret").focus();
  await h.fire("self-secret-form", "submit");
  await h.fire("self-secret-form", "submit", { force: true });
  assert.equal(h.calls.filter(call => call.url === "/api/self-secret").length, 1);
  assert.equal(h.element("new-secret").disabled, true);
  assert.equal(h.activeElement().id, "self-secret-status");
  rotation.resolve(response({ restartRequired: true, outcome: "confirmed" }));
  await settle();
  assert.equal(h.activeElement().id, "login-status");
});

test("read confirmation keeps immutable requested scope and ignores late replies after window rotation", async () => {
  const h = createHarness();
  await h.ready();
  await readSetting(h);
  h.element("settings-document").value = '{"draft":true}';
  h.element("settings-bucket").value = "target-at-review";
  h.element("settings-kind").value = "lifecycle";
  await h.fire("settings-load-form", "submit");
  assert.equal(h.activeElement().id, "back-setting-read");
  h.element("settings-bucket").value = "changed-behind-review";
  const held = deferred();
  h.route("GET", url => url.includes("bucket=target-at-review"), () => held.promise);
  await h.fire("confirm-setting-read");
  assert.equal(h.calls.some(call => call.url === "/api/bucket-settings?bucket=target-at-review&kind=lifecycle"), true);
  await h.fire("close-settings");
  await h.fire("open-account");
  held.resolve(response(setting("target-at-review", "lifecycle")));
  await settle();
  assert.equal(h.element("account-dialog").open, true);
  assert.equal(h.element("settings-dialog").open, false);
  assert.equal(h.element("settings-document").value, "");
});

(async () => {
  let passed = 0;
  for (const item of tests) {
    let guard;
    try {
      await Promise.race([item.run(), new Promise((_, reject) => { guard = setTimeout(() => reject(new Error("test did not complete")), 5000); })]);
      passed++; process.stdout.write(`PASS ${item.name}\n`);
    }
    catch (error) { process.stderr.write(`FAIL ${item.name}\n${error.stack}\n`); process.exitCode = 1; break; }
    finally { clearTimeout(guard); }
  }
  process.stdout.write(`${passed}/${tests.length} console web behavior groups passed\n`);
})();
