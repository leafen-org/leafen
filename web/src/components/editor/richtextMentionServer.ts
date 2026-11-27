import type { TextSpan } from "@/lib/types";
import { MENTION_SERVER_ATTRIBUTE, clipboardServer, otherServerPageUrl } from "@/lib/clipboardServer";
import { escapeHtml } from "./richtextHtmlEscaping";
import { safeMentionIdValue } from "./richtextMentionPolicy";

/**
 * The mark a drawn mention of a page, database, person or workspace emoji
 * carries: the server what it names is on, so its words copied in the
 * browser and pasted on another server read as that server's
 * (otherServerMentionSpan).
 */
export function mentionServerAttribute() {
  return ` ${MENTION_SERVER_ATTRIBUTE}="${escapeHtml(clipboardServer())}"`;
}

/** A mention's words as copied: a workspace emoji drawn as its picture is its name. */
export function mentionWords(element: HTMLElement, content: TextSpan): TextSpan {
  return content.text ? content : { ...content, text: element.querySelector("img[alt]")?.getAttribute("alt") ?? "" };
}

/**
 * A mention copied in the browser from another server's page, as it pastes
 * here: what it names is that server's, so a page's or database's mention is
 * a link there — the address its words led to, else the page's there — and a
 * person's or an emoji's is its words, as a Leafen copy made there pastes
 * (otherServerSpans). With no server a paste can trust, its words alone.
 */
export function otherServerMentionSpan(element: HTMLElement, content: TextSpan, server: string | undefined): TextSpan {
  const words = mentionWords(element, content);
  if (!server) return words;
  const link = linkThere(element.getAttribute("href"), server);
  if (link) return { ...words, link };
  const pageId = element.dataset.mention === "page" ? safeMentionIdValue(element.dataset.pageId) : undefined;
  return pageId ? { ...words, link: otherServerPageUrl(server, pageId, safeMentionIdValue(element.dataset.mentionBlockId)) } : words;
}

/** A mention's link on the server it was drawn on: a web address, one written as a path leading there. */
function linkThere(href: string | null, server: string) {
  if (!href) return undefined;
  try {
    const url = new URL(href, server);
    return url.protocol === "http:" || url.protocol === "https:" ? url.toString() : undefined;
  } catch {
    return undefined;
  }
}
