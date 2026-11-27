import { pageHref } from "@/lib/navigation";
import { replaceRoute, ROUTE_CHANGE_EVENT, type RouteMove } from "@/lib/router";

/**
 * A page made on this device opens before the server makes it
 * (pageMadeHere.ts); one the server then refuses — a term the organization's
 * data loss prevention policy holds in its name, a place no longer theirs to
 * add to — leads back to where the person was, as a refused change rolls
 * back (user decision 2026-11-12: show first, roll back one shared way if
 * the server refuses), and whoever opened it is told, to show itself again
 * as it was (the add window, its name typed). The refusal's one notice says
 * why (sendPageMadeHere); the page isn't read and isn't shown as not found
 * (2026-11-15: a name refused from the add window landed on "페이지 찾지
 * 못함", a second notice saying the page was gone, and the name typed lost).
 */

// Where each page made here was opened from, and whether its address went
// on top of that one in the history (a link's push) or in its place.
type Opening = { from: string; pushed: boolean | null };
const openings = new Map<string, Opening>();
const toldOfRefusal = new Map<string, Set<() => void>>();
// Pages refused lately, for whoever asks to be told after the refusal came.
const refused: string[] = [];

const currentAddress = () => `${window.location.pathname}${window.location.search}`;
const pagePath = (pageId: string) => pageHref(pageId);

if (typeof window !== "undefined") {
  window.addEventListener(ROUTE_CHANGE_EVENT, (event) => {
    for (const [pageId, opening] of openings) {
      if (opening.pushed === null && window.location.pathname === pagePath(pageId)) {
        opening.pushed = !(event as CustomEvent<RouteMove>).detail?.replace;
      }
    }
  });
}

/** Notes where a page made here opens from, before it opens. */
export function notePageMadeHereOpening(pageId: string) {
  openings.set(pageId, { from: currentAddress(), pushed: null });
}

/** The server made it: nothing leads back from it. */
export function forgetPageMadeHereOpening(pageId: string) {
  openings.delete(pageId);
  toldOfRefusal.delete(pageId);
}

/**
 * The server refused a page made here: shown, the person goes back to where
 * they were — back in the history when its address went on top, or that
 * address in its place — and whoever asked is told.
 */
export function leaveRefusedPageMadeHere(pageId: string) {
  const opening = openings.get(pageId);
  openings.delete(pageId);
  if (opening && window.location.pathname === pagePath(pageId)) {
    if (opening.pushed) window.history.back();
    else replaceRoute(opening.from);
  }
  refused.push(pageId);
  if (refused.length > 20) refused.shift();
  const listeners = toldOfRefusal.get(pageId);
  toldOfRefusal.delete(pageId);
  for (const listener of listeners ?? []) listener();
}

/** Tells `listener` once if the server refuses the page made here; the returned function stops it. */
export function whenPageMadeHereRefused(pageId: string, listener: () => void) {
  if (refused.includes(pageId)) {
    listener();
    return () => {};
  }
  const listeners = toldOfRefusal.get(pageId) ?? new Set<() => void>();
  listeners.add(listener);
  toldOfRefusal.set(pageId, listeners);
  return () => { listeners.delete(listener); };
}
