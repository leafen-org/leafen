import { chartTextWidth } from "./ChartLegend";

// A category axis's names as Notion's chart (Highcharts) sets them (seen 2026-11-12): a 12px
// name's line is 15 high, its box 14 (a date's, drawn as a page element, 15); names 8 from the
// plot, turned a quarter back when their room is under 80 and the longest does not fit it.
const LINE = 15;
const DISTANCE = 8;
const TURN_BELOW = 80;
/** The room a name leaves on each side of it in its slot. */
const PADDING = 5;
/** The chart's own margin at its left, which a turned name stays inside. */
const EDGE = 10;
/** 0.8 of the line, which an SVG name narrowed to a width keeps for its "…". */
const ELLIPSIS_ROOM = 0.8 * LINE;
const COS45 = Math.SQRT1_2;
/** Where a turned name's end sits from its slot's middle, across and down: its baseline 12 turned (Highcharts' rotCorr). */
const TURNED_SHIFT = { x: 4 * COS45, y: 12 * COS45 };
/** The room flat names take under the plot, their distance from it included. */
export const CHART_FLAT_NAMES = DISTANCE + 14;

function longestStart(chars: readonly string[], fits: (text: string) => boolean) {
  let low = 0, high = chars.length;
  while (low < high) {
    const middle = Math.ceil((low + high) / 2);
    if (fits(chars.slice(0, middle).join(""))) low = middle;
    else high = middle - 1;
  }
  return chars.slice(0, low).join("");
}

/**
 * A name kept to a width as Notion's chart keeps it (seen 2026-11-12): an
 * SVG name whole when it is no wider than the width less 12, else its
 * longest start within that and "…" — whole after all when that start lacks
 * only its last letter ("No 이름" in 42), nothing when not even its first
 * fits; a date's name, a page element, whole when it fits, else its longest
 * start that fits with "…" after it.
 */
export function chartNameWithin(text: string, width: number, date = false) {
  const letters = Array.from(text);
  if (date) {
    if (chartTextWidth(text) <= width) return text;
    return `${longestStart(letters, (start) => chartTextWidth(`${start}…`) <= width)}…`;
  }
  const room = Math.max(0, width - ELLIPSIS_ROOM);
  if (chartTextWidth(text) <= room) return text;
  const start = longestStart(letters, (part) => chartTextWidth(part) <= room);
  const kept = Array.from(start).length;
  return kept === letters.length - 1 ? text : kept === 0 ? "" : `${start}…`;
}

// Every how many groups a name shows: as many as the room a name asks for (15 turned, 21.2
// along the axis; 11.25 down a bar chart's side) needs of the groups' room, at most all but one apart.
function nameStep(asks: number, slot: number, count: number) {
  const step = asks / (slot || 1);
  const every = step > 1 ? Math.ceil(step) : 1;
  return every > count - 1 && count > 1 ? count - 1 : every;
}

export type ChartCategoryNames = {
  /** The groups named, every step-th from the first, each with the text it shows and where its end or middle sits. */
  names: { index: number; text: string; x: number }[];
  /** Turned a quarter back, each ending under its group's middle. */
  turned: boolean;
  /** The room the names take under the plot, their distance from it included. */
  room: number;
};

/**
 * The names under a column or line chart's groups, as Notion's chart sets
 * them (seen 2026-11-12): with 80 or more for each group every name shows,
 * one wider than its room less 10 narrowed to it; with less, every step-th
 * name shows, a step that leaves 21.2 for each, and when the longest shown
 * does not fit its room less 10 they all turn a quarter back, ending 8 under
 * the plot below their group — narrowed to a third of the chart's height
 * when the longest is over half of it, a name running past the chart's left
 * 10 narrowed to end there — and the plot gives them the room they take.
 */
export function chartCategoryNames(labels: readonly string[], { slot, length, height, center, date }: {
  /** Each group's room along the axis. */
  slot: number;
  /** The axis's length, which the names shown share. */
  length: number;
  /** The chart's height. */
  height: number;
  /** Where a group's middle is. */
  center(index: number): number;
  /** The names are dates, drawn as page elements. */
  date: boolean;
}): ChartCategoryNames {
  const box = date ? LINE : 14;
  const step = slot < TURN_BELOW ? nameStep(LINE / COS45, slot, labels.length) : 1;
  const shown = labels.flatMap((label, index) => index % step === 0 ? [{ index, label, width: chartTextWidth(label) }] : []);
  const inner = Math.max(1, Math.round(length / Math.max(shown.length, 1) - 2 * PADDING));
  const longest = Math.max(0, ...shown.map((name) => name.width));
  const turned = slot < TURN_BELOW && longest > inner && longest > LINE;
  if (!turned) {
    return {
      names: shown.map(({ index, label, width }) => ({ index, text: width > inner ? chartNameWithin(label, inner, date) : label, x: center(index) })),
      turned, room: CHART_FLAT_NAMES,
    };
  }
  const common = longest > height / 2 ? height * 0.33 : longest;
  let room = 0;
  const names = shown.map(({ index, label, width }) => {
    let text = width > common ? chartNameWithin(label, common, date) : label;
    const across = (chartTextWidth(text) + box) * COS45;
    room = Math.max(room, across + DISTANCE);
    const x = center(index) + TURNED_SHIFT.x;
    if (x - across < EDGE) text = chartNameWithin(label, Math.floor(Math.round(x / COS45 - EDGE)), date);
    return { index, text, x };
  });
  return { names, turned, room };
}

/** Under a turned name its end's baseline, 8 under the plot and its turned baseline lower. */
export const CHART_TURNED_BASELINE = DISTANCE + TURNED_SHIFT.y;

/**
 * Every how many groups a bar chart's side names them, as Notion's (seen
 * 2026-11-12): every one while a group has 11.25 down the side, else every
 * step-th, from the first.
 */
export function chartSideNameStep(rowHeight: number, count: number) {
  return nameStep(0.75 * LINE, rowHeight, count);
}

/** A bar chart's side names are 250 at the most (Notion's, seen 2026-11-12). */
export const CHART_SIDE_NAME_WIDTH = 250;
