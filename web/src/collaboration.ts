import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { WebsocketProvider } from "y-websocket";
import * as Y from "yjs";
import { api } from "./api";
import { requestBinaryResponse } from "./apiTransport";
import { notePageTextDocument } from "./lib/pageTextStored";
import { serverWebSocketUrl } from "./lib/serverOrigin";
import { afterPageMade } from "./lib/pagesNotYetMade";
import { afterKeptChangesWent, holdRoomTextBehindKeptChanges } from "./lib/pageTextBehindKeptChanges";
import { forgetYgoRecoveryPage, listYgoRecoveryLocalEdits, replayYgoRecoveryUpdates } from "./offline";
import { recoveryIntake, tryingRecoveryAgain } from "./lib/collaborationRecovery";
import { keepPageCopy, type PageCopyFailureStage } from "./lib/pageCopyKeeping";
import { keepRoomConnectionAsked } from "./lib/pageRoomLiveness";
import { hearRoomAnnouncements } from "./lib/pageRoomAnnouncements";
import { announceRoomReturn, awaitCommentReconnect } from "./lib/commentRefreshEvents";
import { setPagePresenceSnapshot } from "./lib/pagePresenceStore";
import { requestWorkspaceChangeCheck } from "./lib/workspaceChangeCheck";
import {
  collaborationRecoveryFailure,
  collaborationStorageTransition,
  collaborationState as nextCollaborationState,
  collaborationStatusReadFailure,
  collaborationTransportState,
  type CollaborationState,
} from "./lib/collaborationStatus";
import { createReadGenerationOwner, type ReadGenerationContext } from "./lib/readGeneration";
import { createPagePresenceController } from "./lib/pagePresenceController";
import {
  EMPTY_PAGE_PRESENCE_SNAPSHOT,
  pagePresenceTerminalStatus,
} from "./lib/pagePresenceModel";
import { copyMentionsOutdated } from "./lib/collaborationCopyMentions";
import { blockTextUndoManager } from "./lib/blockTextEditing";
import {
  type PagePresenceConnectionStatus,
  type PagePresenceSnapshot,
} from "./lib/pagePresenceView";
import { checkServerDataEpochSoon, dataEpochFor, onServerRestored, serverWasRestored } from "./lib/dataEpoch";
import { ownLockFlip, takeOwnLockFlip } from "./lib/ownLockFlips";
import { isLeavingPage } from "./lib/workspaceLeaving";
import type { Session } from "./types";
import { caretHere, putCaretBack, type RefusedCaret } from "./lib/refusedCaret";
import { holdTextBehindBlockChanges } from "./lib/textBehindBlockChanges";
import { holdPageTextOpen } from "./lib/pageTextOpenLock";
import { serverLacks } from "./lib/serverTextLacks";
import { useRefusedTextLetGo, type RefusedEditsHeard } from "./lib/refusedTextLetGo";
import { subscribeSessionGenerationReset } from "./lib/sessionGeneration";
import { replaceText } from "./lib/collaborativeText";
import { openWordsWithBlocks, wordsCameWithBlocks } from "./lib/pageWordsWithBlocks";
import { hearRoomGraphFrame, ROOM_GRAPH_CHANGE_FRAME } from "./lib/roomGraphChange";
import { createGraphCursorWord, type GraphCursorWord } from "./lib/graphCursorWord";
import { holdRoomTextOnJoin } from "./lib/roomTextOnJoin";
import { pageTextAhead, takePageTextAhead } from "./lib/pageTextAhead";
import { serverAway } from "./lib/serverAnswers";

const recoveryOrigin = Symbol("leafen-local-recovery");

export type { GraphCursorWord } from "./lib/graphCursorWord";

type PageCollaboration = CollaborationState & {
  document: Y.Doc | null;
  changeText: (blockId: string, nextText: string) => void;
  /** The furthest position of the page's blocks the server has told the room about; 0 before any. */
  graphCursor: GraphCursorWord;
  /** How many times the connection came back after closing; what changed meanwhile was told to no one here. */
  reconnects: number;
  /** How many times the room came back after this tab changed the page's lock (lib/ownLockFlips.ts), or a connection's text waited for
   * changes kept here, told to no room as they went (lib/pageTextBehindKeptChanges.ts): what changed meanwhile is caught up on, not read whole. */
  catchUps: number;
  presence: PagePresenceSnapshot;
  /** Told as the page draws its blocks, at the position `blocksShownAt` gives (lib/roomTextOnJoin.ts). */
  blocksShown: () => void;
};

