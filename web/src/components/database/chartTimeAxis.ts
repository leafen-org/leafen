import { chartLabelWidth } from "./ChartLegend";

/** How a date property's X axis groups its days, as its buckets' times say. */
export type ChartTimeUnit = "day" | "week" | "month" | "year";

const DAY = 86_400_000;
// The tick units and the multiples each may step by, the lengths a month and a year count as
// when picking one (their shortest, 28 and 364 days), as Notion's chart's axis has them.
const UNITS: readonly (readonly [ChartTimeUnit, number, readonly number[] | null])[] = [
  ["day", DAY, [1, 2]], ["week", 7 * DAY, [1, 2]], ["month", 28 * DAY, [1, 2, 3, 4, 6]], ["year", 364 * DAY, null],
];
/** The room a tick asks for along the axis. */
const TICK_ROOM = 100;
/** The room a tick's name keeps clear of the next one's, five on each side. */
const LABEL_PADDING = 10;

const magnitude = (value: number) => 10 ** Math.floor(Math.log10(value));

// The multiple nearest the rough count, going up at each halfway point.
function multipleFor(rough: number, multiples: readonly number[], scale: number) {
  const normalized = rough / scale;
  let picked = multiples[0];
  for (let index = 0; index < multiples.length; index++) {
    picked = multiples[index];
    if (normalized <= (multiples[index] + (multiples[index + 1] ?? multiples[index])) / 2) break;
  }
  return picked * scale;
}

// The unit and count ticks step by: the smallest unit whose largest multiple, halfway to the
// next unit, covers the interval; years by 1, 2 or 5 under five years, by 1, 2, 2.5, 5 or 10
// of their decade past it.
function tickStep(interval: number) {
  let index = 0;
  for (; index < UNITS.length - 1; index++) {
    const [, size, multiples] = UNITS[index];
    if (interval <= (size * multiples![multiples!.length - 1] + UNITS[index + 1][1]) / 2) break;
  }
  const [unit, size, given] = UNITS[index];
  const multiples = given ?? (interval < 5 * size ? [1, 2, 5] : [1, 2, 2.5, 5, 10]);
  const count = multipleFor(interval / size, multiples, unit === "year" ? Math.max(magnitude(interval / size), 1) : 1);
  return { unit, count: Math.max(1, Math.round(count)) };
}

// The ticks from the first at or before the lowest time, on the unit's calendar: a day of the
// month a multiple of the count, the week's Monday, the month a multiple of the count, the year
// one; then every count units, past the highest time by one.
function calendarTicks(unit: ChartTimeUnit, count: number, min: number, max: number) {
  const first = new Date(min);
  let year = first.getFullYear(), month = first.getMonth(), date = first.getDate();
  if (unit === "day" || unit === "week") date = Math.max(1, count * Math.floor(date / count));
  else date = 1;
  if (unit === "month") month = count * Math.floor(month / count);
  if (unit === "year") { month = 0; year -= year % count; }
  if (unit === "week") date = date - new Date(year, month, date).getDay() + 1;
  const ticks: number[] = [];
  for (let index = 0; ticks.length < 500; index++) {
    const at = unit === "year" ? new Date(year + index * count, 0, 1)
      : unit === "month" ? new Date(year, month + index * count, 1)
        : new Date(year, month, date + index * count * (unit === "week" ? 7 : 1));
    ticks.push(at.getTime());
    if (at.getTime() >= max) break;
  }
  return ticks;
}

/**
 * A date property's X axis by day, week, month or year, continuous in time
 * as Notion's chart draws it (seen 2026-11-12): each group at its day (a
 * week's first, a month's or year's first), a day, week or month with no
 * row an empty stretch; bars each take the room of the nearest two groups'
 * distance, half of it past the first and last, a line runs from edge to
 * edge. Ticks ask for 100 along it, step by days (1, 2), weeks (1, 2, on
 * Mondays whatever day the chart's weeks start on), months (1, 2, 3, 4, 6)
 * or years, never closer than the nearest two groups, and are named as the
 * groups are, a week by its seven days from the tick on ("2026년 8월 31일
 * ~ 9월 6일"), however far apart they are.
 */
export function chartTimeAxis(values: readonly number[], { line, length }: { line: boolean; length: number }) {
  const times = [...new Set(values.filter(Number.isFinite))].sort((left, right) => left - right);
  if (!times.length) return null;
  const min = times[0], max = times[times.length - 1];
  let closest = Infinity;
  for (let index = 1; index < times.length; index++) closest = Math.min(closest, times[index] - times[index - 1]);
  if (times.length === 1) {
    return { ticks: [min], position: () => 0.5, interval: 1 };
  }
  const padding = line ? 0 : closest / 2;
  const low = min - padding, span = max - min + 2 * padding;
  const position = (value: number) => (value - low) / span;
  const { unit, count } = tickStep(Math.max(closest, ((max - min) * TICK_ROOM) / Math.max(length, TICK_ROOM)));
  const ticks = calendarTicks(unit, count, min, max).filter((tick) => tick >= low && tick <= low + span);
  return { ticks, position, interval: closest / span };
}

/**
 * Where each tick's name goes under the plot along time or a number, as
 * Notion's axis puts them (seen 2026-11-12): centered under its tick; one
 * wider than its share of the axis cut short with "…" to that share less 10
 * ("2026년 8월 31일 ~ 9월 6일" by weeks), one that fits whole kept whole
 * ("2026년 10월 13일" in 103); and kept 10 inside the chart's ends — moved
 * in, and cut shorter, where it would run past one ("7000" ending at 952).
 */
export function chartTickNames(ticks: readonly { at: number; text: string }[], { length, start, end }: {
  /** The axis's length, which the ticks share. */
  length: number; start: number; end: number;
}) {
  const slot = length / Math.max(ticks.length - 1, 1);
  const room = Math.round(slot - LABEL_PADDING);
  return ticks.map(({ at, text }) => {
    let shown = chartLabelWidth(text) > slot ? cut(text, room) : text;
    const width = chartLabelWidth(shown);
    let x = at, fit = slot;
    if (at - width / 2 < start) fit = Math.min(slot, at + slot / 2 - start);
    else if (at + width / 2 > end) fit = Math.min(slot, end - at + slot / 2);
    const toward = at - width / 2 < start ? 1 : -1;
    if (fit < slot) x += toward * (slot - fit - (slot - Math.min(width, fit)) / 2);
    if (width > fit) shown = cut(text, fit);
    return { x, text: shown };
  });
}

function cut(text: string, room: number) {
  if (chartLabelWidth(text) <= room) return text;
  let kept = text;
  while (kept.length > 1 && chartLabelWidth(`${kept}…`) > room) kept = kept.slice(0, -1);
  return `${kept}…`;
}
