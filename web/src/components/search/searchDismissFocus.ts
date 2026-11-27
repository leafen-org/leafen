import { type RefObject, useLayoutEffect, useRef } from "react";
import { placeCaret } from "@/components/editor/focus";
import { matchesKeyboardShortcut } from "@/lib/keyboardShortcuts";

// A page's text: its blocks and its title, which Notion's page takes as one
// field — not a database page's name, nor a side peek's page, whose title is
// a field of its own there (rowOpenPress.ts).
const PAGE_TEXT = "[data-native-editor-content], [data-page-title-editor]:not([data-database-page-name])";
const SIDE_PEEK = "[data-row-peek-panel], [data-page-peek]";

/** The title of the page whose text has the focus, if it has. */
function titleOfTextAt(focused: Element | null): HTMLElement | null {
  const text = focused instanceof HTMLElement ? focused.closest(PAGE_TEXT) : null;
  if (!text || text.closest(SIDE_PEEK)) return null;
  return text.closest("[data-page-search-root]")?.querySelector<HTMLElement>("[data-page-title-editor]:not([data-database-page-name])") ?? null;
}

/**
 * Search dismissed — by its key pressed again (⌘P or ⌘K), by Escape or by a
 * press beside it — after it was opened from a page's text, its title or its
 * blocks: the caret goes to the start of that page's title and the page stays
 * where it is, as Notion's (seen 2026-11-18 in Chrome at 1440, two tabs at
 * different times, nothing run in the page before the keys: after a click in
 * a line or in the middle of the title, ⌘P or ⌘K then ⌘P, ⌘K, Escape or a
 * press beside the window, one letter typed went in before the title's first
 * letter). Opened from anything else — the sidebar's "검색", whose button
 * Notion's Escape gives the focus back to — the focus goes back to it, as
 * the window's layer gives it (useOverlayLayer's restoreFocus). A result
 * opened, or another screen asked for, leaves the focus to that.
 */
export function useSearchDismissFocus(open: boolean, closeRef: RefObject<() => void>) {
  const title = useRef<HTMLElement | null>(null);
  const wasOpen = useRef(false);
  const dismissed = useRef(false);
  // Read as it opens, before its search field takes the focus (useOverlayLayer reads its opener so).
  if (open && !wasOpen.current) title.current = titleOfTextAt(document.activeElement);
  wasOpen.current = open;
  // As it closes: called after useOverlayLayer, this runs once the window's
  // layer has let the focus go and before that layer gives it back to its
  // opener, which it then leaves where this puts it.
  useLayoutEffect(() => () => {
    const to = dismissed.current ? title.current : null;
    dismissed.current = false;
    if (to?.isConnected) placeCaret(to, "start", false);
  }, [open]);
  const dismiss = () => {
    dismissed.current = true;
    closeRef.current();
  };
  /** The quick find key pressed again in the window closes it, as Notion's. */
  const dismissKey = (event: KeyboardEvent) => {
    if (!matchesKeyboardShortcut("quickFind", event)) return false;
    event.preventDefault();
    dismiss();
    return true;
  };
  return { dismiss, dismissKey };
}
