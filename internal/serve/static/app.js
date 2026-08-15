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
  analyze: null, // cached GET /api/analyze result
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

function collectionEditor(container, items, schema, opts = {}) {
  const wrap = document.createElement("div");
  wrap.className = "table-wrap";
  container.appendChild(wrap);

  function cellInput(s, item, onChange) {
    const set = (v) => { item[s.key] = v; onChange(); };
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
  ["sqlc", "SQLC"],
  ["auth", "Auth"],
  ["navigation", "Navigation"],
  ["resources", "Resources"],
  ["pages", "Pages"],
  ["queries", "Queries"],
  ["validate", "Validate"],
  ["sync", "Sync"],
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
    sqlc: pageSQLC,
    auth: pageAuth,
    navigation: pageNavigation,
    resources: pageResources,
    pages: pagePages,
    queries: pageQueries,
    validate: pageValidate,
    sync: pageSync,
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

/* ---------- page: SQLC ---------- */

function pageSQLC() {
  const s = state.config.sqlc;
  const root = content();
  h2(root, "SQLC");
  const card = cardEl(root);
  const g = gridWrap(card);
  textField(g, "Config file", s, "config");
  textField(g, "Queries dir", s, "queries_dir");
  textField(g, "Schema dir", s, "schema_dir");
  textField(g, "Output package", s, "output_pkg");
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

/* ---------- page: Navigation ---------- */

const NAV_ITEM_SCHEMA = [
  { key: "type", label: "Type", type: "select", options: ["resource", "page", "url"] },
  { key: "resource", label: "Resource" },
  { key: "page", label: "Page" },
  { key: "url", label: "URL" },
  { key: "label", label: "Label" },
  { key: "opens_in_new_tab", label: "New tab", type: "bool" },
];

function pageNavigation() {
  const c = state.config;
  if (!Array.isArray(c.navigation)) c.navigation = [];
  const root = content();
  h2(root, "Navigation");
  c.navigation.forEach((group, gi) => {
    const card = cardEl(root);
    const head = document.createElement("div");
    head.className = "toolbar";
    const t = document.createElement("h3");
    t.style.margin = "0";
    t.textContent = group.group || "(unnamed)";
    head.appendChild(t);
    const spacer = document.createElement("div");
    spacer.className = "spacer";
    head.appendChild(spacer);
    const del = btn("Delete group", "danger small");
    del.addEventListener("click", () => confirmModal(`Delete navigation group "${group.group}"?`, () => {
      c.navigation.splice(gi, 1);
      markDirty();
      pageNavigation();
    }));
    head.appendChild(del);
    card.appendChild(head);

    const g = gridWrap(card);
    textField(g, "Group name", group, "group");
    textField(g, "Icon", group, "icon");
    numField(g, "Sort", group, "sort");

    if (!Array.isArray(group.items)) group.items = [];
    const itemsHead = document.createElement("div");
    itemsHead.className = "toolbar";
    const it = document.createElement("h3");
    it.style.margin = "0";
    it.textContent = "Items";
    itemsHead.appendChild(it);
    const addIt = btn("+ Add item", "small");
    addIt.addEventListener("click", () => {
      group.items.push({ type: "resource" });
      markDirty();
      pageNavigation();
    });
    itemsHead.appendChild(addIt);
    card.appendChild(itemsHead);
    collectionEditor(card, group.items, NAV_ITEM_SCHEMA, { jsonTitle: "Edit nav item (JSON)" });
  });

  const add = btn("+ Add group", "primary");
  add.addEventListener("click", () => inputModal("Add navigation group", "Group name", "", (name) => {
    c.navigation.push({ group: name, items: [] });
    markDirty();
    pageNavigation();
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

/* ---------- page: Queries ---------- */

async function pageQueries() {
  const root = content();
  h2(root, "SQLC Queries");
  const hint = document.createElement("p");
  hint.className = "mono";
  hint.textContent = "Click a query to edit its SQL body. Changes are staged and flushed on Save.";
  root.appendChild(hint);
  let rep;
  try {
    rep = await api("GET", "/api/analyze");
  } catch (e) {
    const p = document.createElement("p");
    p.textContent = "analyze failed: " + e.message;
    root.appendChild(p);
    return;
  }
  state.analyze = rep;
  const ul = document.createElement("ul");
  ul.className = "code-list";
  for (const q of rep.queries) {
    const li = document.createElement("li");
    const nameEl = document.createElement("a");
    nameEl.href = "#";
    nameEl.textContent = q.name;
    nameEl.style.color = "var(--accent)";
    nameEl.addEventListener("click", (ev) => { ev.preventDefault(); queryEditor(q.name); });
    const fileEl = document.createElement("span");
    fileEl.className = "origin";
    fileEl.textContent = q.file;
    li.append(nameEl, fileEl);
    ul.appendChild(li);
  }
  root.appendChild(ul);
}

async function queryEditor(name) {
  let data;
  try {
    data = await api("GET", "/api/queries/" + encodeURIComponent(name));
  } catch (e) {
    toast("Cannot load query: " + e.message, "error");
    return;
  }
  openModal("SQL: " + data.name + "  (" + data.file + ")", (body, ok, cancel, close) => {
    const ta = document.createElement("textarea");
    ta.value = data.body;
    body.appendChild(ta);
    const status = document.createElement("div");
    status.className = "save-status";
    body.appendChild(status);
    ok.textContent = "Stage";
    ok.addEventListener("click", async () => {
      status.textContent = "…";
      try {
        await api("PUT", "/api/queries", { name: data.name, body: ta.value });
        status.textContent = "staged (flushed on Save)";
        ok.classList.add("hidden");
      } catch (e) {
        status.textContent = "failed: " + e.message;
      }
    });
    cancel.textContent = "Close";
  });
}

/* ---------- page: Validate ---------- */

async function pageValidate() {
  const root = content();
  h2(root, "Validate");
  const btnRow = document.createElement("div");
  btnRow.className = "toolbar";
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

/* ---------- page: Sync ---------- */

async function pageSync() {
  const root = content();
  h2(root, "SQL ↔ YAML Sync");
  const btnRow = document.createElement("div");
  btnRow.className = "toolbar";
  const gen = btn("Generate missing queries", "primary");
  gen.addEventListener("click", async () => {
    try {
      const res = await api("POST", "/api/generate-queries");
      if (res.written && res.written.length) toast("Generated " + res.written.length + " query file(s): " + res.written.join(", "), "ok");
      else toast("Nothing to generate: all queries present", "ok");
      pageSync();
    } catch (e) {
      toast("generate failed: " + e.message, "error");
    }
  });
  const refresh = btn("Refresh", "small");
  refresh.addEventListener("click", () => pageSync());
  btnRow.append(gen, refresh);
  root.appendChild(btnRow);

  let rep;
  try {
    rep = await api("GET", "/api/analyze");
  } catch (e) {
    toast("analyze failed: " + e.message, "error");
    return;
  }
  state.analyze = rep;
  if (rep.err) {
    const p = document.createElement("p");
    p.textContent = rep.err;
    root.appendChild(p);
    return;
  }
  const ul = document.createElement("ul");
  ul.className = "findings";
  const add = (kind, text) => {
    const li = document.createElement("li");
    li.className = kind;
    li.textContent = text;
    ul.appendChild(li);
  };
  if (rep.missing_queries.length) add("error", "missing queries: " + rep.missing_queries.length);
  if (rep.missing_tables.length) add("error", "missing tables: " + rep.missing_tables.join(", "));
  if (rep.missing_columns.length) add("warning", "missing columns: " + rep.missing_columns.length);
  if (rep.fk_targets.length) add("warning", "FK target List queries missing: " + rep.fk_targets.length);
  if (rep.missing_queries.length === 0 && rep.missing_tables.length === 0 && rep.missing_columns.length === 0 && rep.fk_targets.length === 0) {
    add("good", "Schema, queries and YAML references are in sync.");
  }
  root.appendChild(ul);

  const grid = document.createElement("div");
  grid.className = "grid";
  const tabCard = cardEl(grid);
  const qCard = cardEl(grid);
  root.appendChild(grid);

  h3(tabCard, "Schema tables (" + rep.tables.length + ")");
  const tl = document.createElement("ul");
  tl.className = "code-list";
  for (const t of rep.tables) {
    const li = document.createElement("li");
    li.textContent = t.name;
    const c = document.createElement("span");
    c.className = "origin";
    c.textContent = t.cols + " cols";
    li.appendChild(c);
    tl.appendChild(li);
  }
  tabCard.appendChild(tl);

  h3(qCard, "Query definitions (" + rep.queries.length + ")");
  const ql = document.createElement("ul");
  ql.className = "code-list";
  for (const q of rep.queries) {
    const li = document.createElement("li");
    const a = document.createElement("a");
    a.href = "#";
    a.textContent = q.name;
    a.style.color = "var(--accent)";
    a.addEventListener("click", (ev) => { ev.preventDefault(); queryEditor(q.name); });
    li.appendChild(a);
    ql.appendChild(li);
  }
  qCard.appendChild(ql);
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
  const ta = document.createElement("textarea");
  ta.className = "raw-editor";
  ta.value = data.yaml;
  root.appendChild(ta);

  const row = document.createElement("div");
  row.className = "toolbar";
  const apply = btn("Apply", "primary");
  apply.addEventListener("click", async () => {
    try {
      await apiRawPut("/api/raw", ta.value);
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

async function save() {
  const btn = $("#save-btn");
  btn.disabled = true;
  const status = $("#save-status");
  status.textContent = "validating…";
  try {
    if (state.page === "raw") {
      const ta = $(".raw-editor");
      if (!ta) throw new Error("raw editor not loaded");
      await apiRawPut("/api/raw", ta.value);
    } else {
      await api("PUT", "/api/config", state.config);
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
  $("#config-path").textContent = data.path;
}

/* ---------- init ---------- */

async function init() {
  try {
    const data = await api("GET", "/api/config");
    state.config = data.config;
    state.configPath = data.path;
    $("#config-path").textContent = data.path;
    renderTabs();
    renderPage();
  } catch (e) {
    const root = content();
    const p = document.createElement("p");
    p.textContent = "Failed to load config: " + e.message;
    root.appendChild(p);
  }
}

init();
