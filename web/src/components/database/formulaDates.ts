import { formatFormulaDate, isoWeek, wallMilliseconds, weekday, type FormulaClock, type FormulaMoment, type FormulaWall as Wall } from "./formulaFormats";
import type { FormulaViewer } from "./formulaLocales";
import { FormulaDate, type FormulaValue } from "./formulaSyntax";
import { finiteNumber, toText } from "./formulaValues";
import { FormulaRunError } from "./formulaRunError";
import { BUSINESS_CALENDARS } from "../../shared/businessCalendar.generated";

// Formula dates are read in the viewer's time zone: a day without a time is
// that calendar day, a time without a zone is a wall-clock time there, and
// an instant shows its hour, day and week as it falls there. A time with its
// own offset, or a date whose time zone is named, keeps that clock: adding
// to it counts on it, and it is written at its own offset. Results keep the
// input's precision: a day stays a day, and a time on the viewer's clock is
// written as its UTC instant. As in Notion, parseDate() and a day given
// hours read on the viewer's clock. The server reads dates the same way
// (database_formula_dates.go).

const ISO_DATE = /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2}))?(?:\.(\d+))?(Z|[+-]\d{2}:?\d{2})?)?$/;
const formatters = new Map<string, Intl.DateTimeFormat>();

let viewerTimeZone: string | undefined;

// formulaTimeZone is the zone formula dates are read in: the one given, or
// the viewer's, or UTC when the zone is unknown.
export function formulaTimeZone(timeZone: string | undefined) {
  viewerTimeZone ??= Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  const zone = timeZone || viewerTimeZone;
  try {
    zoneFormatter(zone);
    return zone;
  } catch {
    return "UTC";
  }
}

function zoneFormatter(timeZone: string) {
  let formatter = formatters.get(timeZone);
  if (!formatter) {
    formatter = new Intl.DateTimeFormat("en-US", {
      timeZone, hourCycle: "h23", era: "short", year: "numeric", month: "numeric", day: "numeric",
      hour: "numeric", minute: "numeric", second: "numeric",
    });
    formatters.set(timeZone, formatter);
  }
  return formatter;
}

export function wallFromUtc(milliseconds: number): Wall {
  const date = new Date(milliseconds);
  return {
    year: date.getUTCFullYear(), month: date.getUTCMonth() + 1, day: date.getUTCDate(),
    hour: date.getUTCHours(), minute: date.getUTCMinutes(), second: date.getUTCSeconds(),
    millisecond: date.getUTCMilliseconds(),
  };
}

function wallAt(instant: number, timeZone: string): Wall {
  if (timeZone === "UTC" || !countable(instant)) return wallFromUtc(instant);
  const millisecond = ((instant % 1000) + 1000) % 1000;
  const parts: Record<string, number> = {};
  let bc = false;
  for (const part of zoneFormatter(timeZone).formatToParts(new Date(instant - millisecond))) {
    if (part.type === "era") bc = part.value === "BC";
    else if (part.type !== "literal") parts[part.type] = Number(part.value);
  }
  // A year before 1 is counted back from its era's (1 BC is year 0).
  return {
    year: bc ? 1 - parts.year : parts.year, month: parts.month, day: parts.day,
    hour: parts.hour, minute: parts.minute, second: parts.second, millisecond,
  };
}

// countable is whether an instant is one JavaScript's dates count, 100,000,000 days either side of 1970.
const countable = (instant: number) => Math.abs(instant) <= 8.64e15;

// instantOf reads a wall-clock time in a zone as Go's time.Date does.
function instantOf(wall: Wall, timeZone: string) {
  const guess = wallMilliseconds(wall);
  if (timeZone === "UTC" || !countable(guess)) return guess;
  const offset = wallMilliseconds(wallAt(guess, timeZone)) - guess;
  const instant = guess - offset;
  const corrected = wallMilliseconds(wallAt(instant, timeZone)) - instant;
  return corrected === offset ? instant : guess - corrected;
}

