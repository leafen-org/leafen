import { type RefObject, useLayoutEffect, useMemo, useRef } from "react";
import { ListKeyboardDelegate, mergeProps, useListBox } from "react-aria";
import { Item, useListState } from "react-stately";
import { nativeMenuKeyboardDelegate } from "./menuKeyboard";
import { ariaScrollsNothing, useKeyWalkScroll } from "./menuKeyScroll";
import { movedPointerHover } from "./pointerHover";

/** Native values/actions remain outside the transient Aria collection.
 * Ordinary list/grid movement, disabled skipping, typeahead and focus are
 * supplied by the public engine; DOM geometry determines grid columns. */
export function useNativeList<T>({ items, open = true, listRef, label, keyOf, textOf, onAction,
  firstKey, disabledKeys = [], layout = "stack", autoFocus = true }: {
  items: readonly T[]; open?: boolean; listRef: RefObject<HTMLElement | null>; label: string;
  keyOf(item: T): string; textOf(item: T): string; onAction(item: T): void;
  firstKey?: string; disabledKeys?: string[]; layout?: "stack" | "grid";
  autoFocus?: boolean;
}) {
  const entries = items.map(item => ({ item, id: keyOf(item), text: textOf(item) }));
  const state = useListState({ items: entries, selectionMode: "none", disabledKeys,
    children: entry => <Item key={entry.id} textValue={entry.text}>{entry.text}</Item> });
  const delegate = useMemo(() => layout === "grid"
    ? new ListKeyboardDelegate({ collection: state.collection, disabledKeys: state.disabledKeys,
      ref: listRef, layout, orientation: "vertical", direction: "ltr",
      collator: new Intl.Collator(undefined, { usage: "search", sensitivity: "base" }) })
    : nativeMenuKeyboardDelegate(state.collection, state.disabledKeys, listRef),
  [layout, state.collection, state.disabledKeys, listRef]);
  // A list's rows scroll as Notion's menus scroll theirs (`useKeyWalkScroll`); a grid's, Aria's way.
  const { listBoxProps } = useListBox({ ...(layout === "grid" ? {} : ariaScrollsNothing), "aria-label": label, autoFocus: open && autoFocus && firstKey === undefined ? "first" : false,
    keyboardDelegate: delegate, shouldFocusWrap: true, shouldFocusOnHover: false, escapeKeyBehavior: "none",
    onAction: key => { const entry = state.collection.getItem(key)?.value; if (entry) onAction(entry.item); },
  }, state, listRef);
  useKeyWalkScroll(listRef, layout === "grid" ? undefined : state.selectionManager.focusedKey);
  const wasOpen = useRef(false);
  useLayoutEffect(() => {
    const key = state.selectionManager.focusedKey;
    if (open
      && (!wasOpen.current || !state.collection.getItem(key!) || state.disabledKeys.has(key!))) {
      if (autoFocus) state.selectionManager.setFocused(true);
      state.selectionManager.setFocusedKey(firstKey && state.collection.getItem(firstKey) && !state.disabledKeys.has(firstKey)
        ? firstKey : delegate.getFirstKey());
    }
    wasOpen.current = open;
  }, [open, autoFocus, firstKey, state.collection, state.disabledKeys, delegate]);
  // Hover highlights only where the pointer really moves (`pointerHover`).
  return { state, listBoxProps: mergeProps(listBoxProps, { onPointerMove: movedPointerHover(state) }) };
}
