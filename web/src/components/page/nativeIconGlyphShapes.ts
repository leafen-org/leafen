// Shape helpers and shared marks for Leafen's built-in icon glyphs: a 20px grid
// with a 16px live area, fills and lines with flat ends and sharp corners (painted
// by nativeIconDrawing.tsx), their edges where Notion's are (ROUND_EDGE, SQUARE_EDGE,
// FULL_EDGE, WIDE_EDGE).

export type NativeIconGlyph = {
  fill?: string;
  stroke?: string;
  /** Its line broken by this dash array; the dashes end flat, as every dashed line of Notion's does. */
  dash?: string;
  /** Drawn as a filled disc with the stroke and fill cut out of it. */
  disc?: true;
  /** Its lines' weight in units of the 20 where it is not the set's (NATIVE_ICON_LINE). */
  weight?: number;
};

/** A figure's limbs, as heavy as Notion's people (its run, walk and child at 100px, 2026-11-16). */
export const FIGURE_LINE = 2.4;

export const n = (value: number) => Number(value.toFixed(2));

export function circle(cx: number, cy: number, r: number) {
  return `M${n(cx - r)} ${n(cy)}a${n(r)} ${n(r)} 0 1 0 ${n(2 * r)} 0a${n(r)} ${n(r)} 0 1 0 ${n(-2 * r)} 0Z`;
}

export function polygon(points: Array<[number, number]>) {
  return `M${points.map(([x, y]) => `${n(x)} ${n(y)}`).join("L")}Z`;
}

export function regular(sides: number, r: number, cx = 10, cy = 10, rotation = -90) {
  return Array.from({ length: sides }, (_, index): [number, number] => {
    const angle = ((rotation + (360 / sides) * index) * Math.PI) / 180;
    return [cx + r * Math.cos(angle), cy + r * Math.sin(angle)];
  });
}

export function star(outer: number, inner: number, cx = 10, cy = 10.4) {
  return Array.from({ length: 10 }, (_, index): [number, number] => {
    const r = index % 2 === 0 ? outer : inner;
    const angle = ((-90 + 36 * index) * Math.PI) / 180;
    return [cx + r * Math.cos(angle), cy + r * Math.sin(angle)];
  });
}

export function rect(x: number, y: number, w: number, h: number, r: number) {
  return `M${n(x + r)} ${n(y)}h${n(w - 2 * r)}a${n(r)} ${n(r)} 0 0 1 ${n(r)} ${n(r)}v${n(h - 2 * r)}`
    + `a${n(r)} ${n(r)} 0 0 1 ${n(-r)} ${n(r)}h${n(-(w - 2 * r))}a${n(r)} ${n(r)} 0 0 1 ${n(-r)} ${n(-r)}`
    + `v${n(-(h - 2 * r))}a${n(r)} ${n(r)} 0 0 1 ${n(r)} ${n(-r)}Z`;
}

// A filled sector of a circle, clockwise from twelve o'clock.
export function sector(fraction: number, r = 7, cx = 10, cy = 10) {
  const angle = fraction * 2 * Math.PI;
  const x = cx + r * Math.sin(angle);
  const y = cy - r * Math.cos(angle);
  return `M${cx} ${cy}V${n(cy - r)}A${r} ${r} 0 ${fraction > 0.5 ? 1 : 0} 1 ${n(x)} ${n(y)}Z`;
}

/**
 * Notion's built-in icons' line, in units of their 20: six half steps of
 * its grid (EDGE_STEP), 1.875, wherever it is measured — a ring, a square
 * and the wider shapes, a list's bars, an arrow's arms, the marks cut out
 * of a disc (rastered at 400px and read as paths, 2026-11-18).
 */
export const NATIVE_ICON_LINE = 1.875;
/**
 * How far a round icon's edge is from the middle: Notion's every round icon
 * — a face, a ring, a disc, a sign in a ring — reaches 2.5..17.5 of the 20
 * (its 885 icons rastered at 400px, 2026-11-17: the 13 faces, the circle and
 * its eighths, compass, clock, the shades, user circle, no entry and the
 * rest alike).
 */
