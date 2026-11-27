import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { RowMenu } from "@/components/RowMenu";
import { copyPageLink } from "@/components/editor/pageLinkClipboard";
import { useInlineDatabaseAlone } from "@/components/editor/inlineDatabaseAlone";
import { TopBarEditedTime } from "@/components/page/TopBarEditedTime";
import { useAloneDatabaseFavorite } from "@/components/page/aloneDatabaseFavorite";
import { shareTip, TopBarShareFace } from "@/components/page/TopBarShareFace";
import { styles } from "@/components/page/topBarStyles";
import { useProductPages } from "@/components/product/productPagesContext";
import { useSidebarLayout } from "@/components/shell/AppShell";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { DotsHorizontal, LinkIcon, Star, StarFilled } from "@/icons/leafen";
import { shortcutCompactLabel } from "@/lib/keyboardShortcuts";
import { pageLinkToCopy } from "@/lib/databasePageLink";
import { useProductToast } from "@/lib/productToast";
import { usePageLinkCopyNotice } from "@/lib/usePageLinkCopyNotice";
import { usePageReach } from "@/lib/usePageOnlyReader";
import type { Session } from "@/types";
import { rowPathPage } from "./RowPagePath";
import { RowPageShareMenu } from "./RowPageShareMenu";

/**
 * The right of the bar over one of a database page's own databases opened
 * alone (RowPageDatabaseAlone), as Notion's (seen 2026-11-15, Chrome, the
 * database in a row's body opened as a full page: "지금 편집 · 공유 · 링크 ·
 * ☆ · ⋯") and as a page's inline database opened alone shows it (TopBar):
 * when the page was edited (TopBarEditedTime); the page's share; the
 * link to the database; ☆, which makes the database a favorite, as Notion's
 * did (it then stood among the favorites with its table icon); and "⋯", the
 * page's menu working on the database (useRowMenuDatabaseBlock), as Notion's
 * "데이터베이스 잠금 · 복제 · 옮기기 · CSV와 병합 · 휴지통으로 이동". Without
 * these the database alone had only its path.
 */
export function RowPageDatabaseAloneActions({ blockId, readOnly, rowTitle, session, snapshot, title }: {
  blockId: string;
  readOnly: boolean;
  rowTitle: string;
  session: Session;
  snapshot: DatabaseRowPageSnapshot;
  /** The database's name. */
  title: string;
}) {
  const { t } = useTranslation("topBar");
  const { notify } = useProductToast();
  const { mobile, touch } = useSidebarLayout(false);
  const productPages = useProductPages();
  const alone = useInlineDatabaseAlone(blockId);
  const row = snapshot.row;
  const page = rowPathPage(row, rowTitle, snapshot.workspaceId);
  const [menuOpen, setMenuOpen] = useState<false | "menu" | "updates">(false);
  const [shareOpen, setShareOpen] = useState(false);
  const shareRef = useRef<HTMLButtonElement>(null), menuRef = useRef<HTMLButtonElement>(null);
  const reach = usePageReach(session, row.id, shareOpen ? 1 : 0);
  const notifyLinkCopied = usePageLinkCopyNotice(session, row.id, () => setShareOpen(true));
  const { favorite, change: setFavorite } = useAloneDatabaseFavorite(blockId, { id: row.id, workspaceId: snapshot.workspaceId }, title)
    ?? { favorite: false, change: undefined };
  const changeFavorite = (next: boolean) => {
    if (!setFavorite) return;
    setMenuOpen(false);
    void setFavorite(next).catch(() => notify(t("toast.couldntUpdateFavorites"), "error"));
  };
  const copyLink = async () => {
    setMenuOpen(false);
    setShareOpen(false);
    // The database's own address, the view shown with it (pageLinkToCopy).
    const url = pageLinkToCopy({ id: blockId, kind: "database" }, session.user.id);
    if (await copyPageLink(url, blockId, title)) notifyLinkCopied();
    else notify(t("toast.couldntCopyLink"), "error");
  };
  const exportDatabase = alone?.views ? { id: blockId, title, linked: true } : alone ? { id: alone.dataSourceId, title } : undefined;
  return (
    <div className={styles.right} data-mobile={mobile ? "true" : undefined} data-topbar-action-rail data-row-database-alone-actions>
      <TopBarEditedTime updatedAt={row.updatedAt} data-topbar-edited-time
        onOpenUpdates={() => { setShareOpen(false); setMenuOpen("updates"); }} />
      <span style={{ position: "relative", display: "inline-flex" }}>
        <button ref={shareRef} type="button" className={`${styles.action} ${styles.shareAction}`} data-topbar-share-action
          data-private={reach === "private" ? "true" : undefined} aria-label={t("sharePage", { pageTitle: rowTitle })} {...shareTip(reach)}
          aria-haspopup="dialog" aria-expanded={shareOpen} onClick={() => { setMenuOpen(false); setShareOpen((open) => !open); }}>
          <TopBarShareFace reach={reach} phone={touch} />
        </button>
        {shareOpen ? (
          <RowPageShareMenu anchorRef={shareRef} containerPageId={row.containerPageId} session={session}
            page={{ id: row.id, workspaceId: snapshot.workspaceId, title: rowTitle }} onClose={() => setShareOpen(false)} />
        ) : null}
      </span>
      <button type="button" className={`${styles.iconAction} ${styles.linkAction}`} data-topbar-copy-link-action
        aria-label={t("copyLinkTip")} title={t("copyLinkTip")} data-tip-keys={shortcutCompactLabel("copyPageLink")}
        onClick={() => void copyLink()}>
        <LinkIcon size="regular" aria-hidden="true" />
      </button>
      <button type="button" className={styles.iconAction} data-topbar-favorite-action data-favorite={favorite ? "true" : undefined}
        aria-label={t(favorite ? "removeFavorite" : "addFavorite", { pageTitle: title })}
        title={t(favorite ? "removeFavoriteTip" : "addFavoriteTip")} aria-pressed={favorite} disabled={!setFavorite}
        onClick={() => changeFavorite(!favorite)}>
        {favorite ? <StarFilled size="regular" aria-hidden="true" /> : <Star size="regular" aria-hidden="true" />}
      </button>
      <button ref={menuRef} type="button" className={styles.iconAction} data-page-menu-trigger data-focus-return={`page-menu:${row.id}`}
        aria-label={t("moreActions", { pageTitle: title })} title={t("pageActionsTip")} data-tip-wait="pageMenu" aria-haspopup="menu"
        aria-expanded={menuOpen === "menu"} onClick={() => { setShareOpen(false); setMenuOpen((open) => open === "menu" ? false : "menu"); }}>
        <DotsHorizontal size="wide" aria-hidden="true" />
      </button>
      {menuOpen ? (
        <RowMenu key={menuOpen} opener={menuRef} page={page} pages={productPages?.pages ?? [page]} session={session}
          initialPanel={menuOpen === "updates" ? "updates" : undefined} canEdit={!readOnly} databasePage
          inlineDatabase={blockId} exportDatabase={exportDatabase} favorite={favorite} onFavoriteChange={setFavorite ? async (next) => changeFavorite(next) : undefined}
          onShare={() => { setMenuOpen(false); setShareOpen(true); }} onClose={() => { setMenuOpen(false); menuRef.current?.focus(); }} />
      ) : null}
    </div>
  );
}
