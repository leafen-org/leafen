import { i18next } from "@/i18n";
import { lineWithClosedMentions } from "@/lib/closedMentions";
import { activeDateLocale, isKoreanLocale } from "@/lib/i18n";
import type { PersonValueBounds } from "@/lib/peopleDirectory";
import { pageNamesNow } from "@/lib/pageNames";
import type { DbProperty, Page } from "@/lib/types";
import { chartDateBucketRange, type ChartDateRange } from "./chartDateBucketRange";
import { chartAutomaticGroupColor } from "./chartViewPalette";
import { computedDateText } from "./computedDateValue";
import { chartFormulaValueType } from "./chartGroupingConfiguration";
import { shownDateKey } from "./dateUtils";
import { computedFormulaValue, formulaItemText } from "./formula";
import { isFormulaReference } from "./formulaValues";
import { formatNumberForProperty } from "./numberFormat";
import { percentPresentationProperty } from "./rollupNumberDisplay";
import { normalizePersonIds, personLabel } from "./people";
import { cellValue } from "./query";
import { admitPageLookup, createRollupBudget, valueAsIdsWithin } from "./rollupPolicy";
import { effectiveSummaryValue, type SummaryValueBounds } from "./summaryValue";
import { NO_TABLE_GROUP, tableGrouper } from "./tableGrouping";
import type { ChartSettings } from "./chartViewSettings";
import { statusGroupColumns, statusGroupLabelKey } from "./propertyStatusPolicy";

type ChartDateBy = ChartSettings["dateBy"];
/**
 * How an X axis or stacked groups group: a date's days by unit and the day its weeks start on, a status by its
 * options or groups; stacked, a date's group for no date shows by every unit (chartStackGrouping).
 */
export type ChartAxisGrouping = Partial<Pick<ChartSettings, "dateBy" | "weekStart" | "statusBy" | "sort" | "axisGivenAs">> & { stacked?: true };

/**
 * How a chart's stacked groups group, as its settings pick them: by their own unit, week start and status kind,
 * a date's group for no date first by every unit, "When 없음 · 2026년 10월 · …", even with no row in it (Notion's,
 * seen 2026-11-12).
 */
export function chartStackGrouping(settings: Pick<ChartSettings, "stackDateBy" | "stackWeekStart" | "stackStatusBy">): ChartAxisGrouping {
  return { dateBy: settings.stackDateBy, weekStart: settings.stackWeekStart, statusBy: settings.stackStatusBy, stacked: true };
}

const CHART_NS = "chartView";
const EMPTY_BUCKET_KEY = "__empty";
const BLANK_BUCKET_KEY = "__blank";
const TEXT_TYPES = new Set(["title", "rich_text", "url", "email", "phone"]);
const CHART_GROUPABLE_TYPES = new Set([
  "select", "multi_select", "status", "checkbox", "person", "created_by", "last_edited_by", "relation", "formula", "rollup",
  "date", "created_time", "last_edited_time", "number", ...TEXT_TYPES,
]);

/**
 * Notion's X and stack choices (seen 2026-10-09), including computed values,
 * relations and record authors; files and unique IDs are not group choices.
 */
export function isChartGroupableProperty(prop: Pick<DbProperty, "type">) {
  return CHART_GROUPABLE_TYPES.has(prop.type);
}
const MONTH_KEY = /^(\d{4})-(0[1-9]|1[0-2])$/u;

function tt(key: string, options?: Record<string, unknown>): string {
  return i18next.t(`${CHART_NS}:${key}`, options) as string;
}

function formatMonthLabel(year: number, month: number): string {
  if (isKoreanLocale()) return tt("monthLabel", { year, month });
  return new Date(year, month - 1, 1).toLocaleDateString(activeDateLocale(), {
    month: "short",
    year: "numeric",
  });
}

export interface ChartBucket {
  key: string;
  label: string;
  color: string;
  rows: Page[];
  /** The rows without a value, which Notion's chart draws even when there are none. */
  empty?: boolean;
  /** A formula's rows of empty text, a group of the same name as the rows without a value, right after it. */
  blank?: boolean;
  /** A color of the chart's own, which goes by the place the group is drawn in. */
  automatic?: boolean;
  /** A select's or status's option, which the chart's settings show as its chip. */
  option?: boolean;
  /** Only a native number property uses a continuous category axis. */
  numericValue?: number;
  /** A date property's day, week, month or year, by when it starts, along a continuous time axis. */
  timeValue?: number;
  /** A date property's group's first and last days, its rows window's rule (chartDateBucketRange). */
  range?: ChartDateRange;
}

