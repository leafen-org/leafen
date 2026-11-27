/** The room a tick asks for along the axis. */
const TICK_ROOM = 100;

// Floating-point noise trimmed off a computed tick, as the axis keeps its ticks.
const exact = (value: number, digits = 14) => Number.parseFloat(value.toPrecision(digits));

/**
 * A tick step near `rough`, as Notion's Highcharts axes pick one: 1, 2, 2.5, 5 or 10 times its power of ten,
 * going up at each halfway point; without `decimals` never a fraction of a whole (1, 2, 5 or 10 under 10, a
 * whole 1 under 1) — by default once the step reaches 0.5.
 */
export function chartTickStep(rough: number, decimals = rough < 0.5) {
  const magnitude = 10 ** Math.floor(Math.log10(rough));
  let multiples = [1, 2, 2.5, 5, 10];
  if (!decimals && magnitude === 1) multiples = [1, 2, 5, 10];
  else if (!decimals && magnitude <= 0.1) multiples = [1 / magnitude];
  const normalized = rough / magnitude;
  let picked = multiples[0];
  for (let index = 0; index < multiples.length; index++) {
    picked = multiples[index];
    if (normalized <= (multiples[index] + (multiples[index + 1] ?? multiples[index])) / 2) break;
  }
  return exact(picked * magnitude, 3);
}

/**
 * A number property's X axis, continuous in its values as Notion's chart
 * draws it (seen 2026-11-12, its Highcharts linear axis): from the least
 * value to the greatest, each group at its value, values with no row empty
 * stretches. Bars each take the room of the nearest two values' distance,
 * half of it past the first and last; a line runs from edge to edge. Ticks
 * ask for 100 along it, never closer than bars' room, on 1, 2, 2.5 or 5
 * times a power of ten (no fraction of a whole once a step reaches 0.5),
 * from the multiple at or under the least value to the one at or over the
 * greatest, those past the bars' room left out — 1, 3, 5, 7, 9, 12 by bars
 * on 926 ticks 0 to 12 by 2, by a line 1 to 12 by 1. A value alone sits in
 * the middle of a unit's width with its own tick. Down, as "높음 → 낮음"
 * sorts it, the axis runs the other way.
 */
export function chartCategoryAxis(values: readonly number[], { descending, line, length }: {
  descending: boolean; line: boolean; length: number;
}) {
  const sorted = [...new Set(values.filter(Number.isFinite))].sort((left, right) => left - right);
  if (!sorted.length) return null;
  const first = sorted[0], last = sorted[sorted.length - 1];
  const turn = (fraction: number) => descending ? 1 - fraction : fraction;
  if (sorted.length === 1) return { ticks: [first], position: () => 0.5, interval: 1 };
  // The geometry goes by the values over the largest of them, so that the widest and the
  // nearest doubles alike place within the axis.
  const scale = Math.max(Math.abs(first), Math.abs(last));
  let closest = Infinity;
  for (let index = 1; index < sorted.length; index++) closest = Math.min(closest, sorted[index] / scale - sorted[index - 1] / scale);
  const room = line ? 0 : closest;
  const low = first / scale, span = last / scale - low + room;
  const position = (value: number) => turn((value / scale - low + room / 2) / span);
  const interval = Math.min(1, closest / span);

  // Ticks, in the values' own units.
  let nearest = Infinity;
  for (let index = 1; index < sorted.length; index++) nearest = Math.min(nearest, sorted[index] - sorted[index - 1]);
  const range = last - first, pointRange = line ? 0 : nearest;
  const rough = Math.max(pointRange, (range * TICK_ROOM) / Math.max(length, TICK_ROOM));
  const step = rough > 0 && Number.isFinite(rough) ? chartTickStep(rough) : NaN;
  const ticks: number[] = [];
  if (step > 0 && Number.isFinite(step) && Number.isFinite(range)) {
    const from = exact(Math.floor(first / step) * step), to = exact(Math.ceil(last / step) * step);
    for (let at = from, count = 0; at <= to && count < 1000; count++) {
      if (at >= first - pointRange / 2 && at <= last + pointRange / 2) ticks.push(Object.is(at, -0) ? 0 : at);
      const next = exact(at + step);
      if (next === at) break;
      at = next;
    }
  }
  if (!ticks.length) ticks.push(first, last);
  return { ticks, position, interval };
}
