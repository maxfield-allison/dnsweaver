// dnsweaver: the price, resolved at a switchboard.
//
// DNS is the internet's phone book, so this scene is a night-shift telephone
// switchboard in dnsweaver's navy, cyan and teal. A cord from the
// "price.dnsweaver" jack is patched into $5. Then the price starts following a
// CNAME chain: patch cords leap jack to jack, $50, $500 ... $5B, infinity, and
// the rate board overflows and the infinity jack spits a spark. Rivet jumps,
// does a double take at the board, gives whoever is watching a long look, and
// flutters up to an empty jack hole. He tries his brass washer in it until it
// seats (a ring that reads 0), yanks the root plug so the whole chain zips
// back, and plugs the price into his washer instead. The price resolves to
// $0. He taps the plug to make sure it is seated.
//
// The take is inserted into the action at TAKE_AT: the choreography below
// was authored on an action clock that pauses for TAKE_FOR seconds while the
// take plays, so `act(t)` maps story time to that clock.

import { Rivet, DEFAULT_POSE } from "../rivet.js";
import { TAU, clamp, lerp, ease, track, rng, rgba, smoothstep, noise1 } from "../ink.js";
import { breathAt, blinkSchedule, idleProgramme, applyDeltas, applyDoze, crestFollower } from "../behavior.js";
import { loadArt, rivetKit, drawPlate, layerMatrix } from "../paint.js";

const TAKE_AT = 2.0;
const TAKE_FOR = 1.2;
const act = (t) => (t < TAKE_AT ? t : t < TAKE_AT + TAKE_FOR ? TAKE_AT : t - TAKE_FOR);
const later = (t) => t + TAKE_FOR; // an action-clock time on the story clock
const STORY = later(5.8);
const FREE_AT = later(4.22);
const RIVET_SCALE = 0.9;
const BOARD = { x0: -310, x1: 310, y0: -470, y1: 150 };
const SHELF_Y = 150;
// The painting's worktop starts near 824/1254 of its height. This placement
// aligns it with the live shelf; it follows the action plane, since a
// separate parallax rate would pull that contact boundary apart. At 1400
// units wide its lamp-lit racks stand just either side of the board, inside
// the camera's frames.
const PAINTED_ROOM = { x: -700, y: 155 - (824 / 1254) * 1400, width: 1400, height: 1400 };
const PAINTED_JACK_DIAMETER = 24;
const PAINTED_PLUG = { x: -28, y: -7.5, width: 48, height: 15 };
const VFD = { x0: -186, x1: 186, y0: -372, y1: -212 };
const SOURCE = { x: -226, y: -150 };
const ROW_A = -62;
const RAIL_A = -28;
const ROW_B = 50;
const RAIL_B = 84;
const JACKS = [
  { x: -220, y: ROW_A, label: "", value: 0 },
  { x: -110, y: ROW_A, label: "$5", value: 5 },
  { x: 0, y: ROW_A, label: "$50", value: 50 },
  { x: 110, y: ROW_A, label: "$500", value: 500 },
  { x: 205, y: ROW_A, label: "$5K", value: 5e3 },
  { x: -110, y: ROW_B, label: "$500K", value: 5e5 },
  { x: -10, y: ROW_B, label: "$5M", value: 5e6 },
  { x: 90, y: ROW_B, label: "$5B", value: 5e9 },
  { x: 185, y: ROW_B, label: "∞", value: Infinity },
];
// Each hop extends the CNAME chain by one jack.
const HOP_AT = [0.62, 0.94, 1.22, 1.46, 1.66, 1.83, 1.97];
const HOP_FOR = 0.2;
const RETRACT_AT = (k) => later(3.42) + (HOP_AT.length - 1 - k) * 0.055;
const PLUG_OUT = later(3.4);
const PLUG_IN = later(3.95);
const WASHER_SEAT = later(2.8);
// The take, on the story clock.
const SPARK_AT = HOP_AT[HOP_AT.length - 1] + HOP_FOR + 0.02; // infinity arrives and spits
const TAKE = { startle: SPARK_AT + 0.02, double: 2.52, look: 2.88, ready: 3.12 };
const TIERS = [
  { name: "HOMELAB", factor: 1 },
  { name: "CLUSTER", factor: 3 },
  { name: "FLEET", factor: 10 },
];

const C = {
  navy950: "#0a1422",
  navy900: "#0f1d31",
  cyan: "#56d9f2",
  teal: "#2ec8a6",
  cyan700: "#0e93b0",
};

// ---------------------------------------------------------------- motion

function chainLength(t) {
  let n = 0;
  for (let k = 0; k < HOP_AT.length; k += 1) {
    if (t >= HOP_AT[k] + HOP_FOR && t < RETRACT_AT(k)) n = k + 1;
  }
  return n;
}

function priceAt(t) {
  if (t >= PLUG_IN) return 0;
  if (t >= PLUG_OUT) return null; // unresolved while the plug is in Rivet's beak
  return JACKS[1 + chainLength(t)].value;
}

