import { newTabPlace } from "./desktopTabs";
import { serverUrl } from "./serverOrigin";

// What the desktop app's page asks to open in a new window or tab —
// window.open from a link in text, "새 탭에서 열기", a ⌘-pressed page — goes to
// the app's rule (desktop/new_window.go): a site, mail or a phone number opens
// with the computer's own browser or app, one of the app's pages in a new tab
// of the Mac app's window — behind the shown one, in front with ⇧, and in
// front at the end for a search for a new tab (desktopTabs.ts) — and in a new
// app window elsewhere. A window the page fills itself (an empty or blob:
// address it points or writes later) is the window's own to open, as is going
// in the same window. A browser's page keeps its own window.open.

const OUTSIDE_PROTOCOLS = new Set(["http:", "https:", "mailto:", "tel:"]);
const SAME_WINDOW_TARGETS = new Set(["_self", "_top", "_parent"]);

/** The address the app opens for a window.open call, or "" when the window opens it itself. */
function appAddress(url: string | URL | undefined, target: string | undefined): string {
  if (url === undefined || url === null || String(url) === "") return "";
  if (target && SAME_WINDOW_TARGETS.has(target.toLowerCase())) return "";
  let address: URL;
  try {
    address = new URL(String(url), window.location.href);
  } catch {
    return "";
  }
  const ownPage = address.protocol === window.location.protocol && address.host === window.location.host;
  return ownPage || OUTSIDE_PROTOCOLS.has(address.protocol) ? address.href : "";
}

/**
 * In a computer's app, opens one of its pages in a new window of its own:
 * a page menu's "새 창에서 열기", right under "새 탭에서 열기", as Notion's
 * app's (seen 2026-11-16 on a page block's menu and a row's ⌘/ menu).
 */
export function openPageInNewWindow(href: string): void {
  const address = new URL(href, window.location.href).href;
  void import("@wailsio/runtime")
    .then(({ Call }) => Call.ByName("main.WindowOpener.OpenWindow", address))
    .catch(() => undefined);
}

/** In the desktop app, sends the page's new windows and tabs to the app's rule. */
export function sendNewWindowsToApp(): void {
  const own = window.open.bind(window);
  window.open = (url?: string | URL, target?: string, features?: string) => {
    const address = appAddress(url, target);
    if (!address) return own(url, target, features);
    const { active, atEnd } = newTabPlace();
    void import("@wailsio/runtime")
      .then(({ Call }) => Call.ByName("main.WindowOpener.Open", address, active, atEnd))
      .catch(() => undefined);
    // Opened outside this page, as a "noopener" window is.
    return null;
  };
  // The Mac app's right-press menu on a link to one of its pages
  // (desktop/text_menu_darwin.m): a new tab in front or a new window, or its
  // address on the server, for the browser or the clipboard.
  (window as Window & { leafenLinkMenu?: (action: string, href: string) => string }).leafenLinkMenu = (action, href) => {
    let address: URL;
    try {
      address = new URL(href, window.location.href);
    } catch {
      return "";
    }
    if (action === "browser" || action === "copy") return serverUrl(`${address.pathname}${address.search}${address.hash}`);
    const call = action === "tab" ? ["main.WindowOpener.Open", address.href, true, false]
      : action === "window" ? ["main.WindowOpener.OpenWindow", address.href] : null;
    if (call) {
      const [name, ...args] = call as [string, ...unknown[]];
      void import("@wailsio/runtime").then(({ Call }) => Call.ByName(name, ...args)).catch(() => undefined);
    }
    return "";
  };
}
