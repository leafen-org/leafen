import {
  type HTMLAttributes, type MouseEvent, type PointerEvent, type ReactNode, type RefObject, memo, useLayoutEffect, useMemo, useRef, useState,
} from "react";
import { ListKeyboardDelegate, mergeProps, useListBox } from "react-aria";
import { Item, type ListState, useListState } from "react-stately";
import { isMenuMoveKey, nativeMenuKeyboardDelegate } from "./menuKeyboard";
import { ariaScrollsNothing, useKeyWalkScroll } from "./menuKeyScroll";
import { keyboardPropsEvent } from "./overlayLayer";
import { PickerRow, type PickerRowParts, drawPickerRows, pickerRowId } from "./pickerRow";
import { keysMovedHighlight, movedPointerHover } from "./pointerHover";
import { fieldEnter, imeComposing } from "./fieldKeyBoundary";

/** Native commands retain their caret/lease and commit policy. Aria owns the
 * transient collection, virtual focus, navigation, option interaction and
 * scrolling. Text keys stay with the editor and update its native query.
 * As in Notion's slash and mention menus, Home and End move the caret and
 * PageUp and PageDown leave the highlight where it is. `tab` is what Tab
 * does: picks the highlight (the mention menu), closes the picker and goes on
 * to the editor (the slash menu, whose Tab then indents), or is left alone. */
