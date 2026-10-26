import * as dw from "/dw/dw.js";

const list = document.getElementById("quotes");
const status = document.getElementById("status");
const lastPrice = new Map();

const fmt = (n, currency) =>
  new Intl.NumberFormat(undefined, {
    style: currency ? "currency" : "decimal",
    currency: currency || undefined,
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(n);

function sparkline(values) {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  if (!values || values.length < 2) return svg;
  const w = 100, h = 22;
  const min = Math.min(...values), max = Math.max(...values);
  const span = max - min || 1;
  const pts = values.map((v, i) => `${(i / (values.length - 1)) * w},${h - ((v - min) / span) * h}`);
  svg.setAttribute("viewBox", `0 0 ${w} ${h}`);
  svg.setAttribute("preserveAspectRatio", "none");
  const line = document.createElementNS(svg.namespaceURI, "polyline");
  line.setAttribute("points", pts.join(" "));
  line.setAttribute("vector-effect", "non-scaling-stroke");
  svg.appendChild(line);
  return svg;
}

function row(q) {
  const li = document.createElement("li");
  const sym = Object.assign(document.createElement("span"), { className: "sym", textContent: q.symbol });
  const price = Object.assign(document.createElement("span"), { className: "price" });
  const chg = Object.assign(document.createElement("span"), { className: "chg" });
  if (q.error) {
    li.className = "err";
    price.textContent = "unavailable";
    price.title = q.error;
    li.append(sym, document.createElement("span"), price, chg);
    return li;
  }
  li.className = q.change >= 0 ? "up" : "down";
  price.textContent = fmt(q.price, q.currency);
  chg.textContent = `${q.change >= 0 ? "+" : ""}${q.changePercent.toFixed(2)}%`;
  li.append(sym, sparkline(q.spark), price, chg);

  const prev = lastPrice.get(q.symbol);
  if (prev !== undefined && prev !== q.price) li.classList.add(q.price > prev ? "flash-up" : "flash-down");
  lastPrice.set(q.symbol, q.price);
  return li;
}

dw.subscribe("quotes", (quotes, { error, at }) => {
  if (quotes) list.replaceChildren(...quotes.map(row));
  status.classList.toggle("stale", !!error);
  status.textContent = error
    ? `Update failed: ${error}`
    : `Updated ${at.toLocaleTimeString()}`;
});
