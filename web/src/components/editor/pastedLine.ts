import type { TextSpan } from "@/lib/types";
import { parseInlineMarkdown, parsePastedMarkdown, type MarkdownPasteBounds, type PastedBlock } from "./markdownPaste";
import { pastedUrl } from "./textBlockClipboardModel";

/**
 * What one pasted line of text is, as Notion's paste reads one (real ⌘V,
 * seen 2026-11-11):
 * - "words": nothing in it is Markdown, so it goes in as it is;
 * - "spans": words with their Markdown read (markdownInlinePaste.ts) — a
 *   heading's, list's or quote's marker stays words, as one line does;
 * - "link": one link and nothing else, a bare address or a Markdown link,
 *   which goes in as a link with its "붙여넣기 형식" menu (PastedLinkMenu);
 *   `url` is what the menu acts on: a web address, a mail address
 *   ("mailto:…"), a phone number ("tel:…") or an address in the app ("/…",
 *   "#…"), as Notion's menu offers for each (seen 2026-11-11); null for any
 *   other link (no menu);
 * - "divider": "---", more dashes going on the line after it;
 * - "blocks": an equation "$$…$$" or a picture "![…](…)" alone.
 */
export type PastedLine =
  | { kind: "words" }
  | { kind: "spans"; spans: TextSpan[] }
  | { kind: "link"; text: string; href: string; url: string | null }
  | { kind: "divider"; rest: string }
  | { kind: "blocks"; blocks: PastedBlock[] };

const MARKS = ["bold", "italic", "underline", "strikethrough", "code", "color", "background", "mention", "equation", "template"] as const;

/**
 * Reads one line of pasted text; `blocks` false where no block can go (a
 * table's cell, a caption), which reads only its words.
 */
export function readPastedLine(text: string, bounds: MarkdownPasteBounds, blocks: boolean): PastedLine {
  const line = /[\r\n]/u.test(text) ? text.trimEnd() : text, alone = line.trim();
  if (blocks) {
    // "---" makes a divider, as typing it does; Notion's paste leaves any
    // further dash on the line after it ("----": a divider, then "-").
    const divider = /^---(-*)$/u.exec(alone);
    if (divider) return { kind: "divider", rest: divider[1] };
    // A picture alone is one, as in several lines (Notion's one line shows
    // nothing, the paste lost — kept here as the several-line paste keeps it).
    if (/^\$\$.*\S.*\$\$$/u.test(alone) || alone.startsWith("![")) {
      const read = parsePastedMarkdown(alone, bounds);
      if (read.length === 1 && (read[0].type === "equation" || read[0].type === "image") && !read[0].children?.length) return { kind: "blocks", blocks: read };
    }
  }
  const spans = parseInlineMarkdown(line, bounds);
  if (spans.length === 1 && spans[0].link && MARKS.every(mark => !spans[0][mark])) {
    const [{ text: words, link }] = spans;
    // A link named by a web address links that address, as Notion's.
    const address = pastedUrl(words);
    return address ? { kind: "link", text: address, href: address, url: address } : { kind: "link", text: words, href: link, url: pastedLinkMenuUrl(link) };
  }
  if (spans.every(span => !span.link && MARKS.every(mark => !span[mark])) && spans.map(span => span.text).join("") === line) return { kind: "words" };
  return { kind: "spans", spans };
}

function pastedLinkMenuUrl(link: string) {
  return /^(?:mailto:|tel:)\S|^[/#]/iu.test(link) ? link : pastedUrl(link);
}

/**
 * One line pasted in a field of words — a page's title, a database's title
 * or text value, a comment — with its Markdown read, as Notion's (seen
 * 2026-11-11, a title's and a text cell's: a link alone is a link there,
 * with no menu); null for plain words or more than one line.
 */
export function pastedFieldSpans(text: string, bounds: MarkdownPasteBounds): TextSpan[] | null {
  if (text.trimEnd().includes("\n")) return null;
  const read = readPastedLine(text, bounds, false);
  return read.kind === "spans" ? read.spans : read.kind === "link" ? [{ text: read.text, link: read.href }] : null;
}

/**
 * One line pasted with blocks chosen, as the blocks it makes after them, as
 * Notion's (seen 2026-11-11): a line of its words with their Markdown read,
 * a divider for "---", an equation or a picture alone.
 */
export function pastedLineBlocks(text: string, bounds: MarkdownPasteBounds): PastedBlock[] {
  const read = readPastedLine(text.trim(), bounds, true);
  const line = (rich: TextSpan[]): PastedBlock => ({ type: "paragraph", content: { rich }, plainText: rich.map(span => span.text).join("") });
  if (read.kind === "blocks") return read.blocks;
  if (read.kind === "divider") return [{ type: "divider", content: { rich: [] } }, ...(read.rest ? [line([{ text: read.rest }])] : [])];
  if (read.kind === "spans") return [line(read.spans)];
  if (read.kind === "link") return [line([{ text: read.text, link: read.href }])];
  return [line([{ text: text.trim() }])];
}
