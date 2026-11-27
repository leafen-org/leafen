import { startOfWeek, startOfMonth, getWeeksInMonth } from "@internationalized/date";
import { civilDate, localDate } from "@/lib/nativeDate";
import { dateInShape } from "./dateShapes";

export type CalendarLayout = "month" | "week";

export type CalendarRowAdapters<Row> = {
  dateKey(row: Row): string;
  time(row: Row): string;
  title(row: Row): string;
};

/** Native event projection owns rows and month/week policy; the library owns days. A month runs to `weeks` weeks when given (useCalendarCells). */
export function calendarCells(anchor: Date, layout: CalendarLayout, weekStart: 0 | 1 = 0, weeks?: number) {
  const firstDay = weekStart ? "mon" : "sun";
  const day = civilDate(anchor), month = startOfMonth(day);
  const start = startOfWeek(layout === "week" ? day : month, "en-US", firstDay);
  const count = layout === "week" ? 7 : Math.max(weeks ?? 0, getWeeksInMonth(month, "en-US", firstDay)) * 7;
  return Array.from({ length: count }, (_, index) => localDate(start.add({ days: index })));
}

/** The last of the 7 days of the week the anchor falls in, shown or not. */
function weekEnd(anchor: Date, weekStart: 0 | 1) {
  return calendarCells(anchor, "week", weekStart)[6];
}

/**
 * Where a calendar goes when its unit changes, as Notion's (seen 2026-11-15
 * at 1440 in Chrome, from 2026-12 and 2027-01 months, weekends shown and
 * hidden): to weeks, the week today falls in, whatever month was shown; to
 * months, the month of the shown week's last day (11/29–12/5 → 12월,
 * 12/27–1/2 → 2027년 1월, 4/25–5/1 with weekends hidden → 5월).
 */
export function calendarAnchorForLayout(anchor: Date, layout: CalendarLayout, weekStart: 0 | 1, today = new Date()) {
  if (layout === "week") return today;
  const end = weekEnd(anchor, weekStart);
  return new Date(end.getFullYear(), end.getMonth(), 1);
}

/**
 * The calendar's head, as Notion's (seen 2026-11-15): a month by its name
 * and year; a week whose shown days run over two months by both, the year
 * on the last ("11월 - 2026년 12월") or on each where they differ ("2026년
 * 12월 - 2027년 1월"); any other week by the month of its last day, shown or
 * not (2/1–2/5 with weekends hidden "2027년 2월", 4/26–4/30 "2027년 5월").
 */
export function calendarTitle(anchor: Date, layout: CalendarLayout, weekStart: 0 | 1, shownDays: readonly Date[], locale: string) {
  const monthYear = (date: Date) => dateInShape(date, "longMonthYear", locale);
  if (layout !== "week") return monthYear(anchor);
  const first = shownDays[0], last = shownDays[shownDays.length - 1];
  if (first && last && (first.getMonth() !== last.getMonth() || first.getFullYear() !== last.getFullYear())) {
    const start = first.getFullYear() !== last.getFullYear() ? monthYear(first) : dateInShape(first, "longMonth", locale);
    return `${start} - ${monthYear(last)}`;
  }
  return monthYear(weekEnd(anchor, weekStart));
}

export function projectCalendarRows<Row>(
  rows: readonly Row[],
  adapters: CalendarRowAdapters<Row>,
) {
  const rowsByDay = new Map<string, Row[]>();
  const undated: Row[] = [];
  for (const row of rows) {
    const key = adapters.dateKey(row);
    if (!key) {
      undated.push(row);
      continue;
    }
    const bucket = rowsByDay.get(key);
    if (bucket) bucket.push(row);
    else rowsByDay.set(key, [row]);
  }
  for (const dayRows of rowsByDay.values()) {
    dayRows.sort((left, right) => (
      adapters.time(left).localeCompare(adapters.time(right))
      || adapters.title(left).localeCompare(adapters.title(right))
    ));
  }
  return { rowsByDay, undated };
}

export type CalendarWeekCard<Row> = {
  row: Row;
  /** The first of the week's shown days the card covers, and how many. */
  start: number;
  span: number;
  /** Its line in the week, from the top. */
  lane: number;
  /** Whether its range began before the week's first shown day or ends after its last. */
  continuesBefore: boolean;
  continuesAfter: boolean;
};

/**
 * A week's cards as Notion's calendar lays them: a date range is one bar
 * across the days it covers, from the week's first shown day where it began
 * earlier, to its last where it ends later (2026-10-16: 10/18–21 one bar).
 * Cards go by first day, longer first, then time and title, each in the top
 * line free across its days.
 */
export function layoutCalendarWeek<Row>(
  dayKeys: readonly string[],
  rows: readonly Row[],
  adapters: CalendarRowAdapters<Row> & { endKey(row: Row): string },
): CalendarWeekCard<Row>[] {
  const cards: Omit<CalendarWeekCard<Row>, "lane">[] = [];
  for (const row of rows) {
    const startKey = adapters.dateKey(row);
    if (!startKey) continue;
    const endKey = adapters.endKey(row) > startKey ? adapters.endKey(row) : startKey;
    const start = dayKeys.findIndex((key) => key >= startKey);
    let end = dayKeys.length - 1;
    while (end >= 0 && dayKeys[end] > endKey) end -= 1;
    if (start < 0 || end < start) continue;
    cards.push({
      row, start, span: end - start + 1,
      continuesBefore: dayKeys[start] > startKey, continuesAfter: dayKeys[end] < endKey,
    });
  }
  // Cards of one day and length keep the view's order, as Notion's (seen 2026-11-04: newest first in a new view).
  cards.sort((left, right) => left.start - right.start || right.span - left.span
    || adapters.time(left.row).localeCompare(adapters.time(right.row)));
  const taken: boolean[][] = [];
  return cards.map((card) => {
    let lane = 0;
    while (taken[lane]?.slice(card.start, card.start + card.span).some(Boolean)) lane += 1;
    const line = (taken[lane] ??= []);
    for (let day = card.start; day < card.start + card.span; day += 1) line[day] = true;
    return { ...card, lane };
  });
}
