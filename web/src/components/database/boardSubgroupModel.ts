import type { GraphProperty, GraphRow } from "@/contentGraph";
import { activeDateLocale } from "@/lib/i18n";
import { personLabel } from "@/lib/peopleDirectory";
import { keptItems } from "@/lib/rowsReplaced";
import type { DbProperty, Page } from "@/lib/types";
import { boardLanes } from "./boardColumns";
import { boardGroupOptions, boardGroupValueIds, type BoardGroupingBounds, type BoardMainGroupProperty } from "./boardGrouping";
import { projectBoardColumns, type BoardBucket, type BoardOption } from "./boardViewModel";
import { boardValueGroups, isBoardValueGroupProperty, type BoardValueGroups } from "./boardValueGrouping";
import { valueAsRead } from "./valueAsRead";
import { groupsByComputedDates } from "./computedDateValue";
import { listedViewGroups, tableHiddenGroupKeys, tableHidesEmptyGroups, tableKeptGroupKeys, viewGroupOrderIndex } from "./tableGroupSettingsModel";
import { NO_BOARD_GROUP, noValueLeadsUnordered, tableGroupSort } from "./tableGrouping";
import { optionGroupsStartOrder } from "./optionGroupOrder";
import { formulaValueTypeOf } from "./formulaTypes";
import { formulaPropertyTypes } from "./formulaPropertyTypes";

type ViewConfigLike = Readonly<Record<string, unknown>>;

export type BoardSubgroupSort = "manual" | "ascending" | "descending";

const DATED = new Set(["date", "created_time", "last_edited_time"]);
// The types whose sub-groups sort by value alone.
const VALUE_ORDERED = new Set([...DATED, "number"]);

/**
 * A board's sub-group settings, as Notion's "하위 그룹화" (seen 2026-10-11),
 * kept under the view's `subGroup` with the keys its groups use: the groups
 * hidden, their order by hand, how they sort, whether empty ones hide, the
 * ones made here, which show while empty, and the ones folded.
 */
export function boardSubgroupConfig(config: ViewConfigLike | undefined): Record<string, unknown> {
  const sub = config?.subGroup;
  return sub && typeof sub === "object" && !Array.isArray(sub) ? sub as Record<string, unknown> : NO_SUBGROUP_CONFIG;
}
// The same while none is kept, so that what is kept for the sub-groups it makes stays.
const NO_SUBGROUP_CONFIG: Record<string, unknown> = Object.freeze({});

/** The view's config with these sub-group settings, leaving out what is unset. */
export function viewConfigWithSubgroup(config: ViewConfigLike, sub: ViewConfigLike) {
  const kept = Object.fromEntries(Object.entries(sub).filter(([, value]) => value !== undefined));
  const next: Record<string, unknown> = { ...config, subGroup: kept };
  if (!Object.keys(kept).length) delete next.subGroup;
  return next;
}

/**
 * How a board's sub-groups sort, by the type their order goes by
 * (boardSubgroupOrderType), as Notion's: as the view's groups do
 * (tableGroupSort) — a text's, a relation's, a formula's or rollup's "알파벳순"
 * at first, a status's in its own order (seen 2026-11-12, picked on the
 * screen: "알파벳순"/"오름차순" and ascending through its API), an option's by
 * hand, a person's and a box's by hand alone, offering no sort — but a
 * number's and a date's by value alone, never by hand ("오름차순 · 내림차순",
 * listed without handles).
 */
export function boardSubgroupSort(sub: ViewConfigLike, propertyType = ""): BoardSubgroupSort {
  if (VALUE_ORDERED.has(propertyType)) return sub.groupSort === "descending" ? "descending" : "ascending";
  return tableGroupSort(sub as Record<string, unknown>, propertyType);
}

/** Whether the sub-groups of the type sort by value alone, listed without handles. */
export function boardSubgroupsByValueAlone(orderType: string) {
  return VALUE_ORDERED.has(orderType);
}

/**
 * Whether the sub-groups of the type offer no sort, going by hand alone as
 * the view's groups do (tableGroupSort): a person's, who made or last
 * edited a card, a box's (seen 2026-11-12).
 */
export function boardSubgroupsUnsorted(orderType: string) {
  return tableGroupSort({ groupSort: "ascending" }, orderType) === "manual";
}

/** A status's sub-groups are its options until "상태 표시 기준" shows its groups (seen 2026-11-04). */
export function boardSubgroupMode(sub: ViewConfigLike) {
  return sub.groupByMode === "group" ? "group" as const : "option" as const;
}

