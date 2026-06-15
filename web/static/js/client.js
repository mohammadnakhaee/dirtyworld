// Entry point: WebSocket client, state dispatch, and all UI wiring.
import { initMap, renderMap, isInteracting } from "./map.js";
import { drawSeries, RESOURCE_COLORS } from "./charts.js";
import { initNews, onNews, renderNewsList, pushToast, tellFlare } from "./news.js";

const G = window.GAME;
const app = {
  ws: null,
  state: null,     // latest StateMsg
  me: null,        // own playerId
  view: null,      // playerId currently shown on the map
  marketReady: false,
  mapSig: null,    // last-rendered map signature (skip redundant re-renders)
};

// ---- WebSocket ----
function connect() {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const q = new URLSearchParams({ country: G.country, token: G.token || "" });
  app.ws = new WebSocket(`${proto}://${location.host}/ws/${encodeURIComponent(G.room)}?${q}`);

  app.ws.onopen = () => setConn("live", true);
  app.ws.onclose = (ev) => {
    if (ev.code === 4001) { // final join rejection — do not reconnect
      showFatal(ev.reason || "You can't join this room.");
      return;
    }
    if (app.noReconnect) return; // room was closed
    setConn("disconnected", false);
    setTimeout(connect, 1500);
  };
  app.ws.onerror = () => setConn("error", false);
  app.ws.onmessage = (ev) => {
    let env; try { env = JSON.parse(ev.data); } catch { return; }
    dispatch(env.type, env.payload);
  };
}

function send(type, payload) {
  if (app.ws && app.ws.readyState === 1) {
    app.ws.send(JSON.stringify({ type, payload }));
  }
}

function dispatch(type, p) {
  switch (type) {
    case "snapshot":
      app.me = p.self.playerId;
      if (app.view == null) app.view = app.me;
      renderNewsList(p.news || []);
      applyState(p);
      break;
    case "tick":
    case "map.update":
      applyState(p);
      break;
    case "news.item": onNews(p); break;
    case "combat.result": {
      const verb = p.destroyed ? "destroyed" : "hit";
      pushToast(`${p.attacker} ${verb} ${p.defender}'s ${p.subtype} (-${p.cashLost})`, "warn");
      break;
    }
    case "tell": tellFlare(); pushToast(p.message, "tell"); break;
    case "notice": pushToast(p.text, "info"); break;
    case "error": pushToast(p.error, "error"); break;
    case "win": showWin(p); break;
    case "room.closed":
      app.noReconnect = true;
      showFatal(p.message || "The host closed the room.");
      break;
  }
}

// ---- state -> UI ----
function applyState(s) {
  app.state = s;
  if (!app.marketReady && document.getElementById("market-grid")) {
    initMarketGrid(s.market, s.catalog);
    app.marketReady = true;
  }
  renderLobby(s);
  renderTop(s.self, ownCountry(s));
  renderBoard(s.board);
  updateMarket(s);
  renderStock(s.self.resources, s.catalog);
  renderGoals(s.self, s.catalog);
  renderServiceCosts(s);
  renderTierGates(s);
  renderTabs(s);
  scheduleMapRender();
}

// svcCost converts a general-unit service price into the player's own currency
// at their current exchange rate.
function svcCost(svc) {
  const s = app.state;
  if (!s) return 0;
  const general = (s.catalog.services || {})[svc] || 0;
  const rate = s.country.exchangeRate || 1;
  return Math.max(1, Math.round(general / rate));
}

// renderServiceCosts fills each [data-svc] label with the converted price.
function renderServiceCosts(s) {
  document.querySelectorAll("[data-svc]").forEach((el) => {
    const general = (s.catalog.services || {})[el.dataset.svc] || 0;
    el.textContent = ` (${svcCost(el.dataset.svc)} ${G.currency})`;
    el.title = `${general} general units at exchange rate ${(s.country.exchangeRate || 1).toFixed(2)}`;
  });
}

