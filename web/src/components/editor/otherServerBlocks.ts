import { otherServerPageUrl, otherServerSpans, withoutThreadMarks } from "@/lib/clipboardServer";
import { activePastedBlockLabels, activePersistentGeneratedLabels } from "@/lib/persistentGeneratedLabels";
import type { BlockContent, TextSpan } from "@/lib/types";
import type { PastedBlock } from "./markdownPasteTypes";

/**
 * Where blocks copied on another server came from: that server (null when the
 * copy names none a paste can trust), and the page they were on there when
 * the copy says.
 */
export type OtherServerCopy = { server: string | null; pageId: string | null };

const text = (value: unknown) => typeof value === "string" ? value.trim() : "";
const spans = (value: unknown) => Array.isArray(value) ? value as TextSpan[] : null;

/**
 * A block's content with the words it holds — its text, caption, titles, a
 * table row's cells — as `map` makes them, and a button's blocks' content as
 * `inner` makes it.
 */
function contentWords(content: Record<string, unknown>, map: (words: TextSpan[]) => TextSpan[],
  inner: (content: Record<string, unknown>) => Record<string, unknown>): Record<string, unknown> {
  const kept = { ...content };
  for (const field of ["rich", "caption", "linkedTitleSpans", "pageTitleSpans"]) {
    const value = spans(kept[field]);
    if (value) kept[field] = map(value);
  }
  if (Array.isArray(kept.cells)) kept.cells = kept.cells.map((cell) => spans(cell) ? map(cell as TextSpan[]) : cell);
  if (Array.isArray(kept.buttonTemplate)) kept.buttonTemplate = kept.buttonTemplate.map(function template(block: unknown): unknown {
    if (typeof block !== "object" || block === null) return block;
    const { content: blockContent, children, ...rest } = block as Record<string, unknown>;
    return { ...rest,
      ...(typeof blockContent === "object" && blockContent !== null ? { content: inner(blockContent as Record<string, unknown>) } : {}),
      ...(Array.isArray(children) ? { children: children.map(template) } : {}) };
  });
  return kept;
}

/** A block's content copied on this server as it pastes: its words keep no comment or suggested-edit thread (withoutThreadMarks). */
export function pastedContent(content: Record<string, unknown>): Record<string, unknown> {
  return contentWords(content, withoutThreadMarks, pastedContent);
}

/**
 * A block's content copied on another server as it pastes here: its words
 * name that server's pages by links to them there (otherServerSpans), and an
 * icon from that server's files or workspace emoji gives way to the usual one.
 */
export function otherServerContent(content: Record<string, unknown>, server: string | null): Record<string, unknown> {
  const kept = contentWords(content, (words) => otherServerSpans(words, server), (inner) => otherServerContent(inner, server));
  if (kept.iconType === "image" || kept.iconType === "custom_emoji") {
    delete kept.icon;
    delete kept.iconType;
  }
  return kept;
}

const FILE_TYPES = new Set(["image", "video", "audio", "pdf", "file", "embed"]);

/**
 * The line a block copied on another server is here when it names something
 * there — a page, a link to one, a database, a synced block's original, a
 * file kept there: its name, linked to it there when the copy says where
 * (`id` is the block's own id there). Notion's paste of blocks copied in
 * another workspace keeps naming that workspace's pages, databases and
 * synced blocks, and copies its sub-pages and databases (seen 2026-11-11);
 * another server's are not here, so a link to them there takes their place.
 * Null for any other block, which keeps itself (otherServerContent).
 */
export function otherServerLine(type: string, content: Record<string, unknown>, plainText: string, id: string | null,
  from: OtherServerCopy): { title: string; href: string | null } | null {
  const { server, pageId } = from;
  const atBlock = server && pageId && id ? otherServerPageUrl(server, pageId, id) : null;
  const untitled = () => activePersistentGeneratedLabels().untitled;
  const linkedPage = text(content.childPageId) || text(content.pageId);
  if ((type === "link_to_page" || type === "child_page" || type === "child_database") && linkedPage) {
    return { title: text(content.childPageTitle) || plainText || untitled(), href: server ? otherServerPageUrl(server, linkedPage) : null };
  }
  if (type === "inline_database") {
    // A database opens at its block's own address there; a linked view at its place on its page.
    const copied = text((content.copiedDatabase as { blockId?: unknown } | undefined)?.blockId)
      || (content.linkedDatabaseSource ? "" : id ?? "");
    return { title: text(content.linkedTitle) || plainText || untitled(), href: server && copied ? otherServerPageUrl(server, copied) : atBlock };
  }
  const original = text(content.syncedBlockId);
  if (type === "synced_block" && original) {
    const page = text(content.syncedPageId) || pageId;
    return { title: activePastedBlockLabels().syncedBlock, href: server && page ? otherServerPageUrl(server, page, original) : atBlock };
  }
  // A file uploaded there is fetched from there, by those who can open its page.
  if (FILE_TYPES.has(type) && text(content.fileId) && content.source !== "external" && !text(content.url)) {
    return { title: text(content.name) || text(content.fileName) || activePastedBlockLabels().file, href: atBlock };
  }
  return null;
}

/** A line of words, linked when `href` says where to, in a block's color. */
function lineBlock(content: Record<string, unknown>, line: { title: string; href: string | null }): PastedBlock {
  const color = typeof content.color === "string" ? { color: content.color } : {};
  const span: TextSpan = line.href ? { text: line.title, link: line.href } : { text: line.title };
  return { type: "paragraph", content: { rich: [span], ...color } as BlockContent, plainText: line.title };
}

/** A block of Leafen's own copy made on another server as it pastes here (otherServerLine, otherServerContent). */
export function otherServerBlock(block: PastedBlock, id: string | null, from: OtherServerCopy): PastedBlock {
  const content = (block.content ?? {}) as Record<string, unknown>;
  const line = otherServerLine(block.type, content, text(block.plainText), id, from);
  return line ? lineBlock(content, line) : { ...block, content: otherServerContent(content, from.server) as BlockContent };
}
