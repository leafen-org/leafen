import { useSyncExternalStore } from "react";
import { pushRoute } from "@/lib/router";

// The organization an app opens in when the person picks it in the workspace
// menu, where the same app stands once for each organization they use it in
// (2026-11-15: three "팀 퀴즈" rows all opened the open workspace's
// organization's). It holds for this tab until the app's window closes, so a
// reload opens the same one; an app opened any other way opens in the
// organization of the open workspace (AppHost).
const storeKey = (slug: string) => `leafen:app-tenancy:${slug}`;
const listeners = new Set<() => void>();
const chosen = new Map<string, string>();

function stored(slug: string) {
  if (chosen.has(slug)) return chosen.get(slug) ?? "";
  try {
    return window.sessionStorage.getItem(storeKey(slug)) ?? "";
  } catch {
    // Without the tab's storage the choice lasts until a reload.
    return "";
  }
}

function keep(slug: string, tenancyKey: string) {
  chosen.set(slug, tenancyKey);
  try {
    if (tenancyKey) window.sessionStorage.setItem(storeKey(slug), tenancyKey);
    else window.sessionStorage.removeItem(storeKey(slug));
  } catch {
    // The choice stays in this page.
  }
  for (const listener of listeners) listener();
}

/** Opens the app in the organization the person picked it for. */
export function openAppInTenancy(slug: string, tenancyKey: string) {
  keep(slug, tenancyKey);
  pushRoute(`/apps/${slug}`);
}

/** Its window closed: the next opening goes by the open workspace again. */
export function forgetAppTenancy(slug: string) {
  if (stored(slug)) keep(slug, "");
}

/** The organization the person picked the app for in this tab, or "". */
export function useChosenAppTenancy(slug: string) {
  return useSyncExternalStore((listener) => {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
  }, () => stored(slug));
}
