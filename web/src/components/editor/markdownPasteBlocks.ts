import type { BlockContent, BlockType, TextSpan } from "@/lib/types";
import {
  admitPastedBlock,
  type MarkdownPasteContext,
} from "./markdownPasteBudget";
import type { PastedBlock } from "./markdownPasteTypes";

export const plainPastedContent = (content?: BlockContent) =>
  (content?.rich ?? []).map((span) => span.text).join("");

export function pastedBlockFromRich(
  type: BlockType,
  richText: TextSpan[],
  content: BlockContent,
  depth: number,
  context: MarkdownPasteContext,
): PastedBlock {
  const nextContent = { ...content, rich: richText };
  const block = {
    type,
    content: nextContent,
    plainText: plainPastedContent(nextContent),
  } satisfies PastedBlock;
  admitPastedBlock(block, depth, context);
  return block;
}

export function createPastedBlock(
  type: BlockType,
  text: string,
  content: BlockContent,
  depth: number,
  context: MarkdownPasteContext,
): PastedBlock {
  const block = {
    type,
    content,
    plainText: plainPastedContent(content) || text,
  } satisfies PastedBlock;
  admitPastedBlock(block, depth, context);
  return block;
}

// The blocks whose words Notion's Markdown paste closes up: a list item's
// (bulleted, numbered, to-do) and a heading's. A paragraph, a quote, a code
// block, a table's cells and an equation keep theirs (seen 2026-11-14).
const CLOSED_UP_BLOCKS = new Set<BlockType>(["bulleted_list_item", "numbered_list_item", "to_do",
  "heading_1", "heading_2", "heading_3", "heading_4"]);

/**
 * A list item's or a heading's words with each run of spaces closed up to
 * its last one, as Notion's Markdown paste reads them (seen 2026-11-14: "a  b"
 * is "a b", "a  b" is "a b", "a  b" is "a b", tabs too; in bold,
 * code and links alike). Line breaks stay, and so do equations and mentions.
 */
export function closeUpWordSpaces(block: PastedBlock) {
  const rich = block.content?.rich;
  if (!CLOSED_UP_BLOCKS.has(block.type) || !rich) return;
  const closed = rich.map((span) => span.equation !== undefined || span.mention ? span
    : { ...span, text: span.text.replace(/[^\S\n]{2,}/gu, (run) => run.slice(-1)) });
  block.content = { ...block.content, rich: closed };
  block.plainText = plainPastedContent(block.content);
}

const ITEM_BLOCKS = new Set<BlockType>(["bulleted_list_item", "numbered_list_item", "to_do"]);

/**
 * A list item's underlined words ("<u>…</u>") that don't start its words, as
 * Notion's Markdown paste reads them (seen 2026-11-14): each run of them
 * leaves the words for a paragraph of its own in the item, ahead of what it
 * holds, spaces as written ("- a<u>b  c</u>" is "a" holding "b  c"
 * underlined, "- a <u>b</u> c" is "a  c" holding "b"); the words keep the
 * rest, their end's spaces off. A to-do's mark stands before its words, so
 * any of a to-do's go ("- [ ] <u>a</u>b" is "b" holding "a"); a heading's,
 * a quote's and a paragraph's stay in their words.
 */
export function underlinedPieces(block: PastedBlock, depth: number, context: MarkdownPasteContext): PastedBlock[] {
  if (!ITEM_BLOCKS.has(block.type)) return [];
  const kept: TextSpan[] = [];
  const pieces: TextSpan[][] = [];
  let leading = block.type !== "to_do";
  let piece: TextSpan[] | null = null;
  for (const span of block.content?.rich ?? []) {
    if (span.underline && !leading) {
      if (!piece) pieces.push(piece = []);
      piece.push(span);
      continue;
    }
    piece = null;
    if (!span.underline) leading = false;
    kept.push(span);
  }
  if (!pieces.length) return [];
  const last = kept.at(-1);
  if (last && last.equation === undefined && !last.mention) kept[kept.length - 1] = { ...last, text: last.text.trimEnd() };
  block.content = { ...block.content, rich: kept.filter((span) => span.text || span.equation !== undefined || span.mention) };
  block.plainText = plainPastedContent(block.content);
  return pieces.map((words) => pastedBlockFromRich("paragraph", words, {}, depth + 1, context));
}
