import { Extension } from "@tiptap/core";
import { Plugin, Selection, TextSelection } from "@tiptap/pm/state";

/**
 * A quote whose own words are gone while it holds blocks only holds them
 * (EditorBlocks.module.css draws no line for its words), so a caret left in
 * those words goes to the start of the first block in it, as Notion's (seen
 * 2026-11-11: "zz" over "kk" in a quote, two Backspaces, the caret before "kk").
 */
export const NativeTiptapQuoteHolder = Extension.create({
  name: "nativeTiptapQuoteHolder",
  addProseMirrorPlugins() {
    return [new Plugin({
      appendTransaction: (transactions, _before, state) => {
        const { selection } = state;
        if (!transactions.some(transaction => transaction.docChanged || transaction.selectionSet)
          || !(selection instanceof TextSelection) || !selection.empty) return null;
        const { $from } = selection;
        if ($from.parent.type.name !== "nativeText" || $from.parent.content.size || $from.depth < 1) return null;
        const quote = $from.node($from.depth - 1);
        if (quote.type.name !== "nativeBlock" || quote.attrs.kind !== "quote" || quote.lastChild?.type.name !== "nativeChildren") return null;
        const next = Selection.findFrom(state.doc.resolve($from.after()), 1, true);
        return next ? state.tr.setSelection(next).setMeta("addToHistory", false) : null;
      },
    })];
  },
});
