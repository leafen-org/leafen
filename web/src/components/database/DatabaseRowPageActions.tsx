import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { CommentIcon, DotsHorizontal, DoubleChevronRight } from "@/icons/leafen";
import { shortcutCompactLabel } from "@/lib/keyboardShortcuts";
import { usePageDiscussionsAsked, type OpenCommentsDetail } from "@/components/comments/commentPanelEvents";
import { PageDiscussionsPopover } from "@/components/comments/PageDiscussionsPopover";
import { PagePresence } from "@/components/page/PagePresence";
import { openWholeWith } from "@/components/page/usePageSidePanel";
import { barCommentsLook, barCommentsPress, barDockedPanel, barEditedTimeShown } from "@/components/page/barCommentsButton";
import type { usePageCommentSummary } from "@/components/page/usePageCommentSummary";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { opensPageUpdates } from "@/lib/pageAnalyticsPanelService";
import { barOffersShare, useWorkspaceGuest } from "@/lib/pageReaderChrome";
import type { Page } from "@/lib/types";
import { usePageReach } from "@/lib/usePageOnlyReader";
import type { Session } from "@/types";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { SuggestingPill } from "../page/SuggestingPill";
import { TopBarEditedTime } from "../page/TopBarEditedTime";
import { TopBarOffline } from "../page/TopBarOffline";
import { shareTip, TopBarShareFace } from "../page/TopBarShareFace";
import { styles as topBarStyles } from "../page/topBarStyles";
import type { RowPageMenuExtras } from "./DatabaseRowMenu";
import sheet from "./databaseSheet";
import type { RowPageDetails } from "./RowPageLayoutBody";
import { onRowPageShareRequest } from "./rowPageShareRequest";
import { RowPageShareMenu } from "./RowPageShareMenu";
import {
  RowPageCopyLink, RowPageDetailsButton, RowPageFavorite, type RowPageTopBarActions,
} from "./RowPageTopBarItems";
import type { useRowPageSuggesting } from "./useRowPageSuggesting";

const styles: Record<string, string> = sheet;

/**
 * A database page's top bar actions, as Notion's (share, link, comments,
 * favorite, actions): opened whole, "오프라인" first while the connection is
 * lost, as a page's bar (TopBar) and Notion's row page at its address and
 * opened whole (seen 2026-11-18, 1440 dark: 28 before its edit time) — a peek
 * leaves it to the bar of the page it opens over; its suggesting pill and
 * last edit, who is here, "공유" with its menu, the link, the page's
 * discussions once it has any, the details toggle, the favorite and "…". The share and discussions it opens
 * are its own; a phone's "작업" sheet and an "@" invite open its share too.
 * Beside its docked updates it keeps share, favorite and more, with no link
 * nor edit time, and "패널 닫기" at the panel's start, as a page's bar
 * (TopBar) and Notion's row page (seen 2026-11-16, 1440 dark).
 */
