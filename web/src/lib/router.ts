import { fullPageDatabaseAtAddress, inlineDatabaseHolderPath, pageAtAddress } from "./inlineDatabaseAddress";
import { useEffect, useMemo, useSyncExternalStore } from "react";
import { isActiveServerOrigin } from "./serverOrigin";
import { desktopTabPinned, openInNewDesktopTab } from "./desktopTabPage";
import { openPastPaint, openPressedPastPaint } from "./openPastPaint";
import { forwardFromQuickSearch } from "./quickSearchWindow";
import { holdLeavingPage } from "./pageLeftAfterPaint";
import { closeWindowsForLink, opensSettingsOver } from "./productWindows";
import { pagePathForOldPath } from "./pageAddressForms";

export const ROUTE_CHANGE_EVENT = "leafen:routechange";

export type RouteKind = "page" | "workspace" | "share" | "form" | "site" | "database";

/** The library's views, as Notion's: its teamspaces first, then its pages
 * (AI notes and skills left out), and Leafen's documents. */
export const LIBRARY_VIEWS = ["teamspaces", "recents", "favorites", "documents", "shared", "private"] as const;
export type LibraryViewId = typeof LIBRARY_VIEWS[number];

export function libraryHref(view: LibraryViewId) {
  return `/library/${view}`;
}

/** Notion's developer tools tabs and their addresses, Worker apart. */
export const DEVELOPER_TOOLS_TABS = [
  { id: "start", path: "" },
  { id: "connections", path: "connections" },
  { id: "tokens", path: "tokens" },
  { id: "mcp-tokens", path: "mcp-tokens" },
] as const;
export type DeveloperToolsTab = typeof DEVELOPER_TOOLS_TABS[number]["id"];

export function developerToolsHref(tab: DeveloperToolsTab) {
  const path = DEVELOPER_TOOLS_TABS.find((candidate) => candidate.id === tab)?.path ?? "";
  return path ? `/developers/${path}` : "/developers";
}

/** A connection's own page, as Notion's /developers/connections/〈id〉. */
export function developerConnectionHref(connectionId: string) {
  return `/developers/connections/${encodeURIComponent(connectionId)}`;
}

/** An MCP token's own page, as Notion's /developers/mcp-tokens/〈id〉. */
export function developerMcpTokenHref(connectionId: string) {
  return `/developers/mcp-tokens/${encodeURIComponent(connectionId)}`;
}

export type RouteInfo =
  | { kind: "home" }
  // "/library" alone names no view: the library opens on the last one seen.
  | { kind: "library"; view?: LibraryViewId }
  | { kind: "trash" }
  /** "내 작업": the tasks assigned to the reader in the sources they pick (MyTasksPage). */
  | { kind: "myTasks" }
  /** The workspace's people list, and a member's profile (peopleProfiles.ts). */
  | { kind: "people"; userId?: string }
  | { kind: "settings" }
  | { kind: "account" }
  | { kind: "page"; pageId: string }
  | { kind: "workspace"; workspaceSlug: string }
  | { kind: "share"; shareId: string }
  | { kind: "view" }
  | { kind: "form"; formToken: string }
  | { kind: "site"; siteSlug: string }
  | { kind: "database"; databaseId: string }
  /** An installed app's screen and the path inside it (docs/app-platform.md 4, 17). */
  | { kind: "app"; slug: string; subpath: string }
  /** The apps a person may open, ask for or add (docs/app-platform.md 3). */
  | { kind: "apps" }
  /** "개발자 도구" and its tab, as Notion's /developers. */
  | { kind: "developers"; tab: DeveloperToolsTab; connectionId?: string }
  /** A guest's view of an app through a guest link; its token rides after "#" (docs/app-platform.md 9). */
  | { kind: "appGuest" }
  /** A workspace's invite link; its key rides after "#". */
  | { kind: "join" }
  | { kind: "invalid"; routeKind: RouteKind }
  | { kind: "unknown" };

/**
 * Home's own address, as Notion's home has one (seen 2026-11-13: a reload
 * there or the address opened anew is home again). The root is where the
 * app starts: it opens the start page, and home takes this address once the
 * start page is chosen or none is (AuthenticatedProductRoute).
 */
export const HOME_PATH = "/home";

