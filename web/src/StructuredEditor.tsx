import { useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import type * as Y from "yjs";
import { FloatingPageOutline } from "./components/editor/FloatingPageOutline";
import { PageCustomizationContext } from "./components/page/pageCustomizationContext";
import { Editor } from "./components/editor/Editor";
import { PageButtonExecutionScope } from "./components/editor/pageButtonExecutionState";
import { BlockCommentCountsProvider } from "./components/editor/blockCommentCounts";
import { FieldPagesContext, MentionSnapshotContext, useMentionSnapshot } from "./components/editor/mentionSnapshotContext";
import { focusEditableNextFrame, getEditable } from "./components/editor/focus";
import { nativeTiptapBlockHasFocus } from "./components/editor/nativeTiptapFocus";
import { useCommittedOperationScope } from "./components/product/useCommittedOperationScope";
import { createNativeEditorGraphAdapter } from "./components/editor/nativeEditorGraphAdapter";
import { useReadOnlyToggleView } from "./components/editor/useReadOnlyToggleView";
import { useGraphRowNames } from "./lib/graphRowNames";
import type {
  EditorCommit, EditorDatabaseStartActions, EditorGraphSync, EditorMoveContext, EditorPageStarterActions,
} from "./components/editor/editorRuntimeTypes";
import type { BlockPickerSnapshot } from "./components/editor/blockPickerModel";
import type { EditorDatabaseChoice } from "./components/editor/editorGraphModel";
import {
  consumePendingPageStarterDismiss,
  PAGE_STARTER_DISMISS_REQUEST,
  requestPageStarterDismiss,
  type PageStarterDismissDetail,
} from "./components/editor/pageStarterDismiss";
import type {
  ContentGraph,
  GraphBlock,
  GraphDataSource,
  GraphProperty,
  GraphValueType,
  GraphView,
} from "./contentGraph";
import type { NativeFormService } from "./lib/nativeFormService";
import type { NativeBoardProductAdapter } from "./lib/nativeBoardProductAdapter";
import type { NativeDatabaseViewProductAdapter } from "./lib/nativeDatabaseViewProductAdapter";
import type { NativeTableProductAdapter } from "./lib/nativeTableProductAdapter";
import type { NativeTimelineProductAdapter } from "./lib/nativeTimelineProductAdapter";
import type { Session } from "./types";
import type { DatabasePageLock } from "./components/database/InlineDatabase";

type StructuredEditorProps = {
  showPageOutline?: boolean;
  pageId: string;
  workspaceId: string;
  session: Session | null;
  collaborationDocument: Y.Doc | null;
  mentionSnapshot?: BlockPickerSnapshot | null;
  graph: ContentGraph;
  boardAdapter?: NativeBoardProductAdapter;
  databaseViewAdapter?: NativeDatabaseViewProductAdapter;
  tableAdapter?: NativeTableProductAdapter;
  timelineAdapter?: NativeTimelineProductAdapter;
  loading: boolean;
  /** Whether the page couldn't be read: a note shows instead of its body. */
  unavailable?: boolean;
  pendingToolbarDataSourceIds?: ReadonlySet<string>;
  readOnly?: boolean;
  staticReadOnlySemantics?: boolean;
  /** Edits this block's content alone, inside another page's editor. */
  rootBlockId?: string | null;
  /** Edits this page's whole body inside another page's editor, as a synced copy showing it does. */
  embeddedBody?: boolean;
  /** Shows this one top block as the whole body: an inline database opened alone. */
  onlyBlockId?: string | null;
  moveContext?: EditorMoveContext;
  graphSync?: EditorGraphSync;
  onGraphMutation?: EditorCommit;
  onAddParagraph: () => string | void;
  /** Opens the database chooser, for the line a menu chose when one did. */
  onAddDatabase: (choice?: EditorDatabaseChoice) => void;
  /** What an empty page's starter does besides writing and adding an inline database. */
  starter?: EditorPageStarterActions;
  databaseStart?: EditorDatabaseStartActions;
  /** The body is a full-page database's database. */
  fixedBody?: boolean;
  /** The page is a locked database: its rows stay editable, its views and properties don't. */
  databaseStructureLocked?: boolean;
  /** The page is read only because it is being suggested: its databases still take edits. */
  databasesWritable?: boolean;
  /** The page is read only by its lock alone for someone who may edit it: its databases' own lock still unlocks. */
  databasesLockable?: boolean;
  /** The reader may comment though the page is read only for them (comment access). */
  canComment?: boolean;
  /** A database page's own lock, which its view settings lock and unlock for everyone. */
  databasePageLock?: DatabasePageLock;
  /** The body is a database page's, whose comments are its row's. */
  rowComments?: boolean;
  /** The empty body's starter is this line alone. */
  starterHint?: string;
  /** Whether an empty body offers its starter; a database page's template prompt stands in its place. */
  pageStarter?: boolean;
  onChangeParagraph: (block: GraphBlock, text: string) => void;
  onChangeDatabaseTitle?: (source: GraphDataSource, title: string) => void;
  onSelectView?: (block: GraphBlock, view: GraphView) => void;
  formService?: NativeFormService;
  onAddProperty?: (
    source: GraphDataSource,
    type: GraphValueType,
    name: string,
    config?: Record<string, unknown>,
  ) => Promise<GraphProperty | null>;
  onConfiguredView?: (view: GraphView) => void;
};

// The line the starter gave way to takes the focus once drawn, what is typed
// meanwhile waiting for it, unless it has it already: what was typed into it
// as it showed keeps its caret (2026-10-03: typing right after "텍스트" jumped
// back to the line's start).
function focusPlaceholderSoon(blockId: string) {
  focusEditableNextFrame(blockId, "start", () => nativeTiptapBlockHasFocus(blockId) || getEditable(blockId) === document.activeElement);
}

export function StructuredEditor({
  showPageOutline,
  pageId,
  workspaceId,
  session,
  collaborationDocument,
  mentionSnapshot,
  graph: givenGraph,
  boardAdapter,
  databaseViewAdapter,
  tableAdapter,
  timelineAdapter,
  loading,
  unavailable = false,
  pendingToolbarDataSourceIds = new Set(),
  readOnly = false,
  staticReadOnlySemantics = false,
  rootBlockId = null,
  embeddedBody = false,
  onlyBlockId = null,
  rowComments = false,
  starterHint,
  pageStarter = true,
  moveContext,
  graphSync,
  onGraphMutation = () => {},
  onAddParagraph,
  onAddDatabase,
  starter,
  databaseStart,
  fixedBody = false,
  databaseStructureLocked = false,
  databasesWritable = false,
  databasesLockable = false,
  canComment,
  databasePageLock,
  onChangeParagraph,
  onChangeDatabaseTitle,
  onSelectView,
  formService,
  onAddProperty,
  onConfiguredView,
}: StructuredEditorProps) {
  // Its database rows named as they are named everywhere now (lib/graphRowNames.ts).
  const graph = useGraphRowNames(givenGraph);
  const customization = useContext(PageCustomizationContext);
  // One block's content has no page starter of its own.
  const onlyBlock = rootBlockId === null && !embeddedBody && onlyBlockId === null && graph.blocks.length === 1 ? graph.blocks[0] : null;
  // The only line's emptiness, followed as it is typed and erased: its
  // "시작하기" row comes back once its text is gone, as Notion's does (seen
  // 2026-10-31).
  const onlyLineId = onlyBlock?.type === "paragraph" ? onlyBlock.id : null;
  const onlyLineEmpty = useSyncExternalStore(
    useCallback((notify: () => void) => {
      const text = onlyLineId ? collaborationDocument?.getText(`block:${onlyLineId}`) : undefined;
      if (!text) return () => undefined;
      text.observe(notify);
      return () => text.unobserve(notify);
    }, [collaborationDocument, onlyLineId]),
    () => onlyLineId !== null && (collaborationDocument?.getText(`block:${onlyLineId}`).toString() ?? "").length === 0,
  );
  const pagePlaceholderBlockId = onlyLineEmpty ? onlyLineId : null;
  const [dismissedStarterBlockId, setDismissedStarterBlockId] = useState<string | null>(null);
  // A full-page database's body, its database still being made by the
  // server (startChosenDatabaseHere), offers no starter.
  const showPageStarter = pageStarter && !fixedBody && rootBlockId === null && !embeddedBody && onlyBlockId === null && (graph.blocks.length === 0
    || (pagePlaceholderBlockId !== null && dismissedStarterBlockId !== pagePlaceholderBlockId));
  const outerMentionSnapshot = useMentionSnapshot();
  const generationRef = useRef(0);
  const currentRef = useRef({ pageId, generation: 0 });
  // A page drawn for a person is one generation of its editor; their sign-in
  // handed over or renewed keeps it, as it keeps the page's document
  // (collaboration.ts — 2026-11-14: the body was drawn anew, a moment short).
  const generation = useMemo(() => {
    generationRef.current += 1;
    return generationRef.current;
  }, [pageId, session?.user.id]);
  const operation = useCommittedOperationScope([generation]);
  currentRef.current = { pageId, generation };
  const toggleView = useReadOnlyToggleView(graph, readOnly, generation);

  useEffect(() => {
    function onStarterDismissRequest(event: Event) {
      if (!pagePlaceholderBlockId) return;
      const detail = (event as CustomEvent<PageStarterDismissDetail>).detail;
      if (detail?.pageId !== pageId) return;
      if (detail.blockId && detail.blockId !== pagePlaceholderBlockId) return;
      consumePendingPageStarterDismiss(pageId, pagePlaceholderBlockId);
      setDismissedStarterBlockId(pagePlaceholderBlockId);
      focusPlaceholderSoon(pagePlaceholderBlockId);
    }

    document.addEventListener(PAGE_STARTER_DISMISS_REQUEST, onStarterDismissRequest);
    if (pagePlaceholderBlockId && consumePendingPageStarterDismiss(pageId, pagePlaceholderBlockId)) {
      setDismissedStarterBlockId(pagePlaceholderBlockId);
      focusPlaceholderSoon(pagePlaceholderBlockId);
    }
    return () => document.removeEventListener(PAGE_STARTER_DISMISS_REQUEST, onStarterDismissRequest);
  }, [pageId, pagePlaceholderBlockId]);

  function addParagraphFromStarter() {
    const blockId = onAddParagraph();
    if (graph.blocks.length === 0 && typeof blockId === "string") requestPageStarterDismiss(pageId, blockId);
  }

  const fallbackDatabaseViewAdapter: NativeDatabaseViewProductAdapter = useMemo(() => ({
    changeTitle: (source, title) => onChangeDatabaseTitle?.(source, title),
    changeIcon: () => {},
    changeDescription: () => {},
    changeCover: () => {},
    showRowContent: () => {},
    selectView: (block, view) => onSelectView?.(block, view),
    setLocked: () => {},
    setArchived: () => {},
    setLinkedPresentation: () => {},
    createView: async () => null,
    updateViews: async () => {},
    deleteView: async () => {},
    restoreView: async () => {},
    addDashboardWidget: async () => null,
    duplicateDashboardWidget: async () => null,
    retypeDashboardWidget: async () => {},
    removeDashboardWidget: async () => null,
    arrangeDashboard: async () => {},
    turnViewIntoDashboard: async () => false,
    turnDashboardInto: async () => false,
  }), [onChangeDatabaseTitle, onSelectView]);

  const adapter = createNativeEditorGraphAdapter({
    pageId,
    workspaceId,
    session,
    generation,
    signal: operation.signal,
    graph: toggleView.graph,
    readOnly,
    onReadOnlyToggleChange: toggleView.change,
    fixedBody,
    collaborationDocument,
    mentionSnapshot,
    moveContext,
    graphSync,
    pagePlaceholderBlockId,
    database: {
      boardAdapter,
      databaseViewAdapter: databaseViewAdapter ?? fallbackDatabaseViewAdapter,
      tableAdapter,
      timelineAdapter,
      formService,
      pendingToolbarDataSourceIds,
      structureLocked: databaseStructureLocked,
      writableWhileSuggesting: databasesWritable,
      lockableUnderPageLock: databasesLockable,
      canComment: canComment ?? !readOnly,
      pageLock: databasePageLock,
      onChooseDatabase: onAddDatabase,
      startPanel: databaseStart,
      onAddProperty,
      onConfiguredView,
    },
    commit: onGraphMutation,
    changeText: onChangeParagraph,
    isCurrent: () => currentRef.current.pageId === pageId
      && currentRef.current.generation === generation
      && !operation.signal.aborted,
  });

  const fieldPages = useMemo(() => !readOnly && moveContext ? { moveContext, workspaceId: adapter.workspaceId, pageId } : null,
    [adapter.workspaceId, moveContext, pageId, readOnly]);
  const surface = <><Editor adapter={adapter} loading={loading} unavailable={unavailable} showPageStarter={showPageStarter}
    rootBlockId={rootBlockId} embedded={embeddedBody} onlyBlockId={onlyBlockId} staticReadOnlySemantics={staticReadOnlySemantics}
    pagePlaceholderBlockId={pagePlaceholderBlockId} starter={starter} starterHint={starterHint}
    onAddParagraph={addParagraphFromStarter} onAddDatabase={onAddDatabase} />
    {(showPageOutline ?? customization.outlineEnabled) && rootBlockId === null && !embeddedBody && onlyBlockId === null && !fixedBody
      ? <FloatingPageOutline adapter={adapter} /> : null}</>;
  const editor = session
    ? <BlockCommentCountsProvider pageId={pageId} workspaceId={workspaceId} row={rowComments} session={session}>{surface}</BlockCommentCountsProvider>
    : surface;
  return <PageButtonExecutionScope identity={`${pageId}/${generation}`} blocks={graph.blocks}>
    {/* Always this one tree: access read again can flip read-only for a moment
        (a new snapshot's first read), and that must not build the editor again. */}
    <FieldPagesContext.Provider value={fieldPages}>
      <MentionSnapshotContext.Provider value={readOnly ? outerMentionSnapshot : mentionSnapshot ?? null}>{editor}</MentionSnapshotContext.Provider>
    </FieldPagesContext.Provider>
  </PageButtonExecutionScope>;
}
