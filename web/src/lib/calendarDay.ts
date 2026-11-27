import { civilDate } from "./nativeDate";

/**
 * Compare local calendar dates without treating a civil day as a fixed elapsed
 * duration. Calendar date parts stay one ordinal apart across DST and
 * other IANA time-zone discontinuities.
 */
export function differenceInCalendarDays(date: Date, reference: Date) {
  return civilDate(date).compare(civilDate(reference));
}

/**
 * The heading an activity list puts over a time's day: today, yesterday, or
 * the day's date, as Notion's updates and discussions lists do.
 */
export function calendarDaySection(at: number, labels: { today: string; yesterday: string }, locale: string) {
  const date = new Date(at);
  const difference = differenceInCalendarDays(date, new Date());
  if (difference === 0) return labels.today;
  if (difference === -1) return labels.yesterday;
  return date.toLocaleDateString(locale, { month: "long", day: "numeric" });
}
