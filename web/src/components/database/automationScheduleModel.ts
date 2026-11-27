import type { TFunction } from "i18next";
import { activeDateLocale } from "@/lib/i18n";
import { SCHEDULE_MIDNIGHT, defaultScheduleTriggerDraft, type ScheduleFrequency, type ScheduleTriggerDraft } from "./databaseAutomationPanelModel";
import { formatTime12, relativeDayLabel } from "./dateUtils";

/**
 * A recurring trigger as a phone's sheet picks it, as Notion's (seen
 * 2026-10-24 at 375px): "매..." leaves a "주기" chip and its choices — 일,
 * 주중, 주, 2주, 월, 3개월, 6개월, 년 — and after one, its sheet: "매 [N]
 * [일|주|개월|년]", the weekdays, a month's day or its first to fourth or
 * last weekday, the time, the start (tomorrow at first), the end and the
 * time zone, and when it runs next. The server reads the same
 * (database_automation_schedule.go).
 */
export const SCHEDULE_PRESETS = ["day", "weekdays", "week", "twoWeeks", "month", "threeMonths", "sixMonths", "year"] as const;
export type SchedulePreset = typeof SCHEDULE_PRESETS[number];

const MONDAY = 1;
const WEEKDAYS = [1, 2, 3, 4, 5];

function civilDay(date: Date) {
  return [date.getFullYear(), date.getMonth() + 1, date.getDate()].map((part, index) => String(part).padStart(index ? 2 : 4, "0")).join("-");
}

/** "매...": from tomorrow "자정 전후", its "주기" still to pick, as Notion's. */
export function newScheduleTriggerDraft(now = new Date()): ScheduleTriggerDraft {
  return { ...defaultScheduleTriggerDraft(), time: SCHEDULE_MIDNIGHT, startsOn: civilDay(new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1)), pending: true };
}

/** Monday first, as Notion's circles (seen 2026-10-24 on a phone, 2026-10-25 at 1280px). */
export const SCHEDULE_WEEK = [1, 2, 3, 4, 5, 6, 0];
// "자정 전후" first, then from 3:15am every 15 minutes, as Notion's list (seen 2026-10-24, and 85 rows at 1280px 2026-10-25).
export const SCHEDULE_TIMES = Array.from({ length: 96 }, (_, index) => `${String(Math.floor(index / 4)).padStart(2, "0")}:${String((index % 4) * 15).padStart(2, "0")}`)
  .filter((time) => time > "03:00");

export function scheduleWeekdayName(weekday: number, width: "narrow" | "short" | "long") {
  // 2023-01-01 was a Sunday.
  return new Intl.DateTimeFormat(activeDateLocale(), { weekday: width, timeZone: "UTC" }).format(new Date(Date.UTC(2023, 0, 1 + weekday)));
}

/** A day as Notion's schedule names it: "내일", "다음주 토", or "11월 2일". */
export function scheduleDayLabel(value: string) {
  const day = parseCivil(value);
  if (!day) return "";
  const date = new Date(day.year, day.month - 1, day.day);
  const now = new Date();
  const locale = activeDateLocale();
  return relativeDayLabel(date, new Date(now.getFullYear(), now.getMonth(), now.getDate()), locale, "short")
    ?? date.toLocaleDateString(locale, { month: "long", day: "numeric", ...(day.year !== now.getFullYear() ? { year: "numeric" } : {}) });
}

/** A schedule's time as its row says it: "자정 전후" for the window, else the clock. */
export function scheduleTimeLabel(t: TFunction, time: string) {
  return time === SCHEDULE_MIDNIGHT ? t("databaseView:scheduleMidnight") : formatTime12(time);
}

const PRESETS: Record<SchedulePreset, { frequency: ScheduleFrequency; interval: number; weekdays?: number[] }> = {
  day: { frequency: "daily", interval: 1 },
  weekdays: { frequency: "weekly", interval: 1, weekdays: WEEKDAYS },
  week: { frequency: "weekly", interval: 1, weekdays: [MONDAY] },
  twoWeeks: { frequency: "weekly", interval: 2, weekdays: [MONDAY] },
  month: { frequency: "monthly", interval: 1 },
  threeMonths: { frequency: "monthly", interval: 3 },
  sixMonths: { frequency: "monthly", interval: 6 },
  year: { frequency: "yearly", interval: 1 },
};

/** The preset a schedule is, as "복제 주기" ticks it, or "" when shaped otherwise. */
export function schedulePresetOf(draft: ScheduleTriggerDraft): SchedulePreset | "" {
  const weekdays = [...draft.weekdays].sort((left, right) => left - right).join();
  return SCHEDULE_PRESETS.find((preset) => {
    const shape = PRESETS[preset];
    return shape.frequency === draft.frequency && shape.interval === Number(draft.interval)
      && (!shape.weekdays || shape.weekdays.join() === weekdays);
  }) ?? "";
}

