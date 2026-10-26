// The stage director: one clock for the story and the idle that follows,
// visitor controls, and the rules about when anything is allowed to move.
//
// Motion policy, in order of precedence:
//   1. prefers-reduced-motion shows the settled free scene, still. A visitor
//      can still ask for the story with the play button.
//   2. Nothing animates while the stage is mostly offscreen or the tab is in
//      the background; the clock pauses and resumes where it left off.
//   3. The story plays once. The idle that follows is finite: Rivet dozes off
//      and rendering stops. Moving over the stage or focusing a control wakes
//      him.
//
// Scene contract (see scenes/*.js):
//   storyDuration  seconds until the story settles on free
//   freeAt         story time at which every price reads $0
//   idleEndsAt     idle seconds after which the frame no longer changes
//   prepare()      async; fonts and textures
//   layout(w, h)   choose a composition for the stage size in CSS pixels
//   render(ctx, frame)
//   artIds()       optional; manifest ids of the painted assets decoded
//   onUnavailable(handler) optional; report a lost rendering capability
//
// A frame is { t, idle, woke, dpr, w, h, pointer, playing }.

// Longest step one frame may advance the clock. Slow devices (down to about
// 10 fps) still play the story in real time; longer gaps (a stalled tab) do
// not make the story jump, because the clock is re-based whenever it resumes.
const MAX_STEP = 1 / 10;
const START_RATIO = 0.6; // share of the stage visible before the story starts
// If frames stay slower than this for SLOW_FOR seconds, render the canvas at
// a lower pixel density: a softer image beats a stuttering one.
const SLOW_FRAME = 1 / 26;
const SLOW_FOR = 1.5;
const DENSITY_STEPS = [2, 1.5, 1.25];
const RUN_RATIO = 0.3; // below this the clock stops

