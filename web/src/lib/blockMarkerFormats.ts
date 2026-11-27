import { compareGraphOrder, type GraphBlock } from "@/contentGraph";

// A bulleted list's "목록 형식" and a quote's "인용 크기", as Notion's (seen
// 2026-10-12): 기본 · 속이 찬 원 · 속이 빈 원 · 정사각형 for a list, and
// 기본 · 크게 for a quote, its text 1.2 times as large. 기본 stores nothing.
// A bullet keeps the format chosen on it alone, as a numbered item does
// (numberedList.ts); one without takes the bullet before it's, and the first
// of a run its depth's (seen 2026-11-11: a bullet added after the list's
// items took its ◦, one added before them stayed •, and with the first item
// gone the rest went back to •).

export type BulletedListFormat = "disc" | "circle" | "square";

export const BULLETED_LIST_FORMATS: readonly BulletedListFormat[] = ["disc", "circle", "square"];

const BULLET_GLYPHS: Record<BulletedListFormat, string> = { disc: "•", circle: "◦", square: "▪" };

export function bulletedListFormat(block: GraphBlock): BulletedListFormat | null {
  const value = block.content.listFormat;
  return value === "disc" || value === "circle" || value === "square" ? value : null;
}

// "기본" takes its glyph from how deep the bullet sits in bullets, as
// Notion's does: • ◦ ▪, then again (seen 2026-10-31).
const DEPTH_FORMATS: readonly BulletedListFormat[] = ["disc", "circle", "square"];

const bulletIndexes = new WeakMap<readonly GraphBlock[], {
  byId: Map<string, GraphBlock>; formats: Map<string, BulletedListFormat | null>;
}>();

// Native graph revisions are immutable: each bullet's format, its own or
// the one before it's, is found once per revision.
function bulletIndex(blocks: readonly GraphBlock[]) {
  const cached = bulletIndexes.get(blocks);
  if (cached) return cached;
  const byId = new Map<string, GraphBlock>(), children = new Map<string | null, GraphBlock[]>();
  for (const block of blocks) {
    byId.set(block.id, block);
    const siblings = children.get(block.parentId ?? null) ?? [];
    siblings.push(block); children.set(block.parentId ?? null, siblings);
  }
  const formats = new Map<string, BulletedListFormat | null>();
  for (const siblings of children.values()) {
    let previous: BulletedListFormat | null = null;
    for (const block of siblings.sort(compareGraphOrder)) {
      previous = block.type === "bulleted_list_item" ? bulletedListFormat(block) ?? previous : null;
      if (block.type === "bulleted_list_item") formats.set(block.id, previous);
    }
  }
  const index = { byId, formats }; bulletIndexes.set(blocks, index);
  return index;
}

export function bulletGlyph(block: GraphBlock, blocks: readonly GraphBlock[] = []) {
  const { byId, formats } = bulletIndex(blocks);
  const format = formats.get(block.id) ?? bulletedListFormat(block);
  if (format) return BULLET_GLYPHS[format];
  let depth = 0;
  const seen = new Set<string>();
  for (let parent = block.parentId ? byId.get(block.parentId) : undefined; parent && !seen.has(parent.id);
    parent = parent.parentId ? byId.get(parent.parentId) : undefined) {
    seen.add(parent.id);
    if (parent.type === "bulleted_list_item") depth += 1;
  }
  return BULLET_GLYPHS[DEPTH_FORMATS[depth % DEPTH_FORMATS.length]];
}

/** The run of adjacent bullets under one parent that holds the block. */
export function bulletedListRun(blocks: readonly GraphBlock[], block: GraphBlock) {
  const siblings = blocks.filter((candidate) => (candidate.parentId ?? null) === (block.parentId ?? null))
    .sort(compareGraphOrder);
  const at = siblings.findIndex((candidate) => candidate.id === block.id);
  if (at < 0) return [block];
  let first = at, last = at;
  while (first > 0 && siblings[first - 1].type === "bulleted_list_item") first -= 1;
  while (last < siblings.length - 1 && siblings[last + 1].type === "bulleted_list_item") last += 1;
  return siblings.slice(first, last + 1);
}

export function quoteSize(block: GraphBlock): "large" | null {
  return block.content.quoteSize === "large" ? "large" : null;
}
