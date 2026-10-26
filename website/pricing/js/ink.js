// Shared drawing and timing utilities for the Rivet scenes.
//
// Everything here is deterministic: a scene rendered at story time t always
// produces the same frame, which is what makes replay, skip-to-free and
// frame-exact verification possible.

export const TAU = Math.PI * 2;

export function clamp(v, lo = 0, hi = 1) {
  return v < lo ? lo : v > hi ? hi : v;
}

export function lerp(a, b, k) {
  return a + (b - a) * k;
}

export function invLerp(a, b, v) {
  return clamp((v - a) / (b - a));
}

export function smoothstep(a, b, v) {
  const k = invLerp(a, b, v);
  return k * k * (3 - 2 * k);
}

export const ease = {
  linear: (k) => k,
  in: (k) => k * k * k,
  out: (k) => 1 - Math.pow(1 - k, 3),
  inOut: (k) => (k < 0.5 ? 4 * k * k * k : 1 - Math.pow(-2 * k + 2, 3) / 2),
  sine: (k) => 0.5 - Math.cos(Math.PI * k) / 2,
  // Overshoot then settle: used for landings and snaps.
  back: (k) => {
    const c = 1.9;
    return 1 + (c + 1) * Math.pow(k - 1, 3) + c * Math.pow(k - 1, 2);
  },
  // A quick start that holds: the head "saccade" of a bird.
  snap: (k) => 1 - Math.pow(1 - k, 5),
  hold: () => 0,
};

