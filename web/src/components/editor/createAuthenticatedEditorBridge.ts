import type { ProductStatusSink } from "@/lib/productStatus";
import type { Dispatch, SetStateAction } from "react";
import type { ContentGraph, GraphOperation } from "@/contentGraph";
import { upsertBootstrapPage } from "@/lib/bootstrapPageProjection";
import type { Bootstrap, Page, Session } from "@/types";
import { withMadeDatabase } from "./databaseSimpleTable";
import { applyEditorBlockChanges } from "./editorGraphModel";
import type { EditorCommit, EditorGraphSync, EditorMoveContext } from "./editorRuntimeTypes";

type GraphWriter = {
  commit: (pageId: string, operations: GraphOperation[]) => Promise<boolean>;
  holding: <T>(pageId: string, task: () => Promise<T>) => Promise<T>;
  queue: (pageId: string, operations: GraphOperation[], soon?: boolean) => void;
  reread: (pageId: string) => Promise<boolean>;
};

export function createAuthenticatedEditorBridge({
  graphWrites,
  onDataSourceMade,
  pageId,
  pages,
  refreshPages,
  session,
  setData,
  setGraph,
  setMessage,
}: {
  graphWrites: GraphWriter;
  /** A database made in the editor, whose settings are read only once it is saved. */
  onDataSourceMade?: (dataSourceId: string) => void;
  pageId: string;
  pages: Page[];
  /** Reads the page list again. */
  refreshPages?: () => Promise<unknown>;
  session: Session;
  setData: Dispatch<SetStateAction<Bootstrap>>;
  setGraph: Dispatch<SetStateAction<ContentGraph>>;
  setMessage: ProductStatusSink;
}): { graphSync: EditorGraphSync; moveContext: EditorMoveContext; onGraphMutation: EditorCommit } {
  return {
    graphSync: {
      flush: () => graphWrites.commit(pageId, []),
      reread: () => graphWrites.reread(pageId),
      place: (blockId, { parentId, afterId }) => graphWrites.commit(pageId, [{ kind: "moveBlock", move: { blockId, parentId, afterId } }]),
      holding: (task) => graphWrites.holding(pageId, task),
    },
    moveContext: {
      pages,
      session,
      onPageChange: (page) => setData((current) => upsertBootstrapPage(current, page)),
      onPagesChanged: refreshPages,
      onStatus: setMessage,
    },
    onGraphMutation(nextGraph, operations, change) {
      for (const operation of operations) if (operation.kind === "upsertDataSource") onDataSourceMade?.(operation.dataSource.id);
      setGraph((current) => withMadeDatabase(applyEditorBlockChanges(current, nextGraph, operations), nextGraph, operations));
      graphWrites.queue(pageId, [...operations], change?.reshapes);
    },
  };
}
