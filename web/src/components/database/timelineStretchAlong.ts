import type { Page } from "@/lib/types";
import { writtenDateRange } from "./dateUtils";
import { timelineDatedValues, type TimelineDateFrame } from "./timelineDatedValues";
import { endMoment, moment } from "./timelineDateValues";
import type { TimelineInteractionMode } from "./timelineInteractionModel";
import { timelineColumn } from "./timelineViewModel";

type Bar = { row: Page; start: Date; end: Date };
type Columns = { first: number; last: number };

/*
 * A chosen bar stretched by an end carries the other chosen bars with it, as
 * Notion's (seen 2026-11-24 in Chrome at 1685, dpr 1, a synthetic database, in
 * two tabs at month, two-week and day zoom): every chosen bar's end — or start
 * — goes where the pressed one's goes, the same day under the day zooms (a
 * time it names kept: 11/24 13:00–19:00 with an end taken to 11/28 is 11/24
 * 13:00–11/28 19:00) and the same moment under the hours ones (a whole day
 * then a range of times, its start at 00:00); one landing on its other end's
 * day is a single date (11/27–11/29 with its end taken to 11/27 is 11/27), and
 * one that would pass it stops on it as a range of the two alike (taken to
 * 11/24 it is 11/27–11/27; 11/24 13:00–19:00 with its start taken to 11/25 is
 * 19:00–19:00). Rows chosen without a date keep none. All of them follow as it
 * is dragged, and are written together, one step of ⌘Z. A bar not among the
 * chosen is chosen alone as it is pressed, and goes alone.
 */

/** The other chosen bars a bar stretched from `rowId` carries: none unless it is chosen itself. */
export function timelineStretchedAlong<Item extends Bar>(bars: readonly Item[], chosen: ReadonlySet<string>, rowId: string): Item[] {
  if (!chosen.has(rowId)) return [];
  const seen = new Set([rowId]);
  return bars.filter((bar) => chosen.has(bar.row.id) && !bar.row.isLocked && !seen.has(bar.row.id) && Boolean(seen.add(bar.row.id)));
}

/** The columns a bar carried along is drawn over while `target` is where the stretched one would land, stopped on its other end (meeting it, a moment, under the hour zooms). */
export function timelineAlongColumns(bar: Columns, mode: TimelineInteractionMode, target: Columns, moments = false): Columns {
  const meet = moments ? 1 : 0;
  return mode === "resize-end" ? { first: bar.first, last: Math.max(bar.first - meet, target.last) } : { first: Math.min(bar.last + meet, target.first), last: bar.last };
}

/** The values a bar writes with its pressed side taken to the column `edge` (its first or last), kept from passing its other end. */
export function timelineStretchValues(frame: TimelineDateFrame, bar: Bar, mode: "resize-start" | "resize-end", edge: number) {
  const first = mode === "resize-start" ? edge : timelineColumn(bar.start, frame.anchor, frame.columnStep);
  const last = mode === "resize-end" ? edge : timelineColumn(bar.end, frame.anchor, frame.columnStep);
  const values = timelineDatedValues(frame, bar, first, last, mode);
  const { startProperty, endProperty } = frame;
  const startValue = startProperty.id in values ? values[startProperty.id] : frame.dateOf(bar.row, startProperty);
  if (!endProperty) {
    const from = moment(startValue)?.date.getTime(), to = endMoment(startValue)?.date.getTime() ?? from;
    if (from === undefined || to === undefined || to >= from) return values;
    const parts = writtenDateRange(startValue), at = mode === "resize-end" ? parts.start : parts.end;
    values[startProperty.id] = withEnds(startValue, at, at);
    return values;
  }
  const endValue = endProperty.id in values ? values[endProperty.id] : frame.dateOf(bar.row, endProperty);
  const from = moment(startValue)?.date.getTime(), to = moment(endValue)?.date.getTime();
  if (from === undefined || to === undefined || to >= from) return values;
  if (mode === "resize-end") values[endProperty.id] = startValue; else values[startProperty.id] = endValue;
  return values;
}

// A date's start and end as given, an end the same as its start kept, as Notion keeps a bar stopped on its other end.
function withEnds(value: unknown, start: string, end: string): unknown {
  if (value && typeof value === "object" && !(value instanceof Date) && ("start" in value || "end" in value)) return { ...value, start, end };
  return `${start}/${end}`;
}