const tl = {
  // Stand one body-length from a jack. A low peck comes from the hips and
  // shoulders; standing over the target would require an impossible neck.
  x: track([[0, -196], [2.06, -196], [2.42, -140, "sine"], [2.9, -140], [3.02, -83, "out"], [3.16, -34, "out"], [3.45, -34], [3.58, -83, "out"], [3.72, -140, "out"], [4.68, -140], [5.06, -70, "sine"], [5.8, -70]]),
  y: track([[0, SHELF_Y], [2.06, SHELF_Y], [2.42, RAIL_A, "sine"], [4.68, RAIL_A], [5.06, SHELF_Y, "sine"], [5.8, SHELF_Y]]),
  lift: track([[0, 0], [2.06, 0], [2.26, 70, "out"], [2.42, 0, "in"], [2.9, 0], [2.96, 10, "out"], [3.02, 0], [3.09, 10, "out"], [3.16, 0], [3.45, 0], [3.51, 10, "out"], [3.58, 0], [3.65, 10, "out"], [3.72, 0], [4.68, 0], [4.86, 40, "out"], [5.06, 0, "in"]]),
  facing: track([[0, 1], [2.2, 1], [2.21, -1], [4.86, -1], [4.87, 1]]),
  // How strongly the beak is steered onto a target (hole, plug, jack).
  reach: track([[0, 0], [2.48, 0], [2.56, 1], [2.86, 1], [2.92, 0], [3.18, 0], [3.24, 1], [3.4, 1], [3.42, 0], [3.78, 0], [3.9, 1], [4.0, 1], [4.08, 0], [4.32, 0], [4.36, 1], [4.42, 0.4], [4.46, 1], [4.52, 0]]),
  crouch: track([[0, 0], [1.44, 0], [1.48, 0.35, "out"], [1.6, 0], [1.98, 0], [2.06, 0.8, "out"], [2.12, -0.2], [2.4, 0], [2.46, 0.5, "out"], [2.56, 0.6], [2.86, 0.6], [2.94, 0.05], [3.2, 0.2], [3.24, 0.6], [3.4, 0.6], [3.46, 0.1], [3.8, 0.1], [3.9, 0.6], [4.02, 0.6], [4.1, 0.4], [4.36, 0.6], [4.46, 0.6], [4.6, 0], [4.68, 0.6, "out"], [4.74, -0.2], [5.04, 0], [5.1, 0.5, "out"], [5.3, 0]]),
  squash: track([[0, 0], [2.06, 0], [2.11, -0.6, "out"], [2.26, 0], [2.4, 0], [2.45, 0.7, "out"], [2.6, 0], [3.38, 0], [3.42, 0.6, "out"], [3.56, 0], [5.04, 0], [5.09, 0.7, "out"], [5.24, 0]]),
  wingOpen: track([[0, 0], [1.46, 0], [1.5, 0.45, "out"], [1.6, 0], [2.04, 0], [2.12, 1, "out"], [2.38, 0.9], [2.48, 0], [3.38, 0], [3.42, 0.8, "out"], [3.62, 0], [4.7, 0], [4.78, 0.9, "out"], [5.0, 0.8], [5.1, 0]]),
  bodyPitch: track([[0, 0.05], [2.0, 0.05], [2.12, -0.2], [2.4, 0], [2.56, 0.9], [2.86, 0.9], [2.94, 0.05], [3.18, 0.35], [3.24, 0.9], [3.4, 0.9], [3.46, -0.25, "out"], [3.56, -0.05], [3.8, 0.1], [3.9, 0.9], [4.02, 0.9], [4.1, 0.72], [4.36, 0.9], [4.46, 0.9], [4.52, 0.1], [4.7, 0.1], [4.8, -0.15], [5.1, 0], [5.8, 0]]),
  headAngle: track([[0, 0.05], [0.62, 0.05], [0.66, -0.25, "snap"], [0.94, -0.25], [0.98, -0.12, "snap"], [1.22, -0.12], [1.26, 0.05, "snap"], [1.46, 0.05], [1.5, -0.4, "snap"], [1.62, -0.4], [1.66, 0.15, "snap"], [1.97, 0.15], [2.02, -0.5, "snap"], [2.42, -0.2], [2.56, 0.3], [2.86, 0.3], [2.94, -0.15], [3.24, 0.2], [3.4, -0.3, "out"], [3.6, -0.1], [3.9, 0.25], [4.06, 0.25], [4.16, -0.6, "snap"], [4.3, -0.6], [4.36, 0.25], [4.52, 0], [4.6, -0.5, "snap"], [4.72, -0.3], [5.1, 0], [5.4, -0.1], [5.8, 0]]),
  headTurn: track([[0, 0], [0.64, 0], [0.7, 1, "snap"], [0.9, 1], [0.96, 0, "snap"], [1.24, 0], [1.3, 1, "snap"], [1.42, 1], [1.48, 0, "snap"]]),
  neck: track([[0, 1], [2.5, 1], [2.56, 1.15], [2.86, 1.15], [2.94, 1], [5.8, 1]]),
  brow: track([[0, 0.2], [0.62, 0.2], [0.68, 1, "snap"], [1.5, 1], [1.62, 0.3], [2.0, -0.7], [3.42, -0.7], [3.5, 1, "snap"], [3.9, 0.2], [4.16, 0.9], [4.6, 0.4], [5.8, 0.2]]),
  beakOpen: track([[0, 0.14], [2.86, 0.14], [2.9, 0.4, "out"], [2.98, 0], [3.2, 0], [3.24, 0.32], [3.28, 0.18], [3.96, 0.18], [4.0, 0.4, "out"], [4.06, 0], [5.36, 0], [5.42, 0.5, "out"], [5.5, 0]]),
  lookX: track([[0, -0.2], [0.62, -0.2], [0.66, 0.6, "snap"], [1.97, 0.6], [2.02, -0.4], [4.16, 0.3], [4.6, 0.6], [5.1, 0.3]]),
  lookY: track([[0, -0.4], [0.62, -0.4], [2.02, -0.5], [2.56, 0.5], [3.6, 0], [4.16, -0.9, "snap"], [4.36, 0.4], [4.6, -0.6], [5.1, -0.2]]),
  tailAngle: track([[0, 0], [1.48, 0], [1.52, -0.25, "out"], [1.66, 0], [2.12, 0.15], [2.45, 0.3, "out"], [2.62, 0], [3.42, 0.3, "out"], [3.6, 0], [5.09, 0.3, "out"], [5.25, 0]]),
  tailSpread: track([[0, 0], [2.06, 0], [2.14, 0.8], [2.48, 0], [3.4, 0], [3.44, 0.7], [3.62, 0], [4.72, 0], [4.8, 0.8], [5.1, 0]]),
  puff: track([[0, 0], [5.24, 0], [5.36, 0.7, "out"], [5.7, 0.05], [5.8, 0]]),
};

// Where Rivet's beak is being steered at each moment.
function beakTarget(story) {
  const t = act(story);
  const zero = JACKS[0];
  const five = JACKS[1];
  if (t < 2.9) return { x: zero.x + 2, y: zero.y };
  if (t < 3.44) return { x: five.x + 4, y: five.y + 8 };
  if (t < 4.2) return { x: zero.x + 6, y: zero.y + 6 };
  return { x: zero.x + 6, y: zero.y + 6 };
}

function feetFor(story) {
  const t = act(story);
  const air = (t > 2.11 && t < 2.42) || (t > 4.74 && t < 5.06);
  if (air) return { near: { x: 9, y: -24 }, far: { x: -3, y: -26 }, grip: 0.2 };
  const onRail = t >= 2.42 && t <= 4.74;
  return onRail ? { near: { x: 9, y: 0 }, far: { x: -5, y: 0 }, grip: 0.9 } : { near: { x: 16, y: 0 }, far: { x: -5, y: 0 }, grip: 0 };
}

