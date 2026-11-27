import {
  EDGE_STEP,
  slant,
  slantPiece,
  FULL_EDGE,
  HALF_LINE,
  ROUND_EDGE,
  SQUARE_EDGE,
  bar,
  burst,
  circle,
  diamond,
  ellipse,
  hatch,
  heart,
  inCircle,
  n,
  polygon,
  rect,
  ring,
  square,
  star,
  sunglassesShape,
  type NativeIconGlyph,
} from "./nativeIconGlyphShapes";
import { blockArrow } from "./nativeIconArrows";
import { FRAME_LINE, PLATE_CORNER, bubble } from "./nativeIconFrames";
import { at, back, curve, enters, plate, roundQuarters, split, type Cubic } from "./nativeIconRound";

// Leafen glyphs for catalog names from "connecting-flight" through
// "flag-pennant".

// A question mark cut out of a speech bubble (Notion's dialogue rastered at
// 400px, 2026-11-18): a hook a line wide, Notion's round about (10, 8.125) —
// from its flat end at the left, half a step above its middle, over the top
// and down its right, where it bends in to an upright stem as wide that
// ends flat — and under it a dot four grid steps wide and a line tall.
const questionMark = (() => {
  const middle = 8.125, [wide, narrow] = [11 * EDGE_STEP, 5 * EDGE_STEP];
  const outer = roundQuarters(10, middle, wide), inner = roundQuarters(10, middle, narrow);
  const fromEnd = (quarter: Cubic) => split(quarter, enters(quarter, ([, y]) => y <= middle - EDGE_STEP))[1];
  const [outerUp, innerUp] = [fromEnd(outer[3]), fromEnd(inner[3])];
  const [stemLeft, stemRight, foot] = [10 - FRAME_LINE / 2, 10 + FRAME_LINE / 2, 10.9375];
  return `M${at(outerUp[0])}${curve(outerUp)}${curve(outer[0])}C${10 + wide} 9.6 ${stemRight} 9.9 ${stemRight} ${foot}H${stemLeft}`
    + `C${stemLeft} 8.9 ${10 + narrow} 8.3 ${10 + narrow} ${middle}${curve(back(inner[0]))}${curve(back(innerUp))}Z${bar(8.75, 12.1875, 2.5, FRAME_LINE)}`;
})();

/** A bust cut out of a coin of radius `r` (with the fill's even-odd rule): a head and the shoulders below it. */
const coinBust = (cx: number, cy: number, r: number) => {
  const head = r * 0.33, neck = cy + r * 0.2, base = cy + r * 0.7, wide = r * 0.56;
  return `${circle(cx, cy - r * 0.25, head)}M${n(cx - wide)} ${n(base)}C${n(cx - wide * 0.9)} ${n(neck + r * 0.3)} ${n(cx - wide * 0.5)} ${n(neck)} ${n(cx)} ${n(neck)}`
    + `C${n(cx + wide * 0.5)} ${n(neck)} ${n(cx + wide * 0.9)} ${n(neck + r * 0.3)} ${n(cx + wide)} ${n(base)}Z`;
};

/**
 * A mirror ball's seams cut out of its disc (with the fill's even-odd rule):
 * two rows across, and between them the meridians, bowed out in the middle
 * row. No two seams overlap — the meridians stop at the rows.
 */
function mirrorBallSeams(cx: number, cy: number, r: number) {
  const seam = r * 0.16, half = seam / 2, row = r * 0.36;
  const across = (dy: number) => Math.sqrt(r * r - dy * dy);
  const rows = [-row, row].map((dy) => {
    const top = dy - half, bottom = dy + half;
    return polygon([[cx - across(top), cy + top], [cx + across(top), cy + top], [cx + across(bottom), cy + bottom], [cx - across(bottom), cy + bottom]]);
  }).join("");
  type End = number | "rim";
  const meridian = (x: number, top: End, bottom: End) => {
    const at = (side: number, y: End, sign: number) => (y === "rim" ? cy + sign * across(x + side) : cy + y);
    return polygon([[cx + x - half, at(-half, top, -1)], [cx + x + half, at(half, top, -1)], [cx + x + half, at(half, bottom, 1)], [cx + x - half, at(-half, bottom, 1)]]);
  };
  const inner = row - half, outer = row + half, bow = r * 0.58, cap = r * 0.42;
  return rows
    + [-bow, 0, bow].map((x) => meridian(x, -inner, inner)).join("")
    + [-cap, 0, cap].map((x) => `${meridian(x, "rim", -outer)}${meridian(x, outer, "rim")}`).join("");
}

