import { placeAround, popoverRectAt, type PopoverPlacement, type PopoverRequest, type PopoverSize, type PopoverViewport } from "./popoverPlacement";
import { inlineStartX } from "./readingDirection";

/**
 * The windows that make a linked view — its source panel ("새 데이터베이스"),
 * its "새 보기" and the view settings a chosen view opens — stand one after
 * another where the one before stood, as one Notion window whose screens
 * change (seen 2026-11-08 in 723 and 779 high windows): on the same side of
 * the database's top row, its foot where the last one's was over the row
 * (447, 629, 428) or its top where it was under it (207, 227), the page
 * never scrolled for them; one too tall for that side moves into the window
 * whole on it (12..635 over a row at 436, 143..766 under one at 190), and
 * the "⚙" button reads pressed meanwhile.
 */
export type LinkedViewWindowPlace = { side: "above" | "below"; edge: number; left: number };

// One making goes on at a time, so the last window's place is the one kept,
// a short while: the next opens once the link or the chosen view is saved,
// the source panel's block replaced by the linked view's own by then.
const KEPT_FOR = 30_000;
let kept: (LinkedViewWindowPlace & { at: number }) | null = null;

/** Keeps where a window making a linked view stands now, for the one after it. */
export function keepLinkedViewWindowPlace(element: HTMLElement | null, placement: PopoverPlacement | null) {
  if (!element || !placement || (placement.side !== "above" && placement.side !== "below")) return;
  const rect = element.getBoundingClientRect();
  // `left` is where it starts: its right where the screen reads from the right.
  kept = { side: placement.side, edge: placement.side === "above" ? rect.bottom : rect.top, left: inlineStartX(rect, 0, element), at: Date.now() };
}

/** Where the last window making a linked view stood, while it is fresh. */
export function linkedViewWindowPlace(): LinkedViewWindowPlace | null {
  if (!kept || Date.now() - kept.at > KEPT_FOR) return null;
  return { side: kept.side, edge: kept.edge, left: kept.left };
}

export function forgetLinkedViewWindowPlace() {
  kept = null;
}

/**
 * The next window, where the last stood. The view settings and a new view's
 * window stand there no taller than `cap`, four fifths of the window as
 * anywhere (toolbarListFit), scrolling inside: Notion's, seen 2026-11-18 in
 * a 723 window in two tabs — "+ 새 표 보기"'s "새 보기" and a picked view's
 * "설정 보기" both 578.4 high at 132.2..710.6 after a "새 보기" at 346, their
 * 536.4 bodies scrolling 599 — and 2026-11-08 in a 779 one (623 high).
 */
export function placeAtLinkedViewWindow(place: LinkedViewWindowPlace, size: PopoverSize, viewport: PopoverViewport, cap?: number): PopoverRequest {
  return placeAround(popoverRectAt({ top: place.edge, bottom: place.edge, left: place.left }), size, viewport,
    { prefer: place.side, align: "start", gap: 0, margin: 12, whole: true, holdSide: true, cap });
}
