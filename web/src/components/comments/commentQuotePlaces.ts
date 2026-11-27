import type { CommentBody, CommentQuoteBlock } from "@/commentWire.generated";

/**
 * Where in its block a thread's first words lie besides the block's own text:
 * a media block's caption, or a simple table row's cell ("cell:<n>", counting
 * from 0) — Notion's comment on words chosen in one cell is its row's,
 * marking them in that cell (seen 2026-11-22).
 */
export type CommentQuoteField = NonNullable<CommentBody["quoteField"]>;

const QUOTE_CELL = /^cell:(0|[1-9][0-9]*)$/;

/** A quote's field as the API takes it, or nothing for another value. */
export function commentQuoteField(value: unknown): CommentQuoteField | undefined {
  if (value === "caption") return "caption";
  return typeof value === "string" && QUOTE_CELL.test(value) ? value as CommentQuoteField : undefined;
}

/** The cell (from 0) a quote's field names, or null for its block's text or caption. */
export function commentQuoteCell(field: CommentQuoteField | undefined): number | null {
  const match = field ? QUOTE_CELL.exec(field) : null;
  return match ? Number(match[1]) : null;
}

/** The field naming a simple table row's cell, counting from 0. */
export function commentQuoteCellField(cell: number): CommentQuoteField {
  return `cell:${cell}`;
}

/**
 * Where a thread's quote lies beyond its block's own text, as its first
 * comment names it: in its block's caption, and in the blocks the chosen
 * words run on into, in the page's order — Notion's discussion over words
 * chosen across blocks is one thread marking each block's words, and a
 * media block's caption words take a thread too (both seen 2026-11-09). A
 * thread on one block's text names neither and reads as it always has.
 */
export type CommentQuotePlaces = { quoteField?: CommentQuoteField; quoteBlocks?: CommentQuoteBlock[] };

/** A stretch of a thread's text: a block's text, its caption or a cell of its, from start to end in its characters (UTF-16). */
export type CommentTextPlace = { blockId: string; field?: CommentQuoteField; start: number; end: number };

function quoteBlock(value: unknown): CommentQuoteBlock | null {
  const block = value as Partial<CommentQuoteBlock> | null;
  if (!block || typeof block.blockId !== "string" || !block.blockId || typeof block.start !== "number" || typeof block.end !== "number") return null;
  return { blockId: block.blockId, ...(block.field === "caption" ? { field: "caption" as const } : {}), start: block.start, end: block.end };
}

/** The places a stored body names, as the API takes them again. */
export function commentQuotePlacesOf(body: unknown): CommentQuotePlaces {
  const parts = body && typeof body === "object" ? body as { quoteField?: unknown; quoteBlocks?: unknown } : {};
  const blocks = Array.isArray(parts.quoteBlocks) ? parts.quoteBlocks.map(quoteBlock).filter((block): block is CommentQuoteBlock => block !== null) : [];
  const field = commentQuoteField(parts.quoteField);
  return { ...(field ? { quoteField: field } : {}), ...(blocks.length ? { quoteBlocks: blocks } : {}) };
}

/** Whether there is anything beyond the block's own text to name. */
export function hasQuotePlaces(places: CommentQuotePlaces | undefined): places is CommentQuotePlaces {
  return Boolean(places?.quoteField || places?.quoteBlocks?.length);
}

/** Every stretch of a quote: its block's part, then the blocks it runs on into. */
export function commentTextPlaces(blockId: string, start: number, end: number, places?: CommentQuotePlaces): CommentTextPlace[] {
  return [
    { blockId, ...(places?.quoteField ? { field: places.quoteField } : {}), start, end },
    ...(places?.quoteBlocks ?? []).map(({ blockId: id, field, start: from, end: to }) => ({ blockId: id, ...(field ? { field } : {}), start: from, end: to })),
  ];
}

/** The blocks a thread's text lies in, its own first. */
export function commentBlockIds(comment: { blockId?: string | null; body?: unknown }): string[] {
  if (!comment.blockId) return [];
  const others = commentQuotePlacesOf(comment.body).quoteBlocks?.map((block) => block.blockId) ?? [];
  return [...new Set([comment.blockId, ...others])];
}