export const ROUND_EDGE = 7.5;
/** A ring's radius: its line inside the round edge. */
export const RING_RADIUS = ROUND_EDGE - NATIVE_ICON_LINE / 2;
/** The ring, written to its radius's last digit (6.5625). */
export const ring = `M${10 - RING_RADIUS} 10a${RING_RADIUS} ${RING_RADIUS} 0 1 0 ${2 * RING_RADIUS} 0a${RING_RADIUS} ${RING_RADIUS} 0 1 0 ${-2 * RING_RADIUS} 0Z`;
/** A slash across a ring, end to end on its line, from the top left down, as Notion slashes its round signs ("no", parking, smoking). */
export const ringSlash = (() => {
  const d = RING_RADIUS / Math.SQRT2;
  return `M${n(10 - d)} ${n(10 - d)}L${n(10 + d)} ${n(10 + d)}`;
})();
export const disc = circle(10, 10, ROUND_EDGE);

/** A line's middle sits this far inside the edge its outside reaches. */
export const HALF_LINE = NATIVE_ICON_LINE / 2;
/**
 * Notion's other edges, from the middle, on its grid of 1/32 of the box
 * (0.625; its 885 icons rastered at 400px, 2026-11-17 — 312 reach the
 * round edge, 185 the full edge, 136 the wide edge, 28 the square edge):
 * a square's sides 6.875 (3.125..16.875 of the 20 — the square and its
 * parts, the dice, a newspaper, a jar, a server, rows and columns), a glyph that
 * fills its box 8.125, one line short of the box's edge (a gear, a sun, a
 * diamond sign, a chip's pins, crop marks), and a wide one 8.75
 * (brightness's rays).
 */
export const SQUARE_EDGE = 6.875;
export const FULL_EDGE = 8.125;
export const WIDE_EDGE = 8.75;
/** The box's own side: a glyph that touches it (a battery's tip, a trumpet's bell). */
export const BOX_EDGE = 10;
/**
 * Half a step of that grid (1/64 of the box, 0.3125): every edge Notion's
 * icons reach is a whole number of these in from the box's side (its 885
 * icons, 2026-11-17 — BOX_EDGE 0, WIDE_EDGE 4, FULL_EDGE 6, ROUND_EDGE 8,
 * SQUARE_EDGE 10; the rest of them 1, 2, 3, 5, 7, 9, 12, 13 or 14).
 */
export const EDGE_STEP = 0.3125;
export const edgeIn = (steps: number) => BOX_EDGE - steps * EDGE_STEP;

/** How far a regular polygon's corners are from its middle when its sides' outside is `edge` from it. */
const cornersOnEdge = (sides: number, edge: number) => (edge - HALF_LINE) / Math.cos(Math.PI / sides);

/*
 * Notion's shapes (its square, triangle, pentagon, hexagon and octagon,
 * their dashed and part-filled kinds, and the icons framed by them — the
 * dice, a promoted arrow, an error's octagon — rastered at 400px, 2026-11-17): the
 * square's sides on the square edge; the triangle's point on the round edge
 * and its foot on the square edge; the octagon's and hexagon's sides on the
 * round edge (a hexagon's points 8.66 out, up and down); the pentagon's foot
 * on the round edge, its middle half a grid step (0.3125) below the box's,
 * so its shoulders reach 1.55 and its point 1.43. Their lines meet in sharp
 * corners — but the triangle's, which Notion cuts off where a round corner
 * would end.
 */