export function useCaretPicker<T>({ items, query, id, label, listRef, keyOf, textOf, onPick, onClose,
  ownerBlockId, ownerRef, onOwnerMismatch, closeEmptyEnter = false, open = true,
  layout = "stack", tab = "pick", keep, pointerTakesAtOnce = false, wrap = "round", lightFirst = true, opening }: {
  items: T[]; query: string; id: string; label: string; listRef: RefObject<HTMLElement | null>;
  keyOf(item: T): string; textOf(item: T): string; onPick(item: T): void; onClose(): void;
  ownerBlockId?: string; onOwnerMismatch?: () => void; closeEmptyEnter?: boolean;
  ownerRef?: RefObject<HTMLElement | null>;
  open?: boolean; layout?: "stack" | "grid"; tab?: "pick" | "close" | "leave";
  /** What the keys chose as the query changes (`keptChoice`); by default the first row is chosen again. */
  keep?: KeptChoice<T>;
  /**
   * Whether the pointer's first move after the keys takes the highlight at
   * once, as Notion's formula helper (seen 2026-11-14); elsewhere it waits
   * until the pointer moves into another row (`pointerHover`).
   */
  pointerTakesAtOnce?: boolean;
  /**
   * How the arrows go on past an end, as the picker's Notion list does:
   * round to the other end ("round", the "@" menu), or through no row lit
   * first ("throughNone", the "/" menu: ↑ on its first row lights none, the
   * next ↑ its last; ↓ on its last none, the next its first — seen
   * 2026-11-14); with none lit, Enter closes it as an empty one's does.
   */
  wrap?: "round" | "throughNone";
  /**
   * Whether it opens, and goes on for each query, with its first row lit;
   * unset, none is lit until the arrows move, Enter then closing it, as
   * Notion's "날짜 또는 리마인더" list (seen 2026-11-15 by "+" and by "/":
   * "@", "@내일" lit none, ↓ lit and ringed the first row).
   */
  lightFirst?: boolean;
  /**
   * The row it opens with lit, instead of its first, while what is typed is
   * what it opened with (`key`, none for the first) — once a letter changes
   * that, its first row again for each query, even back to what it opened
   * with — and how that row is shown once drawn lit (`show`), as the "@"
   * menu opens past its dates (mentionOpenChoice.ts).
   */
  opening?: { key(items: readonly T[]): string | undefined; show?(row: HTMLElement): void };
}) {
  const state = useListState<T>({ items, selectionMode: "none",
    children: item => <Item key={keyOf(item)} textValue={textOf(item)}>{textOf(item)}</Item> });
  const delegate = useMemo(() => layout === "grid"
    ? new ListKeyboardDelegate({ collection: state.collection, disabledKeys: state.disabledKeys,
      ref: listRef, layout, orientation: "vertical", direction: "ltr",
      collator: new Intl.Collator(undefined, { usage: "search", sensitivity: "base" }) })
    : nativeMenuKeyboardDelegate(state.collection, state.disabledKeys, listRef),
    [layout, state.collection, state.disabledKeys, listRef]);
  // A list's rows scroll as Notion's menus scroll theirs (`useKeyWalkScroll`); a grid's, Aria's way.
  // A list that opens on another row than its first lights it itself (below).
  const { listBoxProps } = useListBox({ ...(layout === "grid" ? {} : ariaScrollsNothing), id, "aria-label": label,
    autoFocus: open && lightFirst && !opening ? "first" : false,
    shouldUseVirtualFocus: true, shouldFocusWrap: true, shouldFocusOnHover: false,
    escapeKeyBehavior: "none", keyboardDelegate: delegate,
    onAction: key => { const item = state.collection.getItem(key)?.value; if (item) onPick(item); },
  }, state, listRef);
  const owner = useRef<HTMLElement | null>(null);
  const ownerAria = useRef<{ node: HTMLElement; saved: Map<string, string | null>; written: Map<string, string | null> } | null>(null);
  useLayoutEffect(() => {
    if (!open) return;
    owner.current = ownerRef?.current ?? (document.activeElement instanceof HTMLElement ? document.activeElement : null);
    const node = owner.current;
    if (node?.matches('input, textarea, [contenteditable="true"]')) {
      const names = ["aria-controls", "aria-activedescendant"];
      ownerAria.current = { node, saved: new Map(names.map(name => [name, node.getAttribute(name)])), written: new Map() };
    }
    return () => {
      const lease = ownerAria.current;
      if (lease) for (const [name, value] of lease.saved) {
        if (lease.node.getAttribute(name) !== lease.written.get(name)) continue;
        if (value === null) lease.node.removeAttribute(name); else lease.node.setAttribute(name, value);
      }
    };
  }, [open, ownerRef]);
  // What last moved the highlight: nothing yet (the first row, as opened or
  // filtered), the arrow keys, or the pointer. Notion's slash menu rings a
  // row the keys chose and shows a row's tip only once something moved it
  // (seen 2026-11-01).
  const [moved, setMoved] = useState<"keys" | "pointer" | null>(null);
  const previousQuery = useRef<string | undefined>(undefined);
  const previousItems = useRef<T[]>(items);
  // The first row is highlighted for each query, and stays so as later
  // results come in above it (people found on the server after the invite
  // row, as Notion picks the person) until the person moves the highlight;
  // a row the keys chose is kept where the picker keeps it (`keep`).
  const chosenKey = useRef<unknown>(null);
  // The arrows that went past an end leave no row lit until they move again
  // or the query changes ("throughNone").
  const restsAtNone = useRef(false);
  // What was typed as it opened, while nothing typed has changed it since
  // (`opening`); the row it opened on past its first, until shown.
  const openedWith = useRef<{ query: string } | null | undefined>(undefined);
  const openRow = useRef<unknown>(null);
  useLayoutEffect(() => {
    if (!open) { openedWith.current = undefined; return; }
    if (restsAtNone.current && query === previousQuery.current) return;
    restsAtNone.current = false;
    const manager = state.selectionManager;
    if (openedWith.current === undefined) openedWith.current = { query };
    else if (openedWith.current && query !== openedWith.current.query) openedWith.current = null;
    const opened = openedWith.current && opening?.key(items);
    const first = opened && state.collection.getItem(opened) ? opened : delegate.getFirstKey();
    const focused = manager.focusedKey;
    if (!lightFirst) {
      if (query !== previousQuery.current || !state.collection.getItem(focused!)) { manager.setFocused(true); manager.setFocusedKey(null); }
      previousQuery.current = query; previousItems.current = items; return;
    }
    const untouched = focused === chosenKey.current;
    const kept = keep && moved === "keys" && !untouched && focused !== null
      ? keptChoice(keep, previousItems.current, items, keyOf, focused) : undefined;
    if (kept !== undefined) {
      if (kept !== focused) { manager.setFocused(true); manager.setFocusedKey(kept as string); }
    } else if (query !== previousQuery.current || !state.collection.getItem(focused!) || untouched && focused !== first) {
      manager.setFocused(true); manager.setFocusedKey(first);
      chosenKey.current = first;
      openRow.current = first === opened && first !== delegate.getFirstKey() ? first : null;
    }
    previousQuery.current = query;
    previousItems.current = items;
  }, [open, query, state.collection, state.selectionManager, delegate, moved, lightFirst]);
  const focusedKey = state.selectionManager.focusedKey;
  useKeyWalkScroll(listRef, layout === "grid" ? undefined : focusedKey);
  // The row it opened on past its first is shown once drawn lit (`opening`).
  useLayoutEffect(() => {
    const key = openRow.current;
    if (key === null || focusedKey !== key) return;
    openRow.current = null;
    const row = listRef.current?.querySelector<HTMLElement>(`[data-key="${CSS.escape(String(key))}"]`);
    if (row) opening?.show?.(row);
  });
  const active = focusedKey === null ? -1 : Array.from(state.collection.getKeys()).indexOf(focusedKey);
  const [activeId, setActiveId] = useState<string>();
  useLayoutEffect(() => {
    const option = focusedKey === null ? null : listRef.current?.querySelector<HTMLElement>(`[data-key="${CSS.escape(String(focusedKey))}"]`);
    setActiveId(option?.id);
    // Wire Aria's public virtual focus relationship to the real caret owner.
    // The native editor distinguishes virtual announcements from DOM blur.
    const lease = ownerAria.current;
    if (lease) for (const [name, value] of [["aria-controls", id], ["aria-activedescendant", option?.id ?? null]] as const) {
      if (value === null) lease.node.removeAttribute(name); else lease.node.setAttribute(name, value);
      lease.written.set(name, value);
    }
  }, [focusedKey, state.collection, listRef]);
  function onCaretKeyDown(event: KeyboardEvent) {
    if (!open || event.defaultPrevented || imeComposing(event) || event.shiftKey && fieldEnter(event)) return;
    const activeElement = document.activeElement;
    const caret = document.getSelection()?.anchorNode;
    const caretElement = caret instanceof Element ? caret : caret?.parentElement;
    // The block the caret is in, where the caret is in the focused editor:
    // an editor inside a block (a synced block's copy, which edits its
    // original in place) sits within the copy's own block, which isn't the
    // one being typed in (2026-10-30: "@" and a letter closed the menu there).
    const caretBlock = activeElement?.contains(caret ?? null) ? caretElement?.closest<HTMLElement>("[data-block-id]") : null;
    const activeBlock = caretBlock ?? activeElement?.closest<HTMLElement>("[data-block-id]");
    if (ownerBlockId && activeBlock?.dataset.blockId !== ownerBlockId) {
      onOwnerMismatch?.(); return;
    }
    if (activeElement !== document.body && !owner.current?.contains(activeElement) && !listRef.current?.contains(activeElement)) return;
    if (event.altKey || event.ctrlKey || event.metaKey) return;
    const empty = state.collection.size === 0;
    if (event.key === "Escape" || (empty && closeEmptyEnter && fieldEnter(event))) {
      event.preventDefault(); event.stopPropagation(); onClose();
    } else if (event.key === "Tab" && tab === "close") {
      onClose();
    } else if (!empty && (fieldEnter(event) || tab === "pick" && event.key === "Tab")) {
      event.preventDefault(); event.stopPropagation();
      const item = focusedKey === null ? null : state.collection.getItem(focusedKey)?.value;
      if (item) onPick(item); else onClose();
    } else if (!empty && (isMenuMoveKey(event.key)
      || layout === "grid" && ["ArrowLeft", "ArrowRight", "PageUp", "PageDown"].includes(event.key))) {
      const root = listRef.current;
      const lit = focusedKey !== null && state.collection.getItem(focusedKey) ? focusedKey : null;
      restsAtNone.current = wrap === "throughNone" && lit !== null
        && (event.key === "ArrowDown" && lit === delegate.getLastKey() || event.key === "ArrowUp" && lit === delegate.getFirstKey());
      if (restsAtNone.current) {
        event.preventDefault(); event.stopPropagation();
        state.selectionManager.setFocusedKey(null);
      } else if (root) listBoxProps.onKeyDown?.(keyboardPropsEvent(event, root, true));
      setMoved("keys");
      if (!pointerTakesAtOnce) keysMovedHighlight(root);
    } else if (event.key === "PageUp" || event.key === "PageDown") {
      event.preventDefault(); event.stopPropagation();
    }
  }
  // Hover highlights only where the pointer really moves, after the keys
  // only once it moves into another row, taking the ring and the tip with it
  // (`pointerHover`).
  // A grid the pointer took the highlight in draws no ring or tip for the
  // keys' moves until the pointer leaves it, as Notion's emoji grid (seen
  // 2026-11-14: → after the pointer took a cell lit the next one plainly).
  const [pointerLed, setPointerLed] = useState(false);
  const plainKeys = layout === "grid" && pointerLed;
  const onPointerMove = movedPointerHover(state, () => { setMoved("pointer"); setPointerLed(true); });
  // Its rows (`CaretPickerOption`) are pressed as `PickerRow`s; a grid's
  // cells (`CaretPickerCell`) are pressed here: a press marks its cell and
  // keeps the focus where it is, and a click picks it.
  drawPickerRows(state, listBoxProps, key => { const item = state.collection.getItem(key)?.value; if (item) onPick(item); });
  const collection = (listBoxProps as { "data-collection"?: string })["data-collection"];
  const cellKey = (event: MouseEvent<HTMLElement>) => {
    const cell = event.target instanceof Element ? event.target.closest<HTMLElement>("[data-caret-cell]") : null;
    const key = cell && event.currentTarget.contains(cell) ? cell.dataset.key : undefined;
    return key !== undefined && state.collection.getItem(key) && !state.disabledKeys.has(key) ? key : null;
  };
  const press: HTMLAttributes<HTMLElement> = layout !== "grid" ? {} : {
    onMouseDown: (event: MouseEvent<HTMLElement>) => { if (cellKey(event) !== null) event.preventDefault(); },
    onPointerDown: (event: PointerEvent<HTMLElement>) => {
      const key = cellKey(event);
      if (key === null || event.pointerType === "touch") return;
      state.selectionManager.setFocused(true); state.selectionManager.setFocusedKey(key);
    },
    onClick: (event: MouseEvent<HTMLElement>) => {
      const key = cellKey(event), item = key === null ? undefined : state.collection.getItem(key)?.value;
      if (item) onPick(item);
    },
  };
  /** What a grid's cell carries for the grid to find, name and mark it. */
  const cell = (key: string) => ({ id: pickerRowId(listBoxProps.id, key), role: "option",
    "data-key": key, "data-collection": collection, "data-active": focusedKey === key ? "true" : undefined } as const);
  // A grid's lit cell named in its tip: once the keys moved the highlight,
  // or while the pointer that took it is in the grid.
  const tipKey = focusedKey !== null && (moved === "keys" ? !plainKeys : moved === "pointer" && pointerLed) ? String(focusedKey) : null;
  return { state, active, activeId, moved, cell, tipKey,
    listBoxProps: { ...mergeProps(listBoxProps, { onPointerMove, onPointerLeave: () => setPointerLed(false) }, press),
      "data-native-caret-picker": "true", "data-keyed": moved === "keys" && !plainKeys ? "true" : undefined },
    onCaretKeyDown, inside: () => [owner.current] };
}