export function boardFoldedSubgroups(sub: ViewConfigLike): ReadonlySet<string> {
  return new Set(Array.isArray(sub.collapsedGroups) ? sub.collapsedGroups.map(String) : []);
}

export function boardSubgroupConfigAfterFold(sub: ViewConfigLike, key: string, folded: boolean) {
  const rest = [...boardFoldedSubgroups(sub)].filter((candidate) => candidate !== key);
  const collapsedGroups = folded ? [...rest, key] : rest;
  return { ...sub, collapsedGroups: collapsedGroups.length ? collapsedGroups : undefined };
}

/**
 * The sub-groups a board's cards fall in: each option of the property, or
 * each person its cards name (`peopleIn`, its cards unless given), or a
 * checkbox's two; then values it no longer has and the cards without one.
 * By what its cards hold — a title, text, relation — the groups those make
 * (`values`).
 */
export function projectBoardSubgroupLanes<Row extends GraphRow>({
  property, rows, bounds, emptyLabel, currentUserId, values, mode = "option", peopleIn, keptPeople = [],
}: {
  property: BoardMainGroupProperty;
  rows: readonly Row[];
  bounds: BoardGroupingBounds;
  emptyLabel: string;
  currentUserId?: string;
  values?: BoardValueGroups | null;
  mode?: "group" | "option";
  peopleIn?: readonly Row[];
  /** The people added from "+ 신규 그룹", after those the cards name, standing though none does. */
  keptPeople?: readonly string[];
}): BoardBucket<Row>[] {
  // When or by whom a card was made or last edited every card has: no group stands for none unless a card does.
  if (values) return projectBoardColumns<Row>({ rows, options: values.options, membership: (row) => values.keysOf(row), emptyLabel })
    .filter((lane) => values.emptyGroup || lane.key !== NONE || lane.rows.length > 0);
  const membership = (row: Row) => boardGroupValueIds(property,
    valueAsRead(row, property, row.values[property.id]?.value), bounds, mode);
  const checkbox = property.type === "checkbox";
  const status = property.type === "status";
  const options: BoardOption[] = checkbox ? [{ id: "true", name: property.name }, { id: "false", name: property.name }]
    : property.type === "person" ? [...new Set([...(peopleIn ?? rows).flatMap(membership), ...keptPeople])].map((id) => ({ id, name: personLabel(id, currentUserId, false) }))
    : status ? boardGroupOptions(property, [], bounds, currentUserId, mode) as BoardOption[]
    : (property.config?.options ?? []) as BoardOption[];
  return projectBoardColumns({ rows, options, membership, emptyLabel, includeEmpty: !checkbox })
    // A status always has a value, so no group stands for none unless a row does.
    .filter((lane) => !status || lane.key !== "__none" || lane.rows.length > 0)
    .map((lane) => checkbox ? { ...lane, checked: lane.key === "true" } : status && !lane.statusStage ? { ...lane, statusOption: true } : lane);
}

// The main groups a card falls in one of, and the values a card's groups
// read alone, so that a card changed moves between them by itself
// (boardColumns.ts READ_ALONE).
const ONE_COLUMN = new Set(["title", "rich_text", "text", "number", "date", "select", "status", "checkbox", "url", "email", "phone_number",
  "created_time", "last_edited_time"]);
const VALUES_ALONE = new Set(["title", "rich_text", "text", "number", "date", "url", "email", "phone_number", "created_time", "last_edited_time"]);
const NONE = "__none";
const rowId = (row: GraphRow) => row.id;
const shownKept = new WeakMap<object, Map<string, WeakMap<readonly GraphRow[], readonly GraphRow[]>>>();

export type BoardSubgroupLanes = {
  lanes: BoardBucket<GraphRow>[];
  /** Its groups by what its cards hold, when it is grouped so. */
  values: BoardValueGroups | null;
  /** The keys of the sub-groups a card is in (boardColumns.ts bucketKeys). */
  keysOf(row: GraphRow): readonly string[];
};

/**
 * The sub-groups of a board's cards in its shown columns: grouped over all
 * its cards, kept for them and followed for those an edit that arrives
 * changes (boardColumns.ts boardLanes) while their groups read a card
 * alone; then, while columns not shown hold cards, each sub-group's cards
 * in a shown column, followed alike (2026-11-11: a board sub-grouped by
 * values or people, with cards in several columns or in hidden ones,
 * grouped every card again each time it drew).
 */
