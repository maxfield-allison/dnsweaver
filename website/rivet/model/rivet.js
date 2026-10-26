import * as T from '../vendor/three-0.186.1/three.module.js';
import { SkinBuilder, V, clamp, mix, smooth, feather, featherTextures, tube } from './geometry.js';

const charcoal = new T.Color('#4b5059');
const silver = new T.Color('#a6abb2');

// The arm of the near wing in chest space, as [segment direction, wing-plane
// normal] for humerus, forearm and hand; the far wing mirrors z. Folded lays
// the arm along the flank with the plane facing out; spread holds it level.
// Poses between are slerps of these frames, so a half-open wing is a rotation
// of the folded one rather than an average of two directions.
const ARM_KEYS = {
  folded: [[V(-0.503, -0.827, 0.252), V(0.12, 0.05, 1)], [V(0.806, 0.589, 0.062), V(0.1, 0.05, 1)], [V(-0.881, -0.472, -0.031), V(0.06, 0.1, 1)]],
  spread: [[V(-0.18, 0.06, 1), V(0, 1, 0)], [V(0.14, 0, 1), V(0, 1, 0)], [V(-0.32, -0.02, 1), V(0, 1, 0)]],
};
// Where each feather group lies on the folded wing, in chest space: primaries
// cross over the tail, secondaries and coverts shingle above them. Layer is
// the lift off the wing plane, outermost first, so stacked vanes never fight.
const FOLDED_FEATHERS = {
  primary: { along: V(-0.85, -0.52, -0.1), normal: V(0.06, 0.08, 1), fan: 0.1, layer: [0.9, 0] },
  secondary: { along: V(-0.86, -0.5, -0.08), normal: V(0.05, 0.08, 1), fan: 0.16, layer: [1.9, 1.1] },
  covert: { along: V(-0.86, -0.46, -0.04), normal: V(0.05, 0.1, 1), fan: 0.06, layer: [3.2, 2.2] },
};
const mirrorZ = (v, s) => V(v.x, v.y, v.z * s);
function boneFrame(direction, normal) {
  const y = direction.clone().normalize();
  const z = normal.clone().addScaledVector(y, -normal.dot(y)).normalize();
  const x = new T.Vector3().crossVectors(y, z);
  return new T.Quaternion().setFromRotationMatrix(new T.Matrix4().makeBasis(x, y, z));
}
const profiles = [
  [28, -7, 3, 3], [40, -8, 27, 18], [61, -9, 43, 27], [85, -5, 47, 31],
  [100, 0, 39, 29], [115, 3, 27, 23], [127, 6, 14, 14], [134, 8, 2, 2],
];
function profile(y) {
  let i = 0; while (i + 1 < profiles.length - 1 && profiles[i + 1][0] < y) i++;
  const a = profiles[i], b = profiles[Math.min(i + 1, profiles.length - 1)];
  const k = smooth((y - a[0]) / (b[0] - a[0]));
  return { x: mix(a[1], b[1], k), rx: mix(a[2], b[2], k), rz: mix(a[3], b[3], k) };
}

export class RivetModel {
  static async create() { return new RivetModel(await featherTextures()); }

