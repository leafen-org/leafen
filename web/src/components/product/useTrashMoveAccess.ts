import { useEffect, useRef, useState } from "react";
import { afterOwnWrites } from "@/lib/ownWritesInFlight";
import { afterTrashMoves, trashMoveOnItsWay } from "@/lib/pageTrashMoves";

/**
 * The page open whose access isn't known for a move to or out of the trash,
 * if any: until then it is taken as one to write in, asking for nothing.
 *
 * A page in the trash reads only, its banner saying why; one moved to it or
 * restored from it is read again, once, so a restored one may be changed
 * at once. The read waits for this tab's own move to land: made at the
 * press, it answered as the page was before it (2026-11-13: a restore read
 * the page twice in the trash and once more after it). Until that read ends,
 * and for a second at least, an answer to a read made while it was in the
 * trash isn't its own: one came back read-only after the restore and showed
 * its owner "편집 권한 요청" for 300 ms, another, after the read again, for a
 * frame (2026-11-12).
 *
 * A page opened with its move on its way — the trash window's restore opens
 * it as the window closes — is read once the move has landed (`readSettled`
 * below), and what its held or cached copy was kept with isn't its own until
 * that read ends: it may be the trash's (2026-11-15: the owner saw "편집 권한
 * 요청" and what was typed went nowhere until the page was opened again).
 */
export function useTrashMoveAccess(input: {
  pageId: string | null;
  trashed: boolean;
  /** The page's own read ended showing it (useLocalFirstContentGraph's onRead). */
  readPageId: string | null;
  reread(pageId: string): Promise<boolean>;
}): string | null {
  const { pageId, readPageId, reread, trashed } = input;
  const seenTrash = useRef<{ pageId: string | null; trashed: boolean }>({ pageId: null, trashed: false });
  const [unknown, setUnknown] = useState<{ pageId: string; until: "read" | "readAgain" } | null>(null);
  useEffect(() => {
    const before = seenTrash.current;
    seenTrash.current = { pageId, trashed };
    if (!pageId) return;
    if (before.pageId !== pageId) {
      if (!trashed && trashMoveOnItsWay(pageId)) setUnknown({ pageId, until: "read" });
      return;
    }
    if (before.trashed === trashed) return;
    if (!trashed) setUnknown({ pageId, until: "readAgain" });
    const read = afterOwnWrites().then(() => seenTrash.current.pageId === pageId && seenTrash.current.trashed === trashed
      ? reread(pageId).catch(() => false) : false);
    if (trashed) return;
    void Promise.all([read, new Promise((settle) => window.setTimeout(settle, 1_000))])
      .then(() => setUnknown((current) => current?.pageId === pageId && current.until === "readAgain" ? null : current));
  }, [pageId, reread, trashed]);
  useEffect(() => {
    if (unknown?.until === "read" && unknown.pageId === readPageId) setUnknown(null);
  }, [readPageId, unknown]);
  return unknown?.pageId ?? null;
}

type ReadSettled = <Value>(pageId: string, read: () => Promise<Value | null>, here?: boolean) => Promise<Value | null>;

/** A page's own read, made once the moves to or out of the trash made here of it have landed. */
export function readAfterTrashMoves(readSettled: ReadSettled): ReadSettled {
  return (pageId, read, here) => readSettled(pageId, () => afterTrashMoves(pageId).then(read), here);
}
