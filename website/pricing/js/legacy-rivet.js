// Rivet the jackdaw: a skeletal 2D rig with painted parts and an ink fallback.
//
// A pose is plain numbers (see DEFAULT_POSE); draw() composes its parts every
// frame. The original-design skin preserves the illustrated body and two
// head drawings. Flight feathers and coverts move individually on the wing,
// while body, head, tail and legs retain their own transforms. When a painted
// piece is unavailable, the procedural plumage supplies the fallback.
//
// Space conventions: rig units, +x is the way Rivet faces, -y is up, and the
// origin is the ground point between his feet. Rivet stands about 200 units
// tall from feet to crest.

import { TAU, clamp, lerp, rng, rgba, taperedStroke, bezierPoints, rotate, noise1 } from "./ink.js";

export const DEFAULT_POSE = {
  x: 0,
  y: 0,
  scale: 1,
  facing: 1, // 1 faces right, -1 faces left
  bodyPitch: 0, // radians, positive tips the breast forward and down
  crouch: 0, // 0 standing, 1 deep crouch before a hop
  squash: 0, // landing squash (+) or take-off stretch (-)
  puff: 0, // 0 sleek, 1 fully fluffed
  breath: 0, // -1..1, supplied by idle behavior
  neck: 1, // neck extension: 0.6 tucked, 1.35 craned
  headX: 0, // extra head offset in body space (pecking reach)
  headY: 0,
  headAngle: 0, // absolute head rotation in radians
  headTurn: 0, // 0 profile, 1 turned to look back over the shoulder
  beakOpen: 0,
  blink: 0, // 0 open, 1 closed
  lid: 0, // resting upper-lid droop, for a skeptical or sleepy look
  lookX: 0, // pupil offset, -1..1
  lookY: 0,
  brow: 0, // -1 lowered (intent), 1 raised (curious)
  crest: 0, // spring offset of the untidy head feather, radians
  wingOpen: 0, // near wing: 0 folded, 1 spread
  wingRaise: 0, // shoulder rotation while spread, radians (+ is downstroke)
  farWingOpen: 0,
  farWingRaise: 0,
  wingTuck: 0, // folded-wing droop (+) or lift (-), for shrugs and flicks
  tailAngle: 0,
  tailSpread: 0,
  footNear: { x: 16, y: 0 }, // foot targets in root space
  footFar: { x: -4, y: 0 },
  gripNear: 0, // 0 flat on ground, 1 curled around a perch
  gripFar: 0,
  perchAngle: 0, // surface slope under the feet
  sleep: 0, // head tucked and eyes closed
  // Mesh-only controls; the painted 2D fallback ignores them.
  wingSweep: 0, // spread wing swung forward (+) or back (-), radians
  wingFan: 0, // primaries fingered apart (+)
  eyeWide: 0, // 0 normal, 1 eye popped wide in surprise
  pupil: 0, // -1 pinned to a dot, +1 dilated
  crestRaise: 0, // crest stood up (+) or flattened (-)
};

const C = {
  ink: "#0e0f12",
  bodyLit: "#454952",
  body: "#2a2c33",
  bodyDark: "#17181c",
  cap: "#16171b",
  nape: "#9a9fa6",
  napeLight: "#c5c8cd",
  napeDark: "#6a6f77",
  barbDark: "#0f1013",
  barbMid: "#3b3e46",
  barbLight: "#868c96",
  featherEdge: "#7d838d",
  beak: "#26282d",
  beakLight: "#757a83",
  irisCenter: "#f4f8f9",
  irisMid: "#dbe6ec",
  irisEdge: "#a9bfcd",
  pupil: "#0a0b0d",
  leg: "#17181c",
  legLight: "#575b64",
  claw: "#2c2925",
};

// Body outline in body space (body centre at the origin), upright posture.
const BODY_OUTLINE = [
  [36, -50], [52, -24], [55, 0], [46, 26], [26, 44], [-4, 50], [-30, 40],
  [-48, 24], [-56, 8], [-48, -10], [-28, -30], [-4, -46], [14, -54],
];
const BODY_CENTER = { x: -4, y: -86 }; // relative to the rig origin
const HIP = { x: 2, y: -46 }; // body pitch pivot, rig space
const NECK_SOCKET = { x: 22, y: -50 }; // body space
const HEAD_OUTLINE = [
  [25, -12], [20, -23], [10, -30], [-4, -33], [-18, -29], [-28, -17],
  [-32, 0], [-30, 16], [-12, 28], [8, 25], [18, 15], [24, 5],
];
const EYE = { x: 9, y: -8, r: 7.8 };
const GAPE = { x: 20, y: 3 };
const BEAK_GRIP = { x: 30, y: 5 }; // inside the original design's shorter bill
const TAIL_BASE = { x: -50, y: 12 }; // body space
const SHOULDER = { x: 16, y: -34 }; // body space
const CROWN = { x: -5, y: -32 };

// Painted pieces: how they sit against the procedural drawing.
const PAINTED_CONTOUR = 1.1; // ink weight around painted plates (procedural: 1.6-1.8)
const PAINTED_BELLY_SHADE = 0.4;
const PAINTED_FEATHER_WIDTH = 1.15; // painted vanes are soft-edged; give them room
const FAR_WING_SHADE = 0.45;
// Rim light bands: width in device pixels per unit of light.rimWidth, and the
// share of light.rimStrength each one carries.
const RIM_BANDS = [
  { width: 0.9, alpha: 0.55 },
  { width: 1.9, alpha: 0.22 },
  { width: 3.4, alpha: 0.1 },
];
const CREST_REACH = 1.4; // painted crest feathers start under the crown plumage
const CREST_WIDTH = 1.15;
const COVERT_REACH = 1.15; // a covert outline starts behind its base; the paint covers it
// Head offsets articulate a short neck; they cannot move the skull through
// the torso to satisfy a scene target. The ellipse keeps diagonal reaches
// within the same envelope as a straight extension.
const HEAD_REACH = { forward: 28, back: 32, up: 18, down: 24 };

function headOffset(x, y) {
  const rx = x < 0 ? HEAD_REACH.back : HEAD_REACH.forward;
  const ry = y < 0 ? HEAD_REACH.up : HEAD_REACH.down;
  const length = Math.max(1, Math.hypot(x / rx, y / ry));
  return { x: x / length, y: y / length };
}

// Landmarks the paint templates and fitting tools mark, in the spaces noted above.
export const RIG_LANDMARKS = { BODY_CENTER, HIP, NECK_SOCKET, EYE, GAPE, TAIL_BASE, SHOULDER, CROWN };

export class Rivet {
  constructor({ seed = 29 } = {}) {
    this.layer = document.createElement("canvas");
    this.layerCtx = this.layer.getContext("2d");
    this.rimLayer = document.createElement("canvas");
    this.rimCtx = this.rimLayer.getContext("2d");
    const probe = document.createElement("canvas").getContext("2d");
    this.shapes = buildShapes();
    this.plumage = buildPlumage(seed, this.shapes, probe);
    this.kit = null;
  }

  // Paint the rig with painted pieces (paint.js rivetKit). Each part keeps its
  // procedural drawing underneath, so a missing piece or a gap at the edge of
  // a painting shows plumage, never a hole.
  usePaint(kit) {
    this.kit = kit;
  }

  rootMatrix(pose, base = new DOMMatrix()) {
    return base.translate(pose.x, pose.y).scale(pose.scale * pose.facing, pose.scale);
  }

  bodyMatrix(pose, root) {
    const crouchDrop = pose.crouch * 18;
    const tall = 1 - pose.squash * 0.12 + pose.breath * 0.014 + pose.puff * 0.05;
    const wide = 1 + pose.squash * 0.1 + pose.puff * 0.08 + pose.breath * 0.01;
    return root
      .translate(HIP.x, HIP.y + crouchDrop)
      .rotate(deg(pose.bodyPitch))
      .translate(BODY_CENTER.x - HIP.x, BODY_CENTER.y - HIP.y)
      .scale(wide, tall);
  }

  headMatrix(pose, root) {
    const body = this.bodyMatrix(pose, root);
    const reach = clamp(pose.neck, 0.6, 1.35);
    const tuck = pose.sleep;
    const offset = headOffset(pose.headX, pose.headY);
    const end = {
      x: NECK_SOCKET.x + lerp(14 * reach, -10, tuck) + offset.x,
      y: NECK_SOCKET.y + lerp(-28 * reach, -12, tuck) + offset.y,
    };
    const p = root.inverse().transformPoint(body.transformPoint(new DOMPoint(end.x, end.y)));
    // The head keeps its own angle (birds stabilise their heads); only its
    // position follows the body.
    // A quick squash through zero width reads as the head turning in depth.
    // The flip comes before the tilt, so a positive headAngle still means
    // "beak down" when Rivet looks back over his shoulder.
    const turn = Math.cos(Math.PI * clamp(pose.headTurn));
    const width = Math.abs(turn) < 0.08 ? Math.sign(turn || 1) * 0.08 : turn;
    return root
      .translate(p.x, p.y)
      .scale(width, 1)
      .rotate(deg(lerp(pose.headAngle, 0.55, tuck)))
      .scale(1 + pose.puff * 0.05);
  }

