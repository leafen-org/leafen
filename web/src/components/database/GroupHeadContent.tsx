import { useTranslation } from "react-i18next";
import { CaretRightFill, DotsHorizontal, Plus } from "../icons";
import { chipStyle } from "./colors";
import { GroupPageGlyph, type GroupPage } from "./GroupPageGlyph";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { statusGroupTitle } from "./StatusGroupLabel";
import styles from "./databaseSheet";

export type GroupHeadGroup = {
  key: string; label?: string; color?: string; checkbox?: boolean; page?: GroupPage; sprintPlace?: string;
  statusStage?: Parameters<typeof statusGroupTitle>[0]["statusStage"]; statusOption?: boolean;
  /** A person's group: their avatar before their name, as Notion's (seen 2026-11-12). */
  person?: string;
};

/**
 * A group's head, a table's and a list's alike, as Notion's (seen 2026-10-11
 * and 2026-10-25): its filled triangle, turned down while open; its name — an
 * option's chip, a status's mark, a checkbox's box, a related page's glyph —
 * then its count where the view shows counts, its "⋯" and "+", shown on a
 * desktop while the head is pointed at. Its chip, where the view has a menu
 * for it, is pressed to rename its option in place (`onMenu` with `naming`,
 * ViewGroupOptions' GroupRename), as Notion's table, list and gallery do
 * (seen 2026-11-28): the chip in a press area 3px around it, where it stood.
 */
export function GroupHeadContent({ group, collapsed, count, onToggle, onMenu, onAdd }: {
  group: GroupHeadGroup; collapsed: boolean;
  /** How many pages it holds, where the view shows counts. */
  count?: number;
  onToggle(): void; onMenu?(element: HTMLElement, naming?: boolean): void; onAdd?(): void;
}) {
  const { t } = useTranslation(["boardView", "pageTreeItem", "propValue", "databaseView"]);
  const groupName = group.label ?? (group.key === "__no_group__" ? t("boardView:none") : group.key);
  // Both checkbox groups are named for the property; the box tells them apart.
  const groupLabel = group.checkbox === undefined ? groupName
    : `${groupName} ${t(group.checkbox ? "propValue:checkbox.checked" : "propValue:checkbox.unchecked")}`;
  return (
    <>
      <button type="button" className={styles.tableGroupToggle} aria-expanded={!collapsed}
        aria-label={t(collapsed ? "pageTreeItem:expand" : "pageTreeItem:collapse", { title: groupLabel })} onClick={onToggle}>
        {/* Notion's caret by the group's 14px name: 11.2px, turned down while open (seen 2026-11-14). */}
        <CaretRightFill aria-hidden="true" />
      </button>
      {/* A checkbox group's box is an unnamed, disabled checkbox holding the group's state, the property's
          name beside it, as Notion's table and board group heads (seen 2026-11-19). */}
      {group.checkbox !== undefined ? (
        <span className={styles.cardCheckbox} data-checked={group.checkbox ? "true" : undefined} data-table-group-checkbox
          role="checkbox" aria-checked={group.checkbox} aria-disabled="true">
        </span>
      ) : null}
      {group.sprintPlace ? (
        // "현재:" before the current sprint, as Notion's sprint planning (seen 2026-11-04).
        <span className={styles.tableGroupPlace} data-table-group-sprint={group.sprintPlace}>
          {t("databaseView:sprintGroupPlace", { place: t(`databaseView:relativeSprints.${group.sprintPlace}`) })}
        </span>
      ) : null}
      <GroupPageGlyph page={group.page} pixels={16} data-table-group-page />
      {group.person ? <PersonAvatar className={`${styles.personAvatar} ${styles.tableGroupPerson}`} userId={group.person} name={groupName} size={20} /> : null}
      {onMenu && !group.statusStage && (group.statusOption || group.color) ? (
        <button type="button" className={styles.tableGroupNameButton} data-table-group-name onClick={(event) => onMenu(event.currentTarget, true)}>
          {groupNameChip(group, groupName)}</button>
      ) : groupNameChip(group, groupName)}
      <span className={styles.tableGroupTail} data-after-chip={group.color ? "true" : undefined}>
        {count !== undefined ? <span className={styles.tableGroupCount} data-table-group-count>{count}</span> : null}
        {onMenu ? (
          <button type="button" className={styles.tableGroupAction} aria-label={t("boardView:groupActions", { groupName: groupLabel })} aria-haspopup="menu"
            onClick={(event) => onMenu(event.currentTarget)} data-table-group-menu><DotsHorizontal size="small" aria-hidden="true" /></button>
        ) : null}
        {onAdd ? (
          <button type="button" className={styles.tableGroupAction} aria-label={t("boardView:addToGroup", { groupName: groupLabel })}
            onClick={onAdd} data-table-group-add><Plus size="small" aria-hidden="true" /></button>
        ) : null}
      </span>
    </>
  );
}

/** A group's name as its head shows it: a status's mark or option, an option's chip, or its words. */
function groupNameChip(group: GroupHeadGroup, groupName: string) {
  return statusGroupTitle(group, groupName) ?? <span className={styles.tableGroupLabel} data-table-group-chip={group.color ? "true" : undefined}
    style={group.color ? chipStyle(group.color) : undefined}>{groupName}</span>;
}
