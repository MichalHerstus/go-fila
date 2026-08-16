"use strict";

/* WEdit SPA — vanilla JS, no build step. Talks to the JSON REST API in
   internal/serve/handlers.go. The config object mirrors the YAML field names
   (the server round-trips JSON <-> YAML). */

const state = {
  config: null,
  configPath: "",
  page: "panel",
  resource: null, // drill-in: resource being edited
  pageName: null, // drill-in: page being edited
  dirty: false,
  rawDirty: false, // raw-tab textarea has unsaved typing
  rev: 0, // last-known server config revision
  warnedRev: 0, // rev already announced as "changed on server" (toast dedup)
};

const FIELD_TYPES = [
  "integer", "string", "text", "email", "password", "boolean", "badge",
  "datetime", "date", "image", "file", "select", "relation", "json", "float", "gps",
];

/* ---------- DOM helpers ---------- */

const $ = (sel) => document.querySelector(sel);

function content() {
  const el = $("#content");
  el.innerHTML = "";
  return el;
}

function h2(root, text) {
  const el = document.createElement("h2");
  el.textContent = text;
  root.appendChild(el);
  return el;
}

function h3(root, text) {
  const el = document.createElement("h3");
  el.textContent = text;
  root.appendChild(el);
  return el;
}

function cardEl(root) {
  const el = document.createElement("div");
  el.className = "card";
  root.appendChild(el);
  return el;
}

function gridWrap(card) {
  const el = document.createElement("div");
  el.className = "grid";
  card.appendChild(el);
  return el;
}

function btn(label, cls) {
  const el = document.createElement("button");
  el.className = "btn " + (cls || "");
  el.textContent = label;
  return el;
}

function mkButton(label, onClick) {
  const el = btn(label, "small");
  el.addEventListener("click", onClick);
  return el;
}

function toast(msg, kind) {
  const el = $("#toast");
  el.textContent = msg;
  el.className = "toast " + (kind || "");
  el.classList.remove("hidden");
  clearTimeout(toast._t);
  toast._t = setTimeout(() => el.classList.add("hidden"), 4200);
}

function markDirty() {
  state.dirty = true;
  $("#dirty-indicator").classList.remove("hidden");
}

function clearDirty() {
  state.dirty = false;
  $("#dirty-indicator").classList.add("hidden");
}

/* ---------- editor theme (light/dark) ---------- */

function applyEditorTheme() {
  let saved = null;
  try { saved = localStorage.getItem("wedit-theme"); } catch (e) { /* ignore */ }
  const dark = saved
    ? saved === "dark"
    : window.matchMedia("(prefers-color-scheme: dark)").matches;
  document.documentElement.setAttribute("data-theme", dark ? "dark" : "light");
}

function toggleEditorTheme() {
  const dark = document.documentElement.getAttribute("data-theme") === "dark";
  document.documentElement.setAttribute("data-theme", dark ? "light" : "dark");
  try { localStorage.setItem("wedit-theme", dark ? "light" : "dark"); } catch (e) { /* ignore */ }
}

$("#theme-toggle").addEventListener("click", toggleEditorTheme);

/* ---------- modal helpers ---------- */

let modalResolve = null;

function openModal(title, build) {
  $("#modal-title").textContent = title;
  const body = $("#modal-body");
  body.innerHTML = "";
  const okBtn = $("#modal-ok");
  const cancelBtn = $("#modal-cancel");
  okBtn.classList.remove("hidden");
  cancelBtn.classList.remove("hidden");
  const done = build(body, okBtn, cancelBtn, closeModal);
  if (done) okBtn.classList.add("hidden");
  $("#modal").classList.remove("hidden");
}

function closeModal() {
  $("#modal").classList.add("hidden");
  if (modalResolve) { modalResolve(true); modalResolve = null; }
}

$("#modal-close").addEventListener("click", closeModal);
$("#modal-cancel").addEventListener("click", closeModal);
$("#modal").addEventListener("click", (e) => {
  if (e.target.id === "modal") closeModal();
});

function confirmModal(msg, onYes) {
  openModal("Confirm", (body, ok, cancel, close) => {
    const p = document.createElement("p");
    p.textContent = msg;
    body.appendChild(p);
    ok.textContent = "Confirm";
    ok.addEventListener("click", () => { close(); onYes(); });
    cancel.textContent = "Cancel";
  });
}

function inputModal(title, label, initial, onOk) {
  openModal(title, (body, ok, cancel, close) => {
    const f = document.createElement("div");
    f.className = "field";
    const l = document.createElement("label");
    l.textContent = label;
    const i = document.createElement("input");
    i.type = "text";
    i.value = initial || "";
    f.append(l, i);
    body.appendChild(f);
    ok.addEventListener("click", () => {
      const v = i.value.trim();
      if (!v) return;
      close();
      onOk(v);
    });
    setTimeout(() => i.focus(), 30);
  });
}

function textModal(title, label, initial, onOk) {
  openModal(title, (body, ok, cancel, close) => {
    const f = document.createElement("div");
    f.className = "field";
    const l = document.createElement("label");
    l.textContent = label;
    const i = document.createElement("textarea");
    i.value = initial || "";
    f.append(l, i);
    body.appendChild(f);
    ok.addEventListener("click", () => { close(); onOk(i.value); });
    setTimeout(() => i.focus(), 30);
  });
}

/* ---------- API ---------- */

async function api(method, url, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(url, opts);
  let data = null;
  try { data = await res.json(); } catch (e) { /* non-JSON */ }
  if (!res.ok) {
    const msg = data
      ? (data.errors ? data.errors.join("\n") : (data.error || res.statusText))
      : res.statusText;
    const err = new Error(msg);
    err.status = res.status;
    err.data = data;
    throw err;
  }
  return data;
}

async function apiRawPut(url, text) {
  const res = await fetch(url, { method: "PUT", headers: { "Content-Type": "text/yaml" }, body: text });
  let data = null;
  try { data = await res.json(); } catch (e) { }
  if (!res.ok) {
    const err = new Error(data && data.errors ? data.errors.join("\n") : "invalid YAML");
    err.status = res.status;
    err.data = data;
    throw err;
  }
  return data;
}

/* ---------- form field helpers ---------- */

function fieldEl(grid, label) {
  const wrap = document.createElement("div");
  wrap.className = "field";
  const l = document.createElement("label");
  l.textContent = label;
  wrap.appendChild(l);
  grid.appendChild(wrap);
  return wrap;
}

function textField(grid, label, obj, key) {
  const wrap = fieldEl(grid, label);
  const i = document.createElement("input");
  i.type = "text";
  i.value = obj[key] != null ? obj[key] : "";
  i.addEventListener("change", () => {
    const v = i.value.trim();
    if (v === "") delete obj[key];
    else obj[key] = v;
    markDirty();
  });
  wrap.appendChild(i);
  return i;
}

