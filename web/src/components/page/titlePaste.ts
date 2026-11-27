import { graphWireBounds } from "@/contentGraphWire.generated";
import { clippedPaste, fitsField } from "@/lib/pasteBound";

/**
 * Pasted text without the blank lines a copy can start or end with, which are
 * no line of its own: "words\n" is one line. Text with no line break stays
 * exactly as copied.
 */
export function pastedLines(pasted: string) {
  const text = pasted.replace(/\r\n?/gu, "\n");
  if (!text.includes("\n")) return text;
  const lines = text.split("\n");
  let from = 0, to = lines.length;
  while (from < to && !lines[from].trim()) from += 1;
  while (to > from && !lines[to - 1].trim()) to -= 1;
  return lines.slice(from, to).join("\n");
}

/**
 * A title with line breaks (a table's name cell keeps them) as a one-line field
 * shows it, whose own browser field would run its lines together.
 */
export function titleOnOneLine(title: string) {
  return title.replace(/\s*\n+\s*/gu, " ");
}

export type TitlePaste =
  | { into: "title"; text: string }
  | { into: "body" }
  /** Nothing to paste. */
  | { into: "nowhere" }
  /** Too long for a title with no body to take it. */
  | { into: "refused" };

function fitsTitle(around: string, text: string) {
  return fitsField(around, text, graphWireBounds.titleRunes);
}

/**
 * What a paste of one line puts in a title's or a text's table cell: its
 * first 100,000 characters (PASTE_CHARACTERS), as Notion's cell takes (seen
 * 2026-11-10: 150,000 pasted into an item's title cell kept 100,000, 300,000
 * into a text property's too); null when the cell can't hold them beside its
 * words (a title's titleRunes, a text's textCharacters).
 */
export function titleCellPaste(line: string, around: string, max = graphWireBounds.titleRunes) {
  const text = clippedPaste(line);
  return fitsField(around, text, max) ? text : null;
}

/**
 * Where text pasted into a page's title goes, as Notion's (seen 2026-11-04):
 * one line joins the title, its first 100,000 characters as Notion's
 * (PASTE_CHARACTERS); several leave the title as it is, selected words
 * too, and go to the start of the body, read as a paste into a body is
 * (pageBodyStart.ts). A line the title can't hold beside its words goes there
 * too, so a paste drops no more than Notion's (its title grows past 600,000
 * characters, seen 2026-11-10; ours holds 200,000, the most Notion's API
 * writes). A title with no body (a full-page database's) keeps the lines,
 * joined.
 */
export function titlePaste(pasted: string, around: string, hasBody = true): TitlePaste {
  const lines = pastedLines(pasted);
  if (!lines) return { into: "nowhere" };
  const line = hasBody || !lines.includes("\n") ? lines : lines.split("\n").map(part => part.trim()).filter(Boolean).join(" ");
  if (!line.includes("\n")) {
    const text = clippedPaste(line.replace(/^#{1,6}\s+/u, ""));
    if (fitsTitle(around, text)) return { into: "title", text };
  }
  return hasBody ? { into: "body" } : { into: "refused" };
}
