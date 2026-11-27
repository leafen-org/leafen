// When the person last pressed, typed, pasted, cut or dropped something. A
// focus that comes late — a page's or a peek's opening, its caret placed once
// its title or body can take it — asks this first and leaves the focus where
// the person put it in the meantime (2026-11-13: lines pasted into a new
// page's title, opened from the sidebar's "+" in a center peek, were chosen
// in its body and then let go as the opening put the caret in the title).

let lastAct = Number.NEGATIVE_INFINITY;
// The act before it, and the event that made the last one (forgetAct).
let actBefore = Number.NEGATIVE_INFINITY;
let lastActEvent: Event | null = null;
const MODIFIER_KEYS = new Set(["Shift", "Meta", "Control", "Alt", "CapsLock", "Fn", "AltGraph", "OS"]);

if (typeof document !== "undefined") {
  const act = (event: Event) => {
    if (event instanceof KeyboardEvent && MODIFIER_KEYS.has(event.key)) return;
    actBefore = lastAct;
    lastAct = performance.now();
    lastActEvent = event;
  };
  for (const type of ["pointerdown", "keydown", "paste", "cut", "drop"]) {
    document.addEventListener(type, act, { capture: true, passive: true });
  }
}

/** Whether the person did something after `time` (a performance.now()). */
export function actedSince(time: number) {
  return lastAct > time;
}

/** Forgets the act `event` made, when it is the last one: a press that went nowhere (tapWhileOpening.ts). */
export function forgetAct(event: Event) {
  if (lastActEvent !== event) return;
  lastAct = actBefore;
  lastActEvent = null;
}
