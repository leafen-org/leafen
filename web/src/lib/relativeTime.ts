// Shared relative-time labels for menus, search results, and backlinks.
// Date/time parts are display-only and never alter the underlying value.

import { i18next } from "@/i18n";
import { activeDateLocale } from "@/lib/i18n";
import { recentTimeStep } from "@/lib/recentTime";

export function relativeTimeLabels() {
  const t = (key: string, options?: Record<string, unknown>) => String(i18next.t(`relativeTime:${key}`, options));
  return {
    justNow: t("justNow"),
    editedJustNow: t("editedJustNow"),
    editedMinutesAgo: (minutes: number) => t("editedMinutesAgo", { minutes }),
    editedHoursAgo: (hours: number) => t("editedHoursAgo", { hours }),
    editedDaysAgo: (days: number) => t("editedDaysAgo", { days }),
    editedOn: (date: string) => t("editedOn", { date }),
    noEditsYet: t("noEditsYet"),
    todayAt: (time: string) => t("todayAt", { time }),
    yesterdayAt: (time: string) => t("yesterdayAt", { time }),
    dateAt: (date: string, time: string) => t("dateAt", { date, time }),
  };
}

function isSameLocalDay(a: Date, b: Date) {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

/**
 * "5분 전 편집"-style label for a page or block edit stamp, in the steps of
 * Notion's recent times (recentTimeStep), as its top bar words them.
 * Missing/unparseable values yield "" so callers can chain their own fallback.
 */
export function relativeEditedLabel(
  value: string | undefined | null,
  opts: { year?: boolean } = {}
): string {
  if (!value) return "";
  const time = new Date(value).getTime();
  if (Number.isNaN(time)) return "";
  const labels = relativeTimeLabels();
  const recent = recentTimeStep(time, Date.now(), activeDateLocale(), opts.year);
  switch (recent.step) {
    case "now": return labels.editedJustNow;
    case "minutes": return labels.editedMinutesAgo(recent.count);
    case "hours": return labels.editedHoursAgo(recent.count);
    case "days": return labels.editedDaysAgo(recent.count);
    case "date": return labels.editedOn(recent.date);
  }
}

/**
 * Today/yesterday/date absolute-ish menu timestamp.
 * Missing/unparseable values read as "방금".
 */
export function menuTimestampLabel(value?: string | null): string {
  const labels = relativeTimeLabels();
  if (!value) return labels.justNow;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return labels.justNow;

  const now = new Date();
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  const time = new Intl.DateTimeFormat(activeDateLocale(), {
    hour: "numeric",
    minute: "2-digit",
  }).format(date);

  if (isSameLocalDay(date, now)) return labels.todayAt(time);
  if (isSameLocalDay(date, yesterday)) return labels.yesterdayAt(time);

  const datePart = new Intl.DateTimeFormat(activeDateLocale(), {
    month: "short",
    day: "numeric",
    ...(date.getFullYear() === now.getFullYear() ? {} : { year: "numeric" }),
  }).format(date);
  return labels.dateAt(datePart, time);
}

/**
 * Today's time alone ("오전 2:50"), as Notion's feed dates a post's last
 * comment (seen 2026-10-21); another day as a menu dates it.
 */
export function timeTodayLabel(value?: string | null): string {
  const date = value ? new Date(value) : null;
  if (!date || Number.isNaN(date.getTime()) || !isSameLocalDay(date, new Date())) return menuTimestampLabel(value);
  return new Intl.DateTimeFormat(activeDateLocale(), { hour: "numeric", minute: "2-digit" }).format(date);
}

/**
 * "4시간 전"-style label, as Notion's library shows when a page was edited
 * or visited; past a week, the date.
 */
export function relativeAgoLabel(value: string | undefined | null): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const t = (key: string, options?: Record<string, unknown>) => String(i18next.t(`relativeTime:${key}`, options));
  const diff = Date.now() - date.getTime();
  const minute = 60 * 1000;
  const hour = 60 * minute;
  const day = 24 * hour;
  // Under a minute Notion's home cards and library say "지금" (2026-11-07).
  if (diff < minute) return t("now");
  if (diff < hour) return t("minutesAgo", { count: Math.floor(diff / minute) });
  if (diff < day) return t("hoursAgo", { count: Math.floor(diff / hour) });
  if (diff < 7 * day) return t("daysAgo", { count: Math.floor(diff / day) });
  return date.toLocaleDateString(activeDateLocale(), {
    month: "long",
    day: "numeric",
    ...(date.getFullYear() === new Date().getFullYear() ? {} : { year: "numeric" }),
  });
}

