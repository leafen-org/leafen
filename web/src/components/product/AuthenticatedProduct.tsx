import { libraryOpeningView } from "@/components/library/libraryViewOrder";
import { canManageNativeWorkspace } from "@/lib/nativeWorkspaceRolePolicy";
import { beginAddingAccount } from "@/lib/serverAccounts";
import { pageBecameRow, useOpenPageThroughMove } from "@/lib/pageBecomingRow";
import { pageBodyOfPageView } from "@/lib/pageDrawnAsRow"; import { useOpenPageBody } from "@/lib/openPageBody";
import { onWorkspaceListChanged } from "@/lib/workspaceListEvents";
import { useNoWorkspaceRetry } from "./useNoWorkspaceRetry";
import { useRootPageCreation } from "./useRootPageCreation";
import { authenticatedProductErrorMessage } from "@/lib/authenticatedProductErrors";
import { PageActionAccessProvider } from "@/lib/pageActionAccessContext";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { useAppDirectoryRevision, useWorkspacePeopleDirectory } from "@/lib/workspacePeopleDirectory";
import { type ReactNode, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import { RowPageReadingScreen as RowPageReading, StandaloneRowPageScreen as StandaloneRowPage } from "../database/standaloneRowPageScreen";
import { openSearch } from "@/lib/searchOpen";
import { closeProductWindows, databasePickerStore, followTeamspaceWindowOpen, followWorkspaceCreateOpen, importWindowStore, setDatabasePickerOpen, shortcutsWindowStore, trashPanelStore } from "@/lib/productWindows";
import { WithProductWindow } from "./WithProductWindow";
import type { ProductStatusSink } from "@/lib/productStatus";
import { useProductToast } from "@/lib/productToast";
import { collaborationStatusMessage } from "@/lib/collaborationStatus";
import { api } from "@/api";
import { upsertBootstrapPage } from "@/lib/bootstrapPageProjection";
import { readPageOutsideList, withOpenedPage } from "@/lib/pagesOutsideList";
import { OPEN_WORKSPACE_EVENT, type OpenWorkspaceRequest } from "@/lib/openWorkspace";
import type { ContentGraph } from "@/contentGraph";
import { PageGraphStoreContext, usePageGraphFrame, usePageGraphStore } from "@/lib/pageGraphStore";
import { WithPageGraph } from "./WithPageGraph";
import { openAddPageWindow } from "@/components/sidebar/addPageWindowStore";
import { useNewShortcutMenu } from "./NewShortcutMenu";
import { createLocalFirstSnapshotLifecycle } from "@/lib/localFirstSnapshotLifecycle";
import { localFirstProductCache as recordCache } from "@/lib/localFirstProductCache";
import { createBrowserLocalLockSettings } from "@/lib/localLockBrowserSettings";
import type { NativePageMutationBrowser } from "@/lib/nativePageMutationBrowser";
import { useDesktopNotices } from "@/lib/desktopNotices";
import { pageHref } from "@/lib/navigation";
import { openPageInNewTab } from "@/lib/databasePageLink";
import { HOME_PATH, libraryHref, pushRoute, replaceRoute, routeInfoFromPath, useInAppLinks, usePathname, useSearchParams } from "@/lib/router";
import { openPressedPastPaint } from "@/lib/openPastPaint";
import { inlineDatabaseHolderPath } from "@/lib/inlineDatabaseAddress";
import { inlineDatabaseFullPage, inlineDatabaseOpenedAlone, inlineDatabasePageView } from "@/lib/inlineDatabaseFullPage";
import { useTrashedDatabasePage } from "@/components/page/TrashedDatabaseBanner"; import { withTrashedDatabase } from "@/lib/trashedDatabaseAlone";
import { useTheme } from "@/lib/theme";
import type { ImportDestination, ImportSource } from "@/components/import/ImportDialog";
import { useNoWorkspaceOffer } from "@/lib/workspaceAllowedEmailDomains";
import { workspaceDialogActions } from "./workspaceDialogActions";
import { AuthenticatedPagePeek } from "./AuthenticatedPagePeek";
import { ProductPageCards } from "./ProductPageCards";
import { PersonHoverCard } from "@/components/people/PersonHoverCard";
import { ProductDialogHosts } from "./ProductDialogHosts";
import { PagePresentationHost } from "@/components/page/PageSlideshow";
import { HomeView } from "@/components/home/HomeView";
import { databasePageSource, wikiPageSource } from "@/components/page/DatabaseDescription";
import { readDatabaseViewChoice, useDatabaseViewChoices } from "@/lib/databaseViewChoice";
import { PageView } from "@/components/page/PageView";
import { CommentMentionContext } from "@/components/comments/commentMentionContext";
import { WikiPageView } from "./WikiPageView";
import { Sidebar } from "@/components/sidebar/Sidebar";
import { AppShell } from "@/components/shell/AppShell";
import { DeveloperBarWhenOn } from "@/components/shell/DeveloperBarWhenOn";
import { useSettingsWindow } from "@/components/settings/useSettingsWindow";
import SyncStatusBadge from "@/components/SyncStatusBadge";
import { FirstScreenCover } from "./FirstScreenCover";
import type { Bootstrap, Page, Session, Workspace } from "@/types";
import { authenticatedPageChanges } from "./authenticatedPageChanges";
import { PageContentField } from "./PageContentField";
import { useAuthenticatedBootstrap } from "./useAuthenticatedBootstrap";
import { prepareCachedProductRoute } from "./useLocalFirstContentGraph";
import { useAuthenticatedProductShortcuts } from "./useAuthenticatedProductShortcuts";
import { GuestMFARequired } from "./GuestMFARequired";
import { AuthenticatedProductRoute } from "./AuthenticatedProductRoute";
import { AuthenticatedSettingsWindow } from "./AuthenticatedSettingsWindow";
import { AuthenticatedSearch } from "./AuthenticatedSearch";
import { useDeletedWorkspaces } from "./useDeletedWorkspaces";
import { productRouteLabel } from "./productRouteLabel";
import { useNativeSyncStatus } from "./useNativeSyncStatus";
import { useDatabasePageSignpost } from "./useDatabasePageSignpost";
import { usePageFavorites } from "./usePageFavorites";
import { useDatabaseFavorites } from "./useDatabaseFavorites";
import { usePageLinkRoute } from "./usePageLinkRoute";
import { usePageMenuActions } from "./usePageMenuActions";
import { useSidebarPageTreeActions } from "./useSidebarPageTreeActions";
import { wikiDatabaseLayout, wikiDatabaseViews } from "@/lib/wikiDatabaseLayout";
import { usePageBodyEditing } from "./usePageBodyEditing";
import { useWikiChangeRefresh } from "./useWikiChangeRefresh";
import { useWorkspaceChangeFollow } from "./useWorkspaceChangeFollow";
import { ProductPagesProvider } from "./productPagesStore";
import { loadWorkspacePeople } from "@/lib/workspacePeopleChoices";
import { useAdministratorAccess } from "./useAdministratorAccess";
import { usePageLockState } from "./usePageLockState";
import { usePageVisitRecorder } from "./usePageVisitRecorder";
import { useStartPage } from "./useStartPage";
import { usePageSuggesting } from "./usePageSuggesting";
import { useProductSignOut } from "./useProductSignOut";
import { useFirstRun } from "./useFirstRun";
import { pageTrashRoot } from "@/lib/pageTrash";
import { pageLookup } from "@/lib/pageArchive";
import { PageMentionIconStyles, useMentionPageTitles } from "@/components/editor/PageMentionIconStyles";
import { serverIdentity } from "@/lib/serverOrigin";
import { isDefinitiveReadDenial } from "@/lib/readDenial";
import { requestPageAccess } from "@/lib/accessRequests";
import { StandaloneConsentPage } from "@/components/mcp/StandaloneConsentPage";
import { standaloneConsent } from "@/lib/standaloneConsent";
import { serverRefusal, useServerMovedNotice, useServerPair } from "@/components/product/ServerMovedNotice";
import { PageCopyFollow, PageCopyNotice } from "@/components/page/PageCopying";
import { useProductPageListFollow } from "./productPageListFollow";
import { useDrawnPage, useDrawnPageSync } from "./useDrawnPage";
import { localBodySearch } from "./localBodySearch";
import { useProductTopNotice } from "./useProductTopNotice";
import { usePendingShareLinkJoin } from "./usePendingShareLinkJoin";
import { useDatabasePageLocks } from "./databasePageLocks"; import { AuthenticatedPageEditor, usePageEditorMoveContext } from "./AuthenticatedPageArea";
import { workspacePageListsOf } from "@/lib/pageIndex";

// What the first screen does not draw — routes other than a page, dialogs,
// the trash and the developer bar — is downloaded after it, or when opened.
const KeyboardShortcutsDialog = lazyScreen(() => import("@/components/shortcuts/KeyboardShortcutsDialog").then((module) => ({ default: module.KeyboardShortcutsDialog })));
const ImportDialog = lazyScreen(() => import("@/components/import/ImportDialog").then((module) => ({ default: module.ImportDialog })));
const AuthenticatedTrashView = lazyScreen(() => import("@/components/product/AuthenticatedTrashView").then((module) => ({ default: module.AuthenticatedTrashView })), { typedOnOpen: true });
const LibraryView = lazyScreen(() => import("@/components/library/LibraryView").then((module) => ({ default: module.LibraryView })));
const PeopleView = lazyScreen(() => import("@/components/people/PeopleView").then((module) => ({ default: module.PeopleView })));
const PageBodyDatabasePicker = lazyScreen(() => import("./PageBodyDatabasePicker").then((module) => ({ default: module.PageBodyDatabasePicker })));

const noSharedPageIds: readonly string[] = [];

export default function AuthenticatedProduct({
  session,
  onSession,
  pageMutationOutbox,
}: {
  session: Session;
  onSession: (session: Session | null) => void;
  pageMutationOutbox: NativePageMutationBrowser<Awaited<ReturnType<typeof api.updatePage>>>;
}) {
  const [creatingPage, setCreatingPage] = useState(false);
  const [pageProblem, setPageProblem] = useState<{ id: string; problem: "no-access" | "missing" } | null>(null);
  const rememberPageProblem = useCallback((id: string, problem: "no-access" | "missing") => setPageProblem({ id, problem }), []);
  // Results and failures surface as toasts; progress and routine saves do
  // not, nor does an always-present status line, as in Notion.
  const { notify } = useProductToast();
  const announce = useCallback<ProductStatusSink>((message, tone) => {
    if (tone && message) notify(message, tone);
  }, [notify]);
  // The open page's graph, which its body reads as it changes; the app reads
  // its frame, not drawn again for a value typed in a table or a line made,
  // and what is pressed beside the page shows on it at once (lib/pageGraphStore.ts).
  const graphStore = usePageGraphStore();
  const setGraph = graphStore.set;
  const graph = usePageGraphFrame(graphStore);
  // The windows over the product open and close in lib/productWindows.ts, which each follows alone.
  useEffect(() => closeProductWindows, []);
  const openSettingsImport = useRef<(source: ImportSource, destination: ImportDestination | null) => void>(() => undefined);
  const openImport = useCallback((source: ImportSource, destination: ImportDestination | null = null) => {
    // Notion's "가져오기" opens its settings' with the page as the place (seen 2026-11-05).
    if (source !== "notion" && !destination?.mergeIntoPage) {
      openSettingsImport.current(source, destination);
      return;
    }
    // A page's own "Import" imports into that page, as Notion's does.
    importWindowStore.set({ source, destination });
  }, []);
  const [recentPageIds, setRecentPageIds] = useState<string[]>([]);
  const [consent] = useState(() => standaloneConsent(window.location.href));
  const administrator = useAdministratorAccess(session);
  useInAppLinks();
  const administratorSettings = administrator.settings;
  const [themePref, setThemePref] = useTheme();
  const pathname = inlineDatabaseHolderPath(usePathname());
  // Drawn again as the address's query changes (a peek opened or closed).
  useSearchParams();
  const bootstrapLifecycle = useMemo(() => createLocalFirstSnapshotLifecycle<Bootstrap>({
    ...recordCache.bootstrap,
    isDefinitiveDenial: isDefinitiveReadDenial,
  }), []);
  const prepareCachedBootstrap = useCallback(async (
    _next: Bootstrap,
    isCurrent: () => boolean,
  ) => prepareCachedProductRoute({
    isCurrent,
    onGraph: setGraph,
    pathname,
    principalId: session.user.id,
    serverKey: serverIdentity(),
  }), [pathname, session.user.id]);
  const {
    data,
    listSettled, listShown,
    listFollow, refresh,
    reset: resetBootstrap,
    selectedPageId,
    selectedWorkspaceId,
    setData,
    setSelectedPageId,
    setSelectedWorkspaceId,
  } = useAuthenticatedBootstrap({
    keptPageChanges: () => pageMutationOutbox.pendingPageMetadata(), keptNewPages: () => pageMutationOutbox.pendingNewPages(),
    lifecycle: bootstrapLifecycle,
    onStatus: announce,
    pathname,
    prepareCachedSnapshot: prepareCachedBootstrap,
    session,
  });
  const noWorkspaceOffer = useNoWorkspaceOffer(session, data.snapshotRevision > 0 && data.workspaces.length === 0);
  const linkAsked = usePageLinkRoute(session, pathname, data.pages, listSettled);
  const { databaseFavorites, setDatabaseFavorite } = useDatabaseFavorites({ data, refresh, session, setData });
  const { favoritePageIds, pageFavoriteService } = usePageFavorites({
    data,
    refresh,
    session,
    setData,
  });
  const route = routeInfoFromPath(pathname);
  const settingsWindow = useSettingsWindow(pathname, data.snapshotRevision > 0);
  openSettingsImport.current = (source, destination) => settingsWindow.open("import", { importSource: source, importPlace: destination ?? undefined });
  const deletedWorkspaces = useDeletedWorkspaces({ pages: data.pages, workspaces: data.workspaces, leave: async () => {
    await refresh(session); settingsWindow.close(); setSelectedPageId(null);
  } });
  // Settings open a workspace they made or left (lib/openWorkspace.ts): a
  // test organization's, or the organization it was tried for.
  const openRequested = useRef<(request: OpenWorkspaceRequest) => void>(() => undefined);
  openRequested.current = (request) => {
    void refresh(session).then((next) => {
      const workspaces = (next?.workspaces ?? []).filter((workspace) => !workspace.appInstallId);
      const target = workspaces.find((workspace) => workspace.id === request.workspaceId)
        ?? workspaces.find((workspace) => request.organizationId && workspace.organizationId === request.organizationId);
      if (!target) return;
      settingsWindow.close();
      selectWorkspace(target);
    });
  };
  useEffect(() => {
    const open = (event: Event) => openRequested.current((event as CustomEvent<OpenWorkspaceRequest>).detail ?? {});
    window.addEventListener(OPEN_WORKSPACE_EVENT, open);
    return () => window.removeEventListener(OPEN_WORKSPACE_EVENT, open);
  }, []);
  useNoWorkspaceRetry(listShown && data.workspaces.length === 0, () => void refresh(session));
  // The trash is a window over the page, as Notion's (trashPanelStore); its address opens it over home.
  const trashAddressed = route.kind === "trash";
  // A visit is kept for the page on screen, which the address names.
  const routePageId = route.kind === "page" ? route.pageId : route.kind === "database" ? route.databaseId : null;
  const listedPage = useMemo(() => data.pages.find((page) => page.id === selectedPageId) ?? null, [data.pages, selectedPageId]);
  const selectedPage = useOpenPageThroughMove(listedPage, routePageId);
  const pageLock = usePageLockState(session, selectedPage);
  const { visitedPage, drawnPage, showRow, visitedPageCovered } = useDrawnPage(selectedPage, routePageId);
  // A page open at this address that a fresh list no longer holds was taken away, not missed by an old list.
  const openedHere = useRef<{ pageId: string | null; open: boolean }>({ pageId: null, open: false });
  if (openedHere.current.pageId !== routePageId) openedHere.current = { pageId: routePageId, open: false };
  if (visitedPage) openedHere.current.open = true;
  usePageVisitRecorder(session, visitedPageCovered ? null : visitedPage?.id ?? null, visitedPage?.workspaceId);
  const leadingToDatabase = useDatabasePageSignpost(session, selectedPage);
  const selectedWorkspace = useMemo(
    () => data.workspaces.find((workspace) => workspace.id === selectedWorkspaceId)
      ?? data.workspaces.find((workspace) => workspace.id === selectedPage?.workspaceId)
      ?? data.workspaces[0]
      ?? null,
    [data.workspaces, selectedPage?.workspaceId, selectedWorkspaceId],
  );
  const serverPair = useServerPair();
  const serverMovedNotice = useServerMovedNotice(session, selectedWorkspace?.id, serverPair);
  const topNotice = useProductTopNotice({
    session, administratorSettings, administratorKnown: administrator.known, pathname, serverMovedNotice, openServerData: (section = "data") => section === "standby" ? settingsWindow.open("server-standby")
      : section === "updates" ? settingsWindow.open("server-updates") : settingsWindow.open("server-data", { part: "scheduled-backup" }),
  });
  // The workspace's people name its person values, mentions and bylines.
  useWorkspacePeopleDirectory(session, selectedWorkspace?.id, Boolean(selectedWorkspace?.guest));
  useAppDirectoryRevision();
  // A page under a page in the trash is in the trash too (its own mark aside).
  const selectedWorkspaceTreePages = useMemo(() => (
    selectedWorkspace ? workspacePageListsOf(data.pages, selectedWorkspace.id).live : []
  ), [data.pages, selectedWorkspace]);
  useMentionPageTitles(selectedWorkspaceTreePages);
  const selectedWorkspaceTrashedPages = useMemo(() => (
    selectedWorkspace ? workspacePageListsOf(data.pages, selectedWorkspace.id).trashed : []
  ), [data.pages, selectedWorkspace]);
  const openStartPage = useStartPage({
    session, pathname, loaded: data.snapshotRevision > 0 && !consent, pages: data.pages, workspace: selectedWorkspace,
    openPage: (page) => openPage(page, false), openLibrary: (workspace) => pushRoute(libraryHref(libraryOpeningView(session.user.id, workspace))),
  });
  // A body edited away from the page view reads the page list again through
  // the latest bootstrap read.
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;
  const refreshPages = useCallback(() => refreshRef.current(session), [session]);
  useEffect(() => onWorkspaceListChanged(() => void refreshPages()), [refreshPages]);
  const peopleWorkspaceId = selectedWorkspace?.id;
  // The page view's page alone: a wiki's page draws its body, graph and room as its database's page (lib/pageDrawnAsRow.ts).
  const bodyPage = pageBodyOfPageView(selectedPage);
  const pageBody = usePageBodyEditing({
    bootstrapRevision: data.snapshotRevision, graph, graphStore, setGraph, onStatus: announce, page: bodyPage,
    pages: data.pages, mentionPages: selectedWorkspaceTreePages, organizationId: selectedWorkspace?.organizationId,
    refreshPages, session, setData, setDatabasePickerOpen, workspaceId: selectedWorkspace?.id,
    mutationOutbox: pageMutationOutbox, inlineDatabases: data.inlineDatabases,
  });
  const { collaboration, databaseViewAdapter, editorActions, editorBridge, graphAccessPending, graphCanEdit, graphCanSuggest, graphLoading, graphWrites, guestMFARequired, mentionSnapshot, recordDateCommand } = pageBody;
  // The open page's body as the product reads it, whichever screen draws it (lib/openPageBody.ts).
  const openBody = useOpenPageBody(bodyPage && { state: collaboration, canEdit: graphCanEdit, canSuggest: graphCanSuggest, pending: graphLoading || collaboration.status === "loading",
    hasContent: graphLoading ? undefined : graph.blocks.length > 0, read: () => ({ graph: graphStore.get(), document: collaboration.document }) }, bodyPage ? null : routePageId);
  const drawnPageSync = useDrawnPageSync(drawnPage, openBody, bodyPage ? bodyPage.id : routePageId);
  const productPages = useMemo(() => ({
    pages: data.pages, mentionPages: selectedWorkspaceTreePages, workspaces: data.workspaces, sharedPageIds: data.sharedPageIds,
    workspaceWidePageIds: data.workspaceWidePageIds, organizationId: selectedWorkspace?.organizationId, workspaceId: selectedWorkspace?.id, session, setData, onStatus: announce, refreshPages, recordDateCommand,
    openImport: (destination: ImportDestination) => openImport("file", destination), mutationOutbox: pageMutationOutbox,
    workspacePeople: peopleWorkspaceId ? (signal: AbortSignal) => loadWorkspacePeople(session, peopleWorkspaceId, signal) : undefined,
    databaseFavorites, setDatabaseFavorite, inlineDatabases: data.inlineDatabases,
  }), [
    databaseFavorites, setDatabaseFavorite, data.inlineDatabases,
    data.pages, data.sharedPageIds, data.workspaceWidePageIds, data.workspaces, openImport, pageMutationOutbox, peopleWorkspaceId, refreshPages, session,
    selectedWorkspace?.id, selectedWorkspace?.organizationId, selectedWorkspaceTreePages, setData, announce, recordDateCommand,
  ]);
  const suggesting = usePageSuggesting({
    canSuggest: graphCanSuggest, lockedForMe: pageLock.lockedForMe, onStatus: announce, page: selectedPage, session,
    inTrash: Boolean(selectedPage && pageTrashRoot(selectedPage, pageLookup(data.pages))),
  });
  useWikiChangeRefresh(() => refresh(session), graphWrites.reread, bodyPage?.id ?? null);
  useWorkspaceChangeFollow(session, selectedWorkspace?.id ?? null, data.workspaceRefreshTokens
    .find(({ workspaceId }) => workspaceId === selectedWorkspace?.id)?.refreshToken, () => refresh(session),
  listFollow);
  // A wiki whose pages are its database's pages shows that database in its
  // views; its Home shows the page without it.
  const wikiSourceId = selectedPage?.isWiki ? selectedPage.wikiDataSourceId : undefined;
  const viewChoices = useDatabaseViewChoices(Boolean(wikiSourceId));
  const wikiLayoutOf = useCallback((of: ContentGraph) => wikiDatabaseLayout(of, wikiSourceId,
    (blockId) => readDatabaseViewChoice(session.user.id, blockId)), [session.user.id, viewChoices, wikiSourceId]);
  const wikiLayout = useMemo(() => wikiLayoutOf(graph), [graph, wikiLayoutOf]);
  // An inline database alone in the trash, opened alone, drawn beside the page (TrashedDatabaseBanner.tsx).
  const trashedPage = useTrashedDatabasePage(session, selectedPage, graph, selectedWorkspace ? canManageNativeWorkspace(selectedWorkspace) : false);
  const pageGraph = withTrashedDatabase(wikiLayout?.homeGraph ?? graph, trashedPage.trashed);
  // The body draws the graph as it is now, its rows' values too.
  const pickPageGraph = useCallback((of: ContentGraph) => withTrashedDatabase(wikiLayoutOf(of)?.homeGraph ?? of, trashedPage.trashed), [trashedPage.trashed, wikiLayoutOf]);
  const pickWikiDatabase = useCallback((of: ContentGraph) => wikiLayoutOf(of)?.databaseGraph ?? of, [wikiLayoutOf]);
  useProductPageListFollow({ refreshPages, selectedPage, setData,
    openPageHasContent: selectedPage && selectedPage.kind !== "database" ? openBody.hasContent : undefined });
  const pageMenuActions = usePageMenuActions({
    document: collaboration.document, currentPageId: selectedPageId,
    readGraph: graphStore.get,
    // A page whose change waits for the server shows and is kept as changed.
    keepPage: (page) => void applyPageProjection(page),
    onStatus: announce,
    pageMutationOutbox,
    pages: data.pages,
    refreshBootstrap: refresh,
    session,
    setData,
  });
  const databasePageLock = useDatabasePageLocks((page, locked) => void pageMenuActions.changePageMetadata(page, { isLocked: locked }));
  const sidebarPageTree = useSidebarPageTreeActions({
    announce, pageMutationOutbox, pages: data.pages, refreshPages, selectedPageId, session, setData, rereadPage: graphWrites.follow,
  });

  const { applyPageProjection, changePage } = authenticatedPageChanges({
    session, setData, pageMutationOutbox, setMessage: announce,
  });
  const localLockSettings = useMemo(() => createBrowserLocalLockSettings({
    mutationOutbox: pageMutationOutbox,
    userId: session.user.id,
  }), [pageMutationOutbox, session.user.id]);
  const syncStatus = useNativeSyncStatus({
    blocked: Boolean(consent),
    mutationOutbox: pageMutationOutbox,
    onStatus: announce,
    refresh,
    session,
  });

  useEffect(() => {
    if (!drawnPage) return;
    // Losing the connection is the sync badge's to say; a local storage or
    // recovery failure is an error to read, and its recovery a notice.
    const { state } = openBody;
    if (state.status === "storage-error" || state.status === "recovery-error") {
      announce(collaborationStatusMessage(state) ?? "", "error");
    } else if (state.status === "storage-recovered") {
      announce(collaborationStatusMessage(state) ?? "", "default");
    }
    // The server is back: what the outbox kept goes out.
    if (state.status === "synced") void syncStatus.service.flushAllPending().catch(() => undefined);
  }, [openBody.state.diagnosticReference, openBody.state.status, drawnPage]);

  useDesktopNotices(session, data.workspaces.map(({ id }) => id));
  usePendingShareLinkJoin(session);
  const firstRun = useFirstRun({
    session, workspaceId: selectedWorkspace?.id ?? null,
    onSession, onCloseSettings: settingsWindow.close,
  });
  const productSignOut = useProductSignOut({
    flushPending: () => syncStatus.service.flushAllPending(),
    mutationOutbox: pageMutationOutbox, onSession, resetBootstrap, session,
  });

  useAuthenticatedProductShortcuts({
    quickFindEnabled: selectedWorkspace !== null,
    drawnPage,
    pages: data.pages,
    themePref,
    setThemePref,
    onOpenSearch: openSearch,
    onOpenKeyboardShortcuts: () => shortcutsWindowStore.set(true),
    onOpenPage: (page) => openPage(page, false),
    onStatus: announce,
  });

  const { createWorkspaceFromDialog, joinWorkspace } = workspaceDialogActions({
    session, refresh, selectWorkspaceId: setSelectedWorkspaceId, leavePage: () => setSelectedPageId(null), openPage: (page) => openPage(page, false),
    openImport, openMove: () => settingsWindow.open("general", { part: "move" }),
  });

  function openPage(page: Page, newTab: boolean, replace = false) {
    if (newTab) return openPageInNewTab(page, session.user.id); // its own address, nothing of the one open now
    openPressedPastPaint(() => {
      setData((current) => withOpenedPage(current, page));
      setSelectedWorkspaceId(page.workspaceId);
      setSelectedPageId(page.id);
      setRecentPageIds((current) => [page.id, ...current.filter((id) => id !== page.id)].slice(0, 24));
      (replace ? replaceRoute : pushRoute)(pageHref(page.id));
    }, page.id);
  }

  // A page at the top of the private pages or a teamspace's, opened unless a
  // shortcut opens it its own way; a "새 데이터베이스" shortcut's starts as a
  // full-page table database.
  const { createRootPage, loadAddDatabaseSources } = useRootPageCreation({
    session, selectedWorkspace, setCreatingPage, setData, refreshPages, announce, openPage, pageMutationOutbox, pages: data.pages,
  });

  // ⌘N's menu: a new page (in a new tab in the Mac and Windows apps) or a
  // database, as "새로" offers them; with no workspace yet it asks for one.
  const newShortcutMenu = useNewShortcutMenu({
    blocked: creatingPage || Boolean(consent),
    canChoose: Boolean(selectedWorkspace && !selectedWorkspace.guest), unable: () => void createRootPage(),
    onPage: () => void createRootPage(undefined, { inNewTab: true }),
    onDatabase: () => openAddPageWindow({ place: { kind: "private" }, mode: "database" }),
  });

  function openWorkspaceHome() {
    // Home shows no page: the product lets the page go as home opens, the page
    // a press left once home is painted (useAuthenticatedBootstrap, lib/pageLeftAfterPaint.ts).
    pushRoute(HOME_PATH);
  }

  function openTrash() {
    trashPanelStore.set({ query: "" });
  }

  // A workspace without a start page to open — one just joined — opens on
  // its Home, as at the root (useStartPage).
  function selectWorkspace(workspace: Workspace) {
    setSelectedWorkspaceId(workspace.id);
    void openStartPage(workspace, openWorkspaceHome);
  }

  const moveContext = usePageEditorMoveContext(editorBridge, favoritePageIds, pageMenuActions, pageFavoriteService);
  const renderPageEditor = (page: Page, pickGraph: (graph: ContentGraph) => ContentGraph, onlyBlockId?: string) => (
    <AuthenticatedPageEditor page={page} pickGraph={pickGraph} onlyBlockId={onlyBlockId} body={pageBody} pages={data.pages} session={session} graphStore={graphStore}
      lockedForMe={pageLock.lockedForMe} suggesting={suggesting} databasePageLock={databasePageLock} inlineDatabaseBlockId={inlineDatabase?.block.id} moveContext={moveContext} />);

  if (consent) {
    return <StandaloneConsentPage consent={consent} session={session} />;
  }

  // One of the page's inline databases opened alone, as its own page.
  const inlineDatabase = selectedPage && selectedPage.kind !== "database"
    ? inlineDatabaseFullPage(pageGraph, inlineDatabaseOpenedAlone())
    : null;

  const mergeWorkspace = (workspace: Workspace) => setData((current) => ({
    ...current, workspaces: current.workspaces.map((item) => item.id === workspace.id ? { ...item, ...workspace } : item),
  }));
  const homeView = (
    <HomeView
      workspaceName={selectedWorkspace?.name}
      workspace={selectedWorkspace ?? undefined}
      canCreatePage={!selectedWorkspace?.guest && !creatingPage}
      noWorkspace={selectedWorkspace || !listShown ? undefined : listSettled ? "none" : "unread"}
      guest={selectedWorkspace?.guest === true}
      onCreatePage={() => void createRootPage()}
      session={session}
      workspaceId={selectedWorkspace?.id}
      creationRestricted={!selectedWorkspace && noWorkspaceOffer?.creationRestricted === true}
      joinable={!selectedWorkspace && noWorkspaceOffer?.joinable === true}
    />
  );
  return (
    <PageActionAccessProvider session={session} revision={data.snapshotRevision}><PageGraphStoreContext.Provider value={graphStore}>
    <ProductPagesProvider value={productPages}><CommentMentionContext.Provider value={mentionSnapshot}>
    <AppShell
      routeLabel={productRouteLabel({ route, trashOpen: trashAddressed,
        pageTitle: visitedPage ? visitedPage.title || untitledPageDisplayTitle(visitedPage) : drawnPage?.title ?? null, pageTitleSpans: visitedPage?.titleSpans, workspaceName: selectedWorkspace?.name, listSettled,
        pageProblem: deletedWorkspaces.deletedWorkspaceName(routePageId) ? "missing" : pageProblem?.id === routePageId ? pageProblem.problem : undefined })}
      overlays={(
        <>
          <DeveloperBarWhenOn session={session} workspaceId={selectedWorkspace?.id} pageId={selectedPage?.id} />
          <FirstScreenCover listShown={listShown} bodyPending={Boolean(selectedPage) && openBody.pending} graph={graph} />
          {selectedWorkspace && <AuthenticatedSearch
              currentPageId={drawnPage?.id}
              session={session}
              workspace={selectedWorkspace}
              pages={data.pages}
              recentPageIds={recentPageIds}
              favoritePageIds={favoritePageIds}
              localBodyResults={(query) => localBodySearch(selectedPage, openBody.read().graph, openBody.read().document, query)}
              onOpenPage={openPage}
              onSearchTrash={(query) => trashPanelStore.set({ query })}
              onCreatePage={selectedWorkspace.guest ? undefined : () => createRootPage(undefined, { open: false }).catch((error: unknown) => {
                announce(authenticatedProductErrorMessage(error, "pageCreate"), "error");
                return null;
              })}
            />}
          <WithProductWindow store={trashPanelStore}>{(trashPanel) => (trashPanel || trashAddressed) && selectedWorkspace && !selectedWorkspace.guest ? <Suspense fallback={null}>
            <AuthenticatedTrashView
              session={session}
              workspace={selectedWorkspace}
              pages={data.pages}
              onOpenPage={(page) => openPage(page, false)}
              onOpenRow={(item) => {
                // A page of a data source in the trash opens at its own address, and so does a template, whole (useTemplateAddress).
                if (item.sourceInTrash || item.template) {
                  pushRoute(pageHref(item.rowId));
                  return;
                }
                const container = data.pages.find((candidate) => candidate.id === item.containerPageId);
                if (!container) return;
                openPage(container, false);
                replaceRoute(pageHref(container.id, new URLSearchParams({ p: item.rowId, pm: "f" })));
              }}
              onPageChange={(page) => setData((current) => upsertBootstrapPage(current, page))}
              onRefresh={async () => { await refresh(session); }}
              onStatus={announce}
              initialQuery={trashPanel?.query ?? ""}
              onClose={() => {
                trashPanelStore.set(null);
                // Leave the trash's own address, unless a page opened from it already has.
                if (routeInfoFromPath(window.location.pathname).kind === "trash") replaceRoute("/");
              }}
            />
          </Suspense> : null}</WithProductWindow>
          {selectedWorkspace ? <ProductPageCards pages={data.pages} session={session} workspaceId={selectedWorkspace.id} /> : null}
          <PersonHoverCard session={session} />
          <ProductDialogHosts />
          {bodyPage && bodyPage.kind !== "database" && !inlineDatabase ? (
            <WithPageGraph store={graphStore} pick={pickPageGraph}>{(shown) => (
              <PagePresentationHost page={bodyPage} graph={shown} collaborationDocument={collaboration.document} session={session} />)}</WithPageGraph>
          ) : null}
          <AuthenticatedPagePeek pages={data.pages} session={session} organizationId={selectedWorkspace?.organizationId}
            onTitleChange={pageMenuActions.changePageTitle} onTitleTyped={pageMenuActions.showTypedTitle}
            onPresentationChange={(page, change) => void pageMenuActions.changePagePresentation(page, change)}
            onPageChange={applyPageProjection} favoritePageIds={favoritePageIds}
            onFavoriteChange={(pageId, favorite) => pageFavoriteService.setFavorite(pageId, favorite).then(() => undefined)}
            actions={{ onPageMetadataChange: pageMenuActions.changePageMetadata, onDuplicatePage: pageMenuActions.duplicatePage,
              onTrashChange: pageMenuActions.trashPage, onStatus: announce }} />
          {selectedWorkspace && <WithProductWindow store={importWindowStore}>{(importing) => <Suspense fallback={null}>
            <ImportDialog
              open={importing !== null}
              session={session}
              workspaceId={selectedWorkspace.id} workspaceName={selectedWorkspace.name}
              initialSource={importing?.source ?? "file"}
              destination={importing?.destination ?? null}
              onClose={() => importWindowStore.set(null)}
            />
          </Suspense>}</WithProductWindow>}
          <AuthenticatedSettingsWindow settingsWindow={settingsWindow} session={session} onSession={onSession}
            localLockSettings={localLockSettings} workspace={selectedWorkspace} workspaces={data.workspaces}
            refresh={() => refresh(session)} onWorkspaceChange={mergeWorkspace} administrator={administrator}
            onWorkspaceDeleted={deletedWorkspaces.leaveDeleted} />
          <WithProductWindow store={shortcutsWindowStore}>{(shortcuts) => shortcuts && (
            <Suspense fallback={null}><KeyboardShortcutsDialog onClose={() => shortcutsWindowStore.set(null)} /></Suspense>
          )}</WithProductWindow>
          {firstRun}
          {newShortcutMenu}
          {selectedPage && <WithProductWindow store={databasePickerStore}>{(picking) => picking && (
            <Suspense fallback={null}><PageBodyDatabasePicker actions={editorActions} onClose={() => setDatabasePickerOpen(false)} /></Suspense>
          )}</WithProductWindow>}
        </>
      )}
      notice={topNotice}
      status={(
        <>
        <PageCopyNotice />
        {selectedPageId ? <PageCopyFollow session={session} pageId={selectedPageId} onProgress={() => void refreshPages()} /> : null}
        <SyncStatusBadge bounds={syncStatus.bounds} degraded={syncStatus.degraded} service={syncStatus.service} userId={session.user.id}
          // The body drawn, a database's page drawn whole too (useDrawnPage.ts useDrawnPageSync).
          serverRefuses={serverRefusal(serverPair)} realtimeLost={drawnPageSync.realtimeLost} readOnly={drawnPageSync.readOnly} />
        </>
      )}
      sidebar={(layout) => (
        <Sidebar
          {...layout}
          session={session}
          workspace={selectedWorkspace}
          workspaces={data.workspaces} workspacesShown={listShown} noWorkspaceOffer={noWorkspaceOffer}
          pages={selectedWorkspaceTreePages} trashedPages={selectedWorkspaceTrashedPages}
          sharedPageIds={data.sharedPageIds ?? noSharedPageIds}
          workspaceWidePageIds={data.workspaceWidePageIds ?? noSharedPageIds}
          favoritePageIds={favoritePageIds}
          favoriteDatabases={databaseFavorites}
          selectedPageId={visitedPage?.id ?? null}
          trashActive={trashAddressed}
          creatingPage={creatingPage}
          onCreatePage={(teamspaceId, options) => void createRootPage(teamspaceId, options)}
          loadDatabaseSources={loadAddDatabaseSources}
          onCreateRootPage={(database, place) => createRootPage(place?.teamspaceId, { open: "asker", database, title: place?.title, databaseChoice: place?.databaseChoice })}
          onCreateSubpage={sidebarPageTree.createSubpage}
          onDuplicatePage={pageMenuActions.duplicatePage}
          onFavoriteChange={(pageId, favorite) => pageFavoriteService.setFavorite(pageId, favorite).then(() => undefined)}
          onPageChange={sidebarPageTree.changePage}
          onPageMetadataChange={pageMenuActions.changePageMetadata}
          onTransferBlock={sidebarPageTree.transferBlock}
          onStatus={announce}
          onCreateWorkspace={createWorkspaceFromDialog}
          onJoinWorkspace={joinWorkspace}
          onWorkspaceCreateOpenChange={followWorkspaceCreateOpen}
          onTeamspaceDialogOpenChange={followTeamspaceWindowOpen}
          onOpenSettings={settingsWindow.open}
          onOpenHome={openWorkspaceHome}
          onOpenPage={openPage}
          onOpenSearch={openSearch}
          onOpenTrash={openTrash}
          onOpenLibrary={(view) => pushRoute(libraryHref(view))}
          onSelectWorkspace={selectWorkspace}
          onSignOut={productSignOut.requestSignOut}
          pendingSignOut={productSignOut.pendingSignOut}
        />
      )}
    >
      <AuthenticatedProductRoute
        account={session.user.email || session.user.name}
        bootstrapLoaded={listSettled}
        deletedWorkspaceName={deletedWorkspaces.deletedWorkspaceName(routePageId)}
        onAddAccount={beginAddingAccount}
        onPageProblem={rememberPageProblem}
        onReadPageList={refreshPages}
        onReadPage={(pageId) => readPageOutsideList(session, pageId, setData, refreshPages)}
        pageListCurrent={(pageId) => openedHere.current.pageId === pageId && openedHere.current.open && !pageBecameRow(pageId)}
        onRequestAccess={(pageId) => requestPageAccess(session, pageId)}
        pathname={pathname}
        sharedRowView={(fallback) => {
          // A row's own address once usePageLinkRoute leaves it, a peek over it opening beside it (AuthenticatedPagePeek);
          // over a page the reader can't see, the row the peek names.
          const peekId = typeof window === "undefined" ? null : new URLSearchParams(window.location.search).get("p");
          const rowView = (rowId: string, missing: ReactNode) => <Suspense key={rowId} fallback={<div aria-busy="true" />}>
            <StandaloneRowPage session={session} rowId={rowId}
              onShown={(shown) => {
                showRow({ id: rowId, title: shown.title, inTrash: shown.inTrash, parentId: shown.parentId });
                if (shown.workspaceId !== selectedWorkspace?.id && data.workspaces.some(({ id }) => id === shown.workspaceId)) setSelectedWorkspaceId(shown.workspaceId);
              }}
              organizationId={selectedWorkspace?.organizationId} fallback={missing} onClose={() => pushRoute("/")} /></Suspense>;
          const peeked = peekId && peekId !== routePageId ? rowView(peekId, fallback) : fallback;
          return routePageId ? (linkAsked === routePageId ? <Suspense fallback={<div aria-busy="true" />}><RowPageReading rowId={routePageId} /></Suspense> : rowView(routePageId, peeked)) : peeked;
        }}
        selectedPageId={selectedPage?.id ?? null}
        trashView={homeView}
        pageView={selectedPage && !leadingToDatabase ? (guestMFARequired && bodyPage ? <GuestMFARequired session={session} /> : !bodyPage ? (
          <WikiPageView page={selectedPage} pages={data.pages} session={session} organizationId={selectedWorkspace?.organizationId}
            favorite={data.pageFavorites?.find(({ pageId }) => pageId === selectedPage.id)?.favorite ?? false}
            onClose={() => pushRoute(selectedPage.parentId ? pageHref(selectedPage.parentId) : "/")}
            menu={{ onPageChange: applyPageProjection, onPageMetadataChange: pageMenuActions.changePageMetadata,
              onFavoriteChange: (favorite) => pageFavoriteService.setFavorite(selectedPage.id, favorite).then(() => undefined),
              onPresentationChange: (change) => pageMenuActions.changePagePresentation(selectedPage, change),
              onDuplicatePage: pageMenuActions.duplicatePage, onTrashChange: pageMenuActions.trashPage,
              onImport: (page) => openImport("file", { pageId: page.id, title: page.title || untitledPageDisplayTitle(page) }),
              onStatus: announce }} />
        ) : (
          <PageView
            page={selectedPage}
            organizationId={selectedWorkspace?.organizationId} workspaceGuest={selectedWorkspace?.guest === true}
            canDeleteForever={selectedWorkspace ? canManageNativeWorkspace(selectedWorkspace) : false}
            landingPage={Boolean(selectedWorkspace && canManageNativeWorkspace(selectedWorkspace) && selectedWorkspace.landingPageId === selectedPage.id)}
            session={session}
            pages={data.pages}
            presence={collaboration.presence}
            onPageChange={applyPageProjection}
            favorite={data.pageFavorites?.find(({ pageId }) => pageId === selectedPage.id)?.favorite ?? false}
            onFavoriteChange={(favorite) => pageFavoriteService.setFavorite(selectedPage.id, favorite).then(() => undefined)}
            // A full-page database's name follows its title on the server; a typed title shows at once.
            onTitleChange={(title, base, spans) => pageMenuActions.changePageTitle(selectedPage, title, base, spans)}
            onTitleTyped={(title) => pageMenuActions.showTypedTitle(selectedPage, title)}
            onPresentationChange={(change, basePage) => pageMenuActions.changePagePresentation(basePage ?? selectedPage, change)}
            onPageMetadataChange={pageMenuActions.changePageMetadata} onDuplicatePage={pageMenuActions.duplicatePage}
            databasePage={selectedPage.kind === "database" || Boolean(inlineDatabase)}
            inlineDatabasePage={trashedPage.withBanner(inlineDatabasePageView(inlineDatabase, selectedPage, databaseViewAdapter))}
            databaseDescription={databasePageSource(pageGraph, selectedPage.kind, inlineDatabase, databaseViewAdapter) ?? wikiPageSource(graph, selectedPage, databaseViewAdapter.changeDescription)}
            wikiDatabase={wikiLayout ? wikiDatabaseViews(wikiLayout, graph, !graphCanEdit || pageLock.lockedForMe || suggesting.active,
              databaseViewAdapter, renderPageEditor(selectedPage, pickWikiDatabase, wikiLayout.block.id), graphStore.get) : undefined}
            onTrashChange={pageMenuActions.trashPage}
            onImport={(page, mode) => openImport("file", {
              pageId: page.id, title: page.title || untitledPageDisplayTitle(page),
              // A wiki's "CSV와 병합" merges into its database, as a database page's into its own.
              ...(mode === "csvMerge" ? { format: "csv", mergeIntoPage: true,
                ...(page.isWiki && page.wikiRootId === page.id && page.wikiDataSourceId ? { mergeDataSourceId: page.wikiDataSourceId } : {}) } : {}) })}
            readDocumentStats={pageMenuActions.readDocumentStats} lockedForMe={pageLock.lockedForMe && !inlineDatabase}
            viewOnly={!graphCanEdit} accessPending={graphAccessPending}
            canComment={graphCanEdit || graphCanSuggest}
            suggesting={suggesting.control}
            lockForMe={{ unlockedForMe: pageLock.unlockedForMe, change: pageLock.changeForMe }}
            onFocusBody={editorActions.focusFirstBody}
            onStatus={announce}
            onPageDeleted={() => { void refresh(session); pushRoute("/"); }}
          >
              {inlineDatabase ? null : <PageContentField key={selectedPage.id} page={selectedPage}
                readOnly={pageLock.lockedForMe || !graphCanEdit} onChange={(content) => void changePage(selectedPage, content)} />}
              {renderPageEditor(selectedPage, pickPageGraph, inlineDatabase?.block.id)}
          </PageView>
        )) : null}
        homeView={homeView}
        peopleView={selectedWorkspace && !selectedWorkspace.guest && route.kind === "people" ? (
          <Suspense fallback={null}><PeopleView session={session} workspace={selectedWorkspace} userId={route.userId} /></Suspense>
        ) : null}
        libraryView={selectedWorkspace && route.kind === "library" ? (
          <Suspense fallback={null}><LibraryView session={session} workspace={selectedWorkspace} pages={selectedWorkspaceTreePages}
            favoritePageIds={favoritePageIds} sharedPageIds={data.sharedPageIds ?? noSharedPageIds} view={route.view}
            canCreatePage={!selectedWorkspace.guest && !creatingPage} onCreatePage={(teamspaceId) => void createRootPage(teamspaceId)}
            onCreateSubpage={sidebarPageTree.createSubpage} onOpenPage={openPage} actions={{ ...pageMenuActions, pages: data.pages,
              onPageChange: sidebarPageTree.changePage, setFavorite: pageFavoriteService.setFavorite, onStatus: announce }} /></Suspense>
        ) : null}
      />
      <PageMentionIconStyles pages={selectedWorkspaceTreePages} trashedPages={selectedWorkspaceTrashedPages} session={session} workspaceId={selectedWorkspace?.id} pageId={selectedPageId} />
    </AppShell>
    </CommentMentionContext.Provider></ProductPagesProvider>
    </PageGraphStoreContext.Provider></PageActionAccessProvider>
  );
}
