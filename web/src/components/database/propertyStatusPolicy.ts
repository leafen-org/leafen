import type { PropertyConfig, SelectOption, StatusGroup } from "@/lib/types";
import { statusDefaultOptionId } from "@/lib/statusDefaultValue";

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function nonempty(value: unknown) {
  return typeof value === "string" && value.trim() ? value.trim() : "";
}

function statusOptionsFromConfig(config: unknown): SelectOption[] {
  const options = record(config)?.options;
  if (!Array.isArray(options)) return [];
  return options.flatMap((value) => {
    const option = record(value);
    const id = nonempty(option?.id);
    const name = nonempty(option?.name);
    if (!option || !id || !name) return [];
    return [{
      ...option,
      id,
      name,
      color: nonempty(option.color) || "default",
    } as SelectOption];
  });
}

function statusGroupsFromConfig(config: unknown): StatusGroup[] {
  const source = record(config);
  const notion = record(source?.notion);
  const notionStatus = record(notion?.status);
  const rawGroups = Array.isArray(source?.statusGroups)
    ? source.statusGroups
    : Array.isArray(notionStatus?.groups)
      ? notionStatus.groups
      : [];
  const seen = new Set<string>();
  // Every group and every option in it, however many.
  return rawGroups.flatMap((value) => {
    const group = record(value);
    const id = nonempty(group?.id);
    const name = nonempty(group?.name);
    if (!group || !id || !name || seen.has(id)) return [];
    seen.add(id);
    const rawOptionIds = Array.isArray(group.optionIds)
      ? group.optionIds
      : Array.isArray(group.option_ids)
        ? group.option_ids
        : [];
    const optionIds = Array.from(new Set(rawOptionIds.map(nonempty).filter(Boolean)));
    return [{
      id,
      name,
      color: nonempty(group.color) || "default",
      optionIds,
    }];
  });
}

export function statusGroupForOption(
  config: unknown,
  optionOrId: SelectOption | string,
): StatusGroup | undefined {
  const groups = statusGroupsFromConfig(config);
  if (groups.length === 0) return undefined;
  const options = statusOptionsFromConfig(config);
  const option = typeof optionOrId === "string"
    ? options.find((candidate) => candidate.id === optionOrId)
    : record(optionOrId);
  const optionId = nonempty(option?.id);
  const explicitGroup = nonempty(option?.group);
  return (
    (explicitGroup ? groups.find((group) => group.name === explicitGroup) : undefined)
    ?? groups.find((group) => optionId && group.optionIds.includes(optionId))
    ?? groups[0]
  );
}

export function statusGroupColumns(config: unknown): StatusGroup[] {
  const groups = statusGroupsFromConfig(config);
  if (groups.length === 0) return [];
  const options = statusOptionsFromConfig(config);
  return groups.map((group) => ({
    ...group,
    optionIds: options
      .filter((option) => statusGroupForOption(config, option)?.id === group.id)
      .map((option) => option.id),
  }));
}

export function statusOptionsInGroupOrder(config: unknown): SelectOption[] {
  const options = statusOptionsFromConfig(config);
  const groups = statusGroupColumns(config);
  if (groups.length === 0) return options;
  const byId = new Map(options.map((option) => [option.id, option]));
  const ordered: SelectOption[] = [];
  const seen = new Set<string>();
  for (const group of groups) {
    for (const id of group.optionIds) {
      const option = byId.get(id);
      if (!option || seen.has(id)) continue;
      seen.add(id);
      ordered.push(option);
    }
  }
  for (const option of options) {
    if (!seen.has(option.id)) ordered.push(option);
  }
  return ordered;
}

type ByOption = { options: SelectOption[]; standIn: ReadonlyMap<string, string> };
// Worked out once a config, as a board places each card by it.
const byOptionKept = new WeakMap<object, ByOption>();

/**
 * A status as a view shown by its options lists and places them: its options
 * in its groups' order less those standing for a whole group ("내 작업"'s own,
 * named as it, which a plain check and its groups' board write), and a value
 * standing for one under its group's first option, or none when the group has
 * no other. Notion's option view has no column, group, choice or pick for a
 * group itself (seen 2026-11-24 on My tasks: its board and list by option,
 * their grouping's list and a card's status pick), and a task given its whole
 * group stands under an option of it (two tabs: checked off by option, and by
 * group then shown by option, under its database's first "완료" option).
 */
