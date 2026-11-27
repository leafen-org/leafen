import { useEffect, type RefObject } from "react";
import { readsRightToLeft } from "@/lib/readingDirection";
import { notePointerBeside } from "@/lib/typingFade";
import { placeCommentAdd } from "./BlockCommentAdd";

/**
 * How far beside a block the pointer is still on it, as Notion's (seen
 * 2026-11-12 at 1440 and 1024, the discussions beside the page or not, a
 * page of the usual width or full): 150px before its box, where its
 * handles show, and 150px past the end of the page's column, where its
 * "💬+" shows and its handles go away; the room between a block narrower
 * than the column (discussions beside it) and the column's end is no
 * block's, nor is a discussion's card past it.
 */
const REACH = 150;
/** The page's column stands 6px outside its editor (BlockCommentAdd). */
const COLUMN_PAST_EDITOR = 6;

/** A block's row, and a column list's (the room between its columns is its own). */
const ROW = '[role="group"][data-type]';
const LIST = '[data-type="column_list"]';

/**
 * The block of a column list a pointer beside or between its columns reaches,
 * as Notion's (seen 2026-11-24 in Chrome at 1685, dark, dpr 1, two and three
 * columns, lines of different heights): in the gap between two columns, the
 * later column's block at the pointer's height, else the earlier one's (the
 * gap's line staying, the press still the gap's); before the list, the first
 * column's; past it, the last column's — its handles, no "💬+"; in a column,
 * where none of its blocks stands at that height, none, as on any other side.
 * `side` says which: "before" or "past" the list's ends, or "between"; `end`
 * is where the list ends, its last column's far edge.
 */
function columnBlockAt(list: Element, x: number, y: number, rtl: boolean): { row: HTMLElement; side: "before" | "past" | "between"; end: number } | null {
  const columns = [...list.querySelectorAll<HTMLElement>("[data-column-id]")]
    .filter((column) => column.parentElement?.closest(LIST) === list)
    .map((column) => ({ column, box: (column.closest("[data-native-column]") ?? column).getBoundingClientRect() }));
  if (!columns.length) return null;
  // Along the reading direction: a column's start and end, and the pointer.
  const start = ({ box }: { box: DOMRect }) => rtl ? -box.right : box.left, end = ({ box }: { box: DOMRect }) => rtl ? -box.left : box.right;
  const at = rtl ? -x : x, last = columns.length - 1;
  const rowIn = (index: number) => {
    const { column, box } = columns[index];
    const hit = document.elementFromPoint(box.left + box.width / 2, y);
    const row = hit && column.contains(hit) ? hit.closest<HTMLElement>(ROW) : null;
    return row && column.contains(row) ? row : null;
  };
  const listEnd = rtl ? columns[last].box.left : columns[last].box.right;
  if (at < start(columns[0])) { const row = rowIn(0); return row ? { row, side: "before", end: listEnd } : null; }
  if (at > end(columns[last])) { const row = rowIn(last); return row ? { row, side: "past", end: listEnd } : null; }
  // In a column's own room beside its blocks (6px each side), its block at that height, as Notion's blocks reach the column's edges.
  const inside = columns.findIndex((column) => at >= start(column) && at <= end(column));
  const gap = columns.findIndex((column, index) => index < last && at >= end(column) && at < start(columns[index + 1]));
  const row = inside >= 0 ? rowIn(inside) : gap < 0 ? null : rowIn(gap + 1) ?? rowIn(gap);
  return row ? { row, side: "between", end: listEnd } : null;
}

/**
 * Marks the row of the block beside which the pointer is in the page's
 * room (`data-pointer-near`, with `data-pointer-end` past the column), which
 * the block's CSS reads as it reads its hover (EditorBlockHandles). The room
 * keeps its presses: the mark only shows the block's handles or "💬+". Beside
 * and between a column list's columns, its blocks' (columnBlockAt).
 */
