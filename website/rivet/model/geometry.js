import * as T from '../vendor/three-0.186.1/three.module.js';

export const clamp = (x, a = 0, b = 1) => Math.max(a, Math.min(b, x));
export const mix = (a, b, t) => a + (b - a) * t;
export const smooth = x => { const t = clamp(x); return t * t * (3 - 2 * t); };
export const V = (x = 0, y = 0, z = 0) => new T.Vector3(x, y, z);

export class SkinBuilder {
  constructor() { this.p = []; this.uv = []; this.c = []; this.ids = []; this.weights = []; this.indices = []; }
  vertex(p, uv, color, influences) {
    const id = this.p.length / 3;
    this.p.push(p.x, p.y, p.z); this.uv.push(...uv); this.c.push(color.r, color.g, color.b);
    const slots = influences.slice(0, 4);
    while (slots.length < 4) slots.push([0, 0]);
    this.ids.push(...slots.map(s => s[0])); this.weights.push(...slots.map(s => s[1]));
    return id;
  }
  quad(a, b, c, d) { this.indices.push(a, b, d, b, c, d); }
  geometry() {
    const g = new T.BufferGeometry();
    g.setAttribute('position', new T.Float32BufferAttribute(this.p, 3));
    g.setAttribute('uv', new T.Float32BufferAttribute(this.uv, 2));
    g.setAttribute('color', new T.Float32BufferAttribute(this.c, 3));
    g.setAttribute('skinIndex', new T.Uint16BufferAttribute(this.ids, 4));
    g.setAttribute('skinWeight', new T.Float32BufferAttribute(this.weights, 4));
    g.setIndex(this.indices); g.computeVertexNormals(); g.computeBoundingSphere();
    return g;
  }
}

// A curved vane, rather than a flat sprite. Its UVs read the unchanged
// painted feather; shallow camber gives a real highlight and an edge.
export function feather(builder, { base, along, across, normal, length, width, color, weights, bend = 0.8 }) {
  const first = builder.p.length / 3;
  const rows = 9, cols = 4;
  for (let r = 0; r <= rows; r++) {
    const t = r / rows;
    for (let c = 0; c <= cols; c++) {
      const s = c / cols - 0.5;
      const vane = 1;
      const point = base.clone().addScaledVector(along, t * length)
        .addScaledVector(across, s * width * vane)
        .addScaledVector(normal, Math.sin(Math.PI * t) * bend * (1 - s * s * 2));
      builder.vertex(point, [t, 0.5 + s], color, typeof weights === 'function' ? weights(t) : weights);
    }
  }
  for (let r = 0; r < rows; r++) for (let c = 0; c < cols; c++) {
    const a = first + r * (cols + 1) + c;
    builder.quad(a, a + 1, a + cols + 2, a + cols + 1);
  }
}

export async function featherTextures() {
  const url = new URL('../assets/feathers.json', import.meta.url);
  const response = await fetch(url);
  if (!response.ok) throw new Error('Feather manifest: HTTP ' + response.status);
  const rows = await response.json();
  const textures = [];
  for (const row of rows) {
    const asset = await fetch(new URL(row.file, url));
    if (!asset.ok) throw new Error('Feather painting: HTTP ' + asset.status);
    const crop = row.source;
    const image = await createImageBitmap(await asset.blob(), crop.x, crop.y, crop.width, crop.height);
    const canvas = document.createElement('canvas'); canvas.width = 512; canvas.height = 256;
    const ctx = canvas.getContext('2d'); const m = row.fit;
    ctx.setTransform(512 * m.a, 200 * m.b, 512 * m.c, 200 * m.d, 512 * m.e, 128 + 200 * m.f);
    ctx.drawImage(image, 0, 0); image.close();
    const texture = new T.CanvasTexture(canvas); texture.colorSpace = T.SRGBColorSpace;
    texture.anisotropy = 4; texture.name = row.id; textures.push(texture);
  }
  const originalResponse = await fetch(new URL('../assets/original-rivet.png', import.meta.url));
  if (!originalResponse.ok) throw new Error('Original painting: HTTP ' + originalResponse.status);
  const original = await createImageBitmap(await originalResponse.blob());
  for (const [name, crop, fill] of [['body', [47, 89, 370, 326], '#34363b'], ['head', [470, 91, 252, 298], '#4b5059']]) {
    const canvas = document.createElement('canvas'); canvas.width = canvas.height = 1024;
    const ctx = canvas.getContext('2d');
    ctx.drawImage(original, ...crop, 0, 0, 1024, 1024);
    const pixels = ctx.getImageData(0, 0, 1024, 1024).data, bounds = [];
    for (let y = 0; y < 1024; y++) {
      let left = 1024, right = 0;
      for (let x = 0; x < 1024; x++) if (pixels[(y * 1024 + x) * 4 + 3] > 128) { left = Math.min(left, x); right = Math.max(right, x); }
      bounds.push([left / 1024, right / 1024]);
    }
    ctx.globalCompositeOperation = 'destination-over'; ctx.fillStyle = fill; ctx.fillRect(0, 0, 1024, 1024);
    const texture = new T.CanvasTexture(canvas); texture.colorSpace = T.SRGBColorSpace;
    texture.anisotropy = 4; texture.name = 'original.projected.' + name; texture.bounds = bounds; textures[name] = texture;
  }
  const portrait = document.createElement('canvas'); portrait.width = 370; portrait.height = 350;
  portrait.getContext('2d').drawImage(original, 446, 64, 370, 350, 0, 0, 370, 350);
  textures.portrait = new T.CanvasTexture(portrait); textures.portrait.colorSpace = T.SRGBColorSpace; textures.portrait.anisotropy = 4;
  original.close();
  return textures;
}

export function tube(points, radius, material, name) {
  const curve = new T.CatmullRomCurve3(points);
  const mesh = new T.Mesh(new T.TubeGeometry(curve, 18, radius, 8, false), material);
  mesh.name = name; mesh.castShadow = true; return mesh;
}

