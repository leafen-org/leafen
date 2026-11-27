import type { ContentGraph } from "@/contentGraph";
import { keptOutOfSyncedBlocks, TAKES_FIRST_CHILD_BLOCK_TYPES, TOGGLE_BLOCK_TYPES } from "@/lib/blockSemantics";
import { blockLine, takesDroppedChildren, type BlockDrop } from "./blockDropModel";
import { editorBlockChildren, editorSelectionRoots } from "./editorGraphModel";
import { syncedBlockKeepingOut } from "./syncedBlockModel";
import { readsRightToLeft } from "@/lib/readingDirection";

// Where the blocks land, the line or tint that shows it, and the block they
// land inside, which tints faintly as Notion's does.
export type DropTarget = BlockDrop & { rect: DOMRect; line?: { left: number; right: number; top: number }; parent?: DOMRect };

// How far left of a block a drag is still over it, as Notion's (seen 2026-11-08):
// further out, to the page's edge, it makes columns with it.
const LEFT_STRIP = 30;

// The page's sides as it reads: where it reads from the right its blocks'
// handles, gutter and text start stand on their right, and "left of" a block
// here is right of it on the screen. `before(x, edge)`: x is on the start
// side of the edge.
function sides(root: Element) {
  const rtl = readsRightToLeft(root);
  return {
    start: (rect: { left: number; right: number }) => rtl ? rect.right : rect.left,
    end: (rect: { left: number; right: number }) => rtl ? rect.left : rect.right,
    before: (x: number, edge: number) => rtl ? x > edge : x < edge,
    out: (edge: number, by: number) => rtl ? edge + by : edge - by,
    rtl,
  };
}

/**
 * The block across a height nearest a pointer out in the page's margin, the
 * deepest of those as near: beside columns, the first column's block on the
 * left and the last one's on the right, as Notion's (seen 2026-11-08).
 */
function blockBeside(root: HTMLElement, x: number, y: number) {
  let nearest: HTMLElement | null = null;
  let nearestDistance = Number.POSITIVE_INFINITY;
  for (const element of root.querySelectorAll<HTMLElement>("[data-block-id]")) {
    const rect = element.getBoundingClientRect();
    if (rect.height <= 0 || y < rect.top || y > rect.bottom) continue;
    const distance = x < rect.left ? rect.left - x : x > rect.right ? x - rect.right : 0;
    if (distance <= nearestDistance) {
      nearest = element;
      nearestDistance = distance;
    }
  }
  return nearest;
}

/** A block's own line: its text, without the blocks inside it. */
function ownLine(block: HTMLElement) {
  return [...block.querySelectorAll<HTMLElement>(".node-nativeText")].find((node) => node.closest("[data-block-id]") === block) ?? null;
}

/** The blocks right inside a block, as drawn. */
function ownChildBlocks(block: HTMLElement) {
  return [...block.querySelectorAll<HTMLElement>("[data-block-id]")].filter((node) => node.parentElement?.closest("[data-block-id]") === block);
}

/** The block a block stands in, when it is one that takes blocks (a column's isn't tinted). */
function holderOf(root: HTMLElement, block: HTMLElement) {
  const holder = block.parentElement?.closest<HTMLElement>("[data-block-id]");
  if (!holder || !root.contains(holder) || !takesDroppedChildren(holder.dataset.blockType ?? "") || holder.dataset.blockType === "column") return undefined;
  return holder.getBoundingClientRect();
}

/** Where a line's own text starts: blocks inside it stand from there. */
function textStart(line: HTMLElement) {
  return sides(line).start((line.querySelector<HTMLElement>("[data-rt-editable]") ?? line).getBoundingClientRect());
}

/** The line where a block inside a block would stand: from the text's start to 6px short of the block's end. */
function insideLine(line: HTMLElement, rect: DOMRect, top: number) {
  const side = sides(line), at = textStart(line), short = side.rtl ? rect.left + 6 : rect.right - 6;
  return { left: Math.min(at, short), right: Math.max(at, short), top };
}

/**
 * The place right after a block, as deep as the pointer reaches, as Notion's
 * (seen 2026-11-07, OP-02-b): right of the start of the text of a block that
 * takes blocks, the place is inside it — its first block when it holds none
 * (a line of text only when it already holds some), else after its last one,
 * looked at the same way; left of it, after the block.
 */