function numField(grid, label, obj, key) {
  const wrap = fieldEl(grid, label);
  const i = document.createElement("input");
  i.type = "number";
  i.value = obj[key] != null ? obj[key] : "";
  i.addEventListener("change", () => {
    if (i.value === "") delete obj[key];
    else obj[key] = parseInt(i.value, 10);
    markDirty();
  });
  wrap.appendChild(i);
  return i;
}

function boolField(grid, label, obj, key) {
  const wrap = document.createElement("div");
  wrap.className = "checkbox-row field";
  const cb = document.createElement("input");
  cb.type = "checkbox";
  cb.id = "cb-" + key + "-" + Math.random().toString(36).slice(2, 7);
  cb.checked = !!obj[key];
  cb.addEventListener("change", () => { obj[key] = cb.checked; markDirty(); });
  const l = document.createElement("label");
  l.htmlFor = cb.id;
  l.textContent = label;
  wrap.append(cb, l);
  grid.appendChild(wrap);
  return cb;
}

function selectField(grid, label, obj, key, options, { allowEmpty } = {}) {
  const wrap = fieldEl(grid, label);
  const s = document.createElement("select");
  if (allowEmpty) {
    const o = document.createElement("option");
    o.value = "";
    o.textContent = "—";
    s.appendChild(o);
  }
  for (const opt of options) {
    const o = document.createElement("option");
    o.value = opt;
    o.textContent = opt;
    s.appendChild(o);
  }
  s.value = obj[key] != null ? obj[key] : "";
  s.addEventListener("change", () => {
    if (s.value === "") delete obj[key];
    else obj[key] = s.value;
    markDirty();
  });
  wrap.appendChild(s);
  return s;
}

function stringListField(grid, label, arrObj, key) {
  const wrap = fieldEl(grid, label);
  const i = document.createElement("input");
  i.type = "text";
  const cur = arrObj[key] || [];
  i.value = cur.join(", ");
  i.placeholder = "comma-separated";
  i.addEventListener("change", () => {
    const vals = i.value.split(",").map((s) => s.trim()).filter(Boolean);
    if (vals.length) arrObj[key] = vals;
    else delete arrObj[key];
    markDirty();
  });
  wrap.appendChild(i);
  return i;
}

/* ---------- generic collection editor ---------- */

/* ---- Embedded Lua tokenizer + highlighted textarea for script: bodies ---- */
const LUA_KW = new Set("and break do else elseif end false for function goto if in local nil not or repeat return then true until while".split(" "));
function escHtml(s) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}
function luaHighlight(src) {
  let out = "";
  let i = 0;
  const n = src.length;
  const span = (cls, s) => `<span class="${cls}">${s}</span>`;
  while (i < n) {
    const c = src[i];
    if (c === "-" && src[i + 1] === "-" && src[i + 2] === "[") {
      const end = src.indexOf("]]", i + 3);
      const j = end < 0 ? n : end + 2;
      out += span("tk-c", escHtml(src.slice(i, j)));
      i = j;
      continue;
    }
    if (c === "-" && src[i + 1] === "-") {
      let j = src.indexOf("\n", i);
      if (j < 0) j = n;
      out += span("tk-c", escHtml(src.slice(i, j)));
      i = j;
      continue;
    }
    if (c === "[" && src[i + 1] === "[") {
      const end = src.indexOf("]]", i + 2);
      const j = end < 0 ? n : end + 2;
      out += span("tk-s", escHtml(src.slice(i, j)));
      i = j;
      continue;
    }
    if (c === "'" || c === '"') {
      let j = i + 1;
      while (j < n && src[j] !== c) {
        if (src[j] === "\\") j++;
        j++;
      }
      j = Math.min(n, j + 1);
      out += span("tk-s", escHtml(src.slice(i, j)));
      i = j;
      continue;
    }
    if (/[0-9]/.test(c) || (c === "." && /[0-9]/.test(src[i + 1] || ""))) {
      let j = i;
      while (j < n && /[0-9a-fA-FxX._]/.test(src[j])) j++;
      out += span("tk-n", escHtml(src.slice(i, j)));
      i = j;
      continue;
    }
    if (/[A-Za-z_]/.test(c)) {
      let j = i;
      while (j < n && /[A-Za-z0-9_]/.test(src[j])) j++;
      const word = src.slice(i, j);
      out += LUA_KW.has(word) ? span("tk-k", escHtml(word)) : escHtml(word);
      i = j;
      continue;
    }
    out += escHtml(c);
    i++;
  }
  return out;
}

function luaTextArea(value, onChange) {
  const wrap = document.createElement("div");
  wrap.className = "lua-field";
  const view = document.createElement("pre");
  view.className = "lua-view";
  view.innerHTML = luaHighlight(value);
  const ta = document.createElement("textarea");
  ta.spellcheck = false;
  ta.value = value;
  ta.className = "lua-edit";
  ta.addEventListener("input", () => {
    view.innerHTML = luaHighlight(ta.value);
    onChange(ta.value);
  });
  ta.addEventListener("scroll", () => {
    view.scrollTop = ta.scrollTop;
    view.scrollLeft = ta.scrollLeft;
  });
  wrap.append(view, ta);
  return wrap;
}

