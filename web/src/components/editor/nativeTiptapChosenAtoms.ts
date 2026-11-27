import { Extension } from "@tiptap/core";
import { Plugin, PluginKey, TextSelection } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";

/**
 * Marks the atoms a text selection passes over (mentions, inline equations)
 * with `data-in-selection`, so they show the selection's highlight, which the
 * browser paints in no part that isn't written in, as Notion's do (seen
 * 2026-11-07: Shift+← over a person or page mention paints it whole).
 */
export const NativeTiptapChosenAtoms = Extension.create({
  name: "nativeTiptapChosenAtoms",
  addProseMirrorPlugins() {
    return [new Plugin({
      key: new PluginKey("nativeTiptapChosenAtoms"),
      props: {
        decorations: (state) => {
          const { selection } = state;
          if (!(selection instanceof TextSelection) || selection.empty) return null;
          const chosen: Decoration[] = [];
          state.doc.nodesBetween(selection.from, selection.to, (node, pos) => {
            if (node.type.name === "nativeAtom" && pos >= selection.from && pos + node.nodeSize <= selection.to) {
              chosen.push(Decoration.node(pos, pos + node.nodeSize, { "data-in-selection": "true" }));
            }
          });
          return chosen.length ? DecorationSet.create(state.doc, chosen) : null;
        },
      },
    })];
  },
});
