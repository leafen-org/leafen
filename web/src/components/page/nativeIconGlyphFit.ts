// A built-in icon glyph brought into the box Notion's icon of that name
// inks (nativeIconBoxes.ts): its drawing stretched and moved so its ink
// fills the box across and down, or, where its drawing is not Notion's
// shape, scaled about the middle to the farthest edge Notion's reaches —
// its lines as heavy as before. The ink a glyph covers is its fills and its
// lines as they are drawn — half a line each side, flat ends and sharp
// corners (nativeIconGlyphInk.ts).

import { eachInk, glyphInk, type Lines } from "./nativeIconGlyphInk";
import type { Box, Point } from "./nativeIconGlyphPath";

/** How a glyph is placed: stretched `sx` across and `sy` down, then moved by `ox`, `oy`. */
export type Placing = [sx: number, sy: number, ox: number, oy: number];
export const AS_DRAWN: Placing = [1, 1, 0, 0];

/** How far a side of the ink may lie off Notion's box and be left as drawn: what its 400px raster tells apart. */
const ON_BOX = 0.03;
/**
 * How much longer across than down (or down than across) a glyph's ink may
 * be against Notion's box and still be stretched into it: 30% — past that
 * a round thing stretched no longer reads as itself, and the drawing is
 * laid another way than Notion's (its 885 icons and Leafen's rastered at
 * 400px, 2026-11-18: up to it a stretch brought Leafen's nearer Notion's)
 * — 60%
 * for a drawing of straight lines only (a gate, a play button), which a
 * stretch bends nowhere.
 */
const STRETCH = { curved: 1.3, straight: 1.6 };

/** The box a glyph of these fill points and lines inks as drawn. */
function inkBox(fill: Point[], lines: Lines, line: number): Box {
  const box: Box = [Infinity, Infinity, -Infinity, -Infinity];
  for (const [x, y, pastX, pastY] of glyphInk(fill, lines, line / 2)) {
    box[0] = Math.min(box[0], x + pastX); box[1] = Math.min(box[1], y + pastY);
    box[2] = Math.max(box[2], x + pastX); box[3] = Math.max(box[3], y + pastY);
  }
  return box;
}

/** What a glyph's drawing allows: whether it is of straight lines only, whether it can be stretched one way (no turned oval). */
export type Drawn = { straight: boolean; stretches: boolean };

/**
 * How a glyph is brought into `box`, the box Notion's icon of that name
 * inks: left as drawn where its ink already lies on it, every side within
 * what the raster tells apart (a shape drawn to Notion's own measures, a
 * ring on the round edge); stretched into it across and down where its
 * ink's proportions are near enough Notion's (inFrame) — its ink then
 * fills the box on all four sides, its body where Notion's body is; and
 * otherwise — a drawing laid another way than Notion's, or one a one-way
 * stretch cannot draw — scaled about the middle until its farthest ink
 * reaches the farthest edge Notion's does (onEdge).
 */
export function placeInBox(fill: Point[], lines: Lines, box: Box, line: number, drawn: Drawn): Placing {
  const ink = inkBox(fill, lines, line);
  if (!Number.isFinite(ink[0])) return AS_DRAWN;
  if (ink.every((side, index) => Math.abs(side - box[index]!) <= ON_BOX)) return AS_DRAWN;
  const across = (box[2] - box[0]) / (ink[2] - ink[0]), down = (box[3] - box[1]) / (ink[3] - ink[1]);
  const near = Math.max(across, down) / Math.min(across, down) <= (drawn.straight ? STRETCH.straight : STRETCH.curved);
  if (near && drawn.stretches) return inFrame(fill, lines, box, line);
  return onEdge(fill, lines, Math.max(10 - box[0], 10 - box[1], box[2] - 10, box[3] - 10), line);
}

/**
 * The scale about the middle that brings the ink's far edge `edge` from
 * it, for a glyph of these fill points and lines, a `line` wide: each
 * point of its ink is a point of the drawing, which the scale moves, and
 * how far past it the ink goes, which it does not; the scale that brings
 * the farthest one to the edge leaves every other inside it.
 */
export function onEdge(fill: Point[], lines: Lines, edge: number, line: number): Placing {
  let scale = Infinity;
  for (const [x, y, pastX, pastY] of glyphInk(fill, lines, line / 2)) {
    for (const [reach, past] of [[10 - x, -pastX], [x - 10, pastX], [10 - y, -pastY], [y - 10, pastY]] as const) {
      if (reach > 1e-9) scale = Math.min(scale, (edge - past) / reach);
    }
  }
  return Number.isFinite(scale) ? [scale, scale, 10 - 10 * scale, 10 - 10 * scale] : AS_DRAWN;
}

/**
 * The stretch and move that make the glyph's ink fill `frame` (the box
 * Notion's icon of that name inks, on its grid): along each axis the
 * stretch that gives its ink the frame's length — its lines staying as
 * heavy — and then the move that lines its near side up with the frame's.
 * A stretch one way turns a slanted line's flat end and a corner's point,
 * and can cut a corner that was sharp, so each axis's stretch is found on
 * the stretched drawing's own ink, between two stretches that bracket it,
 * and the two are found again in turn until neither moves.
 */
export function inFrame(fill: Point[], lines: Lines, frame: Box, line: number): Placing {
  const stretch: [across: number, down: number] = [1, 1];
  const sides = (axis: 0 | 1): [near: number, far: number] => {
    let near = Infinity, far = -Infinity;
    eachInk(fill, lines, line / 2, stretch[0], stretch[1], (x, y, pastX, pastY) => {
      const value = axis === 0 ? x + pastX : y + pastY;
      if (value < near) near = value;
      if (value > far) far = value;
    });
    return [near, far];
  };
  if (!Number.isFinite(sides(0)[0])) return AS_DRAWN;
  /** The stretch along an axis that gives the ink `length`, by false position — halving where that stalls. */
  const fit = (axis: 0 | 1, length: number) => {
    const over = (value: number) => { stretch[axis] = value; const [near, far] = sides(axis); return far - near - length; };
    let [low, high] = [0.25, 4], [under, above] = [over(low), over(high)];
    for (let step = 0; step < 40 && high - low > 1e-7; step++) {
      const straight = low + ((high - low) * -under) / (above - under);
      const middle = step % 3 === 2 || !(straight > low && straight < high) ? (low + high) / 2 : straight;
      const value = over(middle);
      if (value < 0) [low, under] = [middle, value]; else [high, above] = [middle, value];
      if (Math.abs(value) < 1e-5) return;
    }
    stretch[axis] = (low + high) / 2;
  };
  for (let round = 0; round < 4; round++) {
    const [across, down] = stretch;
    fit(0, frame[2] - frame[0]); fit(1, frame[3] - frame[1]);
    if (Math.abs(stretch[0] - across) < 1e-5 && Math.abs(stretch[1] - down) < 1e-5) break;
  }
  return [stretch[0], stretch[1], frame[0] - sides(0)[0], frame[1] - sides(1)[0]];
}
