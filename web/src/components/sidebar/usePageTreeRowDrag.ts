import { type RefObject, useLayoutEffect, useRef } from "react";
import { combine } from "@atlaskit/pragmatic-drag-and-drop/combine";
import { PAGE_DRAG_TYPE } from "@/components/dndTypes";
import { dragCopies } from "@/lib/dragCopyKey";
import { draggableByPointer, dropTargetByPointer } from "@/lib/pointerDrag";
import { markHoverStale } from "@/lib/staleHover";
import { pageTreeDropIntent, treePageDragData, treePageDragId, type PageTreeDropIntent } from "./pageTreeModel";

type DragData = Record<string | symbol, unknown>;

// A row of the favorites carries this too: a drag among them changes only
// their order, as Notion's (seen 2026-11-06).
const FAVORITE_ROW = "leafen-favorite-row";
export function isFavoriteRowDrag(data: DragData) {
  return data[FAVORITE_ROW] === true;
}

/**
 * A part of the sidebar that takes a page dragged over it where no row
 * inside it does, as Notion's (seen 2026-11-11 and 2026-11-13): the
 * favorites' section — over its heading or between rows, a locked page
 * too — adds it at their top, staying where it is; a teamspace's name
 * moves it to the top of that teamspace. A page a row takes goes there, as
 * before.
 */
export function usePageSectionDrop(element: HTMLElement | null, add: { accepts(pageId: string): boolean; drop(pageId: string): void },
  over: (active: boolean) => void) {
  const latest = useRef(add);
  latest.current = add;
  useLayoutEffect(() => {
    if (!element) return;
    const own = (dropTargets: readonly { element: Element }[]) => dropTargets[0]?.element === element;
    return dropTargetByPointer({
      element,
      canDrop: ({ source }) => {
        const pageId = treePageDragId(source.data);
        return pageId !== null && !isFavoriteRowDrag(source.data) && latest.current.accepts(pageId);
      },
      onDrag: ({ location }) => over(own(location.current.dropTargets)),
      onDragLeave: () => over(false),
      onDrop: ({ source, location }) => {
        over(false);
        const pageId = treePageDragId(source.data);
        if (pageId !== null && own(location.current.dropTargets)) latest.current.drop(pageId);
      },
    });
  }, [element, over]);
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
    const intentAt = (clientY: number) => {
      const rect = element.getBoundingClientRect();
      return pageTreeDropIntent(clientY, rect.top, rect.height);
    };
    return combine(
      draggableByPointer({
        element,
        canDrag: ({ input }) => rowDrag.current?.canDrag(input.altKey) ?? false,
        getInitialData: () => (favorite ? { ...treePageDragData(pageId), [FAVORITE_ROW]: true } : treePageDragData(pageId)),
        getInitialDataForExternal: () => ({ [PAGE_DRAG_TYPE]: pageId, "text/plain": pageId }),
        onDragStart: () => rowDrag.current?.dragging(true),
        onDrop: () => { rowDrag.current?.dragging(false); markHoverStale(element); },
      }),
      dropTargetByPointer({
        element,
        canDrop: ({ source, input }) => rowDrag.current?.accepts(source.data, intentAt(input.clientY), dragCopies(input)) ?? false,
        getDropEffect: ({ input }) => dragCopies(input) ? "copy" : "move",
        onDragEnter: ({ source, location }) => rowDrag.current?.over(source.data,
          intentAt(location.current.input.clientY), dragCopies(location.current.input)),
        onDrag: ({ source, location }) => rowDrag.current?.over(source.data,
          intentAt(location.current.input.clientY), dragCopies(location.current.input)),
        onDragLeave: () => rowDrag.current?.leave(),
        onDrop: ({ source, location }) => rowDrag.current?.drop(source.data,
          intentAt(location.current.input.clientY), dragCopies(location.current.input)),
      }),
    );
  }, [favorite, pageId, rowRef, shown]);
  useLayoutEffect(() => {
    if (rowRef.current) rowRef.current.draggable = movable;
  });
  return rowDrag;
}