  // Target is in scene units, before the camera. Aim in the facing frame,
  // then solve only the small
  // residual in body space. Unreachable targets remain unreachable: the
  // scene must place the feet and bend the body before the neck can reach.
  reachBeak(pose, target, amount) {
    const k = clamp(amount);
    if (k === 0) return pose;
    const root = this.rootMatrix(pose);
    const invRoot = root.inverse();
    const start = headOffset(pose.headX, pose.headY);
    const head = invRoot.transformPoint(this.headMatrix(pose, root).transformPoint(new DOMPoint(0, 0)));
    const goal = invRoot.transformPoint(new DOMPoint(target.x, target.y));
    const jaw = new DOMMatrix().translate(GAPE.x, GAPE.y).rotate(pose.beakOpen * 13).translate(-GAPE.x, -GAPE.y);
    const hold = jaw.transformPoint(new DOMPoint(BEAK_GRIP.x, BEAK_GRIP.y));
    const angle = clamp(Math.atan2(goal.y - head.y, goal.x - head.x) - Math.atan2(hold.y, hold.x), -0.75, 1.55);
    const aimed = { ...pose, headAngle: angle, headTurn: 0, headX: start.x, headY: start.y };
    const beak = this.locate(aimed, "beakHold");
    const invBody = this.bodyMatrix(aimed, root).inverse();
    const from = invBody.transformPoint(new DOMPoint(beak.x, beak.y));
    const to = invBody.transformPoint(new DOMPoint(target.x, target.y));
    const offset = headOffset(start.x + to.x - from.x, start.y + to.y - from.y);
    return {
      ...pose,
      headAngle: lerp(pose.headAngle, angle, k),
      headTurn: lerp(pose.headTurn, 0, k),
      headX: lerp(start.x, offset.x, k),
      headY: lerp(start.y, offset.y, k),
    };
  }

  // World position of a named point, for props, cords and carried parts.
  locate(pose, name, base = new DOMMatrix()) {
    const root = this.rootMatrix(pose, base);
    const head = this.headMatrix(pose, root);
    const at = (m, x, y) => {
      const q = m.transformPoint(new DOMPoint(x, y));
      return { x: q.x, y: q.y };
    };
    switch (name) {
      case "beakTip":
        return at(head, 55, 4);
      case "beakHold":
        return at(this.holdMatrix(pose, base), 0, 0);
      case "eye":
        return at(head, EYE.x, EYE.y);
      case "head":
        return at(head, 0, 0);
      case "crown":
        return at(head, CROWN.x, CROWN.y);
      case "footNear":
        return at(root, pose.footNear.x, pose.footNear.y);
      case "footFar":
        return at(root, pose.footFar.x, pose.footFar.y);
      case "chest":
        return at(this.bodyMatrix(pose, root), 44, -6);
      case "belly":
        return at(this.bodyMatrix(pose, root), 0, 50);
      default:
        return at(root, 0, 0);
    }
  }

  // Head transform for a held prop (a washer, a twig, a cord's plug).
  holdMatrix(pose, base = new DOMMatrix()) {
    const head = this.headMatrix(pose, this.rootMatrix(pose, base));
    return head.translate(GAPE.x, GAPE.y).rotate(pose.beakOpen * 13).translate(BEAK_GRIP.x - GAPE.x, BEAK_GRIP.y - GAPE.y);
  }

  drawShadow(ctx, pose, base, { strength = 0.35, color = "#000000", width = 1 } = {}) {
    const root = this.rootMatrix(pose, base);
    const lift = clamp(-Math.min(pose.footNear.y, pose.footFar.y) / 90, 0, 1);
    ctx.save();
    ctx.setTransform(root);
    const g = ctx.createRadialGradient(0, 0, 2, 0, 0, 80);
    g.addColorStop(0, rgba(color, strength * (1 - lift * 0.75)));
    g.addColorStop(0.6, rgba(color, strength * 0.35 * (1 - lift * 0.75)));
    g.addColorStop(1, rgba(color, 0));
    ctx.fillStyle = g;
    ctx.beginPath();
    ctx.ellipse(-10, 1, 80 * width * (1 - lift * 0.3), 11 * (1 - lift * 0.4), 0, 0, TAU);
    ctx.fill();
    ctx.restore();
  }

  // Renders the bird. `base` maps scene units to device pixels; `light` sets
  // the key direction (pointing toward the light), rim colour and ambient tint.
  draw(ctx, pose, base, light = {}) {
    const lit = {
      dir: light.dir || { x: 0.55, y: -0.83 },
      rim: light.rim || "#f6ead2",
      rimStrength: light.rimStrength ?? 0.5,
      rimWidth: light.rimWidth ?? 1,
      ambient: light.ambient || null,
      ambientStrength: light.ambientStrength ?? 0,
    };
    // Plumage highlights are baked per light direction in bird space.
    const localDir = { x: lit.dir.x * pose.facing, y: lit.dir.y };
    const sheen = this.plumage.sheenFor(localDir);
    const root = this.rootMatrix(pose, base);
    const b = this.deviceBounds(root, pose);
    if (this.layer.width !== b.w || this.layer.height !== b.h) {
      this.layer.width = this.rimLayer.width = b.w;
      this.layer.height = this.rimLayer.height = b.h;
    }
    const lc = this.layerCtx;
    lc.setTransform(1, 0, 0, 1, 0, 0);
    lc.globalCompositeOperation = "source-over";
    lc.globalAlpha = 1;
    lc.clearRect(0, 0, b.w, b.h);
    const local = new DOMMatrix().translate(-b.x, -b.y).multiply(root);
    const pixel = Math.hypot(root.a, root.b);

    this.drawWing(lc, pose, local, "far", sheen);
    // Painted feathers carry no depth of their own; the far wing sinks back
    // under a shade laid over everything drawn so far, which is only it.
    if (this.kit) shadeLayer(lc, FAR_WING_SHADE);
    this.drawTail(lc, pose, local, sheen);
    this.drawNeck(lc, pose, local, sheen, localDir);
    this.drawBody(lc, pose, local, sheen, localDir);
    this.drawWing(lc, pose, local, "near", sheen);
    this.drawHead(lc, pose, local, sheen, localDir);
    this.applyLight(pose, lit, pixel);

    // Legs are thin enough that a rim would read as a halo, so they are drawn
    // straight onto the scene beneath the feathered layer.
    ctx.save();
    this.drawLeg(ctx, pose, root, "far");
    this.drawLeg(ctx, pose, root, "near");
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.drawImage(this.layer, b.x, b.y);
    ctx.restore();
  }

  deviceBounds(root, pose) {
    const open = Math.max(pose.wingOpen, pose.farWingOpen);
    const corners = [
      [-150 - open * 40, -250 - open * 70],
      [140 + open * 60, -250 - open * 70],
      [-150 - open * 40, 30],
      [140 + open * 60, 30],
    ].map(([x, y]) => root.transformPoint(new DOMPoint(x, y)));
    const xs = corners.map((p) => p.x);
    const ys = corners.map((p) => p.y);
    const x = Math.floor(Math.min(...xs));
    const y = Math.floor(Math.min(...ys));
    return { x, y, w: Math.max(1, Math.ceil(Math.max(...xs) - x)), h: Math.max(1, Math.ceil(Math.max(...ys) - y)) };
  }

  drawBody(ctx, pose, local, sheen, dir) {
    const m = this.bodyMatrix(pose, local);
    const { body } = this.shapes;
    const P = this.plumage.body;
    ctx.save();
    ctx.setTransform(m);
    if (this.kit?.originalDesign && this.kit.body) {
      // Keep the original illustration's own contour and layered feathers.
      drawPiece(ctx, this.kit.body);
      ctx.restore();
      return;
    }
    // Tonal form: lit toward the light, settling into a dark belly.
    const g = ctx.createRadialGradient(dir.x * 30 + 6, dir.y * 30 - 6, 4, 0, 6, 72);
    g.addColorStop(0, C.bodyLit);
    g.addColorStop(0.45, C.body);
    g.addColorStop(1, C.bodyDark);
    ctx.fillStyle = g;
    ctx.fill(body.path);
    ctx.save();
    ctx.clip(body.path);
    if (this.kit?.body) {
      drawPiece(ctx, this.kit.body);
      bellyShadow(ctx, PAINTED_BELLY_SHADE);
      ctx.restore();
      inkContour(ctx, body.samples, dir, PAINTED_CONTOUR, P.contourNoise, body.skip);
      ctx.restore();
      return;
    }
    paintWashes(ctx, P.washes);
    const nj = ctx.createRadialGradient(24, -50, 2, 24, -50, 30);
    nj.addColorStop(0, "rgba(20,21,25,0.85)");
    nj.addColorStop(1, "rgba(20,21,25,0)");
    ctx.fillStyle = nj;
    ctx.fillRect(-10, -80, 70, 60);
    ctx.fillStyle = "rgba(92,97,105,0.45)";
    ctx.fill(P.mantle.under);
    ctx.fillStyle = C.napeDark;
    ctx.fill(P.mantle.dark);
    ctx.fillStyle = rgba(C.nape, 0.85);
    ctx.fill(P.mantle.mid);
    strokeBatch(ctx, P.barbsDark, C.barbDark, 0.45, 0.8);
    strokeBatch(ctx, P.barbsMid, C.barbMid, 0.55, 0.7);
    strokeBatch(ctx, P.scallops, C.ink, 0.5, 1);
    strokeBatch(ctx, sheen.body, C.barbLight, 0.32, 0.6);
    strokeBatch(ctx, sheen.bodyEdges, C.featherEdge, 0.42, 0.85);
    bellyShadow(ctx, 0.55);
    strokeBatch(ctx, P.hatch, C.ink, 0.35, 0.8);
    ctx.restore();
    inkContour(ctx, body.samples, dir, 1.8, P.contourNoise, body.skip);
    strokeBatch(ctx, P.fringe, C.ink, 0.9, 1.1);
    ctx.restore();
  }

