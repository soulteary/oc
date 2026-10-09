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
  const downloads = [];
  const asynchronousErrors = [];
  let nextTimer = 0;
  let document;
  class Element {
    constructor(tagName, id = "") {
      this.tagName = tagName;
      if (tagName.toLowerCase() === "textarea") {
        // HTMLTextAreaElement.type is getter-only. Assigning it must fail in
        // strict mode just as it does in the browser, rather than hiding a
        // broken form behind a permissive plain-object DOM stand-in.
        Object.defineProperty(this, "type", { get: () => "textarea", enumerable: true });
      }
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
    removeAttribute(name) { this.attributes.delete(name); }
    select() { this.selected = true; }
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
    location: { origin: "http://127.0.0.1:9001" },
    addEventListener(type, callback) {
      if (!windowListeners.has(type)) windowListeners.set(type, []);
      windowListeners.get(type).push(callback);
    },
    open() { return { close() {}, location: { replace(url) { downloads.push(url); } }, opener: null }; },
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
    document, window, URL, URLSearchParams, AbortController, TextEncoder, XMLHttpRequest,
    setTimeout(callback) { timers.set(++nextTimer, callback); return nextTimer; },
    clearTimeout(id) { timers.delete(id); },
    fetch: async (url, init = {}) => {
      const call = { url: String(url), method: init.method || "GET", body: init.body, headers: init.headers, signal: init.signal, redirect: init.redirect };
      calls.push(call);
      for (const route of [...routes].reverse()) {
        if (route.method === call.method && route.match(call.url)) return await route.handle(call);
      }
      if (url === "/api/session") return response({ alias: "selected", csrfToken: "csrf-only", readOnly: !!options.readOnly, maxUploadSize: 1024 ** 3 });
      if (url === "/api/capabilities") return response(options.capabilities || {});
      if (url === "/api/buckets") return options.listDenied ? response({ code: "AccessDenied", message: "Bucket listing denied." }, 403) : response({ buckets: [{ name: "listed-bucket", created: "2026-10-08T00:00:00Z" }] });
      if (String(url).startsWith("/api/objects?")) return response({ entries: [] });
      if (url === "/api/account") return response({ buckets: [{ name: "listed-bucket", size: 42, read: false, write: false }] });
      if (url === "/api/archives" && call.method === "GET") return response({ archives: [] });
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
    elements, calls, timers, transfers, downloads,
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
    async fireElement(element, type = "click") {
      assert.equal(element.disabled, false);
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

function descendants(element) {
  return element.children.flatMap(child => typeof child === "string" ? [] : [child, ...descendants(child)]);
}

function findText(element, text) {
  const found = descendants(element).find(child => child.textContent === text);
  assert.ok(found, `missing control ${text}`);
  return found;
}

function iamField(h, name) {
  const field = descendants(h.element("iam-fields")).find(child => child.id === `iam-field-${name}`);
  assert.ok(field, `missing IAM field ${name}`);
  return field;
}

const fullCapabilities = { bucketManagement: true, versions: true, sharing: true, archives: true, iam: true };

test("the DOM harness preserves the browser's getter-only textarea type", () => {
  const h = createHarness();
  const textarea = h.element("settings-document");
  assert.equal(textarea.type, "textarea");
  assert.throws(() => { textarea.type = "text"; }, TypeError);
});

test("group member action switches preserve writable multiline fields", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  h.route("GET", "/api/iam/users", () => response({ users: [] }));
  h.route("GET", "/api/iam/groups", () => response({ groups: [] }));
  await h.ready();
  await h.fire("open-iam");
  h.element("iam-kind").value = "groups";
  await h.fire("iam-kind", "change");
  for (const action of ["group.add-members", "group.remove-members"]) {
    h.element("iam-action").value = action;
    await h.fire("iam-action", "change");
    assert.equal(iamField(h, "group").type, "text");
    assert.equal(iamField(h, "members").type, "textarea");
    assert.equal(iamField(h, "members").rows, 5);
    assert.equal(iamField(h, "members").required, true);
    assert.equal(iamField(h, "members").disabled, false);
    iamField(h, "members").value = "member-one\nmember-two";
    assert.equal(iamField(h, "members").value, "member-one\nmember-two");
  }
});

test("user and group policy binding action switches finish building their multiline forms", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  h.route("GET", "/api/iam/users", () => response({ users: [] }));
  h.route("GET", "/api/iam/groups", () => response({ groups: [] }));
  h.route("GET", url => url.startsWith("/api/iam/bindings?"), call => {
    const parameters = new URLSearchParams(call.url.split("?")[1]);
    return response({ kind: parameters.get("kind"), target: parameters.get("target"), policies: ["readonly"], conditional: true, revision: "a".repeat(64) });
  });
  await h.ready();
  await h.fire("open-iam");
  for (const [view, kind, target] of [["users", "user", "alice"], ["groups", "group", "readers"]]) {
    h.element("iam-kind").value = view;
    await h.fire("iam-kind", "change");
    h.element("iam-action").value = `${kind}.policies`;
    await h.fire("iam-action", "change");
    assert.equal(iamField(h, "policies").type, "textarea");
    assert.equal(iamField(h, "policies").required, false);
    assert.equal(iamField(h, "policies").readOnly, true);
    iamField(h, kind).value = target;
    await h.fire("load-iam-bindings");
    assert.equal(iamField(h, "policies").value, "readonly");
    assert.equal(iamField(h, "policies").readOnly, false);
  }
  assert.equal(h.calls.filter(call => call.url.startsWith("/api/iam/bindings?")).length, 2);
});

test("service account creation and restriction policy switches retain fields and run list loading", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  h.route("GET", "/api/iam/users", () => response({ users: [] }));
  h.route("GET", "/api/iam/groups", () => response({ groups: [] }));
  h.route("GET", url => url.startsWith("/api/iam/service-accounts?"), call => {
    assert.equal(new URLSearchParams(call.url.split("?")[1]).get("user"), "parent-user");
    return response({ serviceAccounts: [{ accessKey: "service-key", parentUser: "parent-user", status: "enabled", policy: '{"Statement":[]}' }] });
  });
  await h.ready();
  await h.fire("open-iam");
  h.element("iam-owner").value = "parent-user";
  h.element("iam-kind").value = "service-accounts";
  await h.fire("iam-kind", "change");
  assert.equal(h.element("iam-action").value, "service-account.create");
  assert.equal(iamField(h, "user").value, "parent-user");
  assert.equal(iamField(h, "secretKey").type, "password");
  assert.equal(iamField(h, "policy").type, "textarea");
  assert.equal(iamField(h, "policy").required, false);
  assert.match(h.element("iam-status").textContent, /1 service accounts returned/);
  assert.equal(h.calls.filter(call => call.url.startsWith("/api/iam/service-accounts?")).length, 1);
  await h.fireElement(findText(h.element("iam-list"), "Choose for change"));
  h.element("iam-action").value = "service-account.policy";
  await h.fire("iam-action", "change");
  assert.equal(iamField(h, "accessKey").value, "service-key");
  assert.equal(iamField(h, "policy").type, "textarea");
  assert.equal(iamField(h, "policy").required, true);
  assert.equal(iamField(h, "policy").value, '{"Statement":[]}');
  h.element("iam-kind").value = "groups";
  await h.fire("iam-kind", "change");
  h.element("iam-kind").value = "service-accounts";
  await h.fire("iam-kind", "change");
  assert.equal(iamField(h, "policy").type, "textarea");
  assert.equal(iamField(h, "policy").required, false);
  assert.equal(h.calls.filter(call => call.url.startsWith("/api/iam/service-accounts?")).length, 2);
});

test("version-only identities can enter an exact bucket and key without bucket listing", async () => {
  const h = createHarness({ readOnly: true, listDenied: true, capabilities: fullCapabilities });
  h.route("GET", url => url.startsWith("/api/versions?"), call => {
    const parameters = new URLSearchParams(call.url.split("?")[1]);
    assert.equal(parameters.get("bucket"), "known-history-bucket");
    assert.equal(parameters.get("key"), "deleted/世界.txt");
    return response({ entries: [{ key: "deleted/世界.txt", versionId: "old-version", size: 10 }] });
  });
  await h.ready();
  assert.match(h.element("bucket-status").textContent, /denied/);
  assert.equal(h.element("open-versions").hidden, false);
  assert.equal(h.element("open-versions").disabled, false);
  await h.fire("open-versions");
  assert.equal(h.activeElement(), h.element("versions-bucket"));
  h.element("versions-bucket").value = "known-history-bucket";
  h.element("versions-key").value = "deleted/世界.txt";
  await h.fire("versions-form", "submit");
  assert.match(h.element("versions-list").textContent, /old-version/);
  assert.equal(h.calls.filter(call => call.url.startsWith("/api/versions?")).length, 1);
});

test("Refresh retries a failed capability read and restores all feature controls", async () => {
  const h = createHarness();
  let available = false;
  h.route("GET", "/api/capabilities", () => available ? response(fullCapabilities) : response({ code: "busy", message: "Temporarily busy." }, 429));
  await h.ready();
  assert.equal(h.calls.filter(call => call.url === "/api/capabilities").length, 1);
  assert.equal(h.element("open-versions").hidden, true);
  assert.equal(h.element("open-iam").hidden, true);
  available = true;
  await h.fire("refresh");
  assert.equal(h.calls.filter(call => call.url === "/api/capabilities").length, 2);
  for (const id of ["open-create-bucket", "open-delete-bucket", "open-versions", "open-iam", "read-toolbar"]) assert.equal(h.element(id).hidden, false, id);
});

test("Refresh does not duplicate or cancel an in-flight capability read", async () => {
  const h = createHarness();
  const held = deferred();
  h.route("GET", "/api/capabilities", () => held.promise);
  await h.ready();
  const capabilityRequest = h.calls.find(call => call.url === "/api/capabilities");
  await h.fire("refresh");
  assert.equal(h.calls.filter(call => call.url === "/api/capabilities").length, 1);
  assert.equal(capabilityRequest.signal.aborted, false);
  held.resolve(response(fullCapabilities));
  await settle();
  assert.equal(h.element("open-versions").hidden, false);
  assert.equal(h.element("open-iam").hidden, false);
});

test("feature capabilities keep unavailable controls hidden and read-only ZIP selection enabled", async () => {
  const old = createHarness();
  await old.ready();
  for (const id of ["open-create-bucket", "open-delete-bucket", "open-versions", "open-iam", "read-toolbar"]) assert.equal(old.element(id).hidden, true, id);
  const h = createHarness({ readOnly: true, capabilities: fullCapabilities });
  h.route("GET", url => url.startsWith("/api/objects?"), () => response({ entries: [{ key: "photo.jpg", size: 10 }] }));
  await h.ready();
  assert.equal(h.element("write-toolbar").hidden, true);
  assert.equal(h.element("open-create-bucket").hidden, true);
  assert.equal(h.element("read-toolbar").hidden, false);
  assert.equal(h.element("selection-header").hidden, false);
  assert.equal(h.element("select-loaded").disabled, false);
  h.element("select-loaded").checked = true;
  await h.fire("select-loaded", "change");
  assert.equal(h.element("archive-selected").disabled, false);
  await h.fire("open-iam");
  assert.equal(h.element("iam-action-form").hidden, true);
  await h.fire("iam-action-form", "submit", { force: true });
  assert.equal(h.calls.some(call => call.url === "/api/iam/actions"), false);
});

test("bucket deletion requires exact confirmation and never requests bucket emptying", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  h.route("POST", "/api/buckets/delete", call => {
    assert.deepEqual(JSON.parse(call.body), { bucket: "listed-bucket", confirmBucket: "listed-bucket" });
    assert.equal(call.headers["X-CSRF-Token"], "csrf-only");
    return response({ code: "BucketNotEmpty", message: "Bucket contains versions or objects." }, 409);
  });
  await h.ready();
  await h.fire("open-delete-bucket");
  assert.equal(h.element("bucket-action-name").value, "listed-bucket");
  assert.equal(h.activeElement(), h.element("bucket-action-confirm"));
  h.element("bucket-action-confirm").value = "another-bucket";
  await h.fire("bucket-action-form", "submit");
  assert.equal(h.calls.some(call => call.url === "/api/buckets/delete"), false);
  h.element("bucket-action-confirm").value = "listed-bucket";
  await h.fire("bucket-action-form", "submit");
  assert.match(h.element("bucket-action-status").textContent, /versions/);
  assert.equal(h.element("bucket-dialog").open, true);
  assert.equal(h.calls.filter(call => call.url === "/api/buckets/delete").length, 1);
  assert.equal(h.calls.some(call => /empty|recursive|purge/.test(call.url)), false);
  await h.fire("close-bucket-action");
  h.route("POST", "/api/buckets/create", call => {
    assert.deepEqual(JSON.parse(call.body), { bucket: "private-new" });
    return response({ bucket: "private-new" });
  });
  await h.fire("open-create-bucket");
  h.element("bucket-action-name").value = "private-new";
  await h.fire("bucket-action-form", "submit");
  assert.equal(h.element("bucket-dialog").open, false);
});