  constructor(textures) {
    this.root = new T.Group(); this.root.name = 'Rivet';
    this.bones = []; this.roles = {}; this.skins = []; this.featherDrivers = [];
    this.textures = textures; this.currentPose = {};
    // The painted vanes carry pale tips that read as a white stripe once they
    // stack; each material compresses its highlights toward the body's
    // charcoal by its own amount before the gain.
    const featherPaint = (map, compress, gain, tipShade = 1) => {
      const material = new T.MeshStandardMaterial({ map, vertexColors: false, roughness: 0.88, metalness: 0, alphaTest: 0.16, side: T.DoubleSide });
      material.onBeforeCompile = shader => {
        shader.fragmentShader = shader.fragmentShader.replace('#include <map_fragment>', `
          #ifdef USE_MAP
            vec4 featherPaint = texture2D(map, vMapUv);
            float grain = dot(featherPaint.rgb, vec3(0.299, 0.587, 0.114));
            diffuseColor.rgb = featherPaint.rgb * mix(1.0, ${compress.toFixed(2)}, smoothstep(0.24, 0.58, grain)) * ${gain.toFixed(2)};
            // The underside of a vane sits in the wing's own shadow.
            if (!gl_FrontFacing) diffuseColor.rgb *= 0.68;
            // The painted secondaries fade to pale grey at the tip; a jackdaw's
            // trailing edge stays dark.
            diffuseColor.rgb *= mix(1.0, ${tipShade.toFixed(2)}, smoothstep(0.6, 0.95, vMapUv.x));
            diffuseColor.a *= featherPaint.a;
          #endif
        `);
      };
      material.customProgramCacheKey = () => `rivet-feather-${compress}-${gain}-${tipShade}`;
      return material;
    };
    this.paintMaterial = featherPaint(textures[0], 0.6, 1.32);
    this.softPaint = featherPaint(textures[2], 0.5, 1.2);
    this.secondaryPaint = featherPaint(textures[1], 0.55, 1.3, 0.58);
    this.coreMaterial = new T.MeshStandardMaterial({ color: '#ffffff', map: textures.body, bumpMap: textures.body, bumpScale: 0.55, roughness: 0.92, metalness: 0, vertexColors: false });
    this.legMaterial = new T.MeshStandardMaterial({ color: '#42444a', roughness: 0.72 });
    // Paint anchor: the painting already carries its own shading, so lit
    // colour is pulled part of the way back to the painted value. A wing
    // turned to the sky or a belly turned to the floor keeps Rivet's charcoal
    // instead of going grey or brown.
    this.paintAnchor = { value: 0.38 };
    for (const [name, material] of Object.entries({ primary: this.paintMaterial, covert: this.softPaint, secondary: this.secondaryPaint, body: this.coreMaterial })) this.anchorPaint(material, name);
    this.clawMaterial = new T.MeshStandardMaterial({ color: '#24262c', roughness: 0.44 });
    this.rootBone = this.bone('root', null, V());
    this.pelvis = this.bone('pelvis', this.rootBone, V(0, 48));
    this.chest = this.bone('chest', this.pelvis, V(0, 48));
    this.neckLower = this.bone('neck.lower', this.chest, V(10, 12));
    this.neckUpper = this.bone('neck.upper', this.neckLower, V(6, 10));
    this.head = this.bone('head', this.neckUpper, V(7, 10));
    const core = this.buildCore(); core.add(this.rootBone); this.root.add(core);
    this.root.updateMatrixWorld(true);
    this.buildHead(); this.buildPortrait();
    this.wings = [-1, 1].map(side => this.buildWing(side));
    this.buildTail(); this.legs = [-1, 1].map(side => this.buildLeg(side));
    this.root.updateMatrixWorld(true);
    this.skeleton = new T.Skeleton(this.bones);
    for (const mesh of this.skins) { mesh.bind(this.skeleton); mesh.normalizeSkinWeights(); mesh.frustumCulled = false; }
    this.debug = new T.SkeletonHelper(this.rootBone); this.debug.matrix = this.rootBone.matrix; this.debug.visible = false;
    this.debug.material.depthTest = false; this.debug.renderOrder = 100;
    this.root.add(this.debug);
    this.setPose({});
  }

  anchorPaint(material, name) {
    const previous = material.onBeforeCompile, key = material.customProgramCacheKey?.bind(material);
    material.onBeforeCompile = (shader, renderer) => {
      if (previous) previous.call(material, shader, renderer);
      shader.uniforms.paintAnchor = this.paintAnchor;
      shader.fragmentShader = shader.fragmentShader
        .replace('#include <common>', '#include <common>\nuniform float paintAnchor;')
        .replace('#include <opaque_fragment>', 'outgoingLight = mix(outgoingLight, diffuseColor.rgb, paintAnchor);\n#include <opaque_fragment>');
    };
    material.customProgramCacheKey = () => `anchored-${name}-${key ? key() : material.type}`;
  }

  bone(name, parent, position) {
    const b = new T.Bone(); b.name = name; b.position.copy(position);
    this.roles[name] = this.bones.length; this.bones.push(b);
    if (parent) parent.add(b); return b;
  }
  skin(builder, material, name) {
    const mesh = new T.SkinnedMesh(builder.geometry(), material); mesh.name = name;
    mesh.castShadow = true; mesh.receiveShadow = true; this.skins.push(mesh); this.root.add(mesh); return mesh;
  }

