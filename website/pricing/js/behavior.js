// Rivet's living behavior: breathing, blinking, bird-like head saccades and a
// finite idle programme that ends in a doze.
//
// Every function here is a pure function of time and a seed, so the idle is
// reproducible frame for frame and a paused or inspected frame is exact.

import { clamp, lerp, ease, rng, springResponse, smoothstep } from "./ink.js";

// Breathing: small birds breathe fast; Rivet's chest rises about twice a second
// at rest and slows when he dozes.
export function breathAt(t, rate = 1.7) {
  return Math.sin(t * Math.PI * 2 * (rate / 2));
}

// A deterministic blink schedule: one blink every 2.4-5 s, sometimes doubled.
export function blinkSchedule(seed, span = 600) {
  const r = rng(seed);
  const times = [];
  let t = r.range(0.8, 2);
  while (t < span) {
    times.push(t);
    if (r.next() < 0.22) times.push(t + 0.28);
    t += r.range(2.4, 5);
  }
  return (time, quiet = []) => {
    for (const b of times) {
      if (b > time) break;
      if (quiet.some(([a, z]) => b >= a && b <= z)) continue;
      const k = time - b;
      if (k < 0.07) return ease.out(k / 0.07);
      if (k < 0.19) return 1 - ease.inOut((k - 0.07) / 0.12);
    }
    return 0;
  };
}

// Head saccades: hold still, then snap to a new angle. This stop-start head
// motion is what makes a bird read as a bird rather than a puppet.
function saccades(seed, duration, { range = [-0.28, 0.22], lookRange = 0.8 } = {}) {
  const r = rng(seed);
  const keys = [{ t: 0, a: 0, lx: 0, ly: 0, brow: 0, neck: 0, pupil: 0 }];
  let t = r.range(0.4, 0.9);
  while (t < duration - 0.5) {
    keys.push({
      t,
      a: r.range(range[0], range[1]),
      lx: r.signed(lookRange),
      ly: r.signed(0.4),
      brow: r.next() < 0.3 ? r.range(0.3, 1) : 0,
      neck: r.signed(0.08),
      // A jackdaw signals with its pupils; interest pins them.
      pupil: r.next() < 0.35 ? r.range(-0.8, -0.3) : r.signed(0.15),
    });
    t += r.range(0.55, 1.6);
  }
  keys.push({ t: duration - 0.18, a: 0, lx: 0, ly: 0, brow: 0, neck: 0, pupil: 0 });
  return (time) => {
    let i = 0;
    while (i + 1 < keys.length && keys[i + 1].t <= time) i += 1;
    const from = keys[Math.max(0, i - 1)];
    const to = keys[i];
    const k = i === 0 ? 1 : ease.snap(clamp((time - to.t) / 0.11));
    return {
      headAngle: lerp(from.a, to.a, k),
      lookX: lerp(from.lx, to.lx, k),
      lookY: lerp(from.ly, to.ly, k),
      brow: lerp(from.brow, to.brow, k),
      neck: lerp(from.neck, to.neck, k),
      pupil: lerp(from.pupil, to.pupil, k),
    };
  };
}

// Envelope that rises and falls within a segment, so each action starts and
// ends in the neutral pose and segments join without pops.
function hump(k, attack = 0.2, release = 0.25) {
  return smoothstep(0, attack, k) * (1 - smoothstep(1 - release, 1, k));
}

const ACTIONS = {
  look: (seed, duration) => {
    const s = saccades(seed, duration);
    return (t) => s(t);
  },
  tailFlick: () => (t) => {
    const k = clamp(t / 0.5);
    const flick = Math.sin(k * Math.PI * 3) * (1 - k);
    return { tailAngle: flick * -0.22, wingTuck: Math.max(0, flick) * -1.2, tailSpread: hump(k) * 0.4 };
  },
  call: () => (t) => {
    const k = clamp(t / 0.9);
    const chak = (c) => Math.max(0, Math.sin(clamp((t - c) / 0.16) * Math.PI));
    const open = chak(0.12) + chak(0.42);
    return {
      beakOpen: open * 0.75,
      crestRaise: open * 0.6,
      neck: open * 0.12,
      headAngle: -open * 0.12,
      tailAngle: -open * 0.14,
      bodyPitch: hump(k) * -0.05,
      wingTuck: -open * 0.5,
    };
  },
  preen: () => (t) => {
    const k = clamp(t / 2.6);
    const turn = smoothstep(0.02, 0.1, k) * (1 - smoothstep(0.86, 0.94, k));
    const nibble = Math.sin(t * 22) * 0.5 + 0.5;
    const working = smoothstep(0.12, 0.2, k) * (1 - smoothstep(0.8, 0.86, k));
    return {
      headTurn: turn,
      headX: -30 * turn,
      headY: 12 * turn,
      headAngle: 0.55 * turn + Math.sin(t * 9) * 0.06 * working,
      beakOpen: nibble * 0.35 * working,
      wingTuck: -0.7 * working,
      bodyPitch: 0.06 * turn,
      blink: working * 0.4,
      crestKick: working,
    };
  },
  ruffle: () => (t) => {
    const k = clamp(t / 1.3);
    const puff = hump(k, 0.15, 0.45);
    const shake = Math.sin(t * 64) * smoothstep(0.12, 0.25, k) * (1 - smoothstep(0.45, 0.65, k));
    return {
      puff: puff * 1.05,
      bodyPitch: shake * 0.05,
      headAngle: shake * 0.18,
      tailAngle: shake * 0.12,
      tailSpread: puff * 0.6,
      wingTuck: shake * 0.6,
      blink: smoothstep(0.15, 0.25, k) * (1 - smoothstep(0.55, 0.65, k)),
      crestKick: shake,
    };
  },
  stretch: () => (t) => {
    const k = clamp(t / 2.3);
    const s = hump(k, 0.3, 0.3);
    // Wing down and back alongside the stretched leg (mesh terms: - lowers).
    return {
      wingOpen: s * 0.75,
      wingLift: s * -0.95,
      wingSweep: s * -0.55,
      tailSpread: s,
      tailAngle: s * 0.08,
      footFarDx: -16 * s,
      footFarDy: -12 * s,
      bodyPitch: s * 0.12,
      neck: s * 0.1,
      headAngle: -s * 0.15,
      blink: s * 0.5,
    };
  },
  shuffle: () => (t) => {
    const step = (c) => Math.sin(clamp((t - c) / 0.22) * Math.PI);
    return {
      footNearDy: -6 * step(0.05),
      footNearDx: 3 * step(0.05),
      footFarDy: -6 * step(0.32),
      footFarDx: 3 * step(0.32),
      bodyPitch: (step(0.05) - step(0.32)) * 0.04,
    };
  },
};

