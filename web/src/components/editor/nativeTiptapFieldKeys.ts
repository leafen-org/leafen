import { Extension } from "@tiptap/core";
import { TextSelection } from "@tiptap/pm/state";
import { projectedBlocks } from "./nativeTiptapBinding";
import { nativeTiptapTextFields } from "./nativeTiptapTextFields";
import { nativeTiptapInstance, newNativeTiptapAttrs } from "./nativeTiptapIdentity";
import { joinBackward } from "./nativeTiptapMergeKeys";
import { mergedCellCaret, tableCellElementAt } from "./simpleTableMerges";
import { imeComposing } from "@/lib/fieldKeyBoundary";

/** Native field boundaries are policy; text insertion stays in PM commands. */
export const NativeTiptapFieldKeys = Extension.create({
  name: "nativeFieldKeys", priority: 1100,
  addKeyboardShortcuts() {
    const active = () => {
      if (!this.editor.isEditable || imeComposing(this.editor.view)) return null;
      const { selection } = this.editor.state;
      return nativeTiptapTextFields(projectedBlocks(this.editor.state.doc)).find(field =>
        field.field !== "text" && selection.from >= field.contentFrom && selection.to <= field.contentFrom + field.node.content.size);
    };
    const enter = () => {
      const field = active();
      if (!field) return false;
      if (field.field !== "caption") return this.editor.commands.insertContent({ type: "hardBreak" });
      const { state, view } = this.editor;
      const blocks = projectedBlocks(state.doc);
      const siblings = blocks.filter(block => block.projectionParentId === field.block.projectionParentId);
      const next = siblings[siblings.findIndex(block => block.id === field.block.id) + 1];
      const tr = state.tr;
      if (next?.container.attrs.kind === "paragraph" && !next.node.content.size) {
        tr.setSelection(TextSelection.create(tr.doc, next.contentFrom));
      } else {
        const paragraph = state.schema.nodes.nativeBlock.create(newNativeTiptapAttrs("paragraph", {}, nativeTiptapInstance(field.block.container)),
          state.schema.nodes.nativeText.create());
        const at = field.block.contentFrom - 2 + field.block.container.nodeSize;
        tr.insert(at, paragraph).setSelection(TextSelection.create(tr.doc, at + 2));
      }
      view.dispatch(tr.scrollIntoView());
      return true;
    };
    // Enter in a table's cell goes to the cell below, Shift+Enter breaking
    // the line, as Notion's (seen 2026-11-02); the caret lands at the end of
    // its text, as Tab's does. Measured once 2026-11-15 in Chrome 1440 with
    // no script before the key, the keys sent by the extension: Notion's
    // first Enter, and Tab, from a cell's end took the caret to that cell's
    // start, the next going on to the cell below's (Tab: the next cell's)
    // end — the stop that keys played into the page gave Shift+Tab below,
    // so the rule stays until real keys check it. The last row's stays. A
    // merged cell's below is under its last row, and a cell a merged cell
    // stands over gives the caret to the merged cell.
    const cellBelow = () => {
      const field = active();
      if (!field || typeof field.field !== "number") return false;
      const blocks = projectedBlocks(this.editor.state.doc);
      const rows = blocks.filter(block => block.projectionParentId === field.block.projectionParentId);
      const tall = Number(tableCellElementAt(this.editor.view, field.contentFrom)?.getAttribute("data-table-rows")) || 1;
      const below = rows[rows.findIndex(block => block.id === field.block.id) + tall];
      const cell = below && nativeTiptapTextFields([below]).find(candidate => candidate.field === field.field);
      if (cell) this.editor.view.dispatch(this.editor.state.tr.setSelection(TextSelection.create(this.editor.state.doc,
        mergedCellCaret(this.editor.view, cell.contentFrom, true) ?? cell.contentFrom + cell.node.content.size)).scrollIntoView());
      return true;
    };
    const tab = (backward: boolean) => {
      const field = active();
      if (!field || typeof field.field !== "number") return false;
      // Shift+Tab goes straight to the end of the cell before's text, the row
      // before's last at a row's start, wherever the caret stands in its own
      // (Notion's, seen 2026-11-11 with the desktop app's own keys and
      // 2026-11-12 in Chrome; a stop at the cell's start first came from
      // keys played into Notion's page, not from typing).
      // The cells a merged cell stands over are passed by (simpleTableMerges).
      const cells = nativeTiptapTextFields(projectedBlocks(this.editor.state.doc)).filter(candidate =>
        typeof candidate.field === "number" && candidate.block.projectionParentId === field.block.projectionParentId
        && (candidate.block.id === field.block.id && candidate.field === field.field || !tableCellElementAt(this.editor.view, candidate.contentFrom)?.hasAttribute("data-table-covered")));
      const index = cells.findIndex(candidate => candidate.block.id === field.block.id && candidate.field === field.field);
      const next = cells[backward ? index - 1 : index + 1];
      // Tab at the table's last cell and Shift+Tab at its first stay, the
      // caret where it was, as Notion's (seen 2026-10-31 and 2026-11-11;
      // measured once 2026-11-15 in Chrome with the extension's keys, Tab at
      // the last cell's end kept the cell but put the caret at its start,
      // the same played-key stop as above).
      if (!next) return true;
      this.editor.view.dispatch(this.editor.state.tr.setSelection(TextSelection.create(this.editor.state.doc,
        next.contentFrom + next.node.content.size)).scrollIntoView());
      return true;
    };
    return {
      Enter: () => cellBelow() || enter(), "Shift-Enter": enter, Tab: () => tab(false), "Shift-Tab": () => tab(true),
      Backspace: () => {
        const field = active(), { selection } = this.editor.state;
        if (!field || !selection.empty || selection.$from.parentOffset !== 0) return false;
        // A caption's start joins it with the text before its block, as a line's (nativeTiptapMergeKeys); a cell's stays.
        if (field.field === "caption") joinBackward(this.editor);
        return true;
      },
      Delete: () => !!active() && this.editor.state.selection.empty
        && this.editor.state.selection.$from.parentOffset === this.editor.state.selection.$from.parent.content.size,
    };
  },
});
