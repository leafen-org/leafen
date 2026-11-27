import type { GraphBlock } from "@/contentGraph";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { simpleTableGridBoxes } from "./simpleTableMerges";

/**
 * A simple table's column widths, as Notion's (seen 2026-10-11): a column
 * nobody sized is 120px and grows with its text up to 240px, as far as the
 * table's room lets it (autoSimpleTableWidths); one dragged by
 * its edge or fitted keeps its own width, no narrower than 35px. "너비에
 * 맞추기" gives every column the same width so the table fills the page.
 */
const SIMPLE_TABLE_COLUMN_MIN = 35;
const SIMPLE_TABLE_COLUMN_MAX = 4000;
/** An auto column's narrowest and widest, its words wrapping in between. */
const SIMPLE_TABLE_COLUMN_AUTO_MIN = 120;
const SIMPLE_TABLE_COLUMN_AUTO_MAX = 240;

/** Each column's own width, or 0 for one left to its text. */
export function simpleTableColumnWidths(content: Record<string, unknown>, count: number): number[] {
  const stored = Array.isArray(content.columnWidths) ? content.columnWidths : [];
  return Array.from({ length: count }, (_, index) => {
    const width = Number(stored[index]);
    return Number.isFinite(width) && width >= SIMPLE_TABLE_COLUMN_MIN ? Math.min(SIMPLE_TABLE_COLUMN_MAX, width) : 0;
  });
}

export function simpleTableColumnWidth(width: number) {
  return Math.max(SIMPLE_TABLE_COLUMN_MIN, Math.min(SIMPLE_TABLE_COLUMN_MAX, Math.round(width)));
}

/**
 * The table's style: its grid's tracks, a sized column's width or one fitted
 * to its text up to 240px, and for each such column its cells' 120px floor.
 */
export function simpleTableColumnStyle(widths: readonly number[]): Record<string, string> {
  const style: Record<string, string> = {
    "--simple-table-columns": widths.map((width) => width ? `${width}px` : "fit-content(240px)").join(" "),
  };
  widths.forEach((width, index) => { if (!width) style[simpleTableColumnFloor(index)] = "120px"; });
  return style;
}

/**
 * The widths a table's columns take in the room it has, as Notion's (seen
 * 2026-11-12 at 1440: a table of auto columns 120px to 240px laid out in
 * its block less its inset and the 24px strip of its "+"). A sized column
 * keeps its width (`widths`); one left to its text (0) stands between 120px
 * and its words' width up to 240px (`natural`): all at their words' widths
 * when they fit, all at 120px when even that overflows, and in between each
 * the same share of its own stretch — four 240px columns in 675px are
 * 168.75px each; 240, 131, 158, 240 and 120 are 151, 123, 130, 151 and 120.
 * Null when every column stands at its words' width.
 */
export function autoSimpleTableWidths(widths: readonly number[], natural: readonly number[], room: number): number[] | null {
  const fixed = widths.reduce((sum, width) => sum + (width || 0), 0);
  const free = room - fixed;
  const least = widths.map((width) => width || SIMPLE_TABLE_COLUMN_AUTO_MIN);
  const most = widths.map((width, index) => width || Math.min(SIMPLE_TABLE_COLUMN_AUTO_MAX, Math.max(SIMPLE_TABLE_COLUMN_AUTO_MIN, natural[index] ?? 0)));
  const autoLeast = least.reduce((sum, width, index) => sum + (widths[index] ? 0 : width), 0);
  const autoMost = most.reduce((sum, width, index) => sum + (widths[index] ? 0 : width), 0);
  if (autoMost <= free) return null;
  const share = autoMost > autoLeast ? Math.max(0, (free - autoLeast) / (autoMost - autoLeast)) : 0;
  return widths.map((width, index) => width || Math.round((least[index] + (most[index] - least[index]) * share) * 100) / 100);
}

/**
 * The widths autoSimpleTableWidths gives whatever the columns' words, when
 * even each auto column at its narrowest, 120px, overflows the room: every
 * auto column at 120px. Null when the room holds them at that, where their
 * words' widths decide.
 */
export function overflowingSimpleTableWidths(widths: readonly number[], room: number): number[] | null {
  if (widths.every((width) => width > 0)) return null;
  const least = widths.map((width) => width || SIMPLE_TABLE_COLUMN_AUTO_MIN);
  return least.reduce((sum, width) => sum + width, 0) > room ? least : null;
}

/** The property that holds a column's cells' narrowest width. */
export function simpleTableColumnFloor(index: number) {
  return `--simple-table-column-floor-${index}`;
}

/** The widths kept in the table: none past the last sized column. */
export function storedSimpleTableWidths(widths: readonly number[]) {
  let end = widths.length;
  while (end > 0 && !widths[end - 1]) end--;
  return widths.slice(0, end);
}