function collectionEditor(container, items, schema, opts = {}) {
  const wrap = document.createElement("div");
  wrap.className = "table-wrap";
  container.appendChild(wrap);

  function cellInput(s, item, onChange) {
    const set = (v) => { item[s.key] = v; onChange(); };
    if (s.type === "lua") {
      const el = luaTextArea(item[s.key] != null ? item[s.key] : "", (v) => {
        if (v.trim() === "") delete item[s.key];
        else item[s.key] = v;
        onChange();
      });
      return el;
    }
    if (s.type === "bool") {
      const cb = document.createElement("input");
      cb.type = "checkbox";
      cb.checked = !!item[s.key];
      cb.addEventListener("change", () => set(cb.checked));
      return cb;
    }
    if (s.type === "select") {
      const el = document.createElement("select");
      const none = document.createElement("option");
      none.value = "";
      none.textContent = "—";
      el.appendChild(none);
      for (const o of s.options || []) {
        const opt = document.createElement("option");
        opt.value = o;
        opt.textContent = o;
        el.appendChild(opt);
      }
      el.value = item[s.key] != null ? item[s.key] : "";
      el.addEventListener("change", () => {
        if (el.value === "") delete item[s.key];
        else item[s.key] = el.value;
        onChange();
      });
      return el;
    }
    const el = document.createElement("input");
    el.type = s.type === "number" ? "number" : "text";
    el.value = item[s.key] != null ? item[s.key] : "";
    el.addEventListener("change", () => {
      let v = el.value;
      if (s.type === "number") v = v === "" ? undefined : parseInt(v, 10);
      else v = v.trim() === "" ? undefined : v;
      if (v === undefined) delete item[s.key];
      else item[s.key] = v;
      onChange();
    });
    return el;
  }

  function render() {
    wrap.innerHTML = "";
    if (items.length === 0) {
      const p = document.createElement("p");
      p.className = "mono";
      p.textContent = "No entries.";
      wrap.appendChild(p);
    }
    const table = document.createElement("table");
    table.className = "rows";
    const thead = document.createElement("thead");
    const hr = document.createElement("tr");
    for (const s of schema) {
      const th = document.createElement("th");
      th.textContent = s.label || s.key;
      hr.appendChild(th);
    }
    const last = document.createElement("th");
    hr.appendChild(last);
    thead.appendChild(hr);
    table.appendChild(thead);

    const tbody = document.createElement("tbody");
    items.forEach((item, idx) => {
      const tr = document.createElement("tr");
      for (const s of schema) {
        const td = document.createElement("td");
        td.appendChild(cellInput(s, item, () => { markDirty(); if (opts.onChange) opts.onChange(); }));
        tr.appendChild(td);
      }
      const td = document.createElement("td");
      td.className = "row-actions";
      const jsonBtn = mkButton("⋯", () => editRowJSON(item));
      const delBtn = mkButton("✕", () => {
        items.splice(idx, 1);
        markDirty();
        render();
        if (opts.onChange) opts.onChange();
      });
      td.append(jsonBtn, delBtn);
      tr.appendChild(td);
      tbody.appendChild(tr);
    });
    table.appendChild(tbody);
    wrap.appendChild(table);
  }

  function editRowJSON(item) {
    openModal(opts.jsonTitle || "Edit entry (JSON, YAML field names)", (body, ok, cancel, close) => {
      const ta = document.createElement("textarea");
      ta.value = JSON.stringify(item, null, 2);
      body.appendChild(ta);
      const err = document.createElement("div");
      err.style.color = "var(--red)";
      body.appendChild(err);
      ok.addEventListener("click", () => {
        let v;
        try { v = JSON.parse(ta.value); } catch (e) { err.textContent = "Invalid JSON: " + e.message; return; }
        if (!v || typeof v !== "object" || Array.isArray(v)) { err.textContent = "Expected a JSON object"; return; }
        for (const k of Object.keys(item)) delete item[k];
        Object.assign(item, v);
        markDirty();
        close();
        render();
        if (opts.onChange) opts.onChange();
      });
      cancel.textContent = "Close";
    });
  }

  render();
  return { refresh: render };
}

/* ---------- tabs ---------- */

const TABS = [
  ["panel", "Panel"],
  ["connections", "Connections"],
  ["auth", "Auth"],
  ["navigation", "Navigation"],
  ["resources", "Resources"],
  ["pages", "Pages"],
  ["validate", "Validate"],
  ["preview", "Preview"],
  ["raw", "Raw YAML"],
];

function renderTabs() {
  const nav = $("#tabs");
  nav.innerHTML = "";
  for (const [id, label] of TABS) {
    const t = document.createElement("button");
    t.className = "tab" + (state.page === id ? " active" : "");
    t.textContent = label;
    if (id === "resources" && state.config && state.config.resources && state.config.resources.length) {
      const b = document.createElement("span");
      b.className = "badge";
      b.textContent = state.config.resources.length;
      t.appendChild(b);
    }
    t.addEventListener("click", () => switchPage(id));
    nav.appendChild(t);
  }
}

function switchPage(id) {
  state.page = id;
  state.resource = null;
  state.pageName = null;
  renderTabs();
  renderPage();
}

function renderPage() {
  const fns = {
    panel: pagePanel,
    connections: pageConnections,
    auth: pageAuth,
    navigation: pageNavigation,
    resources: pageResources,
    pages: pagePages,
    validate: pageValidate,
    preview: pagePreview,
    raw: pageRaw,
  };
  (fns[state.page] || pagePanel)();
}

/* ---------- page: Panel ---------- */

function pagePanel() {
  const c = state.config;
  const root = content();
  h2(root, "Panel");
  const card = cardEl(root);
  const g = gridWrap(card);

  textField(g, "Name", c.panel, "name");
  textField(g, "ID", c.panel, "id");
  textField(g, "Path", c.panel, "path");
  textField(g, "Logo", c.panel.brand, "logo");
  textField(g, "Favicon", c.panel.brand, "favicon");
  textField(g, "Primary color", c.panel.brand.colors, "primary");
  textField(g, "Secondary color", c.panel.brand.colors, "secondary");
  numField(g, "Sidebar width", c.panel.layout.sidebar, "width");
  numField(g, "Collapsed width", c.panel.layout.sidebar, "collapsed_width");
  boolField(g, "Sidebar collapsible", c.panel.layout.sidebar, "collapsible");
  boolField(g, "Topbar sticky", c.panel.layout.topbar, "sticky");
  selectField(g, "Max content width", c.panel.layout, "max_content_width",
    ["none", "sm", "md", "lg", "xl", "2xl", "3xl", "4xl", "5xl", "6xl", "7xl"], { allowEmpty: true });
  boolField(g, "Dark mode", c.panel.theme, "dark_mode");
  textField(g, "Font family", c.panel.theme.font, "family");
  textField(g, "Mono font", c.panel.theme.font, "mono");
}

/* ---------- page: Connections ---------- */

function pageConnections() {
  const c = state.config;
  if (!c.connections) c.connections = {};
  const root = content();
  h2(root, "Connections");
  for (const name of Object.keys(c.connections)) {
    const conn = c.connections[name];
    const card = cardEl(root);
    const head = document.createElement("div");
    head.className = "toolbar";
    const t = document.createElement("h3");
    t.style.margin = "0";
    t.textContent = name;
    head.appendChild(t);
    const spacer = document.createElement("div");
    spacer.className = "spacer";
    head.appendChild(spacer);
    const del = btn("Delete", "danger small");
    del.addEventListener("click", () => confirmModal(`Delete connection "${name}"?`, () => {
      delete c.connections[name];
      markDirty();
      pageConnections();
    }));
    head.appendChild(del);
    card.appendChild(head);

    const g = gridWrap(card);
    selectField(g, "Driver", conn, "driver", ["postgres", "sqlite", "sqlite3", "mssql", "sqlserver"]);
    textField(g, "DSN", conn, "dsn");
    if (!conn.pool) conn.pool = {};
    numField(g, "Max open", conn.pool, "max_open");
    numField(g, "Max idle", conn.pool, "max_idle");
    textField(g, "Conn lifetime (e.g. 30m)", conn.pool, "lifetime");
  }
  const add = btn("+ Add connection", "primary");
  add.addEventListener("click", () => inputModal("Add connection", "Connection name (unique key, e.g. primary)", "", (name) => {
    if (c.connections[name]) { toast("Connection already exists: " + name, "error"); return; }
    c.connections[name] = { driver: "postgres", dsn: "" };
    markDirty();
    pageConnections();
  }));
  root.appendChild(add);
}

