import { Trans, useTranslation } from "react-i18next";
import { HoverTip } from "@/components/IconTipButton";
import { ArchiveIcon, TeamspaceRestoreIcon } from "@/icons/leafen";
import { useKeptTeamspaces } from "@/lib/workspaceTeamspaces";
import type { Page, Session } from "@/types";
import { teamspaceIdOf } from "./TopBarTeamspaceCrumb";
import { useArchivedTeamspaceRestore } from "./useArchivedTeamspaceRestore";
import styles from "./PageArchiveBanner.module.css";

/**
 * The yellow band over a page in an archived teamspace, which its owners and
 * members still open, as Notion's (measured 2026-11-13): an archive box, a
 * line that the page is archived with its teamspace, the teamspace's name on
 * its own line, and "팀스페이스 복원", which only the teamspace's
 * owners may press; to everyone else it is dim and says why. The band stays
 * over the page as it scrolls. The teamspaces are those the sidebar last read.
 */
export function PageTeamspaceArchiveBanner({ page, pages, session }: {
  page: Page;
  pages: readonly Page[];
  session: Session;
}) {
  const { t } = useTranslation(["pageArchive", "teamspaceDialog"]);
  const kept = useKeptTeamspaces(session.user.id, page.workspaceId);
  const teamspaceId = teamspaceIdOf(page, pages);
  const place = teamspaceId ? kept?.archived?.find((item) => item.id === teamspaceId) : undefined;
  const { busy, lock, lockReason, restore } = useArchivedTeamspaceRestore(session, page.workspaceId, place, kept);
  if (!place) return null;
  const archived = place;

  const button = (
    <button type="button" className={styles.action} data-teamspace-restore aria-disabled={lock ? "true" : undefined}
      disabled={busy} aria-busy={busy || undefined} onClick={() => void restore()}>
      <TeamspaceRestoreIcon size="small" trim aria-hidden="true" />
      {t("teamspaceDialog:restoreTeamspace")}
    </button>
  );
  return (
    <div className={`${styles.banner} ${styles.teamspace}`} role="status" data-page-archive-banner="teamspace">
      <span className={styles.teamspaceMessage}>
        <ArchiveIcon size="small" weight="fill" aria-hidden="true" />
        <span className={styles.message}>
          <Trans t={t} i18nKey="pageArchive:inArchivedTeamspace" values={{ name: archived.name }}
            components={{ name: <span className={styles.teamspaceName} /> }} />
        </span>
      </span>
      {lock ? <HoverTip label={lockReason} wide>{button}</HoverTip> : button}
    </div>
  );
}