/** The square's line: its middle on the near and far sides (4.0625 and 15.9375). */
export const SQUARE_NEAR = 10 - SQUARE_EDGE + HALF_LINE;
export const SQUARE_FAR = 20 - SQUARE_NEAR;
export const squareCorners: Array<[number, number]> = [[SQUARE_NEAR, SQUARE_NEAR], [SQUARE_FAR, SQUARE_NEAR], [SQUARE_FAR, SQUARE_FAR], [SQUARE_NEAR, SQUARE_FAR]];
export const square = `M${SQUARE_NEAR} ${SQUARE_NEAR}H${SQUARE_FAR}V${SQUARE_FAR}H${SQUARE_NEAR}Z`;
const triangleTop = 10 - ROUND_EDGE + HALF_LINE, triangleFoot = 10 + SQUARE_EDGE - HALF_LINE;
const triangleHalf = (triangleFoot - triangleTop) / Math.sqrt(3);
export const triangleCorners: Array<[number, number]> = [[10, triangleTop], [10 + triangleHalf, triangleFoot], [10 - triangleHalf, triangleFoot]];
export const triangleMiddle: [number, number] = [10, (triangleTop + 2 * triangleFoot) / 3];
export const triangle = polygon(triangleCorners);
/**
 * Notion's triangle corner: its line's two outsides run on past the corner
 * until they meet a cut square across the corner, half a line out from it,
 * as far as a round corner would reach — so its foot reaches 1.70..18.30
 * of the 20 (its triangles and warning rastered at 400px, 2026-11-17).
 * Drawn as a fill beside the line, whose own corner — too narrow to stay
 * sharp (nativeIconGlyphInk.ts) — is cut nearer; one piece a corner.
 */
export const triangleCornerCuts = triangleCorners.map((corner, index) => {
  const unit = ([x, y]: [number, number]): [number, number] => { const length = Math.hypot(x, y); return [x / length, y / length]; };
  const toward = (point: [number, number]) => unit([point[0] - corner[0], point[1] - corner[1]]);
  const [u1, u2] = [toward(triangleCorners[(index + 2) % 3]!), toward(triangleCorners[(index + 1) % 3]!)];
  const out = unit([-(u1[0] + u2[0]), -(u1[1] + u2[1])]);
  // Each side's outward normal: square to it, away from the third corner.
  const normal = (along: [number, number], other: [number, number]): [number, number] => {
    const turned: [number, number] = [-along[1], along[0]];
    return turned[0] * other[0] + turned[1] * other[1] < 0 ? turned : [along[1], -along[0]];
  };
  const [n1, n2] = [normal(u1, u2), normal(u2, u1)];
  const dot = (a: [number, number], b: [number, number]) => a[0] * b[0] + a[1] * b[1];
  const edge = (n: [number, number], u: [number, number]): Array<[number, number]> => {
    const past = (HALF_LINE * (dot(n, out) - 1)) / dot(u, out);
    return [[corner[0] + HALF_LINE * n[0], corner[1] + HALF_LINE * n[1]], [corner[0] + HALF_LINE * n[0] - past * u[0], corner[1] + HALF_LINE * n[1] - past * u[1]]];
  };
  const [side1, cut1] = edge(n1, u1), [side2, cut2] = edge(n2, u2);
  return polygon([corner, side1!, cut1!, cut2!, side2!]);
}).join("");
export const PENTAGON_MIDDLE = 10.3125;
export const pentagonCorners = cornersOnEdge(5, 10 + ROUND_EDGE - PENTAGON_MIDDLE);
export const pentagonPoints = regular(5, pentagonCorners, 10, PENTAGON_MIDDLE);
export const pentagon = polygon(pentagonPoints);
export const hexagonCorners = cornersOnEdge(6, ROUND_EDGE);
export const hexagonPoints = regular(6, hexagonCorners);
export const hexagon = polygon(hexagonPoints);
export const octagon = polygon(regular(8, cornersOnEdge(8, ROUND_EDGE), 10, 10, -67.5));
/**
 * A dashed line's break: a straight cut across it two grid steps wide, its
 * ends flat (Notion's dashed square, triangle, pentagon, hexagon and circle
 * rastered at 400px, 2026-11-18 — 24 of 400 clear where the cut's two edge
 * pixels are half inked).
 */
