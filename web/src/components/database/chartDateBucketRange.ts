import { dateGroupDays } from "./tableDateGroups";

/**
 * The days a chart's group of dates holds, first and last, which its rows
 * window keeps as its rule, as Notion's ("When: 10월 4일 → 10월 10일", seen
 * 2026-11-12): a day itself, a week's seven days, a month's and a year's
 * days, and by how near today the days each group stands for ("지난 30일"
 * 30 to 8 days ago, "2026년 10월" its days more than 30 days off).
 */
export type ChartDateRange = { start: string; end: string };

function dayKey(date: Date) {
  return `${String(date.getFullYear()).padStart(4, "0")}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

function day(year: number, month: number, date: number) {
  return new Date(year, month, date);
}

/**
 * By the group's key as chartBuckets names it: a week by its first day
 * ("2026-10-04"), a month ("2026-10"), and a day, a year or a group by how
 * near today as tableGrouper names them, whose days a table's group holds
 * (dateGroupDays); none for any other.
 */
export function chartDateBucketRange(key: string, now = new Date()): ChartDateRange | undefined {
  const range = (start: Date, end: Date) => ({ start: dayKey(start), end: dayKey(end) });
  let match = /^(\d{4})-(\d{2})-(\d{2})$/u.exec(key);
  if (match) {
    const start = day(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
    return range(start, day(start.getFullYear(), start.getMonth(), start.getDate() + 6));
  }
  match = /^(\d{4})-(\d{2})$/u.exec(key);
  if (match) return range(day(Number(match[1]), Number(match[2]) - 1, 1), day(Number(match[1]), Number(match[2]), 0));
  const days = /^(day|year|relative):/u.test(key) ? dateGroupDays(key, now) : null;
  return days ? range(days.first, days.last) : undefined;
}
