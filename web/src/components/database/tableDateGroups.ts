import { i18next } from "@/i18n";
import { differenceInCalendarDays } from "@/lib/calendarDay";
import { activeDateLocale } from "@/lib/i18n";
import { addDays, dateKey, shiftDateValueBy, shownDate } from "./dateUtils";
import type { TableDateGrouping, TableGroupDescriptor } from "./tableGrouping";

// Notion's relative groups (seen 2026-10-24): 지난 30일 · 지난 7일 · 어제 ·
// 오늘 · 내일 · 다음 7일 · 다음 30일, the seventh and thirtieth days in, each
// its days from today and its place; further off, each month by its name.
const RELATIVE = [["past30", -30, -8, 100], ["past7", -7, -2, 150], ["yesterday", -1, -1, 200], ["today", 0, 0, 300],
  ["tomorrow", 1, 1, 400], ["next7", 2, 7, 500], ["next30", 8, 30, 550]] as const;

function formatDay(date: Date, options: Intl.DateTimeFormatOptions) {
  return new Intl.DateTimeFormat(activeDateLocale(), options).format(date);
}

/** The group a day falls in by a view's date grouping: by how far it is from today ("2026년 9월" past the thirtieth day either way), or by its day, week, month or year. */
export function dateGroup(date: Date, grouping: TableDateGrouping, startDayOfWeek: number, today: Date): TableGroupDescriptor {
  if (grouping === "relative") {
    const days = Math.round((date.getTime() - today.getTime()) / 86_400_000);
    if (Math.abs(days) > 30) {
      const start = new Date(date.getFullYear(), date.getMonth(), 1);
      const month = start.getFullYear() * 12 + start.getMonth();
      return { key: `relative:month:${start.getFullYear()}-${start.getMonth()}`, label: formatDay(start, { year: "numeric", month: "long" }),
        rank: days < 0 ? -1_000_000 + month : 1_000_000 + month };
    }
    const [key, , , rank] = RELATIVE.find(([, from, to]) => days >= from && days <= to)!;
    return { key: `relative:${key}`, label: i18next.t(`tableView:dateGroup.${key}`), rank };
  }
  if (grouping === "day") {
    return { key: `day:${date.toDateString()}`, label: formatDay(date, { year: "numeric", month: "short", day: "numeric" }), rank: date.getTime() };
  }
  if (grouping === "week") {
    const start = new Date(date);
    start.setDate(date.getDate() - ((date.getDay() - startDayOfWeek + 7) % 7));
    const end = new Date(start);
    end.setDate(start.getDate() + 6);
    const format = new Intl.DateTimeFormat(activeDateLocale(), { year: "numeric", month: "short", day: "numeric" });
    return { key: `week:${start.toDateString()}`, label: format.formatRange(start, end), rank: start.getTime() };
  }
  if (grouping === "month") {
    const start = new Date(date.getFullYear(), date.getMonth(), 1);
    return { key: `month:${start.getFullYear()}-${start.getMonth()}`, label: formatDay(start, { year: "numeric", month: "long" }), rank: start.getTime() };
  }
  const start = new Date(date.getFullYear(), 0, 1);
  return { key: `year:${start.getFullYear()}`, label: formatDay(start, { year: "numeric" }), rank: start.getTime() };
}

/** The first and last day a date group (dateGroup's key) holds, today being `today`; null for another key. */
export function dateGroupDays(key: string, today: Date): { first: Date; last: Date } | null {
  const day = new Date(today.getFullYear(), today.getMonth(), today.getDate());
  const [unit, rest = ""] = [key.slice(0, key.indexOf(":")), key.slice(key.indexOf(":") + 1)];
  const month = (text: string) => { const [year, index] = text.split("-").map(Number); return [new Date(year!, index!, 1), new Date(year!, index! + 1, 0)] as const; };
  if (unit === "relative") {
    if (rest.startsWith("month:")) {
      // A month further off than the thirtieth day holds its days past it alone.
      const [first, last] = month(rest.slice("month:".length));
      return first < day ? { first, last: new Date(Math.min(last.getTime(), addDays(day, -31).getTime())) }
        : { first: new Date(Math.max(first.getTime(), addDays(day, 31).getTime())), last };
    }
    const named = RELATIVE.find(([name]) => name === rest);
    return named ? { first: addDays(day, named[1]), last: addDays(day, named[2]) } : null;
  }
  if (unit === "month" && /^\d+-\d+$/u.test(rest)) { const [first, last] = month(rest); return { first, last }; }
  if (unit === "year" && /^\d+$/u.test(rest)) return { first: new Date(Number(rest), 0, 1), last: new Date(Number(rest), 11, 31) };
  const start = new Date(rest);
  if ((unit !== "day" && unit !== "week") || Number.isNaN(start.getTime())) return null;
  return { first: start, last: unit === "week" ? addDays(start, 6) : start };
}

const LAST_MINUTE = 23 * 60 + 59;

/**
 * The date a row dropped in a view's date group (`key`) takes, as Notion's
 * (seen 2026-11-24 in Chrome at 1685, dpr 1, two tabs, by relative days and
 * weeks): its start brought into the group's days — to their first, at
 * 00:00 with a time, from before them; to their last, at 23:59, from after —
 * its end moved with it, so it keeps its length, its time zone and how it is
 * written (11-20~23 into 오늘 11-24~27; 11-25 10:00~12:30 into 오늘
 * 11-24 23:59~11-25 02:29; 11-24 into the week of 11-15~21 11-21; 11-22 into
 * 다음 30일 12-02; 11-22 into a month past the thirtieth day the last day
 * it holds). A row with no date takes the group's first day. Undefined for a
 * key that is no date group, or a date already in it.
 */
export function dateGroupDropValue(key: string, current: unknown, today = new Date()): unknown {
  const days = dateGroupDays(key, today);
  if (!days) return undefined;
  const shown = shownDate(current);
  if (!shown) return dateKey(days.first);
  const before = differenceInCalendarDays(shown.date, days.first) < 0, after = differenceInCalendarDays(shown.date, days.last) > 0;
  if (!before && !after) return undefined;
  const target = before ? days.first : days.last, moved = differenceInCalendarDays(target, shown.date);
  if (!shown.time) return shiftDateValueBy(current, moved);
  const [hours, minutes] = shown.time.split(":").map(Number);
  return shiftDateValueBy(current, moved, ((before ? 0 : LAST_MINUTE) - (hours! * 60 + minutes!)) / 60);
}
