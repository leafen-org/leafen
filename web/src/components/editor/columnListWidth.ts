import { useLayoutEffect, useState, type CSSProperties } from "react";
import type { ContentGraph } from "@/contentGraph";
import { replaceEditorBlocks } from "./editorGraphModel";

/**
 * A column list's own width, as Notion's (seen 2026-11-24 in Chrome at 1685,
 * dark, dpr 1, two, three and four columns, in two tabs; the width read from
 * its style after each drag): its outer edges' handles widen it about the
 * page's middle, twice as far as the pointer went, to the nearest 48px —
 * multiples of 48, a full list's 1615 drawn in by 150 settling at 1296;
 * let go at the page's width or less it is the page's again; past its
 * widest it is as wide as the page's frame, edge to edge. Its widest is
 * the frame's whole width less 207px, scrollbar and all, and it is drawn no
 * wider as the window narrows (seen 2026-11-24 in two tabs: 1208 in a 1415
 * frame, 1423 in 1630, 823 in 1030, 623 in 830, 423 in 630, a 1008 or 1296
 * list drawn that wide). It holds that from the press to the release,
 * drawing nothing on the way. Stored as the list's `width`, in pixels, or
 * "full".
 */
export const LIST_STEP = 48;
/** How much narrower than the page's frame, scrollbar and all, a list is at its widest. */
const FRAME_ROOM = 207;
export type ListWidth = number | "full";

export function storedListWidth(content: Record<string, unknown> | undefined): ListWidth | null {
  const width = content?.width;
  if (width === "full") return "full";
  return typeof width === "number" && Number.isFinite(width) && width > 0 ? width : null;
}

/** The widest a list is drawn in the page scrolling in `scroll`. */
export function widestListIn(scroll: HTMLElement | null) {
  return scroll ? scroll.offsetWidth - FRAME_ROOM : Number.POSITIVE_INFINITY;
}

/** The width a drag of an outer edge leaves a list `width` wide on a page `page` wide, `outward` pixels out, `widest` at most. */
export function draggedListWidth(width: number, outward: number, page: number, widest: number): ListWidth | null {
  const snapped = Math.round((width + 2 * outward) / LIST_STEP) * LIST_STEP;
  return snapped <= page ? null : snapped > widest ? "full" : snapped;
}

export function setColumnListWidth(graph: ContentGraph, listId: string, width: ListWidth | null) {
  const list = graph.blocks.find((candidate) => candidate.id === listId);
  if (!list || list.type !== "column_list" || storedListWidth(list.content) === width) return null;
  const content: Record<string, unknown> = { ...list.content };
  if (width === null) delete content.width;
  else content.width = width;
  return replaceEditorBlocks(graph, [{ ...list, content }]);
}

/**
 * The list's box as wide as it is stored, about the page's middle, as
 * Notion's: a width in pixels no wider than its widest (widestListIn), "full"
 * the page's frame inside its scrollbar; none narrower than the page, nor on
 * a phone or where columns stack (NativeTiptap.module.css). The box is 12px
 * narrower than its columns' row, which stands 6px out on either side.
 */
export function useWideColumnList(element: HTMLElement | null, width: ListWidth | null): CSSProperties | undefined {
  const [frame, setFrame] = useState({ inside: 0, widest: 0 });
  useLayoutEffect(() => {
    const scroll = width === null ? null : element?.closest<HTMLElement>("[data-page-scroll]");
    if (!scroll) return;
    const read = () => setFrame((current) => {
      const inside = scroll.clientWidth, widest = widestListIn(scroll);
      return current.inside === inside && current.widest === widest ? current : { inside, widest };
    });
    read();
    const observer = new ResizeObserver(read);
    observer.observe(scroll);
    return () => observer.disconnect();
  }, [element, width]);
  if (width === null || !frame.inside) return undefined;
  const box = `max(100%, ${(width === "full" ? frame.inside : Math.min(width, frame.widest)) - 12}px)`;
  return { width: box, marginInline: `calc((100% - ${box}) / 2)` };
}
