import { hasDesktopTabs, onDesktopWindowEvent, packagedWindows } from "./desktopMenuCommands";
import { desktopTabBackReach, desktopTabEntryPlace, desktopTabEntryState, noteDesktopTabTitle, setDesktopTabPinned, startDesktopTabPage } from "./desktopTabPage";
import { loadSession } from "./authSessionStore";
import { fullPageDatabasePage } from "./inlineDatabaseAddress";
import { pageHref } from "./navigation";
import { requestPageTitleFocus } from "./pageTitleFocus";
import { serverUrl } from "./serverOrigin";
import { tabSignIn, watchTabSignIn } from "./tabSignIn";
import { printingInLight } from "./theme";
import { TAB_GROUP_COLORS, tabGroupLooks, type TabGroupLook } from "./tabGroupColors";
import { watchPageShade } from "./desktopTabShade";
import { useSyncExternalStore } from "react";
import { i18next } from "@/i18n";

// The Mac and Windows apps' windows hold tabs, as Notion's app's do (desktop/window_tabs.go):
// every page of the app there is one tab, a whole page of its own, under the
// window's strip of tabs. The page tells the app what the strip shows of it —
// its address, name and icon, its sidebar, the inbox's unread count and the
// colors it draws with — and asks for new tabs: a ⌘-pressed page behind the
// shown one (in front with ⇧), and the page a search for a new tab found
// (⌘T, the strip's "+") in front at the end.

/** Whether this page is a tab of the Mac or Windows app's window: every page of the app there is. */
export function inDesktopTab(): boolean {
  return hasDesktopTabs();
}

type TabReport = {
  path: string;
  title: string;
  /** The page whose own name and icon title and icon are, or "" (a screen loading, home). */
  page: string;
  icon: string;
  search: boolean;
  sidebar?: { shown: boolean; expanded: boolean; width: number; inbox: string; new: string };
  unread?: number;
  look?: { colors: Record<string, string>; dark: boolean; rtl: boolean; groups: Record<string, TabGroupLook>; words: Record<string, string> };
  /** The first screen is drawn whole: a tab opened behind may rest (desktop/window_tabs_start.go). */
  drawn?: boolean;
  /** How many entries back the tab's history goes (desktopTabBackReach). */
  back?: number;
  /** The sign-in it shows, which the app keeps with the tab (tabSignIn.ts). */
  signIn?: string;
  /** The shade a window over the page lays on it, "" with none: the strip takes it too (desktopTabShade.ts). */
  shade?: string;
};

const report: TabReport = { path: "", title: "", page: "", icon: "", search: false };
let told = "";
let telling: number | undefined;
// The signed-in app names its tab once its screen has loaded; till then the
// strip shows a tab's empty name, as Notion's does (2026-11-10: a tab opened
// behind showed the window's "Leafen" for a second or two).
let namesItself = false;
let searchers = 0;

function tell() {
  if (!inDesktopTab() || telling !== undefined) return;
  telling = window.setTimeout(() => {
    telling = undefined;
    report.path = `${window.location.pathname}${window.location.search}${window.location.hash}`;
    report.search = searchers > 0;
    report.back = desktopTabBackReach();
    const signIn = tabSignIn();
    report.signIn = signIn ? JSON.stringify(signIn) : undefined;
    // A screen that names nothing itself (the first run, signing in) is
    // named by the window's title once it is drawn: till then — the app
    // still reading its sign-in — the tab keeps the name it has, as Notion's
    // while its page loads.
    const title = report.title || (namesItself || !report.drawn ? "" : document.title);
    const text = JSON.stringify({ ...report, title });
    noteDesktopTabTitle(title);
    if (text === told) return;
    told = text;
    void import("@wailsio/runtime")
      .then(({ Call }) => Call.ByName("main.WindowTabs.Report", JSON.parse(text)))
      // The app answers whether the tab is pinned (desktopTabPage.ts).
      .then(setDesktopTabPinned)
      .catch(() => undefined);
  }, 50);
}

