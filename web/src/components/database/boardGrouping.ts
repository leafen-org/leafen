import {
  normalizePersonIds,
  personLabel,
  type PersonValueBounds,
} from "@/lib/peopleDirectory";
import type {
  DbProperty,
  Page,
  SelectOption,
  StatusGroup,
  ViewConfig,
} from "@/lib/types";
import { i18next } from "@/i18n";
import { valueAsRead } from "./valueAsRead";
import { statusGroupLabelKey, statusOptionByOption, statusOptionsByOption, statusStageOf, type StatusStage } from "./propertyStatusPolicy";
import { isTableGroupProperty } from "./tableGrouping";
import { statusTakesValue } from "./statusRowOptions";

type LeafenBoardMainGroupPropertyType = "select" | "status" | "person" | "multi_select" | "checkbox";

// A board grouped by a checkbox, as Notion's (seen 2026-10-01), has two
// columns, the checked one first, each named by the property; a card moved
// across checks or clears its box.
const CHECKED_GROUP = "true";
const UNCHECKED_GROUP = "false";

export type BoardMainGroupProperty = DbProperty & {
  type: LeafenBoardMainGroupPropertyType;
};

export type BoardGroupMode = "group" | "option";

export type BoardGroupingBounds = {
  maxRows: number;
  maxMembers: number;
  maxValues: number;
  maxStatusOptions: number;
  maxStatusGroups: number;
  maxStatusOptionIds: number;
  maxPersonNodes: number;
  maxPersonDepth: number;
  maxPeopleEntries: number;
  maxStringCharacters: number;
};

type BoardBudget = { stringCharacters: number };

type StatusProjection = {
  options: Array<SelectOption & { group?: string }>;
  groups: StatusGroup[];
  groupByOptionId: Map<string, StatusGroup>;
};

function validBound(value: number) {
  return Number.isSafeInteger(value) && value >= 0;
}

function validateBounds(bounds: BoardGroupingBounds) {
  for (const bound of [
    bounds.maxRows,
    bounds.maxMembers,
    bounds.maxValues,
    bounds.maxStatusOptions,
    bounds.maxStatusGroups,
    bounds.maxStatusOptionIds,
    bounds.maxPersonNodes,
    bounds.maxPersonDepth,
    bounds.maxPeopleEntries,
    bounds.maxStringCharacters,
  ]) {
    if (!validBound(bound)) throw new RangeError("Invalid board grouping bounds.");
  }
}

function consumeString(value: string, bounds: BoardGroupingBounds, budget: BoardBudget) {
  budget.stringCharacters += value.length;
  if (
    !Number.isSafeInteger(budget.stringCharacters)
    || budget.stringCharacters > bounds.maxStringCharacters
  ) {
    throw new RangeError("Board grouping string bound exceeded.");
  }
}

function record(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined;
}

function nonempty(value: unknown, bounds: BoardGroupingBounds, budget: BoardBudget) {
  if (typeof value !== "string") return "";
  consumeString(value, bounds, budget);
  return value.trim();
}