// The take: absolute values for the expressive channels, and how strongly
// they replace the action pose at each moment.
const takeTracks = {
  lift: track([[0, 0], [TAKE.startle, 0], [TAKE.startle + 0.14, 22, "out"], [TAKE.startle + 0.3, 0, "in"]]),
  crouch: track([[0, 0.1], [TAKE.startle - 0.02, 0.1], [TAKE.startle + 0.02, 0.35, "out"], [TAKE.startle + 0.06, -0.15], [TAKE.startle + 0.32, 0.45, "out"], [TAKE.double, 0], [TAKE.ready, 0], [TAKE.ready + 0.1, 0.75, "out"]]),
  squash: track([[0, 0], [TAKE.startle, 0], [TAKE.startle + 0.04, -0.7, "out"], [TAKE.startle + 0.28, 0], [TAKE.startle + 0.32, 0.6, "out"], [TAKE.double, 0]]),
  bodyPitch: track([[0, 0.05], [TAKE.startle, 0.05], [TAKE.startle + 0.04, -0.15, "snap"], [TAKE.double, -0.05], [TAKE.double + 0.08, -0.12], [TAKE.ready, 0.05], [TAKE.ready + 0.1, 0.3]]),
  headAngle: track([[0, 0.1], [TAKE.startle, 0.1], [TAKE.startle + 0.05, -0.2, "snap"], [TAKE.double, -0.1], [TAKE.double + 0.05, -0.6, "snap"], [TAKE.look, -0.6], [TAKE.look + 0.06, -0.06, "snap"], [TAKE.ready, -0.06], [TAKE.ready + 0.06, -0.3, "snap"]]),
  neck: track([[0, 1], [TAKE.double, 1.02], [TAKE.double + 0.08, 1.3, "out"], [TAKE.look, 1.26], [TAKE.look + 0.06, 1], [TAKE.ready + 0.08, 0.86, "out"]]),
  eyeWide: track([[0, 0], [TAKE.startle, 0], [TAKE.startle + 0.05, 1, "snap"], [TAKE.double - 0.02, 0.45], [TAKE.double + 0.05, 1, "snap"], [TAKE.look, 1], [TAKE.look + 0.06, 0, "snap"], [TAKE.ready + 0.06, 0.25]]),
  pupil: track([[0, 0], [TAKE.startle, 0], [TAKE.startle + 0.05, -1, "snap"], [TAKE.look, -1], [TAKE.look + 0.06, -0.45, "snap"], [TAKE.ready, -0.45], [TAKE.ready + 0.08, -0.85, "out"]]),
  crestRaise: track([[0, 0], [TAKE.startle, 0], [TAKE.startle + 0.05, 1.1, "snap"], [TAKE.double - 0.02, 0.5], [TAKE.double + 0.05, 1.1, "snap"], [TAKE.look, 0.9], [TAKE.look + 0.06, -0.5, "snap"], [TAKE.ready - 0.04, -0.5], [TAKE.ready + 0.04, 0.2]]),
  lid: track([[0, 0], [TAKE.look + 0.06, 0], [TAKE.look + 0.12, 0.5, "out"], [TAKE.look + 0.16, 0.5], [TAKE.look + 0.2, 1, "inOut"], [TAKE.look + 0.32, 0.5, "inOut"], [TAKE.ready - 0.06, 0.5], [TAKE.ready, 0, "snap"]]),
  brow: track([[0, 0], [TAKE.ready, 0], [TAKE.ready + 0.06, -1, "snap"]]),
  lookX: track([[0, 0.6], [TAKE.double, 0.6], [TAKE.double + 0.05, 0.3, "snap"], [TAKE.look, 0.3], [TAKE.look + 0.06, -0.35, "snap"], [TAKE.ready, -0.35], [TAKE.ready + 0.06, 0.3, "snap"]]),
  lookY: track([[0, -0.5], [TAKE.startle + 0.05, -0.3, "snap"], [TAKE.double, -0.3], [TAKE.double + 0.05, -0.85, "snap"], [TAKE.look, -0.85], [TAKE.look + 0.06, 0, "snap"], [TAKE.ready + 0.06, -0.6, "snap"]]),
  tailAngle: track([[0, 0], [TAKE.startle, 0], [TAKE.startle + 0.05, -0.3, "out"], [TAKE.startle + 0.35, 0.2, "out"], [TAKE.double + 0.2, 0], [TAKE.ready + 0.1, -0.2]]),
  tailSpread: track([[0, 0], [TAKE.startle, 0], [TAKE.startle + 0.06, 0.8], [TAKE.double, 0]]),
};
const takeWeight = (t) => smoothstep(TAKE.startle - 0.06, TAKE.startle, t) * (1 - smoothstep(TAKE_AT + TAKE_FOR - 0.04, TAKE_AT + TAKE_FOR + 0.06, t));

// Wings in mesh terms (wingLift + raises them): the action track says how
// open they are; they flutter above the body, never sweeping below it.
function wingsAt(t, open) {
  const startle = Math.sin(clamp((t - TAKE.startle) / 0.3) * Math.PI);
  const flutter = 0.9 + Math.sin(t * TAU * 7) * 0.35;
  const o = Math.max(open, startle * 0.6);
  const lift = (open * flutter + startle * 1.0) / Math.max(1, open + startle);
  return { open: o, lift, tuck: -0.9 * Math.sin(clamp((t - STORY + 0.7) / 0.22) * Math.PI) };
}

// ---------------------------------------------------------------- scene

