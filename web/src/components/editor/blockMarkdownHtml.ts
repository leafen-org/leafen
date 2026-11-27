import type { Block, TextSpan } from "@/lib/types";
import { activePersistentGeneratedLabels } from "@/lib/persistentGeneratedLabels";
import { absolutePageUrl } from "@/lib/navigation";
import { escapeHtml, safeMentionId, safeUrl } from "./richtext";
import { calloutCopyIcon } from "./blockIconModel";
import {
  clipboardInlineHtml,
  notionCodeLanguageName,
  plainRichText,
} from "./blockMarkdownInline";
import type {
  BlockMarkdownBounds,
  BlockMarkdownBudget,
} from "./blockMarkdownBudget";

/**
 * A file uploaded to the workspace has no address another app can open: its
 * copy names it as Notion's names an uploaded one, "attachment:{id}:{name}"
 * (seen 2026-11-07), which no app fetches. Empty for a file from the web.
 */
function uploadedFileAddress(block: Block) {
  const id = (block.content as Record<string, unknown> | undefined)?.fileId;
  if (typeof id !== "string" || !id || safeUrl(block.content?.url)) return "";
  return `attachment:${id}:${block.content?.name || block.content?.fileName || ""}`;
}

function blockTextHtml(block: Block, bounds: BlockMarkdownBounds) {
  if (block.type === "code") {
    return escapeHtml(plainRichText(block.content?.rich, bounds).trimEnd().replace(/\n{3,}/g, "\n\n"));
  }
  return clipboardInlineHtml(block.content?.rich, bounds);
}

// Words of spaces alone, no mention or equation among them.
const spacesAlone = (spans: readonly TextSpan[] | undefined) =>
  (spans ?? []).every((span) => !span.mention && span.equation === undefined && !(span.text ?? "").trim());

// The spans from `from` to `to` in a block's words, one with no words kept where it stands.
function spansBetween(spans: readonly TextSpan[], from: number, to: number) {
  const part: TextSpan[] = [];
  let at = 0;
  for (const span of spans) {
    const text = span.text ?? "", start = Math.max(from, at), end = Math.min(to, at + text.length);
    if (end > start) part.push(start === at && end === at + text.length ? span : { ...span, text: text.slice(start - at, end - at) });
    else if (!text && at >= from && at < to) part.push(span);
    at += text.length;
  }
  return part;
}

/**
 * A heading as its copy's Markdown reads, as Notion's copy gives one to
 * another app (seen 2026-11-15): its first line the heading, the lines after
 * a line break a paragraph after it — "<h1>a</h1><p>b<br>c</p>" for a⇧↵b⇧↵c —
 * a toggle heading the same with what it holds after.
 */
function headingHtml(tag: "h1" | "h2" | "h3" | "h4", block: Block, childrenHtml: string, bounds: BlockMarkdownBounds) {
  const spans = Array.isArray(block.content?.rich) ? block.content.rich : [];
  const text = spans.map((span) => span.text ?? "").join("");
  const from = text.length - text.trimStart().length, to = text.trimEnd().length;
  const lineBreak = text.indexOf("\n", from);
  if (lineBreak < 0 || lineBreak >= to) return `<${tag}>${blockTextHtml(block, bounds)}</${tag}>${childrenHtml}`;
  const rest = clipboardInlineHtml(spansBetween(spans, lineBreak + 1, to), bounds);
  return `<${tag}>${clipboardInlineHtml(spansBetween(spans, from, lineBreak), bounds)}</${tag}>${rest ? `<p>${rest}</p>` : ""}${childrenHtml}`;
}

/**
 * A block's words in the paragraphs a blank line parts them into, those with
 * no words left out — as Notion's copy gives another app a line with blank
 * lines in it (seen 2026-11-11: "<p>a</p><p>b</p>" for "a\n\nb", a list
 * item's or a quote's words after the first as paragraphs after it).
 */
function blankLineParts(spans: readonly TextSpan[]): TextSpan[][] | null {
  const text = spans.map((span) => span.text ?? "").join("");
  const blanks = [...text.matchAll(/\n{2,}/g)].map((blank) => [blank.index ?? 0, (blank.index ?? 0) + blank[0].length]);
  if (!blanks.length) return null;
  const parts: TextSpan[][] = [];
  let start = 0;
  for (const [from, to] of [...blanks, [text.length, text.length]]) {
    const part = spansBetween(spans, start, from);
    if (part.length) parts.push(part);
    start = to;
  }
  return parts;
}

