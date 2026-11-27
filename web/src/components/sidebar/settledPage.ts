import { useEffect, useSyncExternalStore } from "react";
import { afterStart } from "@/lib/startSettled";

// Whether the page has come up: its start settled (startSettled.ts), at an
// idle moment after. A touch screen's sidebar reads its rows' access once it
// has (PageTreeItem), so a sidebar's dozens of small reads queue behind none
// of the page's own. The first idle moment, which this waited for before,
// comes while the start waits on the network, and the rows' reads went out
// beside the start's (2026-11-13).
let settled = false;
let waiting = false;
const listeners = new Set<() => void>();

function settle() {
  settled = true;
  for (const listener of listeners) listener();
}

function wait() {
  if (settled || waiting) return;
  waiting = true;
  afterStart(settle);
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** `wanted`, once the page has come up. */
export function useSettledPage(wanted: boolean) {
  const ready = useSyncExternalStore(subscribe, () => settled, () => false);
  useEffect(() => { if (wanted) wait(); }, [wanted]);
  return wanted && ready;
}
