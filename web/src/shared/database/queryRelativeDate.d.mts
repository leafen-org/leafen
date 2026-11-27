export const FIXED_RELATIVE_DATE_FILTER_OPERATORS: readonly string[];
export const RELATIVE_DATE_FILTER_OPERATOR: "relative_to_today";
export const RELATIVE_DATE_FILTER_OPERATORS: readonly string[];
export function isNoValueRelativeDateFilterOperator(value: unknown): boolean;
export function isRelativeDateFilterOperator(value: unknown): boolean;
export function isRelativeDateFilterValue(value: unknown): boolean;
export function isIsoCalendarDay(value: unknown): boolean;
export type RelativeDateFilterValueShape =
  | { direction: "this"; unit: "day" | "week" | "month" | "year" }
  | { direction: "past" | "next"; count: number; unit: "day" | "week" | "month" | "year" };
export const FIXED_RELATIVE_DATE_FILTER_VALUES: Readonly<Record<
  "this_week" | "past_week" | "past_month" | "past_year" | "next_week" | "next_month" | "next_year",
  RelativeDateFilterValueShape
>>;
export function relativeDateFilterDays(
  operator: unknown,
  referenceDay: unknown,
  filterValue?: unknown,
): { start: string; end: string } | null;
export function relativeDateFilterMatches(
  actualDay: unknown,
  operator: unknown,
  referenceDay: unknown,
  filterValue?: unknown,
): boolean;
export type RelativeFilterDay =
  | "today" | "tomorrow" | "yesterday" | "one_week_ago" | "one_week_from_now" | "one_month_ago" | "one_month_from_now";
export const RELATIVE_FILTER_DAYS: readonly RelativeFilterDay[];
export function isRelativeFilterDay(value: unknown): value is RelativeFilterDay;
export function relativeFilterDay(value: unknown, referenceDay: unknown): string | null;
