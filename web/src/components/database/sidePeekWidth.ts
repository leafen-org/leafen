import { useLayoutEffect, type RefObject } from "react";
import { deviceValue, storeDeviceValue } from "@/lib/browserStorage";

/**
 * A side peek's width, shared by a database page's and a page's peeks (as
 * Notion's, which keep one width): from 564px to two thirds of the window,
 * or 564px where that is less, and never past the window, kept in this
 * browser, as Notion's (seen 2026-11-11, 720 kept: 564 at 800 wide, 600 at
 * 900, 703 at 1054, 720 at 1280 and 1600; dragged at 1191: 564 to 794;
 * 2026-11-18 in Chrome at dpr 1, 960 kept: 682.66 at 1024 — two thirds to the
 * fraction —, 600 at 900, 564 at 800, 768, 640 and 600, the page behind 76
 * and 36 wide, and 520 at 520, the whole window). Until one is kept it is
 * Notion's: half the window as the peek opens, to the half pixel, 564px at
 * least, with the sidebar beside it or not, and opening keeps nothing (seen
 * 2026-11-01: 564 at 1000 and 1100 wide, 640 at 1280, 800 at 1600, 1100 at
 * 2200; 2026-11-05: 720 at 1440; 2026-11-11: 595.5 at 1191). An open peek
 * keeps its width as the window narrows, over the whole window where that is
 * less, and fits again only as it opens, as Notion's (seen 2026-11-18, 960 at
 * 1440 narrowed to 1200, 1024, 900 and 640: 960, 960, 900, 640; at 1024 again
 * 960) — the shell gives it its room up to the window (AppShell). A center
 * peek's width keeps room for a scrollbar (below).
 */

const ROW_PEEK_WIDTH_KEY = "leafen:row-peek-side-width:v2";
export const ROW_PEEK_FALLBACK_WIDTH = 640;

// Its words stand 167px in all, and with less than this left a page's columns
// go one under another, as Notion's (seen 2026-11-03: at 457 they do, at 614.5
// they stand side by side; the width between wasn't measured).
const COLUMNS_STACK_AT_TEXT_WIDTH = 500;
export function sidePeekStacksColumns(width: number) {
  return width - 167 <= COLUMNS_STACK_AT_TEXT_WIDTH;
}
const ROW_PEEK_FIRST_WIDTH = 564;

export function rowPeekMaxWidth() {
  if (typeof window === "undefined") return ROW_PEEK_FALLBACK_WIDTH;
  return Math.min(window.innerWidth, Math.max(ROW_PEEK_FIRST_WIDTH, window.innerWidth * 2 / 3));
}

/** The narrowest it drags to: 564px, or its widest where that is less. */
export function rowPeekMinWidth() {
  return Math.min(ROW_PEEK_FIRST_WIDTH, rowPeekMaxWidth());
}

export function clampRowPeekWidth(width: number) {
  return Math.min(Math.max(Math.round(width), rowPeekMinWidth()), rowPeekMaxWidth());
}

// A side peek's width is this browser's, for whoever uses it.
export function storedRowPeekWidth() {
  if (typeof window === "undefined") return ROW_PEEK_FALLBACK_WIDTH;
  const parsed = Number.parseInt(deviceValue(ROW_PEEK_WIDTH_KEY) ?? "", 10);
  if (Number.isFinite(parsed)) return clampRowPeekWidth(parsed);
  return Math.min(Math.max(ROW_PEEK_FIRST_WIDTH, window.innerWidth / 2), rowPeekMaxWidth());
}

/** Keeps a side peek's width for the next one; without storage, for this peek only. */
export function storeRowPeekWidth(width: number) {
  storeDeviceValue(ROW_PEEK_WIDTH_KEY, String(width));
}

// The room the platform's scrollbar takes beside a page, read once: none
// where scrollbars are laid over the page.
let platformScrollbar: number | undefined;
function platformScrollbarWidth() {
  if (platformScrollbar !== undefined) return platformScrollbar;
  const probe = document.createElement("div");
  probe.className = "nscroll";
  probe.style.cssText = "position: absolute; inset-block-start: -200px; inline-size: 100px; block-size: 100px; overflow-y: scroll; visibility: hidden";
  document.body.append(probe);
  platformScrollbar = Math.max(0, probe.offsetWidth - probe.clientWidth);
  probe.remove();
  return platformScrollbar;
}

/**
 * Keeps the platform's scrollbar width on a center peek's panel
 * (`--center-peek-scrollbar`), so the window is 960px wide and the room a
 * scrollbar takes more, whether its page scrolls or not, its page's column
 * giving that room up while the scrollbar shows, as Notion's (seen 2026-11-13
 * at 1024, 1280 and 1440 wide: the window less 144 at 1024, then 975 for an
 * empty page, a long one and a row's alike, the column 723 wide and 708 beside
 * a 15px scrollbar); a scrollbar laid over the page takes none.
 */
export function useCenterPeekScrollbar(panelRef: RefObject<HTMLElement | null>, center: boolean) {
  useLayoutEffect(() => {
    const panel = panelRef.current;
    if (!center || !panel) return undefined;
    panel.style.setProperty("--center-peek-scrollbar", `${platformScrollbarWidth()}px`);
    return () => { panel.style.removeProperty("--center-peek-scrollbar"); };
  }, [panelRef, center]);
}