/** The signed-in app will name its tab itself: till it does, the tab's name is empty. */
export function awaitDesktopTabName(): void {
  if (namesItself) return;
  namesItself = true;
  tell();
}

/** The screen's name and icon for its tab — a page's own, by its id —, which names its window too while it shows. */
export function reportDesktopTabChrome(title: string, icon: string, page: string): void {
  report.title = title;
  report.icon = icon;
  report.page = page;
  tell();
}

/**
 * Tells the app once that the tab's first screen is drawn whole, as Notion's
 * page says its first render, so a tab opened behind may rest
 * (desktop/window_tabs_start.go): the first frame, after the page drew
 * anything, that two frames in a row show no loading screen — a page's list
 * and body once FirstScreenCover lifts, and as well the first run's steps,
 * the sign-in, an error or anything else a tab starts on (2026-11-13: only
 * a page said so, and a tab opened behind during the first run stayed in
 * view its whole minute).
 */
function reportFirstScreenDrawn(): void {
  const root = document.getElementById("root");
  if (!root) return;
  let frame = 0;
  let clear = 0;
  const check = () => {
    frame = 0;
    const drawn = root.childElementCount > 0 && !document.querySelector("[data-loading-screen]");
    clear = drawn ? clear + 1 : 0;
    if (clear < 2) {
      if (drawn) frame = window.requestAnimationFrame(check);
      return;
    }
    watching.disconnect();
    report.drawn = true;
    tell();
  };
  const watching = new MutationObserver(() => { if (!frame) frame = window.requestAnimationFrame(check); });
  watching.observe(document.body, { childList: true, subtree: true });
  frame = window.requestAnimationFrame(check);
}

/**
 * A step of the first run passed in this tab, which the app's tabs behind it
 * follow (lib/followAcrossTabs.ts): the app keeps their pages in view a
 * moment (desktop/window_tabs_start.go), so they go on as they hear it.
 * Hidden, a page's timers and its calls to the app waited seconds at a
 * time, and a tab behind followed 1–16 s after this one (2026-11-14). The
 * app takes it from the shown tab only. Asked before the step is told.
 */
export function wakeTabsBehind(): void {
  if (!inDesktopTab()) return;
  void import("@wailsio/runtime")
    .then(({ Call }) => Call.ByName("main.WindowTabs.WakeBehind"))
    .catch(() => undefined);
}

/** The inbox's unread count, shown over the strip's sidebar button while the sidebar is folded. */
export function reportDesktopTabUnread(count: number): void {
  if (report.unread === count) return;
  report.unread = count;
  tell();
}

// ---- A search for a new tab ----

let newTabSearch = false;
const newTabSearchWatchers = new Set<() => void>();
// What a search for a new tab picked opens in front at the end, though the
// search has closed by then: at once, or once the server has named it (a
// body's line) — within this long. A new page it makes opens so by itself
// (openNewPageInNewDesktopTab).
const PICKED_WAIT_MS = 15_000;
let pickedUntil = 0;

/** Whether the search open now is one for a new tab: what it finds opens in a new tab. */
export function searchingForNewTab(): boolean {
  return newTabSearch;
}

/** Starts or ends a search for a new tab (useAuthenticatedProductShortcuts, SearchDialog). */
export function setSearchingForNewTab(on: boolean): void {
  if (newTabSearch === on) return;
  newTabSearch = on;
  for (const watcher of newTabSearchWatchers) watcher();
}

/** Whether the search open now is one for a new tab, drawn again as that changes. */
export function useSearchingForNewTab(): boolean {
  return useSyncExternalStore((change) => {
    newTabSearchWatchers.add(change);
    return () => { newTabSearchWatchers.delete(change); };
  }, searchingForNewTab);
}

/** A search for a new tab picked what it opens next: that tab comes in front at the end (Notion's ⌘T). */
export function pickedForNewTab(): void {
  if (newTabSearch) pickedUntil = Date.now() + PICKED_WAIT_MS;
}

