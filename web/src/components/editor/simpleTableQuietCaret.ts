/**
 * A caret a join put in a simple table's cell shows no outline on its cell
 * until it moves or the cell's text changes, as Notion's (seen 2026-11-11 and
 * 2026-11-12: ⌫ at the start of the line after a table puts its words in the
 * last cell, the caret at their start and the cell unoutlined; a typed letter
 * or an arrow outlines it, as a press or ↑ into the table does at once).
 * The join marks the page's selection where it left it; the cells' outline
 * (useSimpleTableCellSelection) stays off while the selection is still there.
 */
let quiet: { node: Node; offset: number } | null = null;

/** Marks the selection as a join left it. */
export function quietTableCaret() {
  const selection = document.getSelection();
  quiet = selection?.focusNode ? { node: selection.focusNode, offset: selection.focusOffset } : null;
  // A press outlines the cell even where the caret already is.
  if (quiet) document.addEventListener("pointerdown", () => { quiet = null; }, { capture: true, once: true });
}

/** Whether the selection is still where a join left it; any other lets the mark go. */
export function tableCaretQuiet(selection: Selection | null) {
  if (quiet && selection?.isCollapsed && selection.focusNode === quiet.node && selection.focusOffset === quiet.offset) return true;
  quiet = null;
  return false;
}
