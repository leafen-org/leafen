/**
 * Colors in an imported HTML document, as Notion's and Leafen's exports write
 * them (seen 2026-11-13): a block's in its "block-color-<name>" class — on a
 * paragraph, a heading, a quote, a toggle, a callout, on a Notion list
 * block's list (<ul class="block-color-red to-do-list">) and on a Leafen list
 * item — and a run of words' in a <mark> of the "highlight-<name>" class, a
 * background named "<name>_background", Notion's "teal" being green. Notion's
 * API import keeps them all; Notion's HTML import of any other document
 * keeps a heading's alone (seen 2026-11-13: a paragraph's, a list's, a
 * quote's and a mark's were left out). A run of words' underline goes with
 * its colors (htmlUnderline).
 */

const COLORS = new Set(["gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red"]);

/** A color as Notion names it, as a block keeps it: "" for the default or one Leafen hasn't. */
export function notionColorName(name: string) {
  const color = name.trim().replace(/^teal(?=_background$|$)/, "green");
  return COLORS.has(color.replace(/_background$/, "")) ? color : "";
}

function classColor(element: Element, prefix: string) {
  const name = Array.from(element.classList).find((part) => part.startsWith(prefix));
  return name ? notionColorName(name.slice(prefix.length)) : "";
}

/** A block element's color, "" for none. */
export function htmlBlockColor(element: Element | null | undefined) {
  return element ? classColor(element, "block-color-") : "";
}

/** A run of words' text color and background, each "" for none, and whether it is underlined. */
export type TextStyle = { color: string; background: string; underline: boolean };

export const PLAIN_TEXT: TextStyle = { color: "", background: "", underline: false };

/** The style of a mark's words: its color over the style around it. */
export function htmlMarkColors(mark: Element, around: TextStyle): TextStyle {
  const color = classColor(mark, "highlight-");
  if (!color) return around;
  return color.endsWith("_background") ? { ...around, background: color } : { ...around, color };
}

/**
 * Whether an element underlines its words: a <u> or an <ins>, or a span of
 * the "underline" class (Leafen's export), in any document, as Notion's HTML
 * import reads them; and in an export the span of a solid bottom border that
 * Notion's export underlines words with, which Notion's API import keeps
 * (seen 2026-11-13: Notion's HTML import of any other document leaves that
 * border and a text-decoration style out).
 */
export function htmlUnderline(element: Element, exported: boolean) {
  if (element.tagName === "U" || element.tagName === "INS") return true;
  return element.tagName === "SPAN" && (element.classList.contains("underline")
    || exported && (element as HTMLElement).style.borderBottomStyle === "solid");
}

/** The style of an underlined element's words. */
export function underlinedStyle(around: TextStyle): TextStyle {
  return around.underline ? around : { ...around, underline: true };
}

/**
 * The mark opening a block's words that gives the block its color
 * (workspace_import_markdown_marks.go); none for no color.
 */
export function blockColorMark(color = "") {
  return color ? `[](leafen-color:${color})` : "";
}

/**
 * The mark that gives the words after it, up to the next such mark, this
 * style — its colors, and an underline when it names one: a picture with no
 * words, which Markdown keeps inside a link's words.
 */
export function textStyleMark(style: TextStyle) {
  return `![](leafen-text-color:${style.color},${style.background}${style.underline ? ",underline" : ""})`;
}

/**
 * Spaces, the marks of where they are kept (htmlImportSpacing.ts WORDS_GAP),
 * a to-do's box (htmlImportCheckboxes.ts todoWords) and text style marks, as
 * they may stand at the edges of formatted words.
 */
export const TEXT_EDGE = String.raw`(?:[\s\u0001\u0002\u0003]|!\[\]\(leafen-text-color:[a-z_]*,[a-z_]*(?:,underline)?\))*`;
