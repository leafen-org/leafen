import {
  compareTableGroups, isNoValueGroupKey, NO_BOARD_GROUP, NO_TABLE_GROUP, tableGroupsFollowHandOrder, type TableGroupDescriptor,
} from "./tableGrouping";
import { RELATIVE_SPRINTS } from "@/shared/database/queryFilterPolicy";
import { NO_LAST_SPRINT, relativeSprintIds } from "./databaseSprintContext";
import { statusDefaultOptionId } from "@/lib/statusDefaultValue";
import { formulaValueTypeOf } from "./formulaTypes";
import { keptGroupSeed } from "./viewGroupAddKind";
import { boardOptionGroupOrder } from "./optionGroupOrder";

type ViewConfigLike = Readonly<Record<string, unknown>>;
// The properties every row has a value in.
const ALWAYS_VALUED = new Set(["created_time", "last_edited_time", "created_by", "last_edited_by"]);

/**
 * Whether every row has a value in a property of the type — when and by
 * whom it was made or last edited — so that no group of a view stands for
 * none (Notion's table, board and sub-groups, seen 2026-11-12: "만든이" with
 * no "만든이 없음", empty groups shown or not).
 */
export function propertyAlwaysValued(propertyType: string) {
  return ALWAYS_VALUED.has(propertyType);
}
type Side = "before" | "after";

function strings(value: unknown): string[] | undefined {
  return Array.isArray(value) ? value.map(String) : undefined;
}

/** Whether two keys name one group: the same key, or the group for no value under a table's and a board's name. */
function sameGroupKey(left: string, right: string) {
  return left === right || (isNoValueGroupKey(left) && isNoValueGroupKey(right));
}

/** Where a view's order kept by hand puts a group, -1 where it doesn't name it. */
export function viewGroupOrderIndex(order: readonly string[], key: string) {
  return order.findIndex((candidate) => sameGroupKey(candidate, key));
}

/** Whether a table leaves out its groups without rows: it does unless told not to, as Notion's (seen 2026-09-30). */
export function tableHidesEmptyGroups(config: ViewConfigLike) {
  return config.hideEmptyGroups !== false;
}

/** The groups a view hides by hand, the group for no value under either name. */
export function tableHiddenGroupKeys(config: ViewConfigLike): ReadonlySet<string> {
  const keys = new Set(strings(config.hiddenGroups) ?? []);
  if ([...keys].some(isNoValueGroupKey)) for (const key of [NO_TABLE_GROUP, NO_BOARD_GROUP]) keys.add(key);
  return keys;
}

/**
 * A view's groups as its grouping settings list them, as Notion's table,
 * board and sub-groups (seen 2026-11-08): those it shows in its order, then
 * those it hides — by hand, or while empty groups hide — in its order too.
 */
export function listedViewGroups<Group>(groups: readonly Group[], shown: (group: Group) => boolean): Group[] {
  return [...groups.filter(shown), ...groups.filter((group) => !shown(group))];
}

/**
 * The order a view's groups keep once its "정렬" is set to by hand: as they
 * stand, as Notion's — from A to Z the group for no value still first (seen
 * 2026-11-12: a text's on a table and a board's sub-groups, "글 없음"), from
 * Z to A then last (seen 2026-11-08: a relation's, "관계 없음" went from
 * first to last; 2026-11-12 a sub-group's alike).
 */
export function groupOrderByHand(keys: readonly string[], descending = false) {
  return descending ? keys.filter((key) => !isNoValueGroupKey(key)) : [...keys];
}

/** Groups made from the table, which it shows while still empty, as Notion's (seen 2026-10-06). */
export function tableKeptGroupKeys(config: ViewConfigLike): ReadonlySet<string> {
  return new Set(strings(config.keptGroups) ?? []);
}

/**
 * Keeps a group added by hand. `shown`, the keys in the order the groups
 * stand, puts it last for groups that go by hand alone, a person's: after
 * the group for no one too, as Notion's table and list (seen 2026-11-12).
 */
export function tableConfigAfterGroupKept(config: ViewConfigLike, key: string, shown?: readonly string[]) {
  const kept = { ...config, keptGroups: [...tableKeptGroupKeys(config), key] };
  return shown ? { ...kept, groupOrder: [...(strings(config.groupOrder) ?? shown).filter((candidate) => candidate !== key), key] } : kept;
}

/**
 * The groups a table's property has even without rows, as Notion lists them:
 * an option's or status's groups and then the rows without a value (none for
 * a status with a default), or a checkbox's two. Other properties have only
 * the groups their rows make, and those added by hand (`kept`): a person's,
 * a related page's or a text's from "+ 신규 그룹" (viewGroupAddKind.ts).
 */