/** Says the page can search for a new tab while it listens; returns the way to stop. */
export function listenForNewTabSearch(open: () => void): () => void {
  if (!inDesktopTab()) return () => undefined;
  searchers++;
  tell();
  const stop = onDesktopWindowEvent("leafen:new-tab-search", () => {
    setSearchingForNewTab(true);
    open();
  });
  return () => {
    stop();
    searchers--;
    tell();
  };
}

/** Calls `copy` with the link, page and name of another tab of this window
 * whose page hasn't opened since the app started, when the tab menu asks
 * for its link (desktop/window_tabs_menu.go); returns the way to stop. */
export function onCopyTabLink(copy: (url: string, pageId: string, title: string) => void): () => void {
  if (!inDesktopTab()) return () => undefined;
  return onDesktopWindowEvent("leafen:copy-tab-link", (data) => {
    const asked = data as { window?: string; path?: string; title?: string } | null;
    if (!asked?.window || typeof asked.path !== "string") return;
    const page = /^\/p\/([^/?#]+)/.exec(asked.path);
    if (!page) return;
    // A full-page database's address names its page.
    const id = decodeURIComponent(page[1]);
    copy(serverUrl(asked.path), fullPageDatabasePage(id) ?? id, asked.title ?? "");
  });
}

// ---- A page made in a new tab ----

// A page just made, opened in a new tab, says so in its address: its tab's
// page takes the word away as it starts and gives the page's title the caret
// (Notion's app opens it so, isNewPage), its history one entry long.
const NEW_PAGE_WORD = "new";

/** Opens a page just made in a new tab at the end, in front, with the caret in its title (Notion's app's "새로" and ⌘N,
 * and search for a new tab's "새 페이지"). */
export function openNewPageInNewDesktopTab(pageId: string): void {
  const address = new URL(`${pageHref(pageId)}?${NEW_PAGE_WORD}=page`, window.location.href).href;
  void import("@wailsio/runtime")
    .then(({ Call }) => Call.ByName("main.WindowOpener.Open", address, true, true))
    .catch(() => undefined);
}

function takeNewPageWord() {
  const url = new URL(window.location.href);
  if (url.searchParams.get(NEW_PAGE_WORD) !== "page") return;
  url.searchParams.delete(NEW_PAGE_WORD);
  history.replaceState(history.state, "", `${url.pathname}${url.search}${url.hash}`);
  const page = /^\/p\/([^/?#]+)/.exec(url.pathname);
  if (page) requestPageTitleFocus(decodeURIComponent(page[1]));
}

// ---- Where a page asked for opens ----

let lastPress = { shift: false, at: 0 };

/** How a page this page asks for opens: in front and at the end for a search
 * for a new tab; else behind the shown tab, in front when ⇧ was held with
 * the pointer's press (⌘⇧-click). A key's ⇧ is its shortcut's own: ⌘⇧↵
 * "새 탭에서 열기" opens behind, as Notion's app's (seen 2026-11-16). */
export function newTabPlace(): { active: boolean; atEnd: boolean } {
  if (newTabSearch || Date.now() < pickedUntil) {
    pickedUntil = 0;
    return { active: true, atEnd: true };
  }
  const recent = Date.now() - lastPress.at < 1500;
  return { active: recent && lastPress.shift, atEnd: false };
}

// ---- What the page shows ----

// The strip's colors (desktop/tab_strip.html), as Notion's tab bar takes
// them: the sidebar's head on the sidebar's ground with its edge, the tab
// bar on the page's ground with a borSecTra line under it, a shown or
// pointed tab and a pointed button on bacIntTra, as the page's own.
const LOOK = {
  bar: "--bg-sidebar",
  page: "--bg-app",
  border: "--border-secondary",
  line: "--border-subtle",
  tab: "--bg-tab-selected",
  text: "--text-default",
  textSecondary: "--text-secondary",
  icon: "--icon-secondary",
  iconDim: "--icon-tertiary",
  press: "--bg-hover",
  // The tab's and the group's menus the strip draws (desktop/tab_strip_menu.js,
  // tab_strip_group_menu.js) as the page draws its menus (RowMenu.module.css)
  // and its name fields and buttons.
  menuGround: "--bg-elevated",
  menuShadow: "--shadow-menu",
  menuRadius: "--radius-menu",
  menuDivider: "--menu-divider",
  menuDividerMargin: "--menu-divider-margin",
  menuDanger: "--menu-danger",
  menuIcon: "--icon-primary",
  menuChevron: "--icon-secondary",
  rowHeight: "--menu-row-height",
  rowPadding: "--menu-row-padding",
  rowRadius: "--menu-row-radius",
  rowGap: "--menu-row-gap",
  rowSpace: "--menu-row-space",
  rowFont: "--menu-row-font-size",
  rowLine: "--menu-row-line-height",
  fieldGround: "--name-field-bg",
  fieldRing: "--name-field-ring",
  fieldRingFocus: "--name-field-ring-focus",
  placeholder: "--text-placeholder",
  buttonOutline: "--button-outline",
  // A group menu's icon under the pointer or the keys (Notion's outline
  // button's hovered ground), and a color's name tip's shadow.
  buttonHover: "--bg-secondary",
  tipShadow: "--shadow-tip",
  // What a closing menu of the strip gives the keys back to is ringed as
  // the page's keyboard ring (desktop/tab_strip_ring.js).
  keyRing: "--keyboard-focus-ring",
} as const;

// The strip's menus' words in the page's language (desktopTabs:menu, :groups).
const MENU_WORDS = ["menu.label", "menu.copyLink", "menu.duplicate", "menu.pin", "menu.unpin", "menu.moveTo", "menu.close",
  "menu.closeOthers", "menu.closeLeft", "menu.closeRight", "menu.reopen", "groups.untitled", "groups.removeFromGroup",
  "groups.newGroup", "groups.newWindow", "groups.menu", "groups.name", "groups.namePlaceholder", "groups.changeIcon",
  "groups.ungroup", "groups.closeGroup", ...TAB_GROUP_COLORS.map((color) => `groups.colors.${color}` as const)] as const;

function readLook() {
  const style = getComputedStyle(document.documentElement);
  const colors: Record<string, string> = {};
  for (const [key, name] of Object.entries(LOOK)) {
    const value = style.getPropertyValue(name).trim();
    if (value) colors[key] = value;
  }
  const dark = document.documentElement.dataset.theme === "dark";
  const words: Record<string, string> = {};
  for (const key of MENU_WORDS) words[key] = i18next.t(`desktopTabs:${key}`);
  report.look = { colors, dark, rtl: document.dir === "rtl", groups: tabGroupLooks(), words };
}

// The desktop sidebar as it takes room: shown or not (signing in has none),
// open or folded, and its width; and its head's inbox and "새로", which the
// strip draws over it (SidebarTopActions.tsx): "" for none, else "on",
// "open" or "off", and "ask" for a guest's "새로" that asks to become a member.
function readSidebar() {
  const slot = document.querySelector<HTMLElement>("[data-sidebar-slot]");
  if (!slot) {
    report.sidebar = { shown: false, expanded: false, width: 0, inbox: "", new: "" };
    return;
  }
  const expanded = slot.dataset.collapsed !== "true";
  const head = slot.querySelector<HTMLElement>("[data-sidebar-top-actions][data-in-strip]");
  const inbox = head?.querySelector<HTMLButtonElement>("[data-sidebar-top-row='inbox']");
  const make = head?.querySelector<HTMLButtonElement>("[data-sidebar-new-page]");
  report.sidebar = {
    shown: true, expanded, width: expanded ? Math.round(slot.getBoundingClientRect().width) : 0,
    inbox: !inbox ? "" : inbox.disabled ? "off" : inbox.dataset.active === "true" ? "open" : "on",
    new: !make ? "" : make.disabled ? "off" : make.dataset.asks === "true" ? "ask" : make.getAttribute("aria-expanded") === "true" ? "open" : "on",
  };
}

/** Tells the strip again what the sidebar's head shows, as it changes (SidebarTopActions). */
export function retellDesktopSidebar(): void {
  if (!inDesktopTab()) return;
  readSidebar();
  tell();
}

/** Starts telling the app what this tab shows, once, as the page starts (packagedClient.ts). */
export function startDesktopTabReports(): void {
  if (!inDesktopTab()) return;
  takeNewPageWord();
  startDesktopTabPage();
  // Signed in, the app names the tab from its first screen on.
  namesItself = loadSession() !== null;
  reportFirstScreenDrawn();
  const pressed = (event: MouseEvent) => { lastPress = { shift: event.shiftKey, at: Date.now() }; };
  const keyed = () => { lastPress = { shift: false, at: Date.now() }; };
  window.addEventListener("pointerdown", pressed, true);
  window.addEventListener("click", pressed, true);
  window.addEventListener("keydown", keyed, true);
  const look = () => {
    // A page drawn light for printing keeps the tab strip as it was.
    if (printingInLight()) return;
    readLook();
    readSidebar();
    tell();
  };
  look();
  // The app keeps the sign-in the tab shows with it, to start in again.
  watchTabSignIn(tell);
  watchPageShade((shade) => { report.shade = shade; tell(); });
  // The theme, the contrast and the language's direction are the root's.
  new MutationObserver(look).observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme", "class", "style", "dir", "lang", "data-contrast"] });
  // The sidebar comes and goes with the screen (looked for each second),
  // folds and is dragged wider.
  let watched: Element | null = null;
  const resize = new ResizeObserver(look);
  const sidebar = () => {
    const slot = document.querySelector("[data-sidebar-slot]");
    if (slot !== watched) {
      if (watched) resize.unobserve(watched);
      if (slot) resize.observe(slot);
      watched = slot;
    }
    const before = JSON.stringify(report.sidebar);
    readSidebar();
    if (JSON.stringify(report.sidebar) !== before) tell();
  };
  new MutationObserver(sidebar).observe(document.body, { subtree: true, attributes: true, attributeFilter: ["data-collapsed"] });
  window.setInterval(sidebar, 1000);
  // The address changes as the page moves on in its tab; each entry carries
  // its place in the tab's history, which keeps the last 20 (desktopTabPage.ts).
  const replace = history.replaceState.bind(history);
  replace(desktopTabEntryPlace(), "");
  for (const name of ["pushState", "replaceState"] as const) {
    const own = history[name].bind(history);
    history[name] = (state: unknown, unused: string, url?: string | URL | null) => {
      own(desktopTabEntryState(state, name === "pushState"), unused, url);
      tell();
    };
  }
  // The page's own back (⌘[, the menu's, a peek closing) goes no further.
  const go = history.go.bind(history);
  history.go = (delta?: number) => {
    if (delta && delta < 0 && -delta > desktopTabBackReach()) return;
    go(delta);
  };
  history.back = () => history.go(-1);
  window.addEventListener("popstate", tell);
  window.addEventListener("hashchange", tell);
  if (packagedWindows()) window.addEventListener("keydown", zoomKeys);
}

// ⌃= and ⌃- zoom the pages, as the Windows app's menu keys (desktop/app_menu_windows.go),
// which the app's own keys can't name; the page answers first.
function zoomKeys(event: KeyboardEvent) {
  if (event.defaultPrevented || !event.ctrlKey || event.altKey || event.metaKey) return;
  const change = event.code === "Equal" || event.code === "NumpadAdd" ? 1 : event.code === "Minus" || event.code === "NumpadSubtract" ? -1 : 0;
  if (!change) return;
  event.preventDefault();
  void import("@wailsio/runtime").then(({ Call }) => Call.ByName("main.WindowTabs.Zoom", change)).catch(() => undefined);
}
