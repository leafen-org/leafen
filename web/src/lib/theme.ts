"use client";

import { useCallback, useSyncExternalStore } from "react";
import { deviceValue, storeDeviceValue } from "./browserStorage";

export type ThemePref = "light" | "dark" | "system";

const KEY = "leafen:theme";

// How the app looks is this browser's, as Notion sets it per device.
export function getThemePref(): ThemePref {
  const v = deviceValue(KEY);
  return v === "light" || v === "dark" || v === "system" ? v : "system";
}

export function resolveTheme(pref: ThemePref): "light" | "dark" {
  if (pref === "system") {
    if (typeof window !== "undefined" && window.matchMedia) {
      return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
    }
    return "light";
  }
  return pref;
}

export function applyTheme(pref: ThemePref) {
  if (typeof document === "undefined") return;
  const theme = resolveTheme(pref);
  document.documentElement.dataset.theme = theme;
  document.documentElement.style.colorScheme = theme;
}

// ── single shared source of truth ──────────────────────────────────────────
// Every useTheme() consumer (AppShell, Sidebar, Settings, the Cmd+Shift+L
// shortcut) reads one module-level preference, so a choice in one place shows
// everywhere. setThemePref updates it and notifies all subscribers, and the
// OS/cross-tab listeners feed the same cell.
let currentPref: ThemePref | null = null;
const listeners = new Set<() => void>();
let globalListenersInstalled = false;
let printingLight = false;

/**
 * True while the page is drawn light for printing: what follows the
 * theme into the window's own frame (the app's tab strip, its window color)
 * keeps the chosen theme meanwhile.
 */
export function printingInLight() {
  return printingLight;
}

function readPref(): ThemePref {
  if (currentPref === null) currentPref = getThemePref();
  return currentPref;
}

function emit() {
  for (const listener of listeners) listener();
}

/**
 * Follows the system's light or dark switch, and another tab's choice, from
 * the first screen on (sign-in, language, starting), not only once the
 * workspace shows a theme control; main.tsx starts it with the first theme.
 */
export function followThemeChanges() {
  ensureGlobalListeners();
}

function ensureGlobalListeners() {
  if (globalListenersInstalled || typeof window === "undefined") return;
  globalListenersInstalled = true;
  // OS theme flip: reapply + re-render only while following the system.
  if (window.matchMedia) {
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    mq.addEventListener("change", () => {
      if (readPref() === "system") {
        applyTheme("system");
        emit();
      }
    });
  }
  // A page goes on paper in the light theme, as Notion's print does from its
  // dark theme (seen 2026-11-12), and back to the chosen one after; the
  // window's frame around the page isn't told (printingLight).
  window.addEventListener("beforeprint", () => {
    printingLight = true;
    applyTheme("light");
  });
  window.addEventListener("afterprint", () => {
    applyTheme(readPref());
    printingLight = false;
  });
  // Cross-tab: another tab changed the preference.
  window.addEventListener("storage", (event) => {
    if (event.key !== null && event.key !== KEY) return;
    currentPref = getThemePref();
    applyTheme(currentPref);
    emit();
  });
}

export function setThemePref(pref: ThemePref) {
  storeDeviceValue(KEY, pref);
  currentPref = pref;
  applyTheme(pref);
  emit();
}

function subscribe(listener: () => void): () => void {
  ensureGlobalListeners();
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * Theme preference bound to the shared module-level store. The `data-theme`
 * attribute is set before React renders in main.tsx; this hook keeps every
 * consumer in sync when the user, another tab, or the OS changes it.
 */
export function useTheme(): [ThemePref, (pref: ThemePref) => void] {
  const pref = useSyncExternalStore(subscribe, readPref, (): ThemePref => "system");
  const setPref = useCallback((next: ThemePref) => setThemePref(next), []);
  return [pref, setPref];
}
