import { i18next } from "@/i18n";
import { RELATIVE_SPRINTS, type RelativeSprint } from "@/shared/database/queryFilterPolicy";
import { NO_LAST_SPRINT, relativeSprintIds, relativeSprintPlace } from "./databaseSprintContext";
import type { GraphProperty } from "@/contentGraph";
import { holdsClosedMentions, lineWithClosedMentions } from "@/lib/closedMentions";
import { activeDateLocale, activeNumberLocale } from "@/lib/i18n";
import { normalizePersonIds, personLabel } from "@/lib/peopleDirectory";
import { dateKey, shownDate } from "./dateUtils";
import { computedDateText } from "./computedDateValue";
import { dateGroup, dateGroupDays } from "./tableDateGroups";
import { FormulaDate, type FormulaValue } from "./formulaSyntax";
import { formatFormulaValue } from "./formulaValues";
import { statusGroupLabelKey, statusOptionByOption, statusOptionsByOption, statusOptionsInGroupOrder, statusStageOf, type StatusStage } from "./propertyStatusPolicy";
import type { GroupPage } from "./GroupPageGlyph";

/** How a table grouped by a date groups its rows: by how far the date is from today, or by its day, week, month or year. */
export type TableDateGrouping = "relative" | "day" | "week" | "month" | "year";
export const TABLE_DATE_GROUPINGS: readonly TableDateGrouping[] = ["relative", "day", "week", "month", "year"];

const DATE_TYPES = new Set(["date", "created_time", "last_edited_time"]);
// A table groups by a title or text as Notion's does (seen 2026-09-30).
const TEXT_TYPES = new Set(["title", "rich_text", "url", "email", "phone"]);
// And by a relation, a group for each related page (seen 2026-10-11).
// And by who made or last edited a row, and by a rollup (seen 2026-10-11).
const GROUP_TYPES = new Set(["select", "multi_select", "status", "person", "checkbox", "number", "formula", "relation",
  "created_by", "last_edited_by", "rollup", ...DATE_TYPES, ...TEXT_TYPES]);
// A people cell is read whole, however many people it names (2026-10-13:
// past 1,000 the grouping threw and the table failed).
const PERSON_BOUNDS = { maxNodes: Number.MAX_SAFE_INTEGER, maxDepth: 8, maxEntries: Number.MAX_SAFE_INTEGER };
export const NO_TABLE_GROUP = "__no_group__";
/** A board's column and a board's sub-group for no value. */
export const NO_BOARD_GROUP = "__none";

/**
 * Whether a key names the group for no value: a table's or list's, or a
 * board's — one group, which a view switched between them keeps where its
 * settings put it under either name.
 */
export function isNoValueGroupKey(key: string) {
  return key === NO_TABLE_GROUP || key === NO_BOARD_GROUP;
}

export function isTableGroupProperty(property: { type: string } | null | undefined) {
  return !!property && GROUP_TYPES.has(property.type);
}

export function isDateGroupProperty(property: Pick<GraphProperty, "type"> | null | undefined) {
  return !!property && DATE_TYPES.has(property.type);
}

export function isTextGroupProperty(property: Pick<GraphProperty, "type"> | null | undefined) {
  return !!property && TEXT_TYPES.has(property.type);
}

/** How a table grouped by text groups its rows: by the whole text, or by its first letter. */
export type TableTextGrouping = "exact" | "letter";

export function tableTextGrouping(config: Record<string, unknown>): TableTextGrouping {
  return config.textGroupBy === "letter" ? "letter" : "exact";
}

/** The group of the texts whose first letter is no Latin letter or digit. */
const OTHER_LETTER_GROUP = "letter:other";

/**
 * The group a text falls in by its first letter, as Notion's (seen
 * 2026-11-12): a Latin letter's in its capital's, a digit's in its own, in
 * order up or down, and any other first character — Korean, Japanese, an
 * accented letter, a symbol, an emoji — in one "기타" group after them all,
 * whichever way they go.
 */
function firstLetterGroup(text: string, descending: boolean): TableGroupDescriptor {
  const first = Array.from(text)[0] ?? "";
  if (!/^[A-Za-z0-9]$/u.test(first)) {
    return { key: OTHER_LETTER_GROUP, label: i18next.t("tableView:textGroup.other"), rank: Number.MAX_SAFE_INTEGER - 1 };
  }
  const letter = first.toUpperCase();
  return { key: `value:${letter}`, label: letter, rank: 0, text: letter, descending };
}

