import { useEffect, useState } from "react";
import { createRelationDatabaseSearch } from "@/lib/relationDatabaseSearch";
import type { RelationDatabaseOption } from "@/lib/relationPropertyCreationModel";
import { pageSearchRank } from "@/lib/pageSearchRank";
import { searchWorkspacePages } from "@/lib/workspacePageSearch";
import { foldNfcText } from "@/shared/database/natural-order.mjs";
import type { Page, Session } from "@/types";
import { MOVE_SEARCH_DEBOUNCE_MS, mergeMoveRemotePages } from "./pageMoveDialogModel";
import { useMoveSearchWindow } from "./moveSearchWindow";

type MoveTitleSearchState = {
  complete: boolean;
  queryKey: string;
  workspaceId: string;
};

export type MoveDatabaseResult = Pick<RelationDatabaseOption, "id" | "title" | "pageId" | "containerId" | "mark" | "sourceTitles" | "databaseTitle"> & {
  /** How well its name answers the search (pageSearchRank): where it stands among the pages found (rankedAmongPages). */
  score: number;
};

/**
 * The searches behind "옮기기": the listed workspace's pages by title, which
 * the list adds to the pages it has, and the databases a page may move into
 * as a database's page, searched only where databaseWorkspaceId names a
 * workspace (a copy to another workspace offers none). A database is found
 * by its own name alone, as a page is (pageSearchRank): one only the pages
 * above it name is no answer, as Notion's move window finds none (seen twice
 * 2026-11-16: "옮길" lists the database "… 옮길 표", not "… 아무 표" in the
 * page "… 옮길 곳"), where the lists of sources to link find it by its place
 * too (database_container_picker_place.go).
 */
export function useMoveSearches({
  session,
  query,
  enabled,
  workspaceId,
  databaseWorkspaceId,
}: {
  session: Session;
  query: string;
  enabled: boolean;
  workspaceId: string;
  databaseWorkspaceId: string;
}) {
  const [remotePagesById, setRemotePagesById] = useState<Record<string, Page>>({});
  const [titleSearchState, setTitleSearchState] = useState<MoveTitleSearchState | null>(null);
  const [databases, setDatabases] = useState<MoveDatabaseResult[]>([]);
  // Twenty answers shown, twenty more at each "더 불러오기" (moveSearchWindow.tsx).
  const { shown, loadMore, remoteLimit } = useMoveSearchWindow(query);

  useEffect(() => {
    const trimmedQuery = query.trim();
    if (!enabled || !trimmedQuery || !workspaceId) {
      setTitleSearchState(null);
      return;
    }
    let cancelled = false;
    const controller = new AbortController();
    setTitleSearchState(null);
    const timer = window.setTimeout(() => {
      void searchWorkspacePages(session, trimmedQuery, remoteLimit, workspaceId, { signal: controller.signal, places: true })
        .then((result) => {
          if (cancelled) return;
          setRemotePagesById((current) => mergeMoveRemotePages(current, result.pages, workspaceId));
          setTitleSearchState({
            complete: result.hasMore !== true,
            queryKey: foldNfcText(trimmedQuery),
            workspaceId,
          });
        })
        .catch(() => {
          if (cancelled || controller.signal.aborted) return;
          setTitleSearchState({
            complete: false,
            queryKey: foldNfcText(trimmedQuery),
            workspaceId,
          });
        });
    }, MOVE_SEARCH_DEBOUNCE_MS);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [enabled, query, remoteLimit, session, workspaceId]);

  useEffect(() => {
    const trimmedQuery = query.trim();
    if (!enabled || !trimmedQuery || !databaseWorkspaceId) {
      setDatabases([]);
      return;
    }
    const controller = new AbortController();
    const search = createRelationDatabaseSearch(session, databaseWorkspaceId, { addsRows: true });
    const timer = window.setTimeout(() => {
      search(trimmedQuery, controller.signal).then(
        (found) => {
          if (controller.signal.aborted) return;
          const folded = foldNfcText(trimmedQuery);
          const rank = (name?: string) => name ? pageSearchRank(foldNfcText(name), folded) : Number.POSITIVE_INFINITY;
          setDatabases(found.flatMap(({ id, title, pageId, containerId, mark, sourceTitles, databaseTitle }) => {
            const score = Math.min(rank(title), rank(databaseTitle));
            return score < Number.POSITIVE_INFINITY ? [{ id, title, pageId, containerId, mark, sourceTitles, databaseTitle, score }] : [];
          }));
        },
        () => { if (!controller.signal.aborted) setDatabases([]); },
      );
    }, MOVE_SEARCH_DEBOUNCE_MS);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [databaseWorkspaceId, enabled, query, session]);

  const titleSearchComplete = titleSearchState?.complete === true
    && titleSearchState.queryKey === foldNfcText(query.trim())
    && titleSearchState.workspaceId === workspaceId;
  return { remotePagesById, titleSearchComplete, databases, shown, loadMore };
}
