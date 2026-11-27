import { useSyncExternalStore } from "react";
import type { Page } from "@/types";
import { safeStoredFileUrl } from "./urls";
import { nativeWorkspaceFileIdFromLocator } from "./workspaceFileTransport";
import { namesOtherPages, titleWithClosedMentions } from "./closedMentions";
import { subscribePeopleDirectory } from "./peopleDirectory";
import type { TextSpan } from "./types";
import { reportDesktopTabChrome, reportDesktopTabUnread } from "./desktopTabs";

const FAVICON_MARKER = "leafen-dynamic-favicon";

function escapeXml(value: string) {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function svgDataUrl(svg: string) {
  return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

function emojiFaviconHref(emoji: string) {
  return svgDataUrl(
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><text x="16" y="23" text-anchor="middle" font-size="24">${escapeXml(emoji)}</text></svg>`
  );
}

const DEFAULT_FAVICON_HREF = svgDataUrl(
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect x="6" y="4" width="20" height="24" rx="2.8" fill="#fff" stroke="#2f3437" stroke-width="2"/><path d="M20 4v7h6" fill="none" stroke="#2f3437" stroke-width="2" stroke-linejoin="round"/></svg>'
);

const DATABASE_FAVICON_HREF = svgDataUrl(
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect x="5" y="6" width="22" height="20" rx="2.5" fill="#fff" stroke="#2f3437" stroke-width="2"/><path d="M5 13h22M12 6v20M20 6v20" stroke="#2f3437" stroke-width="2"/></svg>'
);

/**
 * A page's icon as the window's, where it needs nothing read: an emoji, a
 * linked picture, else the document's or database's mark. A built-in icon
 * and a picture read signed in are drawn by usePageFaviconHref
 * (components/page/pageFavicon.ts), which every screen names its icon by.
 */
export function pageFaviconHref(page: Pick<Page, "icon" | "iconType"> & { kind?: string } | undefined | null) {
  if (!page) return DEFAULT_FAVICON_HREF;
  if (page.iconType === "image") {
    if (nativeWorkspaceFileIdFromLocator(page.icon)) return DEFAULT_FAVICON_HREF;
    return safeStoredFileUrl(page.icon, ["data:image/"]) || DEFAULT_FAVICON_HREF;
  }
  if (page.iconType === "emoji" && page.icon) return emojiFaviconHref(page.icon);
  if (page.kind === "database") return DATABASE_FAVICON_HREF;
  return DEFAULT_FAVICON_HREF;
}

// The window's title, as Notion's (seen 2026-10-19: "(7) Page |
// Notion"): the inbox's unread count first, then the screen's name and
// " | Leafen"; a screen with no name of its own is "Leafen". A page shown to
// the public (a shared page, a published site, a view link, an app's guest
// screen) is named alone, as Notion's published site is. Every screen hands
// its name here and nothing else makes the title (2026-10-19: pages read
// "… - Leafen", the library and apps their name alone, and no count showed).
let shown: { title: string; plain: boolean; titleSpans?: readonly TextSpan[]; pageId?: string; opening?: boolean } = { title: "Leafen", plain: false };
// A first run's step over the whole window names the window and its tab
// after itself while it shows, whatever the screen behind it names, as
// Notion's app's first screens name theirs (its web client: the app's login
// names its tab "Log in", read 2026-11-14; 2026-11-13: a first run's tabs
// read "Leafen", or the welcome page's name behind it).
let cover: { name: string } | null = null;
let unreadCount = 0;
// A full-page database's view, once chosen among two or more, follows its
// name (Notion's, seen 2026-11-06: "DB | 보드 | Notion"); when a view counts
// as chosen is useDatabaseDocumentViewName's one rule.
let viewName: string | null = null;

// A window or tab names its page by the title's first 100 UTF-16 units, as
// Notion's does (seen 2026-11-10: a 200,000-character title's window read its
// first 100 characters, one of four-byte characters "0ggg" and 48 of them).
const NAME_UNITS = 100;

function namedFor(name: string) {
  if (name.length <= NAME_UNITS) return name;
  const high = name.charCodeAt(NAME_UNITS - 1);
  return name.slice(0, high >= 0xd800 && high <= 0xdbff ? NAME_UNITS - 1 : NAME_UNITS);
}

function windowTitle() {
  if (shown.plain && !cover) return namedFor(titleWithClosedMentions(shown.title, shown.titleSpans));
  // Past nine it says "9+", as Notion's title does, while the sidebar's
  // badge shows the whole count (seen 2026-11-06: "(9+)" beside a badge of 44).
  const count = unreadCount > 9 ? "(9+) " : unreadCount > 0 ? `(${unreadCount}) ` : "";
  if (cover) return `${count}${namedFor(cover.name)} | Leafen`;
  // Its mentions as a plain title says them (closedMentions.ts).
  const name = namedFor(titleWithClosedMentions(shown.title, shown.titleSpans));
  if (!name || name === "Leafen") return `${count}Leafen`;
  return viewName ? `${count}${name} | ${viewName} | Leafen` : `${count}${name} | Leafen`;
}

// The screen's name on its tab in the Mac app's strip (desktopTabs.ts):
// Notion's tab names the page alone, without the count or " | Notion". A
// screen still opening names none: its tab keeps the name it has, an empty
// skeleton on a new tab, as Notion's while its page loads (2026-11-13: it
// read "Leafen" for the 0.3–3 seconds a page loaded).
function tabName() {
  if (cover) return namedFor(cover.name);
  if (shown.opening) return "";
  if (shown.plain) return namedFor(titleWithClosedMentions(shown.title, shown.titleSpans));
  return namedFor(titleWithClosedMentions(shown.title, shown.titleSpans)) || "Leafen";
}

// The screen's icon on its tab: its own alone. A page or database with no
// icon and a screen showing the app's mark draw none there — the tab shows
// the name only, its first letters when narrow — as Notion's app drops its
// default favicon from its tabs (7.42.3's normalizeDefaultDesktopFavicon,
// seen 2026-11-12).
let shownIcon = "";

function ownIcon(href: string) {
  if (!href || href === DEFAULT_FAVICON_HREF || href === DATABASE_FAVICON_HREF) return "";
  try {
    return new URL(href, window.location.href).pathname === "/favicon.ico" ? "" : href;
  } catch {
    return href;
  }
}

function tellTab() {
  if (typeof document === "undefined") return;
  reportDesktopTabChrome(tabName(), cover ? "" : ownIcon(shownIcon), cover ? "" : shown.pageId ?? "");
}

/**
 * Names the window and its tab after a first run's step covering the app
 * while it shows (`name`, its title), whatever the screen behind names;
 * returns the way to end it, which names them after that screen again.
 */
export function coverDocumentChrome(name: string): () => void {
  const mine = { name };
  cover = mine;
  keptThroughReload = false;
  if (typeof document !== "undefined") document.title = windowTitle();
  tellTab();
  return () => {
    if (cover !== mine) return;
    cover = null;
    if (typeof document !== "undefined") document.title = windowTitle();
    tellTab();
  };
}

/** The view a full-page database shows, named in the window's title after the page; null when none. */
export function setDocumentViewName(name: string | null) {
  viewName = name?.trim() || null;
  if (typeof document !== "undefined" && !keptThroughReload) document.title = windowTitle();
  tellTab();
}

const unreadListeners = new Set<() => void>();

/** The inbox's unread count, shown before the window's title and on the top bar's sidebar button. */
export function setDocumentUnread(count: number) {
  const next = Math.max(0, Math.floor(count));
  const changed = next !== unreadCount;
  unreadCount = next;
  if (typeof document !== "undefined" && !keptThroughReload) document.title = windowTitle();
  reportDesktopTabUnread(next);
  if (changed) unreadListeners.forEach((listener) => listener());
}

/** Calls `listener` whenever the inbox's unread count changes. */
export function onDocumentUnread(listener: () => void): () => void {
  unreadListeners.add(listener);
  return () => { unreadListeners.delete(listener); };
}

/** The inbox's unread count, as the sidebar's inbox reads it. */
export function useDocumentUnread() {
  return useSyncExternalStore(onDocumentUnread, () => unreadCount, () => 0);
}

// A title mentioning a page or database names it as it is now: renamed, the
// window and its tab follow, as Notion's (closedMentions.ts).
if (typeof document !== "undefined") {
  subscribePeopleDirectory(() => {
    if (cover || keptThroughReload || shown.opening || !namesOtherPages(shown.titleSpans)) return;
    const title = windowTitle();
    if (document.title === title) return;
    document.title = title;
    tellTab();
  });
}

export function setDocumentChrome({ title, titleSpans, iconHref, plain = false, pageId, opening = false }: {
  title: string;
  /** The title's spans, which say its mentions (closedMentions.ts). */
  titleSpans?: readonly TextSpan[];
  iconHref?: string;
  plain?: boolean;
  /** The page whose own name and icon these are: in the Mac and Windows
   *  apps its other tabs take them too (desktop/window_tabs_names.go). */
  pageId?: string;
  /** The screen that comes has yet to name itself (resetDocumentChrome). */
  opening?: boolean;
}) {
  shown = { title, plain, titleSpans, pageId, opening };
  // The name kept through a reload of the address stays while the screen opens.
  if (opening && keptThroughReload) return;
  keptThroughReload = false;
  document.title = windowTitle();
  showIcon(iconHref || DEFAULT_FAVICON_HREF);
  tellTab();
}

// The window's icon is the page head's own icon link, its address changed,
// as Notion's is (its one "shortcut icon", read 2026-11-23). Chrome and Edge
// keep for an address the icon of the link the page had as it loaded and
// follow its changes, but took nothing from a second link added after: the
// icon they kept for the page — its history's and a bookmark's — stayed
// Leafen's mark under a page's emoji (measured 2026-11-23, Chrome 158 on a
// Mac and Edge 158 on Windows, nothing attached to them).
function showIcon(href: string) {
  let link = document.head.querySelector<HTMLLinkElement>(`link[data-${FAVICON_MARKER}="true"]`)
    ?? document.head.querySelector<HTMLLinkElement>('link[rel~="icon"]');
  if (!link) {
    link = document.createElement("link");
    link.rel = "icon";
    document.head.appendChild(link);
  }
  link.dataset.leafenDynamicFavicon = "true";
  shownIcon = href;
  link.href = href;
  link.type = href.startsWith("data:image/svg+xml") ? "image/svg+xml" : "";
}

// The same address loaded again — a new build's reload behind other tabs
// (reloadAtRest.ts), a stale screen's, a language's, the person's own —
// keeps the window's name and icon until the screen that comes names
// itself: tabs behind others that a new build reloaded read "Leafen" in the
// tab strip for minutes while they started on a busy machine (2026-11-12,
// OP-15: 25 tabs). Notion's tabs keep their names while a page loads.
const KEPT_CHROME_KEY = "leafen:window-chrome";
let keptThroughReload = false;
if (typeof window !== "undefined" && typeof document !== "undefined") {
  try {
    const kept = JSON.parse(window.sessionStorage.getItem(KEPT_CHROME_KEY) ?? "null") as { href?: unknown; title?: unknown; icon?: unknown } | null;
    window.sessionStorage.removeItem(KEPT_CHROME_KEY);
    if (kept?.href === window.location.href && typeof kept.title === "string" && kept.title) {
      keptThroughReload = true;
      document.title = kept.title;
      // A picture read signed in was this page's own object, gone with it.
      if (typeof kept.icon === "string" && kept.icon && !kept.icon.startsWith("blob:")) showIcon(kept.icon);
    }
  } catch {
    // No session storage: the page loads named as a new one.
  }
  window.addEventListener("pagehide", () => {
    try {
      window.sessionStorage.setItem(KEPT_CHROME_KEY, JSON.stringify({ href: window.location.href, title: document.title, icon: shownIcon }));
    } catch {
      // Named as a new page when it comes back.
    }
  });
}

/**
 * Names the window after `next` while it is held, as a database row opened
 * full names Notion's window (seen 2026-11-08); the release puts back the
 * title, view and icon it showed before.
 */
export function holdDocumentChrome(next: { title: string; titleSpans?: readonly TextSpan[]; iconHref?: string }) {
  const before = shown, view = viewName, iconBefore = shownIcon;
  viewName = null;
  setDocumentChrome(next);
  return () => {
    viewName = view;
    setDocumentChrome({ title: before.title, titleSpans: before.titleSpans, iconHref: iconBefore, plain: before.plain, pageId: before.pageId,
      opening: before.opening });
  };
}

/**
 * The window named "Leafen", as a screen with no name of its own; `opening`
 * while the screen that comes has yet to name itself, its tab in the Mac and
 * Windows apps meanwhile keeping the name it has (tabName).
 */
export function resetDocumentChrome(opening = false) {
  setDocumentChrome({ title: "Leafen", iconHref: "/favicon.ico", opening });
}
