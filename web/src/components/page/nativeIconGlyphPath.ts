// A built-in icon glyph's paths as geometry: each path walked piece by
// piece in absolute terms — where a piece's x and y turn (its box's sides),
// which way it leaves its start and arrives at its end, and points along
// it — and written out again stretched and moved.

import type { NativeIconGlyph } from "./nativeIconGlyphShapes";

export type Box = [x0: number, y0: number, x1: number, y1: number];
export type Point = [x: number, y: number];

/** A piece of a path, one command's: a move, a line, a curve, an arc or a close. */
export type Piece = {
  /** Its command, in capitals, with its arguments in absolute terms; a smooth curve (S, T) as the whole curve (C, Q). */
  upper: string;
  values: number[];
  from: Point;
  to: Point;
  /** Where its x turns and where its y turns, between its ends. */
  turnsX: Point[];
  turnsY: Point[];
  /** Points along it between its ends, close enough that the straight lines through them stay within 0.005 of it. */
  along: () => Point[];
  /** The way it leaves `from` and the way it arrives at `to`, a unit each; none where it has no length (and for a move). */
  leaves: Point | null;
  arrives: Point | null;
};

const ARGUMENTS: Readonly<Record<string, number>> = { M: 2, L: 2, H: 1, V: 1, C: 6, S: 4, Q: 4, T: 2, A: 7, Z: 0 };
const NUMBER = /-?(?:\d*\.\d+|\d+\.?)(?:[eE][-+]?\d+)?/y;
const GAP = /[\s,]*/y;
const COMMAND = /[MLHVCSQTAZ]/i;

/** A path's commands, each with its arguments; an arc's flags read one digit each. */
function commands(path: string): Array<{ command: string; values: number[] }> {
  const out: Array<{ command: string; values: number[] }> = [];
  let at = 0, command = "";
  const skip = () => { GAP.lastIndex = at; GAP.exec(path); at = GAP.lastIndex; };
  for (skip(); at < path.length; skip()) {
    if (COMMAND.test(path[at]!)) command = path[at++]!;
    const upper = command.toUpperCase(), count = ARGUMENTS[upper];
    if (count === undefined) throw new Error(`glyph path: no command at ${at}`);
    const values: number[] = [];
    for (let index = 0; index < count; index++) {
      skip();
      if (upper === "A" && (index === 3 || index === 4)) { values.push(Number(path[at++])); continue; }
      NUMBER.lastIndex = at;
      const match = NUMBER.exec(path);
      if (!match) throw new Error(`glyph path: no number at ${at}`);
      values.push(Number(match[0]));
      at = NUMBER.lastIndex;
    }
    out.push({ command, values });
    if (upper === "Z") command = "";
    // Pairs after a move are lines.
    else if (upper === "M") command = command === "M" ? "L" : "l";
  }
  return out;
}

/** The unit along the first of these that has a length. */
function unit(...ways: Point[]): Point | null {
  for (const [x, y] of ways) {
    const length = Math.hypot(x, y);
    if (length > 1e-9) return [x / length, y / length];
  }
  return null;
}
const way = (from: Point, to: Point): Point => [to[0] - from[0], to[1] - from[1]];

/** Where a curve's coordinate turns, as fractions of its way along: the roots in (0, 1) of a·t² + b·t + c. */
function turns(a: number, b: number, c: number): number[] {
  const inside = (t: number) => t > 0 && t < 1;
  if (Math.abs(a) < 1e-12) return Math.abs(b) < 1e-12 ? [] : [-c / b].filter(inside);
  const square = b * b - 4 * a * c;
  if (square < 0) return [];
  const root = Math.sqrt(square);
  return [(-b + root) / (2 * a), (-b - root) / (2 * a)].filter(inside);
}