// wholeMinute is an instant at the start of its minute. Formula times keep
// whole minutes, as Notion's: a time read from text, now() and
// fromTimestamp() drop their seconds and milliseconds (seen 2026-11-15
// through its API and on screen: timestamp(now()) % 60000 is 0,
// formatDate(now(), "ss.SSS") "00.000", fromTimestamp(1700000012345) is 22:13
// UTC and its timestamp() 1699999980000, parseDate("2026-11-20T10:30:45")
// 10:30, so dateBetween() of times 12 seconds apart in seconds is 0), as
// dateAdd() of hours, minutes and seconds keeps them, and as the server's
// (database_formula_date_minutes.go).
function wholeMinute(instant: number) {
  return Math.floor(instant / 60_000) * 60_000;
}

function momentAt(instant: number, timeZone: string, hasTime: boolean): FormulaMoment {
  return { instant, wall: wallAt(instant, timeZone), hasTime };
}

/** A moment on the clock formatDate()'s zone names, "UTC+1" or "Asia/Seoul"; a name it doesn't know gives none, as Notion's (seen 2026-11-04). */
function momentInNamedZone(moment: FormulaMoment, zone: string): FormulaMoment | null {
  const offset = /^UTC([+-])(\d{1,2})(?::(\d{2}))?$/.exec(zone);
  if (offset) {
    const minutes = Number(offset[2]) * 60 + Number(offset[3] ?? 0);
    return { ...moment, wall: wallFromUtc(moment.instant + (offset[1] === "-" ? -minutes : minutes) * 60_000) };
  }
  try {
    return { ...moment, wall: wallAt(moment.instant, zone) };
  } catch {
    return null;
  }
}

function momentOfWall(wall: Wall, timeZone: string, hasTime: boolean) {
  return momentAt(instantOf(wall, timeZone), timeZone, hasTime);
}

// validZone reports whether a time zone name is one the formula can keep.
export function validZone(zone: string) {
  if (!zone) return false;
  try {
    zoneFormatter(zone);
    return true;
  } catch {
    return false;
  }
}

// clockOffset is a clock's offset from UTC at an instant, in milliseconds.
export function clockOffset(own: FormulaClock, instant: number) {
  return "zone" in own ? wallMilliseconds(wallAt(instant, own.zone)) - instant : own.offset;
}

// clockWall is a time's wall clock on the clock it keeps.
function clockWall(moment: FormulaMoment): Wall {
  return moment.own ? wallFromUtc(moment.instant + clockOffset(moment.own, moment.instant)) : moment.wall;
}

// ownZone names the time zone a moment keeps, "" for an offset or none.
export function ownZone(moment: FormulaMoment) {
  return moment.own && "zone" in moment.own ? moment.own.zone : "";
}

function daysIn(year: number, month: number) {
  const date = new Date(0);
  date.setUTCFullYear(year, month, 0);
  return date.getUTCDate();
}

// parseFormulaDate reads a date's first end; a date's named time zone keeps
// its time on that zone's clock, a wall-clock time among them.
export function parseFormulaDate(value: FormulaValue | undefined, timeZone: string): FormulaMoment | null {
  const zone = value instanceof FormulaDate && validZone(value.zone) ? value.zone : "";
  const match = ISO_DATE.exec(toText(value ?? "").split("/")[0].trim());
  if (!match) return null;
  const [year, month, day] = [Number(match[1]), Number(match[2]), Number(match[3])];
  const [hour, minute, second] = [Number(match[4] ?? 0), Number(match[5] ?? 0), Number(match[6] ?? 0)];
  if (month < 1 || month > 12 || day < 1 || day > daysIn(year, month) || hour > 23 || minute > 59 || second > 59) {
    return null;
  }
  // A time keeps whole minutes (wholeMinute).
  const wall = { year, month, day, hour, minute, second: 0, millisecond: 0 };
  if (match[4] === undefined) return momentOfWall(wall, timeZone, false);
  const offsetText = match[8];
  if (!offsetText) {
    return zone ? { ...momentAt(instantOf(wall, zone), timeZone, true), own: { zone } } : momentOfWall(wall, timeZone, true);
  }
  let offset = 0;
  if (offsetText !== "Z") {
    const digits = offsetText.slice(1).replace(":", "");
    offset = (Number(digits.slice(0, 2)) * 60 + Number(digits.slice(2))) * 60_000 * (offsetText[0] === "-" ? -1 : 1);
  }
  const moment = momentAt(wallMilliseconds(wall) - offset, timeZone, true);
  if (zone) return { ...moment, own: { zone } };
  return offsetText === "Z" ? moment : { ...moment, own: { offset } };
}