/** Whether text groups come in reverse alphabetical order. */
export function tableGroupsDescending(config: Record<string, unknown>) {
  return config.groupSort === "descending";
}

const OPTION_GROUP_TYPES = new Set(["select", "multi_select", "status"]);
// Groups by what these hold sort by it, up at first (Notion's "정렬", seen 2026-11-08).
const VALUE_SORTED_TYPES = new Set(["number", "formula", "rollup", "relation", ...TEXT_TYPES]);

/** How a view's "정렬" orders its groups: as moved by hand, or by their values either way. */
export type TableGroupSort = "manual" | "ascending" | "descending";

/**
 * The "정렬" of a view's groups, as Notion's: an option's by hand at first,
 * else by name either way; a status's in its own order at first, reversed
 * or by hand; a date's oldest or newest first, never by hand (seen
 * 2026-10-24); text, numbers, relations and what formulas and rollups work
 * out up at first, down or by hand; people's and boxes' by hand alone, as
 * Notion's offers no other (seen 2026-11-08). Groups moved by hand before
 * the view kept how they sort stay so.
 */
export function tableGroupSort(config: Record<string, unknown>, propertyType: string): TableGroupSort {
  const sort = config.groupSort;
  if (DATE_TYPES.has(propertyType)) return sort === "descending" ? "descending" : "ascending";
  if (!OPTION_GROUP_TYPES.has(propertyType) && !VALUE_SORTED_TYPES.has(propertyType)) return "manual";
  if (sort === "manual" || sort === "ascending" || sort === "descending") return sort;
  return propertyType === "select" || propertyType === "multi_select" || Array.isArray(config.groupOrder) ? "manual" : "ascending";
}

/** Whether the order its groups were moved in by hand holds: not while they sort by value. */
export function tableGroupsFollowHandOrder(config: Record<string, unknown>, propertyType: string) {
  return tableGroupSort(config, propertyType) === "manual";
}

/**
 * Whether a view's group for no value comes first, as Notion's tables,
 * boards and sub-groups by every kind (seen 2026-11-08): while its groups
 * sort by value, either way — text, numbers, dates, relations, formulas,
 * rollups, options by name, a status in its order or the reverse. By hand it
 * stands where the order kept by hand puts it, else — a view Notion's API or
 * an MCP tool made by hand, with no order yet — first for a text's groups
 * and last for the rest, as a person's always does (seen 2026-11-12 on
 * boards, tables and sub-groups: "글 없음, 가, 다" but "가나, 다라, 관계
 * 없음", and a URL's, an email's, a phone's, a formula's text's last);
 * an option's stands among the options where their first order puts it,
 * kept or not yet (boardOptionGroupOrder, orderTableGroups).
 */
export function noValueGroupLeads(config: Record<string, unknown>, propertyType: string) {
  return tableGroupSort(config, propertyType) !== "manual" || noValueLeadsUnordered(config, propertyType);
}

/** Whether a grouping by hand with no order of its own yet puts its group for no value first (noValueGroupLeads). */
export function noValueLeadsUnordered(config: Readonly<Record<string, unknown>>, propertyType: string) {
  return propertyType === "rich_text" && !Array.isArray(config.groupOrder);
}

/**
 * How a number groups, as Notion's "숫자별" (seen 2026-10-11): by each value
 * ("고유"), or in ranges from "그룹 범위"'s start to its end, end included, one
 * "그룹화 단위" wide (0 to 1000 by 100 at first).
 */
export type TableNumberGrouping = { range: boolean; start: number; end: number; step: number };
export function tableNumberGrouping(config: Record<string, unknown>): TableNumberGrouping {
  const given = record(config.numberGroupRange);
  const number = (value: unknown, fallback: number) => typeof value === "number" && Number.isFinite(value) ? value : fallback;
  const start = number(given?.start, 0), end = number(given?.end, 1000), step = number(given?.step, 100);
  return { range: config.numberGroupBy === "range", start, end: Math.max(start, end), step: step > 0 ? step : 100 };
}