  buildCore() {
    const b = new SkinBuilder(), rows = 48, cols = 48;
    for (let i = 0; i <= rows; i++) {
      const y = mix(28, 134, i / rows), p = profile(y);
      for (let j = 0; j <= cols; j++) {
        const a = j / cols * Math.PI * 2;
        const color = charcoal.clone().lerp(silver, smooth((y - 101) / 35) * smooth((-Math.cos(a) + 0.5) / 0.9));
        const v = mix(0.06, 0.93, i / rows), bounds = this.textures.body.bounds[Math.round((1 - v) * 1023)];
        const u = mix(bounds[0] + 0.055, bounds[1] - 0.055, 1 - Math.acos(Math.cos(a)) / Math.PI);
        b.vertex(V(p.x + Math.cos(a) * p.rx, y, Math.sin(a) * p.rz), [u, v], color, [[1, 1 - smooth((y - 45) / 50)], [2, smooth((y - 45) / 50)]]);
      }
    }
    for (let i = 0; i < rows; i++) for (let j = 0; j < cols; j++) {
      const a = i * (cols + 1) + j; b.quad(a, a + cols + 1, a + cols + 2, a + 1);
    }
    const mesh = this.skin(b, this.coreMaterial, 'continuous.body.neck');
    this.root.remove(mesh); return mesh;
  }

  buildHead() {
    this.jaw = this.bone('bill.lower', this.head, V(12, 0));
    this.crestBase = this.bone('crest.base', this.head, V(-7, 29));
    this.crestTip = this.bone('crest.tip', this.crestBase, V(-8, 3));
    this.grip = new T.Object3D(); this.grip.name = 'contact.beak'; this.grip.position.set(19, -3.5, 30); this.jaw.add(this.grip);
  }

  buildPortrait() {
    const b = new SkinBuilder(), rows = 50, cols = 52;
    this.root.updateMatrixWorld(true);
    for (let row = 0; row <= rows; row++) for (let col = 0; col <= cols; col++) {
      const u = col / cols, v = row / rows, x = -48 + u * 88.8, y = 39.5 - v * 84;
      const point = this.head.localToWorld(V(x, y, 30));
      const lowerBill = x > 12 && y < -1 && y > -12;
      const neck = smooth((-y - 5) / 32) * (1 - smooth((x - 3) / 18));
      const crest = smooth((y - 26) / 12) * (1 - smooth((x - 2) / 10));
      const influences = crest > 0 ? [[this.roles.head, 1 - crest], [this.roles['crest.base'], crest * 0.3], [this.roles['crest.tip'], crest * 0.7]] : lowerBill ? [[this.roles['bill.lower'], 1]] : [[this.roles.head, 1 - neck * 0.8], [this.roles['neck.upper'], neck * 0.5], [this.roles['neck.lower'], neck * 0.3]];
      b.vertex(point, [u, 1 - v], new T.Color('#ffffff'), influences);
    }
    for (let row = 0; row < rows; row++) for (let col = 0; col < cols; col++) {
      const a = row * (cols + 1) + col; b.quad(a, a + cols + 1, a + cols + 2, a + 1);
    }
    const material = new T.MeshStandardMaterial({ map: this.textures.portrait, roughness: 0.95, alphaTest: 0.18, side: T.DoubleSide });
    this.portraitBlink = { value: 0 };
    this.portraitGaze = { value: new T.Vector2() };
    this.portraitWide = { value: 0 };
    this.portraitPupil = { value: 0 };
    // The eye acts on the unchanged painting by moving where it is sampled:
    // wide magnifies the whole eye inside a falloff ring, gaze slides the
    // centre, and pupil remaps radii about the painted pupil (centre 0.35,
    // 0.04 and radius 0.44 in eye units, measured on the original) so the
    // pale iris can pin to a dot or open up without repainting.
    material.onBeforeCompile = shader => {
      Object.assign(shader.uniforms, { blink: this.portraitBlink, gaze: this.portraitGaze, wide: this.portraitWide, pupil: this.portraitPupil });
      shader.fragmentShader = shader.fragmentShader.replace('#include <common>', '#include <common>\nuniform float blink; uniform vec2 gaze; uniform float wide; uniform float pupil;').replace('#include <map_fragment>', `
        const vec2 EYE_C = vec2(0.556, 0.614), EYE_R = vec2(0.052, 0.056), PUPIL_C = vec2(0.346, 0.04);
        vec2 raw = (vMapUv - EYE_C) / EYE_R;
        float magnify = mix(1.0 / (1.0 + 0.24 * wide), 1.0, smoothstep(0.95, 1.75, length(raw)));
        vec2 eye = raw * magnify;
        float pupilRegion = 1.0 - smoothstep(0.25, 0.92, length(eye));
        vec2 shifted = eye - gaze * vec2(0.25, 0.25) * pupilRegion;
        vec2 q = shifted - PUPIL_C;
        float d = length(q), size = 0.44 * clamp(1.0 + pupil * 0.6, 0.38, 1.24), outer = 0.58;
        float source = d < size ? d * 0.44 / size : 0.44 + (d - size) * (outer - 0.44) / max(outer - size, 0.01);
        vec2 sampled = d < outer ? PUPIL_C + q * (source / max(d, 1e-4)) : shifted;
        vec4 paint = texture2D(map, EYE_C + sampled * EYE_R);
        float eyeArea = 1.0 - smoothstep(1.08, 1.22, length(eye));
        float lid = smoothstep(1.03 - blink * 2.6 - 0.035, 1.03 - blink * 2.6 + 0.035, eye.y) * eyeArea * smoothstep(0.0, 0.08, blink);
        // Borrow the dark orbital feathers below the eye; sliding the whole
        // eye UV upward let the pale iris show through a nearly closed lid.
        vec3 lidPaint = texture2D(map, EYE_C + vec2(eye.x * 0.035, -0.105 + eye.y * 0.012)).rgb;
        paint.rgb = mix(paint.rgb, lidPaint, lid);
        float line = (1.0 - smoothstep(0.025, 0.09, abs(eye.y + 0.1 + eye.x * eye.x * 0.16))) * (1.0 - smoothstep(0.8, 1.02, abs(eye.x))) * smoothstep(0.85, 0.98, blink);
        paint.rgb = mix(paint.rgb, vec3(0.012), line * 0.8);
        diffuseColor *= paint;
      `);
    };
    this.anchorPaint(material, 'portrait');
    const mouthGeometry = new T.BufferGeometry().setFromPoints([V(0, 0), V(27, -3), V(0, -10)]); mouthGeometry.setIndex([0, 1, 2]); mouthGeometry.computeVertexNormals();
    this.mouth = new T.Mesh(mouthGeometry, new T.MeshBasicMaterial({ color: '#1b151a', side: T.DoubleSide }));
    this.mouth.position.set(12, 0, 29.8); this.head.add(this.mouth);
    this.portrait = this.skin(b, material, 'painted.portrait.weighted.neck'); this.portrait.receiveShadow = false;
    this.root.updateMatrixWorld(true);
  }

