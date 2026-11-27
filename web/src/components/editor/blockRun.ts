import type { Fragment } from "@tiptap/pm/model";
import type { TextSpan } from "@/lib/types";
import { fragmentToNativeRichText } from "./nativeTiptapInline";

/**
 * The run a block spaces with, as Notion's (its pages and its code, seen
 * 2026-11-11): blocks of one run next to each other stand 1px apart instead
 * of 6, and a run block's own first and last children of a run carry its run
 * on (EditorBlocks.module.css, `data-run`).
 * - "list": bulleted, numbered, to-do and toggle items, of any of the kinds;
 * - "page": page blocks, links to pages, full-page databases, and a line that
 *   is only a link (linkOnlyLine) — such a line between two page blocks runs
 *   on with them;
 * - "button": buttons.
 */
export type BlockRun = "list" | "page" | "button";

const LIST_RUN = new Set<string>(["bulleted_list_item", "numbered_list_item", "to_do", "toggle"]);
const PAGE_RUN = new Set<string>(["child_page", "link_to_page", "child_database"]);

/** A space as Notion counts it: any Unicode separator. */
const SEPARATOR = /\p{Z}/u;

/**
 * Whether a line's words are only a link, as Notion reads its text block:
 * one mention, or a mention and one run of words after it that holds a space
 * somewhere; or words all linked to one address, with nothing but spaces
 * between them (seen 2026-11-11: "@page", "@page and more", a linked word —
 * not "words then @page", not a link inside words, not an equation).
 */
export function linkOnlyLine(spans: readonly TextSpan[]) {
  const [first, second, ...rest] = spans;
  if (first?.mention && !rest.length && (!second || SEPARATOR.test(second.text))) return true;
  let address = "";
  for (const span of spans) {
    if ([...span.text].every(character => SEPARATOR.test(character))) continue;
    if (!span.link || (address && span.link !== address)) return false;
    address = span.link;
  }
  return address !== "";
}

/** A block's run by its kind, a line's by the words it holds (read only for a line). */
export function blockRunOf(type: string, spans: () => readonly TextSpan[] | null): BlockRun | undefined {
  if (LIST_RUN.has(type)) return "list";
  if (PAGE_RUN.has(type)) return "page";
  if (type === "button") return "button";
  if (type !== "paragraph") return undefined;
  const words = spans();
  return words?.length && linkOnlyLine(words) ? "page" : undefined;
}

/** A block's run, a line's read from the words it holds now. */
export function blockRun(type: string, words: Fragment | null): BlockRun | undefined {
  return blockRunOf(type, () => {
    if (!words?.size) return null;
    try {
      return fragmentToNativeRichText(words);
    } catch {
      return null;
    }
  });
}
