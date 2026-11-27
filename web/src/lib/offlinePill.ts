import { useEffect, useSyncExternalStore } from "react";

/**
 * A lost connection says so where Notion's does: "오프라인" at the head of the
 * top bar's right side, its tip telling what waits to be sent, and a
 * struck-out cloud after the workspace's name in the sidebar (seen
 * 2026-11-14, 1440 dark; a phone's bar has none, its drawer the cloud), the
 * toasts at the window's foot clear of them. The connection's state has one
 * owner, SyncStatusBadge, which publishes here what they show; while neither
 * the bar's word nor an open phone drawer shows it, its notice at the
 * window's foot stands in.
 */
export interface OfflinePill {
  /** Changes kept on this device, waiting to be sent. */
  pending: number;
  /** The screen shown can only be read here: nothing of its is waiting. */
  readOnly: boolean;
  retrying: boolean;
  retry: () => void;
}

let pill: OfflinePill | null = null;
// The connection known lost, its mark up or put away: a phone's notice closed
// with × takes its cloud away, and a read must not wait on that without end.
let lost = false;
let bars = 0;
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

function changed() {
  for (const listener of listeners) listener();
}

/**
 * Puts the bar's "오프라인" up, or takes it away (null); `knownLost` says
 * whether the connection is lost, its mark up or put away — a mark staying
 * its moment once the connection is back (SyncStatusBadge) is no lost one.
 */
export function showOfflinePill(next: OfflinePill | null, knownLost = false) {
  if (next === pill && knownLost === lost) return;
  pill = next;
  lost = knownLost;
  changed();
}

/** Whether the connection is known lost: the bar's "오프라인" is up for it, or would be but for its notice put away. */
export function offlineShown() {
  return lost;
}

/** Hears the connection become known lost (true) or not (false); the returned function stops it. */
export function onOfflineShownChange(listener: (shown: boolean) => void) {
  let last = lost;
  return subscribe(() => {
    if (lost === last) return;
    last = lost;
    listener(last);
  });
}

/** What the bar's "오프라인" shows, or null while the connection holds. */
export function useOfflinePill() {
  return useSyncExternalStore(subscribe, () => pill, () => null);
}

/** Whether a place drawn now shows the lost connection (useOfflineMarkHost). */
export function useOfflineShownInPlace() {
  return useSyncExternalStore(subscribe, () => bars > 0, () => false);
}

/** A place that shows the lost connection while it is drawn: the top bar's "오프라인", an open phone drawer's cloud. */
export function useOfflineMarkHost(active: boolean) {
  useEffect(() => {
    if (!active) return undefined;
    bars += 1;
    changed();
    return () => {
      bars -= 1;
      changed();
    };
  }, [active]);
}