export function simpleTableWidthsAfterResize(widths: readonly number[], index: number, width: number) {
  return storedSimpleTableWidths(widths.map((current, at) => at === index ? simpleTableColumnWidth(width) : current));
}

/**
 * Every column the same width, together the room the table has, in parts of
 * a pixel as Notion's (seen 2026-11-13: three in 296px 98.67 each, seven in
 * 641px 91.57 each, the table 297 and 642 with its edge).
 */
export function fittedSimpleTableWidths(count: number, room: number) {
  const width = room / Math.max(1, count);
  return Array.from({ length: count }, () => Math.max(SIMPLE_TABLE_COLUMN_MIN, Math.min(SIMPLE_TABLE_COLUMN_MAX, width)));
}

/**
 * A new table's columns: together as wide as the block it is made from or
 * after, as Notion's, which keep that width when the page's width changes
 * (seen 2026-11-04: 720 on a normal page, 1443 on a full-width one).
 */
export function newSimpleTableWidths(blockId: string | null, count: number) {
  const room = blockId ? document.querySelector(`[data-block-id="${blockId}"]`)?.getBoundingClientRect().width ?? 0 : 0;
  return room > 0 ? fittedSimpleTableWidths(count, room) : undefined;
}

/**
 * The room a table's columns have on its page: its scroller's width less the
 * strip its "+" column button keeps at its end, less what a top table's runs
 * past the text, and for one inside another block, whose scroller reaches
 * before it beside its handles (EditorSimpleTable), less the room before it.
 */
function simpleTableRoom(table: HTMLElement) {
  const scroller = table.firstElementChild as HTMLElement | null;
  const canvas = scroller?.firstElementChild as HTMLElement | null;
  if (!scroller || !canvas) return 0;
  const style = getComputedStyle(scroller);
  const pastText = -(parseFloat(style.marginInlineEnd) || 0);
  const nested = (parseFloat(style.marginInlineStart) || 0) < 0 && scroller.dataset.pageWide !== "true";
  const before = nested ? parseFloat(style.paddingInlineStart) || 0 : 0;
  return scroller.clientWidth - pastText - before - (parseFloat(getComputedStyle(canvas).paddingInlineEnd) || 0);
}

/** One color of a list a table keeps (its columns', a row's cells'), or "". */
export function simpleTableListedColor(value: unknown, index: number): string {
  return Array.isArray(value) && typeof value[index] === "string" ? value[index] as string : "";
}

/**
 * A cell's color: its own, else its column's, else its row's — a text color
 * or a background, as a block's; the one that wins is the whole color, as
 * Notion's (seen 2026-11-09: a column's red text over a row's yellow
 * background shows red text on no background, set before or after it).
 */
export function simpleTableCellColor(table: GraphBlock | undefined, row: GraphBlock | undefined, index: number): string {
  return simpleTableListedColor(row?.content.cellColors, index)
    || simpleTableListedColor(table?.content.columnColors, index)
    || (typeof row?.content.color === "string" ? row.content.color : "");
}

/** A table's colors as kept: none past the last, no list without one. */
export function keptSimpleTableColors(colors: readonly string[]): string[] | undefined {
  let end = colors.length;
  while (end > 0 && !colors[end - 1]) end--;
  return end ? colors.slice(0, end).map((color) => color || "") : undefined;
}

/** "너비에 맞추기" from the table's block menu, opened from its handle. */
/** The widths a shown table's columns stand at, sized or fitted to their text; null when it isn't shown. */
export function shownSimpleTableWidths(handle: Element | null, count: number): number[] | null {
  const table = handle?.closest("[data-native-block]")?.querySelector<HTMLElement>("[data-structured-table]");
  // A table with merged cells stands its columns on its grid.
  const grid = table?.querySelector<HTMLElement>("[role=table][data-table-merged]");
  const tracks = grid ? simpleTableGridBoxes(grid, { left: 0, top: 0 }).columns : null;
  if (tracks) return tracks.length === count ? tracks.map((column) => Math.round(column.width)) : null;
  const row = table?.querySelector<HTMLElement>("[data-native-table-cells]");
  const cells = row ? [...row.querySelectorAll<HTMLElement>("[data-native-table-cell]")].filter((cell) => cell.closest("[data-native-table-cells]") === row) : [];
  return cells.length === count ? cells.map((cell) => Math.round(cell.getBoundingClientRect().width)) : null;
}

export function fitSimpleTable(adapter: Pick<NativeEditorGraphAdapter, "updateBlock">, block: GraphBlock, handle: Element | null) {
  const table = handle?.closest("[data-native-block]")?.querySelector<HTMLElement>("[data-structured-table]");
  const room = table ? simpleTableRoom(table) : 0;
  if (room <= 0) return;
  const count = Math.max(1, Number(block.content.tableWidth) || 1);
  adapter.updateBlock(block, { columnWidths: fittedSimpleTableWidths(count, room) });
}