  buildWing(side) {
    const prefix = side > 0 ? 'wing.near' : 'wing.far';
    const shoulder = this.bone(prefix + '.shoulder', this.chest, V(0, 7, side * 26));
    const elbow = this.bone(prefix + '.elbow', shoulder, V(0, 28));
    const wrist = this.bone(prefix + '.wrist', elbow, V(0, 32));
    const hand = this.bone(prefix + '.hand', wrist, V(0, 32));
    const wing = { side, shoulder, elbow, wrist, hand, prefix, primaries: [], secondaries: [], coverts: [] };
    this.poseWing(wing, { open: 1 });
    this.root.updateMatrixWorld(true);
    const b = new SkinBuilder(), secondary = new SkinBuilder(), coverts = new SkinBuilder();
    const add = (parent, name, worldBase, direction, length, width, shade, builder = b, group = 'primary', k = 0) => {
      const at = parent.worldToLocal(worldBase.clone());
      const f = this.bone(name, parent, at);
      this.root.updateMatrixWorld(true);
      const normal = V(0, 1, 0);
      const parentInverse = parent.getWorldQuaternion(new T.Quaternion()).invert();
      f.userData.restAlong = direction.clone().applyQuaternion(parentInverse);
      f.userData.restNormal = normal.clone().applyQuaternion(parentInverse);
      f.userData.rest = boneFrame(f.userData.restAlong, f.userData.restNormal);
      f.userData.base = at.clone(); f.userData.group = group; f.userData.k = k;
      const across = new T.Vector3().crossVectors(direction, normal).normalize();
      const own = this.roles[name], support = this.roles[parent.name];
      feather(builder, { base: worldBase, along: direction, across, normal, length, width, color: charcoal.clone().multiplyScalar(shade),
        weights: t => [[support, (1 - t) * 0.15], [own, 0.85 + t * 0.15]], bend: 1.3 });
      return f;
    };
    const wristPosition = wrist.getWorldPosition(V()), elbowPosition = elbow.getWorldPosition(V());
    const handPosition = hand.getWorldPosition(V());
    for (let i = 0; i < 9; i++) {
      const k = i / 8, base = wristPosition.clone().lerp(handPosition, k * 0.92);
      const direction = V(mix(-0.95, -0.17, k), -0.06, side * mix(0.32, 1, k)).normalize();
      wing.primaries.push(add(wrist, prefix + '.primary.' + i, base, direction, mix(46, 66, Math.sin(k * Math.PI * 0.62)), 11, 0.92 + i * 0.013, b, 'primary', k));
    }
    for (let i = 0; i < 9; i++) {
      const k = i / 8, base = elbowPosition.clone().lerp(wristPosition, k);
      wing.secondaries.push(add(elbow, prefix + '.secondary.' + i, base, V(-1, -0.04, side * 0.14).normalize(), 40, 11, 1.04, secondary, 'secondary', k));
    }
    // Coverts shingle the arm in three rows. Over the hand they are the long
    // primary coverts, painted like flight feathers; elsewhere small rounded
    // feathers with a seeded jitter, so the rows read as plumage, not scales.
    let seed = 7 + (side > 0 ? 0 : 50);
    const jitter = () => { seed = (seed * 16807) % 2147483647; return seed / 2147483647 - 0.5; };
    for (let row = 0; row < 3; row++) for (let i = 0; i < 14; i++) {
      const k = i / 13, parent = k < 0.36 ? shoulder : k < 0.68 ? elbow : wrist;
      const start = k < 0.36 ? shoulder.getWorldPosition(V()) : k < 0.68 ? elbowPosition : wristPosition;
      const end = k < 0.36 ? elbowPosition : k < 0.68 ? wristPosition : handPosition;
      const span = k < 0.36 ? k / 0.36 : k < 0.68 ? (k - 0.36) / 0.32 : (k - 0.68) / 0.32;
      const base = start.clone().lerp(end, span); base.x -= row * 4.5 + jitter() * 3; base.y += 2 + (3 - row) * 0.7 + jitter() * 2;
      const direction = V(-1, -0.03 + jitter() * 0.22, side * 0.1).normalize();
      const onHand = k >= 0.68;
      wing.coverts.push(add(parent, prefix + '.covert.' + row + '.' + i, base, direction,
        (onHand ? 20 + row * 4 : 12 + row * 3) * (1 + jitter() * 0.24), onHand ? 7.5 : 8.5, 1.02, onHand ? b : coverts, 'covert', 1 - row / 2));
    }
    const support = new SkinBuilder(), joints = [shoulder, elbow, wrist, hand];
    for (let row = 0; row <= 30; row++) {
      const span = row / 30 * 3, segment = Math.min(2, Math.floor(span)), k = span - segment;
      const a = joints[segment], next = joints[segment + 1];
      const leading = a.getWorldPosition(V()).lerp(next.getWorldPosition(V()), k);
      const chord = 23 + Math.sin(row / 30 * Math.PI) * 16;
      for (let column = 0; column <= 8; column++) {
        const v = column / 8, point = leading.clone().add(V(-chord * v, 1.2 + Math.sin(v * Math.PI) * 2.8, 0));
        const blend = smooth((k - 0.65) / 0.35);
        support.vertex(point, [mix(0.25, 0.8, v), mix(0.2, 0.8, row / 30)], charcoal, [[this.roles[a.name], 1 - blend], [this.roles[next.name], blend]]);
      }
    }
    for (let row = 0; row < 30; row++) for (let column = 0; column < 8; column++) { const a = row * 9 + column; support.quad(a, a + 1, a + 10, a + 9); }
    const supportMaterial = this.coreMaterial.clone(); supportMaterial.side = T.DoubleSide;
    wing.support = this.skin(support, supportMaterial, prefix + '.continuous.patagium'); wing.support.receiveShadow = false;
    this.skin(b, this.paintMaterial, prefix + '.primaries').receiveShadow = false;
    this.skin(secondary, this.secondaryPaint, prefix + '.secondaries').receiveShadow = false;
    this.skin(coverts, this.softPaint, prefix + '.coverts').receiveShadow = false; return wing;
  }