// renderLobby shows the waiting room until the host starts the match.
function renderLobby(s) {
  const lobby = document.getElementById("lobby");
  if (s.started) { lobby.hidden = true; return; }
  lobby.hidden = false;
  const list = document.getElementById("lobby-list");
  list.innerHTML = s.board.map((e) => `
    <li class="lobby-row ${e.self ? "you" : ""} ${e.connected ? "" : "off"}">
      <span class="dot" style="background:${e.palette}"></span>
      <span class="lname">${esc(e.name)}</span>
      <span class="tag">${e.host ? "host" : ""}${e.self ? (e.host ? " · you" : "you") : ""}${e.connected ? "" : " · offline"}</span>
    </li>`).join("");
  document.getElementById("lobby-host").hidden = !s.self.isHost;
  document.getElementById("lobby-wait").hidden = s.self.isHost;
}

function ownCountry(s) { return s.country; }

function findView(s, id) {
  if (id === s.country.playerId) return { country: s.country, own: true, spied: true };
  const r = (s.rivals || []).find((x) => x.playerId === id);
  return r ? { country: r, own: false, spied: r.spied } : { country: s.country, own: true, spied: true };
}

// scheduleMapRender debounces render requests, so clicking through several tabs
// quickly collapses into a single render of the final map instead of a pile of
// synchronous SVG rebuilds.
let mapTimer = 0;
function scheduleMapRender(force) {
  if (force) app.mapSig = null;
  if (mapTimer) clearTimeout(mapTimer);
  mapTimer = setTimeout(() => {
    mapTimer = 0;
    if (isInteracting()) { scheduleMapRender(); return; } // wait until the click finishes
    renderActiveMap();
  }, 45);
}

// mapSignature captures everything that affects the rendered map, so we only
// rebuild the (expensive) SVG when something actually changed — not every tick.
function mapSignature(s, v) {
  if (!v.own && !v.spied) return "locked:" + v.country.playerId;
  const bucket = Math.floor((s.tick || 0) / 4); // refresh cooldown rings ~every 2s
  const pls = (v.country.placeables || [])
    .map((p) => `${p.id},${p.x.toFixed(3)},${p.y.toFixed(3)},${Math.round(p.hp)}`)
    .join("|");
  return `${v.country.playerId}|${v.own}|${v.spied}|${bucket}|${pls}`;
}

function renderActiveMap() {
  const mapEl = document.getElementById("map");
  if (!mapEl) return; // not on the map page
  const s = app.state; if (!s) return;
  if (isInteracting()) return;
  const v = findView(s, app.view);

  const sig = mapSignature(s, v);
  if (sig === app.mapSig) return; // nothing changed — skip the rebuild
  app.mapSig = sig;

  const title = document.getElementById("map-title");
  const hint = document.getElementById("map-hint");

  // A rival's map is hidden until you spy them.
  if (!v.own && !v.spied) {
    title.textContent = `${v.country.name} — hidden`;
    mapEl.innerHTML =
      `<text x="500" y="480" text-anchor="middle" fill="#8b96bd" font-size="44">🔒</text>
       <text x="500" y="560" text-anchor="middle" fill="#8b96bd" font-size="30">Spy on ${esc(v.country.name)} to reveal its map</text>`;
    if (hint) hint.textContent = "Buy espionage (Spy) from the console to scout — and then attack — this country.";
    return;
  }

  title.textContent = v.own ? "Your country" : `${v.country.name} (spied)`;
  if (hint) {
    hint.textContent = v.own
      ? "Drag your buildings to reposition. Click a damaged one to repair it."
      : `⚔ Targeting ${v.country.name} — click a building to strike it.`;
  }
  renderMap(mapEl, v.country, v.own);
}

let tabSig = null;

function renderTabs(s) {
  const tabs = document.getElementById("map-tabs");
  if (!tabs) return; // not on the map page

  const entries = [{ id: s.country.playerId, name: G.country + " (you)", palette: s.country.palette, self: true }]
    .concat((s.rivals || []).map((r) => ({ id: r.playerId, name: r.name, palette: r.palette, self: false })));

  // Only rebuild the buttons when the set of players changes — rebuilding every
  // tick was destroying buttons mid-click, so clicks got dropped.
  const sig = entries.map((e) => e.id + ":" + e.name).join("|");
  if (sig !== tabSig) {
    tabSig = sig;
    tabs.innerHTML = "";
    for (const e of entries) {
      const b = document.createElement("button");
      b.className = "tab" + (e.self ? " self" : "");
      b.dataset.view = e.id;
      b.style.setProperty("--c", e.palette);
      b.textContent = e.name;
      b.onclick = () => { app.view = e.id; updateTabActive(); scheduleMapRender(true); };
      tabs.appendChild(b);
    }
  }
  updateTabActive();
}

