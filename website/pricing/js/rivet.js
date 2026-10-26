import * as T from '../../rivet/vendor/three-0.186.1/three.module.js';
import { RivetModel } from '../../rivet/model/rivet.js';
import { Rivet as PaintedFallback, DEFAULT_POSE } from './legacy-rivet.js';
export { DEFAULT_POSE };

const UNIT = 1.17;
const radiansToDegrees = 180 / Math.PI;
const clamp = (x, a = 0, b = 1) => Math.max(a, Math.min(b, x));

// The scene keeps its authored camera and clock. The character owns its
// skeleton, reachable contacts and transparent render; every prop queries
// the same posed joints that are rendered into the scene.
export class Rivet extends PaintedFallback {
  constructor(options) {
    super(options);
    this.model = null; this.lastPose = null;
    this.ready = this.prepareModel().catch(error => {
      this.model = null;
      throw error;
    });
  }

  async prepareModel() {
    const canvas = document.createElement('canvas');
    this.renderer = new T.WebGLRenderer({ canvas, antialias: true, alpha: true, powerPreference: 'high-performance' });
    canvas.addEventListener('webglcontextlost', () => this.onUnavailable?.(new Error('Character graphics became unavailable.')));
    this.renderer.setClearColor(0x000000, 0); this.renderer.outputColorSpace = T.SRGBColorSpace;
    this.renderer.toneMapping = T.ACESFilmicToneMapping; this.renderer.toneMappingExposure = 1.25;
    this.scene = new T.Scene();
    this.scene.add(new T.HemisphereLight('#d3e0ef', '#6e615b', 2.6));
    this.key = new T.DirectionalLight('#fff1da', 3.4); this.scene.add(this.key);
    this.rim = new T.DirectionalLight('#c4d8df', 1.8); this.rim.position.set(-100, 180, -160); this.scene.add(this.rim);
    this.camera = new T.OrthographicCamera(-190, 190, 300, -40, 1, 1500); this.camera.position.set(0, 0, 600);
    this.model = await RivetModel.create(); this.scene.add(this.model.root);
    this.poseModel({ ...DEFAULT_POSE }); this.renderer.setSize(768, 688, false);
    this.key.position.set(-120, 240, 220); this.renderer.render(this.scene, this.camera);
    window.rivetMesh = this;
  }

  poseModel(pose) {
    if (!this.model || this.lastPose === pose) return;
    const target = pose.meshTarget;
    this.model.setPose({
      bodyPitch: pose.bodyPitch, crouch: pose.crouch, puff: pose.puff, squash: pose.squash, crest: pose.crest,
      breath: pose.breath, headDip: pose.headAngle + pose.sleep * 0.25,
      lookX: pose.lookX, lookY: pose.lookY, brow: pose.brow, neck: pose.neck,
      headYaw: clamp(pose.headTurn * 0.45 + pose.lookX * 0.05, -0.45, 0.45),
      neckLower: pose.sleep * 0.18 + pose.headX * 0.004,
      neckUpper: -pose.sleep * 0.22 - pose.headY * 0.003,
      // Scenes author wingRaise as the 2D rig did, + for the downstroke; the
      // mesh lifts with +. wingLift, when a scene sets it, is already in mesh terms.
      wingOpen: pose.wingOpen, farWingOpen: pose.farWingOpen,
      wingLift: pose.wingLift ?? -pose.wingRaise, farWingLift: pose.farWingLift ?? -(pose.farWingRaise ?? pose.wingRaise),
      wingSweep: pose.wingSweep, farWingSweep: pose.farWingSweep, wingFan: pose.wingFan, wingTuck: pose.wingTuck,
      eyeWide: pose.eyeWide, pupil: pose.pupil, crestRaise: pose.crestRaise,
      tailPitch: -pose.tailAngle, tailFan: pose.tailSpread,
      beakOpen: pose.beakOpen, blink: Math.max(pose.blink, pose.sleep), lid: pose.lid,
      time: pose._time || 0, gripFar: pose.gripFar, gripNear: pose.gripNear,
      feet: [{ x: pose.footFar.x / UNIT, y: -pose.footFar.y / UNIT, z: -16 }, { x: pose.footNear.x / UNIT, y: -pose.footNear.y / UNIT, z: 16 }],
      headTarget: target ? { x: target.x / UNIT, y: -target.y / UNIT, contact: target.weight } : null,
    });
    this.lastPose = pose;
  }

