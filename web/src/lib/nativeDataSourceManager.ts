import type { ProductStatusSink } from "@/lib/productStatus";
import i18next from "i18next";
import { api } from "@/api";
import { userErrorText } from "@/lib/settingsErrors";
import type { ContentGraph, GraphBlock, GraphOperation, GraphView } from "@/contentGraph";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { readSourcePages } from "@/lib/databaseSourceReading";
import type { DatabaseDataSourceMoveBackRequest, DatabaseDataSourceMoveRequest } from "@/databaseDataSourceWire.generated";
import { LeafenApiError } from "@/apiTransport";
import { searchWorkspacePages } from "@/lib/workspacePageSearch";
import { readDatabaseStarterSourceViews } from "@/databaseStarterApi";
import type { Page, Session } from "@/types";
import {
  addDataSourceOperations,
  adoptSourceViewOperations,
  linkDataSourceOperations,
  switchViewDataSourceOperations,
  unlinkDataSourceOperations,
} from "@/lib/nativeDataSourceManagement";

// A page's manager of its databases' sources: it writes the membership
// changes nativeDataSourceManagement.ts builds and rereads the page after
// each, and asks the server to move, trash or restore an own source and to
// list the sources and pages to link.

/** A move's state, and where it went with the table view it made there (which its undoing takes away). */
export type DataSourceMoved = { status: "moved" | "moving"; databaseId?: string; viewId?: string };

export type NativeDataSourceManager = {
  add(block: GraphBlock, labels: { title: string; titleProperty: string; view: string; id?: string; viewId?: string }): Promise<boolean>;
  link(block: GraphBlock, candidate: DatabaseContainerSource, starter?: { viewId: string; name: string }): Promise<boolean>;
  switchView(block: GraphBlock, viewId: string, candidate: DatabaseContainerSource, defaults: Record<string, unknown>): Promise<boolean>;
  unlink(block: GraphBlock, dataSourceId: string): Promise<boolean>;
  move(dataSourceId: string, target: DatabaseDataSourceMoveRequest): Promise<DataSourceMoved | null>;
  /** Undoes a move of the source alone; "refused" when the databases no longer allow it. */
  moveBack(dataSourceId: string, back: DatabaseDataSourceMoveBackRequest): Promise<"moved" | "moving" | "refused" | null>;
  trash(dataSourceId: string): Promise<boolean>;
  /** Takes a source just sent to the trash back, as the notice's "실행 취소". */
  restore(dataSourceId: string): Promise<boolean>;
  loadCandidates(query: string, signal: AbortSignal, upTo?: number): Promise<DatabaseContainerSource[]>;
  /** A source's views in its own database, page by page. */
  loadSourceViews(candidate: DatabaseContainerSource, cursor: string | undefined, signal: AbortSignal): Promise<{ views: GraphView[]; nextCursor?: string }>;
  adoptView(viewId: string, saved: GraphView): Promise<boolean>;
  loadPages(query: string, signal: AbortSignal): Promise<Page[]>;
  reread(): Promise<boolean>;
};

export function createNativeDataSourceManager({
  graph,
  pageId,
  workspaceId,
  session,
  persistGraphOperations,
  rereadGraph,
  setMessage,
}: {
  graph: ContentGraph;
  pageId: string;
  workspaceId: string;
  session: Session;
  persistGraphOperations(pageId: string, operations: GraphOperation[]): Promise<boolean>;
  rereadGraph(pageId: string): Promise<boolean>;
  setMessage: ProductStatusSink;
}): NativeDataSourceManager {
  // The server records each database's membership; the page is reread after
  // every change so the membership and views shown match it.
  async function write(operations: GraphOperation[] | null) {
    if (!operations || !await persistGraphOperations(pageId, operations)) return false;
    await rereadGraph(pageId);
    return true;
  }
  return {
    reread: () => rereadGraph(pageId),
    add: (block, labels) => write(addDataSourceOperations(block, labels.title, labels.titleProperty, labels.view, labels.id, graph, labels.viewId)),
    link: (block, candidate, starter) => write(linkDataSourceOperations(block, graph, candidate, starter)),
    switchView: (block, viewId, candidate, defaults) => write(switchViewDataSourceOperations(block, graph, viewId, candidate, defaults)),
    unlink: (block, dataSourceId) => write(unlinkDataSourceOperations(block, graph, dataSourceId)),
    async move(dataSourceId, target) {
      try {
        const { status, databaseId, viewId } = await api.moveDatabaseDataSource(session, pageId, dataSourceId, target);
        await rereadGraph(pageId);
        return { status: status === "moving" ? "moving" : "moved", databaseId, viewId };
      } catch (error) {
        setMessage(userErrorText(error, i18next.t("databaseView:toast.couldntMoveDataSource")), "error");
        return null;
      }
    },
    async moveBack(dataSourceId, back) {
      try {
        const { status } = await api.moveDatabaseDataSourceBack(session, pageId, dataSourceId, back);
        await rereadGraph(pageId);
        return status === "moving" ? "moving" : "moved";
      } catch (error) {
        if (error instanceof LeafenApiError && error.status >= 400 && error.status < 500) return "refused";
        setMessage(userErrorText(error, i18next.t("databaseView:toast.couldntMoveDataSource")), "error");
        return null;
      }
    },
    async trash(dataSourceId) {
      try {
        await api.trashDatabaseDataSource(session, pageId, dataSourceId);
        await rereadGraph(pageId);
        return true;
      } catch (error) {
        setMessage(userErrorText(error, i18next.t("databaseView:toast.couldntTrashDataSource")), "error");
        return false;
      }
    },
    async restore(dataSourceId) {
      try {
        await api.restoreDatabaseDataSource(session, pageId, dataSourceId);
        await rereadGraph(pageId);
        return true;
      } catch (error) {
        setMessage(userErrorText(error, i18next.t("databaseView:toast.couldntRestoreDataSource")), "error");
        return false;
      }
    },
    // The sources to link, a page or with `upTo` that many (readSourcePages).
    loadCandidates: async (query, signal, upTo) => (await readSourcePages((cursor, limit) => api.databaseContainerSources(
      session, workspaceId, query, { cursor, limit: limit ?? 25, signal }), { upTo })).items,
    loadSourceViews: (candidate, cursor, signal) => readDatabaseStarterSourceViews(session, candidate.id, candidate.dataSourceId, cursor, signal),
    adoptView: (viewId, saved) => write(adoptSourceViewOperations(graph, viewId, saved)),
    async loadPages(query, signal) {
      const response = await searchWorkspacePages(session, query, 10, workspaceId, { signal });
      return response.pages;
    },
  };
}
