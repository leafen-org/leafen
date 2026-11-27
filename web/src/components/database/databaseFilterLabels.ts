import { i18next } from "@/i18n";
import type { FilterOperator, RelativeDateFilterUnit } from "@/lib/types";
import { FIXED_RELATIVE_DATE_FILTER_VALUES, type RelativeFilterDay } from "@/shared/database/queryRelativeDate.mjs";

// Filter labels and the rollup function sets filters read, apart from the
// database view label facade.

export function filterOperatorLabels(): Record<FilterOperator, string> {
  const tr = (key: string) => i18next.t(`databaseView:operators.${key}`);
  // A fixed relative operator ("past_month") reads as the relative date it
  // names, the way a date filter's pill words it ("지난 월", "Past month"),
  // as Notion's (seen 2026-11-19).
  const fixed = (operator: keyof typeof FIXED_RELATIVE_DATE_FILTER_VALUES) => {
    const value = FIXED_RELATIVE_DATE_FILTER_VALUES[operator];
    return dateFilterLabels().pillRelative(i18next.t(`databaseView:relativeDateDirections.${value.direction}`), 1, value.unit);
  };
  return {
    equals: tr("equals"),
    does_not_equal: tr("does_not_equal"),
    contains: tr("contains"),
    does_not_contain: tr("does_not_contain"),
    starts_with: tr("starts_with"),
    ends_with: tr("ends_with"),
    is_empty: tr("is_empty"),
    is_not_empty: tr("is_not_empty"),
    greater_than: tr("greater_than"),
    greater_than_or_equal_to: tr("greater_than_or_equal_to"),
    less_than: tr("less_than"),
    less_than_or_equal_to: tr("less_than_or_equal_to"),
    on_or_before: tr("on_or_before"),
    on_or_after: tr("on_or_after"),
    before: tr("before"),
    after: tr("after"),
    is_between: tr("is_between"),
    relative_to_today: tr("relative_to_today"),
    this_week: fixed("this_week"),
    past_week: fixed("past_week"),
    past_month: fixed("past_month"),
    past_year: fixed("past_year"),
    next_week: fixed("next_week"),
    next_month: fixed("next_month"),
    next_year: fixed("next_year"),
  };
}

export const NO_VALUE_FILTERS = new Set<FilterOperator>([
  "is_empty",
  "is_not_empty",
  "this_week",
  "past_week",
  "past_month",
  "past_year",
  "next_week",
  "next_month",
  "next_year",
]);
export const NUMERIC_ROLLUP_FUNCTIONS = new Set([
  "count",
  "count_all",
  "count_values",
  "unique",
  "count_unique",
  "empty",
  "count_empty",
  "not_empty",
  "percent_empty",
  "percent_not_empty",
  "checked",
  "unchecked",
  "percent_checked",
  "percent_unchecked",
  "sum",
  "average",
  "median",
  "min",
  "max",
  "range",
  "count_per_group",
  "percent_per_group",
]);
export const DATE_ROLLUP_FUNCTIONS = new Set(["earliest_date", "latest_date", "date_range"]);

/** The three answers to which of a rollup's related values a filter is about, and the patch each makes. */
export function rollupMatchChoices() {
  return (["any", "none", "every"] as const).map((key) => ({ key, label: i18next.t(`databaseView:rollupMatch.${key}`) }));
}

export const rollupMatchPatch = (key: string): { rollupMatch?: "none" | "every" } => ({ rollupMatch: key === "none" || key === "every" ? key : undefined });

type DateFilterPillOperator = "before" | "after" | "on_or_before" | "on_or_after";

/**
 * A relative date's unit words as its unit list names them, as Notion's (seen
 * 2026-11-11): one unit after "이번" and after a count of 1 ("월", "Month"),
 * several after a larger count ("개월", "Months").
 */
export function relativeDateUnitLabels(value: { direction: string; count?: number }): Record<RelativeDateFilterUnit, string> {
  const tr = (key: string): string => i18next.t(`databaseView:${key}`);
  const one = value.direction === "this" || (value.count ?? 1) <= 1;
  const prefix = one ? "dateFilter.thisUnits" : "relativeDateUnits";
  return { day: tr(`${prefix}.day`), week: tr(`${prefix}.week`), month: tr(`${prefix}.month`), year: tr(`${prefix}.year`) };
}

/**
 * A date filter editor's words, as Notion's (seen 2026-10-01): the date part
 * ("시작일", "종료일"), the day field, the relative date's units (a month is
 * "월" for "이번", "개월" after a count) and the pill's words ("이전 10월 7일",
 * "10월 5일 → 10월 9일", "지난 3 주").
 */
export function dateFilterLabels() {
  const tr = (key: string, options?: Record<string, unknown>): string =>
    i18next.t(`databaseView:dateFilter.${key}`, options ?? {});
  return {
    datePartFor: (name: string) => tr("datePartFor", { name }),
    parts: { start: tr("parts.start"), end: tr("parts.end") },
    dayPlaceholder: tr("dayPlaceholder"),
    relativeDays: (word: RelativeFilterDay) => tr(`relativeDays.${word}`),
    customDay: tr("relativeDays.custom"),
    relativeDayFor: (name: string) => tr("relativeDayFor", { name }),
    pickDay: tr("pickDay"),
    rangeStartFor: (name: string) => tr("rangeStartFor", { name }),
    rangeEndFor: (name: string) => tr("rangeEndFor", { name }),
    rangeFor: (name: string) => tr("rangeFor", { name }),
    pickRange: tr("pickRange"),
    relativeNote: tr("relativeNote"),
    previousMonth: tr("previousMonth"),
    nextMonth: tr("nextMonth"),
    thisUnits: {
      day: tr("thisUnits.day"),
      week: tr("thisUnits.week"),
      month: tr("thisUnits.month"),
      year: tr("thisUnits.year"),
    } satisfies Record<RelativeDateFilterUnit, string>,
    pill: (operator: DateFilterPillOperator, date: string) => tr(`pill.${operator}`, { date }),
    pillRange: (start: string, end: string) => tr("pill.range", { start, end }),
    pillRangeStart: (date: string) => tr("pill.rangeStart", { date }),
    pillRangeEnd: (date: string) => tr("pill.rangeEnd", { date }),
    pillRelative: (direction: string, count: number, unit: RelativeDateFilterUnit) => count > 1
      ? tr("pill.relativeMany", { direction, count, unit: tr(`pillUnitsMany.${unit}`) })
      : tr("pill.relativeOne", { direction, unit: tr(`pillUnitsOne.${unit}`) }),
  };
}