// updateTabActive just toggles the highlight — cheap, and never recreates the
// buttons (so it can't swallow clicks).
function updateTabActive() {
  document.querySelectorAll("#map-tabs .tab").forEach((b) => {
    b.classList.toggle("active", b.dataset.view === app.view);
  });
}

function renderTop(self, country) {
  setStat("stat-cash", fmt(self.cash));
  setStat("stat-capital", fmt(self.capital));
  setStat("stat-nukes", `${self.nukes}/${app.state.catalog.nukeTarget}`);
  setStat("stat-rate", (country.exchangeRate || 0).toFixed(3));
  setStat("stat-world", self.world || "—");
}

function renderBoard(board) {
  const ul = document.getElementById("board");
  if (!ul) return;
  ul.innerHTML = "";
  board.forEach((e, i) => {
    const li = document.createElement("li");
    li.className = "board-row" + (e.self ? " self" : "");
    li.innerHTML = `
      <span class="rank">${i + 1}</span>
      <span class="dot" style="background:${e.palette}"></span>
      <span class="bname">${esc(e.name)} <span class="world world-${tierClass(e.world)}">${esc(e.world)}</span></span>
      <span class="bcur">${esc(e.currency)}</span>
      <span class="brate">${e.exchangeRate.toFixed(3)}</span>`;
    ul.appendChild(li);
  });
}

// renderStock shows the integer count of every resource (icons + numbers).
function renderStock(resources, cat) {
  const el = document.getElementById("stock");
  if (!el || !cat) return;
  resources = resources || {};
  el.innerHTML = cat.resources.map((r) => {
    const n = resources[r.name] || 0;
    return `<span class="stock-item ${n > 0 ? "" : "zero"}" title="${r.name}">${r.icon}<b>${n}</b></span>`;
  }).join("");
}

// renderGoals shows progress toward the two public win conditions.
function renderGoals(self, cat) {
  if (!cat || !document.getElementById("goal-nukes")) return;
  const nf = Math.min(100, (self.nukes / cat.nukeTarget) * 100);
  document.getElementById("goal-nukes").textContent = `${self.nukes} / ${cat.nukeTarget}`;
  document.getElementById("nuke-bar").style.width = nf + "%";
  const cf = Math.min(100, (self.capital / cat.capitalTarget) * 100);
  document.getElementById("goal-capital").textContent =
    `${fmt(self.capital)} / ${cat.capitalTarget.toLocaleString()}`;
  document.getElementById("capital-bar").style.width = cf + "%";
}

// ---- resource market: one figure per resource, with 1-unit buy/sell ----
const TRADE_COOLDOWN = 10; // seconds both buttons are disabled after a click
const marketCells = {};    // resource name -> cell refs

function initMarketGrid(market, cat) {
  const grid = document.getElementById("market-grid");
  if (!grid) return;
  grid.innerHTML = "";
  for (const k in marketCells) delete marketCells[k];
  for (const c of market) {
    const icon = (cat.resources.find((r) => r.name === c.name) || {}).icon || "";
    const cell = document.createElement("div");
    cell.className = "market-cell";
    cell.innerHTML = `
      <div class="mc-head"><span class="mc-name">${icon} ${esc(c.name)}</span><span class="mc-price">—</span></div>
      <canvas width="240" height="42"></canvas>
      <div class="mc-foot">
        <span class="mc-have">own <b>0</b></span>
        <div class="mc-btns">
          <button class="btn small buy">Buy 1</button>
          <button class="btn small sell">Sell 1</button>
        </div>
      </div>`;
    grid.appendChild(cell);
    const buyBtn = cell.querySelector(".buy");
    const sellBtn = cell.querySelector(".sell");
    buyBtn.onclick = () => tradeOne(c.name, "buy");
    sellBtn.onclick = () => tradeOne(c.name, "sell");
    marketCells[c.name] = {
      priceEl: cell.querySelector(".mc-price"),
      haveEl: cell.querySelector(".mc-have b"),
      canvas: cell.querySelector("canvas"),
      buyBtn, sellBtn, timer: null,
    };
  }
}