export const DASH_BREAK = 1.25;
/**
 * A shape's line broken once in the middle of each side, its corners whole,
 * as Notion draws its dashed square, triangle, pentagon and hexagon: the line
 * starts just past one break and dashes a side less the break.
 */
export function dashedCorners(corners: Array<[number, number]>): NativeIconGlyph {
  const [last, first] = [corners[corners.length - 1]!, corners[0]!];
  const side = Math.hypot(first[0] - last[0], first[1] - last[1]), along = (side + DASH_BREAK) / 2 / side;
  const start: [number, number] = [last[0] + (first[0] - last[0]) * along, last[1] + (first[1] - last[1]) * along];
  const at = (value: number) => Number(value.toFixed(3));
  return { stroke: `M${[start, ...corners, start].map(([x, y]) => `${at(x)} ${at(y)}`).join("L")}`, dash: `${at(side - DASH_BREAK)} ${at(DASH_BREAK)}` };
}
/**
 * A ring on the round edge broken in eight, as Notion draws its dashed
 * circle and dashed person in a ring (rastered at 400px, 2026-11-18): a piece
 * across twelve o'clock and one every eighth of a turn, each break a
 * straight cut DASH_BREAK wide along the turn between two pieces, so it
 * clears a wider angle on the line's inside than on its outside. Drawn as
 * a fill, each piece its outside and inside arcs between two cuts.
 */
export const dashedRing = (() => {
  const outer = ROUND_EDGE, inner = ROUND_EDGE - NATIVE_ICON_LINE, pieces = 8;
  const at = (r: number, angle: number) => `${n(10 + r * Math.sin(angle))} ${n(10 - r * Math.cos(angle))}`;
  return Array.from({ length: pieces }, (_, index) => {
    const from = ((index - 0.5) * 2 * Math.PI) / pieces, to = ((index + 0.5) * 2 * Math.PI) / pieces;
    // A cut's edge reaches radius r this far round from the cut's middle.
    const clear = (r: number) => Math.asin(DASH_BREAK / 2 / r);
    return `M${at(outer, from + clear(outer))}A${outer} ${outer} 0 0 1 ${at(outer, to - clear(outer))}`
      + `L${at(inner, to - clear(inner))}A${n(inner)} ${n(inner)} 0 0 0 ${at(inner, from + clear(inner))}Z`;
  }).join("");
})();
/**
 * A diamond standing on its point (a git mark, a road sign), as Notion
 * cuts its (read as a path, 2026-11-18): each point cut flat on the full
 * edge, two half steps either side of the middle, its sides' outside
 * running at 45° between the cuts.
 */
export const diamond = (() => {
  const reach = FULL_EDGE - HALF_LINE, half = 2 * EDGE_STEP - HALF_LINE * (Math.SQRT2 - 1);
  return polygon([
    [10 + reach, 10 - half], [10 + reach, 10 + half], [10 + half, 10 + reach], [10 - half, 10 + reach],
    [10 - reach, 10 + half], [10 - reach, 10 - half], [10 - half, 10 - reach], [10 + half, 10 - reach],
  ]);
})();
export const starPoints = star(8, 3.6);
export const heart = "M10 16.5s-6.5-3.9-6.5-8.6A3.5 3.5 0 0 1 10 6a3.5 3.5 0 0 1 6.5 1.9c0 4.7-6.5 8.6-6.5 8.6Z";
/** A mark cut out of a filled disc, as Notion's circled icons are (seen
 *  2026-11-06: add, clear, checkmark, the arrows, help, info and the rest). */
export const inCircle = (stroke: string, fill?: string): NativeIconGlyph => ({ disc: true, stroke, fill });
/** A mark inside an outlined ring, for the round icons Notion draws otherwise. */
export const inRing = (stroke: string, fill?: string): NativeIconGlyph => ({ stroke: `${ring}${stroke}`, fill });

