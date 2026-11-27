import { type RefObject, useEffect, useLayoutEffect, useRef } from "react";
import { createFocusManager, getFocusableTreeWalker } from "@react-aria/focus";
import { focusComingBack } from "./focusThroughWaits";
import { keyCameLast } from "./lastPress";
import { fieldFocusOnOpen, raisesKeyboard } from "./screenKind";

/**
 * Focus enters a view that replaced another in place or drew its content
 * late (2026-10-28: a dialog still checking, a sent reset link and a reset's
 * result left the focus on the page). When `view` changes and no control
 * inside `scope` holds the focus, the scope's first control takes it; with
 * none drawn yet, the next render tries again. A control that stayed and kept
 * the focus keeps it, and a field a view focuses itself is left alone, as is
 * one its request locked, which gets it back (focusThroughWaits.ts). On a
 * touch screen a first control that is a field waits for a tap, the scope
 * itself taking the focus, as a window's does (fieldFocusOnOpen).
 *
 * Going back to a view shown before, the control the person last pressed
 * there — the one that opened the view now left — takes the focus again,
 * before its first control or a field it focuses itself, with the
 * keyboard's ring only when a key came last: the app's first screen's
 * "로그인" after its sign-in's "뒤로", a sign-in step's way after its
 * "뒤로" (2026-11-16 — the focus went to the first screen's first button).
 * Going back is coming to a view on the way that led here; entering a view
 * again from the one it went back to is going forward, and the view's own
 * field or first control takes the focus as the first time (2026-11-18: the
 * sign-in code step entered again after its "뒤로" focused that "뒤로", and
 * the code typed next went nowhere).
 * Notion's own in-place views do so: the person picker's "초대와 멘션" ←
 * gives the focus back to the '"주소" 초대' row that opened it (seen in
 * Chrome 2026-11-14 and 2026-11-15). Drawn anew, that control is found by
 * its place among the view's controls and its name; one gone leaves the
 * entry above.
 *
 * `enterFirst: false` leaves the first view as the page opens it.
 */
export function useViewEntryFocus(scope: RefObject<HTMLElement | null>, view: string,
  { enabled = true, enterFirst = true }: { enabled?: boolean; enterFirst?: boolean } = {}) {
  const entered = useRef(enterFirst ? "" : view);
  // The view drawn now, the last control pressed in it, and the one each view was left from.
  const shown = useRef(view);
  const pressed = useRef<PressedControl | null>(null);
  const leftFrom = useRef(new Map<string, PressedControl>());
  // The views on the way here, the first first: coming to one is going back.
  const way = useRef<string[]>([]);
  useEffect(() => {
    if (!enabled) return undefined;
    // In the capture phase, before the press's own handler draws the next view.
    const press = (event: MouseEvent) => {
      const root = scope.current;
      const target = event.target instanceof Element ? event.target : null;
      if (!root || !target || !root.contains(target)) return;
      // A pointer's click is its target's (WebKit leaves the focus where it
      // was); a key's — or a field's Enter, which clicks its form's button —
      // is the focused control's.
      const focused = document.activeElement instanceof HTMLElement && root.contains(document.activeElement) ? document.activeElement : null;
      const control = event.detail > 0 ? target.closest<HTMLElement>(CONTROL) : focused;
      if (control && control !== root && root.contains(control)) pressed.current = { view: shown.current, ...placeOf(root, control) };
    };
    document.addEventListener("click", press, true);
    return () => document.removeEventListener("click", press, true);
  }, [enabled, scope]);
  // Every render: content that comes later takes the focus in.
  useLayoutEffect(() => {
    if (!enabled) return undefined;
    if (shown.current !== view) {
      const left = shown.current;
      const last = pressed.current;
      if (last?.view === left) leftFrom.current.set(left, last);
      else leftFrom.current.delete(left);
      pressed.current = null;
      shown.current = view;
      const back = way.current.lastIndexOf(view);
      if (back >= 0) way.current.length = back;
      else way.current.push(left);
      // Before the frame is drawn, so a field the view focuses itself shows no focus first.
      const root = scope.current;
      const opener = back >= 0 && root?.isConnected ? controlAt(root, leftFrom.current.get(view)) : null;
      if (opener) {
        opener.focus({ preventScroll: true, focusVisible: keyCameLast() } as FocusOptions);
        entered.current = view;
        return undefined;
      }
    }
    if (entered.current === view) return undefined;
    const frame = window.requestAnimationFrame(() => {
      const root = scope.current;
      if (!root?.isConnected) return;
      const active = document.activeElement;
      if (!active || active === root || !root.contains(active)) {
        if (!focusComingBack(root) && !enter(scope, root)) return;
      }
      entered.current = view;
    });
    return () => window.cancelAnimationFrame(frame);
  });
}

const CONTROL = 'button, a[href], input, select, textarea, summary, [role="button"], [role="tab"], [role="switch"], [role="menuitem"], [tabindex]';

/** A control pressed in a view: its place among the view's controls and its name. */
type PressedControl = { view: string; index: number; name: string };

function controlsIn(root: HTMLElement): HTMLElement[] {
  const walker = getFocusableTreeWalker(root, { tabbable: true });
  const found: HTMLElement[] = [];
  for (let node = walker.nextNode(); node; node = walker.nextNode()) found.push(node as HTMLElement);
  return found;
}

function nameOf(control: HTMLElement) {
  return `${control.tagName}|${control.getAttribute("aria-label") ?? control.textContent?.trim() ?? ""}`;
}

function placeOf(root: HTMLElement, control: HTMLElement) {
  return { index: controlsIn(root).indexOf(control), name: nameOf(control) };
}

/** The control standing where the pressed one stood, by the same name; else the one of that name. */
function controlAt(root: HTMLElement, place: PressedControl | undefined): HTMLElement | null {
  if (!place) return null;
  const controls = controlsIn(root);
  const there = controls[place.index];
  if (there && nameOf(there) === place.name && !there.matches(":disabled")) return there;
  const named = controls.filter((control) => nameOf(control) === place.name && !control.matches(":disabled"));
  return named.length === 1 ? named[0] : null;
}

function enter(scope: RefObject<HTMLElement | null>, root: HTMLElement) {
  if (!fieldFocusOnOpen() && raisesKeyboard(getFocusableTreeWalker(root, { tabbable: true }).nextNode() as Element | null)) {
    if (!root.hasAttribute("tabindex")) root.tabIndex = -1;
    root.focus({ preventScroll: true });
    return true;
  }
  return Boolean(createFocusManager(scope).focusFirst({ tabbable: true }));
}