  drawTail(ctx, pose, local, sheen) {
    const m = this.bodyMatrix(pose, local)
      .translate(TAIL_BASE.x, TAIL_BASE.y)
      .rotate(deg(pose.tailAngle));
    ctx.save();
    ctx.setTransform(m);
    this.plumage.tail.forEach((f, i) => {
      const spread = f.slot * (0.055 + pose.tailSpread * 0.17);
      ctx.save();
      ctx.rotate(Math.PI - 0.9 + spread);
      if (!this.drawPaintedFeather(ctx, "tail", i, f.len, f.width)) drawFlightFeather(ctx, f, sheen.flight, 0);
      ctx.restore();
    });
    ctx.restore();
  }

  // A painted covert, trimmed to the covert's own outline so neighbouring
  // coverts overlap in rows like scales. False when the kit has none.
  drawPaintedCovert(ctx, c, index) {
    if (!this.kit?.feathers[c.kind]?.length) return false;
    ctx.save();
    ctx.clip(c.shape);
    this.drawPaintedFeather(ctx, c.kind, index, c.len * COVERT_REACH, c.width);
    ctx.restore();
    ctx.strokeStyle = rgba(C.ink, 0.45);
    ctx.lineWidth = 0.8;
    ctx.stroke(c.rim);
    return true;
  }

  // A painted feather in the current feather frame (base at the origin, tip
  // along +x). Variants alternate along the row. False when the kit has none.
  drawPaintedFeather(ctx, kind, index, len, width) {
    const set = this.kit?.feathers[kind];
    if (!set?.length) return false;
    const sprite = set[index % set.length];
    ctx.save();
    ctx.scale(len, width * PAINTED_FEATHER_WIDTH);
    drawPiece(ctx, sprite);
    ctx.restore();
    return true;
  }

  drawWing(ctx, pose, local, side, sheen) {
    const isNear = side === "near";
    const open = clamp(isNear ? pose.wingOpen : pose.farWingOpen);
    const raise = isNear ? pose.wingRaise : pose.farWingRaise;
    if (!isNear && open < 0.03) return;
    const shoulder = isNear ? SHOULDER : { x: SHOULDER.x - 10, y: SHOULDER.y - 8 };
    const m = this.bodyMatrix(pose, local)
      .translate(shoulder.x, shoulder.y)
      .rotate(deg(pose.wingTuck * 0.14 * (1 - open) + raise * open));
    const k = open * open * (3 - 2 * open);
    const darken = isNear ? 0 : 0.45;
    ctx.save();
    ctx.setTransform(m);
    this.plumage.wing.flight.forEach((f, i) => {
      ctx.save();
      ctx.translate(lerp(f.folded.x, f.spread.x, k), lerp(f.folded.y, f.spread.y, k));
      ctx.rotate(lerpAngle(f.folded.angle, f.spread.angle, k));
      const len = lerp(f.folded.len, f.spread.len, k);
      if (!this.drawPaintedFeather(ctx, f.kind, i, len, f.width)) drawFlightFeather(ctx, { ...f, len }, sheen.flight, darken);
      ctx.restore();
    });
    this.plumage.wing.coverts.forEach((c, i) => {
      ctx.save();
      ctx.translate(lerp(c.folded.x, c.spread.x, k), lerp(c.folded.y, c.spread.y, k));
      ctx.rotate(lerpAngle(c.folded.angle, c.spread.angle, k));
      if (!this.drawPaintedCovert(ctx, c, i)) drawCovert(ctx, c, darken);
      ctx.restore();
    });
    ctx.restore();
  }

  drawLeg(ctx, pose, local, side) {
    const isNear = side === "near";
    const body = this.bodyMatrix(pose, local);
    const hipBody = isNear ? { x: 8, y: 40 } : { x: 20, y: 36 };
    const hip = local.inverse().transformPoint(body.transformPoint(new DOMPoint(hipBody.x, hipBody.y)));
    const foot = isNear ? pose.footNear : pose.footFar;
    const grip = isNear ? pose.gripNear : pose.gripFar;
    // Two-bone IK; a bird's "knee" in view is really its heel, bending back.
    const upper = 18;
    const lower = 42;
    const dx = foot.x - hip.x;
    const dy = foot.y - hip.y;
    const dist = clamp(Math.hypot(dx, dy), 8, upper + lower - 0.6);
    const aim = Math.atan2(dy, dx);
    const bend = Math.acos(clamp((upper * upper + dist * dist - lower * lower) / (2 * upper * dist), -1, 1));
    const heel = { x: hip.x + Math.cos(aim + bend) * upper, y: hip.y + Math.sin(aim + bend) * upper };
    const end = { x: hip.x + Math.cos(aim) * dist, y: hip.y + Math.sin(aim) * dist };
    const tone = isNear ? C.leg : "#0d0e10";
    ctx.save();
    ctx.setTransform(local);
    // Feathered "trousers" over the upper leg.
    ctx.fillStyle = tone;
    taperedStroke(ctx, [{ x: lerp(hip.x, heel.x, 0.6), y: lerp(hip.y, heel.y, 0.6) }, heel, end], 4.6, { taperStart: 0, taperEnd: 0 });
    if (this.kit?.tuft) {
      // The painted tuft hangs from the hip along the thigh toward the heel.
      const lean = Math.atan2(heel.x - hip.x, heel.y - hip.y);
      ctx.save();
      ctx.translate(hip.x, hip.y - 4);
      ctx.rotate(-lean * 0.6);
      ctx.scale(1.1, 1.1);
      ctx.clip(this.shapes.tuft);
      drawPiece(ctx, this.kit.tuft);
      ctx.restore();
    }
    // Feathered "trousers": loose tufts hanging over the upper leg.
    ctx.fillStyle = isNear ? "#24262c" : "#141518";
    for (let i = 0; i < (this.kit?.tuft ? 0 : 7); i += 1) {
      const k = i / 6;
      const sx = lerp(hip.x - 8, hip.x + 8, k);
      const sy = hip.y - 4 + Math.sin(k * Math.PI) * -2;
      const ex = lerp(sx, heel.x, 0.75) + (k - 0.5) * 6;
      const ey = lerp(sy, heel.y, 0.8) - Math.abs(k - 0.5) * 6;
      taperedStroke(ctx, [{ x: sx, y: sy }, { x: lerp(sx, ex, 0.5) + (k - 0.5) * 3, y: lerp(sy, ey, 0.5) }, { x: ex, y: ey }], 7, { taperStart: 0, taperEnd: 1 });
    }
    ctx.strokeStyle = rgba(C.legLight, isNear ? 0.42 : 0.18);
    ctx.lineWidth = 0.9;
    const len = Math.hypot(end.x - heel.x, end.y - heel.y) || 1;
    const nx = -(end.y - heel.y) / len;
    const ny = (end.x - heel.x) / len;
    for (let i = 1; i < 7; i += 1) {
      const kk = i / 7;
      const px = lerp(heel.x, end.x, kk);
      const py = lerp(heel.y, end.y, kk);
      ctx.beginPath();
      ctx.moveTo(px - nx * 2.3, py - ny * 2.3);
      ctx.quadraticCurveTo(px + 0.5, py + 1, px + nx * 1.8, py + ny * 1.8 + 0.6);
      ctx.stroke();
    }
    drawToes(ctx, end, pose.perchAngle, grip, tone, isNear);
    ctx.restore();
  }

  // Neck mass, drawn beneath the body so the shoulders overlap it and only the
  // part above the mantle shows: skull and back join without a seam.
  // The neck mass joining skull to mantle, as a path in the coordinates that
  // `local` maps from (root space for a root matrix). `nape` is the
  // back-of-neck point and `back` where it meets the mantle, for the contour.
  neckOutline(pose, local) {
    const body = this.bodyMatrix(pose, local);
    const head = this.headMatrix(pose, local);
    const inv = local.inverse();
    const toLocal = (m, x, y) => inv.transformPoint(m.transformPoint(new DOMPoint(x, y)));
    const a = toLocal(head, -31, -4);
    const bpt = toLocal(body, -14, -38);
    const c = toLocal(body, 18, -30);
    const d = toLocal(body, 46, -30);
    const e = toLocal(head, 14, 22);
    const f = toLocal(head, -12, 28);
    const path = new Path2D();
    path.moveTo(a.x, a.y);
    path.quadraticCurveTo(lerp(a.x, bpt.x, 0.5) - 6, lerp(a.y, bpt.y, 0.5) - 2, bpt.x, bpt.y);
    path.quadraticCurveTo(c.x, c.y + 6, d.x, d.y);
    path.quadraticCurveTo(lerp(d.x, e.x, 0.5) + 5, lerp(d.y, e.y, 0.5), e.x, e.y);
    path.lineTo(f.x, f.y);
    path.closePath();
    return { path, nape: a, back: bpt };
  }

