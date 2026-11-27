import { useCallback, useEffect, useState } from "react";
import { api } from "@/api";
import { lockForMeShownFirst } from "@/lib/lockForMeShownFirst";
import { heldInAnotherTab, unlockedInThisTab } from "@/lib/pageUnlockSession";
import { noteOwnLockFlip, ownLockFlip } from "@/lib/ownLockFlips";
import { useShowPageLockedForMe } from "@/lib/pageLockedForMe";
import type { Page, Session } from "@/types";

/**
 * The selected page's lock as it applies to this person: a locked page takes
 * no edits in the app unless they unlocked it for themselves. The personal
 * unlock is read when the page is locked, lasts as long as the tab that made
 * it (pageUnlockSession.ts), and ends when anyone locks or unlocks the page
 * for everyone. Changing it shows at the press (lockForMeShownFirst.ts).
 */
export function usePageLockState(session: Session | null, page: Page | null) {
  const pageId = page?.id ?? "";
  const locked = Boolean(page?.isLocked);
  const [unlockedForMe, setUnlockedForMe] = useState(false);

  // A renewed sign-in reads the unlock again and shows the one known meanwhile:
  // the page shown locked would let go text its room still takes (lib/refusedTextLetGo.ts).
  useEffect(() => setUnlockedForMe(false), [locked, pageId]);
  useEffect(() => {
    if (!session || !pageId || !locked) return;
    // Locked from this tab for everyone, which ended every personal unlock:
    // nothing to read back (lib/ownLockFlips.ts).
    if (ownLockFlip(pageId)) return;
    const controller = new AbortController();
    api.pageLock(session, pageId, { signal: controller.signal })
      .then(async (state) => {
        // Changed here meanwhile: what was pressed shows, not what was read before it.
        if (controller.signal.aborted || ownLockFlip(pageId)) return;
        if (!state.locked || !state.unlockedForMe || unlockedInThisTab(pageId)) {
          setUnlockedForMe(state.locked && state.unlockedForMe);
          return;
        }
        // Unlocked in a tab since closed or reloaded: locked again here, and
        // for the person too unless another tab still holds it open.
        setUnlockedForMe(false);
        if (!(await heldInAnotherTab(pageId)) && !controller.signal.aborted) await api.setPageLockForMe(session, pageId, false);
      })
      .catch(() => {});
    return () => controller.abort();
  }, [locked, pageId, session]);

  const changeForMe = useCallback(async (unlocked: boolean) => {
    if (!session || !pageId) return;
    noteOwnLockFlip(pageId);
    await lockForMeShownFirst(pageId, unlocked, {
      show: setUnlockedForMe,
      // Locking again here leaves another tab's unlock standing.
      write: async () => (!unlocked && await heldInAnotherTab(pageId) ? null : api.setPageLockForMe(session, pageId, unlocked)),
      settle: (state) => { if (state) setUnlockedForMe(state.locked && state.unlockedForMe); },
    });
  }, [pageId, session]);

  // Its databases' rows' own pages read it (pageLockedForMe.ts).
  useShowPageLockedForMe(pageId, locked && !unlockedForMe);
  return { lockedForMe: locked && !unlockedForMe, unlockedForMe, changeForMe };
}
