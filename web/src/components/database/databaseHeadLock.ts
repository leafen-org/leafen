import { useOpenedAloneDatabaseLock } from "./openedAloneDatabaseLock";

/**
 * Whether an inline database's own lock holds its head opened alone for this
 * reader — its name, icon and cover and its description's words, while
 * whether its description shows stays theirs to change (useDatabaseDescription)
 * — as a full-page database's page lock holds its head, as Notion's (seen
 * 2026-11-18 in Chrome, two tabs each: a locked database, full page or
 * inline opened alone, took no letter typed in its name or description and
 * offered "인증 추가 · 설명 숨기기", "설명 표시" once hidden). Its lock is its
 * block's "locked" (database_container_lock.go databaseLockedFor), unless
 * the reader unlocked it for themselves where it is drawn (InlineDatabase.tsx,
 * openedAloneDatabaseLock.ts); read locked otherwise, as the server reads it.
 */
export function useInlineDatabaseHeadLocked(database: { blockId: string; locked?: boolean } | null | undefined) {
  const alone = useOpenedAloneDatabaseLock();
  if (!database?.locked) return false;
  return alone?.blockId === database.blockId ? alone.lock.locked && !alone.lock.unlockedForMe : true;
}