// The idle programme after the story: varied, finite, then a doze. The
// sequence is fixed so frames are reproducible; only seeds vary the details.
const PROGRAMME = [
  ["look", 3.2], ["tailFlick", 0.5], ["look", 2.6], ["preen", 2.6], ["look", 3.4],
  ["ruffle", 1.3], ["look", 2.4], ["call", 0.9], ["look", 3], ["stretch", 2.3],
  ["look", 3.6], ["shuffle", 0.6], ["look", 3.2],
];
export const DOZE_FALL = 2.6;

export function idleProgramme(seed = 5) {
  let start = 0;
  const segments = PROGRAMME.map(([name, duration], i) => {
    const seg = { name, start, duration, fn: ACTIONS[name](seed + i * 17, duration) };
    start += duration;
    return seg;
  });
  const awake = start;
  return {
    awake,
    // Seconds of idle until Rivet is fully asleep and rendering can stop.
    asleepAt: awake + DOZE_FALL,
    at(t) {
      if (t >= awake) {
        const k = ease.inOut(clamp((t - awake) / DOZE_FALL));
        return { doze: k };
      }
      for (const seg of segments) {
        if (t < seg.start + seg.duration) return { action: seg.name, ...seg.fn(t - seg.start) };
      }
      return {};
    },
  };
}

// Merge additive behavior deltas onto a base pose.
const ADDITIVE = ["headAngle", "neck", "brow", "bodyPitch", "puff", "wingTuck", "tailAngle", "tailSpread", "beakOpen", "headX", "headY", "crouch", "crestRaise"];
const STRONGEST = ["headTurn", "blink", "wingOpen"];

export function applyDeltas(pose, d) {
  const out = { ...pose };
  for (const key of ADDITIVE) if (d[key] !== undefined) out[key] = (out[key] || 0) + d[key];
  for (const key of STRONGEST) if (d[key] !== undefined) out[key] = Math.max(out[key] || 0, d[key]);
  if (d.wingRaise !== undefined && (d.wingOpen || 0) >= (pose.wingOpen || 0)) { out.wingRaise = d.wingRaise; out.wingLift = -d.wingRaise; }
  if (d.wingLift !== undefined && (d.wingOpen || 0) >= (pose.wingOpen || 0)) { out.wingLift = d.wingLift; out.wingSweep = d.wingSweep ?? out.wingSweep; }
  if (d.pupil !== undefined) out.pupil = d.pupil;
  if (d.lookX !== undefined) out.lookX = d.lookX;
  if (d.lookY !== undefined) out.lookY = d.lookY;
  if (d.footNearDx || d.footNearDy) out.footNear = { x: out.footNear.x + (d.footNearDx || 0), y: out.footNear.y + (d.footNearDy || 0) };
  if (d.footFarDx || d.footFarDy) out.footFar = { x: out.footFar.x + (d.footFarDx || 0), y: out.footFar.y + (d.footFarDy || 0) };
  return out;
}

// Doze: fluffed, head sunk into the shoulders, eyes closed.
export function applyDoze(pose, k) {
  if (k <= 0) return pose;
  return {
    ...pose,
    puff: lerp(pose.puff, 1, k),
    sleep: k,
    neck: lerp(pose.neck, 0.7, k),
    crouch: lerp(pose.crouch, 0.25, k),
    headAngle: lerp(pose.headAngle, 0.2, k),
    tailAngle: lerp(pose.tailAngle, 0.06, k),
    lookX: lerp(pose.lookX, 0, k),
    lookY: lerp(pose.lookY, 0, k),
  };
}

// The untidy crest feather lags the head: a spring driven by head angle plus
// any explicit kick (ruffles, landings).
export function crestFollower(sourceAt) {
  const spring = springResponse(sourceAt, { stiffness: 160, damping: 7 });
  return (t) => spring(t) - sourceAt(t);
}
