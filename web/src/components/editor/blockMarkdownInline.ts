import { blockAnchor } from "@/lib/blockAddress";
import type { TextSpan } from "@/lib/types";
import { dateMentionCopyText, dateMentionDisplayText, safeDateMentionFormat, safeDateMentionTimeFormat } from "./dateMentions";
import { safeInlineEquationValue } from "./richtextEquationPolicy";
import {
  escapeHtml,
  safeColorToken,
  safeDateMentionTimeZone,
  safeDateMentionValue,
  safeExternalMentionUrl,
  safeLinkMention,
  safeMentionId,
  spansPlainText,
  textLinkHref,
} from "./richtext";
import type {
  BlockMarkdownBounds,
  BlockMarkdownBudget,
} from "./blockMarkdownBudget";
import { textSpanColors } from "@/lib/textSpanColors";
import { dataSourceMentionId } from "@/lib/dataSourceMentionNames";
import { absolutePageUrl } from "@/lib/navigation";

export function markdownHref(href: string) {
  return href.replace(/\s/g, "%20").replace(/\)/g, "%29");
}

export function markdownTextLiteral(text: string) {
  return text.replace(/\\/g, "\\\\").replace(/([`*_~\[\]])/g, "\\$1");
}

function longestBacktickRun(text: string) {
  let longest = 0;
  let current = 0;
  for (const char of text) {
    if (char === "`") {
      current += 1;
      if (current > longest) longest = current;
    } else current = 0;
  }
  return longest;
}

// A code block's fence names its language as Notion's copy does (seen
// 2026-11-01): plain text none, most by their own name, these by another.
const NOTION_FENCE_LANGUAGES: Readonly<Record<string, string>> = {
  "plain text": "", javascript: "jsx", typescript: "tsx", shell: "bash", "c++": "cpp", "c#": "csharp",
  "f#": "fsharp", "objective-c": "objectivec", "vb.net": "vbnet", "visual basic": "visual-basic",
  webassembly: "wasm", "java/c/c++/c#": "java",
};

/** A code language as Notion's copy names it, in its Markdown fence and its HTML class alike (seen 2026-11-01, 2026-11-06). */
export function notionCodeLanguageName(language: unknown) {
  const name = String(language ?? "").toLowerCase();
  return (NOTION_FENCE_LANGUAGES[name] ?? name).replace(/[^\w-]/g, "");
}

export function markdownCodeBlock(text: string, language: unknown) {
  const fence = "`".repeat(Math.max(3, longestBacktickRun(text) + 1));
  return `${fence}${notionCodeLanguageName(language)}\n${text}\n${fence}`;
}

function markdownInlineCode(text: string) {
  const body = text.replace(/\n/g, " ");
  const fence = "`".repeat(Math.max(1, longestBacktickRun(body) + 1));
  const padded = body.startsWith("`") || body.endsWith("`") ? ` ${body} ` : body;
  return `${fence}${padded}${fence}`;
}

function dateMentionHref(date: string) {
  return `leafen://date/${encodeURIComponent(date)}`;
}

function personMentionHref(userId: string) {
  return `leafen://person/${encodeURIComponent(userId)}`;
}

type MarkedPiece = { marks: readonly string[]; lead: string; core: string; trail: string };

/**
 * Writes pieces of text with their marks one inside another, as Notion's copy
 * does (seen 2026-11-07: "*토글 하나 **가나다***" and
 * "<em>토글 하나 <strong>가나다</strong></em>"): a mark the next pieces keep
 * stays open across them, the one running longest outside, and the spaces at
 * a mark's edge stay outside it. `order` names the marks outside first.
 */
function nestMarks(pieces: readonly MarkedPiece[], order: readonly string[], open: (mark: string) => string, close: (mark: string) => string) {
  const stack: string[] = [];
  let out = "", trail = "";
  const runOf = (from: number, mark: string) => {
    let length = 0;
    while (from + length < pieces.length && pieces[from + length].marks.includes(mark)) length++;
    return length;
  };
  pieces.forEach((piece, index) => {
    // A piece of spaces alone opens nothing.
    const marks = piece.core ? piece.marks : piece.marks.filter((mark) => stack.includes(mark));
    const ended = stack.findIndex((mark) => !marks.includes(mark));
    if (ended >= 0) while (stack.length > ended) out += close(stack.pop()!);
    out += trail + piece.lead;
    for (const mark of marks.filter((mark) => !stack.includes(mark))
      .sort((a, b) => runOf(index, b) - runOf(index, a) || order.indexOf(a) - order.indexOf(b))) {
      out += open(mark);
      stack.push(mark);
    }
    out += piece.core;
    trail = piece.trail;
  });
  while (stack.length) out += close(stack.pop()!);
  return out + trail;
}

const HTML_MARK_TAGS: Readonly<Record<string, string>> = { strikethrough: "s", underline: "u", italic: "em", bold: "strong" };
const COPIED_MARKDOWN_MARKS: Readonly<Record<string, readonly [string, string]>> = {
  underline: ["<u>", "</u>"], bold: ["**", "**"], italic: ["*", "*"], strikethrough: ["~~", "~~"],
};
const spanMarks = (span: TextSpan, names: readonly string[]) => names.filter((name) => span[name as keyof TextSpan] === true);

export function clipboardInlineHtml(spans: TextSpan[] | undefined, bounds: BlockMarkdownBounds) {
  if (!spans?.length) return "";
  const fragments: MarkedPiece[] = [];
  const push = (span: TextSpan, html: string) => fragments.push({ marks: spanMarks(span, Object.keys(HTML_MARK_TAGS)), lead: "", core: html, trail: "" });
  for (const span of spans) {
    const date = span.mention === "date"
      ? safeDateMentionValue(span.date, bounds.richtext)
      : undefined;
    const dateEnd = date ? safeDateMentionValue(span.dateEnd, bounds.richtext) : undefined;
    const dateTimeZone = date
      ? safeDateMentionTimeZone(span.dateTimeZone, bounds.richtext)
      : undefined;
    const raw = date
      ? dateMentionCopyText(date, dateEnd, span.dateFormat, span.dateTimeFormat)
      : (span.text ?? "");
    if (!raw) continue;
    // An inline equation reads as its TeX between "$"s here too, as Notion's
    // HTML of a block or of words over lines: "<p>둘째 줄 $x_1^2$ 끝</p>".
    const equation = safeInlineEquationValue(span.equation);
    let html = equation === raw
      ? `<span data-equation="${escapeHtml(equation)}">${escapeHtml(`$${raw}$`)}</span>`
      : escapeHtml(raw).replace(/\n/g, "<br>");
    // Code stays with its text; the other marks run across the pieces that share them (nestMarks).
    if (span.code) html = `<code>${html}</code>`;
    // A text color and a background each keep their own span, the background
    // outside, named for a paste here and drawn by no other app: Notion's copy
    // gives words no color (seen 2026-11-14), and a light one hid them in a dark app.
    const colors = textSpanColors(span);
    for (const token of [colors.color, colors.background]) {
      const color = safeColorToken(token, bounds.richtext);
      if (color) html = `<span data-color="${escapeHtml(color)}">${html}</span>`;
    }
    if (equation === raw) {
      push(span, html);
      continue;
    }
    const pageId = span.mention === "page" ? safeMentionId(span.pageId, bounds.richtext) : undefined;
    if (pageId) {
      // Its whole address, for another app to open it (Notion's, seen 2026-11-06), at its block for one of a block.
      const blockId = safeMentionId(span.blockId, bounds.richtext);
      const at = blockAnchor(pageId, blockId);
      const block = blockId ? ` data-mention-block-id="${escapeHtml(blockId)}"` : "";
      push(span, `<a href="${escapeHtml(absolutePageUrl(pageId) + at)}" data-mention="page" data-page-id="${escapeHtml(pageId)}"${block}>${html}</a>`);
      continue;
    }
    // A data source's by its own ID, which a paste reads back (richtextHtmlRead.ts).
    const dataSourceId = safeMentionId(dataSourceMentionId(span), bounds.richtext);
    if (dataSourceId) {
      push(span, `<span data-mention="database" data-mention-data-source="${escapeHtml(dataSourceId)}">${html}</span>`);
      continue;
    }
    const databaseId = span.mention === "database"
      ? safeMentionId(span.databaseId, bounds.richtext)
      : undefined;
    if (databaseId) {
      push(span, `<span data-mention="database" data-database-id="${escapeHtml(databaseId)}">${html}</span>`);
      continue;
    }
    const customEmojiId = span.mention === "custom_emoji"
      ? safeMentionId(span.customEmojiId, bounds.richtext)
      : undefined;
    if (customEmojiId) {
      push(span, `<span data-mention="custom_emoji" data-custom-emoji-id="${escapeHtml(customEmojiId)}">${html}</span>`);
      continue;
    }
    if (date) {
      const end = dateEnd ? ` data-date-end="${escapeHtml(dateEnd)}"` : "";
      const timeZone = dateTimeZone
        ? ` data-date-time-zone="${escapeHtml(dateTimeZone)}"`
        : "";
      const format = safeDateMentionFormat(span.dateFormat);
      const timeFormat = safeDateMentionTimeFormat(span.dateTimeFormat);
      const shown = (format ? ` data-date-format="${format}"` : "") + (timeFormat ? ` data-date-time-format="${timeFormat}"` : "");
      push(span, `<span data-mention="date" data-date="${escapeHtml(date)}"${end}${timeZone}${shown}>${html}</span>`);
      continue;
    }
    const userId = span.mention === "person" ? safeMentionId(span.userId, bounds.richtext) : undefined;
    if (userId) {
      push(span, `<span data-mention="person" data-user-id="${escapeHtml(userId)}">${html}</span>`);
      continue;
    }
    const external = span.mention === "external"
      ? safeLinkMention(span.link, bounds.richtext)
      : "";
    if (external) {
      const iconUrl = safeExternalMentionUrl(span.iconUrl, bounds.richtext);
      const imageUrl = safeExternalMentionUrl(span.imageUrl, bounds.richtext);
      // What it keeps of its page's preview goes along for a paste here (richtext.ts).
      const kept = [["data-icon-url", iconUrl], ["data-provider", span.provider], ["data-description", span.description], ["data-image-url", imageUrl]]
        .map(([name, value]) => value ? ` ${name}="${escapeHtml(value)}"` : "").join("");
      push(span, `<a href="${escapeHtml(external)}" data-mention="external"${kept}>${html}</a>`);
      continue;
    }
    const href = textLinkHref(span.link);
    push(span, href ? `<a href="${escapeHtml(href)}">${html}</a>` : html);
  }
  return nestMarks(fragments, ["strikethrough", "underline", "italic", "bold"],
    (mark) => `<${HTML_MARK_TAGS[mark]}>`, (mark) => `</${HTML_MARK_TAGS[mark]}>`);
}

export function pageWikiLink(
  label: string,
  pageId: string | undefined,
  fallback: string,
  bounds: BlockMarkdownBounds,
) {
  if (!label && !pageId) return fallback;
  const title = markdownTextLiteral(label || fallback.replace(/^\[\[|\]\]$/g, ""));
  const admittedPageId = safeMentionId(pageId, bounds.richtext);
  return admittedPageId
    ? `[[${title}]](${markdownHref(`/p/${encodeURIComponent(admittedPageId)}`)})`
    : `[[${title}]]`;
}

export function spansToMarkdown(spans: TextSpan[] | undefined, bounds: BlockMarkdownBounds) {
  if (!spans?.length) return "";
  const fragments: string[] = [];
  // Copied, the marks run across the pieces that share them (nestMarks); code keeps its text and the marks inside it.
  const pieces: MarkedPiece[] = [];
  const piece = (span: TextSpan, lead: string, core: string, trail: string) => pieces.push({
    marks: spanMarks(span, span.code ? ["underline", "bold"] : Object.keys(COPIED_MARKDOWN_MARKS)), lead, core, trail,
  });
  for (const span of spans) {
    const date = span.mention === "date"
      ? safeDateMentionValue(span.date, bounds.richtext)
      : undefined;
    const dateEnd = date ? safeDateMentionValue(span.dateEnd, bounds.richtext) : undefined;
    const raw = date
      ? bounds.copied ? dateMentionCopyText(date, dateEnd, span.dateFormat, span.dateTimeFormat)
        : dateMentionDisplayText(date, span.text ?? "", undefined, dateEnd, span.dateFormat, span.dateTimeFormat)
      : (span.text ?? "");
    if (!raw) continue;
    // An inline equation reads as its TeX between "$"s, unescaped, as Notion's
    // copy of its block or of words over lines gives it (measured 2026-11-15 in
    // Chrome: "둘째 줄 $x_1^2$ 끝", and "$x_1^2$ 끝\n\n셋째 $z^3$ 끝").
    const equation = safeInlineEquationValue(span.equation);
    if (equation) {
      if (bounds.copied) piece(span, "", `$${equation}$`, "");
      else fragments.push(`$${equation}$`);
      continue;
    }
    if (span.code && !bounds.copied) {
      fragments.push(markdownInlineCode(raw));
      continue;
    }
    if (!raw.trim()) {
      if (bounds.copied) piece(span, "", "", raw);
      else fragments.push(raw);
      continue;
    }
    const leading = raw.match(/^\s*/)?.[0] ?? "";
    const trailing = raw.match(/\s*$/)?.[0] ?? "";
    const inner = raw.slice(leading.length, raw.length - trailing.length);
    let body = span.code ? inner : markdownTextLiteral(inner);
    if (bounds.copied) {
      // Every mark, one inside another, and no link's address, as Notion's copy (seen 2026-11-04).
      if (span.code) {
        if (span.strikethrough) body = `~~${body}~~`;
        if (span.italic) body = `*${body}*`;
        body = markdownInlineCode(body);
      }
      // Copied, a date, a page mention or a person reads as itself alone, as Notion's
      // (seen 2026-11-06, a person "@name" 2026-11-07): no address only Leafen opens.
      piece(span, leading, body, trailing);
      continue;
    }
    if (span.bold) body = `**${body}**`;
    if (span.italic) body = `*${body}*`;
    if (span.strikethrough) body = `~~${body}~~`;
    if (date) {
      fragments.push(`${leading}[${body}](${markdownHref(dateMentionHref(date))})${trailing}`);
      continue;
    }
    const userId = span.mention === "person" ? safeMentionId(span.userId, bounds.richtext) : undefined;
    if (userId) {
      fragments.push(`${leading}[${body}](${markdownHref(personMentionHref(userId))})${trailing}`);
      continue;
    }
    const pageId = span.mention === "page" ? safeMentionId(span.pageId, bounds.richtext) : undefined;
    const blockId = pageId ? safeMentionId(span.blockId, bounds.richtext) : undefined;
    const href = pageId ? `/p/${encodeURIComponent(pageId)}${blockAnchor(pageId, blockId)}` : textLinkHref(span.link);
    fragments.push(href
      ? `${leading}[${body}](${markdownHref(href)})${trailing}`
      : `${leading}${body}${trailing}`);
  }
  return bounds.copied
    ? nestMarks(pieces, ["underline", "bold", "italic", "strikethrough"], (mark) => COPIED_MARKDOWN_MARKS[mark][0], (mark) => COPIED_MARKDOWN_MARKS[mark][1])
    : fragments.join("");
}

export function plainRichText(spans: TextSpan[] | undefined, bounds: BlockMarkdownBounds) {
  return spansPlainText(spans, bounds.richtext);
}

export type { BlockMarkdownBudget };
