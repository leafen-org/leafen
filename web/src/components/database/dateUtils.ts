import { activeDateLocale } from "@/lib/i18n";
import { differenceInCalendarDays } from "@/lib/calendarDay";
import { i18next } from "@/i18n";
import type { DbProperty } from "@/lib/types";
import { clockOffset, formulaTimeZone, parseFormulaDate, validZone, wallFromUtc } from "./formulaDates";
import { FormulaDate } from "./formulaSyntax";
import { civilDate, civilAtInstant, civilDateFromParts, civilDays, civilMonth, civilMonths,
  localDate, readWallDateTime, clockAtInstant, readClock } from "@/lib/nativeDate";
import { toCalendarDate } from "@internationalized/date";
import { dayFieldText, plainTimeDraft, readsDayFirst, shortTime, twelveHourTime, withoutDirectionMarks } from "../../lib/dateWords";

const pad2 = (value: number) => String(value).padStart(2, "0");

function isDatePayload(value: unknown): value is { start?: unknown; end?: unknown } {
  return (
    !!value &&
    typeof value === "object" &&
    !(value instanceof Date) &&
    ("start" in value || "end" in value)
  );
}

function dateStart(value: unknown): unknown {
  if (isDatePayload(value)) return value.start ?? "";
  if (typeof value === "string") {
    const slash = value.indexOf("/");
    if (slash >= 0) return value.slice(0, slash);
  }
  return value;
}

