import * as dw from "/dw/dw.js";

dw.subscribe("greeting", (data, { error }) => {
  document.getElementById("text").textContent = data ? data.text : "…";
  document.getElementById("time").textContent = error ? `Error: ${error}` : data.time;
});
