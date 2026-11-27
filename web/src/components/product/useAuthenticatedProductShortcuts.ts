import type { ProductStatusSink } from "@/lib/productStatus";
import { i18next } from "@/i18n";
import { useEffect, useRef } from "react";
import { copyPageLink, copyPageName } from "@/components/editor/pageLinkClipboard";
import { aloneDatabaseLinkTarget } from "@/components/page/aloneDatabaseFavorite";
import { onDesktopMenuCommand } from "@/lib/desktopMenuCommands";
import { awaitDesktopTabName, listenForNewTabSearch, onCopyTabLink } from "@/lib/desktopTabs";
import { matchesKeyboardShortcut } from "@/lib/keyboardShortcuts";
import { pageLinkToCopy } from "@/lib/databasePageLink";
import { pushRoute } from "@/lib/router";
import { SEARCH_REQUEST } from "@/lib/searchRequest";
import { windowOverProduct } from "@/lib/productWindows";
import { isQuickSearchWindow } from "@/lib/quickSearchWindow";
import { resolveTheme, type ThemePref } from "@/lib/theme";
import { toggleHighContrast } from "@/lib/contrast";
import type { Page } from "@/types";
import { drawnPageParent, type DrawnPage } from "./useDrawnPage";

type AuthenticatedProductShortcutOptions = {
  quickFindEnabled: boolean;
  /** The page drawn at the address, which the page link keys copy and the parent key goes up from (useDrawnPage.ts). */
  drawnPage: DrawnPage | null;
  /** The reader's pages, which the parent key opens as pages. */
  pages: readonly Page[];
  themePref: ThemePref;
  setThemePref: (pref: ThemePref) => void;
  onOpenSearch: () => void;
  onOpenKeyboardShortcuts: () => void;
  onOpenPage: (page: Page) => void;
  onStatus: ProductStatusSink;
};

