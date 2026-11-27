import { api } from "@/api";
import type { Session } from "@/types";
import { useEffect, useState } from "react";

const none: ReadonlySet<string> = new Set();

/** A held database's key among the held pages: its trash line's own key. */
export function heldDatabaseKey(blockId: string) {
  return `database:${blockId}`;
}

/**
 * The pages in a workspace's trash that an active legal hold keeps from
 * deletion for good, read for whoever may delete forever (the workspace's
 * owners) as the trash or a trashed page's band opens, so they say it before
 * the press. Empty until the server answers, or when it can't — the deletion
 * itself still refuses then. `again` reads anew when it changes. The held
 * databases in the trash are among them by heldDatabaseKey.
 */
export function useTrashLegalHolds(session: Session | undefined, workspaceId: string, read: boolean, again = ""): ReadonlySet<string> {
  const [held, setHeld] = useState(none);
  useEffect(() => {
    if (!read || !workspaceId || !session) {
      setHeld(none);
      return;
    }
    const controller = new AbortController();
    api.readTrashLegalHolds(session, workspaceId, controller.signal).then((answer) => {
      if (controller.signal.aborted) return;
      const keys = [...answer.pageIds, ...(answer.databaseIds ?? []).map(heldDatabaseKey)];
      setHeld(keys.length ? new Set(keys) : none);
    }, () => undefined);
    return () => controller.abort();
  }, [session, workspaceId, read, again]);
  return held;
}
