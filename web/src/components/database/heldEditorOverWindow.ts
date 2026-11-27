import { cellBoxIntoWindow } from "./cellBoxInWindow";

/**
 * Stands the held cell's editor, drawn open with its view's control's window
 * (chosenCellHeld.ts), over that window, as Notion's: there both are in the
 * page's overlay container and the editor, opened with the window, is drawn
 * over it (seen 2026-11-17 at 1440 in two tabs, after the action only).
 * Leafen's editor is in its cell, inside its block's row, which stacks its
 * own (EditorBlocks .blockRow, z-index 1) under every window (overlayLayer,
 * 2000 up); raising the row would lay the whole table over the window. So the
 * editor alone goes up to the page's top layer as a manual popover, which
 * takes no focus, pinned where it was drawn in its cell (its box, kept inside
 * the window's width as its cell keeps it, cellBoxInWindow.ts) and following
 * the cell as the page scrolls, and back into its cell when it closes. Returns how to put it
 * back, or null where the browser has no popovers (then it stays in its cell).
 */
export function raiseHeldEditor(cell: HTMLElement): (() => void) | null {
  const editor = cell.querySelector<HTMLElement>(":scope > [data-cell-editor]");
  if (!editor || typeof editor.showPopover !== "function") return null;
  editor.style.removeProperty("--cell-editor-shift");
  const drawn = editor.getBoundingClientRect(), from = cell.getBoundingClientRect();
  const rtl = getComputedStyle(editor).direction === "rtl";
  const pin = () => {
    const now = cell.getBoundingClientRect(), width = document.documentElement.clientWidth;
    const inCell = drawn.left + now.left - from.left, left = inCell + cellBoxIntoWindow(inCell, drawn.width, width);
    editor.style.setProperty("inset-block-start", `${drawn.top + now.top - from.top}px`);
    editor.style.setProperty("inset-inline-start", `${rtl ? width - left - drawn.width : left}px`);
  };
  const placed: [string, string][] = [
    ["position", "fixed"], ["inset", "auto"], ["margin", "0"], ["border", "0"], ["color", "inherit"],
    ["translate", "none"], ["box-sizing", "border-box"], ["width", `${drawn.width}px`], ["min-height", `${drawn.height}px`],
  ];
  for (const [name, value] of placed) editor.style.setProperty(name, value);
  pin();
  editor.setAttribute("popover", "manual");
  editor.showPopover();
  window.addEventListener("scroll", pin, { capture: true, passive: true });
  window.addEventListener("resize", pin);
  return () => {
    window.removeEventListener("scroll", pin, { capture: true });
    window.removeEventListener("resize", pin);
    if (editor.matches(":popover-open")) editor.hidePopover();
    editor.removeAttribute("popover");
    for (const [name] of placed) editor.style.removeProperty(name);
    editor.style.removeProperty("inset-block-start");
    editor.style.removeProperty("inset-inline-start");
  };
}