function statusProjection(
  config: unknown,
  bounds: BoardGroupingBounds,
  budget: BoardBudget,
): StatusProjection {
  const source = record(config);
  const rawOptions = source?.options;
  const optionValues = Array.isArray(rawOptions) ? rawOptions : [];
  if (optionValues.length > bounds.maxStatusOptions) {
    throw new RangeError("Board status option bound exceeded.");
  }
  const options: Array<SelectOption & { group?: string }> = [];
  for (const value of optionValues) {
    const option = record(value);
    const id = nonempty(option?.id, bounds, budget);
    const name = nonempty(option?.name, bounds, budget);
    if (!id || !name) continue;
    const color = nonempty(option?.color, bounds, budget) || "default";
    const group = nonempty(option?.group, bounds, budget) || undefined;
    options.push({ id, name, color, group });
  }

  const notion = record(source?.notion);
  const notionStatus = record(notion?.status);
  const rawGroups = Array.isArray(source?.statusGroups)
    ? source.statusGroups
    : Array.isArray(notionStatus?.groups)
      ? notionStatus.groups
      : [];
  if (rawGroups.length > bounds.maxStatusGroups) {
    throw new RangeError("Board status group bound exceeded.");
  }

  const groups: StatusGroup[] = [];
  const seenGroups = new Set<string>();
  let totalOptionIds = 0;
  for (const value of rawGroups) {
    const group = record(value);
    const id = nonempty(group?.id, bounds, budget);
    const name = nonempty(group?.name, bounds, budget);
    if (!id || !name || seenGroups.has(id)) continue;
    seenGroups.add(id);
    const rawOptionIds = Array.isArray(group?.optionIds)
      ? group.optionIds
      : Array.isArray(group?.option_ids)
        ? group.option_ids
        : [];
    totalOptionIds += rawOptionIds.length;
    if (totalOptionIds > bounds.maxStatusOptionIds) {
      throw new RangeError("Board status option-id bound exceeded.");
    }
    const optionIds: string[] = [];
    const seenOptionIds = new Set<string>();
    for (const valueId of rawOptionIds) {
      const optionId = nonempty(valueId, bounds, budget);
      if (!optionId || seenOptionIds.has(optionId)) continue;
      seenOptionIds.add(optionId);
      optionIds.push(optionId);
    }
    groups.push({
      id,
      name,
      color: nonempty(group?.color, bounds, budget) || "default",
      optionIds,
    });
  }

  const groupByName = new Map<string, StatusGroup>();
  for (const group of groups) {
    if (!groupByName.has(group.name)) groupByName.set(group.name, group);
  }
  const explicitGroupByOptionId = new Map<string, StatusGroup>();
  for (const group of groups) {
    for (const optionId of group.optionIds) {
      if (!explicitGroupByOptionId.has(optionId)) explicitGroupByOptionId.set(optionId, group);
    }
    group.optionIds = [];
  }
  const groupByOptionId = new Map<string, StatusGroup>();
  for (const option of options) {
    const group = (option.group ? groupByName.get(option.group) : undefined)
      ?? explicitGroupByOptionId.get(option.id)
      ?? groups[0];
    if (!group) continue;
    group.optionIds.push(option.id);
    groupByOptionId.set(option.id, group);
  }
  return { options, groups, groupByOptionId };
}

function normalizedGroupMode(
  value: unknown,
  bounds: BoardGroupingBounds,
  budget: BoardBudget,
): BoardGroupMode | undefined {
  const normalized = nonempty(value, bounds, budget).toLowerCase();
  return normalized === "group" || normalized === "option" ? normalized : undefined;
}

function rawNotionGroupMode(
  value: unknown,
  bounds: BoardGroupingBounds,
  budget: BoardBudget,
) {
  const source = record(value);
  if (!source) return undefined;
  const groupBy = record(source.group_by ?? source.groupBy);
  return normalizedGroupMode(groupBy?.group_by ?? groupBy?.groupBy, bounds, budget);
}

export function boardGroupMode(
  config: ViewConfig | undefined,
  property: BoardMainGroupProperty,
  bounds: BoardGroupingBounds,
): BoardGroupMode {
  validateBounds(bounds);
  const budget = { stringCharacters: 0 };
  if (property.type !== "status" || statusProjection(property.config, bounds, budget).groups.length === 0) {
    return "option";
  }
  return normalizedGroupMode(config?.groupByMode, bounds, budget)
    ?? rawNotionGroupMode(config?.notionConfiguration, bounds, budget)
    ?? rawNotionGroupMode(record(config?.notion)?.configuration, bounds, budget)
    ?? rawNotionGroupMode(config, bounds, budget)
    // Its options until shown by its groups, as Notion's (seen 2026-10-24).
    ?? "option";
}

