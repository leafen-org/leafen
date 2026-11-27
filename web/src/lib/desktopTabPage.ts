import { useSyncExternalStore } from "react";
import { hasDesktopTabs, onDesktopWindowEvent } from "./desktopMenuCommands";

// What the Mac or Windows app's shown tab draws for its window's strip of tabs
// (desktop/window_tabs_page.go, window_tabs_strip_menus.go), as Notion's app
// has its page draw them (7.42.3 and 7.44.1, measured 2026-11-10 and
// 2026-11-24): the back and forward buttons' history menu, the card of a tab
// the pointer rests on, and while a menu the strip draws stands over it, that
// it does — its presses close it — and a group's menu's icon picker. A pinned
// tab's page also learns it is pinned: the pages it goes to open in new tabs
// (router.ts).

export type TabHistoryEntry = { offset: number; path: string; title: string };
export type TabHistoryMenu = { forward: boolean; entries: TabHistoryEntry[] };
export type TabPreview = { path: string; title: string; icon: string; left: number; right: number };
/** A group of the window's tabs as its menus show it (desktop/window_tabs_groups.go). */
export type StripGroup = { id: string; name: string; color: string; icon: string };
/** A group's icon picker under its menu's icon (at, from the strip's top left; strip, the strip's height). */
export type StripIconPicker = {
  kind: "icon";
  at: { left: number; right: number; top: number; bottom: number };
  strip: number;
  rtl: boolean;
  group: StripGroup;
};
/** What the strip has over the page: a menu of it (through: a press on the page goes on there, as outside a tab's menu), or a group's menu with its icon picker. */
export type StripCover = { kind: "open"; through?: boolean } | StripIconPicker;
/** The strip's menus' boxes, from the strip's top left, whose shadow the page casts under them (Windows; strip, the strip's height). */
export type StripShadows = { boxes: { left: number; top: number; width: number; height: number }[]; strip: number };

type TabPageState = {
  history: TabHistoryMenu | null;
  preview: TabPreview | null;
  menu: StripCover | null;
  shadows: StripShadows | null;
};

// The history menu lists the ten entries nearest the shown one, either way,
// as Notion's (its web client's getHistoryEntries, read 2026-11-13; seen
// 2026-11-12: 18 pages gone through listed the newest ten).
const HISTORY_MENU_ENTRIES = 10;

let state: TabPageState = { history: null, preview: null, menu: null, shadows: null };
const listeners = new Set<() => void>();
let pinned = false;
let started = false;

function update(change: Partial<TabPageState>) {
  state = { ...state, ...change };
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  startDesktopTabPage();
  return () => listeners.delete(listener);
}

/** What the shown tab draws for the strip now. */
export function useDesktopTabPage(): TabPageState {
  return useSyncExternalStore(subscribe, () => state, () => state);
}

/** Whether this page's tab is pinned: the pages it goes to open in new tabs, as Notion's. */
export function desktopTabPinned(): boolean {
  return pinned;
}

/** Takes the pinned state the app answers a report with (desktopTabs.ts). */
export function setDesktopTabPinned(value: unknown): void {
  if (typeof value === "boolean") pinned = value;
}

/** Starts hearing what the app asks the shown tab to draw, once (desktopTabs.ts). */
export function startDesktopTabPage(): void {
  if (started || !hasDesktopTabs()) return;
  started = true;
  // Each names the window it is for (onDesktopWindowEvent).
  const on = (event: string, take: (data: Record<string, unknown>) => void) =>
    onDesktopWindowEvent(event, (data) => { if (typeof data === "object" && data !== null) take(data as Record<string, unknown>); });
  on("leafen:tab-history", (data) => {
    const forward = Boolean(data.forward);
    // Where the app has no list of the page's history (Windows), the page
    // lists its own; a ⌘- or ⌃-pressed arrow opens the nearest in a new tab.
    const listed = data.own ? ownHistory(forward) : (data.entries as TabHistoryEntry[]) ?? [];
    // Back goes no further than the tab keeps its history (desktopTabBackReach).
    const reach = desktopTabBackReach();
    const entries = forward ? listed : listed.filter((entry) => -entry.offset <= reach);
    if (data.open) {
      if (entries[0]) openInNewDesktopTab(entries[0].path, false);
      return;
    }
    if (entries.length) update({ history: { forward, entries: entries.slice(0, HISTORY_MENU_ENTRIES) } });
  });
  on("leafen:tab-preview", (data) => {
    const preview = data as unknown as TabPreview;
    update({ preview: preview.path ? preview : null });
  });
  on("leafen:strip-menu", (data) => {
    const kind = data.kind;
    const menu: StripCover | null = kind === "icon" ? data as unknown as StripIconPicker : kind === "open" ? { kind: "open", through: Boolean(data.through) } : null;
    update(menu ? { menu } : { menu, shadows: null });
  });
  on("leafen:strip-shadows", (data) => {
    if (Array.isArray(data.boxes) && typeof data.strip === "number") update({ shadows: { boxes: data.boxes as StripShadows["boxes"], strip: data.strip } });
  });
  on("leafen:tab-pinned", (data) => { pinned = Boolean(data.pinned); });
}

// ---- How far back a tab's history goes ----
//
// A tab keeps its last 20 entries, as Notion's app's does (its web client's
// electron router store: pushEntry drops the oldest past 20, read
// 2026-11-14), so its back goes at most 19 entries — the strip's button,
// the menu's and the page's own (desktopTabs.ts) and the history menu.
// Each entry the page goes to carries its place in the tab's history, and
// the tab notes the oldest place it keeps.

const TAB_ENTRIES = 20;
const PLACE_KEY = "leafenTabEntry";
const OLDEST_KEY = "leafen-tab-history-oldest";

