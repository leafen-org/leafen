import { GroupPeopleIcon } from "@/icons/leafen";
import { KeptIconGlyph } from "@/components/teamspace/KeptIconGlyph";
import styles from "./WorkspaceGroups.module.css";

/**
 * A group's mark where the group is named — its row in the list, its
 * window's head, its "새 그룹 생성" field, a person's groups — as Notion's:
 * the emoji, built-in icon or picture picked for it, or Notion's two people
 * filling the slot (filled in the list's rows, as Notion's, 2026-11-11). An
 * emoji in a 20px slot is 18px, as in Notion's rows.
 */
export function WorkspaceGroupIcon({ icon, size, filled }: { icon?: string; size: number; filled?: boolean }) {
  return (
    <span className={styles.groupMark} style={{ width: size, height: size }} aria-hidden="true" data-group-icon={icon || undefined}>
      {icon ? <span className={styles.groupEmoji} style={{ fontSize: size === 20 ? 18 : Math.round(size * 0.8) }}><KeptIconGlyph icon={icon} fallback="" /></span>
        : <GroupPeopleIcon pixels={size} weight={filled ? "fill" : undefined} />}
    </span>
  );
}