  // Poses the arm: extension slerps folded to spread; lift raises the wing
  // about the body's long axis (+ up), sweep swings it forward (+), and tuck
  // droops (+) or flicks (-) a folded wing about its shoulder.
  poseWing(wing, { open = 0, lift = 0, sweep = 0, tuck = 0 } = {}) {
    const s = wing.side, e = smooth(open);
    const shoulder = new T.Quaternion().setFromAxisAngle(V(0, 1, 0), s * sweep)
      .multiply(new T.Quaternion().setFromAxisAngle(V(1, 0, 0), -s * lift * e))
      .multiply(new T.Quaternion().setFromAxisAngle(V(0, 0, 1), tuck * 0.16 * (1 - e)))
      .multiply(new T.Quaternion().setFromAxisAngle(V(0, 1, 0), -s * Math.max(0, -tuck) * 0.12 * (1 - e)));
    let parentRotation = new T.Quaternion();
    [wing.shoulder, wing.elbow, wing.wrist].forEach((bone, i) => {
      const [fd, fn] = ARM_KEYS.folded[i], [sd, sn] = ARM_KEYS.spread[i];
      const folded = boneFrame(mirrorZ(fd, s), mirrorZ(fn, s)), spread = boneFrame(mirrorZ(sd, s), mirrorZ(sn, s));
      const desired = shoulder.clone().multiply(folded.slerp(spread, e));
      bone.quaternion.copy(parentRotation.clone().invert().multiply(desired)); parentRotation.copy(desired);
    });
  }

