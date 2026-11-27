// The ink of a built-in icon's lines, drawn as Notion draws its lines (its
// arrows, lists, code, play button, star and flash read as paths and its
// set rastered at 400px, 2026-11-18): a line ends flat, square across it,
// and two lines meet in a sharp corner at a right angle or wider — a
// narrower corner is cut flat. A line's flat end is half a line past its
// last point, where a line that ends alone or beside another has always
// reached; a line that ends on another's middle ends there, inside it; and
// two lines that end on one point meet in a corner like one line's.

import { walk, type Piece, type Point } from "./nativeIconGlyphPath";

/**
 * How far a corner's point may reach from the lines' middle, in half
 * lines, before the corner is cut flat instead (SVG's stroke-miterlimit):
 * a right angle's reaches 1.414 and stays sharp, as Notion's arrowheads,
 * squares and wider shapes are; a corner narrower than 84° is cut, as
 * Notion's play button's 63° corners and its triangle's are.
 */
export const MITER_LIMIT = 1.5;

/** How close two points are to be one: an end on a line, two ends on one point. */
const SAME = 0.05;
/** How far back along its line a flat end's piece starts, so the two overlap. */
const INTO = 0.05;
/** Two lines that end on one point running less than 30° apart lie along each other: each ends alone. */
const ALONG = Math.cos(Math.PI / 6);

/** A line of a path: its pieces that have a length, and whether it closes. */
type Run = { pieces: Piece[]; closed: boolean };

/** An open line's end, and the way the line runs on off it. */
type End = { at: Point; out: Point };
/** The ends of a glyph's open lines that need a piece drawn: those that end alone, and the pairs that end on one point. */
export type LineEnds = { alone: End[]; met: Array<{ at: Point; a: Point; b: Point }> };
/** A glyph's lines, read once: the lines themselves and those ends. */
export type Lines = { runs: Run[]; ends: LineEnds };
export const NO_LINES: Lines = { runs: [], ends: { alone: [], met: [] } };

const dot = (a: Point, b: Point) => a[0] * b[0] + a[1] * b[1];
const apart = (a: Point, b: Point) => Math.hypot(a[0] - b[0], a[1] - b[1]);

/** Whether a point is on a piece: within SAME of the straight lines through its points. */
function on(point: Point, piece: Piece): boolean {
  // Not within its box, it is not on it: its box's sides are its ends and the points where it turns.
  const sides = [piece.from, piece.to, ...piece.turnsX, ...piece.turnsY];
  for (const axis of [0, 1] as const) {
    if (sides.every((side) => side[axis] < point[axis] - SAME) || sides.every((side) => side[axis] > point[axis] + SAME)) return false;
  }
  let from = piece.from;
  for (const to of [...piece.along(), piece.to]) {
    const along: Point = [to[0] - from[0], to[1] - from[1]], length = dot(along, along);
    const t = length ? Math.max(0, Math.min(1, dot([point[0] - from[0], point[1] - from[1]], along) / length)) : 0;
    if (apart(point, [from[0] + t * along[0], from[1] + t * along[1]]) < SAME) return true;
    from = to;
  }
  return false;
}

/** Whether a point is inside these outlines, counted as a fill counts them (evenodd). */
function within([x, y]: Point, outlines: Point[][]): boolean {
  let inside = false;
  for (const outline of outlines) {
    outline.forEach((a, index) => {
      const b = outline[(index + 1) % outline.length]!;
      if (a[1] > y !== b[1] > y && x < ((b[0] - a[0]) * (y - a[1])) / (b[1] - a[1]) + a[0]) inside = !inside;
    });
  }
  return inside;
}

/**
 * A path's lines, and which of their open ends end alone or on another's
 * end — the rest end on a line's middle. A dashed line's ends are where
 * its dashes are cut (`flat` false): none of them needs a piece. Where
 * the closed lines are filled (`filled`), a line that comes from outside
 * them and ends at one's side, heading into it, ends inside that outline's
 * ink: it needs no piece either — one would be cut out of the fill.
 */
