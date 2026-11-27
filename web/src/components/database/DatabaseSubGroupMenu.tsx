import { statusGroupTitle } from "./StatusGroupLabel";
import { SwitchInput } from "@/components/Switch";
import { useState } from "react";
import type { GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { i18next } from "@/i18n";
import { ChevronRight, Trash } from "@/icons/leafen";
import type { DbProperty, Page } from "@/lib/types";
import { useDragReorder } from "@/lib/useDragReorder";
import { DragHandleIcon, EyeIcon, EyeSlashIcon } from "../icons";
import { DatabasePropertyPicker } from "./DatabasePropertyPicker";
import { NotionSelect } from "./NotionSelect";
import { isBoardSubGroupProperty, type BoardMainGroupProperty } from "./boardGrouping";
import { offeredGroupProperty } from "./arrangeChoices";
import {
  boardSubgroupConfig, boardSubgroupMode, boardSubgroupOrderType, boardSubgroupSettings, boardSubgroupSort, boardSubgroupsByValueAlone,
  boardSubgroupsDated, boardSubgroupsUnsorted, formulaGives, orderBoardSubgroups, projectBoardSubgroupLanes, viewConfigWithSubgroup,
} from "./boardSubgroupModel";
import { nativeBoardGroupingBounds } from "./boardViewModel";
import { boardValueGroups, isBoardValueGroupProperty } from "./boardValueGrouping";
import { chipStyle } from "./colors";
import { GroupPageGlyph } from "./GroupPageGlyph";
import { databaseViewLabels } from "./databaseViewLabels";
import {
  groupOrderByHand, tableConfigAfterAllGroupsShown, tableConfigAfterGroupMoved, tableConfigAfterGroupShown, tableHidesEmptyGroups,
} from "./tableGroupSettingsModel";
import { boardOptionGroupOrder } from "./optionGroupOrder";
import {
  isTextGroupProperty, TABLE_DATE_GROUPINGS, tableDateGrouping, tableGroupSort, tableNumberGrouping, tableTextGrouping,
} from "./tableGrouping";
import { GroupNumberRangeScreen } from "./GroupNumberRangeFields";
import { activeDateLocale } from "@/lib/i18n";
import styles from "./databaseSheet";
import listStyles from "./DatabasePropertyVisibilityList.module.css";
import { useSettingsScreenHead } from "./settingsScreenHead";

const SUB_GROUP_DRAG = "application/x-leafen-view-sub-group";

/**
 * A board's sub-group, as Notion's "하위 그룹화" (seen 2026-10-10, 2026-10-11):
 * without one, "하위 그룹화 기준" lists "없음" and the properties it can split
 * by; with one, that property, "정렬" (by hand, A to Z or Z to A), "빈 그룹
 * 숨기기" and its groups, each moved by its handle and shown or hidden by
 * its eye with "모두 숨기기", then "그룹화 제거". By a date — a formula's or
 * rollup's dates too — "날짜별" (relative to today, by day, week, month or
 * year), the day weeks start on, and "정렬" oldest or newest first, its
 * groups without handles, as the grouping's (seen 2026-11-12). Each type as
 * the grouping's too (seen 2026-11-12): a text's — a formula's text too —
 * "텍스트별" and "정렬" by hand, A to Z at first or Z to A; a relation's and
 * a rollup's that "정렬" alone; a number's — a formula's numbers too —
 * "숫자별" and "정렬" up or down, without handles; a person's, who made or
 * last edited a card and a box's no "정렬" (boardSubgroupSort).
 */
export function DatabaseSubGroupMenu({ activeView, properties, rows = [], pagesById, propsByDatabaseId, currentUserId, onUpdateView }: {
  activeView: GraphView;
  properties: readonly GraphProperty[];
  /** The view's rows, whose values make its sub-groups. */
  rows?: readonly GraphRow[];
  /** Pages the view holds besides, which name a relation's groups. */
  pagesById?: Readonly<Record<string, Page>>;
  /** The properties of the databases a rollup reads, without which its related values read none. */
  propsByDatabaseId?: Readonly<Record<string, readonly DbProperty[]>>;
  currentUserId?: string;
  onUpdateView: (view: GraphView) => void;
}) {
  const labels = databaseViewLabels();
  // What a grouping list offers, and the sub-grouping it has (arrangeChoices.ts).
  const choices = properties.filter((property) => isBoardSubGroupProperty(property) && offeredGroupProperty(property, activeView.config.subGroupBy));
  const subGroup = choices.find((property) => property.id === activeView.config.subGroupBy);
  const [picking, setPicking] = useState(!subGroup);
  // Sub-grouping just removed, the screen stays its own, "하위 그룹화 기준 ›" empty (Notion's, seen 2026-11-03).
  const [removed, setRemoved] = useState(false);
  // "숫자별 › 범위" chosen, numbers' ranges set on their own "← 숫자별" screen, as the grouping's (GroupNumberRangeScreen).
  const [ranging, setRanging] = useState(false);
  // Listing the properties, the screen is "하위 그룹화 기준", its "←" back to the sub-grouping while there is one,
  // and "없음" ticked while none is chosen (Notion's, seen 2026-11-05), as the grouping's.
  const listing = picking || (!subGroup && !removed);
  useSettingsScreenHead(listing ? { title: labels.subGroupBy, onBack: subGroup || removed ? () => setPicking(false) : undefined } : null);
  const sub = boardSubgroupConfig(activeView.config);
  const lanes = subGroup ? projectBoardSubgroupLanes({
    property: subGroup as unknown as BoardMainGroupProperty, rows, bounds: nativeBoardGroupingBounds, currentUserId,
    emptyLabel: i18next.t("boardView:noValue", { groupName: subGroup.name }),
    values: isBoardValueGroupProperty(subGroup) ? boardValueGroups(subGroup, rows, { config: sub, properties, pagesById, propsByDatabaseId, currentUserId }) : null,
    mode: boardSubgroupMode(sub),
  }) : [];
  const orderType = boardSubgroupOrderType(subGroup, properties, propsByDatabaseId);
  const dated = boardSubgroupsDated(orderType);
  const settings = boardSubgroupSettings(lanes, sub, orderType);
  const status = subGroup?.type === "status";
  const sort = boardSubgroupSort(sub, orderType);
  const byValue = boardSubgroupsByValueAlone(orderType);
  const textual = !!subGroup && (isTextGroupProperty(subGroup) || subGroup.type === "formula" && formulaGives(subGroup, properties) === "text");
  // The list's own order, the hidden ones last; moves keep the sub-groups' order.
  const listedKeys = settings.map(({ lane }) => lane.key);
  const keys = orderBoardSubgroups(lanes, sub, orderType).map(({ key }) => key);
  const weekday = new Intl.DateTimeFormat(activeDateLocale(), { weekday: "long" });
  // Another property's settings start afresh, its groups being other ones, an
  // option's in its first order as a board's columns (Notion's, seen 2026-11-08).
  const pick = (subGroupBy: string | undefined) => {
    const groupOrder = boardOptionGroupOrder(properties.find((property) => property.id === subGroupBy));
    onUpdateView({ ...activeView, config: { ...activeView.config, subGroupBy, subGroup: groupOrder ? { groupOrder } : undefined } });
  };
  const replace = (next: Record<string, unknown> | null) => {
    if (next) onUpdateView({ ...activeView, config: viewConfigWithSubgroup(activeView.config, next) });
  };
  const reorder = useDragReorder(SUB_GROUP_DRAG, (sourceKey, targetKey, side) => replace(tableConfigAfterGroupMoved(
    { ...sub, groupSort: status ? "manual" : undefined }, keys, sourceKey, targetKey, side)), { live: true });
  if (!picking && !subGroup && removed) {
    return (
      <div className={styles.layoutSettings} data-sub-group-settings>
        <button type="button" className={`${styles.layoutRow} ${styles.layoutChoiceRow}`} data-sub-group-by onClick={() => setPicking(true)}>
          <span>{labels.subGroupBy}</span>
          <span className={styles.layoutChoiceValue}><ChevronRight size="regular" aria-hidden="true" /></span>
        </button>
      </div>
    );
  }
  if (picking || !subGroup) {
    return (
      <DatabasePropertyPicker placeholder={labels.searchProperties} leadingApart={false} checked={subGroup?.id ?? ""}
        properties={choices.map((property) => ({ ...property, config: property.config as DbProperty["config"] }))}
        leading={{ label: labels.none, onPick: () => pick(undefined) }}
        onPick={(property) => { pick(property.id); setPicking(false); }} />
    );
  }
  if (ranging && orderType === "number" && tableNumberGrouping(sub).range) {
    return <GroupNumberRangeScreen grouping={tableNumberGrouping(sub)} onBack={() => setRanging(false)}
      onChange={(range) => replace({ ...sub, numberGroupRange: range })} />;
  }
  const anyShown = settings.some(({ shown }) => shown);
  return (
    <>
      <div className={styles.layoutSettings} data-sub-group-settings>
        <button type="button" className={`${styles.layoutRow} ${styles.layoutChoiceRow}`} data-sub-group-by onClick={() => setPicking(true)}>
          <span>{labels.subGroupBy}</span>
          <span className={styles.layoutChoiceValue}><span data-sheet-value>{subGroup.name}</span><ChevronRight size="regular" aria-hidden="true" /></span>
        </button>
        {status ? (
          <label className={`${styles.layoutRow} ${styles.layoutChoiceRow}`}>
            <span>{labels.groupStatusShowBy}</span>
            <NotionSelect ariaLabel={labels.groupStatusShowBy} className={styles.layoutChoice} buttonClassName={styles.layoutChoiceButton} rowChoice
              value={boardSubgroupMode(sub)}
              options={[{ value: "group", label: labels.groupStatusByGroup }, { value: "option", label: labels.groupStatusByOption }]}
              onChange={(value) => replace({ ...sub, groupByMode: value === "group" ? "group" : undefined, groupOrder: undefined })} />
          </label>
        ) : null}
        {dated && <>
          <label className={`${styles.layoutRow} ${styles.layoutChoiceRow}`}>
            <span>{labels.groupDateBy}</span>
            <NotionSelect ariaLabel={labels.groupDateBy} className={styles.layoutChoice} buttonClassName={styles.layoutChoiceButton} rowChoice
              value={tableDateGrouping(sub)}
              options={TABLE_DATE_GROUPINGS.map((grouping) => ({
                value: grouping, label: grouping === "relative" ? labels.groupDateRelative : labels.groupDateUnits[grouping],
              }))}
              onChange={(value) => replace({ ...sub, dateGroupBy: value === "relative" ? undefined : value })} />
          </label>
          {tableDateGrouping(sub) === "week" && <label className={`${styles.layoutRow} ${styles.layoutChoiceRow}`}>
            <span>{labels.groupWeekStart}</span>
            <NotionSelect ariaLabel={labels.groupWeekStart} className={styles.layoutChoice} buttonClassName={styles.layoutChoiceButton} rowChoice
              value={sub.startDayOfWeek === 1 ? "1" : "0"}
              options={[0, 1].map((day) => ({ value: String(day), label: weekday.format(new Date(2024, 0, 7 + day)) }))}
              onChange={(value) => replace({ ...sub, startDayOfWeek: Number(value) })} />
          </label>}
          <label className={`${styles.layoutRow} ${styles.layoutChoiceRow}`}>
            <span>{labels.groupSort}</span>
            <NotionSelect ariaLabel={labels.groupSort} className={styles.layoutChoice} buttonClassName={styles.layoutChoiceButton} rowChoice
              value={tableGroupSort(sub, "date")}
              options={[{ value: "ascending", label: labels.groupDateOldest }, { value: "descending", label: labels.groupDateNewest }]}
              onChange={(value) => replace({ ...sub, groupSort: value === "descending" ? "descending" : undefined, groupOrder: undefined })} />
          </label>
        </>}
        {textual && <label className={`${styles.layoutRow} ${styles.layoutChoiceRow}`}>
          <span>{labels.groupTextBy}</span>
          <NotionSelect ariaLabel={labels.groupTextBy} className={styles.layoutChoice} buttonClassName={styles.layoutChoiceButton} rowChoice
            value={tableTextGrouping(sub)}
            options={[{ value: "exact", label: labels.groupTextExact }, { value: "letter", label: labels.groupTextLetter }]}
            onChange={(value) => replace({ ...sub, textGroupBy: value === "letter" ? "letter" : undefined })} />
        </label>}
        {orderType === "number" && <>
          <label className={`${styles.layoutRow} ${styles.layoutChoiceRow}`}>
            <span>{labels.groupNumberBy}</span>
            <NotionSelect ariaLabel={labels.groupNumberBy} className={styles.layoutChoice} buttonClassName={styles.layoutChoiceButton} rowChoice
              value={tableNumberGrouping(sub).range ? "range" : "unique"}
              options={[{ value: "unique", label: labels.groupNumberUnique }, { value: "range", label: labels.groupNumberRange }]}
              onChange={(value) => { replace({ ...sub, numberGroupBy: value === "range" ? "range" : undefined }); setRanging(value === "range"); }} />
          </label>
          <label className={`${styles.layoutRow} ${styles.layoutChoiceRow}`}>
            <span>{labels.groupSort}</span>
            <NotionSelect ariaLabel={labels.groupSort} className={styles.layoutChoice} buttonClassName={styles.layoutChoiceButton} rowChoice
              value={sort} options={[{ value: "ascending", label: labels.ascending }, { value: "descending", label: labels.descending }]}
              onChange={(value) => replace({ ...sub, groupSort: value === "descending" ? "descending" : undefined, groupOrder: undefined })} />
          </label>
        </>}
        {/* A box, a person and who made or last edited a card have no sort to set, as Notion's (seen 2026-11-07, 2026-11-12). */}
        {!boardSubgroupsUnsorted(orderType) && !byValue && <label className={`${styles.layoutRow} ${styles.layoutChoiceRow}`}>
          <span>{labels.groupSort}</span>
          <NotionSelect ariaLabel={labels.groupSort} className={styles.layoutChoice} buttonClassName={styles.layoutChoiceButton} rowChoice
            value={sort}
            options={[{ value: "manual", label: labels.groupSortManual },
              { value: "ascending", label: status ? labels.ascending : labels.groupSortAscending },
              { value: "descending", label: status ? labels.descending : labels.groupSortDescending }]}
            onChange={(value) => replace(value === "manual" ? { ...sub, groupSort: status ? "manual" : undefined,
              groupOrder: Array.isArray(sub.groupOrder) ? sub.groupOrder : groupOrderByHand(keys, sort === "descending") }
              : { ...sub, groupSort: value })} />
        </label>}
        <label className={`${styles.layoutRow} ${styles.layoutSwitchRow}`}>
          <span>{labels.hideEmptyGroups}</span>
          <SwitchInput data-sub-group-hide-empty checked={tableHidesEmptyGroups(sub)}
            onChange={(event) => replace({ ...sub, hideEmptyGroups: event.currentTarget.checked ? undefined : false })} />
        </label>
      </div>
      {settings.length > 0 && (
        <div className={listStyles.list} data-sub-group-list>
          <div className={listStyles.group} role="group" aria-label={labels.groups}>
            <div className={listStyles.groupHead}>
              <span>{labels.groups}</span>
              <button type="button" className={listStyles.groupAction}
                onClick={() => replace(tableConfigAfterAllGroupsShown(sub, keys, !anyShown))}>
                {anyShown ? labels.hideAll : labels.showAll}
              </button>
            </div>
            {settings.map(({ lane, shown, hiddenAsEmpty }, index) => {
              // Both checkbox groups are named for the property; their box tells them apart.
              const name = lane.checked === undefined ? lane.label
                : `${lane.label} ${i18next.t(lane.checked ? "propValue:checkbox.checked" : "propValue:checkbox.unchecked")}`;
              return (
                <div key={lane.key} className={listStyles.row} data-sub-group-setting={lane.key} data-shown={shown ? "true" : "false"}
                  {...reorder.row(lane.key)}>
                  {!byValue && <button type="button" className={listStyles.handle} aria-label={labels.dragToMoveGroup(name)}
                    {...reorder.handle(lane.key, true, { above: listedKeys[index - 1], below: listedKeys[index + 1] })}>
                    <DragHandleIcon size="small" aria-hidden="true" />
                  </button>}
                  <span className={listStyles.name}>
                    {lane.checked !== undefined ? (
                      <span className={styles.cardCheckbox} data-checked={lane.checked ? "true" : undefined} data-group-checkbox
                        aria-hidden="true">
                      </span>
                    ) : null}
                    <GroupPageGlyph page={lane.page} pixels={18} data-group-page />
                    {statusGroupTitle(lane, lane.label) ?? (lane.color ? <span className={listStyles.chip} style={chipStyle(lane.color)}>{lane.label}</span> : lane.label)}
                  </span>
                  <button type="button" className={listStyles.eye} aria-pressed={shown} disabled={hiddenAsEmpty}
                    aria-label={labels.toggleGroupShown(name)}
                    onClick={() => replace(tableConfigAfterGroupShown(sub, lane.key, !shown))}>
                    {shown ? <EyeIcon size="regular" aria-hidden="true" /> : <EyeSlashIcon size="regular" aria-hidden="true" />}
                  </button>
                </div>
              );
            })}
          </div>
        </div>
      )}
      <button type="button" className={styles.layoutAction} data-remove-sub-grouping
        onClick={() => { pick(undefined); setRemoved(true); setPicking(false); }}>
        <span className={styles.layoutActionIcon} aria-hidden="true"><Trash size="regular" /></span>
        <span>{labels.removeGrouping}</span>
      </button>
    </>
  );
}