test("an exact known bucket can be deleted without permission to list buckets", async () => {
  const h = createHarness({ listDenied: true, capabilities: fullCapabilities });
  h.route("POST", "/api/buckets/delete", call => {
    assert.deepEqual(JSON.parse(call.body), { bucket: "known-empty", confirmBucket: "known-empty" });
    assert.equal(call.headers["X-CSRF-Token"], "csrf-only");
    return response({ bucket: "known-empty", outcome: "confirmed" });
  });
  await h.ready();
  assert.equal(h.element("open-delete-bucket").hidden, false);
  assert.equal(h.element("open-delete-bucket").disabled, false);
  await h.fire("open-delete-bucket");
  assert.equal(h.element("bucket-action-name").value, "");
  assert.equal(h.element("bucket-action-name").readOnly, false);
  assert.equal(h.activeElement(), h.element("bucket-action-name"));
  h.element("bucket-action-name").value = "known-empty";
  h.element("bucket-action-confirm").value = "wrong-name";
  await h.fire("bucket-action-form", "submit");
  assert.equal(h.calls.some(call => call.url === "/api/buckets/delete"), false);
  assert.equal(h.element("bucket-dialog").open, true);
  h.element("bucket-action-confirm").value = "known-empty";
  await h.fire("bucket-action-form", "submit");
  assert.equal(h.calls.filter(call => call.url === "/api/buckets/delete").length, 1);
  assert.equal(h.element("bucket-dialog").open, false);
  assert.equal(h.calls.some(call => /empty|recursive|purge/.test(call.url)), false);

  for (const options of [{ readOnly: true, capabilities: fullCapabilities }, { capabilities: {} }]) {
    const gated = createHarness({ listDenied: true, ...options });
    await gated.ready();
    assert.equal(gated.element("open-delete-bucket").hidden, true);
    assert.equal(gated.element("open-delete-bucket").disabled, true);
    await gated.fire("open-delete-bucket", "click", { force: true });
    assert.equal(gated.element("bucket-dialog").open, false);
    gated.element("bucket-action-name").value = "known-empty";
    gated.element("bucket-action-confirm").value = "known-empty";
    await gated.fire("bucket-action-form", "submit", { force: true });
    assert.equal(gated.calls.some(call => call.url === "/api/buckets/delete"), false);
  }
});

