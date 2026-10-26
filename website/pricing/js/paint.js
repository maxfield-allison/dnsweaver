// Painted art: the shared manifest, decoded once, plus how to place it.
//
// art/manifest.json belongs to the art lead. It lists every painted file with
// its source rectangle and, for environment plates, a world rectangle and a
// depth. paint-fit.json belongs to the motion side. It holds what placement
// needs beyond the painting: a fitted transform for each Rivet piece, and a
// replacement world rectangle where a plate's painted ground line has to meet
// the scene's. Required art must decode before a scene goes live; the page
// retains its finished poster if preparation fails.

const MANIFEST_URL = new URL("../art/manifest.json", import.meta.url);
const FIT_URL = new URL("./paint-fit.json", import.meta.url);

// Loads the manifest and decodes the assets `wanted(asset)` accepts. Resolves
// to a complete art set. A failed required asset rejects preparation.
export async function loadArt(wanted = () => true) {
  const [manifest, fit] = await Promise.all([fetchJson(MANIFEST_URL), fetchJson(FIT_URL)]);
  const rows = manifest.assets.filter(wanted);
  const decoded = await Promise.all(rows.map((row) => decode(row, fit[row.id] || {})));
  return artSet(decoded.filter(Boolean));
}

async function fetchJson(url) {
  const response = await fetch(url);
  if (!response.ok) throw new Error(`${url.pathname}: HTTP ${response.status}`);
  return response.json();
}

async function decode(row, fit) {
  try {
    const response = await fetch(new URL(row.file, MANIFEST_URL));
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const blob = await response.blob();
    const s = row.source || { x: 0, y: 0, width: row.width, height: row.height };
    const image = await createImageBitmap(blob, s.x, s.y, s.width, s.height);
    return { ...row, ...fit, world: fit.world || row.world, image };
  } catch (error) {
    throw new Error(`Painted asset ${row.id} failed to load.`, { cause: error });
  }
}

function artSet(assets) {
  const byId = new Map(assets.map((a) => [a.id, a]));
  return {
    get: (id) => byId.get(id) || null,
    byPiece: (piece) => assets.filter((a) => a.piece === piece).sort((a, b) => (a.variant || 0) - (b.variant || 0)),
    ids: () => assets.map((a) => a.id),
    size: assets.length,
  };
}

// The feather paintings came out a lighter slate than the body painting, and
// in a folded wing their pale tips line up into stripes. Multiplying them once
// by this colour brings them to the body's value.
const FEATHER_TONE = "#a9abb3";

function toned(image, color) {
  const c = document.createElement("canvas");
  c.width = image.width;
  c.height = image.height;
  const ctx = c.getContext("2d");
  ctx.drawImage(image, 0, 0);
  ctx.globalCompositeOperation = "multiply";
  ctx.fillStyle = color;
  ctx.fillRect(0, 0, c.width, c.height);
  // Multiply fills the transparent margin too; keep only the painting's alpha.
  ctx.globalCompositeOperation = "destination-in";
  ctx.drawImage(image, 0, 0);
  return c;
}

