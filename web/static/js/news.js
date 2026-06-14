// News feed panel with unread badge, plus toast + tell-flare helpers.
let unread = 0;
let badge, panel, list, seen = new Set();

export function initNews({ onCompose }) {
  badge = document.getElementById("news-badge");
  panel = document.getElementById("news-panel");
  list = document.getElementById("news-list");

  document.getElementById("news-btn").addEventListener("click", togglePanel);
  document.getElementById("news-close").addEventListener("click", togglePanel);
  document.getElementById("news-compose").addEventListener("click", onCompose);
}

function togglePanel() {
  panel.classList.toggle("open");
  if (panel.classList.contains("open")) {
    unread = 0; badge.hidden = true; badge.textContent = "0";
  }
}

// onNews renders a single incoming item (and bumps the badge if unseen).
export function onNews(item) {
  if (seen.has(item.id)) return;
  seen.add(item.id);
  prepend(item);
  if (!panel.classList.contains("open")) {
    unread++; badge.textContent = unread; badge.hidden = false;
    document.getElementById("news-btn").classList.add("pulse");
    setTimeout(() => document.getElementById("news-btn").classList.remove("pulse"), 600);
  }
}

// renderNewsList syncs the full feed from a snapshot (oldest first in array).
export function renderNewsList(items) {
  for (const it of items) {
    if (!seen.has(it.id)) { seen.add(it.id); prepend(it); }
  }
}

function prepend(item) {
  const li = document.createElement("li");
  li.className = "news-item" + (item.source === "system" ? " system" : "");
  li.innerHTML = `
    <div class="news-src">${esc(item.sourceName)} <span class="news-tick">t${item.tick}</span></div>
    <div class="news-head-line">${esc(item.headline)}</div>
    ${item.body ? `<div class="news-body">${esc(item.body)}</div>` : ""}`;
  list.prepend(li);
  while (list.children.length > 60) list.removeChild(list.lastChild);
}

export function pushToast(text, kind = "") {
  const wrap = document.getElementById("toasts");
  const t = document.createElement("div");
  t.className = "toast " + kind;
  t.textContent = text;
  wrap.appendChild(t);
  requestAnimationFrame(() => t.classList.add("show"));
  setTimeout(() => {
    t.classList.remove("show");
    setTimeout(() => t.remove(), 300);
  }, 4200);
}

export function tellFlare() {
  const f = document.getElementById("tell-flare");
  f.classList.remove("active");
  void f.offsetWidth; // restart animation
  f.classList.add("active");
}

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}