export function tableDateGrouping(config: Record<string, unknown>): TableDateGrouping {
  return TABLE_DATE_GROUPINGS.includes(config.dateGroupBy as TableDateGrouping) ? config.dateGroupBy as TableDateGrouping : "relative";
}

/**
 * A group of a table's rows: what it is keyed by, what it is called, its
 * color and where it comes — by rank, then, among groups of text, by text.
 */
/** A table's group: its name and color, its place, and, for a checkbox, whether its box is checked. */
/** A related page's group is titled with that page's icon, or a page's glyph. */
export type TableGroupDescriptor = {
  key: string; label: string; color?: string; rank: number; text?: string; descending?: boolean; checkbox?: boolean; page?: GroupPage;
  /** A sprint's group: the current, next or last sprint, its head saying which. */
  sprintPlace?: string;
  /** A status's group, shown by the status's groups: where it stands. */
  statusStage?: StatusStage;
  /** A status's option, shown by its options: titled by a status's dotted chip. */
  statusOption?: boolean;
  /** A person's group: the person, whose avatar comes before the name. */
  person?: string;
};

/** The order a table's groups come in. */
export function compareTableGroups(left: TableGroupDescriptor | undefined, right: TableGroupDescriptor | undefined) {
  const byRank = (left?.rank ?? 0) - (right?.rank ?? 0);
  if (byRank !== 0 || left?.text === undefined || right?.text === undefined) return byRank;
  const byText = left.text.localeCompare(right.text, activeDateLocale(), { numeric: true, sensitivity: "base" });
  return left.descending ? -byText : byText;
}

type Option = { id: string; name: string; color?: string; group?: string };

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

// A status's options in its groups' order, as its board's columns (boardGroupOptions): by its options, none for a group itself.
function options(property: GraphProperty, byOption = false): Option[] {
  const raw = property.type === "status" ? (byOption ? statusOptionsByOption : statusOptionsInGroupOrder)(property.config) : record(property.config)?.options;
  return (Array.isArray(raw) ? raw : []).flatMap((value) => {
    const option = record(value);
    return typeof option?.id === "string" && typeof option.name === "string"
      ? [{ id: option.id, name: option.name, color: typeof option.color === "string" ? option.color : undefined,
        group: typeof option.group === "string" ? option.group : undefined }]
      : [];
  });
}

// A status property's groups (To-do, In progress, Complete) and the group of
// each of its options.
function statusGroups(property: GraphProperty, all: readonly Option[]) {
  const config = record(property.config);
  const raw = Array.isArray(config?.statusGroups) ? config.statusGroups
    : Array.isArray(record(record(config?.notion)?.status)?.groups) ? record(record(config?.notion)?.status)!.groups as unknown[] : [];
  const groups = raw.flatMap((value) => {
    const group = record(value);
    const optionIds = Array.isArray(group?.optionIds) ? group.optionIds : Array.isArray(group?.option_ids) ? group.option_ids : [];
    return typeof group?.id === "string" && typeof group.name === "string"
      ? [{ id: group.id, name: group.name, color: typeof group.color === "string" ? group.color : undefined, optionIds: optionIds.map(String) }]
      : [];
  });
  const byOption = new Map<string, (typeof groups)[number]>();
  for (const option of all) {
    const group = groups.find((candidate) => candidate.name === option.group)
      ?? groups.find((candidate) => candidate.optionIds.includes(option.id)) ?? groups[0];
    if (group) byOption.set(option.id, group);
  }
  return { groups, byOption };
}

function textOfValue(value: unknown) {
  return (Array.isArray(value) ? value : [value]).map((item) => (
    typeof item === "string" ? item : typeof record(item)?.text === "string" ? String(record(item)!.text) : item == null ? "" : String(item)
  )).join("").trim();
}

// A rollup's values, a list's items each (a select's names, people), a single value as one.
function rollupMembers(value: unknown): unknown[] {
  return Array.isArray(value) ? value.flatMap(rollupMembers) : [value];
}

function relationIds(value: unknown) {
  return [...new Set((Array.isArray(value) ? value : [value]).map((id) => typeof id === "string" ? id.trim() : "").filter(Boolean))];
}

function firstValue(value: unknown) {
  const first = Array.isArray(value) ? value[0] : value;
  return typeof first === "string" ? first.trim() : "";
}

