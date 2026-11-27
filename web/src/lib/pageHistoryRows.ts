import { compareGraphOrder } from "@/contentGraph";
import type { PageHistoryBlock } from "@/pageHistoryWire.generated";

// A page's blocks at a version, in the order the page shows them: each under
// its parent by position, columns flattened. How the preview lays them out is
// pageHistoryRowLayout's.

/**
 * A block that shows only as what it holds: columns, and a synced block's
 * own content, which Notion's preview shows as its blocks with no line of
 * its own (seen 2026-10-01).
 */
function holdsOnly(block: PageHistoryBlock) {
  return columnsOnly(block) || (block.type === "synced_block" && !block.content.syncedBlockId && !block.content.sourceId);
}

/** Columns alone: an update lists a synced block as a line of its own (lib/updateLineKinds.ts). */
export const columnsOnly = (block: PageHistoryBlock) => block.type === "column_list" || block.type === "column";

export type PageHistoryPreviewRow = {
  block: PageHistoryBlock;
  depth: number;
  /** A block the version deleted, kept where it stood (pageHistoryPreviewRowsWithRemoved). */
  removed?: boolean;
};

export function pageHistoryPreviewRows(blocks: PageHistoryBlock[], holds = holdsOnly) {
  const byParent = new Map<string, PageHistoryBlock[]>();
  for (const block of blocks) {
    const siblings = byParent.get(block.parentId);
    if (siblings) siblings.push(block);
    else byParent.set(block.parentId, [block]);
  }
  for (const children of byParent.values()) {
    children.sort(compareGraphOrder);
  }
  const rows: PageHistoryPreviewRow[] = [];
  const visited = new Set<string>();
  const walk = (parentId: string, depth: number) => {
    for (const block of byParent.get(parentId) ?? []) {
      if (visited.has(block.id)) continue;
      visited.add(block.id);
      if (!holds(block)) rows.push({ block, depth });
      walk(block.id, holds(block) ? depth : depth + 1);
    }
  };
  walk("", 0);
  for (const block of blocks) {
    if (!visited.has(block.id)) rows.push({ block, depth: 0 });
  }
  return rows;
}

/**
 * A version's rows with the blocks it deleted from the entry before it kept
 * where they stood, as Notion's version preview keeps them under "콘텐츠를
 * 삭제함" (seen 2026-10-01; in its place after the line before it, seen
 * 2026-11-16). Only when both versions' blocks are whole can a block missing
 * from this one be told deleted. A deleted row follows the row it followed in
 * the version before — the last one this version kept before it, with those
 * deleted beside it in their order — not its position: a version's positions
 * are its own reading order (the server numbers each version's blocks in
 * turn), so the line after a deleted one takes its number and the two tied,
 * the deleted line drawn under the next one by its id (2026-11-16, and an
 * updates entry's lines out of order the same way).
 */
export function pageHistoryPreviewRowsWithRemoved(before: PageHistoryBlock[], after: PageHistoryBlock[], holds = holdsOnly) {
  const kept = new Set(after.map((block) => block.id));
  if (before.every((block) => kept.has(block.id))) return pageHistoryPreviewRows(after, holds);
  const following = new Map<string, PageHistoryPreviewRow[]>();
  let anchor = "";
  for (const row of pageHistoryPreviewRows(before, holds)) {
    if (kept.has(row.block.id)) {
      anchor = row.block.id;
      continue;
    }
    following.set(anchor, [...(following.get(anchor) ?? []), { ...row, removed: true }]);
  }
  return [
    ...(following.get("") ?? []),
    ...pageHistoryPreviewRows(after, holds).flatMap((row) => [row, ...(following.get(row.block.id) ?? [])]),
  ];
}

/** Rows grouped as the preview draws them: each block, or a run of deleted blocks side by side as one. */
export function pageHistoryPreviewRowGroups<Row extends PageHistoryPreviewRow>(rows: readonly Row[]) {
  const groups: Array<{ removed: false; row: Row } | { removed: true; rows: Row[] }> = [];
  for (const row of rows) {
    const last = groups.at(-1);
    // An empty line taken out shows nothing, as Notion's preview: no empty
    // "콘텐츠를 삭제함" box for a version that only took blank lines out.
    if (row.removed && row.block.type === "paragraph" && !row.block.plainText.trim()) continue;
    if (row.removed && last?.removed) last.rows.push(row);
    else if (row.removed) groups.push({ removed: true, rows: [row] });
    else groups.push({ removed: false, row });
  }
  return groups;
}

/** A row with the rows a callout holds, drawn inside its box. */
export type PageHistoryPreviewItem = PageHistoryPreviewRow & { inside?: PageHistoryPreviewRow[] };

/**
 * The rows with each callout holding the rows under it, as Notion's preview
 * draws a callout's lines in its box (seen 2026-11-07); a deleted callout
 * keeps only its deleted lines.
 */
export function pageHistoryPreviewCallouts(rows: readonly PageHistoryPreviewRow[]): PageHistoryPreviewItem[] {
  const items: PageHistoryPreviewItem[] = [];
  for (let index = 0; index < rows.length; index += 1) {
    const row = rows[index];
    if (row.block.type !== "callout") {
      items.push(row);
      continue;
    }
    const inside: PageHistoryPreviewRow[] = [];
    while (index + 1 < rows.length && rows[index + 1].depth > row.depth
      && Boolean(rows[index + 1].removed) === Boolean(row.removed)) {
      index += 1;
      inside.push(rows[index]);
    }
    items.push({ ...row, inside });
  }
  return items;
}
