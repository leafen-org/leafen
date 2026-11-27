import { useEffect, useRef } from "react";
import { requestSettingsWindow } from "@/components/settings/settingsWindowRequest";
import { onDesktopWindowEvent, packagedComputer } from "./desktopMenuCommands";

// The desktop app's command search window (desktop/quick_search_desktop.go),
// as Notion's app's /quick-search: the app's page opened with
// `?leafen-quick-search=1` in a window of its own over the app in use. It
// shows the search alone (the rest is hidden, app/globals.css), opens it
// each time the window comes up, and what the search chooses opens in the
// app's window in front instead of here; Esc, or the window losing the
// keys, takes it away. Nothing of this in any other window.

const PARAM = "leafen-quick-search";
const HIDDEN_EVENT = "leafen:quick-search-hidden";

let inWindow = false;
// The window's search is up: a page it goes to opens in the app's window.
let choosing = false;
let pending: string | null = null;
let ready = false;

/** Marks the page as the command search window's, from its first address (main.tsx). */
export function markQuickSearchWindow(): void {
  if (typeof window === "undefined" || !packagedComputer()) return;
  inWindow = new URLSearchParams(window.location.search).has(PARAM);
  if (inWindow) document.documentElement.dataset.quickSearchWindow = "";
}

/** Whether this page is the command search window's. */
export function isQuickSearchWindow(): boolean {
  return inWindow;
}

async function call(method: string, ...args: unknown[]): Promise<unknown> {
  const { Call } = await import("@wailsio/runtime");
  return await Call.ByName(`main.QuickSearch.${method}`, ...args);
}

/**
 * A page or database the window's search goes to opens in the app's window
 * in front (the router asks before it moves); the last one asked in a task
 * goes, as a result opens its page and then its block on it.
 */
export function forwardFromQuickSearch(path: string, kind: string): boolean {
  if (!inWindow || !choosing || (kind !== "page" && kind !== "database")) return false;
  const first = pending === null;
  pending = path;
  if (first) {
    window.setTimeout(() => {
      const going = pending;
      pending = null;
      choosing = false;
      if (going) void call("Open", going).catch(() => undefined);
    }, 0);
  }
  return true;
}

/** Takes the window away: its search closed without opening anything (Esc). */
export function hideQuickSearch(): void {
  if (!inWindow) return;
  choosing = false;
  void call("Hide").catch(() => undefined);
}

/**
 * The search's own part in the window: once open it may send what it
 * chooses on and tells the app it is ready to show; the window going away
 * closes it, so it opens anew the next time.
 */
export function useQuickSearchWindow(open: boolean, close: () => void): void {
  const closeRef = useRef(close);
  closeRef.current = close;
  useEffect(() => {
    if (!inWindow || !open) return;
    choosing = true;
    if (!ready) {
      ready = true;
      void call("Ready").catch(() => undefined);
    }
  }, [open]);
  useEffect(() => {
    if (!inWindow) return;
    return onDesktopWindowEvent(HIDDEN_EVENT, () => {
      choosing = false;
      closeRef.current();
    });
  }, []);
}

/**
 * Opens 설정 › 이 컴퓨터 at the command search's key, as the search's foot
 * asks (Notion's "클릭하여 단축키 편집"): from the window, in the app's
 * window in front.
 */
export function editCommandSearchShortcut(): void {
  if (inWindow) {
    void call("EditShortcut").catch(() => undefined);
    return;
  }
  requestSettingsWindow({ section: "servers", part: "computer" });
}
