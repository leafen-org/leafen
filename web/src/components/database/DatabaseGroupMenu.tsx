import { SwitchInput } from "@/components/Switch";
import { useState } from "react";
import type { GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { i18next } from "@/i18n";
import { ChevronRight, Trash } from "@/icons/leafen";
import { DragHandleIcon, EyeIcon, EyeSlashIcon, PinIcon } from "../icons";
import { activeDateLocale } from "@/lib/i18n";
import type { DbProperty, Page } from "@/lib/types";
import { DatabasePropertyPicker } from "./DatabasePropertyPicker";
import { usePhoneSheetHead } from "../shell/PhoneSheet";
import { useSettingsScreenHead } from "./settingsScreenHead";
import { PhoneMenuChoice } from "./DatabasePhonePickers";
import { NotionSelect } from "./NotionSelect";
import { chipStyle } from "./colors";
import { GroupPageGlyph } from "./GroupPageGlyph";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { NO_BOARD_GROUP } from "./tableGrouping";
import { offeredGroupProperty } from "./arrangeChoices";
import { statusGroupTitle } from "./StatusGroupLabel";
import { databaseViewLabels } from "./databaseViewLabels";
import {
  isDateGroupProperty, isTextGroupProperty, TABLE_DATE_GROUPINGS, tableDateGrouping, tableGrouper,
  tableGroupsDescending, tableGroupSort, tableGroupsOf, tableNumberGrouping, tableTextGrouping,
} from "./tableGrouping";
import { GroupNumberRangeScreen } from "./GroupNumberRangeFields";
import { groupsByComputedDates } from "./computedDateValue";
import { tableGroupPages, tableGroupValueReader } from "./tableGroupValues";
import {
  groupOrderByHand, listedViewGroups, tableConfigAfterAllGroupsShown, tableConfigAfterGroupMoved, tableConfigAfterGroupShown,
  tableGroupsWithCounts, tableHiddenGroupKeys, tableHidesEmptyGroups, tableKeptGroupKeys,
} from "./tableGroupSettingsModel";
import { useDragReorder } from "@/lib/useDragReorder";
import styles from "./databaseSheet";
import { boardGroupPropertyId, regroupedViewConfig } from "./viewLayoutDefaults";
import { chartAxisKept } from "./chartDrawnAxis";
import { isBoardValueGroupProperty } from "./boardValueGrouping";
import { boardConfigAfterGroupPinned, boardPinnedGroupKeys, boardStatusSort, useBoardGroups, type BoardGroupsSurface } from "./boardGroupSettings";
import listStyles from "./DatabasePropertyVisibilityList.module.css";

const GROUP_DRAG = "application/x-leafen-view-group";

const BOARD_GROUP_TYPES = ["select", "multi_select", "status", "person", "checkbox"];
const TABLE_LIKE = new Set(["table", "list", "gallery", "timeline"]);

/**
 * What a view's rows are grouped by, as Notion's grouping screen (seen
 * 2026-09-30): ungrouped, "그룹화 기준" lists "없음" and the properties; once
 * grouped, the property, how a date groups (relative to today or by day,
 * week, month or year, and the day weeks start on) or text groups (the whole
 * text or its first letter, in alphabetical order, its reverse or by hand),
 * then for a table — a list, gallery or timeline alike — "빈 그룹 숨기기" and its groups, each moved by its handle
 * and shown or hidden by its eye, and "그룹화 제거". A table or board groups
 * by options, statuses, people, checkboxes, numbers, dates, text or
 * relations, a board's by what its cards hold sorting as a table's do. A
 * board's (seen 2026-10-11) has "정렬" (by hand or by name), "빈 그룹
 * 숨기기", "열 배경색" for options and its columns as its groups. Options,
 * statuses and dates sort their groups as Notion's (seen 2026-10-24): an
 * option's by hand or by name either way, a status's in its order, the
 * reverse or by hand, a date's oldest or newest first, its groups without
 * handles; a new property's groups start in their own order. A status's
 * shows its options until "상태 표시 기준" shows its groups (seen 2026-10-24).
 */
export function DatabaseGroupMenu({ activeView, properties, rows = [], pagesById, propsByDatabaseId, currentUserId, heading = true, sheet, onUpdateView }: {
  activeView: GraphView;
  properties: readonly GraphProperty[];
  /** The view's rows, whose values make its groups. */
  rows?: readonly GraphRow[];
  /** Pages the view holds besides, which name a relation's groups. */
  pagesById?: Readonly<Record<string, Page>>;
  /** The properties of the databases a rollup reads, without which its related values read none. */
  propsByDatabaseId?: Readonly<Record<string, readonly DbProperty[]>>;
  currentUserId?: string;
  /** Whether it names itself; the view settings panel's heading does it there. */
  heading?: boolean;
  /** In a phone's settings sheet: "그룹화 기준" as Notion's sheet list, "없음" or the property checked. */
  sheet?: boolean;
  onUpdateView: (view: GraphView) => void;
}) {
  const labels = databaseViewLabels();
  // A list's, gallery's or timeline's grouping is a table's, its settings and its groups listed alike (Notion's, seen
  // 2026-11-12: "그룹화 기준", a date's "날짜별" and "정렬", "빈 그룹 숨기기", "그룹" with "모두 숨기기", "그룹화 제거").
  const table = TABLE_LIKE.has(activeView.type);
  // A board's group is the one it shows, which it has even before its settings name one.
  const board = activeView.type === "board";
  // A chart's grouping, reached only from another view's grouping screen (settingsScreenAcrossViews.ts), is a board's
  // — no "없음", no "그룹화 제거", "열 배경색" — and its groups listed, though the chart draws by its own axes, as
  // Notion's (seen 2026-11-18 in Chrome 1440: "마감" picked there left the bars, its X axis and its Y "그룹화" as they were).
  const chart = activeView.type === "chart", boardLike = board || chart;
  // A table or board groups by a title, text, number, date or relation too, as Notion's (seen 2026-10-11), and a gallery or
  // timeline (seen 2026-10-25), a list (seen 2026-11-12) and a chart (seen 2026-11-18) — what a grouping list offers, and its own.
  const anyProperty = table || boardLike;
  const groupId = board ? boardGroupPropertyId(activeView.config, properties) : activeView.config.groupBy;
  const groupable = properties.filter((property) => anyProperty ? offeredGroupProperty(property, groupId) : BOARD_GROUP_TYPES.includes(property.type));
  const groupProperty = properties.find((property) => property.id === groupId);
  const [picking, setPicking] = useState(!groupProperty);
  // Grouping just removed, the screen stays the grouping's, its "그룹화 기준 ›" empty (Notion's, seen 2026-11-02); so
  // does it shown on, as another view is chosen, for a view not grouped (Notion's, seen 2026-11-18: a board's
  // "그룹화" turned a table's, a chart's and a donut's "그룹화 기준 ›" with no value).
  const [removed, setRemoved] = useState(false);
  const [shownFor, setShownFor] = useState(activeView.id);
  if (shownFor !== activeView.id) {
    setShownFor(activeView.id);
    if (!picking) setRemoved(true);
  }
  const listing = picking || (!groupProperty && !removed);
  // Listing the properties, the screen is "그룹화 기준", its "←" back to the grouping while there is one (Notion's, seen 2026-10-24).
  const pickingHead = listing
    ? { title: labels.groupBy, onBack: groupProperty || removed ? () => setPicking(false) : undefined } : null;
  useSettingsScreenHead(heading ? null : pickingHead);
  usePhoneSheetHead(sheet ? pickingHead : null);
  // On a phone a row's choices rise from the bottom, as Notion's (seen 2026-10-24).
  const [phoneChoice, setPhoneChoice] = useState<string | null>(null);
  // "숫자별 › 범위" chosen, numbers' ranges set on their own "← 숫자별" screen (GroupNumberRangeScreen).
  const [ranging, setRanging] = useState(false);
  const dateGrouping = tableDateGrouping(activeView.config);
  // A formula's or rollup's dates group by a unit too, as a date's (Notion's, seen 2026-11-12).
  const dated = !!groupProperty && (isDateGroupProperty(groupProperty) || groupsByComputedDates(groupProperty, properties, propsByDatabaseId));
  const update = (patch: Record<string, unknown>) => onUpdateView({ ...activeView, config: { ...activeView.config, ...patch } });
  const replace = (config: Record<string, unknown> | null) => { if (config) onUpdateView({ ...activeView, config }); };
  const weekday = new Intl.DateTimeFormat(activeDateLocale(), { weekday: "long" });
  // A board groups by its options' order unless by what its cards hold.
  const named = table || (boardLike && isBoardValueGroupProperty(groupProperty));
  const groups = (named || chart) && groupProperty ? tableGroupsWithCounts(groupProperty, rows.map(tableGroupValueReader(groupProperty, properties, rows, pagesById, propsByDatabaseId)),
    activeView.config, tableGroupsOf(groupProperty, tableGrouper(groupProperty, { config: activeView.config, currentUserId,
      ...tableGroupPages(properties, rows, pagesById) }))) : [];
  const groupKeys = groups.map(({ key }) => key);
  const boardGroups = useBoardGroups(activeView.id);
  const boardReorder = useDragReorder(`${GROUP_DRAG}-board`, (sourceKey, targetKey, side) => boardGroups?.move(sourceKey, targetKey, side), { live: true });
  const optionGroups = groupProperty?.type === "select" || groupProperty?.type === "multi_select";
  const statusGroups = groupProperty?.type === "status";
  // A board by a box or a person has no sort or column colors to set, as
  // Notion's (seen 2026-11-07; a person's, 2026-11-12: "그룹화 기준" and
  // "빈 그룹 숨기기", then its groups).
  const sortlessGroups = groupProperty?.type === "checkbox" || groupProperty?.type === "person";
  // A table's groups shown, hidden by hand or, while empty groups hide, empty unless made here; the list shows the
  // hidden ones after the rest (listedViewGroups), moves keeping the table's order.
  const hidden = tableHiddenGroupKeys(activeView.config);
  const hidesEmpty = tableHidesEmptyGroups(activeView.config);
  const kept = tableKeptGroupKeys(activeView.config);
  const hiddenAsEmpty = (group: (typeof groups)[number]) => hidesEmpty && group.count === 0 && !kept.has(group.key);
  const isShown = (group: (typeof groups)[number]) => !hidden.has(group.key) && !hiddenAsEmpty(group);
  const listed = listedViewGroups(groups, isShown);
  const listedKeys = listed.map(({ key }) => key);
  const handMoves = isTextGroupProperty(groupProperty) || groupProperty?.type === "relation" || optionGroups || statusGroups;
  const reorder = useDragReorder(GROUP_DRAG, (sourceKey, targetKey, side) => replace(tableConfigAfterGroupMoved(
    { ...activeView.config, ...(handMoves ? { groupSort: "manual" } : {}) }, groupKeys, sourceKey, targetKey, side)), { live: true });
  // A pick regroups as everywhere grouping is picked (regroupedViewConfig).
  const pick = (id: string) => onUpdateView({ ...activeView, config: { ...regroupedViewConfig(activeView.config,
    typeof groupId === "string" ? groupId : undefined, id, properties, activeView.type), ...chartAxisKept(activeView, properties as unknown as DbProperty[]) } });
  const title = heading ? <div className={styles.toolbarMenuLabel}>{pickingHead ? labels.groupBy : labels.group}</div> : null;
  if (listing || !groupProperty) {
    if (!listing) {
      return (
        <>
          {title}
          <div className={styles.layoutSettings} data-group-settings>
            <button type="button" className={`${styles.layoutRow} ${styles.layoutChoiceRow}`} data-group-by onClick={() => setPicking(true)}>
              <span>{labels.groupBy}</span>
              <span className={styles.layoutChoiceValue}><ChevronRight size="regular" aria-hidden="true" /></span>
            </button>
          </div>
        </>
      );
    }
    return (
      <>
        {title}
        <DatabasePropertyPicker placeholder={labels.searchProperties} sheet={sheet} sheetOpensMore={false} leadingApart={false}
          checked={groupProperty?.id ?? ""}
          properties={groupable.map((property) => ({ ...property, config: property.config as DbProperty["config"] }))}
          // A board is always grouped, as Notion's (seen 2026-10-11): no "없음" or "그룹화 제거" for it.
          leading={boardLike ? undefined : { label: labels.none, onPick: () => update({ groupBy: undefined }) }}
          onPick={(property) => { pick(property.id); setPicking(false); }} />
      </>
    );
  }
  if (ranging && named && groupProperty.type === "number" && tableNumberGrouping(activeView.config).range) {
    return (
      <>
        {heading ? <div className={styles.toolbarMenuLabel}>{labels.groupNumberBy}</div> : null}
        <GroupNumberRangeScreen grouping={tableNumberGrouping(activeView.config)} sheet={sheet} onBack={() => setRanging(false)}
          onChange={(range) => update({ numberGroupRange: range })} />
      </>
    );
  }
  const choice = (label: string, value: string, options: Array<{ value: string; label: string }>, change: (value: string) => void) => sheet ? (
    <>
      <button type="button" className={`${styles.layoutRow} ${styles.layoutChoiceRow}`} aria-label={label} onClick={() => setPhoneChoice(label)}>
        <span>{label}</span>
        <span className={styles.layoutChoiceValue}><span data-sheet-value>{options.find((option) => option.value === value)?.label}</span><ChevronRight size="regular" aria-hidden="true" /></span>
      </button>
      {phoneChoice === label && <PhoneMenuChoice label={label} value={value} options={options} onPick={change} onClose={() => setPhoneChoice(null)} />}
    </>
  ) : (
    <label className={`${styles.layoutRow} ${styles.layoutChoiceRow}`}>
      <span>{label}</span>
      <NotionSelect ariaLabel={label} rowChoice className={styles.layoutChoice} buttonClassName={styles.layoutChoiceButton}
        value={value} options={options} onChange={change} />
    </label>
  );
  return (
    <>
      {title}
      <div className={styles.layoutSettings} data-group-settings>
        <button type="button" className={`${styles.layoutRow} ${styles.layoutChoiceRow}`} data-group-by onClick={() => setPicking(true)}>
          <span>{labels.groupBy}</span>
          <span className={styles.layoutChoiceValue}><span data-sheet-value>{groupProperty.name}</span><ChevronRight size="regular" aria-hidden="true" /></span>
        </button>
        {/* A status shows its options or its groups, its options at first, as Notion's (seen 2026-10-24) — "내 작업"'s its groups (regroupedViewConfig). */}
        {(table || boardLike) && statusGroups && choice(labels.groupStatusShowBy, activeView.config.groupByMode === "group" ? "group" : "option",
          [{ value: "group", label: labels.groupStatusByGroup }, { value: "option", label: labels.groupStatusByOption }],
          (value) => update({ groupByMode: value === "group" ? "group" : "option", groupOrder: undefined }))}
        {named && dated && choice(labels.groupDateBy, dateGrouping,
          TABLE_DATE_GROUPINGS.map((grouping) => ({
            value: grouping, label: grouping === "relative" ? labels.groupDateRelative : labels.groupDateUnits[grouping],
          })),
          (value) => update({ dateGroupBy: value === "relative" ? undefined : value }))}
        {named && dated && dateGrouping === "week" && choice(labels.groupWeekStart,
          activeView.config.startDayOfWeek === 1 ? "1" : "0",
          [0, 1].map((day) => ({ value: String(day), label: weekday.format(new Date(2024, 0, 7 + day)) })),
          (value) => update({ startDayOfWeek: Number(value) }))}
        {named && dated && choice(labels.groupSort, tableGroupSort(activeView.config, groupProperty.type),
          [{ value: "ascending", label: labels.groupDateOldest }, { value: "descending", label: labels.groupDateNewest }],
          (value) => update({ groupSort: value === "descending" ? "descending" : undefined, groupOrder: undefined }))}
        {named && isTextGroupProperty(groupProperty) && choice(labels.groupTextBy, tableTextGrouping(activeView.config),
          [{ value: "exact", label: labels.groupTextExact }, { value: "letter", label: labels.groupTextLetter }],
          (value) => update({ textGroupBy: value === "letter" ? "letter" : undefined }))}
        {/* Numbers each or in ranges — "범위", chosen again too, opening their screen — then up or down, as Notion's (seen 2026-10-11, 2026-11-21). */}
        {named && groupProperty.type === "number" && choice(labels.groupNumberBy, tableNumberGrouping(activeView.config).range ? "range" : "unique",
          [{ value: "unique", label: labels.groupNumberUnique }, { value: "range", label: labels.groupNumberRange }],
          (value) => { update({ numberGroupBy: value === "range" ? "range" : undefined }); setRanging(value === "range"); })}
        {named && groupProperty.type === "number" && choice(labels.groupSort, tableGroupsDescending(activeView.config) ? "descending" : "ascending",
          [{ value: "ascending", label: labels.ascending }, { value: "descending", label: labels.descending }],
          (value) => update({ groupSort: value === "descending" ? "descending" : undefined, groupOrder: undefined }))}
        {named && (isTextGroupProperty(groupProperty) || groupProperty.type === "relation") && choice(labels.groupSort,
          activeView.config.groupSort === "manual" ? "manual" : tableGroupsDescending(activeView.config) ? "descending" : "ascending",
          [{ value: "manual", label: labels.groupSortManual }, { value: "ascending", label: labels.groupSortAscending },
            { value: "descending", label: labels.groupSortDescending }],
          (value) => update(value === "manual" ? { groupSort: "manual", groupOrder: groupOrderByHand(groupKeys, tableGroupsDescending(activeView.config)) }
            : { groupSort: value === "descending" ? "descending" : undefined, groupOrder: undefined }))}
        {table && optionGroups && choice(labels.groupSort, tableGroupSort(activeView.config, groupProperty.type),
          [{ value: "manual", label: labels.groupSortManual }, { value: "ascending", label: labels.groupSortAscending },
            { value: "descending", label: labels.groupSortDescending }],
          (value) => update({ groupSort: value === "manual" ? undefined : value }))}
        {/* By hand, a status's groups keep the order they had moved in, or else the one they are in. */}
        {table && statusGroups && choice(labels.groupSort, tableGroupSort(activeView.config, "status"),
          [{ value: "manual", label: labels.groupSortManual }, { value: "ascending", label: labels.ascending },
            { value: "descending", label: labels.descending }],
          (value) => update(value === "manual" ? { groupSort: "manual",
            groupOrder: activeView.config.groupOrder ?? groupOrderByHand(groupKeys, tableGroupSort(activeView.config, "status") === "descending") }
            : { groupSort: value }))}
        {boardLike && !named && !sortlessGroups && choice(labels.groupSort, statusGroups ? boardStatusSort(activeView.config)
          : activeView.config.groupSort === "ascending" || activeView.config.groupSort === "descending" ? activeView.config.groupSort : "manual",
          [{ value: "manual", label: labels.groupSortManual },
            { value: "ascending", label: statusGroups ? labels.ascending : labels.groupSortAscending },
            { value: "descending", label: statusGroups ? labels.descending : labels.groupSortDescending }],
          (value) => update({ groupSort: value === "manual" && !statusGroups ? undefined : value }))}
        {/* A chart's hides its empty groups as its list does, a table's way (Notion's, seen 2026-11-18: on for a select and a date). */}
        {(table || chart) && (
          <label className={`${styles.layoutRow} ${styles.layoutSwitchRow}`}>
            <span>{labels.hideEmptyGroups}</span>
            <SwitchInput data-hide-empty-groups checked={tableHidesEmptyGroups(activeView.config)}
              onChange={(event) => update({ hideEmptyGroups: event.currentTarget.checked ? undefined : false })} />
          </label>
        )}
        {/* A board by options shows its empty groups until asked not to, one by what cards hold hides them, as Notion's (seen 2026-10-11). */}
        {board && (
          <label className={`${styles.layoutRow} ${styles.layoutSwitchRow}`}>
            <span>{labels.hideEmptyGroups}</span>
            <SwitchInput data-hide-empty-groups
              checked={named ? activeView.config.hideEmptyGroups !== false : activeView.config.hideEmptyGroups === true}
              onChange={(event) => update({ hideEmptyGroups: event.currentTarget.checked ? (named ? undefined : true) : (named ? false : undefined) })} />
          </label>
        )}
        {boardLike && !named && !sortlessGroups && (
          <label className={`${styles.layoutRow} ${styles.layoutSwitchRow}`}>
            <span>{labels.columnColors}</span>
            <SwitchInput data-board-column-colors checked={activeView.config.boardColumnColors !== false}
              onChange={(event) => update({ boardColumnColors: event.currentTarget.checked ? undefined : false })} />
          </label>
        )}
      </div>
      {board && boardGroups && boardGroups.groups.length > 0 && (
        <BoardGroupList surface={boardGroups} reorder={boardReorder} handles={!dated} people={groupProperty.type === "person"} />
      )}
      {(table || chart) && listed.length > 0 && (() => {
        const anyShown = listed.some(isShown);
        const handles = !dated;
        // A chart's groups pin as a board's do, though it draws none of them (Notion's, seen 2026-11-18: the pin
        // brightens and names itself "그룹 고정 해제하기", the bars as they were).
        const pinned = chart ? boardPinnedGroupKeys(activeView.config) : null;
        return (
          <div className={listStyles.list} data-group-list>
            <div className={listStyles.group} role="group" aria-label={labels.groups}>
              <div className={listStyles.groupHead}>
                <span>{labels.groups}</span>
                <button type="button" className={listStyles.groupAction}
                  onClick={() => replace(tableConfigAfterAllGroupsShown(activeView.config, groupKeys, !anyShown))}>
                  {anyShown ? labels.hideAll : labels.showAll}
                </button>
              </div>
              {listed.map((group, index) => {
                const shown = isShown(group);
                // Both checkbox groups are named for the property; their box tells them apart.
                const name = group.checkbox === undefined ? group.label
                  : `${group.label} ${i18next.t(group.checkbox ? "propValue:checkbox.checked" : "propValue:checkbox.unchecked")}`;
                return (
                  <div key={group.key} className={listStyles.row} data-group-setting={group.key} data-shown={shown ? "true" : "false"}
                    {...reorder.row(group.key)}>
                    {handles && <button type="button" className={listStyles.handle} aria-label={labels.dragToMoveGroup(name)}
                      {...reorder.handle(group.key, true, { above: listedKeys[index - 1], below: listedKeys[index + 1] })}>
                      <DragHandleIcon size="small" aria-hidden="true" />
                    </button>}
                    <span className={listStyles.name}>
                      {group.checkbox !== undefined ? (
                        <span className={styles.cardCheckbox} data-checked={group.checkbox ? "true" : undefined} data-group-checkbox
                          aria-hidden="true">
                        </span>
                      ) : null}
                      <GroupPageGlyph page={group.page} pixels={18} data-group-page />
                      {group.person ? <PersonAvatar className={styles.personAvatar} userId={group.person} name={group.label} size={20} /> : null}
                      {statusGroupTitle(group, group.label)
                        ?? (group.color ? <span className={listStyles.chip} style={chipStyle(group.color)}>{group.label}</span> : group.label)}
                    </span>
                    {pinned && <button type="button" className={listStyles.pin} data-group-pin={pinned.has(group.key) ? "pinned" : undefined}
                      aria-label={pinned.has(group.key) ? labels.unpinGroup(name) : labels.pinGroup(name)}
                      onClick={() => replace(boardConfigAfterGroupPinned(activeView.config, group.key, !pinned.has(group.key)))}>
                      <PinIcon size="small" weight={pinned.has(group.key) ? "fill" : undefined} aria-hidden="true" />
                    </button>}
                    {/* With empty groups hidden, an empty group's eye stays shut. */}
                    <button type="button" className={listStyles.eye} aria-pressed={shown} disabled={hiddenAsEmpty(group)}
                      aria-label={labels.toggleGroupShown(name)}
                      onClick={() => replace(tableConfigAfterGroupShown(activeView.config, group.key, !shown))}>
                      {shown ? <EyeIcon size="regular" aria-hidden="true" /> : <EyeSlashIcon size="regular" aria-hidden="true" />}
                    </button>
                  </div>
                );
              })}
            </div>
          </div>
        );
      })()}
      {!boardLike && <button type="button" className={styles.layoutAction} data-remove-grouping
        onClick={() => { update({ groupBy: undefined }); setRemoved(true); setPicking(false); }}>
        <span className={styles.layoutActionIcon} aria-hidden="true"><Trash size="regular" /></span>
        <span>{labels.removeGrouping}</span>
      </button>}
    </>
  );
}

/**
 * A board's groups in its grouping settings: each moved by its handle,
 * pinned beside the board by its pin and shown or hidden by its eye, as
 * Notion's (seen 2026-10-11).
 */
function BoardGroupList({ surface, reorder, handles, people }: {
  surface: BoardGroupsSurface; reorder: ReturnType<typeof useDragReorder>; handles: boolean;
  /** Its groups are people, each named after their avatar with no chip (Notion's, seen 2026-11-12). */
  people?: boolean;
}) {
  const labels = databaseViewLabels();
  const keys = surface.groups.map(({ key }) => key);
  // With empty groups hidden, an empty group's eye stays shut, as on a table.
  const isShown = (key: string) => !surface.hidden.has(key) && !surface.hiddenAsEmpty.has(key);
  const anyShown = surface.groups.some(({ key }) => isShown(key));
  return (
    <div className={listStyles.list} data-group-list data-board-group-list>
      <div className={listStyles.group} role="group" aria-label={labels.groups}>
        <div className={listStyles.groupHead}>
          <span>{labels.groups}</span>
          {surface.movable && <button type="button" className={listStyles.groupAction} onClick={() => surface.setAllHidden(anyShown)}>
            {anyShown ? labels.hideAll : labels.showAll}
          </button>}
        </div>
        {surface.groups.map((group, index) => {
          const shown = isShown(group.key);
          const pinned = surface.pinned.has(group.key);
          const name = group.checked === undefined ? group.label
            : `${group.label} ${i18next.t(group.checked ? "propValue:checkbox.checked" : "propValue:checkbox.unchecked")}`;
          return (
            <div key={group.key} className={listStyles.row} data-group-setting={group.key} data-shown={shown ? "true" : "false"}
              {...reorder.row(group.key)}>
              {surface.movable && handles && <button type="button" className={listStyles.handle} aria-label={labels.dragToMoveGroup(name)}
                {...reorder.handle(group.key, true, { above: keys[index - 1], below: keys[index + 1] })}>
                <DragHandleIcon size="small" aria-hidden="true" />
              </button>}
              <span className={listStyles.name}>
                {group.checked !== undefined ? (
                  <span className={styles.cardCheckbox} data-checked={group.checked ? "true" : undefined} data-group-checkbox aria-hidden="true">
                  </span>
                ) : null}
                <GroupPageGlyph page={group.page} pixels={18} data-group-page />
                {people && group.key !== NO_BOARD_GROUP && !group.key.startsWith("unknown:")
                  ? <><PersonAvatar className={styles.personAvatar} userId={group.key} name={group.label} size={20} />{group.label}</>
                  : statusGroupTitle(group, group.label)
                  ?? (group.color ? <span className={listStyles.chip} style={chipStyle(group.color)}>{group.label}</span> : group.label)}
              </span>
              <button type="button" className={listStyles.pin} disabled={!surface.movable} data-group-pin={pinned ? "pinned" : undefined}
                aria-label={pinned ? labels.unpinGroup(name) : labels.pinGroup(name)} onClick={() => surface.setPinned(group.key, !pinned)}>
                <PinIcon size="small" weight={pinned ? "fill" : undefined} aria-hidden="true" />
              </button>
              <button type="button" className={listStyles.eye} aria-pressed={shown} disabled={!surface.movable || surface.hiddenAsEmpty.has(group.key)}
                aria-label={labels.toggleGroupShown(name)} onClick={() => surface.setHidden(group.key, shown)}>
                {shown ? <EyeIcon size="regular" aria-hidden="true" /> : <EyeSlashIcon size="regular" aria-hidden="true" />}
              </button>
            </div>
          );
        })}
      </div>
    </div>
  );
}
