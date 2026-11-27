import type { GraphTextSpan } from "@/contentGraph";
import { readTextWithMime } from "@/lib/clipboard";
import { clipboardServer, otherClipboardServer, otherServerSpans, withoutThreadMarks } from "@/lib/clipboardServer";
import { EDITOR_CLIPBOARD_BOUNDS } from "./editorClipboardPolicy";
import type { SimpleTableMergeSpan } from "./simpleTableMerges";

/** Cells copied from a simple table: rows of cells of their text. */
const LEAFEN_TABLE_CELLS_MIME = "application/x-leafen-table-cells";

type CellGrid = GraphTextSpan[][][];

const escapeHtml = (value: string) => value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");
const plain = (spans: readonly GraphTextSpan[]) => spans.map((span) => span.text).join("");

/**
 * Writes chosen cells as Notion's ⌘C on a cell range does (seen 2026-11-09):
 * the text a tab between cells and a line between rows, the cells as a
 * simple table in HTML, each cell with the color it shows — and the cells
 * themselves for a paste back into a table, which spreads them over cells.
 * A merged cell among them is one cell across its rows and columns in the
 * HTML, the cells under it left out, and comes back merged where they are
 * pasted (Notion, seen 2026-11-10); its text holds the cells one by one.
 * The cells name the server they were copied on, whose pages they mention.
 */
export function writeTableCells(data: DataTransfer, grid: CellGrid, colorOf: (row: number, column: number) => string,
  merges: readonly SimpleTableMergeSpan[] = []) {
  const payload = JSON.stringify({ cells: grid, ...(merges.length ? { merges } : {}), server: clipboardServer() });
  const under = (row: number, column: number) => merges.find((span) => row >= span.top && row <= span.bottom && column >= span.left && column <= span.right);
  const rows = grid.map((cells, row) => `<tr>${cells.map((spans, column) => {
    const merged = under(row, column);
    if (merged && (merged.top !== row || merged.left !== column)) return "";
    const span = merged ? `${merged.right > merged.left ? ` colspan="${merged.right - merged.left + 1}"` : ""}`
      + `${merged.bottom > merged.top ? ` rowspan="${merged.bottom - merged.top + 1}"` : ""}` : "";
    const color = colorOf(row, column);
    return `<td${span} class="${color ? `block-color-${escapeHtml(color)}` : ""}">${escapeHtml(plain(spans))}</td>`;
  }).join("")}</tr>`).join("");
  data.setData("text/plain", grid.map((cells) => cells.map(plain).join("\t")).join("\n"));
  data.setData("text/html", `<table class="simple-table" data-leafen-clipboard-mime="${LEAFEN_TABLE_CELLS_MIME}"`
    + ` data-leafen-clipboard-payload="${escapeHtml(payload)}"><tbody>${rows}</tbody></table>`);
  data.setData(LEAFEN_TABLE_CELLS_MIME, payload);
}

/**
 * The cells a table copied and the merged cells among them, or null when the
 * clipboard holds other things; cells copied on another server name its
 * pages by links to them there (otherServerSpans), and no cell's words come
 * with a comment or suggested-edit thread (withoutThreadMarks).
 */
export function readTableCells(data: DataTransfer): { grid: CellGrid; merges: SimpleTableMergeSpan[] } | null {
  const raw = readTextWithMime(data, LEAFEN_TABLE_CELLS_MIME, EDITOR_CLIPBOARD_BOUNDS);
  if (!raw) return null;
  try {
    const { cells, merges, server } = JSON.parse(raw) as { cells?: unknown; merges?: unknown; server?: unknown };
    if (!Array.isArray(cells) || !cells.length) return null;
    const other = otherClipboardServer(server);
    const grid = cells.map((row) => (Array.isArray(row) ? row : []).map((cell) => {
      const spans = (Array.isArray(cell) ? cell : [])
        .filter((span): span is GraphTextSpan => typeof span === "object" && span !== null && typeof (span as { text?: unknown }).text === "string");
      return other === null ? withoutThreadMarks(spans) : otherServerSpans(spans, other ?? null);
    }));
    const whole = (value: unknown) => typeof value === "number" && Number.isInteger(value) && value >= 0;
    const spans = (Array.isArray(merges) ? merges : []).filter((span): span is SimpleTableMergeSpan => {
      const { top, bottom, left, right } = (span ?? {}) as Record<string, unknown>;
      return whole(top) && whole(bottom) && whole(left) && whole(right) && (top as number) <= (bottom as number) && (left as number) <= (right as number);
    });
    return grid.some((row) => row.length) ? { grid, merges: spans } : null;
  } catch {
    return null;
  }
}

/** Puts the caret at the start or the end of one cell's text, the editor focused. */
export function placeCaretInCell(canvas: HTMLElement | null, rowId: string, column: number, at: "start" | "end") {
  const field = canvas?.querySelector(`[data-table-cell="${CSS.escape(`${rowId}:${column}`)}"] [data-rt-editable]`);
  const root = field?.closest<HTMLElement>('[contenteditable="true"]');
  if (!field || !root) return;
  if (document.activeElement !== root) root.focus({ preventScroll: true });
  const inward = (node: Node) => at === "start" ? node.firstChild : node.lastChild;
  let spot: Node = field;
  // Down to the first or the last text, or before an empty cell's line break.
  for (let child = inward(spot); child && child.nodeName !== "BR" && child.nodeType !== Node.COMMENT_NODE; child = inward(spot)) spot = child;
  const selection = document.getSelection();
  if (!selection) return;
  const offset = at === "start" ? 0 : spot.nodeType === Node.TEXT_NODE ? (spot as Text).length
    : spot.childNodes.length - (spot.lastChild?.nodeName === "BR" ? 1 : 0);
  selection.collapse(spot, Math.max(0, offset));
}