test("version history preserves exact version refs and delete markers have no actions", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  const exactKey = "folder/目标 +%.txt";
  h.route("GET", url => url.startsWith("/api/versions?"), call => {
    const query = new URLSearchParams(call.url.split("?")[1]);
    assert.equal(query.get("bucket"), "listed-bucket");
    assert.equal(query.get("key"), exactKey);
    return response({ entries: [
      { key: exactKey, versionId: "delete-version", deleteMarker: true, latest: true },
      { key: exactKey, versionId: "old +%/version", size: 4, modified: "2026-10-08" },
    ], nextCursor: "page-2" });
  });
  await h.ready();
  await h.fire("open-versions");
  h.element("versions-key").value = exactKey;
  await h.fire("versions-form", "submit");
  const cards = h.element("versions-list").children;
  assert.equal(cards.length, 2);
  assert.equal(descendants(cards[0]).some(child => child.tagName === "a" || child.tagName === "button"), false);
  const download = descendants(cards[1]).find(child => child.tagName === "a");
  assert.equal(new URLSearchParams(download.href.split("?")[1]).get("versionId"), "old +%/version");
  await h.fireElement(findText(cards[1], "Share this version"));
  h.route("POST", "/api/shares", call => {
    assert.deepEqual(JSON.parse(call.body), { bucket: "listed-bucket", key: exactKey, versionId: "old +%/version", expiresSeconds: 3600, downloadName: "" });
    return response({ url: "https://storage.example/download?signature=transient", expiresAt: "2026-10-09T01:00:00Z" });
  });
  await h.fire("share-form", "submit");
  assert.match(h.element("share-url").value, /signature=transient/);
  await h.fire("copy-share");
  assert.equal(h.element("share-url").selected, true);
  await h.fire("close-share");
  assert.equal(h.element("share-url").value, "");
  assert.equal(h.element("share-scope").textContent, "");
});