function collaborationUrl(): string {
  return serverWebSocketUrl("/api/leafen/v1/collaboration");
}

/** A room connection's parameters for a sign-in: its token, and the history its copy here is from. */
function roomParams(signIn: Session): Record<string, string> {
  const epoch = dataEpochFor(signIn.user.id);
  return epoch ? { token: signIn.token, epoch } : { token: signIn.token };
}

export type { DateMentionChange } from "./lib/collaborativeMentions";
// The text writes and the read hook live in lib/collaborativeText.ts.
export { copyBlockTexts, setBlockRichText, useCollaborativeText } from "./lib/collaborativeText";

// The server closes a page's connection with this when the organization's
// DLP policy refused text written in it (ygo_dlp_write_check.go).
const DLP_REFUSED_CLOSE_CODE = 4451;

// Whether a page's copy here shows its mentions otherwise than they stand to
// its reader now (collaborationCopyMentions.ts), with no edit of this device
// the server lacks; null while that can't be known: offline, or the server
// not answering, the copy is what there is till it can be read.
async function copyOutdated(session: Session, pageId: string, doc: Y.Doc): Promise<boolean | null> {
  if (serverAway()) return null;
  const outdated = await copyMentionsOutdated(session, pageId, doc);
  if (!outdated) return outdated;
  try {
    const edited = (await listYgoRecoveryLocalEdits(session.user.id)).find((page) => page.pageId === pageId)?.updates ?? [];
    return edited.length === 0 || !(await serverLacks(session, pageId, Y.mergeUpdates(edited)));
  } catch {
    return null;
  }
}

/**
 * The room of a page no tab here shows, for this device's copy of its text
 * the server may lack (lib/keptPageTextSend.ts): opened as a page's own is,
 * showing no one on it, and like it let go with the sign-in it was opened
 * under or a restore of the server, when it tells retired; a change of
 * server leaves the window. close() lets it go otherwise.
 */
export function openCopyRoom(session: Session, pageId: string, document: Y.Doc, retired: () => void) {
  const provider = new WebsocketProvider(collaborationUrl(), pageId, document, {
    connect: false,
    params: roomParams(session),
    disableBc: true,
  });
  provider.awareness.setLocalState(null);
  let open = true;
  let stops: (() => void)[] = [];
  const close = () => {
    if (!open) return;
    open = false;
    for (const stop of stops) stop();
    provider.destroy();
  };
  const retire = () => {
    if (!open) return;
    close();
    retired();
  };
  stops = [subscribeSessionGenerationReset(retire), onServerRestored(retire)];
  return { provider, close };
}