export function readLines(stroke: string | undefined, flat: boolean, filled = false): Lines {
  if (!stroke) return NO_LINES;
  const runs: Run[] = [];
  let run: Run | null = null;
  walk(stroke, (piece) => {
    if (piece.upper === "M" || !run) { run = { pieces: [], closed: false }; runs.push(run); }
    if (piece.leaves) run.pieces.push(piece);
    if (piece.upper === "Z") { run.closed = true; run = null; }
  });
  const lines: Lines = { runs: runs.filter((each) => each.pieces.length > 0), ends: { alone: [], met: [] } };
  if (!flat) return lines;
  const ends: Array<End & { piece: Piece }> = [];
  for (const { pieces, closed } of lines.runs) {
    if (closed) continue;
    const first = pieces[0]!, last = pieces[pieces.length - 1]!;
    ends.push({ at: first.from, out: [-first.leaves![0], -first.leaves![1]], piece: first }, { at: last.to, out: last.arrives!, piece: last });
  }
  const outlines = filled ? lines.runs.filter(({ closed }) => closed).map(({ pieces }) => pieces.flatMap((piece) => [piece.from, ...piece.along()])) : [];
  ends.forEach((end, index) => {
    // The other ends on this point; a line too short to tell its two ends apart ends alone at both.
    const together = ends.filter((other, at) => at !== index && other.piece !== end.piece && apart(end.at, other.at) < SAME);
    const theirs = new Set([end.piece, ...together.map((other) => other.piece)]);
    // On the middle of a line that is none of theirs, it ends there, inside that line.
    if (lines.runs.some(({ pieces }) => pieces.some((piece) => !theirs.has(piece) && on(end.at, piece)))) return;
    // The lines that end here running apart meet in a corner each, drawn once; one straight on from another needs none.
    const partners = together.filter((other) => dot(end.out, other.out) < ALONG);
    for (const other of partners) if (ends.indexOf(other) > index && dot(end.out, other.out) > -0.999) lines.ends.met.push({ at: end.at, a: end.out, b: other.out });
    if (partners.length) return;
    const ahead = (far: number): Point => [end.at[0] + end.out[0] * far, end.at[1] + end.out[1] * far];
    if (within(ahead(0.9), outlines) && !within(ahead(-1.2), outlines)) return;
    lines.ends.alone.push({ at: end.at, out: end.out });
  });
  return lines;
}

/** Those ends where the glyph is stretched `sx` across and `sy` down and moved by `ox`, `oy`. */
export function placeEnds(ends: LineEnds, sx: number, sy: number, ox: number, oy: number): LineEnds {
  const at = ([x, y]: Point): Point => [ox + x * sx, oy + y * sy];
  const turned = turn(sx, sy);
  return {
    alone: ends.alone.map((end) => ({ at: at(end.at), out: turned(end.out) })),
    met: ends.met.map((end) => ({ at: at(end.at), a: turned(end.a), b: turned(end.b) })),
  };
}

/** A way as it runs where the drawing is stretched `sx` across and `sy` down, a unit again. */
const turn = (sx: number, sy: number) => ([x, y]: Point): Point => {
  const length = Math.hypot(x * sx, y * sy) || 1;
  return [(x * sx) / length, (y * sy) / length];
};

/**
 * The pieces that make those ends flat and those corners whole, as lines
 * of the same path: from a little way back along its line, half a line on
 * past an end that ends alone; and round the point two lines end on.
 */
export function endsPath(ends: LineEnds, half: number): string {
  const n = (value: number) => Number(value.toFixed(3));
  const along = ([x, y]: Point, [dx, dy]: Point, far: number) => `${n(x + dx * far)} ${n(y + dy * far)}`;
  return ends.alone.map(({ at, out }) => `M${along(at, out, -INTO)}L${along(at, out, half)}`).join("")
    + ends.met.map(({ at, a, b }) => `M${along(at, a, -INTO)}L${along(at, a, 0)}L${along(at, b, -INTO)}`).join("");
}

/** Where ink reaches: a point of the drawing, which moves as the drawing is scaled, and how far past it the ink goes, which does not. */
export type Ink = Array<[x: number, y: number, pastX: number, pastY: number]>;

/** How far a corner's point reaches from its lines' meeting point, in half lines; none where the corner is straight or cut. */
function corner(arrives: Point, leaves: Point): Point | null {
  const cross = arrives[0] * leaves[1] - arrives[1] * leaves[0], along = dot(arrives, leaves);
  if (Math.abs(cross) < 1e-6 || Math.sqrt(2 / (1 + along)) > MITER_LIMIT) return null;
  // The point is on the side the lines turn away from, square to each of them.
  const side = cross > 0 ? 1 : -1;
  return [(side * (arrives[1] + leaves[1])) / (1 + along), (-side * (arrives[0] + leaves[0])) / (1 + along)];
}

/** A fill's points that can be its ink's sides: its pieces' ends and the points where they turn. */
export function fillPoints(path: string | undefined): Point[] {
  const points: Point[] = [];
  if (path) walk(path, ({ to, turnsX, turnsY }) => { points.push(to, ...turnsX, ...turnsY); });
  return points;
}

/**
 * A glyph's ink where it is stretched `sx` across and `sy` down: its
 * fill's points, and its lines' `half` a line each side of them — beside
 * each point where a piece turns, square across each piece's two ends, out
 * to each sharp corner's point, and at the flat ends. A stretch keeps the
 * points where a piece turns and turns the way each piece runs.
 */
export function glyphInk(fill: Point[], lines: Lines, half: number, sx = 1, sy = 1): Ink {
  const ink: Ink = [];
  eachInk(fill, lines, half, sx, sy, (x, y, pastX, pastY) => { ink.push([x, y, pastX, pastY]); });
  return ink;
}

