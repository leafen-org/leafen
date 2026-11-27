import type { ProductStatusSink } from "@/lib/productStatus";
import { i18next } from "@/i18n";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { flushSync } from "react-dom";
import { useAppFileSaved } from "@/components/apps/appFileOpening";
import { usePageCollaboration } from "@/collaboration";
import type { ContentGraph, GraphOperation } from "@/contentGraph";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { withRowsKeptMade } from "./pendingGraphOverlay";
import { onRowMadeHere } from "@/lib/rowMadeHere";
import { tellPageEditedHere } from "@/lib/pageEditedHere";
import { graphWriteEditsPage } from "./graphWritePageEdit";
import { createAuthenticatedEditorBridge } from "@/components/editor/createAuthenticatedEditorBridge";
import { onSettledBlockSuggestion } from "@/components/editor/suggestingMode";
import { stampRowsMadeHere } from "@/lib/graphRowsMadeHere";
import type { NativeGraphMutationOutbox } from "@/lib/nativePageMutationBrowser";
import type { Bootstrap, Page, Session } from "@/types";
import { useAuthenticatedEditorActions, type EditedPage } from "./useAuthenticatedEditorActions";
import { useCommittedResourceDisposal } from "./useCommittedResourceDisposal";
import { useContentGraphWrites } from "./useContentGraphWrites";
import { useEditorMentionSnapshot } from "./useEditorMentionSnapshot";
import { useExternalGraphRefresh } from "./useExternalGraphRefresh";
import { useLocalFirstContentGraph } from "./useLocalFirstContentGraph";
import { readAfterTrashMoves, useTrashMoveAccess } from "./useTrashMoveAccess";
import { usePageDatabaseAdapters } from "./usePageDatabaseAdapters";
import { usePropertyConversionNotice } from "./usePropertyConversionNotice";
import { useLockRefusedKeptNotice, useRefusedEditsNotice } from "./refusedEditsNotice";
import { useDatabaseDateCommandHistory } from "./useDatabaseDateCommandHistory";
import { request } from "@/apiTransport";
import { pageBusyCopyingDatabases, pageBusyNoticeKey, setPageBusy, usePageBusy } from "@/lib/pageBusy";
import { pageCopyUnderWay, usePageFilling } from "@/lib/pageCopyProgress";
import { announceAccessChange } from "@/lib/workspaceChangeCheck";
import { graphWithRoomChange, hearRoomGraphChanges } from "@/lib/roomGraphChange";
import type { InlineDatabaseProjection, PageActionAccess } from "@/workspaceMembershipWire.generated";
import { useHoldPageTextHere } from "@/lib/pageTextsHere";

// How long the room's word waits while this tab's own changes are saved.
const OWN_CHANGES_SETTLE_MS = 250;
// A refused read of a page's changes looks again after this, doubling up to the most.
const FOLLOW_REFUSED_MS = 500;
const FOLLOW_REFUSED_MAX_MS = 4_000;

/**
 * Everything a page body is edited with: its content graph and whether the
 * reader may change it, the writes that keep it, its database adapters, its
 * collaborative text and the editor's actions. A page and a database page
 * are edited alike; the caller owns the graph.
 */
