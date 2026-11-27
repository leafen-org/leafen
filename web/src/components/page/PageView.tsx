import type { BodyFocus } from "@/lib/pageTitleFocus";
import type { ProductStatusSink } from "@/lib/productStatus";
import { useCallback, useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { commentServiceFor } from "@/lib/commentService";
import { setDocumentChrome } from "@/lib/documentChrome";
import { useNamedPage } from "@/lib/pageNames"; import { nameTypedForPage } from "@/lib/databaseNamesKept";
import { usePageFaviconHref } from "./pageFavicon";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { matchesKeyboardShortcut } from "@/lib/keyboardShortcuts";
import type { PagePresenceSnapshot } from "@/lib/pagePresenceView";
import type { NativePageMetadataChange } from "@/lib/nativePageMetadata";
import type { PageLockForMe, PageSuggestingControl } from "@/components/RowMenu";
import type { RowMenuDocumentStats } from "@/lib/rowMenuDocumentStats";
import type { Page, Session } from "@/types";
import type { PageTitleBase } from "@/lib/pageTitleFollow";
import type { TextSpan } from "@/lib/types";
import { WikiDatabaseView, type WikiDatabaseViews } from "../WikiDatabaseView";
import { CommentsPanel } from "../CommentsPanel";
import { AppSidePanel } from "../apps/AppSidePanel";
import { useCommentLinkOpen } from "../comments/commentLinks";
import { MarginDiscussions } from "../comments/MarginDiscussions";
import { inlineDatabasePlaceholderTitle } from "../editor/databaseTitles";
import { type DatabaseDescriptionSource, useDatabaseDescription, useTrashedSourcesHead } from "./DatabaseDescription";
import { useInlineDatabaseHeadLocked } from "../database/databaseHeadLock";
import { nextCover } from "@/lib/covers";
import { blockNameSpans, type InlineDatabasePage } from "@/lib/inlineDatabaseFullPage";
import { InlineDatabasePageTitle } from "./InlineDatabasePageTitle";
import { usePageCommentSummary } from "./usePageCommentSummary";
import { usePageColumn } from "./usePageColumn";
import {
  OPEN_COMMENTS_EVENT,
  openComments,
  useMarginCompose,
  type OpenCommentsDetail,
} from "../comments/commentPanelEvents";
import { pageLookup } from "@/lib/pageArchive";
import { pageTrashRoot } from "@/lib/pageTrash";
import { PageTrashBanner } from "./PageTrashBanner";
import { PageArchiveBanner } from "./PageArchiveBanner";
import { PageTeamspaceArchiveBanner } from "./PageTeamspaceArchiveBanner";
import { PageTemplateBands } from "./PageTemplateBands";
import { PageCover } from "./PageCover";
import {
  PageAnalyticsObserver,
  type PageAnalyticsObservation,
} from "./PageAnalyticsObserver";
import { PageFindBar, selectedTextForPageFind } from "./PageFindBar";
import { PageHeader } from "./PageHeader";
import { PageHeaderBacklinks } from "./PageHeaderBacklinks";
import { ReadFailed } from "@/components/ReadFailed";
import { PageVerifiedBadge, PageVerifyControl, useDatabaseVerification } from "./PageVerification";
import type { PagePresentationChange } from "./pagePresentation";
import { PageSiteBand, PageTopBands } from "./PageTopBands";
import { LandingPageBanner } from "./LandingPageBanner";
import { useAppFileEditAccess } from "@/components/apps/appFileOpening";
import { TopBar } from "./TopBar";
import { panelOpeningBeside, updatesBesideShown, usePageSidePanel } from "./usePageSidePanel";
import { PageUpdatesSidePanel } from "./PageUpdatesSidePanel";
import { PageBodyLead, readPageBodyLead } from "./pageBodyLead";
import { PageCustomizationContext } from "./pageCustomizationContext";
import styles from "./PageView.module.css";
import { usePageDiscussionPlace } from "./usePageDiscussionPlace";
import { usePageScrollPlace } from "@/lib/pageScrollPlace";
import { holdPageScrollHost } from "@/components/shell/pageScrollHost";

export function PageView({
  page,
  children,
  onTitleChange,
  onTitleTyped,
  onPresentationChange,
  onFocusBody,
  publicReadOnly = false,
  sharedToken,
  publicSiteSlug,
  faviconHref,
  unbranded = false,
  hideTopBar = false,
  topBarActions,
  session,
  pages,
  presence,
  onPageChange,
  favorite = false,
  onFavoriteChange,
  onPageMetadataChange,
  onDuplicatePage,
  databasePage,
  inlineDatabasePage,
  onTrashChange,
  onImport,
  readDocumentStats,
  lockedForMe,
  viewOnly = false,
  accessPending = false,
  canComment = true,
  suggesting,
  lockForMe,
  onStatus,
  organizationId,
  onPageDeleted,
  wikiDatabase,
  databaseDescription,
  workspaceGuest = false,
  canDeleteForever = false,
  landingPage = false,
}: {
  page: Page;
  children: ReactNode;
  /** Saves the title typed in the page's head, with the title it was typed on (PageHeader). */
  onTitleChange: (title: string, base?: PageTitleBase, spans?: TextSpan[]) => unknown;
  /** A title as it is typed, shown before it is saved (PageHeader). */
  onTitleTyped?: (title: string) => void;
  onPresentationChange: (
    change: PagePresentationChange,
    basePage?: Page,
  ) => boolean | Page | void | Promise<boolean | Page | void>;
  onFocusBody: (selection: BodyFocus) => void;
  publicReadOnly?: boolean;
  sharedToken?: string;
  publicSiteSlug?: string;
  faviconHref?: string;
  /** A published site with Leafen's branding off (2026-10-16); a page shown to the public is named alone in the window either way, as Notion's published site (2026-10-19). */
  unbranded?: boolean;
  hideTopBar?: boolean;
  /** A public page's own tools in its bar (a web link visitor's "댓글"). */
  topBarActions?: ReactNode;
  session?: Session;
  pages?: Page[];
  presence?: PagePresenceSnapshot;
  onPageChange?: (page: Page) => void;
  favorite?: boolean;
  onFavoriteChange?: (favorite: boolean) => Promise<void>;
  onPageMetadataChange?: (
    page: Page,
    change: NativePageMetadataChange,
    signal: AbortSignal,
  ) => Promise<boolean | Page | void>;
  onDuplicatePage?: (page: Page, signal: AbortSignal) => Promise<Page>;
  databasePage?: boolean;
  /** One of the page's inline databases opened alone, as its own page. */
  inlineDatabasePage?: InlineDatabasePage;
  onTrashChange?: (
    page: Page,
    inTrash: boolean,
    signal: AbortSignal,
  ) => Promise<boolean | Page | void>;
  /** Opens the import screen, which imports into the page. */
  onImport?: (page: Page, mode?: "csvMerge") => void;
  readDocumentStats?: (page: Page) => RowMenuDocumentStats;
  /** The page's lock stops this person's changes. */
  lockedForMe?: boolean;
  /** This person can view or comment on the page but not change it. */
  viewOnly?: boolean;
  /** What this person may do on the page shown is still being read. */
  accessPending?: boolean;
  /** This person can comment on the page; one who may only view reads its discussions. */
  canComment?: boolean;
  /** Suggesting mode, where the reader may suggest edits to the page's text. */
  suggesting?: PageSuggestingControl;
  lockForMe?: PageLockForMe;
  onStatus?: ProductStatusSink;
  organizationId?: string;
  onPageDeleted?: () => void;
  /** A wiki whose pages are its database's pages: its database's views. */
  wikiDatabase?: WikiDatabaseViews;
  /** The database the page shows as its own, whose description sits under its title: its writes, where the reader may change it. */
  databaseDescription?: DatabaseDescriptionSource;
  /** The viewer is a guest in the page's workspace and reads none of its settings. */
  workspaceGuest?: boolean;
  /** The viewer may delete pages forever: the workspace's owners and admins. */
  canDeleteForever?: boolean;
  /** The page is its workspace's landing page and the viewer one who can change it. */
  landingPage?: boolean;
}) {
  const { t } = useTranslation("pageHeader");
  // Its place in each step of the tab's history comes back with the way back (pageScrollPlace.ts).
  const scrollRef = usePageScrollPlace<HTMLDivElement>(page.id);
  // The page area it stands in lays it under the bars over it (pageScrollHost.ts).
  const scrollHostRef = useCallback((scroll: HTMLDivElement | null) => {
    scrollRef.current = scroll;
    const release = holdPageScrollHost(scroll);
    return () => { scrollRef.current = null; release?.(); };
  }, [scrollRef]);
  const [findOpen, setFindOpen] = useState(false);
  const [findFocusTick, setFindFocusTick] = useState(0);
  const [findInitialQuery, setFindInitialQuery] = useState("");
  const [commentPanel, setCommentPanel] = useState<OpenCommentsDetail | null>(null);
  // An app opened beside the page, in the discussions' place.
  const [appPanel, setAppPanel] = useState<string | null>(null);
  // "업데이트와 애널리틱스" beside the page, as Notion's (seen 2026-11-01).
  const [updatesAsked, setUpdatesPanel] = useState(false), updatesPanel = updatesBesideShown(updatesAsked, !viewOnly);
  const docRef = useRef<HTMLDivElement>(null);
  usePageColumn(docRef);
  const bodyLead = useCallback(() => readPageBodyLead(docRef.current), []);
  // A wide page shows its block discussions beside its text.
  const [marginDiscussions, setMarginDiscussions] = useState(false);
  const marginCompose = useMarginCompose(), takeMarginCompose = marginCompose.take;
  const analyticsInput: PageAnalyticsObservation | null = publicSiteSlug
    ? { kind: "publicSite", siteSlug: publicSiteSlug, pageId: page.id }
    : sharedToken
      ? { kind: "public", token: sharedToken, pageId: page.id }
      : publicReadOnly
        ? null
        : {
            kind: "member",
            workspaceId: page.workspaceId,
            pageId: page.id,
            // An inline database opened alone is viewed as itself, not its page (Notion's, seen 2026-11-11).
            ...(inlineDatabasePage ? { containerId: inlineDatabasePage.blockId } : {}),
            inTrash: page.inTrash,
          };
  // A page in the trash, or under one, reads as it was and takes no edits
  // until the trashed page that holds it is restored.
  const trashRoot = !publicReadOnly && pages ? pageTrashRoot(page, pageLookup(pages)) : null;
  // While suggesting, the page's title, icon and cover stay as they are; locked, its verification is still offered (verifyHeld), as Notion's "인증 추가 · 댓글 추가" (seen 2026-11-14 and 2026-11-16).
  const verifyHeld = publicReadOnly || Boolean(trashRoot || inlineDatabasePage?.trashBanner) || viewOnly || Boolean(suggesting?.active), readOnly = verifyHeld || Boolean(lockedForMe);
  // An inline database opened alone heads itself under its own lock, as a full page under its page's (databaseHeadLock.ts).
  const headLocked = useInlineDatabaseHeadLocked(inlineDatabasePage);
  useAppFileEditAccess(page.id, !readOnly);
  // A view block's page — a dashboard's, or a link's still waiting for its
  // source, opened alone or turned into a page — heads itself with its icon
  // alone, its name in its window and path only (Notion's, seen 2026-11-09).
  const viewBlockPage = inlineDatabasePage ? inlineDatabasePage.viewBlock === true
    : page.kind === "database" && page.viewBlock === true && !databaseDescription?.linked;
  // A description is its database's own page's: a linked view's page, full
  // page or opened alone, neither shows nor offers its source's, as Notion's
  // Sprint board and a linked view opened alone (seen 2026-11-08), nor does a
  // view block's (seen 2026-11-09: "아이콘 추가 · 커버 추가 · 인증 추가").
  const description = useDatabaseDescription({
    source: databaseDescription?.linked || inlineDatabasePage?.linked || viewBlockPage ? null : databaseDescription?.source ?? null,
    readOnly: verifyHeld, locked: Boolean(lockedForMe) || headLocked, onChange: databaseDescription?.onChange,
  });
  const isAuthenticatedWikiRoot = page.isWiki && page.wikiRootId === page.id && !publicReadOnly;
  // A wiki lays its page on the whole canvas and heads it as a database's,
  // its cover a database's too, as Notion's (seen 2026-10-31, 2026-11-11).
  const databaseCanvas = Boolean(databasePage || (isAuthenticatedWikiRoot && page.wikiDataSourceId && wikiDatabase));

  // An inline database opened alone names the window after itself, as Notion's.
  const windowTitle = inlineDatabasePage ? inlineDatabasePage.windowTitle || inlineDatabasePlaceholderTitle() : page.title;
  // Its spans say its mentions in the window's title and the bar (closedMentions.ts).
  const windowTitleSpans = (inlineDatabasePage ? inlineDatabasePage.titleSpans : page.titleSpans) as TextSpan[] | undefined;
  // A linked database's own page heads itself as the database opened alone
  // does — its icon and, once it has one, its name over "↗ 〈source〉" — as
  // Notion's (seen 2026-10-15: turned into a page, it is the same database).
  const generatedTitle = databaseDescription?.generatedPageTitle === page.title;
  // A full-page database its page's title names keeps that name, with its
  // formatting, on its block, and the page takes its words from there in the
  // same change (database_page_name_spans.go).
  const pageName = databaseDescription?.pageName;
  // A full-page database whose own sources are all in the trash heads itself with the open view's source.
  const trashedSourcesHead = useTrashedSourcesHead(databaseDescription, session?.user.id);
  const pageNameSpans = pageName ? blockNameSpans(pageName.spans, page.title) : undefined;
  const writePageName = pageName?.onSpansChange ? (title: string, spans: TextSpan[] = []) => {
    const written = pageName.onSpansChange?.(spans.length ? spans : title ? [{ text: title }] : []);
    nameTypedForPage(page, title, spans, written, onPageChange);
  } : undefined;
  // A full page heading itself so, linked or a view block, keeps its icon,
  // cover and name on the page itself.
  const pageIcon = { icon: page.icon ?? "", iconType: (page.iconType ?? "none") as InlineDatabasePage["icon"]["iconType"], workspaceId: page.workspaceId };
  const linkedPage: InlineDatabasePage | undefined = !inlineDatabasePage && databaseDescription?.linked ? {
    blockId: "", title: generatedTitle ? "" : page.title, linked: true, windowTitle: page.title,
    titleSpans: generatedTitle ? undefined : pageNameSpans as InlineDatabasePage["titleSpans"],
    icon: pageIcon, cover: page.cover ?? "", coverPosition: page.coverPosition ?? 50, href: "",
    onTitleChange: writePageName ? (title, spans) => writePageName(title, spans as TextSpan[] | undefined) : (title) => void onTitleChange(title),
    onIconChange: (icon) => void onPresentationChange(icon),
  } : undefined;
  const nameWrite = databaseDescription?.onNameChange;
  const viewBlockFullPage: InlineDatabasePage | undefined = !inlineDatabasePage && viewBlockPage ? {
    blockId: "", title: page.title, viewBlock: true, windowTitle: page.title, placeholder: untitledPageDisplayTitle(page),
    icon: pageIcon, cover: page.cover ?? "", coverPosition: page.coverPosition ?? 50, href: "",
    onTitleChange: nameWrite ? (title, spans) => nameTypedForPage(page, title, spans as TextSpan[] | undefined, nameWrite(title, (spans ?? []) as TextSpan[]), onPageChange) : (title) => void onTitleChange(title),
    onIconChange: (icon) => void onPresentationChange(icon),
  } : undefined;
  const fullPageHead = linkedPage ?? viewBlockFullPage;
  const headerDatabase = inlineDatabasePage ?? fullPageHead;
  const databaseVerification = useDatabaseVerification(publicReadOnly ? null : session, page.id, inlineDatabasePage?.blockId);
  const pageFavicon = usePageFaviconHref(page);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (!matchesKeyboardShortcut("findInPage", event)) return;
      event.preventDefault();
      setFindInitialQuery(selectedTextForPageFind(docRef.current));
      setFindOpen(true);
      setFindFocusTick((tick) => tick + 1);
    }

    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  useEffect(() => {
    const frame = window.requestAnimationFrame(() => setFindOpen(false));
    return () => window.cancelAnimationFrame(frame);
  }, [page.id]);

  // The page keeps its docked discussions or updates open (pageSidePanelKept); not an inline database alone, on its holder's page.
  const keptPageId = session && !publicReadOnly && !inlineDatabasePage ? page.id : undefined;
  const sidePanelDocked = usePageSidePanel(commentPanel?.all ? "discussions" : appPanel ? "app" : updatesPanel ? "updates" : null, () => {
    setCommentPanel((current) => (current?.all ? null : current)); setAppPanel(null); setUpdatesPanel(false);
  }, keptPageId);

  const [pageDiscussionTarget, setPageDiscussionTarget] = useState<string | undefined>();
  const [pageDiscussionQuiet, setPageDiscussionQuiet] = useState(false);
  useEffect(() => {
    const beside = keptPageId ? panelOpeningBeside(keptPageId) : null; // Its updates asked from a peek, or the panel kept open (usePageSidePanel).
    setCommentPanel(beside === "discussions" ? { pageId: page.id, blockId: null, all: true } : null);
    setAppPanel(null); setUpdatesPanel(beside === "updates");
    setPageDiscussionTarget(undefined);
    // An open thread of the page's own is shown in its discussion under the
    // title; anything else opens in the panel.
    function openPageThread(commentId: string, quiet = false) {
      if (!session) return false;
      const comments = commentServiceFor(session, api).getSnapshot(page.id).comments;
      const comment = comments.find((item) => item.id === commentId);
      const root = comment?.parentId ? comments.find((item) => item.id === comment.parentId) : comment;
      if (page.pageCommentsDisplay === "off" || !root || root.blockId || root.resolved) return false;
      setPageDiscussionTarget(commentId);
      setPageDiscussionQuiet(quiet);
      return true;
    }
    function onOpenComments(event: Event) {
      const detail = (event as CustomEvent<OpenCommentsDetail>).detail;
      if (detail?.pageId !== page.id) return;
      if (!detail.blockId && detail.activeCommentId && openPageThread(detail.activeCommentId, detail.quiet)) return;
      if (detail.all) setUpdatesPanel(false);
      // A new comment on a wide page is written beside its block, where its card will stand.
      setCommentPanel(takeMarginCompose(detail, canComment) ? null : detail);
    }
    window.addEventListener(OPEN_COMMENTS_EVENT, onOpenComments);
    return () => window.removeEventListener(OPEN_COMMENTS_EVENT, onOpenComments);
  }, [canComment, keptPageId, takeMarginCompose, page.id, page.pageCommentsDisplay, session]);
  // A comment's address ("#comment-…") opens its discussion.
  useCommentLinkOpen(session ? commentServiceFor(session, api) : null, page.id);
  // The page's own open threads show under its title whenever it opens, as
  // Notion's; "Add comment" in the header starts one when there are none.
  const pageComments = usePageCommentSummary({ pageId: session ? page.id : undefined, session: session ?? undefined });
  const discussionPlace = usePageDiscussionPlace(page.id, pageComments.open, pageComments.reactions);
  const { composing: composingPageComment, setComposing: setComposingPageComment } = discussionPlace;
  // Its place under the title, apart from the same page shown in a peek.
  const commentsHostId = `page-comments-inline-${page.id}-${useId()}`;
  const pageDiscussionShown = page.pageCommentsDisplay !== "off" && Boolean(session) && discussionPlace.shown;

  const outlineEnabled = page.tableOfContentsDisplay !== "off" && !(databaseDescription && !isAuthenticatedWikiRoot) && !inlineDatabasePage;
  const customization = useMemo(() => ({ outlineEnabled }), [outlineEnabled]);
  return (
    <PageCustomizationContext.Provider value={customization}>
    <PageBodyLead.Provider value={bodyLead}>
      <PageWindowChrome pageId={page.id} typedFor={inlineDatabasePage ? null : page.id} title={windowTitle} titleSpans={windowTitleSpans}
        untitled={untitledPageDisplayTitle(page)} iconHref={faviconHref || pageFavicon} plain={publicReadOnly || unbranded} />
      {analyticsInput ? <PageAnalyticsObserver input={analyticsInput} /> : null}
      {!hideTopBar ? (
        <TopBar
          // An inline database opened alone names itself in its bar, as Notion's (seen 2026-10-04).
          title={inlineDatabasePage ? windowTitle : page.title}
          titleSpans={windowTitleSpans}
          standalone={publicReadOnly}
          screenActions={topBarActions}
          page={publicReadOnly ? undefined : page} named={page}
          pages={pages}
          presence={presence}
          session={session}
          // A database has no page discussions of its own: its bar has no comments, as Notion's.
          onOpenComments={session && !inlineDatabasePage ? () => { setAppPanel(null); setUpdatesPanel(false); setCommentPanel({ pageId: page.id, blockId: null, all: true }); } : undefined}
          onPageChange={onPageChange}
          favorite={favorite}
          onFavoriteChange={onFavoriteChange}
          onPresentationChange={onPresentationChange}
          onPageMetadataChange={onPageMetadataChange}
          onDuplicatePage={onDuplicatePage}
          databasePage={databasePage && linkedPage ? "linked" : databasePage}
          // A linked database is exported as itself, its block or its full
          // page, which the server reads as its source named after it.
          exportDatabase={databasePage && databaseDescription ? (inlineDatabasePage?.linked
            ? { id: inlineDatabasePage.blockId, title: windowTitle, linked: true }
            : linkedPage ? { id: page.id, title: windowTitle, linked: true }
              : { id: databaseDescription.source.id, title: databaseDescription.source.title }) : undefined}
          inlineDatabase={inlineDatabasePage?.blockId}
          trailingCrumb={inlineDatabasePage ? {
            label: windowTitle,
            icon: inlineDatabasePage.icon,
            href: inlineDatabasePage.href,
          } : undefined}
          onTrashChange={onTrashChange}
          onImport={onImport}
          readDocumentStats={readDocumentStats}
          lockForMe={lockForMe}
          canEdit={!viewOnly}
          accessPending={accessPending}
          suggesting={suggesting}
          onStatus={onStatus}
          onClosePanel={!sidePanelDocked ? undefined : commentPanel?.all ? () => setCommentPanel(null) : appPanel ? () => setAppPanel(null)
            : updatesPanel ? () => setUpdatesPanel(false) : undefined}
          commentsPanelOpen={Boolean(commentPanel?.all)}
          onOpenUpdatesPanel={session && pages ? () => { setCommentPanel(null); setAppPanel(null); setUpdatesPanel(true); } : undefined}
          onOpenAppPanel={session ? (appId) => { setCommentPanel(null); setUpdatesPanel(false); setAppPanel(appId); } : undefined}
        />
      ) : null}
      {!hideTopBar && !publicReadOnly && session ? (
        <PageTopBands session={session} workspaceId={page.workspaceId} pageId={page.id} guest={workspaceGuest} trashed={Boolean(trashRoot)} />
      ) : null}
      {!hideTopBar && !publicReadOnly && session && landingPage ? <LandingPageBanner /> : null}
      {!publicReadOnly && pages ? <PageTemplateBands page={page} pages={pages} session={session} viewOnly={viewOnly} /> : null}
      {!publicReadOnly && session && pages ? <PageTeamspaceArchiveBanner page={page} pages={pages} session={session} /> : null}
      <PageFindBar
        focusTick={findFocusTick}
        initialQuery={findInitialQuery}
        onClose={() => setFindOpen(false)}
        open={findOpen}
        pageId={page.id}
        revision={`${page.revision}:${page.cursor}:${page.updatedAt}`}
        rootRef={docRef}
      />
      <div ref={scrollHostRef} data-page-scroll className={`${styles.scroll} nscroll`}
        data-side-panel={session && sidePanelDocked ? "true" : undefined} data-database-canvas={databaseCanvas ? "true" : undefined}>
        {!publicReadOnly && session && pages ? (
          <PageArchiveBanner
            page={page}
            pages={pages}
            session={session}
            onPageMetadataChange={onPageMetadataChange}
          />
        ) : null}
        {trashRoot && session ? <>
          <PageTrashBanner
            page={page}
            trashRoot={trashRoot}
            session={session}
            onPageMetadataChange={onPageMetadataChange}
            onPageDeleted={onPageDeleted} canDeleteForever={canDeleteForever}
          />
          {hideTopBar ? null : <PageSiteBand session={session} workspaceId={page.workspaceId} pageId={page.id} guest={workspaceGuest} />}
        </> : inlineDatabasePage?.trashBanner ?? null}
        {/* An inline database opened alone shows its own cover, not the page's. */}
        {inlineDatabasePage ? (
          <PageCover page={{ ...page, cover: inlineDatabasePage.cover, coverPosition: inlineDatabasePage.coverPosition }}
            onChange={(change) => inlineDatabasePage.onCoverChange?.({ cover: change.cover, coverPosition: change.coverPosition })}
            readOnly={readOnly || headLocked || !inlineDatabasePage.onCoverChange} database />
        ) : <PageCover page={page} onChange={onPresentationChange} readOnly={readOnly} database={databaseCanvas} />}
        <div
          ref={docRef}
          className={styles.doc}
          // The page's frame reads as the app does (its handles, menus, head
          // buttons, icon and tables, lib/readingDirection); each text takes
          // its direction from its words (lib/textDirection), as Notion's.
          data-page-search-root
          data-has-cover={(inlineDatabasePage ? inlineDatabasePage.cover : page.cover) ? "true" : "false"}
          data-kind={inlineDatabasePage ? "database" : "page"}
          data-inline-database-page={inlineDatabasePage ? "true" : undefined}
          data-database-page={databaseCanvas ? "true" : undefined}
          data-font={page.font}
          data-small-text={page.smallText ? "true" : "false"}
          data-full-width={page.fullWidth ? "true" : "false"}
          data-margin-discussions={marginDiscussions ? "true" : undefined}
          data-all-discussions={commentPanel?.all ? "true" : undefined}
          data-public-read-only={publicReadOnly ? "true" : undefined}
        >
          {headerDatabase ? (
            <InlineDatabasePageTitle title={headerDatabase.title} titleSpans={headerDatabase.titleSpans as TextSpan[] | undefined} icon={headerDatabase.icon}
              name={headerDatabase.viewBlock ? "none" : headerDatabase.linked ? "onceNamed" : "shown"} placeholder={headerDatabase.placeholder}
              readOnly={readOnly || headLocked} onTitleChange={headerDatabase.onTitleChange}
              onIconChange={headerDatabase.onIconChange} uploadTarget={{ workspaceId: page.workspaceId, pageId: page.id }}
              onAddCover={fullPageHead ? (page.cover ? undefined : () => void onPresentationChange({ cover: nextCover(""), coverPosition: 50 }))
                : inlineDatabasePage?.onCoverChange && !inlineDatabasePage.cover
                  ? () => inlineDatabasePage.onCoverChange?.({ cover: nextCover(""), coverPosition: 50 }) : undefined}
              description={description}
              verifyControl={fullPageHead ? (session && !verifyHeld ? (
                <PageVerifyControl page={page} session={session} onChange={(change) => onPageChange?.({ ...page, ...change })} />
              ) : undefined) : session && !readOnly && databaseVerification && !databaseVerification.failed ? (
                <PageVerifyControl page={databaseVerification.fields} session={session} onChange={databaseVerification.onChange} />
              ) : undefined}
              verifiedBadge={fullPageHead ? (!publicReadOnly && session ? (
                <PageVerifiedBadge page={page} session={session} organizationId={organizationId}
                  readOnly={readOnly} onChange={(change) => onPageChange?.({ ...page, ...change })} />
              ) : undefined) : session && databaseVerification && !databaseVerification.failed ? (
                <PageVerifiedBadge page={databaseVerification.fields} session={session} organizationId={organizationId}
                  readOnly={readOnly} onChange={databaseVerification.onChange} />
              ) : session && databaseVerification?.failed ? (
                <ReadFailed message={t("pageHeader:verify.loadFailed")} onRetry={databaseVerification.retry} />
              ) : undefined} />
          ) : <PageHeader
            page={trashedSourcesHead ? { ...page, title: trashedSourcesHead.title, titleSpans: trashedSourcesHead.spans }
              : databaseDescription?.generatedPageTitle === page.title ? { ...page, title: "" } : page}
            titlePlaceholder={trashedSourcesHead?.placeholder ?? (databaseDescription?.generatedPageTitle === page.title ? t("pageHeader:addTitle") : undefined)}
            databaseDescription={databaseDescription ? description : undefined}
            databaseHead={Boolean(databaseDescription)}
            // Its name is read the same way by whoever opens it — owner, guest, a web link's or site's visitor.
            databaseName={trashedSourcesHead ? { spans: trashedSourcesHead.spans, onChange: trashedSourcesHead.onChange ?? (() => {}) }
              : databaseDescription?.named && databaseDescription.generatedPageTitle !== page.title ? {
              spans: databaseDescription.source.titleSpans as TextSpan[] | undefined,
              onChange: (title, spans) => nameTypedForPage(page, title, spans, databaseDescription.onNameChange?.(title, spans), onPageChange),
            } : pageName && !databaseDescription?.linked ? { spans: pageNameSpans, onChange: writePageName ?? (() => {}) } : undefined}
            onTitleChange={onTitleChange}
            onTitleTyped={onTitleTyped}
            onPresentationChange={onPresentationChange}
            onFocusBody={onFocusBody}
            readOnly={readOnly}
            staticReadOnlySemantics={Boolean(publicSiteSlug)}
            // A full-page database takes no page comment, as Notion's (seen 2026-10-01).
            onOpenComments={page.pageCommentsDisplay !== "off" && session && canComment && !trashRoot && discussionPlace.addInHead && !databaseDescription
              ? () => setComposingPageComment(true) : undefined}
            // A phone's tools in the title start one in a sheet, a discussion under the title or not (CommentsPanel).
            onStartComment={page.pageCommentsDisplay !== "off" && session && canComment && !trashRoot && !databaseDescription
              ? () => openComments({ pageId: page.id, blockId: null, compose: true }) : undefined}
            commentsHostId={commentsHostId}
            verifyControl={session && !verifyHeld ? (
              <PageVerifyControl page={page} session={session}
                onChange={(change) => onPageChange?.({ ...page, ...change })} />
            ) : undefined}
            verifiedBadge={!publicReadOnly && session ? (
              <PageVerifiedBadge page={page} session={session} organizationId={organizationId}
                readOnly={readOnly} onChange={(change) => onPageChange?.({ ...page, ...change })} />
            ) : undefined}
            backlinks={!publicReadOnly && session && pages ? (
              <PageHeaderBacklinks
                page={page}
                pages={pages}
                session={session}
                onPageChange={onPageChange}
              />
            ) : undefined}
          />}
          {isAuthenticatedWikiRoot && page.wikiDataSourceId && wikiDatabase ? (
            <WikiDatabaseView root={page} wiki={wikiDatabase}>{children}</WikiDatabaseView>
          ) : children}
          {page.inlineCommentsDisplay !== "minimal" && session && !publicReadOnly && !inlineDatabasePage ? (
            <MarginDiscussions pageId={page.id} session={session} workspaceId={page.workspaceId}
              canEditPage={!viewOnly} canComment={canComment} compose={marginCompose.compose} onComposeClose={marginCompose.closeCompose} onStatus={onStatus}
              onActiveChange={setMarginDiscussions} onFitChange={marginCompose.setRoom} />
          ) : null}
        </div>
      </div>
      {session && commentPanel ? (
        <CommentsPanel
          key={`${commentPanel.pageId}:${commentPanel.all ? "all" : commentPanel.blockId ?? "page"}:${commentPanel.activeCommentId ?? ""}:${commentPanel.threadIds?.join(",") ?? ""}:${commentPanel.compose ? "new" : ""}`}
          pageId={commentPanel.pageId}
          all={commentPanel.all}
          docked={commentPanel.all && sidePanelDocked}
          workspaceId={page.workspaceId}
          blockId={commentPanel.blockId}
          compose={commentPanel.compose}
          activeCommentId={commentPanel.activeCommentId}
          activeQuiet={commentPanel.quiet}
          initialQuote={commentPanel.initialQuote}
          initialQuoteStart={commentPanel.initialQuoteStart}
          initialQuoteEnd={commentPanel.initialQuoteEnd}
          initialQuotePlaces={commentPanel.initialQuotePlaces}
          unmarked={commentPanel.unmarked}
          threadIds={commentPanel.threadIds} tableRowIds={commentPanel.tableRowIds}
          organizationId={organizationId}
          session={session}
          canEditPage={!viewOnly}
          canComment={canComment}
          onClose={() => setCommentPanel(null)}
          onStatus={onStatus}
        />
      ) : null}
      {session && appPanel && !commentPanel
        ? <AppSidePanel key={`${page.id}:${appPanel}`} session={session} appId={appPanel} pageId={page.id} organizationId={organizationId}
          docked={sidePanelDocked} onClose={() => setAppPanel(null)} /> : null}
      {session && pages && updatesPanel && !commentPanel?.all && !appPanel ? (
        <PageUpdatesSidePanel key={page.id} page={page} pages={pages} session={session} canEdit={!viewOnly}
          database={inlineDatabasePage ? { blockId: inlineDatabasePage.blockId, title: windowTitle, href: inlineDatabasePage.href,
            sourceId: inlineDatabasePage.sourceId, titleLabel: inlineDatabasePage.titleLabel } : undefined}
          onClose={() => setUpdatesPanel(false)} onPageChange={onPageChange} onStatus={onStatus} />
      ) : null}
      {session && pageDiscussionShown ? (
        <CommentsPanel
          key={`page-discussion:${page.id}`}
          inline sideOnly={discussionPlace.sideOnly}
          inlineHostId={commentsHostId}
          autoFocus={composingPageComment}
          activeCommentId={pageDiscussionTarget}
          activeQuiet={pageDiscussionQuiet}
          pageId={page.id}
          workspaceId={page.workspaceId}
          blockId={null}
          organizationId={organizationId}
          session={session}
          canEditPage={!viewOnly}
          canComment={canComment}
          onClose={() => setComposingPageComment(false)}
          onStatus={onStatus}
        />
      ) : null}
    </PageBodyLead.Provider>
    </PageCustomizationContext.Provider>
  );
}

/**
 * Names the window after the page: a title being typed in its field as it is
 * typed, which draws this alone and not the page (lib/pageNames.ts).
 */
function PageWindowChrome({ pageId, typedFor, title, titleSpans, untitled, iconHref, plain }: {
  pageId: string;
  /** The page whose typed title names the window, none where the window is named otherwise (an inline database opened alone). */
  typedFor: string | null;
  title: string;
  titleSpans?: TextSpan[];
  untitled: string;
  iconHref: string;
  plain: boolean;
}) {
  const shown = useNamedPage(typedFor ? { id: typedFor, title } : null)?.title ?? title;
  useEffect(() => {
    setDocumentChrome({ title: shown || untitled, titleSpans, iconHref, plain, pageId });
  }, [iconHref, pageId, plain, shown, titleSpans, untitled]);
  return null;
}
