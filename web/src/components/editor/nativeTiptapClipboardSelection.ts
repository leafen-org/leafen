import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { takeDomSelection } from "@/lib/tiptapDomSelection";

/**
 * A paste, copy or cut acts on the words the browser shows chosen, as a key
 * does (takeDomSelection): ⇧← moves the browser's selection at once but the
 * editor hears of it only in a later selectionchange, and a paste on its
 * heels read the editor's older selection — a link pasted over three
 * letters chosen with ⇧← linked the two the editor had heard of (2026-11-13,
 * a table's text cell, two runs in three). Every editor takes it first:
 * a page's text, its title, a database's fields and a formula.
 */
export const NativeTiptapClipboardSelection = Extension.create({
  name: "nativeClipboardSelection",
  // Before every other editor handler, which may read the selection.
  priority: 10_000,
  addProseMirrorPlugins() {
    const first = (view: Parameters<typeof takeDomSelection>[0]) => { takeDomSelection(view); return false; };
    return [new Plugin({
      key: new PluginKey("nativeClipboardSelection"),
      props: { handleDOMEvents: { paste: first, copy: first, cut: first } },
    })];
  },
});
