import { dateInShape, dateInShapeParts } from "./dateShapes";

export type TimelineZoom = "hours" | "day" | "week" | "bi_week" | "month" | "quarter" | "year" | "5_years";

/** Zoom levels from nearest to farthest, as Notion's timeline offers them. */
export const TIMELINE_ZOOMS: TimelineZoom[] = ["hours", "day", "week", "bi_week", "month", "quarter", "year", "5_years"];

/**
 * The width of a timeline's window, its middle the date shown: the browser
 * window's width and 4000px past it on either side, at every zoom and in
 * whatever box it is shown, as Notion's (seen 2026-11-24 in Chrome, dpr 2, two
 * tabs: 9100 at 1100, 9440 at 1440, 9685 at 1685 and 9900 at 1900, a
 * database's own page 1100 or 1440 wide and one in a page's text 1085, 1425
 * or 708 wide alike, month and hours). It is read as the window is drawn
 * around a time (opened, a new zoom, drawn again near an end), not as the
 * browser is resized: 9100 stayed from 1100 to 1900 until drawn again near an
 * end, then 9900. A zoom's columns cover it, the last cut at its end.
 */
export function timelineWindowWidth() {
  return (typeof window === "undefined" ? 1440 : window.innerWidth) + 2 * 4000;
}

/** A date written in one of the date views' shapes for a language (the browser's when none; dateShapes.ts). */
export const timelineDateText = dateInShape;

export type TimelineMonthSpan = {
  key: string;
  label: string;
  span: number;
  start: number;
};

export type TimelineProjectedItem<Row> = {
  row: Row;
  start: Date;
  end: Date;
  first: number;
  last: number;
  lane: number;
};

/** A dated row wholly past the canvas's window, before its start or after its end. */
export type TimelineAwayItem<Row> = {
  row: Row;
  start: Date;
  end: Date;
  side: "start" | "end";
};

// How wide a day is at each zoom, as Notion's (seen 2026-11-24 in Chrome, dpr
// 2, a tab at 1440 and another at 1300: 7000 under 시간, 1250 under 일, 180,
// 80, 40, 12, 5 and 1 from 주 to 5년 — its quarter hours 72.9167px and its
// hours 52.0833px apart to the window's end, within 0.005px, and its bars on
// them: the day's width divided as it is, with no rounding);
// near zooms' columns are clock intervals of it, the others whole days. The
// columns fill the window (timelineWindowWidth), not a limit on the rows the
// view reads. The day zoom's hours are four quarter-hour columns each, named
// by the hour (`labelMinutes`): Notion places, moves and stretches its bars
// and makes new ones there by quarter hours (seen 2026-11-24 in Chrome at
// 1685, dpr 1, two tabs: 13:30–15:45 drawn from half into one hour to three
// quarters into another, its end stretched half an hour on 17:15, a bar moved
// half an hour 30 minutes later, a new bar pressed at 15:52 made 15:45–18:45);
// its window whole hours, as before.
const ZOOM_SETTINGS: Record<TimelineZoom, { perDay: number; stepMinutes?: number; labelMinutes?: number }> = {
  hours: { perDay: 7000, stepMinutes: 15 },
  day: { perDay: 1250, stepMinutes: 15, labelMinutes: 60 },
  week: { perDay: 180 },
  bi_week: { perDay: 80 },
  month: { perDay: 40 },
  quarter: { perDay: 12 },
  year: { perDay: 5 },
  "5_years": { perDay: 1 },
};

/** A zoom's column width: its day's width over the columns in a day. */
function columnWidth(zoom: TimelineZoom) {
  const { perDay, stepMinutes } = ZOOM_SETTINGS[zoom];
  return stepMinutes ? perDay * stepMinutes / 1440 : perDay;
}

/** How many columns of a zoom cover a window this wide: whole named columns (the day zoom's hours, not its quarters). */
function windowColumns(zoom: TimelineZoom, windowWidth: number) {
  const { stepMinutes, labelMinutes } = ZOOM_SETTINGS[zoom], per = stepMinutes && labelMinutes ? labelMinutes / stepMinutes : 1;
  return per * Math.ceil(windowWidth / (columnWidth(zoom) * per) - 1e-9);
}

/** A view's zoom: month where it names none of these. */
export function timelineZoomOf(config: { readonly timelineZoom?: unknown } | null | undefined): TimelineZoom {
  const zoom = config?.timelineZoom as TimelineZoom;
  return TIMELINE_ZOOMS.includes(zoom) ? zoom : "month";
}