type Shape = Pick<Piece, "turnsX" | "turnsY" | "along" | "leaves" | "arrives">;
const CURVE_STEPS = 16;
const steps = (count: number) => Array.from({ length: count - 1 }, (_, index) => (index + 1) / count);
const NONE_ALONG = (): Point[] => [];
/** Points worked out once, when first asked for. */
const once = (points: () => Point[]) => { let kept: Point[] | undefined; return () => (kept ??= points()); };

function line(from: Point, to: Point): Shape {
  const along = unit(way(from, to));
  return { turnsX: [], turnsY: [], along: NONE_ALONG, leaves: along, arrives: along };
}

function cubic(p0: Point, p1: Point, p2: Point, p3: Point): Shape {
  const at = (t: number): Point => {
    const u = 1 - t, a = u * u * u, b = 3 * u * u * t, c = 3 * u * t * t, d = t * t * t;
    return [a * p0[0] + b * p1[0] + c * p2[0] + d * p3[0], a * p0[1] + b * p1[1] + c * p2[1] + d * p3[1]];
  };
  const axis = (i: 0 | 1) => turns(3 * (-p0[i] + 3 * p1[i] - 3 * p2[i] + p3[i]), 6 * (p0[i] - 2 * p1[i] + p2[i]), 3 * (p1[i] - p0[i])).map(at);
  return {
    turnsX: axis(0), turnsY: axis(1), along: once(() => steps(CURVE_STEPS).map(at)),
    leaves: unit(way(p0, p1), way(p0, p2), way(p0, p3)), arrives: unit(way(p2, p3), way(p1, p3), way(p0, p3)),
  };
}

function quadratic(p0: Point, p1: Point, p2: Point): Shape {
  const at = (t: number): Point => {
    const u = 1 - t;
    return [u * u * p0[0] + 2 * u * t * p1[0] + t * t * p2[0], u * u * p0[1] + 2 * u * t * p1[1] + t * t * p2[1]];
  };
  const axis = (i: 0 | 1) => turns(0, 2 * (p0[i] - 2 * p1[i] + p2[i]), 2 * (p1[i] - p0[i])).map(at);
  return {
    turnsX: axis(0), turnsY: axis(1), along: once(() => steps(CURVE_STEPS).map(at)),
    leaves: unit(way(p0, p1), way(p0, p2)), arrives: unit(way(p1, p2), way(p0, p2)),
  };
}

/** An SVG arc by its centre (SVG 1.1, F.6.5). */
function arc(from: Point, rx: number, ry: number, degrees: number, large: number, sweep: number, to: Point): Shape {
  if (rx === 0 || ry === 0) return line(from, to);
  const phi = (degrees * Math.PI) / 180, cos = Math.cos(phi), sin = Math.sin(phi);
  const dx = (from[0] - to[0]) / 2, dy = (from[1] - to[1]) / 2;
  const x1 = cos * dx + sin * dy, y1 = -sin * dx + cos * dy;
  let [a, b] = [Math.abs(rx), Math.abs(ry)];
  const grow = (x1 * x1) / (a * a) + (y1 * y1) / (b * b);
  if (grow > 1) { a *= Math.sqrt(grow); b *= Math.sqrt(grow); }
  const square = Math.max(0, (a * a * b * b - a * a * y1 * y1 - b * b * x1 * x1) / (a * a * y1 * y1 + b * b * x1 * x1));
  const sign = large === sweep ? -1 : 1, root = sign * Math.sqrt(square);
  const cx1 = (root * a * y1) / b, cy1 = (-root * b * x1) / a;
  const cx = cos * cx1 - sin * cy1 + (from[0] + to[0]) / 2, cy = sin * cx1 + cos * cy1 + (from[1] + to[1]) / 2;
  const angle = (ux: number, uy: number) => Math.atan2(uy, ux);
  const start = angle((x1 - cx1) / a, (y1 - cy1) / b);
  let delta = angle((-x1 - cx1) / a, (-y1 - cy1) / b) - start;
  if (sweep && delta < 0) delta += 2 * Math.PI;
  if (!sweep && delta > 0) delta -= 2 * Math.PI;
  const point = (theta: number): Point => {
    const ex = a * Math.cos(theta), ey = b * Math.sin(theta);
    return [cos * ex - sin * ey + cx, sin * ex + cos * ey + cy];
  };
  /** The way the arc runs at `theta`. */
  const heading = (theta: number) => {
    const ex = -a * Math.sin(theta) * Math.sign(delta), ey = b * Math.cos(theta) * Math.sign(delta);
    return unit([cos * ex - sin * ey, sin * ex + cos * ey]);
  };
  // Where x turns (tan θ = −b·sin φ / a·cos φ) and where y does (tan θ = b·cos φ / a·sin φ), half a turn apart each.
  const thetaX = Math.atan2(-b * sin, a * cos), thetaY = Math.atan2(b * cos, a * sin);
  const swept = (theta: number) => {
    const along = ((((theta - start) * Math.sign(delta)) % (2 * Math.PI)) + 2 * Math.PI) % (2 * Math.PI);
    return along > 0 && along < Math.abs(delta);
  };
  // A step this long round keeps the straight line across it within 0.005 of the arc.
  const count = Math.max(2, Math.ceil(Math.abs(delta) / Math.sqrt(0.04 / Math.max(a, b))));
  return {
    turnsX: [thetaX, thetaX + Math.PI].filter(swept).map(point), turnsY: [thetaY, thetaY + Math.PI].filter(swept).map(point),
    along: once(() => steps(count).map((t) => point(start + delta * t))),
    leaves: heading(start), arrives: heading(start + delta),
  };
}