  localPoint(object, point = new T.Vector3()) {
    const p = this.model.root.worldToLocal(object.localToWorld(point));
    return { x: p.x * UNIT, y: -p.y * UNIT };
  }

  reachBeak(pose, target, amount) {
    if (!this.model) return super.reachBeak(pose, target, amount);
    const k = clamp(amount); if (!k) return pose;
    this.poseModel(pose);
    const from = this.localPoint(this.model.grip);
    const to = this.rootMatrix(pose).inverse().transformPoint(new DOMPoint(target.x, target.y));
    return { ...pose, meshTarget: { x: from.x + (to.x - from.x) * k, y: from.y + (to.y - from.y) * k, weight: k } };
  }

  headMatrix(pose, root) {
    if (!this.model) return super.headMatrix(pose, root);
    this.poseModel(pose);
    const at = this.localPoint(this.model.head);
    const tip = this.localPoint(this.model.head, new T.Vector3(1, 0, 0));
    const angle = Math.atan2(tip.y - at.y, tip.x - at.x) * radiansToDegrees;
    return root.translate(at.x, at.y).rotate(angle).scale(UNIT);
  }

  holdMatrix(pose, base = new DOMMatrix()) {
    if (!this.model) return super.holdMatrix(pose, base);
    this.poseModel(pose);
    const at = this.localPoint(this.model.grip);
    const tip = this.localPoint(this.model.grip, new T.Vector3(1, 0, 0));
    const angle = Math.atan2(tip.y - at.y, tip.x - at.x) * radiansToDegrees;
    return this.rootMatrix(pose, base).translate(at.x, at.y).rotate(angle).scale(UNIT);
  }

  locate(pose, name, base = new DOMMatrix()) {
    if (!this.model) return super.locate(pose, name, base);
    this.poseModel(pose);
    const m = this.model;
    const locations = {
      beakHold: [m.grip], beakTip: [m.head, new T.Vector3(39, -3, 30)],
      eye: [m.head, new T.Vector3(1, 7, 30)], crown: [m.head, new T.Vector3(-12, 33, 30)], head: [m.head],
      footNear: [m.legs[1].ankleBone], footFar: [m.legs[0].ankleBone],
      chest: [m.chest, new T.Vector3(30, -4)], belly: [m.pelvis, new T.Vector3(0, -14)],
    };
    const point = locations[name] ? this.localPoint(...locations[name]) : { x: 0, y: 0 };
    const result = this.rootMatrix(pose, base).transformPoint(new DOMPoint(point.x, point.y));
    return { x: result.x, y: result.y };
  }

  draw(ctx, pose, base, light = {}) {
    if (!this.model) return super.draw(ctx, pose, base, light);
    this.poseModel(pose);
    const root = this.rootMatrix(pose, base), pixels = Math.hypot(root.a, root.b);
    const height = clamp(Math.ceil(340 * UNIT * pixels / 128) * 128, 384, 1024);
    const width = Math.round(height * 380 / 340);
    if (this.renderer.domElement.height !== height) this.renderer.setSize(width, height, false);
    const dir = light.dir || { x: -0.5, y: -0.8 };
    this.key.position.set(dir.x * 250, -dir.y * 280, 220);
    this.rim.color.set(light.rim || '#c4d8df'); this.rim.intensity = 1 + (light.rimStrength || 0.5);
    this.renderer.render(this.scene, this.camera);
    ctx.save(); ctx.setTransform(root);
    ctx.drawImage(this.renderer.domElement, -190 * UNIT, -300 * UNIT, 380 * UNIT, 340 * UNIT); ctx.restore();
  }
}
