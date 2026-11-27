import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { ArrowDown } from "@/icons/leafen";
import { Plus } from "../icons";
import { LoadLimitMenuButton } from "./LoadLimitMenuButton";
import { GroupHeadContent } from "./GroupHeadContent";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import type { TimelineAddLane, TimelineGroupLane } from "./timelineLaneProjection";
import { TimelineCreateLane } from "./TimelineCreateLane";
import { ViewGroupAdd } from "./ViewGroupAdd";
import type { ViewGroupAddKind } from "./viewGroupAddKind";
import styles from "./databaseSheet";
import moreStyles from "./TimelineGroupMore.module.css";

/** What a grouped timeline's heads and lanes may do; each is left out where it may not. */
export type TimelineGroupActions = {
  /** A new row in the group: the head's "+" and its "+ 새로 만들기". */
  onAdd?(key: string): void;
  /** The table's "+ 새로 만들기" of a group: a row in it with no date yet, as Notion's (seen 2026-11-15 in Chrome at 1440, two groups). */
  onAddInTable?(key: string): void;
  onMenu?(group: TimelineGroupLane, element: HTMLElement, naming?: boolean): void;
  /** "+ 신규 그룹": what it adds (viewGroupAddKind), the names a new group may not take, the groups it has, and its making. */
  newGroup?: { kind: ViewGroupAddKind; property: GraphProperty; environment?: PropertyCellEnvironment;
    taken: readonly string[]; has(value: string): boolean; onAdd(value: string): void };
  showCounts?: boolean;
  /** A group's "더 불러오기": its next rows, and the "⋯" choosing how many each group first shows. */
  onLoadMore?(key: string): void;
  loadLimit?: { limit: number; onLimit?(limit: number): void };
};

/** A new bar's lane where rows may be made (TimelineCreateLane), a group's made in it. */
export type TimelineNewBarLane = { dayWidth: number; rangeDays: number; columns: number; cover: number; label: string;
  onCreate(first: number, columns: number, groupKey?: string): void; onPoint?(first: number | null, last: number): void };

/**
 * A grouped timeline's lanes without its table, as Notion's (seen 2026-11-01
 * at 1280px): each group's head kept at the canvas's start edge, its bars, its
 * "+ 새로 만들기", then "+ 신규 그룹" after the last group. Beside a table the
 * table holds the heads and words, and the canvas each group's lane alone.
 * A group's "+ 새로 만들기" lane is the whole line, as the ungrouped one:
 * pointed at, it draws the bar a new row would be, its words giving way, and
 * pressed, the row is made in the group there, with or without the table, as
 * Notion's (seen 2026-11-15 at 1440, both: a 197×34 box over 19–23 in the
 * hover ground, round 6, crosshair; pressed, the group's new row and its bar's
 * title open).
 */
export function TimelineGroupLanes({ groups, addLanes, newGroupLane, onToggle, actions, table = false, newBar }: {
  groups: readonly TimelineGroupLane[]; addLanes: readonly TimelineAddLane[]; newGroupLane?: number;
  onToggle(key: string): void; actions: TimelineGroupActions; table?: boolean; newBar?: TimelineNewBarLane;
}) {
  return <>
    {table ? null : groups.map(group => <TimelineGroupLaneHeader key={group.key} group={group} onToggle={onToggle} actions={actions} />)}
    {addLanes.map(add => add.more ? table ? null : <TimelineGroupMoreLane key={`more:${add.key}`} add={add} actions={actions} />
      : newBar ? <TimelineCreateLane key={add.key} {...newBar} lane={add.lane} labelled={!table}
      onCreate={(first, columns) => newBar.onCreate(first, columns, add.key)} />
      : table ? null : <TimelineGroupAddLane key={add.key} add={add} actions={actions} />)}
    {newGroupLane !== undefined && !table ? <TimelineNewGroupLane lane={newGroupLane} actions={actions} /> : null}
  </>;
}