/**
 * How many columns past its first a bar made from "+ 새로 만들기" under the
 * bars runs, by zoom, as Notion's (seen 2026-11-15 at 768 and 1440: an hour
 * of 15-minute columns, three hours of them, one day written as a lone date,
 * three days, five, two weeks, four weeks, 140 days).
 */
const NEW_BAR_COLUMNS: Record<TimelineZoom, number> = { hours: 4, day: 12, week: 0, bi_week: 2, month: 4, quarter: 13, year: 27, "5_years": 139 };
export function timelineNewBarColumns(zoom: TimelineZoom) {
  return NEW_BAR_COLUMNS[zoom];
}

/** Whether a zoom shows day numbers, and which. */
export function timelineDayLabel(day: Date, zoom: TimelineZoom, locale?: string) {
  if (zoom === "week" || zoom === "bi_week") return `${timelineDateText(day, "weekday", locale)} ${day.getDate()}`;
  if (zoom === "month") return String(day.getDate());
  // Quarter and year name Mondays, as Notion's (seen 2026-11-09: 2, 9, 16).
  if (zoom === "quarter" || zoom === "year") return day.getDay() === 1 ? String(day.getDate()) : "";
  if (zoom === "5_years") return day.getDate() === 1 ? String(day.getMonth() + 1) : "";
  return "";
}


/**
 * Whether a zoom's day labels stand on the day's start, the tick, rather than
 * in its column: columns too narrow for a number (quarter, year, five years)
 * center it on that day's 00:00, on one line, as Notion's (seen 2026-11-09);
 * the hour zooms' times on their own start, over their quarter hours (Notion's
 * "오후 1" centred on 13:00 under 일, seen 2026-11-24 in Chrome at 1685; at
 * 1440, dpr 2, in two tabs, "오전 9:00" under 시간 and "오전 4" under 일
 * centred on their lines, 505.42 and 410.18 by lines at 505.42 and 410.17).
 */
export function timelineDayLabelsOnTick(zoom: TimelineZoom) {
  return zoom === "quarter" || zoom === "year" || zoom === "5_years" || zoom === "day" || zoom === "hours";
}

/**
 * How many of a zoom's columns apart its lines down the days stand, each at a
 * column's start, or none: each quarter hour under 시간, each hour under 일,
 * each day under 주, none from 2주 on, as Notion's (seen 2026-11-24 in Chrome
 * at 1440, dpr 2, in two tabs; TimelineTickLines).
 */
export function timelineLineColumns(zoom: TimelineZoom) {
  const { stepMinutes, labelMinutes } = ZOOM_SETTINGS[zoom];
  return zoom === "hours" || zoom === "day" ? (labelMinutes ?? stepMinutes!) / stepMinutes! : zoom === "week" ? 1 : 0;
}

export function addTimelineDays(date: Date, days: number) {
  const next = new Date(date);
  next.setDate(date.getDate() + days);
  return next;
}

function timelineDiffDays(a: Date, b: Date) {
  const day = 24 * 60 * 60 * 1000;
  const utcA = Date.UTC(a.getFullYear(), a.getMonth(), a.getDate());
  const utcB = Date.UTC(b.getFullYear(), b.getMonth(), b.getDate());
  return Math.round((utcA - utcB) / day);
}

function addTimelineHours(date: Date, hours: number) {
  return new Date(date.getTime() + hours * 3_600_000);
}

/**
 * The columns of a zoom from its anchor over a window this wide (the one its
 * anchor was found for, timelineAnchorFor): `days` are the columns' starts,
 * `rangeDays` their count and `dayWidth` their width, hours or days.
 */
export function timelineRange(anchor: Date, zoom: TimelineZoom, windowWidth = timelineWindowWidth()) {
  const setting = ZOOM_SETTINGS[zoom], rangeDays = windowColumns(zoom, windowWidth);
  const hourly = setting.stepMinutes !== undefined;
  return {
    dayWidth: columnWidth(zoom),
    days: Array.from({ length: rangeDays }, (_, index) => hourly ? addTimelineHours(anchor, index * setting.stepMinutes! / 60) : addTimelineDays(anchor, index)),
    hourly,
    stepMinutes: setting.stepMinutes,
    /** How many minutes apart its columns are named: an hour under the day zoom's quarter hours. */
    labelMinutes: setting.labelMinutes ?? setting.stepMinutes,
    rangeDays,
    windowWidth,
  };
}

