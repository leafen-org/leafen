import { createFocusManager } from "@react-aria/focus";
import { ariaHideOutside } from "@react-aria/overlays";

// Native policy excludes explicitly unavailable controls. React Aria owns
// visibility, inert/disabled/shadow DOM traversal, Tab order and wrapping.
const available = (node: Element) => node.getAttribute("aria-disabled") !== "true";

export function trapModalTab(event: {
  key: string; shiftKey: boolean; defaultPrevented?: boolean; preventDefault(): void;
}, root: HTMLElement | null) {
  if (event.key !== "Tab" || event.defaultPrevented || !root) return false;
  const manager = createFocusManager({ current: root }, { tabbable: true, accept: available });
  event.preventDefault();
  const next = event.shiftKey ? manager.focusPrevious({ wrap: true }) : manager.focusNext({ wrap: true });
  if (!next) root.focus({ preventScroll: true });
  return true;
}

// What Aria keeps open over a modal (a toast, a live announcer) stays so.
const KEPT = "[data-live-announcer], [data-react-aria-top-layer]";

// The page behind a modal window, or behind a phone's drawer, is shut by the
// backdrop over it, the window's Tab ring and aria-hidden, not by inert: a box
// turned inert has the style of everything in it worked out again (2026-11-13
// in iPhone Safari: 289–305 ms on a page of 38k elements each time a window,
// a sheet or the drawer opened or closed; aria-hidden 0 ms), and Notion's
// phone drawer, search and sheets leave the page as it is under a backdrop
// (seen 2026-11-13). What inert did besides is done here: the focus leaves
// the page as it is shut, and a focus sent there while it is shut goes back
// to where it was.
const shutters: Array<(node: Element) => boolean> = [];

/** Whether a node is shut from the focus: inert, or behind an open modal window or drawer. */
export function shutBehindModal(node: Element) {
  return Boolean(node.closest("[inert]")) || shutters.some(shut => shut(node));
}

/** Keeps the focus out of what `shut` names while it lasts; returns what ends it. */
export function keepFocusOut(shut: (node: Element) => boolean): () => void {
  shutters.push(shut);
  const active = document.activeElement;
  if (active instanceof HTMLElement && active !== document.body && shut(active)) active.blur();
  const onFocus = (event: FocusEvent) => {
    const target = event.target;
    if (!(target instanceof HTMLElement) || !shut(target)) return;
    const back = event.relatedTarget;
    if (back instanceof HTMLElement && back.isConnected && !shutBehindModal(back)) back.focus({ preventScroll: true });
    else target.blur();
  };
  document.addEventListener("focusin", onFocus, true);
  return () => {
    document.removeEventListener("focusin", onFocus, true);
    shutters.splice(shutters.indexOf(shut), 1);
  };
}

/**
 * Keeps the focus in a part of the page that lies over the rest without being
 * a layer of its own — a phone's drawer — and in the layers drawn over it
 * since (its sheets and menus): all else, the page and the layers already
 * drawn when it opened (a row's page opened whole) among them, is shut, and
 * Tab and Shift+Tab go round its controls and those layers', from the first
 * after the last and back, as a modal window's ring does. What Aria keeps
 * open stays so; a modal window opened over it keeps its own ring
 * (overlayLayer.ts), as it shuts the drawer in turn.
 */
export function holdFocusIn(owner: HTMLElement): () => void {
  const box = document.querySelector<HTMLElement>("[data-overlay-root]");
  const before = new Set(box ? [...box.children] : []);
  const under = [...before].filter((child): child is HTMLElement => child instanceof HTMLElement
    && !child.matches(KEPT) && !child.querySelector(KEPT));
  const shut = (node: Element) => !owner.contains(node) && !node.closest(KEPT)
    && (!box?.contains(node) || under.some((layer) => layer.contains(node)));
  // Hidden from assistive tools as the page beside it is (AppShell's aria-hidden).
  const hidden = under.filter((layer) => !layer.hasAttribute("aria-hidden"));
  for (const layer of hidden) layer.setAttribute("aria-hidden", "true");
  const stopKeeping = keepFocusOut(shut);
  const part = (root: HTMLElement) => createFocusManager({ current: root }, { tabbable: true, accept: available });
  // After the controls' own keys, which a menu keeps to itself (keepLayerKeys).
  const onKey = (event: KeyboardEvent) => {
    if (event.key !== "Tab" || event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey) return;
    if (shutters.some((other) => other !== shut && other(owner))) return;
    const ring = [owner, ...(box ? [...box.children] : []).filter((child): child is HTMLElement => child instanceof HTMLElement && !before.has(child))];
    const active = document.activeElement;
    const at = ring.findIndex((layer) => layer.contains(active));
    if (at < 0 && active && active !== document.body && !shut(active)) return;
    event.preventDefault();
    const back = event.shiftKey;
    if (at >= 0 && (back ? part(ring[at]).focusPrevious() : part(ring[at]).focusNext())) return;
    const from = at >= 0 ? at : back ? 0 : -1;
    for (let step = 1; step <= ring.length; step += 1) {
      const layer = ring[(from + (back ? -step : step) + ring.length * 2) % ring.length];
      if (back ? part(layer).focusLast() : part(layer).focusFirst()) return;
    }
  };
  document.addEventListener("keydown", onKey);
  return () => {
    document.removeEventListener("keydown", onKey);
    stopKeeping();
    for (const layer of hidden) layer.removeAttribute("aria-hidden");
  };
}

/**
 * Shuts all but a modal window and what it owns from the keys, the pointer
 * and assistive tools. The layers already drawn in the box at the body's end
 * that floating layers are drawn in (overlayRoot.ts) under it turn inert
 * (they are small); the box itself stays open to the focus: a menu opened
 * over the window is drawn into the box before it is a layer of its own, and
 * a field it focuses as it is drawn could take no focus in a shut box
 * (2026-11-13: the CSV window's type menu kept the focus on its button, so
 * Escape closed nothing and the next press only closed the menu).
 */
export function isolateBodyForModal(ownedNodes: Array<HTMLElement | null>) {
  const owned = ownedNodes.filter((node): node is HTMLElement => node instanceof HTMLElement);
  if (!owned.length) return () => {};
  const box = document.querySelector<HTMLElement>("[data-overlay-root]");
  const holds = (node: Element) => owned.some((part) => part.contains(node) || node.contains(part));
  const shut = box ? [...box.children].filter((child): child is HTMLElement => child instanceof HTMLElement && !child.inert
    && !child.matches(KEPT) && !child.querySelector(KEPT) && !holds(child)) : [];
  for (const child of shut) child.inert = true;
  // Hidden from assistive tools as Aria does by default: a window in the box
  // hides the rest of it as well; one drawn in the page leaves the box shown.
  const restore = ariaHideOutside(!box || owned.some((node) => box.contains(node)) ? owned : [...owned, box]);
  const open = box ? [...owned, box] : owned;
  const stopKeeping = keepFocusOut((node) => !open.some((part) => part.contains(node)) && !node.closest(KEPT));
  return () => {
    stopKeeping();
    restore();
    for (const child of shut) child.inert = false;
  };
}