// A page or database block copies as a link to it, its whole address for
// another app, an untitled one as "제목 없음", as Notion's (seen 2026-11-06).
function pageLinkHtml(block: Block, bounds: BlockMarkdownBounds) {
  const title = escapeHtml(block.plainText?.trim() || (typeof block.content?.childPageTitle === "string" && block.content.childPageTitle.trim())
    || activePersistentGeneratedLabels().untitled);
  const pageId = safeMentionId(block.content?.childPageId, bounds.richtext);
  if (!pageId) return title;
  return `<a href="${escapeHtml(absolutePageUrl(pageId))}" data-mention="page" data-page-id="${escapeHtml(pageId)}">${title}</a>`;
}

export function tabLabelHtml(
  block: Block,
  childrenHtml: string,
  bounds: BlockMarkdownBounds,
) {
  const icon = typeof block.content?.icon === "string" ? block.content.icon.trim() : "";
  const iconAttr = icon ? ` data-leafen-tab-icon="${escapeHtml(icon)}"` : "";
  const iconHtml = icon ? `<span data-leafen-tab-icon-text="true">${escapeHtml(icon)}</span> ` : "";
  const titleHtml = blockTextHtml(block, bounds)
    || escapeHtml(block.plainText?.trim() || activePersistentGeneratedLabels().untitled);
  return `<section data-leafen-tab-label="true"${iconAttr}><p data-leafen-tab-title="true">${iconHtml}${titleHtml}</p>${childrenHtml ? `<div data-leafen-tab-panel="true">${childrenHtml}</div>` : ""}</section>`;
}

export const COPIED_LIST_ITEM_TYPES: ReadonlySet<Block["type"]> = new Set<Block["type"]>(["bulleted_list_item", "numbered_list_item", "to_do", "toggle"]);

/** A list item's parts, which its list puts beside the items next to it (copiedSiblingsHtml). */
export type CopiedListItem = {
  list: "ul" | "ol";
  words: string;
  held: string;
  // It holds a block that is not a list item, and one that shows something.
  holdsBlocks: boolean;
  showsBlocks: boolean;
  // Paragraphs its words have after a blank line, after its list.
  after: string;
};

/**
 * A list item as its copy's Markdown writes it: a bulleted, to-do or toggle
 * item a "- " line, a numbered one "1. ", a to-do's box its words "[ ]  " or
 * "[x]  " (Notion's, seen 2026-11-11); its words after a blank line no longer
 * in it, so paragraphs after its list.
 */
export function copiedListItem(
  block: Block,
  childrenHtml: string,
  held: { holdsBlocks: boolean; showsBlocks: boolean },
  bounds: BlockMarkdownBounds,
): CopiedListItem {
  const parts = Array.isArray(block.content?.rich) ? blankLineParts(block.content.rich) : null;
  const text = clipboardInlineHtml(parts ? parts[0] ?? [] : block.content?.rich, bounds);
  return {
    list: block.type === "numbered_list_item" ? "ol" : "ul",
    words: block.type === "to_do" ? `${block.content?.checked ? "[x]" : "[ ]"}  ${text}` : text,
    held: childrenHtml,
    ...held,
    after: (parts ?? []).slice(1).map((part) => `<p>${clipboardInlineHtml(part, bounds)}</p>`).join(""),
  };
}

/**
 * Blocks side by side as Notion's copy gives them to another app: the HTML
 * its copy's Markdown reads as (seen 2026-11-14). List items next to each
 * other are one list — bulleted, to-do and toggle items a `<ul>`, numbered
 * ones an `<ol>` counting on — also with an empty line, which gives nothing,
 * between them; a paragraph after an item's words ends its list. A list
 * whose items stand a blank line apart (an empty line between them, or an
 * item before the last holding a block that is not a list) or hold a block
 * that shows has each item's words as a paragraph, as such a Markdown list
 * reads; otherwise they are the item's words alone.
 */