/**
 * Its columns as a grid's tracks, each column starting where its count of
 * the zoom's width puts it. A grid rounds each track down to the browser's
 * steps (1/64px, 1/128 on a 2x screen), so where a width falls between them
 * (the quarter hours, 72.9167 under 시간 and 13.0208 under 일) the tracks take
 * whole 1/64px steps in turn, each ending on the step its own sum reaches —
 * where a bar, a time or now's line placed at that sum stands — rather than
 * one rounded width added up (130 quarter hours of 72.90625px had ended 1.35px
 * short of Notion's 9406.25, 182 hours of 52.078125px 0.95px short).
 */
export function timelineGridTemplate({ rangeDays, dayWidth }: { rangeDays: number; dayWidth: number }) {
  const steps = dayWidth * 64;
  if (Math.abs(steps - Math.round(steps)) < 1e-6) return `repeat(${rangeDays}, ${dayWidth}px)`;
  // The fewest columns whose width is whole steps, after which the tracks repeat (three for both quarter hours).
  let period = 1;
  while (period < rangeDays && Math.abs(period * steps - Math.round(period * steps)) > 1e-6) period += 1;
  const at = (index: number) => Math.floor(index * steps + 1e-6), track = (index: number) => `${(at(index + 1) - at(index)) / 64}px`;
  const whole = Math.floor(rangeDays / period), tracks = Array.from({ length: period }, (_, index) => track(index)).join(" ");
  const rest = Array.from({ length: rangeDays - whole * period }, (_, index) => track(whole * period + index));
  return [whole ? `repeat(${whole}, ${tracks})` : "", ...rest].filter(Boolean).join(" ");
}

/** The column holding a moment: its hour from the anchor, or its day. */
export function timelineColumn(date: Date, anchor: Date, step: boolean | number) {
  return step ? Math.floor((date.getTime() - anchor.getTime()) / (typeof step === "number" ? step * 60_000 : 3_600_000)) : timelineDiffDays(date, anchor);
}

/** The start of a column counted from the anchor. */
export function timelineColumnStart(anchor: Date, column: number, step: boolean | number) {
  return step ? addTimelineHours(anchor, column * (typeof step === "number" ? step / 60 : 1)) : addTimelineDays(anchor, column);
}

/** The time a point this many columns from the anchor stands at, part of a column too. */
export function timelineTimeAt(anchor: Date, columns: number, step: boolean | number) {
  const whole = Math.floor(columns), start = timelineColumnStart(anchor, whole, step).getTime();
  return start + (columns - whole) * (timelineColumnStart(anchor, whole + 1, step).getTime() - start);
}

/**
 * Where a zoom's columns start for a date shown: half its window before that
 * day's midnight (that hour's start under the hours and day zooms, whose
 * window is a day or a week of hours), the date in the window's middle and
 * its scrollbar's thumb there, as Notion's (seen 2026-11-13 at 1440: 4366px
 * of its 9440 before the 708px shown, at every zoom; 2026-11-24: 4196 of 9100
 * before 708 at 1100, 4000 of 9440 before 1440); drawn again around the part
 * shown as that nears an end (TimelineCanvas), it scrolls on without one.
 */
export function timelineAnchorFor(date: Date, zoom: TimelineZoom, windowWidth = timelineWindowWidth()) {
  const { stepMinutes } = ZOOM_SETTINGS[zoom], rangeDays = windowColumns(zoom, windowWidth);
  if (stepMinutes) {
    const hour = new Date(date.getFullYear(), date.getMonth(), date.getDate(), date.getHours());
    return addTimelineHours(hour, -Math.floor(rangeDays * stepMinutes / 120));
  }
  return addTimelineDays(new Date(date.getFullYear(), date.getMonth(), date.getDate()), -Math.floor(rangeDays / 2));
}

/** The anchor one range before or after, as the arrows move. */
export function timelineShiftedAnchor(anchor: Date, zoom: TimelineZoom, direction: 1 | -1) {
  if (zoom === "hours") return addTimelineHours(anchor, direction);
  if (zoom === "day" || zoom === "week" || zoom === "bi_week") return addTimelineDays(anchor, direction * (zoom === "day" ? 1 : zoom === "week" ? 7 : 14));
  const next = new Date(anchor);
  const months = zoom === "month" ? 1 : zoom === "quarter" ? 3 : zoom === "year" ? 12 : 60;
  next.setDate(1); next.setMonth(next.getMonth() + direction * months);
  return next;
}