test("late share replies and session expiry cannot restore signed URLs", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  h.route("GET", url => url.startsWith("/api/objects?"), () => response({ entries: [{ key: "selected-key", size: 10 }] }));
  const held = deferred();
  h.route("POST", "/api/shares", () => held.promise);
  await h.ready();
  await h.fireElement(findText(h.element("objects-body"), "Share"));
  await h.fire("share-form", "submit");
  await h.fire("close-share");
  held.resolve(response({ url: "https://example.invalid/?signature=late", expiresAt: "2026-10-09T01:00:00Z" }));
  await settle();
  assert.equal(h.element("share-url").value, "");
  assert.equal(h.element("share-result").hidden, true);
});

test("readonly ZIP fixes selected keys, reports progress and exposes only ready archives", async () => {
  const h = createHarness({ readOnly: true, capabilities: fullCapabilities });
  h.route("GET", url => url.startsWith("/api/objects?"), () => response({ entries: [{ key: "first-object", size: 10 }, { key: "second-object", size: 20 }] }));
  const archive = { id: "owned-archive", status: "planning", count: 2, size: 30, transferred: 0 };
  h.route("POST", "/api/archives", call => {
    assert.deepEqual(JSON.parse(call.body), { refs: [{ bucket: "listed-bucket", key: "first-object" }, { bucket: "listed-bucket", key: "second-object" }] });
    assert.equal(call.headers["X-CSRF-Token"], "csrf-only");
    return response(archive);
  });
  h.route("GET", "/api/archives/owned-archive", () => response({ ...archive, status: "ready", transferred: 30 }));
  h.route("POST", "/api/archives/owned-archive/cancel", () => response({ ...archive, status: "canceled" }));
  await h.ready();
  h.element("select-loaded").checked = true;
  await h.fire("select-loaded", "change");
  await h.fire("archive-selected");
  await h.fire("start-archive");
  assert.equal(h.element("download-archive").hidden, true);
  await h.fire("close-archive");
  assert.equal(h.element("open-current-archive").hidden, false);
  const callback = [...h.timers.values()][0];
  assert.ok(callback);
  callback();
  await settle();
  await h.fire("open-current-archive");
  assert.equal(h.element("download-archive").hidden, false);
  assert.equal(h.element("download-archive").href, "/api/archives/owned-archive/download");
  await h.fire("cancel-archive");
  assert.match(h.element("archive-status").textContent, /canceled/);
  assert.equal(h.element("download-archive").hidden, true);
  assert.equal(h.calls.some(call => call.url.startsWith("/api/deletions")), false);
});

test("IAM actions require exact targets and returned secrets are cleared on dismissal and logout", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  h.route("GET", "/api/iam/users", () => response({ users: [{ accessKey: "existing-user", status: "enabled", policies: ["readonly"], memberOf: ["readers"], memberOfKnown: true }] }));
  h.route("POST", "/api/iam/actions", call => {
    assert.deepEqual(JSON.parse(call.body), { action: "user.create", user: "new-user", confirmTarget: "new-user" });
    return response({ outcome: "confirmed", restartRequired: false, credentials: { accessKey: "new-user", secretKey: "show-once-secret" } });
  });
  await h.ready();
  await h.fire("open-iam");
  assert.match(h.element("iam-list").textContent, /readonly/);
  assert.match(h.element("iam-list").textContent, /readers/);
  iamField(h, "user").value = "new-user";
  h.element("iam-confirm").value = "incorrect";
  await h.fire("iam-action-form", "submit");
  assert.equal(h.calls.some(call => call.url === "/api/iam/actions"), false);
  h.element("iam-confirm").value = "new-user";
  await h.fire("iam-action-form", "submit");
  assert.equal(h.element("iam-secret-result").hidden, false);
  assert.equal(h.element("iam-result-secret").value, "show-once-secret");
  assert.equal(h.element("submit-iam-action").disabled, true);
  await h.fire("dismiss-iam-secret");
  assert.equal(h.element("iam-result-secret").value, "");
  assert.equal(h.element("iam-result-access").value, "");
  assert.equal(h.element("submit-iam-action").disabled, false);
  h.element("iam-action").value = "user.rotate";
  await h.fire("iam-action", "change");
  iamField(h, "secretKey").value = "pending-new-secret";
  await h.fire("logout");
  assert.equal(h.element("iam-fields").children.length, 0);
  assert.equal(h.element("iam-result-secret").value, "");
  assert.equal(h.element("iam-dialog").open, false);
});

