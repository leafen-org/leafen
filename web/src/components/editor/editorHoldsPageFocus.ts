import type { Editor } from "@tiptap/core";

/**
 * Whether an editor still holds the page's focus, for the menus and the
 * chosen words' toolbar that stand by its words ("/", "@", "[[", ":", "+").
 * Only another place in the page taking the focus ends it. Another tab or
 * window brought to the front blurs the editor too, but the page's focus
 * stays on it (document.activeElement), and its menus stay open as they
 * were, as Notion's (its "/" menu, seen 2026-11-20 by hand across a tab
 * switch in the desktop app and in Chrome).
 */
export function editorHoldsPageFocus(editor: Editor): boolean {
  return !editor.isDestroyed && editor.view.hasFocus();
}
