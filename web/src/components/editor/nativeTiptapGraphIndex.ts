import { compareGraphOrder, type GraphBlock } from "@/contentGraph";
import { bulletGlyph } from "@/lib/blockMarkerFormats";

/** A graph's blocks by id and by parent, in order, with each one's place among its siblings. */
export type NativeTiptapGraphIndex = {
  blocks: readonly GraphBlock[];
  byId: Map<string, GraphBlock>;
  children: Map<string | null, GraphBlock[]>;
  place: Map<string, number>;
};

/** One disposable lookup per native graph revision, shared by all node views. */
export function nativeTiptapGraphIndex(blocks: readonly GraphBlock[]): NativeTiptapGraphIndex {
  const byId = new Map<string, GraphBlock>();
  const children = new Map<string | null, GraphBlock[]>();
  for (const block of blocks) {
    byId.set(block.id, block);
    const siblings = children.get(block.parentId) ?? [];
    siblings.push(block); children.set(block.parentId, siblings);
  }
  const place = new Map<string, number>();
  for (const siblings of children.values()) {
    siblings.sort(compareGraphOrder);
    siblings.forEach((block, index) => place.set(block.id, index));
  }
  return { blocks, byId, children, place };
}

/** Whether a block changed only in its place among its siblings. */
export function sameBesidePlace(next: GraphBlock, last: GraphBlock) {
  return next.id === last.id && next.parentId === last.parentId && next.type === last.type && next.content === last.content
    && next.baseContent === last.baseContent && next.restorePageConversion === last.restorePageConversion;
}

// Blocks a node view draws from the whole page: a synced block and its
// copies (where the original is), a tab block (its tabs and their blocks),
// a table of contents (the page's headings).
const READS_WHOLE_GRAPH = new Set(["synced_block", "tab", "table_of_contents"]);
// Blocks whose children each draw from all of them: a column list's columns
// (their widths), a tab block's tabs (which one is open). A table's rows
// draw from where the others stand and their merged cells (readBeside).
const SIBLINGS_READ = new Set(["column_list", "tab"]);

/**
 * What a node view draws of the graph beside its own block, read from a
 * graph: its block and the blocks around it it may show — the ones before
 * and after it, its children, its parent and the kinds of the blocks above
 * (a list's mark by its depth, a template's or a tab's line), the mark a
 * bullet takes from the run above it, all its siblings in a column list,
 * tab block or table — compared by what they hold, not their places, which
 * a line made renumbers through the whole page. A view reading the whole
 * page reads the graph itself. A view draws again past the frame of a
 * change only when these differ (useNativeTiptapNode): every view of a long
 * page drew again after each line made, 23,600 fibers 0.7 s after an Enter
 * (2026-11-14, 1,022 blocks, CPU ×2).
 *
 * Read once for each graph and block, as every view of a block reads the
 * same (a table row's cells, one view each), and compared once for each two
 * such reads: a 100×300 table's 30,000 cells each read all 300 rows again
 * after each letter typed, some 0.6 s (2026-11-19).
 */
export function besideReads(index: NativeTiptapGraphIndex, id: string | undefined, copied: boolean): readonly unknown[] {
  let known = readsByGraph.get(index);
  if (!known) readsByGraph.set(index, known = new Map());
  const key = `${copied ? "copy" : "own"}:${id ?? ""}`;
  const kept = known.get(key);
  if (kept) return kept;
  const reads = readBeside(index, id, copied);
  known.set(key, reads);
  return reads;
}

const readsByGraph = new WeakMap<NativeTiptapGraphIndex, Map<string, readonly unknown[]>>();

function readBeside(index: NativeTiptapGraphIndex, id: string | undefined, copied: boolean): readonly unknown[] {
  const block = id ? index.byId.get(id) : undefined;
  if (!block) return [id];
  if (copied || READS_WHOLE_GRAPH.has(block.type) || block.content.syncedBlockId || block.content.sourceId) return [index];
  const reads: unknown[] = [];
  const add = (shown: GraphBlock | undefined) => {
    if (shown) reads.push(shown.id, shown.parentId, shown.type, shown.content, shown.baseContent, shown.restorePageConversion);
    else reads.push(undefined);
  };
  add(block);
  const siblings = index.children.get(block.parentId) ?? [];
  const at = index.place.get(block.id) ?? -1;
  add(siblings[at - 1]); add(siblings[at + 1]);
  if (block.type === "bulleted_list_item") reads.push(bulletGlyph(block, index.blocks));
  for (const child of index.children.get(block.id) ?? []) add(child);
  const parent = block.parentId ? index.byId.get(block.parentId) : undefined;
  add(parent);
  // A table's cells show of its other rows only where they stand (the
  // header row) and their merged cells (NativeTiptapTableCellView), not
  // their words: a letter typed in one cell drew all the table's cells again.
  if (parent?.type === "table") for (const sibling of siblings) reads.push(sibling.id, sibling.type, sibling.content.cellSpans);
  else if (parent && SIBLINGS_READ.has(parent.type)) for (const sibling of siblings) add(sibling);
  const seen = new Set<string>();
  for (let above = parent?.parentId ? index.byId.get(parent.parentId) : undefined; above && !seen.has(above.id);
    above = above.parentId ? index.byId.get(above.parentId) : undefined) {
    seen.add(above.id);
    reads.push(above.id, above.type);
  }
  return reads;
}

export function sameReads(left: readonly unknown[], right: readonly unknown[]) {
  if (left === right) return true;
  let known = comparedReads.get(left);
  const kept = known?.get(right);
  if (kept !== undefined) return kept;
  const same = left.length === right.length && left.every((read, index) => Object.is(read, right[index]));
  if (!known) comparedReads.set(left, known = new WeakMap());
  known.set(right, same);
  return same;
}

const comparedReads = new WeakMap<readonly unknown[], WeakMap<readonly unknown[], boolean>>();
