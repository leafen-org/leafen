import type { TextSpan } from "./types";

/**
 * A paste puts at most this many characters in a rich text field — a title,
 * a text cell, a caption — as Notion's drops the rest (seen 2026-11-10:
 * 300,000 pasted into a page's title, a text property or an image's caption
 * kept 100,000; 110,000 four-byte characters kept 100,000 — characters, not
 * UTF-16 units).
 */
export const PASTE_CHARACTERS = 100_000;

/** Text's first characters (code points, as the server counts runes), at most `count`. */
export function firstCharacters(text: string, count: number) {
  if (text.length <= count) return text;
  const characters = Array.from(text);
  return characters.length <= count ? text : characters.slice(0, count).join("");
}

/** Pasted text as a rich text field takes it: its first PASTE_CHARACTERS. */
export function clippedPaste(text: string) {
  return firstCharacters(text, PASTE_CHARACTERS);
}

/**
 * Spans' first characters, at most `count`, each span keeping its marks; a
 * mention or an equation that doesn't fit whole is left out.
 */
export function firstSpanCharacters(spans: readonly TextSpan[], count: number): TextSpan[] {
  const kept: TextSpan[] = [];
  let left = count;
  for (const span of spans) {
    const characters = Array.from(span.text);
    if (characters.length <= left) {
      kept.push(span);
      left -= characters.length;
      continue;
    }
    if (left > 0 && !span.mention && !span.equation) kept.push({ ...span, text: characters.slice(0, left).join("") });
    break;
  }
  return kept;
}

/**
 * Whether a field holding `max` characters takes `text` beside the words
 * around where it goes. A character takes at most two UTF-16 units, so what is
 * far too long isn't spread to count it.
 */
export function fitsField(around: string, text: string, max: number) {
  return around.length + text.length <= 2 * max && Array.from(around).length + Array.from(text).length <= max;
}

/**
 * Whether a field holding at most `max` characters (code points, as the
 * server counts runes) takes `next`, `before` being what it held: a change
 * that passes the bound is refused where it is typed — a page's title, a
 * caption, a text cell, a database's description — rather than written and
 * refused by the server, and one that leaves a field already past it no
 * longer than it was is taken.
 */
export function fieldTakes(next: string, max: number, before: () => string) {
  if (next.length <= max) return true;
  const count = Array.from(next).length;
  return count <= max || count <= Array.from(before()).length;
}
