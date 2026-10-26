import * as dw from "/dw/dw.js";

const $ = (id) => document.getElementById(id);
const main = document.querySelector("main");
let cfg = { style: "analog", format24h: true, showSeconds: true, timeZone: "" };

// Hour ticks
for (let i = 0; i < 60; i += 5) {
  const t = document.createElementNS("http://www.w3.org/2000/svg", "line");
  const major = i % 15 === 0;
  t.setAttribute("y1", -88);
  t.setAttribute("y2", major ? -76 : -82);
  t.setAttribute("transform", `rotate(${i * 6})`);
  if (major) t.classList.add("major");
  $("ticks").appendChild(t);
}

// Read wall-clock parts in the configured zone (falls back to local time).
function parts(now) {
  try {
    const f = new Intl.DateTimeFormat("en-US", {
      timeZone: cfg.timeZone || undefined,
      hour: "numeric", minute: "numeric", second: "numeric", hourCycle: "h23",
    });
    const p = Object.fromEntries(f.formatToParts(now).map((x) => [x.type, x.value]));
    return { h: +p.hour, m: +p.minute, s: +p.second };
  } catch {
    return { h: now.getHours(), m: now.getMinutes(), s: now.getSeconds() };
  }
}

function zone() {
  try {
    new Intl.DateTimeFormat(undefined, { timeZone: cfg.timeZone || undefined });
    return cfg.timeZone || undefined;
  } catch {
    return undefined;
  }
}

let lastSecond = -1;
function render() {
  const now = new Date();
  const { h, m, s } = parts(now);
  if (s === lastSecond) return;
  lastSecond = s;

  const tz = zone();
  $("date").textContent = now.toLocaleDateString(undefined, { timeZone: tz, weekday: "short", day: "numeric", month: "short" });

  if (cfg.style === "digital") {
    $("time").textContent = now.toLocaleTimeString(undefined, {
      timeZone: tz,
      hour: "2-digit",
      minute: "2-digit",
      second: cfg.showSeconds ? "2-digit" : undefined,
      hour12: !cfg.format24h,
    });
    return;
  }
  const sec = $("second");
  sec.style.display = cfg.showSeconds ? "" : "none";
  // Avoid the hand sweeping backwards from 59 to 0.
  sec.classList.toggle("no-anim", s === 0);
  sec.style.transform = `rotate(${s * 6}deg)`;
  $("minute").style.transform = `rotate(${m * 6 + s * 0.1}deg)`;
  $("hour").style.transform = `rotate(${(h % 12) * 30 + m * 0.5}deg)`;
}

dw.onSettings((s) => {
  cfg = { ...cfg, ...s };
  main.className = `dw-card ${cfg.style}`;
  lastSecond = -1;
  render();
});

main.className = `dw-card ${cfg.style}`;
render();
setInterval(render, 200);
