import type { CSSProperties, RefObject } from "react";
import {
  placeAround,
  placeBeside,
  type PopoverRect,
  popoverPlacementStyle,
  popoverRectOf,
  usePopoverPlacement,
} from "@/lib/popoverPlacement";

// Where a move popover sits, as Notion's "옮기기" (seen 2026-09-30): under
// what opened it — the page menu's button or the block — or over it where
// there's no room below, shortened where neither side has room for all of it.

/**
 * What it hangs from; `beside`, at the start side of a menu's row, as Notion's "다음으로 이동:" picker opens by its menu (seen 2026-11-11);
 * `whole`, shown whole however it was opened (useMovePopoverStyle), as a page block's move window (editor/PageBlockMoveDialog.tsx).
 */
export type MovePopoverAnchor = PopoverRect & { beside?: boolean; whole?: boolean };

const MARGIN = 12;
const GAP = 4;

/** The anchor of an element on the page, for a popover it opens. */
export function movePopoverAnchorOf(element: Element | null | undefined): MovePopoverAnchor | null {
  return popoverRectOf(element);
}

/** The popover's place, measured again as its content changes size. */
export function useMovePopoverStyle(
  anchor: MovePopoverAnchor | null | undefined,
  popoverRef: RefObject<HTMLElement | null>,
  contentKey: unknown,
  gap = GAP,
  /** The most it grows to, where it has more room. */
  cap?: number,
  /**
   * Shown whole, as Notion's block move list (seen 2026-11-14 in a 723
   * window): under its line where that holds it, else over it where that
   * does, else under it moved up into the window over the line (291..711
   * under a line at 392..432), not cut short over it.
   */
  whole = false,
): CSSProperties | undefined {
  // Lined up with what opened it on the left, slid back 12px in from the
  // window's edge where it would run off, as Notion's (seen 2026-11-13: under
  // the page's "…" at 1098 and under a new page's window's "…" at 1098.5, its
  // end past the button's). The dialog that shows it is the layer over the
  // page; this is only its place.
  const place = usePopoverPlacement(popoverRef, anchor ? (size, viewport) => anchor.beside
    ? placeBeside(anchor, size, viewport, { margin: MARGIN, gap: 0, prefer: "left", cap })
    : placeAround(anchor, size, viewport, { margin: MARGIN, gap, minHeight: 120, cap, whole: whole || anchor.whole === true },
  ) : null, [anchor, contentKey, cap, whole], undefined, false);
  return anchor ? { position: "fixed", margin: 0, ...popoverPlacementStyle(place) } : undefined;
}