/**
 * What a board's cards split again by within each group: what a table groups
 * by — a title and relations too — the group's own property among them, as
 * Notion's (seen 2026-10-11).
 */
export function isBoardSubGroupProperty(property: { type: string }): boolean {
  return isTableGroupProperty(property);
}

export function isBoardMainGroupProperty(
  property: DbProperty | null | undefined,
): property is BoardMainGroupProperty {
  return !!property && (
    property.type === "select"
    || property.type === "status"
    || property.type === "person"
    || property.type === "multi_select"
    || property.type === "checkbox"
  );
}

function distinctNonemptyStrings(
  values: readonly unknown[],
  bounds: BoardGroupingBounds,
  budget: BoardBudget,
) {
  if (values.length > bounds.maxValues) throw new RangeError("Board group value bound exceeded.");
  const seen = new Set<string>();
  const ids: string[] = [];
  for (const value of values) {
    const raw = String(value ?? "");
    consumeString(raw, bounds, budget);
    const id = raw.trim();
    if (!id || seen.has(id)) continue;
    seen.add(id);
    ids.push(id);
  }
  return ids;
}

function personBounds(bounds: BoardGroupingBounds): PersonValueBounds {
  return {
    maxNodes: bounds.maxPersonNodes,
    maxDepth: bounds.maxPersonDepth,
    maxEntries: bounds.maxPeopleEntries,
  };
}

function boardGroupValueIdsWithin(
  property: BoardMainGroupProperty,
  value: unknown,
  bounds: BoardGroupingBounds,
  budget: BoardBudget,
  groupMode: BoardGroupMode,
) {
  if (property.type === "checkbox") return [value === true || value === "true" ? CHECKED_GROUP : UNCHECKED_GROUP];
  const ids = property.type === "person"
    ? distinctNonemptyStrings(normalizePersonIds(value, personBounds(bounds)), bounds, budget)
    : distinctNonemptyStrings(Array.isArray(value) ? value : [value], bounds, budget);
  if (property.type === "status" && groupMode === "group") {
    const projection = statusProjection(property.config, bounds, budget);
    if (projection.groups.length > 0) {
      return ids.slice(0, 1).map(
        (id) => projection.groupByOptionId.get(id)?.id ?? projection.groups[0].id,
      );
    }
  }
  // By its options, a group's own stands under its group's first (statusOptionByOption).
  if (property.type === "status") return ids.slice(0, 1).map((id) => statusOptionByOption(property.config, id));
  return property.type === "multi_select" || property.type === "person" ? ids : ids.slice(0, 1);
}

export function boardGroupValueIds(
  property: BoardMainGroupProperty,
  value: unknown,
  bounds: BoardGroupingBounds,
  groupMode: BoardGroupMode = "group",
) {
  validateBounds(bounds);
  return boardGroupValueIdsWithin(property, value, bounds, { stringCharacters: 0 }, groupMode);
}

export function boardGroupReplacementValue(
  property: BoardMainGroupProperty,
  targetId: string | null,
  bounds: BoardGroupingBounds,
  groupMode: BoardGroupMode = "group",
  /** The card moved: a status value its own database's options can't take gives none (statusTakesValue). */
  rowId?: string,
) {
  validateBounds(bounds);
  if (property.type === "checkbox") return targetId === CHECKED_GROUP;
  if (!targetId) return null;
  const budget = { stringCharacters: 0 };
  consumeString(targetId, bounds, budget);
  const taken = <T>(value: T) => property.type !== "status" || rowId === undefined || statusTakesValue(property.config, rowId, value) ? value : undefined;
  if (property.type === "status" && groupMode === "group") {
    const projection = statusProjection(property.config, bounds, budget);
    if (projection.groups.length > 0) {
      return taken(projection.groups.find((group) => group.id === targetId)?.optionIds[0] ?? null);
    }
  }
  return property.type === "person" || property.type === "multi_select" ? [targetId] : taken(targetId);
}

