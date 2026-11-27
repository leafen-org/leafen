import { afterNextPaint } from "./afterPress";

/*
 * A page left by a press for a screen of another kind — home, the library,
 * my tasks, apps, people, the developer tools — is taken down after that
 * screen is painted, not in its frame: the page's body let go, every part of
 * it, held the press (2026-11-14: about 50 ms of the library's 135 ms first
 * frame from a page holding a table, at CPU ×2). Till then the page stays as
 * it was drawn, hidden (components/product/useLeavingPage.ts), and still the page open, so
 * nothing in it is told to change while it goes: the product lets the page
 * go (useAuthenticatedBootstrap.ts) when the screen has been painted.
 */

const listeners = new Set<() => void>();
let held = false;
let boxes: ReadonlyMap<Element, LeftPageBox> = new Map();

/** Where one of the page area's children stood, in the page area, and how tall. */
export type LeftPageBox = { top: number; height: number };

/** The page left stands till the next paint, which lets it go. */
export function holdLeavingPage() {
  held = true;
  boxes = pageAreaBoxes();
  afterNextPaint(letLeavingPageGo);
}

/**
 * The page area's children as the press found them, laid out as last painted:
 * the page left keeps their places and sizes while it stands hidden, so
 * nothing in it is told its size changed (useLeavingPage.ts).
 */
export function leavingPageBoxes(): ReadonlyMap<Element, LeftPageBox> {
  return boxes;
}

function pageAreaBoxes(): ReadonlyMap<Element, LeftPageBox> {
  const main = document.querySelector("[data-app-main]");
  if (!main) return new Map();
  const top = main.getBoundingClientRect().top + main.clientTop - main.scrollTop;
  return new Map([...main.children].map((child) => {
    const box = child.getBoundingClientRect();
    return [child, { top: box.top - top, height: box.height }];
  }));
}

/** Whether a page just left is still standing, its screen not yet painted. */
export function leavingPageHeld() {
  return held;
}

function letLeavingPageGo() {
  if (!held) return;
  held = false;
  boxes = new Map();
  // Together, in one task: the page's place and the product's page go in one drawing.
  for (const listener of [...listeners]) listener();
}

/** Follows the page left going, its screen painted; returns how to stop. */
export function onLeavingPageGone(listener: () => void): () => void {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}
