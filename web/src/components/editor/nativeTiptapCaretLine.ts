import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet, type EditorView } from "@tiptap/pm/view";

const caretLineKey = new PluginKey<boolean>("nativeTiptapCaretLine");

/**
 * Marks the text line the caret stands in while the editor has focus: its
 * view carries `data-caret-block`, for a touch screen to show that line's
 * handles alone (EditorBlockHandles.module.css), and, empty, `data-caret-line`,
 * so that line alone shows its hint, as Notion's does (seen 2026-10-31;
 * EditorText.module.css).
 */
export const NativeTiptapCaretLine = Extension.create({
  name: "nativeTiptapCaretLine",
  addProseMirrorPlugins() {
    let editorView: EditorView | null = null;
    return [new Plugin<boolean>({
      key: caretLineKey,
      state: {
        init: () => false,
        apply: (transaction, focused) => transaction.getMeta(caretLineKey) ?? focused,
      },
      view: (view) => {
        editorView = view;
        return { destroy: () => { editorView = null; } };
      },
      // Focus comes with the change that places the caret; a change made at
      // focus itself would put a pressed caret back where it was.
      appendTransaction: (_transactions, _before, state) => {
        const focused = editorView?.hasFocus() ?? false;
        return caretLineKey.getState(state) === focused ? null
          : state.tr.setMeta(caretLineKey, focused).setMeta("addToHistory", false);
      },
      props: {
        handleDOMEvents: {
          // Leaving moves no caret: a step-less change carries it once focus is gone.
          blur: (view) => {
            window.setTimeout(() => {
              if (!view.isDestroyed && !view.hasFocus() && caretLineKey.getState(view.state)) {
                view.dispatch(view.state.tr.setMeta(caretLineKey, false).setMeta("addToHistory", false));
              }
            }, 0);
            return false;
          },
        },
        decorations: (state) => {
          const { selection } = state;
          const line = selection.$from.parent;
          if (!caretLineKey.getState(state) || !selection.empty || line.type.name !== "nativeText") return null;
          const at = selection.$from.before();
          return DecorationSet.create(state.doc, [Decoration.node(at, at + line.nodeSize,
            line.content.size > 0 ? { "data-caret-block": "true" } : { "data-caret-block": "true", "data-caret-line": "true" })]);
        },
      },
    })];
  },
});
