import { leafenPagePath, pageIdFromPageHref } from "@/lib/pageLinks";
import { pushRoute } from "@/lib/router";
import type { Page } from "@/types";

/**
 * Where a link goes in the app: an anchor, a path, or this server's address of a page as its path, a block
 * on the page open as its anchor alone; null for any other.
 */
export function appPath(href: string) {
  if (href.startsWith("#")) return href.length > 1 ? href : null;
  if (!href.startsWith("/") && !pageIdFromPageHref(href)) return null;
  const url = new URL(href, window.location.href);
  if (url.hash && url.pathname === window.location.pathname && url.search === window.location.search) return url.hash;
  return href.startsWith("/") ? href : url.pathname + url.search + url.hash;
}

// The link a press last opened, and that press's time: its button coming up and its click are a few milliseconds apart at most.
let opened: { href: string; at: number } | null = null;
const SAME_PRESS_MS = 50;

/**
 * Whether a click on a link opens it: its first click alone. The second
 * click of a double-click chooses the word and opens nothing more, as in
 * Notion's text (seen 2026-11-18 in Chrome at 1440: a linked word
 * double-clicked in a page's text opened one tab and stood chosen). Its
 * editing text cell took the same two clicks as two tabs, played by the
 * browser extension — a hand's second click lands on the tab the first
 * opened; here one gesture opens a link once in every field.
 */
export function opensLink(event: { button: number; detail: number }) {
  return event.button === 0 && event.detail <= 1;
}

/**
 * Opens a pressed link as Notion's text and picture links open (seen
 * 2026-11-11 and 2026-11-12): an anchor (#block) stays on the page and goes
 * to its block; a page here, by its path or this server's address of it,
 * opens in its place; any other address, or any link pressed with ⌘ or Ctrl,
 * in a new tab. A page's leafen: address opens as its path (leafenPagePath).
 */
export function openPressedLink(link: string, newTab: boolean, press?: { timeStamp: number }) {
  const href = leafenPagePath(link) ?? link;
  // One press opens a link once: a database value being edited opens its link as the button comes up
  // (richTextCellEngine), and the click that follows reaches the page's text it may stand in, which
  // opens its own links at a click (NativeTiptapLinkPill) — the same press, by the time it was made.
  if (press) {
    if (opened && opened.href === href && Math.abs(press.timeStamp - opened.at) < SAME_PRESS_MS) return;
    opened = { href, at: press.timeStamp };
  }
  const here = newTab ? null : appPath(href);
  if (here?.startsWith("#")) window.location.hash = here;
  else if (here) pushRoute(here);
  else window.open(href, "_blank", "noopener,noreferrer");
}

/** The page of this server a link leads to, by its path or its address here (pageIdFromPageHref). */
export function linkedPage(href: string, pages: readonly Page[] | undefined) {
  const pageId = pageIdFromPageHref(href);
  return pageId ? pages?.find((candidate) => candidate.id === pageId && !candidate.inTrash) : undefined;
}
