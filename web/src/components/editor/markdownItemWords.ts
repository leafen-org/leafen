import type { BlockType, TextSpan } from "@/lib/types";
import type { PastedBlock, PastedEntry } from "./markdownPasteTypes";

export const LIST_BLOCKS: ReadonlySet<BlockType> = new Set(["bulleted_list_item", "numbered_list_item", "to_do"]);

/**
 * A list item's words over the lines after its mark, as Notion's paste reads
 * them (seen 2026-11-13, CommonMark's list rules):
 * - an item whose mark line holds no words ("-", "1.", the "*" of "- *")
 *   takes the next line that reaches its words as its words, no line break
 *   before them ("-⏎  b" is "b"), or, four columns further in, that code's
 *   lines as its words in code; a line that doesn't reach it never goes on
 *   it ("-⏎b" is an empty item and "b"), and after a blank line it holds
 *   nothing ("-⏎⏎  b" is an empty item and "b" on the page);
 * - such a mark line doesn't break into words: right under a paragraph's or
 *   a list item's words it is a line of them ("x⏎*⏎  b" is "x⏎*⏎b"; "-"
 *   there underlines a heading instead);
 * - while no blank line parts an item's blocks or its list's items, a
 *   paragraph a line starts in it after a block of it is more of its words
 *   ("1. -⏎   b" is "b" holding the empty item, "- a⏎  # h⏎  b" is "a⏎⏎b"
 *   holding the heading), a blank line between them past a heading, list
 *   item or divider, a line break past code and nothing past an equation;
 *   else it stays a paragraph in it ("- *⏎  b⏎⏎  c" holds the empty item,
 *   "b" and "c"). Words that start with a list mark keep it there ("1. -⏎
 *   *b" is "*b");
 * - a line of "=" or "-" right under an item's words, in it, underlines
 *   them: the item is a heading of them, 1 or 2, when nothing more goes in
 *   it ("- a⏎  -" is a heading 2 "a", a to-do's too), else it keeps them and
 *   the line is no word ("- a⏎  ---⏎  b" is "a⏎b", "- a⏎  ===⏎  - c" is
 *   "a" holding "c"); a paragraph a line starts in it after that is more of
 *   its words past a line break, whatever block stands between ("- a⏎  -⏎
 *   -⏎  b" is "a⏎b" holding an empty item).
 */
export type ItemWords = {
  wordless: Set<PastedBlock>;
  // Paragraphs that may be more of an item's words, each with what goes before it.
  pieces: Map<PastedBlock, { paragraph: PastedBlock; joint: string }[]>;
  // The first block an item got after its last words.
  after: Map<PastedBlock, BlockType>;
  loose: Set<PastedBlock>;
  // Items a blank line stands right before.
  parted: Set<PastedBlock>;
  // Items whose words a line underlines, with the heading they make and whether a block went in them after.
  headed: Map<PastedBlock, { level: 1 | 2; held: boolean }>;
};

export function createItemWords(): ItemWords {
  return { wordless: new Set(), pieces: new Map(), after: new Map(), loose: new Set(), parted: new Set(), headed: new Map() };
}

// A block a line puts right in an item; after a blank line it parts the item's blocks.
export function placedIn(words: ItemWords, item: PastedBlock, pasted: PastedBlock, parted: boolean) {
  if (parted) words.loose.add(item);
  if (!words.after.has(item)) words.after.set(item, pasted.type);
  const head = words.headed.get(item);
  if (head) head.held = true;
}

// A line of "=" or "-" under an item's words.
export function underline(words: ItemWords, item: PastedBlock, line: string) {
  words.headed.set(item, { level: line.startsWith("=") ? 1 : 2, held: false });
}

const JOINTS: Partial<Record<BlockType, string>> = { code: "\n", equation: "" };

// What goes between an item's words and a paragraph that may be more of them.
export function jointAfter(words: ItemWords, item: PastedBlock) {
  const after = words.after.get(item);
  return (after && JOINTS[after]) ?? (words.headed.has(item) ? "\n" : "\n\n");
}

export function addPiece(words: ItemWords, item: PastedBlock, paragraph: PastedBlock, joint: string) {
  words.pieces.set(item, [...(words.pieces.get(item) ?? []), { paragraph, joint }]);
  words.after.delete(item);
}

// A list whose items a blank line parts, or one of whose items has its
// blocks parted, has every item's paragraphs stay paragraphs.
function spreadLooseness(words: ItemWords, entries: readonly PastedEntry[]) {
  const open: { depth: number; numbered: boolean; items: PastedBlock[] }[] = [];
  const close = () => {
    const list = open.pop();
    if (list?.items.some((item, at) => words.loose.has(item) || (at > 0 && words.parted.has(item)))) {
      for (const item of list.items) words.loose.add(item);
    }
  };
  for (const { depth, block } of entries) {
    while (open.length > 0 && open[open.length - 1].depth > depth) close();
    const listed = LIST_BLOCKS.has(block.type);
    const numbered = block.type === "numbered_list_item";
    const top = open[open.length - 1];
    if (top?.depth === depth) {
      if (listed && top.numbered === numbered) {
        top.items.push(block);
        continue;
      }
      close();
    }
    if (listed) open.push({ depth, numbered, items: [block] });
  }
  while (open.length > 0) close();
}

// The paragraphs that are more of their items' words (still paragraphs: "==="
// under one makes it a heading).
export function joinedParagraphs(words: ItemWords, entries: readonly PastedEntry[]) {
  spreadLooseness(words, entries);
  const joined = new Set<PastedBlock>();
  for (const [item, pieces] of words.pieces) {
    if (words.loose.has(item)) continue;
    for (const { paragraph } of pieces) if (paragraph.type === "paragraph") joined.add(paragraph);
  }
  return joined;
}

export function joinItemWords(words: ItemWords, joined: ReadonlySet<PastedBlock>) {
  for (const [item, pieces] of words.pieces) {
    let rich: TextSpan[] = item.content?.rich ?? [];
    for (const { paragraph, joint } of pieces) {
      if (!joined.has(paragraph)) continue;
      const more = paragraph.content?.rich ?? [];
      rich = rich.length > 0 && more.length > 0 && joint ? [...rich, { text: joint }, ...more] : [...rich, ...more];
    }
    if (rich === item.content?.rich) continue;
    item.content = { ...item.content, rich };
    item.plainText = rich.map((span) => span.text).join("");
  }
}

// Underlined items nothing went in after: headings of their words.
export function headItems(words: ItemWords) {
  for (const [item, { level, held }] of words.headed) {
    if (held) continue;
    item.type = level === 1 ? "heading_1" : "heading_2";
    item.content = { rich: item.content?.rich ?? [] };
  }
}