test("IAM users distinguish confirmed empty membership from unknown and show only listed group evidence", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  h.route("GET", "/api/iam/users", () => response({ users: [
    { accessKey: "unknown-user", status: "enabled", memberOf: null, memberOfKnown: false },
    { accessKey: "known-empty", status: "enabled", memberOf: [], memberOfKnown: true },
    { accessKey: "known-member", status: "enabled", memberOf: ["readers"], memberOfKnown: true },
    { accessKey: "legacy-user", status: "enabled", memberOf: [] },
  ] }));
  h.route("GET", "/api/iam/groups", () => response({ groups: [
    { name: "a", status: "enabled", members: ["unknown-user"] },
    { name: "ab", status: "enabled", members: ["unknown-user-extra"] },
  ] }));
  h.route("POST", "/api/iam/actions", () => response({ outcome: "confirmed", restartRequired: false }));
  await h.ready();
  await h.fire("open-iam");
  const card = accessKey => h.element("iam-list").children.find(item => item.children[0].textContent === accessKey);
  assert.match(card("unknown-user").textContent, /Group membership: unknown/);
  assert.doesNotMatch(card("unknown-user").textContent, /Groups: none/);
  assert.match(card("known-empty").textContent, /Groups: none/);
  assert.match(card("known-member").textContent, /Groups: readers/);
  assert.match(card("legacy-user").textContent, /Group membership: unknown/);
  h.element("iam-kind").value = "groups";
  await h.fire("iam-kind", "change");
  h.element("iam-kind").value = "users";
  await h.fire("iam-kind", "change");
  assert.match(card("unknown-user").textContent, /Listed groups: a · Group membership: unknown/);
  assert.doesNotMatch(card("unknown-user").textContent, /Listed groups: a, ab/);
  assert.doesNotMatch(card("legacy-user").textContent, /Groups: none/);
  await h.fireElement(findText(card("unknown-user"), "Choose for change"));
  h.element("iam-confirm").value = "unknown-user";
  await h.fire("iam-action-form", "submit");
  assert.match(card("unknown-user").textContent, /Group membership: unknown/);
  assert.doesNotMatch(card("unknown-user").textContent, /Listed groups:/);
  await h.fire("close-iam");
  await h.fire("open-iam");
  assert.doesNotMatch(card("unknown-user").textContent, /Listed groups:/);
});

test("IAM deletion retains unknown memberships and reports authoritative rejection without replay", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  h.route("GET", "/api/iam/users", () => response({ users: [{ accessKey: "unknown-user", status: "enabled", memberOf: null, memberOfKnown: false }] }));
  h.route("POST", "/api/iam/actions", call => {
    assert.deepEqual(JSON.parse(call.body), { action: "user.delete", user: "unknown-user", confirmTarget: "unknown-user" });
    return response({ code: "user_has_groups", message: "Remove this user from every group before deleting it." }, 409);
  });
  await h.ready();
  await h.fire("open-iam");
  await h.fireElement(findText(h.element("iam-list"), "Choose for change"));
  h.element("iam-action").value = "user.delete";
  await h.fire("iam-action", "change");
  assert.match(h.element("iam-action-help").textContent, /Current membership is checked again/);
  h.element("iam-confirm").value = "unknown-user";
  await h.fire("iam-action-form", "submit");
  assert.match(h.element("iam-action-status").textContent, /Remove this user from every group/);
  assert.match(h.element("iam-list").textContent, /Group membership: unknown/);
  assert.equal(h.element("workspace").hidden, false);
  assert.equal(h.calls.filter(call => call.url === "/api/iam/actions").length, 1);
  await settle();
  assert.equal(h.calls.filter(call => call.url === "/api/iam/actions").length, 1);
});

test("IAM group membership and service account restrictions use structured exact targets", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  h.route("GET", "/api/iam/groups", () => response({ groups: [{ name: "readers", status: "enabled", members: ["old-user"], policies: ["readonly"] }] }));
  h.route("GET", url => url.startsWith("/api/iam/service-accounts?"), call => {
    assert.equal(new URLSearchParams(call.url.split("?")[1]).get("user"), "parent-user");
    return response({ serviceAccounts: [{ accessKey: "service-key", parentUser: "parent-user", status: "enabled", policy: '{"Statement":[]}' }] });
  });
  h.route("POST", "/api/iam/actions", call => {
    const body = JSON.parse(call.body);
    if (body.action === "group.add-members") assert.deepEqual(body, { action: "group.add-members", group: "readers", members: ["first-user", "second-user"], confirmTarget: "readers" });
    else assert.deepEqual(body, { action: "service-account.policy", accessKey: "service-key", policy: '{"Statement":[]}', confirmTarget: "service-key" });
    return response({ outcome: "confirmed", restartRequired: false });
  });
  await h.ready();
  await h.fire("open-iam");
  h.element("iam-kind").value = "groups";
  await h.fire("iam-kind", "change");
  h.element("iam-action").value = "group.add-members";
  await h.fire("iam-action", "change");
  iamField(h, "group").value = "readers";
  iamField(h, "members").value = "first-user\n\n second-user ";
  h.element("iam-confirm").value = "readers";
  await h.fire("iam-action-form", "submit");
  h.element("iam-kind").value = "service-accounts";
  await h.fire("iam-kind", "change");
  h.element("iam-owner").value = "parent-user";
  await h.fire("iam-list-form", "submit");
  await h.fireElement(findText(h.element("iam-list"), "Choose for change"));
  h.element("iam-action").value = "service-account.policy";
  await h.fire("iam-action", "change");
  assert.equal(iamField(h, "accessKey").value, "service-key");
  assert.equal(iamField(h, "policy").value, '{"Statement":[]}');
  h.element("iam-confirm").value = "service-key";
  await h.fire("iam-action-form", "submit");
});

test("all new dialogs and fields have named accessible status and confirmation controls", () => {
  for (const id of ["bucket-dialog", "versions-dialog", "share-dialog", "archive-dialog", "iam-dialog", "stopped-credential-dialog"]) {
    assert.match(html, new RegExp(`<dialog[^>]*id="${id}"[^>]*aria-labelledby="[^"]+"`));
  }
  for (const id of ["bucket-action-status", "versions-status", "share-status", "archive-status", "iam-status", "iam-action-status"]) assert.match(html, new RegExp(`<p[^>]*id="${id}"[^>]*role="status"`));
  for (const id of ["bucket-action-confirm", "versions-key", "share-url", "iam-confirm", "iam-result-secret", "stopped-result-access", "stopped-result-secret"]) assert.match(html, new RegExp(`<label[^>]*for="${id}"`));
});

