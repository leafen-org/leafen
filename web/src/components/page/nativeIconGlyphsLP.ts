import {
  HALF_LINE,
  n,
  cross,
  minus,
  plus,
  bar,
  burst,
  circle,
  disc,
  ellipse,
  hatch,
  inCircle,
  polygon,
  rect,
  ring,
  ringSlash,
  type NativeIconGlyph,
} from "./nativeIconGlyphShapes";
import { at, back, curve, roundQuarters } from "./nativeIconRound";
import { offSlash } from "./nativeIconOff";

// Leafen glyphs for catalog names from "leo" through "passport". The "notion"
// name keeps the neutral mark: it depicts another company's logo, which
// Leafen does not redraw.

const documentOutline = "M5.5 2.75h5.75L14.5 6v10.5a.75.75 0 0 1-.75.75H5.5a.75.75 0 0 1-.75-.75v-13a.75.75 0 0 1 .75-.75ZM11 2.75V6.5h3.5";
const folderOutline = "M2.75 5.5a1 1 0 0 1 1-1h3.75l1.75 2h7a1 1 0 0 1 1 1v7.75a1 1 0 0 1-1 1H3.75a1 1 0 0 1-1-1Z";
/** A flame: a drop 1.875 across, round at its foot about (x, y), pointed up to `tip`. */
const flame = (x: number, y: number, tip: number) => `M${x - 0.9375} ${y}A0.9375 0.9375 0 0 0 ${x + 0.9375} ${y}Q${x + 0.9375} ${y - 0.6} ${x} ${tip}Q${x - 0.9375} ${y - 0.6} ${x - 0.9375} ${y}Z`;
/** A menorah's branch: a band 1.25 wide bending down in Notion's round `r` about (10, 6.875), its ends on 6.875. */
const branch = (r: number) => {
  const outer = roundQuarters(10, 6.875, r + 0.625), inner = roundQuarters(10, 6.875, r - 0.625);
  return `M${at(outer[2][3])}${curve(back(outer[2]))}${curve(back(outer[1]))}H${n(10 + r - 0.625)}${curve(inner[1])}${curve(inner[2])}Z`;
};
const microphone = `${rect(7.5, 2.75, 5, 9, 2.5)}M5 9.5a5 5 0 0 0 10 0M10 14.5V17M7.5 17h5`;
const clapboard = `${rect(3, 8, 14, 9, 1.25)}M3 8l.5-3.5 13-2 .5 3.5M6.5 4.25 8 7.5M10.5 3.6 12 6.9M14.5 3 16 6`;
const noteHead = ellipse(9.75, 14.75, 2.25, 1.75);
const pin = "M10 17.5s5.5-5 5.5-9a5.5 5.5 0 0 0-11 0c0 4 5.5 9 5.5 9Z";
const parking = "M8 14V6h3a2.25 2.25 0 0 1 0 4.5H8";
const lowCloud = "M7.5 16.5a3 3 0 0 1-.35-6A3.9 3.9 0 0 1 14.6 9.4a3.25 3.25 0 0 1 .15 7.1Z";
const skirtBody = "M7 3l3 1.5L13 3";

/**
 * A globe cut out of a fill (with the fill's even-odd rule): a disc of
 * radius `r` with its meridian, an ellipse's rim, and its equator left
 * standing in lines about a ninth of its width; the equator stops at the
 * meridian's rim so that no two lines overlap.
 */
function globeCut(cx: number, cy: number, r: number) {
  const line = r / 4.3, half = line / 2;
  const outer = r / 2, inner = outer - line, innerTall = r - line;
  const across = (rx: number, ry: number) => rx * Math.sqrt(1 - (half / ry) ** 2);
  const edge = Math.sqrt(r * r - half * half), rim = across(outer, r), middle = across(inner, innerTall);
  const equator = (from: number, to: number) => bar(cx + from, cy - half, to - from, line);
  return `${circle(cx, cy, r)}${ellipse(cx, cy, outer, r)}${ellipse(cx, cy, inner, innerTall)}`
    + `${equator(-edge, -rim)}${equator(-middle, middle)}${equator(rim, edge)}`;
}

/**
 * A badge's twelve points, as Notion's new badge and new alert have them
 * (read as paths, 2026-11-18): each a right angle reaching 1.6..18.4, so
 * the corners between them are √(2/3) as far out. Drawn as a line, the
 * badge is that shape a line's sharp corner (√2 half lines) smaller.
 */
