import { useMemo, useRef } from "react";
import type { ContentGraph } from "@/contentGraph";
import { StructuredEditor } from "@/StructuredEditor";
import { SuggestingModeContext } from "@/components/editor/suggestingMode";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { pageLookup } from "@/lib/pageArchive";
import { pageTrashRoot } from "@/lib/pageTrash";
import type { Page, Session } from "@/types";
import type { useDatabasePageLocks } from "./databasePageLocks";
import { pageLockHoldsBody } from "./pageLockHoldsBody";
import type { usePageBodyEditing } from "./usePageBodyEditing";
import type { usePageFavorites } from "./usePageFavorites";
import type { usePageMenuActions } from "./usePageMenuActions";
import type { usePageSuggesting } from "./usePageSuggesting";
import { WithPageGraph } from "./WithPageGraph";
import { trashedAloneBlock } from "@/lib/trashedDatabaseAlone";

type PageBodyEditing = ReturnType<typeof usePageBodyEditing>;
type MoveContext = PageBodyEditing["editorBridge"] extends infer Bridge ? Bridge extends { moveContext: infer Context } ? Context : never : never;

/**
 * What the body's editor is drawn with besides its graph, the same as the
 * app redraws: its moves made anew told a long page's views each edit.
 */
export function usePageEditorMoveContext(
  editorBridge: PageBodyEditing["editorBridge"],
  favoritePageIds: readonly string[],
  pageMenuActions: ReturnType<typeof usePageMenuActions>,
  pageFavoriteService: ReturnType<typeof usePageFavorites>["pageFavoriteService"],
) {
  const menuNow = useRef({ pageMenuActions, pageFavoriteService }); menuNow.current = { pageMenuActions, pageFavoriteService };
  return useMemo(() => editorBridge && { ...editorBridge.moveContext, pageMenu: { favoritePageIds,
    onPageMetadataChange: (...change: Parameters<typeof pageMenuActions.changePageMetadata>) => menuNow.current.pageMenuActions.changePageMetadata(...change),
    onDuplicatePage: (...copy: Parameters<typeof pageMenuActions.duplicatePage>) => menuNow.current.pageMenuActions.duplicatePage(...copy),
    onFavoriteChange: (pageId: string, favorite: boolean) => menuNow.current.pageFavoriteService.setFavorite(pageId, favorite).then(() => undefined) } }, [editorBridge, favoritePageIds]);
}

/**
 * The open page's body in the page area: the editor drawn with the graph the
 * caller picks from the page's (its Home, its wiki's database), as the page's
 * lock, trash, access and suggesting leave it to edit.
 */
export function AuthenticatedPageEditor({
  page, pickGraph, onlyBlockId, body, pages, session, graphStore, lockedForMe, suggesting, databasePageLock, inlineDatabaseBlockId, moveContext,
}: {
  page: Page;
  pickGraph: (graph: ContentGraph) => ContentGraph;
  onlyBlockId?: string;
  body: PageBodyEditing;
  pages: Page[];
  session: Session;
  graphStore: PageGraphStore;
  lockedForMe: boolean;
  suggesting: ReturnType<typeof usePageSuggesting>;
  databasePageLock: ReturnType<typeof useDatabasePageLocks>;
  /** The page's inline database opened alone, if one is. */
  inlineDatabaseBlockId?: string;
  moveContext: MoveContext | undefined;
}) {
  const {
    addGraphProperty, applyConfiguredFormView, boardAdapter, collaboration, databaseViewAdapter, editorActions, editorBridge, formService,
    graphCanEdit, graphCanSuggest, graphLoading, graphUnavailable, mentionSnapshot, pendingToolbarDataSourceIds, tableAdapter, timelineAdapter,
  } = body;
  const pageLocked = pageLockHoldsBody(page, lockedForMe, onlyBlockId && onlyBlockId === inlineDatabaseBlockId ? onlyBlockId : undefined), writable = !pageTrashRoot(page, pageLookup(pages)) && graphCanEdit, editable = writable && !pageLocked;
  const readOnly = !editable || suggesting.active;
  const lock = databasePageLock(page, graphCanEdit);
  const loading = graphLoading || collaboration.status === "loading";
  return (
    <SuggestingModeContext.Provider value={suggesting.mode}><WithPageGraph store={graphStore} pick={pickGraph}>{(editorGraph) => (
    <StructuredEditor
      // An inline database alone in the trash, opened alone, reads as it was (lib/trashedDatabaseAlone.ts).
      readOnly={readOnly || trashedAloneBlock(editorGraph, onlyBlockId)}
      databaseStructureLocked={lockedForMe && page.kind === "database"} databasesWritable={editable && suggesting.active} databasesLockable={writable && pageLocked && !suggesting.active}
      canComment={graphCanEdit || graphCanSuggest}
      databasePageLock={lock}
      pageId={page.id}
      workspaceId={page.workspaceId}
      session={session}
      collaborationDocument={collaboration.document}
      mentionSnapshot={mentionSnapshot}
      graph={editorGraph}
      boardAdapter={boardAdapter}
      databaseViewAdapter={databaseViewAdapter}
      tableAdapter={tableAdapter}
      timelineAdapter={timelineAdapter}
      formService={formService}
      loading={loading} unavailable={graphUnavailable}
      pendingToolbarDataSourceIds={pendingToolbarDataSourceIds}
      {...editorBridge}
      moveContext={moveContext}
      onAddParagraph={editorActions.addParagraph}
      onAddDatabase={editorActions.addDatabase}
      starter={editorActions.pageStarter} databaseStart={editorActions.databaseStart}
      fixedBody={page.kind === "database" || Boolean(onlyBlockId)}
      onlyBlockId={onlyBlockId ?? null}
      onChangeParagraph={editorActions.changeParagraph}
      onAddProperty={addGraphProperty}
      onConfiguredView={applyConfiguredFormView}
    />)}</WithPageGraph></SuggestingModeContext.Provider>
  );
}
