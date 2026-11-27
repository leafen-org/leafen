import type { Dispatch, SetStateAction } from "react";
import type { TFunction } from "i18next";
import type { GraphRow, GraphView } from "../../contentGraph";
import type { PopoverRect } from "@/lib/popoverPlacement";
import type { DbProperty, DbView } from "@/lib/types";
import { laneCell } from "./BoardLanes";
import { GroupOptionsMenu } from "./GroupOptionsMenu";
import { boardGroupValueIds, isBoardSubGroupProperty, type BoardMainGroupProperty } from "./boardGrouping";
import type { BoardBucket, BoardOption } from "./boardViewModel";
import {
  boardFoldedSubgroups, boardSubgroupConfig, boardSubgroupConfigAfterFold, boardSubgroupMode, boardSubgroupSettings, boardSubgroupLanes,
  boardSubgroupOrderType, viewConfigWithSubgroup,
} from "./boardSubgroupModel";
import { newNamedOption } from "./optionNameText";
import { tableConfigAfterGroupKept, tableConfigAfterGroupShown } from "./tableGroupSettingsModel";
import { useKeepNewGroup } from "./useKeepNewGroup";
import { valueAsRead } from "./valueAsRead";
import { viewGroupAddedKey, viewGroupAddKind } from "./viewGroupAddKind";
import type { BoardViewOperationContext, BoardViewOperations, BoardViewSnapshot } from "./BoardView";

type LaneMenu = { lane: BoardBucket<GraphRow>; position: PopoverRect; returnTo: HTMLElement };

/**
 * A board's sub-groups (BoardView): the property its rows are split by, its
 * lanes across the columns in the order and with the groups its settings
 * say, each card's lanes, a lane's fold, a new lane, a lane's "⋯" menu and
 * the cards of one column's cell. Its hooks run in the board's order, after
 * its columns.
 */
