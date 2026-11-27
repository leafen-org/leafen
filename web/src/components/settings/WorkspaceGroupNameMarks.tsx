import { useTranslation } from "react-i18next";
import { HoverTip } from "@/components/IconTipButton";
import type { WorkspacePermissionGroup } from "@/workspaceMembershipApi";
import tags from "@/components/people/NameTag.module.css";
import styles from "./WorkspaceGroupNameMarks.module.css";

/**
 * The marks after a group's name in the list, as Notion's (seen 2026-11-15
 * twice, in two tabs, in its free workspace's 설정 › 사람 › 그룹, 1440 dark):
 * "그룹 멤버" in blue where the reader is in the group, then "제한됨" in
 * yellow where it holds a restricted member, with its tip above it; a group
 * with neither has none. The server says which (its list's readerMember and
 * hasRestrictedMember). A phone's line takes its members' line's tag shape.
 */
export function WorkspaceGroupNameMarks({ group, phone = false }: { group: WorkspacePermissionGroup; phone?: boolean }) {
  const { t } = useTranslation(["workspaceSettingsDialog", "peopleProfiles"]);
  const tag = phone ? `${tags.tag} ${tags.phone}` : tags.tag;
  return (
    <>
      {group.readerMember ? (
        <span className={`${tag} ${tags.blue}`} data-group-reader-member>{t("groupReaderMemberTag")}</span>
      ) : null}
      {group.hasRestrictedMember ? (
        <HoverTip label={t("groupRestrictedMemberTip")} placement="top" wide className={`${tag} ${tags.yellow}`}>
          {t("peopleProfiles:restricted")}
        </HoverTip>
      ) : null}
    </>
  );
}

/** A group's name with its marks after it, 8px apart, the name giving way first. */
export function WorkspaceGroupNameLine({ group, nameClass }: { group: WorkspacePermissionGroup; nameClass: string }) {
  return (
    <span className={styles.line}>
      <span className={nameClass}>{group.name}</span>
      <WorkspaceGroupNameMarks group={group} />
    </span>
  );
}