function placeAfter(root: HTMLElement, graph: ContentGraph, block: HTMLElement, x: number): DropTarget | null {
  const id = block.dataset.blockId;
  if (!id) return null;
  const rect = block.getBoundingClientRect(), type = block.dataset.blockType ?? "";
  const after: DropTarget = { id, mode: "after", rect, parent: holderOf(root, block) };
  const line = ownLine(block);
  const collapsed = TOGGLE_BLOCK_TYPES.has(type as never) && graph.blocks.find((candidate) => candidate.id === id)?.content.collapsed === true;
  if (!line || collapsed || !takesDroppedChildren(type) || sides(root).before(x, textStart(line))) return after;
  const last = ownChildBlocks(block).at(-1);
  if (last) return placeAfter(root, graph, last, x) ?? after;
  if (!TAKES_FIRST_CHILD_BLOCK_TYPES.has(type as never)) return after;
  const own = line.getBoundingClientRect();
  return { id, mode: "start", rect, line: insideLine(line, rect, own.bottom - 2), parent: rect };
}

/**
 * Where the blocks dragged (blockIds) land over this element, as Notion's:
 * where the pointer is (pointedDropTarget), or beside a synced block for a
 * synced block it would stand in (besideSyncedBlock).
 */
export function dropTargetAt(
  root: HTMLElement, graph: ContentGraph, target: EventTarget | null, x: number, y: number, blockIds: readonly string[] = [],
): DropTarget | null {
  const pointed = pointedDropTarget(root, graph, target, x, y);
  const drop = pointed && blockIds.length > 0 ? besideOwnPlace(root, graph, pointed, blockIds, x) : pointed;
  return drop && blockIds.length > 0 ? besideSyncedBlock(root, graph, drop, blockIds, y) : drop;
}

// How far in from a picture's sides a drag at the dragged blocks' own place
// makes columns with it, as Notion's (seen 2026-11-08).
const OWN_PLACE_SIDE = 200;

/**
 * Before the block right after the blocks dragged is where they already
 * stand, and Notion's offers another place there (seen 2026-11-08): beside a
 * picture, within 200px of its sides, columns with it, and none between; a
 * block that takes blocks (a list item) takes them as its first.
 */
function besideOwnPlace(root: HTMLElement, graph: ContentGraph, drop: DropTarget, blockIds: readonly string[], x: number): DropTarget | null {
  if (drop.mode !== "before") return drop;
  const block = graph.blocks.find((candidate) => candidate.id === drop.id);
  if (!block) return drop;
  const siblings = editorBlockChildren(graph, block.parentId ?? null);
  const before = siblings[siblings.findIndex((candidate) => candidate.id === block.id) - 1];
  if (!before || !editorSelectionRoots(graph, blockIds).some((root) => root.id === before.id)) return drop;
  const element = root.querySelector<HTMLElement>(`[data-block-id="${CSS.escape(block.id)}"]`);
  if (!element) return drop;
  const rect = element.getBoundingClientRect(), side = sides(root);
  if (block.type === "image") {
    const reach = Math.min(OWN_PLACE_SIDE, rect.width / 2);
    if (Math.abs(x - side.start(rect)) <= reach) return { id: block.id, mode: "left", rect };
    if (Math.abs(side.end(rect) - x) <= reach) return { id: block.id, mode: "right", rect };
    return null;
  }
  const line = ownLine(element);
  const collapsed = TOGGLE_BLOCK_TYPES.has(block.type as never) && block.content.collapsed === true;
  if (!line || collapsed || !TAKES_FIRST_CHILD_BLOCK_TYPES.has(block.type as never)) return drop;
  return { id: block.id, mode: "start", rect, line: insideLine(line, rect, line.getBoundingClientRect().bottom - 2), parent: rect };
}

/**
 * A synced block dragged where it would stand inside a synced block, at any
 * depth, has no place there, as Notion's (seen 2026-11-08,
 * keptOutOfSyncedBlocks): out past the edges it goes into columns with the
 * outermost one, and over it before it, or, nearer the end of the block after
 * it, after that block (Notion's line then stands there, and a drop puts it
 * there). None where that synced block isn't drawn here, as in an editor
 * showing one's content; a block dropped into itself is left to the drop.
 */
function besideSyncedBlock(root: HTMLElement, graph: ContentGraph, drop: DropTarget, blockIds: readonly string[], y: number): DropTarget | null {
  const dragged = new Set(blockIds);
  if (!graph.blocks.some((block) => dragged.has(block.id) && keptOutOfSyncedBlocks(block.type))) return drop;
  const roots = editorSelectionRoots(graph, blockIds);
  const holderId = drop.mode === "inside" || drop.mode === "start" ? drop.id : graph.blocks.find((block) => block.id === drop.id)?.parentId ?? null;
  const outer = syncedBlockKeepingOut(graph, holderId, roots.map((block) => block.type));
  if (!outer) return drop;
  const line = blockLine(graph, outer.id);
  if (roots.some((block) => line.has(block.id))) return drop;
  const element = (id: string) => root.querySelector<HTMLElement>(`[data-block-id="${CSS.escape(id)}"]`);
  const outerElement = element(outer.id);
  if (!outerElement) return null;
  const rect = outerElement.getBoundingClientRect();
  if (drop.mode === "left" || drop.mode === "right") return { id: outer.id, mode: drop.mode, rect };
  const moving = new Set(roots.map((block) => block.id));
  const siblings = editorBlockChildren(graph, outer.parentId).filter((block) => !moving.has(block.id));
  const next = siblings[siblings.findIndex((block) => block.id === outer.id) + 1];
  const nextElement = next ? element(next.id) : null;
  const box = nextElement?.getBoundingClientRect();
  if (next && nextElement && box && y > (rect.top + box.bottom) / 2) {
    return { id: next.id, mode: "after", rect: box, parent: holderOf(root, nextElement) };
  }
  return { id: outer.id, mode: "before", rect, parent: holderOf(root, outerElement) };
}

