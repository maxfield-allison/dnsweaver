// Mount the pricing scene and announce playback-control results.

import { mountStage } from "./stage.js";
import { paperTexture } from "./ink.js";

export async function boot(scene) {
  const announcer = document.getElementById("announcer");
  const announce = (message) => {
    announcer.textContent = "";
    // A fresh text node after a tick makes screen readers repeat a message.
    setTimeout(() => {
      announcer.textContent = message;
    }, 60);
  };
  const root = document.querySelector("[data-stage]");
  const grain = root.querySelector(".stage__grain");
  if (grain) grain.style.backgroundImage = `url(${paperTexture(384, 5, { tone: 128, spread: 34 }).toDataURL()})`;
  const stage = mountStage({ root, scene, announce });
  try {
    await stage.start();
  } catch (error) {
    stage.fail(error);
  }
}