function admitRawOptions(
  options: readonly SelectOption[],
  bounds: BoardGroupingBounds,
  budget: BoardBudget,
) {
  if (options.length > bounds.maxStatusOptions) {
    throw new RangeError("Board status option bound exceeded.");
  }
  for (const option of options) {
    consumeString(option.id, bounds, budget);
    consumeString(option.name, bounds, budget);
    consumeString(option.color, bounds, budget);
    if (option.group) consumeString(option.group, bounds, budget);
  }
}

export function boardGroupOptions(
  property: BoardMainGroupProperty,
  rows: Page[],
  bounds: BoardGroupingBounds,
  currentUserId?: string,
  groupMode: BoardGroupMode = "group",
  keptPeople: readonly string[] = [],
): Array<SelectOption & { statusStage?: StatusStage }> {
  validateBounds(bounds);
  if (rows.length > bounds.maxRows) throw new RangeError("Board row bound exceeded.");
  const budget = { stringCharacters: 0 };
  if (currentUserId) consumeString(currentUserId, bounds, budget);
  if (property.type === "checkbox") {
    return [CHECKED_GROUP, UNCHECKED_GROUP].map((id) => ({ id, name: property.name, color: "" }));
  }
  if (property.type === "status" && groupMode === "group") {
    const groups = statusProjection(property.config, bounds, budget).groups;
    if (groups.length > 0) {
      // Named in the reader's language and marked by where it stands, as Notion's (seen 2026-10-24).
      return groups.map((group, index) => {
        const key = statusGroupLabelKey(group.name);
        return { id: group.id, name: key ? i18next.t(`propertyCell:${key}`) : group.name, color: group.color,
          statusStage: statusStageOf(index, groups.length) };
      });
    }
  }
  if (property.type !== "person") {
    // A status's options go in its groups' order, as Notion's board columns
    // (seen 2026-11-03: an option added to "진행 중" stood among its group),
    // none for a group itself (statusOptionsByOption).
    const options = property.type === "status" ? statusOptionsByOption(property.config) : property.config?.options ?? [];
    admitRawOptions(options, bounds, budget);
    return options;
  }

  const ids: string[] = [];
  const seen = new Set<string>();
  const addId = (rawId: string) => {
    consumeString(rawId, bounds, budget);
    const id = rawId.trim();
    if (!id || seen.has(id)) return;
    if (ids.length >= bounds.maxValues) throw new RangeError("Board group value bound exceeded.");
    seen.add(id);
    ids.push(id);
  };
  // A person's columns are the people its cards name, each where it is first
  // named, as Notion's: no column for a member no card names, the viewer's
  // own neither, and a person's goes once no card names them (seen
  // 2026-11-11, empty groups shown) — but for one added from the board's
  // "+ 신규 그룹", which stays though no card names them (2026-11-12).
  for (const row of rows) {
    const rowIds = normalizePersonIds(valueAsRead(row, property), personBounds(bounds));
    if (rowIds.length > bounds.maxValues) throw new RangeError("Board group value bound exceeded.");
    for (const id of rowIds) addId(id);
  }
  for (const id of keptPeople) addId(id);
  // Named alone, one's own too, with their avatar before it (BoardColumn), as Notion's (seen 2026-11-12).
  return ids.map((id) => ({ id, name: personLabel(id, currentUserId, false), color: "default" }));
}

export function hasEditableBoardGroupOptions(
  property: BoardMainGroupProperty,
  bounds: BoardGroupingBounds,
  groupMode: BoardGroupMode = "group",
) {
  validateBounds(bounds);
  return property.type !== "person" && property.type !== "checkbox" && !(
    property.type === "status"
    && groupMode === "group"
    && statusProjection(property.config, bounds, { stringCharacters: 0 }).groups.length > 0
  );
}