/** A unit picked in the sheet: a week on Monday, a month on its 1st, as Notion starts them. */
export function withScheduleUnit(draft: ScheduleTriggerDraft, frequency: ScheduleFrequency): ScheduleTriggerDraft {
  return {
    ...draft,
    frequency,
    pending: undefined,
    weekdays: frequency === "weekly" ? draft.frequency === "weekly" && draft.weekdays.length ? draft.weekdays : [MONDAY] : [],
    monthlyBy: frequency === "monthly" && draft.frequency === "monthly" ? draft.monthlyBy : "day",
    monthDay: frequency === "monthly" ? draft.frequency === "monthly" && draft.monthDay ? draft.monthDay : "1" : "",
    monthWeek: frequency === "monthly" && draft.frequency === "monthly" ? draft.monthWeek : "",
    monthWeekday: frequency === "monthly" && draft.frequency === "monthly" ? draft.monthWeekday : "",
  };
}

export function withSchedulePreset(draft: ScheduleTriggerDraft, preset: SchedulePreset): ScheduleTriggerDraft {
  const { frequency, interval, weekdays } = PRESETS[preset];
  // A preset starts its unit afresh: a month on its 1st.
  const next = withScheduleUnit({ ...draft, frequency: "daily", weekdays: [] }, frequency);
  return { ...next, interval: String(interval), ...(weekdays ? { weekdays } : {}) };
}

/**
 * The words around the chip, as Notion's trigger reads (seen 2026-11-19 in Chrome 1440): "Every" before it in
 * English, none in Korean.
 */
export function scheduleChipAround(t: TFunction) {
  const [before = "", after = ""] = t("databaseView:scheduleChipTitle", { chip: "\u0001" }).split("\u0001");
  return { before: before.trim(), after: after.trim() };
}

/**
 * The chip's words, lower case after "Every" (scheduleChipAround) in English: "주기"/"frequency" before one is
 * picked, "평일"/"weekday", a unit — "일"/"day" — or "3 일"/"3 days", as Notion's (seen 2026-11-19 in Chrome
 * 1440, ko and en: "Every frequency ⌄", "Every 2 weeks ⌄").
 */
export function scheduleChipLabel(t: TFunction, draft: ScheduleTriggerDraft) {
  if (draft.pending) return t("databaseView:scheduleChip.pending");
  const count = Number(draft.interval) || 1;
  const weekdays = [...draft.weekdays].sort((left, right) => left - right).join();
  if (draft.frequency === "weekly" && count === 1 && weekdays === WEEKDAYS.join()) return t("databaseView:scheduleChip.weekdays");
  return count === 1 ? t(`databaseView:scheduleChip.${draft.frequency}`) : t(`databaseView:scheduleUnitMany.${draft.frequency}`, { count });
}

/** A unit as its details name it beside the interval, by how many: "Day", "Days" (Notion's, seen 2026-11-19). */
export function scheduleUnitName(t: TFunction, frequency: ScheduleFrequency, interval: string) {
  return t(`databaseView:scheduleUnit.${frequency}`, { count: Number(interval) || 1 });
}

type Civil = { year: number; month: number; day: number };

function ordinalOf({ year, month, day }: Civil) {
  return Math.floor(Date.UTC(year, month - 1, day) / 86_400_000);
}

function civilOf(ordinal: number): Civil & { weekday: number } {
  const date = new Date(ordinal * 86_400_000);
  return { year: date.getUTCFullYear(), month: date.getUTCMonth() + 1, day: date.getUTCDate(), weekday: date.getUTCDay() };
}

function parseCivil(value: string): Civil | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  return match ? { year: Number(match[1]), month: Number(match[2]), day: Number(match[3]) } : null;
}

function daysIn(year: number, month: number) {
  return new Date(Date.UTC(year, month, 0)).getUTCDate();
}

/** The day a monthly schedule runs in a month, or 0 when it has no such weekday. */
function monthDayOf(draft: ScheduleTriggerDraft, year: number, month: number) {
  const days = daysIn(year, month);
  if (draft.monthlyBy === "day") return Math.min(Number(draft.monthDay) || 1, days);
  const first = 1 + ((Number(draft.monthWeekday) - new Date(Date.UTC(year, month - 1, 1)).getUTCDay() + 7) % 7);
  const week = Number(draft.monthWeek) || 1;
  if (week === 5) return first + 7 * Math.floor((days - first) / 7);
  const day = first + 7 * (week - 1);
  return day > days ? 0 : day;
}

