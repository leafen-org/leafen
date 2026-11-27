import { CalendarDate, CalendarDateTime, GregorianCalendar, fromDate, getLocalTimeZone,
  parseDate, parseDateTime, parseTime, parseAbsolute, startOfMonth, toCalendarDate } from "@internationalized/date";

/** Civil dates cross older Date-shaped view adapters by their local parts,
 * never by treating a day as a fixed duration or UTC midnight. */
export function civilDate(date: Date): CalendarDate {
  return new CalendarDate(date.getFullYear(), date.getMonth() + 1, date.getDate());
}
export function localDate(date: CalendarDate): Date { return date.toDate(getLocalTimeZone()); }
export function readCivilDate(value: string): CalendarDate | null {
  try { const date = parseDate(value); return date.year >= 0 && date.year <= 9999 ? date : null; } catch { return null; }
}
export function readWallDateTime(value: string): CalendarDateTime | null {
  try { return parseDateTime(value.replace(" ", "T")); } catch { return null; }
}
export function readClock(value: string) {
  try { return parseTime(value); } catch { return null; }
}
export function civilDateFromParts(year: number, month: number, day: number): CalendarDate | null {
  // A day of a four-digit year, 0 to 9999, as Notion shows one (seen 2026-11-15: a formula's parseDate("0050-01-01")
  // shows "50년 1월 1일", a day past 9999 nothing); overflow is refused rather than taken as another date.
  if (![year, month, day].every(Number.isInteger) || year < 0 || year > 9999) return null;
  return readCivilDate(`${String(year).padStart(4, "0")}-${String(month).padStart(2, "0")}-${String(day).padStart(2, "0")}`);
}
export function civilAtInstant(value: string, zone = getLocalTimeZone()): CalendarDate | null {
  try { return toCalendarDate(parseAbsolute(value.replace(" ", "T"), zone)); } catch { return null; }
}
export function clockAtInstant(date: Date, zone = getLocalTimeZone()) {
  return fromDate(date, zone);
}
/** The moment a day starts on a zone's clock — the day `daysBack` days before `at`'s there — across its
 * daylight-saving changes; a zone this browser doesn't know (or none) reads the device's. */
export function dayStartOnClock(at: Date, zone: string, daysBack = 0): Date {
  const start = (clock: string) => toCalendarDate(fromDate(at, clock)).subtract({ days: daysBack }).toDate(clock);
  try { return start(zone || getLocalTimeZone()); } catch { return start(getLocalTimeZone()); }
}
export function civilMonth(date: Date) { return localDate(startOfMonth(civilDate(date))); }
export function civilDays(date: Date, days: number) { return localDate(civilDate(date).add({ days })); }
export function civilMonths(date: Date, months: number) { return localDate(civilDate(date).add({ months })); }
export const nativeCalendar = () => new GregorianCalendar();
