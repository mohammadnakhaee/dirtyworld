// SVG map rendering: procedural landmass smoothing, draggable own buildings,
// and HP / cooldown progress rings on every placeable.
const SVGNS = "http://www.w3.org/2000/svg";
const W = 1000, H = 1000;

// Agency/military use named icons; factories carry their emoji directly.
const ICONS = {
  newspaper: "📰", shield: "🛡️",
};
function glyphFor(pl) {
  return ICONS[pl.icon] || pl.icon || "•";
}

let dragging = false;
let interacting = false; // a pointer is currently pressed on a building
let cfg = { onMove: () => {}, onClick: () => {} };

export function initMap({ onMove, onClick }) {
  cfg = { onMove, onClick };
}

export function isDragging() { return dragging; }

// isInteracting is true from pointerdown to pointerup on a placeable, so the
// caller can avoid rebuilding the SVG mid-click (which would eat the click).
export function isInteracting() { return interacting; }

// Catmull-Rom -> cubic Bézier, closed loop, over normalized points.
function smoothPath(pts) {
  const p = pts.map(([x, y]) => [x * W, y * H]);
  const n = p.length;
  let d = `M ${p[0][0].toFixed(1)} ${p[0][1].toFixed(1)} `;
  for (let i = 0; i < n; i++) {
    const p0 = p[(i - 1 + n) % n], p1 = p[i];
    const p2 = p[(i + 1) % n], p3 = p[(i + 2) % n];
    const c1 = [p1[0] + (p2[0] - p0[0]) / 6, p1[1] + (p2[1] - p0[1]) / 6];
    const c2 = [p2[0] - (p3[0] - p1[0]) / 6, p2[1] - (p3[1] - p1[1]) / 6];
    d += `C ${c1[0].toFixed(1)} ${c1[1].toFixed(1)} ${c2[0].toFixed(1)} ${c2[1].toFixed(1)} ${p2[0].toFixed(1)} ${p2[1].toFixed(1)} `;
  }
  return d + "Z";
}

function el(tag, attrs = {}) {
  const e = document.createElementNS(SVGNS, tag);
  for (const k in attrs) e.setAttribute(k, attrs[k]);
  return e;
}

// describeArc draws a ring arc from 0..frac (clockwise from top) at radius r.
function ringPath(cx, cy, r, frac) {
  frac = Math.max(0, Math.min(1, frac));
  if (frac >= 0.9999) frac = 0.9999;
  const a0 = -Math.PI / 2;
  const a1 = a0 + frac * 2 * Math.PI;
  const x0 = cx + r * Math.cos(a0), y0 = cy + r * Math.sin(a0);
  const x1 = cx + r * Math.cos(a1), y1 = cy + r * Math.sin(a1);
  const large = frac > 0.5 ? 1 : 0;
  return `M ${x0} ${y0} A ${r} ${r} 0 ${large} 1 ${x1} ${y1}`;
}

function hpColor(frac) {
  if (frac > 0.6) return "#4ade80";
  if (frac > 0.3) return "#fbbf24";
  return "#f87171";
}

// renderMap draws one country into the svg. `own` enables dragging + repair.
export function renderMap(svg, country, own) {
  if (interacting) return; // don't destroy buildings mid-click
  svg.innerHTML = "";
  const palette = country.palette || "#5eead4";
  const gid = "grad-" + country.playerId;

  const defs = el("defs");
  const grad = el("radialGradient", { id: gid, cx: "40%", cy: "35%", r: "75%" });
  grad.appendChild(el("stop", { offset: "0%", "stop-color": palette, "stop-opacity": "0.55" }));
  grad.appendChild(el("stop", { offset: "70%", "stop-color": palette, "stop-opacity": "0.18" }));
  grad.appendChild(el("stop", { offset: "100%", "stop-color": "#0b1020", "stop-opacity": "0.05" }));
  defs.appendChild(grad);
  svg.appendChild(defs);

  if (country.boundary && country.boundary.length) {
    // Gradient fill + bright stroke gives the glow look with no per-render
    // filter cost (filters re-rasterize on every switch and cause jank).
    const land = el("path", {
      d: smoothPath(country.boundary),
      fill: `url(#${gid})`,
      stroke: palette,
      "stroke-width": "4",
      class: "landmass",
    });
    svg.appendChild(land);
  }

  for (const pl of country.placeables || []) {
    svg.appendChild(buildPlaceable(svg, country, pl, own, palette));
  }
}