function byOption(config: unknown): ByOption {
  const source = record(config);
  const kept = source ? byOptionKept.get(source) : undefined;
  if (kept) return kept;
  const ordered = statusOptionsInGroupOrder(config);
  const standIn = new Map<string, string>();
  if (ordered.some((option) => option.standsForGroup)) {
    const named = new Set(ordered.filter((option) => !option.standsForGroup).map((option) => option.id));
    for (const group of statusGroupColumns(config)) {
      const first = group.optionIds.find((id) => named.has(id)) ?? "";
      for (const id of group.optionIds) if (!named.has(id)) standIn.set(id, first);
    }
  }
  const made = { options: standIn.size ? ordered.filter((option) => !option.standsForGroup) : ordered, standIn };
  if (source) byOptionKept.set(source, made);
  return made;
}

/** A status's options as a view shown by its options lists them: columns, groups, its grouping's list, a filter's choices, a pick (byOption). */
export function statusOptionsByOption(config: unknown): SelectOption[] {
  return byOption(config).options;
}

/** The option a status's value stands under in a view shown by its options: itself, or for a group's own its group's first, "" for none (byOption). */
export function statusOptionByOption(config: unknown, id: string): string {
  return byOption(config).standIn.get(id) ?? id;
}

export function synchronizeStatusGroupMembership(
  config: PropertyConfig | undefined,
  options: SelectOption[],
): PropertyConfig {
  const source = record(config) ?? {};
  const normalizedOptions = Array.isArray(options) ? options.map((option) => ({ ...option })) : [];
  const groups = statusGroupsFromConfig(source);
  if (groups.length === 0) return { ...source, options: normalizedOptions };
  const configWithOptions = { ...source, options: normalizedOptions };
  return {
    ...source,
    options: normalizedOptions,
    statusGroups: groups.map((group) => ({
      ...group,
      optionIds: normalizedOptions
        .filter((option) => statusGroupForOption(configWithOptions, option)?.id === group.id)
        .map((option) => option.id),
    })),
  };
}

/**
 * Whether an option is its status's default, which rows that set none read:
 * Notion keeps it from being deleted (seen 2026-11-08: its settings say the
 * default option can't be deleted; a status with no default, made from a select,
 * lets each go).
 */
export function isStatusDefaultOption(config: unknown, optionId: string) {
  return statusDefaultOptionId((record(config) ?? {}) as Record<string, unknown>) === optionId;
}

function statusGroupIdForValue(config: unknown, value: unknown) {
  const id = nonempty(value);
  if (!id) return undefined;
  return statusGroupForOption(config, id)?.id;
}

const STANDARD_STATUS_GROUPS: Record<string, string> = {
  "to-do": "statusGroups.todo", "in progress": "statusGroups.inProgress", complete: "statusGroups.complete",
};

/**
 * The label key a status group's standard name reads by in the reader's
 * language, as Notion names "To-do" "할 일" in Korean (seen 2026-10-08); a
 * group someone named keeps its name.
 */
export function statusGroupLabelKey(name: string): string | undefined {
  return STANDARD_STATUS_GROUPS[name.trim().toLowerCase()];
}

/**
 * Where a status group stands, which Notion marks it by (seen 2026-10-24):
 * the first group is To-do, the last Complete, any between In progress.
 */
export type StatusStage = "todo" | "inProgress" | "complete";

export function statusStageOf(index: number, count: number): StatusStage {
  return index <= 0 ? "todo" : index >= count - 1 ? "complete" : "inProgress";
}

/**
 * A status shown as a checkbox, as Notion's (seen 2026-10-11): checked in its
 * last group ("Complete"), half in one between, empty in its first.
 */
export function statusCheckboxState(config: unknown, value: unknown): "checked" | "mixed" | "unchecked" {
  const groups = statusGroupColumns(config);
  const group = statusGroupIdForValue(config, Array.isArray(value) ? value[0] : value);
  if (!group || groups.length < 2 || group === groups[0].id) return "unchecked";
  return group === groups.at(-1)?.id ? "checked" : "mixed";
}

/**
 * The status a click on its checkbox sets, as Notion's (seen 2026-10-11): an
 * unfinished one becomes its last group's first option; a finished one its
 * default, or its first group's first option when the default is finished too.
 */
export function statusAfterCheckboxClick(config: unknown, value: unknown): string | undefined {
  const groups = statusGroupColumns(config);
  if (statusCheckboxState(config, value) !== "checked") return groups.at(-1)?.optionIds[0];
  const fallback = statusDefaultOptionId((record(config) ?? {}) as Record<string, unknown>);
  return fallback && statusCheckboxState(config, fallback) !== "checked" ? fallback : groups[0]?.optionIds[0];
}
