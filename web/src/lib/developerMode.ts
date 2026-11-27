import { useSyncExternalStore } from "react";
import { request } from "@/apiTransport";
import type { Session } from "@/types";
import { useDisplayPreferences } from "./displayPreferences";
import { deviceValue, storeDeviceValue } from "./browserStorage";
import { primaryModifier } from "@/lib/keyboardShortcuts";

/**
 * The developer UI of the Developer item's "개발자 기능 활성화", as Notion's
 * (seen 2026-10-02): a bar along the window's bottom with the IDs the
 * Notion-compatible API knows the person, the workspace and the page by, and
 * a "개발자" group in the page menu. The account keeps whether it is on; this
 * device keeps whether the bar is hidden, which ⌘⇧. toggles.
 */
export type DeveloperIds = { user: string; workspace?: string; page?: string };

export function useDeveloperMode(): boolean {
  return useDisplayPreferences().developerMode;
}

const HIDDEN_KEY = "leafen:developer-bar-hidden";
const listeners = new Set<() => void>();
let hidden = readHidden();

function readHidden() {
  return deviceValue(HIDDEN_KEY) === "true";
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function useDeveloperBarHidden(): boolean {
  return useSyncExternalStore(subscribe, () => hidden, () => hidden);
}

export function toggleDeveloperBar() {
  hidden = !hidden;
  // Without storage the bar stays as toggled until the page reloads.
  storeDeviceValue(HIDDEN_KEY, String(hidden));
  for (const listener of listeners) listener();
}

/** ⌘⇧. (Ctrl+Shift+. elsewhere), which shows or hides the bar. */
export function isDeveloperBarShortcut(event: KeyboardEvent): boolean {
  return primaryModifier(event) && event.shiftKey && !event.altKey && event.code === "Period";
}

/** An ID as the bar shows it: its first 8 and last 6 characters. */
export function shortDeveloperId(id: string): string {
  const plain = id.replace(/-/g, "");
  return plain.length > 16 ? `${plain.slice(0, 8)}…${plain.slice(-6)}` : plain;
}

export function readDeveloperIds(
  session: Session,
  target: { workspaceId?: string | null; pageId?: string | null },
  signal?: AbortSignal,
) {
  const query = new URLSearchParams();
  if (target.workspaceId) query.set("workspaceId", target.workspaceId);
  if (target.pageId) query.set("pageId", target.pageId);
  const search = query.toString();
  return request<DeveloperIds>(`/account/developer-ids${search ? `?${search}` : ""}`, { signal }, session);
}