function pad(value: number, width: number) {
  return `${value < 0 ? "-" : ""}${String(Math.abs(value)).padStart(width, "0")}`;
}

// A time is a date while its UTC year is 0 to 9999, a day while JavaScript
// counts it, past 9999 too ("10001-02-03"); past that it is Notion's invalid
// date (seen 2026-11-15 through its API: fromTimestamp(3e14) and dateAdd(now(),
// 3e6, "days") are "Invalid DateTime", dateAdd(parseDate("9999-12-31"), 400,
// "days") 10001-02-03, fromTimestamp(-62135596800001) 0000-12-31T23:59).
function momentValid(moment: FormulaMoment) {
  if (!countable(moment.instant)) return false;
  const year = new Date(moment.instant).getUTCFullYear();
  return !moment.hasTime || (year >= 0 && year <= 9999);
}

export function momentText(moment: FormulaMoment) {
  if (!moment.hasTime) return `${pad(moment.wall.year, 4)}-${pad(moment.wall.month, 2)}-${pad(moment.wall.day, 2)}`;
  const offset = moment.own ? clockOffset(moment.own, moment.instant) : 0;
  const wall = wallFromUtc(moment.instant + offset);
  const text = `${pad(wall.year, 4)}-${pad(wall.month, 2)}-${pad(wall.day, 2)}T${pad(wall.hour, 2)}:${pad(wall.minute, 2)}:${pad(wall.second, 2)}`;
  if (!moment.own) return `${text}Z`;
  const minutes = Math.trunc(Math.abs(offset) / 60_000);
  return `${text}${offset < 0 ? "-" : "+"}${pad(Math.floor(minutes / 60), 2)}:${pad(minutes % 60, 2)}`;
}

function momentValue(moment: FormulaMoment): FormulaValue {
  return momentValid(moment) ? new FormulaDate(momentText(moment), ownZone(moment)) : new FormulaDate(INVALID_FORMULA_DATE);
}

/**
 * Notion's invalid date, which a formula keeps as a date: its cell shows
 * nothing, empty() of it is false, and a function that reads a date fails on
 * it, giving a number that is no number — on a day past 9999 too, whose text
 * has more than four digits — but dateAdd(), dateStart(), dateEnd() and
 * dateRange() keep it, and as text it is "" ("@" joined to text), as Notion's
 * (seen 2026-11-15 through its API: timestamp(), year(), day() and
 * dateBetween() of it null, format() ""; its editor "num" with no value).
 */
export const INVALID_FORMULA_DATE = "Invalid DateTime";

// unreadable is a date given where a date is read that can't be read: an invalid one or a day past 9999.
const unreadable = (value: FormulaValue | undefined) => value instanceof FormulaDate && !parseFormulaDate(value, "UTC");
const invalidDate = () => new FormulaDate(INVALID_FORMULA_DATE);

