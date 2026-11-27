/**
 * A reminder on a date mention is the number of minutes before the date it
 * fires; a date alone is reminded from 9:00 that day. The server keeps the
 * same choices (reminder_values.go).
 */
const TIMED_REMINDER_MINUTES = [0, 5, 10, 15, 30, 60, 120, 1440, 2880] as const;
export const ALL_DAY_REMINDER_MINUTES = [0, 1440, 2880, 10080] as const;
const ALL_DAY_REMINDER_HOUR = 9;

function dateValueHasTime(value: string) {
  return /[T ]\d{2}:\d{2}/.test(value);
}

export function reminderChoices(value: string): readonly number[] {
  return dateValueHasTime(value) ? TIMED_REMINDER_MINUTES : ALL_DAY_REMINDER_MINUTES;
}

/**
 * Whether a reminder rings at 9:00 on its day: a date alone's, and a timed
 * date's a day or more ahead, as Notion's says "(오전 9:00) 1일 전" of a 16:00
 * date too (seen 2026-11-05). The server keeps the same rule (reminder_values.go).
 */
function reminderRingsAtNine(withTime: boolean, minutes: number) {
  return !withTime || minutes >= 1440;
}

/** A reminder choice's words, by when it rings. */
export function reminderChoiceKey(withTime: boolean, minutes: number) {
  return `blockItem:dateMention.reminder.${reminderRingsAtNine(withTime, minutes) ? "allDay" : "timed"}.${minutes}`;
}

/**
 * A chosen reminder's words on the date window's "리마인더" row: without the
 * time it rings at, as Notion's row says "당일" and "1일 전" where its list
 * says "당일 (오전 9:00)" and "(오전 9:00) 1일 전" (seen 2026-11-13, a date
 * alone and a timed date alike).
 */
export function reminderValueKey(withTime: boolean, minutes: number) {
  return `blockItem:dateMention.reminder.${reminderRingsAtNine(withTime, minutes) ? "day" : "timed"}.${minutes}`;
}

export function safeDateReminderValue(value: string | undefined, date: string | undefined) {
  const trimmed = value?.trim();
  if (!trimmed || !date || !/^\d+$/.test(trimmed) || String(Number(trimmed)) !== trimmed) return undefined;
  return reminderChoices(date).includes(Number(trimmed)) ? trimmed : undefined;
}

/** The wall-clock parts of a date mention value, ignoring any offset. */
function dateValueParts(value: string) {
  const match = /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2}))?/.exec(value.trim());
  if (!match) return null;
  return {
    year: Number(match[1]),
    month: Number(match[2]),
    day: Number(match[3]),
    hour: match[4] === undefined ? null : Number(match[4]),
    minute: match[5] === undefined ? null : Number(match[5]),
  };
}

/**
 * Whether an inline reminder reads as due: the moment it reminds at has come,
 * on the viewer's clock. Notion's turns at that moment rather than with the
 * day (seen 2026-11-05: today's date blue until 9:00 and red after, 9:40 with
 * "1시간 전" red from 8:40, 11:00 PM blue, yesterday red).
 */
export function reminderDateDue(value: string, reminder: string | undefined, now = new Date()) {
  const parts = dateValueParts(value);
  if (!parts) return false;
  const atNine = parts.hour === null || reminderRingsAtNine(true, Number(reminder ?? 0));
  const anchor = new Date(
    parts.year, parts.month - 1, parts.day,
    atNine ? ALL_DAY_REMINDER_HOUR : parts.hour ?? ALL_DAY_REMINDER_HOUR, atNine ? 0 : parts.minute ?? 0,
  );
  return anchor.getTime() - Number(reminder ?? 0) * 60_000 <= now.getTime();
}

/** The device's time zone. */
export function deviceTimeZone() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "";
  } catch {
    return "";
  }
}

let accountTimeZone = "";

/**
 * The time zone "기본 설정 → 시간대" chose while "위치를 사용하여 자동으로
 * 시간대 설정" is off, as Notion's (seen 2026-10-02); empty while it follows
 * the device. The account's display preferences set it.
 */
export function setAccountTimeZone(zone: string) {
  accountTimeZone = zone;
}

/**
 * The person's time zone: the one chosen in 기본 설정, or the device's. A date
 * without a time reminds at 9:00 in it, as in Notion (seen 2026-11-05: chosen
 * Adelaide, the reminder came at 9:00 there).
 */
export function chosenTimeZone() {
  return accountTimeZone || deviceTimeZone();
}

/**
 * The time zone a date with a time is written in: the device's, whichever one
 * 기본 설정 chose, as in Notion (seen 2026-11-05).
 */
export function localTimeZone() {
  return deviceTimeZone();
}

/** The next whole hour on the device's clock, "06:00" at 5:22. */
export function nextWholeHour(now = new Date()) {
  return `${String((now.getHours() + 1) % 24).padStart(2, "0")}:00`;
}

/**
 * The clock a date takes when its time is turned on, its start and end alike,
 * as Notion's date window, mention and property (seen 2026-11-14): the next
 * whole hour on today, 9:00 on any other day. `day` is "YYYY-MM-DD"; an empty
 * date turned to today takes today's.
 */
export function clockForDay(day: string, now = new Date()) {
  return day === localDateTimeValue(now, false) ? nextWholeHour(now) : "09:00";
}

/** "2026-09-27T09:00" style value for a local date and time. */
export function localDateTimeValue(date: Date, withTime: boolean) {
  const pad = (value: number) => String(value).padStart(2, "0");
  const day = `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
  return withTime ? `${day}T${pad(date.getHours())}:${pad(date.getMinutes())}` : day;
}

/** Data attributes marking a date mention's reminder; the value is digits only. */
export function dateReminderHtmlAttributes(reminder: string | undefined, date: string, now = new Date()) {
  const value = safeDateReminderValue(reminder, date);
  if (!value) return "";
  return ` data-date-reminder="${value}"${reminderDateDue(date, value, now) ? ' data-reminder-due="true"' : ""}`;
}