// mulberry32: small, fast, good enough for layout jitter.
export function rng(seed) {
  let s = seed >>> 0;
  const next = () => {
    s = (s + 0x6d2b79f5) >>> 0;
    let t = s;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
  return {
    next,
    range: (a, b) => a + (b - a) * next(),
    signed: (m = 1) => (next() * 2 - 1) * m,
  };
}

// Smooth 1D value noise, for organic drift that never repeats exactly.
export function noise1(seed) {
  const r = rng(seed);
  const table = Array.from({ length: 256 }, () => r.next() * 2 - 1);
  return (x) => {
    const i = Math.floor(x);
    const f = x - i;
    const a = table[i & 255];
    const b = table[(i + 1) & 255];
    const u = f * f * (3 - 2 * f);
    return a + (b - a) * u;
  };
}

// A keyframed track. Keys are [time, value, easeName] where the ease shapes the
// segment arriving at that key. Values may be numbers or {x, y} points.
export function track(keys) {
  const sorted = [...keys].sort((a, b) => a[0] - b[0]);
  return (t) => {
    if (t <= sorted[0][0]) return sorted[0][1];
    const last = sorted[sorted.length - 1];
    if (t >= last[0]) return last[1];
    let i = 1;
    while (sorted[i][0] < t) i += 1;
    const [t0, v0] = sorted[i - 1];
    const [t1, v1, name = "inOut"] = sorted[i];
    const k = ease[name]((t - t0) / (t1 - t0));
    if (typeof v0 === "number") return lerp(v0, v1, k);
    return { x: lerp(v0.x, v1.x, k), y: lerp(v0.y, v1.y, k) };
  };
}

// A set of named tracks sampled together. Parameters without keys fall back to
// the supplied defaults, so a choreography only lists what it moves.
export function timeline(defaults, spec) {
  const tracks = Object.fromEntries(
    Object.entries(spec).map(([name, keys]) => [name, track(keys)]),
  );
  return (t) => {
    const out = { ...defaults };
    for (const name in tracks) out[name] = tracks[name](t);
    return out;
  };
}

// A damped spring integrated with a fixed step from rest. Used for secondary
// motion (crest feather, hanging parts) so that seeking to any time gives the
// same result as playing up to it.
export function springResponse(input, { stiffness = 120, damping = 9, step = 1 / 240 } = {}) {
  let cacheT = 0;
  let pos = input(0);
  let vel = 0;
  const reset = () => {
    cacheT = 0;
    pos = input(0);
    vel = 0;
  };
  return (t) => {
    if (t < cacheT) reset();
    while (cacheT + step <= t) {
      cacheT += step;
      const target = input(cacheT);
      const acc = stiffness * (target - pos) - damping * vel;
      vel += acc * step;
      pos += vel * step;
    }
    return pos;
  };
}

// Paper grain: fibrous value noise, rendered once and reused as an overlay.
export function paperTexture(size = 512, seed = 7, { tone = 128, spread = 26, fibers = 900 } = {}) {
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = size;
  const ctx = canvas.getContext("2d");
  const img = ctx.createImageData(size, size);
  const r = rng(seed);
  const coarse = Array.from({ length: 64 * 64 }, () => r.next());
  const sample = (x, y) => {
    const gx = (x / size) * 64;
    const gy = (y / size) * 64;
    const ix = Math.floor(gx);
    const iy = Math.floor(gy);
    const fx = gx - ix;
    const fy = gy - iy;
    const at = (i, j) => coarse[((j & 63) << 6) | (i & 63)];
    const top = lerp(at(ix, iy), at(ix + 1, iy), fx);
    const bottom = lerp(at(ix, iy + 1), at(ix + 1, iy + 1), fx);
    return lerp(top, bottom, fy);
  };
  for (let y = 0; y < size; y += 1) {
    for (let x = 0; x < size; x += 1) {
      const v = tone + (sample(x, y) - 0.5) * spread + (r.next() - 0.5) * spread * 0.9;
      const i = (y * size + x) * 4;
      img.data[i] = img.data[i + 1] = img.data[i + 2] = v;
      img.data[i + 3] = 255;
    }
  }
  ctx.putImageData(img, 0, 0);
  ctx.lineCap = "round";
  for (let i = 0; i < fibers; i += 1) {
    const x = r.next() * size;
    const y = r.next() * size;
    const a = r.next() * TAU;
    const len = r.range(4, 22);
    ctx.strokeStyle = r.next() < 0.5 ? "rgba(255,255,255,0.10)" : "rgba(0,0,0,0.08)";
    ctx.lineWidth = r.range(0.4, 1.1);
    ctx.beginPath();
    ctx.moveTo(x, y);
    ctx.quadraticCurveTo(
      x + Math.cos(a + 0.6) * len * 0.5,
      y + Math.sin(a + 0.6) * len * 0.5,
      x + Math.cos(a) * len,
      y + Math.sin(a) * len,
    );
    ctx.stroke();
  }
  return canvas;
}

export function hexToRgb(hex) {
  const v = parseInt(hex.slice(1), 16);
  return [(v >> 16) & 255, (v >> 8) & 255, v & 255];
}

export function mixColor(a, b, k) {
  const ca = hexToRgb(a);
  const cb = hexToRgb(b);
  const c = ca.map((v, i) => Math.round(lerp(v, cb[i], k)));
  return `rgb(${c[0]},${c[1]},${c[2]})`;
}

export function rgba(hex, alpha) {
  const [r, g, b] = hexToRgb(hex);
  return `rgba(${r},${g},${b},${alpha})`;
}

// A brush stroke with tapered ends, drawn as a filled ribbon along a polyline.
// Width follows a sine taper so both ends thin out like a dipped pen.
export function taperedStroke(ctx, points, width, { taperStart = 1, taperEnd = 1 } = {}) {
  if (points.length < 2) return;
  const left = [];
  const right = [];
  const n = points.length;
  for (let i = 0; i < n; i += 1) {
    const p = points[i];
    const a = points[Math.max(0, i - 1)];
    const b = points[Math.min(n - 1, i + 1)];
    const dx = b.x - a.x;
    const dy = b.y - a.y;
    const len = Math.hypot(dx, dy) || 1;
    const k = i / (n - 1);
    const taper = Math.min(
      taperStart ? Math.sin(Math.min(1, k * 2.4) * Math.PI * 0.5) : 1,
      taperEnd ? Math.sin(Math.min(1, (1 - k) * 2.4) * Math.PI * 0.5) : 1,
    );
    const w = (width * (0.25 + 0.75 * taper)) / 2;
    left.push({ x: p.x - (dy / len) * w, y: p.y + (dx / len) * w });
    right.push({ x: p.x + (dy / len) * w, y: p.y - (dx / len) * w });
  }
  ctx.beginPath();
  ctx.moveTo(left[0].x, left[0].y);
  for (const p of left) ctx.lineTo(p.x, p.y);
  for (let i = right.length - 1; i >= 0; i -= 1) ctx.lineTo(right[i].x, right[i].y);
  ctx.closePath();
  ctx.fill();
}

// Sample a cubic Bezier into points, for tapered strokes along curves.
export function bezierPoints(p0, p1, p2, p3, steps = 16) {
  const out = [];
  for (let i = 0; i <= steps; i += 1) {
    const t = i / steps;
    const u = 1 - t;
    out.push({
      x: u * u * u * p0.x + 3 * u * u * t * p1.x + 3 * u * t * t * p2.x + t * t * t * p3.x,
      y: u * u * u * p0.y + 3 * u * u * t * p1.y + 3 * u * t * t * p2.y + t * t * t * p3.y,
    });
  }
  return out;
}

export function rotate(p, a) {
  const c = Math.cos(a);
  const s = Math.sin(a);
  return { x: p.x * c - p.y * s, y: p.x * s + p.y * c };
}

export function formatMoney(value) {
  return "$" + Math.round(value).toLocaleString("en-US");
}
