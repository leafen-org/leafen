// When the dark tip comes and goes, as Notion's (its tooltip in Chrome at 1440,
// 2026-11-18, the top bar, the sidebar, a side peek's head, a database's
// toolbar and a block's handles, beside its code): the pointer resting on a
// control shows the tip once a wait has passed — 150 ms, 300 on a block's
// or a row's "⋮⋮" and "+", 400 on a page's "⋯" — and a tip comes in and
// goes out over 50 ms. Leaving the control or a key takes the tip at once,
// and so does a press, which also drops a wait not yet over (Notion's tip
// closes on the mouse going down, every one); the next control waits its own
// time again: a tip just up
// does not hurry the next. A handle's tip stays 500 ms after the pointer
// leaves the handle while the handle stays (onto the block's words, or to its
// "+"), and goes with the handle at once. The keyboard's focus shows a tip
// with no wait.

import { useEffect, useMemo, useRef } from "react";
import styles from "@/components/IconTipButton.module.css";

/** How long the pointer rests on each kind of control before its tip shows. */
export const TIP_WAIT = { none: 0, control: 150, handle: 300, pageMenu: 400 } as const;
export type TipWait = keyof typeof TIP_WAIT;

// The places whose tips wait other than 150 ms where their controls aren't
// marked `data-tip-wait` themselves, one table. Notion's code gives some 47
// tips no wait at all; measured in Chrome (2026-11-18, two workspaces' inbox
// at 1440, 38–50 ms from the pointer coming to the tip drawn, its drawing
// alone) are the inbox panel's head: pin, close and filter.
const TIP_WAIT_PLACES: ReadonlyArray<readonly [string, TipWait]> = [
  ["[data-inbox-pin], [data-inbox-close], [data-inbox-filter], [data-inbox-settings]", "none"],
];

/** How long a handle's tip stays once the pointer has left the handle. */
export const HANDLE_TIP_LINGER_MS = 500;

/** The fade a tip comes in and goes out with (its CSS animation holds the same 50 ms). */
const TIP_FADE_MS = 50;

/** The wait a control's tip takes: what its `data-tip-wait` or its place names, else 150 ms. */
export function tipWaitOf(control: Element) {
  const kind = control.getAttribute("data-tip-wait");
  if (kind && kind in TIP_WAIT) return TIP_WAIT[kind as TipWait];
  const place = TIP_WAIT_PLACES.find(([selector]) => control.matches(selector));
  return TIP_WAIT[place ? place[1] : "control"];
}

/**
 * A press anywhere takes the tip of a component that draws its own while
 * `armed` (shown, or its wait running), as Notion's goes as the mouse goes
 * down: `hide` takes it and drops the wait. With its `control`, a move of the
 * pointer found off it takes the tip as leaving it does: a control moved or
 * drawn again under a resting pointer gets no leave from the browser, and its
 * tip stayed on through presses elsewhere (2026-11-24: a list's value passed
 * under the pointer as a dragged row was dropped kept its "Due"). A key
 * leaves a card value's name up while the pointer rests on it, as Notion's
 * (seen 2026-11-24: "S" under a list row's value through Shift and Escape).
 */
export function useTipPress(armed: boolean, hide: () => void, control?: () => Element | null | undefined) {
  const latest = useRef({ hide, control });
  useEffect(() => { latest.current = { hide, control }; });
  useEffect(() => {
    if (!armed) return;
    const press = () => latest.current.hide();
    const moved = (event: PointerEvent) => {
      const own = latest.current.control?.();
      if (own && event.target instanceof Node && !own.contains(event.target)) latest.current.hide();
    };
    document.addEventListener("pointerdown", press, true);
    document.addEventListener("pointermove", moved, { capture: true, passive: true });
    return () => {
      document.removeEventListener("pointerdown", press, true);
      document.removeEventListener("pointermove", moved, true);
    };
  }, [armed]);
}

/**
 * Lets a tip go out over the fade instead of at once: the element itself, or
 * a copy of one its owner takes away now. What fades is no tip any more to
 * readers or to a search for one.
 */
export function fadeTipOut(element: HTMLElement | null, { copy = false } = {}) {
  if (!element?.isConnected || typeof document === "undefined") return;
  const leaving = copy ? element.cloneNode(true) as HTMLElement : element;
  for (const name of ["role", "id", "data-icon-tip", "data-icon-tip-shown"]) leaving.removeAttribute(name);
  leaving.setAttribute("aria-hidden", "true");
  leaving.classList.add(styles.tipLeaving);
  leaving.style.pointerEvents = "none";
  if (copy) element.after(leaving);
  window.setTimeout(() => leaving.remove(), TIP_FADE_MS);
}

// Whether a control still shows in full: drawn, and nothing over it fading
// it out (a handle going as the pointer leaves its block).
function shownInFull(control: HTMLElement | null | undefined) {
  if (!control?.isConnected || !(control.checkVisibility?.({ visibilityProperty: true }) ?? true)) return false;
  for (let at: HTMLElement | null = control; at; at = at.parentElement) {
    if (Number.parseFloat(getComputedStyle(at).opacity) < 0.99) return false;
  }
  return true;
}

/**
 * A tip's own clock for a component that draws its tip itself: `rest` shows
 * it once the pointer has rested the wait, `leave` takes it now — or, on a
 * handle, after its stay, and at once with no fade if the handle itself goes
 * from sight meanwhile, as Notion's goes with its handle — and `stop` drops
 * what is running.
 */
export function useTipClock(kind: TipWait = "control") {
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return useMemo(() => {
    const stop = () => {
      window.clearTimeout(timer.current);
      timer.current = undefined;
    };
    // The wait is the kind's, or for a plain control what its place in the table asks.
    const rest = (show: () => void, control?: Element | null) => {
      stop();
      const wait = kind === "control" && control ? tipWaitOf(control) : TIP_WAIT[kind];
      if (wait === 0) { show(); return; }
      timer.current = window.setTimeout(() => { timer.current = undefined; show(); }, wait);
    };
    const leave = (hide: (atOnce?: boolean) => void, control?: HTMLElement | null) => {
      stop();
      if (kind !== "handle") { hide(); return; }
      const until = performance.now() + HANDLE_TIP_LINGER_MS;
      const check = () => {
        timer.current = undefined;
        if (!shownInFull(control)) hide(true);
        else if (performance.now() >= until) hide();
        else timer.current = window.setTimeout(check, 16);
      };
      timer.current = window.setTimeout(check, 16);
    };
    return { rest, leave, stop };
  }, [kind]);
}
