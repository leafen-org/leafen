import { imeComposing } from "@/lib/fieldKeyBoundary";
import { cellLine, cellOnLine } from "./tableCellLine";
import type { TablePart } from "./tableInteractionModel";

/** A cell's own controls: what opens its editor (a number's, a choice's) or changes it (a checkbox). */
export const CELL_CONTROL = '[data-cell-editor] button, [data-cell-editor] input[type="checkbox"], [data-cell-editor] [role="checkbox"]';
const MODIFIERS = new Set(["Shift", "Control", "Alt", "Meta", "CapsLock", "Fn", "OS"]);

/**
 * Gives the cell focus back once its editor closed, unless focus went on
 * somewhere — or, `below`, the cell under it in its column where `below`,
 * the part of the table the cell stands in, has one, as Notion's title and
 * text editors left with Enter do (seen 2026-11-02; the last row's keeps its
 * own, a grouped table's group's last row's too, tablePart). The choice is
 * the line the cell stood on as its editor closed: a row its new value
 * moves or takes out leaves it there, as Notion's (seen 2026-11-14 in a
 * view sorted by a number: Escape kept line 1 chosen as the row went on to
 * line 4, Enter chose line 2) — unless `followRow`, the row's own cell
 * wherever it is.
 * A row the view had no more while it was written (useHeldCellEditor)
 * leaves none chosen, as Notion's (seen 2026-11-14, Escape and Enter).
 * The focus goes in the next frame, the editor closed by then; a key pressed
 * before it, the focus on the page's body meanwhile, chooses the cell at once
 * and goes to it, as if it had come a frame later (2026-11-27: an Escape, an
 * arrow or a letter pressed right after ⌘Z, Enter or Escape went to the body
 * and was lost — the next Escape then counted as the first).
 */
export function keepCellChosen(cell: HTMLElement, below: TablePart | false = false, followRow = false) {
  if (cell.closest('[data-row-held-out="true"]')) return;
  const grid = cell.closest<HTMLElement>('[role="grid"]');
  const row = cellLine(cell), column = cell.dataset.colIndex;
  const onLine = (line: number) => followRow ? null : cellOnLine(grid, line, column);
  let done = false;
  // Whether it chose the cell (or the one below): once, in the next frame or at a key before it.
  const choose = () => {
    if (done) return false;
    done = true;
    window.removeEventListener("keydown", early, true);
    // An editor still open over it (a files list a link was just added in) keeps it unchosen.
    if (cell.querySelector('[aria-expanded="true"]')) return false;
    const active = document.activeElement;
    // Focus its closing picker gave back to the cell's own button is no
    // choice (Notion rings no button there, seen 2026-11-02; measured once
    // 2026-11-15 in Chrome 1440 with no script before the key: a select's
    // picker opened by Enter and closed by Escape left the cell chosen, ↓
    // choosing the one below).
    const handedBack = active instanceof HTMLElement && cell.contains(active) && active.matches(CELL_CONTROL);
    if (active && active !== document.body && !handedBack) return false;
    const next = below && row + 1 < below.end ? (followRow ? cellBelow(cell) : onLine(row + 1)) : null;
    (next ?? onLine(row) ?? cell).focus({ preventScroll: true });
    return true;
  };
  // A ⌘ or Ctrl chord (another ⌘Z) and a modifier alone stay the page's, the cell chosen in its frame.
  const early = (event: KeyboardEvent) => {
    const onPage = event.target === document.body || event.target === document.documentElement;
    if (!onPage || imeComposing(event) || event.metaKey || event.ctrlKey || MODIFIERS.has(event.key) || !choose()) return;
    const chosen = document.activeElement;
    if (!(chosen instanceof HTMLElement) || chosen === document.body) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    chosen.dispatchEvent(new KeyboardEvent("keydown", event));
  };
  window.addEventListener("keydown", early, true);
  window.requestAnimationFrame(() => { choose(); });
}

function cellBelow(cell: HTMLElement) {
  return cellOnLine(cell.closest('[role="grid"]'), cellLine(cell) + 1, cell.dataset.colIndex);
}
