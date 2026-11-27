import { dateKey, shownDate, shownEndDate } from "./dateUtils";

/**
 * A date value's moment on the viewer's clock, with whether it names a time:
 * a date on a clock of its own shows at the viewer's day and time, as Notion's
 * views place it.
 */
export function moment(value: unknown) {
  return momentOf(shownDate(value));
}

/** A date value's end on the viewer's clock, or null without a range. */
export function endMoment(value: unknown) {
  return momentOf(shownEndDate(value));
}

function momentOf(shown: { date: Date; time: string } | null) {
  if (!shown) return null;
  const date = new Date(shown.date);
  if (shown.time) date.setHours(Number(shown.time.slice(0, 2)), Number(shown.time.slice(3, 5)));
  return { date, timed: !!shown.time };
}

/** The day a date value shows on for the viewer. */
export function shownDay(value: unknown) {
  return shownDate(value)?.date ?? null;
}

/** A wall-clock date and time, as a date property stores one. */
export function dateTimeValue(date: Date) {
  return `${dateKey(date)}T${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
}
