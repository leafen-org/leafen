// Leafen's own glyphs for built-in icons, drawn on a 20px grid inside a 16px
// live area: solid shapes as fills, line work as lines, painted as
// nativeIconDrawing.tsx says (outlines filled, Notion's line weight, flat
// ends and sharp corners). What
// Notion draws solid is a closed outline here (a flag, a bulb, a bottle), and
// a line that meets one — a pole, a base, a handle — starts half a line clear
// of it, as a line inside an outline is cut out of its fill. They
// express the same subject as the catalog name without copying or tracing
// any other icon set. Names without a glyph keep the neutral mark.

import {
  bar,
  slant,
  slantPiece,
  check,
  circle,
  cross,
  dashedCorners,
  dashedRing,
  disc,
  heart,
  hexagon,
  hexagonPoints,
  inCircle,
  minus,
  smallCheck,
  smallCross,
  smallMinus,
  smallPlus,
  octagon,
  pentagon,
  pentagonPoints,
  plus,
  polygon,
  rect,
  ring,
  ringSlash,
  sector,
  square,
  squareCorners,
  SQUARE_FAR,
  SQUARE_NEAR,
  FULL_EDGE,
  SQUARE_EDGE,
  EDGE_STEP,
  edgeIn,
  star,
  starPoints,
  triangle,
  triangleCornerCuts,
  triangleCorners,
  bell,
  calendarHead,
  calendarRim,
  sunRays,
  ROUND_EDGE,
  type NativeIconGlyph,
} from "./nativeIconGlyphShapes";
import { NECK, person } from "./nativeIconPerson";
import { chevron, doubleArrow, lineArrow } from "./nativeIconArrows";
import { back, curve, roundQuarters } from "./nativeIconRound";
import { NATIVE_ICON_GLYPHS_AB } from "./nativeIconGlyphsAB";
import { NATIVE_ICON_GLYPHS_C } from "./nativeIconGlyphsC";
import { NATIVE_ICON_GLYPHS_DEF } from "./nativeIconGlyphsDEF";
import { NATIVE_ICON_GLYPHS_FL } from "./nativeIconGlyphsFL";
import { NATIVE_ICON_GLYPHS_LP } from "./nativeIconGlyphsLP";
import { NATIVE_ICON_GLYPHS_PS } from "./nativeIconGlyphsPS";
import { NATIVE_ICON_GLYPHS_ST } from "./nativeIconGlyphsST";
import { NATIVE_ICON_GLYPHS_TZ } from "./nativeIconGlyphsTZ";

export type { NativeIconGlyph };

const partialCircles = Object.fromEntries(
  ["one-eighth", "two-eighths", "three-eighths", "four-eighths", "five-eighths", "six-eighths", "seven-eighths"]
    .map((part, index): [string, NativeIconGlyph] => [`circle-${part}`, { stroke: ring, fill: sector((index + 1) / 8) }]),
);

// A square and a disc across each other's corner, as Notion's square-circle
// (2026-11-17, its 885 icons rastered — the pair 16.625 across, off its
// grid's half steps): the square 10 on a side in the far corner, the disc
// in the near one without its quarter toward the square, cut through its
// middle two grid steps (1.25) clear of it.
const squareCircle = (() => {
  const reach = 8.3125, side = 10, gap = 4 * EDGE_STEP;
  const far = 10 + reach, near = 10 - reach, c = far - side - gap, r = c - near;
  return `M${far - side} ${far - side}H${far}V${far}H${far - side}ZM${c + r} ${c}A${r} ${r} 0 1 0 ${c} ${c + r}L${c} ${c}Z`;
})();

/** The square filled out to the square edge, which Notion cuts a check out of. */
const filledSquare = polygon([[10 - SQUARE_EDGE, 10 - SQUARE_EDGE], [10 + SQUARE_EDGE, 10 - SQUARE_EDGE], [10 + SQUARE_EDGE, 10 + SQUARE_EDGE], [10 - SQUARE_EDGE, 10 + SQUARE_EDGE]]);
// A square's parts fill by quarters clockwise from twelve o'clock, as a circle's do, to its line's middle.
const near = SQUARE_NEAR, far = SQUARE_FAR;
const squareQuarters = [
  polygon([[10, near], [far, near], [far, 10], [10, 10]]),
  polygon([[10, near], [far, near], [far, far], [10, far]]),
  polygon([[10, near], [far, near], [far, far], [near, far], [near, 10], [10, 10]]),
];