function buildPlaceable(svg, country, pl, own, palette) {
  const x = pl.x * W, y = pl.y * H;
  const g = el("g", {
    transform: `translate(${x} ${y})`,
    class: "placeable" + (own ? " own" : " enemy"),
  });
  g._pl = pl;

  const dead = pl.hp <= 0;
  const hpFrac = pl.maxHp ? pl.hp / pl.maxHp : 0;
  const r = 30;

  // base disc
  g.appendChild(el("circle", { r: r + 6, fill: "#0b1020", "fill-opacity": "0.6", stroke: palette, "stroke-width": "1.5" }));
  // HP ring (background + value)
  g.appendChild(el("circle", { r, fill: "none", stroke: "#ffffff", "stroke-opacity": "0.12", "stroke-width": "5" }));
  if (!dead && hpFrac > 0) {
    g.appendChild(el("path", { d: ringPath(0, 0, r, hpFrac), fill: "none", stroke: hpColor(hpFrac), "stroke-width": "5", "stroke-linecap": "round" }));
  }
  // cooldown / repair ring (inner)
  if (pl.cooldownMax > 0 && pl.cooldown > 0) {
    const cf = 1 - pl.cooldown / pl.cooldownMax;
    g.appendChild(el("path", { d: ringPath(0, 0, r - 8, cf), fill: "none", stroke: "#60a5fa", "stroke-width": "3", "stroke-linecap": "round", opacity: "0.9" }));
  }

  const icon = el("text", { class: "icon", "text-anchor": "middle", "dominant-baseline": "central", y: "1", "font-size": "30" });
  icon.textContent = dead ? "💥" : glyphFor(pl);
  if (dead) icon.setAttribute("opacity", "0.5");
  g.appendChild(icon);

  // value label when known (own country, or spied rival)
  if (typeof pl.value === "number") {
    const v = el("text", { class: "val", "text-anchor": "middle", y: r + 22, "font-size": "16" });
    v.textContent = "$" + Math.round(pl.value);
    g.appendChild(v);
  }

  attachPointer(g, svg, country, pl, own);
  return g;
}

function attachPointer(g, svg, country, pl, own) {
  let moved = false, startX = 0, startY = 0;
  g.addEventListener("pointerdown", (e) => {
    e.preventDefault();
    g.setPointerCapture(e.pointerId);
    moved = false;
    interacting = true;
    startX = e.clientX; startY = e.clientY;

    const move = (ev) => {
      if (!own) return;
      if (Math.abs(ev.clientX - startX) + Math.abs(ev.clientY - startY) > 4) {
        moved = true; dragging = true;
      }
      if (!dragging) return;
      const rect = svg.getBoundingClientRect();
      const nx = Math.max(0, Math.min(1, (ev.clientX - rect.left) / rect.width));
      const ny = Math.max(0, Math.min(1, (ev.clientY - rect.top) / rect.height));
      pl.x = nx; pl.y = ny;
      g.setAttribute("transform", `translate(${nx * W} ${ny * H})`);
    };
    const up = () => {
      g.removeEventListener("pointermove", move);
      dragging = false;
      interacting = false;
      if (moved && own) {
        cfg.onMove(pl.id, pl.x, pl.y);
      } else {
        cfg.onClick(country, pl, own);
      }
      cfg.afterInteract && cfg.afterInteract();
    };
    g.addEventListener("pointermove", move);
    g.addEventListener("pointerup", up, { once: true });
    g.addEventListener("pointercancel", () => { dragging = false; interacting = false; cfg.afterInteract && cfg.afterInteract(); }, { once: true });
  });
}