/** Each command of a path as a piece, in absolute terms. */
export function walk(path: string, visit: (piece: Piece) => void) {
  let x = 0, y = 0, startX = 0, startY = 0, controlX = 0, controlY = 0, last = "";
  for (const { command, values } of commands(path)) {
    const upper = command.toUpperCase(), relative = command !== upper;
    const ax = (value: number) => (relative ? x + value : value), ay = (value: number) => (relative ? y + value : value);
    const from: Point = [x, y];
    let absolute: number[] = [], shape: Shape | null = null;
    if (upper === "Z") { x = startX; y = startY; }
    else if (upper === "M") { x = startX = ax(values[0]!); y = startY = ay(values[1]!); absolute = [x, y]; }
    else if (upper === "L") { x = ax(values[0]!); y = ay(values[1]!); absolute = [x, y]; }
    else if (upper === "H") { x = ax(values[0]!); absolute = [x]; }
    else if (upper === "V") { y = ay(values[0]!); absolute = [y]; }
    else if (upper === "C" || upper === "S") {
      const reflected: Point = /[CS]/.test(last) ? [2 * x - controlX, 2 * y - controlY] : [x, y];
      const [c1, c2, end]: Point[] = upper === "C"
        ? [[ax(values[0]!), ay(values[1]!)], [ax(values[2]!), ay(values[3]!)], [ax(values[4]!), ay(values[5]!)]]
        : [reflected, [ax(values[0]!), ay(values[1]!)], [ax(values[2]!), ay(values[3]!)]];
      shape = cubic(from, c1!, c2!, end!);
      absolute = [...c1!, ...c2!, ...end!];
      [controlX, controlY] = c2!; [x, y] = end!;
    } else if (upper === "Q" || upper === "T") {
      const control: Point = upper === "Q" ? [ax(values[0]!), ay(values[1]!)] : /[QT]/.test(last) ? [2 * x - controlX, 2 * y - controlY] : [x, y];
      const end: Point = upper === "Q" ? [ax(values[2]!), ay(values[3]!)] : [ax(values[0]!), ay(values[1]!)];
      shape = quadratic(from, control, end);
      absolute = [...control, ...end];
      [controlX, controlY] = control; [x, y] = end;
    } else if (upper === "A") {
      const end: Point = [ax(values[5]!), ay(values[6]!)];
      shape = arc(from, values[0]!, values[1]!, values[2]!, values[3]!, values[4]!, end);
      absolute = [values[0]!, values[1]!, values[2]!, values[3]!, values[4]!, ...end];
      [x, y] = end;
    }
    last = upper;
    const to: Point = [x, y];
    // A smooth curve is written out whole, as the curve it is.
    visit({ upper: upper === "S" ? "C" : upper === "T" ? "Q" : upper, values: absolute, from: upper === "M" ? to : from, to, ...(upper === "M" ? { turnsX: [], turnsY: [], along: NONE_ALONG, leaves: null, arrives: null } : shape ?? line(from, to)) });
  }
}