/* ---------- page: Auth ---------- */

function pageAuth() {
  const a = state.config.auth;
  const root = content();
  h2(root, "Auth");
  const card = cardEl(root);
  const g = gridWrap(card);
  textField(g, "Guard", a, "guard");
  textField(g, "Provider", a, "provider");
  textField(g, "Auth table", a, "table");
  stringListField(g, "Login fields", a.login, "fields");
  textField(g, "Login redirect", a.login, "redirect");
  boolField(g, "Registration", a, "registration");
  boolField(g, "Password reset", a, "password_reset");
  boolField(g, "Remember me", a, "remember_me");

  if (!a.login.rate_limit) a.login.rate_limit = {};
  h3(root, "Login rate limit");
  const card2 = cardEl(root);
  const g2 = gridWrap(card2);
  numField(g2, "Max attempts", a.login.rate_limit, "max_attempts");
  numField(g2, "Window seconds", a.login.rate_limit, "window_seconds");
  const hint = document.createElement("p");
  hint.className = "mono";
  hint.textContent = "max_attempts: 0 (or absent) disables rate limiting";
  root.appendChild(hint);
}

/* ---------- page: Navigation (tree) ---------- */

const NAV_ITEM_TYPES = ["resource", "page", "url"];

/* groups whose items are collapsed; defaults to open */
const navCollapsed = new Set();

function navItemTarget(item) {
  if (item.resource) return "/" + String(item.resource).toLowerCase();
  if (item.page) return item.page;
  return item.url || "";
}

function navItemMissing(c, item) {
  if (item.resource) {
    const name = String(item.resource).toLowerCase();
    return !(c.resources || []).some((r) => String(r.name || "").toLowerCase() === name);
  }
  if (item.page) {
    return !(c.pages || []).some((p) => p.name === item.page);
  }
  return false;
}

function navItemModal(c, item, onSave) {
  const typeSel = document.createElement("select");
  for (const t of NAV_ITEM_TYPES) {
    const o = document.createElement("option");
    o.value = t;
    o.textContent = t;
    typeSel.appendChild(o);
  }
  const cur = item.resource ? "resource" : item.page ? "page" : item.url ? "url" : (item.type || "resource");
  typeSel.value = NAV_ITEM_TYPES.includes(cur) ? cur : "resource";

  function field(label, initial, hint) {
    const wrap = document.createElement("div");
    wrap.className = "field";
    const l = document.createElement("label");
    l.textContent = label;
    const i = document.createElement("input");
    i.type = "text";
    i.value = initial || "";
    wrap.append(l, i);
    if (hint) {
      const h = document.createElement("div");
      h.className = "mono";
      h.textContent = hint;
      wrap.appendChild(h);
    }
    return wrap;
  }

  openModal("Edit nav item", (body, ok, cancel, close) => {
    const typeWrap = document.createElement("div");
    typeWrap.className = "field";
    const typeLabel = document.createElement("label");
    typeLabel.textContent = "Type";
    const typeHint = document.createElement("div");
    typeHint.className = "mono";
    typeHint.textContent = "resource / page / url";
    typeWrap.append(typeLabel, typeSel, typeHint);
    body.appendChild(typeWrap);
    const resourceIn = field("Resource (PascalCase)", item.resource, "must match a resource name");
    const pageIn = field("Page", item.page, "must match a page name");
    const urlIn = field("URL", item.url, "external link");
    const labelIn = field("Label (optional override)", item.label, "falls back to the target name");
    const newTab = document.createElement("label");
    newTab.className = "checkbox-row";
    const cb = document.createElement("input");
    cb.type = "checkbox";
    cb.checked = !!item.opens_in_new_tab;
    newTab.append(cb, document.createTextNode("Open in new tab"));
    body.append(resourceIn, pageIn, urlIn, labelIn, newTab);

    const err = document.createElement("div");
    err.style.color = "var(--red)";
    body.appendChild(err);

    const sync = () => {
      resourceIn.style.display = typeSel.value === "resource" ? "" : "none";
      pageIn.style.display = typeSel.value === "page" ? "" : "none";
      urlIn.style.display = typeSel.value === "url" ? "" : "none";
    };
    typeSel.addEventListener("change", sync);
    sync();

    ok.addEventListener("click", () => {
      for (const k of Object.keys(item)) delete item[k];
      item.type = typeSel.value;
      if (typeSel.value === "resource") item.resource = resourceIn.value.trim();
      else if (typeSel.value === "page") item.page = pageIn.value.trim();
      else item.url = urlIn.value.trim();
      if (labelIn.value.trim()) item.label = labelIn.value.trim();
      if (cb.checked) item.opens_in_new_tab = true;
      if (typeSel.value === "url" && !item.url) { err.textContent = "URL is required for url items"; return; }
      if ((typeSel.value === "resource" && !item.resource) || (typeSel.value === "page" && !item.page)) {
        err.textContent = "Target is required"; return;
      }
      close();
      onSave();
    });
    cancel.textContent = "Cancel";
  });
}