export function mountStage({ root, scene, announce }) {
  const canvas = root.querySelector("canvas");
  // Composite the painted layers onto an opaque scene surface.
  const ctx = canvas.getContext("2d", { alpha: false });
  const buttons = {
    replay: root.querySelector('[data-action="replay"]'),
    pause: root.querySelector('[data-action="pause"]'),
    skip: root.querySelector('[data-action="skip"]'),
  };
  for (const button of Object.values(buttons)) button.disabled = true;
  buttons.replay.querySelector('[data-label]').textContent = 'Preparing scene';
  const params = new URLSearchParams(location.search);
  const reduce = window.matchMedia("(prefers-reduced-motion: reduce)");
  const state = {
    mode: "waiting", // waiting | story | idle | asleep | still
    t: 0,
    idle: 0,
    woke: false,
    paused: false,
    ratio: 0,
    pointer: null,
    w: 0,
    h: 0,
    dpr: 1,
    dprCap: 2,
    slowFor: 0,
    frameMs: 0,
    lastNow: 0,
    raf: 0,
    ticking: false, // the loop is running; the clock is only re-based on resume
    pinned: false, // a test or inspection param froze the clock
    ready: false, // prepare() has finished; the story may not start before it
    failed: false,
    obscured: false, // an open preview dialog suspends, rather than pauses, the clock
  };

  function resize() {
    if (state.failed) return;
    const rect = canvas.getBoundingClientRect();
    state.dpr = Math.min(state.dprCap, window.devicePixelRatio || 1);
    state.w = Math.max(1, rect.width);
    state.h = Math.max(1, rect.height);
    canvas.width = Math.round(state.w * state.dpr);
    canvas.height = Math.round(state.h * state.dpr);
    scene.layout(state.w, state.h);
    draw();
  }

  function draw() {
    if (state.failed || !state.ready) return;
    const start = performance.now();
    // Each frame owns its pixels, including translucent edges and motes.
    ctx.resetTransform();
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    scene.render(ctx, {
      t: state.t,
      idle: state.idle,
      woke: state.woke,
      dpr: state.dpr,
      w: state.w,
      h: state.h,
      pointer: state.pointer,
      playing: isRunning(),
    });
    const spent = performance.now() - start;
    state.frameMs = state.frameMs ? state.frameMs * 0.9 + spent * 0.1 : spent;
    updatePhase();
  }

  function updatePhase() {
    let phase = state.mode;
    if (state.mode === "story") phase = state.t >= scene.freeAt ? "free" : "rising";
    root.dataset.phase = phase;
    root.dataset.paused = String(state.paused);
    buttons.pause.setAttribute("aria-pressed", String(state.paused));
    buttons.pause.querySelector("[data-label]").textContent = state.paused ? "Play" : "Pause";
    buttons.pause.hidden = state.mode === "still" || state.mode === "asleep" || state.mode === "waiting";
    const settled = state.mode !== "story" || state.t >= scene.freeAt;
    buttons.replay.disabled = !state.ready || state.failed;
    buttons.pause.disabled = !state.ready || state.failed;
    buttons.skip.disabled = settled || !state.ready || state.failed;
    buttons.replay.querySelector("[data-label]").textContent = state.mode === "still" || state.mode === "waiting" ? "Play the scene" : "Replay";
  }

  function isRunning() {
    if (state.failed || !state.ready || state.obscured) return false;
    if (state.pinned || state.paused || document.hidden) return false;
    if (state.ratio < RUN_RATIO) return false;
    return state.mode === "story" || state.mode === "idle";
  }

  function tick(now) {
    state.raf = 0;
    const interval = Math.max(0, (now - state.lastNow) / 1000);
    const dt = Math.min(MAX_STEP, interval);
    state.lastNow = now;
    adaptDensity(interval);
    advance(dt);
    draw();
    if (!isRunning()) state.ticking = false;
    schedule();
  }

  function adaptDensity(interval) {
    state.slowFor = interval > SLOW_FRAME ? state.slowFor + interval : Math.max(0, state.slowFor - interval * 0.5);
    if (state.slowFor < SLOW_FOR) return;
    state.slowFor = 0;
    const next = DENSITY_STEPS.find((d) => d < state.dpr - 0.01);
    if (!next) return;
    state.dprCap = next;
    resize();
  }

  function advance(dt) {
    if (state.mode === "story") {
      state.t += dt;
      if (state.t >= scene.storyDuration) {
        state.t = scene.storyDuration;
        // A visitor who prefers reduced motion asked for the story, not for
        // the idle that follows it.
        state.mode = reduce.matches ? "still" : "idle";
        state.idle = 0;
      }
      return;
    }
    if (state.mode === "idle") {
      state.idle += dt;
      if (state.idle >= scene.idleEndsAt) {
        state.idle = scene.idleEndsAt;
        state.mode = "asleep";
      }
    }
  }

  function schedule() {
    if (state.raf || !isRunning()) return;
    if (!state.ticking) state.lastNow = performance.now();
    state.ticking = true;
    state.raf = requestAnimationFrame(tick);
  }

  function stop() {
    if (state.raf) cancelAnimationFrame(state.raf);
    state.raf = 0;
    state.ticking = false;
  }

  function play() {
    if (state.failed || !state.ready) return;
    state.mode = "story";
    state.t = 0;
    state.idle = 0;
    state.woke = false;
    state.paused = false;
    stop();
    draw();
    schedule();
  }

  function settle(mode) {
    if (state.failed) return;
    stop();
    state.mode = mode;
    state.t = scene.storyDuration;
    state.idle = 0;
    state.woke = false;
    state.paused = false;
    draw();
    schedule();
  }

  function wake() {
    if (state.failed) return;
    if (state.mode !== "asleep" || reduce.matches) return;
    state.mode = "idle";
    state.idle = 0;
    state.woke = true;
    draw();
    schedule();
  }

  buttons.replay.addEventListener("click", () => {
    state.pinned = false;
    play();
  });
  buttons.pause.addEventListener("click", () => {
    state.paused = !state.paused;
    stop();
    draw();
    schedule();
  });
  buttons.skip.addEventListener("click", () => {
    state.pinned = false;
    settle(reduce.matches ? "still" : "idle");
    announce?.("Skipped to the end. Every plan is free.");
  });

  canvas.addEventListener("pointermove", (event) => {
    const rect = canvas.getBoundingClientRect();
    state.pointer = { x: event.clientX - rect.left, y: event.clientY - rect.top };
    wake();
  });
  canvas.addEventListener("pointerleave", () => {
    state.pointer = null;
  });
  root.addEventListener("focusin", wake);

  // The story starts on its own once the scene is ready and the stage is
  // mostly visible. Before ready, the painted art may still be decoding and
  // the canvas is hidden behind the poster, so a running clock would play the
  // opening unseen.
  function autoplay() {
    if (!state.ready || state.mode !== "waiting") return false;
    if (state.ratio < START_RATIO || reduce.matches || params.get("autoplay") === "0") return false;
    play();
    return true;
  }

  const visibility = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) state.ratio = entry.intersectionRatio;
      if (autoplay()) return;
      if (isRunning()) schedule();
      else stop();
    },
    { threshold: [0, 0.15, 0.3, 0.45, 0.6, 0.75, 0.9, 1] },
  );
  visibility.observe(canvas);

  document.addEventListener("visibilitychange", () => {
    if (document.hidden) stop();
    else schedule();
  });

  reduce.addEventListener("change", () => {
    if (reduce.matches && state.mode !== "waiting") settle("still");
  });

  new ResizeObserver(resize).observe(canvas);

  // Inspection hooks: ?t=2.4 freezes the story at 2.4 s; ?idle=12 freezes the
  // idle at 12 s. Used for frame review and automated checks.
  const api = {
    seek(t) {
      if (state.failed) return;
      stop();
      state.pinned = true;
      state.mode = "story";
      state.t = Math.max(0, Math.min(scene.storyDuration, t));
      draw();
    },
    idleAt(s) {
      if (state.failed) return;
      stop();
      state.pinned = true;
      state.mode = "idle";
      state.t = scene.storyDuration;
      state.idle = Math.max(0, Math.min(scene.idleEndsAt, s));
      draw();
    },
    release() {
      state.pinned = false;
      schedule();
    },
    // The painted assets the scene decoded, by manifest id, so a check can
    // tell the painted scene from the procedural fallback.
    art: () => (scene.artIds ? scene.artIds() : []),
    state: () => ({
      mode: state.mode,
      t: state.t,
      idle: state.idle,
      paused: state.paused,
      running: isRunning(),
      ratio: state.ratio,
      frameMs: state.frameMs,
      dpr: state.dpr,
      reduced: reduce.matches,
    }),
  };
  window.rivetStage = api;

  function fail(error) {
    if (state.failed) return;
    state.failed = true;
    state.ready = false;
    state.mode = 'unavailable';
    stop();
    root.classList.remove('is-live');
    root.dataset.failed = 'true';
    const message = 'The scene is unavailable. Every plan is free. Reload to try again.';
    root.querySelector('.stage__note').textContent = message;
    for (const button of Object.values(buttons)) button.disabled = true;
    buttons.replay.querySelector('[data-label]').textContent = 'Scene unavailable';
    announce?.(message);
    console.warn('Rivet scene unavailable:', error);
  }
  scene.onUnavailable?.(fail);

  return {
    fail,
    setObscured(obscured) {
      state.obscured = obscured;
      stop();
      if (!obscured) schedule();
    },
    async start() {
      await scene.prepare();
      state.ready = true;
      root.classList.add("is-live");
      resize();
      if (params.has("t")) api.seek(Number(params.get("t")));
      else if (params.has("idle")) api.idleAt(Number(params.get("idle")));
      else if (reduce.matches) settle("still");
      else autoplay();
      updatePhase();
    },
  };
}
