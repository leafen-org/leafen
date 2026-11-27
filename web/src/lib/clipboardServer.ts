import { blockAnchor } from "./blockAddress";
import type { TextSpan } from "./types";
import { isActiveServerOrigin, linkOrigin, serverOrigin } from "./serverOrigin";

/**
 * The server a Leafen copy names as the one it was made on: the address its
 * pages open at for others (linkOrigin), so a paste on another server can
 * lead back to what it names there.
 */
export function clipboardServer(): string {
  return linkOrigin();
}

/**
 * The other server a pasted Leafen copy was made on, by the server it names:
 * null for this one — at its own, public or earlier address — or for a copy
 * naming none (made before copies named theirs); undefined for a name that
 * is no web server's, which no paste trusts.
 */
export function otherClipboardServer(server: unknown): string | null | undefined {
  if (server === undefined || server === linkOrigin() || server === serverOrigin()) return null;
  if (typeof server !== "string") return undefined;
  let url: URL;
  try {
    url = new URL(server);
  } catch {
    return undefined;
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return undefined;
  const own = (typeof window !== "undefined" && url.origin === window.location.origin) || isActiveServerOrigin(url.origin);
  return own ? null : url.origin;
}

/** The mark a mention the app draws carries: the server what it names is on (clipboardServer). */
export const MENTION_SERVER_ATTRIBUTE = "data-mention-server";

/** Whether a mention names what is on its server: a page, a database, a person, a workspace emoji. */
export function isServerMention(mention: unknown) {
  return typeof mention === "string" && SERVER_MENTIONS.has(mention);
}

/**
 * The other server a mention in copied HTML (the app's or a published page's
 * words copied in the browser, not a Leafen copy) names, by the same rule:
 * the server its mark names, else the one its link is at. One naming neither
 * — drawn before mentions named theirs, its link left relative — is this
 * one's (null).
 */
export function otherMentionServer(named: string | null, href: string | null): string | null | undefined {
  if (named !== null) return otherClipboardServer(named);
  let origin: string;
  try {
    origin = new URL(href ?? "").origin;
  } catch {
    return null;
  }
  return otherClipboardServer(origin);
}

/** The fields of a span naming the comment and suggested-edit threads on its words. */
export const THREAD_FIELDS: ReadonlySet<string> = new Set(["commentId", "suggestion", "suggestionId", "suggestionBefore"]);

/**
 * Words as a paste brings them, wherever they were copied — this page,
 * another, another server, another app: no comment or suggested-edit thread
 * is on them, their words staying (a suggestion's, in or away, as words), as
 * Notion's paste (seen 2026-11-11).
 */
export function withoutThreadMarks<Span extends object>(spans: readonly Span[]): Span[] {
  return spans.map((span) => {
    if (!Object.keys(span).some((key) => THREAD_FIELDS.has(key))) return span;
    return Object.fromEntries(Object.entries(span).filter(([key]) => !THREAD_FIELDS.has(key))) as Span;
  });
}

// The formatting a format suggestion changes, which it keeps as it was
// (suggestionBefore, JSON) — as the server's text_suggestions.go names it.
const SUGGESTED_FORMATTING = new Set(["bold", "italic", "underline", "strikethrough", "code", "color", "background", "link"]);

function formattingBefore(raw: string | undefined): Record<string, unknown> {
  try {
    const before: unknown = JSON.parse(raw ?? "{}");
    return before && typeof before === "object" && !Array.isArray(before) ? before as Record<string, unknown> : {};
  } catch {
    return {};
  }
}

/**
 * Words as a copy gives them around suggested edits, as Notion's (seen
 * 2026-11-11): no suggestion on them, and suggested formatting as it was
 * before. Copied as blocks (chosen with Esc, ⌘C) or as words over more than
 * one line, words suggested in are left out — each line's text as it was
 * before its suggestions, a suggested line coming as an empty one; copied
 * as words within a line (some, or all of them) and as other apps' text,
 * they stay with the words suggested away.
 */
export function copiedWords(spans: readonly TextSpan[], asBlocks: boolean): TextSpan[] {
  const words: TextSpan[] = [];
  for (const span of spans) {
    if (span.suggestion === undefined && span.suggestionId === undefined && span.suggestionBefore === undefined) {
      words.push(span);
      continue;
    }
    if (asBlocks && span.suggestion === "insert") continue;
    const { suggestion, suggestionId: _thread, suggestionBefore, ...kept } = span;
    const fields = kept as Record<string, unknown>;
    if (suggestion === "format") {
      for (const [mark, value] of Object.entries(formattingBefore(suggestionBefore))) {
        if (!SUGGESTED_FORMATTING.has(mark)) continue;
        if (value === true || (typeof value === "string" && value !== "")) fields[mark] = value;
        else delete fields[mark];
      }
    }
    words.push(kept);
  }
  return words;
}

/** A block's content keys naming the suggestion it is part of: a suggested block's thread, a suggested break's and the block after it. */
const BLOCK_SUGGESTION_FIELDS = ["suggestionId", "suggestionAlone", "splitSuggestionId", "splitBlockId"];

/** A copied block's content as it stands, no part of a suggestion, as Notion's copied block pastes (seen 2026-11-11). */
export function copiedBlockContent<Content extends object>(content: Content): Content {
  if (!BLOCK_SUGGESTION_FIELDS.some((key) => key in content)) return content;
  return Object.fromEntries(Object.entries(content).filter(([key]) => !BLOCK_SUGGESTION_FIELDS.includes(key))) as Content;
}

/** Where a page of another server opens there, at its block for a block's. */
export function otherServerPageUrl(server: string, pageId: string, blockId?: string) {
  return `${server}/p/${encodeURIComponent(pageId)}${blockAnchor(pageId, blockId)}`;
}

// A link written as a path in the app, or as Leafen's own page address,
// opens on the server it was written on.
const APP_PAGE_LINK = /^leafen:\/\/page\/([^/?#]+)(.*)$/iu;

function otherServerLink(link: string, server: string | null) {
  const page = APP_PAGE_LINK.exec(link);
  const path = page ? `/p/${page[1]}${page[2]}` : /^\/(?![/\\])/u.test(link) ? link : null;
  return path === null ? link : server ? `${server}${path}` : undefined;
}

// What a span names on the server it was written on: the thing a mention
// names, a comment's thread, a suggested edit's, and what a published page
// read of them for its visitor.
const SERVER_FIELDS = ["pageId", "blockId", "databaseId", "customEmojiId", "userId", "commentId", "suggestion", "suggestionId",
  "suggestionBefore", "unavailable", "mentionIcon", "mentionIconType", "mentionHasContent", "mentionPageId", "mentionPageKind",
  "mentionViewBlock", "mentionSourceTitles", "customEmojiPublic"] as const;
const SERVER_MENTIONS = new Set(["page", "database", "person", "custom_emoji"]);

/** The fields of a span, as a page's text or a table's cell keeps it, that say what it names. */
type NamingSpan = Pick<TextSpan, "text" | "pageId" | "blockId" | "link"> & { mention?: string };

/**
 * Words copied on another server as they paste here: what they name there —
 * a page, a block, a database, a person, a workspace emoji, a comment, a
 * suggested edit — is that server's, so a page's or block's mention is a
 * link to it there, as a Leafen copy gives another app
 * (blockMarkdownInline.ts), and the other mentions keep their words alone;
 * a link to a page by its path in the app leads to that server. Dates,
 * outside links' mentions, equations and formatting stay as they are. With
 * no server a paste can trust (null), what names that server's is words alone.
 */
export function otherServerSpans<Span extends NamingSpan>(spans: readonly Span[], server: string | null): Span[] {
  return spans.map((span) => {
    const kept: Span = { ...span };
    for (const field of SERVER_FIELDS) delete (kept as Record<string, unknown>)[field];
    if (span.mention === "page" && span.pageId && server) {
      delete kept.mention;
      return { ...kept, link: otherServerPageUrl(server, span.pageId, span.blockId) };
    }
    if (span.mention && SERVER_MENTIONS.has(span.mention)) delete kept.mention;
    else if (kept.link && span.mention !== "external") {
      const link = otherServerLink(kept.link, server);
      if (link) kept.link = link;
      else delete kept.link;
    }
    return kept;
  });
}
