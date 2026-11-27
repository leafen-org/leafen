import { useEffect, useLayoutEffect, useRef, type MutableRefObject } from "react";
import type { Editor } from "@tiptap/core";
import { focusTitleStartInPlace, getEditable } from "./focus";
import { useNativeTiptapUi } from "./nativeTiptapReactContext";

/**
 * How long the ⌘/ menu put away leaves the caret nowhere before it comes to
 * the page's title, as Notion's: a letter typed right after Escape or a press
 * beside the menu went nowhere (three times), one typed 0.2 s after it too
 * (twice), and one typed 0.3 s after it or later went in before the title's
 * first letter (0.3 s twice, 0.4, 0.5 and 1 s) — measured 2026-11-18 in
 * Chrome at 1440 in three tabs between 20:04 and 20:24, on a page made by
 * hand, nothing run in the page before the keys.
 */
const TITLE_CARET_AFTER_MS = 250;

/**
 * Where the caret goes when a menu a key opened from a line (⌘/, the color
 * and turn shortcuts) closes with nothing chosen meanwhile: back in the line,
 * or, when the ⌘/ menu was put away — Escape on it, or a press beside it,
 * which it takes for itself — at the start of the page's title, the page
 * staying where it is (focusTitleStartInPlace), a moment later
 * (TITLE_CARET_AFTER_MS). Notion's, measured 2026-11-15 in the worker Chrome
 * six times in three tabs (Escape) and 2026-11-18 in two tabs at different
 * times on a page made by hand (Escape, a press on the page beside the menu,
 * a press on the title's row beside its words; on a long page from a line far
 * down, the page not moving until the letter), with no script in the page
 * before the keys and one letter typed after: the letter went in before the
 * title's first letter every time it was typed a moment after. Opened by the
 * handle or with its block chosen (the selection toolbar's "⋯"), the menu
 * holds no caret and gives none back: the block stays chosen and a letter
 * typed goes nowhere, as Notion's (2026-11-18). Escape on the color and turn
 * menus is not measured and keeps the line.
 *
 * It returns what the menu's own putting away (its Escape and outside press)
 * calls; the menu's rows close it themselves.
 */
export function useBlockMenuCaretReturn({ editor, pageId, open, caretHeld, mainMenu, close }: {
  editor: Editor | undefined;
  pageId: string;
  open: boolean;
  /** Set when a key opened the menu from text with the caret in it; read and cleared as it closes. */
  caretHeld: MutableRefObject<boolean>;
  /** The menu opened on its main panel (⌘/), not on colors or "전환". */
  mainMenu: boolean;
  /** Closes the menu. */
  close(): void;
}) {
  const anyChosen = useNativeTiptapUi(state => state.selection.size > 0, false);
  const chosenNow = useRef(anyChosen);
  chosenNow.current = anyChosen;
  const latest = useRef({ mainMenu, close });
  latest.current = { mainMenu, close };
  // Put away by its Escape or a press beside it, not closed by a row.
  const dismissed = useRef(false);
  // The caret on its way to the title, given up by a press or another menu meanwhile.
  const toTitleLater = useRef<(() => void) | undefined>(undefined);
  useEffect(() => () => toTitleLater.current?.(), []);
  // Only a menu that was drawn open closes: a handle a key woke (BlockHandleGate)
  // opens its menu in the same pass it comes up in, before this first runs with
  // the closed state it was first drawn with, which took the held caret back to
  // the line a frame later, under the open menu (2026-11-18: ⌘/ or the
  // selection toolbar's "⋯" left the keys in the text, the next letter over
  // the chosen words and Escape lost on the menu).
  const wasOpen = useRef(false);
  // As it closes, after the menu's layer has let the focus go and before that
  // layer would give it back to the line it was opened from (overlayLayer's
  // restore, in a microtask after): the title takes it, so the layer leaves it
  // be, and lets it go in the next microtask, keys going nowhere until the
  // caret comes.
  useLayoutEffect(() => {
    const closed = wasOpen.current && !open;
    wasOpen.current = open;
    const toTitle = dismissed.current && latest.current.mainMenu;
    if (closed || open) dismissed.current = false;
    if (open) toTitleLater.current?.();
    if (!closed || !caretHeld.current) return;
    caretHeld.current = false;
    if (toTitle && !chosenNow.current && editor && !editor.isDestroyed
      && getEditable(`title:${pageId}`)?.isConnected && focusTitleStartInPlace(pageId)) {
      queueMicrotask(() => {
        const held = document.activeElement;
        if (held instanceof HTMLElement && held !== document.body) held.blur();
        const timer = window.setTimeout(() => {
          giveUp();
          if (!document.activeElement || document.activeElement === document.body) focusTitleStartInPlace(pageId);
        }, TITLE_CARET_AFTER_MS);
        const giveUp = () => {
          window.clearTimeout(timer);
          window.removeEventListener("pointerdown", giveUp, true);
          if (toTitleLater.current === giveUp) toTitleLater.current = undefined;
        };
        toTitleLater.current?.();
        toTitleLater.current = giveUp;
        window.addEventListener("pointerdown", giveUp, true);
      });
      return;
    }
    window.requestAnimationFrame(() => {
      if (chosenNow.current || !editor || editor.isDestroyed) return;
      editor.view.focus();
    });
  }, [open, editor, pageId, caretHeld]);
  return () => {
    dismissed.current = true;
    latest.current.close();
  };
}
