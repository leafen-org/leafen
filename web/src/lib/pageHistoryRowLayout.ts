import type { GraphBlock } from "@/contentGraph";
import { blockRunOf, type BlockRun } from "@/components/editor/blockRun";
import type { PageHistoryBlock } from "@/pageHistoryWire.generated";
import type { TextSpan } from "./types";
import { bulletGlyph } from "./blockMarkerFormats";
import { numberedListMarkerColumn, type NumberedListMarker } from "./numberedList";
import type { PageHistoryPreviewRow } from "./pageHistoryRows";

// A version's rows laid out as the editor lays its blocks out, which Notion's
// preview does too (measured 2026-11-12, one renderer): each block's children
// stand in from its start as the editor's do (EditorBlocks.module.css
// --children-indent and the 2px of .children: 30 under a paragraph, 32 under
// a list item, a to-do or a toggle, 25 under a quote, 26 elsewhere); a list
// item's marker is the editor's (• ◦ ▪ by its depth in bullets, 1. a. i. in
// numbered items); a closed toggle shows no children; and the rows space as
// the editor's boxes do, flattened: a block's 5px top and bottom (8 under a
// quote) stand around its line and its children, so a block that holds
// children ends with its own bottom after them, a run block (blockRun) next
// to one of its run or at the edge of a run block it is in keeps none there,
// and a child under a line (a paragraph's, or a run block's that isn't of a
// run) stands 1px up into it and ends 1px further down.

const TOGGLES = new Set(["toggle", "toggle_heading_1", "toggle_heading_2", "toggle_heading_3", "toggle_heading_4"]);
const LIST_INDENTED = new Set(["bulleted_list_item", "numbered_list_item", "to_do", ...TOGGLES]);
/** Blocks whose children stand where they would without them: columns, a synced block's own content, a callout's box, a table's rows. */
const NO_INDENT = new Set(["column_list", "column", "synced_block", "callout", "table"]);
/** A block's own top and bottom around its line and children (.blockGroup padding-block), and a quote's foot. */
const GROUP_SPACE = 5;
const QUOTE_FOOT = 8;

export type PageHistoryRowLayout = {
  /** How far the row stands in from the column's start, in px. */
  indent: number;
  /** A list item's marker as the editor writes it. */
  marker?: NumberedListMarker;
  /** Of the block's own top, what the row keeps not: a run block's carried on from the one before. */
  topCut: number;
  /** Of the block's own bottom, what the row keeps not: a run block's carried on, or one that comes after its children. */
  bottomCut: number;
  /** Space over the row past its own: a first child's 1px up into the line it is under. */
  over: number;
  /** Space under the row past its own: its 1px under a line and the bottoms of the blocks it ends. */
  under: number;
  /** For each block the row ends, how far past the row's own bottom its box (a background's) ends. */
  ends: Map<string, number>;
  /** A quote that holds rows, whose line runs on down them. */
  quoteHolds: boolean;
};

function asGraphBlock(block: PageHistoryBlock): GraphBlock {
  return { ...block, parentId: block.parentId || null, afterId: null } as GraphBlock;
}

function spansOf(block: PageHistoryBlock): readonly TextSpan[] | null {
  const rich = block.content.rich;
  return Array.isArray(rich) ? rich.filter((span): span is TextSpan => typeof (span as TextSpan)?.text === "string") : null;
}

function childrenIndent(type: string) {
  if (NO_INDENT.has(type)) return 0;
  if (type === "paragraph") return 30;
  if (LIST_INDENTED.has(type)) return 32;
  if (type === "quote") return 25;
  return 26;
}

/**
 * The rows a version's preview draws, closed toggles' children left out, each
 * with its layout. `blocks` is every block the rows stand in, each once.
 */
