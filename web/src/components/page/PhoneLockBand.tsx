import { useLayoutEffect, useSyncExternalStore } from "react";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { TopBarLock } from "./TopBarLock";
import { bandChanges, bands, lockChanges, locks } from "./phoneLockBandStore";
import styles from "./PhoneLockBand.module.css";

/**
 * A locked page's band on a phone, as Notion's (seen 2026-11-16 on the A90's
 * Chrome at 411, a page, a full-page database and a database's page): under
 * the top bar across the screen, 44 high on the gray ground, 8px over and 12
 * in, its "🔒 잠금" in 17px/500 gray with a 22px lock 6px before it, 10 in
 * and 14 after; unlocked for the reader, "🔓 다시 잠금" in blue. Pressed, it
 * opens the bar's own menu right under it ("잠금 해제", "모든 사용자에 대해
 * 잠금 해제"), so it is the bar's lock (TopBarLock) drawn here.
 */
export function PhoneLockBand({ pageId }: { pageId: string }) {
  const { phone } = useSidebarLayout(true);
  useSyncExternalStore(lockChanges.subscribe, lockChanges.read, lockChanges.read);
  useLayoutEffect(() => {
    if (!phone) return undefined;
    bands.set(pageId, (bands.get(pageId) ?? 0) + 1);
    bandChanges.changed();
    return () => {
      const left = (bands.get(pageId) ?? 1) - 1;
      if (left > 0) bands.set(pageId, left); else bands.delete(pageId);
      bandChanges.changed();
    };
  }, [pageId, phone]);
  const lock = phone ? locks.get(pageId) : undefined;
  if (!lock) return null;
  return (
    <div className={styles.band} data-page-lock-band={lock.unlockedForMe ? "unlocked" : "locked"}>
      <TopBarLock {...lock} className={styles.button} />
    </div>
  );
}