export function useBlockHoverReach(ref: RefObject<HTMLElement | null>) {
  useEffect(() => {
    const doc = ref.current;
    const page = doc?.parentElement;
    if (!doc || !page) return;
    let marked: HTMLElement | null = null;
    const clear = () => {
      if (!marked) return;
      delete marked.dataset.pointerNear;
      delete marked.dataset.pointerEnd;
      marked = null;
      notePointerBeside(null);
    };
    const mark = (row: HTMLElement, end: boolean) => {
      if (marked !== row) clear();
      if (marked !== row) notePointerBeside(row);
      marked = row;
      row.dataset.pointerNear = "true";
      if (end && row.dataset.pointerEnd !== "true") placeCommentAdd(row);
      if (end) row.dataset.pointerEnd = "true";
      else delete row.dataset.pointerEnd;
    };
    const move = (event: PointerEvent) => {
      const target = event.target instanceof Element ? event.target : null;
      // On a block itself, a card or anything open in the page, those keep
      // their own hover; a preview that is itself a dialog keeps its reach.
      const own = target?.closest(`${ROW}, [data-margin-discussion], [role="dialog"], [role="menu"], [role="listbox"]`);
      // The room between a column list's columns, and a column's beside its blocks, is the list's row, yet no block's.
      const between = Boolean(own?.matches(LIST));
      if (event.pointerType !== "mouse" || event.buttons || !target || !doc.contains(target) && target !== page || own && page.contains(own) && !between) {
        clear();
        return;
      }
      const editor = doc.querySelector<HTMLElement>("[data-editor-root]:not([data-editor-nested])");
      if (!editor) return clear();
      const box = editor.getBoundingClientRect();
      const rtl = readsRightToLeft(editor);
      const x = event.clientX;
      const columnEnd = rtl ? box.left - COLUMN_PAST_EDITOR : box.right + COLUMN_PAST_EDITOR;
      // The block at the pointer's height, the innermost there; a column list's, as its columns stand (columnBlockAt).
      const found = document.elementFromPoint(box.left + box.width / 2, event.clientY);
      const inner = found && editor.contains(found) ? found.closest<HTMLElement>(ROW) : null;
      // A synced block's lines show its handle while it isn't being edited
      // (EditorFileBlocks), and no "💬+" past them, as Notion's (seen 2026-11-13).
      const synced = inner?.parentElement?.closest<HTMLElement>('[role="group"][data-type="synced_block"]');
      const frame = synced?.querySelector("section");
      const whole = Boolean(frame && frame.dataset.caretInside !== "true" && !frame.matches(":focus-within"));
      const list = whole ? null : inner?.closest(LIST);
      if (list) {
        const reached = columnBlockAt(list, x, event.clientY, rtl);
        const near = reached?.row.parentElement?.getBoundingClientRect();
        // Past a list widened beyond the page's column (columnListWidth), 150px past its own end, as Notion's (seen
        // 2026-11-24 at 1685, dark, dpr 1: a 1008 list ending at 1481.5 kept its last column's handles to 1628 and not
        // from 1632).
        const listEnd = !reached ? columnEnd : rtl ? Math.min(columnEnd, reached.end) : Math.max(columnEnd, reached.end);
        const inReach = !reached || !near ? false : reached.side === "between"
          || reached.side === "before" && (rtl ? x <= near.right + REACH : x >= near.left - REACH)
          || reached.side === "past" && (rtl ? x >= listEnd - REACH : x <= listEnd + REACH);
        if (reached && inReach) mark(reached.row, false);
        else clear();
        return;
      }
      const row = whole ? synced! : inner;
      const group = row?.parentElement;
      if (!row || !group) return clear();
      const block = group.getBoundingClientRect();
      const before = rtl ? x > block.right && x <= block.right + REACH : x < block.left && x >= block.left - REACH;
      const past = !whole && (rtl ? x <= columnEnd && x > columnEnd - REACH : x >= columnEnd && x < columnEnd + REACH);
      if (before) mark(row, false);
      else if (past) mark(row, true);
      else clear();
    };
    page.addEventListener("pointermove", move);
    page.addEventListener("pointerleave", clear);
    page.addEventListener("scroll", clear, { passive: true });
    return () => {
      clear();
      page.removeEventListener("pointermove", move);
      page.removeEventListener("pointerleave", clear);
      page.removeEventListener("scroll", clear);
    };
  }, [ref]);
}