// dateUnit is a duration unit as Notion's date functions read it: a year,
// quarter, month, week, day, hour, minute, second or millisecond, one or
// more, in any case and with nothing around it; any other fails the
// formula, named as the unit with an "s" (seen 2026-11-14: "foo" is
// "foos", " days" fails, "Days" is days).
const DURATION_UNITS = ["years", "quarters", "months", "weeks", "days", "hours", "minutes", "seconds", "milliseconds"];
function dateUnit(value: FormulaValue | undefined, name: string) {
  const unit = toText(value ?? "days").toLowerCase();
  const plural = unit.endsWith("s") ? unit : `${unit}s`;
  if (DURATION_UNITS.includes(plural)) return plural;
  throw new FormulaRunError("durationUnit", { name, unit: plural });
}

// The milliseconds of a unit dateAdd() and dateBetween() count on the clock.
const CLOCK_UNITS: Record<string, number> = { hours: 3_600_000, minutes: 60_000, seconds: 1000, milliseconds: 1 };

// addMonths keeps the day of the month, or the month's last day when it is
// shorter, and the time of day.
function addMonths(wall: Wall, months: number): Wall {
  const first = new Date(0);
  first.setUTCFullYear(wall.year, wall.month - 1 + months, 1);
  const year = first.getUTCFullYear();
  const month = first.getUTCMonth() + 1;
  return { ...wall, year, month, day: Math.min(wall.day, daysIn(year, month)) };
}

function addDays(wall: Wall, days: number): Wall {
  const date = new Date(0);
  date.setUTCFullYear(wall.year, wall.month - 1, wall.day + days);
  return { ...wall, year: date.getUTCFullYear(), month: date.getUTCMonth() + 1, day: date.getUTCDate() };
}

// How many days a unit's fraction counts, as Notion's dateAdd: whole units
// go on the calendar and the rest as time, a month of 30 days, a quarter of
// 91 and a year of 365 (seen 2026-10-10: 1.5 months from January 1 is
// February 16). The server counts the same (databaseFormulaCasualDays).
const CASUAL_DAYS: Record<string, number> = { years: 365, quarters: 91, months: 30, weeks: 7, days: 1 };

function dateAdd(value: FormulaValue | undefined, amountValue: FormulaValue | undefined, unitValue: FormulaValue | undefined, sign: number, timeZone: string) {
  const unit = dateUnit(unitValue, sign > 0 ? "dateAdd" : "dateSubtract");
  const moment = parseFormulaDate(value, timeZone);
  if (!moment && unreadable(value)) return invalidDate();
  // An empty amount leaves the date empty, as Notion's (seen 2026-10-10).
  if (!moment || amountValue === undefined || amountValue === null || amountValue === "") return "";
  const amount = sign * finiteNumber(amountValue) + 0;
  if (CLOCK_UNITS[unit]) {
    // Times keep whole minutes, as Notion's (2.5 minutes adds two).
    const instant = moment.instant + Math.round(amount * CLOCK_UNITS[unit]);
    const next = momentAt(wholeMinute(instant), timeZone, true);
    return momentValue(moment.own ? { ...next, own: moment.own } : next);
  }
  // A time counts on the clock it keeps.
  const own = moment.own;
  const place = (wall: Wall) => placeWall(moment, wall, timeZone);
  const whole = Math.trunc(amount) + 0;
  const clock = clockWall(moment);
  const moved = place(unit === "years" ? addMonths(clock, whole * 12)
    : unit === "quarters" ? addMonths(clock, whole * 3)
      : unit === "months" ? addMonths(clock, whole)
        : addDays(clock, unit === "weeks" ? whole * 7 : whole));
  const rest = amount - whole;
  if (!rest) return momentValue(moved);
  const instant = moved.instant + Math.round(rest * CASUAL_DAYS[unit] * 86_400_000);
  if (moment.hasTime) {
    const next = momentAt(wholeMinute(instant), timeZone, true);
    return momentValue(own ? { ...next, own } : next);
  }
  // A date without a time stays at the day's start (1.5 days from January 1
  // is January 2, 00:00).
  const day = own ? wallFromUtc(instant + clockOffset(own, instant)) : wallAt(instant, timeZone);
  return momentValue(place({ ...day, hour: 0, minute: 0, second: 0, millisecond: 0 }));
}