const BADGE_POINTS = 8.4375, BADGE_CORNERS = BADGE_POINTS * Math.sqrt(2 / 3);
const BADGE_LINE = (BADGE_POINTS - HALF_LINE * Math.SQRT2) / BADGE_POINTS;

export const NATIVE_ICON_GLYPHS_LP: Readonly<Record<string, NativeIconGlyph>> = {
  leo: { fill: `${circle(4.75, 13.6, 2.85)}${circle(4.75, 13.6, 1)}`, stroke: "M6.65 13.6c0-3.1-1.75-4.9-1.75-6.4a4.4 4.4 0 0 1 8.8 0c0 3.3-2.3 5.6-2.3 8.6 0 1.4 1.05 2.4 2.45 2.4 1.5 0 2.7-1.05 2.85-2.9" },
  libra: { stroke: "M3.45 11.5H7.9v-1.3a3.7 3.7 0 1 1 4.2 0v1.3h4.45M3.45 15.95h13.1" },
  library: { stroke: "M3 17h14M3.5 8h13L10 3.5ZM5 8v7M8.33 8v7M11.67 8v7M15 8v7M4 15h12" },
  lights: { stroke: ring, fill: hatch(3, 0.9, 2.4, 6.5) },
  link: { stroke: "M8.5 11.5a3 3 0 0 0 4.25 0l2.5-2.5a3 3 0 0 0-4.25-4.25l-1 1M11.5 8.5a3 3 0 0 0-4.25 0l-2.5 2.5a3 3 0 0 0 4.25 4.25l1-1" },
  lipstick: { stroke: "M6.75 11h6.5v6h-6.5ZM8 11V7l4-3v7" },
  // Notion's indented list (read 2026-11-18): a dot 2.5 across with its line, and under them two set in by 3.125.
  "list-indent": {
    stroke: `M${6.25 + HALF_LINE} 5H${15 - HALF_LINE}M${9.375 + HALF_LINE} 10H${18.125 - HALF_LINE}M${9.375 + HALF_LINE} 15H${18.125 - HALF_LINE}`,
    fill: `${circle(3.125, 5, 1.25)}${circle(6.25, 10, 1.25)}${circle(6.25, 15, 1.25)}`,
  },
  // Notion's litter disposal (read 2026-11-18), filled: a person — a head 3.125 across, a body throwing with an arm out to the
  // right on 6.25..7.81 and the other bent down to the left, legs apart to the round edge's foot — and a bin narrowing down at
  // the right, a piece of litter falling over it and two cut in it.
  "litter-disposal": {
    fill: `${circle(6.25, 3.75, 1.5625)}M7.81 7.81H11.875V6.25H6.25Q3.715 6.25 2.68 8.32L1.56 10.55L2.96 11.25L4.19 8.78Q4.35 8.44 4.69 8.44V10.94L2.5 17.5H4.47L6.25 12.18L8.03 17.5H10L7.81 10.94Z`
      + `${polygon([[15.625, 7.5], [14.375, 6.25], [13.125, 7.5], [14.375, 8.75]])}${polygon([[18.125, 10], [16.875, 17.5], [11.875, 17.5], [10.625, 10]])}`
      + `${circle(15.3125, 12.1875, 0.9375)}${polygon([[15.3125, 15], [14.0625, 13.75], [12.8125, 15], [14.0625, 16.25]])}`,
  },

  location: { fill: polygon([[16.5, 3.5], [10.5, 16.5], [9, 11], [3.5, 9.5]]) },
  "lock-keyhole": {
    stroke: `${rect(4, 8.5, 12, 9, 2)}M6.75 8.5V6.25a3.25 3.25 0 0 1 6.5 0V8.5`,
    fill: `${circle(10, 12, 1.25)}${polygon([[9.4, 12.5], [10.6, 12.5], [10.9, 15], [9.1, 15]])}`,
  },
  "log-in": { stroke: "M11.5 3.5H15A1.5 1.5 0 0 1 16.5 5v10a1.5 1.5 0 0 1-1.5 1.5h-3.5ZM3 10h7.5M7.5 7l3 3-3 3" },
  "log-out": { stroke: "M8.5 3.5H5A1.5 1.5 0 0 0 3.5 5v10A1.5 1.5 0 0 0 5 16.5h3.5ZM9.5 10H17M14 7l3 3-3 3" },
  "long-bone": { stroke: "M5 13.4 14.9 5.8M14.9 5.8l.05-1.75M14.9 5.8l2.9.9M5 13.4l-2.8-.45M5 13.4l-.4 2.55" },
  "long-sleeve-shirt": { stroke: `${skirtBody}l3.25 1.75.75 10.25h-2.5l-.5-8V17H6V7l-.5 8H3l.75-10.25Z` },
  "looped-square": { stroke: "M7.5 7.5v-2a2 2 0 1 0-2 2h9a2 2 0 1 0-2-2v9a2 2 0 1 0 2-2h-9a2 2 0 1 0 2 2Z" },
  "lost-and-found": {
    stroke: "M2.75 9a5 5 0 0 1 10 0ZM7.75 9v5.5a1.25 1.25 0 0 1-2.5 0M12.5 17v-5a2 2 0 0 1 4 0v5ZM12.5 14.25l-1.25-1.25a.9.9 0 0 0-1.25 1.25l2.5 2.5M12.5 15.5h4",
  },
  lounge: { stroke: "M3 13l3-6h3l2 4h6v2ZM5 13v3M15 13v3" },
  luggage: { stroke: `${rect(5, 5.5, 10, 10, 1.5)}M8 5.5v-2h4v2M7 15.5V17M13 15.5V17M8 8v5M12 8v5` },
  "luggage-cart": { stroke: `M3 3h2l2.5 11h9${rect(8, 5, 7, 6, 1)}${circle(8.5, 16, 1.25)}${circle(15, 16, 1.25)}` },
  lungs: { stroke: "M10 2.5v6M10 8.5 8.5 10M10 8.5l1.5 1.5M8.5 7.5c-3-1-5.5 2-5.5 6 0 2.25 1.25 3.5 3 3.5 2 0 2.5-2 2.5-4.5ZM11.5 7.5c3-1 5.5 2 5.5 6 0 2.25-1.25 3.5-3 3.5-2 0-2.5-2-2.5-4.5Z" },
  "magic-wand": { stroke: "M4 16 13.5 6.5M12 5l3 3M15.5 2.5v2M14.5 3.5h2M17 8.5V10M16.25 9.25h1.5M8.5 3v1.5M7.75 3.75h1.5" },
  // A horseshoe magnet, its poles apart.
  // Notion's magnet (read 2026-11-18): a U from 6.875 down, 13.75 across round its foot and 5 inside, and its two poles
  // over it from the round edge's top, 1.25 above it.
  magnet: { fill: `M3.125 6.875H7.5V10.625A2.5 2.5 0 0 0 12.5 10.625V6.875H16.875V10.625A6.875 6.875 0 0 1 3.125 10.625Z${bar(3.125, 2.5, 4.375, 3.125)}${bar(12.5, 2.5, 4.375, 3.125)}` },
  mahjong: { stroke: `${rect(5, 2.75, 10, 14.5, 1.75)}${rect(6.75, 4.5, 6.5, 11, 1)}M10 7.5v5M8.25 10h3.5` },
  "makeup-brush": { stroke: "M8 11l-4.5 5.5 1 1L10 13M8 11l4.5-6a2.5 2.5 0 0 1 3.5 3.5L10 13ZM8 11l2 2" },
  mandir: { stroke: "M10 2.25v1.5M10 3.75 6.5 10h7ZM4.5 17v-7h11v7ZM8.5 17v-3.25a1.5 1.5 0 0 1 3 0V17M3 17h14M10 2.25l1.75.75L10 3.75" },
  map: { stroke: "M3 5l4.5-1.5 5 1.5L17 3.5V15l-4.5 1.5-5-1.5L3 16.5ZM7.5 3.5V15M12.5 5v11.5" },
  // Notion's map pin (read 2026-11-18), filled, upright: a round head 8.75 across from 1.875, and a stem a line wide down from
  // it narrowing to a point cut 0.62 across at the round edge's foot.
  "map-pin": { fill: "M10.94 10.52V15L10.31 17.5H9.69L9.06 15V10.52A4.375 4.375 0 1 1 10.94 10.52Z" },
  "map-pin-alternate": { fill: `${pin}${circle(10, 8.25, 2)}` },
  // Notion's mathematics (read 2026-11-18): a plus, a minus, a cross and a division sign, each reaching 3.4375 from its middle
  // in a quarter of the box (5.9375 and 14.0625 across and down), the division's dots 1.09 round, 2.66 off its bar.
  mathematics: { fill: `${plus(5.9375, 5.9375, 3.4375)}${minus(14.0625, 5.9375, 3.4375)}${cross(5.9375, 14.0625, 3.4375)}${minus(14.0625, 14.0625, 3.4375)}${circle(14.0625, 11.41, 1.09)}${circle(14.0625, 16.72, 1.09)}` },
  meat: { stroke: "M11 3.5c3 0 5.5 2.5 5.5 5.5 0 3.5-3.5 5-6.5 4l-2.5 2.5a1.5 1.5 0 1 1-2-2 1.5 1.5 0 1 1-1-2L7 9c-1-3 .5-5.5 4-5.5Z" },
  medication: { stroke: "M5.5 3h9v3h-9ZM6.5 6h7v11h-7ZM8.5 11.5h3M10 10v3" },
  meeting: { fill: `${circle(2.3, 6, 1.65)}${circle(10, 6, 1.65)}${circle(17.7, 6, 1.65)}M0 15v-4.9a1.5 1.5 0 0 1 1.5-1.5h1.3c.5 0 .95.25 1.2.65l1.55 2.4h.85v1H5.1L3.95 11v4ZM20 15v-4.9a1.5 1.5 0 0 0-1.5-1.5h-1.3c-.5 0-.95.25-1.2.65l-1.55 2.4h-.85v1h1.3l1.15-1.65v4ZM8.2 11.85V9.6a1 1 0 0 1 1-1h1.6a1 1 0 0 1 1 1v2.25Z${bar(5, 13, 10, 2)}` },
  megaphone: { stroke: "M3 8.5v3h2.5l7.5 4v-11l-7.5 4ZM5.5 11.5l1 4h2l-1-3.5M15.5 8v4" },
  // Notion's menorah (read 2026-11-18): a stem 1.25 wide from 5 to its foot, three branches either side, bands 1.25 wide bending
  // in Notion's round (nativeIconRound.ts) 2.5, 5 and 7.5 about (10, 6.875) from 6.875 down, a foot 10 across, and on each
  // a flame, a drop 1.875 across pointed up.
  menorah: {
    weight: 1.25,
    stroke: "M10 5.625V15.625",
    fill: `${[2.5, 5, 7.5].map(branch).join("")}${bar(5, 16.25, 10, 1.25)}${[2.5, 5, 7.5, 12.5, 15, 17.5].map((x) => flame(x, 5, 3.28)).join("")}${flame(10, 3.125, 1.41)}`,
  },
  // Notion's pad (read 2026-11-18), filled, along the slant from the foot left to the top right: round ends 8.34 across about
  // (6.67, 13.33) and (13.33, 6.67) joined by straight sides, a square tab 3.75 wide out of each side, and inside it a like
  // shape cut out, its ends 4.58 across and its sides bowed in round 6.04.
  "menstrual-pad": {
    fill: "M13.33 2.5A4.17 4.17 0 0 0 9.52 4.97L8.94 6.29L7.65 5L5 7.65L6.29 8.94L4.97 9.53A4.17 4.17 0 1 0 10.47 15.03L11.06 13.71L12.35 15L15 12.35L13.71 11.06L15.03 10.47A4.17 4.17 0 0 0 13.33 2.5Z"
      + "M14.27 8.76L12.58 9.51A6.04 6.04 0 0 0 9.51 12.58L8.76 14.27A2.29 2.29 0 1 1 5.74 11.24L7.43 10.49A6.04 6.04 0 0 0 10.5 7.42L11.25 5.74A2.29 2.29 0 1 1 14.27 8.76Z",
  },
  merge: { stroke: "M5 3.5v3.25a4 4 0 0 0 1.75 3.3L10 12.25v4.25M15 3.5v3.25a4 4 0 0 1-1.75 3.3L10 12.25M7.75 14.25 10 16.5l2.25-2.25" },
  // Notion's metronome (read 2026-11-18), filled: a body narrowing up from 3.75..16.25 at its foot to a top 3.75 across at the
  // round edge's top, its arm slanting up to the right out of it, and a window cut under the arm's foot.
  metronome: {
    fill: polygon([[16.25, 11.25], [16.25, 17.5], [3.75, 17.5], [3.75, 11.25], [8.125, 2.5], [11.875, 2.5], [12.37, 3.49], [11.32, 5.59], [10.72, 4.375], [9.29, 4.375],
      [5.85, 11.25], [9.89, 11.25], [13.96, 3.125], [15.625, 3.96], [11.99, 11.25], [14.16, 11.25], [13.77, 10.48], [14.82, 8.38], [16.26, 11.25]]),
  },
  microphone: { stroke: microphone },
  // Notion's microphone off (read 2026-11-18), filled: the microphone — a head 5 across with a round top from 1.875, a holder
  // round 5.625 out from 8.75, a stem and a foot 7.5 across on the round edge's foot — cut away either side of the slash, the
  // head's foot left gone (nativeIconOff.ts).
  "microphone-off": {
    fill: "M14.74 12.08L13.33 10.67Q13.75 9.8 13.75 8.75H15.625Q15.625 10.6 14.74 12.08Z"
      + `M12.5 8.75V4.375${curve(back(roundQuarters(10, 4.375, 2.5)[0]))}${curve(back(roundQuarters(10, 4.375, 2.5)[3]))}V4.85L12.35 9.7Q12.5 9.25 12.5 8.75Z`
      + `M10.94 12.5H10${curve(roundQuarters(10, 8.75, 3.75)[2])}H4.375Q4.375 13.6 9.06 14.31V15.625H6.25V17.5H13.75V15.625H10.94Z${offSlash}`,
  },
  microscope: { stroke: "M9 3l3 1.5-2.5 5L6.5 8ZM8 8.5l-.75 1.5M12 12.5a4 4 0 0 0-2-5M4 17h12M8 17v-2.5h5a3 3 0 0 0 .5-6" },
  microwave: { stroke: `${rect(2.75, 4, 14.5, 11.5, 1.5)}${rect(4.75, 6, 7.5, 7.5, 1)}M14.5 6.5v1M14.5 9.5v1M14 13h1` },
  midtones: { stroke: ring, fill: hatch(4, 1.4, 1.6, 6.5) },
  // Notion's mirror (read 2026-11-18), filled: a round glass 6.25 across in a frame round 7.5 about (10, 6.875) open at the
  // top, its rim parted from its inside by a cut 6.25 round, on a stem 2.5 wide over a foot 10 across on the round edge's foot.
  mirror: {
    fill: "M14.96 6.25A5 5 0 0 0 5.04 6.25H2.5V6.875A7.5 7.5 0 0 0 8.75 14.27V16.25H5V17.5H15V16.25H11.25V14.27A7.5 7.5 0 0 0 17.5 6.875V6.25Z"
      + `M10 13.125A6.25 6.25 0 0 1 3.78 7.5H5.04A5 5 0 0 0 14.96 7.5H16.22A6.25 6.25 0 0 1 10 13.125Z${circle(10, 6.875, 3.125)}`,
  },
  mobile: {
    stroke: "M10 2.5v3M4 5.5h12M4 5.5v4M10 5.5v6M16 5.5v3",
    fill: `${burst(5, 1.9, 0.85, 4, 11)}${circle(10, 13, 1.5)}${circle(16, 10, 1.5)}`,
  },
  // Notion's monorail (read 2026-11-18), filled, front on: a car swelling to 16.25 across its middle and narrowing to its
  // top's light at 0.94, its windscreen and two lamps cut out, its foot parted over the rail, the rail to the box's foot.
  monorail: {
    fill: "M15.82 12.06L14.21 16.875H12.5V14.375H7.5V16.875H5.78L4.17 12.06Q3.3 9.7 4.17 7.32L5.46 3.47Q6.8 2.7 8.87 2.55L9.4 0.94H10.59L11.12 2.55Q13.2 2.7 14.53 3.47L15.82 7.32Q16.7 9.7 15.82 12.06Z"
      + `${bar(8.75, 15.625, 2.5, 3.125)}${circle(6.875, 11.56, 0.94)}${circle(13.125, 11.56, 0.94)}M14.32 8.75L13.03 4.86Q11.8 4.375 10 4.375Q8.2 4.375 6.98 4.86L5.69 8.75Z`,
  },
  mop: { stroke: "M9.25 11 13.75 2.5M5.5 11H13l1 6H4.5ZM7 11.5 6.5 17M9.25 11.5V17M11.5 11.5l.5 5.5" },
  "mop-and-bucket": { stroke: "M9 9h8l-1 8h-6ZM10.5 9V7.5a2.5 2.5 0 0 1 5 0V9M3.5 2.5 6 12M3.5 12h5l.5 5H3Z" },
  mosque: { stroke: "M5 17v-6a5 5 0 0 1 10 0v6ZM10 6V3.5M3 17h14M2.75 17V8M2 8l.75-1.5L3.5 8M17.25 17V8M16.5 8l.75-1.5L18 8M8.5 17v-3a1.5 1.5 0 0 1 3 0v3" },
  // A motorbike: heavy wheels, the seat and tank, the engine between them, the fork and bars.
  motorcycle: {
    fill: `${circle(5, 14, 3)}${circle(5, 14, 1.1)}${circle(15, 14, 3)}${circle(15, 14, 1.1)}`
      + `${polygon([[3.5, 8.25], [8.25, 8.25], [9.75, 9.75], [12.75, 9.75], [11.25, 11], [6.75, 11]])}${bar(8.5, 11.75, 3, 2.75)}`,
    stroke: "M14.25 10.75 12.5 6.25h-1.75",
  },
  mountains: { stroke: "M2.5 16.5 7.5 7l3 5 2-3 5 7.5ZM6 9.75l1.5 1 1.5-1" },
  mouth: { stroke: "M3 10c2-2.5 3.75-3 5-2.25.75.5 1.25.5 2 0s1.25-.5 2 0C13.25 7 15 7.5 17 10c-2 3-4.25 4.5-7 4.5s-5-1.5-7-4.5ZM3 10h14" },
  move: { stroke: `${folderOutline}M7.5 11.25h5M10.5 9l2.25 2.25-2.25 2.25` },
  "move-document": { stroke: `${documentOutline}M7 11.5h6M10.75 9.25 13 11.5l-2.25 2.25` },
  movie: { stroke: `${rect(3.5, 2.75, 13, 14.5, 1.5)}M6.5 2.75v14.5M13.5 2.75v14.5M3.5 6.5h3M3.5 10h3M3.5 13.5h3M13.5 6.5h3M13.5 10h3M13.5 13.5h3` },
  "movie-camera": { stroke: `${rect(2.75, 8, 10, 8, 1.5)}${circle(5.5, 5.25, 2.25)}${circle(10, 5, 2.5)}M12.75 11l4.5-2.5v7l-4.5-2.5` },
  "movie-clapboard": { stroke: clapboard },
  "movie-clapboard-play": { stroke: clapboard, fill: polygon([[8.5, 10.25], [12.75, 12.5], [8.5, 14.75]]) },
  mushroom: {
    stroke: "M2.75 10a7.25 6.5 0 0 1 14.5 0ZM7.5 10v5A1.5 1.5 0 0 0 9 16.5h2a1.5 1.5 0 0 0 1.5-1.5v-5",
    fill: `${circle(7, 6.5, 0.9)}${circle(12, 5.75, 0.9)}${circle(14, 8.25, 0.7)}`,
  },
  music: { stroke: "M7 4.5l9-2V5L7 7ZM7 14.5V7.95M16 12.5V5.95", fill: `${ellipse(5.25, 14.75, 2, 1.5)}${ellipse(14.25, 12.75, 2, 1.5)}` },
  // An album: its sleeve with two beamed notes cut out of it, and the record slid out to its side.
  "music-album": {
    fill: `${rect(2.25, 3.25, 11, 13.5, 0.75)}M14.25 3.8A7.25 7.25 0 0 1 14.25 16.2Z`
      + "M6.5 6.75 11.75 5.5v6.3A1.85 1.4 0 1 1 10.75 10.56V7.33L7.5 8.1v5.2A1.85 1.4 0 1 1 6.5 12.06Z",
  },
  // Notion's music artist (read 2026-11-18), filled: a microphone held up to the right — its head 6.25 across at the top right,
  // its handle slanting down to the left — on a stand a line wide down to the round edge's foot.
  "music-artist": {
    fill: polygon([[11.45, 5.625], [13.125, 8.98], [10.94, 10.07], [10.94, 17.5], [9.06, 17.5], [9.06, 11.01], [6.68, 12.21], [6.26, 11.37], [3.34, 12.83], [2.5, 11.15], [5.42, 9.69], [5, 8.85]])
      + "M15.625 2.5A3.125 3.125 0 0 0 12.56 5.03L14.25 8.43Q14.9 8.75 15.625 8.75A3.125 3.125 0 0 0 15.625 2.5Z",
  },
  // A heading arrow pointing up, cut out of a disc.
  navigation: inCircle("", polygon([[10, 4.75], [14, 14.25], [10, 12.25], [6, 14.25]])),
  necktie: { stroke: "M8.45 2.8h3.1l-.6 1.9h-1.9ZM9.45 7.2h1.1l.95 8.2L10 17.9l-1.5-2.5Z" },
  network: { stroke: `${circle(10, 4.5, 2)}${circle(4.5, 15, 2)}${circle(15.5, 15, 2)}M10 6.5v4M10 10.5l-4.25 3M10 10.5l4.25 3` },
  "new-alert": { stroke: `${burst(12, BADGE_POINTS * BADGE_LINE, BADGE_CORNERS * BADGE_LINE)}M10 6.5v4`, fill: circle(10, 13, 0.85) },
  "new-badge": { fill: burst(12, BADGE_POINTS, BADGE_CORNERS) },
  "new-document": { stroke: `${documentOutline}M9.5 10v5M7 12.5h5` },
  "new-folder": { stroke: `${folderOutline}M10 9.5v5M7.5 12h5` },
  // Its page and back on the square edge.
  news: { stroke: "M4.075 4.075h9.15v10.5a1.35 1.35 0 0 0 1.35 1.35H5.425a1.35 1.35 0 0 1-1.35-1.35ZM13.225 6.5h2.7v8.075a1.35 1.35 0 0 1-2.7 0M6.5 6.75h4.25v2.75H6.5ZM6.5 12.75h4.25" },
  "no-entry": { fill: `${disc}${rect(5.5, 8.75, 9, 2.5, 0.75)}` },
  "note-eighth": { stroke: "M12 14.5V3c.5 2 3 3 3 5.5", fill: noteHead },
  "note-half": { stroke: `M12 14.5V3${noteHead}` },
  "note-quarter": { fill: ellipse(8, 14.9, 2.7, 2.6), stroke: "M10.75 3.3v11.3", weight: 1.6 },
  "note-sixteenth": { stroke: "M12 14.5V3c.5 2 3 3 3 5.5M12 6.5c.5 2 3 3 3 5.5", fill: noteHead },
  // Notion's beamed sixteenths (read 2026-11-18): two note heads 5.625 by 5 at the left foot and lower at the right, their stems
  // a line wide up into two beams rising 0.25 a unit — the upper across both stems, the lower between them.
  "note-sixteenth-beamed": {
    stroke: "M6.5625 4V12.5M15.3125 6V14.6875",
    fill: `${polygon([[5.625, 1.875], [16.25, 4.53], [16.25, 6.47], [5.625, 3.81]])}${polygon([[7.5, 6.04], [14.375, 7.76], [14.375, 9.69], [7.5, 7.97]])}${ellipse(4.6875, 13.4375, 2.8125, 2.5)}${ellipse(13.4375, 15.625, 2.8125, 2.5)}`,
  },
  "note-whole": { stroke: `${ellipse(10, 10, 5.25, 3.75)}M8.5 7.75c1 .75 2 2.75 3 4.5` },
  notification: { stroke: rect(3, 4.5, 12.5, 12.5, 2.5), fill: circle(15, 5, 2.5) },
  numero: { stroke: "M1.8 14.5V5.4l7.25 9.1V5.4M12.15 14.5h6.05", fill: `${circle(15.17, 8.4, 3.97)}${circle(15.17, 8.4, 2)}` },
  // A hazelnut in its husk.
  // Notion's nut (read 2026-11-18), filled: a cap round at its sides from 3.75 over a body narrowing at 45° to a foot cut
  // 2.5 across at the round edge's foot, and the cap's rim cut out of it.
  nut: { fill: "M13.26 3.75H6.73A4.86 4.86 0 0 0 3.3 12.05L8.75 17.5H11.25L16.7 12.05A4.86 4.86 0 0 0 13.26 3.75ZM10 8.75Q6.6 8.75 4.28 6.92A2.98 2.98 0 0 1 6.73 5.625H13.26Q14.4 5.625 15.72 6.92Q13.4 8.75 10 8.75Z" },
  "official-document": {
    stroke: `${documentOutline}M7 9h4M7 11.5h2.5M11.5 15l-.5 2.5 1.5-.75 1.5.75-.5-2.5`,
    fill: circle(12.5, 13, 2.25),
  },
  onion: { fill: circle(9.3, 10.8, 6.6), stroke: "M14.1 6.3l2.5-2.7M4.6 15.4l-1.1 1.1" },
  orange: {
    stroke: `${circle(10, 11, 6)}M10 5V3.5`,
    fill: `M10 5c.5-1.5 2-2.5 4-2.25-.5 1.5-2 2.5-4 2.25Z${circle(8, 10, 0.5)}${circle(12, 12, 0.5)}${circle(9.5, 13.5, 0.5)}`,
  },
  // A body in a ring, its moon on the ring with the ring parted round it.
  orbit: { stroke: "M16.28 8.32A6.5 6.5 0 1 1 11.68 3.72", fill: `${circle(10, 10, 2.75)}${circle(14.6, 5.4, 1.6)}` },
  ornament: { stroke: `${circle(10, 11.5, 5.5)}${rect(8.5, 4.25, 3, 1.75, 0.5)}M10 4.25c0-1 .5-1.5 1.25-1.5M4.75 10h10.5` },
  oven: { stroke: `${rect(3, 2.75, 14, 14.5, 1.5)}M3 7h14M5.5 5h1M8.5 5h1${rect(5.5, 9.25, 9, 5.5, 1)}` },
  package: { stroke: "M10 2.75 16.75 6.5v7L10 17.25 3.25 13.5v-7ZM3.25 6.5 10 10.25l6.75-3.75M10 10.25v7M6.75 4.6l6.75 3.75" },
  paifang: { stroke: "M3 3.5 4 5h12l1-1.5ZM3.5 8.5h13M5.5 5.95V17M14.5 5.95V17M8.5 8.5V5.95M11.5 8.5V5.95M3.75 17h3.5M12.75 17h3.5" },
  "paint-brush": { stroke: "M16.5 3.5 10 10M10 10c-1.75-.5-3.5.5-3.75 2.25-.2 1.4-.75 2.5-2.5 3.25 3.5 1.25 6.75.5 7.5-2.25.4-1.5-.25-2.75-1.25-3.25Z" },
  // Notion's wide brush (read 2026-11-18), filled: a handle with a round top 5 across narrowing to a neck 2.5 wide at 8.125,
  // shoulders out to 4.17..15.83 at 11.25, and under them bristles widening to the round edge's foot, a notch cut in them.
  "paint-brush-wide": {
    fill: "M4.17 11.25L4.34 10.21Q4.6 8.125 6.8 8.125H8.75Q8.75 6.2 7.5 3.75A2.5 2.5 0 0 1 12.5 3.75Q11.25 6.2 11.25 8.125H13.19Q15.4 8.125 15.66 10.21L15.83 11.25Z"
      + polygon([[3.96, 12.5], [16.04, 12.5], [16.875, 17.5], [6.25, 17.5], [6.67, 15], [5.42, 15], [5, 17.5], [3.125, 17.5]]),
  },
  // Tipped over as Notion's (rastered at 400px, 2026-11-18): a bucket square to the diagonal with a hole near its top
  // corner, its handle up to the top left, and paint running from its right corner down and round to fall straight.
  "paint-bucket": {
    // The paint's drip, as Notion's (read 2026-11-18): 1.25 off the bucket's side, bending down, its two ends round — its line
    // stops at each end's middle, a dot a line across there.
    fill: `${polygon([[0.625, 11.25], [8.75, 3.125], [15.625, 10], [7.5, 18.125]])}${circle(9.6, 9.05, 0.9)}${circle(14.64, 5.925, 0.9375)}${circle(18.44, 13.4375, 0.9375)}`,
    stroke: "M3.81 3.16 6.5 5.85M15.3 6.59 17.43 8.715Q18.44 9.73 18.44 11.14V12.5",
  },
  "paint-roller": { stroke: `${rect(3, 3, 11, 4, 1.25)}M14 5h2.5v4.5h-7V12${rect(8.25, 12, 2.5, 5.25, 1)}` },
  "palm-tree": { stroke: "M10 8C8.5 5 5.5 4.25 3 5.5 5.5 5.75 8 6.5 10 8ZM10 8c-2.5-.25-5 1.25-5.75 4 1.75-1.75 3.75-2.75 5.75-4ZM10 8c1.5-3 4.5-3.75 7-2.5-2.5.25-5 1-7 2.5ZM10 8c2.5-.25 5 1.25 5.75 4-1.75-1.75-3.75-2.75-5.75-4ZM10 9.5c.4 2.5.2 5-1 7.5M7 17h6" },
  pants: { stroke: "M5.5 3h9l1 14H12L10 8l-2 9H4.5ZM5.5 5.5h9" },
  "paper-towels": { stroke: `${ellipse(9, 4.5, 4, 1.5)}${ellipse(9, 4.5, 1.25, 0.5)}M5 4.5V15a4 1.5 0 0 0 8 0V4.5M13 8h3v7.5` },
  parking: inCircle(parking),
  "parking-no": inCircle(`${parking}${ringSlash}`),
  "partly-cloudy-day": { stroke: `${lowCloud}M9.75 7.75a3 3 0 0 0-5.25 1.5M6.25 2.75V4M2.5 6.5h1.25M3.6 3.85l.9.9M8.9 3.85l-.9.9` },
  "partly-cloudy-night": { stroke: lowCloud, fill: "M9.25 3a4.25 4.25 0 1 0 4.25 5.25A3.5 3.5 0 0 1 9.25 3Z" },
  // A passport: its cover with a globe and the name line cut out of it.
  passport: { fill: `${rect(3.75, 2.25, 12.5, 15.5, 1.25)}${globeCut(10, 8.5, 4.35)}${rect(7, 14.25, 6, 1.5, 0.75)}` },
};