  drawNeck(ctx, pose, local, sheen, dir) {
    const head = this.headMatrix(pose, local);
    const H = this.plumage.head;
    const { path: neck, nape: a, back: bpt } = this.neckOutline(pose, local);
    ctx.save();
    ctx.setTransform(local);
    if (this.kit?.originalDesign && this.kit.body) {
      // Feather the bridge beneath the original portrait. A flat ink fill
      // leaves a black band where its silver nape overlaps the shoulder.
      ctx.clip(neck);
      ctx.setTransform(this.bodyMatrix(pose, local).translate(0, -45).scale(1, 1.8));
      drawPiece(ctx, this.kit.body);
      ctx.restore();
      return;
    }
    ctx.fillStyle = C.cap;
    ctx.fill(neck);
    ctx.clip(neck);
    ctx.setTransform(head);
    if (this.kit?.head) {
      // The head painting runs down the neck, so the neck shows its lower part.
      drawPiece(ctx, this.kit.head);
    } else {
      const ng = ctx.createRadialGradient(dir.x * 18 + 6, 30, 4, 4, 34, 50);
      ng.addColorStop(0, "#30323a");
      ng.addColorStop(1, "rgba(22,23,27,0)");
      ctx.fillStyle = ng;
      ctx.fillRect(-60, -10, 120, 100);
      strokeBatch(ctx, H.neckBarbs, C.barbDark, 0.5, 0.75);
      paintNape(ctx, H, sheen);
    }
    ctx.restore();
    ctx.save();
    ctx.setTransform(local);
    ctx.strokeStyle = rgba(C.ink, 0.9);
    ctx.lineWidth = 1.8;
    ctx.lineCap = "round";
    ctx.beginPath();
    ctx.moveTo(a.x, a.y);
    ctx.quadraticCurveTo(lerp(a.x, bpt.x, 0.5) - 6, lerp(a.y, bpt.y, 0.5) - 2, bpt.x, bpt.y);
    ctx.stroke();
    ctx.restore();
  }

  drawHead(ctx, pose, local, sheen, dir) {
    const head = this.headMatrix(pose, local);
    const H = this.plumage.head;
    ctx.save();
    ctx.setTransform(head);
    if (this.kit?.originalDesign && this.kit.head) {
      // These two unchanged atlas drawings retain the original face, bill
      // and swept crest. Joint motion supplies the in-between movement.
      drawPiece(ctx, pose.beakOpen > 0.28 && this.kit.headOpen ? this.kit.headOpen : this.kit.head);
      this.drawPortraitBlink(ctx, pose);
      ctx.restore();
      return;
    }
    const hp = this.shapes.head;
    ctx.fillStyle = C.cap;
    ctx.fill(hp.path);
    ctx.save();
    ctx.clip(hp.path);
    if (this.kit?.head) {
      drawPiece(ctx, this.kit.head);
      ctx.restore();
      inkContour(ctx, hp.samples, dir, PAINTED_CONTOUR, H.contourNoise, hp.skip);
    } else {
      const hg = ctx.createRadialGradient(dir.x * 14 + 4, dir.y * 14 - 6, 2, 0, 0, 36);
      hg.addColorStop(0, "#34363d");
      hg.addColorStop(1, "rgba(22,23,27,0)");
      ctx.fillStyle = hg;
      ctx.fillRect(-40, -40, 80, 80);
      strokeBatch(ctx, H.faceBarbs, C.barbDark, 0.6, 0.75);
      strokeBatch(ctx, sheen.face, C.barbLight, 0.45, 0.65);
      paintNape(ctx, H, sheen);
      ctx.restore();
      inkContour(ctx, hp.samples, dir, 1.6, H.contourNoise, hp.skip);
      strokeBatch(ctx, H.fringe, C.ink, 0.85, 1);
    }
    this.drawBeak(ctx, pose);
    this.drawEye(ctx, pose);
    this.drawCrest(ctx, pose);
    ctx.restore();
  }

  drawPortraitBlink(ctx, pose) {
    const lid = clamp(Math.max(pose.blink, pose.sleep, pose.lid * 0.45));
    if (lid <= 0.01) return;
    const r = 4.3;
    ctx.save();
    ctx.beginPath();
    ctx.ellipse(EYE.x, EYE.y, r, r * 1.08, 0, 0, TAU);
    ctx.clip();
    ctx.fillStyle = "#2b2e30";
    ctx.fillRect(EYE.x - r, EYE.y - r * 1.08, r * 2, r * 2.16 * lid);
    if (lid > 0.9) drawClosedEye(ctx, EYE.x, EYE.y, r);
    ctx.restore();
  }

  drawBeak(ctx, pose) {
    const open = pose.beakOpen;
    ctx.save();
    ctx.translate(GAPE.x, GAPE.y);
    ctx.rotate((open * 26 * Math.PI) / 180);
    ctx.translate(-GAPE.x, -GAPE.y);
    const { lower, upper } = this.shapes.beak;
    ctx.fillStyle = "#202226";
    ctx.fill(lower);
    if (this.kit?.lowerMandible) drawClippedPiece(ctx, this.kit.lowerMandible, lower);
    ctx.strokeStyle = C.ink;
    ctx.lineWidth = 1.3;
    ctx.stroke(lower);
    ctx.restore();
    if (open > 0.05) {
      // Mouth interior shows only when the beak parts.
      ctx.fillStyle = "#3a2b2d";
      ctx.beginPath();
      ctx.moveTo(20, 3);
      ctx.lineTo(44, 4.5);
      ctx.lineTo(22, 3 + open * 6);
      ctx.closePath();
      ctx.fill();
    }
    const g = ctx.createLinearGradient(20, -14, 28, 4);
    g.addColorStop(0, C.beakLight);
    g.addColorStop(0.4, C.beak);
    g.addColorStop(1, "#18191c");
    ctx.fillStyle = g;
    ctx.fill(upper);
    if (this.kit?.upperMandible) {
      // The painting carries its own ridge highlight and bristles.
      drawClippedPiece(ctx, this.kit.upperMandible, upper);
      ctx.strokeStyle = C.ink;
      ctx.lineWidth = 1.2;
      ctx.stroke(upper);
      return;
    }
    ctx.strokeStyle = C.ink;
    ctx.lineWidth = 1.5;
    ctx.stroke(upper);
    ctx.strokeStyle = "rgba(214,218,224,0.42)";
    ctx.lineWidth = 1.1;
    ctx.beginPath();
    ctx.moveTo(24, -13.8);
    ctx.bezierCurveTo(34, -13, 44, -8, 51, 0.6);
    ctx.stroke();
    // Nasal bristles: a corvid detail that also hides the nostril.
    ctx.strokeStyle = "rgba(10,11,13,0.95)";
    ctx.lineWidth = 0.9;
    ctx.lineCap = "round";
    for (let i = 0; i < 10; i += 1) {
      const y = -12.5 + i * 1.25;
      ctx.beginPath();
      ctx.moveTo(16 + (i % 2), y + 1);
      ctx.quadraticCurveTo(24, y - 1.4, 30 + (i % 3) * 1.2, y + 0.6);
      ctx.stroke();
    }
  }

