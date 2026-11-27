import type { CalendarDate } from "@internationalized/date";

export type DateRangeEnd = "start" | "end";

/**
 * Where a day picked on the calendar goes while a date has an end, as in
 * Notion's date editor (seen 2026-11-14): it replaces the end being set —
 * the ringed one — the two kept in order (a start picked past the end, or an
 * end before the start, swaps them), and the ring passes to the other end,
 * so picks alternate start, end, start… With the ring on the start, 4~20
 * then 25 is 20~25; with it on the end, 20~25 then 10 is 10~20.
 */
export function pickInRange(start: string, end: string, picked: string, active: DateRangeEnd):
  { start: string; end: string; active: DateRangeEnd } {
  const [from, to] = active === "start" ? [picked, end] : [start, picked];
  return { start: from <= to ? from : to, end: from <= to ? to : from, active: active === "start" ? "end" : "start" };
}

/**
 * A range's marks on the calendar, as Notion's: both ends chosen, the one
 * being edited in full and the other lighter, the days between on a band.
 * A date without an end is marked as a single day.
 */
export function rangeCalendarMarks(start: string | null, end: string | null, active: DateRangeEnd) {
  const ranged = Boolean(start && end && end !== start);
  return {
    isSelected: (date: CalendarDate) => {
      const key = date.toString();
      return key === start || (ranged && key === end);
    },
    marker: (date: CalendarDate): Record<string, string | undefined> => {
      if (!ranged) return {};
      const key = date.toString();
      const side: DateRangeEnd | "middle" | undefined = key === start ? "start" : key === end ? "end"
        : key > start! && key < end! ? "middle" : undefined;
      return side ? { "data-range": side, "data-range-active": side === active ? "true" : undefined } : {};
    },
  };
}