// placeWall is a moment at a wall-clock time on the clock the moment keeps.
function placeWall(moment: FormulaMoment, wall: Wall, timeZone: string): FormulaMoment {
  const own = moment.own;
  if (!own) return momentOfWall(wall, timeZone, moment.hasTime);
  const instant = "zone" in own ? instantOf(wall, own.zone) : wallMilliseconds(wall) - own.offset;
  return { ...momentAt(instant, timeZone, moment.hasTime), own };
}

// workday moves a date to a working day of a calendar — Leafen's, beside
// Notion's functions; the server counts the same (databaseFormulaEnvironment
// .workday): the next one for nextWorkday, a count of them on for
// addWorkdays, on the clock the date keeps, its time of day as it was. A
// calendar Leafen hasn't, or a count of more than ten years, gives no date.
function workday(value: FormulaValue | undefined, countValue: FormulaValue | undefined, calendarValue: FormulaValue | undefined,
  adding: boolean, timeZone: string) {
  const moment = parseFormulaDate(value, timeZone);
  if (!moment) return "";
  const code = calendarValue === undefined || calendarValue === null ? "" : toText(calendarValue).trim().toUpperCase();
  const calendar = BUSINESS_CALENDARS[code || "KR"];
  if (!calendar) return "";
  const pad = (part: number) => String(part).padStart(2, "0");
  const working = (wall: Wall) => !calendar.weekend.includes(weekday(wall))
    && !calendar.holidays[`${wall.year}-${pad(wall.month)}-${pad(wall.day)}`];
  let wall = clockWall(moment);
  if (adding) {
    if (countValue === undefined || countValue === null || countValue === "") return "";
    let count = Math.trunc(finiteNumber(countValue));
    if (Math.abs(count) > 3660) return "";
    const step = count < 0 ? -1 : 1;
    count = Math.abs(count);
    while (count > 0) {
      wall = addDays(wall, step);
      if (working(wall)) count -= 1;
    }
  } else {
    for (let steps = 0; steps < 366 && !working(wall); steps += 1) wall = addDays(wall, 1);
  }
  return momentValue(placeWall(moment, wall, timeZone));
}

// A range from one date to another is on one clock, as a date property's
// range is: its start's when the start has a time, else its end's. Two days
// make a range of days. Given a time at one end and a day at the other, the
// day takes a time on that clock — a start the day's first minute, an end its
// last, 23:59 — and a time kept on another clock keeps its wall-clock time on
// the range's, as Notion's (seen 2026-11-15 through its API in UTC and on
// screen in Seoul: dateRange(now(), d) of a day d ends at 23:59 of d,
// dateRange(d, now()) starts at 00:00, dateRange(t, d) of a 10:30 +09:00 time
// ends at 23:59 +09:00, dateRange(now(), t) of a 10:30 New York time ends at
// 10:30 on the viewer's clock). The server's is the same
// (database_formula_date_range.go). It names one time zone, its clock's.
function dateRange(startValue: FormulaValue | undefined, endValue: FormulaValue | undefined, timeZone: string): FormulaValue {
  let start = parseFormulaDate(startValue, timeZone);
  let end = parseFormulaDate(endValue, timeZone);
  // An end that can't be read stays Notion's invalid date, the other as it is.
  if ((start || unreadable(startValue)) && (end || unreadable(endValue)) && (!start || !end)) {
    const text = (moment: FormulaMoment | null) => moment ? momentText(moment) : INVALID_FORMULA_DATE;
    return new FormulaDate(`${text(start)}/${text(end)}`, start ? ownZone(start) : end ? ownZone(end) : "");
  }
  if (!start || !end || !momentValid(start) || !momentValid(end)) return "";
  if (start.hasTime || end.hasTime) {
    const clock = start.hasTime ? start : end;
    start = onRangeClock(start, clock, 0, 0, timeZone);
    end = onRangeClock(end, clock, 23, 59, timeZone);
    if (!momentValid(start) || !momentValid(end)) return "";
  }
  return new FormulaDate(`${momentText(start)}/${momentText(end)}`, ownZone(start));
}