  drawEye(ctx, pose) {
    const { x, y, r } = EYE;
    const lid = clamp(Math.max(pose.blink, pose.sleep, pose.lid * 0.45));
    if (lid > 0.9) {
      drawClosedEye(ctx, x, y, r);
      return;
    }
    ctx.save();
    ctx.fillStyle = "#08090b";
    ctx.beginPath();
    ctx.ellipse(x, y, r + 1.9, r + 1.6, 0, 0, TAU);
    ctx.fill();
    ctx.save();
    ctx.beginPath();
    ctx.ellipse(x, y, r, r * 0.97, 0, 0, TAU);
    ctx.clip();
    const g = ctx.createRadialGradient(x - 1.2, y - 1.6, 0.4, x, y, r);
    g.addColorStop(0, C.irisCenter);
    g.addColorStop(0.62, C.irisMid);
    g.addColorStop(1, C.irisEdge);
    ctx.fillStyle = g;
    ctx.fillRect(x - r, y - r, r * 2, r * 2);
    ctx.strokeStyle = "rgba(122,148,164,0.42)";
    ctx.lineWidth = 0.5;
    ctx.beginPath();
    for (let i = 0; i < 22; i += 1) {
      const a = (i / 22) * TAU;
      ctx.moveTo(x + Math.cos(a) * r * 0.42, y + Math.sin(a) * r * 0.42);
      ctx.lineTo(x + Math.cos(a) * r * 0.96, y + Math.sin(a) * r * 0.96);
    }
    ctx.stroke();
    // Upper shadow: the brow overhang keeps the pale eye from looking blank.
    ctx.fillStyle = "rgba(20,30,38,0.22)";
    ctx.fillRect(x - r, y - r, r * 2, r * 0.6);
    const px = x + pose.lookX * 2.7;
    const py = y + pose.lookY * 2.3;
    ctx.fillStyle = C.pupil;
    ctx.beginPath();
    ctx.arc(px, py, 2.8, 0, TAU);
    ctx.fill();
    ctx.fillStyle = "rgba(255,255,255,0.96)";
    ctx.beginPath();
    ctx.ellipse(px + 1.7, py - 2, 1.35, 1.05, -0.4, 0, TAU);
    ctx.fill();
    if (lid > 0.01) {
      const top = y - r - 1;
      const meet = lerp(top, y + r * 0.3, lid);
      ctx.fillStyle = "#17181c";
      ctx.beginPath();
      ctx.moveTo(x - r - 2, top - 2);
      ctx.lineTo(x + r + 2, top - 2);
      ctx.lineTo(x + r + 2, meet - 1);
      ctx.quadraticCurveTo(x, meet + 3.2 * lid, x - r - 2, meet + 1);
      ctx.closePath();
      ctx.fill();
      ctx.strokeStyle = "rgba(90,95,104,0.7)";
      ctx.lineWidth = 0.8;
      ctx.beginPath();
      ctx.moveTo(x - r, meet + 0.8);
      ctx.quadraticCurveTo(x, meet + 3.2 * lid, x + r, meet - 0.6);
      ctx.stroke();
      if (lid > 0.5) {
        const low = lerp(y + r + 1, y + r * 0.3, (lid - 0.5) * 2 * 0.85);
        ctx.fillStyle = "#17181c";
        ctx.fillRect(x - r - 2, low, r * 2 + 4, r + 4);
      }
    }
    ctx.restore();
    // Brow: a short feathered ridge, raised when curious, lowered when intent.
    ctx.strokeStyle = "rgba(5,5,6,0.85)";
    ctx.lineWidth = 1.25;
    ctx.lineCap = "round";
    ctx.beginPath();
    ctx.moveTo(x - 8, y - r - 1.2 - pose.brow * 1.2);
    ctx.quadraticCurveTo(x - 1, y - r - 3.6 - pose.brow * 2, x + 7.5, y - r - 0.4 + pose.brow * 0.6 - (pose.brow < 0 ? pose.brow * 2.2 : 0));
    ctx.stroke();
    ctx.restore();
  }

  drawCrest(ctx, pose) {
    ctx.save();
    ctx.translate(CROWN.x, CROWN.y);
    ctx.fillStyle = "#101114";
    if (this.kit?.feathers.tail.length) {
      // Painted crest: slender feathers on the same spring, each leaning the
      // way its ink stroke curls.
      this.plumage.head.crest.forEach((t, i) => {
        const a = t.angle + pose.crest * t.give + Math.atan2(t.bend, t.len) * 0.8;
        ctx.save();
        ctx.translate(t.x, t.y);
        ctx.rotate(a);
        this.drawPaintedFeather(ctx, "tail", i, t.len * CREST_REACH, t.width * CREST_WIDTH);
        ctx.restore();
      });
      ctx.restore();
      return;
    }
    for (const t of this.plumage.head.crest) {
      const a = t.angle + pose.crest * t.give;
      const tip = rotate({ x: t.len, y: 0 }, a);
      const ctrl = rotate({ x: t.len * 0.55, y: t.bend }, a);
      const pts = [];
      for (let i = 0; i <= 10; i += 1) {
        const k = i / 10;
        const u = 1 - k;
        pts.push({ x: t.x + 2 * u * k * ctrl.x + k * k * tip.x, y: t.y + 2 * u * k * ctrl.y + k * k * tip.y });
      }
      taperedStroke(ctx, pts, t.width, { taperStart: 0, taperEnd: 1 });
    }
    ctx.restore();
  }

  // Ambient tint and rim light, applied to the finished bird layer. The rim is
  // the band that leaves the silhouette when it is shifted toward the light,
  // inset by a pixel or two so the ink contour stays dark.
  applyLight(pose, lit, pixel) {
    const w = this.layer.width;
    const h = this.layer.height;
    const lc = this.layerCtx;
    const inset = Math.max(0.8, 0.9 * pixel);
    lc.save();
    lc.setTransform(1, 0, 0, 1, 0, 0);
    lc.globalCompositeOperation = "source-atop";
    if (lit.ambient && lit.ambientStrength > 0) {
      lc.globalAlpha = lit.ambientStrength;
      lc.fillStyle = lit.ambient;
      lc.fillRect(0, 0, w, h);
    }
    // Nested bands, narrow and bright to wide and faint, so the rim falls off
    // like light wrapping a soft edge instead of reading as an outline.
    for (const band of RIM_BANDS) {
      const width = Math.max(0.6, band.width * pixel * lit.rimWidth);
      this.rimMask(lit.dir, inset, width, lit.rim);
      lc.globalAlpha = lit.rimStrength * band.alpha;
      lc.drawImage(this.rimLayer, 0, 0);
    }
    lc.restore();
  }

  // The band of the bird's silhouette, `width` wide, on the side facing the
  // light, filled with the rim colour on the rim layer.
  rimMask(dir, inset, width, color) {
    const rc = this.rimCtx;
    rc.setTransform(1, 0, 0, 1, 0, 0);
    rc.globalCompositeOperation = "source-over";
    rc.clearRect(0, 0, this.rimLayer.width, this.rimLayer.height);
    rc.drawImage(this.layer, -dir.x * inset, -dir.y * inset);
    rc.globalCompositeOperation = "source-in";
    rc.fillStyle = color;
    rc.fillRect(0, 0, this.rimLayer.width, this.rimLayer.height);
    rc.globalCompositeOperation = "destination-out";
    rc.drawImage(this.layer, -dir.x * (inset + width), -dir.y * (inset + width));
    rc.globalCompositeOperation = "source-over";
  }
}

// A painted piece in the current frame: its fit carries pixels into the frame.
function drawPiece(ctx, piece) {
  const { a, b, c, d, e, f } = piece.m;
  ctx.save();
  ctx.transform(a, b, c, d, e, f);
  ctx.drawImage(piece.image, 0, 0);
  ctx.restore();
}

function drawClippedPiece(ctx, piece, outline) {
  ctx.save();
  ctx.clip(outline);
  drawPiece(ctx, piece);
  ctx.restore();
}

// Darkens everything already on a layer: used while the layer holds only the
// far wing.
function shadeLayer(ctx, alpha) {
  ctx.save();
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.globalCompositeOperation = "source-atop";
  ctx.fillStyle = `rgba(6,7,10,${alpha})`;
  ctx.fillRect(0, 0, ctx.canvas.width, ctx.canvas.height);
  ctx.restore();
}

// The underside of the body turning away from the light, in body space.
function bellyShadow(ctx, alpha) {
  const sg = ctx.createLinearGradient(0, 10, 0, 52);
  sg.addColorStop(0, "rgba(10,10,12,0)");
  sg.addColorStop(1, `rgba(10,10,12,${alpha})`);
  ctx.fillStyle = sg;
  ctx.fillRect(-70, 10, 140, 50);
}

function drawClosedEye(ctx, x, y, r) {
  ctx.save();
  ctx.fillStyle = "#1b1c20";
  ctx.beginPath();
  ctx.ellipse(x, y, r + 1.2, r * 0.8, 0, 0, TAU);
  ctx.fill();
  ctx.strokeStyle = "#060607";
  ctx.lineWidth = 1.5;
  ctx.lineCap = "round";
  ctx.beginPath();
  ctx.moveTo(x - r, y + 0.5);
  ctx.quadraticCurveTo(x, y + r * 0.55, x + r, y - 0.5);
  ctx.stroke();
  ctx.strokeStyle = "rgba(120,126,136,0.45)";
  ctx.lineWidth = 0.8;
  ctx.beginPath();
  ctx.moveTo(x - r * 0.8, y - 1.6);
  ctx.quadraticCurveTo(x, y + r * 0.3, x + r * 0.8, y - 2.2);
  ctx.stroke();
  ctx.restore();
}

function deg(rad) {
  return (rad * 180) / Math.PI;
}

// Catmull-Rom spline through closed points, sampled densely for contours.
function sampleClosed(points, tension = 0.5, steps = 10) {
  const n = points.length;
  const P = (i) => points[(i + n) % n];
  const out = [];
  for (let i = 0; i < n; i += 1) {
    const p0 = P(i - 1);
    const p1 = P(i);
    const p2 = P(i + 1);
    const p3 = P(i + 2);
    const c1 = { x: p1[0] + ((p2[0] - p0[0]) * tension) / 3, y: p1[1] + ((p2[1] - p0[1]) * tension) / 3 };
    const c2 = { x: p2[0] - ((p3[0] - p1[0]) * tension) / 3, y: p2[1] - ((p3[1] - p1[1]) * tension) / 3 };
    const seg = bezierPoints({ x: p1[0], y: p1[1] }, c1, c2, { x: p2[0], y: p2[1] }, steps);
    seg.pop();
    out.push(...seg);
  }
  return out;
}

function pathFrom(samples) {
  const p = new Path2D();
  p.moveTo(samples[0].x, samples[0].y);
  for (const s of samples) p.lineTo(s.x, s.y);
  p.closePath();
  return p;
}