// How far in from a PDF's sides a drag over its frame makes columns with it,
// and how far inside the block its frame starts and ends (Notion's, seen 2026-11-08).
const PDF_SIDE = 199;
const PDF_FRAME_INSET = 4;

/**
 * Over a PDF's or an embed's frame, which Notion's covers while blocks are
 * dragged (seen 2026-11-08): a PDF's frame makes columns with it within
 * 199px of its sides and has no place between them; an embed's upper half
 * goes before it and its lower half makes columns on the side of the
 * column's middle it is on. Undefined for any other block or place.
 */
function frameDropTarget(
  side: ReturnType<typeof sides>, type: string, rect: DOMRect, x: number, y: number, halves: () => DropTarget,
): DropTarget | null | undefined {
  const id = halves().id;
  if (type === "pdf" && y >= rect.top + PDF_FRAME_INSET && y <= rect.bottom - PDF_FRAME_INSET) {
    const reach = Math.min(PDF_SIDE, rect.width / 2);
    if (Math.abs(x - side.start(rect)) <= reach) return { id, mode: "left", rect };
    if (Math.abs(side.end(rect) - x) <= reach) return { id, mode: "right", rect };
    return null;
  }
  if (type === "embed") {
    if (y - rect.top <= rect.height / 2) return halves();
    return { id, mode: side.before(x, (rect.left + rect.right) / 2) ? "left" : "right", rect };
  }
  return undefined;
}

/**
 * Where blocks dragged over this element land, as Notion's (seen 2026-11-03,
 * 2026-11-07 and 2026-11-08): more than 30px left of a block, or anywhere
 * right of it, makes columns with it, out to the page's edges, whatever the
 * block; nearer its left edge goes before or after the whole block, by which
 * half it is beside. Over a column's empty space goes into the column; over a
 * block's own line, its upper half goes in the place after the block before
 * it (placeAfter: inside that block when the pointer is right of its text's
 * start) or else before the block, and its lower half into it first, when it
 * takes blocks even empty (TAKES_FIRST_CHILD_BLOCK_TYPES) or, a line of text,
 * already holds some, and the pointer is right of its text's start, or else
 * after it; over the middle of a closed toggle goes in it last; over a PDF's
 * or an embed's frame, as its frame takes it (frameDropTarget); anywhere else
 * goes before or after the block, by which half it is over, its middle
 * before. Inside a block that takes blocks, that block tints faintly.
 */
