import type { ProductStatusSink } from "@/lib/productStatus";
import { pageLookup } from "@/lib/pageArchive";
import { pageTrashRoot } from "@/lib/pageTrash";
import { Suspense, useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react"; import { takeHandedRing } from "@/lib/overlayKinds"; import { focusAsByKeys } from "@/lib/lastPress";
import { lazyScreen } from "@/lib/lazyScreen";
import { useTranslation } from "react-i18next";
import { pushRoute } from "@/lib/router";
import type { PageLockForMe, PageSuggestingControl } from "@/components/RowMenu";
import { useSidebarLayout } from "@/components/shell/AppShell";
import {
  CommentIcon,
  DotsHorizontal,
  DoubleChevronRight,
  LinkIcon,
  Star,
  StarFilled,
} from "@/icons/leafen";
import { copyPageLink as writePageLink } from "@/components/editor/pageLinkClipboard";
import { matchesKeyboardShortcut, shortcutCompactLabel } from "@/lib/keyboardShortcuts";
import { pageLinkToCopy } from "@/lib/databasePageLink";
import type { PagePresenceSnapshot } from "@/lib/pagePresenceView";
import type { NativePageMetadataChange } from "@/lib/nativePageMetadata";
import { useProductToast } from "@/lib/productToast";
import { usePageLinkCopyNotice } from "@/lib/usePageLinkCopyNotice";
import type { RowMenuDocumentStats } from "@/lib/rowMenuDocumentStats";
import type { Page, Session } from "@/types";
import { PagePresence } from "./PagePresence";
import { PageEditedTime } from "./TopBarEditedTime";
import { useAloneDatabaseFavorite } from "./aloneDatabaseFavorite";
import { SuggestingPill } from "./SuggestingPill";
import { TopBarOffline } from "./TopBarOffline";
import type { PagePresentationChange } from "./pagePresentation";
import { movePopoverAnchorOf } from "../movePopoverPlacement";
import { TopBarBreadcrumbs, type TopBarTrailingCrumb } from "./TopBarBreadcrumbs";
import { requestPageRename } from "./pageRenameRequest";
import { TopBarLock } from "./TopBarLock";
import { useOpenedAloneDatabaseLock } from "@/components/database/openedAloneDatabaseLock";
import { inlineDatabaseOpenedAlone } from "@/lib/inlineDatabaseFullPage";
import { TopBarPrivatePlace } from "./TopBarPrivatePlace";
import { TopBarTeamspaceCrumb } from "./TopBarTeamspaceCrumb";
import { TopBarTitleCrumb } from "./TopBarTitle";
import { TitleWithCustomEmoji } from "./TitleWithCustomEmoji";
import { styles } from "./topBarStyles";
import { TopBarSidebarToggle } from "./TopBarSidebarToggle";
import { TopBarShare } from "./TopBarShare";
import { shareKeyOverMenu, shareTip, TopBarShareFace } from "./TopBarShareFace";
import { TopBarEditRequest } from "./TopBarEditRequest";
import { OPEN_PAGE_SHARE_EVENT, takePendingPageShare, type OpenPageShareDetail } from "./pageShareEvents";
import { usePageCommentSummary } from "./usePageCommentSummary";
import { barCommentsLook, barCommentsPress, barDockedPanel, barEditedTimeShown } from "./barCommentsButton";
import { TopBarPageMenu, usePageMenuWarm, usePrintOpensExport } from "./TopBarPageMenu";
import { PageDiscussionsPopover } from "../comments/PageDiscussionsPopover";
import { usePageDiscussionsAsked } from "../comments/commentPanelEvents";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import { untitledPageDisplayTitle, usePageNames } from "@/lib/pageNames";
import { useProductPages } from "@/components/product/productPagesContext";
import { usePageBusy } from "@/lib/pageBusy";
import { usePageCopyProgress } from "@/lib/pageCopyProgress";
import { PageCopyPercent } from "./PageCopying";
import { useInlineDatabaseAlone } from "../editor/inlineDatabaseAlone";
import { useInlineDatabaseShownIn } from "@/lib/inlineDatabaseMovedShown";
import { InlineDatabaseMoveDialog } from "../useRowMenuDatabaseBlock";
import { usePageReach } from "@/lib/usePageOnlyReader";
import { pageOpened } from "@/lib/typingFade";
import type { TextSpan } from "@/lib/types";

// The content width below which long access actions go into the page menu.
const ACCESS_ACTIONS_MIN_BAR_WIDTH = 880;

const MoveToDialog = lazyScreen(() => import("@/components/MoveToDialog").then(({ MoveToDialog: Dialog }) => ({ default: Dialog })), { typedOnOpen: true });

export function TopBar({
  title,
  titleSpans,
  standalone = false,
  page,
  named,
  pages,
  presence, session,
  onOpenComments, onPageChange,
  favorite: pageFavorite = false,
  onFavoriteChange: onPageFavoriteChange,
  onPresentationChange,
  onPageMetadataChange,
  onDuplicatePage,
  databasePage,
  exportDatabase,
  inlineDatabase,
  trailingCrumb,
  crumbs,
  screenActions,
  onTrashChange,
  onImport,
  readDocumentStats,
  lockForMe,
  canEdit = true,
  accessPending = false,
  suggesting,
  onStatus,
  onClosePanel,
  commentsPanelOpen = false,
  onOpenUpdatesPanel,
  onOpenAppPanel,
}: {
  title: string;
  /** Its spans, which say its mentions as a plain title does (closedMentions.ts). */
  titleSpans?: readonly TextSpan[];
  standalone?: boolean;
  page?: Page;
  /** What names the page while it has no title where its bar is not given it (a public read): its kind and sources. */
  named?: Parameters<typeof untitledPageDisplayTitle>[0];
  pages?: Page[]; presence?: PagePresenceSnapshot; session?: Session;
  onOpenComments?: () => void;
  onPageChange?: (page: Page) => void;
  favorite?: boolean;
  onFavoriteChange?: (favorite: boolean) => Promise<void>;
  onPresentationChange?: (
    change: PagePresentationChange,
    basePage?: Page,
  ) => boolean | Page | void | Promise<boolean | Page | void>;
  onPageMetadataChange?: (
    page: Page,
    change: NativePageMetadataChange,
    signal: AbortSignal,
  ) => Promise<boolean | Page | void>;
  onDuplicatePage?: (page: Page, signal: AbortSignal) => Promise<Page>;
  /** A database's page; "linked" a linked database's, whose lock locks its views. */
  databasePage?: boolean | "linked";
  /** The database a database page exports (RowMenu's). */
  exportDatabase?: { id: string; title: string; linked?: boolean };
  /** The block of an inline database opened alone, which its menu and ⌘⇧P work on (RowMenu's). */
  inlineDatabase?: string;
  /** A piece after the page in its path: an inline database opened alone. */
  trailingCrumb?: TopBarTrailingCrumb;
  /** The path a screen that isn't a page shows in place of its title. */
  crumbs?: ReactNode;
  /** The actions a screen that isn't a page shows on the bar's right, as People's link and "…". */
  screenActions?: ReactNode;
  onTrashChange?: (
    page: Page,
    inTrash: boolean,
    signal: AbortSignal,
  ) => Promise<boolean | Page | void>;
  /** Opens the import screen, which imports into the page. */
  onImport?: (page: Page, mode?: "csvMerge") => void;
  readDocumentStats?: (page: Page) => RowMenuDocumentStats;
  lockForMe?: PageLockForMe;
  /** The reader may change the page; a view-only page offers no changes. */
  canEdit?: boolean;
  /** Asks for edit access only once the page shown said this reader can't edit it. */
  accessPending?: boolean;
  suggesting?: PageSuggestingControl;
  onStatus?: ProductStatusSink;
  /** Closes the panel beside the page, as the bar's "»" over its left edge does. */
  onClosePanel?: () => void;
  /** The panel beside the page is all its comments: the comments button closes it. */
  commentsPanelOpen?: boolean;
  /** Opens the page's updates beside it, from its menu and its edited time. */
  onOpenUpdatesPanel?: () => void;
  onOpenAppPanel?: (appId: string) => void;
}) {
  const { t } = useTranslation(["topBar", "common", "rowMenu"]), pageNames = usePageNames();
  const productPages = useProductPages();
  const pageWorkspace = page ? productPages?.workspaces?.find((workspace) => workspace.id === page.workspaceId) : undefined;
  const appManaged = Boolean(pageWorkspace?.appInstallId);
  // A guest who can't edit the page sees only its place, edit time, ☆ and "…", as Notion's (seen 2026-10-08): no edit request, share or link button.
  const readOnlyGuest = !canEdit && pageWorkspace?.guest === true;
  // Edit access is asked for by the workspace's own members: a guest's bar and
  // window have no such request, nor a restricted member's, as Notion's (seen
  // 2026-11-07: "지금 편집 · 공유 · 링크 · 💬 · ☆ · ⋯", no band in the window).
  const asksEdit = !appManaged && pageWorkspace?.guest !== true && pageWorkspace?.restricted !== true;
  const { notify } = useProductToast();
  const aloneLock = useOpenedAloneDatabaseLock();
  // An untitled database is "새 데이터베이스", a view block's page "제목 없음", as its page names it —
  // a visitor's bar too, as its heading and window (seen 2026-11-18). A title naming a page closed
  // to its reader says so, a visitor's too (closedMentions.ts).
  const pageTitle = titleWithClosedMentions(title, titleSpans) || untitledPageDisplayTitle(page ?? named);
  // A database opened alone is what its ☆ makes a favorite and its link copies, as Notion's (aloneDatabaseFavorite.ts).
  const { favorite, change: onFavoriteChange } = useAloneDatabaseFavorite(inlineDatabase, page, pageTitle) ?? { favorite: pageFavorite, change: onPageFavoriteChange };
  // The … menu, its page updates alone (from the edited time), or its export window (the app's 파일 › 인쇄).
  const [menuOpen, setMenuOpen] = useState<false | "menu" | "updates" | "export">(false);
  const [menuWarm, warmMenu] = usePageMenuWarm(page?.id);
  usePrintOpensExport(Boolean(page && session && !standalone), () => { setShareOpen(false); setMenuOpen("export"); });
  const [moveOpen, setMoveOpen] = useState(false);
  // What opened the move popover, which it hangs under: the private place, else the "…".
  const moveFromRef = useRef<HTMLElement | null>(null);
  const databaseAlone = useInlineDatabaseAlone(inlineDatabase);
  // An inline database opened alone, moving, has the path of the page it goes to at the press.
  const movingTo = useInlineDatabaseShownIn(inlineDatabase);
  const pathPage = (movingTo && pages?.find(({ id }) => id === movingTo)) || page;
  // Another page opens with its bar showing, except one reached by going back or forward (typingFade).
  useEffect(() => { pageOpened(); }, [page?.id]);
  const [shareOpen, setShareOpen] = useState(false);
  // Who else opens the page: its share's face, and its private place.
  const reach = usePageReach(session, page?.id, shareOpen ? 1 : 0);
  const onlyReader = reach === undefined ? undefined : reach === "private";
  // A name the "@" menu asked to invite, ready in the share menu.
  const [shareInvite, setShareInvite] = useState("");
  const [shareTab, setShareTab] = useState<"share" | "publish">("share");
  const [copyState, setCopyState] = useState<"idle" | "pending" | "copied" | "failed">("idle");
  const [favoriteBusy, setFavoriteBusy] = useState(false);
  const menuTriggerRef = useRef<HTMLButtonElement>(null);
  const [discussionsOpen, setDiscussionsOpen] = useState(false);
  const commentTriggerRef = useRef<HTMLButtonElement>(null);
  // A narrow bar leaves its long access actions in the page menu.
  const barRef = useRef<HTMLElement>(null);
  // A page panel beside it, its comments too, whose button stays (Notion's, seen 2026-11-16).
  const docked = barDockedPanel(onClosePanel, commentsPanelOpen), sidePanelOpen = docked !== null, commentsLook = barCommentsLook(docked, discussionsOpen);
  const [actionsRoomy, setActionsRoomy] = useState(true);
  useEffect(() => {
    const bar = barRef.current;
    if (!bar || typeof ResizeObserver === "undefined") return;
    // As the observer saw it: measured again, the page was laid out anew for
    // the styles the page's own observers had just written (some 100ms each
    // time a row's peek opened or closed beside a page holding a 120-row
    // table, seen 2026-11-12).
    const observer = new ResizeObserver(([entry]) => {
      setActionsRoomy(entry.contentRect.width > ACCESS_ACTIONS_MIN_BAR_WIDTH);
    });
    observer.observe(bar);
    return () => observer.disconnect();
  }, []);
  // Cmd/Ctrl+Shift+P with no block selected moves the page; with a row's
  // peek open, or its own page shown, its row (RowPeekAddTo). Not in the trash (as the page view reads it), nor locked
  // for the reader — the page, or the database opened alone — whose menu has no 옮기기, as Notion's (seen 2026-11-14).
  const inTrash = useMemo(() => Boolean(page && pageTrashRoot(page, pageLookup(pages ?? [page]))), [page, pages]);
  const canMove = Boolean(page && session && onPageChange && !inTrash && canEdit) && !(databaseAlone ? aloneLock?.lock.locked && !aloneLock.lock.unlockedForMe : page?.isLocked && !lockForMe?.unlockedForMe);
  useEffect(() => {
    if (!canMove) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || !matchesKeyboardShortcut("movePage", event)) return;
      if (document.querySelector("[data-row-page-mode]")) return;
      event.preventDefault();
      setMenuOpen(false);
      moveFromRef.current = null;
      setMoveOpen(true);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [canMove]);
  const shareTriggerRef = useRef<HTMLButtonElement>(null);
  const copyResetRef = useRef<number | null>(null);
  const shareMenuId = useId();
  const comments = usePageCommentSummary({
    onStatus,
    pageId: page?.id,
    session,
  });
  const { mobile, phone, touch } = useSidebarLayout(standalone);
  // Keep long access actions with the other page actions when the bar is
  // narrow (including a narrow desktop pane).
  const requestEditInMenu = phone || !actionsRoomy;
  // A page an operation holds for now isn't one to ask edit access to (lib/pageBusy.ts).
  const busy = usePageBusy(page?.id);
  const copyPercent = usePageCopyProgress(page?.id);

  useEffect(() => {
    setMenuOpen(false);
    setShareOpen(false);
    setDiscussionsOpen(false);
    setCopyState("idle");
    setFavoriteBusy(false);
  }, [page?.id]);
  usePageDiscussionsAsked(page?.id, Boolean(onOpenComments && comments.any), () => setDiscussionsOpen(true));

  useEffect(() => () => {
    if (copyResetRef.current !== null) window.clearTimeout(copyResetRef.current);
  }, []);

  useEffect(() => {
    function openShare(detail: OpenPageShareDetail) {
      setMenuOpen(false);
      setShareInvite(detail.invite ?? "");
      setShareTab(detail.tab ?? "share");
      setShareOpen(true);
    }
    function onOpenShare(event: Event) {
      const detail = (event as CustomEvent<OpenPageShareDetail>).detail;
      if (!page || detail?.pageId !== page.id) return;
      takePendingPageShare(page.id);
      openShare(detail);
    }
    // Asked for before this page showed.
    const waiting = page ? takePendingPageShare(page.id) : null;
    if (waiting) openShare(waiting);
    window.addEventListener(OPEN_PAGE_SHARE_EVENT, onOpenShare);
    return () => window.removeEventListener(OPEN_PAGE_SHARE_EVENT, onOpenShare);
  }, [page]);

  function closeMenu() { setMenuOpen(false);
    // Ringed, its tip shown, after a window opened from the menu asked so as a press on its shade closed it (ringsAfterPress).
    window.requestAnimationFrame(() => { const trigger = menuTriggerRef.current; if (trigger && takeHandedRing()) focusAsByKeys(trigger, { preventScroll: true }); else trigger?.focus(); });
  }

  function closeShare() {
    setShareOpen(false);
  }

  async function changeLockForMe(unlocked: boolean) {
    if (!lockForMe) return;
    try {
      await lockForMe.change(unlocked);
    } catch {
      notify(t("rowMenu:toast.couldntChangeLockForMe"), "error");
    }
  }

  async function unlockForEveryone() {
    if (!page || !onPageMetadataChange) return;
    try {
      const result = await onPageMetadataChange(page, { isLocked: false }, new AbortController().signal);
      if (result === false) throw new Error("Page lock mutation failed.");
    } catch {
      notify(t("rowMenu:toast.couldntChangePageLock"), "error");
    }
  }

  function closeTransientSurfaces() {
    setMenuOpen(false);
    setShareOpen(false);
  }

  const notifyLinkCopied = usePageLinkCopyNotice(session, page?.id, () => {
    setShareTab("share");
    setShareOpen(true);
  });

  async function copyPageLink() {
    if (!page || copyState === "pending") return;
    closeTransientSurfaces();
    setCopyState("pending");
    const link = pageLinkToCopy(inlineDatabase ? { id: inlineDatabase, kind: "database" } : page, session?.user.id);
    const copied = await writePageLink(link, inlineDatabase ?? page.id, inlineDatabase ? pageTitle : pageNames.title(page));
    const nextState = copied ? "copied" : "failed";
    setCopyState(nextState);
    if (copied) notifyLinkCopied();
    else {
      notify(t("topBar:toast.couldntCopyLink"), "error");
      onStatus?.(t("topBar:toast.couldntCopyLink"), "error");
    }
    if (copyResetRef.current !== null) window.clearTimeout(copyResetRef.current);
    copyResetRef.current = window.setTimeout(() => {
      setCopyState("idle");
      copyResetRef.current = null;
    }, 1_600);
  }

  async function toggleFavorite() {
    if (!onFavoriteChange || favoriteBusy) return;
    closeTransientSurfaces();
    setFavoriteBusy(true);
    try {
      await onFavoriteChange(!favorite);
    } catch {
      notify(t("topBar:toast.couldntUpdateFavorites"), "error");
      onStatus?.(t("topBar:toast.couldntUpdateFavorites"), "error");
    } finally {
      setFavoriteBusy(false);
    }
  }

  return (
    <header ref={barRef} className={styles.topbar} data-phone={phone ? "true" : undefined} data-topbar-bar>
      <TopBarSidebarToggle />
      <div className={styles.left}>
        {pathPage && session && !phone ? (
          <TopBarTeamspaceCrumb page={pathPage} pages={pages ?? [pathPage]} session={session} onOpen={() => {
            setMenuOpen(false);
            setShareOpen(false);
          }} />
        ) : null}
        {page ? (
          <TopBarBreadcrumbs
            currentOnly={phone}
            onRenameCurrent={canEdit && !inTrash && !page.isLocked ? (at) => requestPageRename({ pageId: page.id, at }) : undefined}
            page={pathPage ?? page}
            pages={pages ?? [page]}
            trailing={trailingCrumb}
          />
        ) : crumbs ?? <TopBarTitleCrumb title={pageTitle}>
          {/* A published page's bar draws its title's custom emoji, as a member's path (TitleWithCustomEmoji). */}
          <TitleWithCustomEmoji text={pageTitle} named={{ title, titleSpans }} />
        </TopBarTitleCrumb>}
        {page && canMove && !phone && !page.parentId && !page.teamspaceId && onlyReader === true ? (
          <TopBarPrivatePlace onMove={(from) => {
            setMenuOpen(false);
            setShareOpen(false);
            moveFromRef.current = from;
            setMoveOpen(true);
          }} />
        ) : null}
        {page?.isLocked && session && !standalone && !databaseAlone ? (
          <TopBarLock pageId={page.id} unlockedForMe={Boolean(lockForMe?.unlockedForMe)} lockedBy={page.lockedBy}
            onUnlockForMe={canEdit && lockForMe ? () => void changeLockForMe(true) : undefined}
            onLockAgain={lockForMe ? () => void changeLockForMe(false) : undefined}
            onUnlockForEveryone={canEdit && onPageMetadataChange ? () => void unlockForEveryone() : undefined} />
        ) : aloneLock?.lock.locked && session && !standalone && inlineDatabaseOpenedAlone() === aloneLock.blockId ? (
          // An inline database opened alone shows its own lock here, as Notion's (openedAloneDatabaseLock.ts).
          <TopBarLock unlockedForMe={aloneLock.lock.unlockedForMe} onLockAgain={aloneLock.lock.relock}
            onUnlockForMe={aloneLock.lock.unlockForMe} onUnlockForEveryone={aloneLock.lock.unlockForEveryone} />
        ) : null}
      </div>
      <div className={styles.right} data-mobile={mobile ? "true" : undefined} data-topbar-action-rail>
        <TopBarOffline active={!standalone && !phone} />
        {page ? null : screenActions}
        {/* Beside a page's own panel the bar keeps share, favorite and more:
            no link to copy nor when it was edited, as Notion's beside its
            updates and its discussions (seen 2026-11-07, 2026-11-16). */}
        {!standalone && page && session ? (
          <>
            {suggesting?.active ? <SuggestingPill onStop={() => suggesting.onChange(false)} /> : null}
            {copyPercent !== null ? <PageCopyPercent percent={copyPercent} /> : barEditedTimeShown(docked) && !suggesting?.active ? <PageEditedTime page={page} onOpenUpdates={() => {
              setShareOpen(false);
              if (onOpenUpdatesPanel) onOpenUpdatesPanel();
              else setMenuOpen("updates");
            }} /> : null}
            {/* Who is here after when it was edited: "1시간 전 편집 · 나 · june · 공유", as Notion's (seen 2026-11-15). */}
            {presence && canEdit ? <PagePresence pageId={page.id} database={inlineDatabase} session={session} /> : null}
            {/* An app's own workspace keeps what people may only view; its account can't be asked. */}
            {!canEdit && !accessPending && !inTrash && !busy ? (appManaged
              ? <><span className={styles.appManaged} data-topbar-app-managed data-app-openable={pageWorkspace?.appSlug ? "true" : undefined}>{t("topBar:appManaged")}</span>
                {pageWorkspace?.appSlug ? <button type="button" className={styles.appOpen} data-topbar-app-open
                  onClick={() => pushRoute(`/apps/${pageWorkspace.appSlug}`)}>{t("topBar:openApp")}</button> : null}</>
              : requestEditInMenu || !asksEdit ? null : <TopBarEditRequest page={page} session={session} onStatus={onStatus} />) : null}
            {/* A page in the trash keeps its share, link and "⋯", a full-page database's too, and no ☆ on a desktop, as Notion's (seen 2026-11-13
                and 2026-11-15, two loads each); a wider touch screen keeps its ☆, which makes it a favorite, as Notion's iPad (2026-11-17, 834 and 1210). */}
            {readOnlyGuest ? null : <><button
              ref={shareTriggerRef}
              type="button"
              className={`${styles.action} ${styles.shareAction}`}
              data-topbar-share-action
              data-private={onlyReader === true ? "true" : undefined}
              aria-label={t("topBar:sharePage", { pageTitle })}
              {...shareTip(reach)}
              aria-haspopup="dialog"
              aria-expanded={shareOpen}
              aria-controls={shareOpen ? shareMenuId : undefined}
              onClick={() => {
                if (!shareKeyOverMenu()) setMenuOpen(false); // ⌘⇧O over the "⋯" menu leaves it under the share (TopBarShareFace).
                setShareInvite("");
                setShareTab("share");
                setShareOpen((open) => !open);
              }}
            >
              <TopBarShareFace reach={reach} phone={touch} />
            </button>
            {shareOpen ? (
              <TopBarShare
                key={`${page.id}:${session.user.id}:${shareInvite}:${shareTab}`}
                id={shareMenuId}
                anchorRef={shareTriggerRef}
                initialInvite={shareInvite}
                initialTab={shareTab}
                page={page}
                pages={pages ?? [page]}
                session={session}
                onStatus={onStatus}
                canRequestEdit={asksEdit}
                onClose={closeShare}
                // "게시"'s "복원" for a page in the trash itself, as the page's menu restores it.
                onRestore={page.inTrash && onTrashChange ? () => onTrashChange(page, false, new AbortController().signal).catch(() => false as const).then((restored) => {
                  if (restored === false) onStatus?.(t("rowMenu:toast.couldntRestorePage"), "error");
                  else if (typeof restored === "object") onPageChange?.(restored);
                  return restored !== false;
                }) : undefined}
              />
            ) : null}
            {sidePanelOpen ? null : <button
              type="button"
              className={`${styles.iconAction} ${styles.linkAction}`}
              data-topbar-copy-link-action
              data-copy-state={copyState === "idle" ? undefined : copyState}
              aria-label={copyState === "copied" ? t("topBar:copiedPageLink") : t("topBar:copyLinkTip")}
              title={copyState === "copied" ? t("topBar:copiedPageLink") : t("topBar:copyLinkTip")}
              data-tip-keys={copyState === "copied" ? undefined : shortcutCompactLabel("copyPageLink")}
              aria-busy={copyState === "pending" || undefined}
              disabled={copyState === "pending"}
              onClick={() => void copyPageLink()}
            >
              <LinkIcon size="regular" aria-hidden="true" />
            </button>}</>}
            {/* As Notion's, the bar offers a page's comments once it has any. */}
            {onOpenComments && comments.any ? (
              <button
                ref={commentTriggerRef}
                type="button"
                className={`${styles.action} ${styles.commentAction} ${styles.panelButton}`}
                data-topbar-comment-action data-panel-open={commentsLook.open || undefined}
                aria-haspopup="dialog"
                aria-expanded={discussionsOpen}
                title={t(commentsLook.tip)}
                aria-label={comments.open
                  ? t("topBar:openComments", { count: comments.open, pageTitle })
                  : t("topBar:comment")}
                onClick={() => {
                  setMenuOpen(false); setShareOpen(false);
                  // Over its docked "모든 토론" it closes it ("모든 토론 닫기"); over another docked panel it turns it into "모든 토론".
                  barCommentsPress(docked,
                    { closeDocked: onClosePanel, openAll: onOpenComments, toggleList: () => setDiscussionsOpen((current) => !current) });
                }}
              >
                <CommentIcon size="regular" aria-hidden="true" />
                {comments.unread ? <span className={styles.commentUnread} aria-hidden="true" data-topbar-comment-unread /> : null}
              </button>
            ) : null}
            {discussionsOpen && onOpenComments && page && session ? (
              <PageDiscussionsPopover anchor={commentTriggerRef} pageId={page.id} session={session}
                discussions={comments.discussions} latestReplies={comments.latestReplies} replies={comments.replies} actorProfiles={comments.actorProfiles}
                onOpenAll={onOpenComments} onClose={() => setDiscussionsOpen(false)} onStatus={onStatus} />
            ) : null}
            {inTrash && !(touch && !phone) ? null : <button
              type="button"
              className={styles.iconAction}
              data-topbar-favorite-action
              data-favorite={favorite ? "true" : undefined}
              aria-label={favorite
                ? t("topBar:removeFavorite", { pageTitle })
                : t("topBar:addFavorite", { pageTitle })}
              title={favorite ? t("topBar:removeFavoriteTip") : t("topBar:addFavoriteTip")}
              aria-pressed={favorite}
              aria-busy={favoriteBusy || undefined}
              disabled={!onFavoriteChange || favoriteBusy}
              onClick={() => void toggleFavorite()}
            >
              {favorite ? <StarFilled size="regular" aria-hidden="true" /> : <Star size="regular" aria-hidden="true" />}
            </button>}
            <button
              ref={menuTriggerRef}
              type="button"
              className={styles.iconAction}
              data-page-menu-trigger
              data-focus-return={page?.id ? `page-menu:${page.id}` : undefined}
              aria-label={t("topBar:moreActions", { pageTitle })}
              title={t("topBar:pageActionsTip")} data-tip-wait="pageMenu"
              aria-haspopup="menu"
              aria-expanded={menuOpen === "menu"}
              onPointerEnter={warmMenu}
              onFocus={warmMenu}
              onClick={() => {
                setShareOpen(false);
                setMenuOpen((open) => open === "menu" ? false : "menu");
              }}
            >
              <DotsHorizontal size="wide" aria-hidden="true" />
            </button>
            {/* ⌘⇧P moves an inline database opened alone, not the page holding it. */}
            {moveOpen && databaseAlone && !moveFromRef.current ? <InlineDatabaseMoveDialog alone={databaseAlone}
              anchor={movePopoverAnchorOf(menuTriggerRef.current)} onClose={() => setMoveOpen(false)} />
            : moveOpen && page && session && onPageChange ? (
              <Suspense fallback={null}>
                <MoveToDialog pageId={page.id} pages={pages ?? [page]} session={session}
                  anchor={movePopoverAnchorOf(moveFromRef.current ?? menuTriggerRef.current)} anchorGap={moveFromRef.current ? undefined : 0} onClose={() => setMoveOpen(false)}
                  onMoved={() => setMoveOpen(false)} onPageChange={onPageChange} onStatus={onStatus} />
              </Suspense>
            ) : null}
            <TopBarPageMenu
              open={menuOpen}
              warm={menuWarm && !phone}
              requestEdit={requestEditInMenu && !canEdit && !accessPending && !busy && !inTrash && asksEdit}
              opener={menuTriggerRef}
              page={page}
              databasePage={databasePage}
              exportDatabase={exportDatabase}
              inlineDatabase={inlineDatabase}
              pages={pages ?? [page]}
              session={session}
              onPageChange={onPageChange}
              favorite={favorite}
              onFavoriteChange={onFavoriteChange}
              onPresentationChange={onPresentationChange}
              onPageMetadataChange={onPageMetadataChange}
              onDuplicatePage={onDuplicatePage}
              onTrashChange={onTrashChange}
              onImport={onImport}
              readDocumentStats={readDocumentStats}
              lockForMe={lockForMe}
              canEdit={canEdit}
              suggesting={suggesting}
              onStatus={onStatus}
              onClose={closeMenu}
              onOpenAppPanel={onOpenAppPanel}
              onOpenUpdatesPanel={onOpenUpdatesPanel}
              onShare={!readOnlyGuest ? () => { setMenuOpen(false); setShareInvite(""); setShareTab("share"); setShareOpen(true); } : undefined}
            />
          </>
        ) : null}
      </div>
      {onClosePanel ? (
        <button type="button" className={styles.closePanel} onClick={onClosePanel} data-topbar-close-panel
          aria-label={t("topBar:closePanel")} title={t("topBar:closePanel")} data-tip-keys={shortcutCompactLabel("closeSidePanel")}>
          <DoubleChevronRight size="regular" aria-hidden="true" />
        </button>
      ) : null}
    </header>
  );
}
