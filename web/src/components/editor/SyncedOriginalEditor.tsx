import { useContext, useEffect, useMemo, useRef, useState } from "react";
import { useOpenPageGraphStore, usePageGraph, usePageGraphStore } from "@/lib/pageGraphStore";
import { useProductPages, type ProductPages } from "@/components/product/productPagesContext";
import { PageBodyDatabasePicker } from "@/components/product/PageBodyDatabasePicker";
import { usePageBodyEditing } from "@/components/product/usePageBodyEditing";
import { StructuredEditor } from "@/StructuredEditor";
import type { Session } from "@/types";
import { blockItemText } from "./blockItemLabels";
import styles from "./editorStyles.generated.module.css";
import { SyncedOriginalTrail } from "./syncedBlockContext";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { registerSyncedOriginalShown, type SyncedOriginalSide } from "./syncedCopyPlaces";

const noPages: ProductPages = { pages: [], mentionPages: [], setData: () => {} };
const noStatus = () => {};

/**
 * A copy of a synced block on another page edits its original's content in
 * place, with the page editor of the original's page and under that page's
 * permission, as Notion edits a synced block from every page it is in.
 * Blocks move by keys across the copy's edge between the two pages
 * (syncedCopyMove).
 */
export function SyncedOriginalEditor({ session, pageId, blockId, workspaceId, readOnly, copy, wholePage = false }: {
  session: Session;
  /** The original's page. */
  pageId: string;
  /** The original: a block, or the page itself when it shows the page's whole body. */
  blockId: string;
  /**
   * Whether it shows the page's whole body: its editor then draws the page's
   * top level, inside the copy as a nested editor does, none of the page's
   * own (its title's caret, starter, outline, presence) coming with it.
   */
  wholePage?: boolean;
  workspaceId: string;
  readOnly: boolean;
  /** The copy that shows it, and the copy's page as its editor has it. */
  copy?: { blockId: string; page(): NativeEditorGraphAdapter };
}) {
  const product = useProductPages() ?? noPages;
  const trail = useContext(SyncedOriginalTrail);
  // The way in from the open page, which a copy standing on it starts.
  const openPage = trail.length ? null : copy?.page().pageId ?? null;
  const inner = useMemo(() => [...trail.length ? trail : openPage ? [openPage] : [], blockId], [trail, openPage, blockId]);
  // Held apart, so blocks crossing the copy's edge show where they go at the press (syncedCopyMove).
  const store = usePageGraphStore();
  const graph = usePageGraph(store);
  const setGraph = store.set;
  const copyStore = useOpenPageGraphStore();
  const [databasePickerOpen, setDatabasePickerOpen] = useState(false);
  const page = useMemo(() => ({ id: pageId, workspaceId }), [pageId, workspaceId]);
  const body = usePageBodyEditing({
    bootstrapRevision: 0, graph, setGraph, onStatus: product.onStatus ?? noStatus, page,
    pages: product.pages, mentionPages: product.mentionPages, organizationId: product.organizationId,
    refreshPages: product.refreshPages, session, setData: product.setData, setDatabasePickerOpen, workspaceId,
    mutationOutbox: product.mutationOutbox,
  });
  const canEdit = !readOnly && body.graphCanEdit;
  const side = useRef<SyncedOriginalSide | null>(null);
  side.current = {
    pageId, workspaceId, session, graph, graphSync: body.editorBridge?.graphSync, readOnly: !canEdit,
    collaborationDocument: body.collaboration.document, store, copyStore,
  };
  const copyPageId = copy?.page().pageId, copyId = copy?.blockId;
  useEffect(() => copyPageId && copyId ? registerSyncedOriginalShown(copyPageId, copyId, () => side.current!) : undefined, [copyId, copyPageId]);
  const moveContext = body.editorBridge && copy ? { ...body.editorBridge.moveContext, syncedCopy: copy } : body.editorBridge?.moveContext;
  if (wholePage ? body.graphUnavailable : !graph.blocks.some((block) => block.id === blockId)) {
    return body.graphLoading ? null : <div className={styles.syncedMissing}>{blockItemText("synced.unavailable")}</div>;
  }
  return (
    <SyncedOriginalTrail.Provider value={inner}>
      <StructuredEditor
        rootBlockId={wholePage ? null : blockId}
        embeddedBody={wholePage}
        readOnly={!canEdit}
        pageId={pageId}
        workspaceId={workspaceId}
        session={session}
        collaborationDocument={body.collaboration.document}
        mentionSnapshot={body.mentionSnapshot}
        graph={graph}
        boardAdapter={body.boardAdapter}
        databaseViewAdapter={body.databaseViewAdapter}
        tableAdapter={body.tableAdapter}
        timelineAdapter={body.timelineAdapter}
        formService={body.formService}
        loading={body.graphLoading || body.collaboration.status === "loading"}
        pendingToolbarDataSourceIds={body.pendingToolbarDataSourceIds}
        {...body.editorBridge}
        moveContext={moveContext}
        onAddParagraph={body.editorActions.addParagraph}
        onAddDatabase={body.editorActions.addDatabase}
        onChangeParagraph={body.editorActions.changeParagraph}
        onAddProperty={body.addGraphProperty}
        onConfiguredView={body.applyConfiguredFormView}
      />
      {databasePickerOpen && (
        <PageBodyDatabasePicker actions={body.editorActions} onClose={() => setDatabasePickerOpen(false)} />
      )}
    </SyncedOriginalTrail.Provider>
  );
}
