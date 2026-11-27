import type { ContentGraph, GraphBlockType } from "@/contentGraph";
import { selectedRootsInOrder } from "./blockSelectionKeyboard";
import { type EditorGraphChange, spreadInColumns, wrapInColumns } from "./columnLayoutModel";
import { insertEditorBlock, moveEditorBlock } from "./editorGraphModel";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { syncedBlockMayGoIn } from "./syncedBlockModel";

/**
 * A "전환" choice: a block type, or columns — `columns` of them with the
 * block in the first, or a column for each block when it is unset. A page
 * `placeIn` goes inside a page the person picks, leaving a link behind.
 */
export type BlockTurnChoice = { key: string; type: GraphBlockType; columns?: number; placeIn?: true };

const TURN_TYPES: readonly GraphBlockType[] = [
  "paragraph", "heading_1", "heading_2", "heading_3", "heading_4", "child_page",
  "bulleted_list_item", "numbered_list_item", "to_do", "toggle", "code", "quote", "callout", "equation", "synced_block",
];
const TOGGLE_HEADING_TYPES: readonly GraphBlockType[] = ["toggle_heading_1", "toggle_heading_2", "toggle_heading_3", "toggle_heading_4"];

const typeChoice = (type: GraphBlockType): BlockTurnChoice => ({ key: type, type });
// "페이지 위치:" comes right after "페이지".
const turnChoices = (): BlockTurnChoice[] => TURN_TYPES.flatMap((type) => type === "child_page"
  ? [typeChoice(type), { key: "page_in", type, placeIn: true }]
  : [typeChoice(type)]);

/**
 * What blocks turn into, as Notion's "전환" lists it (seen 2026-10-01, the
 * block menu and the "/" menu): text, the headings, page and "페이지 위치:",
 * the lists, code, quote, callout, block equation and synced block, then for
 * one block the toggle headings and 2–5 columns. For several, one columns
 * item that gives each block a column of its own comes before the toggle
 * headings. Blocks inside a synced block have no synced block (`synced`
 * false), as Notion's (seen 2026-11-08 — keptOutOfSyncedBlocks).
 */
export function blockTurnChoices(blockCount: number, synced = true): BlockTurnChoice[] {
  const choices = synced ? turnChoices() : turnChoices().filter((choice) => choice.type !== "synced_block");
  if (blockCount > 1) {
    return [...choices, { key: "column_list", type: "column_list" }, ...TOGGLE_HEADING_TYPES.map(typeChoice)];
  }
  return [
    ...choices,
    ...TOGGLE_HEADING_TYPES.map(typeChoice),
    ...[2, 3, 4, 5].map((columns): BlockTurnChoice => ({ key: `column_list_${columns}`, type: "column_list", columns })),
  ];
}

/**
 * What the block menu leaves chosen once "전환" has turned the chosen blocks
 * (`left`, what turnBlocksInto gave back), or null where the choice stands as
 * it was. Turned pages are left unchosen, as Notion's (seen 2026-11-18 in
 * Chrome at 1440 in two tabs, the pointer moved off the page: after "전환 →
 * 페이지" nothing showed chosen and one letter typed went nowhere; after
 * "제목1" the block stayed chosen). A callout stands where its line was, and
 * its lines where a callout was (seen 2026-11-05): those stand chosen.
 */
export function chosenAfterTurn(choice: BlockTurnChoice, chosenIds: readonly string[], left: boolean | readonly string[]) {
  if (choice.type === "child_page") return new Set<string>();
  return typeof left !== "boolean" && left.some((id) => !chosenIds.includes(id)) ? new Set(left) : null;
}

/**
 * Puts blocks, in order, in a new synced block where the first one was; null
 * where that is inside a synced block (keptOutOfSyncedBlocks).
 */
export function wrapInSyncedBlock(graph: ContentGraph, blockIds: readonly string[]): EditorGraphChange | null {
  const [first] = blockIds;
  if (!first || !syncedBlockMayGoIn(graph, graph.blocks.find((block) => block.id === first)?.parentId ?? null)) return null;
  const synced = insertEditorBlock(graph, first, "synced_block");
  let change: EditorGraphChange = { graph: synced.graph, operations: [...synced.operations] };
  // From the last, each block goes first, so no move follows a block moved
  // in the same change: the server checks a move's anchor where it was.
  for (const id of [...blockIds].reverse()) {
    const moved = moveEditorBlock(change.graph, id, synced.block.id, null);
    if (!moved) return null;
    change = { graph: moved.graph, operations: [...change.operations, ...moved.operations] };
  }
  return change;
}

/**
 * Turns blocks into a choice, as Notion's "전환" does (seen 2026-10-01): a
 * synced block or columns take in the outermost of them, in reading order,
 * where the first one was; any other type is each block's new type, a page
 * keeping its text as the title. Returns whether anything changed, or the
 * blocks that stand for the ones asked for.
 */
export function turnBlocksInto(adapter: NativeEditorGraphAdapter, blockIds: readonly string[], choice: BlockTurnChoice) {
  if (choice.type !== "synced_block" && choice.type !== "column_list") return adapter.changeType(blockIds, choice.type);
  const roots = selectedRootsInOrder(adapter.graph, new Set(blockIds));
  if (roots.length === 0) return false;
  const ids = roots.map(({ id }) => id);
  if (choice.type === "synced_block") {
    // A synced block already is one.
    if (roots.every(({ type }) => type === "synced_block")) return false;
    return adapter.changeLayout((graph) => wrapInSyncedBlock(graph, ids));
  }
  return adapter.changeLayout((graph) => ids.length === 1
    ? wrapInColumns(graph, ids[0], choice.columns ?? 2)
    : spreadInColumns(graph, ids));
}