  // Lays each feather between its spread rest and its place on the folded
  // wing. The fold target is held in chest space, so a folded wing follows
  // the body when Rivet leans; fan opens (+) or closes (-) the spread vanes.
  poseFeathers(wing, open, fan, time) {
    const e = smooth(open), s = wing.side;
    const chestQ = this.chest.getWorldQuaternion(new T.Quaternion());
    for (const bone of [...wing.primaries, ...wing.secondaries, ...wing.coverts]) {
      const { group, k, rest, base } = bone.userData, target = FOLDED_FEATHERS[group];
      const parentInverse = bone.parent.getWorldQuaternion(new T.Quaternion()).invert().multiply(chestQ);
      const along = mirrorZ(target.along, s).applyAxisAngle(mirrorZ(target.normal, s).normalize(), s * (k - 0.5) * target.fan);
      const foldedLocal = boneFrame(along.applyQuaternion(parentInverse), mirrorZ(target.normal, s).applyQuaternion(parentInverse));
      const toFolded = foldedLocal.multiply(rest.clone().invert());
      bone.quaternion.identity().slerp(toFolded, 1 - e);
      if (group === 'primary') bone.rotateZ(-s * (k - 0.3) * fan * 0.32 * e);
      bone.rotateY(Math.sin(time * 19 - k * 2) * 0.025 * e);
      const layer = mix(target.layer[1], target.layer[0], group === 'primary' ? 1 - k : k);
      bone.position.copy(base).addScaledVector(mirrorZ(target.normal, s).normalize().applyQuaternion(parentInverse), layer * (1 - e));
    }
  }

  buildTail() {
    this.tailBones = []; const b = new SkinBuilder(); this.root.updateMatrixWorld(true);
    for (let i = 0; i < 7; i++) {
      const name = 'tail.' + i, bone = this.bone(name, this.pelvis, V(-38, 14 + (i - 3) * 1.2, (i - 3) * 2.1));
      this.root.updateMatrixWorld(true); this.tailBones.push(bone);
      feather(b, { base: bone.getWorldPosition(V()), along: V(-0.82, -0.57 + (i - 3) * 0.035, (i - 3) * 0.025).normalize(), across: V(0.57, -0.82, 0.15).normalize(), normal: V(0, 0, 1), length: 59 + (3 - Math.abs(i - 3)) * 2, width: 9.3, color: charcoal.clone().multiplyScalar(0.9), weights: [[this.roles[name], 1]], bend: 1.2 });
    }
    this.skin(b, this.paintMaterial, 'tail.fan').receiveShadow = false;
  }

  buildLeg(side) {
    const name = side > 0 ? 'leg.near' : 'leg.far';
    const hipBone = this.bone(name + '.hip', this.pelvis, V(8, -3, side * 16));
    const heelBone = this.bone(name + '.hock', hipBone, V(0, -22));
    const ankleBone = this.bone(name + '.ankle', heelBone, V(0, -34));
    const thigh = new T.Mesh(new T.CylinderGeometry(2.8, 2, 22, 16), this.legMaterial);
    const shank = new T.Mesh(new T.CylinderGeometry(1.8, 1.4, 34, 16), this.legMaterial);
    thigh.position.y = -11; shank.position.y = -17;
    thigh.castShadow = shank.castShadow = true; hipBone.add(thigh); heelBone.add(shank);
    for (let i = 0; i < 9; i++) {
      const ring = new T.Mesh(new T.TorusGeometry(1.65 - i * 0.025, 0.16, 4, 12), this.legMaterial);
      ring.rotation.x = Math.PI / 2; ring.position.y = -14 - i * 1.8; heelBone.add(ring);
    }
    const foot = new T.Group(); foot.name = name + '.foot'; ankleBone.add(foot);
    const digits = [];
    for (let digit = 0; digit < 4; digit++) {
      const back = digit === 3, lengths = back ? [4, 4, 3] : [5, 5, digit === 1 ? 5 : 3.5];
      const base = this.bone(name + '.digit.' + digit + '.base', foot, V(0, 1.2, back ? -side : (digit - 1) * 1.7));
      base.rotation.y = back ? Math.PI : (digit - 1) * -0.28;
      const joints = [base];
      for (let segment = 0; segment < 3; segment++) {
        const joint = joints[segment];
        const mesh = new T.Mesh(new T.CylinderGeometry(0.95 - segment * 0.14, 0.82 - segment * 0.14, lengths[segment], 10), this.legMaterial);
        mesh.rotation.z = Math.PI / 2; mesh.position.x = lengths[segment] / 2; mesh.castShadow = true; joint.add(mesh);
        if (segment < 2) joints.push(this.bone(name + '.digit.' + digit + '.' + (segment + 1), joint, V(lengths[segment], 0)));
      }
      const last = joints[2], reach = lengths[2];
      last.add(tube([V(reach, 0), V(reach + 2, 0.1), V(reach + 3.5, -1)], 0.55, this.clawMaterial, name + '.claw.' + digit));
      digits.push({ joints, back });
    }
    return { side, hipBone, heelBone, ankleBone, foot, digits, contact: V(), hip: V(), heel: V(), error: 0 };
  }

