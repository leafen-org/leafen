import type { ProductStatusSink } from "@/lib/productStatus";
import { useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { CommentIcon, DotsHorizontal, LinkIcon, Star, StarFilled } from "@/icons/leafen";
import { PageDiscussionsPopover } from "@/components/comments/PageDiscussionsPopover";
import type { PageSuggestingControl } from "@/components/RowMenu";
import { useSidebarLayout } from "@/components/shell/AppShell";
import sheet from "@/components/database/databaseSheet";
import { usePageLinkCopyNotice } from "@/lib/usePageLinkCopyNotice";
import { copyPageLink } from "@/components/editor/pageLinkClipboard";
import type { NativePageMetadataChange } from "@/lib/nativePageMetadata";
import { pageLinkToCopy, readPageLinkToCopy } from "@/lib/databasePageLink";
import { useInlineDatabaseAlone } from "@/components/editor/inlineDatabaseAlone";
import type { PageDuplicationOptions } from "@/lib/pageDuplicationService";
import type { Page, Session } from "@/types";
import type { PagePresentationChange } from "./pagePresentation";
import { TopBarPageMenu, usePageMenuWarm } from "./TopBarPageMenu";
import { openWholeWith, openWholeWithUpdates } from "./usePageSidePanel";
import { barCommentsLook } from "./barCommentsButton";
import { TopBarShare } from "./TopBarShare";
import { shareTip, TopBarShareFace } from "./TopBarShareFace";
import { SuggestingPill } from "./SuggestingPill";
import { shortcutCompactLabel } from "@/lib/keyboardShortcuts";
import { styles as topBarStyles } from "./topBarStyles";
import { usePageReach } from "@/lib/usePageOnlyReader";
import { usePageCommentSummary } from "./usePageCommentSummary";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { usePageNames } from "@/lib/pageNames";
import { useDatabaseFavorite } from "./aloneDatabaseFavorite";

/**
 * The right of a page peek's top bar, as Notion's (seen 2026-10-01): "공유",
 * "링크 복사하기", "댓글" once the page has any, "즐겨찾기" and the page's
 * "…" menu, each as the page's own top bar does them; while the peek
 * suggests edits, its pill before them (seen 2026-11-13). An inline database
 * opened alone in it has the bar it has opened alone as a page (TopBar), as
 * Notion's (seen 2026-11-25 in Chrome, two tabs: "공유 · 링크 복사하기 ·
 * 즐겨찾기 · 작업", its "⋯" the same as on its own page, without the edited
 * time): the page's share, the database's link, the ☆ making the database a
 * favorite, and the "⋯" working on the database (useRowMenuDatabaseBlock),
 * each named by the name the database goes by; no comments, a database
 * having no discussion of its own.
 */
export function PagePeekActions({
  page,
  pages,
  session,
  favorite: pageFavorite,
  canEdit,
  onFavoriteChange: onPageFavoriteChange,
  onPageChange,
  onPresentationChange,
  onPageMetadataChange,
  onDuplicatePage,
  onTrashChange,
  onOpenFullPage,
  onStatus,
  suggesting,
  inlineDatabase,
}: {
  page: Page;
  pages: Page[];
  session: Session;
  favorite: boolean;
  canEdit: boolean;
  onFavoriteChange: (favorite: boolean) => Promise<void>;
  onPageChange: (page: Page) => void;
  onPresentationChange: (change: PagePresentationChange) => void;
  onPageMetadataChange: (page: Page, change: NativePageMetadataChange, signal: AbortSignal) => Promise<boolean | Page | void>;
  onDuplicatePage: (page: Page, signal: AbortSignal, options?: PageDuplicationOptions) => Promise<Page>;
  onTrashChange: (page: Page, inTrash: boolean, signal: AbortSignal) => Promise<boolean | Page | void>;
  /** Opens the page itself, as "모든 토론" and "업데이트와 애널리틱스" do from the peek. */
  onOpenFullPage: () => void;
  onStatus: ProductStatusSink;
  /** The peek's suggesting mode, which its "…" menu turns on, where the reader may suggest edits. */
  suggesting?: PageSuggestingControl;
  /** An inline database of the page opened alone in the peek, by the name it goes by (inlineDatabaseShownName). */
  inlineDatabase?: { blockId: string; title: string };
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["topBar"]);
  const shareId = useId();
  const commentTrigger = useRef<HTMLButtonElement>(null);
  const menuTrigger = useRef<HTMLButtonElement>(null);
  const [shareOpen, setShareOpen] = useState(false), shareRef = useRef<HTMLButtonElement>(null);
  const reach = usePageReach(session, page.id, shareOpen ? 1 : 0);
  const onlyReader = reach === undefined ? undefined : reach === "private";
  // Its comments button as every bar's, a peek docking nothing beside it (barCommentsButton.ts).
  const [discussionsOpen, setDiscussionsOpen] = useState(false), peekComments = barCommentsLook(null, discussionsOpen);
  const [menuOpen, setMenuOpen] = useState(false);
  // Its "…" menu is drawn ahead, hidden, as the page's own bar's (TopBarPageMenu).
  const [menuWarm, warmMenu] = usePageMenuWarm(page.id);
  const { phone } = useSidebarLayout(true);
  const [copied, setCopied] = useState(false);
  const [favoriteBusy, setFavoriteBusy] = useState(false);
  const comments = usePageCommentSummary({ pageId: inlineDatabase ? undefined : page.id, session, onStatus });
  const databaseFavorite = useDatabaseFavorite(inlineDatabase?.blockId, page, inlineDatabase?.title ?? "");
  // What its "⋯" exports and locks: a linked database's or a dashboard's views, else the source it shows (RowPageDatabaseAloneActions).
  const alone = useInlineDatabaseAlone(inlineDatabase?.blockId);
  const pageTitle = inlineDatabase?.title || page.title || untitledPageDisplayTitle(page);
  const favorite = databaseFavorite ? databaseFavorite.favorite : pageFavorite;
  const changeDatabaseFavorite = databaseFavorite?.change;
  const onFavoriteChange = !databaseFavorite ? onPageFavoriteChange : changeDatabaseFavorite
    && ((next: boolean) => changeDatabaseFavorite(next).catch(() => onStatus(t("topBar:toast.couldntUpdateFavorites"), "error")));

  const notifyLinkCopied = usePageLinkCopyNotice(session, page.id, () => setShareOpen(true));

  async function copyLink() {
    // A full-page database's names its view, as Notion's peek does (databasePageLink.ts); a database
    // opened alone copies its own address, with the view the address names, else its own (TopBar).
    const done = inlineDatabase
      ? await copyPageLink(pageLinkToCopy({ id: inlineDatabase.blockId, kind: "database" }, session.user.id), inlineDatabase.blockId, inlineDatabase.title)
      : await copyPageLink(await readPageLinkToCopy(session, page), page.id, pageNames.title(page));
    if (done) {
      setCopied(true);
      notifyLinkCopied();
      window.setTimeout(() => setCopied(false), 1500);
    }
  }

  async function toggleFavorite() {
    if (!onFavoriteChange) return;
    setFavoriteBusy(true);
    try {
      await onFavoriteChange(!favorite);
    } finally {
      setFavoriteBusy(false);
    }
  }

  return (
    <div className={sheet.rowPeekActions} data-page-peek-actions>
      {suggesting?.active ? <SuggestingPill compact onStop={() => suggesting.onChange(false)} /> : null}
      <span style={{ position: "relative", display: "inline-flex" }}>
        {/* The top bar's "공유", as Notion's peek shows it (seen 2026-10-28). */}
        <button ref={shareRef} type="button" className={`${topBarStyles.action} ${topBarStyles.shareAction}`} data-page-peek-share aria-haspopup="dialog"
          {...shareTip(reach)} data-private={onlyReader === true ? "true" : undefined}
          aria-expanded={shareOpen} aria-controls={shareOpen ? shareId : undefined} onClick={() => setShareOpen((open) => !open)}>
          <TopBarShareFace reach={reach} />
        </button>
        {shareOpen ? (
          <TopBarShare id={shareId} anchorRef={shareRef} page={page} pages={pages} session={session} onStatus={onStatus}
            onClose={() => setShareOpen(false)} />
        ) : null}
      </span>
      {/* Each named in the tip the page's own bar gives it, as Notion's peek (seen 2026-11-13; the link
          button "링크 복사하기" / "Copy link" in the page's and a row peek's bar alike, seen 2026-11-19). */}
      <button type="button" className={sheet.rowPeekIconAction} data-page-peek-copy-link
        aria-label={copied ? t("topBar:copiedPageLink") : t("topBar:copyLinkTip")}
        title={copied ? t("topBar:copiedPageLink") : t("topBar:copyLinkTip")}
        data-tip-keys={copied ? undefined : shortcutCompactLabel("copyPageLink")} onClick={() => void copyLink()}>
        <LinkIcon size="regular" aria-hidden="true" />
      </button>
      {comments.any ? (
        <button ref={commentTrigger} type="button" className={`${sheet.rowPeekIconAction} ${topBarStyles.panelButton}`} data-page-peek-comments
          aria-label={t("topBar:comment")} title={t(peekComments.tip)} aria-haspopup="dialog" aria-expanded={discussionsOpen} data-panel-open={peekComments.open || undefined}
          onClick={() => setDiscussionsOpen((open) => !open)}>
          <CommentIcon size="regular" aria-hidden="true" />
        </button>
      ) : null}
      {discussionsOpen ? (
        <PageDiscussionsPopover anchor={commentTrigger} pageId={page.id} session={session}
          discussions={comments.discussions} latestReplies={comments.latestReplies} replies={comments.replies} actorProfiles={comments.actorProfiles}
          // "모두 보기" opens the page whole with "모든 토론" docked beside it, as a database page's peek does (openWholeWith).
          onOpenAll={() => openWholeWith(page.id, "discussions", onOpenFullPage)} onClose={() => setDiscussionsOpen(false)} onStatus={onStatus} />
      ) : null}
      <button type="button" className={sheet.rowPeekIconAction} data-page-peek-favorite aria-pressed={favorite}
        aria-label={favorite ? t("topBar:removeFavorite", { pageTitle }) : t("topBar:addFavorite", { pageTitle })}
        title={favorite ? t("topBar:removeFavoriteTip") : t("topBar:addFavoriteTip")}
        disabled={favoriteBusy || !onFavoriteChange} onClick={() => void toggleFavorite()}>
        {favorite ? <StarFilled size="regular" aria-hidden="true" /> : <Star size="regular" aria-hidden="true" />}
      </button>
      <span style={{ position: "relative", display: "inline-flex" }}>
        <button ref={menuTrigger} type="button" className={sheet.rowPeekIconAction} data-page-peek-menu aria-haspopup="menu"
          aria-expanded={menuOpen} aria-label={t("topBar:moreActions", { pageTitle })} title={t("topBar:pageActionsTip")} data-tip-wait="pageMenu"
          onPointerEnter={warmMenu} onFocus={warmMenu} onClick={() => setMenuOpen((open) => !open)}>
          <DotsHorizontal size="regular" aria-hidden="true" />
        </button>
        <TopBarPageMenu open={menuOpen ? "menu" : false} warm={menuWarm && !phone}
          opener={menuTrigger} page={page} pages={pages} session={session} canEdit={canEdit} favorite={favorite} onFavoriteChange={onFavoriteChange} suggesting={suggesting}
          {...inlineDatabase ? { inlineDatabase: inlineDatabase.blockId, databasePage: alone?.views ? "linked" as const : true,
            exportDatabase: alone?.views ? { id: inlineDatabase.blockId, title: inlineDatabase.title, linked: true } : alone ? { id: alone.dataSourceId, title: inlineDatabase.title } : undefined } : {}}
          onPageChange={onPageChange} onPresentationChange={onPresentationChange} onPageMetadataChange={onPageMetadataChange}
          onDuplicatePage={onDuplicatePage} onTrashChange={onTrashChange} onStatus={onStatus} onClose={() => setMenuOpen(false)}
          // Its "업데이트와 애널리틱스" opens the page whole, its updates beside it (usePageSidePanel).
          onOpenUpdatesPanel={() => openWholeWithUpdates(page.id, onOpenFullPage)} />
      </span>
    </div>
  );
}