function pageNavigation() {
  const c = state.config;
  if (!Array.isArray(c.navigation)) c.navigation = [];
  const root = content();
  h2(root, "Navigation");
  const hint = document.createElement("p");
  hint.className = "mono";
  hint.textContent = "Groups sort by their sort value. Click a group to expand/collapse; hover a row for actions.";
  root.appendChild(hint);

  const list = document.createElement("ul");
  list.className = "tree";
  root.appendChild(list);

  const rerender = () => pageNavigation();

  c.navigation.forEach((group, gi) => {
    if (!Array.isArray(group.items)) group.items = [];

    const li = document.createElement("li");
    li.className = "tree-group" + (navCollapsed.has(gi) ? " collapsed" : "");

    const head = document.createElement("div");
    head.className = "tree-group-head";
    head.addEventListener("click", () => {
      if (navCollapsed.has(gi)) navCollapsed.delete(gi);
      else navCollapsed.add(gi);
      li.classList.toggle("collapsed");
    });

    const chev = document.createElement("span");
    chev.className = "chevron";
    chev.textContent = "▾";

    const title = document.createElement("span");
    title.className = "tree-group-title";
    title.textContent = group.group || "(unnamed)";

    const meta = document.createElement("span");
    meta.className = "tree-group-meta";
    meta.textContent = (group.items || []).length + " item" + (group.items.length === 1 ? "" : "s") +
      (group.icon ? "  ·  " + group.icon : "");

    const actions = document.createElement("span");
    actions.className = "tree-actions";
    actions.addEventListener("click", (e) => e.stopPropagation());
    const editG = mkButton("Edit", () => {
      openModal("Edit group: " + (group.group || "(unnamed)"), (body, ok, cancel, close) => {
        const g = gridWrap(cardEl(body));
        textField(g, "Group name", group, "group");
        textField(g, "Icon", group, "icon");
        numField(g, "Sort", group, "sort");
        ok.addEventListener("click", () => { close(); markDirty(); rerender(); });
        cancel.textContent = "Cancel";
      });
    });
    const addI = mkButton("+ Item", () => {
      group.items.push({ type: "resource" });
      markDirty();
      rerender();
    });
    const delG = mkButton("✕", () => confirmModal(`Delete navigation group "${group.group}"?`, () => {
      c.navigation.splice(gi, 1);
      markDirty();
      rerender();
    }));
    actions.append(editG, addI, delG);

    head.append(chev, title, meta, actions);
    li.appendChild(head);

    const children = document.createElement("ul");
    children.className = "tree-children";
    group.items.forEach((item, ii) => {
      const itemLi = document.createElement("li");
      itemLi.className = "tree-item";

      const badge = document.createElement("span");
      badge.className = "type-badge";
      badge.textContent = item.type || (item.resource ? "resource" : item.page ? "page" : "url");

      const label = document.createElement("span");
      label.className = "tree-label";
      label.textContent = item.label || item.resource || item.page || item.url || "(unnamed)";

      const metaEl = document.createElement("span");
      metaEl.className = "tree-meta";
      metaEl.textContent = navItemTarget(item);

      const itActions = document.createElement("span");
      itActions.className = "tree-actions";
      const editI = mkButton("Edit", () => navItemModal(c, item, () => { markDirty(); rerender(); }));
      const delI = mkButton("✕", () => confirmModal(`Delete nav item "${label.textContent}"?`, () => {
        group.items.splice(ii, 1);
        markDirty();
        rerender();
      }));
      itActions.append(editI, delI);

      itemLi.append(badge, label, metaEl);
      if (item.opens_in_new_tab) {
        const dot = document.createElement("span");
        dot.className = "new-tab-dot";
        dot.title = "opens in new tab";
        itemLi.appendChild(dot);
      }
      if (navItemMissing(c, item)) {
        const m = document.createElement("span");
        m.className = "missing";
        m.textContent = "missing";
        m.title = "references a resource/page that does not exist in this config";
        itemLi.appendChild(m);
      }
      itemLi.appendChild(itActions);
      children.appendChild(itemLi);
    });
    li.appendChild(children);
    list.appendChild(li);
  });

  const add = btn("+ Add group", "primary");
  add.addEventListener("click", () => inputModal("Add navigation group", "Group name", "", (name) => {
    c.navigation.push({ group: name, items: [] });
    markDirty();
    rerender();
  }));
  root.appendChild(add);
}

/* ---------- page: Resources ---------- */

const COLUMN_SCHEMA = [
  { key: "name", label: "Name" },
  { key: "label", label: "Label" },
  { key: "type", label: "Type", type: "select", options: FIELD_TYPES },
];

const FORM_FIELD_SCHEMA = [
  { key: "name", label: "Name" },
  { key: "label", label: "Label" },
  { key: "type", label: "Type", type: "select", options: FIELD_TYPES },
  { key: "required", label: "Req", type: "bool" },
  { key: "options_query", label: "Options query" },
];

const ACTION_SCHEMA = [
  { key: "name", label: "Name" },
  { key: "label", label: "Label" },
  { key: "query", label: "Query" },
  { key: "script", label: "Script (Lua)", type: "lua" },
  { key: "bulk", label: "Bulk", type: "bool" },
  { key: "requires_confirmation", label: "Confirm", type: "bool" },
];

const CHILD_SCHEMA = [
  { key: "name", label: "Section name" },
  { key: "resource", label: "Child resource" },
  { key: "column", label: "FK column" },
];

function renderResourceList() {
  const c = state.config;
  if (!Array.isArray(c.resources)) c.resources = [];
  const root = content();
  h2(root, "Resources");
  c.resources.forEach((r, idx) => {
    const card = cardEl(root);
    const head = document.createElement("div");
    head.className = "toolbar";
    const t = document.createElement("h3");
    t.style.margin = "0";
    t.textContent = r.name;
    if (r.label) {
      const lbl = document.createElement("span");
      lbl.className = "mono";
      lbl.textContent = "— " + r.label;
      t.appendChild(lbl);
    }
    head.appendChild(t);
    const spacer = document.createElement("div");
    spacer.className = "spacer";
    head.appendChild(spacer);
    const editBtn = btn("Edit", "small");
    editBtn.addEventListener("click", () => renderResourceEditor(r.name));
    const del = btn("Delete", "danger small");
    del.addEventListener("click", () => confirmModal(`Delete resource "${r.name}"?`, () => {
      c.resources.splice(idx, 1);
      markDirty();
      renderResourceList();
    }));
    head.append(editBtn, del);
    card.appendChild(head);

    const g = gridWrap(card);
    textField(g, "Label", r, "label");
    textField(g, "Icon", r, "icon");
    textField(g, "Group", r, "group");
    textField(g, "Table", r, "table");
    textField(g, "ID type", r, "id_type");
    textField(g, "ID column", r, "id_column");
    boolField(g, "Import CSV", r, "import_csv");
  });

  const add = btn("+ Add resource", "primary");
  add.addEventListener("click", () => inputModal("Add resource", "Resource name (PascalCase)", "", (name) => {
    c.resources.push({ name });
    markDirty();
    renderResourceList();
  }));
  root.appendChild(add);
}

function resourceCollection(root, r, key, label, schema, title) {
  if (!r[key]) r[key] = {};
  const section = r[key];
  const head = document.createElement("div");
  head.className = "toolbar";
  const it = document.createElement("h3");
  it.style.margin = "0";
  it.textContent = label;
  head.appendChild(it);
  const addIt = btn("+ Add", "small");
  addIt.addEventListener("click", () => {
    section.push({});
    markDirty();
    renderResourceEditor(r.name);
  });
  head.appendChild(addIt);
  root.appendChild(head);
  collectionEditor(root, section, schema, { jsonTitle: title });
}

