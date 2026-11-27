import { useEffect, useRef, useSyncExternalStore } from "react";
import type { ImportDestination, ImportSource } from "@/components/import/ImportDialog";
import type { SettingsWindowState } from "@/components/settings/settingsWindowModel";
import { onRestoredQuestionShown, restoredQuestionShown } from "./dataEpoch";
import { searchIsOpen, subscribeSearchOpen } from "./searchOpen";

// The windows over the product besides search — settings, an import, the
// shortcuts list, a page's database picker, a new workspace, a teamspace's
// window, the trash — kept open here and not in the product's own state, as
// search is (searchOpen.ts): held by the product, opening or closing one
// redrew the whole product — the sidebar, the page, its bar — on the press's
// frame (about 165 ms of a 44,000-page workspace's search press under the NAS
// condition, 2026-11-14). Each window draws from its own store; what holds
// back while one is open — ⌘N's menu, ⌘/, the first run's steps — asks
// windowOverProduct as a key comes, or follows useWindowOverProduct.

export type ProductWindowStore<T> = {
  /** What the window shows now, null while it is closed. */
  get(): T | null;
  set(next: T | null): void;
  subscribe(listener: () => void): () => void;
};

const stores: ProductWindowStore<unknown>[] = [];
// The windows that hold back ⌘N, ⌘/ and the first run's steps (the trash does not, as before).
const holding = new Set<ProductWindowStore<unknown>>();
const anyListeners = new Set<() => void>();

