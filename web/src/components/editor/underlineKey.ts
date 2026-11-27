import type { Mark } from "@tiptap/core";
import { Plugin } from "@tiptap/pm/state";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";

/**
 * Underline's key is ⌘U alone (Ctrl+U elsewhere), in a page's lines and in
 * a title or a field alike: ⌘⇧U stays the product's "상위 페이지 열기"
 * (useAuthenticatedProductShortcuts.ts) wherever the caret is, as Notion's
 * goes up from a caret in a body's line and from words chosen there without
 * underlining them, while its ⌘U underlines them (seen 2026-11-18 in Chrome
 * 1440 on a database's page, the screen looked at after each key and the
 * page's text read back). Tiptap's underline binds "Mod-u" and "Mod-U", and
 * ProseMirror's keymap takes a shifted letter for its unshifted binding too,
 * so the key is read here by its modifiers instead.
 */
export function underlineByItsKey(underline: Mark): Mark {
  return underline.extend({
    addKeyboardShortcuts: () => ({}),
    addProseMirrorPlugins() {
      return [...(this.parent?.() ?? []), new Plugin({ props: {
        handleKeyDown: (_view, event) => underlineKey(event) && this.editor.commands.toggleUnderline(),
      } })];
    },
  });
}

function underlineKey(event: KeyboardEvent) {
  return primaryModifier(event) && !event.shiftKey && !event.altKey && !(event.metaKey && event.ctrlKey) && chordLetter(event) === "u";
}
