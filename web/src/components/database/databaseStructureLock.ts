import { createContext, useContext } from "react";

// As in Notion, a locked database keeps its rows editable and its views and
// properties as they are: inside it, the controls that would change a view
// or a property are hidden or off.
export const DatabaseStructureLockContext = createContext(false);

export function useDatabaseStructureLocked() {
  return useContext(DatabaseStructureLockContext);
}

// A linked database's "보기 잠금" locks its views, not its source: its
// properties and data source settings stay editable (Notion's, seen
// 2026-10-13). Unset, the source locks with the structure.
export const DatabaseSourceLockContext = createContext<boolean | null>(null);

export function useDatabaseSourceLocked() {
  const source = useContext(DatabaseSourceLockContext);
  const structure = useContext(DatabaseStructureLockContext);
  return source ?? structure;
}

/**
 * A database's lock, as Notion's (seen 2026-10-01): the view settings'
 * "데이터베이스 잠금" locks it for everyone; locked, an inline database's
 * toolbar shows "잠금", which unlocks it for the reader alone or for
 * everyone, and unlocked for the reader it reads "다시 잠금". A database's
 * own page locks with its page.
 */
export type DatabaseLockControls = {
  locked: boolean;
  /**
   * The lock is its page's, as a database's own page locks (Notion's, seen
   * 2026-10-01): the page's top bar shows "잠금", not the database's toolbar.
   */
  page?: boolean;
  /**
   * The database is opened alone, so its lock shows in the top bar after the
   * path (openedAloneDatabaseLock.ts), not in its toolbar.
   */
  topBar?: boolean;
  /**
   * The lock is a linked database's "보기 잠금", as Notion names it for a
   * database shown from another place (seen 2026-10-13).
   */
  views?: boolean;
  unlockedForMe: boolean;
  lock?(): void;
  unlockForEveryone?(): void;
  unlockForMe?(): void;
  relock(): void;
};

export const DatabaseLockContext = createContext<DatabaseLockControls | null>(null);

export function useDatabaseLock() {
  return useContext(DatabaseLockContext);
}