export function createDnsweaverScene() {
  const rivet = new Rivet({ seed: 29 });
  const blink = blinkSchedule(61);
  const idle = idleProgramme(13);
  const flicker = noise1(5);
  const crest = crestFollower((t) => tl.headAngle(act(Math.min(t, STORY))) * 1.2 + tl.squash(act(Math.min(t, STORY))) * 0.4);
  const CLOSE = { x: -150, y: 40 };
  const r = rng(21);
  const idleLamps = Array.from({ length: 10 }, () => ({ phase: r.range(0, 20), rate: r.range(0.05, 0.12) }));
  let art = null;
  let camera = { s: 1, w: 1, h: 1, zoom: 1, fx: 0, fy: 0, shakeX: 0, shakeY: 0 };
  let layoutMode = "wide";
  // The action's camera runs on the action clock; during the take it holds
  // the whole board while infinity sparks, then pushes in close on Rivet for
  // his look at the viewer.
  const actionShots = {
    wide: {
      zoom: track([[0, 1.12], [0.55, 1.12], [1.95, 1.0, "inOut"], [2.3, 1.36, "inOut"], [3.45, 1.36], [4.0, 1.48, "inOut"], [4.5, 1.48], [5.3, 1.14, "inOut"]]),
      fx: track([[0, -40], [0.55, -40], [1.2, 30, "inOut"], [1.95, 40, "inOut"], [2.3, -150, "inOut"], [3.2, -130], [3.45, -150], [4.0, -170], [4.5, -170], [5.3, -60, "inOut"]]),
      fy: track([[0, -120], [1.95, -110], [2.3, -110, "inOut"], [4.0, -140], [4.5, -140], [5.3, -110, "inOut"]]),
    },
    // On a phone the rate board and the jacks must stay in one frame, so the
    // camera only breathes in and out rather than panning away.
    tall: {
      zoom: track([[0, 0.94], [0.55, 0.94], [1.95, 0.92, "inOut"], [2.3, 0.97, "inOut"], [4.5, 0.97], [5.3, 0.94, "inOut"]]),
      fx: track([[0, -36], [5.8, -36]]),
      fy: track([[0, -128], [1.95, -120], [2.3, -132, "inOut"], [4.5, -132], [5.3, -124, "inOut"]]),
    },
  };
  const closeIn = track([[0, 0], [TAKE.double + 0.1, 0], [TAKE.look - 0.02, 1, "inOut"], [TAKE.ready, 1], [TAKE_AT + TAKE_FOR + 0.15, 0, "inOut"]]);
  const closeShot = { wide: { zoom: 2.3, fx: CLOSE.x, fy: CLOSE.y }, tall: { zoom: 1.9, fx: CLOSE.x - 30, fy: CLOSE.y } };
  const shotAt = (mode, t) => {
    const a = actionShots[mode], k = closeIn(t), c = closeShot[mode];
    return { zoom: lerp(a.zoom(act(t)), c.zoom, k), fx: lerp(a.fx(act(t)), c.fx, k), fy: lerp(a.fy(act(t)), c.fy, k) };
  };

  const scene = {
    storyDuration: STORY,
    freeAt: FREE_AT,
    idleEndsAt: idle.asleepAt,
    artIds: () => (art ? art.ids() : []),
    onUnavailable: handler => { rivet.onUnavailable = handler; },

    async prepare() {
      await rivet.ready;
      const [loaded] = await Promise.all([
        loadArt((a) => a.id.startsWith("dnsweaver-") || (a.piece || "").startsWith("rivet-")),
        document.fonts.load("600 20px Sora"),
        document.fonts.load("400 28px 'JetBrains Mono'"),
      ]);
      art = loaded;
      if (art?.size) rivet.usePaint(rivetKit(art));
    },

    layout(w, h) {
      layoutMode = h > w * 1.05 ? "tall" : "wide";
      const view = layoutMode === "tall" ? { w: 480, h: 690 } : { w: 1180, h: 700 };
      camera = { ...camera, s: Math.min(w / view.w, h / view.h), w, h };
    },

    render(ctx, frame) {
      const { t, dpr, w, h } = frame;
      const idleT = frame.idle;
      const now = t + idleT;
      const sleepK = smoothstep(idle.awake, idle.asleepAt, idleT);
      const shot = shotAt(layoutMode, t);
      camera.zoom = shot.zoom;
      camera.fx = shot.fx;
      camera.fy = shot.fy;
      const pop = (at) => (t > at ? Math.exp(-(t - at) * 14) : 0);
      const jolt = (pop(SPARK_AT) + pop(PLUG_OUT) + pop(PLUG_IN) * 0.6) * 2.4;
      // layerMatrix applies camera shake in pixels; the original shake was
      // in world units, so preserve its amplitude under the same zoom.
      camera.shakeX = Math.sin(t * 57) * jolt * camera.s * camera.zoom;
      camera.shakeY = Math.cos(t * 43) * jolt * 0.6 * camera.s * camera.zoom;
      const start = shotAt(layoutMode, 0);
      const ref = { zoom: start.zoom, fx: start.fx, fy: start.fy };
      const base = layerMatrix(camera, ref, 1, dpr);

      ctx.setTransform(1, 0, 0, 1, 0, 0);
      paintRoom(ctx, w * dpr, h * dpr);
      ctx.setTransform(base);
      const far = art?.get("dnsweaver-far");
      const jack = art?.get("dnsweaver-jack-bezel");
      const plug = art?.get("dnsweaver-plug");
      const washer = art?.get("dnsweaver-washer");
      // The painted worktop shares the contact plane with the keyshelf.
      // Keep that boundary locked to it through the camera moves.
      if (far) drawPlate(ctx, { ...far, world: PAINTED_ROOM }, layerMatrix(camera, ref, 1, dpr));
      else paintWallDetails(ctx, now, sleepK);
      paintBoard(ctx, Boolean(far));
      // The painted room's own racks and lamps flank the board; the drawn
      // source and provider wings only stand in for them.
      if (layoutMode === "wide" && !far) paintWings(ctx, now, sleepK, idleLamps, jack);
      paintVfd(ctx, t, now, flicker, sleepK);
      paintSource(ctx, jack);
      paintJacks(ctx, t, now, sleepK, jack);
      paintChain(ctx, t, plug);
      paintSpark(ctx, t);

      const pose = poseAt(t, frame, idleT, sleepK);
      const hold = rivet.holdMatrix(pose, base);
      const grip = rivet.holdMatrix(pose);
      // Grip the black barrel at its centre, keeping the original prop size.
      // The bill's rotation carries the plug rather than a fixed screen angle.
      const heldPlug = grip.scale(0.9 / Math.hypot(grip.a, grip.b)).translate(16, 0);
      paintRootCord(ctx, t, heldPlug, base, "behind", plug);
      paintShelf(ctx, now, sleepK, idleLamps, Boolean(far));
      const light = { dir: { x: 0.05, y: -1 }, rim: "#8fe9fb", rimStrength: 0.62, rimWidth: 1.1, ambient: "#123049", ambientStrength: 0.14 };
      rivet.drawShadow(ctx, pose, base, { strength: 0.4, color: "#02060c", width: 0.7 });
      rivet.draw(ctx, pose, base, light);
      paintRootCord(ctx, t, heldPlug, base, "front", plug);
      paintWasher(ctx, t, hold, washer);

    },
  };

  function poseAt(t, frame, idleT, sleepK) {
    const feet = feetFor(t);
    const sample = Object.fromEntries(Object.entries(tl).map(([k, f]) => [k, f(act(t))]));
    const w = takeWeight(t);
    if (w > 0) for (const [k, f] of Object.entries(takeTracks)) sample[k] = lerp(sample[k] ?? DEFAULT_POSE[k] ?? 0, f(t), w);
    const wings = wingsAt(t, sample.wingOpen);
    let pose = {
      ...DEFAULT_POSE,
      ...sample,
      x: sample.x,
      y: sample.y - sample.lift,
      facing: sample.facing < 0 ? -1 : 1,
      scale: RIVET_SCALE,
      footNear: feet.near,
      footFar: feet.far,
      gripNear: feet.grip,
      gripFar: feet.grip,
      wingOpen: wings.open,
      farWingOpen: wings.open * 0.92,
      wingLift: wings.lift,
      farWingLift: wings.lift + 0.08,
      wingTuck: wings.tuck,
      wingFan: wings.open * 0.6,
      breath: breathAt(t + idleT),
      blink: blink(t + idleT, [[TAKE.startle - 0.1, later(4.6)]]),
      crest: crest(t) * 0.9,
    };
    if (t < STORY && sample.reach > 0) pose = rivet.reachBeak(pose, beakTarget(t), sample.reach);
    if (t >= STORY) {
      const d = idle.at(idleT);
      pose = applyDeltas(pose, d);
      if (d.crestKick) pose.crest += Math.sin(idleT * 30) * 0.25 * d.crestKick;
      pose = gazeAtPointer(pose, frame);
      // Waking eases out of the doze; the programme's own doze still ends
      // the idle that follows, so a woken Rivet dozes off again.
      const wakeFade = frame.woke ? 1 - smoothstep(0, 1.3, idleT) : 0;
      pose = applyDoze(pose, Math.max(wakeFade, d.doze || 0, sleepK));
      pose.breath = breathAt(t + idleT, lerp(1.7, 0.9, pose.sleep));
    }
    pose._time = t + idleT;
    return pose;
  }

  function gazeAtPointer(pose, frame) {
    if (!frame.pointer) return pose;
    const k = camera.s * camera.zoom;
    const wx = (frame.pointer.x - camera.w / 2) / k + camera.fx;
    const wy = (frame.pointer.y - camera.h / 2) / k + camera.fy;
    const dx = (wx - pose.x) * pose.facing;
    const dy = wy - (pose.y - 110);
    const a = clamp(Math.atan2(dy, Math.max(20, dx)), -0.5, 0.45);
    return { ...pose, headAngle: lerp(pose.headAngle, a * 0.7, 0.75), lookX: clamp(dx / 300, -1, 1), lookY: clamp(dy / 300, -1, 1) };
  }

  return scene;
}

// ---------------------------------------------------------------- painting

function roundRect(ctx, x0, y0, x1, y1, r) {
  ctx.beginPath();
  ctx.moveTo(x0 + r, y0);
  ctx.arcTo(x1, y0, x1, y1, r);
  ctx.arcTo(x1, y1, x0, y1, r);
  ctx.arcTo(x0, y1, x0, y0, r);
  ctx.arcTo(x0, y0, x1, y0, r);
  ctx.closePath();
}

function paintRoom(ctx, w, h) {
  const g = ctx.createLinearGradient(0, 0, 0, h);
  g.addColorStop(0, "#060c16");
  g.addColorStop(1, "#0a1422");
  ctx.fillStyle = g;
  ctx.fillRect(0, 0, w, h);
}

