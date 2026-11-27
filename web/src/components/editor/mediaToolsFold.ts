import { type RefObject, useLayoutEffect, useState } from "react";
import type { ContentGraph, GraphBlock } from "@/contentGraph";
import { columnRatio } from "./columnLayoutModel";
import { embedBoxWidth } from "./blockFileMenuModel";
import { BLOCK_INSET, FIGURE_INSET, keptBlockWidth, keptSized } from "./frameKeptWidth";

/**
 * A media block's tools fold to its menu alone ("⋯", in the group's own 28px
 * tile at the same corner) where the block is narrow, as Notion's: where its
 * width is 230 or less, or the column it stands in keeps a ratio of a third
 * or less (its media tools' own rule, read 2026-11-28; seen the same way in
 * two tabs on 2026-11-27 and 2026-11-28 at 1440, dpr 2 and 1). A picture's
 * width is the room its block gives it, measured: pictures across columns
 * 230 wide or less folded, 230.14 and wider kept the whole group; a picture
 * at its own size keeps the whole group in a wide column, cut at its own
 * edges, and folds in a narrow one however small it is (a 100px one folded
 * in a 172.7 room, kept seven tools in a 287.3 one); one pasted or uploaded
 * in the page is sized at the width it is drawn (192 drawn 160: "⋯").
 * Another media block's width is the one its block keeps (Notion's
 * block_width, its frame and 8px each side), none until sized: a video
 * sized to 192 folded, and a video or PDF across a column folded at a ratio
 * of 0.25 or 0.333 and kept its tools at 0.35 (235.9 wide) and in columns
 * that keep no ratio at all, 145.5 wide too.
 */
const MEDIA_TOOLS_MENU_ONLY_WIDTH = 230;
const MEDIA_TOOLS_MENU_ONLY_RATIO = 1 / 3;

export type MediaToolsMode = "image" | "video" | "pdf" | "embed" | "web";

/**
 * Where a picture's room is read: "block" is its figure's laid-out width less
 * the picture's own 2px each side (a picture sized, or at its own size
 * across its column); "frame" is the picture's frame itself, for one drawn
 * at the width its upload kept rather than across its block.
 */
export type MediaToolsRoom = "block" | "frame";

/** How a block's width is found: kept in px, or measured on its figure ("block") or frame plus `add`. */
type WidthSource = { px: number } | { box: MediaToolsRoom; add: number } | null;

function widthSource(mode: MediaToolsMode, block: GraphBlock, room: MediaToolsRoom | undefined): WidthSource {
  if (mode === "image") return room === "frame" ? { box: "frame", add: 0 } : { box: "block", add: -FIGURE_INSET.image };
  const px = keptBlockWidth(block.content) ?? embedBoxWidth(block);
  if (px !== undefined) return { px };
  // A share its sides kept before its px was: the block it is drawn at now.
  if (keptSized(block.content)) return { box: "block", add: BLOCK_INSET - FIGURE_INSET[mode === "web" ? "embed" : mode] };
  return null;
}

// Each graph's blocks by id, made once for its blocks (they are never changed
// in place once read; one pushed to since is told by its length), so every
// media block of a long page finds its column without a pass over the page.
const blockIndexes = new WeakMap<readonly GraphBlock[], { length: number; byId: Map<string, GraphBlock> }>();
function blockById(graph: ContentGraph, id: string) {
  let index = blockIndexes.get(graph.blocks);
  if (!index || index.length !== graph.blocks.length) {
    index = { length: graph.blocks.length, byId: new Map(graph.blocks.map((candidate) => [candidate.id, candidate])) };
    blockIndexes.set(graph.blocks, index);
  }
  return index.byId.get(id);
}

/** The ratio of the column `block` stands in, the nearest one above it; none outside a column or where it keeps none. */
export function mediaColumnRatio(graph: ContentGraph, block: GraphBlock): number | undefined {
  const seen = new Set<string>();
  for (let id = block.parentId; id && !seen.has(id); ) {
    seen.add(id);
    const parent = blockById(graph, id);
    if (!parent || parent.type === "child_page") return undefined;
    if (parent.type === "column") return columnRatio(parent);
    id = parent.parentId;
  }
  return undefined;
}

/**
 * Whether the tools group at `groupRef`, standing in its block's frame,
 * folds to "⋯" alone. A measured width is read before it paints and again
 * as it changes (a drag on its side handles, a narrower window or column).
 */
export function useMediaToolsFold(groupRef: RefObject<HTMLElement | null>, mode: MediaToolsMode, block: GraphBlock,
  ratio: number | undefined, room?: MediaToolsRoom): boolean {
  const byRatio = ratio !== undefined && ratio <= MEDIA_TOOLS_MENU_ONLY_RATIO;
  const source = widthSource(mode, block, room);
  const px = source && "px" in source ? source.px : undefined;
  const box = source && "box" in source ? source.box : undefined;
  const add = source && "box" in source ? source.add : 0;
  const [narrow, setNarrow] = useState(false);
  useLayoutEffect(() => {
    const frame = groupRef.current?.parentElement ?? null;
    const measured = box === "block" ? frame?.parentElement ?? null : box === "frame" ? frame : null;
    if (byRatio || !measured) return undefined;
    const measure = () => {
      const width = measured.getBoundingClientRect().width;
      // A box not laid out yet (a hidden tab, a waiting place) says nothing.
      if (width > 0) setNarrow(width + add <= MEDIA_TOOLS_MENU_ONLY_WIDTH);
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(measured);
    return () => observer.disconnect();
  }, [groupRef, byRatio, box, add]);
  if (byRatio) return true;
  if (px !== undefined) return px <= MEDIA_TOOLS_MENU_ONLY_WIDTH;
  return box !== undefined && narrow;
}