export type ChartValueContext = {
  properties: DbProperty[];
  pagesById: Record<string, Page>;
  bounds: SummaryValueBounds;
  propertiesByDatabaseId?: Record<string, DbProperty[]>;
};

// "Priority 없음", as Notion's chart and table name it (seen 2026-10-05).
function noValueLabel(prop: DbProperty) {
  return i18next.t("tableView:dateGroup.none", { name: prop.name || tt("untitledProperty") }) as string;
}
function appendEmptyBucket(buckets: ChartBucket[], rows: Page[], prop: DbProperty): ChartBucket[] {
  return [...buckets, { key: EMPTY_BUCKET_KEY, label: noValueLabel(prop), color: "default", rows, empty: true }];
}

// A week's key: the day it starts on, a Sunday or a Monday as the chart says (Notion's, seen 2026-11-12).
function weekStartKey(dayKey: string, weekStart: 0 | 1) {
  const [year, month, day] = dayKey.split("-").map(Number);
  const date = new Date(year, month - 1, day);
  date.setDate(date.getDate() - ((date.getDay() - weekStart + 7) % 7));
  return `${String(date.getFullYear()).padStart(4, "0")}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

/**
 * A week by its first and last days with no commas, "2026년 10월 18일 ~ 24일", "Aug 31 – Sep 6 2026",
 * "Dec 27 2026 – Jan 2 2027" (Notion's, seen 2026-11-12).
 */
function formatWeekLabel(key: string) {
  const [year, month, day] = key.split("-").map(Number);
  const range = new Intl.DateTimeFormat(activeDateLocale(), { year: "numeric", month: "short", day: "numeric" })
    .formatRange(new Date(year, month - 1, day), new Date(year, month - 1, day + 6)).replace(/,/gu, "");
  // One space each side of "~", whether the browser's range puts them there or not.
  return isKoreanLocale() ? range.replace(/\s*~\s*/u, " ~ ") : range;
}

/** A day with its month's whole name, "2026년 10월 1일", "October 1, 2026" (Notion's, seen 2026-11-12). */
function formatDayLabel(time: number) {
  return new Intl.DateTimeFormat(activeDateLocale(), { year: "numeric", month: "long", day: "numeric" }).format(new Date(time));
}

const dayTime = (key: string) => {
  const [year, month, day = 1] = key.split("-").map(Number);
  return new Date(year, month - 1, day).getTime();
};

/** A time axis's tick named as its groups are: "2026년 10월 1일", a week's seven days from it, "2026년 10월", "2026". */
export function chartTimeTickName(tick: number, unit: "day" | "week" | "month" | "year") {
  const date = new Date(tick);
  if (unit === "year") return String(date.getFullYear());
  if (unit === "month") return formatMonthLabel(date.getFullYear(), date.getMonth() + 1);
  if (unit === "week") {
    return formatWeekLabel(`${String(date.getFullYear()).padStart(4, "0")}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`);
  }
  return formatDayLabel(tick);
}

/**
 * A chart's groups by the property. An X axis given as another type than its
 * property's draws only the groups that hold rows, its group for no value
 * too (Notion's, seen 2026-11-12: a title given as text "R1 … R5" with no
 * "이름 없음", a status given as a select only its options with rows, a
 * checkbox given as text "체크" alone; "이름 없음 1" once a row has none).
 */
export function chartBuckets(
  rows: readonly Page[],
  prop: DbProperty,
  personValueBounds: PersonValueBounds,
  context?: ChartValueContext,
  grouping: ChartAxisGrouping = {},
): ChartBucket[] {
  // A date's own groups, not one given as another type.
  const dated = (prop.type === "date" || prop.type === "created_time" || prop.type === "last_edited_time") && !grouping.axisGivenAs;
  const buckets = propertyBuckets(rows, prop, personValueBounds, context, grouping)
    .map((bucket) => {
      const range = dated && !bucket.empty ? chartDateBucketRange(bucket.key) : undefined;
      return range ? { ...bucket, range } : bucket;
    });
  return grouping.axisGivenAs === true ? buckets.filter((bucket) => bucket.rows.length) : buckets;
}

function propertyBuckets(
  rows: readonly Page[],
  prop: DbProperty,
  personValueBounds: PersonValueBounds,
  context: ChartValueContext | undefined,
  { dateBy: givenDateBy, weekStart = 0, statusBy = "option", sort, axisGivenAs, stacked }: ChartAxisGrouping,
): ChartBucket[] {
  // Months until another unit is picked; a rollup's dates by how near today (chartAxisKind).
  const dateBy = givenDateBy ?? (prop.type === "rollup" ? "relative" : "month");
  // A date's group for no date shows by how near today, when the chart sorts by value and stacked, at 0 with no
  // row without one; a created or edited time's, which every row has, never (Notion's, seen 2026-11-12).
  const keepsNoDate = sort === "y_ascending" || sort === "y_descending" || stacked === true;
  const emptyDateGroup = (empty: readonly Page[]) => empty.length > 0 || prop.type === "date";
  if (prop.type === "checkbox") {
    // Checked rows under the property's own name, the others "〈속성〉 없음", in the chart's
    // own colors by their place (Notion's, seen 2026-11-11: "완료함" blue, "완료함 없음" yellow).
    const name = prop.name || tt("untitledProperty");
    return [
      { key: "checked", label: name, color: chartAutomaticGroupColor(0), automatic: true, rows: rows.filter((row) => !!cellValue(row, prop)) },
      { key: "unchecked", label: i18next.t("tableView:dateGroup.none", { name }) as string, color: chartAutomaticGroupColor(1), automatic: true,
        rows: rows.filter((row) => !cellValue(row, prop)) },
    ];
  }

  if (prop.type === "person" || prop.type === "created_by" || prop.type === "last_edited_by") {
    const byPerson = new Map<string, Page[]>();
    const empty: Page[] = [];
    for (const row of rows) {
      const ids = Array.from(new Set(normalizePersonIds(cellValue(row, prop), personValueBounds)));
      if (ids.length === 0) {
        empty.push(row);
        continue;
      }
      for (const id of ids) byPerson.set(id, [...(byPerson.get(id) ?? []), row]);
    }
    const buckets = Array.from(byPerson.entries()).map(([id, bucketRows], index) => ({
      key: id,
      label: personLabel(id),
      color: chartAutomaticGroupColor(index), automatic: true,
      rows: bucketRows,
    }));
    return prop.type === "person" || empty.length ? appendEmptyBucket(buckets, empty, prop) : buckets;
  }

  if (prop.type === "relation" || prop.type === "formula" || prop.type === "rollup") {
    // A formula given another type or family than its own result's keeps its group for no value only with rows.
    const own = prop.type !== "formula" || axisGivenAs === undefined
      || axisGivenAs !== true && [undefined, axisGivenAs].includes(chartFormulaValueType(prop, context?.properties ?? []));
    return context ? computedBuckets(rows, prop, context, dateBy, weekStart, own) : [];
  }

  if ((prop.type === "date" || prop.type === "created_time" || prop.type === "last_edited_time") && (dateBy === "day" || dateBy === "year" || dateBy === "relative")) {
    // By day, by year, or by how near today ("오늘", "지난 7일", …), as a table
    // groups dates; a year is named by its number alone, "2026". A row with
    // no date is left off by day and by year, as by week and month, but by how
    // near today it is "날짜 없음", first (Notion's chart, seen 2026-11-06).
    const grouper = tableGrouper(prop as unknown as Parameters<typeof tableGrouper>[0], { config: { dateGroupBy: dateBy } });
    const groups = new Map<string, { label: string; rank: number; rows: Page[] }>();
    const empty: Page[] = [];
    for (const row of rows) {
      const group = grouper(cellValue(row, prop));
      if (group.key === NO_TABLE_GROUP) { empty.push(row); continue; }
      const found = groups.get(group.key) ?? { label: dateBy === "year" ? String(new Date(group.rank).getFullYear())
        : dateBy === "day" ? formatDayLabel(group.rank) : group.label, rank: group.rank, rows: [] };
      found.rows.push(row);
      groups.set(group.key, found);
    }
    const buckets = [...groups.entries()].sort(([, left], [, right]) => left.rank - right.rank)
      .map(([key, group], index) => ({ key, label: group.label, color: chartAutomaticGroupColor(index), automatic: true, rows: group.rows,
        ...(dateBy === "relative" ? {} : { timeValue: group.rank }) }));
    return (dateBy === "relative" || keepsNoDate) && emptyDateGroup(empty) ? appendEmptyBucket(buckets, empty, prop) : buckets;
  }

  if ((prop.type === "date" || prop.type === "created_time" || prop.type === "last_edited_time") && dateBy === "week") {
    // By week a row with no date is left off, as Notion's new chart leaves it (seen 2026-11-02).
    const byWeek = new Map<string, Page[]>();
    const empty: Page[] = [];
    for (const row of rows) {
      const day = shownDateKey(cellValue(row, prop)).slice(0, 10);
      if (day.length < 10) { empty.push(row); continue; }
      const key = weekStartKey(day, weekStart);
      byWeek.set(key, [...(byWeek.get(key) ?? []), row]);
    }
    // A week with no row is the axis's empty stretch, by bars and by line, not a group of its own (Notion's, seen 2026-11-12).
    const buckets = Array.from(byWeek.keys()).sort().map((key, index) => ({
      key,
      label: formatWeekLabel(key),
      color: chartAutomaticGroupColor(index), automatic: true,
      rows: byWeek.get(key) ?? [],
      timeValue: dayTime(key),
    }));
    return keepsNoDate && emptyDateGroup(empty) ? appendEmptyBucket(buckets, empty, prop) : buckets;
  }

  if (prop.type === "date" || prop.type === "created_time" || prop.type === "last_edited_time") {
    // By month too a row with no date is left off, by bars and by line (Notion's, seen 2026-11-06), and a
    // month with no row is the axis's empty stretch (seen 2026-11-12).
    const byMonth = new Map<string, Page[]>();
    const empty: Page[] = [];
    for (const row of rows) {
      const key = shownDateKey(cellValue(row, prop)).slice(0, 7);
      if (!key) { empty.push(row); continue; }
      byMonth.set(key, [...(byMonth.get(key) ?? []), row]);
    }
    const buckets = Array.from(byMonth.keys()).sort().map((key, index) => ({
      key,
      label: formatMonthLabel(Number(key.slice(0, 4)), Number(key.slice(5, 7))),
      color: chartAutomaticGroupColor(index), automatic: true,
      rows: byMonth.get(key) ?? [],
      timeValue: dayTime(key),
    }));
    return keepsNoDate && emptyDateGroup(empty) ? appendEmptyBucket(buckets, empty, prop) : buckets;
  }

  if (prop.type === "number" || TEXT_TYPES.has(prop.type)) return valueBuckets(rows, prop);
  if (prop.type === "status" && statusBy === "group") {
    const groups = statusBuckets(rows, prop);
    if (groups) return groups;
  }

  const options = prop.config?.options ?? [];
  const known = new Set(options.map((option) => option.id));
  const byOption = new Map<string, Page[]>();
  const empty: Page[] = [];
  for (const row of rows) {
    const raw = cellValue(row, prop);
    const ids = (Array.isArray(raw) ? raw : raw == null || raw === "" ? [] : [raw])
      .map(String)
      .filter((id) => known.has(id));
    if (ids.length === 0) {
      empty.push(row);
      continue;
    }
    const bucketIds = prop.type === "multi_select" ? Array.from(new Set(ids)) : ids.slice(0, 1);
    for (const id of bucketIds) byOption.set(id, [...(byOption.get(id) ?? []), row]);
  }
  const buckets = options.map((option) => ({
    key: option.id,
    label: option.name,
    // A status option with no color of its own ("시작 전") is drawn as Notion's
    // default, near white, not in its group's grey (seen 2026-11-06).
    color: option.colorFromGroup ? "default" : option.color,
    rows: byOption.get(option.id) ?? [],
    option: true,
  }));
  // A select's or multi-select's rows with no value have their group even when there are none,
  // at 0; a status's only when there are any (Notion's, seen 2026-11-12).
  return empty.length || prop.type !== "status" ? appendEmptyBucket(buckets, empty, prop) : buckets;
}

/**
 * A status by its groups, "할 일 · 진행 중 · 완료" in their order and colors,
 * each holding its options' rows (Notion's, seen 2026-11-12); none where the
 * status keeps no groups.
 */
function statusBuckets(rows: readonly Page[], prop: DbProperty): ChartBucket[] | null {
  const groups = statusGroupColumns(prop.config);
  if (!groups.length) return null;
  const groupOf = new Map(groups.flatMap((group) => group.optionIds.map((id) => [id, group.id] as const)));
  const byGroup = new Map<string, Page[]>();
  const empty: Page[] = [];
  for (const row of rows) {
    const raw = cellValue(row, prop);
    const group = groupOf.get(String(Array.isArray(raw) ? raw[0] : raw ?? ""));
    if (group) byGroup.set(group, [...(byGroup.get(group) ?? []), row]);
    else empty.push(row);
  }
  const buckets = groups.map((group) => {
    const key = statusGroupLabelKey(group.name);
    return { key: group.id, label: key ? i18next.t(`propertyCell:${key}`) as string : group.name, color: group.color, rows: byGroup.get(group.id) ?? [], option: true };
  });
  return empty.length ? appendEmptyBucket(buckets, empty, prop) : buckets;
}

// Text in the reader's order with the numbers in it by their value, "_x · 9 · 10 · 나무 · apple · Banana"
// (Notion's, seen 2026-11-12).
const byText = (left: string, right: string) => left.localeCompare(right, activeDateLocale(), { numeric: true });

// Numbers by their value in order, as Notion's chart places them on its
// axis (seen 2026-10-05, rows without one left out), and text by its whole
// text in alphabetical order (byText) with the rows without text last.
function valueBuckets(rows: readonly Page[], prop: DbProperty): ChartBucket[] {
  const grouper = tableGrouper(prop as unknown as Parameters<typeof tableGrouper>[0], { config: {} });
  const groups = new Map<string, { label: string; rank: number; text?: string; rows: Page[] }>();
  const empty: Page[] = [];
  for (const row of rows) {
    const raw = cellValue(row, prop);
    const group = grouper(prop.type === "number" && typeof raw === "string" && raw.trim() ? Number(raw) : raw);
    if (group.key === NO_TABLE_GROUP) {
      empty.push(row);
      continue;
    }
    const found = groups.get(group.key) ?? { label: group.label, rank: group.rank, text: group.text, rows: [] };
    found.rows.push(row);
    groups.set(group.key, found);
  }
  const buckets = [...groups.entries()]
    .sort(([, left], [, right]) => left.rank - right.rank || byText(left.text ?? "", right.text ?? ""))
    .map(([key, group], index) => ({
      key, label: prop.type === "number" ? formatNumberForProperty(group.rank, { config: { ...prop.config } }) : group.label,
      color: chartAutomaticGroupColor(index), automatic: true, rows: group.rows,
      ...(prop.type === "number" ? { numericValue: group.rank } : {}),
    }));
  return appendEmptyBucket(buckets, empty, prop);
}

/**
 * A computed date's group by the axis's unit, as a date property's: its
 * month ("2026년 11월"), its week, its day, its year, or how near today it
 * is (Notion's, seen 2026-11-11 for a formula's dates).
 */
function computedDateGroup(text: string, prop: DbProperty, dateBy: ChartDateBy, weekStart: 0 | 1) {
  const day = shownDateKey(text).slice(0, 10);
  if (dateBy === "month") {
    const key = day.slice(0, 7);
    return MONTH_KEY.test(key) ? { key, label: formatMonthLabel(Number(key.slice(0, 4)), Number(key.slice(5, 7))), rank: 0 } : null;
  }
  if (dateBy === "week") {
    if (day.length < 10) return null;
    const key = weekStartKey(day, weekStart);
    return { key, label: formatWeekLabel(key), rank: 0 };
  }
  const group = tableGrouper({ ...prop, type: "date" } as unknown as Parameters<typeof tableGrouper>[0], { config: { dateGroupBy: dateBy } })(text);
  if (group.key === NO_TABLE_GROUP) return null;
  return { key: group.key, label: dateBy === "year" ? String(new Date(group.rank).getFullYear()) : dateBy === "day" ? formatDayLabel(group.rank) : group.label,
    rank: group.rank };
}

const DAY_KEY = /^\d{4}-\d{2}-\d{2}$/u;
const calendarKey = (key: string) => MONTH_KEY.test(key) || DAY_KEY.test(key);
// A relation's pages go by their titles in English order, Latin before Hangul ("_under · 123 · a · B · Ölwechsel ·
// zeta · 가 · 셋째"); other values in the reader's (Notion's, seen 2026-11-12, on a fresh load).
const RELATION_TITLE_ORDER = new Intl.Collator("en").compare;

/** Computed lists and relations group each distinct member once per row. */
function computedBuckets(rows: readonly Page[], prop: DbProperty, context: ChartValueContext, dateBy: ChartDateBy, weekStart: 0 | 1,
  own = true): ChartBucket[] {
  const numberProperty = percentPresentationProperty(prop, context.properties, context.propertiesByDatabaseId);
  const groups = new Map<string, { label: string; rank: number; rows: Page[] }>();
  const empty: Page[] = [], blank: Page[] = [];
  for (const row of rows) {
    const raw = effectiveSummaryValue(row, prop, context.properties, context.pagesById, context.bounds, context.propertiesByDatabaseId);
    const budget = createRollupBudget(context.bounds.projection);
    const seen = new Set<string>();
    const add = (key: string, label: string, rank = 0) => {
      if (seen.has(key)) return;
      seen.add(key);
      const group = groups.get(key) ?? { label, rank, rows: [] };
      group.rows.push(row);
      groups.set(key, group);
    };
    let blankText = false;
    if (prop.type === "relation") {
      for (const id of valueAsIdsWithin(raw, context.bounds.projection, budget)) {
        if (!id) continue;
        admitPageLookup(id, context.bounds.projection, budget);
        add(id, pageNamesNow.title(context.pagesById[id]));
      }
    } else {
      const visit = (value: ReturnType<typeof computedFormulaValue>) => {
        if (Array.isArray(value)) { value.forEach(visit); return; }
        if (value === "" && prop.type === "formula") blankText = true;
        if (value == null || value === "" || value === false) return;
        if (typeof value === "number") {
          if (Number.isFinite(value)) add(`number:${value}`, formatNumberForProperty(value, { config: { ...numberProperty.config } }), value);
        } else if (value === true) {
          add("checked", prop.name);
        } else if (computedDateText(value, prop)) {
          // A formula's dates, and a rollup's — its earliest, latest, a range's start, each it shows — as a date's.
          const group = computedDateGroup(computedDateText(value, prop), prop, dateBy, weekStart);
          if (group) add(group.key, group.label, group.rank);
        } else if (isFormulaReference(value)) {
          admitPageLookup(value.id, context.bounds.projection, budget);
          add(`${value.kind}:${value.id}`, value.kind === "page" ? pageNamesNow.title(context.pagesById[value.id]) : personLabel(value.id));
        } else {
          // A mention its reader may not open, in marks, reads as its cell shows it (closedMentions.ts).
          const label = formulaItemText(value, context.bounds.formula).trim();
          if (label) add(`value:${label}`, lineWithClosedMentions(label));
        }
      };
      visit(computedFormulaValue(raw));
    }
    if (!seen.size) (blankText ? blank : empty).push(row);
  }
  const byLabel = prop.type === "relation" ? RELATION_TITLE_ORDER : byText;
  const entries = [...groups.entries()].sort(([leftKey, left], [rightKey, right]) =>
    left.rank - right.rank || (calendarKey(leftKey) && calendarKey(rightKey) ? leftKey.localeCompare(rightKey) : byLabel(left.label, right.label)));
  // A line by months or weeks runs from one with rows to the next, those between left out (Notion's, seen 2026-11-12).
  const buckets: ChartBucket[] = entries.map(([key, group], index) => ({ key, label: group.label, color: chartAutomaticGroupColor(index), automatic: true, rows: group.rows }));
  // A formula's or relation's rows with no value have their group even when there are none, at 0; a rollup's
  // only when there are any. A formula's rows of empty text are a group of their own of that name, colored as
  // a value (Notion's, seen 2026-11-12: "글식 없음" 0, "글식 없음" 3, "글0" …).
  if (blank.length) {
    buckets.push({ key: BLANK_BUCKET_KEY, label: noValueLabel(prop), color: "default", automatic: true, rows: blank, blank: true });
  }
  return empty.length || prop.type !== "rollup" && own ? appendEmptyBucket(buckets, empty, prop) : buckets;
}
