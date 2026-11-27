import { activeDateLocale } from "@/lib/i18n";

// A day or a moment as the screen writes it, as Notion does ("2026년 10월
// 16일", "2026년 10월 16일 오전 9:05"; "October 16, 2026" in English).
// 2026-10-19: lines read "2026. 10. 16." and "2026. 10. 19. 오전 3:12:45"
// beside "10월 23일까지".

function dateOf(value: Date | string | number): Date | null {
  const date = value instanceof Date ? value : new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

/** A day, or empty for what isn't one. */
export function displayDate(value: Date | string | number, locale = activeDateLocale()): string {
  const date = dateOf(value);
  return date ? new Intl.DateTimeFormat(locale, { dateStyle: "long" }).format(date) : "";
}

/** A day and its time to the minute, or empty for what isn't one. */
export function displayDateTime(value: Date | string | number, locale = activeDateLocale()): string {
  const date = dateOf(value);
  return date ? new Intl.DateTimeFormat(locale, { dateStyle: "long", timeStyle: "short" }).format(date) : "";
}
