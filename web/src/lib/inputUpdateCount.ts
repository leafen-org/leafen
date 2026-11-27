import { flushSync } from "react-dom";
import { createRoot, type Root } from "react-dom/client";

let marker: Root | null = null;
let turn = 0;

/**
 * Each key a person presses, and each text it puts in, starts React's count
 * of updates made one after another anew, so a field keeps every letter
 * however fast they come.
 *
 * React (19.2) counts each change it draws at once while other changes wait
 * to be drawn, and past 50 it refuses the next one as an endless loop. A
 * field's letter is drawn at once, and many fields start a slower change
 * from it (a search's results, a list's chosen row): letters that come
 * faster than that change can be drawn — an agent typing through the
 * browser, a tool that types a text in — reached 50 within one run, and the
 * 51st letter's change was refused and the field put back without it
 * (2026-11-19: one letter in 51 lost in the sidebar's add window search and
 * in quick find, every time; a person typing, even at CPU ×6, never reached
 * it). A change drawn in a root of its own that leaves nothing waiting ends
 * the count, so each pressed key draws one there first: one key is one new
 * cause, as a person's presses are; a loop within one key's changes is still
 * stopped. Only the browser's own key and text events count, never one a
 * script sends.
 */
export function startEachInputFresh() {
  if (marker || typeof document === "undefined") return;
  const root = createRoot(document.createElement("div"));
  marker = root;
  const fresh = (event: Event) => {
    if (!event.isTrusted) return;
    turn = 1 - turn;
    flushSync(() => root.render(turn));
  };
  window.addEventListener("keydown", fresh, true);
  window.addEventListener("beforeinput", fresh, true);
}