function occursOn(draft: ScheduleTriggerDraft, start: Civil, ordinal: number) {
  const startOrdinal = ordinalOf(start);
  if (ordinal < startOrdinal) return false;
  const date = civilOf(ordinal);
  const interval = Number(draft.interval) || 1;
  switch (draft.frequency) {
  case "daily": return (ordinal - startOrdinal) % interval === 0;
  case "weekly": {
    const weeks = Math.floor((ordinal - date.weekday - (startOrdinal - civilOf(startOrdinal).weekday)) / 7);
    return draft.weekdays.includes(date.weekday) && weeks % interval === 0;
  }
  case "monthly": {
    const months = (date.year - start.year) * 12 + date.month - start.month;
    return months % interval === 0 && date.day === monthDayOf(draft, date.year, date.month);
  }
  case "yearly":
    return (date.year - start.year) % interval === 0 && date.month === start.month && date.day === Math.min(start.day, daysIn(date.year, start.month));
  }
}

/** Now on the schedule's clock: its day and its "HH:MM". */
function clockIn(timeZone: string, now: Date): { today: Civil; time: string } {
  try {
    const parts = Object.fromEntries(new Intl.DateTimeFormat("en-US", { timeZone, year: "numeric", month: "2-digit", day: "2-digit",
      hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).formatToParts(now).map((part) => [part.type, part.value]));
    return { today: { year: Number(parts.year), month: Number(parts.month), day: Number(parts.day) }, time: `${parts.hour}:${parts.minute}` };
  } catch {
    return { today: { year: now.getFullYear(), month: now.getMonth() + 1, day: now.getDate() }, time: "" };
  }
}

/** When it runs next, on its own clock, as the server finds it; null when it has ended or is unfinished. */
export function nextScheduleRun(draft: ScheduleTriggerDraft, now = new Date()): { day: Civil; today: Civil; time: string } | null {
  const start = parseCivil(draft.startsOn);
  const clock = draft.time === SCHEDULE_MIDNIGHT ? "00:00" : draft.time;
  if (draft.pending || !start || !/^\d{2}:\d{2}$/.test(clock)) return null;
  if (draft.frequency === "weekly" && !draft.weekdays.length) return null;
  const end = draft.endsOn ? parseCivil(draft.endsOn) : null;
  const { today, time } = clockIn(draft.timeZone, now);
  const first = Math.max(ordinalOf(start), ordinalOf(today));
  for (let ordinal = first; ordinal <= first + 11 * 366; ordinal += 1) {
    if (end && ordinal > ordinalOf(end)) return null;
    if (!occursOn(draft, start, ordinal)) continue;
    if (ordinal === ordinalOf(today) && clock <= time) continue;
    const { year, month, day } = civilOf(ordinal);
    return { day: { year, month, day }, today, time: draft.time };
  }
  return null;
}

/** "다음 주기: 다음주 월 오전 4:00", "Next occurrence: Nov 30 between 12–3 AM", as Notion's sheet ends (nextScheduleRunWhen). */
export function nextScheduleRunText(t: TFunction, draft: ScheduleTriggerDraft, now = new Date()) {
  const when = nextScheduleRunWhen(t, draft, now);
  return when === null ? null : t("databaseView:scheduleNextRun", { when });
}

/**
 * When it runs next, as "다음 주기:" goes on, as Notion's (seen 2026-11-19 in
 * Chrome, in English and in Korean): its day — "Today", "Tomorrow", this
 * week's "Sat", "Next Tue", further off "Nov 30", another year's "Jan 14,
 * 2027" — then "at 4:00 AM", or "between 12–3 AM" for its midnight window;
 * Korean the day and the time alone ("내일 자정~오전 3시", "토 오전 4:00",
 * "다음주 화 오전 4:00", "11월 30일 오전 4:00", "2027년 1월 14일 오전 4:00").
 */
export function nextScheduleRunWhen(t: TFunction, draft: ScheduleTriggerDraft, now = new Date()) {
  const next = nextScheduleRun(draft, now);
  if (!next) return null;
  const locale = activeDateLocale();
  const date = new Date(next.day.year, next.day.month - 1, next.day.day);
  const today = new Date(next.today.year, next.today.month - 1, next.today.day);
  const day = relativeDayLabel(date, today, locale, "short")
    ?? date.toLocaleDateString(locale, { month: "short", day: "numeric", ...(next.day.year !== next.today.year ? { year: "numeric" } : {}) });
  return next.time === SCHEDULE_MIDNIGHT ? t("databaseView:scheduleNextMidnight", { day })
    : t("databaseView:scheduleNextAt", { day, time: formatTime12(next.time) });
}
