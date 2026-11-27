import { useEffect, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { TopBarEditedTime } from "@/components/page/TopBarEditedTime";
import { TopBarShare } from "@/components/page/TopBarShare";
import { shareTip, TopBarShareFace } from "@/components/page/TopBarShareFace";
import { styles } from "@/components/page/topBarStyles";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { DotsHorizontal, LinkIcon, Star, StarFilled } from "@/icons/leafen";
import { copyText } from "@/lib/copyText";
import { usePageEditedAt } from "@/lib/pageEditedHere";
import { shortcutCompactLabel } from "@/lib/keyboardShortcuts";
import { absolutePageUrl } from "@/lib/navigation";
import type { PeopleDatabase } from "@/lib/peopleDatabase";
import { PEOPLE_DIRECTORY_HREF } from "@/lib/peopleProfiles";
import { useProductToast } from "@/lib/productToast";
import { serverUrl } from "@/lib/serverOrigin";
import { usePageLinkCopyNotice } from "@/lib/usePageLinkCopyNotice";
import { usePageReach } from "@/lib/usePageOnlyReader";
import { PeopleDatabaseMenu } from "./PeopleDatabaseMenu";
import { setPeopleDatabaseFavorite } from "./peopleFavorite";
import type { Session, Workspace } from "@/types";

/**
 * People's bar is its database page's, as Notion's People page is a
 * database page (seen 2026-11-16, a free workspace's owner at 1440, in two
 * tabs at two times): when it was last edited, "공유" with the page's share
 * window — whose general access holds the workspace's owners and everyone in
 * it, both fixed (populatePublicMutationWorkspaceAccess) — its link, ☆ and
 * "⋯" at 1342, 1372 and 1402, as a person's page's bar
 * (PersonProfileTopBarActions). Its link is the page's own address, which
 * opens People; its "⋯" is People's page menu (PeopleDatabaseMenu). A
 * restricted member, who doesn't open People's page itself, has none of
 * them (PeopleView). In its peek beside a page the bar has no edit time, as
 * Notion's (PeoplePeek).
 */
export function PeopleDatabaseTopBarActions({ session, workspace, database, title, editedTime = true }: {
  session: Session; workspace: Workspace; database: PeopleDatabase; title: string; editedTime?: boolean;
}) {
  const { t } = useTranslation(["topBar", "rowMenu"]);
  const { notify } = useProductToast();
  const { phone } = useSidebarLayout(false);
  const shareId = useId();
  const pageId = database.pageId;
  // The ☆ shows the press at once; the server is asked after, and turns it back if it refuses.
  const [favorite, setFavorite] = useState({ on: database.favorite, revision: database.favoriteRevision });
  useEffect(() => { setFavorite({ on: database.favorite, revision: database.favoriteRevision }); }, [database.favorite, database.favoriteRevision]);
  const [shareOpen, setShareOpen] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const shareRef = useRef<HTMLButtonElement>(null), menuRef = useRef<HTMLButtonElement>(null);
  const reach = usePageReach(session, pageId, shareOpen ? 1 : 0);
  const notifyLinkCopied = usePageLinkCopyNotice(session, pageId, () => setShareOpen(true));
  const page = { id: pageId, workspaceId: workspace.id, title, kind: "database" as const };
  // Its last edit as read, or one made here after (usePeopleDatabaseWrites).
  const editedAt = usePageEditedAt({ id: pageId, updatedAt: database.updatedAt });

  async function copyLink() {
    setMenuOpen(false);
    if (await copyText(absolutePageUrl(pageId))) notifyLinkCopied();
    else notify(t("topBar:toast.couldntCopyLink"), "error");
  }
  async function changeFavorite(next: boolean) {
    const before = favorite;
    setFavorite({ on: next, revision: before.revision });
    try {
      const saved = await setPeopleDatabaseFavorite(session, workspace.id, pageId, next, before.revision);
      setFavorite({ on: saved.favorite, revision: saved.revision });
    } catch {
      setFavorite(before);
      notify(t("topBar:toast.couldntUpdateFavorites"), "error");
    }
  }

  return (
    <>
      {editedTime ? <TopBarEditedTime updatedAt={editedAt} data-people-edited-time /> : null}
      <span style={{ position: "relative", display: "inline-flex" }}>
        <button ref={shareRef} type="button" className={`${styles.action} ${styles.shareAction}`} data-people-share
          aria-haspopup="dialog" aria-label={t("topBar:sharePage", { pageTitle: title })} {...shareTip(reach)}
          data-private={reach === "private" ? "true" : undefined} aria-expanded={shareOpen}
          onClick={() => { setMenuOpen(false); setShareOpen((open) => !open); }}>
          <TopBarShareFace reach={reach} phone={phone} />
        </button>
        {shareOpen ? <TopBarShare id={shareId} anchorRef={shareRef} page={page} pages={[page]} canRequestEdit={false}
          pageUrl={serverUrl(PEOPLE_DIRECTORY_HREF)} session={session} onClose={() => setShareOpen(false)} /> : null}
      </span>
      <button type="button" className={`${styles.iconAction} ${styles.linkAction}`} data-people-copy-link
        aria-label={t("topBar:copyLinkTip")} title={t("topBar:copyLinkTip")} data-tip-keys={shortcutCompactLabel("copyPageLink")}
        onClick={() => void copyLink()}>
        <LinkIcon size="regular" aria-hidden="true" />
      </button>
      <button type="button" className={styles.iconAction} data-people-favorite data-favorite={favorite.on ? "true" : undefined}
        aria-label={t(favorite.on ? "topBar:removeFavorite" : "topBar:addFavorite", { pageTitle: title })}
        title={t(favorite.on ? "topBar:removeFavoriteTip" : "topBar:addFavoriteTip")} aria-pressed={favorite.on}
        onClick={() => void changeFavorite(!favorite.on)}>
        {favorite.on ? <StarFilled size="regular" aria-hidden="true" /> : <Star size="regular" aria-hidden="true" />}
      </button>
      <button ref={menuRef} type="button" className={styles.iconAction} data-people-menu
        aria-label={t("topBar:moreActions", { pageTitle: title })} title={t("topBar:pageActionsTip")} data-tip-wait="pageMenu"
        aria-haspopup="menu" aria-expanded={menuOpen} onClick={() => { setShareOpen(false); setMenuOpen((open) => !open); }}>
        <DotsHorizontal size="wide" aria-hidden="true" />
      </button>
      {menuOpen ? <PeopleDatabaseMenu anchor={menuRef} session={session} workspaceId={workspace.id} pageId={pageId} updatedAt={database.updatedAt}
        lastEditedBy={database.lastEditedBy} title={title}
        onCopyLink={() => void copyLink()} onClose={() => setMenuOpen(false)} /> : null}
    </>
  );
}
