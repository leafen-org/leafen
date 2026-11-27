import type { DbProperty, Page } from "@/lib/types";
import { dateKey, keepsOwnClock, parseDate, shiftDateValueBy, withDateRange, withTimeOf, writtenDateRange } from "./dateUtils";
import { dateTimeValue, endMoment, moment } from "./timelineDateValues";
import type { TimelineInteractionMode } from "./timelineInteractionModel";
import { addTimelineDays, timelineColumn, timelineColumnStart } from "./timelineViewModel";

/** What a timeline's bars stand on: its dates' properties, its first column and how long its columns are. */
export type TimelineDateFrame = {
  anchor: Date;
  /** Minutes a column lasts where its columns are hours (true: an hour), else false (days), as timelineColumn takes it. */
  columnStep: number | boolean;
  startProperty: DbProperty;
  endProperty?: DbProperty;
  dateOf(row: Page, property: DbProperty): unknown;
};

type DatedBar = { row: Page; start: Date; end: Date };

/**
 * Where a bar over columns first..last begins and ends: its first column's
 * start, and where its columns are hours the moment its last one ends (the
 * next one's start), else its last day — the moments its stretched edge's
 * label names (TimelineResizeEdge) and its values are written at.
 */
export function timelineColumnsSpan(anchor: Date, first: number, last: number, step: number | boolean) {
  return { start: timelineColumnStart(anchor, first, step), end: timelineColumnStart(anchor, step ? last + 1 : last, step) };
}

/**
 * The values a bar moved or stretched to columns first..last writes. Under
 * the hours zooms a whole-day bar moved by whole days stays whole days and
 * one moved by hours takes them on its days' starts; a stretched one takes
 * the hours, its pressed edge at that edge's column edge (timelineColumnsSpan).
 */