function paintWallDetails(ctx, now, sleepK) {
  // Wainscot and a wall clock on UTC, the only sensible timezone for DNS.
  ctx.fillStyle = "#0c1828";
  ctx.fillRect(-1600, -40, 3200, 900);
  ctx.strokeStyle = "rgba(86,217,242,0.06)";
  ctx.lineWidth = 2;
  for (let x = -1600; x < 1600; x += 90) {
    ctx.beginPath();
    ctx.moveTo(x, -40);
    ctx.lineTo(x, 900);
    ctx.stroke();
  }
  const cx = 470;
  const cy = -360;
  ctx.fillStyle = "#e9eef2";
  ctx.beginPath();
  ctx.arc(cx, cy, 58, 0, TAU);
  ctx.fill();
  ctx.strokeStyle = "#1b2a40";
  ctx.lineWidth = 7;
  ctx.stroke();
  ctx.fillStyle = "#1b2a40";
  for (let i = 0; i < 12; i += 1) {
    const a = (i / 12) * TAU;
    ctx.fillRect(cx + Math.cos(a) * 46 - 2, cy + Math.sin(a) * 46 - 2, 4, 4);
  }
  ctx.font = "600 10px Sora, sans-serif";
  ctx.textAlign = "center";
  ctx.fillText("UTC", cx, cy + 24);
  const sec = (now * (1 - sleepK)) % 60;
  const hand = (len, a, width) => {
    ctx.lineWidth = width;
    ctx.beginPath();
    ctx.moveTo(cx, cy);
    ctx.lineTo(cx + Math.cos(a) * len, cy + Math.sin(a) * len);
    ctx.stroke();
  };
  ctx.strokeStyle = "#1b2a40";
  hand(28, -Math.PI / 2 + 0.9, 5);
  hand(40, -Math.PI / 2 + 2.6, 3.5);
  ctx.strokeStyle = "#0e93b0";
  hand(44, -Math.PI / 2 + (sec / 60) * TAU, 1.5);
}

function paintBoard(ctx, isPainted) {
  roundRect(ctx, BOARD.x0, BOARD.y0, BOARD.x1, BOARD.y1 + 20, 18);
  if (isPainted) {
    // A smoked panel bolted to the painted wall: the plaster shows through,
    // framed in the same brass as the room's fittings.
    ctx.fillStyle = "rgba(6,16,30,0.62)";
    ctx.fill();
    ctx.strokeStyle = "#7a5a2c";
    ctx.lineWidth = 8;
    ctx.stroke();
    roundRect(ctx, BOARD.x0 + 3, BOARD.y0 + 3, BOARD.x1 - 3, BOARD.y1 + 17, 16);
    ctx.strokeStyle = "rgba(255,214,150,0.4)";
    ctx.lineWidth = 1.5;
    ctx.stroke();
    for (const [x, y] of [[BOARD.x0 + 16, BOARD.y0 + 16], [BOARD.x1 - 16, BOARD.y0 + 16], [BOARD.x0 + 16, BOARD.y1 + 4], [BOARD.x1 - 16, BOARD.y1 + 4]]) {
      const bolt = ctx.createRadialGradient(x - 1.5, y - 1.5, 0.5, x, y, 5);
      bolt.addColorStop(0, "#f3d692");
      bolt.addColorStop(1, "#6d4e22");
      ctx.fillStyle = bolt;
      ctx.beginPath();
      ctx.arc(x, y, 5, 0, TAU);
      ctx.fill();
    }
    roundRect(ctx, BOARD.x0, BOARD.y0, BOARD.x1, BOARD.y1 + 20, 18);
  } else {
    const g = ctx.createLinearGradient(0, BOARD.y0, 0, BOARD.y1);
    g.addColorStop(0, "#13243b");
    g.addColorStop(1, "#0d1b2e");
    ctx.fillStyle = g;
    ctx.fill();
    ctx.strokeStyle = "#1f3552";
    ctx.lineWidth = 5;
    ctx.stroke();
  }
  // Light from the rate board falls down the face of the cabinet.
  const lg = ctx.createRadialGradient(0, -260, 30, 0, -200, 420);
  lg.addColorStop(0, "rgba(86,217,242,0.16)");
  lg.addColorStop(1, "rgba(86,217,242,0)");
  ctx.fillStyle = lg;
  ctx.fill();
  // Header: the dnsweaver mark and wordmark.
  ctx.save();
  ctx.translate(-136, -426);
  ctx.scale(0.62, 0.62);
  ctx.lineCap = "round";
  ctx.lineWidth = 5.5;
  ctx.strokeStyle = C.cyan;
  ctx.beginPath();
  ctx.moveTo(12, 21);
  ctx.bezierCurveTo(27, 21, 37, 43, 48, 43);
  ctx.stroke();
  ctx.strokeStyle = C.teal;
  ctx.beginPath();
  ctx.moveTo(12, 43);
  ctx.bezierCurveTo(27, 43, 37, 21, 48, 21);
  ctx.stroke();
  ctx.fillStyle = C.cyan;
  ctx.beginPath();
  ctx.arc(10.5, 21, 4.2, 0, TAU);
  ctx.fill();
  ctx.fillStyle = C.teal;
  ctx.beginPath();
  ctx.arc(10.5, 43, 4.2, 0, TAU);
  ctx.fill();
  ctx.lineWidth = 3.2;
  ctx.strokeStyle = C.teal;
  ctx.beginPath();
  ctx.arc(54.5, 21, 4, 0, TAU);
  ctx.stroke();
  ctx.strokeStyle = C.cyan;
  ctx.beginPath();
  ctx.arc(54.5, 43, 4, 0, TAU);
  ctx.stroke();
  ctx.restore();
  ctx.font = "600 34px Sora, sans-serif";
  ctx.textAlign = "left";
  ctx.textBaseline = "middle";
  ctx.fillStyle = C.cyan;
  ctx.fillText("dns", -88, -406);
  const dnsW = ctx.measureText("dns").width;
  ctx.fillStyle = "#e9f7fb";
  ctx.fillText("weaver", -88 + dnsW, -406);
  ctx.font = "400 11px 'JetBrains Mono', monospace";
  ctx.fillStyle = "rgba(233,247,251,0.55)";
  ctx.textAlign = "center";
  ctx.fillText("RESOLVER DESK · NIGHT SHIFT", 0, -384);
}

