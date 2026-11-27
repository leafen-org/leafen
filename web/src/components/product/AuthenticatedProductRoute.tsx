import { Suspense, useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import { useTranslation } from "react-i18next";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { TopBar } from "@/components/page/TopBar";
import {
  HOME_PATH,
  homeRouteRedirectForPath,
  replaceRoute,
  ROUTE_CHANGE_EVENT,
  routeInfoFromPath,
  type RouteMove,
} from "@/lib/router";
import styles from "@/components/page/PageView.module.css";
import type { AccessRequestOutcome } from "@/lib/accessRequests";
import { useAppPlatformKnown, useAppPlatformMissing } from "@/lib/appPlatformAvailability";
import { resetDocumentChrome } from "@/lib/documentChrome";
import { useStartChoosing } from "@/lib/startChoice";
import { usePageWorkspace } from "@/components/auth/usePageLoginWall";
import { resolveAuthenticatedProductRoute } from "./authenticatedProductRouteModel";
import { PageNoAccess, PageNotFound, PageProblemPending } from "./PageNoAccess";
import { useLeavingPage } from "./useLeavingPage";

// The screens a link opens at their top (not pages, which keep their own place).
const SCREENS_FROM_TOP: ReadonlySet<string> = new Set(["apps", "app", "developers", "myTasks"]);
const AppHost = lazyScreen(() => import("@/components/apps/AppHost"));
const PersonAppsPage = lazyScreen(() => import("@/components/apps/PersonAppsPage"));
const DeveloperToolsPage = lazyScreen(() => import("@/components/developers/DeveloperToolsPage"));
const MyTasksPage = lazyScreen(() => import("@/components/myTasks/MyTasksPage"));
const AppsMissingPage = lazyScreen(() => import("@/components/apps/AppsMissingPage"), { entry: true });
const JoinWorkspacePage = lazyScreen(() => import("@/components/join/JoinWorkspacePage"), { entry: true });
const MemberFormPage = lazyScreen(() => import("@/components/forms/MemberFormPage"), { entry: true });

function RouteFallback() {
  const { t } = useTranslation(["app", "common"]);
  return <div aria-busy="true" aria-label={t("app:loading")} style={{ minHeight: "100%" }} />;
}

/**
 * A page that is there but the person can't open has its own screen. Only
 * one that isn't there at all reads as missing. One they may open that their
 * list doesn't hold opens in its place (pagesOutsideList.ts).
 */
function PageProblem({ pageId, account, onAddAccount, onRequestAccess, onKnown, onReadList, onReadPage, listCurrent }: {
  pageId: string;
  account: string;
  onAddAccount?: () => void;
  onRequestAccess: (pageId: string) => Promise<AccessRequestOutcome>;
  onKnown?: (pageId: string, problem: "no-access" | "missing") => void;
  /** Reads the person's page list again; a page now in it opens in this screen's place. */
  onReadList?: () => Promise<unknown>;
  /** Reads the page itself; one the person may open opens in this screen's place, refused when not. */
  onReadPage?: (pageId: string) => Promise<unknown>;
  /** The list on screen was read after the page left it: it was open here and was taken away. */
  listCurrent?: boolean;
}) {
  const workspace = usePageWorkspace(pageId);
  const workspaceName = workspace?.name;
  // The list on screen may be older than the share: a page someone was just
  // given, opened from its notice, was called closed until the window was
  // reloaded (2026-11-05). It is read once before saying so, beside the read
  // of the page's workspace rather than after it, and not at all when the page
  // was open here and a fresh list took it away (2026-11-07: a guest whose
  // access was removed waited on both reads in turn behind an empty page).
  const [listReadFor, setListReadFor] = useState<string | null>(null);
  const listRead = listReadFor === pageId || !onReadList || Boolean(listCurrent);
  useEffect(() => {
    if (listRead) return;
    let current = true;
    void Promise.resolve(onReadList?.()).catch(() => undefined).finally(() => { if (current) setListReadFor(pageId); });
    return () => { current = false; };
  }, [listRead, onReadList, pageId]);
  // The page itself is read beside them: one no list holds (pagesOutsideList.ts)
  // said by its address that it couldn't be opened (2026-11-14).
  const [pageReadFor, setPageReadFor] = useState<string | null>(null);
  const pageRead = pageReadFor === pageId || !onReadPage;
  const readPage = useRef(onReadPage);
  readPage.current = onReadPage;
  useEffect(() => {
    if (pageRead) return;
    let current = true;
    void Promise.resolve(readPage.current?.(pageId)).catch(() => undefined).finally(() => { if (current) setPageReadFor(pageId); });
    return () => { current = false; };
  }, [pageId, pageRead]);
  const known = workspaceName !== undefined && (!workspaceName || (listRead && pageRead));
  const checkAccess = useCallback(async () => { await onReadList?.(); }, [onReadList]);
  useEffect(() => {
    if (known) onKnown?.(pageId, workspaceName ? "no-access" : "missing");
  }, [known, onKnown, pageId, workspaceName]);
  // Its outline, as Notion's comes before its card, not an empty page beside a list that holds nothing.
  if (!known) return <PageProblemPending />;
  if (!workspaceName) return <PageNotFound account={account} onAddAccount={onAddAccount} />;
  return <PageNoAccess pageId={pageId} workspaceName={workspaceName} appSlug={workspace?.appSlug} account={account} onAddAccount={onAddAccount}
    onRequest={onRequestAccess} onCheckAccess={onReadList ? checkAccess : undefined} />;
}

function RouteProblem({ kind, workspaceName = "" }: {
  kind: "invalid" | "not-found" | "shared" | "deleted-workspace";
  /** The deleted workspace the page was in. */
  workspaceName?: string;
}) {
  const { t } = useTranslation(["routeState", "common"]);
  const title =
    kind === "shared"
      ? t("routeState:sharedTitle")
      : kind === "not-found"
        ? t("routeState:notFoundTitle")
        : kind === "deleted-workspace"
          ? t("routeState:deletedWorkspaceTitle")
          : t("routeState:invalidTitle");
  const body =
    kind === "shared"
      ? t("routeState:sharedBody")
      : kind === "not-found"
        ? t("routeState:notFoundBody")
        : kind === "deleted-workspace"
          ? t("routeState:deletedWorkspaceBody", { name: workspaceName })
          : t("routeState:invalidBody");
  // A page that was open and is no longer reachable keeps no name in the
  // window, as the same address loaded afresh.
  useEffect(() => { resetDocumentChrome(); }, []);
  const home = (
    <button
      type="button"
      className={styles.restoreButton}
      onClick={() => replaceRoute("/")}
    >
      {t("routeState:openHome")}
    </button>
  );

  return (
    <>
      <TopBar title={title} />
      <div className={styles.missing} data-surface="route-problem" role="status">
        <h1 className={styles.missingHeading}>{title}</h1>
        <p>{body}</p>
        <div className={styles.missingActions}>{home}</div>
      </div>
    </>
  );
}

export function AuthenticatedProductRoute({
  account,
  bootstrapLoaded,
  deletedWorkspaceName,
  homeView,
  libraryView,
  onAddAccount,
  onPageProblem,
  onReadPageList,
  onReadPage,
  onRequestAccess,
  pageListCurrent,
  pageView,
  pathname,
  peopleView,
  selectedPageId,
  sharedRowView,
  trashView,
}: {
  /** How the person is signed in, for the screen of a page they can't open. */
  account?: string;
  onAddAccount?: () => void;
  /** What a page not in the list turned out to be, for what a screen reader hears. */
  onPageProblem?: (pageId: string, problem: "no-access" | "missing") => void;
  /** Reads the page list again before a page not in it is called closed. */
  onReadPageList?: () => Promise<unknown>;
  /** Reads a page not in the list, which opens when the person may open it (PageProblem's onReadPage). */
  onReadPage?: (pageId: string) => Promise<unknown>;
  /** Whether the list was read after a page left it (PageProblem's listCurrent). */
  pageListCurrent?: (pageId: string) => boolean;
  bootstrapLoaded: boolean;
  /** The workspace this tab deleted that the address's page was in. */
  deletedWorkspaceName?: string;
  homeView: ReactNode;
  /** The library, which a reader without a workspace has none of. */
  libraryView?: ReactNode;
  /** Asks for a page the person can't open. */
  onRequestAccess?: (pageId: string) => Promise<AccessRequestOutcome>;
  pageView: ReactNode;
  pathname: string;
  /** The people list and a member's profile, which a guest has none of. */
  peopleView?: ReactNode;
  selectedPageId: string | null;
  /**
   * A database page the link names, opened on its own when its database's
   * page is not open to the viewer; it shows the fallback when that fails.
   */
  sharedRowView?: (fallback: ReactNode) => ReactNode;
  trashView: ReactNode;
}) {
  const homeRedirect = homeRouteRedirectForPath(pathname);
  const route = routeInfoFromPath(pathname);
  // A build without the app platform has no app places (null until known),
  // and a phone's own server or the App Store edition says why there.
  const appPlatform = useAppPlatformKnown();
  const appPlatformMissing = useAppPlatformMissing();
  const resolution = resolveAuthenticatedProductRoute(
    route,
    selectedPageId,
    bootstrapLoaded,
  );

  useEffect(() => {
    if (homeRedirect) replaceRoute("/");
  }, [homeRedirect]);
  // Home shown at the root takes its own address once the start page is
  // chosen, or none is: a reload there or the address opened again is home
  // again, as Notion's (seen 2026-11-13), while the root stays where the app
  // starts on its start page (useStartPage).
  const startChoosing = useStartChoosing();
  useEffect(() => {
    if (pathname === "/" && bootstrapLoaded && !startChoosing) replaceRoute(`${HOME_PATH}${window.location.search}${window.location.hash}`);
  }, [pathname, bootstrapLoaded, startChoosing]);

  // Each screen names the window as it opens; till it does, the window shows
  // "Leafen", not the screen before it (2026-10-25: a new screen kept
  // "시작하기 | Leafen"), and its tab in the Mac and Windows apps no name, as
  // Notion's while a page loads. A screen with no name of its own says so
  // (resetDocumentChrome). A layout effect, so it comes before the screen's own.
  const routeKey = route.kind === "app" ? `/apps/${route.slug}` : pathname;
  useLayoutEffect(() => { resetDocumentChrome(true); }, [routeKey]);
  // A screen that isn't a page opens at its top when a link opens it, its own
  // too; the shared scroller kept the last place (2026-11-05: Apps reopened
  // from a link halfway down). A link's move is the router's event: one to
  // another screen, or a push to the very address it is on. A window or side
  // view opening or closing on the same screen (`?p=`, settings) keeps the
  // place, as back and forward (the browser's popstate) and a page's own do.
  useEffect(() => {
    const here = () => `${window.location.pathname}${window.location.search}${window.location.hash}`;
    let lastPath = window.location.pathname;
    let lastAddress = here();
    const remember = () => { lastPath = window.location.pathname; lastAddress = here(); };
    const onLinkMove = (event: Event) => {
      const replace = (event as CustomEvent<RouteMove | undefined>).detail?.replace ?? false;
      const otherScreen = window.location.pathname !== lastPath;
      const sameAddressAgain = !replace && here() === lastAddress;
      remember();
      if (!(otherScreen || sameAddressAgain)) return;
      if (!SCREENS_FROM_TOP.has(routeInfoFromPath(window.location.pathname).kind)) return;
      window.requestAnimationFrame(() => document.querySelector<HTMLElement>("[data-app-main]")?.scrollTo({ top: 0 }));
    };
    window.addEventListener(ROUTE_CHANGE_EVENT, onLinkMove);
    window.addEventListener("popstate", remember);
    return () => {
      window.removeEventListener(ROUTE_CHANGE_EVENT, onLinkMove);
      window.removeEventListener("popstate", remember);
    };
  }, []);

  let content: ReactNode;
  if (resolution === "home") content = homeView;
  // Settings and trash the reader has none of, such as a guest's, are not
  // found once the page list has loaded.
  else if (resolution === "trash") content = trashView ?? (bootstrapLoaded ? <RouteProblem kind="not-found" /> : <RouteFallback />);
  else if (resolution === "library") content = libraryView ?? (bootstrapLoaded ? <RouteProblem kind="not-found" /> : <RouteFallback />);
  else if (resolution === "people") content = peopleView ?? (bootstrapLoaded ? <RouteProblem kind="not-found" /> : <RouteFallback />);
  else if (resolution === "page") content = pageView ?? <RouteFallback />;
  else if ((route.kind === "app" || route.kind === "apps") && appPlatform !== true) {
    content = appPlatform === null ? <RouteFallback />
      : appPlatformMissing ? <AppsMissingPage missing={appPlatformMissing} /> : <RouteProblem kind="not-found" />;
  }
  else if (route.kind === "app") content = <AppHost slug={route.slug} subpath={route.subpath} />;
  else if (route.kind === "apps") content = <PersonAppsPage />;
  else if (route.kind === "developers") content = <DeveloperToolsPage tab={route.tab} connectionId={route.connectionId} />;
  else if (route.kind === "myTasks") content = <MyTasksPage />;
  else if (route.kind === "join") content = <JoinWorkspacePage />;
  else if (route.kind === "form") content = <MemberFormPage token={route.formToken} />;
  else if (resolution === "loading") content = <RouteFallback />;
  else if (resolution === "shared") content = <RouteProblem kind="shared" />;
  else if (resolution === "not-found" && deletedWorkspaceName !== undefined) {
    content = <RouteProblem kind="deleted-workspace" workspaceName={deletedWorkspaceName} />;
  }
  else if (resolution === "not-found") {
    const pageId = route.kind === "page" ? route.pageId : route.kind === "database" ? route.databaseId : undefined;
    const problem = pageId && onRequestAccess
      ? <PageProblem pageId={pageId} account={account ?? ""} onAddAccount={onAddAccount} onRequestAccess={onRequestAccess} onKnown={onPageProblem} onReadList={onReadPageList}
        onReadPage={onReadPage} listCurrent={pageListCurrent?.(pageId)} />
      : <RouteProblem kind="not-found" />;
    // A page open here and taken away is no row shared on its own to try first.
    content = sharedRowView && !(pageId && pageListCurrent?.(pageId)) ? sharedRowView(problem) : problem;
  }
  else content = <RouteProblem kind="invalid" />;

  // An app keeps its frame while it moves within itself.
  const screen = (
    <ErrorBoundary scope="route" key={routeKey} reloadOnRetry>
      <Suspense fallback={<RouteFallback />}>
        {content}
      </Suspense>
    </ErrorBoundary>
  );
  // A page left for another kind of screen goes once that screen is painted (lib/pageLeftAfterPaint.ts).
  const { leaving, place } = useLeavingPage(routeKey, screen, resolution === "page" ? selectedPageId : null, selectedPageId);
  return <>{leaving}{place}{screen}</>;
}
