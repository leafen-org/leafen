import type { ProductStatusSink } from "@/lib/productStatus";
import { withPagesShownAhead } from "@/lib/pagesShownAhead";
import { recheckPageOutsideList, withPagesOutsideList } from "@/lib/pagesOutsideList";
import { onWorkspaceChangeCheck } from "@/lib/workspaceChangeCheck";
import { createListFollowReads, withKeptPageChanges } from "./listFollowReads";
import { onUncertainWritesSettled } from "@/lib/uncertainWrites";
import { i18next } from "@/i18n";
import { type SetStateAction, useCallback, useEffect, useRef, useState } from "react";
import { api, throwInvalidBootstrapContinuation } from "@/api";
import { collectBootstrapPages } from "@/lib/bootstrapCollection";
import { sharingShownParts } from "@/lib/bootstrapSameParts";
import { createReadGenerationOwner } from "@/lib/readGeneration";
import { requestedPageIdFromLocation, screenDrawsPage } from "@/lib/authenticatedProductRoute";
import { pushRoute, replaceRoute, routeInfoFromPath, useSearchParams } from "@/lib/router";
import { addressWithoutWorkspace, workspaceAskedBy } from "@/lib/workspaceAddress";
import { noteFullPageDatabases } from "@/lib/inlineDatabaseAddress";
import { onWorkspaceOpenRequest } from "@/lib/workspaceOpenEvents";
import type { LocalFirstSnapshotLoadResult } from "@/lib/localFirstSnapshotLifecycle";
import type { Bootstrap, Page, Session } from "@/types";
import type { NativeMetadataMutation } from "@/lib/nativePageMutationOutbox";
import { notePageTitlesFromServer } from "@/lib/pageTitleFollow";
import { peekWorkspaceOpenRequest, takeWorkspaceOpenRequest } from "@/lib/profileRegistry";
import { serverIdentity } from "@/lib/serverOrigin";
import { BOOTSTRAP_CACHE, cacheBehind, clearCacheBehind, markCacheBehind } from "@/lib/cacheStaleness";
import { observeServerDataEpoch } from "@/lib/dataEpoch";
import { resumeWorkspaceDeletions } from "@/components/settings/resumeWorkspaceDeletions";
import { personValue, storePersonValue } from "@/lib/browserStorage";
import { markSwitcherEntry } from "@/components/sidebar/switcherEntry";
import { leavingPageHeld, onLeavingPageGone } from "@/lib/pageLeftAfterPaint";
import { afterNextPaint } from "@/lib/afterPress";
import { withDatabaseNamesKept } from "@/lib/databaseNamesKept";

/** The workspace a person last used on this browser. */
const LAST_WORKSPACE_KEY = "last-workspace";

// A list read a later one retired.
const retiredRead = Symbol("retired bootstrap read");

const emptyBootstrap: Bootstrap = {
  snapshotRevision: 0,
  workspaces: [],
  pages: [],
  pageFavorites: [],
  workspaceRefreshTokens: [],
  cursor: 0,
  nextCursor: null,
  hasMore: false,
};

export interface AuthenticatedBootstrapLifecycle {
  load(input: {
    cacheBehind?: () => boolean;
    copyUnneeded?: boolean;
    isCurrent(): boolean;
    key: string;
    onDefinitiveDenial(): void;
    onNetworkKept?(kept: boolean): void;
    onSnapshot(value: Bootstrap, source: "cache" | "network"): void;
    principalId: string;
    readNetwork(): Promise<Bootstrap>;
    serverKey: string;
  }): Promise<LocalFirstSnapshotLoadResult<Bootstrap>>;
  /** Keeps the list as the cached copy until the store takes it; stops trying when called. */
  keepUntilKept(identity: { key: string; principalId: string; serverKey: string }, value: Bootstrap, onKept: () => void): () => void;
}

