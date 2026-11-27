import { useEffect, useRef, type PointerEvent, type RefObject } from "react";

/**
 * What an inline database tells the editor row it stands in, for that row's
 * "+ ⋮⋮" (EditorBlockHandles.module.css): what the pointer is on
 * (`data-database-pointed` — "tool", a tool button or link of its toolbar;
 * "empty-row", an empty table's first line). A tool button, link or view tab
 * the keyboard reached tells nothing: Notion's shows only its ring there, no
 * "+ ⋮⋮" (measured 2026-11-15 in Chrome 1440 after the key only — 필터 to
 * 정렬 by Tab, 설정 back by Escape, a view tab by ⇧Tab, "+ 새 보기 추가" by Tab). The
 * row's rules read these rather than look into the database with :has(),
 * which made the browser go through its whole table at every change in it
 * (2026-11-15: some 12,000 elements for a table of 30 rows by 40 properties,
 * 6.5 ms a focus move). A database outside the editor (its own page, a peek)
 * has no such row and tells nothing.
 */
type Mark = "pointed";
const KEY: Record<Mark, "databasePointed"> = { pointed: "databasePointed" };

function rowOf(element: Element | null) {
  return element?.closest<HTMLElement>('[role="group"][data-type="inline_database"]') ?? null;
}

/** Sets or clears (`on` false: only while it is `value`) one mark on the row `from` stands in; the row marked, or null. */
function markRow(from: Element | null, mark: Mark, value: string, on: boolean) {
  const row = rowOf(from);
  if (!row) return null;
  const key = KEY[mark], now = row.dataset[key];
  if (on && now !== value) row.dataset[key] = value;
  else if (!on && now === value) delete row.dataset[key];
  return row;
}

/** One kind of mark a part of the database keeps on its row, cleared when the part goes. */
function useRowMark(mark: Mark, value: string) {
  const marked = useRef<HTMLElement | null>(null);
  useEffect(() => () => {
    if (marked.current && marked.current.dataset[KEY[mark]] === value) delete marked.current.dataset[KEY[mark]];
    marked.current = null;
  }, [mark, value]);
  return (from: Element, on: boolean) => {
    const row = markRow(from, mark, value, on);
    if (row) marked.current = row;
  };
}

/** The database's tool row (DatabaseView's chrome): its tool buttons and links under the pointer. */
export function useDatabaseChromeMarks() {
  const tool = useRowMark("pointed", "tool");
  return {
    onPointerOver(event: PointerEvent<HTMLElement>) {
      const target = event.target instanceof Element ? event.target.closest("button, a") : null;
      const toolbar = target?.closest('[role="toolbar"]');
      tool(event.currentTarget, Boolean(toolbar && event.currentTarget.contains(toolbar)));
    },
    onPointerOut(event: PointerEvent<HTMLElement>) {
      if (!(event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget))) tool(event.currentTarget, false);
    },
  };
}

/** An empty table's first line under the pointer (TableEmptyRows). */
export function useEmptyRowMark() {
  return useRowMark("pointed", "empty-row");
}

// How many databases stand under each block row and group that holds them.
const holding = new WeakMap<HTMLElement, number>();

/**
 * A database standing in another block — a toggle, a column, a callout —
 * marks each block row and group it stands in (`data-holds-database`): the
 * editor's rules that look into a row with :has() leave such a row out, or
 * look only while a window, menu or tip is open, as for the database's own
 * row (EditorBlocks, EditorBlockHandles .module.css). Looked into, the
 * browser went through the whole table under it at every change in it.
 */
export function useDatabaseHolderMarks(wrapper: RefObject<HTMLElement | null>) {
  useEffect(() => {
    const marked: HTMLElement[] = [];
    const holderOf = (element: Element | null | undefined) => element?.closest<HTMLElement>('[role="group"][data-type]') ?? null;
    for (let row = holderOf(rowOf(wrapper.current)?.parentElement); row; row = holderOf(row.parentElement)) {
      for (const element of [row, row.parentElement]) {
        if (!element) continue;
        holding.set(element, (holding.get(element) ?? 0) + 1);
        element.dataset.holdsDatabase = "true";
        marked.push(element);
      }
    }
    return () => {
      for (const element of marked) {
        const count = (holding.get(element) ?? 1) - 1;
        if (count > 0) holding.set(element, count);
        else { holding.delete(element); delete element.dataset.holdsDatabase; }
      }
    };
  }, [wrapper]);
}