function paintVfd(ctx, t, now, flicker, sleepK) {
  roundRect(ctx, VFD.x0 - 8, VFD.y0 - 8, VFD.x1 + 8, VFD.y1 + 8, 12);
  ctx.fillStyle = "#1a2b44";
  ctx.fill();
  roundRect(ctx, VFD.x0, VFD.y0, VFD.x1, VFD.y1, 8);
  ctx.fillStyle = "#03080f";
  ctx.fill();
  // Faint grid of the glass.
  ctx.save();
  ctx.clip();
  ctx.strokeStyle = "rgba(86,217,242,0.05)";
  ctx.lineWidth = 1;
  for (let y = VFD.y0; y < VFD.y1; y += 4) {
    ctx.beginPath();
    ctx.moveTo(VFD.x0, y);
    ctx.lineTo(VFD.x1, y);
    ctx.stroke();
  }
  const price = priceAt(t);
  const shake = t > 1.97 && t < PLUG_OUT ? flicker(t * 30) * 0.5 + 0.5 : 0;
  ctx.textBaseline = "middle";
  ctx.font = "500 11px 'JetBrains Mono', monospace";
  ctx.fillStyle = "rgba(46,200,166,0.85)";
  ctx.textAlign = "left";
  ctx.fillText("RATE / MONTH", VFD.x0 + 14, VFD.y0 + 16);
  ctx.textAlign = "right";
  ctx.fillText(price === null ? "QUERY: price.dnsweaver" : price === 0 ? "ANSWER: price.dnsweaver" : "", VFD.x1 - 14, VFD.y0 + 16);
  TIERS.forEach((tier, i) => {
    const y = VFD.y0 + 50 + i * 40;
    ctx.textAlign = "left";
    ctx.font = "600 14px Sora, sans-serif";
    ctx.fillStyle = "rgba(46,200,166,0.9)";
    ctx.fillText(tier.name, VFD.x0 + 14, y);
    let text;
    if (price === null) text = "NXDOMAIN";
    else if (price === Infinity) text = shake > 0.5 ? "∞∞∞∞∞∞∞∞" : "OVERFLOW";
    else text = "$" + (price * tier.factor).toLocaleString("en-US");
    ctx.textAlign = "right";
    ctx.font = "400 24px 'JetBrains Mono', monospace";
    const free = price === 0;
    const color = free ? "#9ef7d7" : price === null ? "#ffb08a" : C.cyan;
    ctx.save();
    ctx.shadowColor = color;
    ctx.shadowBlur = 12 * (1 - sleepK * 0.4);
    ctx.fillStyle = color;
    ctx.fillText(text, VFD.x1 - 14 + (shake > 0.8 ? 1.5 : 0), y);
    ctx.restore();
  });
  ctx.restore();
}

function paintSource(ctx, paintedJack) {
  // The jack the price is published from.
  ctx.fillStyle = "#e6dcc6";
  roundRect(ctx, SOURCE.x - 46, SOURCE.y + 18, SOURCE.x + 82, SOURCE.y + 38, 4);
  ctx.fill();
  ctx.fillStyle = "#1b2a40";
  ctx.font = "500 10px 'JetBrains Mono', monospace";
  ctx.textAlign = "left";
  ctx.textBaseline = "middle";
  ctx.fillText("price.dnsweaver", SOURCE.x - 40, SOURCE.y + 28);
  paintJackHole(ctx, SOURCE.x, SOURCE.y, true, paintedJack);
}

function paintJackHole(ctx, x, y, lit, painted) {
  if (painted) {
    const r = PAINTED_JACK_DIAMETER / 2;
    ctx.drawImage(painted.image, x - r, y - r, PAINTED_JACK_DIAMETER, PAINTED_JACK_DIAMETER);
    return;
  }
  const ring = ctx.createRadialGradient(x - 3, y - 3, 1, x, y, 13);
  ring.addColorStop(0, "#f2d38e");
  ring.addColorStop(1, "#7a5a26");
  ctx.fillStyle = ring;
  ctx.beginPath();
  ctx.arc(x, y, 12, 0, TAU);
  ctx.fill();
  ctx.fillStyle = "#04080e";
  ctx.beginPath();
  ctx.arc(x, y, 6.5, 0, TAU);
  ctx.fill();
  if (!lit) return;
}

function paintJacks(ctx, t, now, sleepK, paintedJack) {
  // Label rails (Rivet perches on these) and jack labels.
  for (const [y, x0, x1] of [[RAIL_A, -258, 240], [RAIL_B, -150, 222]]) {
    ctx.fillStyle = "#e6dcc6";
    roundRect(ctx, x0, y, x1, y + 16, 4);
    ctx.fill();
    ctx.fillStyle = "rgba(0,0,0,0.25)";
    ctx.fillRect(x0 + 4, y + 16, x1 - x0 - 8, 4);
  }
  const chain = chainLength(t);
  JACKS.forEach((jack, i) => {
    if (i === 0) {
      paintZeroHole(ctx, t, jack);
      return;
    }
    paintJackHole(ctx, jack.x, jack.y, false, paintedJack);
    ctx.fillStyle = "#1b2a40";
    ctx.font = "600 12px 'JetBrains Mono', monospace";
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    ctx.fillText(jack.label, jack.x, (jack.y === ROW_A ? RAIL_A : RAIL_B) + 8);
    // Supervisory lamp above the jack: lit while the chain runs through it.
    const active = t < PLUG_OUT ? i - 1 <= chain : false;
    const arrive = i >= 2 && t > HOP_AT[i - 2] + HOP_FOR ? Math.exp(-(t - HOP_AT[i - 2] - HOP_FOR) * 8) : 0;
    paintLamp(ctx, jack.x, jack.y - 26, active ? 0.75 + arrive * 0.5 : 0.06, i === 8 ? "#ff8a6a" : C.cyan);
  });
}

function paintLamp(ctx, x, y, level, color) {
  ctx.fillStyle = "#081120";
  ctx.beginPath();
  ctx.arc(x, y, 6, 0, TAU);
  ctx.fill();
  if (level <= 0.07) {
    ctx.fillStyle = "rgba(86,217,242,0.1)";
    ctx.beginPath();
    ctx.arc(x, y, 4, 0, TAU);
    ctx.fill();
    return;
  }
  ctx.save();
  ctx.globalCompositeOperation = "lighter";
  const g = ctx.createRadialGradient(x, y, 0, x, y, 22);
  g.addColorStop(0, rgba(color, clamp(level)));
  g.addColorStop(0.25, rgba(color, clamp(level) * 0.45));
  g.addColorStop(1, rgba(color, 0));
  ctx.fillStyle = g;
  ctx.beginPath();
  ctx.arc(x, y, 22, 0, TAU);
  ctx.fill();
  ctx.restore();
}

function paintZeroHole(ctx, t, jack) {
  // An empty hole until Rivet's washer seats in it and becomes the 0 jack.
  ctx.fillStyle = "#04080e";
  ctx.beginPath();
  ctx.arc(jack.x, jack.y, 7.5, 0, TAU);
  ctx.fill();
  ctx.strokeStyle = "rgba(233,247,251,0.18)";
  ctx.lineWidth = 1;
  ctx.setLineDash([3, 3]);
  ctx.beginPath();
  ctx.arc(jack.x, jack.y, 12, 0, TAU);
  ctx.stroke();
  ctx.setLineDash([]);
  const seated = smoothstep(WASHER_SEAT, WASHER_SEAT + 0.12, t);
  ctx.fillStyle = "#1b2a40";
  ctx.font = "600 12px 'JetBrains Mono', monospace";
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  ctx.globalAlpha = seated;
  ctx.fillStyle = "#0b7a93";
  ctx.fillText("$0", jack.x, RAIL_A + 8);
  ctx.globalAlpha = 1;
  if (seated > 0) paintLamp(ctx, jack.x, jack.y - 26, t > PLUG_IN ? 0.95 : 0.25 * seated, C.teal);
}