export function tableGroupCatalog(propertyType: string, optionIds: readonly string[], grouper: (value: unknown) => TableGroupDescriptor,
  propertyConfig?: unknown, kept: ReadonlySet<string> = new Set()) {
  // A status with a default has no group for none: a row that sets none reads as its default (valueAsRead), as Notion's
  // table and board (seen 2026-11-11); one with no default, made from a select, has it.
  const none = propertyType === "status" && statusDefaultOptionId((propertyConfig ?? {}) as Record<string, unknown>) ? [] : [null];
  // A sprint relation's current, next and last sprints and the backlog show even while empty (Notion's sprint planning, seen 2026-11-04).
  // Another's groups are its rows' values, and the group for none, shown empty where empty groups show (a table made
  // through Notion's API, seen 2026-11-12: "글 없음", "담당 없음"); a property every row has a value in has none.
  // A formula that gives a box, checked or not, has no group for none either (Notion's, seen 2026-11-12).
  const box = propertyType === "checkbox" || propertyType === "formula" && formulaGivesBox(propertyConfig);
  const seeds: unknown[] = box ? [true, false]
    : ["select", "multi_select", "status"].includes(propertyType) ? [...optionIds, ...none]
    : propertyType === "relation" && optionIds.length ? [...optionIds.map((id) => [id]), null]
    : propertyAlwaysValued(propertyType) ? [] : [null];
  const byKey = new Map<string, TableGroupDescriptor>();
  for (const seed of seeds) {
    const group = grouper(seed);
    if (!byKey.has(group.key)) byKey.set(group.key, group);
  }
  for (const key of kept) {
    const seed = keptGroupSeed(propertyType, key);
    const group = seed === undefined ? undefined : grouper(seed);
    // One the grouping now names otherwise (a text by its first letter) is left out.
    if (group?.key === key && !byKey.has(key)) byKey.set(key, group);
  }
  return [...byKey.values()];
}

// Whether a formula's expression gives a box whatever its properties hold, as a comparison does.
function formulaGivesBox(config: unknown) {
  const { formula, expression } = (config ?? {}) as { formula?: unknown; expression?: unknown };
  return formulaValueTypeOf(String(formula ?? expression ?? ""), () => "unknown")?.type === "boolean";
}

/**
 * Orders a table's groups: in the order the view keeps once they were moved
 * by hand, else by what their property holds; groups the order doesn't name
 * follow in that order. An option's by hand with no order kept yet go in the
 * order a view grouped by it starts in (boardOptionGroupOrder, given its
 * property's settings), as Notion's grouped on the screen or by its API —
 * tags x, y, z with no value first, a select's a, b with it last (seen
 * 2026-11-24). A property sorting its groups by value sets the hand order
 * aside. By hand, a group added from "+ 신규 그룹" that the order doesn't name
 * stands after all the others, the group for no value too, in the order
 * they were added (manualAddedLast).
 */
export function orderTableGroups<Group extends { key: string }>(groups: readonly Group[], config: ViewConfigLike,
  describe: (key: string) => TableGroupDescriptor | undefined, propertyType?: string, propertyConfig?: unknown) {
  const byHand = !propertyType || tableGroupsFollowHandOrder(config, propertyType);
  const order = byHand ? strings(config.groupOrder) ?? boardOptionGroupOrder(propertyType ? { type: propertyType, config: propertyConfig } : undefined, NO_TABLE_GROUP) ?? [] : [];
  const added = byHand ? manualAddedLast(config, propertyType) : [];
  const at = (key: string) => {
    const index = viewGroupOrderIndex(order, key);
    if (index >= 0) return index;
    const late = added.indexOf(key);
    return late < 0 ? order.length : order.length + 1 + late;
  };
  return [...groups].sort((left, right) => (at(left.key) - at(right.key)) || compareTableGroups(describe(left.key), describe(right.key)));
}

/**
 * The groups added from "+ 신규 그룹" — a related page's, a person's, a text's —
 * in the order they were added, which a view going by hand puts after its
 * other groups, the group for no value too, as Notion's board (seen
 * 2026-11-12: a relation's "다라, 관계 없음" then "마바" and "가나" as added;
 * a person's after "담당 없음"; a text's "글 없음, 다" then "가"). An option's
 * new group keeps its options' place.
 */
export function manualAddedLast(config: ViewConfigLike, propertyType = "") {
  return ["select", "multi_select", "status"].includes(propertyType) ? [] : strings(config.keptGroups) ?? [];
}

/** Shows or hides one group. */
export function tableConfigAfterGroupShown(config: ViewConfigLike, key: string, show: boolean) {
  const hidden = (strings(config.hiddenGroups) ?? []).filter((candidate) => !sameGroupKey(candidate, key));
  const next: Record<string, unknown> = { ...config, hiddenGroups: show ? hidden : [...hidden, key] };
  if (!(next.hiddenGroups as string[]).length) delete next.hiddenGroups;
  return next;
}

