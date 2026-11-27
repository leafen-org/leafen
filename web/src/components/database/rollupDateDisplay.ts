import { i18next } from "@/i18n";
import { formatDate } from "./dateUtils";
import { lineWithClosedMentions } from "@/lib/closedMentions";

// How a date rollup reads, as Notion shows it (seen 2026-10-04): the
// earliest and the latest as a date with its year ("2026년 10월 1일"), times
// left out; a range as the time between its ends — minutes under an hour,
// hours under a day, days under 30, then months of 30 days under a year and
// years of 365, those two to one decimal ("2.5개월", "1.7년"). The values stay
// dates, for sorting, filters and the API.

const MINUTE = 60_000;

const oneDecimal = (value: number) => Math.round(value * 10) / 10;

/** The time between a range's ends, in the reader's language. */
export function rollupDurationText(milliseconds: number): string {
  const minutes = Math.abs(milliseconds) / MINUTE;
  const t = (key: string, count: number) => i18next.t(`propertyCell:rollupDuration.${key}`, { count });
  if (minutes < 60) return t("minutes", Math.round(minutes));
  const hours = minutes / 60;
  if (hours < 24) return t("hours", Math.round(hours));
  const days = hours / 24;
  if (days < 30) return t("days", Math.round(days));
  if (days < 365) return t("months", oneDecimal(days / 30));
  return t("years", oneDecimal(days / 365));
}

/**
 * A rollup's text as its cell shows it: a date function's date or range read
 * as Notion reads it, anything else as it is, a mention its reader may not
 * open as the mention says itself (closedMentions.ts). The text is the
 * server's or the app's own: a date, or a range's two ends joined by " → ".
 */
export function rollupShownText(rollupFunction: unknown, text: string): string {
  if (!text) return text;
  if (rollupFunction === "earliest_date" || rollupFunction === "latest_date") {
    const day = /^\d{4}-\d{2}-\d{2}/u.exec(text.trim())?.[0];
    return day ? formatDate(day) || text : text;
  }
  if (rollupFunction === "date_range") {
    const [start, end = start] = text.split(" → ");
    const from = Date.parse(start.trim());
    const to = Date.parse(end.trim());
    return Number.isNaN(from) || Number.isNaN(to) ? text : rollupDurationText(to - from);
  }
  return lineWithClosedMentions(text);
}
