import type { ProductStatusSink } from "@/lib/productStatus";
import { isGuestMFARequired } from "@/lib/guestMFARequired";
import { i18next } from "@/i18n";
import { useEffect, useMemo, useRef, useState, type MutableRefObject } from "react";
import { collectOpenedPageGraph } from "@/lib/authenticatedContentGraph";
import { showWordsWithBlocks } from "@/lib/pageWordsWithBlocks";
import { authenticatedProductErrorMessage } from "@/lib/authenticatedProductErrors";
import { createLocalFirstSnapshotLifecycle } from "@/lib/localFirstSnapshotLifecycle";
import { localFirstProductCache as recordCache, type CachedContentGraph } from "@/lib/localFirstProductCache";
import { offlinePagePinService } from "@/lib/offlinePagePins";
import { followPinnedPages } from "@/lib/offlinePagePinFreshen";
import { cacheBehind, clearCacheBehind, markCacheBehind, pageGraphCache } from "@/lib/cacheStaleness";
import { emptyContentGraph, type ContentGraph } from "@/contentGraph";
import { readingKeptWrites, showKeptWrites, type KeptGraphWrites, type KeptWritesRead } from "./snapshotKeptWrites";
import { routeInfoFromPath } from "@/lib/router";
import type { Session } from "@/types";
import { serverIdentity } from "@/lib/serverOrigin";
import { graphAsShown } from "@/lib/keptGraph";
import { graphReadAheadOr } from "@/lib/graphReadAhead";
import { forgetShownGraph, holdShownGraph, shownGraph } from "@/lib/shownGraphs";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { useAccessRevision } from "@/lib/workspaceChangeCheck";
import { LeafenApiError, request } from "@/apiTransport";
import { answeredUnlessAway, isServerUnreachable, retryWhenServerMayAnswer } from "@/lib/serverReach";
import { followAcrossTabs, tellTabs } from "@/lib/followAcrossTabs";
import { copyTopReadAgain, pageAccessTopic } from "@/lib/pageCopyProgress";
import { hearPageHeld, keepingHeldRefusal, pageBusyCopyingDatabases } from "@/lib/pageBusy";
import { isDefinitiveReadDenial, refusedDuringSignInHandover } from "@/lib/readDenial";
import type { PageActionAccess } from "@/workspaceMembershipWire.generated";

// The open page's cached graph the cached list was drawn with: the page's
// own read keeps it on screen, with the access it was kept with, instead of
// placeholder lines while it reads again.
let preparedGraph: { pageId: string; principalId: string; canEdit?: boolean; canSuggest?: boolean } | null = null;

// The cached list is drawn with the open page's cached graph when one is
// kept, from whichever list it was read with — the server's answer replaces
// both. A list isn't held back for an older page graph: starting again then
// drew an empty workspace first (2026-10-16). A long page's body is drawn
// after the workspace instead, as a page being opened is: drawing 3,000
// paragraphs held the first screen for 3 seconds.
const FIRST_SCREEN_BODY_BLOCKS = 400;

export async function prepareCachedProductRoute(input: {
  isCurrent(): boolean;
  onGraph(graph: ContentGraph): void;
  pathname: string;
  principalId: string;
  serverKey: string;
}): Promise<boolean> {
  const route = routeInfoFromPath(input.pathname);
  const requestedPageId = new URL(window.location.href).searchParams.get("page")
    ?? (route.kind === "page" ? route.pageId : null);
  if (!requestedPageId) return true;
  const cached = await recordCache.graph.readCache({
    key: requestedPageId,
    principalId: input.principalId,
    serverKey: input.serverKey,
  });
  if (!input.isCurrent()) return false;
  if (!cached?.complete
    || (cacheBehind(input.principalId, pageGraphCache(requestedPageId)) && navigator.onLine)
    || cached.key !== requestedPageId
    || cached.principalId !== input.principalId
    || cached.serverKey !== input.serverKey
    || cached.value.graph.blocks.length > FIRST_SCREEN_BODY_BLOCKS) return true;
  input.onGraph(cached.value.graph);
  preparedGraph = { pageId: requestedPageId, principalId: input.principalId, canEdit: cached.value.canEdit, canSuggest: cached.value.canSuggest };
  return true;
}

