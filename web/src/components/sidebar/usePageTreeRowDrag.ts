import { type RefObject, useLayoutEffect, useRef } from "react";
import { combine } from "@atlaskit/pragmatic-drag-and-drop/combine";
import { PAGE_DRAG_TYPE } from "@/components/dndTypes";
import { dragCopies } from "@/lib/dragCopyKey";
import { draggableByPointer, dropTargetByPointer } from "@/lib/pointerDrag";
import { markHoverStale } from "@/lib/staleHover";
import { pageTreeDropIntent, treePageDragData, treePageDragId, type PageTreeDropIntent } from "./pageTreeModel";
import { dropSpotAt, favoriteRowData, isFavoriteRowDrag, registerDropTaker, spotTaken } from "./sidebarDropMap";

export { isFavoriteRowDrag };

type DragData = Record<string | symbol, unknown>;

// What a row's drop target carries: where on it the pointer last chose, and whether the page is already there.
const INTENT = "leafen-tree-drop-intent";
const OWN_PLACE = "leafen-tree-drop-own-place";

/**
 * A part of the sidebar that takes a page dragged to its top: a section's
 * heading and its "모두 보기", a teamspace's name — where the sidebar's
 * drop map (sidebarDropSpot) says the drag is at its head. The favorites
 * (`fallback`) also take a page no row of theirs takes, under the pointer
 * or meant by it — a locked page too, and their own subpage's row (seen
 * 2026-11-11, 2026-11-13 and 2026-11-27, two tabs at two times) —, adding
 * it at their top, staying where it is; a teamspace's name moves it to the
 * top of that teamspace, the private and shared pages to theirs.
 */
export function usePageSectionDrop(element: HTMLElement | null, add: { accepts(pageId: string): boolean; drop(pageId: string): void },
  over: (active: boolean) => void, fallback = false) {
  const latest = useRef(add);
  latest.current = add;
  useLayoutEffect(() => {
    if (!element) return;
    const own = (dropTargets: readonly { element: Element }[]) => dropTargets[0]?.element === element;
    const takes = (data: DragData) => {
      const pageId = treePageDragId(data);
      return pageId !== null && !isFavoriteRowDrag(data) && latest.current.accepts(pageId);
    };
    const drop = (data: DragData) => {
      over(false);
      const pageId = treePageDragId(data);
      if (pageId !== null) latest.current.drop(pageId);
    };
    return combine(
      registerDropTaker(element, () => ({ accepts: takes, over: () => over(true), leave: () => over(false), drop })),
      dropTargetByPointer({
        element,
        canDrop: ({ source, input }) => {
          if (!takes(source.data)) return false;
          const spot = dropSpotAt(element, input, source.data);
          if (spot?.element === element) return true;
          return fallback && (!spot || (element.contains(spot.element) && !spotTaken(spot, source.data, dragCopies(input))));
        },
        onDrag: ({ location }) => over(own(location.current.dropTargets)),
        onDragLeave: () => over(false),
        onDrop: ({ source, location }) => {
          if (own(location.current.dropTargets)) drop(source.data);
          else over(false);
        },
      }),
    );
  }, [element, over, fallback]);
}

/** What a sidebar row does with a drag, from its page's latest state. */
export type PageTreeRowDrag = {
  canDrag(copy: boolean): boolean;
  accepts(data: DragData, intent: PageTreeDropIntent, copy: boolean): boolean;
  over(data: DragData, intent: PageTreeDropIntent, copy: boolean): void;
  leave(): void;
  drop(data: DragData, intent: PageTreeDropIntent, copy: boolean): void;
  dragging(active: boolean): void;
};

/**
 * A sidebar row's drags: the row carries its page, and takes a page (before,
 * inside or after it, by where the pointer is) or blocks dragged from a page
 * (into it). Alt (Option) held as it is let go copies instead (dragCopyKey).
 * A mouse's or a pen's drag of the row follows the pointer's own events
 * (pointerDrag), a finger's and a page's blocks' are the browser's. A row
 * being renamed stays undraggable, so its text still selects.
 */
export function usePageTreeRowDrag(rowRef: RefObject<HTMLElement | null>, pageId: string, shown: boolean, movable: boolean, favorite = false) {
  const rowDrag = useRef<PageTreeRowDrag | null>(null);
  useLayoutEffect(() => {
    const element = rowRef.current;
    if (!element) return;
    // A page lands where the sidebar's drop map says (sidebarDropSpot), on this row when it says so; blocks by the row's thirds.
    const spotAt = (input: { clientX: number; clientY: number }, data: DragData) => {
      if (treePageDragId(data) === null) {
        const rect = element.getBoundingClientRect();
        return { element, intent: pageTreeDropIntent(input.clientY, rect.top, rect.height), ownPlace: false };
      }
      return dropSpotAt(element, input, data);
    };
    // Where on the row the pointer chose, read as it moves: let go without a move, the drop keeps it,
    // though the row moved from under the pointer since (a section sorted again by someone's write).
    const chosen = (self: { data: DragData }) => self.data[INTENT] as PageTreeDropIntent;
    return combine(
      registerDropTaker(element, () => rowDrag.current),
      draggableByPointer({
        element,
        canDrag: ({ input }) => rowDrag.current?.canDrag(input.altKey) ?? false,
        getInitialData: () => (favorite ? favoriteRowData(treePageDragData(pageId)) : treePageDragData(pageId)),
        getInitialDataForExternal: () => ({ [PAGE_DRAG_TYPE]: pageId, "text/plain": pageId }),
        onDragStart: () => rowDrag.current?.dragging(true),
        onDrop: () => { rowDrag.current?.dragging(false); markHoverStale(element); },
      }),
      dropTargetByPointer({
        element,
        canDrop: ({ source, input }) => {
          const spot = spotAt(input, source.data);
          return spot?.element === element && (rowDrag.current?.accepts(source.data, spot.intent, dragCopies(input)) ?? false);
        },
        getData: ({ input, source }) => {
          const spot = spotAt(input, source.data);
          return { [INTENT]: spot?.intent ?? "inside", [OWN_PLACE]: spot?.ownPlace === true };
        },
        getDropEffect: ({ input }) => dragCopies(input) ? "copy" : "move",
        onDragEnter: ({ source, location, self }) => rowDrag.current?.over(source.data, chosen(self), dragCopies(location.current.input)),
        onDrag: ({ source, location, self }) => rowDrag.current?.over(source.data, chosen(self), dragCopies(location.current.input)),
        onDragLeave: () => rowDrag.current?.leave(),
        // Where the page already is, it lit and lands nowhere.
        onDrop: ({ source, location, self }) => (self.data[OWN_PLACE] ? rowDrag.current?.leave()
          : rowDrag.current?.drop(source.data, chosen(self), dragCopies(location.current.input))),
      }),
    );
  }, [favorite, pageId, rowRef, shown]);
  useLayoutEffect(() => {
    if (rowRef.current) rowRef.current.draggable = movable;
  });
  return rowDrag;
}