test("failed ZIP preparation never offers a partial archive download", async () => {
  const h = createHarness({ readOnly: true, capabilities: fullCapabilities });
  h.route("POST", "/api/archives", () => response({ id: "failed-archive", status: "failed", count: 2, size: 42, error: { code: "archive_failed", message: "An object could not be read." } }));
  await h.ready();
  await h.fire("archive-prefix");
  await h.fire("start-archive");
  assert.match(h.element("archive-status").textContent, /failed/);
  assert.match(h.element("archive-status").textContent, /An object could not be read/);
  assert.doesNotMatch(h.element("archive-status").textContent, /\[object Object\]/);
  assert.equal(h.element("download-archive").hidden, true);
  assert.equal(h.element("cancel-archive").hidden, true);
});

test("late version reads cannot reopen a closed dialog or populate another target", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  const held = deferred();
  h.route("GET", url => url.startsWith("/api/versions?"), () => held.promise);
  await h.ready();
  await h.fire("open-versions");
  h.element("versions-key").value = "old-target";
  await h.fire("versions-form", "submit");
  await h.fire("close-versions");
  await h.fire("open-versions");
  held.resolve(response({ entries: [{ key: "old-target", versionId: "late-version", size: 5 }] }));
  await settle();
  assert.equal(h.element("versions-key").value, "");
  assert.equal(h.element("versions-list").children.length, 0);
});

test("IAM transport loss clears secret and stops all writes without replay", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  h.route("GET", "/api/iam/users", () => response({ users: [] }));
  h.route("POST", "/api/iam/actions", () => { throw new Error("connection dropped"); });
  await h.ready();
  await h.fire("open-iam");
  h.element("iam-action").value = "user.rotate";
  await h.fire("iam-action", "change");
  iamField(h, "user").value = "selected-user";
  iamField(h, "secretKey").value = "unconfirmed-new-secret";
  h.element("iam-confirm").value = "selected-user";
  await h.fire("iam-action-form", "submit");
  assert.equal(h.element("workspace").hidden, true);
  assert.equal(h.element("login-code").disabled, true);
  assert.equal(h.element("iam-result-secret").value, "");
  assert.equal(h.element("iam-fields").children.length, 0);
  assert.match(h.element("login-status").textContent, /unconfirmed/);
  assert.equal(h.calls.filter(call => call.url === "/api/iam/actions").length, 1);
});

test("IAM policy edits require conditional support and a valid fresh revision", async () => {
  for (const binding of [
    { conditional: false, revision: "" },
    { conditional: true, revision: "invalid-revision" },
  ]) {
    const h = createHarness({ capabilities: fullCapabilities });
    h.route("GET", "/api/iam/users", () => response({ users: [] }));
    h.route("GET", url => url.startsWith("/api/iam/bindings?"), () => response({ kind: "user", target: "alice", policies: ["readonly"], ...binding }));
    await h.ready();
    await h.fire("open-iam");
    h.element("iam-action").value = "user.policies";
    await h.fire("iam-action", "change");
    iamField(h, "user").value = "alice";
    await h.fire("load-iam-bindings");
    assert.equal(iamField(h, "policies").value, "readonly");
    assert.equal(iamField(h, "policies").readOnly, true);
    assert.equal(h.element("submit-iam-action").disabled, true);
    h.element("iam-confirm").value = "alice";
    await h.fire("iam-action-form", "submit", { force: true });
    assert.equal(h.calls.some(call => call.url === "/api/iam/actions"), false);
  }
});

test("IAM conditional bindings capture complete policy list and block target changes and repeated writes", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  h.route("GET", "/api/iam/groups", () => response({ groups: [] }));
  h.route("GET", url => url.startsWith("/api/iam/bindings?"), call => {
    const query = new URLSearchParams(call.url.split("?")[1]);
    assert.equal(query.get("kind"), "group");
    assert.equal(query.get("target"), "readers");
    return response({ kind: "group", target: "readers", policies: ["readonly"], revision: "a".repeat(64), conditional: true });
  });
  h.route("POST", "/api/iam/actions", call => {
    assert.deepEqual(JSON.parse(call.body), { action: "group.policies", group: "readers", policies: ["readwrite", "audit"], revision: "a".repeat(64), confirmTarget: "readers" });
    return response({ outcome: "confirmed", restartRequired: false });
  });
  await h.ready();
  await h.fire("open-iam");
  h.element("iam-kind").value = "groups";
  await h.fire("iam-kind", "change");
  h.element("iam-action").value = "group.policies";
  await h.fire("iam-action", "change");
  iamField(h, "group").value = "readers";
  await h.fire("load-iam-bindings");
  assert.equal(iamField(h, "policies").readOnly, false);
  iamField(h, "policies").value = "readwrite\naudit\nreadwrite";
  iamField(h, "group").value = "other-group";
  await h.fireElement(iamField(h, "group"), "input");
  assert.equal(h.element("submit-iam-action").disabled, true);
  await h.fire("iam-action-form", "submit", { force: true });
  assert.equal(h.calls.some(call => call.url === "/api/iam/actions"), false);
  iamField(h, "group").value = "readers";
  await h.fireElement(iamField(h, "group"), "input");
  h.element("iam-confirm").value = "readers";
  await h.fire("iam-action-form", "submit");
  assert.equal(h.element("submit-iam-action").disabled, true);
  assert.match(h.element("iam-binding-status").textContent, /Read current bindings/);
  assert.equal(iamField(h, "policies").value, "readwrite\naudit");
  await h.fire("iam-action-form", "submit", { force: true });
  assert.equal(h.calls.filter(call => call.url === "/api/iam/actions").length, 1);
});

