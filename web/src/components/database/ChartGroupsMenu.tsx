import { useId } from "react";
import { useTranslation } from "react-i18next";
import { useDragReorder } from "@/lib/useDragReorder";
import { DragHandleIcon, EyeIcon, EyeSlashIcon } from "@/icons/leafen";
import { ChartChoiceList, type ChartChoice } from "./ChartPanelMenus";
import { chipStyle } from "./colors";
import type { ChartGroup } from "./chartViewSeries";
import styles from "./ChartGroupsMenu.module.css";
import menuStyles from "./ChartPanelMenus.module.css";

/**
 * A chart axis's "정렬 기준", as Notion's (seen 2026-10-05): its sorts, then
 * "그룹" with "모두 숨기기" and each group — an option's chip or "Priority
 * 없음" — with an eye to hide or show it and, when sorted by hand, a handle
 * to drag it to another place, the groups making way as in Notion's menu
 * lists (useDragReorder's live list; a mouse's drag follows the pointer's
 * own events, as the browser's drop was lost at times in WebKit).
 */
export function ChartGroupsMenu({ label, sorts, sort, groups, manual, onSort, onHidden, onOrder, marker }: {
  label: string;
  sorts: readonly ChartChoice[];
  sort: string;
  groups: readonly ChartGroup[];
  manual: boolean;
  onSort(sort: string): void;
  onHidden(hidden: string[]): void;
  onOrder(order: string[]): void;
  marker: string;
}) {
  const { t } = useTranslation("chartView");
  const hidden = groups.filter((group) => group.hidden).map((group) => group.key);
  const allHidden = groups.length > 0 && hidden.length === groups.length;
  const reorder = useDragReorder(`chart-groups:${useId()}`, (dragged, target, side) => {
    const keys = groups.map((group) => group.key).filter((key) => key !== dragged);
    keys.splice(keys.indexOf(target) + (side === "after" ? 1 : 0), 0, dragged);
    onOrder(keys);
  }, { live: true });
  return <>
    <ChartChoiceList label={label} options={sorts} value={sort} onPick={onSort} marker={marker} />
    {groups.length > 0 && <>
      <div className={menuStyles.sectionLine} role="separator" />
      <div className={styles.head}>
        <span>{t("groups")}</span>
        <button type="button" className={styles.all} data-chart-groups-all
          onClick={() => onHidden(allHidden ? [] : groups.map((group) => group.key))}>
          {allHidden ? t("showAll") : t("hideAll")}
        </button>
      </div>
      <ul className={styles.groups} aria-label={t("groups")}>
        {groups.map((group) => (
          // Sorted by hand, a group drags by its whole row.
          <li key={group.key} className={styles.group} data-chart-group={group.key} data-hidden={group.hidden ? "true" : undefined}
            {...reorder.row(group.key, () => true, manual)}>
            {manual && <span className={styles.handle} aria-hidden="true"><DragHandleIcon size="small" /></span>}
            <span className={styles.name}>
              {group.option
                ? <span className={styles.chip} style={chipStyle(group.color)}>{group.label}</span>
                : group.label}
            </span>
            <button type="button" className={styles.eye} aria-pressed={!group.hidden}
              aria-label={group.hidden ? t("showGroup", { name: group.label }) : t("hideGroup", { name: group.label })}
              onClick={() => onHidden(group.hidden ? hidden.filter((key) => key !== group.key) : [...hidden, group.key])}>
              {group.hidden ? <EyeSlashIcon size="regular" /> : <EyeIcon size="regular" />}
            </button>
          </li>
        ))}
      </ul>
    </>}
  </>;
}