/**
 * Names the group each value of a table's group property falls in, as the
 * table shows it: options by their names, colors and order, a status by its
 * status group unless the view groups by option, people by name, checkboxes
 * as checked or not, numbers in order, dates by the view's date grouping and
 * other values by their text, text's groups in alphabetical order — by the
 * whole text or its first letter — as Notion's (seen 2026-09-30). Rows
 * without a value make a group first or last as noValueGroupLeads says.
 */
export function tableGrouper(property: GraphProperty, context: {
  config: Record<string, unknown>;
  currentUserId?: string;
  today?: Date;
  /** What a related page is called. */
  pageTitle?: (pageId: string) => string;
  /** The icon a related page shows. */
  pageIcon?: (pageId: string) => GroupPage | undefined;
}): (value: unknown) => TableGroupDescriptor {
  const none: TableGroupDescriptor = {
    key: NO_TABLE_GROUP, label: i18next.t("tableView:dateGroup.none", { name: property.name }),
    rank: noValueGroupLeads(context.config, property.type) ? Number.MIN_SAFE_INTEGER : Number.MAX_SAFE_INTEGER,
  };
  const byDate = () => {
    const grouping = tableDateGrouping(context.config);
    const startDayOfWeek = context.config.startDayOfWeek === 1 ? 1 : 0;
    const now = context.today ?? new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    // A date groups by the day it shows on for the viewer, as its cell shows
    // it; its groups go oldest or newest first (Notion's, seen 2026-10-24).
    const newest = tableGroupSort(context.config, "date") === "descending";
    return (value: unknown) => {
      const date = shownDate(value)?.date;
      if (!date) return none;
      const group = dateGroup(date, grouping, startDayOfWeek, today);
      return newest ? { ...group, rank: -group.rank } : group;
    };
  };
  // A checkbox group is titled by its property with its box, checked or not,
  // the checked one first (Notion's table and board, seen 2026-10-01).
  const byCheckbox = (value: unknown): TableGroupDescriptor => value === true
    ? { key: "checked", label: property.name, checkbox: true, rank: 0 }
    : { key: "unchecked", label: property.name, checkbox: false, rank: 1 };
  // Numbers go up, or down when the view asks; a range's group is named by
  // its two ends, and the values outside the ranges go after the ranges.
  const numbers = tableNumberGrouping(context.config);
  const sign = tableGroupsDescending(context.config) ? -1 : 1;
  const format = new Intl.NumberFormat(activeNumberLocale());
  const byNumber = (value: unknown): TableGroupDescriptor => {
    const number = typeof value === "number" ? value : Number.NaN;
    if (!Number.isFinite(number)) return none;
    if (!numbers.range) return { key: `number:${number}`, label: format.format(number), rank: sign * number };
    if (number < numbers.start || number > numbers.end) {
      return { key: "number-out-of-range", label: i18next.t("tableView:numberGroup.outOfRange"), rank: Number.MAX_SAFE_INTEGER - 1 };
    }
    const low = numbers.start + Math.floor((number - numbers.start) / numbers.step) * numbers.step;
    return { key: `range:${low}`, label: i18next.t("tableView:numberGroup.range", { from: format.format(low), to: format.format(low + numbers.step) }), rank: sign * low };
  };
  const byText = () => {
    const letter = tableTextGrouping(context.config) === "letter";
    const descending = tableGroupsDescending(context.config);
    return (text: string): TableGroupDescriptor => {
      if (!text) return none;
      if (letter) return firstLetterGroup(text, descending);
      // A mention its reader may not open, which the text holds in marks,
      // is titled as its cell shows it (closedMentions.ts).
      const label = lineWithClosedMentions(text);
      return { key: `value:${text}`, label, rank: 0, text: label, descending };
    };
  };
  if (property.type === "formula") {
    // A formula groups by what it works out, as Notion's (seen 2026-10-01): a
    // number by its value in order, a checkbox as checked or not, a date as
    // the view groups dates and text by its whole text in alphabetical order.
    const date = byDate();
    const text = byText();
    return (value) => {
      if (typeof value === "boolean") return byCheckbox(value);
      if (typeof value === "number") return byNumber(value);
      if (value instanceof FormulaDate) return date(value.text);
      if (value === null || value === undefined) return none;
      return text(formatFormulaValue(value as FormulaValue));
    };
  }
  if (property.type === "rollup") {
    // A rollup groups by each value it works out (tableGroupValueReader,
    // tableGroupsOf), in order as a formula's: a number by its value, a date
    // as the view groups dates — by how near today until a unit is picked —
    // a range by its start, anything else by its text alphabetically, up or
    // down as the view says (Notion's, seen 2026-11-08: numbers ascending;
    // 2026-11-12: "가 · 나 · 다" each, dates "지난 7일 · 다음 30일").
    const text = byText();
    const date = byDate();
    return (value) => {
      const item = Array.isArray(value) ? value.find((member) => textOfValue(member) || typeof member === "number") : value;
      const day = computedDateText(item, property);
      if (day) return date(day);
      const named = record(item)?.name;
      const shown = typeof item === "number" ? String(item) : typeof named === "string" ? named : textOfValue(item);
      if (!shown) return none;
      return /^-?\d+(?:\.\d+)?$/u.test(shown) ? { key: `value:${shown}`, label: shown, rank: sign * Number(shown) } : text(shown);
    };
  }
  if (isDateGroupProperty(property)) return byDate();
  if (property.type === "relation") {
    // Related pages' groups go by their titles in alphabetical order (Notion's,
    // seen 2026-10-11); one value's first page. A sprint relation's go
    // current, next and last sprint first, each head saying which, and its
    // rows without one are the backlog, last (Notion's sprint planning, seen
    // 2026-11-04).
    const descending = tableGroupsDescending(context.config);
    const sprints = record(property.config)?.relationDatabaseId;
    const sprintRelation = relativeSprintIds(sprints, "current") !== undefined;
    return (value) => {
      const id = relationIds(value)[0];
      const label = id ? context.pageTitle?.(id) ?? id : "";
      if (!id) return sprintRelation ? { ...none, label: i18next.t("databaseView:sprintBacklogGroup"), rank: Number.MAX_SAFE_INTEGER } : none;
      if (id === NO_LAST_SPRINT) {
        return { key: NO_LAST_SPRINT, label: i18next.t("databaseView:relativeSprints.last"), rank: RELATIVE_SPRINTS.indexOf("last") - RELATIVE_SPRINTS.length, descending };
      }
      const place = sprintRelation ? relativeSprintPlace(sprints, id) : undefined;
      const rank = place ? RELATIVE_SPRINTS.indexOf(place as RelativeSprint) - RELATIVE_SPRINTS.length : 0;
      return { key: `page:${id}`, label, rank, text: label, descending, page: context.pageIcon?.(id) ?? true, sprintPlace: place };
    };
  }
  if (property.type === "checkbox") return byCheckbox;
  if (property.type === "number") return byNumber;
  if (property.type === "person" || property.type === "created_by" || property.type === "last_edited_by") {
    const seen = new Map<string, number>();
    return (value) => {
      const id = normalizePersonIds(value, PERSON_BOUNDS)[0];
      if (!id) return none;
      if (!seen.has(id)) seen.set(id, seen.size);
      // Named alone, one's own too, after their avatar, as Notion's group heads (seen 2026-11-12).
      return { key: `person:${id}`, label: personLabel(id, context.currentUserId, false), rank: seen.get(id)!, person: id };
    };
  }
  if (isTextGroupProperty(property)) {
    const text = byText();
    return (value) => text(textOfValue(value));
  }
  if (!["select", "multi_select", "status"].includes(property.type)) {
    const seen = new Map<string, number>();
    return (value) => {
      const text = textOfValue(value);
      if (!text) return none;
      if (!seen.has(text)) seen.set(text, seen.size);
      return { key: `value:${text}`, label: text, rank: seen.get(text)! };
    };
  }
  const all = options(property, true);
  // A status's groups go in its order or the reverse; an option's in the
  // property's order or by name either way (Notion's, seen 2026-10-24).
  const sort = tableGroupSort(context.config, property.type);
  const direction = sort === "descending" ? -1 : 1;
  // A status shows its options until shown by its groups, as Notion's (seen 2026-10-24).
  const status = property.type === "status" && context.config.groupByMode === "group" ? statusGroups(property, options(property)) : null;
  if (status && status.groups.length > 0) {
    const groups = status.groups;
    return (value) => {
      // A status that reads none (one with no default, made from a select) has the group for none.
      if (!firstValue(value)) return none;
      const group = status.byOption.get(firstValue(value)) ?? groups[0]!;
      const index = groups.indexOf(group);
      const key = statusGroupLabelKey(group.name);
      return { key: group.id, label: key ? i18next.t(`propertyCell:${key}`) : group.name, color: group.color, rank: direction * index,
        statusStage: statusStageOf(index, groups.length) };
    };
  }
  const byName = property.type !== "status" && sort !== "manual";
  return (value) => {
    // A status reads its default where it sets none (tableGroupValueReader), one with no default none; a group's own its group's first.
    const id = property.type === "status" ? statusOptionByOption(property.config, firstValue(value)) : firstValue(value);
    if (!id) return none;
    const index = all.findIndex((option) => option.id === id);
    const option = all[index];
    if (byName) return { key: option ? option.id : `unknown:${id}`, label: option?.name ?? id, color: option?.color, rank: 0,
      text: option?.name ?? id, descending: sort === "descending" };
    return option ? { key: option.id, label: option.name, color: option.color, rank: property.type === "status" ? direction * index : index,
      ...(property.type === "status" ? { statusOption: true } : {}) }
      : { key: `unknown:${id}`, label: id, rank: all.length + 50 };
  };
}