test("binding conflicts preserve drafts and fresh reads require confirmation before discarding edits", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  h.route("GET", "/api/iam/users", () => response({ users: [] }));
  h.route("GET", url => url.startsWith("/api/iam/bindings?"), () => response({ kind: "user", target: "alice", policies: ["readonly"], revision: "a".repeat(64), conditional: true }));
  h.route("POST", "/api/iam/actions", () => response({ code: "bindings_changed", message: "Policy bindings changed. Read again." }, 412));
  await h.ready();
  await h.fire("open-iam");
  h.element("iam-action").value = "user.policies";
  await h.fire("iam-action", "change");
  iamField(h, "user").value = "alice";
  await h.fire("load-iam-bindings");
  iamField(h, "policies").value = "draft-policy";
  h.element("iam-confirm").value = "alice";
  await h.fire("iam-action-form", "submit");
  assert.equal(iamField(h, "policies").value, "draft-policy");
  assert.equal(h.element("submit-iam-action").disabled, true);
  await h.fire("load-iam-bindings");
  assert.equal(h.element("iam-binding-read-confirmation").hidden, false);
  await h.fire("cancel-iam-bindings-read");
  assert.equal(iamField(h, "policies").value, "draft-policy");
  await h.fire("load-iam-bindings");
  h.route("GET", url => url.startsWith("/api/iam/bindings?"), () => response({ code: "AccessDenied", message: "Read denied." }, 403));
  await h.fire("confirm-iam-bindings-read");
  assert.equal(iamField(h, "policies").value, "draft-policy");
  assert.equal(h.element("submit-iam-action").disabled, true);
});

test("ZIP manifest shows resolved versions and download transitions consume a ready archive once", async () => {
  const h = createHarness({ readOnly: true, capabilities: fullCapabilities });
  const archive = { id: "one-use", status: "ready", count: 1, size: 12, transferred: 12, entries: [{ bucket: "listed-bucket", key: "raw-name", versionId: "fixed-v1", size: 12, archivePath: "safe-name" }] };
  h.route("POST", "/api/archives", () => response(archive));
  h.route("GET", "/api/archives/one-use", () => response({ ...archive, status: "succeeded" }));
  await h.ready();
  await h.fire("archive-prefix");
  await h.fire("start-archive");
  assert.match(h.element("archive-items").textContent, /fixed-v1.*safe-name/);
  await h.fire("download-archive");
  assert.equal(h.element("download-archive").hidden, true);
  assert.match(h.element("archive-status").textContent, /Downloading/);
  assert.deepEqual(h.downloads, ["http://127.0.0.1:9001/api/archives/one-use/download"]);
  const callback = [...h.timers.values()][0];
  callback();
  await settle();
  assert.match(h.element("archive-status").textContent, /completed/);
  assert.equal(h.element("download-archive").hidden, true);
  await h.fire("download-archive", "click", { force: true });
  assert.equal(h.downloads.length, 1);
});

test("expired ZIP tasks can be replaced without offering a stale download", async () => {
  const h = createHarness({ readOnly: true, capabilities: fullCapabilities });
  h.route("POST", "/api/archives", () => response({ id: "expired-archive", status: "ready", count: 1, size: 1 }));
  h.route("GET", "/api/archives/expired-archive", () => response({ code: "archive_expired", message: "Archive expired." }, 410));
  await h.ready();
  await h.fire("archive-prefix");
  await h.fire("start-archive");
  const callback = [...h.timers.values()][0];
  callback();
  await settle();
  assert.equal(h.element("download-archive").hidden, true);
  await h.fire("close-archive");
  await h.fire("archive-prefix");
  assert.equal(h.element("start-archive").hidden, false);
  assert.equal(h.element("start-archive").disabled, false);
});

test("session restore recovers its active ZIP task without repeating preparation", async () => {
  const h = createHarness({ readOnly: true, capabilities: fullCapabilities });
  const ready = { id: "recovered-archive", status: "ready", count: 1, size: 10, created: "2026-10-09T00:00:00Z", entries: [{ bucket: "old-bucket", key: "old-key", versionId: "fixed-v1", size: 10, archivePath: "objects/0001/file-old-key" }] };
  h.route("GET", "/api/archives", () => response({ archives: [{ id: "old-finished", status: "succeeded", count: 1, size: 10 }, ready] }));
  h.route("POST", "/api/archives/recovered-archive/cancel", () => response({ ...ready, status: "canceled" }));
  await h.ready();
  assert.equal(h.element("open-current-archive").hidden, false);
  await h.fire("open-current-archive");
  assert.match(h.element("archive-scope").textContent, /Recovered ZIP task/);
  assert.match(h.element("archive-items").textContent, /old-bucket\/old-key.*fixed-v1/);
  assert.equal(h.element("download-archive").href, "/api/archives/recovered-archive/download");
  assert.equal(h.calls.some(call => call.url === "/api/archives" && call.method === "POST"), false);
  await h.fire("cancel-archive");
  assert.match(h.element("archive-status").textContent, /canceled/);
  assert.equal(h.element("download-archive").hidden, true);
});

test("a lost ZIP creation acknowledgement recovers the existing task through a read", async () => {
  const h = createHarness({ readOnly: true, capabilities: fullCapabilities });
  let created = false;
  const ready = { id: "lost-ack", status: "ready", count: 1, size: 10 };
  h.route("GET", "/api/archives", () => response({ archives: created ? [ready] : [] }));
  h.route("POST", "/api/archives", () => { created = true; throw new Error("response lost"); });
  await h.ready();
  await h.fire("archive-prefix");
  await h.fire("start-archive");
  assert.equal(h.element("download-archive").hidden, false);
  assert.equal(h.element("download-archive").href, "/api/archives/lost-ack/download");
  assert.match(h.element("archive-scope").textContent, /Recovered/);
  assert.equal(h.calls.filter(call => call.url === "/api/archives" && call.method === "POST").length, 1);
  assert.equal(h.calls.filter(call => call.url === "/api/archives" && call.method === "GET").length, 2);
});