export function copiedSiblingsHtml(pieces: ReadonlyArray<string | CopiedListItem>): string {
  let html = "", run: CopiedListItem[] = [], apart = false, blank = false;
  const close = () => {
    if (!run.length) return;
    const loose = apart || run.some((item, index) => item.showsBlocks || item.holdsBlocks && index < run.length - 1);
    const items = run.map((item) => `<li>${loose && item.words ? `<p>${item.words}</p>` : item.words}${item.held}</li>`);
    html += `<${run[0].list}>${items.join("")}</${run[0].list}>${run[run.length - 1].after}`;
    run = [];
    apart = false;
  };
  for (const piece of pieces) {
    if (typeof piece === "string") {
      if (!piece) {
        blank = run.length > 0;
        continue;
      }
      close();
      html += piece;
    } else {
      if (run.length && run[0].list !== piece.list) close();
      apart ||= blank && run.length > 0;
      run.push(piece);
      if (piece.after) close();
    }
    blank = false;
  }
  close();
  return html;
}

export function blockHtml(
  block: Block,
  childrenHtml: string,
  bounds: BlockMarkdownBounds,
  budget: BlockMarkdownBudget,
): string {
  const parts = block.type === "code" || COPIED_LIST_ITEM_TYPES.has(block.type) || !Array.isArray(block.content?.rich)
    ? null : blankLineParts(block.content.rich);
  // Words after a blank line are paragraphs after the block's first ones: a
  // callout's inside it, a quote's before a quote of what it holds.
  if (parts) {
    const after = parts.slice(1).map((part) => `<p>${clipboardInlineHtml(part, bounds)}</p>`).join("");
    const held = block.type === "callout" ? `${after}${childrenHtml}` : block.type === "quote" && after ? "" : childrenHtml;
    const first = blockHtml({ ...block, content: { ...block.content, rich: parts[0] ?? [] } } as Block, held, bounds, budget);
    return block.type === "callout" || !after ? first : `${first}${after}${block.type === "quote" ? `<blockquote>${childrenHtml}</blockquote>` : ""}`;
  }
  const text = blockTextHtml(block, bounds);
  // A block's color goes to no other app, as Notion's copy gives none (seen
  // 2026-11-14): a light background hid its words in a dark app.
  switch (block.type) {
    // An empty line gives nothing, nor one of spaces alone (Notion's, seen 2026-11-11).
    case "paragraph": return text && !spacesAlone(block.content?.rich) ? `<p>${text}</p>${childrenHtml}` : childrenHtml;
    case "heading_1":
    case "toggle_heading_1": return headingHtml("h1", block, childrenHtml, bounds);
    case "heading_2":
    case "toggle_heading_2": return headingHtml("h2", block, childrenHtml, bounds);
    case "heading_3":
    case "toggle_heading_3": return headingHtml("h3", block, childrenHtml, bounds);
    case "heading_4":
    case "toggle_heading_4": return headingHtml("h4", block, childrenHtml, bounds);
    // A list item alone, which knows nothing of the blocks it holds; in a tree its list puts it with the items beside it (blockTreeHtml).
    case "bulleted_list_item":
    case "numbered_list_item":
    case "to_do":
    case "toggle": return copiedSiblingsHtml([copiedListItem(block, childrenHtml, { holdsBlocks: false, showsBlocks: false }, bounds)]);
    // Its words a paragraph in it, then what it holds (Notion's, seen 2026-11-15).
    case "quote": return `<blockquote>${text && !spacesAlone(block.content?.rich) ? `<p>${text}</p>` : ""}${childrenHtml}</blockquote>`;
    // As Notion's copy (seen 2026-11-07): "<aside>" and the icon in a first
    // paragraph, the callout's lines as paragraphs, "</aside>" in a last one.
    case "callout": {
      const icon = calloutCopyIcon(block.content as Record<string, unknown> | undefined);
      const shown = !icon ? "" : "src" in icon ? `<img src="${escapeHtml(icon.src)}" alt="" width="40">` : escapeHtml(icon.text);
      const head = [shown, text].filter(Boolean).join(" ");
      return `<p>&lt;aside&gt;${head ? `<br>${head}` : ""}</p>${childrenHtml}<p>&lt;/aside&gt;</p>`;
    }
    case "code": {
      // Named as Notion's copy names it: "language-jsx" for JavaScript.
      const language = notionCodeLanguageName(block.content?.language);
      // Its last line ends in a line break, as Notion's copy writes it (seen 2026-11-11).
      return `<pre><code${language ? ` class="language-${escapeHtml(language)}"` : ""}>${text}\n</code></pre>${childrenHtml}`;
    }
    case "equation": return `<p>${escapeHtml(block.content?.expression ?? plainRichText(block.content?.rich, bounds))}</p>${childrenHtml}`;
    case "divider": return `<hr>${childrenHtml}`;
    case "table": return childrenHtml;
    case "table_row": return "";
    case "image": {
      // An uploaded picture is a picture to another app too, named as its file (Notion's, seen 2026-11-07).
      const src = safeUrl(block.content?.url) || uploadedFileAddress(block);
      const caption = clipboardInlineHtml(block.content?.caption, bounds);
      const alt = plainRichText(block.content?.caption, bounds) || block.content?.name || block.content?.fileName || "";
      // With no caption it stands in a line of its own, as Notion's.
      const image = `<img src="${escapeHtml(src)}" alt="${escapeHtml(alt)}">`;
      return `${!src ? "" : caption ? `<figure>${image}<figcaption>${caption}</figcaption></figure>` : `<p>${image}</p>`}${childrenHtml}`;
    }
    case "video":
    case "audio":
    case "bookmark":
    case "link_preview":
    case "embed":
    case "pdf":
    case "file": {
      // An uploaded file links to it as an attachment, as Notion's (seen 2026-11-07).
      const href = safeUrl(block.content?.url) || uploadedFileAddress(block);
      // A frame from an address with no name of its own reads as its address,
      // as Notion's copy of a video, audio, PDF or embed (seen 2026-11-06).
      // A bookmark's link reads as its page's title (Notion's, seen 2026-11-07).
      const stored = block.type === "bookmark" ? (block.content as Record<string, unknown> | undefined)?.title : undefined;
      const title = typeof stored === "string" ? stored.trim() : "";
      const label = block.content?.name || block.content?.fileName || title || block.plainText || href || block.type;
      return `<p>${href ? `<a href="${escapeHtml(href)}">${escapeHtml(label ?? "")}</a>` : escapeHtml(label ?? "")}</p>${childrenHtml}`;
    }
    case "child_page":
    case "link_to_page": return `<p>${pageLinkHtml(block, bounds)}</p>${childrenHtml}`;
    case "child_database":
    case "inline_database": return `<p>${pageLinkHtml(block, bounds)}</p>${childrenHtml}`;
    case "button": return `<p>${escapeHtml(block.content?.buttonLabel ?? block.plainText ?? "New button")}</p>${childrenHtml}`;
    case "template": return `<p data-leafen-block-type="template">${escapeHtml(block.plainText ?? "Template")}</p>${childrenHtml}`;
    // Nothing, as in its copy's Markdown (Notion's, seen 2026-11-15).
    case "table_of_contents":
    case "breadcrumb": return "";
    case "tab": return `<div data-leafen-block-type="tab"><strong>Tabs</strong>${childrenHtml}</div>`;
    // Columns and synced blocks give what they hold, in a tree a blank line
    // apart from the blocks around them (blockTreePiece).
    case "synced_block":
    case "column_list":
    case "column": return childrenHtml;
    default: {
      const exhaustive: never = block.type;
      return exhaustive;
    }
  }
}