function renderResourceEditor(name) {
  const c = state.config;
  const r = c.resources.find((x) => x.name === name);
  if (!r) { renderResourceList(); return; }
  const root = content();

  const back = btn("← Resources", "small");
  back.addEventListener("click", () => { state.resource = null; renderPage(); });
  root.appendChild(back);

  const head = document.createElement("div");
  head.className = "toolbar";
  const t = document.createElement("h2");
  t.style.margin = "0";
  t.textContent = "Resource: " + r.name;
  head.appendChild(t);
  root.appendChild(head);

  const card = cardEl(root);
  const g = gridWrap(card);
  textField(g, "Name", r, "name");
  textField(g, "Label", r, "label");
  textField(g, "Icon", r, "icon");
  textField(g, "Group", r, "group");
  textField(g, "Table", r, "table");
  textField(g, "ID type", r, "id_type");
  textField(g, "ID column", r, "id_column");
  boolField(g, "Import CSV", r, "import_csv");

  /* List */
  if (!r.list) r.list = {};
  h3(root, "List");
  const cardL = cardEl(root);
  const gL = gridWrap(cardL);
  textField(gL, "Query", r.list, "query");
  textField(gL, "Count query", r.list, "count_query");
  numField(gL, "Per page", r.list, "per_page");
  textField(gL, "Default sort (leading - = desc)", r.list, "default_sort");
  stringListField(gL, "CSV export columns", r.list, "export");
  if (!r.list.columns) r.list.columns = [];
  const lCols = document.createElement("div");
  lCols.className = "toolbar";
  const lc = document.createElement("h3");
  lc.style.margin = "0";
  lc.textContent = "Columns";
  lCols.appendChild(lc);
  const addCol = btn("+ Add column", "small");
  addCol.addEventListener("click", () => { r.list.columns.push({}); markDirty(); renderResourceEditor(name); });
  lCols.appendChild(addCol);
  cardL.appendChild(lCols);
  collectionEditor(cardL, r.list.columns, COLUMN_SCHEMA, { jsonTitle: "Edit column (JSON)" });

  /* Card */
  if (!r.card) r.card = {};
  h3(root, "Card");
  const cardC = cardEl(root);
  const gC = gridWrap(cardC);
  numField(gC, "Columns", r.card, "columns");
  numField(gC, "Rows", r.card, "rows");
  textField(gC, "Kanban field", r.card, "kanban_field");
  if (!r.card.fields) r.card.fields = [];
  const cCols = document.createElement("div");
  cCols.className = "toolbar";
  const cc = document.createElement("h3");
  cc.style.margin = "0";
  cc.textContent = "Card fields";
  cCols.appendChild(cc);
  const addCardCol = btn("+ Add field", "small");
  addCardCol.addEventListener("click", () => { r.card.fields.push({}); markDirty(); renderResourceEditor(name); });
  cCols.appendChild(addCardCol);
  cardC.appendChild(cCols);
  collectionEditor(cardC, r.card.fields, COLUMN_SCHEMA, { jsonTitle: "Edit card field (JSON)" });

  /* Detail */
  if (!r.detail) r.detail = {};
  h3(root, "Detail");
  const cardD = cardEl(root);
  const gD = gridWrap(cardD);
  textField(gD, "Query", r.detail, "query");
  if (!r.detail.fields) r.detail.fields = [];
  const dCols = document.createElement("div");
  dCols.className = "toolbar";
  const dc = document.createElement("h3");
  dc.style.margin = "0";
  dc.textContent = "Detail fields";
  dCols.appendChild(dc);
  const addDCol = btn("+ Add field", "small");
  addDCol.addEventListener("click", () => { r.detail.fields.push({}); markDirty(); renderResourceEditor(name); });
  dCols.appendChild(addDCol);
  cardD.appendChild(dCols);
  collectionEditor(cardD, r.detail.fields, COLUMN_SCHEMA, { jsonTitle: "Edit detail field (JSON)" });

  /* Form */
  if (!r.form) r.form = {};
  for (const [key, label] of [["create", "Form / Create"], ["update", "Form / Update"], ["delete", "Form / Delete"]]) {
    if (!r.form[key]) r.form[key] = {};
    h3(root, label);
    const cardF = cardEl(root);
    const gF = gridWrap(cardF);
    textField(gF, "Query", r.form[key], "query");
    textField(gF, "Populate query", r.form[key], "populate_query");
    if (!r.form[key].fields) r.form[key].fields = [];
    const fCols = document.createElement("div");
    fCols.className = "toolbar";
    const fc = document.createElement("h3");
    fc.style.margin = "0";
    fc.textContent = "Fields";
    fCols.appendChild(fc);
    const addFCol = btn("+ Add field", "small");
    addFCol.addEventListener("click", () => { r.form[key].fields.push({}); markDirty(); renderResourceEditor(name); });
    fCols.appendChild(addFCol);
    cardF.appendChild(fCols);
    collectionEditor(cardF, r.form[key].fields, FORM_FIELD_SCHEMA, { jsonTitle: "Edit field (JSON)" });
  }

  /* Actions */
  if (!Array.isArray(r.actions)) r.actions = [];
  h3(root, "Actions");
  const cardA = cardEl(root);
  const aCols = document.createElement("div");
  aCols.className = "toolbar";
  const ac = document.createElement("h3");
  ac.style.margin = "0";
  ac.textContent = "Actions";
  aCols.appendChild(ac);
  const addAct = btn("+ Add action", "small");
  addAct.addEventListener("click", () => { r.actions.push({}); markDirty(); renderResourceEditor(name); });
  aCols.appendChild(addAct);
  cardA.appendChild(aCols);
  collectionEditor(cardA, r.actions, ACTION_SCHEMA, { jsonTitle: "Edit action (JSON)" });

  /* Policies */
  if (!r.policies) r.policies = {};
  h3(root, "Policies");
  const cardP = cardEl(root);
  const gP = gridWrap(cardP);
  textField(gP, "view_any", r.policies, "view_any");
  textField(gP, "view", r.policies, "view");
  textField(gP, "create", r.policies, "create");
  textField(gP, "update", r.policies, "update");
  textField(gP, "delete", r.policies, "delete");

  /* Children */
  if (!Array.isArray(r.children)) r.children = [];
  h3(root, "Children (master-detail)");
  const cardCh = cardEl(root);
  const chCols = document.createElement("div");
  chCols.className = "toolbar";
  const chc = document.createElement("h3");
  chc.style.margin = "0";
  chc.textContent = "Children";
  chCols.appendChild(chc);
  const addCh = btn("+ Add child", "small");
  addCh.addEventListener("click", () => { r.children.push({}); markDirty(); renderResourceEditor(name); });
  chCols.appendChild(addCh);
  cardCh.appendChild(chCols);
  collectionEditor(cardCh, r.children, CHILD_SCHEMA, { jsonTitle: "Edit child (JSON)" });
}

function pageResources() {
  if (state.resource) renderResourceEditor(state.resource);
  else renderResourceList();
}

/* ---------- page: Pages ---------- */

