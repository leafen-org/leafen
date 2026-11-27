import { useEffect, useRef } from "react";
import { matchesKeyboardShortcut } from "@/lib/keyboardShortcuts";

/** The rows before and after the open one in its view, which its peek opens with ∧ and ∨ or ⌃⇧K and ⌃⇧J. */
export type RowPeekSiblings = { previous?: string; next?: string; onOpen: (rowId: string) => void };

const KEYS = [["previous", "previousPeekPage"], ["next", "nextPeekPage"]] as const;

/**
 * ⌃⇧K and ⌃⇧J, which open the row before or after this one from anywhere
 * while its peek is open, as Notion's do (seen 2026-11-01).
 */
export function useRowPeekSiblingKeys(siblings: RowPeekSiblings) {
  const latest = useRef(siblings);
  latest.current = siblings;
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const side = KEYS.find(([, shortcut]) => matchesKeyboardShortcut(shortcut, event))?.[0];
      if (!side) return;
      event.preventDefault();
      const rowId = latest.current[side];
      if (rowId) latest.current.onOpen(rowId);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);
}

/**
 * The keys alone, for a peek whose bar shows no ∧ and ∨: a task's opened
 * from "내 작업" or Home's "내 작업", as Notion's (seen 2026-11-26 in the
 * test workspace, two tabs: no ∧ ∨ in the bar, yet ⌃⇧J and ⌃⇧K step through
 * the list's tasks of the same group).
 */
export function RowPeekSiblingKeys({ siblings }: { siblings: RowPeekSiblings }) {
  useRowPeekSiblingKeys(siblings);
  return null;
}