export function useAuthenticatedBootstrap({
  keptNewPages,
  keptPageChanges,
  lifecycle,
  onStatus,
  pathname,
  prepareCachedSnapshot,
  session,
}: {
  /** The pages made on this device the server doesn't have yet, shown in a list it reads. */
  keptNewPages?: () => Promise<Page[]>;
  /** The page settings changes this device keeps for the server, shown over a list it reads. */
  keptPageChanges?: () => Promise<Map<string, NativeMetadataMutation[]>>;
  lifecycle: AuthenticatedBootstrapLifecycle;
  onStatus: ProductStatusSink;
  pathname: string;
  prepareCachedSnapshot?: (
    value: Bootstrap,
    isCurrent: () => boolean,
  ) => Promise<boolean>;
  session: Session;
}) {
  const [list, setList] = useState<Bootstrap>(emptyBootstrap);
  // Each change to the list is worked out once, as it is asked for, from the
  // list as last changed, and the list takes the result: a change asked for
  // in a transition (a page menu's switch, PageMenu.tsx) otherwise had each
  // change after it worked out again from the list before it at every draw,
  // the list new each time, and the app drew itself over and over until
  // React stopped it ("문제가 발생했습니다", 2026-11-14).
  const listNow = useRef(list);
  const [setData] = useState(() => (change: SetStateAction<Bootstrap>) => {
    listNow.current = typeof change === "function" ? change(listNow.current) : change;
    setList(listNow.current);
  });
  // What is shown: the list. Its pages' edit times known after it are kept
  // apart (lib/pageEditedHere.ts), so a body edited here makes no new list.
  const data = list;
  // The pages shown now, of which those open though the list lacks them stay (pagesOutsideList.ts).
  const shownPages = useRef(data.pages);
  shownPages.current = data.pages;
  const [selectedPageId, setSelectedPageId] = useState<string | null>(null);
  // A screen that draws no page — home, the library, my tasks, apps, people,
  // the developer tools — holds none open: the page left is let go as the
  // address moves there, in the same drawing, not in one more drawing of the
  // whole product after it (the effect below did, 2026-11-14). Kept, its
  // graph and room stayed open drawn nowhere, and ⌘⌥L copied it (2026-11-18).
  // A page a press left stands till its new screen is painted, still the page
  // open, and is let go then with its place (lib/pageLeftAfterPaint.ts).
  if (!screenDrawsPage(pathname) && selectedPageId !== null && !leavingPageHeld()) setSelectedPageId(null);
  // The page let go meanwhile — home opened, a workspace left — goes with it too.
  const letGoAfterPaint = useRef(false);
  const selectPage = useCallback((next: SetStateAction<string | null>) => {
    letGoAfterPaint.current = next === null && leavingPageHeld();
    if (!letGoAfterPaint.current) setSelectedPageId(next);
  }, []);
  useEffect(() => onLeavingPageGone(() => {
    if (!screenDrawsPage() || letGoAfterPaint.current) setSelectedPageId(null);
    letGoAfterPaint.current = false;
  }), []);
  // A profile switch may ask for one of the profile's workspaces; the ask
  // stays until a list that has the workspace arrives.
  const [selectedWorkspaceId, setSelectedWorkspaceId] = useState<string | null>(() => peekWorkspaceOpenRequest() || null);
  // The address's page in the list is picked as the address moves, in the
  // same drawing, as home lets the page go above (the effect below picks it
  // too). Picked only once drawn, a wiki's page opened whole from its peek
  // was drawn first as a row alone and then as the page, its row page and
  // body made twice in the press's frame (2026-11-17, a NAS-class server).
  const routePicked = useRef<{ pathname: string; pages: readonly Page[] } | null>(null);
  if (!routePicked.current || routePicked.current.pathname !== pathname || routePicked.current.pages !== list.pages) {
    routePicked.current = { pathname, pages: list.pages };
    const route = routeInfoFromPath(pathname);
    const page = route.kind === "page" ? list.pages.find((item) => item.id === route.pageId) : undefined;
    if (page && selectedWorkspaceId !== page.workspaceId) setSelectedWorkspaceId(page.workspaceId);
    if (page && selectedPageId !== page.id) setSelectedPageId(page.id);
  }
  const [readOwner] = useState(createReadGenerationOwner);
  // Whose read is shown: what the product shows then is at least as new as
  // the cached copy, so later reads keep it until the server answers.
  const shownPrincipalId = useRef<string | null>(null);
  // Whether the page list shown is the server's answer, or the cached copy
  // standing in while the server can't be reached. A page the cached copy
  // lacks may have been made since it was kept, so an address naming it
  // waits for the server rather than being not found.
  const [listSettled, setListSettled] = useState(false);
  // Whether any list was drawn yet, cached or the server's, or the first read
  // ended without one: until then the product isn't drawn, so starting shows
  // the person's workspace and not an empty one first (2026-10-16).
  const [listShown, setListShown] = useState(false);
  const readContext = useRef({
    principalId: session.user.id,
    route: pathname,
    sessionToken: session.token,
  });
  readContext.current = {
    principalId: session.user.id,
    route: pathname,
    sessionToken: session.token,
  };

  // The list as read or as cached; of it the whole list read or cached, which
  // needs no keeping (a list the change check brought, listFollowReads.ts,
  // does: skipped, it left the copy and its mark behind, cacheStaleness); and
  // a count that keeps what shows again when the store refused the server's answer.
  const readSnapshot = useRef<Bootstrap>(emptyBootstrap);
  const wholeList = useRef<Bootstrap>(emptyBootstrap);
  const [keepAgain, setKeepAgain] = useState(0);
  const applySnapshot = (next: Bootstrap) => {
    const normalized = Array.isArray(next.pageFavorites)
      ? next
      : { ...next, pageFavorites: [] };
    readSnapshot.current = wholeList.current = normalized;
    setData(normalized);
    setListShown(true);
    const requestedPageId = requestedPageIdFromLocation();
    const requestedPage = requestedPageId
      ? normalized.pages.find((page) => page.id === requestedPageId)
      : undefined;
    const asked = peekWorkspaceOpenRequest();
    const askedWorkspace = asked && normalized.workspaces.some(({ id }) => id === asked) ? asked : "";
    if (askedWorkspace) {
      takeWorkspaceOpenRequest();
      markSwitcherEntry(askedWorkspace);
    }
    // Opened again, the app goes back to the workspace last used here, as
    // Notion does (2026-10-16: it always opened the first one).
    const remembered = personValue(LAST_WORKSPACE_KEY, { userId: session.user.id });
    const lastUsed = remembered && normalized.workspaces.some(({ id }) => id === remembered) ? remembered : null;
    // A link naming its workspace opens there (lib/workspaceAddress.ts).
    const addressed = workspaceAskedBy(window.location.search, normalized.workspaces);
    if (addressed) replaceRoute(addressWithoutWorkspace(window.location));
    setSelectedWorkspaceId((current) => (
      askedWorkspace || addressed || (current && normalized.workspaces.some(({ id }) => id === current)
        ? current
        : requestedPage?.workspaceId ?? lastUsed ?? normalized.workspaces[0]?.id ?? null)
    ));
    // An address naming a page the list lacks opens no other page in its
    // place, and a screen that draws none opens none (above).
    setSelectedPageId(requestedPage ? requestedPage.id : null);
  };

  // A list read gives way to one begun after it. Whoever waited on it gets
  // that newer read's list, the one shown, rather than nothing; one the
  // address retired with no read after it reads again (2026-10-16: a copy
  // was reported as not made when its workspace's change began a read while
  // the copy's own was reading).
  const newestRefresh = useRef<{ run: Promise<Bootstrap | null>; token: string } | null>(null);
  const mounted = useRef(true);
  function refresh(activeSession = session, signal?: AbortSignal, again = 0): Promise<Bootstrap | null> {
    const run: Promise<Bootstrap | null> = readBootstrap(activeSession, signal).then((value) => {
      if (value !== retiredRead) return value;
      const newer = newestRefresh.current;
      if (newer && newer.run !== run && newer.token === activeSession.token) return newer.run;
      // Read again a few times at most: a read retired at once, again and
      // again, gives up rather than loop (2026-10-16: a read built from an
      // older address was retired as it began, and reading again forever
      // froze the page).
      const context = readContext.current;
      if (again < 3 && mounted.current && context.principalId === activeSession.user.id && context.sessionToken === activeSession.token) {
        return refresh(activeSession, signal, again + 1);
      }
      // Given up, the first screen isn't left covered: what there is shows.
      if (mounted.current) setListShown(true);
      return null;
    });
    newestRefresh.current = { run, token: activeSession.token };
    return run;
  }

  async function readBootstrap(activeSession: Session, signal?: AbortSignal): Promise<Bootstrap | null | typeof retiredRead> {
    // The address as it is now: a refresh kept from an earlier render holds
    // an older one, which the read owner would retire as it began.
    const context = {
      principalId: activeSession.user.id,
      route: readContext.current.route,
      sessionToken: activeSession.token,
    };
    const read = readOwner.begin(context, () => readContext.current);
    const isCurrent = () => read.isCurrent() && !signal?.aborted;
    const readSignal = signal
      ? AbortSignal.any([read.signal, signal])
      : read.signal;
    const showing = shownPrincipalId.current === activeSession.user.id;
    let denied = false;
    let result: LocalFirstSnapshotLoadResult<Bootstrap>;
    const loadStarted = Date.now();
    try {
      result = await lifecycle.load({
        cacheBehind: () => cacheBehind(activeSession.user.id, BOOTSTRAP_CACHE),
        // A list shown is the server's or newer than the copy (onSnapshot
        // below leaves the copy unshown then), and stays shown without the
        // server: the copy isn't read.
        copyUnneeded: showing,
        isCurrent,
        key: "default",
        // The server's answer is the cached copy now, once the store took
        // it; one it refused is kept as a change made here is.
        onNetworkKept(kept) {
          if (kept) clearCacheBehind(activeSession.user.id, BOOTSTRAP_CACHE, loadStarted);
          else if (isCurrent()) {
            readSnapshot.current = wholeList.current = emptyBootstrap;
            setKeepAgain((current) => current + 1);
          }
        },
        onDefinitiveDenial() {
          denied = true;
          shownPrincipalId.current = null;
          setData(emptyBootstrap);
          setListSettled(false);
          setSelectedPageId(null);
          setSelectedWorkspaceId(null);
        },
        async onSnapshot(next, source) {
          if (source === "cache" && showing) return false;
          // Its full-page databases' addresses open their pages, the cached
          // route's first (inlineDatabaseAddress.ts) — a page shown ahead as
          // one counting as one: read between its steps, the server still
          // listed it as a page, which took its address away, and the
          // database open there read as missing until its last step
          // (2026-11-14, NAS condition: "페이지 찾지 못함" for about 1.3 s
          // after "/데이터베이스 - 전체 페이지").
          noteFullPageDatabases(withPagesShownAhead(next).pages);
          if (
            source === "cache"
            && prepareCachedSnapshot
            && !(await prepareCachedSnapshot(next, isCurrent))
          ) {
            return false;
          }
          // A title or icon this device keeps for the server shows over the
          // server's older one until it goes (2026-11-07: a new page's title,
          // kept while the server took nothing, read back as "새 페이지"), and
          // a page made here stays listed until the server has it.
          const kept = keptPageChanges ? await keptPageChanges().catch(() => null) : null;
          const made = keptNewPages ? await keptNewPages().catch(() => null) : null;
          if (!isCurrent()) return false;
          // An open title field takes the server's titles as read (pageTitleFollow.ts).
          if (source === "network") notePageTitlesFromServer(next.pages);
          // Pages read ahead of the list after its read began, which it lacks, stay.
          const read = source === "network" ? withPagesReadAhead(next) : next;
          // A page shown ahead of a change in steps shows so till its last (pagesShownAhead.ts);
          // one open though the list lacks it stays as shown (pagesOutsideList.ts).
          // What is the same as shown stays the same (lib/bootstrapSameParts.ts).
          const shownNext = await sharingShownParts(withPagesOutsideList(withPagesShownAhead(kept?.size || made?.length
            ? withKeptPageChanges(read, kept, made, activeSession.user.id) : read), shownPages.current), listNow.current);
          if (!isCurrent()) return false;
          // A page read ahead while they were compared stays too, and a database's name typed here (databaseNamesKept.ts).
          applySnapshot(withDatabaseNamesKept(source === "network" ? withPagesReadAhead(shownNext) : shownNext));
          // The open one of those is asked again, as the list was (pagesOutsideList.ts).
          if (source === "network") recheckPageOutsideList(activeSession, requestedPageIdFromLocation(), setData);
          setListSettled(source === "network");
          shownPrincipalId.current = activeSession.user.id;
          onStatus(source === "cache" ? i18next.t("syncStatusBadge:status.openingFromCache") : i18next.t("syncStatusBadge:status.synced"));
          return true;
        },
        principalId: activeSession.user.id,
        async readNetwork() {
          const collected = await collectBootstrapPages({
            onInvalidContinuation: throwInvalidBootstrapContinuation,
            onSnapshot() {},
            // Each answer tells which history the server's data is in.
            readPage: async (...args: Parameters<typeof api.bootstrap>) => {
              const page = await api.bootstrap(...args);
              observeServerDataEpoch(activeSession.user.id, page.dataEpoch);
              if (page.deletingWorkspaces?.length) resumeWorkspaceDeletions(activeSession, page.deletingWorkspaces);
              return page;
            },
            session: activeSession,
            shouldContinue: isCurrent,
            signal: readSignal,
          });
          if (!collected) throw new DOMException("Bootstrap read retired.", "AbortError");
          return collected;
        },
        serverKey: serverIdentity(),
      });
    } catch (error) {
      if (!denied && !signal?.aborted && !read.isCurrent()) return retiredRead;
      if (isCurrent()) setListShown(true);
      // Away from the server, what is shown stays, as a cached copy would.
      if (!showing || denied) throw error;
      if (isCurrent()) {
        setListSettled(true);
        onStatus(i18next.t("syncStatusBadge:status.usingOfflineCache"));
      }
      return null;
    }
    if (result?.source === "cache" && isCurrent()) {
      setListSettled(true);
      onStatus(i18next.t("syncStatusBadge:status.usingOfflineCache"));
    }
    if (!signal?.aborted && !read.isCurrent()) return retiredRead;
    if (isCurrent()) setListShown(true);
    return isCurrent() ? result?.value ?? null : null;
  }

  // Others' page changes read ahead of, or in place of, the whole list (listFollowReads.ts).
  const keptChanges = useRef(keptPageChanges);
  keptChanges.current = keptPageChanges;
  const [follow] = useState(() => createListFollowReads({
    listNow, readSnapshot, shownPrincipalId, shownPages, keptPageChanges: keptChanges, setData,
  }));
  const withPagesReadAhead = follow.withPagesReadAhead;

  // An open page the list doesn't hold is read again as it opens and when its
  // page says it changed, which no list read may follow (pagesOutsideList.ts).
  useEffect(() => {
    const recheck = () => recheckPageOutsideList(session, requestedPageIdFromLocation(), setData);
    recheck();
    return onWorkspaceChangeCheck(recheck);
  }, [pathname, session]);

  useEffect(() => {
    const route = routeInfoFromPath(pathname);
    if (!screenDrawsPage(pathname)) {
      if (!leavingPageHeld()) setSelectedPageId(null);
      return;
    }
    if (route.kind !== "page") return;
    const page = data.pages.find((item) => item.id === route.pageId);
    // An address no listed page has — a database's page opened whole (⤢),
    // drawn at its address alone — lets the page it was opened over go once
    // its screen is painted, as that address opened anew has none: kept, the
    // page's graph and room stayed open, drawn nowhere, and what the product
    // reads of the open page (⌘L's link, the search's open page) was that
    // page's. One the list lost as it moves (lib/pageBecomingRow.ts) is the
    // address's own and stays.
    if (!page) return afterNextPaint(() => {
      if (window.location.pathname === pathname) setSelectedPageId((current) => (current === route.pageId ? current : null));
    });
    setSelectedWorkspaceId(page.workspaceId);
    setSelectedPageId(route.pageId);
  }, [data.pages, pathname]);

  // What changed here since the server answered (a page made, moved or
  // renamed) is kept as the cached copy too, so the app opens without the
  // server as it was left (2026-10-03: a page made online was "not found"
  // offline).
  useEffect(() => {
    if (data === wholeList.current || shownPrincipalId.current !== session.user.id) return;
    const principalId = session.user.id;
    const changed = Date.now();
    markCacheBehind(principalId, BOOTSTRAP_CACHE, changed);
    let stopKeeping: (() => void) | undefined;
    const timer = window.setTimeout(() => {
      if (shownPrincipalId.current !== principalId) return;
      // The list kept is the one still shown now: marks made since it changed are kept with it (cacheStaleness).
      const shownUntil = Date.now();
      stopKeeping = lifecycle.keepUntilKept({ key: "default", principalId, serverKey: serverIdentity() }, data,
        () => clearCacheBehind(principalId, BOOTSTRAP_CACHE, shownUntil));
    }, 500);
    return () => {
      window.clearTimeout(timer);
      stopKeeping?.();
    };
  }, [data, keepAgain, lifecycle, session.user.id]);

  // An app's screen opens the app's workspace, which a new member doesn't
  // list yet.
  useEffect(() => {
    if (selectedWorkspaceId) storePersonValue(LAST_WORKSPACE_KEY, selectedWorkspaceId, { userId: session.user.id });
  }, [selectedWorkspaceId, session.user.id]);

  // A link naming its workspace, gone to while the app is open (an inbox
  // notice, the app's own link), opens there too.
  const search = useSearchParams();
  useEffect(() => {
    const addressed = workspaceAskedBy(`?${search.toString()}`, data.workspaces);
    if (!addressed) return;
    setSelectedWorkspaceId(addressed);
    replaceRoute(addressWithoutWorkspace(window.location));
  }, [data.workspaces, search]);

  useEffect(() => onWorkspaceOpenRequest((workspaceId) => {
    void refresh(session).then(() => {
      setSelectedWorkspaceId(workspaceId);
      setSelectedPageId(null);
      pushRoute("/");
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }), [session]);

  useEffect(() => {
    readOwner.observe(readContext.current);
    return () => readOwner.retire();
  }, [pathname, readOwner, session.token, session.user.id]);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  // A change sent without the server's answer may have been taken after all
  // (a workspace's name saved as the network stopped): once the server
  // answers again, the workspaces and pages are read again, so the sidebar
  // and the settings show what it holds (2026-11-06: the name stayed the old
  // one on both while the server had the new one).
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;
  useEffect(() => onUncertainWritesSettled(() => { void refreshRef.current().catch(() => undefined); }), []);

  const reset = () => {
    shownPrincipalId.current = null;
    setData(emptyBootstrap);
    setListSettled(false);
    setSelectedWorkspaceId(null);
    setSelectedPageId(null);
  };

  // What the change check follows the list by (useWorkspaceChangeFollow.ts).
  const listFollow = {
    pageChangeSeq: () => listNow.current.pageChangeSeq,
    readNewPages: (workspaceId: string) => follow.readNewPages(session, workspaceId),
    takePageChanges: (workspaceId: string, changes: Bootstrap, token: string) => follow.takePageChanges(session, workspaceId, changes, token),
    readWorkspaces: () => follow.readWorkspaces(session),
  };

  return {
    data,
    listFollow,
    listSettled,
    listShown,
    refresh,
    reset,
    selectedPageId,
    selectedWorkspaceId,
    setData,
    setSelectedPageId: selectPage,
    setSelectedWorkspaceId,
  };
}