/**
 * Every group a value falls in: one for each of its tags, people or related
 * pages, as Notion's tables, lists, galleries, boards and timelines put a row
 * under each (seen 2026-10-11; tags and people 2026-11-24), one for each value
 * a rollup shows (seen 2026-11-12), else its one group.
 */
export function tableGroupsOf(property: GraphProperty, grouper: (value: unknown) => TableGroupDescriptor) {
  const members = property.type === "rollup" ? rollupMembers
    : property.type === "person" ? (value: unknown) => normalizePersonIds(value, PERSON_BOUNDS).map((id) => [id])
    : property.type === "relation" || property.type === "multi_select" ? (value: unknown) => relationIds(value).map((id) => [id]) : null;
  if (!members) return (value: unknown) => [grouper(value)];
  return (value: unknown) => {
    const groups = new Map<string, TableGroupDescriptor>();
    for (const member of members(value)) {
      const group = grouper(member);
      if (group.key !== NO_TABLE_GROUP && !groups.has(group.key)) groups.set(group.key, group);
    }
    return groups.size ? [...groups.values()] : [grouper(null)];
  };
}

/**
 * The value a row made in a group starts with, so that it lands in the group,
 * as a row made under a Notion table's group does — a date group's first day,
 * as Notion's "+ 새 페이지" and a group head's "+" give it (seen 2026-11-24 in
 * two tabs: 지난 7일 11-17, 내일 11-25, 다음 30일 12-02, 2026년 10월 10-01, the
 * week of 11-15~21 11-15, a day with no time); undefined when the group stands
 * for no one value (no value, a date worked out, a first letter).
 */