export function boardSubgroupLanes(input: Omit<Parameters<typeof projectBoardSubgroupLanes<GraphRow>>[0], "rows" | "values" | "peopleIn"> & {
  graphProperty: GraphProperty;
  /** What its groups by values read (boardValueGroups). */
  valueContext: { config: Record<string, unknown>; properties: readonly GraphProperty[]; pagesById?: Readonly<Record<string, Page>>;
    propsByDatabaseId?: Readonly<Record<string, readonly DbProperty[]>> };
  cards: readonly GraphRow[];
  columns: readonly BoardBucket<GraphRow>[];
  shownColumns: readonly BoardBucket<GraphRow>[];
  groupType: string;
  /** The keys of the board's columns a card is in (boardColumns.ts). */
  columnKeysOf(row: GraphRow): readonly string[];
}): BoardSubgroupLanes {
  const { property, cards, bounds, currentUserId, emptyLabel, mode = "option", shownColumns, valueContext } = input;
  const byValues = isBoardValueGroupProperty(property), alone = !byValues || VALUES_ALONE.has(property.type);
  const status = property.type === "status", person = property.type === "person";
  const shownKeys = shownColumns.map((column) => column.key).join("\n");
  const keptPeople = person ? [...tableKeptGroupKeys(valueContext.config)] : [];
  const inputs = [property, bounds, currentUserId, emptyLabel, mode, person ? shownKeys : undefined, keptPeople.join("\n"),
    ...(byValues ? [input.graphProperty, valueContext.config, valueContext.properties, alone ? undefined : valueContext.pagesById, new Date().toDateString(),
      alone ? undefined : valueContext.propsByDatabaseId] : [])];
  const membership = (row: GraphRow) => boardGroupValueIds(property, valueAsRead(row, property, row.values[property.id]?.value), bounds, mode);
  // A sub-group its cards make goes with its last card: a value's, a
  // person's not added by hand, a value no longer an option's, and a
  // status's for none.
  const made = (lane: BoardBucket<GraphRow>) => lane.key === NONE ? status : byValues || person && !keptPeople.includes(lane.key) || lane.key.startsWith("unknown:");
  const all = boardLanes<BoardValueGroups | null>(cards, inputs, () => {
    const values = byValues ? boardValueGroups(input.graphProperty, cards, {
      config: valueContext.config, properties: valueContext.properties, pagesById: valueContext.pagesById,
      propsByDatabaseId: valueContext.propsByDatabaseId, currentUserId,
    }) : null;
    // A person's sub-groups come in the order the shown columns first name them.
    const peopleIn = person ? [...new Map(shownColumns.flatMap((column) => column.rows).map((row) => [row.id, row])).values()] : undefined;
    return { buckets: projectBoardSubgroupLanes({ property, rows: cards, bounds, emptyLabel, currentUserId, values, mode, peopleIn, keptPeople }),
      membership: values ? values.keysOf : membership, extra: values };
  }, alone ? { membership: (before) => before.extra?.keysOf ?? membership, made } : undefined);
  const result = { lanes: all.buckets as BoardBucket<GraphRow>[], values: all.extra, keysOf: all.keysOf };
  let shownCards = 0;
  for (const column of shownColumns) shownCards += column.rows.length;
  const shown = new Set(shownColumns.map((column) => column.key));
  if (ONE_COLUMN.has(input.groupType) ? shownCards === cards.length : !input.columns.some((column) => column.rows.length > 0 && !shown.has(column.key))) return result;
  let byShown = shownKept.get(input.columnKeysOf);
  if (!byShown) shownKept.set(input.columnKeysOf, byShown = new Map());
  let store = byShown.get(shownKeys);
  if (!store) byShown.set(shownKeys, store = new WeakMap());
  const kept = store, keep = (row: GraphRow) => input.columnKeysOf(row).some((key) => shown.has(key));
  return { ...result, lanes: result.lanes.flatMap((lane) => {
    const rows = keptItems(lane.rows, kept, keep, rowId) as GraphRow[];
    return rows === lane.rows ? [lane] : rows.length || !made(lane) ? [{ ...lane, rows }] : [];
  }) };
}

/**
 * The type a sub-group's order goes by: a formula's or a rollup's that
 * gives dates goes by dates (groupsByComputedDates), as Notion's (seen
 * 2026-11-12: a rollup's "지난 30일 · 어제 · 오늘 · 다음 7일 · 다음 30일"
 * oldest first, not by name), a formula's that gives numbers by numbers
 * ("숫자별", "오름차순", seen 2026-11-12), any other by its own.
 */
export function boardSubgroupOrderType(property: SubgroupProperty | undefined, properties: readonly SubgroupProperty[],
  propsByDb?: Readonly<Record<string, readonly DbProperty[]>>) {
  if (!property) return "";
  if (groupsByComputedDates(property, properties, propsByDb)) return "date";
  return property.type === "formula" && formulaGives(property, properties) === "number" ? "number" : property.type;
}
type SubgroupProperty = { id: string; name: string; type: string; config?: unknown };

