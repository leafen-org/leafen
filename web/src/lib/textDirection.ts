/**
 * A block's text direction, as Notion's (its help, read 2026-10-02): text
 * blocks find their direction from their words unless set to left-to-right
 * or right-to-left from the block menu's "텍스트 방향", which the menu offers
 * when the app speaks a right-to-left language or "텍스트 방향 제어 항상
 * 표시" is on. Simple tables and code keep left-to-right.
 */
export const TEXT_DIRECTIONS = ["auto", "ltr", "rtl"] as const;
export type TextDirection = (typeof TEXT_DIRECTIONS)[number];

export const TEXT_DIRECTION_BLOCK_TYPES = new Set([
  "paragraph", "heading_1", "heading_2", "heading_3", "heading_4", "bulleted_list_item", "numbered_list_item",
  "to_do", "toggle", "toggle_heading_1", "toggle_heading_2", "toggle_heading_3", "toggle_heading_4", "quote", "callout",
]);

/** The direction a block's content sets; "auto" when it sets none. */
export function blockTextDirection(content: Record<string, unknown> | undefined): TextDirection {
  const value = content?.textDirection;
  return value === "ltr" || value === "rtl" ? value : "auto";
}

// A letter of a script written from the right, or any other letter: the first
// of them in a text sets which way it reads, as a browser's dir="auto" finds it.
const FIRST_STRONG = /(\p{Script=Hebrew}|\p{Script=Arabic}|\p{Script=Syriac}|\p{Script=Thaana}|\p{Script=Nko}|\p{Script=Samaritan}|\p{Script=Mandaic}|\p{Script=Adlam})|\p{L}/u;

/** Which way a text reads by its first letter, left to right with none: the
 * direction a dir="auto" block finds, for what CSS can't ask of it (its
 * marks' sides and arrows, `data-reads`). */
export function wordsDirection(text: string): "ltr" | "rtl" {
  return FIRST_STRONG.exec(text)?.[1] ? "rtl" : "ltr";
}

/** Whether a text has a letter to read its way by: none in an empty line or one of digits and marks. */
export function wordsNameDirection(text: string): boolean {
  return FIRST_STRONG.test(text);
}

// The app's language sets <html dir> (i18n/index.ts), which the page's frame
// reads by; a change of it starts the app again (AccountPreferences).
export function appReadsFromRight() {
  return typeof document !== "undefined" && document.documentElement.dir === "rtl";
}

/**
 * The way a text block's children indent (EditorBlocks.module.css .children):
 * its set direction, or its words' while it finds it from them, as Notion's
 * (seen 2026-11-11: an Arabic list item's children in from the right in a
 * left-to-right app); none for other blocks and a callout, whose text alone
 * finds its own. Its words are what the editor holds of its line, or else
 * its content's spans; the way its line is `shown` by the line's own view
 * where given (nativeTiptapLineWay.ts), which follows each letter typed and
 * a line with none.
 */
export function blockChildrenDirection(block: { type: string; content?: Record<string, unknown> }, words?: string,
  shown?: "ltr" | "rtl"): "ltr" | "rtl" | undefined {
  if (!TEXT_DIRECTION_BLOCK_TYPES.has(block.type)) return undefined;
  const set = blockTextDirection(block.content);
  if (set !== "auto") return set;
  const rich = block.content?.rich;
  if (block.type === "callout") return undefined;
  if (shown) return shown;
  if (words !== undefined) return wordsDirection(words);
  return Array.isArray(rich) ? wordsDirection(rich.map((span) => (typeof span?.text === "string" ? span.text : "")).join("")) : undefined;
}

const RIGHT_TO_LEFT_LANGUAGES = new Set(["ar", "he", "fa", "ur"]);

export function isRightToLeftLanguage(language: string) {
  return RIGHT_TO_LEFT_LANGUAGES.has(language.toLowerCase().split(/[-_]/u)[0] ?? "");
}