test("a lost ZIP cancellation acknowledgement refreshes state without repeating cancellation", async () => {
  const h = createHarness({ readOnly: true, capabilities: fullCapabilities });
  let canceled = false;
  const ready = { id: "cancel-lost", status: "ready", count: 1, size: 10 };
  h.route("GET", "/api/archives", () => response({ archives: [{ ...ready, status: canceled ? "canceled" : "ready" }] }));
  h.route("POST", "/api/archives/cancel-lost/cancel", () => { canceled = true; throw new Error("response lost"); });
  await h.ready();
  await h.fire("open-current-archive");
  await h.fire("cancel-archive");
  assert.match(h.element("archive-status").textContent, /canceled/);
  assert.equal(h.element("download-archive").hidden, true);
  assert.equal(h.calls.filter(call => call.url === "/api/archives/cancel-lost/cancel").length, 1);
});

test("service account creation requires a known access key when a secret is supplied", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  await h.ready();
  await h.fire("open-iam");
  h.element("iam-kind").value = "service-accounts";
  await h.fire("iam-kind", "change");
  iamField(h, "user").value = "parent-user";
  iamField(h, "secretKey").value = "supplied-secret-key";
  h.element("iam-confirm").value = "parent-user";
  await h.fire("iam-action-form", "submit");
  assert.match(h.element("iam-action-status").textContent, /Enter an access key/);
  assert.equal(h.calls.some(call => call.url === "/api/iam/actions"), false);
  h.route("POST", "/api/iam/actions", call => {
    assert.deepEqual(JSON.parse(call.body), { action: "service-account.create", user: "parent-user", accessKey: "supplied-access-key", secretKey: "supplied-secret-key", confirmTarget: "parent-user" });
    return response({ outcome: "confirmed", restartRequired: false });
  });
  iamField(h, "accessKey").value = "supplied-access-key";
  await h.fire("iam-action-form", "submit");
  assert.equal(h.calls.filter(call => call.url === "/api/iam/actions").length, 1);
  assert.equal(h.element("iam-result-secret").value, "");
  assert.equal(iamField(h, "secretKey").value, "");
});

test("a confirmed IAM creation preserves its one-time credential after the console stops", async () => {
  for (const closeMethod of ["dismiss", "close", "cancel", "pagehide", "logout"]) {
    const h = createHarness({ capabilities: fullCapabilities });
    h.route("POST", "/api/iam/actions", () => response({ outcome: "confirmed", restartRequired: true, credentials: { accessKey: "created-user", secretKey: "only-confirmed-secret" } }));
    await h.ready();
    await h.fire("open-iam");
    iamField(h, "user").value = "created-user";
    h.element("iam-confirm").value = "created-user";
    await h.fire("iam-action-form", "submit");
    assert.equal(h.element("workspace").hidden, true);
    assert.equal(h.element("iam-dialog").open, false);
    assert.equal(h.element("iam-fields").children.length, 0);
    assert.equal(h.element("iam-result-secret").value, "");
    assert.equal(h.element("stopped-credential-dialog").open, true);
    assert.equal(h.element("stopped-result-access").value, "created-user");
    assert.equal(h.element("stopped-result-secret").value, "only-confirmed-secret");
    assert.equal(h.element("login-code").disabled, true);
    assert.equal(h.element("login-submit").disabled, true);
    await h.fire("reveal-stopped-secret");
    assert.equal(h.element("stopped-result-secret").type, "text");
    const requestsAfterStop = h.calls.length;
    h.element("login-code").value = "no-relogin";
    await h.fire("login-form", "submit", { force: true });
    await h.fire("iam-action-form", "submit", { force: true });
    await h.fire("refresh", "click", { force: true });
    assert.equal(h.calls.length, requestsAfterStop);
    if (closeMethod === "dismiss") await h.fire("dismiss-stopped-secret");
    else if (closeMethod === "close") await h.fire("close-stopped-credential");
    else if (closeMethod === "cancel") await h.fire("stopped-credential-dialog", "cancel");
    else if (closeMethod === "pagehide") await h.windowEvent("pagehide");
    else await h.fire("logout");
    assert.equal(h.element("stopped-credential-dialog").open, false);
    assert.equal(h.element("stopped-result-access").value, "");
    assert.equal(h.element("stopped-result-secret").value, "");
    assert.equal(h.element("stopped-result-secret").type, "password");
    assert.equal(h.calls.length, requestsAfterStop);
    await h.windowEvent("pageshow", { persisted: true });
    assert.equal(h.calls.length, requestsAfterStop);
    assert.equal(h.element("stopped-credential-dialog").open, false);
    assert.equal(h.element("login-submit").disabled, true);
  }
});

test("an unconfirmed IAM reply never displays claimed credentials", async () => {
  const h = createHarness({ capabilities: fullCapabilities });
  h.route("POST", "/api/iam/actions", () => response({ outcome: "unknown", restartRequired: true, credentials: { accessKey: "unconfirmed-user", secretKey: "unconfirmed-secret" } }));
  await h.ready();
  await h.fire("open-iam");
  iamField(h, "user").value = "unconfirmed-user";
  h.element("iam-confirm").value = "unconfirmed-user";
  await h.fire("iam-action-form", "submit");
  assert.equal(h.element("workspace").hidden, true);
  assert.equal(h.element("stopped-credential-dialog").open, false);
  assert.equal(h.element("stopped-result-access").value, "");
  assert.equal(h.element("stopped-result-secret").value, "");
  assert.equal(h.element("iam-result-secret").value, "");
  assert.equal(h.element("login-submit").disabled, true);
  assert.equal(h.calls.filter(call => call.url === "/api/iam/actions").length, 1);
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
