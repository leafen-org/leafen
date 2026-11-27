"use client";

import { useCallback, useSyncExternalStore } from "react";
import { deviceValue, storeDeviceValue } from "./browserStorage";

/**
 * This device's contrast, as Notion's "고대비" (seen 2026-10-02): following
 * the system's "increase contrast", standard, or high. It marks the page
 * `data-contrast` ("auto", "standard" or "high"); globals.css darkens text,
 * icons and borders for "high", and for "auto" while the system asks for
 * more contrast.
 */
export type ContrastPref = "system" | "standard" | "high";

const KEY = "leafen:contrast";

export function getContrastPref(): ContrastPref {
  const value = deviceValue(KEY);
  return value === "system" || value === "standard" || value === "high" ? value : "system";
}

export function applyContrast(pref: ContrastPref) {
  if (typeof document === "undefined") return;
  document.documentElement.dataset.contrast = pref === "system" ? "auto" : pref;
}

let current: ContrastPref | null = null;
const listeners = new Set<() => void>();
let followingOtherTabs = false;

function read(): ContrastPref {
  if (current === null) current = getContrastPref();
  return current;
}

function emit() {
  for (const listener of listeners) listener();
}

/** Another tab's choice shows here too; main.tsx starts it with the first contrast. */
export function followContrastChanges() {
  if (followingOtherTabs || typeof window === "undefined") return;
  followingOtherTabs = true;
  window.addEventListener("storage", (event) => {
    if (event.key !== null && event.key !== KEY) return;
    current = getContrastPref();
    applyContrast(current);
    emit();
  });
}

export function setContrastPref(pref: ContrastPref) {
  storeDeviceValue(KEY, pref);
  current = pref;
  applyContrast(pref);
  emit();
}

/** Whether this device shows high contrast now: chosen, or asked for by the system while following it. */
export function highContrastShown(pref: ContrastPref = read()) {
  return pref === "high" || (pref === "system" && typeof window !== "undefined"
    && window.matchMedia?.("(prefers-contrast: more)").matches === true);
}

/**
 * ⌘⇧⌥L: standard contrast where high contrast shows, high contrast
 * elsewhere, with no message, as Notion's "고대비 모드 전환" (measured
 * 2026-11-20 in Chrome at 1440: from "시스템 설정 사용" with the system
 * asking for none, 고대비; then 표준 대비, then 고대비 again).
 */
export function toggleHighContrast() {
  setContrastPref(highContrastShown() ? "standard" : "high");
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function useContrast(): [ContrastPref, (pref: ContrastPref) => void] {
  const pref = useSyncExternalStore(subscribe, read, (): ContrastPref => "system");
  const setPref = useCallback((next: ContrastPref) => setContrastPref(next), []);
  return [pref, setPref];
}