/** A sharp-cornered bar, for pixel art, bar codes and charts. */
export function bar(x: number, y: number, w: number, h: number) {
  return `M${n(x)} ${n(y)}h${n(w)}v${n(h)}h${n(-w)}Z`;
}

/*
 * Notion's marks — a plus, a minus, a cross, a check, alone, cut out of a
 * disc or a speech bubble, or beside a person (its plus, remove, close,
 * checkmark-line, add, circle-remove, clear, checkmark, comment, follow,
 * following and unfollow rastered at 400px, 2026-11-18): bars a line wide
 * on its grid, six half steps, their ends flat and their corners sharp.
 * Each is drawn as one outline.
 */
/** Half a mark's bar. */
export const MARK_HALF = 3 * EDGE_STEP;
/** A minus: a level bar `reach` each way from its middle. */
export const minus = (cx: number, cy: number, reach: number) => bar(cx - reach, cy - MARK_HALF, 2 * reach, 2 * MARK_HALF);
/** A plus's outline about nothing, `reach` each way: each arm the one before it turned a quarter clockwise. */
function plusPoints(reach: number): Array<[number, number]> {
  const arm: Array<[number, number]> = [[-MARK_HALF, -reach], [MARK_HALF, -reach], [MARK_HALF, -MARK_HALF]];
  const turn = ([x, y]: [number, number]): [number, number] => [-y, x];
  return [arm, arm.map(turn), arm.map(turn).map(turn), arm.map(turn).map(turn).map(turn)].flat();
}
/** A plus, `reach` each way from its middle. */
export function plus(cx: number, cy: number, reach: number) {
  return polygon(plusPoints(reach).map(([x, y]): [number, number] => [cx + x, cy + y]));
}
/** A cross: a plus turned an eighth, its ends' corners `tip` from its middle across and down. */
export function cross(cx: number, cy: number, tip: number) {
  return polygon(plusPoints(tip * Math.SQRT2 - MARK_HALF).map(([x, y]): [number, number] => [cx + (x - y) / Math.SQRT2, cy + (x + y) / Math.SQRT2]));
}
/**
 * A check: from its corner's middle at (x, y) a short arm up to the left
 * and a long one up to the right, both at 45°, each as far as brings its
 * flat end's corner to `left` and to `right`.
 */
export function check(x: number, y: number, left: number, right: number) {
  const c = MARK_HALF / Math.SQRT2, [short, long] = [x - left - c, right - c - x];
  return polygon([
    [x - short + c, y - short - c], [x, y - 2 * c], [x + long - c, y - long - c],
    [x + long + c, y - long + c], [x, y + 2 * c], [x - short - c, y - short + c],
  ]);
}
/**
 * The marks at the size Notion cuts out of a disc, a square or a speech
 * bubble and sets beside a person, about (cx, cy): a plus and a minus
 * thirteen half steps each way, a cross's corners twelve, and a check
 * fourteen to its left and fifteen to its right, its corner three left of
 * the middle and eight below it.
 */
export const smallPlus = (cx = 10, cy = 10) => plus(cx, cy, 13 * EDGE_STEP);
export const smallMinus = (cx = 10, cy = 10) => minus(cx, cy, 13 * EDGE_STEP);
export const smallCross = (cx = 10, cy = 10) => cross(cx, cy, 12 * EDGE_STEP);
export const smallCheck = (cx = 10, cy = 10) => check(cx - 3 * EDGE_STEP, cy + 8 * EDGE_STEP, cx - 14 * EDGE_STEP, cx + 15 * EDGE_STEP);

export function ellipse(cx: number, cy: number, rx: number, ry: number) {
  return `M${n(cx - rx)} ${n(cy)}a${n(rx)} ${n(ry)} 0 1 0 ${n(2 * rx)} 0a${n(rx)} ${n(ry)} 0 1 0 ${n(-2 * rx)} 0Z`;
}