export function usePageCollaboration(
  session: Session | null,
  pageId: string | null,
  options: {
    /** Whether the person may edit the page now, once known; the server refuses their text when not. */
    editable?: boolean;
    /** Told, with the page as it was here, when edits made on this device are let go because the page turned read-only or locked. */
    onRefusedEdits?: RefusedEditsHeard;
    /** Told when the open page turned read-only for the person and nothing of theirs was lost. */
    onTurnedReadOnly?: () => void;
    /** Told, with the page as it was here, when the organization's DLP policy refused text written here. */
    onDLPRefusedEdits?: (document: Y.Doc) => void;
    /**
     * Held by an operation for now (lib/pageBusy.ts): it takes no edits until
     * done, but nothing changed about the person's access, so what was
     * written here is kept, and reaches the page once it takes edits again.
     */
    held?: boolean;
    /**
     * Asked before what was written here is let go: whether an operation
     * holds the page (and says so), so it is kept instead.
     */
    isHeld?: () => Promise<boolean>;
    /**
     * The position of the page's blocks drawn now: the room's text waits for
     * it as a connection opens (lib/roomTextOnJoin.ts). Without, it doesn't.
     */
    blocksShownAt?: () => number;
  } = {},
): PageCollaboration {
  const editable = options.editable;
  // The page's document lives as long as the person and the page; their
  // sign-in may change meanwhile — handed over from another tab, or its token
  // renewed — and only the room's connection and the page's reads follow it
  // (2026-11-14: a sign-in handed over opened the page anew, its body gone
  // for some 20 ms while the copy here was read again).
  const signedIn = useRef(session);
  signedIn.current = session;
  const followSignIn = useRef<((next: Session) => void) | null>(null);
  const held = options.held === true;
  const heldRef = useRef(held);
  heldRef.current = held;
  const isHeld = useRef(options.isHeld);
  isHeld.current = options.isHeld;
  const onRefusedEdits = useRef(options.onRefusedEdits);
  onRefusedEdits.current = options.onRefusedEdits;
  const onTurnedReadOnly = useRef(options.onTurnedReadOnly);
  onTurnedReadOnly.current = options.onTurnedReadOnly;
  const onDLPRefusedEdits = useRef(options.onDLPRefusedEdits);
  onDLPRefusedEdits.current = options.onDLPRefusedEdits;
  const blocksShownAt = useRef(options.blocksShownAt);
  blocksShownAt.current = options.blocksShownAt;
  const joinGate = useRef<ReturnType<typeof holdRoomTextOnJoin> | null>(null);
  // A page whose copy here was let go opens again from the server's.
  const [generation, setGeneration] = useState(0);
  const forgetBeforeOpen = useRef(false);
  // Where the caret was when the DLP policy refused what was written: the
  // page opens again from the server's copy with the caret put back there.
  const caretAfterRefusal = useRef<RefusedCaret | null>(null);
  const closing = useRef<Promise<unknown>>(Promise.resolve());
  const [openedDocument, setDocument] = useState<Y.Doc | null>(() => session && pageId ? pageTextAhead(session.user.id, pageId) : null); // ahead: drawn at once
  const [collaborationState, setCollaborationState] = useState<CollaborationState>(
    () => nextCollaborationState(pageId ? "loading" : "idle"),
  );
  const [openedPresence, setPresence] = useState<PagePresenceSnapshot>(
    EMPTY_PAGE_PRESENCE_SNAPSHOT,
  );
  const [graphCursor] = useState(createGraphCursorWord);
  const [reconnects, setReconnects] = useState(0);
  const [catchUps, setCatchUps] = useState(0);
  // The page the document, its state and its presence above are of. Drawn
  // for another page before the effect below has let them go, they are the
  // last page's: they are not given out for this one, which waits for its
  // own (2026-11-13: a page opened from another first drew its editor with
  // the page left's text document, then again with its own). One whose
  // document was read ahead is drawn with it at once, opened first too (lib/pageTextAhead.ts).
  const [openedFor, setOpenedFor] = useState(pageId);
  const opened = openedFor === pageId;
  const document = opened ? openedDocument : session && pageId ? pageTextAhead(session.user.id, pageId) : null;
  const presence = opened ? openedPresence : EMPTY_PAGE_PRESENCE_SNAPSHOT;
  const waiting = useMemo(() => nextCollaborationState(pageId ? "loading" : "idle"), [pageId]);

  useEffect(() => {
    const ahead = session && pageId && !forgetBeforeOpen.current ? takePageTextAhead(session.user.id, pageId) : null;
    setOpenedFor(pageId);
    setDocument(ahead?.document ?? null);
    setPresence(EMPTY_PAGE_PRESENCE_SNAPSHOT);
    graphCursor.tell(0);
    if (!session || !pageId) {
      setCollaborationState(nextCollaborationState("idle"));
      return;
    }

    let disposed = false;
    const signIn = () => (signedIn.current?.user.id === session.user.id ? signedIn.current : session);
    // Its text goes through its room here, not in the background (lib/keptPageTextSend.ts).
    const letTextGo = holdPageTextOpen(session.user.id, pageId);
    const roomWait = new AbortController();
    let stopAsking = () => {};
    let provider: WebsocketProvider | null = null;
    let forgetTextDocument = () => {};
    let sendHeldText = () => {};
    let stopOnRestore = () => {};
    let stopConnectingOnSight = () => {};
    // A connection closed and coming back reads the page's comments; a return
    // meanwhile waits for that read (lib/commentRefreshEvents.ts).
    let reconnectAwaited: (() => void) | null = null;
    const reconnectNotAwaited = () => { reconnectAwaited?.(); reconnectAwaited = null; };
    let presenceController: ReturnType<typeof createPagePresenceController> | null = null;
    let presenceStatus: PagePresenceConnectionStatus = "connecting";
    let presenceRecoveryFailed = false;
    let presenceConnectedOnce = false;
    const refreshPresence = (
      nextStatus = presenceStatus,
      nextRecoveryFailed = presenceRecoveryFailed,
    ) => {
      presenceStatus = nextStatus;
      presenceRecoveryFailed = nextRecoveryFailed;
      presenceController?.refresh(presenceStatus, presenceRecoveryFailed);
    };
    const storageStatusOwner = createReadGenerationOwner();
    let storageStatusContext: ReadGenerationContext | null = {
      principalId: session.user.id,
      sessionToken: signIn().token,
      route: `collaboration-storage:${pageId}`,
    };
    storageStatusOwner.observe(storageStatusContext);
    const ydoc = ahead?.document ?? new Y.Doc();
    // The editor's undo follows its own text changes from the first keystroke.
    blockTextUndoManager(ydoc);
    const reportRecoveryFailure = (error: unknown, stage: PageCopyFailureStage) => {
      const failure = collaborationRecoveryFailure(error, stage);
      refreshPresence(undefined, true);
      if (!disposed) setCollaborationState(failure);
    };
    const { recovery, scheduleCompaction, stop: stopCompaction } = keepPageCopy(session.user.id, pageId, reportRecoveryFailure);
    setCollaborationState(nextCollaborationState("loading"));

    // The room's updates are kept as they come, this tab's own text as it goes (lib/collaborationRecovery.ts).
    const { collect: collectRecovery, keepOwn: keepOwnText } = recoveryIntake(recovery, recoveryOrigin, () => provider);
    // What another tab here wrote while away and closed before it could send is in the page's copy: a return reads it in
    // behind the page's changes kept here, and the room takes it (2026-11-13: a line written in a tab closed offline came
    // back empty, here and on the server, until the page was opened again; lib/pageTextBehindKeptChanges.ts).
    const takeKeptText = () => void afterKeptChangesWent(pageId, roomWait.signal).then(() => replayYgoRecoveryUpdates(session.user.id, pageId, (update) => {
      if (!disposed) Y.applyUpdate(ydoc, update, recoveryOrigin);
    })).catch(() => undefined);
    const flushForPageHide = () => { void recovery.flush(); };

    // A copy being let go is gone before the page opens from the server's.
    const forget = forgetBeforeOpen.current;
    forgetBeforeOpen.current = false;
    const opened = forget
      ? closing.current.catch(() => undefined).then(() => forgetYgoRecoveryPage(session.user.id, pageId))
      : Promise.resolve();
    let fromCopy = ahead?.fromCopy ?? false;
    // The page's words are read once over the connection its other reads
    // use, as its copy here is read: its room's new connection takes two
    // round trips more before it answers (2026-11-13: the body came some
    // 150 ms after its discussion, a NAS-class server 100 ms away).
    const wordsAhead = new AbortController();
    roomWait.signal.addEventListener("abort", () => wordsAhead.abort(), { once: true });
    const words = afterPageMade(pageId, roomWait.signal).then(async () => (serverWasRestored() || wordsAhead.signal.aborted || await wordsCameWithBlocks(pageId) ? null
      : requestBinaryResponse(`/collaboration-state/${encodeURIComponent(pageId)}/missing`, signIn(), {
        // An empty state vector: the whole document.
        method: "POST", body: new Uint8Array([0]) as BodyInit,
        headers: { "content-type": "application/octet-stream" }, signal: wordsAhead.signal,
      }).then(async (response) => new Uint8Array(await response.arrayBuffer())))).catch(() => null);
    void opened.then(() => ahead ? undefined : tryingRecoveryAgain(() => replayYgoRecoveryUpdates(session.user.id, pageId, (update) => {
      fromCopy = true;
      Y.applyUpdate(ydoc, update, recoveryOrigin);
    }), (delayMs) => new Promise((resolve) => { window.setTimeout(resolve, delayMs); }))).then(() => {
      if (disposed) return;
      // A copy whose mentions stand otherwise to the reader now is let go and
      // the page opens again from the server's, unless it holds edits the
      // server lacks; it opens at once and is read meanwhile. One that can't
      // be read yet (offline, the server not answering) is read once the
      // page is in step with the server again.
      let copyUnread = fromCopy, copyReading = false;
      const readCopy = () => {
        if (!copyUnread || copyReading || disposed) return;
        copyReading = true;
        void copyOutdated(signIn(), pageId, ydoc).then((outdated) => {
          copyReading = false;
          if (outdated === null || disposed) return;
          copyUnread = false;
          if (!outdated) return;
          forgetBeforeOpen.current = true;
          setGeneration((current) => current + 1);
        });
      };
      readCopy();
      ydoc.on("update", collectRecovery);

      // The connection names the history the page's copy is from; a
      // restored server refuses it, so the copy can't carry that back.
      provider = new WebsocketProvider(collaborationUrl(), pageId, ydoc, {
        connect: false,
        params: roomParams(signIn()),
        disableBc: true,
        maxBackoffTime: 2500,
      });
      // What is written here goes after the block changes made with it (lib/textBehindBlockChanges.ts), each connection's text behind
      // those kept here (lib/pageTextBehindKeptChanges.ts); the words read with the blocks go in as drawn (lib/pageWordsWithBlocks.ts).
      sendHeldText = openWordsWithBlocks(pageId, ydoc, provider, holdTextBehindBlockChanges(provider, ydoc, pageId, keepOwnText));
      holdRoomTextBehindKeptChanges(provider, pageId, roomWait.signal, () => { if (!disposed) setCatchUps((current) => current + 1); });
      // What the room brings as it opens waits for the page's blocks to stand where its did.
      joinGate.current = holdRoomTextOnJoin(provider, ydoc, () => blocksShownAt.current?.() ?? Number.MAX_SAFE_INTEGER);
      // After the text held goes as the page goes, so the copy keeps it too.
      window.addEventListener("pagehide", flushForPageHide);
      // What the server tells the room: block changes, comments and rows read (lib/pageRoomAnnouncements.ts).
      hearRoomAnnouncements(provider.awareness, pageId, graphCursor, () => disposed);
      presenceController = createPagePresenceController({
        awareness: provider.awareness,
        identity: session.user,
        isCurrent: () => !disposed && provider !== null,
        onSnapshot: (snapshot) => {
          setPresence(snapshot);
          setPagePresenceSnapshot(pageId, snapshot);
        },
        pageId,
      });
      refreshPresence("connecting", false);
      // The room's storage as last read, which a room this tab's own lock
      // change closed comes back with (lib/ownLockFlips.ts).
      let storageRead: Awaited<ReturnType<typeof api.collaborationStorageStatus>> | null = null;
      const readStorageStatus = (fallback: "disconnected" | "synced") => {
        if (!storageStatusContext) return;
        const read = storageStatusOwner.begin(storageStatusContext, () => storageStatusContext);
        void api.collaborationStorageStatus(signIn(), pageId, read.signal).then((status) => {
          if (disposed || !read.isCurrent()) return;
          storageRead = status;
          setCollaborationState((current) => collaborationStorageTransition(
            current,
            status,
            fallback,
          ));
        }).catch((error) => {
          if (disposed || !read.isCurrent()) return;
          setCollaborationState(collaborationStatusReadFailure(error, fallback));
        });
      };
      // A change of the page's lines told as it is committed, with its text (lib/roomGraphChange.ts).
      provider.messageHandlers[ROOM_GRAPH_CHANGE_FRAME] = (_encoder, decoder, room) => {
        if (!disposed) hearRoomGraphFrame(pageId, decoder, (words) => Y.applyUpdate(ydoc, words, room));
      };
      // The connection is kept by asking it, and started again when it doesn't open (lib/pageRoomLiveness.ts).
      stopAsking = keepRoomConnectionAsked(provider, () => disposed);
      // Whether the connection closed since it last synced: what the person
      // may do may have changed meanwhile, unseen while away.
      let closedSinceSync = false;
      provider.on("status", ({ status: nextStatus }: { status: "connected" | "disconnected" | "connecting" }) => {
        if (disposed) return;
        // Opened, it catches up. Opening again after it closed, it is still
        // lost; its first opening is on its way, however far the server is
        // (lib/collaborationStatus.ts collaborationLost).
        setCollaborationState((current) => collaborationTransportState(current, nextStatus === "connected" ? "connecting"
          : nextStatus === "connecting" && closedSinceSync ? "disconnected" : nextStatus));
        if (nextStatus === "connected") {
          presenceConnectedOnce = true;
          refreshPresence("connecting");
        } else if (nextStatus === "connecting") {
          refreshPresence(presenceConnectedOnce ? "reconnecting" : "connecting");
        } else {
          refreshPresence("disconnected");
        }
      });
      // Whether the page's state and what changed were asked since it last
      // synced, as its connection closed (below).
      let breakAsked = false;
      // A connection closed while the page was out of sight, to open once it shows.
      let connectInSight = false;
      const connectOnSight = () => {
        if (disposed || !connectInSight || window.document.visibilityState !== "visible") return;
        connectInSight = false;
        if (!serverWasRestored() && !isLeavingPage(pageId)) provider?.connect();
        else reconnectNotAwaited();
      };
      window.document.addEventListener("visibilitychange", connectOnSight);
      stopConnectingOnSight = () => window.document.removeEventListener("visibilitychange", connectOnSight);
      provider.on("connection-close", () => {
        if (disposed) return;
        closedSinceSync = true;
        // A connection that had opened asks the page's state and what changed
        // as it closes. Attempts that never open after that, the server out
        // of reach, ask nothing more and keep what the ask found: the visible
        // tab asked at each, every 0.2 to 2.5 seconds (2026-11-13: 5 state and
        // 7 change reads in a 12-second outage). The sync after reads both.
        const asking = provider?.wsconnected === true || !breakAsked;
        setCollaborationState(asking ? nextCollaborationState("disconnected")
          : (current) => collaborationTransportState(current, "disconnected"));
        refreshPresence("disconnected");
        // Closed for this tab's own lock change, the room opens again at
        // once under it: nothing changed that this tab doesn't show.
        if (ownLockFlip(pageId)) return;
        // Closed as this tab deletes the page's workspace: it stays closed and
        // nothing is asked of the gone workspace (lib/workspaceLeaving.ts).
        // The socket is closing already, so it is only not opened again, as
        // out of sight below: disconnect() here closed the same socket again,
        // which told this handler again, until the stack ran out (2026-11-13).
        if (isLeavingPage(pageId)) {
          if (provider) provider.shouldConnect = false;
          return;
        }
        reconnectAwaited ??= awaitCommentReconnect(pageId);
        // Out of sight, a closed connection opens again once the page shows,
        // as a hidden tab's other asks wait for sight (lib/followAcrossTabs.ts):
        // Chrome freezes a hidden page now and then and closes its socket as
        // it wakes, and every hidden tab connected again, reading its page's
        // status, comments and blocks anew, every minute or two (2026-11-13).
        // Opened again, it catches up and reads them once.
        if (window.document.visibilityState !== "visible" && provider) {
          provider.shouldConnect = false;
          connectInSight = true;
          return;
        }
        if (asking) {
          breakAsked = true;
          readStorageStatus("disconnected");
          // The server closes the page's connection when the person's access
          // to it changed; what they may do now is read at once, not at the
          // next periodic check.
          requestWorkspaceChangeCheck();
        }
        // A server restored meanwhile refuses the connection for good: asked
        // at most every ten seconds (lib/dataEpoch.ts).
        checkServerDataEpochSoon(session.user.id);
      });
      provider.on("closed", ({ code }: { code: number; reason: string }) => {
        if (disposed) return;
        // A connection the server ended for good isn't opened again on sight,
        // and a return waiting for its comments reads them now.
        connectInSight = false;
        reconnectNotAwaited();
        // The organization's DLP policy refused text written here: it isn't
        // sent again, the page opens from the server's copy and says why.
        if (code === DLP_REFUSED_CLOSE_CODE) {
          provider?.disconnect();
          caretAfterRefusal.current = caretHere();
          forgetBeforeOpen.current = true;
          onDLPRefusedEdits.current?.(ydoc);
          setGeneration((current) => current + 1);
          return;
        }
        refreshPresence(pagePresenceTerminalStatus(code));
      });
      provider.on("sync", (synced: boolean) => {
        if (!disposed && synced) {
          readCopy();
          // Back after being away, what the person may do is read again: the
          // server refuses text written while their access turned read-only,
          // which the page then lets go and says so (2026-10-16: the tab
          // stayed editable, holding refused text, and sent it once access
          // came back). Its comments are read again too: what changed while
          // away wasn't announced to it. A first sync isn't a return — the
          // page's own read brings its comments, as it brings its blocks
          // (2026-11-12: a page's comments were read twice as it opened).
          // Back from this tab's own lock change, it only catches up on the
          // page's changes since it showed it (lib/ownLockFlips.ts).
          const ownFlip = closedSinceSync && takeOwnLockFlip(pageId);
          if (ownFlip) setCatchUps((current) => current + 1);
          else if (closedSinceSync) {
            takeKeptText();
            requestWorkspaceChangeCheck();
            setReconnects((current) => current + 1);
            announceRoomReturn(pageId);
          }
          closedSinceSync = false;
          breakAsked = false;
          reconnectNotAwaited();
          const caret = caretAfterRefusal.current;
          caretAfterRefusal.current = null;
          if (caret) putCaretBack(caret);
          refreshPresence("connected", false);
          const known = storageRead;
          if (ownFlip && known) setCollaborationState((current) => collaborationStorageTransition(current, known, "synced"));
          else readStorageStatus("synced");
        }
      });
      // A new sign-in of the person keeps the page's document: the room's next
      // connection goes with it, and one closed or still trying with the one
      // let go opens again with it now; an open one keeps going.
      followSignIn.current = (next) => {
        if (disposed || !provider) return;
        provider.params = roomParams(next);
        if (storageStatusContext) storageStatusOwner.observe(storageStatusContext = { ...storageStatusContext, sessionToken: next.token });
        if (provider.shouldConnect && !provider.wsconnected) {
          provider.disconnect();
          provider.connect();
        }
      };
      forgetTextDocument = notePageTextDocument(pageId, ydoc, session.user.id);
      setDocument(ydoc);
      // Its words read ahead come as the room's own update: neither sent back
      // nor kept as an edit made here. A copy here holds them already.
      if (fromCopy) wordsAhead.abort();
      else void words.then((update) => { if (update && !disposed && provider) Y.applyUpdate(ydoc, update, provider); });
      setCollaborationState(nextCollaborationState("connecting"));
      // Once the server is known to be restored, the copy stays here,
      // unsent, until it is let go. A page made on this device has its room
      // once the server made it, which refuses it before (pagesNotYetMade).
      void afterPageMade(pageId, roomWait.signal).then(() => {
        if (!disposed && !serverWasRestored()) provider?.connect();
      });
      stopOnRestore = onServerRestored(() => provider?.disconnect());
      scheduleCompaction();
    }).catch((error) => {
      const failure = collaborationRecoveryFailure(error, "browser_recovery_replay");
      if (!disposed) {
        setPresence({
          ...EMPTY_PAGE_PRESENCE_SNAPSHOT,
          recoveryFailed: true,
          status: "disconnected",
        });
        setCollaborationState(failure);
      }
    });

    return () => {
      disposed = true;
      followSignIn.current = null;
      roomWait.abort();
      stopAsking();
      stopOnRestore();
      stopConnectingOnSight();
      reconnectNotAwaited();
      storageStatusOwner.retire();
      storageStatusContext = null;
      stopCompaction();
      window.removeEventListener("pagehide", flushForPageHide);
      // The text held goes first, kept in the copy as it goes.
      sendHeldText();
      joinGate.current?.stop();
      joinGate.current = null;
      closing.current = recovery.dispose();
      ydoc.off("update", collectRecovery);
      if (presenceController) presenceController.dispose();
      setPagePresenceSnapshot(pageId, null);
      forgetTextDocument();
      provider?.destroy();
      ydoc.destroy();
      void closing.current.catch(() => undefined).then(letTextGo);
    };
    // The session is read through signedIn: its token alone changing is followed below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session?.user.id, pageId, generation, graphCursor]);
  useEffect(() => {
    if (session) followSignIn.current?.(session);
  }, [session, session?.token]);

  useRefusedTextLetGo({
    session, pageId, document, editable, held, heldRef, isHeld, onRefusedEdits, onTurnedReadOnly,
    letGo: () => {
      forgetBeforeOpen.current = true;
      setGeneration((current) => current + 1);
    },
  });

  const changeText = useCallback((blockId: string, nextText: string) => {
    if (document) replaceText(document, blockId, nextText);
  }, [document]);

  const blocksShown = useCallback(() => joinGate.current?.shown(), []);
  return { document, ...(opened ? collaborationState : waiting), changeText, graphCursor, reconnects, catchUps, presence, blocksShown };
}