export function pageHistoryRowLayout<Row extends PageHistoryPreviewRow>(rows: readonly Row[], blocks: readonly PageHistoryBlock[]) {
  const byId = new Map<string, PageHistoryBlock>();
  for (const block of blocks) byId.set(block.id, block);
  for (const row of rows) if (!byId.has(row.block.id)) byId.set(row.block.id, row.block);
  // A closed toggle's children are not drawn, as Notion's preview draws a
  // toggle closed or open as the page shows it (seen 2026-11-12).
  const shown: Row[] = [];
  let closedDepth = Infinity;
  for (const row of rows) {
    if (row.depth > closedDepth) continue;
    closedDepth = TOGGLES.has(row.block.type) && row.block.content.collapsed === true ? row.depth : Infinity;
    shown.push(row);
  }
  const graph = [...byId.values()].map(asGraphBlock);
  const graphById = new Map(graph.map((block) => [block.id, block]));
  const isShown = new Set(shown.map((row) => row.block.id));
  const children = new Map<string, Row[]>();
  for (const row of shown) {
    const list = children.get(row.block.parentId);
    if (list) list.push(row);
    else children.set(row.block.parentId, [row]);
  }
  const runs = new Map<string, BlockRun | undefined>();
  const runOf = (block: PageHistoryBlock) => {
    if (!runs.has(block.id)) runs.set(block.id, blockRunOf(block.type, () => spansOf(block)));
    return runs.get(block.id);
  };
  const parentOf = (block: PageHistoryBlock) => byId.get(block.parentId);
  const shownParentOf = (block: PageHistoryBlock) => {
    const parent = parentOf(block);
    return parent && isShown.has(parent.id) ? parent : undefined;
  };
  const isLastChild = (block: PageHistoryBlock) => children.get(block.parentId)?.at(-1)?.block.id === block.id;
  // Whether the block, as a first or last child, stands 1px into the line it is under.
  const underLine = (block: PageHistoryBlock) => {
    const parent = shownParentOf(block);
    if (!parent) return false;
    const parentRun = runOf(parent);
    return parent.type === "paragraph" && !parentRun ? true : Boolean(parentRun) && !runOf(block);
  };
  const indents = new Map<string, number>();
  const indentOf = (block: PageHistoryBlock): number => {
    const known = indents.get(block.id);
    if (known !== undefined) return known;
    indents.set(block.id, 0);
    const parent = parentOf(block);
    const value = parent ? indentOf(parent) + childrenIndent(parent.type) : 0;
    indents.set(block.id, value);
    return value;
  };
  // The bottom a block keeps after its line and children: none when it runs on.
  const feet = new Map<string, number>();
  const layouts = new Map<string, PageHistoryRowLayout>();
  for (const row of shown) {
    const { block } = row;
    const siblings = children.get(block.parentId) ?? [];
    const place = siblings.indexOf(row);
    const run = runOf(block);
    const inRun = Boolean(shownParentOf(block) && runOf(shownParentOf(block)!));
    const first = place === 0, last = place === siblings.length - 1;
    const runsIn = Boolean(run) && (first ? inRun : runOf(siblings[place - 1].block) === run);
    const runsOn = Boolean(run) && (last ? inRun : runOf(siblings[place + 1].block) === run);
    const foot = runsOn ? 0 : block.type === "quote" ? QUOTE_FOOT : GROUP_SPACE;
    feet.set(block.id, foot);
    const holds = Boolean(children.get(block.id)?.length);
    const graphBlock = graphById.get(block.id)!;
    layouts.set(block.id, {
      indent: indentOf(block),
      marker: block.type === "bulleted_list_item" ? { text: bulletGlyph(graphBlock, graph), width: 24, roman: false }
        : block.type === "numbered_list_item" ? numberedListMarkerColumn(graph, graphBlock) : undefined,
      topCut: runsIn ? GROUP_SPACE : 0,
      bottomCut: holds ? (block.type === "quote" ? QUOTE_FOOT : GROUP_SPACE) : runsOn ? GROUP_SPACE : 0,
      over: first && underLine(block) ? -1 : 0,
      // A block that holds rows takes its 1px after them (below).
      under: last && !holds && underLine(block) ? 1 : 0,
      ends: new Map(),
      quoteHolds: holds && block.type === "quote",
    });
  }
  // A block that holds rows ends after its last one with its bottom: under
  // that row, or under the callout it is in, whose box stands in the column
  // as one row (the callout's own end is its box's).
  for (const row of shown) {
    const layout = layouts.get(row.block.id)!;
    if (row.block.type !== "callout" && children.get(row.block.id)?.length) continue;
    for (let block = row.block, parent = shownParentOf(block); parent && parent.type !== "callout" && isLastChild(block);
      block = parent, parent = shownParentOf(block)) {
      // Its box ends 1px over its bottom.
      layout.ends.set(parent.id, layout.under - 1);
      layout.under += feet.get(parent.id)! + (isLastChild(parent) && underLine(parent) ? 1 : 0);
    }
  }
  return { rows: shown, layouts };
}
