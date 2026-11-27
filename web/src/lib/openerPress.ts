import { useEffect, useRef } from "react";
import { openerOf } from "./lastPress";

// A control that opens a popup and says so: a block's 💬 mark, an image's comment count, a menu's button. A
// disclosure (a toggle, a sidebar section: aria-expanded alone) opens no popup.
const OPENS = '[aria-haspopup]:not([aria-haspopup="false"])';

/**
 * The control a layer opening now comes from, when it is one that opens a popup: the control the last press began
 * on (WebKit leaves the focus where it was, lastPress.ts), or else the focus — never the text a caret menu was typed
 * or pasted in, whose press beside the menu goes on to the page.
 */
export function popupOpenerOf(focused: Element | null): HTMLElement | null {
  for (const control of [openerOf(null), focused]) {
    if (control instanceof HTMLElement && control.isConnected && !control.isContentEditable && control.matches(OPENS)) return control;
  }
  return null;
}

// The openers of the layers open now (usePopupOpener): a control one of them came from is that layer's, not another's.
const openers = new Set<Element>();

/** The popup opener of a layer, read in the pass it opens in — before what it draws takes the focus — and kept while it stays open. */
export function usePopupOpener(open: boolean) {
  const kept = useRef<{ open: boolean; opener: HTMLElement | null }>({ open: false, opener: null });
  if (open && !kept.current.open) kept.current.opener = typeof document === "undefined" ? null : popupOpenerOf(document.activeElement);
  kept.current.open = open;
  useEffect(() => {
    const opener = open ? kept.current.opener : null;
    if (!opener) return undefined;
    openers.add(opener);
    return () => { openers.delete(opener); };
  }, [open]);
  return kept;
}

// The opener whose press closed its layer: the click that press ends in goes no further (pressOnOpener).
let cutting: Element | null = null;
if (typeof document !== "undefined") {
  // Registered before any layer's own press listener, so each press starts with none held.
  document.addEventListener("pointerdown", () => { cutting = null; }, true);
  document.addEventListener("pointercancel", () => { cutting = null; }, true);
  document.addEventListener("click", (event) => {
    const control = cutting;
    cutting = null;
    if (!control || !(event.target instanceof Node) || !control.contains(event.target)) return;
    event.preventDefault();
    event.stopPropagation();
  }, true);
}

/**
 * A press, beside a layer whose press goes on to the page (pressGoesOn), on a control that has that layer open: the
 * one it was opened from (usePopupOpener), while it does not say it is closed, or one that says it has a popup open
 * (aria-expanded) and opened no other layer open now — a block's 💬 mark beside the card its words opened. It is that
 * control's own open and close, as Notion's 💬 mark pressed again closes the block's card, opened from the mark or
 * from its words, the focus on the mark and the letter typed next going nowhere (2026-11-06; 2026-11-27 in Chrome
 * 1000 wide, dpr 1, two tabs each at different times, judged after the press by the screen and one typed letter).
 * The layer closes and the press goes no further, its click neither, so the control does not open it again — as
 * React Aria's dismissal stops a press beside a layer it does not go on from; the press still focuses the control.
 * Whether it is is returned.
 */
export function pressOnOpener(kept: { opener: HTMLElement | null }, event: PointerEvent): boolean {
  const control = event.target instanceof Element ? event.target.closest<HTMLElement>(OPENS) : null;
  if (!control || control.isContentEditable) return false;
  const expanded = control.getAttribute("aria-expanded");
  if (control === kept.opener ? expanded === "false" : expanded !== "true" || openers.has(control)) return false;
  event.stopPropagation();
  cutting = control;
  return true;
}
