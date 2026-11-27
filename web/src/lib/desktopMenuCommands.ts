import { afterHeldTyping } from "./heldTypingPlayback";
import { PACKAGED_ORIGINS } from "./packagedOrigins";

// What the desktop app's menu bar asks of the window in front
// (desktop/app_menu_bar_darwin.go), as Notion's app menu does: Settings
// ("환경설정…", ⌘,), the sidebar ("사이드바 표시/숨기기", ⌘\), and the current
// page's link ("현재 페이지 링크 복사", ⌘L) or its name as a link ("현재
// 페이지의 이름 복사", ⌥⌘L), the current page's export window ("인쇄", as
// Notion's app's), and the desktop app's command search (⇧⌘K from anywhere,
// desktop/command_search_desktop.go) its search. Each names the window it is
// for, so only that one answers when the app has more than one.
const DESKTOP_MENU_EVENTS = {
  openSettings: "leafen:open-settings",
  toggleSidebar: "leafen:toggle-sidebar",
  copyPageLink: "leafen:copy-page-link",
  copyPageName: "leafen:copy-page-name",
  exportPage: "leafen:export-page",
  openSearch: "leafen:open-search",
} as const;

export type DesktopMenuCommand = keyof typeof DESKTOP_MENU_EVENTS;

/** Whether this window is the desktop app's own page on a computer, not a
 * browser's or a phone's (packagedClient.ts asks the same of the origin;
 * this file stays light for keyboardShortcuts.ts). */
export function packagedComputer(): boolean {
  return typeof window !== "undefined" && PACKAGED_ORIGINS.includes(window.location.origin)
    && !/Android|iPhone|iPad/u.test(navigator.userAgent);
}

/** Whether this window is the Mac app's, under its menu bar. */
export function hasDesktopMenuBar(): boolean {
  return packagedComputer() && /Macintosh/u.test(navigator.userAgent) && navigator.maxTouchPoints < 2;
}

/** Whether this window is the Windows app's, which has no menu bar. */
export function packagedWindows(): boolean {
  return packagedComputer() && /Windows/u.test(navigator.userAgent);
}

/** Whether this window's pages are tabs of the app's windows: the Mac and Windows apps' are (desktop/window_tabs.go). */
export function hasDesktopTabs(): boolean {
  return hasDesktopMenuBar() || packagedWindows();
}

/**
 * Calls `take` with each of the desktop app's events of that name meant for
 * this window: its data names the window, as itself or in its `window`, or
 * names none and is meant for every window. The window's name is asked when
 * an event comes, again until the app answers: a page whose first ask was
 * lost as its window was made (a Mac tab's page, put in its window's frame
 * as it loads, 2026-11-10) would otherwise hear nothing. Returns the way to stop.
 */
export function onDesktopWindowEvent(name: string, take: (data: unknown) => void): () => void {
  if (!packagedComputer()) return () => undefined;
  let stopped = false;
  let stop: (() => void) | undefined;
  void import("@wailsio/runtime").then(({ Events, Window }) => {
    if (stopped) return;
    let mine = "";
    const named = async () => mine || (mine = await Window.Name().catch(() => ""));
    stop = Events.On(name, (event) => {
      const data: unknown = event.data;
      const meant = typeof data === "string" ? data
        : typeof data === "object" && data !== null && typeof (data as { window?: unknown }).window === "string"
          ? (data as { window: string }).window : "";
      if (!meant) {
        take(data);
        return;
      }
      void named().then((own) => { if (!stopped && own === meant) take(data); });
    });
  }, () => undefined);
  return () => {
    stopped = true;
    stop?.();
  };
}

/**
 * Calls `take` with each of the desktop app's menu events of that name meant
 * for this window, once what was typed before it has reached its place, as a
 * shortcut pressed in the page waits for it (afterHeldTyping); returns the way to stop.
 */
export function onDesktopMenuEvent(name: string, take: (data: unknown) => void): () => void {
  return onDesktopWindowEvent(name, (data) => afterHeldTyping(() => take(data)));
}

/** Calls `run` whenever the desktop app's menu asks this window for `command`; returns the way to stop. */
export function onDesktopMenuCommand(command: DesktopMenuCommand, run: () => void): () => void {
  return onDesktopMenuEvent(DESKTOP_MENU_EVENTS[command], () => run());
}

/**
 * Calls `open` whenever the desktop app's menu asks for Settings, with the
 * section and part asked for when one is (the command search's "단축키 변경"
 * asks for 이 컴퓨터, desktop/quick_search_desktop.go); returns the way to stop.
 */
export function onDesktopSettingsCommand(open: (asked?: { section: string; part?: string }) => void): () => void {
  return onDesktopMenuEvent(DESKTOP_MENU_EVENTS.openSettings, (data) => {
    const asked = typeof data === "object" && data !== null ? data as { section?: unknown; part?: unknown } : {};
    if (typeof asked.section === "string") open({ section: asked.section, part: typeof asked.part === "string" ? asked.part : undefined });
    else open();
  });
}