/** A star or burst with the given number of points. */
export function burst(points: number, outer: number, inner: number, cx = 10, cy = 10) {
  return polygon(Array.from({ length: points * 2 }, (_, index): [number, number] => {
    const r = index % 2 === 0 ? outer : inner;
    const angle = ((-90 + (180 / points) * index) * Math.PI) / 180;
    return [cx + r * Math.cos(angle), cy + r * Math.sin(angle)];
  }));
}

/** Pixel art from rows of "X" (filled) and "." (empty) cells. */
export function pixels(rows: string[], cell: number, x0: number, y0: number) {
  return rows.flatMap((row, y) => [...row].flatMap((value, x) => (
    value === "X" ? [bar(x0 + x * cell, y0 + y * cell, cell, cell)] : []
  ))).join("");
}

/** A calendar's rim with its two rings, and its head filled across the top (a frame with filled content, as Notion's). */
export const calendarRim = `${rect(3, 4.25, 14, 12.75, 1.75)}M7 2.75v3M13 2.75v3`;
export const calendarHead = "M4.75 4.25h10.5A1.75 1.75 0 0 1 17 6v3H3V6a1.75 1.75 0 0 1 1.75-1.75Z";

/**
 * A lens of Notion's glasses and sunglasses (rastered at 400px, 2026-11-18):
 * about its middle `cx`, from the bar's foot `top` straight down to `side`,
 * then half an oval round to its foot, `rx` across and `ry` down.
 */
export const lens = (cx: number, top: number, side: number, rx: number, ry: number) =>
  `M${n(cx - rx)} ${n(top)}V${n(side)}A${n(rx)} ${n(ry)} 0 0 0 ${n(cx + rx)} ${n(side)}V${n(top)}Z`;
/**
 * Notion's sunglasses, `scale` of their own size with the bar's top left
 * corner at (x, y): a bar a line thick across 18.75, two lenses under it
 * 7.2 wide and 5 deep, and from each a glint cut, a slanting band from the
 * bar down to the lens's lower left — as the sunglasses and the face wearing
 * them are drawn (rastered at 400px, 2026-11-18).
 */
export function sunglassesShape(scale = 1, x = 0.625, y = 7.1875) {
  const at = (px: number, py: number): [number, number] => [x + (px - 0.625) * scale, y + (py - 7.1875) * scale];
  const foot = 9.0625, side = 10.3;
  const glass = (cx: number) => {
    const [left, top] = at(cx - 3.59, foot), [, sideY] = at(cx, side);
    const glint = [[cx + 0.295, foot], [cx + 1.495, foot], [cx - 0.069, 12.1875], [cx - 1.269, 12.1875]].map(([px, py]) => at(px!, py!));
    return `${lens(left + 3.59 * scale, top, sideY, 3.59 * scale, 3.76 * scale)}${polygon(glint)}`;
  };
  const [bx, by] = at(0.625, 7.1875);
  return `${bar(bx, by, 18.75 * scale, NATIVE_ICON_LINE * scale)}${glass(5.625)}${glass(14.375)}`;
}

// Notion's rain cloud (its rain and sleet, read 2026-11-18), filled: four round lobes — 1.875 at (15, 9.375), 2.5 at
// (12.82, 7.19), 3.75 at (8.75, 6.25) and 2.5 at (5.625, 8.75) — on a flat foot at 11.25 (the points where they meet worked
// out to three places).
export const rainCloud = "M5.625 11.25H15A1.875 1.875 0 0 0 15.298 7.524A2.5 2.5 0 0 0 12.195 4.769A3.75 3.75 0 0 0 5.001 6.329A2.5 2.5 0 0 0 5.625 11.25Z";
/**
 * A point on the slant from the box's foot left corner up to its top right (Notion's crayon and pencil lie along it, read
 * 2026-11-18): `along` it from that corner and `across` it, to the top left negative.
 */