// How often a page an operation holds asks whether it is done.
const BUSY_CHECK_MS = 3_000;
// A copy's top fills in as soon as its own body is made.
const COPY_CHECK_MS = 1_000;
// A workspace moved to another server takes edits again only if the move failed.
const WORKSPACE_MOVED_CHECK_MS = 60_000;
// A page that shows nothing yet, refused while the sign-in is handed over, asks again then.
const HANDOVER_READ_AGAIN_MS = 2_000;

export function useLocalFirstContentGraph(input: {
  bootstrapRevision: number;
  /**
   * The page's place in the list — its parent, teamspace and kind —
   * which reads it again when it changes; other changes of the list don't.
   */
  pageEntryKey?: string;
  /** The graph on screen, which is kept as the page's cached copy as it changes. */
  graph: ContentGraph;
  /**
   * Where the page's graph is held apart from `graph` (lib/pageGraphStore.ts),
   * whose rows' values change without it: the copy kept is the store's.
   */
  graphStore?: Pick<PageGraphStore, "get" | "subscribe">;
  /** The page revision the graph shows, which its writes follow. */
  graphRevision: MutableRefObject<number>;
  onGraph(graph: ContentGraph): void;
  /** Whether the reader may change the page, as the server last said. */
  onCanEdit(canEdit: boolean): void;
  /** Whether the reader may suggest edits to the page's text, as the server last said. */
  onCanSuggest(canSuggest: boolean): void;
  /** The operation holding the page for now (copied, blocks moved, …), or null. */
  onBusy?(busy: string | null): void;
  onLoading(loading: boolean): void;
  /**
   * The page whose own read has ended showing it (from the server, or its
   * cached copy when the server couldn't answer), or null while a page is
   * still being read: what changed after it is followed from then.
   */
  onRead?(pageId: string | null): void;
  /** Whether the open page couldn't be read and nothing of it shows; it is read again. */
  onUnavailable?(unavailable: boolean): void;
  /**
   * Whether the page's organization keeps it from the reader until they turn
   * on 2-step verification, as the server last said.
   */
  onGuestMFARequired?(required: boolean): void;
  onStatus: ProductStatusSink;
  pageId: string | null;
  /**
   * The writes of the page's structure changes kept for the server, which a
   * cached copy and the server's answer show on top but where the copy shows
   * them already (snapshotKeptWrites.ts).
   */
  pendingWrites?: (pageId: string) => Promise<KeptGraphWrites>;
  /** This tab's writes of a page kept or sent for the server and not yet answered, which what it shows holds. */
  writesShown?: (pageId: string) => readonly string[];
  /** Reads a page once the reader's own writes to it are saved; `here`, those made in this tab. */
  readSettled<Value>(pageId: string, read: () => Promise<Value | null>, here?: boolean): Promise<Value | null>;
  session: Session;
}) {
  const { graphRevision } = input;
  const accessRevision = useAccessRevision();
  const lifecycle = useMemo(() => createLocalFirstSnapshotLifecycle<CachedContentGraph>({
    ...recordCache.graph,
    isDefinitiveDenial: isDefinitiveReadDenial,
  }), []);
  // The page the graph shows once a read of it was shown, and where from.
  const shownPageId = useRef<string | null>(null);
  const shownSource = useRef<string | null>(null);
  // The writes kept for the server, not this tab's, that what the page shows
  // holds: those its copy showed and those shown on top of it.
  const otherWritesShown = useRef<{ pageId: string | null; ids: readonly string[] }>({ pageId: null, ids: [] });
  // The graph as read or as cached, which needs no keeping, and what the
  // server last said the reader may do with the page.
  const readGraph = useRef<ContentGraph | null>(null);
  const shownAccess = useRef<{ canEdit?: boolean; canSuggest?: boolean }>({});
  const readSettled = useRef(input.readSettled);
  readSettled.current = input.readSettled;
  // A page that couldn't be read is read again: soon, then less often, and at
  // once when the browser is back online.
  const [retry, setRetry] = useState(0);
  const failed = useRef<{ pageId: string; times: number } | null>(null);
  useEffect(() => {
    const again = () => { if (failed.current) setRetry((current) => current + 1); };
    window.addEventListener("online", again);
    return () => window.removeEventListener("online", again);
  }, []);

  useEffect(() => {
    if (!input.pageId) {
      shownPageId.current = null;
      graphRevision.current = 0;
      input.onGraph(emptyContentGraph);
      input.onCanEdit(true);
      input.onCanSuggest(false);
      return;
    }
    let cancelled = false;
    // The page on screen is read again when the person's access to it
    // changed (below) or its place did; its own changes reach it through its
    // room (2026-10-16: a large page read its whole graph again, and froze,
    // for every page made elsewhere). It keeps what it shows until the
    // server answers: its cached copy is older.
    const rereading = shownPageId.current === input.pageId;
    // Until its own read ends, a page follows no changes: one followed from
    // the cached copy drawn first, its position unknown or older, read the
    // whole graph again beside the page's own read (2026-11-11: a table of
    // 100,000 rows read its thousand windows twice once it opened).
    if (!rereading) input.onRead?.(null);
    const prepared = preparedGraph?.pageId === input.pageId && preparedGraph.principalId === input.session.user.id ? preparedGraph : null;
    preparedGraph = null;
    if (prepared) {
      input.onCanEdit(prepared.canEdit !== false);
      input.onCanSuggest(prepared.canSuggest === true);
    } else if (!rereading) {
      // Another page's graph doesn't stand in for this one while it is read
      // (2026-10-16: offline, a page never read showed the page before it,
      // and what was typed there went nowhere).
      if (shownPageId.current !== null) {
        shownPageId.current = null;
        graphRevision.current = 0;
        readGraph.current = null;
        input.onGraph(emptyContentGraph);
      }
      input.onLoading(true);
      input.onGuestMFARequired?.(false);
    }
    if (failed.current && failed.current.pageId !== input.pageId) failed.current = null;
    let retryTimer = 0;
    let stopWaiting: (() => void) | undefined;
    const loadStarted = Date.now();
    // The page's words read with its blocks, which go into its text as they are drawn, and the outbox's kept writes read beside them.
    let readWords: string | undefined;
    let keptAtRead: KeptWritesRead | undefined;
    void lifecycle.load({
      // A copy behind what a tab here showed waits for the server, changes
      // the outbox keeps for it or not: the read that opens the page waits
      // for none of them (below) but draws with them on top, as a server
      // that can't answer draws the copy (2026-11-05: a moved or stopped
      // server never took them, and a read waiting for them left the page a
      // skeleton).
      cacheBehind: () => cacheBehind(input.session.user.id, pageGraphCache(input.pageId!)),
      // What showed of it last in this tab draws it at once (shownGraphs.ts);
      // a page made here draws from the copy it was made with (pageMadeHere.ts).
      held: rereading || prepared ? undefined : () => shownGraph(input.session.user.id, input.pageId!),
      isCurrent: () => !cancelled,
      key: input.pageId,
      // The server's answer is the cached copy now, once the store took it;
      // one it refused is kept as a change made here is.
      onNetworkKept: (kept, value) => {
        if (kept) clearCacheBehind(input.session.user.id, pageGraphCache(input.pageId!), loadStarted);
        else if (!cancelled && readGraph.current === value.graph) {
          readGraph.current = null;
          setKeepAgain((current) => current + 1);
        }
      },
      onDefinitiveDenial: () => {
        forgetShownGraph(input.session.user.id, input.pageId!);
        shownPageId.current = null;
        input.onGraph(emptyContentGraph);
      },
      onSnapshot(next, source) {
        if (cancelled) return;
        if (rereading && source !== "network") return false;
        shownPageId.current = input.pageId;
        shownSource.current = source;
        graphRevision.current = next.graphRevision;
        readGraph.current = next.graph;
        shownAccess.current = { canEdit: next.canEdit, canSuggest: next.canSuggest };
        if (source === "network") showWordsWithBlocks(input.pageId!, readWords);
        const pageId = input.pageId!;
        // What the outbox keeps for the page shows on top of a copy here and
        // of the server's answer alike, read beside the answer (snapshotKeptWrites.ts).
        showKeptWrites(next, source === "network" ? keptAtRead : source === "cache" ? input.pendingWrites?.(pageId) : undefined, (graph, ids) => {
          otherWritesShown.current = { pageId, ids };
          if (graph) input.onGraph(graph);
        }, () => !cancelled && shownSource.current === source);
        input.onCanEdit(next.canEdit !== false);
        input.onCanSuggest(next.canSuggest === true);
        if (source === "network") setBusy(next.busy ? { pageId: input.pageId!, reason: next.busy } : null);
        input.onGuestMFARequired?.(false);
        input.onLoading(false);
        failed.current = null;
        input.onUnavailable?.(false);
        if (source !== "held") input.onStatus(
          source === "cache" ? i18next.t("syncStatusBadge:status.pageOpenedFromCache") : i18next.t("syncStatusBadge:status.synced"),
        );
      },
      principalId: input.session.user.id,
      async readNetwork() {
        // Read once the server holds the changes made in this tab (another
        // tab's come through the room — 2026-11-13: waiting for them drew a
        // merged line beside its emptied one), taking one read ahead on the
        // press (graphReadAhead.ts). It fails at once while the connection is
        // known lost (answeredUnlessAway): a page shown nothing of says it
        // couldn't open, as a refused network has it say, rather than reading
        // on (2026-11-16: on a stalled network a page not on this device stood
        // as a grey line past 70 s, the top bar saying "오프라인" all along).
        const opening = keepingHeldRefusal(() => graphReadAheadOr(input.session, input.pageId!, () => collectOpenedPageGraph(input.session, input.pageId!, () => !cancelled)), !rereading);
        const withKept = readingKeptWrites(opening.read, input.pendingWrites && (() => input.pendingWrites!(input.pageId!)));
        const result = await answeredUnlessAway(readSettled.current(input.pageId!, withKept.read, !rereading));
        // One a hold refused (it comes to nothing there, lib/pageBusy.ts) waits as it opens (below), not as superseded; read again, a page shown stays as it is.
        if (!result) throw opening.kept() ?? new DOMException("Content graph load was superseded.", "AbortError");
        readWords = result.words;
        keptAtRead = await withKept.kept();
        return {
          bootstrapRevision: input.bootstrapRevision,
          graphRevision: result.snapshotRevision,
          graph: result.graph,
          canEdit: result.canEdit,
          canSuggest: result.canSuggest,
          busy: result.busy,
        };
      },
      serverKey: serverIdentity(),
    }).then((result) => {
      if (!cancelled && result) input.onRead?.(input.pageId);
      if (!cancelled && result?.source === "network") {
        void offlinePagePinService.retry(input.session.user.id);
      }
    }).catch((error: unknown) => {
      if (cancelled) return;
      if (shownPageId.current === input.pageId) input.onRead?.(input.pageId);
      input.onLoading(false);
      if (isGuestMFARequired(error) && input.onGuestMFARequired) {
        input.onGuestMFARequired(true);
        return;
      }
      const pageId = input.pageId!;
      // Held by an operation under way: what shows stays, and is read again
      // once it's done; one that shows nothing yet waits as it opens, rather
      // than saying it couldn't (2026-11-14: a row's copy opened while its
      // database was copied into it said it couldn't open, and "locked").
      if (error instanceof LeafenApiError && error.code === "LEAFEN_PAGE_BUSY") {
        input.onCanEdit(false);
        if (shownPageId.current !== pageId) input.onLoading(true);
        setBusy({ pageId, reason: "other" });
        return;
      }
      // Refused while this browser hands its sign-in over: nothing is said,
      // what shows stays, and the page is read again with the next sign-in,
      // which reads it anew (lib/readDenial.ts). One that shows nothing yet
      // waits as it opens, and asks again a while later had none come.
      if (refusedDuringSignInHandover(error)) {
        if (shownPageId.current === pageId) return;
        input.onLoading(true);
        retryTimer = window.setTimeout(() => setRetry((current) => current + 1), HANDOVER_READ_AGAIN_MS);
        return;
      }
      const times = failed.current?.pageId === pageId ? failed.current.times : 0;
      // The server just unreachable, a page that shows nothing yet says so in
      // its place (EditorSurface); one that shows stays open, and the
      // connection's own notice says the rest after ten seconds; either is
      // read again when the server may answer, so it opens as the connection
      // comes back. Neither reports a failure (2026-11-11: a copy open on
      // screen said it couldn't open as its server stopped).
      const unreachable = isServerUnreachable(error) && !isDefinitiveReadDenial(error);
      if (times === 0 && !unreachable) input.onStatus(authenticatedProductErrorMessage(error, "contentGraphRead"), "error");
      if (unreachable) stopWaiting = retryWhenServerMayAnswer(() => { stopWaiting?.(); stopWaiting = undefined; setRetry((current) => current + 1); });
      if (unreachable && shownPageId.current === pageId) return;
      if (isDefinitiveReadDenial(error) || shownPageId.current === pageId) return;
      // Nothing of the page shows: it says so instead of an editor, and is
      // read again.
      failed.current = { pageId, times: times + 1 };
      input.onUnavailable?.(true);
      retryTimer = window.setTimeout(() => setRetry((current) => current + 1), Math.min(30_000, 2_000 * 2 ** Math.min(times, 4)));
    });
    return () => {
      cancelled = true;
      window.clearTimeout(retryTimer);
      stopWaiting?.();
    };
  }, [
    retry,
    graphRevision,
    input.pageEntryKey,
    input.onGraph,
    input.onCanEdit,
    input.onCanSuggest,
    input.onLoading,
    input.onRead,
    input.onUnavailable,
    input.onGuestMFARequired,
    input.onStatus,
    input.pageId,
    input.pendingWrites,
    input.session,
    lifecycle,
  ]);

  // While an operation holds the page, it asks every few seconds whether it
  // is done, and is read again then: one of the person's tabs in sight asks
  // for all of them, and a tab behind asks nothing (followAcrossTabs). A
  // page whose read was refused for a hold of a kind not told asks at once
  // which, as its body shows as being filled while its databases are copied
  // into it (lib/pageDatabaseCopies.ts).
  const [busy, setBusy] = useState<{ pageId: string; reason: string } | null>(null);
  const heldNow = busy?.pageId === input.pageId ? busy : null;
  const shownBusy = heldNow?.reason ?? null;
  useEffect(() => { input.onBusy?.(shownBusy); }, [input.onBusy, shownBusy]);
  // A later read of it refused for a hold holds it so too (lib/pageBusy.ts heldRead).
  useEffect(() => hearPageHeld((pageId) => { if (pageId === input.pageId) setBusy((held) => held?.pageId === pageId ? held : { pageId, reason: "other" }); }), [input.pageId]);
  useEffect(() => {
    const pageId = input.pageId;
    if (!heldNow || !pageId) return;
    const reason = heldNow.reason;
    // Once done it is read again once, and follows again only if that read
    // says it is held still (each read's hold is a new one): followed on,
    // each look that heard it done began the read again, so a read slower
    // than a look never ended, and the page stayed held (2026-11-14: a page
    // filled with two databases' copies, read in some two seconds on a
    // NAS-class server, was read again and again every second).
    let done = false;
    let stop: (() => void) | undefined;
    const topic = pageAccessTopic(input.session.user.id, pageId);
    const copying = reason === "copying" || reason === pageBusyCopyingDatabases;
    stop = followAcrossTabs(topic,
      reason === "workspaceMoved" || reason === "workspaceSending" ? WORKSPACE_MOVED_CHECK_MS : copying ? COPY_CHECK_MS : BUSY_CHECK_MS,
      () => request<PageActionAccess>(`/pages/${encodeURIComponent(pageId)}?accessOnly=1`, {}, input.session).then((access) => tellTabs(topic, access)),
      (value) => {
        if (done || !value) return;
        const next = (value as PageActionAccess).busy;
        // Held still, by what it says; a copy's top read again once its body is written (lib/pageCopyProgress.ts).
        if (next) {
          if (next !== reason) setBusy({ pageId, reason: next });
          else if (copyTopReadAgain(pageId, (value as PageActionAccess).copyTopWritten)) setRetry((current) => current + 1);
          return;
        }
        // Done: read again, which clears the hold with the page's access.
        done = true;
        stop?.();
        setRetry((current) => current + 1);
      }, { now: reason === "other" });
    if (done) stop();
    return () => { done = true; stop?.(); };
  }, [heldNow, input.pageId, input.session]);

  // When the person's access may have changed, the page's own access is
  // asked — one small read — and the whole page is read again only if it
  // differs from what it shows, or can't be read.
  const accessSeen = useRef(accessRevision);
  useEffect(() => {
    if (accessSeen.current === accessRevision) return;
    accessSeen.current = accessRevision;
    const pageId = input.pageId;
    if (!pageId || shownPageId.current !== pageId) return;
    let cancelled = false;
    void request<PageActionAccess>(`/pages/${encodeURIComponent(pageId)}?accessOnly=1`, {}, input.session).then((access) => {
      if (cancelled || shownPageId.current !== pageId) return;
      if (access.canEdit !== (shownAccess.current.canEdit !== false) || (access.canSuggest === true) !== (shownAccess.current.canSuggest === true)) {
        setRetry((current) => current + 1);
      }
    }, () => {
      if (!cancelled && shownPageId.current === pageId) setRetry((current) => current + 1);
    });
    return () => { cancelled = true; };
  }, [accessRevision, input.pageId, input.session]);

  // The page as shown, with what was changed in it here since it was read,
  // is kept as its cached copy, so it opens without the server as it was
  // left (2026-10-03: lines added online were missing offline). Changes the
  // outbox still keeps show on top of it where it lacks them.
  const { bootstrapRevision, graph, graphStore, pageId } = input;
  const principalId = input.session.user.id;
  const writesShown = useRef(input.writesShown);
  writesShown.current = input.writesShown;
  // The graph last kept, which a list read again doesn't keep again
  // (2026-11-11: each page list refresh sealed a table of 100,000 rows anew).
  const keptGraph = useRef<{ pageId: string; graph: ContentGraph; graphRevision: number; access: object } | null>(null);
  // The server's answer the store refused, kept as a change is.
  const [keepAgain, setKeepAgain] = useState(0);
  useEffect(() => {
    if (!pageId) return;
    // A graph held in a store is kept as the store changes: a cell typed
    // changes its rows' values alone, which `graph` doesn't show.
    let stop: (() => void) | undefined;
    const keep = () => {
      stop?.();
      stop = keepShown(pageId, graphStore?.get() ?? graph);
    };
    keep();
    const stopHearing = graphStore?.subscribe(keep);
    return () => {
      stopHearing?.();
      stop?.();
    };
    // A read is shown keeping what it holds unchanged (lib/keptGraph.ts), the same read still.
    function keepShown(pageId: string, shown: ContentGraph) {
      if (shownPageId.current !== pageId) return undefined;
      const others = () => otherWritesShown.current.pageId === pageId ? otherWritesShown.current.ids : [];
      holdShownGraph(principalId, pageId, { bootstrapRevision, graphRevision: graphRevision.current, graph: shown, ...shownAccess.current,
        ...(others().length ? { shown: [...others()] } : {}) });
      if (readGraph.current && shown === graphAsShown(readGraph.current)) return undefined;
      const kept = keptGraph.current;
      if (kept?.pageId === pageId && kept.graph === shown && kept.graphRevision === graphRevision.current
        && kept.access === shownAccess.current) return undefined;
      // Until the copy is kept, a reload reads the server first (cacheStaleness).
      const changed = Date.now();
      markCacheBehind(principalId, pageGraphCache(pageId), changed);
      // Kept once the page rests a moment; a copy the store refuses is kept
      // again until it takes it, and the mark stays until then.
      let stopKeeping: (() => void) | undefined;
      const timer = window.setTimeout(() => {
        if (shownPageId.current !== pageId) return;
        const keeping = { pageId, graph: shown, graphRevision: graphRevision.current, access: shownAccess.current };
        // The copy holds the writes on screen — this tab's made by now (a
        // later change starts the wait again) and those it showed from its
        // copy — and the outbox's changes show on top of it without them:
        // made again, a move goes by where the blocks stand after it
        // (2026-11-14: offline, two lines moved down twice each showed in
        // another order once the page was opened again).
        const writes = [...new Set([...others(), ...writesShown.current?.(pageId) ?? []])];
        stopKeeping = lifecycle.keepUntilKept({ key: pageId, principalId, serverKey: serverIdentity() }, {
          bootstrapRevision,
          graphRevision: keeping.graphRevision,
          graph: shown,
          ...shownAccess.current,
          ...(writes.length ? { shown: [...writes] } : {}),
        }, () => {
          keptGraph.current = keeping;
          clearCacheBehind(principalId, pageGraphCache(pageId), changed);
        });
      }, 1_500);
      return () => {
        window.clearTimeout(timer);
        stopKeeping?.();
      };
    }
  }, [bootstrapRevision, graph, graphRevision, graphStore, keepAgain, lifecycle, pageId, principalId]);

  useEffect(() => {
    const retry = () => { void offlinePagePinService.retry(input.session.user.id); };
    window.addEventListener("online", retry);
    return () => window.removeEventListener("online", retry);
  }, [input.session.user.id]);

  // Pages made available offline take what changed elsewhere while connected.
  useEffect(() => followPinnedPages(input.session), [input.session]);
}
