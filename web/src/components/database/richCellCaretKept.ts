import { pressedByFinger } from "@/lib/lastPress";

/** The editor row of the block a field stands in (BlockFrame's), as drawnHandles finds a block's row. */
const BLOCK_ROW = "[data-block-id] > [role='group'][data-type]";

/**
 * A phone's keyboard put away (iOS's "완료" over it) takes the focus from a
 * field but leaves its caret in it; the field keeps the look it has while it
 * is written in — data-caret-kept, beside :focus in its styles — until the
 * caret leaves it or the focus comes back, as Notion's empty database name
 * keeps its words cut short with no "…" (seen 2026-11-16 on a 402pt phone:
 * the focus on the page's body after "완료", the caret still in the name).
 * The block row it stands in is told too (data-caret-kept-in), beside its
 * :focus-within, for a touch screen to keep that block's "+ ⋮⋮" as Notion's
 * iPad keeps a database's after its keyboard is put away from its name
 * (EditorBlockHandles.module.css). Only after a finger's press: a mouse's
 * press elsewhere moves the caret itself. Returns what stops it.
 */
export function keepWritingLookWhileCaretStays(field: HTMLElement): () => void {
  let row: HTMLElement | null = null;
  const caretInside = () => {
    const anchor = window.getSelection()?.anchorNode;
    return Boolean(anchor && field.contains(anchor));
  };
  const clear = () => {
    document.removeEventListener("selectionchange", check);
    field.removeAttribute("data-caret-kept");
    row?.removeAttribute("data-caret-kept-in");
    row = null;
  };
  const check = () => {
    if (document.activeElement === field || !caretInside()) clear();
  };
  const left = () => {
    if (!pressedByFinger() || !caretInside()) return;
    field.setAttribute("data-caret-kept", "true");
    row = field.closest<HTMLElement>(BLOCK_ROW);
    row?.setAttribute("data-caret-kept-in", "true");
    document.addEventListener("selectionchange", check);
  };
  field.addEventListener("blur", left);
  field.addEventListener("focus", clear);
  return () => {
    field.removeEventListener("blur", left);
    field.removeEventListener("focus", clear);
    clear();
  };
}