// Rivet's paint kit from an art set: each fitted piece as an image plus the
// matrix that carries its pixels into the piece's frame (see tools/lab/fit.js).
// Pieces without a fit are left out, so the rig draws those procedurally.
export function rivetKit(art) {
  const piece = (name, key) => {
    const asset = art.byPiece(name)[0];
    return asset?.[key] ? { image: asset.image, m: new DOMMatrix([asset[key].a, asset[key].b, asset[key].c, asset[key].d, asset[key].e, asset[key].f]) } : null;
  };
  const feathers = (name) => art.byPiece(name)
    .filter((a) => a.feather)
    .map((a) => ({ image: toned(a.image, FEATHER_TONE), m: new DOMMatrix([a.feather.a, a.feather.b, a.feather.c, a.feather.d, a.feather.e, a.feather.f]) }));
  const kit = {
    body: piece("rivet-body", "rig"),
    head: piece("rivet-head", "rig"),
    headOpen: piece("rivet-head-open", "rig"),
    originalDesign: art.byPiece("rivet-head")[0]?.design === "original-painted",
    upperMandible: piece("rivet-upper-mandible", "rig"),
    lowerMandible: piece("rivet-lower-mandible", "rig"),
    tuft: piece("rivet-thigh-tuft", "rig"),
    feathers: {
      primary: feathers("rivet-primary"),
      secondary: feathers("rivet-secondary"),
      tertial: feathers("rivet-tertial"),
      tail: feathers("rivet-tail"),
      greater: feathers("rivet-greater-covert"),
      lesser: feathers("rivet-lesser-covert"),
    },
  };
  // Tertials borrow the broad secondaries until they have paintings of their own.
  if (!kit.feathers.tertial.length) kit.feathers.tertial = kit.feathers.secondary;
  return kit;
}

// Camera matrix for a plate at parallax depth `depth`: 1 moves with the
// action plane, 0 stays fixed to the screen. A plate's world rectangle is
// where it sits when the camera is at `ref`; as the camera pans and zooms
// away from that, a deeper layer follows only part of the way, which also
// keeps a soft far painting from being magnified as much as the action.
export function layerMatrix(cam, ref, depth, dpr) {
  const zoom = ref.zoom * Math.pow(cam.zoom / ref.zoom, depth);
  const fx = ref.fx + (cam.fx - ref.fx) * depth;
  const fy = ref.fy + (cam.fy - ref.fy) * depth;
  return new DOMMatrix()
    .scale(dpr, dpr)
    .translate(cam.w / 2 + cam.shakeX * depth, cam.h / 2 + cam.shakeY * depth)
    .scale(cam.s * zoom)
    .translate(-fx, -fy);
}

// A far plate must always fill the canvas: when parallax or a pulled-back
// camera would bring one of its edges inside the frame, shift it (and scale
// it about the canvas centre if it is too small) just enough to cover, with
// `bleed` device pixels to spare. Only backgrounds get this; anything with a
// fixed relation to the action keeps its exact matrix.
export function coverMatrix(plate, matrix, width, height, bleed = 2) {
  const { x, y, width: w, height: h } = plate.world;
  const a = matrix.transformPoint(new DOMPoint(x, y));
  const b = matrix.transformPoint(new DOMPoint(x + w, y + h));
  const rect = { x0: Math.min(a.x, b.x), y0: Math.min(a.y, b.y), x1: Math.max(a.x, b.x), y1: Math.max(a.y, b.y) };
  const need = Math.max((width + bleed * 2) / (rect.x1 - rect.x0), (height + bleed * 2) / (rect.y1 - rect.y0), 1);
  let m = matrix;
  if (need > 1) {
    m = new DOMMatrix().translate(width / 2, height / 2).scale(need).translate(-width / 2, -height / 2).multiply(m);
    const cx = width / 2;
    const cy = height / 2;
    rect.x0 = cx + (rect.x0 - cx) * need;
    rect.x1 = cx + (rect.x1 - cx) * need;
    rect.y0 = cy + (rect.y0 - cy) * need;
    rect.y1 = cy + (rect.y1 - cy) * need;
  }
  const dx = rect.x0 > -bleed ? -bleed - rect.x0 : rect.x1 < width + bleed ? width + bleed - rect.x1 : 0;
  const dy = rect.y0 > -bleed ? -bleed - rect.y0 : rect.y1 < height + bleed ? height + bleed - rect.y1 : 0;
  return dx || dy ? new DOMMatrix().translate(dx, dy).multiply(m) : m;
}

export function drawPlate(ctx, plate, matrix) {
  const { x, y, width, height } = plate.world;
  ctx.save();
  ctx.setTransform(matrix);
  ctx.imageSmoothingQuality = "low";
  ctx.drawImage(plate.image, x, y, width, height);
  ctx.restore();
}