/** The same points of a glyph's ink, each handed to `visit` as it is found: a placing measures its ink many times over. */
export function eachInk(fill: Point[], lines: Lines, half: number, sx: number, sy: number, visit: (x: number, y: number, pastX: number, pastY: number) => void) {
  for (const [x, y] of fill) visit(x * sx, y * sy, 0, 0);
  const turned = turn(sx, sy);
  const add = (at: Point, x: number, y: number) => visit(at[0] * sx, at[1] * sy, x * half, y * half);
  const sharp = (at: Point, arrives: Point, leaves: Point) => { const point = corner(arrives, leaves); if (point) add(at, point[0], point[1]); };
  for (const { pieces, closed } of lines.runs) {
    pieces.forEach((piece, index) => {
      for (const at of piece.turnsX) { add(at, 1, 0); add(at, -1, 0); }
      for (const at of piece.turnsY) { add(at, 0, 1); add(at, 0, -1); }
      const leaves = turned(piece.leaves!), arrives = turned(piece.arrives!);
      add(piece.from, -leaves[1], leaves[0]); add(piece.from, leaves[1], -leaves[0]);
      add(piece.to, -arrives[1], arrives[0]); add(piece.to, arrives[1], -arrives[0]);
      const next = pieces[index + 1] ?? (closed ? pieces[0] : undefined);
      if (next) sharp(piece.to, arrives, turned(next.leaves!));
    });
  }
  for (const end of lines.ends.alone) { const [x, y] = turned(end.out); add(end.at, x - y, y + x); add(end.at, x + y, y - x); }
  for (const { at, a, b } of lines.ends.met) { const back = turned(b); sharp(at, turned(a), [-back[0], -back[1]]); }
}

/** How far from its outlines' lines a point of a line is to be inside them and not on them. */
const CLEAR = 0.05;
/** How far from either end of a line its points are its own, not where it meets an outline. */
const FROM_ENDS = 1.875;

/**
 * Of a solid glyph's open lines, each a subpath of `open`, those cut out of
 * its filled outlines (`closed`): the lines that run inside them — a book's
 * pages, a face's eyes — or along their lines, parting two of them (a bin's
 * lid from its body, a boot's sole). A line that only comes up to an
 * outline from outside and ends on its ink — a pin's needle at its head, a
 * flag's pole at its cloth — is drawn and not cut: a cut of it would end on
 * the outline's line and leave a thin seam along it.
 */
export function cutLines(open: string, closed: string): string {
  const outlines: Point[][] = [];
  walk(closed, (piece) => {
    if (piece.upper === "M") outlines.push([piece.to]);
    else outlines[outlines.length - 1]?.push(...piece.along(), piece.to);
  });
  // Each outline's box, a little wider: a point outside it is clear of that outline's lines.
  const boxes = outlines.map((outline) => [
    Math.min(...outline.map(([x]) => x)) - CLEAR, Math.min(...outline.map(([, y]) => y)) - CLEAR,
    Math.max(...outline.map(([x]) => x)) + CLEAR, Math.max(...outline.map(([, y]) => y)) + CLEAR,
  ]);
  const clear = (point: Point) => outlines.every((outline, at) => {
    const [x0, y0, x1, y1] = boxes[at]!;
    return point[0] < x0! || point[0] > x1! || point[1] < y0! || point[1] > y1! || outline.every((a, index) => {
    const b = outline[(index + 1) % outline.length]!, along: Point = [b[0] - a[0], b[1] - a[1]], length = dot(along, along);
    const t = length ? Math.max(0, Math.min(1, dot([point[0] - a[0], point[1] - a[1]], along) / length)) : 0;
    return apart(point, [a[0] + t * along[0], a[1] + t * along[1]]) > CLEAR;
    });
  });
  return open.split(/(?=M)/).filter((line) => {
    // Its points in order — along a straight piece a tenth of its way apart — each with how far along the line it is.
    const points: Array<[Point, number]> = [];
    let far = 0;
    walk(line, (piece) => {
      if (piece.upper === "M") { points.push([piece.to, 0]); return; }
      const tenths = !"LHVZ".includes(piece.upper) ? [] : Array.from({ length: 9 }, (_, step): Point => [
        piece.from[0] + ((piece.to[0] - piece.from[0]) * (step + 1)) / 10, piece.from[1] + ((piece.to[1] - piece.from[1]) * (step + 1)) / 10]);
      for (const point of [...piece.along(), ...tenths, piece.to]) { far += apart(points[points.length - 1]![0], point); points.push([point, far]); }
    });
    return points.some(([point, along]) => {
      if (clear(point)) return within(point, outlines);
      return along > FROM_ENDS && far - along > FROM_ENDS;
    });
  }).join("");
}