  solveLeg(leg, target, grip) {
    const hip = this.root.worldToLocal(leg.hipBone.getWorldPosition(V()));
    const requested = V(target.x, target.y, target.z ?? leg.side * 16);
    const delta = requested.clone().sub(hip), distance = clamp(delta.length(), 12.01, 55.99);
    const aim = delta.clone().normalize(), end = hip.clone().addScaledVector(aim, distance);
    const pole = V(-1, 0, 0).addScaledVector(aim, aim.x).normalize();
    const a = (22 * 22 + distance * distance - 34 * 34) / (2 * distance);
    const height = Math.sqrt(Math.max(0, 22 * 22 - a * a));
    const heel = hip.clone().addScaledVector(aim, a).addScaledVector(pole, height);
    const pointAt = (bone, point) => {
      const local = bone.parent.worldToLocal(this.root.localToWorld(point.clone())).sub(bone.position).normalize();
      bone.quaternion.setFromUnitVectors(V(0, -1), local); this.root.updateMatrixWorld(true);
    };
    pointAt(leg.hipBone, heel); pointAt(leg.heelBone, end);
    const parentRotation = leg.ankleBone.getWorldQuaternion(new T.Quaternion());
    const rootRotation = this.root.getWorldQuaternion(new T.Quaternion());
    leg.foot.quaternion.copy(parentRotation.invert().multiply(rootRotation));
    leg.foot.rotateZ(-grip * 0.1);
    for (const digit of leg.digits) digit.joints.forEach((bone, i) => { bone.rotation.z = -grip * (i === 0 ? 0.3 : 0.7); });
    leg.contact.copy(end); leg.hip.copy(hip); leg.heel.copy(heel); leg.error = end.distanceTo(requested);
  }