/** What a formula gives, a list's items' kind for a list: "number", "text", "date", "boolean" or "unknown". */
export function formulaGives(property: SubgroupProperty, properties: readonly SubgroupProperty[]) {
  const config = (property.config ?? {}) as Record<string, unknown>;
  const result = formulaValueTypeOf(String(config.formula ?? config.expression ?? ""), formulaPropertyTypes(properties));
  return result?.type === "array" ? result.items : result?.type;
}

/** Whether sub-groups of the type go by dates: a date's unit, week start and oldest or newest first. */
export function boardSubgroupsDated(orderType: string) {
  return DATED.has(orderType);
}

/**
 * A board's sub-groups in their order, as Notion's (seen 2026-10-11): by
 * hand, the order kept by hand, a group it doesn't name yet coming before
 * the cards without a value; sorted, A to Z or Z to A, a status's in its own
 * order or the reverse. The cards without a value come first while they
 * sort, either way, as a board's columns and a table's groups
 * (noValueGroupLeads, seen 2026-11-08 and, for a text's, a relation's and a
 * formula's sub-groups, 2026-11-12); by hand where the order puts them, else
 * last, as a person's — a sub-grouping by options with no order kept yet
 * goes as a new one starts, by name with them as "uncategorized"
 * (optionGroupsStartOrder, seen 2026-11-24), and a text's made by hand with
 * no order by Notion's API or an MCP tool leaves them first
 * (noValueLeadsUnordered, seen 2026-11-12). Groups by what cards hold come as their groups are made
 * (boardValueGroups, by these settings): text and related pages by name
 * either way with "기타" last, a number's and a date's by value, never by
 * hand (propertyType the order type, boardSubgroupOrderType).
 */
export function orderBoardSubgroups<Lane extends { key: string; label: string; statusOption?: boolean; statusStage?: unknown }>(lanes: readonly Lane[], sub: ViewConfigLike,
  propertyType = ""): Lane[] {
  const status = lanes.some((lane) => lane.statusOption || lane.statusStage);
  const type = status ? "status" : propertyType;
  const sort = boardSubgroupSort(sub, type);
  const named = lanes.filter((lane) => lane.key !== NO_BOARD_GROUP);
  const none = lanes.filter((lane) => lane.key === NO_BOARD_GROUP);
  if (sort === "manual") {
    const order = Array.isArray(sub.groupOrder) ? sub.groupOrder.map(String) : optionGroupsStartOrder(type, lanes, NO_BOARD_GROUP) ?? [];
    const noneAt = viewGroupOrderIndex(order, NO_BOARD_GROUP);
    const noneFirst = noValueLeadsUnordered(sub, type);
    const at = (key: string) => {
      const index = viewGroupOrderIndex(order, key);
      if (index >= 0) return index;
      if (key === NO_BOARD_GROUP) return noneFirst ? -1 : Number.MAX_SAFE_INTEGER;
      return noneAt >= 0 ? noneAt - 0.5 : Number.MAX_SAFE_INTEGER - 1;
    };
    return [...lanes].sort((left, right) => at(left.key) - at(right.key));
  }
  // An option's by name; the rest come in their own order, which sorting has made.
  if (BY_NAME.has(type)) named.sort((left, right) => left.label.localeCompare(right.label, activeDateLocale(), { numeric: true, sensitivity: "base" }));
  return [...none, ...(sort === "descending" && (status || BY_NAME.has(type)) ? named.reverse() : named)];
}
// The sub-groups by options sorted by their names.
const BY_NAME = new Set(["select", "multi_select"]);

/**
 * The sub-groups as the settings list them, in order with the hidden ones
 * last (listedViewGroups): each says whether it shows, which an empty one
 * doesn't while empty ones hide unless it was made here.
 */
export function boardSubgroupSettings<Lane extends { key: string; label: string; rows: readonly unknown[]; statusOption?: boolean; statusStage?: unknown }>(
  lanes: readonly Lane[], sub: ViewConfigLike, propertyType = "",
) {
  const hidden = tableHiddenGroupKeys(sub);
  const kept = tableKeptGroupKeys(sub);
  const hidesEmpty = tableHidesEmptyGroups(sub);
  const items = orderBoardSubgroups(lanes, sub, propertyType).map((lane) => {
    const hiddenAsEmpty = hidesEmpty && lane.rows.length === 0 && !kept.has(lane.key);
    return { lane, hiddenAsEmpty, shown: !hidden.has(lane.key) && !hiddenAsEmpty };
  });
  return listedViewGroups(items, (item) => item.shown);
}
