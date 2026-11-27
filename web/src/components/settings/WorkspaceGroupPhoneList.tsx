import { useTranslation } from "react-i18next";
import type { WorkspacePermissionGroup } from "@/workspaceMembershipApi";
import { WorkspaceGroupIcon } from "./WorkspaceGroupIcon";
import { WorkspaceGroupNameMarks } from "./WorkspaceGroupNameMarks";
import styles from "./WorkspaceMemberPhoneList.module.css";

/**
 * A phone's groups, in its members' shape (WorkspaceMemberPhoneList, as
 * Notion's phone "멤버" lines, seen 2026-11-12 at 402pt): no table, a line
 * for each group — its 28px mark, its name in 16px, how many members and
 * linked teamspaces it has in 14px under it, its marks after its name
 * (WorkspaceGroupNameMarks, in the members' tag shape) — that opens the group's
 * window, as the computer's row does. Notion's phone has no groups screen.
 */
export function WorkspaceGroupPhoneList({ groups, onOpen }: {
  groups: readonly WorkspacePermissionGroup[];
  onOpen(groupId: string): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  return (
    <div className={styles.list} role="list" aria-label={t("peopleTabGroups")} data-group-phone-list>
      {groups.map((group) => (
        <div key={group.id} role="listitem" className={styles.row} data-workspace-group={group.id}>
          <button type="button" className={styles.lineButton} aria-haspopup="dialog" onClick={() => onOpen(group.id)}>
            <span className={styles.avatar}><WorkspaceGroupIcon icon={group.icon} size={28} filled /></span>
            <span className={styles.text}>
              <span className={styles.nameLine}>
                <strong className={styles.name}>{group.name}</strong>
                <WorkspaceGroupNameMarks group={group} phone />
              </span>
              <span className={styles.sub}>
                {t("groupMemberCount", { count: group.memberCount })} · {t("memberTeamspaceCount", { count: group.teamspaceCount ?? 0 })}
              </span>
            </span>
          </button>
        </div>
      ))}
    </div>
  );
}
