import { useLayoutEffect, useSyncExternalStore } from "react";
import type { TopBarLockProps } from "./TopBarLock";

export type BandLock = Omit<TopBarLockProps, "className" | "pageId">;

/** A store's change count, its readers told of each change. */
function changes() {
  const listeners = new Set<() => void>();
  let version = 0;
  return {
    changed() { version += 1; for (const listener of listeners) listener(); },
    subscribe: (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
    read: () => version,
  };
}

// The pages whose band is drawn now (PageTopBands, on a phone), which their
// top bars' locks read, and the lock each top bar hands its band, which the
// band reads: apart, so a bar handing its lock on each draw draws no bar again.
export const bands = new Map<string, number>(), bandChanges = changes();
export const locks = new Map<string, BandLock>(), lockChanges = changes();

/**
 * Whether a phone's band under the top bar draws this page's lock in place of
 * the bar's "잠금" (TopBarLock), which hands it the lock as it would draw it.
 */
export function usePhoneLockBand(pageId: string | undefined, lock: BandLock): boolean {
  useSyncExternalStore(bandChanges.subscribe, bandChanges.read, bandChanges.read);
  const banded = Boolean(pageId && bands.get(pageId));
  useLayoutEffect(() => {
    if (!pageId || !banded) return undefined;
    locks.set(pageId, lock);
    lockChanges.changed();
    return () => {
      if (locks.get(pageId) !== lock) return;
      locks.delete(pageId);
      lockChanges.changed();
    };
  });
  return banded;
}

