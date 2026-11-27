import { parseDate, formatDayAs, relativeDayLabel } from "@/components/database/dateUtils";
import { shortTime } from "@/lib/dateWords";
import { dateReminderHtmlAttributes } from "@/lib/dateReminders";
import { escapeHtml } from "./richtextHtmlEscaping";

/**
 * The formats a date mention shows its date in, as Notion's (seen
 * 2026-10-02): "@2026년 10월 9일", "@10월 9", "@10/09/2026", "@09/10/2026",
 * "@2026/10/09" or relative to today — "@오늘", "@내일", "@어제", a weekday
 * of this week, "@다음주 금요일" or "@지난주 금요일" (weeks from Monday), and
 * the full date beyond. A mention without one reads as relative.
 */
export const DATE_MENTION_FORMATS = ["full", "short", "month_day_year", "day_month_year", "year_month_day", "relative"] as const;
export type DateMentionFormat = typeof DATE_MENTION_FORMATS[number];

export function safeDateMentionFormat(value: unknown): DateMentionFormat | undefined {
  return typeof value === "string" && (DATE_MENTION_FORMATS as readonly string[]).includes(value)
    ? value as DateMentionFormat
    : undefined;
}

/**
 * How a date mention with a time shows it, as Notion's "시간 형식" (seen
 * 2026-10-05): in 12 hours (the default, kept as no value), 24 hours
 * ("9:00") or not at all.
 */
export const DATE_MENTION_TIME_FORMATS = ["hidden", "12", "24"] as const;
export type DateMentionTimeFormat = typeof DATE_MENTION_TIME_FORMATS[number];

export function safeDateMentionTimeFormat(value: unknown): DateMentionTimeFormat | undefined {
  return typeof value === "string" && (DATE_MENTION_TIME_FORMATS as readonly string[]).includes(value)
    ? value as DateMentionTimeFormat
    : undefined;
}

function startOfLocalDay(date: Date) {
  const next = new Date(date);
  next.setHours(0, 0, 0, 0);
  return next;
}

function parseLocalIsoDate(value: string | undefined) {
  const key = /^(\d{4}-\d{2}-\d{2})/.exec(value?.trim() ?? "")?.[1];
  return key ? parseDate(key) : null;
}

function dateMentionTimeLabel(value: string, timeFormat?: string) {
  const match = /[T ](\d{2}):(\d{2})/.exec(value);
  const shown = safeDateMentionTimeFormat(timeFormat);
  if (!match || shown === "hidden") return "";
  if (shown === "24") return `${Number(match[1])}:${match[2]}`;
  return shortTime(Number(match[1]), Number(match[2]));
}

export function dateMentionLabel(value: string, now = new Date(), format?: string, timeFormat?: string) {
  const time = dateMentionTimeLabel(value, timeFormat);
  const day = dateMentionDayLabel(value, now, safeDateMentionFormat(format) ?? "relative");
  return time && day !== value ? `${day} ${time}` : day;
}

function dateMentionDayLabel(value: string, now: Date, format: DateMentionFormat) {
  const date = parseLocalIsoDate(value);
  if (!date) return value;
  if (format === "relative") return relativeDayLabel(date, startOfLocalDay(now)) ?? formatDayAs(date, "full");
  return formatDayAs(date, format);
}

export function dateMentionDisplayText(
  value: string,
  storedText = "",
  now = new Date(),
  end = "",
  format?: string,
  timeFormat?: string,
) {
  const prefix = storedText.trimStart().startsWith("@") ? "@" : "";
  const startLabel = dateMentionLabel(value, now, format, timeFormat);
  const endLabel = end ? dateMentionLabel(end, now, format, timeFormat) : "";
  return `${prefix}${startLabel}${endLabel ? ` → ${endLabel}` : ""}`;
}

/**
 * A date mention's text in a copy for other apps: the date itself, with no
 * "@" and no "오늘" that would be wrong another day, as Notion's ⌘C of
 * "@오늘" gives "2026년 11월 6일" (seen 2026-11-06).
 */
export function dateMentionCopyText(value: string, end = "", format?: string, timeFormat?: string) {
  const shown = safeDateMentionFormat(format);
  return dateMentionDisplayText(value, "", undefined, end, !shown || shown === "relative" ? "full" : shown, timeFormat);
}

/**
 * A date mention element. Its label changes with the day, so it keeps the
 * text the collaborative document stores in `data-text` whenever they differ.
 */
export function dateMentionHtml({
  value,
  end,
  timeZone,
  reminder,
  format,
  timeFormat,
  displayText,
  storedText,
  html,
}: {
  value: string;
  end?: string;
  timeZone?: string;
  reminder?: string;
  format?: string;
  timeFormat?: string;
  displayText: string;
  storedText: string;
  html: string;
}) {
  const endAttribute = end ? ` data-date-end="${escapeHtml(end)}"` : "";
  const zone = timeZone ? ` data-date-time-zone="${escapeHtml(timeZone)}"` : "";
  const shownFormat = safeDateMentionFormat(format);
  const shownTime = safeDateMentionTimeFormat(timeFormat);
  const formatAttribute = (shownFormat ? ` data-date-format="${shownFormat}"` : "")
    + (shownTime ? ` data-date-time-format="${shownTime}"` : "");
  const stored = displayText !== storedText ? ` data-text="${escapeHtml(storedText)}"` : "";
  return `<span data-mention="date" data-date="${escapeHtml(value)}"${endAttribute}${zone}${formatAttribute}`
    + `${dateReminderHtmlAttributes(reminder, value)}${stored} contenteditable="false">${html}</span>`;
}