const partialSquares = Object.fromEntries(
  ["one-fourth", "two-fourths", "three-fourths"].map((part, index): [string, NativeIconGlyph] =>
    [`square-${part}`, { stroke: square, fill: squareQuarters[index] }]),
);

// Notion's exclamation mark (its exclamation-mark and -double, read 2026-11-18): a bar 3.75 wide from the round edge's top
// down to 8.75, narrowing to half its width at 12.5, and under it a block dot from 14.375 to the round edge's foot; `x` its left.
const exclamation = (x: number) => `${polygon([[x, 2.5], [x + 3.75, 2.5], [x + 3.75, 8.75], [x + 2.8125, 12.5], [x + 0.9375, 12.5], [x, 8.75]])}${bar(x, 14.375, 3.75, 3.125)}`;
const userMark = person(10, 10 - ROUND_EDGE, (2 * ROUND_EDGE - NECK) / 4);

export const NATIVE_ICON_GLYPHS: Readonly<Record<string, NativeIconGlyph>> = {
  ...NATIVE_ICON_GLYPHS_AB,
  ...NATIVE_ICON_GLYPHS_C,
  ...NATIVE_ICON_GLYPHS_DEF,
  ...NATIVE_ICON_GLYPHS_FL,
  ...NATIVE_ICON_GLYPHS_LP,
  ...NATIVE_ICON_GLYPHS_PS,
  ...NATIVE_ICON_GLYPHS_ST,
  ...NATIVE_ICON_GLYPHS_TZ,
  ...partialCircles,
  ...partialSquares,
  circle: { stroke: ring },
  "circle-alternate": { fill: disc },
  "circle-dashed": { fill: dashedRing },
  // A thick ring: the disc with a hole ten half steps round cut out of its middle, as Notion's (rastered at 400px, 2026-11-18).
  "circle-dot": { fill: `${disc}${circle(10, 10, 10 * EDGE_STEP)}` },
  // A filled shape is its outline drawn solid: the same edge.
  square: { stroke: square },
  "square-alternate": { stroke: square },
  "square-dashed": dashedCorners(squareCorners),
  "square-circle": { fill: squareCircle },
  triangle: { stroke: triangle, fill: triangleCornerCuts },
  "triangle-alternate": { stroke: triangle, fill: triangleCornerCuts },
  "triangle-dashed": { ...dashedCorners(triangleCorners), fill: triangleCornerCuts },
  pentagon: { stroke: pentagon },
  "pentagon-alternate": { stroke: pentagon },
  "pentagon-dashed": dashedCorners(pentagonPoints),
  hexagon: { stroke: hexagon },
  "hexagon-alternate": { stroke: hexagon },
  "hexagon-dashed": dashedCorners(hexagonPoints),
  octagon: { stroke: octagon },
  // The marks alone (nativeIconGlyphShapes.ts): a plus and a minus out to the square edge, a cross's corners twelve
  // half steps in from the box's sides, a check's on the round edge; and the smaller ones cut out of a disc.
  plus: { fill: plus(10, 10, SQUARE_EDGE) },
  remove: { fill: minus(10, 10, SQUARE_EDGE) },
  close: { fill: cross(10, 10, edgeIn(12)) },
  add: { fill: `${disc}${smallPlus()}` },
  "circle-remove": { fill: `${disc}${smallMinus()}` },
  clear: { fill: `${disc}${smallCross()}` },
  no: inCircle(ringSlash),
  "checkmark-line": { fill: check(8.125, 13.75, 10 - ROUND_EDGE, 10 + ROUND_EDGE) },
  checkmark: { fill: `${disc}${smallCheck()}` },
  "checkmark-square": { fill: `${filledSquare}${smallCheck()}` },
  "arrow-up-basic": { stroke: lineArrow(0) },
  "arrow-down-basic": { stroke: lineArrow(2) },
  "arrow-left-basic": { stroke: lineArrow(3) },
  "arrow-right-basic": { stroke: lineArrow(1) },
  "arrow-up": inCircle("M10 13.5v-7M7 9.5l3-3 3 3"),
  "arrow-down": inCircle("M10 6.5v7M7 10.5l3 3 3-3"),
  "arrow-left": inCircle("M13.5 10h-7M9.5 7l-3 3 3 3"),
  "arrow-right": inCircle("M6.5 10h7M10.5 7l3 3-3 3"),
  "arrow-northeast": { stroke: "M5 15 15 5M7.5 5H15v7.5" },
  "arrow-northwest": { stroke: "M15 15 5 5M12.5 5H5v7.5" },
  "arrow-southeast": { stroke: "M5 5l10 10M15 7.5V15H7.5" },
  "arrow-southwest": { stroke: "M15 5 5 15M5 7.5V15h7.5" },
  "arrows-horizontal": { stroke: doubleArrow(1) },
  "arrows-vertical": { stroke: doubleArrow(0) },
  "chevrons-vertical": { stroke: `${chevron(2.5, 5)}${chevron(2.5, 5, 2)}` },
  "priority-high": { stroke: "M5 10.5l5-5 5 5M5 15.5l5-5 5 5" },
  "priority-low": { stroke: "M5 4.5l5 5 5-5M5 9.5l5 5 5-5" },
  "priority-mid": { stroke: "M5 7.75h10M5 12.25h10" },
  more: { fill: `${circle(4, 10, 1.6)}${circle(10, 10, 1.6)}${circle(16, 10, 1.6)}` },
  hashtag: { stroke: "M8.2 3.5 6.8 16.5M13.2 3.5l-1.4 13M4 7.5h13M3 12.5h13" },
  asterisk: { stroke: "M10 3.5v13M4.4 6.75l11.2 6.5M15.6 6.75l-11.2 6.5" },
  infinity: { stroke: "M10 10C8.6 8.2 7.3 7 5.75 7a3 3 0 0 0 0 6c1.55 0 2.85-1.2 4.25-3s2.7-3 4.25-3a3 3 0 0 1 0 6c-1.55 0-2.85-1.2-4.25-3Z" },
  "star-outline": { stroke: polygon(starPoints) },
  star: { fill: polygon(star(8.6, 3.9)) },
  "star-half": { stroke: polygon(starPoints), fill: polygon([...starPoints.slice(5), starPoints[0]]) },
  // Notion's outlined heart (read 2026-11-18), filled: a heart 15.62 across from 3.125 to a point cut 1.25 across at the round
  // edge's foot, its lobes Notion's round, and inside it a like heart from 5 to 15.65 cut out, its notch at 7.97.
  "heart-outline": {
    fill: `M10 5.38Q8.79 3.125 6.25 3.125${curve(back(roundQuarters(6.25, 7.5, 4.06, 4.375)[3]))}Q2.19 12.5 9.375 17.5H10.625Q17.81 12.5 17.81 7.5${curve(back(roundQuarters(13.75, 7.5, 4.06, 4.375)[0]))}Q11.21 3.125 10 5.38Z`
      + `M10 15.65Q4.06 11.48 4.06 7.5${curve(roundQuarters(6.25, 7.5, 2.19, 2.5)[3])}Q8.2 5 10 7.97Q11.8 5 13.75 5${curve(roundQuarters(13.75, 7.5, 2.19, 2.5)[0])}Q15.94 11.48 10 15.65Z`,
  },
  heart: { fill: heart },
  "question-mark": {
    stroke: "M7 7.25a3 3 0 1 1 4.3 2.7c-.8.4-1.3 1-1.3 1.9v.9",
    fill: circle(10, 15.75, 1.05),
  },
  "exclamation-mark": { fill: exclamation(8.125) },
  "exclamation-mark-double": { fill: `${exclamation(5)}${exclamation(11.25)}` },
  "help-alternate": inCircle("M8.1 8.2a1.9 1.9 0 1 1 2.7 1.7c-.5.25-.8.65-.8 1.2v.35", circle(10, 13.7, 0.85)),
  "info-alternate": inCircle("M10 9.25v4.5", circle(10, 6.6, 0.85)),
  report: inCircle("M10 6.25v4.5", circle(10, 13.5, 0.85)),
  warning: { stroke: `${triangle}M10 7.75v3.75`, fill: `${circle(10, 13.9, 0.85)}${triangleCornerCuts}` },
  error: { stroke: `${octagon}M10 6.5v4.25`, fill: circle(10, 13.5, 0.85) },
  sun: {
    fill: circle(10, 10, 3.5),
    stroke: sunRays(FULL_EDGE),
  },
  moon: { fill: "M16 12.6A6.75 6.75 0 0 1 7.4 4a6.75 6.75 0 1 0 8.6 8.6Z" },
  "bookmark-outline": { stroke: "M6 3.25h8a.75.75 0 0 1 .75.75v12.5L10 13.25 5.25 16.5V4A.75.75 0 0 1 6 3.25Z" },
  bookmark: { fill: "M5.5 2.5h9a1 1 0 0 1 1 1v14L10 13.75 4.5 17.5v-14a1 1 0 0 1 1-1Z" },
  flag: { stroke: "M5 17V3.5M5.95 4h8.55l-2.25 3.75 2.25 3.75H5.95Z" },
  home: { stroke: "M3.5 9.25 10 3.5l6.5 5.75v7.25h-4.25V12h-4.5v4.5H3.5Z" },
  // A lens's handle starts on its ring's line (4.75 out at 45°), so it ends there, inside the ring.
  search: { stroke: `${circle(8.5, 8.5, 4.75)}M11.86 11.86L16.5 16.5` },
  "zoom-in": { stroke: `${circle(8.5, 8.5, 4.75)}M11.86 11.86L16.5 16.5M8.5 6.5v4M6.5 8.5h4` },
  "zoom-out": { stroke: `${circle(8.5, 8.5, 4.75)}M11.86 11.86L16.5 16.5M6.5 8.5h4` },
  lock: { fill: rect(4, 8.5, 12, 9, 2), stroke: "M6.75 8.5V6.25a3.25 3.25 0 0 1 6.5 0V8.5" },
  unlock: { fill: rect(4, 8.5, 12, 9, 2), stroke: "M6.75 8.5V6.25a3.25 3.25 0 0 1 6.4-.8" },
  "clock-alternate": inCircle("M10 5.75V10l2.75 1.75"),
  document: { stroke: "M5.5 2.75h5.75L14.5 6v10.5a.75.75 0 0 1-.75.75H5.5a.75.75 0 0 1-.75-.75v-13a.75.75 0 0 1 .75-.75ZM11 2.75V6.5h3.5" },
  folder: { stroke: "M2.75 5.5a1 1 0 0 1 1-1h3.75l1.75 2h7a1 1 0 0 1 1 1v7.75a1 1 0 0 1-1 1H3.75a1 1 0 0 1-1-1Z" },
  mail: { stroke: `${rect(2.75, 4.5, 14.5, 11, 1.5)}M3.5 5.5 10 10.75 16.5 5.5` },
  inbox: {
    stroke: "M3.6 11.5 4.75 4.25h10.5l1.15 7.25",
    fill: "M2.75 11.25H8a2 2 0 0 0 4 0h5.25v4a1.25 1.25 0 0 1-1.25 1.25H4a1.25 1.25 0 0 1-1.25-1.25Z",
  },
  // Notion's pencil (read 2026-11-18), filled, along the slant: 3.125 across, its point cut 0.62 across at 0.31, sharpening
  // from 3.33, its body to 15.34 and, 1.25 on, its eraser to 19.65.
  pencil: { fill: `${polygon([slant(15.34, -1.5625), slant(15.34, 1.5625), slant(3.33, 1.5625), slant(0.31, 0.31), slant(0.31, -0.31), slant(3.33, -1.5625)])}${slantPiece(16.59, 19.65, 1.5625)}` },
  delete: { stroke: "M3.5 5h13M8 5V3.5h4V5M5.25 6.95h9.5l-.75 9.55H6ZM8.5 9.5v4M11.5 9.5v4" },
  globe: { stroke: `${ring}M3.45 10h13.1M10 3.45c-2.06 1.87-2.81 4.12-2.81 6.55s.75 4.68 2.81 6.55M10 3.45c2.06 1.87 2.81 4.12 2.81 6.55s-.75 4.68-2.81 6.55` },
  // A person drawn solid, head over shoulders, as Notion's person mark is
  // (seen 2026-11-15: a task tracker's "내 작업" tab): Notion's person as
  // tall as the round edge, head and shoulders (twice the head) sharing the
  // 15 less a neck (nativeIconPerson.ts).
  user: { fill: `${userMark.head}${userMark.outline}` },
  list: {
    fill: `${circle(4.5, 5.5, 1.1)}${circle(4.5, 10, 1.1)}${circle(4.5, 14.5, 1.1)}`,
    stroke: "M8 5.5h8.5M8 10h8.5M8 14.5h8.5",
  },
  grid: { fill: `${rect(3, 3, 6.25, 6.25, 1.5)}${rect(10.75, 3, 6.25, 6.25, 1.5)}${rect(3, 10.75, 6.25, 6.25, 1.5)}${rect(10.75, 10.75, 6.25, 6.25, 1.5)}` },
  table: { stroke: `${rect(2.75, 3.75, 14.5, 12.5, 1.5)}M2.75 8h14.5M2.75 12.1h14.5M8 3.75v12.5` },
  code: { stroke: "M6.5 5.5 2.75 10l3.75 4.5M13.5 5.5l3.75 4.5-3.75 4.5M11.25 4.5l-2.5 11" },
  flash: { fill: "M11.5 2 4.5 11.25h4.75L8.25 18l7.25-9.5h-4.8Z" },
  "light-bulb": { stroke: "M7.5 14.25c0-1.75-2.75-3-2.75-6.25a5.25 5.25 0 0 1 10.5 0c0 3.25-2.75 4.5-2.75 6.25ZM8.25 17h3.5M8.75 9.25 10 10.5l1.25-1.25M10 10.5v1.75" },
  bell: { stroke: bell },
  calendar: { stroke: calendarRim, fill: calendarHead },
  // Notion's tag (read 2026-11-18), filled: a square 7.5 on a side at the top left of 3.125..18.125, its far corner cut off
  // at 45° to a point, and a hole 1.875 across 3.4375 in from its corner.
  tag: { fill: `${polygon([[3.125, 3.125], [10.625, 3.125], [18.125, 10.625], [10.625, 18.125], [3.125, 10.625]])}${circle(6.5625, 6.5625, 0.9375)}` },
  token: inCircle("", polygon(star(4.4, 1.9, 10, 10.25))),
  // Notion's pizza (read 2026-11-18), filled: a slice pointing to the foot left, its point cut square, its crust round 16.3
  // about that corner with a round end either side, and three slices of topping 2.5 across cut out of it.
  pizza: {
    fill: "M5.93 4.72L1.88 16.88L3.13 18.13L15.28 14.07A1.6 1.6 0 0 0 18.13 12.84A16.3 16.3 0 0 0 7.16 1.88A1.6 1.6 0 0 0 5.93 4.72Z"
      + `${circle(5.94, 14.06, 1.25)}${circle(8.12, 8.13, 1.25)}${circle(10.62, 12.5, 1.25)}`,
  },
  robot: {
    stroke: `${rect(3.75, 6.5, 12.5, 9.5, 2.5)}M10 6.5V4M7.5 13h5`,
    fill: `${circle(10, 3.25, 1.1)}${circle(7.5, 10, 1.15)}${circle(12.5, 10, 1.15)}`,
  },
  rocket: { stroke: `${rect(9.05, 2.2, 1.9, 11.75, 0.95)}M8.6 8.9l-2.65 2.65v2.5l2.65-2.15ZM11.4 8.9l2.65 2.65v2.5l-2.65-2.15Z`, fill: "M10 19.35c-.7-.95-1.25-1.6-1.25-2.25a1.25 1.25 0 0 1 2.5 0c0 .65-.55 1.3-1.25 2.25Z" },
};
