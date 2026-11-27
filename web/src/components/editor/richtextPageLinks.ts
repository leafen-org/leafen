import { blockAnchor } from "@/lib/blockAddress";
import type { TextSpan } from "@/lib/types";
import { dataSourceMentionAddress, dataSourceMentionId } from "@/lib/dataSourceMentionNames";
import { escapeHtml } from "./richtextHtmlEscaping";
import { safeMentionIdValue } from "./richtextMentionPolicy";
import { mentionServerAttribute } from "./richtextMentionServer";

/**
 * How a published page shows the pages its text names: through its own
 * links, an untitled page by what its kind is called (richtext.ts), and a
 * page or database the visitor cannot open by the no-access label alone.
 */
export type RichtextPageLinks = {
  pageHref(pageId: string): string;
  unavailable: string;
};

// A link written as an app page path opens that page.
const APP_PAGE_PATH = /^\/p\/([^/?#]+)/;

function decodedPathPart(value: string) {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

/** Where a page mention opens: the published page's link, or the app's; at its block for a mention of one. */
function pageMentionHref(pageId: string, links?: RichtextPageLinks, blockId?: string) {
  const page = links ? links.pageHref(pageId) : `/p/${encodeURIComponent(pageId)}`;
  return page + blockAnchor(pageId, blockId);
}

/** Where a link opens; on a published page an app page path opens there. */
export function richtextLinkHref(href: string, links?: RichtextPageLinks) {
  const linkedPage = links ? APP_PAGE_PATH.exec(href)?.[1] : undefined;
  return linkedPage && links ? links.pageHref(decodedPathPart(linkedPage)) : href;
}

const PUBLIC_MENTION_ICON_TYPES = new Set(["emoji", "icon", "image", "custom_emoji"]);

/**
 * A page mention: a link to the page, in the app or on the published page;
 * a mention of a block on it links to the page at the block, as Notion's.
 */
export function pageMentionHtml(span: TextSpan, pageId: string, html: string, links?: RichtextPageLinks) {
  const blockId = safeMentionIdValue(span.blockId);
  const block = blockId ? ` data-mention-block-id="${escapeHtml(blockId)}"` : "";
  return `<a href="${escapeHtml(pageMentionHref(pageId, links, blockId))}" data-mention="page" data-page-id="${escapeHtml(pageId)}"`
    + `${block}${publicMentionIconAttributes(span, links)}${mentionServerAttribute()} contenteditable="false">${html}</a>`;
}

/**
 * A published page's mention shows the icon of the page it names, or without
 * one a lined document for a page holding anything, as the server read it
 * for the visitor (PublicMentionIconStyles draws it).
 */
function publicMentionIconAttributes(span: TextSpan, links?: RichtextPageLinks) {
  if (!links) return "";
  const filled = span.mentionHasContent === true ? ' data-mention-has-content="true"' : "";
  if (!span.mentionIcon || !PUBLIC_MENTION_ICON_TYPES.has(span.mentionIconType ?? "")) return filled;
  return ` data-mention-icon-type="${escapeHtml(span.mentionIconType ?? "")}" data-mention-icon="${escapeHtml(span.mentionIcon)}"${filled}`;
}

/**
 * A published page's database mention: a link to the page holding the
 * database there, marked when that page is the database's own, with the
 * database's icon the server read for the visitor; empty in the app or when
 * the server named no page.
 */
export function publicDatabaseMentionHtml(span: TextSpan, databaseId: string, html: string, links?: RichtextPageLinks) {
  const pageId = links ? safeMentionIdValue(span.mentionPageId) : undefined;
  if (!links || !pageId) return "";
  const fullPage = span.mentionPageKind === "database" ? ' data-database-page-kind="database"' : "";
  return `<a href="${escapeHtml(pageMentionHref(pageId, links))}" data-mention="database" data-database-id="${escapeHtml(databaseId)}" `
    + `data-database-page-id="${escapeHtml(pageId)}"${fullPage}${publicMentionIconAttributes(span, links)}${mentionServerAttribute()} contenteditable="false">${html}</a>`;
}

/**
 * What a database's or a data source's mention drawn closed or missing keeps
 * of what it names, as data-mention-database, which opens nothing: the
 * database's ID, or the source's address (dataSourceMentionNames.ts), by
 * which the app asks again how it stands to its reader as access changes
 * (useMentionReach.ts) and reads its name back as it opens
 * (PageMentionIconStyles.tsx, DataSourceMentionStyles.tsx), as a page's
 * keeps its page's link. A data source's drawn closed kept nothing, and
 * stayed "사용 권한 없음" after its share came back until a reload (2026-11-25).
 */
function keptTargetAttribute(span: TextSpan) {
  if (span.mention !== "database") return "";
  const source = dataSourceMentionId(span);
  const kept = source !== undefined ? safeMentionIdValue(source) && dataSourceMentionAddress(source) : safeMentionIdValue(span.databaseId);
  return kept ? ` data-mention-database="${escapeHtml(kept)}"` : "";
}

/**
 * A published page's mention its visitor can't open: "사용 권한 없음", a
 * link to the page's own address in the app when the server gave its id,
 * as Notion's leads where the page isn't found (user decision 2026-10-01).
 * A database's or a data source's keeps what it names (keptTargetAttribute).
 */
export function unavailableMentionHtml(span: TextSpan, html: string) {
  const pageId = span.mention === "page" ? safeMentionIdValue(span.pageId) : undefined;
  if (pageId) {
    return `<a href="${escapeHtml(pageMentionHref(pageId))}" data-mention="page" data-mention-unavailable="true" contenteditable="false">${html}</a>`;
  }
  return `<span data-mention="${escapeHtml(String(span.mention ?? ""))}"${keptTargetAttribute(span)} data-mention-unavailable="true" contenteditable="false">${html}</span>`;
}

/**
 * A data source's mention, the source's own (dataSourceMentionNames.ts): its
 * icon or the cylinder and its name, as Notion's (seen 2026-11-22); a press
 * opens its database at a view of it (textBlockMentionClicks.ts), its name
 * and icon read as it comes on screen (DataSourceMentionStyles.tsx). On a
 * published page, named as the server read it for the visitor, it links the
 * page holding its database there (public_content_mentioned_sources.go).
 */
export function dataSourceMentionHtml(span: TextSpan, dataSourceId: string, html: string, links?: RichtextPageLinks) {
  const pageId = links ? safeMentionIdValue(span.mentionPageId) : undefined;
  const source = `data-mention="database" data-mention-data-source="${escapeHtml(dataSourceId)}"`;
  if (links && pageId) {
    return `<a href="${escapeHtml(pageMentionHref(pageId, links))}" ${source} data-database-page-id="${escapeHtml(pageId)}" contenteditable="false">${html}</a>`;
  }
  return `<span ${source}${mentionServerAttribute()} contenteditable="false">${html}</span>`;
}

/**
 * A mention of a page or database nothing answers to: "찾을 수 없음" by its
 * kind's mark, as Notion draws one (seen 2026-11-09, 2026-11-22). A page's,
 * and a database's — which Notion keeps as its page's, so one of nothing it
 * draws as a page's — by a page's mark, a link to the page's own address,
 * where the app says it isn't found; a data source's, kept by its address
 * (notion_markdown_kept_data_sources.go), by the cylinder, linking nowhere.
 * A database's and a data source's keep what they name (keptTargetAttribute).
 */
export function missingMentionHtml(span: TextSpan, html: string) {
  const kept = keptTargetAttribute(span);
  if (dataSourceMentionId(span) !== undefined) {
    return `<span data-mention="database" data-mention-data-source=""${kept} data-mention-missing="true" contenteditable="false">${html}</span>`;
  }
  const pageId = span.mention === "page" ? safeMentionIdValue(span.pageId) : undefined;
  const href = pageId ? ` href="${escapeHtml(pageMentionHref(pageId))}"` : "";
  return `<a${href} data-mention="page"${kept} data-mention-missing="true" contenteditable="false">${html}</a>`;
}