/** A group's head: its toggle, its name, its count where the view shows counts, its "⋯" and "+" while pointed at; `pinned`, held at the page's top with the view's heads (useStickyViewHeads). */
export function TimelineGroupLaneHeader({ group, onToggle, actions, table = false, pinned = false }: {
  group: TimelineGroupLane; onToggle(key: string): void; actions: TimelineGroupActions; table?: boolean; pinned?: boolean;
}) {
  return <div className={styles.timelineGroupHead} data-table={table ? "true" : undefined} style={{ gridRow: group.lane + 1 }}
    data-sticky-head={pinned ? "" : undefined} data-timeline-group-lane={group.key} data-timeline-lane={group.lane}>
    <div className={styles.timelineGroupHeadInner}>
      <GroupHeadContent group={group} collapsed={group.collapsed} count={actions.showCounts ? group.count : undefined}
        onToggle={() => onToggle(group.key)}
        onMenu={actions.onMenu ? (element, naming) => actions.onMenu?.(group, element, naming) : undefined}
        onAdd={actions.onAdd ? () => actions.onAdd?.(group.key) : undefined} />
    </div>
  </div>;
}

/** A group's "+ 새로 만들기", its plus 8px in and its words 30px in; the table's makes the group's row with no date (`onAddInTable`). */
export function TimelineGroupAddLane({ add, actions, table = false }: { add: TimelineAddLane; actions: TimelineGroupActions; table?: boolean }) {
  const { t } = useTranslation("timelineView");
  if (add.more) return <TimelineGroupMoreLane add={add} actions={actions} table={table} />;
  return <div className={styles.timelineGroupAdd} data-table={table ? "true" : undefined} style={{ gridRow: add.lane + 1 }}
    data-timeline-group-add={add.key} data-timeline-lane={add.lane}>
    <button type="button" onClick={() => (table ? actions.onAddInTable ?? actions.onAdd : actions.onAdd)?.(add.key)}>
      <Plus size="small" aria-hidden="true" />{t("toolbar.createInCanvas")}</button>
  </div>;
}

/** "+ 신규 그룹", which asks the new group's name, person or page under it (ViewGroupAdd). */
export function TimelineNewGroupLane({ lane, actions, table = false }: { lane: number; actions: TimelineGroupActions; table?: boolean }) {
  const newGroup = actions.newGroup;
  return <div className={styles.timelineNewGroup} data-table={table ? "true" : undefined} style={{ gridRow: lane + 1 }} data-timeline-lane={lane}>
    {newGroup ? <ViewGroupAdd kind={newGroup.kind} property={newGroup.property} environment={newGroup.environment} className=""
      taken={newGroup.taken} has={newGroup.has} marker="timeline-new-group" onAdd={newGroup.onAdd} /> : null}
  </div>;
}

/**
 * A group's "↓ 더 불러오기 ⋯" under its first rows, before its "+ 새로
 * 만들기", as Notion's (seen 2026-11-27 in Chrome at 1440: a 33px lane, on
 * the canvas at its start edge, beside a table across the table with its
 * "⋯" at the end, the canvas's lane left empty); pressed, that group shows
 * more, the others as they were.
 */
export function TimelineGroupMoreLane({ add, actions, table = false }: { add: TimelineAddLane; actions: TimelineGroupActions; table?: boolean }) {
  const { t } = useTranslation("tableView");
  const more = () => actions.onLoadMore?.(add.key), limit = actions.loadLimit;
  return <div className={moreStyles.lane} data-table={table ? "true" : undefined} style={{ gridRow: add.lane + 1 }}
    data-timeline-group-more={add.key} data-timeline-lane={add.lane}
    onClick={(event) => { if (!(event.target as HTMLElement).closest("button, [role='menu']")) more(); }}>
    <span className={moreStyles.line}>
      <button type="button" className={moreStyles.loadMore} onClick={more}><ArrowDown size="small" aria-hidden="true" /><span>{t("loadMoreAria")}</span></button>
      {limit?.onLimit ? <LoadLimitMenuButton limit={limit.limit} onLimit={limit.onLimit} /> : null}
    </span>
  </div>;
}
