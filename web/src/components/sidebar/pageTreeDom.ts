import type { KeyboardEvent } from "react";
import { inlineEndKey, inlineStartKey } from "@/lib/readingDirection";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

// Every sidebar tree — the private pages, a teamspace's, the shared, the
// favorites, the recent pages, a database view's — takes keys one way. Each
// row is its own Tab stop, and so is each of its buttons (open or close,
// "+", "⋯"), as Notion's sidebar rows and their buttons are (seen
// 2026-11-11); Enter opens the row, ⌘/Ctrl+Enter in a new tab. The arrows,
// which Notion's sidebar leaves to the page's block selection, walk the
// tree here, a difference kept for keyboard use: ↑↓ row to row, Home/End
// to its ends, → opens a row or goes to the first row inside it, ← closes
// it or goes to the row it is in.

export type TreeRowKeys = {
  open(newTab: boolean): void;
  /** A row that opens and closes on the rows inside it. */
  expanded?: boolean;
  setExpanded?(next: boolean): void;
  /** F2. */
  rename?(): void;
  /** The context menu key or Shift+F10. */
  menu?(): void;
};

function levelOf(row: HTMLElement) {
  return Number(row.getAttribute("aria-level")) || 1;
}

export function onTreeRowKeyDown(event: KeyboardEvent<HTMLElement>, keys: TreeRowKeys) {
  const row = event.currentTarget;
  if (event.target !== row || imeComposing(event)) return;
  const key = event.key;
  const handled = () => event.preventDefault();
  if (fieldEnter(event) || key === " ") {
    handled();
    keys.open(event.metaKey || event.ctrlKey);
    return;
  }
  if (key === "F2" && keys.rename) {
    handled();
    keys.rename();
    return;
  }
  if ((key === "ContextMenu" || (event.shiftKey && key === "F10")) && keys.menu) {
    handled();
    keys.menu();
    return;
  }
  // The rows drawn in the tree, inside its open rows too, in reading order.
  const rows = Array.from((row.closest('[role="tree"]') ?? row).querySelectorAll<HTMLElement>('[role="treeitem"]'));
  const at = rows.indexOf(row);
  let next: HTMLElement | undefined;
  if (key === inlineEndKey(row)) {
    // Opening a row is toward the reading direction's end, as its chevron points.
    handled();
    if (keys.setExpanded && !keys.expanded) keys.setExpanded(true);
    else if (rows[at + 1] && levelOf(rows[at + 1]) > levelOf(row)) next = rows[at + 1];
  } else if (key === inlineStartKey(row)) {
    handled();
    if (keys.setExpanded && keys.expanded) keys.setExpanded(false);
    else next = rows.slice(0, Math.max(at, 0)).reverse().find((before) => levelOf(before) < levelOf(row));
  } else if (key === "ArrowDown" || key === "ArrowUp") {
    handled();
    next = rows[key === "ArrowDown" ? Math.min(at + 1, rows.length - 1) : Math.max(at - 1, 0)];
  } else if (key === "Home" || key === "End") {
    handled();
    next = key === "Home" ? rows[0] : rows.at(-1);
  }
  next?.focus();
}
