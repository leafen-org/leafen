export const FIXED_RELATIVE_DATE_FILTER_OPERATORS = Object.freeze([
  "this_week",
  "past_week",
  "past_month",
  "past_year",
  "next_week",
  "next_month",
  "next_year",
]);

export const RELATIVE_DATE_FILTER_OPERATOR = "relative_to_today";
export const RELATIVE_DATE_FILTER_OPERATORS = Object.freeze([
  ...FIXED_RELATIVE_DATE_FILTER_OPERATORS,
  RELATIVE_DATE_FILTER_OPERATOR,
]);

const FIXED_RELATIVE_DATE_FILTER_OPERATOR_SET = new Set(FIXED_RELATIVE_DATE_FILTER_OPERATORS);
const RELATIVE_DATE_FILTER_OPERATOR_SET = new Set(RELATIVE_DATE_FILTER_OPERATORS);
const RELATIVE_DATE_FILTER_UNITS = new Set(["day", "week", "month", "year"]);

/** @param {unknown} value */
export function isNoValueRelativeDateFilterOperator(value) {
  return typeof value === "string" && FIXED_RELATIVE_DATE_FILTER_OPERATOR_SET.has(value);
}

/** @param {unknown} value */
export function isRelativeDateFilterOperator(value) {
  return typeof value === "string" && RELATIVE_DATE_FILTER_OPERATOR_SET.has(value);
}

/** @param {unknown} value */
export function isRelativeDateFilterValue(value) {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return false;
  const keys = Object.keys(value).sort();
  const direction = value.direction;
  const unit = value.unit;
  if (!RELATIVE_DATE_FILTER_UNITS.has(unit)) return false;
  if (direction === "this") {
    return keys.length === 2 && keys[0] === "direction" && keys[1] === "unit";
  }
  return (
    (direction === "past" || direction === "next")
    && keys.length === 3
    && keys[0] === "count"
    && keys[1] === "direction"
    && keys[2] === "unit"
    && Number.isSafeInteger(value.count)
    && value.count > 0
  );
}

/** @param {unknown} value */
export function isIsoCalendarDay(value) {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const parsed = new Date(`${value}T00:00:00.000Z`);
  return Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value;
}

/** @param {Date} value */
function utcDay(value) {
  return new Date(Date.UTC(value.getUTCFullYear(), value.getUTCMonth(), value.getUTCDate()));
}

/**
 * Shift a UTC calendar day while clamping short-month and leap-year endpoints.
 *
 * @param {Date} value
 * @param {number} count
 * @param {"day" | "week" | "month" | "year"} unit
 */
function shiftedClampedUtcDay(value, count, unit) {
  const shifted = utcDay(value);
  if (unit === "day" || unit === "week") {
    shifted.setUTCDate(shifted.getUTCDate() + count * (unit === "week" ? 7 : 1));
    return shifted;
  }

  const originalDay = shifted.getUTCDate();
  shifted.setUTCDate(1);
  if (unit === "month") shifted.setUTCMonth(shifted.getUTCMonth() + count);
  else shifted.setUTCFullYear(shifted.getUTCFullYear() + count);
  if (!Number.isFinite(shifted.getTime())) return shifted;

  const nextMonth = new Date(Date.UTC(shifted.getUTCFullYear(), shifted.getUTCMonth() + 1, 1));
  nextMonth.setUTCDate(0);
  shifted.setUTCDate(Math.min(originalDay, nextMonth.getUTCDate()));
  return shifted;
}

/**
 * The days a relative value covers, from `start` up to but not including
 * `end`, as Notion's (seen 2026-10-01): "이번" is the calendar day, week
 * (from Sunday), month or year holding today; "지난 N" runs from N units
 * before today to today and "다음 N" from today to N units after, both ends
 * included.
 *
 * @param {Date} today
 * @param {{ direction: "past" | "next" | "this", count?: number, unit: "day" | "week" | "month" | "year" }} value
 */
function relativeDateValueInterval(today, value) {
  const dayAfter = (/** @type {Date} */ day) => shiftedClampedUtcDay(day, 1, "day");
  if (value.direction === "past") {
    return [shiftedClampedUtcDay(today, -value.count, value.unit), dayAfter(today)];
  }
  if (value.direction === "next") {
    return [utcDay(today), dayAfter(shiftedClampedUtcDay(today, value.count, value.unit))];
  }

  const start = utcDay(today);
  if (value.unit === "week") {
    start.setUTCDate(start.getUTCDate() - start.getUTCDay());
  } else if (value.unit === "month") {
    start.setUTCDate(1);
  } else if (value.unit === "year") {
    start.setUTCMonth(0, 1);
  }
  return [start, shiftedClampedUtcDay(start, 1, value.unit)];
}

/**
 * The relative value each fixed operator ("this_week", "past_month") names,
 * so both read alike and the editor shows them as "오늘 기준".
 */