export function timelineDatedValues(frame: TimelineDateFrame, item: DatedBar, startOffset: number, endOffset: number, mode: TimelineInteractionMode) {
  const { anchor, columnStep, startProperty, endProperty, dateOf } = frame;
  const values: Record<string, unknown> = {};
  const step = typeof columnStep === "number" ? columnStep : columnStep ? 60 : 0, hourly = step > 0, columnsPerDay = hourly ? 1440 / step : 1;
  const startValue = dateOf(item.row, startProperty);
  const endValue = endProperty ? dateOf(item.row, endProperty) : undefined;
  const delta = startOffset - timelineColumn(item.start, anchor, columnStep);
  const span = timelineColumnsSpan(anchor, startOffset, endOffset, columnStep);
  // Without an end date of its own, the bar's start and end are the two
  // ends of its date's range.
  const parts = writtenDateRange(startValue);
  const commit = (start: unknown, end: unknown) => {
    if (!endProperty) {
      let from = start === undefined ? parts.start : String(start), to = end === undefined ? parts.end : String(end);
      // Stretched by an hour, a whole day left at its other end takes a time too, its range one of
      // times: a start its day's first minute, an end its day's last, as Notion's (seen 2026-11-24 in
      // Chrome, day zoom: 11/24 stretched an hour on is 11/24 00:00–11/25 01:00, 11/25 an hour back
      // 11/24 23:00–11/25 23:59).
      if (hourly && mode !== "move" && timedPart(from) !== timedPart(to)) {
        if (timedPart(to)) from = `${from}T00:00`; else to = `${to}T23:59`;
      }
      values[startProperty.id] = withDateRange(startValue, from, to);
      return values;
    }
    if (start !== undefined) values[startProperty.id] = start;
    if (end !== undefined) values[endProperty.id] = end;
    return values;
  };
  const shownEnd = endProperty ? moment(endValue) : endMoment(startValue);
  // A date on a clock of its own keeps its clock, as the viewer sees it placed on
  // theirs: moved, by the days or hours it was dragged; stretched under the hours
  // zooms, its edge by the minutes from where it shows to where the columns put
  // that edge (an end that names only its day, from that day's start).
  if (keepsOwnClock(startValue) || keepsOwnClock(endValue)) {
    const endDay = new Date(item.end.getFullYear(), item.end.getMonth(), item.end.getDate());
    const minutesTo = (target: Date, shown: { date: Date } | null) => shown ? (target.getTime() - shown.date.getTime()) / 60_000 : 0;
    const by = (value: unknown, shift: number, minutes: boolean) => minutes ? shiftDateValueBy(value, 0, shift / 60) : shiftDateValueBy(value, shift);
    const wholeDays = !hourly || delta % columnsPerDay === 0;
    const startShift = !hourly || mode === "move" ? (wholeDays ? delta / columnsPerDay : delta * step) : minutesTo(span.start, moment(startValue));
    const endShift = mode === "move" ? startShift
      : hourly ? minutesTo(span.end, shownEnd ?? moment(startValue)) : endOffset - timelineColumn(endDay, anchor, false);
    const startMinutes = hourly && !(mode === "move" && wholeDays), endMinutes = hourly && (mode !== "move" || !wholeDays);
    if (!endProperty) {
      return commit(mode !== "resize-end" ? writtenDateRange(by(startValue, startShift, startMinutes)).start : undefined,
        mode !== "resize-start" ? writtenDateRange(by(startValue, endShift, endMinutes)).end : undefined);
    }
    return commit(mode !== "resize-end" ? by(startValue, startShift, startMinutes) : undefined, mode !== "resize-start" ? by(endValue, endShift, endMinutes) : undefined);
  }
  const wholeDays = !hourly || (mode === "move" && delta % columnsPerDay === 0 && !moment(startValue)?.timed && !shownEnd?.timed);
  if (wholeDays) {
    const day = (value: unknown, offset: number, shown: Date) => hourly
      ? withTimeOf(value, addTimelineDays(parseDate(value) ?? shown, delta / columnsPerDay)) : withTimeOf(value, addTimelineDays(anchor, offset));
    return commit(mode !== "resize-end" ? day(endProperty ? startValue : parts.start, startOffset, item.start) : undefined,
      mode !== "resize-start" ? day(endProperty ? endValue : parts.end, endOffset, new Date(item.end.getFullYear(), item.end.getMonth(), item.end.getDate())) : undefined);
  }
  // A whole-day bar moved by hours takes them on its first and last days'
  // starts, as Notion's (seen 2026-11-11: 11/10–11/12 moved an hour is
  // 11/10 01:00–11/12 01:00).
  if (mode === "move" && !moment(startValue)?.timed && !shownEnd?.timed) {
    const moved = (value: unknown, shown: Date) => {
      const at = new Date(parseDate(value) ?? shown);
      at.setMinutes(at.getMinutes() + delta * step);
      return dateTimeValue(at);
    };
    return commit(moved(endProperty ? startValue : parts.start, item.start),
      moved(endProperty ? endValue : parts.end, new Date(item.end.getFullYear(), item.end.getMonth(), item.end.getDate())));
  }
  return commit(mode !== "resize-end" ? dateTimeValue(span.start) : undefined, mode !== "resize-start" ? dateTimeValue(span.end) : undefined);
}

/**
 * The values a bar placed on columns first.. `columns` on writes for a row
 * with no date: those days, or those hours where the columns are hours, as a
 * new row's bar is made (useTimelineRowsMade); no columns on, a single day.
 */
export function timelinePlacedValues(frame: Pick<TimelineDateFrame, "anchor" | "columnStep" | "startProperty" | "endProperty">, first: number, columns: number) {
  const { anchor, columnStep, startProperty, endProperty } = frame;
  const start = timelineColumnStart(anchor, first, columnStep), end = timelineColumnStart(anchor, first + columns, columnStep);
  const [from, to] = columnStep ? [dateTimeValue(start), dateTimeValue(end)] : [dateKey(start), dateKey(end)];
  if (endProperty) return { [startProperty.id]: from, [endProperty.id]: to };
  return { [startProperty.id]: columns ? `${from}/${to}` : from };
}

/** Whether a written date names a time. */
function timedPart(text: string) {
  return /T\d{2}:\d{2}/u.test(text);
}