export function structuredTableHtml(
  table: Block,
  rows: Block[],
  bounds: BlockMarkdownBounds,
  budget: BlockMarkdownBudget,
) {
  void budget;
  const tableRows = rows.filter((row) => row.type === "table_row");
  if (tableRows.length === 0) return "";
  if (tableRows.length > bounds.maxTableRows) {
    throw new RangeError("Block table rows exceed their caller-owned bound.");
  }
  // One without width writes nothing, as Notion's exports leave it out.
  const width = Number(table.content?.tableWidth) || 0;
  if (width < 1) return "";
  if (width > bounds.maxTableColumns) {
    throw new RangeError("Block table columns exceed their caller-owned bound.");
  }
  // As its copied Markdown's table reads, as Notion's copy gives one (seen
  // 2026-11-14): the first row the head, header row or not, the rest cells,
  // a header column too; no borders, shading or colors, which another app
  // draws in its own (a light head in a dark app hid its words).
  const renderedRows = tableRows.map((row, rowIndex) => {
    const cells = Array.isArray(row.content?.cells) ? row.content.cells : [];
    const tag = rowIndex === 0 ? "th" : "td";
    return `<tr>${Array.from({ length: width }, (_, columnIndex) =>
      `<${tag}>${clipboardInlineHtml(Array.isArray(cells[columnIndex]) ? cells[columnIndex] : [], bounds)}</${tag}>`).join("")}</tr>`;
  });
  return `<table><thead>${renderedRows[0]}</thead><tbody>${renderedRows.slice(1).join("")}</tbody></table>`;
}