function productWindowStore<T>({ holdsBack = true } = {}): ProductWindowStore<T> {
  let value: T | null = null;
  const listeners = new Set<() => void>();
  const store: ProductWindowStore<T> = {
    get: () => value,
    set(next) {
      if (Object.is(value, next)) return;
      value = next;
      for (const listener of [...listeners]) listener();
      for (const listener of [...anyListeners]) listener();
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
  };
  stores.push(store as ProductWindowStore<unknown>);
  if (holdsBack) holding.add(store as ProductWindowStore<unknown>);
  return store;
}

/** The settings window's state (settings/useSettingsWindow.ts). */
export const settingsWindowStore = productWindowStore<SettingsWindowState>();
/** An import's window: where it imports from and to. */
export const importWindowStore = productWindowStore<{ source: ImportSource; destination: ImportDestination | null }>();
/** The keyboard shortcuts list (⌘/). */
export const shortcutsWindowStore = productWindowStore<true>();
/** The database picker a page's body opens. */
export const databasePickerStore = productWindowStore<true>();
/** The sidebar's new workspace window. */
export const workspaceCreateStore = productWindowStore<true>();
/** The sidebar's teamspace window. */
export const teamspaceWindowStore = productWindowStore<true>();
/** The trash over the page, with what it opens searching for. */
export const trashPanelStore = productWindowStore<{ query: string }>({ holdsBack: false });

/**
 * Whether a window that holds the product's keys back is over it now, search
 * too, for a key or a press to ask as it comes. The question about a restored
 * server's copies (ServerRestoredDialog) is one: the first run's steps opened
 * over it shut it from the pointer and the keys while it stayed drawn on top,
 * its buttons pressing nothing (2026-11-22, a restore whose owner had steps
 * left).
 */
export function windowOverProduct() {
  if (searchIsOpen() || restoredQuestionShown()) return true;
  for (const store of holding) if (store.get() !== null) return true;
  return false;
}

function subscribeAny(listener: () => void) {
  anyListeners.add(listener);
  const leaveSearch = subscribeSearchOpen(listener);
  const leaveRestored = onRestoredQuestionShown(listener);
  return () => { anyListeners.delete(listener); leaveSearch(); leaveRestored(); };
}

/** Whether a window is over the product, drawn again as one opens or closes: for the part that holds back for it alone. */
export function useWindowOverProduct() {
  return useSyncExternalStore(subscribeAny, windowOverProduct, () => false);
}

/** What one window shows, drawn again as it opens and closes: for that window alone. */
export function useProductWindow<T>(store: ProductWindowStore<T>) {
  return useSyncExternalStore(store.subscribe, store.get, () => null);
}

/** Whether one window is open, drawn again only as that changes. */
export function useProductWindowOpen(store: ProductWindowStore<unknown>) {
  return useSyncExternalStore(store.subscribe, () => store.get() !== null, () => false);
}

/** Closes them all as the product leaves (another account, signed out), settings aside: its hook keeps what an address opened. */
export function closeProductWindows() {
  for (const store of stores) if (store !== settingsWindowStore) store.set(null);
}

// The windows a link closes their own way; the settings window's own way
// with a link is apart (settingsWindowForLink).
const closedForLink = new Set<() => void>();

/**
 * The settings window's way with a link (useSettingsWindow): its close, which
 * asks first about changes it hasn't saved, and what opens it over the page
 * for a settings address — null for any other address.
 */
export type SettingsForLink = { close(): void; openFor(path: string): (() => void) | null };
// The latest mounted of the product's settings hooks (it mounts more than once as it opens).
const settingsWays: SettingsForLink[] = [];

/** Sets the settings window's way with a link; returns its removal. */
export function settingsWindowForLink(way: SettingsForLink) {
  settingsWays.push(way);
  return () => {
    const at = settingsWays.indexOf(way);
    if (at >= 0) settingsWays.splice(at, 1);
  };
}

/** Adds a window's own close to those a link calls (closeWindowsForLink); returns its removal. */
export function closeForLink(close: () => void) {
  closedForLink.add(close);
  return () => { closedForLink.delete(close); };
}

/** A window drawn only while open closes its own way as a link goes to a place here. */
export function useClosedForLink(close: () => void) {
  const latest = useRef(close);
  latest.current = close;
  useEffect(() => closeForLink(() => latest.current()), []);
}

/**
 * A link going to `path` in this window — pressed in the app or handed to
 * it, as the desktop app's leafen: links are — closes the windows over the
 * page, so the place shows: the router's one way there (lib/router.ts). A
 * settings address (/settings?section=…, /account, /admin/settings) opens the
 * settings window at its section over the page instead, the page and its
 * address left as they are, and says so (true): the router goes nowhere —
 * going there and back showed the start page under it.
 * Notion's settings close for a link pressed in them (seen 2026-10-27 and
 * 2026-11-19, "사람 디렉토리 보기") and its trash as a page opens from it
 * (twice 2026-11-19); a link handed to its Mac app leaves the settings over
 * the page it opens (twice 2026-11-19), which Leafen doesn't, an approved
 * difference: an agent hands the person links, such as a migration's report,
 * while they wait in 설정 › 가져오기. Going back or forward keeps them, as
 * Notion's settings stay over the page "뒤로" changes (twice 2026-11-19).
 */
export function closeWindowsForLink(path: string): boolean {
  const settingsForLink = settingsWays.at(-1);
  const openSettings = settingsForLink?.openFor(path) ?? null;
  for (const store of [importWindowStore, shortcutsWindowStore, databasePickerStore, trashPanelStore]) store.set(null);
  for (const close of [...closedForLink]) close();
  if (openSettings) openSettings();
  else settingsForLink?.close();
  return openSettings !== null;
}

/**
 * Whether a link to `path` is a settings address, which opens the settings
 * over the page and leaves the page where it is (closeWindowsForLink): a
 * pinned tab of the Mac app opens it over its own page too, where any other
 * place opens in a new tab (lib/router.ts).
 */
export function opensSettingsOver(path: string): boolean {
  return (settingsWays.at(-1)?.openFor(path) ?? null) !== null;
}

/** A press that opens or closes the page's database picker, as a state's setter. */
export function setDatabasePickerOpen(next: boolean | ((open: boolean) => boolean)) {
  const open = typeof next === "function" ? next(databasePickerStore.get() !== null) : next;
  databasePickerStore.set(open ? true : null);
}

/** The sidebar's word that its new workspace window opened or closed. */
export function followWorkspaceCreateOpen(open: boolean) {
  workspaceCreateStore.set(open ? true : null);
}

/** The sidebar's word that its teamspace window opened or closed. */
export function followTeamspaceWindowOpen(open: boolean) {
  teamspaceWindowStore.set(open ? true : null);
}
