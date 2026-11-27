import { useSyncExternalStore } from "react";
import { timelineColumn, timelineColumnStart, timelineNowLabel, type TimelineNowLabel } from "./timelineViewModel";

// The start of the minute it is now, for every timeline shown: it changes as
// each minute starts, the line and its time with it, as Notion's (seen
// 2026-11-24 in Chrome at 1440, dpr 2: "오후 9:07" became "오후 9:08" and its
// line moved 5px under 시간 within a second of 21:08:00).
const listeners = new Set<() => void>();
let timer = 0;

function minuteStart() {
  return Math.floor(Date.now() / 60_000) * 60_000;
}

// Woken at the next minute's start; woken early, it finds the same minute and sleeps again until then.
function wake() {
  timer = window.setTimeout(() => {
    for (const listener of listeners) listener();
    wake();
  }, 60_000 - (Date.now() % 60_000));
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  if (!timer) wake();
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0 && timer) {
      window.clearTimeout(timer);
      timer = 0;
    }
  };
}

/**
 * Where it is now on a timeline's canvas, to the minute, as Notion's line and
 * the time over it stand (seen 2026-11-24 in Chrome at 1440, dpr 2, two tabs):
 * the column the minute falls in, how far along the canvas it is (its line on
 * the whole pixel it falls in, its time centred on it), and under the hour
 * zooms that time and how far apart their columns are named.
 */
export function useTimelineNow(anchor: Date, range: { dayWidth: number; stepMinutes?: number; labelMinutes?: number }, locale?: string) {
  const now = new Date(useSyncExternalStore(subscribe, minuteStart, minuteStart)), step = range.stepMinutes ?? false;
  const column = timelineColumn(now, anchor, step), start = timelineColumnStart(anchor, column, step).getTime();
  const offset = (column + (now.getTime() - start) / (timelineColumnStart(anchor, column + 1, step).getTime() - start)) * range.dayWidth;
  const label: TimelineNowLabel | undefined = range.stepMinutes
    ? { text: timelineNowLabel(now, locale), apart: range.dayWidth * (range.labelMinutes ?? range.stepMinutes) / range.stepMinutes } : undefined;
  return { column, offset, label };
}