export const slant = (along: number, across: number): [number, number] => [2.5 + (along + across) / Math.SQRT2, 17.5 + (across - along) / Math.SQRT2];
/** A piece of a strip along that slant, from `from` to `to` along it and `half` either side. */
export const slantPiece = (from: number, to: number, half: number) => polygon([slant(from, -half), slant(to, -half), slant(to, half), slant(from, half)]);
export const bell = "M5 13.75V9a5 5 0 0 1 10 0v4.75l1.5 1.5h-13ZM8.25 17.25h3.5";
/** A sun's eight rays, 1.5 long, their ends' outside on `edge` (a sun's on the full edge, brightness's on the wide edge). */
export const sunRays = (edge: number) => rays(8, edge - HALF_LINE - 1.5, edge - HALF_LINE);

/** A cog outline with square-ended teeth. */
export function cog(teeth: number, outer: number, inner: number, cx = 10, cy = 10) {
  const step = (2 * Math.PI) / teeth;
  const at = (r: number, angle: number): [number, number] => [cx + r * Math.sin(angle), cy - r * Math.cos(angle)];
  return polygon(Array.from({ length: teeth }, (_, index) => {
    const angle = index * step;
    return [at(inner, angle - step * 0.3), at(outer, angle - step * 0.17), at(outer, angle + step * 0.17), at(inner, angle + step * 0.3)];
  }).flat());
}

/** A regular polygon filled from its top vertex through `parts` of its sides. */
export function polygonPart(sides: number, parts: number, r: number, cx = 10, cy = 10) {
  return polygon([[cx, cy], ...regular(sides, r, cx, cy).slice(0, parts + 1)]);
}

/** A piece of a ring, clockwise from twelve o'clock between two angles in degrees. */
export function ringPiece(from: number, to: number, outer: number, inner: number, cx = 10, cy = 10) {
  const at = (r: number, degrees: number) => {
    const angle = (degrees * Math.PI) / 180;
    return `${n(cx + r * Math.sin(angle))} ${n(cy - r * Math.cos(angle))}`;
  };
  const large = to - from > 180 ? 1 : 0;
  return `M${at(outer, from)}A${outer} ${outer} 0 ${large} 1 ${at(outer, to)}L${at(inner, to)}A${inner} ${inner} 0 ${large} 0 ${at(inner, from)}Z`;
}

/** Straight rays round a point, clockwise from twelve o'clock, from `inner` out to `outer`. */
export function rays(count: number, inner: number, outer: number, cx = 10, cy = 10) {
  return Array.from({ length: count }, (_, index) => {
    const angle = (index * 2 * Math.PI) / count, x = Math.sin(angle), y = -Math.cos(angle);
    return `M${n(cx + inner * x)} ${n(cy + inner * y)}L${n(cx + outer * x)} ${n(cy + outer * y)}`;
  }).join("");
}

/**
 * A tone drawn as `count` stripes `width` wide and `gap` apart, rising to
 * the right inside a circle of radius `r`: a shading's lights, midtones and darks.
 */
export function hatch(count: number, width: number, gap: number, r = 5.75, cx = 10, cy = 10) {
  // Across a stripe runs (1, 1), along it (1, -1); the circle closes its ends.
  const at = (across: number, along: number): [number, number] => [cx + (across + along) / Math.SQRT2, cy + (across - along) / Math.SQRT2];
  const half = (across: number) => Math.sqrt(Math.max(0, r * r - across * across));
  const first = -((count - 1) * (width + gap)) / 2;
  return Array.from({ length: count }, (_, index) => {
    const middle = first + index * (width + gap);
    const from = Math.max(-r, middle - width / 2), to = Math.min(r, middle + width / 2);
    const steps = Array.from({ length: 7 }, (__, step) => from + ((to - from) * step) / 6);
    return polygon([...steps.map((across) => at(across, half(across))), ...[...steps].reverse().map((across) => at(across, -half(across)))]);
  }).join("");
}