function pagePages() {
  const c = state.config;
  if (state.pageName) { renderPageEditor(state.pageName); return; }
  if (!Array.isArray(c.pages)) c.pages = [];
  const root = content();
  h2(root, "Pages");
  c.pages.forEach((p, idx) => {
    const card = cardEl(root);
    const head = document.createElement("div");
    head.className = "toolbar";
    const t = document.createElement("h3");
    t.style.margin = "0";
    t.textContent = p.name + (p.path ? "  (" + p.path + ")" : "");
    if (p.default) {
      const b = document.createElement("span");
      b.className = "badge tab";
      b.textContent = "default";
      t.appendChild(b);
    }
    head.appendChild(t);
    const spacer = document.createElement("div");
    spacer.className = "spacer";
    head.appendChild(spacer);
    const editBtn = btn("Edit", "small");
    editBtn.addEventListener("click", () => { state.pageName = p.name; pagePages(); });
    const del = btn("Delete", "danger small");
    del.addEventListener("click", () => confirmModal(`Delete page "${p.name}"?`, () => {
      c.pages.splice(idx, 1);
      markDirty();
      pagePages();
    }));
    head.append(editBtn, del);
    card.appendChild(head);
  });
  const add = btn("+ Add page", "primary");
  add.addEventListener("click", () => inputModal("Add page", "Page name (PascalCase)", "", (name) => {
    c.pages.push({ name });
    markDirty();
    pagePages();
  }));
  root.appendChild(add);
}

const WIDGET_SCHEMA = [
  { key: "type", label: "Type", type: "select", options: ["stat", "stats_grid", "chart", "table", "list", "html"] },
  { key: "label", label: "Label" },
  { key: "query", label: "Query" },
  { key: "limit", label: "Limit", type: "number" },
  { key: "columns", label: "Columns", type: "number" },
];

function renderPageEditor(name) {
  const c = state.config;
  const p = c.pages.find((x) => x.name === name);
  if (!p) { state.pageName = null; pagePages(); return; }
  const root = content();
  const back = btn("← Pages", "small");
  back.addEventListener("click", () => { state.pageName = null; pagePages(); });
  root.appendChild(back);
  h2(root, "Page: " + p.name);

  const card = cardEl(root);
  const g = gridWrap(card);
  textField(g, "Name", p, "name");
  textField(g, "Path", p, "path");
  boolField(g, "Default page", p, "default");

  if (!Array.isArray(p.widgets)) p.widgets = [];
  h3(root, "Widgets");
  const cardW = cardEl(root);
  const wCols = document.createElement("div");
  wCols.className = "toolbar";
  const wc = document.createElement("h3");
  wc.style.margin = "0";
  wc.textContent = "Widgets";
  wCols.appendChild(wc);
  const addW = btn("+ Add widget", "small");
  addW.addEventListener("click", () => { p.widgets.push({ type: "stat" }); markDirty(); renderPageEditor(name); });
  wCols.appendChild(addW);
  cardW.appendChild(wCols);
  collectionEditor(cardW, p.widgets, WIDGET_SCHEMA, { jsonTitle: "Edit widget (JSON)" });
}

/* ---------- page: Validate ---------- */

async function pageValidate() {
  const root = content();
  h2(root, "Validate");
  const btnRow = document.createElement("div");
  btnRow.className = "toolbar";
  const fix = btn("Fix", "small");
  fix.addEventListener("click", async () => {
    let r;
    try {
      r = await api("POST", "/api/fix");
    } catch (e) {
      toast("fix failed: " + e.message, "error");
      pageValidate();
      return;
    }
    const remaining = (r.errors || []).length;
    if (r.changed) {
      toast("Fixed " + (r.fixed || []).length + " item(s)" + (remaining ? " — " + remaining + " problem(s) remain" : ""), remaining ? "warn" : "ok");
      try { await reloadConfig(); } catch (e) { /* keep the page */ }
    } else {
      toast(remaining ? "Nothing to fix — validation still fails" : "Nothing to fix", remaining ? "warn" : "ok");
    }
    pageValidate();
  });
  btnRow.appendChild(fix);
  const refresh = btn("Refresh", "small");
  refresh.addEventListener("click", () => pageValidate());
  btnRow.appendChild(refresh);
  root.appendChild(btnRow);

  let data;
  try {
    data = await api("GET", "/api/validate");
  } catch (e) {
    toast("validate failed: " + e.message, "error");
    return;
  }
  const findings = data.findings || [];
  const ul = document.createElement("ul");
  ul.className = "findings";
  if (findings.length === 0) {
    const li = document.createElement("li");
    li.className = "good";
    li.textContent = "No problems found.";
    ul.appendChild(li);
  }
  for (const f of findings) {
    const li = document.createElement("li");
    li.className = f.kind === "warning" ? "warning" : "error";
    li.textContent = f.label;
    if (f.detail) {
      const d = document.createElement("div");
      d.className = "origin";
      d.textContent = f.detail;
      li.appendChild(d);
    }
    ul.appendChild(li);
  }
  root.appendChild(ul);
}

/* ---------- page: Preview ---------- */

state.preview = { view: "page", target: "", theme: "auto" };

function pagePreview() {
  const c = state.config;
  const root = content();
  h2(root, "Preview");

  const toolbar = document.createElement("div");
  toolbar.className = "preview-toolbar";

  const errDiv = document.createElement("div");
  errDiv.className = "preview-error hidden";

  const iframe = document.createElement("iframe");
  iframe.className = "preview-frame";
  iframe.title = "Dashboard preview";

  function targets() {
    if (state.preview.view === "resource") return (c.resources || []).map((r) => r.name);
    return (c.pages || []).map((p) => p.name);
  }
  function pickTarget() {
    const ts = targets();
    if (!ts.includes(state.preview.target)) state.preview.target = ts[0] || "";
  }
  pickTarget();

  function loadPreview() {
    const q = new URLSearchParams({ view: state.preview.view, theme: state.preview.theme });
    if (state.preview.target) q.set(state.preview.view === "resource" ? "resource" : "page", state.preview.target);
    iframe.src = "/preview?" + q.toString();
  }

  async function syncConfigThenLoad() {
    errDiv.classList.add("hidden");
    if (state.dirty) {
      try {
        const r = await api("PUT", "/api/config", state.config);
        state.rev = r.rev || state.rev;
      } catch (e) {
        const errors = (e.data && e.data.errors) || [e.message];
        errDiv.textContent = "Config is currently invalid; preview shows the last saved config:\n" + errors.join("\n");
        errDiv.classList.remove("hidden");
      }
    }
    loadPreview();
  }

  const viewSel = document.createElement("select");
  viewSel.id = "preview-view";
  for (const [v, l] of [["page", "Page"], ["resource", "Resource"]]) {
    const o = document.createElement("option");
    o.value = v;
    o.textContent = l;
    viewSel.appendChild(o);
  }
  viewSel.value = state.preview.view;

  const targetSel = document.createElement("select");
  targetSel.id = "preview-target";
  function fillTargets() {
    targetSel.innerHTML = "";
    const ts = targets();
    if (ts.length === 0) {
      const o = document.createElement("option");
      o.value = "";
      o.textContent = state.preview.view === "resource" ? "(no resources)" : "(no pages)";
      targetSel.appendChild(o);
    }
    for (const t of ts) {
      const o = document.createElement("option");
      o.value = t;
      o.textContent = t;
      targetSel.appendChild(o);
    }
    if (ts.includes(state.preview.target)) targetSel.value = state.preview.target;
  }
  fillTargets();

  const themeSel = document.createElement("select");
  themeSel.id = "preview-theme";
  for (const [v, l] of [["auto", "Auto"], ["light", "Light"], ["dark", "Dark"]]) {
    const o = document.createElement("option");
    o.value = v;
    o.textContent = l;
    themeSel.appendChild(o);
  }
  themeSel.value = state.preview.theme;

  const refresh = btn("Refresh", "primary");
  refresh.addEventListener("click", syncConfigThenLoad);

  viewSel.addEventListener("change", () => {
    state.preview.view = viewSel.value;
    state.preview.target = targets()[0] || "";
    fillTargets();
    syncConfigThenLoad();
  });
  targetSel.addEventListener("change", () => {
    state.preview.target = targetSel.value;
    syncConfigThenLoad();
  });
  themeSel.addEventListener("change", () => {
    state.preview.theme = themeSel.value;
    loadPreview();
  });

  toolbar.append(viewSel, targetSel, themeSel);
  const spacer = document.createElement("div");
  spacer.className = "spacer";
  toolbar.appendChild(spacer);
  toolbar.appendChild(refresh);

  root.append(toolbar, errDiv, iframe);
  syncConfigThenLoad();
}

