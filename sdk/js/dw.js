// DesktopWidgets frontend SDK. Served by the runtime at /dw/dw.js.
//
//   import * as dw from "/dw/dw.js";
//   dw.subscribe("time", (data) => render(data));
//   dw.onSettings((s) => applySettings(s));
//
import { Events } from "/wails/runtime.js";

const pending = new Map();
const subscribers = new Map();
const latest = new Map();
const settingsListeners = new Set();
const updateListeners = new Set();
let settings = {};
let info = null;
let seq = 0;
const clientId = Math.random().toString(36).slice(2);

Events.On("dw:reply", (ev) => {
  const r = ev.data;
  const p = r && pending.get(r.id);
  if (!p) return;
  pending.delete(r.id);
  r.error ? p.reject(new Error(r.error)) : p.resolve(r.result);
});

Events.On("dw:data", (ev) => {
  const d = ev.data;
  latest.set(d.name, d);
  for (const cb of subscribers.get(d.name) || []) deliver(cb, d);
});

Events.On("dw:settings", (ev) => {
  settings = ev.data || {};
  for (const cb of settingsListeners) cb(settings);
});

Events.On("dw:update", (ev) => {
  if (info) info.update = ev.data;
  for (const cb of updateListeners) cb(ev.data);
  if (updateListeners.size === 0) showUpdateBadge(ev.data);
});

Events.On("dw:locked", (ev) => setLocked(ev.data));

function deliver(cb, d) {
  try {
    cb(d.data, { error: d.error || null, at: new Date(d.at) });
  } catch (e) {
    console.error(e);
  }
}

/** Call a runtime ("dw.*") or widget handler registered with runtime.Handle. */
export function call(method, args = {}) {
  const id = `${clientId}-${++seq}`;
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    Events.Emit("dw:call", { id, method, args });
    setTimeout(() => {
      if (pending.delete(id)) reject(new Error(`dw.call ${method}: timeout`));
    }, 60000);
  });
}

/**
 * Receive data from a provider declared in widget.yml.
 * cb(data, {error, at}) runs for every refresh; on error, data is the last good value.
 */
export function subscribe(name, cb) {
  if (!subscribers.has(name)) subscribers.set(name, new Set());
  subscribers.get(name).add(cb);
  if (latest.has(name)) deliver(cb, latest.get(name));
  return () => subscribers.get(name).delete(cb);
}

/** Ask a provider (or all, if name is omitted) to refresh now. */
export const refresh = (name = "") => call("dw.refresh", { name });

/** Current settings; cb runs now and on every change. */
export function onSettings(cb) {
  settingsListeners.add(cb);
  if (info) cb(settings);
  return () => settingsListeners.delete(cb);
}
export const getSettings = () => settings;

/**
 * Handle update notifications yourself. Without a listener the SDK shows a
 * small badge for major/critical updates. info.prompt says whether to be visible.
 */
export function onUpdate(cb) {
  updateListeners.add(cb);
  if (info && info.update) cb(info.update);
  return () => updateListeners.delete(cb);
}

export const openURL = (url) => call("dw.openURL", { url });

/** Open the pending update in the library app (or the release page). */
export const openUpdate = () => call("dw.update");
export const widget = () => info;

/** Resolves once the runtime handshake is done. */
export const ready = call("dw.ready").then((i) => {
  info = i;
  settings = i.settings || {};
  setLocked(i.locked);
  for (const cb of settingsListeners) cb(settings);
  if (i.update) {
    for (const cb of updateListeners) cb(i.update);
    if (updateListeners.size === 0) showUpdateBadge(i.update);
  }
  return i;
});

function setLocked(locked) {
  document.documentElement.classList.toggle("dw-locked", !!locked);
}

function showUpdateBadge(u) {
  if (!u || !u.prompt || document.getElementById("dw-update")) return;
  const b = document.createElement("button");
  b.id = "dw-update";
  b.className = "dw-update-badge" + (u.critical ? " dw-critical" : "");
  b.title = `${u.critical ? "Security update" : "Update"} ${u.version} available`;
  b.textContent = "↑";
  b.addEventListener("click", () => openUpdate());
  document.body.appendChild(b);
}

// Base styles + context menu + drag behaviour for every widget page.
// Pages that are not the widget itself opt out with <html data-dw-plain>.
if (!document.documentElement.hasAttribute("data-dw-plain")) {
  const css = document.createElement("link");
  css.rel = "stylesheet";
  css.href = "/dw/dw.css";
  document.head.prepend(css);
}