/**
 * What the heading names for the day at the canvas's visible start edge, as
 * Notion's (seen 2026-11-09): under 5년 its year, under 시간 and 일 its date,
 * else its month — under 분기 and 년 the month of the Monday the edge has
 * passed, as their week labels stand on Mondays ("2026년 10월" while the edge
 * is on 11월 1일, a Sunday; "11월" once 11월 2일 reaches it).
 */
export function timelineHeadingLabel(day: Date, zoom: TimelineZoom, locale?: string) {
  if (zoom === "5_years") return timelineDateText(day, "year", locale);
  const named = zoom === "quarter" || zoom === "year" ? addTimelineDays(day, -((day.getDay() + 6) % 7)) : day;
  return timelineDateText(named, zoom === "hours" || zoom === "day" ? "longDate" : "longMonthYear", locale);
}

/**
 * The day the heading goes by as `next` reaches the visible start edge: the
 * one it has while it names the same — a new day there drew the whole
 * timeline again for the same words, in the frame it opened in (2026-11-15).
 */
export function timelineHeadingDay(current: Date, next: Date, zoom: TimelineZoom, locale?: string) {
  return current.getTime() === next.getTime() || timelineHeadingLabel(current, zoom, locale) === timelineHeadingLabel(next, zoom, locale) ? current : next;
}

/** A column's label: its words, or under the hour zooms a time of day written before its hour apart (timelineHourHead). */
export type TimelineDayLabel = string | { period: string; time: string };

/**
 * A column's label under the hour zooms: its time, where its columns are
 * named `stepMinutes` apart (none between). A time of day written before the
 * hour (Korean's 오전, 오후) stands apart, drawn dimmer with 3px after it
 * (TimelineDayHeads), the hour after it without "시", as Notion's (seen
 * 2026-11-24 in Chrome at 1440, dpr 2, dark and light: "오전" then "9:00" or
 * "4", no space between their words). One written after the hour (English's
 * AM, PM) stays in its words.
 */
export function timelineHourHead(hour: Date, locale?: string, stepMinutes = 60): TimelineDayLabel {
  if ((hour.getHours() * 60 + hour.getMinutes()) % stepMinutes) return "";
  const parts = dateInShapeParts(hour, stepMinutes < 60 ? "hourMinute" : "hour", locale);
  const at = parts.findIndex((part) => part.type === "dayPeriod");
  const leads = at >= 0 && parts.slice(0, at).every((part) => !part.value.trim());
  const words = (leads ? parts.slice(at + 1) : parts).map((part) => part.value).join("").trim();
  const time = locale?.toLowerCase().startsWith("ko") ? words.replace(/시$/, "") : words;
  return leads ? { period: parts[at].value, time } : time;
}

/**
 * The time over now's line under the hour zooms, and how far apart their
 * columns are named, which sizes its box and the room the ticks' times leave
 * it (TimelineDayHeads).
 */
export type TimelineNowLabel = { text: string; apart: number };

/**
 * Now's time over its line under the hour zooms: its hour and minute, with a
 * time of day in the same words as Notion's ("오후 8:56", one run of words with
 * its space, seen 2026-11-24 in Chrome at 1440, dpr 2, two tabs).
 */
export function timelineNowLabel(now: Date, locale?: string) {
  return dateInShape(now, "hourMinute", locale);
}

/** A column's label under the hour zooms in words, its time of day and hour together. */
export function timelineHourLabel(hour: Date, locale?: string, stepMinutes = 60) {
  const head = timelineHourHead(hour, locale, stepMinutes);
  return typeof head === "string" ? head : head.period + head.time;
}

/**
 * The columns a dated item covers under the hour zooms, as the starts of its
 * first and last, as Notion's places a bar there (seen 2026-11-24 in Chrome at
 * 1685, dpr 1, two tabs, at the hours and day zooms): its start and end each at
 * the column they fall in — 13:05–13:25 over 13:00–13:15, 13:10–14:10 over
 * 13:00–14:00, 13:00–13:40 over 13:00–13:30 — its end the column's edge, so
 * the last column the one before it; a time with no end, or an end in its
 * start's column, a moment there: no column at all, its last the one before
 * its first (an 11px stub, TimelineBar). A date without a time runs from its
 * day's start to its end date's start (11/24–11/26 two days, 11/24–11/24 a
 * moment at its midnight), or with no end to the next day's start.
 */