export const FIXED_RELATIVE_DATE_FILTER_VALUES = Object.freeze({
  this_week: Object.freeze({ direction: "this", unit: "week" }),
  past_week: Object.freeze({ direction: "past", count: 1, unit: "week" }),
  past_month: Object.freeze({ direction: "past", count: 1, unit: "month" }),
  past_year: Object.freeze({ direction: "past", count: 1, unit: "year" }),
  next_week: Object.freeze({ direction: "next", count: 1, unit: "week" }),
  next_month: Object.freeze({ direction: "next", count: 1, unit: "month" }),
  next_year: Object.freeze({ direction: "next", count: 1, unit: "year" }),
});

/**
 * Match one ISO calendar day against Leafen's relative date operators: the
 * seven fixed operators name the values in FIXED_RELATIVE_DATE_FILTER_VALUES,
 * and relative_to_today carries its own direction/count/unit value.
 *
 * @param {unknown} actualDay
 * @param {unknown} operator
 * @param {unknown} referenceDay
 * @param {unknown} [filterValue]
 */
export function relativeDateFilterMatches(actualDay, operator, referenceDay, filterValue) {
  if (
    !isIsoCalendarDay(actualDay)
    || !isIsoCalendarDay(referenceDay)
    || !isRelativeDateFilterOperator(operator)
  ) {
    return false;
  }
  const value = operator === RELATIVE_DATE_FILTER_OPERATOR
    ? filterValue
    : FIXED_RELATIVE_DATE_FILTER_VALUES[/** @type {keyof typeof FIXED_RELATIVE_DATE_FILTER_VALUES} */ (operator)];
  if (!isRelativeDateFilterValue(value)) return false;

  const actual = new Date(`${actualDay}T00:00:00.000Z`).getTime();
  const [start, end] = relativeDateValueInterval(new Date(`${referenceDay}T00:00:00.000Z`), value);
  if (!Number.isFinite(start.getTime()) || !Number.isFinite(end.getTime())) return false;
  return actual >= start.getTime() && actual < end.getTime();
}

/**
 * The days a relative date filter covers from today, first and last, for
 * its editor's calendar; null when the value is not one.
 *
 * @param {unknown} operator
 * @param {unknown} referenceDay
 * @param {unknown} [filterValue]
 * @returns {{ start: string, end: string } | null}
 */
export function relativeDateFilterDays(operator, referenceDay, filterValue) {
  if (!isIsoCalendarDay(referenceDay) || !isRelativeDateFilterOperator(operator)) return null;
  const value = operator === RELATIVE_DATE_FILTER_OPERATOR
    ? filterValue
    : FIXED_RELATIVE_DATE_FILTER_VALUES[/** @type {keyof typeof FIXED_RELATIVE_DATE_FILTER_VALUES} */ (operator)];
  if (!isRelativeDateFilterValue(value)) return null;
  const [start, end] = relativeDateValueInterval(new Date(`${referenceDay}T00:00:00.000Z`), value);
  if (!Number.isFinite(start.getTime()) || !Number.isFinite(end.getTime())) return null;
  const last = shiftedClampedUtcDay(end, -1, "day");
  return { start: start.toISOString().slice(0, 10), end: last.toISOString().slice(0, 10) };
}

/**
 * The days a day condition's value may name from today instead of a date, as
 * Notion's "사용자 지정 날짜 ⌄" choices (seen 2026-11-11: "1주 전" reads back
 * through its API as before "one_week_ago"), each today shifted; a month
 * clamps to its last day (the server's databaseQueryRelativeFilterDays).
 */
export const RELATIVE_FILTER_DAYS = Object.freeze([
  "today", "tomorrow", "yesterday", "one_week_ago", "one_week_from_now", "one_month_ago", "one_month_from_now",
]);

/** @type {Readonly<Record<string, [number, "day" | "week" | "month"]>>} */
const RELATIVE_FILTER_DAY_SHIFTS = Object.freeze({
  today: [0, "day"], tomorrow: [1, "day"], yesterday: [-1, "day"],
  one_week_ago: [-1, "week"], one_week_from_now: [1, "week"],
  one_month_ago: [-1, "month"], one_month_from_now: [1, "month"],
});

/** @param {unknown} value */
export function isRelativeFilterDay(value) {
  return typeof value === "string" && Object.hasOwn(RELATIVE_FILTER_DAY_SHIFTS, value);
}

/**
 * The day such a value names from `referenceDay`, or null for any other value.
 *
 * @param {unknown} value
 * @param {unknown} referenceDay
 * @returns {string | null}
 */
export function relativeFilterDay(value, referenceDay) {
  if (!isRelativeFilterDay(value) || !isIsoCalendarDay(referenceDay)) return null;
  const [count, unit] = RELATIVE_FILTER_DAY_SHIFTS[/** @type {string} */ (value)];
  const day = shiftedClampedUtcDay(new Date(`${referenceDay}T00:00:00.000Z`), count, unit);
  return Number.isFinite(day.getTime()) ? day.toISOString().slice(0, 10) : null;
}