// A die is the square drawn solid with its pips cut out, half way from its middle to its line.
const pip = (SQUARE_EDGE - HALF_LINE) / 2;
const pips: Record<string, [number, number]> = {
  c: [10, 10], tl: [10 - pip, 10 - pip], tr: [10 + pip, 10 - pip], ml: [10 - pip, 10], mr: [10 + pip, 10], bl: [10 - pip, 10 + pip], br: [10 + pip, 10 + pip],
};
const die = (...spots: string[]): NativeIconGlyph => ({
  stroke: square,
  fill: spots.map((spot) => circle(pips[spot][0], pips[spot][1], 1.1)).join(""),
});
/** Where a crop mark ends for its outside to reach the full edge. */
const cropEnd = n(10 - FULL_EDGE + HALF_LINE);
// Notion's face (its 13 emoji faces, read 2026-11-18): eyes 1.875 wide and 3.125 tall with round ends, a quarter of the round
// edge out from the middle; its smile a band 1.25 wide bending down from 12.5 at its ends to 14.69 under the middle, its ends
// cut square to it; the sad mouth the smile turned over, and the neutral one a bar 6.875 by 1.25 — each cut out of the face.
const eyes = `${rect(6.5625, 7.1875, 1.875, 3.125, 0.9375)}${rect(11.5625, 7.1875, 1.875, 3.125, 0.9375)}`;
const smile = "M5.94 12.5Q10 16.88 14.06 12.5L13.02 11.81Q10 15.07 6.98 11.81Z";
const sadMouth = "M5.94 13.75Q10 9.37 14.06 13.75L13.02 14.44Q10 11.18 6.98 14.44Z";
const smilingEyes = "M6.25 9a1.25 1.25 0 0 1 2.5 0M11.25 9a1.25 1.25 0 0 1 2.5 0";
const grin = "M6.25 11h7.5a3.75 3.75 0 0 1-7.5 0Z";
const face = (stroke: string, fill?: string): NativeIconGlyph => ({ stroke: `${ring}${stroke}`, fill });
const smallHeart = (cx: number, cy: number) =>
  `M${cx} ${cy + 1.4}s-1.9-1.15-1.9-2.5a1 1 0 0 1 1.9-.55 1 1 0 0 1 1.9.55c0 1.35-1.9 2.5-1.9 2.5Z`;
// Notion's one-way signs (read 2026-11-18): a post a line wide from the round edge's top to its foot, and across it a board
// filled from 5 to 12.5, pointing to its side 3.75 out to the box's edge (0.625 or 18.75), its other end square.
const post = `M10 ${2.5 + HALF_LINE}V${17.5 - HALF_LINE}`;
const fireworkRays = Array.from({ length: 8 }, (_, index) => {
  const angle = (index * Math.PI) / 4;
  const [x1, y1] = [10 + 2.75 * Math.cos(angle), 10 + 2.75 * Math.sin(angle)];
  const [x2, y2] = [10 + 6.25 * Math.cos(angle), 10 + 6.25 * Math.sin(angle)];
  return `M${x1.toFixed(2)} ${y1.toFixed(2)}L${x2.toFixed(2)} ${y2.toFixed(2)}`;
}).join("");
const checkered = [[0, 0], [2, 0], [1, 1], [3, 1], [0, 2], [2, 2]]
  .map(([column, row]) => bar(4.5 + column * 2.75, 3.75 + row * (8 / 3), 2.75, 8 / 3)).join("");