function placeOf(state: unknown): number | null {
  const place = state && typeof state === "object" ? (state as Record<string, unknown>)[PLACE_KEY] : undefined;
  return typeof place === "number" && Number.isInteger(place) && place >= 0 ? place : null;
}

/** The place of the entry shown in the tab's history: an entry without one is the newest. */
function placeHere(): number {
  return placeOf(window.history.state) ?? Math.max(0, window.history.length - 1);
}

function oldestKept(): number {
  try {
    const oldest = Number(sessionStorage.getItem(OLDEST_KEY));
    return Number.isInteger(oldest) && oldest > 0 ? oldest : 0;
  } catch {
    return 0;
  }
}

function keepFrom(place: number): void {
  if (place - (TAB_ENTRIES - 1) <= oldestKept()) return;
  try {
    sessionStorage.setItem(OLDEST_KEY, String(place - (TAB_ENTRIES - 1)));
  } catch {
    // Without storage the tab goes back as far as its web view's history.
  }
}

function withPlace(state: unknown, place: number): unknown {
  if (state === null || state === undefined) return { [PLACE_KEY]: place };
  if (typeof state !== "object" || Array.isArray(state) || Object.getPrototypeOf(state) !== Object.prototype) return state;
  return { ...(state as Record<string, unknown>), [PLACE_KEY]: place };
}

/** The state an entry the page goes to (push) or puts in place of the shown one carries: its place in the tab's history (desktopTabs.ts). */
export function desktopTabEntryState(state: unknown, push: boolean): unknown {
  const place = push ? placeHere() + 1 : placeHere();
  if (push) keepFrom(place);
  return withPlace(state, place);
}

/** The shown entry's place, given it as the tab starts (desktopTabs.ts). */
export function desktopTabEntryPlace(): unknown {
  const place = placeHere();
  keepFrom(place);
  return withPlace(window.history.state, place);
}

/** How many entries back the tab's history goes from the shown one. */
export function desktopTabBackReach(): number {
  return Math.max(0, placeHere() - oldestKept());
}

// ---- The page's own history (Windows) ----
//
// WebView2 lists no history to the app: the page lists its entries itself
// with the browser's Navigation API, each with the title it had there.

type HistoryNavigation = {
  currentEntry: { key: string; index: number } | null;
  entries(): { key: string; index: number; url: string | null }[];
};

const TITLES_KEY = "leafen-tab-history-titles";
const TITLES_KEPT = 200;

function navigationApi(): HistoryNavigation | null {
  return (window as unknown as { navigation?: HistoryNavigation }).navigation ?? null;
}

function readTitles(): Record<string, string> {
  try {
    return JSON.parse(sessionStorage.getItem(TITLES_KEY) ?? "{}") as Record<string, string>;
  } catch {
    return {};
  }
}

/** Keeps the title the page has now for its history entry, to list it later (desktopTabs.ts). */
export function noteDesktopTabTitle(title: string): void {
  const entry = navigationApi()?.currentEntry;
  if (!entry || !title) return;
  const titles = readTitles();
  if (titles[entry.key] === title) return;
  delete titles[entry.key];
  titles[entry.key] = title;
  const keys = Object.keys(titles);
  for (const key of keys.slice(0, Math.max(0, keys.length - TITLES_KEPT))) delete titles[key];
  try {
    sessionStorage.setItem(TITLES_KEY, JSON.stringify(titles));
  } catch {
    // Without storage the entries are listed without their titles.
  }
}

/** The page's history one way: back from the newest entry, or forward from the nearest — the app's pages only. */
function ownHistory(forward: boolean): TabHistoryEntry[] {
  const navigation = navigationApi();
  const current = navigation?.currentEntry;
  if (!navigation || !current) return [];
  const titles = readTitles();
  const entries = navigation.entries().filter((entry) => (forward ? entry.index > current.index : entry.index < current.index));
  if (!forward) entries.reverse();
  return entries.flatMap((entry) => {
    if (!entry.url) return [];
    const url = new URL(entry.url);
    if (url.origin !== window.location.origin) return [];
    return [{ offset: entry.index - current.index, path: `${url.pathname}${url.search}${url.hash}`, title: titles[entry.key] ?? "" }];
  });
}

/** Closes the history menu; its button on the strip takes the keys back, as Notion's (desktop/window_tabs_strip_keys.go). */
export function closeTabHistoryMenu(): void {
  const shown = state.history;
  update({ history: null });
  if (shown) tellStripMenu({ do: "historyClosed", forward: shown.forward });
}

/** A place of the page, from its top left. */
type PageRect = { left: number; top: number; right: number; bottom: number };

/**
 * Tells the app what the page under a strip menu said (WindowTabs.StripMenu):
 * that it was pressed, or of a group's icon picker an icon picked, where it
 * stands, or that it closed, which closes it here at once; or that the
 * history menu closed.
 */
export function tellStripMenu(message: { do: "pressed" | "icon" | "picker" | "pickerClosed" | "historyClosed"; group?: string; icon?: string; rect?: PageRect; forward?: boolean }): void {
  if (message.do === "pickerClosed") update({ menu: { kind: "open" } });
  void import("@wailsio/runtime")
    .then(({ Call }) => Call.ByName("main.WindowTabs.StripMenu", message))
    .catch(() => undefined);
}

/** Opens one of the app's pages in a new tab after this one's, in front or behind. */
export function openInNewDesktopTab(path: string, active: boolean): void {
  const address = new URL(path, window.location.href).href;
  void import("@wailsio/runtime")
    .then(({ Call }) => Call.ByName("main.WindowOpener.Open", address, active, false))
    .catch(() => undefined);
}