export function tableGroupSeedValue(property: GraphProperty, key: string, config: Record<string, unknown>, today = new Date()): unknown {
  if (key === NO_TABLE_GROUP || key.startsWith("unknown:")) return undefined;
  if (property.type === "date") { const days = dateGroupDays(key, today); return days ? dateKey(days.first) : undefined; }
  if (property.type === "checkbox") return key === "checked";
  if (property.type === "number") return key.startsWith("number:") ? Number(key.slice("number:".length)) : undefined;
  if (property.type === "person") return key.startsWith("person:") ? [key.slice("person:".length)] : undefined;
  if (property.type === "select") return key;
  if (property.type === "multi_select") return [key];
  if (property.type === "relation") return key.startsWith("page:") ? [key.slice("page:".length)] : undefined;
  if (property.type === "status") {
    if (config.groupByMode !== "group") return key;
    const all = options(property);
    const status = statusGroups(property, all);
    if (!status.groups.length) return key;
    return all.find((option) => status.byOption.get(option.id)?.id === key)?.id;
  }
  // A text holding a mention its reader may not open has no words to start
  // a row with: the marks stand for a title they may not read.
  if (isTextGroupProperty(property) && tableTextGrouping(config) === "exact" && key.startsWith("value:")
    && !holdsClosedMentions(key)) {
    return key.slice("value:".length);
  }
  return undefined;
}
