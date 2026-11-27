import { useEffect, useRef, type RefObject } from "react";
import { onReloadKeepingPlace, takeKeptPlace } from "./reloadKeepingPlace";
import { historyStep, keptHistoryStep } from "./historyStep";
import { keepPlaceAcrossWidths } from "./pageWidthPlace";

/*
 * Where a page was read, kept for each step of the tab's history: going back
 * or forward to a page shows it where it was left, as Notion's (seen
 * 2026-11-13 at 1440×723: a long page left from a sub-page's link 310px down,
 * or at its foot, came back there by the way back, and again after forward
 * and back). A link opens a page as before: its step is new and has no place.
 *
 * Each step is marked in its history state (historyStep.ts). A reload the
 * app asks for (a new build, one at rest) keeps the step's places and the
 * page comes back where it was read (2026-11-14: one at rest showed a long
 * page from its top, and the next press went into a block there).
 */
// The place of a page in a step, by `${step}:${page}`.
const places = new Map<string, number>();
// Long enough for a page's blocks to come from the server.
const SETTLE_MS = 4000;
const keptMark = keptHistoryStep, stepMark = historyStep;

if (typeof window !== "undefined") {
  const kept = takeKeptPlace<Record<string, unknown>>("pageScroll");
  for (const [key, top] of Object.entries(kept ?? {})) if (typeof top === "number") places.set(key, top);
  onReloadKeepingPlace("pageScroll", () => {
    const mark = keptMark();
    const step = mark ? [...places].filter(([key]) => key.startsWith(`${mark}:`)) : [];
    return step.length ? Object.fromEntries(step) : null;
  });
}

/**
 * The page's scroller, which keeps the page's place in each step of the
 * history and takes it back when the way back or forward returns to it. The
 * place is held while the page's blocks come — till a turn of the wheel, a
 * press or a key — and an address to a block leaves the scroll to it. The
 * block it is read at stays put as its width changes (pageWidthPlace.ts).
 */
export function usePageScrollPlace<T extends HTMLElement>(pageId: string): RefObject<T | null> {
  const ref = useRef<T>(null);
  useEffect(() => {
    const scroller = ref.current;
    return scroller ? keepPlaceAcrossWidths(scroller) : undefined;
  }, [pageId]);
  useEffect(() => {
    const scroller = ref.current;
    if (!scroller) return;
    const mark = keptMark();
    let settling = !mark || window.location.hash ? undefined : places.get(`${mark}:${pageId}`);
    const remember = () => { if (settling === undefined) places.set(`${stepMark()}:${pageId}`, scroller.scrollTop); };
    scroller.addEventListener("scroll", remember, { passive: true });
    if (settling === undefined) return () => scroller.removeEventListener("scroll", remember);
    const sizes = new ResizeObserver(() => place());
    const children = new MutationObserver(() => { for (const child of scroller.children) sizes.observe(child); });
    const end = () => {
      settling = undefined;
      sizes.disconnect(); children.disconnect(); window.clearTimeout(timer);
      for (const type of ["wheel", "pointerdown", "keydown", "touchstart"]) window.removeEventListener(type, settled, true);
    };
    // Held no longer, the page is where it stands now.
    const settled = () => { if (settling === undefined) return; end(); remember(); };
    // Held there while the page is drawn again (its blocks read once more
    // after what was kept of them showed), not only until it first fits.
    function place() {
      if (settling === undefined) return;
      const room = Math.max(0, scroller!.scrollHeight - scroller!.clientHeight);
      if (Math.abs(scroller!.scrollTop - Math.min(settling, room)) > 1) scroller!.scrollTop = Math.min(settling, room);
    }
    const timer = window.setTimeout(settled, SETTLE_MS);
    for (const type of ["wheel", "pointerdown", "keydown", "touchstart"]) window.addEventListener(type, settled, { capture: true, passive: true });
    for (const child of scroller.children) sizes.observe(child);
    children.observe(scroller, { childList: true });
    place();
    return () => {
      end();
      scroller.removeEventListener("scroll", remember);
    };
  }, [pageId]);
  return ref;
}