/* ---------- page: Raw YAML ---------- */

async function pageRaw() {
  const root = content();
  h2(root, "Raw YAML");
  const hint = document.createElement("p");
  hint.className = "mono";
  hint.textContent = "Full config as YAML. Apply validates it and replaces the in-memory config; Save then writes it to disk.";
  root.appendChild(hint);
  let data;
  try {
    data = await api("GET", "/api/raw");
  } catch (e) {
    toast("load failed: " + e.message, "error");
    return;
  }
  state.rawDirty = false;
  const ta = document.createElement("textarea");
  ta.className = "raw-editor";
  ta.value = data.yaml;
  ta.addEventListener("input", () => { state.rawDirty = true; });
  root.appendChild(ta);

  const row = document.createElement("div");
  row.className = "toolbar";
  const apply = btn("Apply", "primary");
  apply.addEventListener("click", async () => {
    try {
      if (!(await checkStaleRev())) {
        state.rawDirty = false;
        await reloadConfig();
        renderTabs();
        renderPage();
        return;
      }
      const r = await apiRawPut("/api/raw", ta.value);
      state.rev = r.rev || state.rev;
      state.rawDirty = false;
      toast("YAML applied (validated). Press Save to write it.", "ok");
      clearDirty();
      await reloadConfig();
      renderTabs();
    } catch (e) {
      toast("Invalid YAML:\n" + e.message, "error");
    }
  });
  row.appendChild(apply);
  root.appendChild(row);
}

/* ---------- save ---------- */

$("#save-btn").addEventListener("click", save);

/* Confirm-style modal that resolves true for "Overwrite" and false for
   "Reload" — used by the stale-write guard below. */
function confirmOverwriteModal() {
  return new Promise((resolve) => {
    openModal("Confirm", (body, ok, cancel, close) => {
      const p = document.createElement("p");
      p.textContent = "The config changed on the server since you loaded it (likely an agent/MCP edit). " +
        "Overwrite the server copy with your changes, or reload to see the latest?";
      body.appendChild(p);
      ok.textContent = "Overwrite";
      ok.addEventListener("click", () => { close(); resolve(true); });
      cancel.textContent = "Reload";
      cancel.addEventListener("click", () => { close(); resolve(false); });
    });
  });
}

/* Checks whether another writer (MCP/agent) replaced the config since we last
   loaded it. Resolves true to proceed, false to reload first. */
async function checkStaleRev() {
  let rev;
  try {
    rev = (await api("GET", "/api/rev")).rev;
  } catch (e) {
    return true; // rev endpoint unavailable — do not block saves
  }
  if (rev === state.rev) return true;
  return confirmOverwriteModal();
}

async function save() {
  const btn = $("#save-btn");
  btn.disabled = true;
  const status = $("#save-status");
  status.textContent = "validating…";
  try {
    if (!(await checkStaleRev())) {
      state.rawDirty = false;
      await reloadConfig();
      renderTabs();
      renderPage();
      status.textContent = "";
      toast("Reloaded the latest config; your unsaved edits were discarded.", "ok");
      return;
    }
    if (state.page === "raw") {
      const ta = $(".raw-editor");
      if (!ta) throw new Error("raw editor not loaded");
      const r = await apiRawPut("/api/raw", ta.value);
      state.rev = r.rev || state.rev;
      state.rawDirty = false;
    } else {
      const r = await api("PUT", "/api/config", state.config);
      state.rev = r.rev || state.rev;
    }
    status.textContent = "saving…";
    await api("POST", "/api/save");
    status.textContent = "";
    clearDirty();
    toast("Saved to " + state.configPath, "ok");
    await reloadConfig();
    renderTabs();
  } catch (e) {
    status.textContent = "";
    toast("Save failed:\n" + e.message, "error");
  } finally {
    btn.disabled = false;
  }
}

async function reloadConfig() {
  const data = await api("GET", "/api/config");
  state.config = data.config;
  state.configPath = data.path;
  state.rev = data.rev || 0;
  $("#config-path").textContent = data.path;
}

/* ---------- server-change events (SSE) ---------- */

function connectEvents() {
  const es = new EventSource("/api/events");
  es.addEventListener("rev", (e) => onServerRev(Number(e.data)));
}

async function onServerRev(rev) {
  if (!rev || rev === state.rev) return;
  if (state.dirty || state.rawDirty) {
    if (state.warnedRev !== rev) {
      state.warnedRev = rev;
      toast("Config changed on the server (agent/MCP) — save or reload to see it.", "warn");
    }
    return;
  }
  state.warnedRev = 0;
  try {
    await reloadConfig();
  } catch (e) {
    return;
  }
  renderTabs();
  renderPage();
}

/* ---------- init ---------- */

async function init() {
  applyEditorTheme();
  try {
    const data = await api("GET", "/api/config");
    state.config = data.config;
    state.configPath = data.path;
    state.rev = data.rev || 0;
    $("#config-path").textContent = data.path;
    renderTabs();
    renderPage();
    connectEvents();
  } catch (e) {
    const root = content();
    const p = document.createElement("p");
    p.textContent = "Failed to load config: " + e.message;
    root.appendChild(p);
  }
}

init();