export const NATIVE_ICON_GLYPHS_DEF: Readonly<Record<string, NativeIconGlyph>> = {
  "connecting-flight": {
    fill: "M10 2.75c.45 0 .7.5.7 1.1v1.8l3.05 1.8v1l-3.05-.85v1.95l1 .7v.85L10 10.7l-1.7.45v-.85l1-.7V7.65l-3.05.85v-1l3.05-1.8v-1.8c0-.6.25-1.1.7-1.1Z",
    stroke: "M4 13a6.25 4 0 0 0 11.5 1M15.75 11.75l-.25 2.5-2.5-.5",
  },
  "construction-crane": { stroke: "M5 17V3.5M5 3.5h11M5 3.5 3 6M5 6h11M14.5 6v4.5M13 10.5h3V12h-3ZM3 17h4M5 3.5 7.5 6M8.5 3.5 11 6M12 3.5 14.5 6" },
  contrast: { stroke: ring, fill: "M5.05 5.05A7 7 0 0 0 14.95 14.95Z" },
  conversation: { stroke: "M3.4 11.36A3.95 4 0 1 0 1.6 9.6L.95 12.65ZM18.41 13.83A4.75 4.45 0 1 0 15.92 15.78L19.05 16.55Z" },
  copy: { stroke: `${rect(6.5, 6.5, 10, 10.5, 1.5)}M13.5 6.5v-2A1.5 1.5 0 0 0 12 3H5a1.5 1.5 0 0 0-1.5 1.5V12A1.5 1.5 0 0 0 5 13.5h1.5` },
  // A solid cob with kernels cut out of it, in two solid husk leaves.
  corn: {
    fill: "M10 2.25c1.75 0 2.75 2.5 2.75 5.6 0 2.9-1.1 5-2.75 5-1.65 0-2.75-2.1-2.75-5 0-3.1 1-5.6 2.75-5.6Z"
      + `${rect(9.35, 5.25, 1.1, 1.1, 0.2)}${rect(10.35, 7, 1.1, 1.1, 0.2)}${rect(9.15, 8.75, 1.1, 1.1, 0.2)}`
      + "M10 17.75C6.75 15.75 4.5 12.5 4.75 8.5c1.75 1 3.25 2.75 4.25 5.25ZM10 17.75c3.25-2 5.5-5.25 5.25-9.25-1.75 1-3.25 2.75-4.25 5.25Z",
  },
  couch: { stroke: "M4.5 9V6.5A1.5 1.5 0 0 1 6 5h8a1.5 1.5 0 0 1 1.5 1.5V9M2.75 10.5a1.5 1.5 0 0 1 3 0V12h8.5v-1.5a1.5 1.5 0 0 1 3 0V15H2.75ZM4.5 15v1.75M15.5 15v1.75" },
  // Notion's cow (read 2026-11-18), filled, in profile: its head and horns at the top right, its back level at 5 to its tail at
  // the left, its belly and four legs to 16.875.
  cow: { fill: `M19.375 7.81L18.91 8.75H15.94L14.72 11.18Q14.4 11.875 13.6 11.875H13.44V15.625L14.06 16.875H11.875L11.25 15.625V11.875H10.625L9.52 12.24Q8.8 12.5 7.94 12.5H6.56L5.78 14.06L7.19 16.875H5L3.75 14.375V10L2.5 8.75V12.5H1.25V7.5${curve(roundQuarters(3.75, 7.5, 2.5)[3])}H14.375L15 4.375V3.125H15.94Q16.56 3.125 16.56 3.75V4.375H16.875Q17.7 4.375 18 5.06Z` },
  crab: { stroke: `${ellipse(10, 11.5, 4.5, 3)}M5.5 10 3.5 7.5l.5-2.5M3.5 7.5h2M14.5 10l2-2.5-.5-2.5M16.5 7.5h-2M5.75 13 3 14.5M6.5 14.25 4.5 16.5M14.25 13 17 14.5M13.5 14.25l2 2.25M8.5 8.5V7M11.5 8.5V7` },
  // Notion's crayon (read 2026-11-18), filled, along the slant: 5 across in three pieces 1.25 apart — its body 7.8..15.18, its
  // cap 16.42..18.72, and its point's band 4.26..6.55 sharpening from 3.9 across to a tip cut 0.62 across at 0.31.
  crayon: {
    fill: `${slantPiece(7.8, 15.18, 2.5)}${slantPiece(16.42, 18.72, 2.5)}`
      + polygon([slant(6.55, -2.5), slant(6.55, 2.5), slant(4.26, 2.5), slant(4.27, 1.95), slant(0.31, 0.31), slant(0.31, -0.31), slant(4.27, -1.95), slant(4.26, -2.5)]),
  },
  create: { stroke: "M9 3.5H5A1.5 1.5 0 0 0 3.5 5v10A1.5 1.5 0 0 0 5 16.5h10a1.5 1.5 0 0 0 1.5-1.5v-4M16.75 3.25 11.5 8.5M11.5 8.5c-1.5-.25-2.75.75-2.75 2.25 0 1-.5 1.75-1.25 2 2.5 1 4.75-.25 5-2.25" },
  // Notion's credit card (read 2026-11-18), filled: a plate 15 by 11.25 with Notion's round corners, and cut out of it a chip
  // 3.125 by 1.875 and four dashes 1.875 by 1.25, 1.25 apart.
  "credit-card": { fill: `${plate(2.5, 4.375, 17.5, 15.625, PLATE_CORNER)}${bar(4.375, 7.5, 3.125, 1.875)}${[4.375, 7.5, 10.625, 13.75].map((x) => bar(x, 11.25, 1.875, 1.25)).join("")}` },
  crop: { stroke: `M5.5 ${cropEnd}V14.5H${20 - cropEnd}M${cropEnd} 5.5H14.5V${20 - cropEnd}` },
  // Notion's crutch (read 2026-11-18), filled: lying at 45° from its armrest at the top right to its foot at the foot left, its
  // frame a line wide with the grip's two openings cut out.
  crutch: {
    fill: "M15.44 4.56Q13.75 2.9 13.75 1.875H12.5Q12.5 2.75 12.87 3.43L7.17 9.14A3.1 3.1 0 0 0 6.25 11.35V12.42L3.99 14.68H3.64Q3.1 14.68 2.76 15.05L2.19 15.625L4.375 17.81L4.95 17.24Q5.31 16.85 5.31 16.35V16.01L7.58 13.75H8.65Q10.1 13.75 10.86 12.83L16.56 7.13A3.24 3.24 0 0 0 18.125 7.5V6.25Q17.1 6.25 15.44 4.56Z"
      + "M9.97 11.95Q9.4 12.5 8.65 12.5H7.5V11.35Q7.5 10.6 8.05 10.03L8.9 9.17L10.83 11.1ZM11.71 10.21L9.79 8.28L13.61 4.46Q14.6 5.5 15.54 6.39Z",
  },
  cupcake: { stroke: "M4.5 10h11L14 17H6ZM4.5 10a5.5 4.5 0 0 1 11 0M8 10.5l.5 6.5M12 10.5l-.5 6.5", fill: circle(10, 4.25, 1.1) },
  // A bank note, a coin before its corner.
  // A note behind a coin with a bust cut out of it.
  currency: { fill: `M4.25 3.5H14.5A1.5 1.5 0 0 1 16 5v3.79A5 5 0 0 0 9.07 11.5H4.25a1.5 1.5 0 0 1-1.5-1.5V5a1.5 1.5 0 0 1 1.5-1.5Z${circle(8.25, 7.5, 1.75)}${circle(13.75, 13.25, 3.75)}${coinBust(13.75, 13.25, 3.75)}` },
  // Two coins: one behind with a star cut out of it, bitten by the gap round the one in front, which has a bust.
  "currency-coin": {
    fill: `M11.59 6.97A4.6 4.6 0 1 0 7.14 11.85A5.65 5.65 0 0 1 11.59 6.97Z${polygon(star(2.3, 1, 6.3, 6.6))}`
      + `${circle(12.75, 12.5, 4.75)}${coinBust(12.75, 12.5, 4.75)}`,
  },
  cursor: { fill: polygon([[5, 2.75], [15.25, 11.25], [10.75, 11.75], [13.25, 16.5], [11.25, 17.5], [8.75, 12.75], [5, 15.75]]) },
  "cursor-button": {
    stroke: "M4.75 4h10.5a2 2 0 0 1 2 2v1.75a2 2 0 0 1-2 2H4.75a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z",
    fill: polygon([[10.5, 11.25], [15.19, 15], [13.13, 15.26], [14.25, 17.36], [13.31, 17.81], [12.19, 15.79], [10.5, 17.14]]),
  },
  // Notion's click (read 2026-11-18), filled: an arrow pointer from (9.38, 9.34) down to its tail at the round edge's foot, and
  // five rays 1.25 wide round its point — left, up, right and the two upper diagonals and the lower left.
  "cursor-click": {
    fill: `${bar(1.875, 9.375, 4.375, 1.25)}${bar(9.375, 1.875, 1.25, 4.375)}${bar(13.75, 9.375, 4.375, 1.25)}`
      + `${polygon([[16.19, 4.7], [15.3, 3.81], [12.21, 6.91], [13.09, 7.79]])}${polygon([[3.81, 15.3], [4.7, 16.19], [7.79, 13.09], [6.91, 12.21]])}${polygon([[3.81, 4.7], [6.91, 7.79], [7.79, 6.91], [4.7, 3.81]])}`
      + polygon([[9.38, 9.34], [9.38, 16.36], [9.81, 16.8], [11.37, 15.25], [12.69, 18.44], [14.13, 17.84], [12.81, 14.65], [15, 14.65], [15, 14.02], [10.04, 9.06]]),
  },

  customs: { stroke: `M2.75 12h14.5v5H2.75Z${circle(10, 5.75, 2)}M6.5 12c.5-2 1.75-3 3.5-3s3 1 3.5 3` },
  // Notion's scissors (read 2026-11-18), filled: two blades crossing from the round edge's top down to two finger rings, each a
  // round 4.4 out with a hole cut in it, the front blade whole and the back one parted from it.
  cut: {
    fill: "M11.67 9.375L10.7 7.44L13.17 2.5H13.44A1.56 1.56 0 0 1 14.83 4.76L12.53 9.375Z"
      + "M16.25 15.02A2.48 2.48 0 0 1 11.55 16.13L10 13.03L8.45 16.13A2.48 2.48 0 0 1 3.75 15.02A4.4 4.4 0 0 1 8.1 10.625L5.16 4.76A1.56 1.56 0 0 1 6.56 2.5H6.83L10.89 10.625H11.85A4.4 4.4 0 0 1 16.25 15.02Z"
      + "M8.17 12.5A2.52 2.52 0 0 0 5.63 15.02A0.61 0.61 0 0 0 6.78 15.29ZM14.38 15.02A2.52 2.52 0 0 0 11.86 12.5L13.22 15.29A0.61 0.61 0 0 0 14.38 15.02Z",
  },
  dairy: { stroke: "M6 7l1.5-3.5h5L14 7v10H6ZM6 7h8M7.5 3.5 9.5 7M8 10.5h4V14H8Z" },
  daisy: { fill: `M8.57 5A2.15 2.15 0 0 1 10 1.25A2.15 2.15 0 0 1 11.43 5A2.3 2.3 0 0 1 11.58 5.13A2.15 2.15 0 0 1 15.15 4.72A2.15 2.15 0 0 1 14.12 8.16A2.15 2.15 0 0 1 12.9 11.54A2.15 2.15 0 0 1 10.28 9.08A2.3 2.3 0 0 1 9.72 9.08A2.15 2.15 0 0 1 7.1 11.54A2.15 2.15 0 0 1 5.88 8.16A2.15 2.15 0 0 1 4.85 4.72A2.15 2.15 0 0 1 8.42 5.13A2.3 2.3 0 0 1 8.57 5Z${circle(10, 6.6, 1.15)}M4.4 13.05c1.9-.6 3.7.05 5 1.6v2.85H8.45ZM15.6 13.05c-1.9-.6-3.7.05-5 1.6v2.85h.95Z`, stroke: "M10 8.9v8", weight: 1.2 },
  // A mirror ball under its cap, its seams cut out of it, and two sparkles off it.
  dance: { fill: `${circle(9.25, 11.25, 5.6)}${mirrorBallSeams(9.25, 11.25, 5.6)}${rect(8.25, 4.1, 2, 1.1, 0.3)}${burst(4, 2.6, 0.7, 15.6, 4.4)}${burst(4, 1.6, 0.5, 3.6, 16.4)}` },
  // Shading tones drawn as stripes in a ring (outlines, nativeIconOutlined.ts): lights thin, midtones even, darks heavy.
  darks: { stroke: ring, fill: hatch(4, 2.1, 0.9, 6.5) },
  // A round gauge, its needle and its reading.
  // A gauge's dial filled with its needle cut out, and a bar below it, in a ring.
  dashboard: {
    stroke: ring,
    fill: `M5 10.75a5 5 0 0 1 10 0Z${polygon([[9.35, 10.55], [6.85, 7.55], [7.65, 6.9], [10.55, 9.85]])}${rect(6.75, 12.5, 6.5, 1.75, 0.4)}`,
  },
  database: { stroke: `${ellipse(10, 5, 6.5, 2.25)}M3.5 5v10c0 1.25 2.9 2.25 6.5 2.25s6.5-1 6.5-2.25V5M3.5 10c0 1.25 2.9 2.25 6.5 2.25s6.5-1 6.5-2.25` },
  defibrillator: { stroke: heart, fill: "M10.75 7.5 8.25 11h1.9l-.8 3 2.7-3.75h-1.9Z" },
  "delivery-truck": { stroke: `M2.75 5.5h9v8.75h-9ZM11.75 8h3l2.5 3v3.25h-5.5${circle(5.75, 14.75, 1.5)}${circle(14.25, 14.75, 1.5)}` },
  // A tube of toothpaste and a toothbrush.
  dental: {
    fill: `M4.75 7.25h4l1 9.75h-6Z${bar(5.5, 4.25, 2.5, 2.25)}${rect(12.5, 2.75, 3.75, 5.75, 1)}${bar(13.25, 4.25, 2.25, 0.65)}${bar(13.25, 5.85, 2.25, 0.65)}`,
    stroke: "M14.375 8.75v7.75",
  },
  departures: {
    stroke: "M2.75 16.75h14.5",
    fill: "M3.9 12.4 3 10.7l1.3-.5 2.1 1.2 3.4-1.7L7.9 6.9l1.5-.6 4.2 2.9 2.2-1.1c.9-.4 1.8-.2 2.1.5s-.1 1.4-.9 1.8L5.9 15Z",
  },
  dependency: { stroke: `${rect(2.75, 2.75, 5.5, 5.5, 1.25)}${rect(11.75, 11.75, 5.5, 5.5, 1.25)}M5.5 8.25v3.5a2.75 2.75 0 0 0 2.75 2.75h3.5M9.75 12.5l2 2-2 2` },
  description: { stroke: "M3.5 5h13M3.5 8.5h13M3.5 12h13M3.5 15.5h8" },
  dialogue: { fill: `${bubble}${questionMark}` },
  die1: die("c"),
  die2: die("tl", "br"),
  die3: die("tl", "c", "br"),
  die4: die("tl", "tr", "bl", "br"),
  die5: die("tl", "tr", "c", "bl", "br"),
  die6: die("tl", "tr", "ml", "mr", "bl", "br"),
  dining: { fill: `${circle(13.1, 10, 6.9)}${circle(13.1, 10, 5)}${circle(13.1, 10, 3.75)}${bar(0, 2.5, 0.75, 4.5)}${bar(2.25, 2.5, 0.75, 4.5)}${bar(4.5, 2.5, 0.75, 4.5)}M0 7h5.25v.4c0 1.3-.9 2.2-1.85 2.4v7.7H1.85V9.8C.9 9.6 0 8.7 0 7.4Z` },
  "directional-sign": { stroke: "M10 2.75V17M7 17h6M10.95 4H15l1.5 1.5L15 7h-4.05ZM9.05 9H5l-1.5 1.5L5 12h4.05Z" },
  "directional-sign-left": { stroke: post, fill: polygon([[0.625, 8.75], [4.375, 5], [16.25, 5], [16.25, 12.5], [4.375, 12.5]]) },
  "directional-sign-right": { stroke: post, fill: polygon([[3.125, 5], [15, 5], [18.75, 8.75], [15, 12.5], [3.125, 12.5]]) },
  directions: { stroke: `${diamond}M8 12.5v-2a1 1 0 0 1 1-1h3.5M11 8l1.5 1.5L11 11` },
  "dish-soap": { stroke: "M8.5 2.8h3v1.6l2.1 2.2c.45.45.4 1.2 0 1.7l-1.2 2.2c-.3.45-.25 1.05 0 1.5l1.05 2.3c.3.7.5 1.5.5 2.25H6.05c0-.75.2-1.55.5-2.25L7.6 12c.25-.45.3-1.05 0-1.5L6.4 8.3c-.4-.5-.45-1.25 0-1.7l2.1-2.2Z" },
  dna: { stroke: "M5 3.45c0 3.3 10 3.3 10 6.55s-10 3.25-10 6.55M15 3.45c0 3.3-10 3.3-10 6.55s10 3.25 10 6.55M6.6 5.2h6.8M6.6 14.8h6.8" },
  "do-not-disturb": { stroke: `${rect(6, 3, 8, 14, 1.5)}${circle(10, 6, 1.75)}M8 11h4M8 13.5h4` },
  dog: {
    stroke: "M6 6h8l1.5 1.5V12a5.5 5 0 0 1-11 0V7.5Z",
    fill: `M6 6 2.75 7.5l1 5L6 11ZM14 6l3.25 1.5-1 5L14 11Z${circle(8, 10, 0.8)}${circle(12, 10, 0.8)}${ellipse(10, 13, 1.25, 0.9)}`,
  },
  // Notion's donkey (read 2026-11-18), filled, in profile: its head down at the top right with long ears up to 3.125, its back
  // level at 6.25 rounding in Notion's round down to its tail at the left, its belly and four legs to the round edge's foot.
  donkey: { fill: `M18.125 3.75L17.41 5.17Q18.125 5.9 18.125 7.19V10.625H16.25L15.47 8.28L13.125 10.625V16.25L13.75 17.5H11.56L10.94 16.25V12.54Q9.85 13.125 8.44 13.125Q7.5 13.125 6.69 12.87L5.94 14.375L7.5 17.5H5.31L3.75 14.375V11.875L2.5 10.625V14.375H1.25V10.625${curve(roundQuarters(5.625, 10.625, 4.375)[3])}H12.27L13.26 5.26Q14 4.45 15.06 4.39Q15.35 4.37 15.625 4.375L16.14 3.86Q16.9 3.125 17.91 3.125H18.125Z` },
  download: { stroke: "M6.5 13.5H6a3.5 3.5 0 0 1-.4-7A4.5 4.5 0 0 1 14.2 5.1a3.75 3.75 0 0 1 .05 8.4h-.75M10 9v8M7.75 14.75 10 17l2.25-2.25" },
  downward: { fill: blockArrow(2) },
  drafts: { stroke: "M3 5h8M3 9h6M3 13h4M15.25 7.25l1.5 1.5-6 6-2.25.5.5-2.25Z" },
  dress: { stroke: "M6.75 2.2v2.2c0 1.6.85 2.6.55 3.9l-1.35 8.6c1.35.7 2.65.9 4.05.9s2.7-.2 4.05-.9L12.7 8.3c-.3-1.3.55-2.3.55-3.9V2.2v2.2c0 1-1.25 1.2-3.25 1.2s-3.25-.2-3.25-1.2Z" },
  drink: { stroke: "M5 6h10l-1.25 11h-7.5ZM11 6l2-3.5h2.5M5.5 10h9" },
  duck: {
    stroke: "M5 6.5a3 3 0 0 1 6 0c0 1-.5 1.75-1 2.5h4.5c1.5 0 2.75-1 2.75-1 0 4.5-2.5 8-7.25 8C5.5 16 3 13 3 11.5c0-1.25 1.5-2.25 3-2.5-.75-.75-1-1.5-1-2.5ZM5 6.5H2.75l1.5 1",
    fill: circle(7.5, 6, 0.6),
  },
  // Notion's duster (read 2026-11-18), filled: a handle at 45° from the foot left corner to a head that rises in three steps,
  // each rounding 2.5 in Notion's round, from 13.125 at the box's side to the round edge's top at 6.875.
  duster: { fill: `M17.5 13.125H8.2L3.83 17.5L2.5 16.18L6.875 11.8V2.5H8.125${curve(roundQuarters(8.125, 5, 2.5)[0])}H12.5${curve(roundQuarters(12.5, 7.5, 2.5)[0])}V9.375${curve(roundQuarters(15, 11.875, 2.5)[0])}Z` },
  ear: { stroke: "M6 8a4.5 4.5 0 0 1 9 0c0 2.5-1.75 3.25-2.5 4.75-.6 1.2-.6 2.25-1.4 3.25-1.2 1.5-3.6 1-3.85-.75C6.75 13.5 6 11 6 8ZM8.5 8.25a2 2 0 0 1 4 0c0 1.1-1 1.5-1.5 2.25" },
  // Notion's ear with a hearing aid (read 2026-11-18), filled: an ear a line wide curling in from its top at 1.875 round to its
  // lobe at the foot, and the aid hooked over its top at the left, its tip at the ear's foot right.
  "ear-hearing-aid": {
    fill: "M17.34 12.19H16.56Q15.36 13.44 13.75 13.44Q10.94 13.44 10.94 10.625Q10.94 7.85 13.73 7.81Q13.5 5 11.25 5Q8.125 5 8.125 9.06Q8.125 10.6 8.75 11.93L7.63 12.5A8 8 0 0 1 6.875 9.06Q6.875 3.75 11.25 3.75Q15 3.75 15 7.81Q15 9.06 13.75 9.06Q12.19 9.06 12.19 10.625Q12.19 12.19 13.75 12.19Q16.875 12.19 16.875 7.81Q16.875 2.1 11.24 1.89"
      + "L11.25 1.875H7.18Q3.4 1.875 2.41 5.38A12.6 12.6 0 0 0 1.875 9.06Q1.875 9.5 1.91 10H3.79A12 12 0 0 1 3.75 9.06Q3.75 7 4.42 5.32Q5 3.75 6.75 3.75H6.8Q5 5.7 5 9.06Q5 18.125 13.44 18.125Q16.25 18.125 16.25 15.31L16.97 14.48A2.24 2.24 0 0 0 17.34 12.19Z",
  },
  earthquake: { stroke: "M3.5 9.25 10 3.5l6.5 5.75v7.25h-13ZM10 3.5 8.75 7l2.5 2.5-2 3 1.5 4" },
  egg: { stroke: "M10 3c3 0 5.25 4.25 5.25 8a5.25 5.25 0 0 1-10.5 0C4.75 7.25 7 3 10 3Z" },
  eject: { fill: `${polygon([[10, 3.5], [16, 10.5], [4, 10.5]])}${rect(4, 13, 12, 2.75, 0.75)}` },
  "electric-guitar": { stroke: "M10 11.5c-.4 0-.8-.5-.9-1.3-.2-1.4-2.9-1.6-2.9.4 0 1.3.9 2 .9 3 0 .7-1.15 1.4-1.15 3 0 1.4 1.85 2.45 4.05 2.45s4.05-1.05 4.05-2.45c0-1.6-1.15-2.3-1.15-3 0-.8.7-1.3.7-2 0-.7-.5-1.1-1-1.1-.6 0-1 .5-1 1ZM10 10.55V4.6M10 4.6 12.05.95M8.85 15.95h2.3" },
  // Notion's plug (read 2026-11-18): two prongs 1.875 wide from 1.875, a base across 4.375..15.625 on 6.25..8.125, a body under
  // it to 13.125 with its foot corners round 2.5, and a cord a line wide down from its middle bending round 2.8 to the right.
  "electric-plug": {
    fill: `${bar(6.5625, 1.875, 1.875, 4.375)}${bar(11.5625, 1.875, 1.875, 4.375)}${bar(4.375, 6.25, 11.25, 1.875)}${plate(5.625, 8.125, 14.375, 13.125, [0, 0, 2.5, 2.5])}`,
    stroke: "M10 13.4V14.375A2.8125 2.8125 0 0 0 12.8125 17.1875",
  },
  // Notion's elephant (read 2026-11-18), filled, in profile: its head at the top right with its trunk curling down and back up
  // round 1.25, its back rising from its tail at the left, its legs to the round edge's foot.
  elephant: { fill: `M18.75 7.91V11.875${curve(roundQuarters(15.625, 11.875, 3.125)[1])}H15V13.125H15.625Q16.875 13.125 16.875 11.875Q16.875 10.625 15.625 10.625H13.75V15.76Q14.375 16.1 14.375 16.875V17.5H10.625V13.125H6.875V15.76Q7.5 16.1 7.5 16.875V17.5H3.75V11.875L2.5 10.625V13.75H1.25V9.375A3.75 3.75 0 0 1 5 5.625H10`
    + `${curve(roundQuarters(12.5, 5.625, 2.5)[3])}H13.97Q15.2 3.125 15.73 3.86L18.02 6.14Q18.75 6.9 18.75 7.91Z` },

  // A lift: its car with a rider cut out of it, and the up and down arrow beside it.
  elevator: {
    fill: `${rect(2.25, 3, 8.25, 14, 0.75)}${circle(6.375, 5.9, 1.35)}M4.6 8.1h3.55l.75 3.2-.95.25-.35-1.45v5.65h-.95V12.4h-.55v3.35h-.95V10.1l-.35 1.45-.95-.25Z`
      + polygon([[15, 3], [18, 6.5], [16, 6.5], [16, 13.5], [18, 13.5], [15, 17], [12, 13.5], [14, 13.5], [14, 6.5], [12, 6.5]]),
  },
  emoji: face("", `${eyes}${smile}`),
  "emoji-angry": face("M6 6.25l2.5 1M14 6.25l-2.5 1M7.25 13.75a3.25 3.25 0 0 1 5.5 0", eyes),
  "emoji-big-sad": face("M6.5 14.5a4 4 0 0 1 7 0", eyes),
  "emoji-disappointed": face("M6.5 8.75h2M11.5 8.75h2M7.5 13.25a3 3 0 0 1 5 0"),
  "emoji-grinning": face("", `${eyes}${grin}`),
  "emoji-grinning-smiling-eyes": face(smilingEyes, grin),
  "emoji-heart-eyes": face("", `${smallHeart(7.5, 8.25)}${smallHeart(12.5, 8.25)}${smile}`),
  "emoji-neutral": face("", `${eyes}${bar(6.5625, 12.5, 6.875, 1.25)}`),
  "emoji-sad": face("", `${eyes}${sadMouth}`),
  "emoji-smiling-eyes": face(smilingEyes, smile),
  // The sunglasses cut out of the face, their glints left in it, as Notion's (rastered at 400px, 2026-11-18).
  "emoji-sunglasses": face("", `${sunglassesShape(11.2 / 18.75, 4.4, 6.875)}${smile}`),
  "emoji-surprised": face(circle(10, 13, 1.75), eyes),
  "emoji-winking": face("M11.5 8.75h2", `${rect(6.5625, 7.1875, 1.875, 3.125, 0.9375)}${smile}`),
  escalator: { stroke: "M3.05 11.55H8.1l6.15-6.15h2.7a1.5 1.5 0 0 1 0 3H14.9l-6.15 6.3h-5.7a1.55 1.55 0 0 1 0-3.15Z", fill: `${circle(3.6, 3.75, 1.25)}M2.35 9.1v-.9a1.25 1.25 0 0 1 2.5 0v.9Z` },
  // A round door's two leaves sliding apart.
  exit: { fill: "M9.25 3A7 7 0 0 0 9.25 17ZM10.75 3A7 7 0 0 1 10.75 17Z" },
  expand: { stroke: "M8 8 3.5 3.5M3.5 6.75V3.5h3.25M12 8l4.5-4.5M13.25 3.5h3.25v3.25M8 12l-4.5 4.5M3.5 13.25v3.25h3.25M12 12l4.5 4.5M16.5 13.25v3.25h-3.25" },
  extension: { stroke: "M4 6.5h3a2 2 0 1 1 4 0h3v3a2 2 0 1 1 0 4V17H4v-3.25a2 2 0 1 0 0-4Z" },
  // A pinked swatch, scalloped all round, with its label cut out near its foot.
  "fabric-swatch": {
    fill: `M3.75 3.75${"a1.6 1.6 0 0 1 2.5 0".repeat(5)}${"a1.6 1.6 0 0 1 0 2.5".repeat(5)}${"a1.6 1.6 0 0 1-2.5 0".repeat(5)}${"a1.6 1.6 0 0 1 0-2.5".repeat(5)}Z`
      + bar(5.75, 11.75, 8.5, 2.5),
  },
  // Notion's tissues (read 2026-11-18), filled: a box across 2.5..17.5 from 10 to the round edge's foot, and over it a tissue
  // narrowing down to 8.75, its top waving from 3.125 at its corners.
  "facial-tissues": { fill: `M6.875 8.75L5 3.125Q6.1 2.5 7.19 2.5Q9.4 2.5 10.4 3.2Q11 3.75 11.56 3.75Q12.7 3.75 13.75 3.125H15L13.125 8.75Z${bar(2.5, 10, 15, 7.5)}` },
  factory: { stroke: "M2.75 17V9l4 2.5V9l4 2.5V9l4 2.5V3.5h2.5V17ZM2.75 17h14.5M5.5 14h1.5M9.5 14H11M13.5 14H15" },
  // A swatch book: its front strip with two chips and its pin hole cut out,
  // and the next strip swung out beside it, standing on a corner.
  "fan-deck": { stroke: `${rect(3.45, 3.45, 5.3, 13.1, 0.6)}M12.05 7.25 13.9 4.8l4.1 3.55-5.95 5.65Z`, fill: `${bar(4.6, 4.4, 3, 2.4)}${bar(4.6, 8.6, 3, 2.4)}${circle(6.1, 14.3, 0.9)}` },
  feather: { stroke: "M16 4c-6-.5-10 3.5-10 10 4 0 9.5-4.5 10-10ZM6 17v-2M8.75 11.75l3.5-3.5" },
  feed: {
    stroke: rect(2.75, 2.75, 14.5, 14.5, 1.75),
    fill: `${rect(5, 5, 4, 4, 0.6)}${rect(10.25, 5, 4.75, 1.5, 0.6)}${rect(10.25, 7.5, 3.25, 1.5, 0.6)}${rect(5, 10.5, 10, 4.5, 0.6)}`,
  },
  // Three narrowing bars, cut out of a disc.
  // Three bars cut out of the disc, each shorter than the one above (Notion's are 10, 6.25 and 2.5 long, flat-ended).
  filtered: inCircle("M6 7h8M7.75 10h4.5M9.65 13h.7"),
  fire: { fill: "M10 17c-3.5 0-5.75-2.4-5.75-5.4 0-3.1 2.5-4.6 3.25-7.35.25-.9 1.3-1.15 1.8-.4 1 1.45 1.25 2.85 1.2 4.15.9-.65 1.45-1.5 1.7-2.45.2-.7 1-.9 1.45-.35 1.4 1.7 2.1 3.6 2.1 5.6 0 3.35-2.3 6.2-5.75 6.2Z" },
  // Notion's extinguisher (read 2026-11-18), filled: a body 7.5 wide from 6.25 to the round edge's foot, its shoulders Notion's
  // round 1.875; a neck to a handle across 9.06..14.375 at 1.875..3.125 slanting down to 4.34 at its right end; and a hose a
  // line wide from the neck bending round to the left and down to 15.625.
  "fire-extinguisher": {
    fill: `M10.94 4.34V6.25H11.875${curve(roundQuarters(11.875, 8.125, 1.875)[0])}V17.5H6.25V8.125${curve(roundQuarters(8.125, 8.125, 1.875)[3])}H9.06V5H8.125`
      + `${curve(back(roundQuarters(8.125, 8.125, 3.125)[3]))}V15.625H3.125V8.125${curve(roundQuarters(8.125, 8.125, 5)[3])}H9.06V1.875H14.375V3.125Z`,
  },
  "fire-truck": { stroke: "M2.2 8.35h8.7v-1.1h2.75v-1h1.4v1h1.85l.9 3.35v2.85H2.2ZM3.4 9.95h6.2", fill: `${bar(1.25, 5, 8.75, 1.3)}${bar(1.25, 6.3, 0.9, 1.15)}${bar(3.85, 6.3, 0.9, 1.15)}${bar(6.45, 6.3, 0.9, 1.15)}${bar(9.1, 6.3, 0.9, 1.15)}${bar(13.2, 8.3, 3.3, 2.3)}${circle(5, 15.25, 1.65)}${circle(15, 15.25, 1.65)}` },
  fireworks: { stroke: fireworkRays, fill: circle(10, 10, 1.1) },
  // Its arms end on the round edge.
  "first-aid": { fill: `M8 ${10 - ROUND_EDGE}h4V8H${10 + ROUND_EDGE}v4H12V${10 + ROUND_EDGE}H8V12H${10 - ROUND_EDGE}V8H8Z` },
  "first-aid-kit": { stroke: `${rect(2.75, 5.5, 14.5, 11, 1.75)}M7.5 5.5V4a1 1 0 0 1 1-1h3a1 1 0 0 1 1 1v1.5M10 8.5v5M7.5 11h5` },
  fish: { stroke: "M18.45 10c-1.55-2.9-3.95-4.05-6.7-4.05s-5.15 1.15-6.85 4.05c1.7 2.9 4.1 4.05 6.85 4.05s5.15-1.15 6.7-4.05ZM4.9 10 1.55 6.55 2.5 10l-.95 3.45Z", fill: circle(13.3, 8.8, 1.15) },
  "flag-checkered": { stroke: "M4.5 17V3M4.5 3.75h11v8h-11", fill: checkered },
  "flag-pennant": { stroke: "M5 17V3M5.95 3.8 16 7.25 5.95 10.7Z" },
};