export function useAuthenticatedProductShortcuts({
  quickFindEnabled,
  drawnPage,
  pages,
  themePref,
  setThemePref,
  onOpenSearch,
  onOpenKeyboardShortcuts,
  onOpenPage,
  onStatus,
}: AuthenticatedProductShortcutOptions) {
  const handlers = useRef({
    onOpenKeyboardShortcuts,
    onOpenPage,
    onOpenSearch,
    onStatus,
    setThemePref,
  });
  handlers.current = {
    onOpenKeyboardShortcuts,
    onOpenPage,
    onOpenSearch,
    onStatus,
    setThemePref,
  };

  useEffect(() => {
    if (!quickFindEnabled) return;
    const onQuickFind = (event: KeyboardEvent) => {
      // A chosen picture's ⌘K is its link's (useBlockSelectionKeys).
      if (event.defaultPrevented || !matchesKeyboardShortcut("quickFind", event)) return;
      event.preventDefault();
      handlers.current.onOpenSearch();
    };
    window.addEventListener("keydown", onQuickFind);
    // Search asked for elsewhere, as a wiki's "검색" (searchRequest.ts).
    const onRequest = () => handlers.current.onOpenSearch();
    window.addEventListener(SEARCH_REQUEST, onRequest);
    // The Mac app's ⌘T and its strip's "+": a search whose page opens in a
    // new tab (desktopTabs.ts).
    const stopNewTab = listenForNewTabSearch(() => handlers.current.onOpenSearch());
    // The desktop app's command search, ⇧⌘K from anywhere (desktopMenuCommands.ts);
    // its own window opens its search as it starts (quickSearchWindow.ts).
    const stopCommandSearch = onDesktopMenuCommand("openSearch", () => handlers.current.onOpenSearch());
    if (isQuickSearchWindow()) handlers.current.onOpenSearch();
    // The signed-in app names its tab from its screens (documentChrome.ts).
    awaitDesktopTabName();
    return () => {
      window.removeEventListener("keydown", onQuickFind);
      window.removeEventListener(SEARCH_REQUEST, onRequest);
      stopNewTab();
      stopCommandSearch();
    };
  }, [quickFindEnabled]);

  // The page drawn, read as a key is pressed: a row's page tells it as it shows (useDrawnPage.ts).
  const drawn = useRef(drawnPage);
  drawn.current = drawnPage;
  const listed = useRef(pages);
  listed.current = pages;
  useEffect(() => {
    // The page's link, or its name as a link — Notion's app's Edit › 현재
    // 페이지 링크 복사 (⌘L) and 현재 페이지의 이름 복사 (⌥⌘L), which the Mac
    // app's menu bar asks for (desktopMenuCommands.ts); its ⌥⌘L is the menu's.
    // A database opened alone gives its own (aloneDatabaseFavorite.ts); the
    // link is the one the page's bar copies (pageLinkToCopy).
    const target = () => aloneDatabaseLinkTarget()
      ?? (drawn.current && !drawn.current.inTrash ? drawn.current : null);
    const copy = (named: boolean) => {
      const shown = target();
      if (!shown) return;
      const block = shown.id !== drawn.current?.id;
      const url = pageLinkToCopy(listed.current.find((page) => page.id === shown.id) ?? { id: shown.id, kind: block ? "database" : undefined }, undefined);
      void (named ? copyPageName : copyPageLink)(url, shown.id, shown.title).then((ok) => {
        const copied = block ? "blockHandle:database.copiedLink" : "topBar:copiedPageLink";
        handlers.current.onStatus(ok ? i18next.t(copied) : i18next.t("appShell:couldntCopyLink"), ok ? "default" : "error");
      });
    };
    const onCopyPageLink = (event: KeyboardEvent) => {
      if (!target() || !matchesKeyboardShortcut("copyPageLink", event)) return;
      event.preventDefault();
      copy(false);
    };
    window.addEventListener("keydown", onCopyPageLink);
    const stopLink = onDesktopMenuCommand("copyPageLink", () => copy(false));
    const stopName = onDesktopMenuCommand("copyPageName", () => copy(true));
    // The Mac app's tab menu "링크 복사" for a tab whose page hasn't opened
    // since the app started: this page copies that one's link.
    const stopTab = onCopyTabLink((url, pageId, title) => {
      void copyPageLink(url, pageId, title).then((ok) => {
        handlers.current.onStatus(ok ? i18next.t("topBar:copiedPageLink") : i18next.t("appShell:couldntCopyLink"), ok ? "default" : "error");
      });
    });
    return () => {
      window.removeEventListener("keydown", onCopyPageLink);
      stopLink();
      stopName();
      stopTab();
    };
  }, []);

  useEffect(() => {
    // The appearance keys change the screen and say nothing, as Notion's
    // (seen 2026-11-20 in Chrome: no toast after ⌘⇧L, ⌃⇧L, ⌘⇧⌥L or ⌃⇧⌥L,
    // at once or a second on, light or dark) — the screen is the answer.
    const onToggleTheme = (event: KeyboardEvent) => {
      if (matchesKeyboardShortcut("toggleHighContrast", event)) {
        event.preventDefault();
        toggleHighContrast();
        return;
      }
      if (!matchesKeyboardShortcut("toggleTheme", event)) return;
      event.preventDefault();
      handlers.current.setThemePref(resolveTheme(themePref) === "dark" ? "light" : "dark");
    };
    window.addEventListener("keydown", onToggleTheme);
    return () => window.removeEventListener("keydown", onToggleTheme);
  }, [themePref]);

  useEffect(() => {
    const onNavigateHistory = (event: KeyboardEvent) => {
      if (matchesKeyboardShortcut("navigateBack", event)) {
        event.preventDefault();
        window.history.back();
      } else if (matchesKeyboardShortcut("navigateForward", event)) {
        event.preventDefault();
        window.history.forward();
      }
    };
    window.addEventListener("keydown", onNavigateHistory);
    return () => window.removeEventListener("keydown", onNavigateHistory);
  }, []);

  useEffect(() => {
    // Up from the page drawn, a database's page too (useDrawnPage.ts drawnPageParent).
    const onOpenParentPage = (event: KeyboardEvent) => {
      if (!matchesKeyboardShortcut("openParentPage", event)) return;
      const parent = drawnPageParent(drawn.current, listed.current);
      if (!parent) return;
      event.preventDefault();
      if (parent.page) handlers.current.onOpenPage(parent.page);
      else pushRoute(parent.href);
    };
    window.addEventListener("keydown", onOpenParentPage);
    return () => window.removeEventListener("keydown", onOpenParentPage);
  }, []);

  useEffect(() => {
    const onKeyboardShortcutsShortcut = (event: KeyboardEvent) => {
      // Held back while a window is over the product, search among them (lib/productWindows.ts).
      if (windowOverProduct() || !matchesKeyboardShortcut("openKeyboardShortcuts", event)) return;
      event.preventDefault();
      handlers.current.onOpenKeyboardShortcuts();
    };
    window.addEventListener("keydown", onKeyboardShortcutsShortcut);
    return () => window.removeEventListener("keydown", onKeyboardShortcutsShortcut);
  }, []);
}