// onRangeClock is one end of a range on the range's clock: a day at the given
// time of day, a time at its wall-clock time there.
function onRangeClock(moment: FormulaMoment, clock: FormulaMoment, hour: number, minute: number, timeZone: string): FormulaMoment {
  if (moment.hasTime && sameClock(moment.own, clock.own)) return moment;
  const wall = moment.hasTime ? clockWall(moment) : { ...moment.wall, hour, minute, second: 0, millisecond: 0 };
  return placeWall({ ...clock, hasTime: true }, wall, timeZone);
}

function sameClock(left: FormulaClock | undefined, right: FormulaClock | undefined) {
  if (!left || !right) return left === right;
  return "zone" in left ? "zone" in right && left.zone === right.zone : "offset" in right && left.offset === right.offset;
}

// monthDiff is how many months, with a fraction, left is after right.
function monthDiff(left: Wall, right: Wall): number {
  if (left.day < right.day) return -monthDiff(right, left);
  const whole = (right.year - left.year) * 12 + (right.month - left.month);
  const anchor = wallMilliseconds(addMonths(left, whole));
  const target = wallMilliseconds(right);
  const adjust = target - anchor < 0
    ? (target - anchor) / (anchor - wallMilliseconds(addMonths(left, whole - 1)))
    : (target - anchor) / (wallMilliseconds(addMonths(left, whole + 1)) - anchor);
  return -(whole + adjust);
}

function dateBetween(leftValue: FormulaValue | undefined, rightValue: FormulaValue | undefined, unitValue: FormulaValue | undefined, timeZone: string) {
  const unit = dateUnit(unitValue, "dateBetween");
  const left = parseFormulaDate(leftValue, timeZone);
  const right = parseFormulaDate(rightValue, timeZone);
  if (unreadable(leftValue) || unreadable(rightValue)) return NaN;
  if (!left || !right) return 0;
  const wallDifference = wallMilliseconds(left.wall) - wallMilliseconds(right.wall);
  const difference = unit === "years" ? monthDiff(left.wall, right.wall) / 12
    : unit === "quarters" ? monthDiff(left.wall, right.wall) / 3
      : unit === "months" ? monthDiff(left.wall, right.wall)
        : unit === "weeks" ? wallDifference / 604_800_000
          : CLOCK_UNITS[unit] ? (left.instant - right.instant) / CLOCK_UNITS[unit]
            : wallDifference / 86_400_000;
  return Math.trunc(difference) + 0;
}

// Notion's date functions by the places of the values they take as a date or
// a number: an empty one given there fails the formula, as Notion's (seen
// 2026-11-14 through its API: dateAdd(prop("d"), 1, "days"), dateAdd(now(),
// prop("n"), "days"), dateBetween(), dateRange(), dateStart(), dateEnd(),
// timestamp(), fromTimestamp(), formatDate() and year() … minute() of an empty
// property are no value, empty() of them too), as numbers do
// (formulaFunctions.ts NUMBERS_ONLY). The server's are the same
// (database_formula_dates.go databaseFormulaDatesOnly).
const DATES_ONLY: Readonly<Record<string, readonly number[]>> = {
  dateAdd: [0, 1], dateSubtract: [0, 1], dateBetween: [0, 1], dateRange: [0, 1],
  dateStart: [0], dateEnd: [0], timestamp: [0], fromTimestamp: [0], formatDate: [0],
  year: [0], month: [0], date: [0], day: [0], week: [0], hour: [0], minute: [0],
};

