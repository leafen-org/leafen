// Notion's round, as its built-in icons turn one (a person's head and
// shoulders, a speech bubble's body, a badge's and a card's corners —
// rastered at 400px, 2026-11-18): each quarter a cubic a little fuller than
// a quarter circle's. With the cubics' own tools, to cut one such shape
// along another.

import { n } from "./nativeIconGlyphShapes";

export type Point = [x: number, y: number];
export type Cubic = [Point, Point, Point, Point];

/**
 * How full a round quarter is: a cubic whose handles reach this share of
 * its width and its height (a quarter circle's reach 0.552) — fitted to
 * Notion's user, follow and user circle within a fiftieth of the 20, and to
 * its chat's body and its credit card's corners within a hundredth; a round
 * shape reaches 3% further out across its diagonals than along its axes, as
 * such a quarter does.
 */
export const ROUND_HANDLE = 0.615;

const between = (a: Point, b: Point, t: number): Point => [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t];
/** A cubic cut in two at `t`. */
export function split([p0, p1, p2, p3]: Cubic, t: number): [Cubic, Cubic] {
  const a = between(p0, p1, t), b = between(p1, p2, t), c = between(p2, p3, t);
  const d = between(a, b, t), e = between(b, c, t), f = between(d, e, t);
  return [[p0, a, d, f], [f, e, c, p3]];
}
export const pointAt = (cubic: Cubic, t: number) => split(cubic, t)[0][3];
/** A cubic mirrored across the upright line at `about`, run the other way. */
export const flip = (cubic: Cubic, about: number): Cubic => cubic.map(([x, y]): Point => [2 * about - x, y]).reverse() as Cubic;
/** A cubic run the other way. */
export const back = (cubic: Cubic): Cubic => cubic.slice().reverse() as Cubic;
/** A cubic drawn on from where the pen is. */
export const curve = ([, a, b, c]: Cubic) => `C${n(a[0])} ${n(a[1])} ${n(b[0])} ${n(b[1])} ${n(c[0])} ${n(c[1])}`;
export const at = ([x, y]: Point) => `${n(x)} ${n(y)}`;

/** Where along a cubic a test first holds, from its start (the test failing at the start and holding at the end). */
export function enters(cubic: Cubic, holds: (point: Point) => boolean) {
  let from = 0, to = 1;
  for (let step = 0; step < 40; step++) {
    const middle = (from + to) / 2;
    if (holds(pointAt(cubic, middle))) to = middle;
    else from = middle;
  }
  return to;
}

/** A round shape's quarters, `rx` across and `ry` down from its middle, clockwise from the top. */
export function roundQuarters(cx: number, cy: number, rx: number, ry = rx): [Cubic, Cubic, Cubic, Cubic] {
  const kx = ROUND_HANDLE * rx, ky = ROUND_HANDLE * ry;
  return [
    [[cx, cy - ry], [cx + kx, cy - ry], [cx + rx, cy - ky], [cx + rx, cy]],
    [[cx + rx, cy], [cx + rx, cy + ky], [cx + kx, cy + ry], [cx, cy + ry]],
    [[cx, cy + ry], [cx - kx, cy + ry], [cx - rx, cy + ky], [cx - rx, cy]],
    [[cx - rx, cy], [cx - rx, cy - ky], [cx - kx, cy - ry], [cx, cy - ry]],
  ];
}

/** That shape, closed. */
export function round(cx: number, cy: number, rx: number, ry = rx) {
  const quarters = roundQuarters(cx, cy, rx, ry);
  return `M${at(quarters[0][0])}${quarters.map(curve).join("")}Z`;
}

/** A plate from (x0, y0) to (x1, y1), each corner a round quarter of radius `r`, or of its own radius (top left, top right, foot right, foot left), 0 sharp. */
export function plate(x0: number, y0: number, x1: number, y1: number, r: number | readonly [number, number, number, number]) {
  const [left, right, footRight, footLeft] = typeof r === "number" ? [r, r, r, r] : r;
  const corner = (cx: number, cy: number, radius: number, quarter: 0 | 1 | 2 | 3) => (radius ? curve(roundQuarters(cx, cy, radius)[quarter]) : "");
  return `M${at([x0 + left, y0])}H${n(x1 - right)}${corner(x1 - right, y0 + right, right, 0)}V${n(y1 - footRight)}${corner(x1 - footRight, y1 - footRight, footRight, 1)}`
    + `H${n(x0 + footLeft)}${corner(x0 + footLeft, y1 - footLeft, footLeft, 2)}V${n(y0 + left)}${corner(x0 + left, y0 + left, left, 3)}Z`;
}
