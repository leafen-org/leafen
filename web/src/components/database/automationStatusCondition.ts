import type { DbProperty } from "@/lib/types";
import { statusGroupColumns, statusOptionsInGroupOrder, statusStageOf, type StatusStage } from "./propertyStatusPolicy";
import type { TriggerOperator } from "./automationTriggerConditions";

/**
 * A status trigger's value, as Notion's (seen 2026-11-11 on a synthetic
 * database, 1975px dark): no operator to pick, its groups ticked whole or by
 * their options. All of them ticked is any edit ("상태 속성 편집"), none is
 * the status cleared, else "is" some — a group
 * ticked whole kept as the group, so an option added to it later counts too
 * (the server reads it as its options as it runs,
 * database_automation_trigger_status.go), the rest as options.
 */
export type StatusConditionValue = { operator?: TriggerOperator; value?: unknown };
export type StatusTick = "on" | "mixed" | "off";

type Option = { id: string; name: string; color?: string };
export type StatusConditionGroup = {
  /** "" for a status with no groups, whose options stand alone. */
  id: string;
  name: string;
  color?: string;
  stage: StatusStage;
  options: Option[];
  /** What ticks it whole: its options, or the group itself while it has none. */
  members: string[];
};

export function statusConditionGroups(property: DbProperty): StatusConditionGroup[] {
  const options = statusOptionsInGroupOrder(property.config)
    .filter((option) => typeof option?.id === "string")
    .map(({ id, name, color }) => ({ id, name: name ?? "", color }));
  const groups = statusGroupColumns(property.config);
  if (!groups.length) return [{ id: "", name: "", stage: "todo", options, members: options.map(({ id }) => id) }];
  return groups.map((group, index) => {
    const own = options.filter((option) => group.optionIds.includes(option.id));
    return { id: group.id, name: group.name, color: group.color, stage: statusStageOf(index, groups.length), options: own,
      members: own.length ? own.map(({ id }) => id) : [group.id] };
  });
}

const tickOf = (ids: readonly string[], ticked: ReadonlySet<string>): StatusTick => {
  const on = ids.filter((id) => ticked.has(id)).length;
  return on && on === ids.length ? "on" : on ? "mixed" : "off";
};

/** What a condition's list names: each option, and a group's members for the group. */
function named(groups: readonly StatusConditionGroup[], list: readonly unknown[]) {
  const options = new Set(groups.flatMap((group) => group.options.map(({ id }) => id)));
  const ids = new Set<string>();
  for (const item of list) {
    const id = String(item);
    // An option's ID wins over a group's, as the server reads them.
    const group = options.has(id) ? undefined : groups.find((candidate) => candidate.id && candidate.id === id);
    for (const member of group ? group.members : [id]) ids.add(member);
  }
  return ids;
}

/** The ticks a condition shows: all for any edit, none once cleared, the complement of what "is not" names. */
export function statusConditionTicks(groups: readonly StatusConditionGroup[], condition: StatusConditionValue): Set<string> {
  const universe = groups.flatMap((group) => group.members);
  if (!condition.operator) return new Set(universe);
  if (condition.operator === "cleared") return new Set();
  if (!Array.isArray(condition.value)) return new Set(universe);
  const ids = named(groups, condition.value);
  return new Set(universe.filter((id) => condition.operator === "is_not" ? !ids.has(id) : ids.has(id)));
}

/** The condition ticks make: any edit, cleared, or "is" each group ticked whole and the other ticked options. */
export function statusConditionOf(groups: readonly StatusConditionGroup[], ticked: ReadonlySet<string>): StatusConditionValue {
  const universe = groups.flatMap((group) => group.members);
  const all = tickOf(universe, ticked);
  if (all === "on") return {};
  if (all === "off") return { operator: "cleared" };
  return { operator: "is", value: groups.flatMap((group) => group.id && tickOf(group.members, ticked) === "on"
    ? [group.id] : group.members.filter((id) => ticked.has(id))) };
}

/** A status condition's window: "모든 옵션", each group and each option, ticked; each press gives the next condition. */
export function statusConditionChoices(property: DbProperty, condition: StatusConditionValue) {
  const groups = statusConditionGroups(property);
  const ticked = statusConditionTicks(groups, condition);
  const universe = groups.flatMap((group) => group.members);
  const next = (change: (draft: Set<string>) => void) => {
    const draft = new Set(ticked);
    change(draft);
    return statusConditionOf(groups, draft);
  };
  const all = tickOf(universe, ticked);
  return {
    groups,
    all,
    group: (group: StatusConditionGroup) => tickOf(group.members, ticked),
    option: (id: string) => ticked.has(id),
    // A half-ticked "모든 옵션" ticks all, as Notion's.
    toggleAll: () => all === "on" ? statusConditionOf(groups, new Set()) : statusConditionOf(groups, new Set(universe)),
    toggleGroup: (group: StatusConditionGroup) => next((draft) => {
      const on = tickOf(group.members, ticked) === "on";
      for (const id of group.members) if (on) draft.delete(id); else draft.add(id);
    }),
    toggleOption: (id: string) => next((draft) => { if (!draft.delete(id)) draft.add(id); }),
  };
}

/**
 * What a status condition's card and list name, in its window's order: a
 * group ticked whole by its own name and color, as Notion's ("[Complete]",
 * the name as the group has it), else its options; what no longer exists by
 * its ID.
 */
export function statusConditionItems(property: DbProperty, list: readonly unknown[]): Array<Option & { group?: boolean }> {
  const groups = statusConditionGroups(property);
  const ids = named(groups, list);
  const items: Array<Option & { group?: boolean }> = [];
  const known = new Set<string>();
  for (const group of groups) {
    for (const member of group.members) known.add(member);
    if (group.id && tickOf(group.members, ids) === "on") items.push({ id: group.id, name: group.name, color: group.color, group: true });
    else items.push(...group.options.filter(({ id }) => ids.has(id)));
  }
  return [...items, ...[...ids].filter((id) => !known.has(id)).map((id) => ({ id, name: id }))];
}
