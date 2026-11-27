import { PersonAvatar } from "@/components/people/PersonAvatar";
import { SharePeopleIcon } from "@/icons/leafen";
import { teamspaceGlyph } from "./KeptIconGlyph";
import type { DialogTeamspace } from "./teamspaceDialogModel";
import styles from "./TeamspaceSettings.module.css";

/**
 * The "팀스페이스 소유자" row's mark in a teamspace's "설정" tab, as Notion's
 * (seen 2026-11-16 in Chrome, 1440 dark, with a person owning it and with a
 * group owning it alone): its owner's face — a person's picture, a group's
 * people mark — in a 20px circle, and the teamspace's own mark on an 11.5px
 * badge at its lower end, 11.5 in and 13.5 down. With a person among its
 * owners, the person's face (which of several Notion shows was not seen).
 */
export function TeamspaceOwnerGlyph({ teamspace }: { teamspace: Pick<DialogTeamspace, "icon" | "name" | "summary"> }) {
  const index = teamspace.summary.ownerUserIds.findIndex(Boolean);
  const userId = index >= 0 ? teamspace.summary.ownerUserIds[index] : "";
  return (
    <span className={styles.ownerGlyph} aria-hidden="true" data-teamspace-owner-glyph={userId ? "person" : "group"}>
      {userId
        ? <PersonAvatar className={styles.ownerFace} userId={userId} name={teamspace.summary.ownerLabels[index]} size={20} />
        : <span className={styles.ownerFace}><SharePeopleIcon size="small" /></span>}
      <span className={styles.ownerBadge}><span>{teamspaceGlyph(teamspace)}</span></span>
    </span>
  );
}
