// A switch pressed while its screen waits for the server's answer moves at
// once, in the dimmed look of a control that waits (2026-11-05: on a network
// that stopped, a settings switch stayed put for ten seconds and more before
// it said anything; Notion's moves as it is pressed). Most switches lock
// while their change is out and turn only on the answer: the one pressed
// shows where it is going while it is locked, and once free again shows
// what its screen holds — turned, or back where it was.
//
// A lock waits on the server unless the switch says otherwise (`data-lock`):
// "browser" or "device" — the browser's or the system's permission ask, its
// push service, a feature of the device — draws the turn without the
// server's words, its own step saying what didn't end; "held" is a lock that
// waits on nothing — the browser or the system refused, or this device
// doesn't offer it — and the switch shows what its screen holds at once
// (2026-11-21: push refused in Android Chrome stayed drawn on, and after ten
// seconds said the server wasn't answering).

import { WAITING_NOTE_MS, waitingNoteText } from "./pendingChoice";

const LOCK_WAIT_MS = 1_000;
let watching = false;

function follow(control: HTMLElement, state: "aria-checked" | "aria-pressed", before: string) {
  const turned = () => control.getAttribute(state) !== before;
  const locked = () => control.hasAttribute("disabled") || control.getAttribute("aria-disabled") === "true";
  let waiting = false;
  const observer = new MutationObserver(() => check());
  // A switch that doesn't lock soon is one that turns at once or not at all.
  const timer = window.setTimeout(() => { if (!waiting) done(); }, LOCK_WAIT_MS);
  let slow = 0;
  function quiet() {
    window.clearTimeout(slow);
    slow = 0;
    delete control.dataset.waitingNote;
  }
  function done() {
    observer.disconnect();
    window.clearTimeout(timer);
    quiet();
    delete control.dataset.turning;
  }
  function check() {
    if (!control.isConnected || turned()) { done(); return; }
    if (locked()) {
      const lock = control.dataset.lock ?? "server";
      if (lock === "held") { done(); return; }
      if (!waiting) control.dataset.turning = before === "true" ? "off" : "on";
      waiting = true;
      // Ten seconds without the server's answer: the switch says so under it.
      if (lock !== "server") quiet();
      else if (!slow) slow = window.setTimeout(() => { if (control.isConnected) control.dataset.waitingNote = waitingNoteText(); }, WAITING_NOTE_MS);
      return;
    }
    if (waiting) done();
  }
  observer.observe(control, { attributes: true, attributeFilter: [state, "disabled", "aria-disabled", "data-lock"] });
}

/** Starts showing a pressed switch's turn while its screen waits for the answer. */
export function watchSwitchTurns() {
  if (watching || typeof document === "undefined" || typeof MutationObserver === "undefined") return;
  watching = true;
  document.addEventListener("click", (event) => {
    // A button drawn as the switch: a switch's own, a row that is one, or a
    // pressed-or-not toggle with the switch's mark (an organization's policies).
    const control = event.target instanceof Element ? event.target.closest<HTMLElement>("button") : null;
    if (!control || !control.querySelector("[data-switch-mark]")) return;
    if (control.hasAttribute("disabled") || control.getAttribute("aria-disabled") === "true") return;
    const state = control.hasAttribute("aria-checked") ? "aria-checked" : "aria-pressed";
    const before = control.getAttribute(state);
    if (before === "true" || before === "false") follow(control, state, before);
  }, true);
}
