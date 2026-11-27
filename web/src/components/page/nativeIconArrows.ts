// Notion's arrows and chevrons (its arrow-*-basic, arrows-horizontal and
// -vertical, chevrons-vertical, upward, downward, forward and backward, read
// 2026-11-18 and rastered at 400px): a head of two arms at 45° a line wide
// meeting in a sharp point, on a shaft a line wide, or a block arrow
// filled. Each is drawn pointing up and turned a quarter clockwise as
// often as it points round from there. A line that ends alone is drawn
// half a line on past its end (nativeIconGlyphInk.ts endsPath), so each
// ends that much short of where its ink does.

import { HALF_LINE, n, polygon } from "./nativeIconGlyphShapes";

type Point = [number, number];

/** A point turned a quarter clockwise about the middle of the 20, `quarters` times. */
export function turned([x, y]: Point, quarters: number): Point {
  for (let turn = 0; turn < ((quarters % 4) + 4) % 4; turn++) [x, y] = [20 - y, x];
  return [x, y];
}

/** A line through `points`, turned `quarters`. */
export function polyline(points: Point[], quarters = 0) {
  return `M${points.map((point) => turned(point, quarters)).map(([x, y]) => `${n(x)} ${n(y)}`).join("L")}`;
}

/**
 * A head of two arms at 45°, a line wide, meeting in a sharp point about
 * `middle` across (the box's middle): its outside's point `tip` from the
 * box's top and its arms' outsides running `reach` back from it across and
 * down — pointing up, turned `quarters`.
 */
export function chevron(tip: number, reach: number, quarters = 0, middle = 10) {
  const point = tip + HALF_LINE * Math.SQRT2, arm = reach - HALF_LINE * Math.SQRT2;
  return polyline([[middle - arm, point + arm], [middle, point], [middle + arm, point + arm]], quarters);
}

/** Where a head's lines meet, its point `tip` from the box's top: where a shaft into it ends. */
export const headPoint = (tip: number) => tip + HALF_LINE * Math.SQRT2;

/**
 * Notion's line arrow: a shaft up from the box's foot (17.5) into a head
 * whose point reaches 2.43 and whose arms' outsides reach the box's sides
 * (2.5 and 17.5), turned `quarters`.
 */
export function lineArrow(quarters: number) {
  const tip = 2.43;
  return `${polyline([[10, 17.5 - HALF_LINE], [10, headPoint(tip)]], quarters)}${chevron(tip, 10 - 2.5, quarters)}`;
}

/** Notion's double arrow: a shaft end to end, a head at each end whose point reaches 0.55 from the box's side and whose arms' outsides run 5.625 back; up and down, or turned a quarter. */
export function doubleArrow(quarters: number) {
  const tip = 0.55, reach = 5.625;
  return `${polyline([[10, headPoint(tip)], [10, 20 - headPoint(tip)]], quarters)}${chevron(tip, reach, quarters)}${chevron(tip, reach, quarters + 2)}`;
}

/**
 * Notion's block arrow, filled: a head 13.75 across from its point at 2.5
 * down at 45° to its shoulders, square below them for 1.175, and a stem
 * 5 wide down to the box's foot (17.5), turned `quarters`.
 */
export function blockArrow(quarters: number) {
  const half = 6.875, shoulder = 2.5 + half, foot = shoulder + 1.175, stem = 2.5;
  return polygon(([[10, 2.5], [10 + half, shoulder], [10 + half, foot], [10 + stem, foot], [10 + stem, 17.5], [10 - stem, 17.5],
    [10 - stem, foot], [10 - half, foot], [10 - half, shoulder]] as Point[]).map((point) => turned(point, quarters)));
}

/**
 * Notion's reply arrow, filled (its reply, reply-all and send-to, read
 * 2026-11-18): a head at each of `tips`, pointing left from its point on
 * 9.375 back at 45° to its back 7.5 on, 12.5 tall (3.125..15.625), each
 * further head's side running on from the back of the one before; from the
 * last back a band 6.25 deep runs `straight` on and bends down, its outside
 * round 5, its inside 3.125, to its foot on 15.625 — mirrored, it points right.
 */
export function replyArrow(tips: number[], straight: number, mirrored = false) {
  const top = 3.125, foot = 15.625, middle = 9.375, deep = 6.25, wide = 7.5;
  const x = (value: number) => n(mirrored ? 20 - value : value), sweep = mirrored ? 0 : 1;
  const back = tips[tips.length - 1]! + wide, start = back + straight;
  let path = `M${x(tips[0]!)} ${middle}`;
  tips.forEach((tip, index) => {
    path += `L${x(tip + deep)} ${top}H${x(tip + wide)}`;
    const next = tips[index + 1];
    if (next !== undefined) path += `V${n(middle - (tip + wide - next))}`;
  });
  path += `V6.25H${x(start)}A5 5 0 0 ${sweep} ${x(start + 5)} 11.25V${foot}H${x(start + 3.125)}A3.125 3.125 0 0 ${1 - sweep} ${x(start)} 12.5H${x(back)}V${foot}`;
  for (let index = tips.length - 1; index >= 0; index--) {
    const tip = tips[index]!, before = tips[index - 1];
    path += `H${x(tip + deep)}`;
    path += before === undefined ? `L${x(tip)} ${middle}Z` : `L${x(before + wide)} ${n(middle + (before + wide - tip))}V${foot}`;
  }
  return path;
}
