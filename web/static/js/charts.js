// Tiny dependency-free canvas sparkline used for each resource's price figure.

export function drawSeries(canvas, data, color) {
  const ctx = canvas.getContext("2d");
  const w = canvas.width, h = canvas.height, pad = 4;
  ctx.clearRect(0, 0, w, h);
  if (!data || data.length < 2) return;
  const view = data.slice(-120);
  let min = Infinity, max = -Infinity;
  for (const v of view) { if (v < min) min = v; if (v > max) max = v; }
  if (max - min < 1e-9) max = min + 1;
  const sx = (w - pad * 2) / (view.length - 1);
  const sy = (h - pad * 2) / (max - min);
  const yOf = (v) => h - pad - (v - min) * sy;

  ctx.beginPath();
  ctx.moveTo(pad, yOf(view[0]));
  view.forEach((v, i) => ctx.lineTo(pad + i * sx, yOf(v)));
  ctx.lineTo(pad + (view.length - 1) * sx, h - pad);
  ctx.lineTo(pad, h - pad);
  ctx.closePath();
  ctx.fillStyle = hexA(color, 0.15);
  ctx.fill();

  ctx.beginPath();
  ctx.moveTo(pad, yOf(view[0]));
  view.forEach((v, i) => ctx.lineTo(pad + i * sx, yOf(v)));
  ctx.lineWidth = 2;
  ctx.strokeStyle = color;
  ctx.stroke();

  const lx = pad + (view.length - 1) * sx, ly = yOf(view[view.length - 1]);
  ctx.beginPath();
  ctx.arc(lx, ly, 2.5, 0, Math.PI * 2);
  ctx.fillStyle = color;
  ctx.fill();
}

function hexA(hex, a) {
  const n = parseInt(hex.slice(1), 16);
  return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${a})`;
}

export const RESOURCE_COLORS = {
  oil: "#fbbf24", grain: "#4ade80", water: "#22d3ee", coal: "#9ca3af",
  wood: "#b45309", iron: "#94a3b8", cotton: "#e5e7eb", gold: "#facc15",
  glass: "#67e8f9", plastic: "#f0abfc", uranium: "#86efac",
};