export function dateFunction(name: string, args: FormulaValue[], now: Date, viewer: FormulaViewer): { value: FormulaValue } | undefined {
  const { timeZone } = viewer;
  if (DATES_ONLY[name]?.some((index) => index < args.length && args[index] == null)) throw new Error(`${name}() takes only dates`);
  const parsed = () => parseFormulaDate(args[0], timeZone);
  const part = (read: (moment: FormulaMoment) => number) => {
    const moment = parsed();
    if (!moment && unreadable(args[0])) return { value: NaN };
    return { value: moment ? read(moment) : 0 };
  };
  switch (name) {
    case "now":
      return { value: momentValue(momentAt(wholeMinute(now.getTime()), timeZone, true)) };
    case "today": {
      const wall = wallAt(now.getTime(), timeZone);
      return { value: momentValue(momentOfWall({ ...wall, hour: 0, minute: 0, second: 0, millisecond: 0 }, timeZone, false)) };
    }
    case "dateAdd":
    case "dateSubtract":
      return { value: dateAdd(args[0], args[1], args[2], name === "dateAdd" ? 1 : -1, timeZone) };
    case "dateBetween":
      return { value: dateBetween(args[0], args[1], args[2], timeZone) };
    case "nextWorkday":
      return { value: workday(args[0], undefined, args[1], false, timeZone) };
    case "addWorkdays":
      return { value: workday(args[0], args[1], args[2], true, timeZone) };
    case "dateRange":
      return { value: dateRange(args[0], args[1], timeZone) };
    case "dateStart": {
      const moment = parsed();
      return { value: moment ? momentValue(moment) : unreadable(args[0]) ? invalidDate() : "" };
    }
    case "parseDate": {
      // Text is read on the viewer's clock, as Notion reads it; text that is
      // no date gives an empty date, as Notion's (seen 2026-11-14:
      // parseDate("abc") is {"type":"date","date":null} and empty).
      const moment = parsed();
      return { value: moment ? momentValue({ ...moment, own: undefined }) : null };
    }
    case "dateEnd": {
      const text = toText(args[0] ?? "");
      const end = text.split("/")[1];
      const zone = args[0] instanceof FormulaDate ? args[0].zone : "";
      const moment = parseFormulaDate(new FormulaDate(end !== undefined && end.trim() ? end : text, zone), timeZone);
      return { value: moment ? momentValue(moment) : args[0] instanceof FormulaDate ? invalidDate() : "" };
    }
    case "timestamp":
      return part((moment) => moment.instant);
    case "fromTimestamp": {
      const milliseconds = Math.trunc(finiteNumber(args[0] ?? null));
      return { value: momentValue(momentAt(wholeMinute(milliseconds), timeZone, true)) };
    }
    case "formatDate": {
      const moment = parsed();
      if (!moment) return { value: "" };
      const shown = args.length > 2 ? momentInNamedZone(moment, toText(args[2])) : moment;
      // A zone it doesn't know fails it, as Notion's (seen 2026-11-14:
      // empty(formatDate(now(), "YYYY", "Nowhere/Zone")) is no value).
      if (!shown) throw new Error("formatDate() takes a time zone it knows");
      return { value: formatFormulaDate(shown, toText(args[1] ?? "") || "YYYY-MM-DD", viewer.locale) };
    }
    case "year":
      return part((moment) => moment.wall.year);
    case "month":
      return part((moment) => moment.wall.month);
    case "date":
      return part((moment) => moment.wall.day);
    case "day":
      return part((moment) => ((weekday(moment.wall) + 6) % 7) + 1);
    case "week":
      return part((moment) => isoWeek(moment.wall));
    case "hour":
      return part((moment) => moment.wall.hour);
    case "minute":
      return part((moment) => moment.wall.minute);
  }
  return undefined;
}