export function useBoardLanes({ props, view, snapshot, t, activeGroupProperty, cardRows, columns, shownColumns, columnKeysOf, ownFolds, setOwnFolds,
  readOnly, structureLocked, addsGroups, laneMenu, setLaneMenu, writeView, runMutation, operations }: {
  props: readonly DbProperty[]; view: DbView; snapshot: BoardViewSnapshot; t: TFunction<["boardView", "common", "databaseView", "propertyTypes"]>;
  activeGroupProperty: BoardMainGroupProperty; cardRows: readonly GraphRow[]; columns: BoardBucket<GraphRow>[]; shownColumns: BoardBucket<GraphRow>[];
  columnKeysOf(row: GraphRow): readonly string[]; ownFolds: ReadonlySet<string> | null; setOwnFolds: Dispatch<SetStateAction<ReadonlySet<string> | null>>;
  readOnly: boolean; structureLocked: boolean; addsGroups: boolean; laneMenu: LaneMenu | null; setLaneMenu: Dispatch<SetStateAction<LaneMenu | null>>;
  writeView(config: GraphView["config"], step?: boolean): Promise<unknown>;
  runMutation<Result>(requestKey: string, perform: (context: BoardViewOperationContext) => Promise<Result>): Promise<unknown>;
  operations: BoardViewOperations;
}) {
  const subgroupProperty = props.find((property) => property.id === view.config?.subGroupBy
    && isBoardSubGroupProperty(property)) as BoardMainGroupProperty | undefined;
  const subgroup = boardSubgroupConfig(snapshot.view.config);
  // Its sub-groups, kept, and followed for the cards an edit that arrives changes (boardSubgroupModel.ts).
  const subLanes = subgroupProperty ? boardSubgroupLanes({ cards: cardRows, columns, shownColumns, groupType: activeGroupProperty.type, columnKeysOf,
    property: subgroupProperty, graphProperty: snapshot.properties.find((candidate) => candidate.id === subgroupProperty.id)!, bounds: snapshot.groupingBounds,
    valueContext: { config: subgroup, properties: snapshot.properties, pagesById: snapshot.propertyValueContext.pagesById,
      propsByDatabaseId: snapshot.propertyValueContext.propsByDatabaseId }, currentUserId: snapshot.currentUserId,
    emptyLabel: t("boardView:noValue", { groupName: subgroupProperty.name }), mode: boardSubgroupMode(subgroup) }) : null;
  const subValues = subLanes?.values ?? null;
  // Split by the group's own property, a card's column says its sub-group too.
  const ownSubgroup = subgroupProperty?.id === activeGroupProperty.id;
  // A sub-group splits the board into rows across its columns, as Notion's,
  // in the order and with the groups its settings say.
  const laneMembership = (row: GraphRow) => subValues ? subValues.keysOf(row) : subgroupProperty ? boardGroupValueIds(subgroupProperty,
    valueAsRead(row, subgroupProperty, row.values[subgroupProperty.id]?.value), snapshot.groupingBounds, boardSubgroupMode(subgroup)) : [];
  const lanes = subgroupProperty && subLanes ? boardSubgroupSettings(subLanes.lanes, subgroup, boardSubgroupOrderType(subgroupProperty, snapshot.properties,
    snapshot.propertyValueContext.propsByDatabaseId)).flatMap(({ lane, shown }) => shown ? [lane] : []) : null;
  const laneFolds = ownFolds ?? boardFoldedSubgroups(subgroup);
  function foldLane(key: string, folded: boolean) {
    const next = boardSubgroupConfigAfterFold({ ...subgroup, collapsedGroups: [...laneFolds] }, key, folded);
    if (readOnly || structureLocked) { setOwnFolds(boardFoldedSubgroups(next)); return; }
    void writeView(viewConfigWithSubgroup(snapshot.view.config, boardSubgroupConfigAfterFold(subgroup, key, folded)), false);
  }
  // "+ 신규 그룹" under the sub-groups adds what they are grouped by, as the columns' (viewGroupAddKind; Notion's, seen
  // 2026-11-12: a title's, text's, URL's, person's or related page's too, not a status's or first letters'), shown though empty.
  const laneOptions = subgroupProperty?.type === "select" || subgroupProperty?.type === "multi_select" ? (subgroupProperty.config?.options ?? []) as BoardOption[] : null;
  const laneAdd = addsGroups && subgroupProperty ? viewGroupAddKind(subgroupProperty, subgroup) : undefined;
  const laneKeyOf = (value: string) => laneAdd === "person" || laneAdd === "option" || !laneAdd ? value : viewGroupAddedKey(laneAdd, value);
  const keepLane = useKeepNewGroup((key) => void writeView(viewConfigWithSubgroup(snapshot.view.config, tableConfigAfterGroupKept(subgroup, key)), false));
  function addLane(name: string) {
    if (laneAdd !== "option") return void (laneAdd && keepLane(laneKeyOf(name)));
    if (!subgroupProperty || !laneOptions) return;
    const option = newNamedOption(name);
    void runMutation(`group-options:${subgroupProperty.id}`, (context) => operations.updateGroupOptions(subgroupProperty.id, [...laneOptions, option], context))
      .then(() => keepLane(option.id));
  }
  // A sub-group's "⋯" menu does to it what a column's does: its settings,
  // its count (hidden until shown, as Notion's were), hiding it, its cards
  // to the trash, and an option's color (Notion's, seen 2026-11-04).
  const laneCounts = new Set(Array.isArray(subgroup.shownGroupCounts) ? subgroup.shownGroupCounts.map(String) : []);
  // An option's group offers its colors under "색", a select's or multi-select's; a status's doesn't (Notion's, seen 2026-11-06).
  const laneColors = subgroupProperty && !subValues && ["select", "multi_select"].includes(subgroupProperty.type)
    ? (subgroupProperty.config?.options ?? []) as BoardOption[] : null;
  function writeSubgroup(next: Record<string, unknown>) {
    void writeView(viewConfigWithSubgroup(snapshot.view.config, next));
  }
  const toggled = (keys: Iterable<string>, key: string) => { const next = new Set(keys); if (next.has(key)) next.delete(key); else next.add(key); return next.size ? [...next] : undefined; };
  const laneMenuElement = laneMenu && subgroupProperty ? <GroupOptionsMenu name={laneMenu.lane.label} position={laneMenu.position} returnTo={laneMenu.returnTo}
    countShown={laneCounts.has(laneMenu.lane.key)} structureLocked={structureLocked} color={laneMenu.lane.color}
    onColor={laneColors?.some((option) => option.id === laneMenu.lane.key) ? (color) => { setLaneMenu(null); void runMutation(`group-options:${subgroupProperty.id}`, (context) => operations.updateGroupOptions(subgroupProperty.id,
      laneColors.map((option) => option.id === laneMenu.lane.key ? { ...option, color, colorFromGroup: undefined } : option), context)); } : undefined}
    onClose={() => setLaneMenu(null)} onEdit={() => { const trigger = laneMenu.returnTo; setLaneMenu(null); operations.openGroupSettings?.(trigger, true); }}
    onToggleCount={() => { setLaneMenu(null); if (!readOnly && !structureLocked) writeSubgroup({ ...subgroup, shownGroupCounts: toggled(laneCounts, laneMenu.lane.key) }); }}
    onHide={() => { setLaneMenu(null); if (!structureLocked) writeSubgroup(tableConfigAfterGroupShown(subgroup, laneMenu.lane.key, false)); }}
    onTrash={operations.trashRows ? () => { const ids = laneMenu.lane.rows.map((row) => row.id); setLaneMenu(null);
      if (ids.length) void runMutation(`group-trash:${laneMenu.lane.key}`, (context) => operations.trashRows!(ids, context)); } : undefined} /> : null;
  const laneRows = (columnKey: string, laneKey: string | undefined) => {
    const rows = columns.find((candidate) => candidate.key === columnKey)?.rows ?? [];
    return laneKey === undefined || !lanes || !subLanes ? rows : laneCell(rows, laneKey, subLanes.keysOf);
  };
  return { subgroupProperty, subgroup, subLanes, subValues, ownSubgroup, laneMembership, lanes, laneFolds, foldLane, laneOptions, laneAdd, laneKeyOf,
    addLane, laneCounts, laneMenuElement, laneRows };
}
