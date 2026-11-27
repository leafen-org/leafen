import { createContext } from "react";
import type { TextSpan } from "@/lib/types";
import { richTextSummary } from "@/lib/richTextSummary";
import { htmlToSpans } from "../editor/richtextHtmlRead";
import { CELL_RICHTEXT_BOUNDS } from "../database/richTextCellModel";

/**
 * A line of a page's body as the publish preview shows it: its formatted
 * text where it reads (lib/richTextSummary.ts), and its plain text, an inline
 * equation's TeX in it, where it doesn't.
 */
export type PageBodyLeadLine = { type: string; text: string; spans?: TextSpan[] };

const LEAD_TYPES = new Set([
  "paragraph", "heading_1", "heading_2", "heading_3", "bulleted_list_item", "numbered_list_item",
  "to_do", "quote", "callout", "toggle", "toggle_heading_1", "toggle_heading_2", "toggle_heading_3",
]);
const LEAD_CHARACTERS = 120;

/** A block's own text: its node view's content without the blocks inside it, a toggle's title without its body. */
function ownBlockContent(block: HTMLElement) {
  const content = block.querySelector("[data-node-view-content]");
  if (!content) return null;
  const own = content.cloneNode(true) as HTMLElement;
  own.querySelectorAll("[data-block-id]").forEach(inner => inner.remove());
  return own;
}

/** The line's spans as the page holds them, read from what it draws; none when they don't read. */
function ownBlockSpans(own: HTMLElement) {
  try {
    const spans = richTextSummary(htmlToSpans(own, CELL_RICHTEXT_BOUNDS), LEAD_CHARACTERS);
    return spans.length ? spans : undefined;
  } catch {
    return undefined;
  }
}

/**
 * The first few lines of the open page's body, as the "웹에 게시" preview shows
 * them under the title, as Notion's does (seen 2026-10-16: ours showed the
 * title and sub-pages alone). Read from the page as it stands when asked.
 */
export function readPageBodyLead(root: HTMLElement | null, limit = 8): PageBodyLeadLine[] {
  if (!root) return [];
  const lines: PageBodyLeadLine[] = [];
  for (const block of root.querySelectorAll<HTMLElement>('[data-block-id][data-depth="0"]')) {
    const type = block.dataset.blockType ?? "";
    if (!LEAD_TYPES.has(type)) continue;
    const own = ownBlockContent(block);
    if (!own) continue;
    const spans = ownBlockSpans(own);
    // An equation's drawing is no text: its TeX stands for it.
    own.querySelectorAll<HTMLElement>("[data-equation]").forEach(equation => equation.replaceWith(equation.dataset.equation ?? ""));
    const text = (own.textContent ?? "").replace(/\s+/gu, " ").trim();
    if (!text) continue;
    lines.push({ type, text: text.length > LEAD_CHARACTERS ? `${text.slice(0, LEAD_CHARACTERS - 1)}…` : text, ...(spans ? { spans } : {}) });
    if (lines.length >= limit) break;
  }
  return lines;
}

/** The open page's body lead, read when the publish preview shows. */
export const PageBodyLead = createContext<() => PageBodyLeadLine[]>(() => []);
