import type { ImportDestination, ImportSource } from "@/components/import/ImportDialog";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { settingsWindowForLink, settingsWindowStore } from "@/lib/productWindows";
import { onDesktopSettingsCommand } from "@/lib/desktopMenuCommands";
import { replaceRoute } from "@/lib/router";
import { onReloadKeepingPlace, takeKeptPlace } from "@/lib/reloadKeepingPlace";
import { legacySettingsAddress, type SettingsSection, type SettingsWindowState } from "./settingsWindowModel";
import { onSettingsWindowRequest } from "./settingsWindowRequest";
import { mayLeaveSettings, settingsChangesUnsaved } from "./settingsUnsaved";

// The window an address opened, whose address is gone by then, stays through
// the product opening anew when a local lock is undone (AuthenticatedSession).
// A reload the app made comes back with the window it had open, kept the
// same way (the product mounts more than once as it opens).
let openedByAddress: SettingsWindowState | null = typeof window === "undefined" ? null : takeKeptPlace<SettingsWindowState>("settingsWindow");

/**
 * The settings window over the current screen, as Notion's, which keeps the
 * address as it is (seen 2026-10-02). An address of the former consoles
 * opens it over the place it came from, once the product has read its
 * workspaces: a change of address while it reads them drops that read.
 * Whether it is open is kept in lib/productWindows.ts, which the window
 * alone follows: the product is not drawn again as it opens and closes.
 */
export function useSettingsWindow(pathname: string, loaded: boolean) {
  // The product opening anew comes back with the window an address opened.
  useState(() => { if (openedByAddress && !settingsWindowStore.get()) settingsWindowStore.set(openedByAddress); });
  useEffect(() => () => settingsWindowStore.set(openedByAddress), []);
  const setState = settingsWindowStore.set;
  const returnTo = useRef<string | null>(null);
  // What had the focus when the window was asked for: the page behind is
  // shut as the window draws, its code perhaps still coming on its first
  // opening, and the focus leaves it then (lib/modalFocus.ts)
  // (2026-11-08: closing gave the focus to the page, not the app notice's
  // "서버 → 앱 열기"). The window gives the focus back to it.
  const opener = useRef<HTMLElement | null>(null);
  const rememberOpener = useCallback(() => {
    if (settingsWindowStore.get()) return;
    const active = typeof document === "undefined" ? null : document.activeElement;
    opener.current = active instanceof HTMLElement && active !== document.body ? active : null;
  }, []);
  useEffect(() => onReloadKeepingPlace("settingsWindow", settingsWindowStore.get), []);

  useEffect(() => {
    const legacy = legacySettingsAddress(window.location.pathname, window.location.search);
    if (!legacy) return;
    openedByAddress = legacy.state;
    setState(legacy.state);
    returnTo.current = legacy.returnTo;
  }, [pathname]);

  useEffect(() => {
    if (!loaded || returnTo.current === null) return;
    const address = returnTo.current;
    returnTo.current = null;
    replaceRoute(address);
  }, [loaded, pathname]);

  // Opened without a section it shows "기본 설정", as Notion's does whatever
  // was seen last (seen 2026-10-28).
  // A part (the developer section's tokens, say) opens the section at it.
  const open = useCallback((section: SettingsSection = "preferences", options: { invite?: boolean; part?: string; importPlace?: ImportDestination; importSource?: ImportSource } = {}) => {
    rememberOpener();
    openedByAddress = null;
    setState({ section, invite: options.invite === true, part: options.part, importPlace: options.importPlace, importSource: options.importSource });
  }, [rememberOpener]);
  const select = useCallback((section: SettingsSection) => {
    openedByAddress = null;
    setState({ section });
  }, []);
  const close = useCallback(() => {
    openedByAddress = null;
    setState(null);
  }, []);

  // A link going to a place here closes it (closeWindowsForLink), at once
  // when no change is left unsaved, or once the person lets the changes go.
  // A link to a settings address — pressed in the app, or a leafen: link
  // handed to the desktop app — opens it at that section over the page as it
  // is, not by a trip to the address and back (the start page showed under
  // it, 2026-11-19), asking first as moving to another section does.
  useEffect(() => settingsWindowForLink({
    close() {
      const shown = settingsWindowStore.get();
      if (!shown) return;
      if (!settingsChangesUnsaved()) close();
      else void mayLeaveSettings().then((leave) => { if (leave && settingsWindowStore.get() === shown) close(); });
    },
    openFor(path) {
      const url = new URL(path, window.location.href);
      const asked = legacySettingsAddress(url.pathname, url.search);
      if (!asked) return null;
      return () => {
        const shown = settingsWindowStore.get();
        const show = () => {
          if (settingsWindowStore.get() !== shown) return;
          rememberOpener();
          openedByAddress = null;
          setState(asked.state);
        };
        if (!shown || shown.section === asked.state.section || !settingsChangesUnsaved()) show();
        else void mayLeaveSettings(asked.state.section).then((leave) => { if (leave) show(); });
      };
    },
  }), [close, rememberOpener]);
  // The desktop app's "환경설정…" (⌘,) opens it where it opens from the sidebar.
  useEffect(() => onDesktopSettingsCommand((asked) => asked
    ? open(asked.section as SettingsSection, { part: asked.part }) : open()), [open]);
  // A screen without the window's hold asks for a section (requestSettingsWindow).
  useEffect(() => onSettingsWindowRequest((requested) => { rememberOpener(); openedByAddress = null; setState(requested); }), [rememberOpener]);
  return useMemo(() => ({ open, select, close, opener }), [close, open, select]);
}
