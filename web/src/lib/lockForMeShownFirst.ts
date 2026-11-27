import type { Dispatch, SetStateAction } from "react";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { noteUnlockedInThisTab } from "./pageUnlockSession";

/**
 * Unlocking a locked page for oneself ("잠금 해제") or locking it again for
 * oneself ("다시 잠금") shows at the press, as Notion's does (measured
 * 2026-11-16 in Chrome: a page's and a database page's "잠금" turned in 48 to
 * 163 ms, their requests ending 240 to 1,700 ms after). The server is told
 * after, and one it refuses goes back to what showed before the press, the
 * caller saying so (user decision 2026-11-12: a press answers on the next
 * frame, the server is reconciled last). Of presses made on one page before
 * the server answered, the last one decides what shows. Every place that
 * offers it goes through here: a page's (usePageLockState.ts) and a database
 * page's (databaseRowPageService.ts, StandaloneRowPage.tsx,
 * DatabaseTemplateEditor.tsx), from the top bar, a phone's lock band and the
 * phone's "페이지 사용자 지정" sheet alike.
 */

// Each page's last press, numbered across pages so a number is never reused.
const presses = new Map<string, number>();
let pressed = 0;

export type LockForMeChange<T> = {
  /** Shows the page unlocked for the person, or locked again for them. */
  show(unlocked: boolean): void;
  /** Tells the server. */
  write(): Promise<T>;
  /** What the server answered, while this is still the page's last press. */
  settle?(answer: T): void;
};

/** Changes the person's own unlock of `pageId`, shown first; true once the server has it and no later press came. */
export async function lockForMeShownFirst<T>(pageId: string, unlocked: boolean, change: LockForMeChange<T>) {
  const press = ++pressed;
  presses.set(pageId, press);
  const last = () => presses.get(pageId) === press;
  // The tab's own unlock lasts as long as the tab (pageUnlockSession.ts).
  const show = (value: boolean) => {
    noteUnlockedInThisTab(pageId, value);
    change.show(value);
  };
  show(unlocked);
  try {
    const answer = await change.write();
    if (last()) change.settle?.(answer);
    return last();
  } catch (error) {
    if (last()) show(!unlocked);
    throw error;
  } finally {
    if (last()) presses.delete(pageId);
  }
}

/** A database page's snapshot showing it unlocked for the person, or locked again for them. */
export function withUnlockedForMe<S extends { row: { locked: boolean; unlockedForMe: boolean } }>(snapshot: S, unlocked: boolean): S {
  const unlockedForMe = snapshot.row.locked && unlocked;
  return snapshot.row.unlockedForMe === unlockedForMe ? snapshot : { ...snapshot, row: { ...snapshot.row, unlockedForMe } };
}

/** The same for a database page drawn from its snapshot held as a component's state. */
export function lockForMeShownFirstIn(
  setSnapshot: Dispatch<SetStateAction<DatabaseRowPageSnapshot | null>>, rowId: string, unlocked: boolean, write: () => Promise<unknown>,
) {
  return lockForMeShownFirst(rowId, unlocked, {
    show: (value) => setSnapshot((current) => (current?.row.id === rowId ? withUnlockedForMe(current, value) : current)),
    write,
  });
}