/** How far along the curve's way a handle found on its end is set. */
const HANDLE = 0.1;

/**
 * A path stretched `sx` across and `sy` down, then moved by `ox`, `oy`,
 * written out in absolute commands, whole curves for smooth ones. An arc's
 * radii stretch with the axis each lies along; a turned one only where it
 * stretches alike both ways. A path left as drawn keeps its numbers to
 * the grid's last digit (a sixty-fourth of the box has four).
 */
export function transformPath(path: string, sx: number, sy: number, ox: number, oy: number): string {
  let out = "";
  walk(path, ({ upper, values, from, to, leaves, arrives }) => {
    if (upper === "Z") { out += "Z"; return; }
    const digits = sx === 1 && sy === 1 ? 4 : 3, grain = 10 ** digits;
    const at = (value: number) => Number(value.toFixed(digits));
    const x = (value: number) => at(ox + value * sx), y = (value: number) => at(oy + value * sy);
    let moved: number[];
    if (upper === "C" && leaves && arrives) {
      // A handle that sits on its end gives the curve no way to run there, and a browser then draws no corner
      // at that end (a pin's point, a drop's): each such handle is set a little along the curve's way instead.
      const [c1x, c1y, c2x, c2y] = values as [number, number, number, number];
      const first: Point = Math.hypot(c1x - from[0], c1y - from[1]) < 1e-6 ? [from[0] + HANDLE * leaves[0], from[1] + HANDLE * leaves[1]] : [c1x, c1y];
      const second: Point = Math.hypot(c2x - to[0], c2y - to[1]) < 1e-6 ? [to[0] - HANDLE * arrives[0], to[1] - HANDLE * arrives[1]] : [c2x, c2y];
      moved = [x(first[0]), y(first[1]), x(second[0]), y(second[1]), x(to[0]), y(to[1])];
    } else if (upper === "H") moved = [x(values[0]!)];
    else if (upper === "V") moved = [y(values[0]!)];
    else if (upper === "A") {
      const [rx, ry, degrees] = values as [number, number, number];
      const across = ((degrees % 180) + 180) % 180;
      if (sx !== sy && across !== 0 && across !== 90) throw new Error("glyph path: a turned arc cannot stretch one way");
      const [rxScale, ryScale] = across === 90 ? [sy, sx] : [sx, sy];
      // Its radii rounded down: a half turn's radius rounded up past half the way between its ends would move
      // its middle off that way by far more (0.12 for 0.0005), where one rounded down is only scaled back up.
      const down = (value: number) => Math.floor(value * grain + 1e-6) / grain;
      moved = [down(rx * rxScale), down(ry * ryScale), degrees, values[3]!, values[4]!, x(values[5]!), y(values[6]!)];
    } else moved = values.map((value, index) => (index % 2 === 0 ? x(value) : y(value)));
    out += `${upper}${moved.join(" ")}`;
  });
  return out;
}

/** A glyph's paths stretched and moved alike. */
export function transformGlyph(glyph: NativeIconGlyph, sx: number, sy: number, ox: number, oy: number): NativeIconGlyph {
  return {
    ...glyph,
    ...(glyph.fill ? { fill: transformPath(glyph.fill, sx, sy, ox, oy) } : {}),
    ...(glyph.stroke ? { stroke: transformPath(glyph.stroke, sx, sy, ox, oy) } : {}),
  };
}
