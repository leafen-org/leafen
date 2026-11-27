import { blockAnchorId } from "@/components/editor/blockHandleModel";
import { shownBlockElement } from "@/lib/shownBlockCopy";
import { MARGIN_ANCHOR_RISE } from "./marginDiscussionLayout";

const rowCell = (blockId: string) => `[data-table-cell^="${CSS.escape(`${blockId}:`)}"]:not([data-table-covered])`;

/**
 * Where a block stands for its discussions: its drawn box — the shown copy's,
 * or the one inside `root` — or, for a simple table's row, which draws no box
 * of its own (its cells stand in the table's grid), its first cell shown.
 */
export function commentBlockElement(blockId: string, root?: ParentNode): HTMLElement | null {
  if (typeof document === "undefined") return null;
  const own = root ? root.querySelector<HTMLElement>(`#${CSS.escape(blockAnchorId(blockId))}`) : shownBlockElement(blockId);
  return own ?? (root ?? document).querySelector<HTMLElement>(rowCell(blockId));
}

// What holds a block's own words as drawn: its box, or a simple table row's
// element, which its cells' words stand in.
const WORDS_HOLDER = '[id^="block-"], [data-table-row-id]';

/** The element holding a block's own words — a table row's as well — as drawn. */
export function commentWordsHolder(blockId: string): HTMLElement | null {
  if (typeof document === "undefined") return null;
  return shownBlockElement(blockId) ?? document.querySelector<HTMLElement>(`[data-table-row-id="${CSS.escape(blockId)}"]`);
}

/** Whether a mark stands in the block's own words, not a block or row inside it. */
export function inCommentWordsHolder(mark: Element, holder: Element) {
  return mark.closest(WORDS_HOLDER) === holder;
}

/** The block whose words an element stands in: a table cell's, its row. */
export function commentWordsBlockId(element: Element): string | null {
  const holder = element.closest<HTMLElement>(WORDS_HOLDER);
  return holder ? holder.getAttribute("data-table-row-id") ?? holder.id.slice("block-".length) : null;
}

/** A simple table row's cell (from 0) as drawn: the field holding its words. */
export function commentCellText(blockId: string, cell: number): HTMLElement | null {
  return document.querySelector<HTMLElement>(`[data-table-cell="${CSS.escape(`${blockId}:${cell}`)}"] [data-rt-editable]`);
}

/**
 * The top a block's cards stand by (MARGIN_ANCHOR_RISE over it, a new
 * comment's field DRAFT_DROP under that): its box's, or for a table row's
 * cell 2 over the cell's, as Notion's card by a cell's words stands 9.5 over
 * the cell's box and its new comment's field 4.5 under it (seen 2026-11-22
 * in Chrome at 1440, a second and a third row, two pages).
 */
export function commentBlockTop(element: Element): number {
  return element.getBoundingClientRect().top - (element.hasAttribute("data-table-cell") ? ROW_RISE : 0);
}

const ROW_RISE = 2;

/** A new comment's field on words stands lower than a card: 6.5px under its block's top, as Notion's (seen 2026-11-12: 226.5 by a block at 220). */
const DRAFT_DROP = 14;
// A discussion on the block itself, no words marked, stands 18 over what the
// block draws and its new comment's field 4 under the block's top.
const BLOCK_CARD_RISE = 18;
const BLOCK_DRAFT_DROP = 4;

/** What a block draws, below any room it keeps over it: a table's grid, else the block. */
function drawnTop(element: Element) {
  const grid = element.closest('[data-kind="table"]') ? element.querySelector('[role="table"]') : null;
  return (grid ?? element).getBoundingClientRect().top;
}

// A heading's words stand under room its block keeps over them: a discussion
// on them stands by their first line, 9 over it.
const FIRST_LINE_RISE = 9;

/**
 * The top a discussion on a block's words stands by: the block's, or — a
 * heading, whose block keeps room over its words — 9 over its words' first
 * line, as Notion's (seen 2026-11-22 in Chrome at 1440: cards 315.5, 430.5
 * and 533.2 by headings 1–3 whose first lines stand at 332, 447 and 550.2 in
 * blocks at 300, 419 and 526.2; a paragraph's 212.5 by its block at 220, its
 * line at 228). A caption's words and a cell's stand by the block or row.
 */
function wordsTop(element: Element) {
  const top = commentBlockTop(element);
  if (element.hasAttribute("data-table-cell")) return top;
  const words = [...element.querySelectorAll<HTMLElement>("[data-rt-editable]")].find((field) => !field.closest("[data-native-caption]"));
  if (!words) return top;
  return Math.max(top, words.getBoundingClientRect().top + (Number.parseFloat(getComputedStyle(words).paddingTop) || 0) - FIRST_LINE_RISE);
}

/**
 * Where a block's discussion card stands, from `origin`. One on words — a
 * block's text, a caption's, a cell's — stands MARGIN_ANCHOR_RISE over the
 * top its words stand by (wordsTop). One on the block itself, no words marked — a
 * table, a picture — stands 18 over what the block draws, as Notion's block
 * discussions (seen 2026-11-22 in Chrome at 1440, two pages: a table's card
 * 258.5 by its grid at 276 under the block's 16px of room; a code block's and
 * a callout's — on the block, unmarked, in Notion — 556 and 824.4 by blocks at
 * 574 and 842.4).
 */
export function commentCardTop(element: Element, origin: number, onWords: boolean) {
  return onWords ? Math.round(wordsTop(element) - origin) - MARGIN_ANCHOR_RISE : Math.round(drawnTop(element) - origin) - BLOCK_CARD_RISE;
}

/** Where a block's new comment's field stands: 14 under a card on words, or 4 under the block's top for the block itself (Notion's 264 by a table at 260, 578 and 846.4 by blocks at 574 and 842.4). */
export function commentDraftTop(element: Element, origin: number, onWords: boolean) {
  return onWords ? Math.round(wordsTop(element) - origin) - MARGIN_ANCHOR_RISE + DRAFT_DROP
    : Math.round(commentBlockTop(element) - origin) + BLOCK_DRAFT_DROP;
}
