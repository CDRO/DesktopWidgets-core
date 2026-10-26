import { call, ready } from "/dw/dw.js";
import { Events } from "/wails/runtime.js";

const grid = document.getElementById("grid");
const status = document.getElementById("status");
const search = document.getElementById("search");
const tmpl = document.getElementById("card");
let state = { widgets: [] };
let focusId = "";

function cmpVersion(a, b) {
  const pa = a.split(".").map(Number), pb = b.split(".").map(Number);
  for (let i = 0; i < 3; i++) if ((pa[i] || 0) !== (pb[i] || 0)) return (pa[i] || 0) - (pb[i] || 0);
  return 0;
}

function setStatus(text, error = false) {
  status.textContent = text;
  status.classList.toggle("error", error);
}

function render() {
  const q = search.value.trim().toLowerCase();
  const list = state.widgets.filter(
    (w) => w.kind !== "app" && (!q || `${w.name} ${w.description} ${w.category}`.toLowerCase().includes(q)),
  );
  grid.replaceChildren(...list.map(card));
  const el = focusId && grid.querySelector(`[data-id="${CSS.escape(focusId)}"]`);
  if (el) el.scrollIntoView({ block: "center" });
}

function card(w) {
  const el = tmpl.content.firstElementChild.cloneNode(true);
  el.dataset.id = w.id;
  el.classList.toggle("focus", w.id === focusId);
  el.querySelector(".icon").src = w.icon;
  el.querySelector("h2").textContent = w.name;
  el.querySelector(".desc").textContent = w.description;
  const meta = el.querySelector(".meta");
  const primary = el.querySelector(".primary");
  const open = el.querySelector(".open");

  const hasUpdate = w.installed && cmpVersion(w.version, w.installed) > 0;
  if (!w.installed) {
    meta.textContent = `Version ${w.version}`;
    primary.textContent = "Install";
    open.hidden = true;
  } else if (hasUpdate) {
    meta.innerHTML = "";
    meta.append(`Installed ${w.installed} · `);
    const u = document.createElement("span");
    u.className = w.critical ? "update critical" : "update";
    u.textContent = `${w.critical ? "Security update" : "Update"} ${w.version}`;
    meta.append(u);
    if (w.notes) meta.title = w.notes;
    primary.textContent = "Update";
  } else {
    meta.textContent = `Installed ${w.installed} · up to date`;
    primary.hidden = true;
  }
  if (!w.asset) {
    primary.disabled = true;
    primary.title = "No download for this system";
  } else {
    primary.title = `${w.asset.file} (${(w.asset.size / 1048576).toFixed(1)} MB)`;
  }
  primary.addEventListener("click", () => doInstall(w, primary));
  open.addEventListener("click", () => call("library.launch", { id: w.id }).catch((e) => setStatus(e.message, true)));
  return el;
}

async function doInstall(w, btn) {
  btn.disabled = true;
  const label = btn.textContent;
  btn.textContent = "Installing…";
  setStatus(`Installing ${w.name} ${w.version}…`);
  try {
    await call("library.install", { id: w.id });
    setStatus(`${w.name} ${w.version} installed.`);
    await load();
  } catch (e) {
    setStatus(e.message, true);
    btn.textContent = label;
    btn.disabled = false;
  }
}

async function load() {
  try {
    state = await call("library.list");
    const n = state.widgets.filter((w) => w.kind !== "app").length;
    setStatus(`${n} widgets · release ${state.release}`);
    render();
  } catch (e) {
    setStatus(e.message, true);
  }
}

search.addEventListener("input", render);
document.getElementById("reload").addEventListener("click", load);
Events.On("dw:second-instance", (ev) => {
  const args = ev.data || [];
  const i = args.indexOf("--show");
  if (i >= 0 && args[i + 1]) {
    focusId = args[i + 1];
    load();
  }
});

await ready;
focusId = await call("library.focus");
load();