/** Shows every group, or hides all of these. */
export function tableConfigAfterAllGroupsShown(config: ViewConfigLike, keys: readonly string[], show: boolean) {
  const next: Record<string, unknown> = { ...config, hiddenGroups: [...keys] };
  if (show || !keys.length) delete next.hiddenGroups;
  return next;
}

/** Moves a group before or after another, keeping the whole order by hand from then on. */
export function tableConfigAfterGroupMoved(config: ViewConfigLike, orderedKeys: readonly string[], sourceKey: string, targetKey: string, side: Side) {
  if (sourceKey === targetKey || !orderedKeys.includes(sourceKey) || !orderedKeys.includes(targetKey)) return null;
  const order = orderedKeys.filter((key) => key !== sourceKey);
  order.splice(order.indexOf(targetKey) + (side === "after" ? 1 : 0), 0, sourceKey);
  return { ...config, groupOrder: order };
}

/** The option ids a property's groups come from, for `tableGroupCatalog`: none standing for a status's group (statusOptionsByOption). */
export function tableGroupOptionIds(config: unknown): string[] {
  const sprints = config && typeof config === "object" ? (config as { relationDatabaseId?: unknown }).relationDatabaseId : undefined;
  if (sprints !== undefined) {
    const ids = RELATIVE_SPRINTS.flatMap((place) => relativeSprintIds(sprints, place) ?? []);
    return relativeSprintIds(sprints, "last")?.length === 0 ? [...ids, NO_LAST_SPRINT] : ids;
  }
  const raw = config && typeof config === "object" ? (config as { options?: unknown }).options : undefined;
  return (Array.isArray(raw) ? raw : []).flatMap((option) => (
    option && typeof option === "object" && typeof (option as { id?: unknown }).id === "string"
      && (option as { standsForGroup?: unknown }).standsForGroup !== true ? [(option as { id: string }).id] : []
  ));
}

/**
 * A table's groups as its settings list them: those its rows make and the
 * rest its property has, in the table's order, each with its row count.
 */
export function tableGroupsWithCounts(property: { type: string; config?: unknown }, values: readonly unknown[], config: ViewConfigLike,
  grouper: (value: unknown) => TableGroupDescriptor | TableGroupDescriptor[]) {
  const described = new Map<string, TableGroupDescriptor>();
  const counts = new Map<string, number>();
  // A value may fall in several groups (a relation's pages) and counts in each.
  const groupsOf = (value: unknown) => [grouper(value)].flat();
  for (const value of values) {
    for (const group of groupsOf(value)) {
      described.set(group.key, group);
      counts.set(group.key, (counts.get(group.key) ?? 0) + 1);
    }
  }
  for (const group of tableGroupCatalog(property.type, tableGroupOptionIds(property.config), (seed) => groupsOf(seed)[0]!, property.config,
    tableKeptGroupKeys(config))) {
    if (!described.has(group.key)) described.set(group.key, group);
  }
  return orderTableGroups([...described.values()], config, (key) => described.get(key), property.type, property.config)
    .map((group) => ({ ...group, count: counts.get(group.key) ?? 0 }));
}

/**
 * A view's groups as Notion shows them, a table's and a list's alike: the
 * groups its rows make, its empty ones where it is told to show them, less
 * the hidden ones, in the order kept by hand or by what they hold, each
 * named and colored as its property's value is.
 */
export function arrangeViewGroups<Group extends { key: string }>(groups: readonly Group[], { propertyType, propertyConfig, grouper, config, described, empty }: {
  propertyType: string; propertyConfig: unknown; grouper: (value: unknown) => TableGroupDescriptor; config: ViewConfigLike;
  /** What names each group the rows made; the empty ones shown are added to it. */
  described: Map<string, TableGroupDescriptor>;
  empty(key: string): Group;
}) {
  const shown = [...groups];
  const kept = tableKeptGroupKeys(config);
  if (!tableHidesEmptyGroups(config) || kept.size > 0) {
    for (const group of tableGroupCatalog(propertyType, tableGroupOptionIds(propertyConfig), grouper, propertyConfig, kept)) {
      if (described.has(group.key) || (tableHidesEmptyGroups(config) && !kept.has(group.key))) continue;
      described.set(group.key, group);
      shown.push(empty(group.key));
    }
  }
  const hidden = tableHiddenGroupKeys(config);
  return orderTableGroups(shown.filter((group) => !hidden.has(group.key)), config, (key) => described.get(key), propertyType, propertyConfig)
    .map((group) => {
      const descriptor = described.get(group.key);
      return { ...group, label: descriptor?.label, color: descriptor?.color, checkbox: descriptor?.checkbox, page: descriptor?.page,
        statusStage: descriptor?.statusStage, statusOption: descriptor?.statusOption, sprintPlace: descriptor?.sprintPlace, person: descriptor?.person };
    });
}