function pointedDropTarget(root: HTMLElement, graph: ContentGraph, target: EventTarget | null, x: number, y: number): DropTarget | null {
  if (!(target instanceof HTMLElement)) return null;
  // Left of the block pointed at, in its gutter, the pointer is as out in the margin.
  const side = sides(root);
  const pointed = target.closest<HTMLElement>("[data-block-id]");
  const blockElement = pointed && !side.before(x, side.start(pointed.getBoundingClientRect())) ? pointed : blockBeside(root, x, y);
  const columnElement = target.closest<HTMLElement>("[data-column-id]");
  if (columnElement?.dataset.columnId && (!blockElement || blockElement.contains(columnElement))) {
    return { id: columnElement.dataset.columnId, mode: "inside", rect: columnElement.getBoundingClientRect() };
  }
  const id = blockElement?.dataset.blockId;
  if (!blockElement || !id) return null;
  const rect = blockElement.getBoundingClientRect();
  const type = blockElement.dataset.blockType ?? "";
  // Columns are not nested, so a column list only takes blocks before or after it.
  // "left" and "right" are before and after it: its right and left read from the right.
  if (type !== "column_list") {
    if (side.before(x, side.out(side.start(rect), LEFT_STRIP))) return { id, mode: "left", rect };
    if (side.before(side.end(rect), x)) return { id, mode: "right", rect };
  }
  const parent = holderOf(root, blockElement);
  // Its upper half, to its middle, is before it, as Notion's (seen 2026-11-08).
  const halves = (): DropTarget => ({ id, mode: y - rect.top <= rect.height / 2 ? "before" : "after", rect, parent });
  // Just left of a synced block, beside it or the blocks in it, is no place,
  // as Notion's (seen 2026-11-08).
  const synced = blockElement.closest<HTMLElement>('[data-block-type="synced_block"]');
  if (synced && root.contains(synced) && side.before(x, side.start(synced.getBoundingClientRect()))) return null;
  const framed = frameDropTarget(side, type, rect, x, y, halves);
  if (framed !== undefined) return framed;
  // Just left of the block, it goes before or after the whole block.
  if (side.before(x, side.start(rect)) || !takesDroppedChildren(type)) return halves();
  if (TOGGLE_BLOCK_TYPES.has(type as never) && graph.blocks.find((block) => block.id === id)?.content.collapsed === true) {
    const offset = y - rect.top;
    if (offset >= rect.height * 0.28 && offset <= rect.height * 0.72) return { id, mode: "inside", rect };
    return halves();
  }
  const line = ownLine(blockElement);
  if (!line) return halves();
  const own = line.getBoundingClientRect();
  const holds = editorBlockChildren(graph, id).length > 0;
  if (x >= own.left && x <= own.right && y <= own.bottom) {
    if (y <= own.top + own.height / 2) {
      const siblings = editorBlockChildren(graph, graph.blocks.find((block) => block.id === id)?.parentId ?? null);
      const before = siblings[siblings.findIndex((block) => block.id === id) - 1];
      const beforeElement = before ? root.querySelector<HTMLElement>(`[data-block-id="${CSS.escape(before.id)}"]`) : null;
      const placed = beforeElement ? placeAfter(root, graph, beforeElement, x) : null;
      // Reaching inside the block before, it goes there; else before this one.
      if (placed && !(placed.mode === "after" && placed.id === before?.id)) return placed;
      return { id, mode: "before", rect, parent };
    }
    // Holding none yet, it goes inside only right of the text's start.
    if (!holds && (!TAKES_FIRST_CHILD_BLOCK_TYPES.has(type as never) || side.before(x, textStart(line)))) return { id, mode: "after", rect, parent };
    // The line runs where a block inside it stands: from the text's start to 6px short of the block's end.
    return { id, mode: "start", rect, line: insideLine(line, rect, own.bottom - 2), parent: rect };
  }
  // In the margin beside the blocks inside it: before or after the one there.
  if (holds && y > own.bottom) {
    const beside = ownChildBlocks(blockElement).find((child) => {
      const box = child.getBoundingClientRect();
      return box.top <= y && y <= box.bottom;
    });
    const childId = beside?.dataset.blockId;
    if (beside && childId) {
      const box = beside.getBoundingClientRect();
      return { id: childId, mode: y - box.top <= box.height / 2 ? "before" : "after", rect: box, parent: rect };
    }
  }
  return halves();
}

/**
 * Where files let go over a block land: before it over its upper half, after
 * it from its middle down, as Notion's (seen 2026-11-15 by a script's drop of
 * a .txt at 12 heights on a 40px line of text: 1–19px down before, 20–39
 * after). A block holding blocks is halved over its own line alone.
 */
export function fileDropMode(block: HTMLElement, y: number): "before" | "after" {
  const rect = block.getBoundingClientRect(), line = ownChildBlocks(block).length ? ownLine(block) : null;
  const bottom = line ? line.getBoundingClientRect().bottom : rect.bottom;
  return y < (rect.top + bottom) / 2 ? "before" : "after";
}

// A 4px line across the block, or down beside it; a block taking blocks inside tints whole.
export function indicatorStyle(target: DropTarget) {
  const { rect } = target;
  if (target.mode === "left" || target.mode === "right") {
    // Before the block on its start side: its right read from the right.
    const onLeft = (target.mode === "left") !== readsRightToLeft();
    return { position: "fixed" as const, left: onLeft ? rect.left - 8 : rect.right + 4, top: rect.top, width: 4, height: rect.height };
  }
  if (target.mode === "inside") return { position: "fixed" as const, left: rect.left, top: rect.top, width: rect.width, height: rect.height, opacity: 0.35 };
  if (target.line) return { position: "fixed" as const, left: target.line.left, top: target.line.top, width: target.line.right - target.line.left, height: 4 };
  const top = target.mode === "before" ? rect.top - 2 : rect.bottom - 2;
  return { position: "fixed" as const, left: rect.left, top, width: rect.width, height: 4 };
}

// The block the blocks land inside: a faint blue 2px inside its edge, as Notion's.
export function holderStyle(rect: DOMRect) {
  return { position: "fixed" as const, left: rect.left + 2, top: rect.top + 2, width: rect.width - 4, height: rect.height - 4,
    background: "rgba(35, 131, 226, 0.07)", zIndex: 3, pointerEvents: "none" as const };
}
