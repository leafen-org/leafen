// Notion's "off" icons (its volume-off, read 2026-11-18): the icon with a
// slash 1.25 wide from the box's top left corner to its foot right, and
// the icon cut away 1.25 either side of the slash.

import { polygon } from "./nativeIconGlyphShapes";

type Point = [number, number];

/** The slash: a bar 1.25 wide from corner to corner of the box (1.87..18.13), its ends square to it. */
export const offSlash = polygon([[2.76, 1.875], [18.125, 17.24], [17.24, 18.125], [1.875, 2.76]]);

/** How far the cut reaches either side of the slash's middle, square to it: the slash's half and the gap. */
const CUT = 0.625 + 1.25;

/** The part of a polygon on one side of a line: where `a`·x + `b`·y + `c` is not below nothing. */
function clip(points: Point[], a: number, b: number, c: number): Point[] {
  const out: Point[] = [], side = ([x, y]: Point) => a * x + b * y + c;
  points.forEach((point, index) => {
    const next = points[(index + 1) % points.length]!, here = side(point), there = side(next);
    if (here >= 0) out.push(point);
    if ((here >= 0) !== (there >= 0)) {
      const t = here / (here - there);
      out.push([point[0] + (next[0] - point[0]) * t, point[1] + (next[1] - point[1]) * t]);
    }
  });
  return out;
}

/** A filled polygon cut away either side of the slash: its piece above it and its piece below, each a polygon. */
export function offCut(points: Point[]) {
  const reach = CUT * Math.SQRT2;
  return [clip(points, 1, -1, -reach), clip(points, -1, 1, -reach)].filter((piece) => piece.length >= 3).map(polygon).join("");
}
