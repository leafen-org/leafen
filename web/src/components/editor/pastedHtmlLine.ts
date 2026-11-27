import { LEAFEN_BLOCKS_MIME } from "@/lib/clipboard";
import { sanitizeTextSpans } from "@/lib/textOperations";
import { spansToPlainText, type TextSpan } from "@/lib/types";
import { EDITOR_PASTE_BOUNDS } from "./editorClipboardPolicy";
import { parsePastedHtml } from "./htmlPaste";
import { notionCopy } from "./pastedHtmlChoice";
import type { PastedBlock } from "./markdownPasteTypes";

const LINE_BULLETS = ["•", "◦", "▪"];
const LINE_HEADINGS = new Set(["heading_1", "heading_2", "heading_3", "heading_4"]);

// Outside HTML whose plain text is one line goes into the line, as Notion's
// paste does (its paste code, seen 2026-11-01: blocks only when the text has
// more than one line or the HTML is a table): each block's words on a line of
// their own — a list item's always, even first, its bullet's mark or number
// before them and four spaces a level; a code block's as code after an empty
// line — and a heading's bold (seen 2026-11-13: '<ul><li>가</li></ul>' with
// '가' is '↵• 가', '<h1>제목</h1><p>본문</p>' with '제목 본문' is '제목↵본문'
// with '제목' bold, '<p>가</p><pre>코드</pre>' with '가 코드' is '가↵↵코드').
export function pastedLineSpans(blocks: readonly PastedBlock[], depth = 0, out: TextSpan[] = []): TextSpan[] {
  let number = 0;
  for (const block of blocks) {
    const { rich, text } = (block.content ?? {}) as Record<string, unknown>;
    const spans = sanitizeTextSpans(rich) ?? (typeof text === "string" ? [{ text }] : block.plainText ? [{ text: block.plainText }] : []);
    number = block.type === "numbered_list_item" ? number + 1 : 0;
    const mark = block.type === "bulleted_list_item" ? `${LINE_BULLETS[depth % LINE_BULLETS.length]} `
      : block.type === "numbered_list_item" ? `${number}. ` : "";
    if (block.type === "code") out.push({ text: "\n\n" });
    else if (out.length || mark) out.push({ text: "\n" });
    if (depth || mark) out.push({ text: " ".repeat(depth * 4) + mark });
    const bold = LINE_HEADINGS.has(block.type), code = block.type === "code";
    out.push(...(bold || code ? spans.map(span => ({ ...span, ...(bold ? { bold } : {}), ...(code ? { code } : {}) })) : spans));
    if (block.children?.length) pastedLineSpans(block.children, depth + 1, out);
  }
  return out;
}

// What of outside HTML goes in the line beside one line of its text: any
// blocks but a table, and one block of words of these kinds (Notion's, seen
// 2026-11-13: a heading, list item, quote or code block alone too).
export const LINE_KINDS = new Set([...LINE_HEADINGS, "bulleted_list_item", "numbered_list_item", "quote", "code"]);

/**
 * Another app's HTML pasted into a page's title beside one line of its text
 * (`text`, as the title takes it), read as a line of the body reads it
 * (pastedLineSpans) on the title's one line, as Notion's title keeps its
 * marks (seen 2026-11-13: '<p><b>굵</b> 글</p>' and a 30px '큰' bold there,
 * '<h1>제목</h1><p><i>본</i></p>' '제목' bold and '본' italic). Null for
 * Leafen's own copy, or where its words read other than the text (a list's
 * bullets, a table), which then goes in as it is.
 */
export function titleHtmlSpans(data: DataTransfer, text: string): TextSpan[] | null {
  const html = data.getData("text/html");
  if (!html || html.includes("data-native-") || data.types.includes(LEAFEN_BLOCKS_MIME)) return null;
  try {
    const blocks = parsePastedHtml(html, EDITOR_PASTE_BOUNDS, text, notionCopy(data));
    if (!blocks.length || blocks.some(block => block.type === "table")) return null;
    const spans = pastedLineSpans(blocks).map(span => ({ ...span, text: span.text.replace(/\s*\n+\s*/gu, " ") }));
    if (spans.length) {
      spans[0] = { ...spans[0]!, text: spans[0]!.text.trimStart() };
      spans[spans.length - 1] = { ...spans[spans.length - 1]!, text: spans[spans.length - 1]!.text.trimEnd() };
    }
    const words = spans.filter(span => span.text);
    return words.length && spansToPlainText(words) === text ? words : null;
  } catch (error) {
    if (error instanceof RangeError) return null;
    throw error;
  }
}
