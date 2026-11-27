import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";

/**
 * Whether an editor's text box can be written in, said to assistive
 * technology: every Tiptap editor's root is a role="textbox" (Tiptap gives it
 * one when its owner gives none), and its aria-readonly is "true" while the
 * editor is not editable and "false" while it is — read from the editor's own
 * editable state, which ProseMirror reads again at each setEditable, so the
 * mark follows a page locked or unlocked, taken to the trash or back, or a
 * suggesting mode turned on. A page opened read only (a public share or site,
 * a guest's view, a locked page, the trash, a version's preview) was heard as
 * a box to type in: its root said nothing (2026-11-18). An owner's own
 * attributes come first in ProseMirror, so no editor names aria-readonly itself.
 */
export const TiptapReadOnlyMark = Extension.create({
  name: "readOnlyMark",
  addProseMirrorPlugins() {
    const editor = this.editor;
    return [new Plugin({
      key: new PluginKey("readOnlyMark"),
      props: { attributes: (): Record<string, string> => ({ "aria-readonly": String(!editor.isEditable) }) },
    })];
  },
});
