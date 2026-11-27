// ⌘/ over one chosen row or card opens Notion's page item menu (seen
// 2026-11-12: a table's row, a board's card), not its "⋯" menu: the view
// opens the row's menu through `openRowMenuByKey`, and the row menu reads
// `rowMenuOpenedByKey` while it opens (useRowContextMenu). Whether a menu
// opened from the row chooses it is asked here too (rowMenuChoosesRow), and
// whether a long press opens it at all (longPressKeptForWords).
import type { MouseEvent } from "react";
import { keepRightPress } from "@/lib/desktopTextMenu";
import { phoneScreen } from "@/lib/screenKind";

let openingByKey = false;

/** Opens a row's or card's menu as ⌘/ over it alone does. */
export function openRowMenuByKey(open: () => void) {
  openingByKey = true;
  try {
    open();
  } finally {
    openingByKey = false;
  }
}

/** Whether the row menu opening now is ⌘/'s over one chosen row or card. */
export function rowMenuOpenedByKey() {
  return openingByKey;
}

/**
 * Whether a row's menu opened from the row itself — a right press, a long
 * press — chooses that row, the one rule every view asks (TableView,
 * cardSelection's menuChoose). On a pointer's screen it does, as Notion's
 * table, list and gallery leave it chosen. On a phone it does not: its menu is
 * the "작업" sheet (DatabaseRowPhoneSheet) and the row is chosen neither while
 * it is open nor once it closes, as Notion's table and list rows long pressed
 * (seen 2026-11-25 on a Galaxy A90 in Chrome at 411, two tabs).
 */
export function rowMenuChoosesRow() {
  return !phoneScreen();
}

// The field being written as a finger's press went down: Android Chrome
// gives a long press's field the focus before its menu event, the press let
// go (seen 2026-11-25 on a Galaxy A90: focusin, then contextmenu), so the
// field is read as the press began. A key since ends the press: the menu key's event is the row's.
let writing: HTMLElement | null = null;
if (typeof document !== "undefined") {
  document.addEventListener("pointerdown", (event) => {
    const field = document.activeElement;
    writing = event.pointerType === "touch" && field instanceof HTMLElement && (field.isContentEditable || field.matches("input, textarea")) ? field : null;
  }, true);
  document.addEventListener("keydown", () => { writing = null; }, true);
}

/**
 * Whether a finger's long press fell on a value being written as it began —
 * in its field, or in the box a table's cell writes it in ([data-cell-editor])
 * — where it chooses the word under it with the system's own menu and opens
 * no row menu, nor the block's around it, as the Notion Android app's (seen
 * 2026-11-25 on a Galaxy A90, twice: a long press in a table text cell's open
 * box chose a word alone; on a cell not being written it opens the "작업"
 * sheet). Such a press is kept from the menus around it here (keepRightPress),
 * its default left to the browser. Every view's row menu from a press asks it
 * first (useRowContextMenu, TableView).
 */
export function longPressKeptForWords(event: MouseEvent<HTMLElement>) {
  const field = writing, target = event.target;
  if (!field?.isConnected || !(target instanceof Node)) return false;
  const box = (target instanceof Element ? target : target.parentElement)?.closest("[data-cell-editor]");
  if (!field.contains(target) && !box?.contains(field)) return false;
  keepRightPress(event);
  return true;
}