export function timelineHourSpan(
  start: { date: Date; timed: boolean },
  end: { date: Date; timed: boolean } | null,
  stepMinutes = 60,
) {
  const column = (date: Date) => new Date(date.getFullYear(), date.getMonth(), date.getDate(), date.getHours(), Math.floor(date.getMinutes() / stepMinutes) * stepMinutes);
  const edge = (at: { date: Date; timed: boolean }) => at.timed ? column(at.date) : new Date(at.date.getFullYear(), at.date.getMonth(), at.date.getDate());
  const first = edge(start);
  const to = end ? edge(end) : start.timed ? first : addTimelineDays(first, 1);
  // The start of the column before its end's edge: an end before its start stands at the start.
  return { start: first, end: new Date(Math.max(to.getTime(), first.getTime()) - stepMinutes * 60_000) };
}

/**
 * The top row of a timeline's head: each day under the hour zooms ("11월 25일"),
 * each year under 5년 ("2026년"), else each month ("1월"), never with a year
 * of its own, this year's or another's, across a year's end too — the heading
 * names the year (timelineHeadingLabel) — as Notion's (seen 2026-11-24 in
 * Chrome at 1440, dpr 2, two tabs: "12월 31일", "1월 1일", "1월 2일" under 일
 * and 시간 across 2027's start, "12월", "1월" under 주, 2주, 월, 분기 and 년,
 * "2025년" to "2039년" under 5년).
 */
export function timelineTopSpans(days: Date[], zoom: TimelineZoom, locale?: string) {
  if (zoom === "hours" || zoom === "day") return timelineMonthSpans(days, (date) => timelineDateText(date, "shortDay", locale), "day");
  if (zoom === "5_years") return timelineMonthSpans(days, (date) => timelineDateText(date, "year", locale), true);
  return timelineMonthSpans(days, (date) => timelineDateText(date, "month", locale));
}

export function timelineMonthSpans(
  days: Date[],
  label: (date: Date) => string = (date) => timelineDateText(date, "monthYear"),
  byYear: boolean | "day" = false,
) {
  const spans: TimelineMonthSpan[] = [];
  for (let index = 0; index < days.length; index += 1) {
    const day = days[index];
    const key = byYear === "day" ? `${day.getFullYear()}-${day.getMonth()}-${day.getDate()}`
      : byYear ? String(day.getFullYear()) : `${day.getFullYear()}-${day.getMonth()}`;
    const last = spans.at(-1);
    if (last?.key === key) last.span += 1;
    else spans.push({ key, label: label(day), start: index + 1, span: 1 });
  }
  return spans;
}

/**
 * Each dated page on a line of its own in the view's order, as Notion's
 * timeline (seen 2026-11-01: three pages three lines, whether or not their
 * dates overlap); the pages without a date apart. A page dated wholly past
 * the canvas's window keeps its line too (`away`), an arrow at that edge of
 * it, as Notion's (seen 2026-11-13: a year off, every line its arrow).
 */
export function projectTimelineRows<Row>({
  anchor,
  column = timelineDiffDays,
  end,
  moments = false,
  rangeDays,
  rows,
  start,
}: {
  anchor: Date;
  column?(date: Date, anchor: Date): number;
  /** Where its last column starts. */
  end(row: Row): Date | null;
  /** Its ends may come a column before its starts: a moment, over no column (timelineHourSpan). */
  moments?: boolean;
  rangeDays: number;
  rows: readonly Row[];
  start(row: Row): Date | null;
}) {
  const undated: Row[] = [];
  const away: TimelineAwayItem<Row>[] = [];
  const visible: Array<Omit<TimelineProjectedItem<Row>, "lane">> = [];
  for (const row of rows) {
    const rowStart = start(row);
    if (!rowStart) {
      undated.push(row);
      continue;
    }
    const candidateEnd = end(row);
    const startOffset = column(rowStart, anchor);
    const least = moments ? startOffset - 1 : startOffset;
    const rowEnd = candidateEnd && column(candidateEnd, anchor) >= least ? candidateEnd : rowStart;
    const endOffset = column(rowEnd, anchor);
    if (Math.max(startOffset, endOffset) < 0 || startOffset > rangeDays - 1) {
      away.push({ row, start: rowStart, end: rowEnd, side: startOffset < 0 ? "start" : "end" });
      continue;
    }
    visible.push({
      row,
      start: rowStart,
      end: rowEnd,
      first: Math.max(0, startOffset),
      last: Math.min(rangeDays - 1, endOffset),
    });
  }
  return { items: visible.map((item, lane) => ({ ...item, lane })), undated, away };
}