/**
 * How a picker keeps a row the keys chose as what is typed changes the rows,
 * as Notion's "@" and "[[" menus do (seen 2026-11-13; its "/" menu chooses
 * its first row again): the row itself wherever it moves, or else the row
 * at its place in its group ("@": a person typed away gives the people's
 * invite, a page the page now in its place, the group's first past its
 * end); or the place alone ("[[": the third row stays chosen as pages come
 * in above it).
 */
export type KeptChoice<T> = { groupOf(item: T): string } | "place";

/** The row `keep` chooses in `items` for the one `focused` was in `before`, if any. */
function keptChoice<T>(keep: KeptChoice<T>, before: readonly T[], items: readonly T[], keyOf: (item: T) => string, focused: unknown) {
  const at = before.findIndex((item) => keyOf(item) === focused);
  if (keep === "place") return at < 0 || !items.length ? undefined : keyOf(items[Math.min(at, items.length - 1)]);
  if (items.some((item) => keyOf(item) === focused)) return focused;
  if (at < 0) return undefined;
  const group = keep.groupOf(before[at]);
  const place = before.slice(0, at).filter((item) => keep.groupOf(item) === group).length;
  const rows = items.filter((item) => keep.groupOf(item) === group);
  return rows.length ? keyOf(rows[place] ?? rows[0]) : undefined;
}

/**
 * A cell of a grid of many choices (the emoji and the icons): a plain button
 * whose press, hover and mark are its grid's (`useCaretPicker`'s `cell` and
 * list props), holding no state of its own, so a grid of two thousand opens
 * without an update for each, and a move redraws the two cells it leaves and
 * reaches alone.
 */
export const CaretPickerCell = memo(function CaretPickerCell({ children, ...attributes }: HTMLAttributes<HTMLButtonElement>
  & ReturnType<ReturnType<typeof useCaretPicker>["cell"]> & { [data: `data-${string}`]: string | number | undefined }) {
  return <button type="button" data-native-picker-option data-caret-cell {...attributes}>{children}</button>;
});

/** A row of a list of choices (the "/" and "@" menus, the formula helper), a
 * `PickerRow` out of the tab order. */
export function CaretPickerOption<T>({ state, itemKey, className, children, ...attributes }: {
  state: ListState<T>; itemKey: string; className?: string; children(parts: PickerRowParts): ReactNode;
} & Omit<HTMLAttributes<HTMLButtonElement>, "children"> & { [data: `data-${string}`]: unknown }) {
  return <PickerRow {...attributes} state={state} itemKey={itemKey} className={className} tabIndex={-1}>{children}</PickerRow>;
}