// The CNAME chain: patch cords leaping from jack to jack.
function paintChain(ctx, t, paintedPlug) {
  for (let k = 0; k < HOP_AT.length; k += 1) {
    const from = JACKS[1 + k];
    const to = JACKS[2 + k];
    const start = HOP_AT[k];
    if (t < start) continue;
    const back = RETRACT_AT(k);
    if (t > back + 0.14) continue;
    // Flight out, then settling sag; on retract the far end flies home.
    let kk = ease.out(clamp((t - start) / HOP_FOR));
    if (t > back) kk = 1 - ease.in(clamp((t - back) / 0.14));
    const end = { x: lerp(from.x, to.x, kk), y: lerp(from.y, to.y, kk) - Math.sin(kk * Math.PI) * 70 };
    const settle = t - start - HOP_FOR;
    const wobble = settle > 0 && t < back ? Math.sin(settle * 18) * Math.exp(-settle * 5) * 24 : 0;
    const dist = Math.hypot(end.x - from.x, end.y - from.y);
    const sag = (kk < 1 ? 20 : 46) + dist * 0.18 + wobble;
    paintCord(ctx, from, end, sag, k % 2 ? C.cyan : C.teal, 1);
    paintPlugEndOn(ctx, from, 1);
    if (kk > 0.98) paintPlugEndOn(ctx, end, 1);
    else paintPlugSide(ctx, end, Math.atan2(end.y - from.y, end.x - from.x), 0.8, paintedPlug);
  }
}

function cordPath(ctx, a, b, sag) {
  ctx.beginPath();
  ctx.moveTo(a.x, a.y);
  const mx = (a.x + b.x) / 2;
  const my = Math.max(a.y, b.y) + sag;
  ctx.bezierCurveTo(lerp(a.x, mx, 0.6), my, lerp(b.x, mx, 0.6), my, b.x, b.y);
}

function paintCord(ctx, a, b, sag, color, alpha) {
  ctx.save();
  ctx.globalAlpha = alpha;
  ctx.lineCap = "round";
  cordPath(ctx, a, b, sag);
  ctx.strokeStyle = "#04101a";
  ctx.lineWidth = 9;
  ctx.stroke();
  ctx.strokeStyle = color;
  ctx.lineWidth = 6;
  ctx.stroke();
  // Woven cloth: a fine dash along the cord.
  ctx.strokeStyle = "rgba(255,255,255,0.28)";
  ctx.lineWidth = 1.4;
  ctx.setLineDash([2, 3]);
  ctx.stroke();
  ctx.setLineDash([]);
  ctx.restore();
}

function paintPlugEndOn(ctx, p, alpha) {
  ctx.save();
  ctx.globalAlpha = alpha;
  ctx.fillStyle = "#151515";
  ctx.beginPath();
  ctx.arc(p.x, p.y, 10, 0, TAU);
  ctx.fill();
  ctx.strokeStyle = "rgba(255,255,255,0.22)";
  ctx.lineWidth = 1.5;
  ctx.beginPath();
  ctx.arc(p.x - 1, p.y - 1, 7, Math.PI * 1.1, Math.PI * 1.7);
  ctx.stroke();
  ctx.restore();
}

function paintPlugSide(ctx, p, angle, scale, painted) {
  ctx.save();
  ctx.translate(p.x, p.y);
  ctx.rotate(angle);
  ctx.scale(scale, scale);
  if (painted) {
    ctx.drawImage(painted.image, PAINTED_PLUG.x, PAINTED_PLUG.y, PAINTED_PLUG.width, PAINTED_PLUG.height);
    ctx.restore();
    return;
  }
  ctx.fillStyle = "#151515";
  roundRect(ctx, -28, -7, 0, 7, 4);
  ctx.fill();
  const g = ctx.createLinearGradient(0, -4, 0, 4);
  g.addColorStop(0, "#ffe3a0");
  g.addColorStop(1, "#8a6526");
  ctx.fillStyle = g;
  ctx.fillRect(0, -3.5, 16, 7);
  ctx.beginPath();
  ctx.arc(17, 0, 3.6, 0, TAU);
  ctx.fill();
  ctx.restore();
}

// The root cord from price.dnsweaver: in $5, then in Rivet's beak, then in
// the washer jack. Drawn in two passes so it can hang in front of Rivet
// while he carries it.
function paintRootCord(ctx, t, heldPlug, base, pass, paintedPlug) {
  const carried = t >= PLUG_OUT && t < PLUG_IN;
  if ((pass === "front") !== carried) return;
  let end;
  if (t < PLUG_OUT) end = { x: JACKS[1].x, y: JACKS[1].y };
  else if (t < PLUG_IN) end = heldPlug.transformPoint(new DOMPoint(PAINTED_PLUG.x, 0));
  else end = { x: JACKS[0].x, y: JACKS[0].y };
  const tug = t > later(3.24) && t < PLUG_OUT ? smoothstep(later(3.24), PLUG_OUT, t) * 10 : 0;
  const settle = t > PLUG_IN ? Math.sin((t - PLUG_IN) * 16) * Math.exp(-(t - PLUG_IN) * 5) * 18 : 0;
  const dist = Math.hypot(end.x - SOURCE.x, end.y - SOURCE.y);
  paintCord(ctx, SOURCE, end, 30 + Math.max(0, 200 - dist) * 0.3 - tug + settle, C.teal, 1);
  paintPlugEndOn(ctx, SOURCE, 1);
  if (carried) {
    ctx.save();
    ctx.setTransform(base.multiply(heldPlug));
    paintPlugSide(ctx, { x: 0, y: 0 }, 0, 1, paintedPlug);
    ctx.restore();
  } else paintPlugEndOn(ctx, end, 1);
}

// Infinity will not fit in a jack: a spark and a puff of smoke when the
// chain reaches it.
function paintSpark(ctx, t) {
  const k = (t - SPARK_AT) / 0.45;
  if (k <= 0 || k >= 1) return;
  const jack = JACKS[JACKS.length - 1];
  const fade = 1 - k;
  ctx.save();
  ctx.globalCompositeOperation = "lighter";
  const glow = ctx.createRadialGradient(jack.x, jack.y, 1, jack.x, jack.y, 70 * (0.4 + k));
  glow.addColorStop(0, rgba("#fff6d0", 0.9 * fade));
  glow.addColorStop(0.3, rgba(C.cyan, 0.45 * fade));
  glow.addColorStop(1, rgba(C.cyan, 0));
  ctx.fillStyle = glow;
  ctx.fillRect(jack.x - 120, jack.y - 120, 240, 240);
  ctx.strokeStyle = rgba("#fff6d0", fade);
  ctx.lineWidth = 2;
  ctx.lineCap = "round";
  const r = rng(17);
  for (let i = 0; i < 9; i += 1) {
    const a = r.range(-Math.PI, 0.3);
    const reach = r.range(30, 80) * ease.out(clamp(k * 2.2));
    ctx.beginPath();
    ctx.moveTo(jack.x + Math.cos(a) * reach * 0.55, jack.y + Math.sin(a) * reach * 0.55);
    ctx.lineTo(jack.x + Math.cos(a) * reach, jack.y + Math.sin(a) * reach + k * k * 30);
    ctx.stroke();
  }
  ctx.globalCompositeOperation = "source-over";
  ctx.fillStyle = rgba("#9fb3c6", 0.35 * fade);
  for (let i = 0; i < 4; i += 1) {
    ctx.beginPath();
    ctx.arc(jack.x + (i - 1.5) * 9 + k * 10, jack.y - 20 - k * 70 - i * 8, 8 + k * 16, 0, TAU);
    ctx.fill();
  }
  ctx.restore();
}

