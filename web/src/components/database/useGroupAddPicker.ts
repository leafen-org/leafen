import { type FocusEvent, type HTMLAttributes, type KeyboardEvent, type RefObject, useEffect, useState } from "react";
import { useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { keysMovedHighlight } from "@/lib/pointerHover";
import { pickerRowId } from "@/lib/pickerRow";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { shortcutModifierHeld } from "@/lib/keyboardShortcuts";

type Row = { id: string; label: string };

// Keys the list takes and does nothing with, as Notion's (seen 2026-11-12).
const LIST_INERT_KEYS = new Set(["Home", "End", "PageUp", "PageDown", " "]);

/**
 * The keys of a view's "+ 신규 그룹" window for a person or a related page, as
 * Notion's (seen 2026-11-12 at 1440, dark): opened, no line is marked; ↓
 * marks the first and ↑ the last, each going round past the end; once the
 * search is typed in — emptied again too — its first line is marked; the
 * pointer marks the line it moves into, after the keys only once it moves
 * into another line (seen 2026-11-14, a person's and a related page's window
 * alike, `pointerHover`); Enter picks the marked line. Escape
 * closes it, where Notion's takes no Escape (seen 2026-11-12): a window the
 * keys cannot leave is no difference Leafen keeps.
 *
 * Tab and ⇧Tab go between the search and the list, the window's two stops
 * (seen 2026-11-12 on a board and a table): the list taken, its marked line
 * stays or, with none, its first is marked, and drawn with the keyboard's
 * ring; there the arrows move the mark, going round, Enter picks it, and
 * letters, Space, Home and End do nothing; back in the search the mark
 * stays. The lines themselves take no Tab.
 */
export function useGroupAddPicker<T extends Row>({ open, rows, query, onQuery, onPick, onClose, inputRef, listRef, label }: {
  open: boolean;
  rows: T[];
  query: string;
  onQuery(value: string): void;
  onPick(row: T): void;
  onClose(): void;
  inputRef: RefObject<HTMLInputElement | null>;
  listRef: RefObject<HTMLElement | null>;
  label: string;
}) {
  // Whether the search was typed in since the window opened.
  const [typed, setTyped] = useState(false);
  // Whether the list holds the focus, which names its marked line then.
  const [listFocused, setListFocused] = useState(false);
  useEffect(() => { if (!open) { setTyped(false); setListFocused(false); } }, [open]);
  const picker = useNativeAutocomplete({
    items: rows, query, open, inputRef, listRef, label, onPick, onClose,
    onQuery: (value) => { setTyped(true); onQuery(value); },
    keyOf: (row) => row.id, textOf: (row) => row.label,
    highlightFirst: typed, commitTab: false,
  });
  const { state } = picker, manager = state.selectionManager;
  const marked = manager.isFocused && manager.focusedKey != null && state.collection.getItem(manager.focusedKey) ? manager.focusedKey : null;
  const base = picker.listBoxProps as HTMLAttributes<HTMLElement>;
  const listBoxProps: HTMLAttributes<HTMLElement> & { "data-group-add-list": "" } = {
    ...base,
    tabIndex: 0,
    "data-group-add-list": "",
    "aria-activedescendant": listFocused && marked != null ? pickerRowId(base.id, String(marked)) : undefined,
    onFocus: (event: FocusEvent<HTMLElement>) => {
      base.onFocus?.(event);
      if (event.target !== event.currentTarget) return;
      setListFocused(true);
      if (marked != null || rows[0] === undefined) return;
      manager.setFocused(true);
      manager.setFocusedKey(rows[0].id);
    },
    onBlur: (event: FocusEvent<HTMLElement>) => {
      base.onBlur?.(event);
      if (event.target !== event.currentTarget) return;
      setListFocused(false);
      // Back in the search the line stays marked.
      if (event.relatedTarget === inputRef.current && manager.focusedKey != null) manager.setFocused(true);
    },
    // Letters seek no line here (Aria's type-ahead), as Notion's.
    onKeyDownCapture: (event: KeyboardEvent<HTMLElement>) => {
      if (event.key.length !== 1 || shortcutModifierHeld(event)) base.onKeyDownCapture?.(event);
    },
    onKeyDown: (event: KeyboardEvent<HTMLElement>) => {
      if (event.target === event.currentTarget && !imeComposing(event)) {
        if (fieldEnter(event)) {
          event.preventDefault();
          if (marked != null) state.commit();
          return;
        }
        if (LIST_INERT_KEYS.has(event.key)) { event.preventDefault(); return; }
        if (event.key === "ArrowDown" || event.key === "ArrowUp") keysMovedHighlight(event.currentTarget);
      }
      base.onKeyDown?.(event);
    },
  };
  return { ...picker, listBoxProps };
}