  setPose(input) {
    const p = { bodyPitch: 0.04, crouch: 0, breath: 0, puff: 0, headDip: 0, headYaw: 0, blink: 0, neck: 1, lookX: 0, lookY: 0, brow: 0,
      wingOpen: 0, wingLift: 0, wingSweep: 0, wingFan: 0, wingTuck: 0, crest: 0, crestRaise: 0, eyeWide: 0, pupil: 0, squash: 0, tailFan: 0, tailPitch: 0, beakOpen: 0, x: 0, lift: 0, turn: 0,
      feet: [{ x: -4, y: 0, z: -16 }, { x: 16, y: 0, z: 16 }], grip: 0, ...input };
    this.portraitBlink.value = Math.max(p.blink, p.lid || 0, clamp(-p.brow) * 0.1);
    this.portraitGaze.value.set(clamp(p.lookX, -1, 1), -clamp(p.lookY, -1, 1));
    this.portraitWide.value = clamp(p.eyeWide); this.portraitPupil.value = clamp(p.pupil, -1, 1);
    this.currentPose = p; this.root.position.set(p.x, p.lift, 0); this.root.rotation.y = p.turn;
    this.rootBone.position.y = -p.crouch * 16;
    this.pelvis.rotation.z = -p.bodyPitch;
    this.chest.rotation.z = p.bodyPitch * 0.14;
    this.chest.scale.set(1 + p.breath * 0.009 + p.puff * 0.035 + p.squash * 0.075, 1 + p.breath * 0.007 - p.squash * 0.1, 1 + p.breath * 0.015 + p.puff * 0.05 + p.squash * 0.05);
    // Squash, stretch and puff belong to the body; the painted face keeps its size.
    this.neckLower.scale.set(1 / this.chest.scale.x, 1 / this.chest.scale.y, 1 / this.chest.scale.z);
    const neckShift = clamp(p.neck, 0.6, 1.35) - 1;
    const neckLower = (p.neckLower || 0) - neckShift * 0.8;
    const neckUpper = (p.neckUpper || 0) + neckShift * 0.6;
    this.neckLower.rotation.set(0, 0, neckLower);
    this.neckUpper.rotation.set(0, 0, neckUpper);
    this.head.rotation.set(0, p.headYaw, p.bodyPitch * 0.86 - p.headDip - neckLower - neckUpper);
    // crest is the spring that trails head motion; crestRaise stands the
    // untidy feathers up for surprise (+) or flattens them (-).
    this.crestBase.rotation.z = -p.crest * 0.1 - p.crestRaise * 0.42;
    this.crestTip.rotation.z = p.crest * 0.3 - p.crestRaise * 0.3 + Math.sin((p.time || 0) * 5) * 0.025;
    this.jaw.rotation.z = -p.beakOpen * 0.36; this.mouth.visible = p.beakOpen > 0.01; this.mouth.scale.y = p.beakOpen;
    for (const w of this.wings) {
      const far = w.side < 0;
      const open = far ? (p.farWingOpen ?? p.wingOpen) : p.wingOpen;
      const lift = far ? (p.farWingLift ?? p.wingLift) : p.wingLift;
      const sweep = far ? (p.farWingSweep ?? p.wingSweep) : p.wingSweep;
      w.support.visible = open > 0.6;
      this.poseWing(w, { open, lift, sweep, tuck: p.wingTuck });
      this.root.updateMatrixWorld(true);
      this.poseFeathers(w, open, far ? (p.farWingFan ?? p.wingFan) : p.wingFan, p.time || 0);
    }
    this.tailBones.forEach((b, i) => { b.rotation.y = (i - 3) * p.tailFan * 0.085; b.rotation.z = p.tailPitch + (i - 3) * p.tailFan * 0.035; });
    this.root.updateMatrixWorld(true);
    if (p.headTarget) this.reach(p.headTarget);
    this.root.updateMatrixWorld(true);
    this.legs.forEach((leg, i) => this.solveLeg(leg, p.feet[i], (i ? p.gripNear : p.gripFar) ?? p.grip));
    this.root.updateMatrixWorld(true);
  }

  // Beak IK over the neck chain. weight (0..1) blends from the posed neck to
  // the solution, so a scene can ease into and out of a contact without
  // inventing intermediate targets the neck could not reach.
  reach(target) {
    const goal = V(target.x, target.y, 0), weight = clamp(target.weight ?? 1);
    const joints = [[this.head, -1.1, 1.1], [this.neckUpper, -1.1, 0.8], [this.neckLower, -0.9, 0.7]];
    const posed = joints.map(([bone]) => bone.rotation.z);
    for (let pass = 0; pass < 20; pass++) for (const [bone, lo, hi] of joints) {
      const origin = this.root.worldToLocal(bone.getWorldPosition(V()));
      const end = this.root.worldToLocal(this.grip.getWorldPosition(V()));
      const from = end.sub(origin), to = goal.clone().sub(origin);
      const delta = Math.atan2(to.y, to.x) - Math.atan2(from.y, from.x);
      bone.rotation.z = clamp(bone.rotation.z + Math.atan2(Math.sin(delta), Math.cos(delta)) * 0.85, lo, hi);
      this.root.updateMatrixWorld(true);
    }
    if (weight < 1) {
      joints.forEach(([bone], i) => { bone.rotation.z = mix(posed[i], bone.rotation.z, weight); });
      this.root.updateMatrixWorld(true);
    }
  }

  metrics() {
    const point = b => this.root.worldToLocal(b.getWorldPosition(V())).toArray();
    return { bones: this.bones.length, skins: this.skins.length, triangles: this.skins.reduce((n, m) => n + m.geometry.index.count / 3, 0),
      blink: this.portraitBlink.value, beak: point(this.grip), reachError: this.currentPose.headTarget && (this.currentPose.headTarget.weight ?? 1) >= 0.999 ? Math.hypot(point(this.grip)[0] - this.currentPose.headTarget.x, point(this.grip)[1] - this.currentPose.headTarget.y) : 0, head: point(this.head), neck: [point(this.neckLower), point(this.neckUpper)],
      feet: this.legs.map(l => l.contact.toArray()), legErrors: this.legs.map(l => l.error), legLengths: this.legs.map(l => [l.hip.distanceTo(l.heel), l.heel.distanceTo(l.contact)]), skeleton: this.bones.map(b => ({ name: b.name, position: point(b) })) };
  }
}