function paintWasher(ctx, t, hold, painted) {
  if (t >= WASHER_SEAT) {
    // Seated: a brass ring around the jack, the 0 the price resolves to.
    const z = JACKS[0];
    if (painted) ctx.drawImage(painted.image, z.x - 13, z.y - 13, 26, 26);
    else {
      const g = ctx.createRadialGradient(z.x - 3, z.y - 3, 1, z.x, z.y, 14);
      g.addColorStop(0, "#ffe7a3");
      g.addColorStop(0.5, "#d4a443");
      g.addColorStop(1, "#7d5a1e");
      ctx.fillStyle = g;
      ctx.beginPath();
      ctx.arc(z.x, z.y, 13, 0, TAU);
      ctx.arc(z.x, z.y, 6.5, 0, TAU, true);
      ctx.fill("evenodd");
    }
    const flash = Math.exp(-(t - WASHER_SEAT) * 10);
    if (flash > 0.02) {
      ctx.strokeStyle = rgba("#fff1c4", flash);
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.arc(z.x, z.y, 14 + (1 - flash) * 14, 0, TAU);
      ctx.stroke();
    }
    return;
  }
  // Held in the beak, slightly turned while he tries the fit.
  const turn = t > later(2.5) && t < WASHER_SEAT ? Math.sin((t - later(2.5)) * 30) * 0.4 : 0;
  ctx.save();
  ctx.setTransform(hold);
  ctx.scale(1 / RIVET_SCALE, 1 / RIVET_SCALE * Math.cos(turn));
  if (painted) {
    ctx.drawImage(painted.image, -11, -10, 22, 20);
    ctx.restore();
    return;
  }
  const g = ctx.createRadialGradient(-3, -3, 1, 0, 0, 11);
  g.addColorStop(0, "#ffe7a3");
  g.addColorStop(0.5, "#d4a443");
  g.addColorStop(1, "#7d5a1e");
  ctx.fillStyle = g;
  ctx.beginPath();
  ctx.ellipse(0, 0, 11, 10, 0, 0, TAU);
  ctx.ellipse(0, 0, 5, 4.5, 0, 0, TAU, true);
  ctx.fill("evenodd");
  ctx.strokeStyle = "rgba(40,26,8,0.8)";
  ctx.lineWidth = 0.9;
  ctx.stroke();
  ctx.restore();
}

function paintShelf(ctx, now, sleepK, idleLamps, isPainted) {
  // The keyshelf: a wooden ledge with answer keys and spare plugs. On the
  // painted worktop only the keys and their lamps are added.
  if (isPainted) {
    // The board's foot runs behind the worktop: a walnut lip along its front
    // gives Rivet's feet a surface rather than the board's vertical face.
    const lip = ctx.createLinearGradient(0, SHELF_Y - 1, 0, SHELF_Y + 26);
    lip.addColorStop(0, "#7a5532");
    lip.addColorStop(0.12, "#4c321c");
    lip.addColorStop(1, "#21150b");
    ctx.fillStyle = lip;
    ctx.fillRect(BOARD.x0 - 34, SHELF_Y - 1, BOARD.x1 - BOARD.x0 + 68, 27);
    ctx.fillStyle = "rgba(255,214,160,0.28)";
    ctx.fillRect(BOARD.x0 - 34, SHELF_Y - 1, BOARD.x1 - BOARD.x0 + 68, 2);
    ctx.fillStyle = "rgba(0,0,0,0.35)";
    ctx.fillRect(BOARD.x0 - 34, SHELF_Y + 26, BOARD.x1 - BOARD.x0 + 68, 4);
    paintShelfKeys(ctx, now, sleepK, idleLamps);
    return;
  }
  const g = ctx.createLinearGradient(0, SHELF_Y, 0, SHELF_Y + 70);
  g.addColorStop(0, "#5a3a22");
  g.addColorStop(0.2, "#3f2817");
  g.addColorStop(1, "#24170d");
  ctx.fillStyle = g;
  ctx.fillRect(BOARD.x0 - 40, SHELF_Y, BOARD.x1 - BOARD.x0 + 80, 90);
  ctx.fillStyle = "rgba(255,220,170,0.18)";
  ctx.fillRect(BOARD.x0 - 40, SHELF_Y, BOARD.x1 - BOARD.x0 + 80, 3);
  paintShelfKeys(ctx, now, sleepK, idleLamps);
}

function paintShelfKeys(ctx, now, sleepK, idleLamps) {
  for (let i = 0; i < 6; i += 1) {
    const x = 60 + i * 38;
    ctx.fillStyle = "#151515";
    roundRect(ctx, x - 6, SHELF_Y + 14, x + 6, SHELF_Y + 40, 4);
    ctx.fill();
    ctx.fillStyle = "#cfd8de";
    ctx.fillRect(x - 2, SHELF_Y + 6, 4, 10);
    const lamp = idleLamps[i];
    const lit = (Math.sin((now + lamp.phase) * lamp.rate * TAU) > 0.6 ? 0.8 : 0.1) * (1 - sleepK * 0.8);
    paintLamp(ctx, x, SHELF_Y + 54, lit, i % 2 ? C.cyan : C.teal);
  }
}

// Wide frames show the rest of the board: sources on the left patched to
// providers on the right, crossing like the dnsweaver mark.
function paintWings(ctx, now, sleepK, idleLamps, paintedJack) {
  const sources = ["traefik", "caddy", "proxmox", "k8s", "incus"];
  const providers = ["technitium", "cloudflare", "pi-hole", "opnsense", "rfc2136"];
  for (const [x0, names, side] of [[-560, sources, -1], [330, providers, 1]]) {
    roundRect(ctx, x0, -360, x0 + 230, 150, 14);
    ctx.fillStyle = "#0e1c30";
    ctx.fill();
    ctx.strokeStyle = "#1c304b";
    ctx.lineWidth = 4;
    ctx.stroke();
    ctx.fillStyle = "rgba(233,247,251,0.5)";
    ctx.font = "500 11px 'JetBrains Mono', monospace";
    ctx.textAlign = "center";
    ctx.fillText(side < 0 ? "SOURCES" : "PROVIDERS", x0 + 115, -334);
    names.forEach((name, i) => {
      const y = -290 + i * 80;
      paintJackHole(ctx, x0 + 50, y, false, paintedJack);
      ctx.fillStyle = "#e6dcc6";
      roundRect(ctx, x0 + 72, y - 9, x0 + 206, y + 9, 3);
      ctx.fill();
      ctx.fillStyle = "#1b2a40";
      ctx.textAlign = "left";
      ctx.fillText(name, x0 + 78, y + 1);
      const lamp = idleLamps[(i + (side > 0 ? 5 : 0)) % idleLamps.length];
      const lit = Math.sin((now + lamp.phase) * lamp.rate * TAU) > 0.2 ? 0.55 : 0.12;
      paintLamp(ctx, x0 + 20, y, lit * (1 - sleepK * 0.7), side < 0 ? C.teal : C.cyan);
    });
  }
  // Two long cords crossing under the board, like the mark.
  const sway = Math.sin(now * 0.8) * 4 * (1 - sleepK);
  paintCord(ctx, { x: -510, y: -130 }, { x: 380, y: 30 }, 160 + sway, C.cyan, 0.55);
  paintCord(ctx, { x: -510, y: 30 }, { x: 380, y: -130 }, 210 - sway, C.teal, 0.55);
}