function updateMarket(s) {
  const rate = s.country.exchangeRate || 1;
  for (const c of s.market) {
    const cell = marketCells[c.name];
    if (!cell) continue;
    // Convert the world price to the player's currency by exchange rate, like
    // the Industry-card prices.
    const price = Math.round(c.price / rate);
    cell.priceEl.innerHTML = `${price} <span class="cur">${esc(G.currency)}</span>`;
    cell.haveEl.textContent = (s.self.resources || {})[c.name] || 0;
    drawSeries(cell.canvas, c.history || [], RESOURCE_COLORS[c.name] || "#a78bfa");
  }
}

// tradeOne buys/sells exactly one unit, then locks both of that resource's
// buttons for TRADE_COOLDOWN seconds.
function tradeOne(name, side) {
  send("market.order", { commodity: name, side, qty: 1 });
  const cell = marketCells[name];
  if (!cell || cell.timer) return; // already cooling down
  let left = TRADE_COOLDOWN;
  const paint = () => {
    cell.buyBtn.disabled = cell.sellBtn.disabled = true;
    cell.buyBtn.textContent = `Buy ${left}s`;
    cell.sellBtn.textContent = `Sell ${left}s`;
  };
  paint();
  cell.timer = setInterval(() => {
    left -= 1;
    if (left <= 0) {
      clearInterval(cell.timer);
      cell.timer = null;
      cell.buyBtn.disabled = cell.sellBtn.disabled = false;
      cell.buyBtn.textContent = "Buy 1";
      cell.sellBtn.textContent = "Sell 1";
    } else {
      paint();
    }
  }, 1000);
}

// ---- helpers ----
function setStat(id, v) {
  const el = document.querySelector(`#${id} span`);
  if (el) el.textContent = v;
}
function setConn(text, ok) {
  const e = document.getElementById("conn");
  e.textContent = text; e.className = ok ? "ok" : "bad";
}
function fmt(n) { return Math.round(n).toLocaleString(); }
function tierClass(world) {
  return world === "First World" ? "first" : world === "Second World" ? "second" : "third";
}
function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

// ---- modal ----
function modal(title, bodyHTML, onOk) {
  const m = document.getElementById("modal");
  document.getElementById("modal-title").textContent = title;
  document.getElementById("modal-body").innerHTML = bodyHTML;
  m.hidden = false;
  const ok = document.getElementById("modal-ok");
  const cancel = document.getElementById("modal-cancel");
  const close = () => { m.hidden = true; ok.onclick = null; cancel.onclick = null; };
  cancel.onclick = close;
  ok.onclick = () => { if (onOk(m) !== false) close(); };
}

function rivalOptions() {
  return (app.state.rivals || [])
    .map((r) => `<option value="${r.playerId}">${esc(r.name)}</option>`).join("");
}

// on sets a click handler only if the element exists (pages differ by mode).
function on(id, handler) {
  const el = document.getElementById(id);
  if (el) el.onclick = handler;
}

// ---- operations wiring (most ops live on the console page) ----
function wireOps() {
  on("op-build-mil", () => send("service.buy", { kind: "military", subtype: "army" }));
  on("op-build-agency", () => send("service.buy", { kind: "agency", subtype: "press" }));
  on("op-factory", openFactoryMenu);
  on("op-nuke", openNuke);

  on("op-spy", () => {
    if (!app.state.rivals.length) return pushToast("No rivals to spy on yet.", "info");
    modal(`Run espionage (${svcCost("spy")} ${G.currency})`,
      `<label>Target<select id="x-t">${rivalOptions()}</select></label>
       <p class="muted small">Permanent visibility on their map. ${app.state.catalog.services.spy} general units → ${svcCost("spy")} ${G.currency}.</p>`,
      () => send("spy", { targetPlayer: document.getElementById("x-t").value }));
  });

  on("op-hack", () => {
    if (!app.state.rivals.length) return pushToast("No rivals to target yet.", "info");
    modal(`Plant fake news (${svcCost("hack")} ${G.currency})`, `
      <label>Frame this nation<select id="x-t">${rivalOptions()}</select></label>
      <label>Headline<input id="x-h" placeholder="Central bank in crisis" /></label>
      <label>Body<textarea id="x-b" rows="3"></textarea></label>
      <p class="muted small">${app.state.catalog.services.hack} general units → ${svcCost("hack")} ${G.currency}.</p>`, () => {
      send("news.hack", {
        targetPlayer: document.getElementById("x-t").value,
        headline: document.getElementById("x-h").value,
        body: document.getElementById("x-b").value,
      });
    });
  });

  on("op-satellite", openSatellite);
  on("lobby-start", () => send("game.start", {}));
}

