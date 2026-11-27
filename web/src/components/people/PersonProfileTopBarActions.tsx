import { useEffect, useRef, useState, type CSSProperties, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import rowMenuStyles from "@/components/RowMenu.module.css";
import { TopBarEditedTime } from "@/components/page/TopBarEditedTime";
import { shareTip, TopBarShareFace } from "@/components/page/TopBarShareFace";
import { styles } from "@/components/page/topBarStyles";
import { RowPageShareMenu } from "@/components/database/RowPageShareMenu";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { readDatabaseRowPage, setDatabaseRowPageFavorite } from "@/databaseRowPageApi";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { DotsHorizontal, LinkIcon, Star, StarFilled } from "@/icons/leafen";
import { copyText } from "@/lib/copyText";
import { shortcutCompactLabel } from "@/lib/keyboardShortcuts";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { absolutePageUrl } from "@/lib/navigation";
import { placeAround, popoverBackdropStyle, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { useProductToast } from "@/lib/productToast";
import { usePageLinkCopyNotice } from "@/lib/usePageLinkCopyNotice";
import { usePageReach } from "@/lib/usePageOnlyReader";
import type { Session } from "@/types";

/**
 * A profile's bar is its person's page's, as Notion's person page is a
 * People row's page (seen 2026-11-16, a free workspace's owner on a member's
 * page at 1440, twice): when it was last edited, "공유" with the page's share
 * menu, its link, ☆ and "⋯" — whose menu is "링크 복사 ⌘⌥L" alone here
 * ("Mac 앱에서 열기" is the open-in-app band's, which Leafen leaves out) —
 * all of the person's People row (rowPageId): its link is that page's own
 * address, which opens as the profile, and ☆ makes it a favourite, which
 * the sidebar and the library show by the person's photo. A reader the row
 * isn't read for sees none of them. A restricted member, who opens their own
 * page alone, has no "공유" and no link there, as Notion's restricted member
 * reads "N시간 전 편집 · ☆ · ⋯" (seen 2026-11-16 in Chrome at 1440, in two tabs
 * at two times).
 */
export function PersonProfileTopBarActions({ session, rowPageId, title, restricted = false }: {
  session: Session; rowPageId: string; title: string; restricted?: boolean;
}) {
  const { t } = useTranslation(["topBar", "rowMenu"]);
  const { notify } = useProductToast();
  const { phone } = useSidebarLayout(false);
  const [snapshot, setSnapshot] = useState<DatabaseRowPageSnapshot | null>(null);
  // The ☆ shows the press at once; the server is asked after (and turns it back if it refuses).
  const [favorite, setFavorite] = useState<boolean | null>(null);
  const [shareOpen, setShareOpen] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const shareRef = useRef<HTMLButtonElement>(null), menuRef = useRef<HTMLButtonElement>(null);
  const reach = usePageReach(session, rowPageId, shareOpen ? 1 : 0);
  const notifyLinkCopied = usePageLinkCopyNotice(session, rowPageId, () => setShareOpen(true));
  useEffect(() => {
    const controller = new AbortController();
    readDatabaseRowPage(session, rowPageId, { limit: 1, signal: controller.signal })
      .then((read) => { setSnapshot(read); setFavorite(null); }, () => {});
    return () => controller.abort();
  }, [rowPageId, session]);
  if (!snapshot) return null;
  const shown = favorite ?? snapshot.row.favorite;

  async function copyLink() {
    setMenuOpen(false);
    if (await copyText(absolutePageUrl(rowPageId))) notifyLinkCopied();
    else notify(t("topBar:toast.couldntCopyLink"), "error");
  }
  async function changeFavorite(next: boolean) {
    if (!snapshot) return;
    setFavorite(next);
    try {
      await setDatabaseRowPageFavorite(session, rowPageId, { favorite: next, expectedAuthorityRevision: snapshot.row.authorityRevision });
      setSnapshot(await readDatabaseRowPage(session, rowPageId, { limit: 1 }));
      setFavorite(null);
    } catch {
      setFavorite(null);
      notify(t("topBar:toast.couldntUpdateFavorites"), "error");
    }
  }

  return (
    <>
      <TopBarEditedTime updatedAt={snapshot.row.updatedAt} data-person-profile-edited-time />
      {restricted ? null : <><span style={{ position: "relative", display: "inline-flex" }}>
        <button ref={shareRef} type="button" className={`${styles.action} ${styles.shareAction}`} data-person-profile-share
          aria-haspopup="dialog" aria-label={t("topBar:sharePage", { pageTitle: title })} {...shareTip(reach)}
          data-private={reach === "private" ? "true" : undefined} aria-expanded={shareOpen}
          onClick={() => { setMenuOpen(false); setShareOpen((open) => !open); }}>
          <TopBarShareFace reach={reach} phone={phone} />
        </button>
        {shareOpen ? (
          <RowPageShareMenu anchorRef={shareRef} containerPageId={snapshot.row.containerPageId} session={session}
            page={{ id: rowPageId, workspaceId: snapshot.workspaceId, title, kind: "page" }} onClose={() => setShareOpen(false)} />
        ) : null}
      </span>
      <button type="button" className={`${styles.iconAction} ${styles.linkAction}`} data-person-profile-copy-link
        aria-label={t("topBar:copyLinkTip")} title={t("topBar:copyLinkTip")} data-tip-keys={shortcutCompactLabel("copyPageLink")}
        onClick={() => void copyLink()}>
        <LinkIcon size="regular" aria-hidden="true" />
      </button></>}
      <button type="button" className={styles.iconAction} data-person-profile-favorite data-favorite={shown ? "true" : undefined}
        aria-label={t(shown ? "topBar:removeFavorite" : "topBar:addFavorite", { pageTitle: title })}
        title={t(shown ? "topBar:removeFavoriteTip" : "topBar:addFavoriteTip")} aria-pressed={shown}
        onClick={() => void changeFavorite(!shown)}>
        {shown ? <StarFilled size="regular" aria-hidden="true" /> : <Star size="regular" aria-hidden="true" />}
      </button>
      <button ref={menuRef} type="button" className={styles.iconAction} data-person-profile-menu
        aria-label={t("topBar:moreActions", { pageTitle: title })} title={t("topBar:pageActionsTip")} data-tip-wait="pageMenu"
        aria-haspopup="menu" aria-expanded={menuOpen} onClick={() => { setShareOpen(false); setMenuOpen((open) => !open); }}>
        <DotsHorizontal size="wide" aria-hidden="true" />
      </button>
      {menuOpen ? <ProfileMenu anchor={menuRef} title={title} onCopyLink={() => void copyLink()} onClose={() => setMenuOpen(false)} /> : null}
    </>
  );
}

/** The "⋯" menu under its button, its end at the button's end, as Notion's. */
function ProfileMenu({ anchor, title, onCopyLink, onClose }: {
  anchor: RefObject<HTMLButtonElement | null>; title: string; onCopyLink(): void; onClose(): void;
}) {
  const { t } = useTranslation(["topBar", "rowMenu"]);
  const ref = useRef<HTMLDivElement>(null);
  const rect = popoverRectOf(anchor.current);
  // As the page's "…" menu hangs (RowMenuPlaced): at the bar's foot, 256 wide as Notion's (seen
  // 2026-11-16 at 1440: 1172–1428 from 44, its two rows in that width).
  const place = usePopoverPlacement(ref, rect ? (size, viewport) => placeAround(rect, size, viewport, { gap: 8, align: "end", keepSide: true }) : null,
    [anchor], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose });
  return createPortal(
    <>
      <button type="button" className={rowMenuStyles.backdrop} style={popoverBackdropStyle(place)} tabIndex={-1}
        aria-label={t("rowMenu:aria.closePageActions")} onClick={onClose} />
      <div ref={ref} className={`${rowMenuStyles.menu} ${rowMenuStyles.hangingMenu}`} data-person-profile-actions-menu
        style={{ width: 256, ...(place ? popoverPlacementStyle(place) : { visibility: "hidden" }) } as CSSProperties}>
        <NativeActionMenu aria-label={t("topBar:moreActions", { pageTitle: title })} autoFocus>
          <NativeActionMenuItem type="button" className={rowMenuStyles.item} data-menu-item role="menuitem" onClick={onCopyLink}>
            <LinkIcon size="regular" aria-hidden="true" />
            <span>{t("rowMenu:actions.copyLink")}</span>
            <span className={rowMenuStyles.itemHint}>{shortcutCompactLabel("copyPageLink")}</span>
          </NativeActionMenuItem>
        </NativeActionMenu>
      </div>
    </>,
    document.body,
  );
}