export function dateKey(value: unknown) {
  if (value instanceof Date) {
    return Number.isNaN(value.getTime()) ? "" : civilDate(value).toString();
  }

  const source = dateStart(value);
  if (source == null || source === "") return "";
  const s = String(source).trim();
  // An absolute instant (explicit Z / numeric offset) denotes a point in time
  // whose calendar day depends on the viewer's zone — fall through to local-time
  // resolution below so day grouping/calendar placement match the local time the
  // cell displays and the shared filter/sort engine (query-core, given the browser
  // zone). A zone-less "YYYY-MM-DD[THH:MM]" is a wall-clock value taken verbatim.
  const isZonedInstant = /T.*(Z|[+-]\d{2}:?\d{2})$/.test(s);
  if (!isZonedInstant && /^\d{4}-\d{2}-\d{2}/.test(s)) return s.slice(0, 10);
  if (isZonedInstant) return civilAtInstant(s)?.toString() ?? "";
  const parsed = new Date(s);
  if (Number.isNaN(parsed.getTime())) return "";
  const y = parsed.getFullYear();
  const m = String(parsed.getMonth() + 1).padStart(2, "0");
  const d = String(parsed.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

export function makeDate(year: number, month: number, day: number): Date | null {
  const date = civilDateFromParts(year, month, day);
  return date ? localDate(date) : null;
}

export function parseDate(value: unknown): Date | null {
  const raw = dateKey(value);
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(raw);
  if (!m) return null;
  return makeDate(Number(m[1]), Number(m[2]), Number(m[3]));
}

export function parseDateDraft(
  value: string,
  fallbackYear = new Date().getFullYear()
): Date | null {
  const raw = value.trim();
  if (!raw) return null;

  // Korean display formats round-trip through the date-cell draft inputs:
  // "2026. 7. 9." (ko-KR toLocaleDateString) and "2026년 7월 9일". Parse them
  // explicitly — Date.parse support for these is engine-dependent.
  const koreanDraft = /^(\d{4})(?:\.|년)\s*(\d{1,2})(?:\.|월)\s*(\d{1,2})(?:\.|일)?\s*$/.exec(raw);
  if (koreanDraft) {
    return makeDate(Number(koreanDraft[1]), Number(koreanDraft[2]), Number(koreanDraft[3]));
  }

  // Numeric drafts must win before parseDate(): its new Date() fallback would
  // consume "7/4" with V8's default year (2001) and "1/2/99" as 1999,
  // bypassing fallbackYear and the two-digit-year expansion below.
  const numeric = /^(\d{1,2})[\/.-](\d{1,2})(?:[\/.-](\d{2,4}))?$/.exec(raw);
  if (numeric) {
    let year = numeric[3] ? Number(numeric[3]) : fallbackYear;
    if (year < 100) year += 2000;
    return makeDate(year, Number(numeric[1]), Number(numeric[2]));
  }

  const iso = parseDate(raw);
  if (iso) return iso;

  const parsed = new Date(raw);
  if (Number.isNaN(parsed.getTime())) return null;
  return makeDate(parsed.getFullYear(), parsed.getMonth() + 1, parsed.getDate());
}

export function formatDateInput(date: Date | null): string {
  if (!date) return "";
  return date.toLocaleDateString(activeDateLocale(), {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

/**
 * A day as the date editor's input writes it: in the property's numeric
 * format, as Notion's editor does ("2023/08/30"), and otherwise as its field
 * reads in the language (dayFieldText).
 */
export function formatDateDraft(date: Date | null, dateFormat?: string): string {
  if (!date) return "";
  const day = pad2(date.getDate());
  const month = pad2(date.getMonth() + 1);
  const year = date.getFullYear();
  if (dateFormat === "month_day_year") return `${month}/${day}/${year}`;
  if (dateFormat === "day_month_year") return `${day}/${month}/${year}`;
  if (dateFormat === "year_month_day") return `${year}/${month}/${day}`;
  return dayFieldText(date);
}

/** A typed day, read first in the property's numeric format, else in the language's order ("1.11.2026" day first in German). */
export function parseDateDraftIn(value: string, dateFormat?: string, fallbackYear?: number): Date | null {
  const typed = withoutDirectionMarks(value).trim();
  const numeric = /^(\d{1,4})[/.-] ?(\d{1,2})[/.-] ?(\d{1,4})\.?$/u.exec(typed);
  if (numeric) {
    const [first, second, third] = numeric.slice(1).map(Number);
    if (numeric[1].length === 4) return makeDate(first, second, third);
    const dayFirst = dateFormat === "day_month_year" || (!NUMERIC_DAY_FORMATS.has(dateFormat ?? "") && readsDayFirst());
    if (dayFirst && numeric[3].length >= 2) return makeDate(third < 100 ? third + 2000 : third, second, first);
  }
  return parseDateDraft(typed, fallbackYear);
}
const NUMERIC_DAY_FORMATS = new Set(["month_day_year", "day_month_year", "year_month_day"]);

/** A time as the date editor's input writes it, in the property's time format. */
export function formatTimeDraft(time: string, timeFormat?: string) {
  return formatClock(time, { timeFormat: timeFormat === "24" ? "24" : "12" });
}

export function startOfMonth(date: Date) {
  return civilMonth(date);
}

export function addDays(date: Date, days: number) {
  const next = civilDays(date, days);
  next.setHours(date.getHours(), date.getMinutes(), date.getSeconds(), date.getMilliseconds());
  return next;
}

export function addMonths(date: Date, months: number) {
  return civilMonths(date, months);
}

export function extractTime(value: unknown): string {
  const source = dateStart(value);
  const m = /[T ](\d{2}:\d{2})/.exec(String(source ?? ""));
  return m ? m[1] : "";
}

/** A time on the 12-hour clock in the language's order and day period words: "2:05 PM", "오후 2:05", "2:05 م". */
export function formatTime12(time: string): string {
  const [h, m] = time.split(":").map(Number);
  return Number.isNaN(h) || Number.isNaN(m) ? time : twelveHourTime(h, m);
}

// A typed time takes am/pm, 오전/오후 or the language's own day period words
// (plainTimeDraft) before or after the hour, its minutes after ":" or ".".
const TIME_DRAFT = /^(?:(오전|오후)\s*)?(\d{1,2})(?:[:.](\d{1,2}))?\s*(a|am|p|pm|오전|오후)?$/u;

export function parseTimeDraft(value: string): string | null {
  const raw = plainTimeDraft(value);
  if (!raw) return null;
  const m = TIME_DRAFT.exec(raw);
  if (!m || (m[1] && m[4])) return null;
  let hour = Number(m[2]);
  const minute = Number(m[3] ?? "0");
  const word = m[1] ?? m[4];
  const period = word === "오전" ? "a" : word === "오후" ? "p" : word?.[0];
  if (period) {
    if (hour < 1 || hour > 12) return null;
    if (period === "p" && hour < 12) hour += 12;
    if (period === "a" && hour === 12) hour = 0;
  }
  const time = readClock(`${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`);
  return time?.toString().slice(0, 5) ?? null;
}

export function extractEnd(value: unknown): string {
  if (isDatePayload(value)) return value.end ? String(value.end) : "";
  const s = String(value ?? "");
  const slash = s.indexOf("/");
  return slash >= 0 ? s.slice(slash + 1) : "";
}

/**
 * Set a date value's day to `targetDay` while preserving its time-of-day. Used
 * by calendar/timeline drag so moving an event to another day doesn't discard
 * the time (dragging with a bare `dateKey(day)` silently lost `T14:00`).
 */
export function withTimeOf(value: unknown, targetDay: Date): string {
  const time = extractTime(value);
  const key = dateKey(targetDay);
  return time ? `${key}T${time}` : key;
}

/** A date as it was stored, with a new start and end: a "start[/end]" string, or an object keeping its clock and reminder. */
function withDateParts(value: unknown, start: string, end: string): unknown {
  if (isDatePayload(value)) {
    const { end: _end, ...kept } = value as Record<string, unknown>;
    return { ...kept, start, ...(end ? { end } : {}) };
  }
  return end ? `${start}/${end}` : start;
}

/** A date value's start and end as written, the end being the start when it has no range. */
export function writtenDateRange(value: unknown): { start: string; end: string } {
  const start = String(dateStart(value) ?? "");
  return { start, end: extractEnd(value) || start };
}

/**
 * A date value with a new start and end, keeping how it was stored; an end
 * the same as the start leaves a single date.
 */
export function withDateRange(value: unknown, start: string, end: string): unknown {
  return withDateParts(value, start, end && end !== start ? end : "");
}

/** A written date and time moved by whole days and hours on its own clock. */
function shiftWrittenPart(part: unknown, days: number, hours = 0): string {
  const text = String(part ?? "").trim();
  const day = writtenDate(text);
  if (!day) return text;
  const time = extractTime(text);
  // What follows the minutes (seconds, a "Z" or an offset) stays as written.
  const rest = time ? text.slice(text.indexOf(time) + time.length) : "";
  if (!hours) return withTimeOf(text, addDays(day, days)) + rest;
  const moment = readWallDateTime(`${dateKey(day)}T${time || "00:00"}`)?.add({ days, minutes: Math.round(hours * 60) });
  return moment ? `${moment.toString().slice(0, 16)}${rest}` : text;
}

/**
 * Move a date value by whole days and hours, its end with it, keeping how it
 * was stored: a date on a clock of its own keeps its time zone and reminder.
 */
export function shiftDateValueBy(value: unknown, days: number, hours = 0): unknown {
  const end = extractEnd(value);
  return withDateParts(value, shiftWrittenPart(dateStart(value), days, hours), end ? shiftWrittenPart(end, days, hours) : "");
}

/**
 * Move a (possibly range) date value so the day it shows on becomes
 * `targetDay`, keeping its times, its duration and how it was stored. A date
 * on a clock of its own moves by the days between the day it shows on for the
 * viewer and the target, as Notion's calendar places it by that day.
 */
export function shiftDateValueToDay(value: unknown, targetDay: Date): unknown {
  const shown = shownDate(value);
  if (!shown) return withDateParts(value, withTimeOf(dateStart(value), targetDay), extractEnd(value));
  return shiftDateValueBy(value, differenceInCalendarDays(targetDay, shown.date));
}

type DateDisplayOptions = {
  locale?: string;
  /** The property's date format, as Notion's date editor sets it for the property. */
  dateFormat?: string;
  /** The property's time format: "12" (the default), "24" or "hidden". */
  timeFormat?: string;
};

/** A date's day in the property's format, as Notion writes it (seen 2026-09-30). */
function formatDay(date: Date, options: DateDisplayOptions, locale: string) {
  const day = date.getDate();
  const month = date.getMonth() + 1;
  const year = date.getFullYear();
  switch (options.dateFormat) {
    case "short":
      return locale.toLowerCase().startsWith("ko")
        ? `${month}월 ${day}`
        : date.toLocaleDateString(locale, { month: "short", day: "numeric" });
    case "month_day_year":
      return `${pad2(month)}/${pad2(day)}/${year}`;
    case "day_month_year":
      return `${pad2(day)}/${pad2(month)}/${year}`;
    case "year_month_day":
      return `${year}/${pad2(month)}/${pad2(day)}`;
    case "relative": {
      const today = new Date();
      return relativeDayLabel(date, new Date(today.getFullYear(), today.getMonth(), today.getDate()), locale)
        ?? fullDay(date, locale);
    }
    default:
      return fullDay(date, locale);
  }
}

function mondayOf(date: Date) {
  return civilDays(date, -((date.getDay() + 6) % 7));
}

/**
 * A day relative to today, as Notion's relative dates and date mentions
 * name it (seen 2026-10-02): today, tomorrow and yesterday, a weekday of
 * this week, "다음주"/"지난주" and a weekday for the weeks either side —
 * weeks from Monday — and null beyond, where the full date stands.
 */
export function relativeDayLabel(date: Date, today: Date, locale = activeDateLocale(), weekdayWidth: "long" | "short" = "long"): string | null {
  const offset = differenceInCalendarDays(date, today);
  if (offset === 0) return i18next.t("dateUtils:relative.today");
  if (offset === -1) return i18next.t("dateUtils:relative.yesterday");
  if (offset === 1) return i18next.t("dateUtils:relative.tomorrow");
  const weeks = Math.round(differenceInCalendarDays(mondayOf(date), mondayOf(today)) / 7);
  // A schedule's next run says the weekday short ("다음주 월"), as Notion's (seen 2026-10-24).
  const weekday = date.toLocaleDateString(locale, { weekday: weekdayWidth });
  if (weeks === 0) return weekday;
  if (weeks === 1) return i18next.t("dateUtils:relative.nextWeekday", { weekday });
  if (weeks === -1) return i18next.t("dateUtils:relative.lastWeekday", { weekday });
  return null;
}

/** A day in one of the date formats, outside "relative", as a property shows it. */
export function formatDayAs(date: Date, dateFormat: string, locale = activeDateLocale()) {
  return formatDay(date, { dateFormat }, locale);
}

/** The full date, Notion's default in every language ("November 3, 2026", seen 2026-11-03). */
function fullDay(date: Date, locale: string) {
  let format = fullDayFormats.get(locale);
  if (!format) {
    format = new Intl.DateTimeFormat(locale, { year: "numeric", month: "long", day: "numeric" });
    fullDayFormats.set(locale, format);
  }
  return format.format(date);
}

// One formatter a language: toLocaleDateString makes one for each call, and
// a table's date column writes every row's date again with each letter typed
// in any of its cells (2026-11-12).
const fullDayFormats = new Map<string, Intl.DateTimeFormat>();

/**
 * A time in the property's time format; "" when the format hides times. Its
 * default ("12") is Notion's unset one, the language's short time (shortTime).
 */
function formatClock(time: string, options: DateDisplayOptions) {
  if (options.timeFormat === "hidden") return "";
  const [hour, minute] = time.split(":").map(Number);
  if (Number.isNaN(hour) || Number.isNaN(minute)) return time;
  return options.timeFormat === "24" ? `${hour}:${pad2(minute)}` : shortTime(hour, minute);
}

// A date part's own clock: the instant of a time kept on a clock of its own
// (its time zone, its written offset, or UTC for "Z") and that clock's offset
// from UTC there; null for a day, or a time on the viewer's clock.
function ownClockOf(part: unknown, zone: string): { instant: number; offset: number } | null {
  const text = String(part ?? "").trim();
  if (!extractTime(part) || (!zone && !writesOwnClock(text))) return null;
  const moment = parseFormulaDate(new FormulaDate(text, zone), formulaTimeZone(undefined));
  if (!moment) return null;
  return { instant: moment.instant, offset: moment.own ? clockOffset(moment.own, moment.instant) : 0 };
}

// The day and time a date shows: a time kept on a clock of its own on the
// viewer's clock, as Notion's date cells show it, or on its own clock (`own`),
// as a copy gives it. A day, or a time without a clock of its own, shows as
// written.
function shownDateParts(part: unknown, zone: string, own = false): { date: Date; time: string } | null {
  const date = parseDate(part);
  if (!date) return null;
  const time = extractTime(part);
  const clock = ownClockOf(part, zone);
  if (!clock) return { date, time };
  if (own) {
    const wall = wallFromUtc(clock.instant + clock.offset);
    return { date: makeDate(wall.year, wall.month, wall.day) ?? date, time: `${pad2(wall.hour)}:${pad2(wall.minute)}` };
  }
  // Resolve ambiguous/nonexistent native wall times in the shared formula
  // policy (matching Go time.Date), then represent that instant with the
  // date core. Library disambiguation defaults never change stored meaning.
  const shown = clockAtInstant(new Date(clock.instant), formulaTimeZone(undefined));
  return {
    date: localDate(toCalendarDate(shown)),
    time: `${pad2(shown.hour)}:${pad2(shown.minute)}`,
  };
}

function formatDatePart(shown: { date: Date; time: string } | null, options: DateDisplayOptions = {}) {
  if (!shown) return "";
  const { date, time } = shown;
  // Display-only: the locale controls presentation (month names, ordering),
  // never the stored value. Defaults to the active app locale.
  const locale = options.locale ?? activeDateLocale();
  const day = formatDay(date, options, locale);
  const clock = time ? formatClock(time, options) : "";
  return clock ? `${day} ${clock}` : day;
}

/**
 * The day and time a date value shows on for the viewer: a time on a clock of
 * its own on the viewer's clock, as its cell shows it and as Notion's calendar
 * places it (seen 2026-09-30); a day, or a time without a clock of its own,
 * as written.
 */
export function shownDate(value: unknown): { date: Date; time: string } | null {
  return shownDateParts(dateStart(value), namedTimeZone(value));
}

/** The day a date value's end shows on for the viewer, or null without an end. */
export function shownEndDate(value: unknown): { date: Date; time: string } | null {
  const end = extractEnd(value);
  return end ? shownDateParts(end, namedTimeZone(value)) : null;
}

// A time written with a "Z" or an offset keeps a clock of its own.
function writesOwnClock(text: string) {
  return /(?:z|[+-]\d{2}:?\d{2})$/iu.test(text);
}

/**
 * Whether a date value keeps its times on a clock of its own: a time zone, or
 * a time written with a "Z" or an offset.
 */
export function keepsOwnClock(value: unknown) {
  const timed = (part: unknown) => Boolean(extractTime(part)) && writesOwnClock(String(part ?? "").trim());
  return Boolean(namedTimeZone(value)) || timed(dateStart(value)) || timed(extractEnd(value));
}

/** The "YYYY-MM-DD" of the day a date value shows on for the viewer. */
export function shownDateKey(value: unknown) {
  const shown = shownDate(value);
  return shown ? dateKey(shown.date) : "";
}

function namedTimeZone(value: unknown) {
  const stored = isDatePayload(value) ? (value as { timeZone?: unknown }).timeZone : undefined;
  return typeof stored === "string" && validZone(stored) ? stored : "";
}

/**
 * The time zone a stored date keeps its times in: its own, UTC for a time
 * written with "Z", or "" for a time on the viewer's clock.
 */
export function storedTimeZone(value: unknown) {
  return namedTimeZone(value)
    || (/[T ]\d{2}:\d{2}\S*(?:z|[+-]00:?00)$/iu.test(String(dateStart(value) ?? "").trim()) ? "UTC" : "");
}

/**
 * What a time written with an offset of its own other than UTC's keeps after
 * its minutes (":00.000-04:00", as Notion's API writes one), or "".
 */
export function writtenOffset(value: unknown) {
  const text = String(dateStart(value) ?? "").trim();
  const match = /[T ]\d{2}:\d{2}(\S*[+-]\d{2}:?\d{2})$/u.exec(text);
  return match && !storedTimeZone(value) ? match[1] : "";
}

/** A date's day as it is written, on its own clock: its leading "YYYY-MM-DD". */
export function writtenDate(value: unknown): Date | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})/u.exec(String(dateStart(value) ?? "").trim());
  return match ? makeDate(Number(match[1]), Number(match[2]), Number(match[3])) : parseDate(value);
}

export function formatDate(value: unknown, options: DateDisplayOptions = {}) {
  return formatDateOnClock(value, options, false);
}

/**
 * A date as its own zone's clock reads it, as a copy of Notion's date cell gives it (seen
 * 2026-11-06: a 16:00 New York time copies as "2026년 11월 5일 16:00 (GMT-5)" where its cell
 * shows Seoul's "2026년 11월 6일 6:00 (GMT+9)").
 */
function formatDateInItsZone(value: unknown, options: DateDisplayOptions = {}) {
  return formatDateOnClock(value, options, true);
}

// A date kept on a clock whose offset isn't the viewer's says which clock it
// shows on (Notion's, seen 2026-11-12): a cell shows it on the viewer's clock
// and names the viewer's offset, "… 오후 7:30 (GMT+9)", a copy on its own clock
// and names that, "… 오전 10:30 (UTC)". The name follows the start; a range
// ending on another day names it after its end too, "… (GMT+9) → 2026년 1월 4일
// 오후 5:00 (GMT+9)", one ending the same day only after the start, "… (GMT+9) →
// 오전 2:00". A time zone at the viewer's offset (Tokyo's for Seoul's) and a
// time on the viewer's clock say none.
function formatDateOnClock(value: unknown, options: DateDisplayOptions, ownClock: boolean) {
  const startValue = dateStart(value);
  const named = namedTimeZone(value);
  const viewer = formulaTimeZone(undefined);
  const startShown = shownDateParts(startValue, named, ownClock);
  const start = formatDatePart(startShown, options);
  if (!start && !extractEnd(value)) return ""; // a start it can't read shows its end alone, the viewer's clock named
  const startClock = ownClockOf(startValue, named);
  const other = !start || Boolean(startClock && startClock.offset !== clockOffset({ zone: viewer }, startClock.instant));
  const said = (part: unknown) => {
    const clock = other ? ownClockOf(part, named) : null;
    if (!clock) return "";
    return ` (${ownClock ? clockName(clock.offset, named || (clock.offset === 0 ? "UTC" : ""))
      : clockName(clockOffset({ zone: viewer }, clock.instant), viewer)})`;
  };
  const endPart = extractEnd(value);
  const endShown = endPart ? shownDateParts(endPart, named, ownClock) : null;
  const end = formatDatePart(endShown, options);
  if (!end) return `${start}${said(startValue)}${endPart ? " →" : ""}`; // an end it can't read: its arrow alone, as Notion's formula cell
  const endClock = endShown?.time ? formatClock(endShown.time, options) : "";
  if (startShown && endClock && dateKey(startShown.date) === dateKey(endShown!.date)) {
    return `${start}${said(startValue)} → ${endClock}`;
  }
  return `${start}${said(startValue)} → ${end}${said(endPart)}`;
}

// A clock's name as Notion's date says it (seen 2026-11-12): "UTC" for UTC's
// own, and otherwise its offset, "GMT+9", "GMT-5", "GMT+5:30", "GMT+0" for
// London's in winter.
function clockName(offset: number, zone: string) {
  if (zone === "UTC" || zone === "Etc/UTC") return "UTC";
  const minutes = Math.round(Math.abs(offset) / 60_000);
  const rest = minutes % 60;
  return `GMT${offset < 0 ? "-" : "+"}${Math.floor(minutes / 60)}${rest ? `:${pad2(rest)}` : ""}`;
}

export function formatDateForProperty(value: unknown, prop: Pick<DbProperty, "config">) {
  const formats = { dateFormat: prop.config?.dateFormat, timeFormat: prop.config?.timeFormat };
  return formatDate(value, formats);
}

/** A date property's value as a copy gives it: on its own zone's clock (formatDateInItsZone), in the property's formats. */
export function copiedDateForProperty(value: unknown, prop: Pick<DbProperty, "config">) {
  return formatDateInItsZone(value, { dateFormat: prop.config?.dateFormat, timeFormat: prop.config?.timeFormat });
}

/**
 * A created or last edited time as its property shows it: in the date and
 * time formats its "속성 편집" sets, as Notion's (seen 2026-10-05), or as
 * Notion's timestamp while none is set.
 */
export function formatTimestampForProperty(value: unknown, prop?: Pick<DbProperty, "config">) {
  const dateFormat = prop?.config?.dateFormat;
  const timeFormat = prop?.config?.timeFormat;
  if (!dateFormat && !timeFormat) return formatNotionTimestamp(value);
  return formatDate(instantOnViewerClock(value), { dateFormat, timeFormat });
}

/**
 * A moment in time written as a UTC instant ("…Z": a page's created or
 * edited time) as the viewer's clock reads it, a time with no clock of its
 * own, so it shows as a time on that clock and names none, unlike a date set
 * in UTC (formatDateOnClock).
 */
export function instantOnViewerClock(value: unknown): unknown {
  const text = typeof value === "string" ? value.trim() : "";
  if (!text || !extractTime(text) || !/z$/iu.test(text)) return value;
  const shown = shownDateParts(text, "");
  return shown ? `${dateKey(shown.date)}T${shown.time}` : value;
}

export function formatNotionTimestamp(value: unknown) {
  const source = dateStart(value);
  if (source == null || source === "") return "";
  const raw = String(source);
  const trimmed = raw.trim();
  const literalDay = /^\d{4}-\d{2}-\d{2}$/.test(trimmed) ? parseDate(trimmed) : null;
  const date = literalDay ?? new Date(raw);
  if (Number.isNaN(date.getTime())) return String(source);
  // The full date and the language's short time, as Notion's created and
  // edited times (seen 2026-11-19: "19. November 2026 11:03", "19 نوفمبر 2026 11:03 ص").
  return `${fullDay(date, activeDateLocale())} ${shortTime(date.getHours(), date.getMinutes())}`;
}