export function DatabaseRowPageActions({
  snapshot, title, mode, phone, session, valueRow, topBar, suggesting, canEditPage, onOpenComments, comments, details,
  onOpenActions, menuExtras, openWhole,
}: {
  snapshot: DatabaseRowPageSnapshot;
  title: string;
  mode: "side" | "center" | "full";
  phone: boolean;
  session?: Session;
  valueRow: Page;
  topBar?: RowPageTopBarActions;
  suggesting: ReturnType<typeof useRowPageSuggesting>;
  canEditPage: boolean;
  onOpenComments?: (detail?: OpenCommentsDetail) => void;
  comments: ReturnType<typeof usePageCommentSummary>;
  details: RowPageDetails;
  onOpenActions?: (trigger: HTMLElement, extras: RowPageMenuExtras) => void;
  /** What the "…" menu offers beside the page's own rows, read as it opens. */
  menuExtras: () => RowPageMenuExtras;
  /** A peek's ⤢: its "토론" list's "모두 보기" opens the page whole with "모든 토론" docked beside it (openWholeWith). */
  openWhole?: () => void;
}) {
  const { t } = useTranslation(["topBar"]);
  // A touch screen's page bar shows share's mark alone, as a phone's and Notion's iPad (seen 2026-11-13 and 2026-11-16).
  const { touch } = useSidebarLayout(true);
  const [shareOpen, setShareOpen] = useState(false), [shareInvite, setShareInvite] = useState(""), shareRef = useRef<HTMLButtonElement>(null);
  const reach = usePageReach(session, snapshot.row.id, shareOpen ? 1 : 0);
  const onlyReader = reach === undefined ? undefined : reach === "private";
  // A phone's "작업" sheet opens the page's share from its "공유", and an "@" invite in its text or properties with the name typed.
  useEffect(() => onRowPageShareRequest((rowId, invite) => { if (rowId !== snapshot.row.id) return false; setShareInvite(invite ?? ""); setShareOpen(true); return true; }), [snapshot.row.id]);
  // The bar's "댓글" opens the row page's discussions, as the page top bar's does.
  const [discussionsOpen, setDiscussionsOpen] = useState(false);
  usePageDiscussionsAsked(snapshot.row.id, Boolean(onOpenComments && comments.any), () => setDiscussionsOpen(true));
  const commentTriggerRef = useRef<HTMLButtonElement>(null);
  // Its comments button and edited time beside a docked panel, as a page's bar's (barCommentsButton.ts).
  const docked = barDockedPanel(topBar?.onClosePanel, topBar?.discussionsDocked), commentsLook = barCommentsLook(docked, discussionsOpen);
  // A guest who may not edit the page has no share nor link, opened whole or peeked, as a page's bar (barOffersShare).
  const shares = barOffersShare(canEditPage, useWorkspaceGuest(snapshot.workspaceId));
  return (
    <div className={styles.rowPeekActions} data-row-peek-actions>
      <TopBarOffline active={mode === "full" && !phone} />
      {suggesting.active ? <SuggestingPill compact={mode !== "full"} onStop={() => suggesting.control?.onChange(false)} /> : null}
      {topBar && mode === "full" && !suggesting.active && barEditedTimeShown(docked)
        ? <TopBarEditedTime updatedAt={snapshot.row.updatedAt} onOpenUpdates={opensPageUpdates(canEditPage) ? topBar.onOpenUpdates : undefined}
          activity={session ? { session, page: { ...snapshot.row, workspaceId: snapshot.workspaceId } } : undefined} data-row-peek-edited-time /> : null}
      {session && canEditPage ? <PagePresence pageId={snapshot.row.id} containerPageId={snapshot.row.containerPageId} session={session} /> : null}
      {session && shares ? (
        <span style={{ position: "relative", display: "inline-flex" }}>
          {/* The top bar's "공유", as Notion's peek shows it (seen 2026-10-28). */}
          <button ref={shareRef} type="button" className={`${topBarStyles.action} ${topBarStyles.shareAction}`} data-row-peek-share aria-haspopup="dialog"
            aria-label={t("topBar:sharePage", { pageTitle: title })} {...shareTip(reach)}
            data-private={onlyReader === true ? "true" : undefined} aria-expanded={shareOpen} onClick={() => setShareOpen((open) => !open)}>
            <TopBarShareFace reach={reach} phone={phone || (touch && mode === "full")} />
          </button>
          {shareOpen ? (
            <RowPageShareMenu anchorRef={shareRef} containerPageId={snapshot.row.containerPageId} session={session} page={{ ...valueRow, id: snapshot.row.id, workspaceId: snapshot.workspaceId, title }} invite={shareInvite} onClose={() => { setShareOpen(false); setShareInvite(""); }} />
          ) : null}
        </span>
      ) : null}
      {topBar?.onCopyLink && !topBar.onClosePanel && shares ? <RowPageCopyLink onCopyLink={topBar.onCopyLink} session={session} pageId={snapshot.row.id} onOpenShare={() => setShareOpen(true)} /> : null}
      {/* As Notion's, the bar offers the page's comments once it has any. */}
      {onOpenComments && comments.any ? (
        <button ref={commentTriggerRef} type="button" className={`${styles.rowPeekIconAction} ${topBarStyles.panelButton}`} data-row-peek-comments
          aria-label={t("topBar:comment")} title={t(commentsLook.tip)} aria-haspopup="dialog" aria-expanded={discussionsOpen} data-panel-open={commentsLook.open || undefined}
          // Over its docked "모든 토론" it closes them, over its docked updates it turns them into "모든 토론" at once (barCommentsPress).
          onClick={() => barCommentsPress(docked, {
            closeDocked: topBar?.onClosePanel, openAll: () => { topBar?.onClosePanel?.(); onOpenComments({ pageId: snapshot.row.id, all: true }); },
            toggleList: () => setDiscussionsOpen((current) => !current),
          })}>
          <CommentIcon size="regular" />
        </button>
      ) : null}
      {discussionsOpen && onOpenComments && session ? (
        <PageDiscussionsPopover anchor={commentTriggerRef} pageId={snapshot.row.id} session={session}
          discussions={comments.discussions} latestReplies={comments.latestReplies} replies={comments.replies} actorProfiles={comments.actorProfiles}
          onOpenAll={() => openWhole ? openWholeWith(snapshot.row.id, "discussions", openWhole) : onOpenComments({ pageId: snapshot.row.id, all: true })}
          onClose={() => setDiscussionsOpen(false)} />
      ) : null}
      {details.panel && !phone ? <RowPageDetailsButton details={details} /> : null}
      {topBar?.onFavorite ? (
        <RowPageFavorite favorite={snapshot.row.favorite} pageTitle={title} onFavorite={topBar.onFavorite} />
      ) : null}
      {onOpenActions ? (
        <button type="button" className={styles.rowPeekIconAction} data-row-page-actions aria-haspopup="menu" data-focus-return={`page-menu:${snapshot.row.id}`}
          aria-label={t("topBar:moreActions", { pageTitle: title })} title={t("topBar:pageActionsTip")} data-tip-wait="pageMenu"
          onClick={(event) => onOpenActions(event.currentTarget, menuExtras())}>
          <DotsHorizontal size="regular" aria-hidden="true" />
        </button>
      ) : null}
      {topBar?.onClosePanel ? (
        <button type="button" className={`${topBarStyles.closePanel} ${styles.rowPeekClosePanel}`} onClick={topBar.onClosePanel} data-topbar-close-panel
          aria-label={t("topBar:closePanel")} title={t("topBar:closePanel")} data-tip-keys={shortcutCompactLabel("closeSidePanel")}>
          <DoubleChevronRight size="regular" aria-hidden="true" />
        </button>
      ) : null}
    </div>
  );
}
