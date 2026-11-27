import { pageIdFromPageHref as sharedPageIdFromPageHref } from "./pageReferencePath.mjs";
import { blockIsPageItself, blockLinkTarget } from "./blockAddress";
import { fullPageDatabasePage } from "./inlineDatabaseAddress";
import { isActiveServerOrigin } from "./serverOrigin";

/**
 * Whether a web address's origin is this server's: the page's own, the
 * server's public address or an earlier one that forwards to it, so a link
 * copied on another of one's own devices names the same page.
 */
function ownOrigin(origin: string) {
  return (typeof window !== "undefined" && origin === window.location.origin) || isActiveServerOrigin(origin);
}

/**
 * The page of this server a link names: leafen://page/<ID>, /p/<ID>, or a
 * web address of this server at /p/<ID> — a full-page database's address
 * names its page (pageHref) — null for any other, a Notion page's or another
 * Leafen server's address among them — those stay links.
 */
export function pageIdFromPageHref(href: unknown) {
  const id = sharedPageIdFromPageHref(href, ownOrigin);
  return id === null ? null : fullPageDatabasePage(id) ?? id;
}

/**
 * The path in the app a page's own address leafen://page/<ID> stands for:
 * /p/<ID> and its anchor as written, so one at a block (#block-<id>, or
 * Notion's #<32 hex>) goes to that block as /p/<ID> with it does — but an
 * anchor at the page itself (blockAddress.ts), which opens the page; null for
 * any other link. Every reader of the address goes by it: a paste, a link the
 * packaged app is opened with, a link pressed in the app or its text menu.
 */
export function leafenPagePath(href: string): string | null {
  const value = href.trim();
  if (!/^leafen:\/\/page\//iu.test(value)) return null;
  const pageId = pageIdFromPageHref(value);
  if (!pageId) return null;
  const hash = value.includes("#") ? value.slice(value.indexOf("#")) : "";
  const block = blockLinkTarget(hash);
  return `/p/${encodeURIComponent(pageId)}${block && blockIsPageItself(pageId, block) ? "" : hash}`;
}

/**
 * Where in the app a link to this server goes: one at the page's own address
 * or at the server's (ownOrigin) — a packaged app's page is not at its
 * server's address, and its server may be another profile's — as the same
 * path at the page's address, a page's leafen: address as its path
 * (leafenPagePath); null for any other link.
 */
export function ownLinkInApp(href: string): URL | null {
  try {
    const address = new URL(leafenPagePath(href) ?? href, window.location.href);
    return ownOrigin(address.origin) ? new URL(`${address.pathname}${address.search}${address.hash}`, window.location.href) : null;
  } catch {
    return null;
  }
}

export function decodePathPart(value: string) {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}