export function homeRouteRedirectForPath(pathname: string): "/" | null {
  // A device's join link on a device already signed in (AuthGate takes it otherwise).
  if (/^\/device\/[^/?#]+\/?$/u.test(pathname)) return "/";
  // The sign-in address, once signed in (a phone's code sign-in at /login
  // landed on "페이지 찾지 못함", 2026-11-09), as Notion's /login opens the workspace.
  if (/^\/login\/?$/u.test(pathname)) return "/";
  return pathname === "/p" || pathname === "/p/" ? "/" : null;
}

/** An old page address opens as the app's own, its query and fragment kept. */
export function forwardOldPageAddress() {
  if (typeof window === "undefined") return;
  const path = pagePathForOldPath(window.location.pathname);
  if (path) window.history.replaceState(window.history.state, "", `${path}${window.location.search}${window.location.hash}`);
}

const SEGMENT_ROUTES: ReadonlyArray<{
  kind: RouteKind;
  pattern: RegExp;
  prefix: string;
}> = [
  { kind: "page", pattern: /^\/p\/([^/?#]+)\/?$/, prefix: "/p" },
  { kind: "workspace", pattern: /^\/workspace\/([^/?#]+)\/?$/, prefix: "/workspace" },
  { kind: "share", pattern: /^\/share\/([^/?#]+)\/?$/, prefix: "/share" },
  { kind: "form", pattern: /^\/form\/([^/?#]+)\/?$/, prefix: "/form" },
  { kind: "site", pattern: /^\/site\/([^/?#]+)\/?$/, prefix: "/site" },
  { kind: "database", pattern: /^\/database\/([^/?#]+)\/?$/, prefix: "/database" },
];

function decodeRouteSegment(encoded: string): string | null {
  try {
    const decoded = decodeURIComponent(encoded);
    // Decoded separators would change the path's meaning after matching, while
    // controls and query/hash delimiters never belong in an id/slug segment.
    if (!decoded || /[\/\\?#\u0000-\u001f\u007f]/.test(decoded)) return null;
    return decoded;
  } catch {
    // A stray or incomplete percent escape is an invalid address, not an app
    // exception that should fall through to the root error boundary.
    return null;
  }
}

export function routeInfoFromPath(pathname: string): RouteInfo {
  if (pathname === "/" || pathname === "" || pathname === HOME_PATH || pathname === `${HOME_PATH}/` || homeRouteRedirectForPath(pathname)) {
    return { kind: "home" };
  }
  if (pathname === "/trash" || pathname === "/trash/") return { kind: "trash" };
  if (pathname === "/my-tasks" || pathname === "/my-tasks/") return { kind: "myTasks" };
  if (pathname === "/people" || pathname === "/people/") return { kind: "people" };
  const person = pathname.match(/^\/people\/([^/?#]+)\/?$/);
  if (person) {
    const userId = decodeRouteSegment(person[1]);
    return userId ? { kind: "people", userId } : { kind: "unknown" };
  }
  const library = pathname.match(/^\/library(?:\/([^/?#]+))?\/?$/);
  if (library) {
    const view = library[1];
    if (view === undefined) return { kind: "library" };
    return (LIBRARY_VIEWS as readonly string[]).includes(view)
      ? { kind: "library", view: view as LibraryViewId }
      : { kind: "unknown" };
  }
  // The former consoles' addresses, which open the settings window.
  if (pathname === "/settings" || pathname === "/settings/") return { kind: "settings" };
  if (pathname === "/admin/settings" || pathname === "/admin/settings/") return { kind: "settings" };
  if (pathname === "/account" || pathname === "/account/") return { kind: "account" };
  if (pathname === "/apps" || pathname === "/apps/") return { kind: "apps" };
  const developers = pathname.match(/^\/developers(?:\/([a-z-]+))?(?:\/([A-Za-z0-9_-]+))?\/?$/);
  if (developers) {
    const tab = DEVELOPER_TOOLS_TABS.find((candidate) => candidate.path === (developers[1] ?? ""));
    if (developers[2]) return tab?.id === "connections" || tab?.id === "mcp-tokens"
      ? { kind: "developers", tab: tab.id, connectionId: developers[2] } : { kind: "unknown" };
    return tab ? { kind: "developers", tab: tab.id } : { kind: "unknown" };
  }
  const app = pathname.match(/^\/apps\/([a-z0-9][a-z0-9-]{1,39})(?:\/(.*))?$/);
  if (app) return { kind: "app", slug: app[1], subpath: app[2] ?? "" };
  // A view link's page; its token rides after "#".
  if (pathname === "/view" || pathname === "/view/") return { kind: "view" };
  if (pathname === "/app-guest" || pathname === "/app-guest/") return { kind: "appGuest" };
  if (pathname === "/join" || pathname === "/join/") return { kind: "join" };

  for (const route of SEGMENT_ROUTES) {
    const match = pathname.match(route.pattern);
    if (match?.[1]) {
      const segment = decodeRouteSegment(match[1]);
      if (!segment) return { kind: "invalid", routeKind: route.kind };
      if (route.kind === "page") return { kind: "page", pageId: pageAtAddress(segment) };
      if (route.kind === "workspace") return { kind: "workspace", workspaceSlug: segment };
      if (route.kind === "share") return { kind: "share", shareId: segment };
      if (route.kind === "form") return { kind: "form", formToken: segment };
      if (route.kind === "site") return { kind: "site", siteSlug: segment };
      return { kind: "database", databaseId: segment };
    }
    if (pathname === route.prefix || pathname.startsWith(`${route.prefix}/`)) {
      return { kind: "invalid", routeKind: route.kind };
    }
  }

  return { kind: "unknown" };
}

export function isPublicSharePath(pathname: string) {
  return pathname === "/share" || pathname.startsWith("/share/");
}

export function isPublicFormPath(pathname: string) {
  return pathname === "/form" || pathname.startsWith("/form/");
}

export function isPublicSitePath(pathname: string) {
  return pathname === "/site" || pathname.startsWith("/site/");
}

export function isPublicAnonymousPath(pathname: string) {
  return isPublicSharePath(pathname) || isPublicFormPath(pathname) || isPublicSitePath(pathname) ||
    pathname === "/view" || pathname === "/view/" || pathname === "/app-guest" || pathname === "/app-guest/";
}

// The address with the page that holds the inline database it opens, if it
// opens one: the database moved to another page keeps its address and opens
// alone there (2026-11-13: once moved, its old page drew whole at the same
// address, the key counting holders known, not which page holds it); and
// with the page of the full-page database it opens, which is known after its
// address may be.
function getLocationKey() {
  if (typeof window === "undefined") return "/";
  const { pathname, search, hash } = window.location;
  return `${pathname}${search}${hash} ${inlineDatabaseHolderPath(pathname)} ${fullPageDatabaseAtAddress(pathname) ?? ""}`;
}

// The page or database a path shows, or else its kind of screen.
function pageOf(pathname: string) {
  const route = routeInfoFromPath(pathname);
  return route.kind === "page" ? route.pageId : route.kind === "database" ? route.databaseId : null;
}
const screenOf = (pathname: string) => pageOf(pathname) ?? routeInfoFromPath(pathname).kind;
// The screens a page left for is taken down after (pageLeftAfterPaint.ts).
const LEFT_AFTER_PAINT: ReadonlySet<string> = new Set(["home", "library", "myTasks", "people", "apps", "app", "developers"]);

// Back and forward (the browser's, ⌘[ and ⌘], a tab's history menu) show the
// address they reach as a link does: from a large page past the frame,
// the sidebar's row of the page reached shown as opening first
// (lib/openPastPaint.ts; 2026-11-13: a 38,000-element page torn down in the
// traversal's own task held the key's paint 127 ms).
const HISTORY_MOVE_EVENT = "leafen:historymove";
let historyMovesWaiting = 0;
let routedPath = typeof window === "undefined" ? "/" : window.location.pathname;

function followHistoryMove() {
  const from = routedPath, to = window.location.pathname;
  routedPath = to;
  historyMovesWaiting += 1;
  const show = () => {
    historyMovesWaiting -= 1;
    window.dispatchEvent(new Event(HISTORY_MOVE_EVENT));
  };
  if (screenOf(from) === screenOf(to)) show();
  else openPastPaint(null, show, pageOf(to));
}

if (typeof window !== "undefined") {
  window.addEventListener("popstate", followHistoryMove);
  window.addEventListener(ROUTE_CHANGE_EVENT, () => { routedPath = window.location.pathname; });
}

function subscribe(callback: () => void) {
  // A traversal's own hash change shows with it.
  const hashMoved = () => { if (!historyMovesWaiting) callback(); };
  window.addEventListener(HISTORY_MOVE_EVENT, callback);
  window.addEventListener("hashchange", hashMoved);
  window.addEventListener(ROUTE_CHANGE_EVENT, callback);
  return () => {
    window.removeEventListener(HISTORY_MOVE_EVENT, callback);
    window.removeEventListener("hashchange", hashMoved);
    window.removeEventListener(ROUTE_CHANGE_EVENT, callback);
  };
}

/** What a route move carries: whether it took the place of the address. */
export type RouteMove = { replace: boolean };

function emitRouteChange(oldUrl: string, newUrl: string, replace = false) {
  window.dispatchEvent(new CustomEvent<RouteMove>(ROUTE_CHANGE_EVENT, { detail: { replace } }));
  if (oldUrl !== newUrl && new URL(oldUrl).hash !== new URL(newUrl).hash) {
    window.dispatchEvent(new HashChangeEvent("hashchange", { oldURL: oldUrl, newURL: newUrl }));
  }
}

// The pages and databases opened in this tab, newest last, kept through a
// reload: where trashing a page with none above it leads (pageTrash.ts).
const OPENED_PAGES_KEY = "leafen:opened-pages";
const OPENED_PAGES_KEPT = 50;
const openedPages: string[] = readOpenedPages();

function readOpenedPages(): string[] {
  try {
    const kept = JSON.parse(window.sessionStorage.getItem(OPENED_PAGES_KEY) ?? "[]") as unknown;
    return Array.isArray(kept) ? kept.filter((id): id is string => typeof id === "string").slice(-OPENED_PAGES_KEPT) : [];
  } catch {
    return [];
  }
}

function noteOpenedPage() {
  const route = routeInfoFromPath(window.location.pathname);
  const id = route.kind === "page" ? route.pageId : route.kind === "database" ? route.databaseId : null;
  if (!id || openedPages[openedPages.length - 1] === id) return;
  const index = openedPages.indexOf(id);
  if (index >= 0) openedPages.splice(index, 1);
  openedPages.push(id);
  if (openedPages.length > OPENED_PAGES_KEPT) openedPages.shift();
  try {
    window.sessionStorage.setItem(OPENED_PAGES_KEY, JSON.stringify(openedPages));
  } catch {
    // Without the tab's storage the pages opened are kept until a reload.
  }
}

// Whether this document has gone to another screen since it opened: by a
// link, the sidebar, search or the history's back and forward, not by an
// address it was sent on to as it started (the home's start page, a sign-in's
// return). A screen reached so was opened in the app; the one before, by its
// address (emptyPageTitleCaret.ts).
let movedInApp = false;
let shownPath = typeof window === "undefined" ? "/" : window.location.pathname;

function noteMoveInApp(event: Event) {
  const path = window.location.pathname;
  if (path === shownPath) return;
  shownPath = path;
  if (event.type === "popstate" || !(event as CustomEvent<RouteMove>).detail?.replace) movedInApp = true;
}

/** Whether the screen shown now was opened in the app rather than by the address this document opened at. */
export function openedInApp() {
  return movedInApp;
}

if (typeof window !== "undefined") {
  noteOpenedPage();
  window.addEventListener("popstate", noteOpenedPage);
  window.addEventListener(ROUTE_CHANGE_EVENT, noteOpenedPage);
  window.addEventListener("popstate", noteMoveInApp);
  window.addEventListener(ROUTE_CHANGE_EVENT, noteMoveInApp);
}


/** The pages and databases opened in this tab, newest first. */
export function openedPagesNewestFirst(): readonly string[] {
  return [...openedPages].reverse();
}

function navigate(href: string, replace = false, inPress = false) {
  const currentUrl = window.location.href;
  const nextUrl = new URL(href, currentUrl);

  // A link to the active server's page opens here, as one to this page's.
  if (nextUrl.origin !== window.location.origin && !isActiveServerOrigin(nextUrl.origin)) {
    window.location.href = nextUrl.toString();
    return;
  }

  nextUrl.pathname = pagePathForOldPath(nextUrl.pathname) ?? nextUrl.pathname;
  const nextPath = `${nextUrl.pathname}${nextUrl.search}${nextUrl.hash}`;
  const currentPath = `${window.location.pathname}${window.location.search}${window.location.hash}`;

  // The desktop app's command search window opens what its search chose in
  // the app's window in front (quickSearchWindow.ts).
  if (forwardFromQuickSearch(nextPath, routeInfoFromPath(nextUrl.pathname).kind)) return;

  // A pinned tab of the Mac app keeps its page: another one it would go to opens in a new
  // tab after it, shown, as Notion's pinned tabs do; a settings address opens over its page.
  if (!replace && desktopTabPinned() && leavesPinnedPage(new URL(currentUrl), nextUrl) && !opensSettingsOver(nextPath)) {
    openInNewDesktopTab(nextPath, true);
    return;
  }

  // A link going here takes the windows over the page away; one to a settings address opens the settings over it instead (productWindows.ts).
  if (!replace && closeWindowsForLink(nextPath)) return;
  const go = () => {
    const fromUrl = window.location.href;
    if (nextPath !== `${window.location.pathname}${window.location.search}${window.location.hash}`) {
      if (replace) window.history.replaceState(window.history.state, "", nextPath);
      else window.history.pushState(window.history.state, "", nextPath);
    }
    emitRouteChange(fromUrl, window.location.href, replace);
  };
  // Another page or screen from a press — a link in the body, a breadcrumb,
  // a menu's "open" — shows the press first when the page left is large; a
  // page left for a screen of another kind goes after that screen is painted
  // (pageLeftAfterPaint.ts).
  if (screenOf(nextUrl.pathname) === screenOf(window.location.pathname) || nextPath === currentPath) go();
  else if (pageOf(window.location.pathname) && LEFT_AFTER_PAINT.has(routeInfoFromPath(nextUrl.pathname).kind)) {
    if (inPress) { holdLeavingPage(); go(); }
    else openPressedPastPaint(() => { holdLeavingPage(); go(); }, null);
  } else if (inPress) go();
  else openPressedPastPaint(go, pageOf(nextUrl.pathname));
}

/**
 * A link in a window over the screen that closes it — the settings' "사람",
 * "개발자 도구 열기" — goes in its press, with the window, in one frame: past
 * the press's frame the page under the window showed alone a frame between
 * them, and the two frames each worked out the page area's styles (some
 * 60 ms more to the screen at CPU ×2, 2026-11-15). A page left for another
 * kind of screen still goes after that screen is painted (pageLeftAfterPaint.ts).
 */
export function pushRouteClosingWindow(href: string) {
  if (href === "#") return;
  navigate(href, false, true);
}

/**
 * Whether going there leaves a pinned tab's page, by Notion's rule (its web
 * client's pinned-tab check, read 2026-11-10): another kind of screen, or
 * another page — but not opening a peek, nor its own page again.
 */
function leavesPinnedPage(current: URL, next: URL): boolean {
  const from = routeInfoFromPath(current.pathname), to = routeInfoFromPath(next.pathname);
  if (from.kind !== to.kind) return true;
  const id = (route: RouteInfo) => route.kind === "page" ? route.pageId : route.kind === "database" ? route.databaseId : null;
  if (id(from) === null) return false;
  return id(from) !== id(to) && !next.searchParams.has("p");
}

export function pushRoute(href: string) {
  // A bare "#" stays put: a view link names the pages its page links to
  // without opening them (ViewLinkView), and going there would drop the
  // link's token from the address (2026-11-07: a child page's line blanked
  // the view).
  if (href === "#") return;
  navigate(href);
}

/**
 * A page or database link inside the signed-in app goes there in the app, as
 * Notion's: a plain left click on a same-address `<a href="/p/…">` (or home's) that
 * nothing else handled moves the route instead of loading the whole app again
 * (a home card or an import's "첫 페이지 열기" did, 2026-11-05). A modified or
 * middle click, a new tab target or a download keeps the browser's own way.
 */
export function useInAppLinks() {
  useEffect(() => {
    const follow = (event: MouseEvent) => {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const link = event.target instanceof Element ? event.target.closest<HTMLAnchorElement>("a[href]") : null;
      if (!link || (link.target && link.target !== "_self") || link.hasAttribute("download")) return;
      const url = new URL(link.href, window.location.href);
      if (url.origin !== window.location.origin) return;
      const kind = routeInfoFromPath(url.pathname).kind;
      // Home's own address too, which a home task opens over (HomeMyTasks).
      if (kind !== "page" && kind !== "database" && url.pathname !== HOME_PATH) return;
      event.preventDefault();
      navigate(url.pathname + url.search + url.hash);
    };
    document.addEventListener("click", follow);
    return () => document.removeEventListener("click", follow);
  }, []);
}

export function replaceRoute(href: string) {
  navigate(href, true);
}

function useLocationKey() {
  return useSyncExternalStore(subscribe, getLocationKey, () => "/");
}

export function usePathname() {
  useLocationKey();
  if (typeof window === "undefined") return "/";
  return window.location.pathname;
}

export function useSearchParams() {
  const locationKey = useLocationKey();
  return useMemo(
    () => {
      if (typeof window === "undefined") return new URLSearchParams();
      return new URLSearchParams(window.location.search);
    },
    // `locationKey` isn't read inside the callback, but the callback reads the
    // live `window.location.search`, which the linter can't track. Keeping
    // `locationKey` as a dep is exactly what forces the params to recompute on
    // navigation — removing it would return stale params.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [locationKey],
  );
}

export function useRouter() {
  return useMemo(
    () => ({
      push: (href: string) => navigate(href),
      replace: (href: string) => navigate(href, true),
      back: () => window.history.back(),
      forward: () => window.history.forward(),
    }),
    []
  );
}