/**
 * The library's times, as Notion's (seen 2026-11-07): a week on "1주 전"
 * (English "16h ago", "5d ago", "3w ago", short), past a month the date
 * ("10월 6일", "Oct 6"), with its year when another year's. Home's cards
 * keep relativeAgoLabel, a date from a week.
 */
export function libraryAgoLabel(value: string | undefined | null, now = Date.now()): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const t = libraryAgoWords;
  const minutes = Math.floor(Math.max(0, now - date.getTime()) / 60_000);
  if (minutes < 1) return t("now");
  if (minutes < 60) return t("libraryMinutesAgo", minutes);
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t("libraryHoursAgo", hours);
  const days = Math.floor(hours / 24);
  if (days < 7) return t("libraryDaysAgo", days);
  if (days < 30) return t("libraryWeeksAgo", Math.floor(days / 7));
  return libraryDateFormat(activeDateLocale(), date.getFullYear() !== new Date(now).getFullYear()).format(date);
}

// The library's time words and date formats, each worked out once for its
// language: a library's rows asked i18next and made a date format for each
// cell, some 40 ms at CPU ×2 for a screen of rows (2026-11-15).
const agoWords = new Map<string, string>();
const dateFormats = new Map<string, Intl.DateTimeFormat>();

function libraryAgoWords(key: string, count?: number) {
  const id = `${i18next.language}\0${key}\0${count ?? ""}`;
  let words = agoWords.get(id);
  if (words === undefined) {
    words = String(i18next.t(`relativeTime:${key}`, count === undefined ? undefined : { count }));
    // Words not loaded yet aren't kept.
    if (i18next.exists(`relativeTime:${key}`)) agoWords.set(id, words);
  }
  return words;
}

function libraryDateFormat(locale: string, withYear: boolean) {
  const id = `${locale}\0${withYear}`;
  let format = dateFormats.get(id);
  if (!format) {
    format = new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", ...(withYear ? { year: "numeric" } : {}) });
    dateFormats.set(id, format);
  }
  return format;
}

/**
 * "지금", "2분", "13시간" — when a comment or a feed post was made, as
 * Notion writes it beside the writer's name (seen 2026-10-21); a day on,
 * "3일" (inferred: not seen), past a week the date, with its year when
 * another year's (not seen).
 */
export function shortAgoLabel(value: string | undefined | null, now = Date.now()): string {
  const t = (key: string, options?: Record<string, unknown>) => String(i18next.t(`relativeTime:${key}`, options));
  const date = value ? new Date(value) : null;
  if (!date || Number.isNaN(date.getTime())) return t("now");
  const minutes = Math.floor(Math.max(0, now - date.getTime()) / 60_000);
  if (minutes < 1) return t("now");
  if (minutes < 60) return t("minutesShort", { count: minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t("hoursShort", { count: hours });
  const days = Math.floor(hours / 24);
  if (days < 7) return t("daysShort", { count: days });
  return date.toLocaleDateString(activeDateLocale(), {
    month: "short",
    day: "numeric",
    ...(date.getFullYear() === new Date(now).getFullYear() ? {} : { year: "numeric" }),
  });
}

/** "생성 일시: 2026년 10월 21일 오전 2:50:28", as Notion's tip over that time reads. */
export function createdAtLabel(value: string | undefined | null): string {
  const date = value ? new Date(value) : null;
  if (!date || Number.isNaN(date.getTime())) return "";
  return String(i18next.t("relativeTime:createdAt", {
    date: date.toLocaleString(activeDateLocale(), {
      year: "numeric", month: "long", day: "numeric", hour: "numeric", minute: "2-digit", second: "2-digit",
    }),
  }));
}

/** "11월 15일", with its year when another year's: the day alone, as an archived page's band says it (Notion's, seen 2026-11-15). */
export function dayLabel(value: string | undefined | null, now = Date.now()): string {
  const date = value ? new Date(value) : null;
  if (!date || Number.isNaN(date.getTime())) return "";
  return date.toLocaleDateString(activeDateLocale(), {
    month: "long",
    day: "numeric",
    ...(date.getFullYear() === new Date(now).getFullYear() ? {} : { year: "numeric" }),
  });
}
