import { useEffect, useRef, type Dispatch, type SetStateAction } from "react";
import { LeafenApiError, request } from "@/apiTransport";
import { graphWireBounds, type ContentGraph } from "@/contentGraph";
import {
  collectAuthenticatedContentGraph,
  collectExternalGraphChanges,
  type ExternalSince,
} from "@/lib/authenticatedContentGraph";
import {
  externalChangesShowOtherSources,
  externalDataSourceIds,
  mergeExternalContentGraph,
  withExternalChanges,
} from "@/lib/externalContentGraph";
import { onWindowReturn } from "@/lib/windowReturn";
import { afterOwnWrites } from "@/lib/ownWritesInFlight";
import { onGraphReaderWord } from "@/lib/graphReaderEvents";
import type { Session } from "@/types";
import { withCopiesOnTheirWay } from "@/lib/copiesOnTheirWay";

type ExternalPage = { pageId: string; revision: number; inTrash?: boolean };
type ExternalRevisions = { pages: ExternalPage[] };

const EXTERNAL_REFRESH_INTERVAL_MS = 20_000;

/**
 * The pages whose changes moved on, each with the position the page showed,
 * when only that happened; null when a page came or left, went to the trash
 * or came back, or its position went back, or more pages moved on than one
 * read takes the changes of (graphWireBounds.externalPages, as the server
 * holds), and the page is read whole (2026-11-25: 101 pages that all changed
 * between two checks were asked for, refused, and asked again every 20
 * seconds, the old values showing till the page was reopened).
 */
function pagesMovedOn(known: readonly ExternalPage[], now: readonly ExternalPage[]): ExternalSince[] | null {
  if (known.length !== now.length) return null;
  const moved: ExternalSince[] = [];
  for (const [index, page] of now.entries()) {
    const before = known[index];
    if (before.pageId !== page.pageId || Boolean(before.inTrash) !== Boolean(page.inTrash) || page.revision < before.revision) return null;
    if (page.revision > before.revision) moved.push({ pageId: page.pageId, since: before.revision });
  }
  return moved.length > 0 && moved.length <= graphWireBounds.externalPages ? moved : null;
}

/**
 * Rows from databases on other pages change there, not through this page's
 * own graph. The server tells this page's room when they do
 * (lib/graphReaderEvents.ts), and the page then asks for the latest change of
 * those pages; it also asks every twenty seconds while visible, and whenever
 * the window comes back. When only their changes moved on, it reads what
 * those changes changed of the rows it shows, when they say, and replaces
 * those rows where they stand (2026-11-25: one value typed in a source of
 * 5,000 rows had a page linking it read its whole graph, 51 windows, some
 * 8 seconds on a NAS-class server, before it drew the value). When one went
 * to the trash or came back (or a page left, as after losing access), or the
 * changes don't say, it reads the page again and takes only the other-page
 * sources from it: a source whose page is in the trash shows to read only.
 */
export function useExternalGraphRefresh({
  graph,
  pageId,
  session,
  setGraph,
}: {
  graph: ContentGraph;
  pageId: string | null;
  session: Session;
  setGraph: Dispatch<SetStateAction<ContentGraph>>;
}) {
  const watching = pageId !== null && externalDataSourceIds(graph, pageId).size > 0;
  // The graph as drawn, which a read of changes is weighed against.
  const shown = useRef(graph);
  useEffect(() => {
    shown.current = graph;
  }, [graph]);
  useEffect(() => {
    if (!pageId || !watching || typeof document === "undefined") return;
    let cancelled = false;
    let running = false;
    // A word that came while asking is asked about once that ends.
    let again = false;
    let known: ExternalPage[] | null = null;
    const stillOpen = () => !cancelled;
    const readWhole = async () => {
      const read = await collectAuthenticatedContentGraph(session, pageId, null, stillOpen);
      if (!read || cancelled) return;
      setGraph((current) => withCopiesOnTheirWay(mergeExternalContentGraph(current, read.graph, pageId)));
    };
    const readChanges = async (moved: ExternalSince[]) => {
      let read: Awaited<ReturnType<typeof collectExternalGraphChanges>>;
      try {
        read = await collectExternalGraphChanges(session, pageId, moved, stillOpen);
      } catch (error) {
        // A read of changes the server refuses as asked is read whole, not
        // asked the same again at every tick and word, the old values showing.
        if (error instanceof LeafenApiError && error.status === 400 && !cancelled) return readWhole();
        throw error;
      }
      if (!read || cancelled) return;
      if (read.changes === null) {
        const whole = read.graph;
        setGraph((current) => withCopiesOnTheirWay(mergeExternalContentGraph(current, whole, pageId)));
        return;
      }
      const { changes } = read;
      // Sources it shows now whose rows it doesn't hold come with the whole graph.
      if (externalChangesShowOtherSources(shown.current, changes, pageId)) return readWhole();
      setGraph((current) => withExternalChanges(current, changes, pageId));
    };
    const check = async () => {
      if (cancelled || document.visibilityState !== "visible") return;
      if (running) {
        again = true;
        return;
      }
      running = true;
      again = false;
      try {
        const result = await request<ExternalRevisions>(
          `/pages/${encodeURIComponent(pageId)}/content-graph/external-revisions`, {}, session,
        );
        if (known !== null && JSON.stringify(result.pages) !== JSON.stringify(known)) {
          // A row changed here and still on its way would read back unchanged (lib/ownWritesInFlight.ts).
          await afterOwnWrites();
          if (cancelled) return;
          const moved = pagesMovedOn(known, result.pages);
          await (moved ? readChanges(moved) : readWhole());
          if (cancelled) return;
        }
        if (!cancelled) known = result.pages;
      } catch {
        // The next tick asks again.
      } finally {
        running = false;
        if (again && !cancelled) void check();
      }
    };
    void check();
    const timer = window.setInterval(() => void check(), EXTERNAL_REFRESH_INTERVAL_MS);
    const stopReturn = onWindowReturn(() => void check());
    const stopWord = onGraphReaderWord("external", pageId, () => void check());
    return () => {
      cancelled = true;
      window.clearInterval(timer);
      stopReturn();
      stopWord();
    };
  }, [pageId, session, setGraph, watching]);
}