function buildShapes() {
  const bodySamples = sampleClosed(BODY_OUTLINE, 0.55, 12);
  const headSamples = sampleClosed(HEAD_OUTLINE, 0.55, 10);
  // Mandibles in head space; the lower one hinges at the gape.
  const lower = new Path2D();
  lower.moveTo(19, 3);
  lower.bezierCurveTo(32, 4, 44, 5, 51, 6.4);
  lower.bezierCurveTo(46, 10, 33, 11.6, 20, 11);
  lower.closePath();
  const upper = new Path2D();
  upper.moveTo(18, -15.5);
  upper.bezierCurveTo(32, -16, 47, -9, 56, 4);
  upper.bezierCurveTo(52, 5.8, 46, 4.6, 40, 4.4);
  upper.lineTo(19, 3.6);
  upper.closePath();
  // A thigh tuft hanging from its attachment at the origin.
  const tuft = new Path2D();
  tuft.moveTo(-10, 0);
  tuft.bezierCurveTo(-12, 10, -7, 22, -2, 27);
  tuft.bezierCurveTo(1, 24, 3, 27, 6, 24);
  tuft.bezierCurveTo(10, 16, 12, 8, 10, 0);
  tuft.closePath();
  return {
    beak: { upper, lower },
    tuft,
    body: {
      samples: bodySamples,
      path: pathFrom(bodySamples),
      // Leave the contour open where the neck joins the chest and mantle.
      skip: (p) => p.y < -36 && p.x > -14 && p.x < 46,
    },
    head: {
      samples: headSamples,
      path: pathFrom(headSamples),
      // The back and underside of the head melt into the neck feathers.
      skip: (p) => (p.x < -20 && p.y > -8) || (p.y > 10 && p.x < 22),
    },
  };
}

// Variable-width ink contour: heavier on the shadow side, with small breaks
// and swells so it reads as a dip pen rather than a vector stroke.
function inkContour(ctx, samples, dir, weight, wobble, skip) {
  ctx.fillStyle = C.ink;
  let run = [];
  const flush = () => {
    if (run.length > 2) taperedStroke(ctx, run, weight * run.width, { taperStart: 1, taperEnd: 1 });
    run = [];
  };
  const n = samples.length;
  for (let i = 0; i <= n; i += 1) {
    const p = samples[i % n];
    const q = samples[(i + 1) % n];
    const nx = q.y - p.y;
    const ny = -(q.x - p.x);
    const len = Math.hypot(nx, ny) || 1;
    // Outward normal; away from the light means heavier ink.
    const away = clamp(0.5 - ((nx / len) * dir.x + (ny / len) * dir.y) * 0.5);
    const gap = wobble(i * 0.35) > 0.72;
    if (skip(p) || gap) {
      flush();
      continue;
    }
    run.push(p);
    run.width = Math.max(run.width || 0, 0.55 + away * 0.95 + wobble(i * 0.9) * 0.25);
    if (run.length > 14) {
      flush();
      run.push(p);
    }
  }
  flush();
}

function strokeBatch(ctx, path, color, alpha, width) {
  if (!path) return;
  ctx.strokeStyle = rgba(color, alpha);
  ctx.lineWidth = width;
  ctx.lineCap = "round";
  ctx.stroke(path);
}

function paintWashes(ctx, washes) {
  for (const w of washes) {
    ctx.fillStyle = w.color;
    ctx.beginPath();
    ctx.ellipse(w.x, w.y, w.rx, w.ry, w.a, 0, TAU);
    ctx.fill();
  }
}

// The gray nape is built from overlapping feather dabs whose density thins
// toward its edge, so it frays into the black cap and back instead of ending
// on a line.
function paintNape(ctx, H, sheen) {
  ctx.fillStyle = "rgba(112,117,125,0.8)";
  ctx.fill(H.napeDabs.under);
  ctx.fillStyle = C.napeDark;
  ctx.fill(H.napeDabs.dark);
  ctx.fillStyle = C.nape;
  ctx.fill(H.napeDabs.mid);
  ctx.fillStyle = rgba(C.napeLight, 0.9);
  ctx.fill(H.napeDabs.light);
  strokeBatch(ctx, H.napeStreaks, "#3f444c", 0.55, 0.7);
  strokeBatch(ctx, sheen.nape, "#f0f1f3", 0.4, 0.55);
}

function addDab(path, x, y, a, len, width) {
  const ux = Math.cos(a);
  const uy = Math.sin(a);
  const vx = -uy * width;
  const vy = ux * width;
  path.moveTo(x - vx, y - vy);
  path.quadraticCurveTo(x + ux * len * 0.6 - vx * 1.1, y + uy * len * 0.6 - vy * 1.1, x + ux * len, y + uy * len);
  path.quadraticCurveTo(x + ux * len * 0.6 + vx * 1.1, y + uy * len * 0.6 + vy * 1.1, x + vx, y + vy);
  path.closePath();
}

// Flight feather drawn along +x from its base, in its own rotated frame. Its
// paths are built once (see shapeFlightFeather); a wing feather that changes
// length between folded and spread is scaled uniformly around its base.
function drawFlightFeather(ctx, f, sheen, darken) {
  const k = f.len / f.baseLen;
  ctx.save();
  ctx.scale(k, k);
  ctx.fillStyle = darken ? "#121316" : f.fill;
  ctx.fill(f.blade);
  ctx.lineCap = "round";
  ctx.strokeStyle = rgba("#0b0c0e", darken ? 0.3 : 0.55);
  ctx.lineWidth = 1;
  ctx.stroke(f.barbs);
  // The paler outer edge gives a folded wing its layers.
  ctx.strokeStyle = rgba(C.featherEdge, darken ? 0.18 : 0.5 + sheen * 0.3);
  ctx.lineWidth = 1.6;
  ctx.stroke(f.edge);
  ctx.strokeStyle = C.ink;
  ctx.lineWidth = 1.15;
  ctx.stroke(f.blade);
  ctx.strokeStyle = darken ? "rgba(70,74,82,0.15)" : "rgba(130,136,146,0.38)";
  ctx.lineWidth = 0.7;
  ctx.stroke(f.shaft);
  ctx.restore();
}

function drawCovert(ctx, c, darken) {
  ctx.fillStyle = darken ? "#131417" : c.fill;
  ctx.fill(c.shape);
  ctx.strokeStyle = rgba(C.featherEdge, darken ? 0.15 : c.edgeAlpha);
  ctx.lineWidth = 1.1;
  ctx.stroke(c.edge);
  ctx.strokeStyle = rgba(C.ink, 0.85);
  ctx.lineWidth = 1;
  ctx.stroke(c.rim);
}

function lerpAngle(a, b, k) {
  let d = b - a;
  while (d > Math.PI) d -= TAU;
  while (d < -Math.PI) d += TAU;
  return a + d * k;
}

function drawToes(ctx, foot, perchAngle, grip, tone, isNear) {
  ctx.save();
  ctx.translate(foot.x, foot.y);
  ctx.rotate(perchAngle);
  const toes = [
    { angle: -0.04, len: 23, front: true },
    { angle: 0.2, len: 17, front: true },
    { angle: -0.3, len: 16, front: true },
    { angle: Math.PI + 0.06, len: 16, front: false },
  ];
  for (const t of toes) {
    const curl = grip * (t.front ? 1.15 : -1.05);
    const pts = [{ x: 0, y: 0 }];
    let a = t.angle;
    let px = 0;
    let py = 0;
    for (let i = 0; i < 3; i += 1) {
      a += curl * 0.52;
      px += Math.cos(a) * (t.len / 3);
      py += Math.sin(a) * (t.len / 3) * (grip > 0.2 ? 1 : 0.45);
      pts.push({ x: px, y: py });
    }
    ctx.fillStyle = tone;
    taperedStroke(ctx, pts, 3.9, { taperStart: 0, taperEnd: 1 });
    const tip = pts[pts.length - 1];
    const hook = a + (t.front ? 0.9 : -0.9) + curl * 0.4;
    ctx.fillStyle = C.claw;
    taperedStroke(
      ctx,
      bezierPoints(
        tip,
        { x: tip.x + Math.cos(a) * 3, y: tip.y + Math.sin(a) * 3 },
        { x: tip.x + Math.cos(hook) * 5, y: tip.y + Math.sin(hook) * 5 },
        { x: tip.x + Math.cos(hook) * 6.5 + Math.cos(hook + 0.6) * 1.5, y: tip.y + Math.sin(hook) * 6.5 + 2.5 },
        6,
      ),
      2.8,
      { taperStart: 0, taperEnd: 1 },
    );
    if (isNear) {
      ctx.strokeStyle = rgba(C.legLight, 0.55);
      ctx.lineWidth = 0.7;
      ctx.beginPath();
      for (let i = 1; i < pts.length - 1; i += 1) {
        ctx.moveTo(pts[i].x - 1.4, pts[i].y - 0.6);
        ctx.arc(pts[i].x, pts[i].y - 0.6, 1.4, Math.PI, TAU);
      }
      ctx.stroke();
    }
  }
  ctx.restore();
}

// ---------------------------------------------------------------- plumage

