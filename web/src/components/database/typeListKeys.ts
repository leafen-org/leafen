import { useState, type KeyboardEvent, type PointerEvent, type RefObject } from "react";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { scrollKeyWalkedRow } from "@/lib/menuKeyScroll";
import { keysMovedHighlight, pointerTakenRow } from "@/lib/pointerHover";
import { tableAddPropertyStep } from "./tableAddPropertyModel";

// A one-column type list's keys — a database page's "속성 추가", a layout's
// new property, the view settings' "새 속성" and a property's "유형" screen —
// as Notion's (seen 2026-11-21 at 1440 in Chrome, without its AI rows, in
// four tabs at different times, judged by the screen and a letter typed
// after each key). The arrows move one mark around the types as the table's
// two columns do (`tableAddPropertyStep`): round the ends straight while the
// search is closed, through a stop that marks nothing while it is open.
// The name keeps the keys as they move the mark; the open search hands them
// to the list, where letters go nowhere: with words in it from where the
// mark is, empty only handing them over — ↓ leaving the mark where it is, ↑
// taking it two up, or to the last type when none is marked, unscrolled.

const ONE_COLUMN = [0];

/** Where ↑ or ↓ takes a one-column list's mark (an index, null for none). */
export function typeListStep(count: number, at: number | null, key: string, searchOpen: boolean): number | null {
  const rows = Array.from({ length: count }, () => ONE_COLUMN);
  return tableAddPropertyStep(rows, at === null ? null : { row: at, column: 0 }, key, searchOpen)?.row ?? null;
}

/** Where ↑ or ↓ in the open search takes the mark as the keys leave it for the list. */
export function typeListSearchStep(count: number, at: number | null, key: string, words: boolean): number | null {
  if (words) return typeListStep(count, at, key, true);
  if (key === "ArrowDown") return at;
  if (at === null) return count ? count - 1 : null;
  return typeListStep(count, typeListStep(count, at, key, true), key, true);
}

const vertical = (event: KeyboardEvent) => (event.key === "ArrowDown" || event.key === "ArrowUp")
  && !event.altKey && !event.ctrlKey && !event.metaKey && !imeComposing(event);

export type TypeListKeys = ReturnType<typeof useTypeListKeys>;

/**
 * The mark and keys of a one-column type list: `keys` are its rows' `data-key`s
 * in order, under `listRef` (the pointer's list); `menuRef` takes the keys as
 * they leave the search, its own handler calling `onListKeyDown`.
 */
export function useTypeListKeys({ keys, first = null, searchOpen, searchWords, listRef, menuRef, onChoose }: {
  keys: readonly string[];
  /** The row marked as the list opens. */
  first?: string | null;
  searchOpen: boolean;
  searchWords: boolean;
  listRef: RefObject<HTMLElement | null>;
  menuRef: RefObject<HTMLElement | null>;
  onChoose(key: string): void;
}) {
  const [marked, setMarked] = useState<string | null>(first);
  const found = marked === null ? -1 : keys.indexOf(marked);
  const at = found < 0 ? null : found;

  // The pointer then takes the mark only as it moves into another row
  // (`pointerHover`); the list scrolls as Notion's menus do to keep it in sight.
  function mark(next: number | null, scroll = true) {
    keysMovedHighlight(listRef.current);
    setMarked(next === null ? null : keys[next] ?? null);
    if (!scroll || next === null) return;
    window.requestAnimationFrame(() => scrollKeyWalkedRow(listRef.current?.querySelector<HTMLElement>('[data-active="true"]'),
      document.documentElement));
  }

  /** The name's (or another field's that keeps the keys) ↑ and ↓; true when taken. */
  function onNameKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (!vertical(event)) return false;
    event.preventDefault();
    mark(typeListStep(keys.length, at, event.key, searchOpen));
    return true;
  }

  /** The open search's ↑ and ↓, which hand the keys to the list; true when taken. */
  function onSearchKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (!vertical(event)) return false;
    event.preventDefault();
    const up = event.key === "ArrowUp";
    mark(typeListSearchStep(keys.length, at, event.key, searchWords), !(up && !searchWords && at === null));
    menuRef.current?.focus({ preventScroll: true });
    return true;
  }

  /** The keys the list holds (the menu itself focused): true when taken. */
  function onListKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (event.target !== event.currentTarget || imeComposing(event)) return false;
    if (vertical(event)) {
      event.preventDefault();
      mark(typeListStep(keys.length, at, event.key, searchOpen));
    } else if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
      event.preventDefault();
    } else if (fieldEnter(event)) {
      event.preventDefault();
      if (marked !== null && at !== null) onChoose(marked);
    } else if (searchOpen && event.key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey) {
      event.preventDefault();
    } else {
      return false;
    }
    event.stopPropagation();
    return true;
  }

  // The pointer marks the row it moves onto, after the keys only once it
  // moves into another row.
  function onPointerMove(event: PointerEvent<HTMLElement>) {
    const key = pointerTakenRow(event);
    if (key !== null && key !== marked && keys.includes(key)) setMarked(key);
  }

  return { at, marked, setMarked, onNameKeyDown, onSearchKeyDown, onListKeyDown, onPointerMove };
}
