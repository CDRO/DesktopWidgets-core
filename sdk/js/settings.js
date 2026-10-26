// Settings window, generated from the `settings` schema in widget.yml.
import { call, ready } from "/dw/dw.js";

const fields = document.getElementById("fields");
const error = document.getElementById("error");

const [info, schema] = await Promise.all([ready, call("dw.schema")]);
document.getElementById("title").textContent = `${info.name} settings`;
document.title = `${info.name} – Settings`;

for (const s of schema || []) fields.appendChild(field(s, info.settings[s.key]));

function field(s, value) {
  const wrap = document.createElement("label");
  wrap.className = `field ${s.type}`;
  const label = document.createElement("span");
  label.textContent = s.label;
  let input;
  switch (s.type) {
    case "bool":
      input = document.createElement("input");
      input.type = "checkbox";
      input.checked = !!value;
      break;
    case "enum":
      input = document.createElement("select");
      for (const o of s.options) input.add(new Option(o, o, false, o === value));
      break;
    case "number":
      input = document.createElement("input");
      input.type = "number";
      input.step = "any";
      if (s.min != null) input.min = s.min;
      if (s.max != null) input.max = s.max;
      input.value = value ?? "";
      break;
    default:
      input = document.createElement("input");
      input.type = s.type === "secret" ? "password" : "text";
      input.value = value ?? "";
  }
  input.name = s.key;
  input.dataset.type = s.type;
  if (s.type === "bool") wrap.append(input, label);
  else wrap.append(label, input);
  if (s.description) {
    const d = document.createElement("small");
    d.textContent = s.description;
    wrap.append(d);
  }
  return wrap;
}

document.getElementById("form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const values = {};
  for (const el of fields.querySelectorAll("[name]")) {
    const t = el.dataset.type;
    values[el.name] = t === "bool" ? el.checked : t === "number" ? Number(el.value) : el.value;
  }
  try {
    await call("dw.save", values);
    await call("dw.close");
  } catch (err) {
    error.textContent = err.message;
  }
});

document.getElementById("cancel").addEventListener("click", () => call("dw.close"));