// openSatellite confirms launching a First-World satellite (reveals all maps).
function openSatellite() {
  modal(`Launch a satellite (${svcCost("satellite")} ${G.currency})`, `
    <p>A spy satellite permanently reveals <b>every rival's map</b> — and lets you target them — without paying for espionage each time.</p>
    <p class="muted small">First-World tech. ${app.state.catalog.services.satellite} general units → ${svcCost("satellite")} ${G.currency}.</p>`,
    () => send("satellite.build", {}));
}

// renderTierGates disables tech the nation hasn't developed to yet.
function renderTierGates(s) {
  const tier = s.self.tier || 0;
  gate("op-spy", tier < 1, "Reach Second World to run espionage");
  gate("op-nuke", tier < 2, "Reach First World to assemble nukes");
  gate("op-satellite", tier < 2 || s.self.hasSatellite,
    s.self.hasSatellite ? "Satellite already in orbit" : "Reach First World to launch a satellite");
}

function gate(id, locked, why) {
  const el = document.getElementById(id);
  if (!el) return;
  el.disabled = !!locked;
  el.title = locked ? why : "";
}

// openFactoryMenu lists every factory with its recipe, payout and whether the
// player can currently afford it; building consumes the recipe from stock.
function openFactoryMenu() {
  const cat = app.state.catalog;
  const res = app.state.self.resources || {};
  const tier = app.state.self.tier || 0;
  const iconOf = (name) => (cat.resources.find((r) => r.name === name) || {}).icon || "";
  const sections = [
    { tier: 0, name: "Third World" },
    { tier: 1, name: "Second World" },
    { tier: 2, name: "First World" },
  ];
  const html = sections.map((sec) => {
    const locked = tier < sec.tier;
    const rows = cat.factories.filter((f) => f.minTier === sec.tier).map((f) => {
      // The recipe (needs) is always shown.
      const recipe = Object.entries(f.recipe)
        .map(([r, n]) => `${n}×${iconOf(r)}${r}`).join(" + ");
      const resOK = Object.entries(f.recipe).every(([r, n]) => (res[r] || 0) >= n);
      const ok = !locked && resOK;
      return `<div class="fac-row ${ok ? "" : "short"}">
        <div class="fac-info">
          <span class="fac-ttl">${f.icon} ${esc(f.title)}</span>
          <span class="muted small">${recipe} · ~${Math.round(f.payout)}/cycle</span>
        </div>
        <button class="btn small ${ok ? "primary" : ""}" data-fac="${f.key}" ${ok ? "" : "disabled"}>Build</button>
      </div>`;
    }).join("");
    return `<div class="fac-section${locked ? " locked" : ""}">
      <div class="fac-sec-h">${esc(sec.name)}${locked ? " 🔒" : ""}</div>${rows}</div>`;
  }).join("");
  modal("Build a factory", `<div class="fac-list">${html}</div>
    <p class="muted small">Recipes are always shown; a section unlocks when your nation reaches that world level. Resources are consumed when you build.</p>`,
    () => false);
  document.querySelectorAll("#modal [data-fac]").forEach((b) => {
    b.onclick = () => {
      send("factory.build", { type: b.dataset.fac });
      document.getElementById("modal").hidden = true;
    };
  });
}

// openNuke confirms assembling a nuclear weapon (oil + uranium + cash).
function openNuke() {
  const cat = app.state.catalog;
  const recipe = Object.entries(cat.nukeRecipe).map(([r, n]) => `${n} ${r}`).join(" + ");
  modal("Assemble a nuclear weapon", `
    <p>Cost: <b>${recipe}</b> + <b>${Math.round(cat.nukeCash)}</b> cash.</p>
    <p class="muted small">You have ${app.state.self.nukes}/${cat.nukeTarget}. Reach ${cat.nukeTarget} to win instantly.</p>`,
    () => send("nuke.build", {}));
}