export function usePageBodyEditing(input: {
  bootstrapRevision: number;
  /** The graph the caller draws with: the store's frame where it holds one in `graphStore`. */
  graph: ContentGraph;
  setGraph: Dispatch<SetStateAction<ContentGraph>>;
  /**
   * Where the graph is held apart from the caller (lib/pageGraphStore.ts):
   * the writes read it as drawn, and the cached copy is kept from it.
   */
  graphStore?: PageGraphStore;
  onStatus: ProductStatusSink;
  /** The page edited, by what the editor needs of it. */
  page: (EditedPage & { inTrash?: boolean }) | null;
  /** The pages a body links and mentions, and where a new sub-page goes. */
  pages: Page[];
  /** The workspace's pages the editor offers as mentions. */
  mentionPages: Page[];
  organizationId?: string;
  /** Reads the page list again, once the body's page blocks changed pages. */
  refreshPages?: () => Promise<unknown>;
  session: Session;
  setData: Dispatch<SetStateAction<Bootstrap>>;
  setDatabasePickerOpen: Dispatch<SetStateAction<boolean>>;
  workspaceId?: string;
  /** A database template's body, whose @ menu offers "when duplicated" mentions. */
  templateMentions?: boolean;
  /** Where the body's structure changes wait for a server that can't be reached. */
  mutationOutbox?: NativeGraphMutationOutbox;
  /** The databases the pages hold in their bodies, which the @ menu offers too. */
  inlineDatabases?: readonly InlineDatabaseProjection[];
}) {
  const { graph, graphStore, onStatus, page, session, setData, setGraph } = input;
  const pageId = page?.id ?? null;
  // The graph as drawn, which writes and actions are made against: a change
  // painted ahead of the write that sends it is not yet the server's
  // (lib/pageGraphStore.ts, drawn).
  const graphShown = useRef(graph);
  graphShown.current = graph;
  const readGraph = useCallback(() => graphStore ? graphStore.drawn() : graphShown.current, [graphStore]);
  const [graphLoading, setGraphLoading] = useState(false);
  // The page whose own read ended showing it, which changes are followed after.
  const [readPageId, setReadPageId] = useState<string | null>(null);
  // The page that couldn't be read, which shows a note instead of an editor.
  const [unavailablePageId, setUnavailablePageId] = useState<string | null>(null);
  // The page's organization keeps it until the reader, a guest, turns on
  // 2-step verification.
  const [guestMFARequired, setGuestMFARequired] = useState(false);
  // A page the reader can view or comment on but not change opens read-only.
  const [graphCanEdit, setGraphCanEdit] = useState(true);
  // A reader who can comment may suggest edits to the page's text.
  const [graphCanSuggest, setGraphCanSuggest] = useState(false);
  const [pendingToolbarDataSourceIds, setPendingToolbarDataSourceIds] = useState<ReadonlySet<string>>(() => new Set());
  const graphRevision = useRef(0);
  const markToolbarDataSourcePending = useCallback((dataSourceId: string) => {
    setPendingToolbarDataSourceIds((current) => new Set(current).add(dataSourceId));
  }, []);
  const settleToolbarDataSources = useCallback((operations: readonly GraphOperation[]) => {
    const settled = new Set(operations.flatMap((operation) => (
      operation.kind === "upsertDataSource" ? [operation.dataSource.id] : []
    )));
    if (settled.size === 0) return;
    setPendingToolbarDataSourceIds((current) => {
      if (![...settled].some((id) => current.has(id))) return current;
      return new Set([...current].filter((id) => !settled.has(id)));
    });
  }, []);
  const conversionNotice = usePropertyConversionNotice();
  const { mutationOutbox } = input;
  // Whether the page may be edited is known once it's read for this page as
  // it stands, in its own trash or out of it: one restored is read again
  // (useTrashMoveAccess.ts), the answer before not its own (2026-11-07: a page restored
  // from the trash showed its owner "편집 권한 요청" and no place until read
  // again).
  const accessKey = pageId ? `${pageId}\u0000${page?.inTrash === true ? "trash" : "page"}` : null;
  const [canEditPage, setCanEditPage] = useState<string | null>(null);
  const currentPageId = useRef(pageId);
  currentPageId.current = pageId;
  const currentAccessKey = useRef(accessKey);
  currentAccessKey.current = accessKey;
  const onStatusRef = useRef(onStatus);
  onStatusRef.current = onStatus;
  const onGraphUnavailable = useCallback((unavailable: boolean) => {
    setUnavailablePageId(unavailable ? currentPageId.current : null);
  }, []);
  const reportCanEdit = useCallback((canEdit: boolean) => {
    setGraphCanEdit(canEdit);
    setCanEditPage(currentAccessKey.current);
  }, []);
  // A page an operation holds says which, once for each time (lib/pageBusy.ts).
  // A hold only read says so once it has lasted a second on the page shown,
  // so a quick copy says nothing, as Notion's (seen 2026-10-31); an edit it
  // refused says so at once. A body being filled with its databases' copies
  // shows so in its place instead (lib/pageDatabaseCopies.ts).
  const busyNoticed = useRef("");
  const busyPending = useRef<{ key: string; timer: number } | null>(null);
  useEffect(() => () => { if (busyPending.current) window.clearTimeout(busyPending.current.timer); }, []);
  const reportBusy = useCallback((busy: string | null, now = false) => {
    const shownPageId = currentPageId.current;
    if (!shownPageId) return;
    setPageBusy(shownPageId, busy);
    // A hold whose kind isn't known is the one already said or about to be, if any.
    const known = busyPending.current?.key ?? busyNoticed.current;
    const noticed = busy === "other" && known.startsWith(`${shownPageId}:`) ? known
      : busy ? `${shownPageId}:${busy}` : "";
    const pending = busyPending.current;
    if (pending && (pending.key !== noticed || now)) {
      window.clearTimeout(pending.timer);
      busyPending.current = null;
    }
    if (!busy || busyNoticed.current === noticed) {
      busyNoticed.current = noticed;
      return;
    }
    const say = () => {
      busyPending.current = null;
      if (currentPageId.current !== shownPageId) return;
      busyNoticed.current = noticed;
      // A copy being made says so in its bar.
      if (pageCopyUnderWay(shownPageId) || (busy === pageBusyCopyingDatabases && !now)) return;
      onStatusRef.current(i18next.t(pageBusyNoticeKey(busy)), "default");
    };
    if (now) say();
    else if (!busyPending.current) busyPending.current = { key: noticed, timer: window.setTimeout(say, 1_000) };
  }, []);
  const onAccess = useCallback((canEdit: boolean, canSuggest: boolean) => {
    reportCanEdit(canEdit);
    setGraphCanSuggest(canSuggest);
  }, [reportCanEdit]);
  const dateWriter = useRef<Pick<ReturnType<typeof useContentGraphWrites>, "commit" | "reread"> | null>(null);
  const dateCommands = useDatabaseDateCommandHistory({
    workspaceId: page?.workspaceId, pageId, principalId: session.user.id, sessionIdentity: session.token, onStatus,
    restore: (authorityPageId, mutationId, forward) => dateWriter.current?.commit(authorityPageId,
      [{ kind: "restoreDateCommand", dateRestore: { mutationId, forward } }]) ?? Promise.resolve(false),
    reread: () => pageId ? dateWriter.current?.reread(pageId) ?? Promise.resolve(false) : Promise.resolve(false),
  });
  // An edit made here — its words typed, a write that edits the page
  // (graphWritePageEdit.ts), not what the server or another tab sends — shows
  // the page last edited now, by this person, wherever its row is listed (a
  // sidebar section in "최종 편집 일시" order puts it first), at most every
  // two seconds, as a row's page does (rowBodyEdits.ts; 2026-11-09: it moved
  // up only at the next list read), kept apart from the list past the frame
  // that shows the edit (lib/pageEditedHere.ts).
  const editedTold = useRef(0);
  const editedBy = session.user.id, databasePage = page?.kind === "database";
  const noteBodyEdited = useCallback((editedPageId: string, operations?: readonly GraphOperation[]) => {
    if (editedPageId !== pageId || Date.now() - editedTold.current < 2000) return;
    if (operations && !graphWriteEditsPage(readGraph(), operations, databasePage)) return;
    editedTold.current = Date.now();
    tellPageEditedHere({ id: editedPageId, updatedAt: new Date(editedTold.current).toISOString(), lastEditedBy: editedBy });
  }, [databasePage, editedBy, pageId, readGraph]);
  const graphWrites = useContentGraphWrites({
    onBodyEdited: noteBodyEdited,
    conversionNotice, currentPageId: pageId, readGraph, graphRevision, mutationOutbox, onAccess, session, setGraph, setMessage: onStatus,
    settleToolbarDataSources,
    onDateCommand: dateCommands.onDateCommand,
    onPropertyConversion: dateCommands.recordValueCommand,
  });
  dateWriter.current = graphWrites;
  const pendingWrites = useCallback((id: string) => (
    mutationOutbox
      ? mutationOutbox.pendingGraphWrites(id) as Promise<{ mutationId: string; operations: GraphOperation[] }[]>
      : Promise.resolve([])
  ), [mutationOutbox]);
  const writesShown = useCallback((id: string) => mutationOutbox?.graphPagesWaiting.unanswered(id) ?? [], [mutationOutbox]);
  useLocalFirstContentGraph({
    bootstrapRevision: input.bootstrapRevision,
    // Its trash is read below, once for each move to or out of it.
    pageEntryKey: input.page ? [input.page.parentId, input.page.teamspaceId, input.page.kind ?? "page"].join("\u0000") : "",
    graph,
    graphStore,
    graphRevision,
    onGraph: setGraph,
    onCanEdit: reportCanEdit,
    onBusy: reportBusy,
    onCanSuggest: setGraphCanSuggest,
    onLoading: setGraphLoading,
    onRead: setReadPageId,
    onUnavailable: onGraphUnavailable,
    onGuestMFARequired: setGuestMFARequired,
    onStatus,
    pageId,
    pendingWrites,
    readSettled: readAfterTrashMoves(graphWrites.readSettled),
    session,
    writesShown,
  });
  // Rows the editors make here take their made time and maker at once.
  const userId = session.user.id;
  const setMadeGraph = useCallback<Dispatch<SetStateAction<ContentGraph>>>((next) => setGraph((current) => (
    stampRowsMadeHere(current, typeof next === "function" ? next(current) : next, userId)
  )), [setGraph, userId]);
  // The same while what it writes through is, so the editor's page and
  // what it hands its blocks stay the same as the page's values change.
  const editedPageId = page?.id;
  const refreshPagesRef = useRef(input.refreshPages);
  refreshPagesRef.current = input.refreshPages;
  const refreshesPages = Boolean(input.refreshPages);
  const editorBridge = useMemo(() => editedPageId ? createAuthenticatedEditorBridge({
    graphWrites, onDataSourceMade: markToolbarDataSourcePending, pageId: editedPageId, pages: input.pages,
    refreshPages: refreshesPages ? () => refreshPagesRef.current?.() ?? Promise.resolve() : undefined,
    session, setData, setGraph: setMadeGraph, setMessage: onStatus,
  }) : undefined, [editedPageId, graphWrites, input.pages, markToolbarDataSourcePending, onStatus, refreshesPages, session, setData, setMadeGraph]);
  const { addGraphProperty, applyConfiguredFormView, formService, databaseViewAdapter, tableAdapter, boardAdapter, timelineAdapter } = usePageDatabaseAdapters({
    graph, readGraph, pageId, graphWrites, session, organizationId: input.organizationId, workspaceId: page?.workspaceId ?? null,
    setGraph: setMadeGraph, onStatus, dateCommands, graphStore,
  });
  useExternalGraphRefresh({ graph, pageId, session, setGraph });
  // A settled suggestion that made blocks kept or removed them on the server.
  const { follow: followChanges, reread, writingHere: ownWritesHere, writingOwn: ownWritesGoing } = graphWrites;
  useEffect(() => {
    if (!pageId) return;
    return onSettledBlockSuggestion(pageId, () => void reread(pageId));
  }, [pageId, reread]);
  // A page moved to or out of the trash is read again once the move landed (useTrashMoveAccess.ts).
  const restoredReading = useTrashMoveAccess({ pageId, trashed: page?.inTrash === true, readPageId, reread });
  const onRefusedEdits = useRefusedEditsNotice(readGraph);
  // The DLP policy's refusal says so; what was written is offered to copy alike.
  const onDLPRefusedEdits = useRefusedEditsNotice(readGraph, "syncStatusBadge:dlpRefusedEdits");
  const busy = usePageBusy(pageId);
  // Its own databases being copied into it after the rest of its body, for a
  // copy asked for here or as its read says: its body shows as being filled
  // until they come, as Notion's (lib/pageDatabaseCopies.ts); one followed
  // here is read again then, and shows as being filled until that read
  // ends (a hold read is read again as it ends, and shows so until then).
  const followedFilling = usePageFilling(pageId);
  const [fillRead, setFillRead] = useState<string | null>(null);
  const filling = followedFilling || busy === pageBusyCopyingDatabases || (Boolean(pageId) && fillRead === pageId);
  const wasFollowedFilling = useRef<{ pageId: string | null | undefined; filling: boolean; held: boolean }>({ pageId, filling: false, held: false });
  useEffect(() => {
    const before = wasFollowedFilling.current;
    const held = busy === pageBusyCopyingDatabases;
    wasFollowedFilling.current = { pageId, filling: followedFilling, held: followedFilling && (held || (before.pageId === pageId && before.held)) };
    // A hold its read knew of is read again by that read (useLocalFirstContentGraph).
    if (pageId && before.pageId === pageId && before.filling && !followedFilling && !held && !before.held) {
      setFillRead(pageId);
      void reread(pageId).catch(() => false).finally(() => setFillRead((current) => current === pageId ? null : current));
    }
  }, [busy, followedFilling, pageId, reread]);
  // The position of the blocks drawn, which the room's text waits for as its
  // connection opens (lib/roomTextOnJoin.ts); told as each graph is drawn,
  // before it is painted: the editor takes the graph in the same frame.
  const blocksDrawnAt = useRef(0);
  const collaboration = usePageCollaboration(session, pageId, {
    blocksShownAt: () => blocksDrawnAt.current,
    editable: canEditPage === accessKey ? graphCanEdit : undefined,
    held: Boolean(busy),
    onRefusedEdits,
    onDLPRefusedEdits,
    // A page that turns read-only with nothing of the person's lost just
    // stops taking edits, unannounced, as Notion's (seen 2026-11-07); only
    // edits let go are said (onRefusedEdits).
    // An operation holding the page isn't a change of access: what was
    // written here is kept, the page says which operation, and is read
    // again for its access.
    isHeld: async () => {
      const heldPageId = currentPageId.current;
      if (!heldPageId) return false;
      const busy = await request<PageActionAccess>(`/pages/${encodeURIComponent(heldPageId)}?accessOnly=1`, {}, session)
        .then((access) => access.busy ?? "", () => "");
      if (!busy || currentPageId.current !== heldPageId) return false;
      reportBusy(busy, true);
      announceAccessChange();
      return true;
    },
  });
  useHoldPageTextHere(pageId, collaboration.document); // Its words as written, for what reads the page while it is open here.
  // A row made for this page's databases elsewhere in the app while the
  // server is out of reach (a sidebar section's "+") waits to be sent; it
  // shows in this page's table at once, as it will once sent (2026-11-09:
  // it showed only after the server was reached again). Rows already here
  // stay as this page has them, and one a later kept change deletes stays
  // gone (withRowsKeptMade). One made on this device first (rowMadeHere.ts)
  // shows the moment it is made, so its page opens over this page's table at once.
  useEffect(() => {
    if (!mutationOutbox || !pageId) return undefined;
    const overlay = (operations: readonly GraphOperation[]) => setGraph((current) => withRowsKeptMade(current, operations));
    const stopMadeHere = onRowMadeHere(pageId, (row) => overlay([{ kind: "upsertRow", row }]));
    const stopLane = mutationOutbox.subscribeGraphLane((event) => {
      if (event.kind !== "kept" || event.pageId !== pageId) return;
      void (mutationOutbox.pendingGraphOperations(pageId) as Promise<GraphOperation[]>).then(overlay, () => undefined);
    });
    return () => { stopMadeHere(); stopLane(); };
  }, [mutationOutbox, pageId, setGraph]);
  // Words typed here are a body edit too (noteBodyEdited); what a lock refused of them may be copied.
  const textDocument = collaboration.document;
  useLockRefusedKeptNotice(mutationOutbox, pageId, textDocument);
  useEffect(() => {
    if (!textDocument || !pageId) return;
    const hear = (_update: Uint8Array, _origin: unknown, _document: unknown, transaction: { local: boolean }) => {
      if (transaction.local) noteBodyEdited(pageId);
    };
    textDocument.on("update", hear);
    return () => textDocument.off("update", hear);
  }, [textDocument, pageId, noteBodyEdited]);
  // Blocks another session made, moved or removed: once the room says the
  // page's graph moved past what this page shows, read what changed since
  // (graphWrites.follow: the rows alone, when that is all the changes
  // touched). While this tab's own changes are being saved, the word may be
  // of them, which usually settle first: it waits a moment before asking.
  // Without, the word is another's, another tab's here too: it asks at once,
  // as the change's text came at once through the room (2026-11-13: a line
  // merged elsewhere showed beside its emptied line, and one put back by an
  // Undo went again, for half a second; a page opened beside a tab merging
  // and undoing waited for that tab's changes, its room's text ahead).
  // One read at a time, after the page's first: one read the page as it was
  // when it began, and finds it behind again if it changed meanwhile
  // (2026-11-11: each edit to a database of 100,000 rows began another whole
  // read beside the others, fifteen at once, and the page never opened).
  // The room's word is heard, not drawn: only what the read brings is.
  // Changes are followed once the page's own read ended (useLocalFirstContentGraph's onRead).
  const announcedGraphCursor = collaboration.graphCursor;
  const { blocksShown } = collaboration;
  // A graph held in a store is told as its body draws it: the caller's
  // frame doesn't change for a line made (lib/pageGraphStore.ts).
  const loadingNow = useRef(graphLoading);
  loadingNow.current = graphLoading;
  useLayoutEffect(() => {
    const drawn = () => {
      blocksDrawnAt.current = loadingNow.current ? 0 : graphRevision.current;
      blocksShown();
    };
    drawn();
    return graphStore?.subscribeDrawn(drawn);
  }, [blocksShown, graph, graphLoading, graphStore]);
  const following = useRef<string | null>(null);
  const followAgain = useRef<() => void>(() => undefined);
  const followsFrom = readPageId === pageId;
  // A form says whether its views are the server's answer yet: a copy kept
  // here or held from before may be older than a form saved since, which it
  // would take for one never set up (FormView's first question).
  const pageFormService = useMemo(() => formService && { ...formService, viewsRead: followsFrom }, [formService, followsFrom]);
  // A page an operation holds, or being filled, follows nothing until it
  // ends, and is read again then; a read refused meanwhile, before the hold
  // is known, looks again after a while, not at once (2026-11-14: a page
  // open while a database was copied into it read its changes some fifteen
  // times a second, each refused, until the copy ended).
  const followHeld = Boolean(busy) || filling;
  useEffect(() => {
    if (!pageId || graphLoading || !followsFrom || followHeld) return;
    let timer = 0;
    let stopped = false;
    let refused = 0;
    const behind = () => announcedGraphCursor.current() > graphRevision.current;
    const follow = () => {
      if (stopped || timer || following.current === pageId || !behind()) return;
      const settle = ownWritesHere(pageId) ? OWN_CHANGES_SETTLE_MS : 0;
      timer = window.setTimeout(() => {
        timer = 0;
        if (stopped || following.current === pageId || !behind()) return;
        following.current = pageId;
        // The read that ends looks again, by the follower drawn last.
        void followChanges(pageId).then((read) => { refused = read ? 0 : refused + 1; }, () => { refused += 1; }).finally(() => {
          if (following.current === pageId) following.current = null;
          followAgain.current();
        });
      }, refused ? Math.max(settle, Math.min(FOLLOW_REFUSED_MAX_MS, FOLLOW_REFUSED_MS * 2 ** (refused - 1))) : settle);
    };
    followAgain.current = follow;
    follow();
    const stop = announcedGraphCursor.subscribe(follow);
    // A change of lines the room tells of as it is committed, ahead of its
    // position, is drawn at once with its text, unread, by a page standing
    // just before it with nothing of its own on the way (lib/roomGraphChange.ts),
    // kept ones too, as the read waits for them ("own") — drawn, a line moved
    // twice offline stood once moved as the two were sent (2026-11-25); another
    // tab's changes kept in the outbox since don't hold it, nor the read
    // (2026-11-13: a tab following another's run of merges stood still meanwhile).
    const stopChanges = hearRoomGraphChanges(pageId, (change) => {
      if (stopped || following.current === pageId || ownWritesGoing(pageId) || graphRevision.current !== change.from) return;
      const next = graphWithRoomChange(graphStore ? graphStore.get() : graphShown.current, change);
      if (!next) return;
      graphRevision.current = change.cursor;
      flushSync(() => setGraph(next));
      change.showWords();
    });
    return () => {
      stopped = true;
      window.clearTimeout(timer);
      stop();
      stopChanges();
      if (followAgain.current === follow) followAgain.current = () => undefined;
    };
  }, [announcedGraphCursor, followChanges, followHeld, followsFrom, graphLoading, graphStore, ownWritesGoing, ownWritesHere, pageId, setGraph]);
  // What changed while the connection was gone was told to a room this page
  // wasn't in, or to none: read it again when the connection comes back.
  const reconnects = collaboration.reconnects;
  const seenReconnects = useRef(reconnects);
  useEffect(() => {
    if (reconnects === seenReconnects.current) return;
    seenReconnects.current = reconnects;
    if (pageId) void reread(pageId);
  }, [pageId, reconnects, reread]);
  // Back from this tab's own lock change, or joined behind changes kept here (lib/pageTextBehindKeptChanges.ts), only what changed
  // since (lib/ownLockFlips.ts), once the page's own read ended: one under way may have begun before they went.
  const catchUps = collaboration.catchUps;
  const seenCatchUps = useRef(catchUps);
  useEffect(() => {
    if (catchUps === seenCatchUps.current || graphLoading || !followsFrom) return;
    seenCatchUps.current = catchUps;
    if (pageId) void followChanges(pageId);
  }, [catchUps, followChanges, followsFrom, graphLoading, pageId]);
  // A document saved from this tab: the room's word can be missed, and the
  // person looks at the file's row next (2026-11-05) — and at the page's
  // edited time above it, which the others' tabs already showed as now.
  const refreshPages = input.refreshPages;
  useAppFileSaved(useCallback((savedPageId: string) => {
    if (savedPageId !== pageId) return;
    void reread(savedPageId);
    void refreshPages?.();
  }, [pageId, reread, refreshPages]));
  const mentionSnapshot = useEditorMentionSnapshot({
    session, workspaceId: input.workspaceId, organizationId: input.organizationId,
    pages: input.mentionPages, presence: collaboration.presence, templateMentions: input.templateMentions,
    inlineDatabases: input.inlineDatabases,
  });
  const editorActions = useAuthenticatedEditorActions({
    bodyLoading: graphLoading || collaboration.status === "loading",
    changeText: collaboration.changeText,
    readGraph,
    graphWrites,
    markToolbarDataSourcePending,
    readText: (blockId) => collaboration.document?.getText(`block:${blockId}`).toString() ?? "",
    refreshPages: input.refreshPages,
    selectedPage: page,
    selectedPageId: pageId,
    session,
    setData,
    setDatabasePickerOpen: input.setDatabasePickerOpen,
    setGraph,
    setMessage: onStatus,
  });
  useCommittedResourceDisposal(graphWrites);
  // What the reader may do is this page's own once read; until then it is what
  // a page is taken as on the first load (one's to write in, nothing to
  // suggest), never the page before's: its comment line and presence were
  // drawn with the last page's answer while the new one was read (2026-11-06).
  const accessKnown = canEditPage === accessKey && restoredReading !== pageId;
  return {
    addGraphProperty, applyConfiguredFormView, boardAdapter, collaboration, databaseViewAdapter, editorActions,
    editorBridge, formService: pageFormService, graphLoading: graphLoading || filling, graphFilling: filling, graphRevision, graphWrites, guestMFARequired,
    graphCanEdit: accessKnown ? graphCanEdit : true,
    graphCanSuggest: accessKnown ? graphCanSuggest : false,
    graphUnavailable: unavailablePageId !== null && unavailablePageId === pageId,
    // Until the page shown says what this reader may do, the last page's answer is not its own.
    graphAccessPending: !accessKnown,
    mentionSnapshot, pendingToolbarDataSourceIds, tableAdapter, timelineAdapter,
    recordDateCommand: dateCommands.recordDateCommand,
  };
}
