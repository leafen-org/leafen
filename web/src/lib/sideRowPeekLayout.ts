import { useEffect, useSyncExternalStore, type RefObject } from "react";

export const SIDE_ROW_PEEK_LAYOUT_EVENT = "leafen:side-row-peek-layout-change";

/** A side peek's place: whether one is open, how wide, and the item it shows. */
export type SideRowPeekLayoutDetail = {
  open: boolean;
  width?: number;
  itemId?: string;
};

type SideRowPeekPlace = Omit<SideRowPeekLayoutDetail, "open">;

// The side peeks beside the page now, by their panels, and the order each was
// first placed in: the page gives its room to the last one opened, and as it
// goes to the one it was opened over — a row opened from a view in a side
// peek (`pp`) stands over that peek, which shows again as the way back takes
// it away. Told by the last one alone, the page took its room back as the one
// over another went, the one under it still beside it, and the page's views
// laid out under it again (seen 2026-11-20: the page 454 to 1170 wide, its
// databases' tools all drawn under the peek). A peek placed again — its width
// or item changed, or back after a press that kept it — keeps its order, so
// the one under another never comes over it.
const places = new Map<object, SideRowPeekPlace>();
const placedOrder = new WeakMap<object, number>();
let placedCount = 0;

// The latest, for a view drawn after the peek opened.
let current: SideRowPeekLayoutDetail = { open: false };

/** Tells the page where `owner`'s side peek stands beside it, or (null) that it is not beside it. */
export function announceSideRowPeekLayout(owner: object, place: SideRowPeekPlace | null) {
  if (place) {
    if (!placedOrder.has(owner)) placedOrder.set(owner, ++placedCount);
    places.set(owner, place);
  } else {
    places.delete(owner);
  }
  let top: SideRowPeekPlace | null = null, topOrder = 0;
  for (const [each, eachPlace] of places) {
    const order = placedOrder.get(each) ?? 0;
    if (order > topOrder) [top, topOrder] = [eachPlace, order];
  }
  tell(top ? { open: true, ...top } : { open: false });
}

function tell(next: SideRowPeekLayoutDetail) {
  current = next;
  window.dispatchEvent(new CustomEvent<SideRowPeekLayoutDetail>(SIDE_ROW_PEEK_LAYOUT_EVENT, { detail: current }));
}

/**
 * Takes every side peek's place away at once and answers what they were: a
 * press closing the one in front ("»", Escape, a press beside it) closes the
 * peek beside the page, those it was opened over too, and the page widens as
 * it slides out; one that stays is put back (sidePeekMotion.ts).
 */
export function withdrawSideRowPeeks(): Array<[object, SideRowPeekPlace]> {
  const was = [...places];
  places.clear();
  tell({ open: false });
  return was;
}

/**
 * The page makes room for a side peek beside it and its views mark the item
 * it shows (Notion's, seen 2026-11-03), from the panel's place while it is
 * beside the page (`side`).
 */
export function useSideRowPeekPlace(panelRef: RefObject<HTMLElement | null>, side: boolean, width: number, itemId: string) {
  useEffect(() => {
    const panel = panelRef.current;
    if (!side || !panel) return undefined;
    announceSideRowPeekLayout(panel, { width, itemId });
    return () => announceSideRowPeekLayout(panel, null);
  }, [panelRef, side, width, itemId]);
}

export function sideRowPeekLayoutFromEvent(event: Event): SideRowPeekLayoutDetail {
  if (!(event instanceof CustomEvent) || event.detail?.open !== true) return { open: false };
  return event.detail as SideRowPeekLayoutDetail;
}

function subscribe(onChange: () => void) {
  window.addEventListener(SIDE_ROW_PEEK_LAYOUT_EVENT, onChange);
  return () => window.removeEventListener(SIDE_ROW_PEEK_LAYOUT_EVENT, onChange);
}

/**
 * Whether the side peek shows this item, which its views mark as Notion's do
 * (seen 2026-11-03): each item asks this alone, so the peek opening or moving
 * draws again only the items it changes.
 */
export function useSideRowPeekShows(itemId: string): boolean {
  return useSyncExternalStore(subscribe, () => current.open && current.itemId === itemId, () => false);
}
