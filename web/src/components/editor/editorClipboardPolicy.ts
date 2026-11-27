import type { ClipboardBounds } from "@/lib/clipboard";
import type { BlockMarkdownBounds } from "./blockMarkdown";
import type { MarkdownPasteBounds } from "./markdownPaste";
import { ONE_CHANGE_CHARACTERS } from "@/lib/oneChange";

// What one paste, copy or conversion takes (2026-10-13): as much as one saved
// change holds, however many blocks, rows, columns or pieces of text that
// makes (they were held to 500 blocks, 500 rows, 64 columns and 20,000
// pieces, short of what Notion takes). Only nesting stays bounded: the
// parsers walk it recursively, and a paste past it is refused with the reason.
const ANY = Number.MAX_SAFE_INTEGER;

const richtext = {
  maxSpans: ANY, maxTextCharacters: ONE_CHANGE_CHARACTERS, maxHtmlCharacters: ONE_CHANGE_CHARACTERS, maxDomNodes: ANY, maxDomDepth: 64,
};
export const EDITOR_CLIPBOARD_BOUNDS: ClipboardBounds = {
  maxTextCharacters: ONE_CHANGE_CHARACTERS, maxHtmlCharacters: ONE_CHANGE_CHARACTERS, maxEntries: ANY,
  maxEntryCharacters: ONE_CHANGE_CHARACTERS, maxTotalEntryCharacters: ANY,
};
export const EDITOR_MARKDOWN_BOUNDS: BlockMarkdownBounds = {
  maxBlocks: ANY, maxDepth: 32, maxChildrenPerBlock: ANY, maxTableRows: ANY, maxTableColumns: ANY,
  maxStringCharacters: ONE_CHANGE_CHARACTERS, maxOutputCharacters: ANY, richtext,
};
export const EDITOR_PASTE_BOUNDS: MarkdownPasteBounds = { ...EDITOR_MARKDOWN_BOUNDS, clipboard: EDITOR_CLIPBOARD_BOUNDS };
