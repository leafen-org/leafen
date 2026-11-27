import { useCallback, useEffect, useState } from "react";
import { request } from "@/apiTransport";
import {
  databaseDataSourceOperations,
  type DeletedDatabase,
  type DeletedDatabaseList,
  type DeletedDatabaseRestored,
} from "@/databaseDataSourceWire.generated";
import type { Session } from "@/types";
import { untitledDatabaseSourcesName, type DatabaseSourceTitle } from "@/components/editor/databaseTitles";
import { untitledPageDisplayTitle } from "./pageTitle";
import { isServerUnreachable } from "./serverReach";
import { useReadAgainWhenAnswered } from "./unansweredRead";

// The databases in the trash (deleted_databases.go): a deleted inline
// database's block, or one turned into a simple table, with its rows kept.

function path(template: string, values: Record<string, string>) {
  return Object.entries(values).reduce(
    (current, [key, value]) => current.replace(`{${key}}`, encodeURIComponent(value)),
    template,
  );
}

/**
 * A database's name in the trash, as the server names a database everywhere
 * (deleted_database_line_names.go) and its pages' paths show it: its own
 * name, or its sources' names listed while it has none, a new database's
 * while it has neither. A database's line passes its `title`, a source kept
 * in its database that database's `databaseTitle`; `sourceTitles` are the
 * database's either way.
 */
export function deletedDatabaseName(name: string | undefined, sourceTitles: readonly DatabaseSourceTitle[] | undefined): string {
  return name?.trim() || (sourceTitles && sourceTitles.length > 1 ? untitledDatabaseSourcesName(sourceTitles) : "")
    || untitledPageDisplayTitle("database");
}

export function restoreDeletedDatabase(session: Session, workspaceId: string, blockId: string) {
  return request<DeletedDatabaseRestored>(
    path(databaseDataSourceOperations.restoreDeletedDatabase.path, { workspaceId, blockId }),
    { method: databaseDataSourceOperations.restoreDeletedDatabase.method, body: "{}" },
    session,
  );
}

export async function deleteDeletedDatabase(session: Session, workspaceId: string, blockId: string) {
  await request<unknown>(
    path(databaseDataSourceOperations.deleteDeletedDatabase.path, { workspaceId, blockId }),
    { method: databaseDataSourceOperations.deleteDeletedDatabase.method, body: "{}" },
    session,
  );
}

/**
 * The workspace's databases in the trash, read as the trash opens and after
 * each change, a window at a time to the last, each shown as it comes. A
 * read the server wasn't there for says so (`unanswered`) and is read again
 * once it may answer.
 */
export function useDeletedDatabases(session: Session, workspaceId: string) {
  const [items, setItems] = useState<DeletedDatabase[]>([]);
  const [generation, setGeneration] = useState(0);
  const [unanswered, setUnanswered] = useState(false);
  const reload = useCallback(() => setGeneration((value) => value + 1), []);
  useReadAgainWhenAnswered(unanswered, reload);
  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      let read: DeletedDatabase[] = [];
      let cursor = "";
      do {
        const list = await request<DeletedDatabaseList>(
          path(databaseDataSourceOperations.deletedDatabases.path, { workspaceId })
            + (cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""),
          { method: databaseDataSourceOperations.deletedDatabases.method, signal: controller.signal },
          session,
        );
        if (controller.signal.aborted) return;
        read = [...read, ...list.items];
        setItems(read);
        cursor = list.nextCursor ?? "";
      } while (cursor);
      setUnanswered(false);
    })().catch((error: unknown) => {
      if (!controller.signal.aborted) setUnanswered(isServerUnreachable(error));
    });
    return () => controller.abort();
  }, [generation, session, workspaceId]);
  return { items, reload, unanswered };
}