function buildPlumage(seed, shapes, probe) {
  const r = rng(seed);
  const inBody = (x, y) => probe.isPointInPath(shapes.body.path, x, y);
  const inHead = (x, y) => probe.isPointInPath(shapes.head.path, x, y);

  // Body flow: tracts run away from where the neck meets the body.
  const bodyFlow = (x, y) => Math.atan2(y - NECK_SOCKET.y + 26, x - NECK_SOCKET.x + 6);
  const body = {
    washes: [],
    barbsDark: new Path2D(),
    barbsMid: new Path2D(),
    scallops: new Path2D(),
    hatch: new Path2D(),
    fringe: new Path2D(),
    contourNoise: noise1(seed + 1),
    sheenSeeds: [],
    mantle: { under: new Path2D(), dark: new Path2D(), mid: new Path2D() },
  };
  // Gray of the nape running down onto the mantle, thinning as it goes.
  for (let y = -60; y <= -14; y += 2.6) {
    for (let x = -40; x <= 26; x += 2.8) {
      const jx = x + r.signed(1.3);
      const jy = y + r.signed(1.2);
      if (!inBody(jx, jy)) continue;
      const fromNape = Math.hypot((jx - 4) * 0.8, jy + 58);
      const keep = clamp(1 - fromNape / 34) * clamp((14 - jx) / 14);
      if (r.next() > keep) continue;
      const a = Math.atan2(jy + 70, jx - 30) + r.signed(0.3);
      addDab(r.next() < 0.6 ? body.mantle.dark : body.mantle.mid, jx, jy, a, r.range(3, 5), r.range(1.2, 2));
      if (keep > 0.5) addDab(body.mantle.under, jx, jy, a, 6, 2.6);
    }
  }
  for (let i = 0; i < 26; i += 1) {
    const x = r.range(-50, 46);
    const y = r.range(-44, 46);
    const light = r.next() < 0.45;
    body.washes.push({
      x, y,
      rx: r.range(8, 22),
      ry: r.range(5, 14),
      a: r.range(0, Math.PI),
      color: light ? "rgba(98,104,116,0.10)" : "rgba(8,8,10,0.16)",
    });
  }
  // Fine barbs everywhere; their density is what reads as soft plumage.
  for (let i = 0; i < 900; i += 1) {
    const x = r.range(-58, 56);
    const y = r.range(-56, 52);
    if (!inBody(x, y)) continue;
    const a = bodyFlow(x, y) + r.signed(0.35);
    const len = r.range(3, 7.5);
    const target = r.next() < 0.55 ? body.barbsDark : body.barbsMid;
    addCurl(target, x, y, a, len, r.signed(1.2));
    body.sheenSeeds.push({ x, y, a, len });
  }
  // Feather tips: broken scallops in loose rows, smaller toward the neck.
  for (let y = -50; y <= 48; y += 6.5) {
    for (let x = -56; x <= 56; x += 7.5) {
      const jx = x + r.signed(2.6) + ((Math.round(y / 6.5) & 1) * 3.6);
      const jy = y + r.signed(2.2);
      if (!inBody(jx, jy) || r.next() < 0.35) continue;
      const a = bodyFlow(jx, jy) + r.signed(0.2);
      const dist = Math.hypot(jx - NECK_SOCKET.x, jy - NECK_SOCKET.y);
      const size = clamp(3.4 + dist * 0.04, 3.4, 7);
      addScallop(body.scallops, jx, jy, a, size, r.range(0.55, 0.95));
    }
  }
  for (let i = 0; i < 40; i += 1) {
    const x = r.range(-46, 30);
    const y = r.range(20, 48);
    if (!inBody(x, y)) continue;
    const a = 2.25 + r.signed(0.1);
    const len = r.range(4, 9);
    body.hatch.moveTo(x, y);
    body.hatch.lineTo(x + Math.cos(a) * len, y + Math.sin(a) * len);
  }
  // Fluff that breaks the silhouette at the belly, vent and rump.
  for (let i = 0; i < shapes.body.samples.length; i += 3) {
    const p = shapes.body.samples[i];
    if (p.y < -16 || r.next() < 0.45) continue;
    const a = Math.atan2(p.y - 6, p.x + 6) + r.signed(0.5);
    addCurl(body.fringe, p.x, p.y, a, r.range(2.5, 5.5), r.signed(1));
  }

  const head = {
    faceBarbs: new Path2D(),
    napeStreaks: new Path2D(),
    fringe: new Path2D(),
    contourNoise: noise1(seed + 2),
    faceSeeds: [],
    napeSeeds: [],
    crest: [
      { x: -5, y: 2, angle: -2.5, len: 11, bend: -2.5, width: 3.2, give: 0.7 },
      { x: -2, y: 1, angle: -2.05, len: 17, bend: -3.5, width: 3.6, give: 1 },
      { x: 1, y: 1, angle: -1.75, len: 21, bend: 3.8, width: 3.8, give: 1.35 },
      { x: 3, y: 2, angle: -1.45, len: 15, bend: 4.5, width: 3.2, give: 1.2 },
      { x: 5, y: 3, angle: -1.2, len: 10, bend: 3, width: 2.6, give: 1.5 },
      { x: -1, y: 2, angle: -2.25, len: 8, bend: -1.5, width: 2.4, give: 0.9 },
    ],
  };
  const faceFlow = (x, y) => Math.atan2(y + 2, x + 6) + Math.PI + 0.25;
  for (let i = 0; i < 420; i += 1) {
    const x = r.range(-32, 26);
    const y = r.range(-34, 30);
    if (!inHead(x, y)) continue;
    const a = faceFlow(x, y) + r.signed(0.3);
    const len = r.range(2.2, 4.6);
    addCurl(head.faceBarbs, x, y, a, len, r.signed(0.8));
    head.faceSeeds.push({ x, y, a, len });
  }
  // Nape region: behind a ragged slant from the crown, past the eye, down to
  // the throat; it continues below the skull to colour the back of the neck.
  const napeLimit = (y) => {
    if (y < -24) return -27 + (y + 24) * 0.4;
    if (y < -6) return -22 + (y + 24) * 1.1;
    if (y < 20) return -2.2 - (y + 6) * 0.38;
    return -12 - (y - 20) * 0.22;
  };
  const nape = new Path2D();
  nape.moveTo(-80, -40);
  for (let y = -40; y <= 84; y += 3) nape.lineTo(napeLimit(y) + r.signed(2.2), y);
  nape.lineTo(-80, 90);
  nape.closePath();
  head.napeRegion = nape;
  head.napeDabs = { under: new Path2D(), dark: new Path2D(), mid: new Path2D(), light: new Path2D() };
  for (let y = -34; y <= 84; y += 2.6) {
    for (let x = -64; x <= 8; x += 2.8) {
      const jx = x + r.signed(1.3);
      const jy = y + r.signed(1.2);
      const depth = napeLimit(jy) - jx;
      const fadeIn = clamp((depth + 1.5) / 7);
      const fadeDown = clamp(1 - (jy - 34) / 46);
      if (r.next() > fadeIn * fadeDown) continue;
      const a = Math.PI * 0.55 + r.signed(0.35) + (jy > 18 ? 0.2 : 0);
      const len = r.range(2.8, 4.8);
      const width = r.range(1.1, 1.9);
      const height = clamp((jy + 30) / 100);
      const roll = r.next() + height * 0.8 - 0.1;
      const target = roll > 0.95 ? head.napeDabs.dark : roll > 0.32 ? head.napeDabs.mid : head.napeDabs.light;
      addDab(target, jx, jy, a, len, width);
      if (depth > 1.5) addDab(head.napeDabs.under, jx, jy, a, len * 1.6, width * 1.8);
    }
  }
  head.neckBarbs = new Path2D();
  for (let i = 0; i < 260; i += 1) {
    const x = r.range(-40, 30);
    const y = r.range(0, 74);
    addCurl(head.neckBarbs, x, y, Math.PI * 0.52 + r.signed(0.35), r.range(2.5, 5.5), r.signed(0.8));
  }
  for (let i = 0; i < 700; i += 1) {
    const x = r.range(-62, 6);
    const y = r.range(-36, 80);
    const limit = napeLimit(y);
    if (x > limit) continue;
    const a = Math.PI * 0.56 + r.signed(0.3) + (y > 20 ? 0.25 : 0);
    const len = r.range(2.2, 5);
    if (r.next() < 0.55) addCurl(head.napeStreaks, x, y, a, len, r.signed(0.6));
    head.napeSeeds.push({ x, y, a, len });
  }
  for (let i = 0; i < shapes.head.samples.length; i += 2) {
    const p = shapes.head.samples[i];
    if (p.y > -6 || p.x > 14 || r.next() < 0.4) continue;
    const a = Math.atan2(p.y, p.x) + r.signed(0.5);
    addCurl(head.fringe, p.x, p.y, a, r.range(1.5, 3.2), r.signed(0.8));
  }

  const tail = [];
  for (let i = 0; i < 7; i += 1) {
    const slot = i - 3;
    tail.push(makeFlightFeather(r, { len: 72 - Math.abs(slot) * 3.5, width: 13, slot }));
  }
  tail.sort((a, b) => Math.abs(b.slot) - Math.abs(a.slot));

  const wing = buildWing(r);
  const sheenCache = new Map();
  return {
    body,
    head,
    tail,
    wing,
    // Highlights depend on which side faces the light, so they are baked once
    // per light direction and reused.
    sheenFor(dir) {
      const key = `${dir.x.toFixed(2)},${dir.y.toFixed(2)}`;
      if (!sheenCache.has(key)) sheenCache.set(key, bakeSheen(dir, body, head, r));
      return sheenCache.get(key);
    },
  };
}