// confirmAttack is the map-driven path: the player has clicked a specific rival
// building, so we just confirm the target and ask for a budget.
function confirmAttack(country, pl) {
  if (pl.hp <= 0) { pushToast(`${pl.subtype} is already destroyed.`, "info"); return; }
  const value = typeof pl.value === "number" ? ` · worth ~$${Math.round(pl.value)}` : " · value hidden (spy to reveal)";
  modal(`Strike ${esc(country.name)}`, `
    <p>Target: <b>${esc(pl.subtype)}</b> (${pl.kind}) — about ${Math.round(pl.hp)} HP${value}.</p>
    <label>Budget to commit<input id="x-s" type="number" min="100" value="1500" /></label>
    <p class="muted small">Spend more than the target's defense to wreck it. You need a ready military building to project force.</p>`,
    () => {
      app.view = country.playerId; // keep watching the target so you see the hit land
      send("attack", {
        targetPlayer: country.playerId,
        targetPlaceable: pl.id,
        spend: parseFloat(document.getElementById("x-s").value) || 0,
      });
    });
}

// ---- map interactions ----
function onMapMove(id, x, y) { send("placeable.move", { id, x, y }); }

function onMapClick(country, pl, own) {
  if (own) {
    if (pl.hp < pl.maxHp) {
      const cost = Math.round((pl.maxHp - pl.hp) * 6);
      modal("Repair " + pl.subtype, `<p>Restore ${esc(pl.subtype)} to full health for ~${cost} ${esc(G.currency)}?</p>`,
        () => send("repair", { id: pl.id }));
    } else {
      pushToast(`${pl.subtype}: ${Math.round(pl.hp)} HP`, "info");
    }
  } else {
    confirmAttack(country, pl);
  }
}

// ---- news compose ----
function compose() {
  modal("Publish to your agency", `
    <label>Headline<input id="x-h" placeholder="We strike oil!" /></label>
    <label>Body<textarea id="x-b" rows="3"></textarea></label>`, () => {
    send("news.publish", {
      headline: document.getElementById("x-h").value,
      body: document.getElementById("x-b").value,
    });
  });
}

// showFatal blocks the screen with a final message (e.g. duplicate country
// name) and a way back to the lobby; the socket will not reconnect.
function showFatal(msg) {
  setConn("rejected", false);
  const ov = document.getElementById("win-overlay");
  document.getElementById("win-title").textContent = msg;
  document.getElementById("win-reveals").innerHTML =
    `<p class="muted">Go back and choose a different country name.</p>`;
  ov.hidden = false;
}

// ---- win ----
function showWin(p) {
  const ov = document.getElementById("win-overlay");
  document.getElementById("win-title").textContent =
    (p.winnerId === app.me ? "You win!" : `${p.winnerName} wins`) + ` — ${p.reason}`;
  const rows = (p.standings || [])
    .slice()
    .sort((a, b) => (b.won - a.won) || (b.capital - a.capital))
    .map((r) =>
      `<div class="reveal ${r.won ? "won" : ""}">
         <b>${esc(r.name)}</b>
         <span class="muted">${fmt(r.capital)} capital · ☢️ ${r.nukes}</span></div>`).join("");
  document.getElementById("win-reveals").innerHTML = rows;

  // The host's "Back to lobby" closes the whole room; everyone else just leaves.
  const back = document.getElementById("win-back");
  const isHost = app.state && app.state.self && app.state.self.isHost;
  if (back) {
    back.textContent = isHost ? "Close room & back to lobby" : "Back to lobby";
    back.onclick = (e) => {
      if (isHost) {
        e.preventDefault();
        app.noReconnect = true;
        send("room.close", {});
        setTimeout(() => { location.href = "/"; }, 200); // let the close frame flush
      }
      // non-host: default <a href="/"> navigation
    };
  }
  ov.hidden = false;
}

// ---- boot ----
if (document.getElementById("map")) {
  initMap({ onMove: onMapMove, onClick: onMapClick, afterInteract: () => scheduleMapRender(true) });
}
initNews({ onCompose: compose });
wireOps();
connect();
