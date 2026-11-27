import { focusEditable, getEditable } from "./focus";
import { dropTextRangeIn } from "./editorScope";
import styles from "./editorStyles.generated.module.css";

type Point = { x: number; y: number };

/** A press in a simple table's own room, off its grid: in its frame, or in the room over the grid. */
export type TableRoomPress = { block: HTMLElement; over: boolean };

/** An editor's own lines as drawn: its text and code fields, not a nested editor's, not a closed toggle's. */
export function shownEditorLines(root: HTMLElement | null) {
  return [...root?.querySelectorAll<HTMLElement>(".node-nativeText, .node-nativeCode") ?? []]
    .filter((field) => field.closest("[data-editor-drop-layer]") === root && !field.querySelector(".node-nativeText, .node-nativeCode"))
    .map((field) => ({ field, rect: field.getBoundingClientRect() }))
    .filter(({ rect }) => rect.height > 0);
}

// Notion draws a simple table as its own block — the grid, 8px of room over
// it and 24px under it, where its "+" bar stands — inside an 8px frame that
// runs across the page's whole width (seen 2026-11-20 in Chrome 1440: a
// table block 260–414 from 278 to 1433, its own 268–406, the grid 276–382).
const TABLE_ROOM_ABOVE = 8;
const TABLE_ROOM_BELOW = 24;

/** Where `y` falls in `block`, a simple table drawn with rows: its frame, the room over its grid, or neither. */
function tableRoomAt(block: HTMLElement, y: number): TableRoomPress | null {
  const rows = [...block.querySelectorAll<HTMLElement>("[data-table-row-id]")].map((row) => row.getBoundingClientRect()).filter((row) => row.height > 0);
  const box = block.getBoundingClientRect();
  if (!rows.length || y < box.top || y > box.bottom) return null;
  const top = rows[0].top, bottom = rows[rows.length - 1].bottom;
  if (y < top - TABLE_ROOM_ABOVE || y > bottom + TABLE_ROOM_BELOW) return { block, over: false };
  return y < top ? { block, over: true } : null;
}

/**
 * The simple table room a press on a table at `y` is in, or null. A press
 * there is no press on the table: it chooses no block and puts no caret in a
 * cell (pressTableRoom). Under the grid, its first 8px choose the table and
 * the next 16px are its "+" bar's, as Notion's.
 */
export function pressedTableRoom(target: Element, y: number): TableRoomPress | null {
  const block = target.closest<HTMLElement>(`.${styles.blockGroup}`);
  return block?.dataset.blockType === "table" ? tableRoomAt(block, y) : null;
}

/**
 * A press in a table's room, as Notion's (seen 2026-11-20 in Chrome 1440, no
 * script in the page before the press, a letter typed after, each twice in
 * two tabs at two times). In its frame the caret goes to the text beside the
 * table: the start of the first line after it, from under it, over it and
 * from the page's margin at that height; with none after it, the end of the
 * last line before it, past a divider between too; with no line at all, the
 * page's title — its end when pressed past the title's words, else its
 * start — when the table is the page's own and not in an editor inside a
 * block. In the room over its grid the press leaves no caret and chooses
 * nothing: a letter typed then goes nowhere, the caret a line had before let
 * go too. Notion's picture, code and equation blocks have no such frame: a
 * press in their own 8px under them chooses the block (seen once each), as
 * BlockFrame's press does here.
 */
export function pressTableRoom({ block, over }: TableRoomPress, lines: readonly { field: HTMLElement }[], pageId: string, x: number) {
  if (over) {
    if (document.activeElement instanceof HTMLElement && document.activeElement.isContentEditable) document.activeElement.blur();
    dropTextRangeIn(block.closest("[data-editor-drop-layer]"));
    return false;
  }
  const beside = (field: HTMLElement, way: number) => !block.contains(field) && Boolean(block.compareDocumentPosition(field) & way);
  const after = lines.find(({ field }) => beside(field, Node.DOCUMENT_POSITION_FOLLOWING));
  const line = after ?? [...lines].reverse().find(({ field }) => beside(field, Node.DOCUMENT_POSITION_PRECEDING));
  const id = line?.field.closest<HTMLElement>("[data-block-id]")?.dataset.blockId;
  if (id) return focusEditable(id, after ? "start" : "end");
  if (block.closest("[data-editor-drop-layer]")?.parentElement?.closest("[data-editor-drop-layer]")) return false;
  const title = getEditable(`title:${pageId}`), box = title ? wordsBox(title) : null;
  const past = !!title && !!box && (getComputedStyle(title).direction === "rtl" ? x < box.left : x > box.right);
  return focusEditable(`title:${pageId}`, past ? "end" : "start");
}

/** How far across a field's words reach, its letters' own boxes, not its lines'. */
function wordsBox(field: HTMLElement) {
  const texts = document.createTreeWalker(field, NodeFilter.SHOW_TEXT), range = document.createRange();
  let left = Infinity, right = -Infinity;
  for (let node = texts.nextNode(); node; node = texts.nextNode()) {
    range.selectNodeContents(node);
    for (const rect of range.getClientRects()) { left = Math.min(left, rect.left); right = Math.max(right, rect.right); }
  }
  return right > left ? { left, right } : null;
}

/** A press beside the blocks, in the page's margin, at the height of a table's frame of this editor is one in that frame. */
export function caretBesideMarginFrame(root: HTMLElement | null, point: Point, pageId: string) {
  const tables = [...root?.querySelectorAll<HTMLElement>(`.${styles.blockGroup}[data-block-type="table"]`) ?? []]
    .filter((table) => table.closest("[data-editor-drop-layer]") === root);
  const room = tables.map((table) => tableRoomAt(table, point.y)).find((found) => found && !found.over);
  return room ? pressTableRoom(room, shownEditorLines(root), pageId, point.x) : false;
}