function bakeSheen(dir, body, head, r) {
  const facing = (x, y, cx, cy, rad) => ((x - cx) * dir.x + (y - cy) * dir.y) / rad;
  const out = { body: new Path2D(), bodyEdges: new Path2D(), face: new Path2D(), nape: new Path2D(), flight: 0.4 };
  for (const s of body.sheenSeeds) {
    const f = facing(s.x, s.y, 0, 6, 56);
    if (f < 0.15 || (s.x * 7 + s.y * 13) % 5 > 2.4) continue;
    addCurl(out.body, s.x, s.y, s.a, s.len * 0.9, 0.4);
    if (f > 0.5 && (s.x * 3 + s.y) % 4 < 1) addScallop(out.bodyEdges, s.x, s.y, s.a, 4.2, 0.7);
  }
  for (const s of head.faceSeeds) {
    if (facing(s.x, s.y, -2, 2, 30) < 0.2) continue;
    addCurl(out.face, s.x, s.y, s.a, s.len, 0.3);
  }
  for (const s of head.napeSeeds) {
    if (facing(s.x, s.y, -10, 10, 40) < -0.2 || (s.x * 5 + s.y * 3) % 3 > 1.6) continue;
    addCurl(out.nape, s.x, s.y, s.a, s.len, 0.3);
  }
  out.flight = clamp(0.4 + dir.y * -0.3);
  return out;
}

function addCurl(path, x, y, a, len, bend) {
  const ex = x + Math.cos(a) * len;
  const ey = y + Math.sin(a) * len;
  const mx = (x + ex) / 2 - Math.sin(a) * bend;
  const my = (y + ey) / 2 + Math.cos(a) * bend;
  path.moveTo(x, y);
  path.quadraticCurveTo(mx, my, ex, ey);
}

// A feather tip: a shallow arc across the flow direction.
function addScallop(path, x, y, a, size, sweep) {
  const ux = Math.cos(a);
  const uy = Math.sin(a);
  const vx = -uy;
  const vy = ux;
  const w = size * sweep;
  path.moveTo(x - vx * w, y - vy * w);
  path.quadraticCurveTo(x + ux * size * 0.9, y + uy * size * 0.9, x + vx * w, y + vy * w);
}

function makeFlightFeather(r, { len, width, slot = 0 }) {
  const tone = 34 + Math.round(r.signed(5));
  const f = { len, width, slot, fill: `rgb(${tone},${tone + 2},${tone + 7})` };
  if (len > 0) shapeFlightFeather(f, len, r);
  return f;
}

// Build a flight feather's blade, vane barbs, pale edge and shaft at a base
// length. The barbs stay inside the blade, so drawing needs no clip.
// A flight feather's outline in its own frame: base at the origin, tip at
// +x = length, vane edges at roughly +-width/2.
export function featherBlade(L, width) {
  const W = width / 2;
  const blade = new Path2D();
  blade.moveTo(0, -W * 0.4);
  blade.bezierCurveTo(L * 0.3, -W * 1.05, L * 0.78, -W * 1.0, L * 0.97, -W * 0.3);
  blade.quadraticCurveTo(L + W * 0.35, W * 0.15, L * 0.95, W * 0.55);
  blade.bezierCurveTo(L * 0.75, W * 1.0, L * 0.3, W * 0.9, 0, W * 0.4);
  blade.closePath();
  return blade;
}

// A covert's outline in its own frame: base just behind the origin, tip at +x.
export function covertOutline(L, width) {
  const W = width / 2;
  const shape = new Path2D();
  shape.moveTo(-L * 0.1, -W * 0.8);
  shape.bezierCurveTo(L * 0.5, -W * 1.15, L, -W * 0.7, L, 0);
  shape.bezierCurveTo(L, W * 0.7, L * 0.5, W * 1.15, -L * 0.1, W * 0.8);
  shape.quadraticCurveTo(-L * 0.35, 0, -L * 0.1, -W * 0.8);
  return shape;
}

function shapeFlightFeather(f, L, r) {
  const W = f.width / 2;
  const blade = featherBlade(L, f.width);
  const barbs = new Path2D();
  for (let i = 0; i < 26; i += 1) {
    const x = r.range(0.06, 0.82) * L;
    const side = r.next() < 0.6 ? -1 : 1;
    const taper = Math.min(1, (L - x) / (0.3 * L));
    barbs.moveTo(x, side * W * 0.08);
    barbs.lineTo(x + r.range(0.05, 0.1) * L, side * W * r.range(0.5, 0.78) * taper);
  }
  const edge = new Path2D();
  edge.moveTo(L * 0.1, -W * 0.68);
  edge.bezierCurveTo(L * 0.4, -W * 0.96, L * 0.8, -W * 0.9, L * 0.96, -W * 0.25);
  const shaft = new Path2D();
  shaft.moveTo(2, 0);
  shaft.quadraticCurveTo(L * 0.5, -W * 0.12, L * 0.9, -W * 0.05);
  Object.assign(f, { baseLen: L, blade, barbs, edge, shaft });
}

function shapeCovert(c) {
  const L = c.len;
  const W = c.width / 2;
  c.shape = covertOutline(L, c.width);
  c.edge = new Path2D();
  c.edge.moveTo(L * 0.35, -W * 0.85);
  c.edge.bezierCurveTo(L * 0.85, -W * 0.65, L * 0.95, W * 0.5, L * 0.45, W * 0.85);
  c.rim = new Path2D();
  c.rim.moveTo(L * 0.5, -W * 0.98);
  c.rim.bezierCurveTo(L * 1.03, -W * 0.65, L * 1.05, W * 0.55, L * 0.55, W * 0.98);
  return c;
}

function buildWing(r) {
  const flight = [];
  // Primaries: longest, from the hand; drawn first so they sit underneath.
  for (let i = 0; i < 7; i += 1) {
    const k = i / 6;
    flight.push({
      ...makeFlightFeather(r, { len: 0, width: 13 }),
      kind: "primary",
      folded: { x: lerp(-26, -38, k), y: lerp(12, 20, k), angle: Math.PI - 0.66 + k * 0.05, len: lerp(80, 68, k) },
      spread: { x: lerp(20, 13, k), y: lerp(-80, -56, k), angle: lerp(-1.62, -2.35, k), len: lerp(84, 74, k) },
    });
  }
  // Secondaries along the forearm, then three broad tertials on top.
  for (let i = 0; i < 8; i += 1) {
    const k = i / 7;
    flight.push({
      ...makeFlightFeather(r, { len: 0, width: 15 }),
      kind: "secondary",
      folded: { x: lerp(-34, -4, k), y: lerp(16, 8, k), angle: Math.PI - 0.72 - k * 0.08, len: lerp(56, 46, k) },
      spread: { x: lerp(12, 2, k), y: lerp(-52, -8, k), angle: lerp(-2.45, Math.PI - 0.15, k), len: lerp(60, 48, k) },
    });
  }
  for (let i = 0; i < 3; i += 1) {
    const k = i / 2;
    flight.push({
      ...makeFlightFeather(r, { len: 0, width: 18 }),
      kind: "tertial",
      folded: { x: lerp(-14, 2, k), y: lerp(4, -2, k), angle: Math.PI - 0.78, len: lerp(50, 42, k) },
      spread: { x: lerp(0, -2, k), y: lerp(-8, 0, k), angle: Math.PI - 0.1, len: lerp(46, 40, k) },
    });
  }
  // Coverts in three rows; greater first, lesser (at the shoulder) last.
  const coverts = [];
  const rows = [
    { count: 7, len: 15, width: 13, from: { x: 6, y: 2 }, to: { x: -32, y: 14 }, sFrom: { x: 6, y: -8 }, sTo: { x: 16, y: -52 } },
    { count: 7, len: 11, width: 10, from: { x: 10, y: -6 }, to: { x: -22, y: 4 }, sFrom: { x: 9, y: -10 }, sTo: { x: 18, y: -46 } },
    { count: 8, len: 8, width: 8, from: { x: 12, y: -14 }, to: { x: -10, y: -6 }, sFrom: { x: 12, y: -12 }, sTo: { x: 20, y: -40 } },
  ];
  rows.forEach((row, rowIndex) => {
    for (let i = 0; i < row.count; i += 1) {
      const k = i / (row.count - 1);
      const tone = 40 + Math.round(r.signed(6));
      coverts.push(shapeCovert({
        // The two outer rows are greater and median coverts; the shoulder row is lesser.
        kind: rowIndex < 2 ? "greater" : "lesser",
        len: row.len * r.range(0.9, 1.1),
        width: row.width * r.range(0.9, 1.1),
        fill: `rgb(${tone},${tone + 2},${tone + 8})`,
        edgeAlpha: r.range(0.3, 0.5),
        folded: { x: lerp(row.from.x, row.to.x, k) + r.signed(1.2), y: lerp(row.from.y, row.to.y, k) + r.signed(1), angle: Math.PI - 0.62 + r.signed(0.12) },
        spread: { x: lerp(row.sFrom.x, row.sTo.x, k) - 6, y: lerp(row.sFrom.y, row.sTo.y, k), angle: Math.PI - 0.35 },
      }));
    }
  });
  for (const f of flight) shapeFlightFeather(f, (f.folded.len + f.spread.len) / 2, r);
  return { flight, coverts };
}
