import { useMemo } from "react";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { useDatabaseLock } from "./databaseStructureLock";

/**
 * A row's page opened from an inline database its reader unlocked for
 * themselves ("잠금" › "잠금 해제") reads that database unlocked too: its
 * "…" › 테마와 레이아웃 offers 목차 and 레이아웃 사용자 지정, as Notion's
 * (seen 2026-11-16 in Chrome: an inline database's lock unlocked for oneself
 * left its row page's theme menu as an unlocked one's, and was locked again
 * after a reload, as a full-page database's unlock for oneself does). The
 * server reads the database locked for everyone (the unlock lasts as long as
 * the view, InlineDatabase.tsx). A lock of the page holding it leaves its
 * properties and page layout to its own (page_lock_database_structure.go on
 * the server), unlocked for the reader there too.
 */
export function useRowPageWithDatabaseUnlocked(snapshot: DatabaseRowPageSnapshot | null) {
  const lock = useDatabaseLock();
  const unlocked = Boolean(lock && !lock.page && !lock.views && lock.locked && lock.unlockedForMe);
  return useMemo(() => (unlocked && snapshot?.databaseLocked ? { ...snapshot, databaseLocked: false, databaseStructureLocked: false } : snapshot), [snapshot, unlocked]);
}
