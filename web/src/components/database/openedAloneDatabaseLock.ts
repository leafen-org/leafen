import { useSyncExternalStore } from "react";
import type { DatabaseLockControls } from "./databaseStructureLock";

// An inline database opened alone shows its lock after the path in the top
// bar, as Notion's does (seen 2026-11-11: "〈page〉 / 〈database〉 🔒 잠금",
// none in its toolbar), as a full-page database's page lock shows. The
// database owns the lock; the top bar reads it here.

type OpenedAloneLock = { blockId: string; lock: DatabaseLockControls };

let current: OpenedAloneLock | null = null;
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** Says, or with null stops saying, the lock of the database opened alone. */
export function publishOpenedAloneDatabaseLock(next: OpenedAloneLock | null) {
  if (next === null && current === null) return;
  current = next;
  for (const listener of listeners) listener();
}

/** Takes back what this database said, if it is still what is said. */
export function withdrawOpenedAloneDatabaseLock(blockId: string) {
  if (current?.blockId === blockId) publishOpenedAloneDatabaseLock(null);
}

export function useOpenedAloneDatabaseLock() {
  return useSyncExternalStore(subscribe, () => current, () => null);
}
