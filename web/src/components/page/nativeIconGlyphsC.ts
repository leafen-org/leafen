import {
  EDGE_STEP,
  FIGURE_LINE,
  FULL_EDGE,
  HALF_LINE,
  SQUARE_FAR,
  SQUARE_NEAR,
  bar,
  calendarHead,
  calendarRim,
  circle,
  disc,
  ellipse,
  inCircle,
  n,
  polygon,
  rect,
  ring,
  ringPiece,
  sector,
  smallPlus,
  square,
  type NativeIconGlyph,
} from "./nativeIconGlyphShapes";
import { BUBBLE_MIDDLE, FRAME_LINE, PLATE_CORNER, bubble, bubbleBowl } from "./nativeIconFrames";
import { PERSON_HEAD, person, shouldersInBowl, smallPerson } from "./nativeIconPerson";
import { back, curve, plate, roundQuarters } from "./nativeIconRound";
import { polyline, turned } from "./nativeIconArrows";

// Leafen glyphs for catalog names from "button" through "conifer-tree".

const calendarFrame = `${rect(3, 4.25, 14, 12.75, 1.75)}M3 8.5h14M7 2.75v3M13 2.75v3`;
const camera = "M3 7a1.5 1.5 0 0 1 1.5-1.5h2L8 3.75h4l1.5 1.75h2A1.5 1.5 0 0 1 17 7v8a1.5 1.5 0 0 1-1.5 1.5h-11A1.5 1.5 0 0 1 3 15Z";
// A playing card's plate, as Notion's (read 2026-11-18): filled 4.375..15.625 by the round edge, its corners Notion's round
// (nativeIconRound.ts), the suit cut out of it.
const cardPlate = plate(4.375, 2.5, 15.625, 17.5, PLATE_CORNER);
const chart = "M3 3v14h14";
// Notion's bar chart (read 2026-11-18): a base a line thick across 1.25..18.75 at the foot, and standing on it three bars 3.75
// wide and 1.25 apart, to 10, 3.125 and 7.5; turned a quarter, it lies on its side.
const barChart = (quarters: number) => polygon(([[1.25, 15], ...[[3.125, 10], [8.125, 3.125], [13.125, 7.5]].flatMap(([x, top]) => [[x!, 15], [x!, top!], [x! + 3.75, top!], [x! + 3.75, 15]]),
  [18.75, 15], [18.75, 16.875], [1.25, 16.875]] as Array<[number, number]>).map((point) => turned(point, quarters)));
// Notion's condense (read 2026-11-18): in each corner a bracket turning at 7.5 from the box's side and foot, its arms' ends on
// them, and from its turn a line out to the corner, its end past the box (1.9..3.2).
const condensed = [0, 1, 2, 3].map((quarters) => polyline([[2.5 + HALF_LINE, 12.5], [7.5, 12.5], [7.5, 17.5 - HALF_LINE]], quarters)
  + polyline([[7.5, 12.5], [3.22, 16.78]], quarters)).join("");
// A card's small person (smallPerson): its head a line below the plate's top, its shoulders a line in from the plate's side.
const cardHead = 5 * EDGE_STEP;
const cardPerson = person(2.5 + FRAME_LINE + smallPerson(cardHead).width, 4.375 + FRAME_LINE, cardHead, smallPerson(cardHead));
// A person in a speech bubble: the usual head, its top a head's radius below the bubble's, its shoulders kept within the bubble's inside.
const chatPerson = person(10, 2 * PERSON_HEAD, PERSON_HEAD);
const cloud = "M6 15.5a3.5 3.5 0 0 1-.4-7A4.5 4.5 0 0 1 14.2 7.1a3.75 3.75 0 0 1 .05 8.4Z";
const chessBase = bar(5, 15, 10, 2);
/** The square's line, near and far side, and a third of it: a grid's columns and rows. */
const near = SQUARE_NEAR, far = SQUARE_FAR, third = (far - near) / 3;
/** Where a pin ends for its outside to reach the full edge. */
const pin = n(10 - FULL_EDGE + HALF_LINE);
/** A clover's heart leaf, its point to the middle, turned clockwise by quarters from the top. */
function cloverLeaf(quarters: number) {
  const points = [[10, 9.1], [8.5, 7.9], [7.1, 6.6], [7.25, 4.9], [7.4, 3.3], [9.1, 3], [10, 4.4],
    [10.9, 3], [12.6, 3.3], [12.75, 4.9], [12.9, 6.6], [11.5, 7.9], [10, 9.1]].map(([x, y]) => {
    let at = [x, y];
    for (let turn = 0; turn < quarters; turn += 1) at = [20 - at[1], at[0]];
    return `${n(at[0])} ${n(at[1])}`;
  });
  return `M${points[0]}C${points.slice(1, 4).join(" ")} ${points.slice(4, 7).join(" ")} ${points.slice(7, 10).join(" ")} ${points.slice(10).join(" ")}Z`;
}

export const NATIVE_ICON_GLYPHS_C: Readonly<Record<string, NativeIconGlyph>> = {
  button: inCircle("M6 9.75A4.25 4.25 0 0 1 9.75 6"),
  // Notion's cactus (read 2026-11-18), filled: a stem 5 wide with a round top 5 across from 1.875 to the round edge's foot, an
  // arm 3.125 wide either side with a round top, the left from 5 bending into the stem at 12.5, the right from 8.125 at 15.625.
  cactus: { fill: "M14.0625 9.69A1.5625 1.5625 0 0 1 17.1875 9.69V12.81Q17.1875 15.625 14.375 15.625H12.5V17.5H7.5V12.5H5.625Q2.8125 12.5 2.8125 9.69V6.5625A1.5625 1.5625 0 0 1 5.9375 6.5625V9.375H7.5V4.375A2.5 2.5 0 0 1 12.5 4.375V12.5H14.0625Z" },
  // Notion's cafe (read 2026-11-18), filled: a cup 10 wide from 3.125 to 12.5 with its foot corners and its handle Notion's
  // round 2.5, the handle's hole cut out, on a saucer across the box at 13.75 narrowing to the round edge's foot.
  cafe: {
    fill: `M16.25 3.125H5V10${curve(back(roundQuarters(7.5, 10, 2.5)[2]))}H12.5${curve(back(roundQuarters(12.5, 10, 2.5)[1]))}H16.25${curve(back(roundQuarters(16.25, 7.5, 2.5)[1]))}V5.625${curve(back(roundQuarters(16.25, 5.625, 2.5)[0]))}Z`
      + "M16.875 7.5Q16.875 8.125 16.25 8.125H15V5H16.25Q16.875 5 16.875 5.625ZM1.25 13.75H18.75V15.625H16.41L16.09 17.5H3.91L3.59 15.625H1.25Z",
  },
  cake: { fill: `M10 .65c.75.9 1.15 1.6 1.15 2.25a1.15 1.15 0 0 1-2.3 0c0-.65.4-1.35 1.15-2.25Z${bar(9.6, 3.6, 0.8, 4.2)}`, stroke: "M4.7 10.9v-.55a2 2 0 0 1 2-2h6.6a2 2 0 0 1 2 2v.55ZM4.7 14.05h10.6v1.9H4.7Z" },
  calculator: {
    stroke: `${rect(4, 2.75, 12, 14.5, 1.75)}${rect(6.25, 4.75, 7.5, 3, 0.5)}`,
    fill: [10.25, 12.5, 14.75].flatMap((y) => [7, 10, 13].map((x) => circle(x, y, 0.8))).join(""),
  },
  "calendar-day": { stroke: calendarFrame, fill: rect(8.25, 10.5, 3.5, 3.5, 0.75) },
  "calendar-month": {
    stroke: calendarFrame,
    fill: [11, 14].flatMap((y) => [6.5, 10, 13.5].map((x) => circle(x, y, 0.85))).join(""),
  },
  "calendar-week": { stroke: calendarRim, fill: `${calendarHead}${[5.15, 8.7, 12.25].map((x) => bar(x, 10.25, 2.6, 4.5)).join("")}` },
  camera: { stroke: `${camera}${circle(10, 11, 3)}` },
  "camera-off": { stroke: `${camera}${circle(10, 11, 3)}M3 3l14 14` },
  "camera-roll": { stroke: `${rect(2.75, 6, 11.5, 8.5, 1.5)}M6 3.75h9.75a1.5 1.5 0 0 1 1.5 1.5v7` },
  "camera-roll-portrait": { stroke: `${rect(4, 5.5, 8.5, 11.5, 1.5)}M7 2.75h7a1.5 1.5 0 0 1 1.5 1.5v9.5` },
  // Notion's tent (read 2026-11-18), filled: a tent rising at 2:1 to a flat top 1.875 across at 3.125, on a ground a line
  // deep across 1.25..18.75, its door a like triangle from 9.375 cut out of it.
  "camping-tent": { fill: `${polygon([[16.875, 15], [10.94, 3.125], [9.06, 3.125], [3.125, 15], [1.25, 15], [1.25, 16.875], [18.75, 16.875], [18.75, 15]])}${polygon([[7.19, 15], [10, 9.375], [12.81, 15]])}` },
  "camping-trailer": { stroke: "M2.8 13.45v-3.4a3.5 3.5 0 0 1 3.5-3.5h7.75v6.9ZM15.4 13.45h3.05", fill: `${bar(10.6, 7.45, 3.4, 3.15)}${circle(7.4, 15.35, 1.55)}` },
  cancer: { stroke: `M4.5 8.5a5.5 3.5 0 0 1 10.5-1.5M15.5 11.5A5.5 3.5 0 0 1 5 13${circle(5.5, 8.5, 1.75)}${circle(14.5, 11.5, 1.75)}` },
  // Notion's candy (read 2026-11-18), filled: a sweet 11.25 across in the middle with a swirl cut in it, and its wrapper's two
  // ends flaring out to the box's sides, their ends crinkled.
  candy: {
    fill: `${circle(10, 10, 5.625)}M11.875 12.5Q11.875 10.8 10.32 10.65Q9.4 11.94 7.96 11.94Q7.1 11.94 6.27 11.46L6.9 10.38Q7.4 10.69 7.96 10.69Q8.8 10.69 9.28 9.96Q8.97 9.3 8.97 8.62Q8.97 6.95 10.6 6.04L11.23 7.13Q10.22 7.7 10.22 8.63Q10.22 9 10.4 9.39Q13.125 9.7 13.125 12.5Z`
      + "M4.87 7.71Q3 5.78 0.625 5.78V7.19Q1.5625 7.19 1.5625 8.125Q1.5625 9.06 0.625 9.06V10.94Q1.5625 10.94 1.5625 11.875Q1.5625 12.81 0.625 12.81V14.22Q3 14.22 4.87 12.29A5.625 5.625 0 0 1 4.87 7.71ZM15.13 7.71Q17 5.78 19.375 5.78V7.19Q18.4375 7.19 18.4375 8.125Q18.4375 9.06 19.375 9.06V10.94Q18.4375 10.94 18.4375 11.875Q18.4375 12.81 19.375 12.81V14.22Q17 14.22 15.13 12.29A5.625 5.625 0 0 0 15.13 7.71Z",
  },
  capricorn: { stroke: "M3.5 5.5c1.5 0 2.5 1 2.5 2.5v7M6 8c0-1.5 1-2.75 2.5-2.75C10 5.25 11 6.5 11 8v5.5c0 1.5 1 2.5 2.5 2.5a2.25 2.25 0 1 0-2.25-2.25" },
  car: { stroke: `M2.75 13.5v-3l1.75-.5 1.75-3.25A1.5 1.5 0 0 1 7.6 6h4.9a1.5 1.5 0 0 1 1.3.75L15.5 10l1.75.5v3ZM4.5 10h11${circle(6, 13.75, 1.6)}${circle(14, 13.75, 1.6)}` },
  // A plate with a person and two lines of writing cut out of it, the lines two grid steps deep and ending a line in from the plate's side.
  card: {
    fill: `${plate(2.5, 4.375, 17.5, 15.625, PLATE_CORNER)}${cardPerson.head}${cardPerson.outline}`
      + `${bar(10.625, 8.125, 17.5 - FRAME_LINE - 10.625, 1.25)}${bar(11.875, 10.625, 17.5 - FRAME_LINE - 11.875, 1.25)}`,
  },
  "card-club": {
    fill: `${cardPlate}${circle(10, 7.9, 1.5)}${circle(8.1, 10.5, 1.5)}${circle(11.9, 10.5, 1.5)}${polygon([[10, 10], [9, 13.25], [11, 13.25]])}`,
  },
  "card-diamond": { fill: cardPlate + polygon([[10, 6.5], [12.5, 10], [10, 13.5], [7.5, 10]]) },
  "card-heart": { fill: cardPlate + "M10 13.5s-3-1.8-3-4a1.6 1.6 0 0 1 3-.9 1.6 1.6 0 0 1 3 .9c0 2.2-3 4-3 4Z" },
  "card-spade": {
    fill: `${cardPlate}M10 6.25s3 2.25 3 4.25a1.6 1.6 0 0 1-3 .75 1.6 1.6 0 0 1-3-.75c0-2 3-4.25 3-4.25Z${polygon([[10, 11], [9, 13.5], [11, 13.5]])}`,
  },
  cards: {
    stroke: `${rect(3, 5.25, 9, 11.75, 1.5)}M7 5.25V4a1.25 1.25 0 0 1 1.25-1.25h7.5A1.25 1.25 0 0 1 17 4v10a1.25 1.25 0 0 1-1.25 1.25H12`,
    fill: "M7.5 13s-2.25-1.35-2.25-3a1.2 1.2 0 0 1 2.25-.65A1.2 1.2 0 0 1 9.75 10c0 1.65-2.25 3-2.25 3Z",
  },
  carrot: { stroke: "M12.75 7.25c-2-2-4.5-1.25-6 1.25L3 16.5l8-3.75c2.5-1.5 3.25-4 1.75-5.5ZM12.75 7.25 16.5 3.5M13.5 5.5l2.5-.25M14.5 6.5l.25 2.25M7 11l1.25 1.25M8.75 8.75 10 10" },
  // A note with a notch in each end and a portrait in an oval cut out of it.
  cash: {
    fill: `${rect(2.5, 5, 15, 10, 1)}M2.5 8.75a1.25 1.25 0 0 1 0 2.5ZM17.5 8.75a1.25 1.25 0 0 0 0 2.5Z${ellipse(10, 10, 3.25, 3)}${ellipse(10, 10, 1.75, 1.6)}`,
  },
  "cash-register": { stroke: "M3 17v-4.5h14V17ZM4.5 12.5l1-5h9l1 5M7 7.5V4h6v3.5M8.5 10h3M8.5 14.75h3" },
  // A cassette with its reels' window cut out and the tape between them in it.
  cassette: { fill: `${rect(2.5, 5, 15, 10, 1)}${rect(4.75, 7.75, 10.5, 4.5, 2.25)}${rect(7.75, 9, 4.5, 2, 0.5)}` },
  // Two battlemented towers and the wall between them, an arched gate in it.
  castle: { fill: `M2.5 17.5V3h1.6v1.5h1.3V3H7v4.5h2.25V6h1.5v1.5H13V3h1.6v1.5h1.3V3h1.6v14.5ZM8 17.5v-3a2 2 0 0 1 4 0v3Z${bar(4.25, 8, 1.25, 2.5)}${bar(14.5, 8, 1.25, 2.5)}` },
  "castle-japanese": { stroke: "M4 7.5h12L14 5H6ZM3 11.5h14L14.5 9h-9ZM5 11.5V17h10v-5.5M9 17v-3h2v3M8.5 5V3.5h3V5" },
  // Notion's cat (read 2026-11-18), filled, sitting in profile: its head with two ears at the top right, its back level at 7.5,
  // its tail curling up from its rump at the top left, and its legs to the round edge's foot.
  cat: { fill: "M15.625 5V3.125H14.53L12.19 7.5H4.54L4.01 6.44A2.54 2.54 0 0 1 3.75 5.32A2.51 2.51 0 0 1 6.25 2.81H7.65V1.25H6.25A4.07 4.07 0 0 0 2.61 7.14L3.125 8.15V9.18Q3.125 10.2 3.83 10.81Q4.375 11.4 4.375 12.33V12.5L3.125 14.375L3.28 14.85Q3.8 16.4 4.79 17.29L5 17.5H6.875V16.56Q6.875 15.625 5.94 15.625H5.39L5 14.84L8.125 12.5H11.25V15L11.34 15.45Q11.6 16.7 12.36 17.37L12.5 17.5H14.375V16.56Q14.375 15.625 13.43 15.625H13.125V12.5L14.96 10.66Q15.625 10 15.625 9.06Q15.625 8.125 16.56 8.125H17.5L18.125 6.875L15.625 5Z" },

  categories: { stroke: `${rect(3, 9, 14, 7.5, 1.5)}M4.5 6.5h11M6 4h8` },
  cd: inCircle(`${circle(10, 10, 2)}M10 5.5a4.5 4.5 0 0 1 4.5 4.5`),
  cellular: {
    stroke: "M10 10.5V17M6.75 5.75a4.5 4.5 0 0 0 0 6.5M13.25 5.75a4.5 4.5 0 0 1 0 6.5M4.25 3.5a7.75 7.75 0 0 0 0 11M15.75 3.5a7.75 7.75 0 0 1 0 11",
    fill: circle(10, 9, 1.25),
  },
  // Notion's chair (read 2026-11-18), filled: a back 7.5 wide from the round edge's top, its top bowed, to 9.375; and under it a
  // seat on 10.625..14.375 between arms 1.875 wide with round ends 2.5 across from 6.25, on four legs to 16.25.
  chair: {
    fill: "M16.875 6.25A1.25 1.25 0 0 1 16.875 8.75V14.375H15V16.25H13.125V14.375H6.875V16.25H5V14.375H3.125V8.75A1.25 1.25 0 0 1 3.125 6.25H5V10.625H15V6.25Z"
      + "M13.75 4.32Q13.75 3.5 13.17 3.27Q11.9 2.5 10 2.5Q8.1 2.5 6.83 3.27Q6.25 3.5 6.25 4.32V9.375H13.75Z",
  },
  champagne: { stroke: "M7.4 3.45h5.2l1 3.6c.45 2-.85 3.6-3.6 3.6s-4.05-1.6-3.6-3.6ZM8.3 5h3.4M7.55 16.55h4.9", fill: bar(9.4, 11, 1.2, 5) },
  // Notion's champagne (read 2026-11-18), filled: a bottle 7.5 wide at the left, its cap 3.75 wide from 1.25 over a neck 2.5
  // wide to 5.625, its shoulders slanting out at 45° to 10.19, its foot corners round 1.25, a label cut out of it with a notch
  // to 10.625; and at the right a flute narrowing up to 4.61 across at the round edge's top, its bowl's foot round into a stem
  // 1.875 wide over a foot from 15.625, a band cut across its top.
  "champagne-bottle": {
    fill: `M6.875 5.625V3.125H7.5V1.25H3.75V3.125H4.375V5.625L3.34 6.66Q1.875 8.1 1.875 10.19V16.25${curve(back(roundQuarters(3.125, 16.25, 1.25)[2]))}`
      + `H8.125${curve(back(roundQuarters(8.125, 16.25, 1.25)[1]))}V10.19Q9.375 8.1 7.91 6.66Z`
      + "M7.5 15.625H3.75V10.2Q3.75 9.5 3.96 8.96L5.625 10.625L7.29 8.96Q7.5 9.5 7.5 10.2Z"
      + "M18.125 8.75Q18.125 8.27 18.01 7.8L16.68 2.5H12.07L10.74 7.8Q10.62 8.27 10.62 8.75Q10.62 11.9 13.43 12.43V15.625H10.93V17.5H17.81V15.625H15.31V12.43Q18.125 11.9 18.125 8.75Z"
      + polygon([[15.22, 4.375], [15.53, 5.625], [13.22, 5.625], [13.53, 4.375]]),
  },
  chart: { fill: barChart(0) },
  "chart-alternate": { fill: barChart(1) },
  "chart-area": { stroke: chart, fill: "M4.75 15.25V11l3.25-3.5 3 2.5 4.75-5.25v10.5Z" },
  // A ring cut into its shares.
  "chart-donut": { fill: `${ringPiece(5, 175, 7.5, 4)}${ringPiece(185, 265, 7.5, 4)}${ringPiece(275, 355, 7.5, 4)}` },
  "chart-line": { stroke: `${chart}M5.5 13l3.25-4 3 2.5 4.25-6` },
  "chart-mixed": {
    stroke: "M4 7.5 8.5 5l4 2.5L16.5 4",
    fill: `${rect(4.25, 11, 2.5, 6, 0.6)}${rect(8.75, 9, 2.5, 8, 0.6)}${rect(13.25, 12, 2.5, 5, 0.6)}`,
  },
  "chart-pie": { fill: sector(0.75, 7, 9.5, 10.5), stroke: "M11 9V2.5a6.5 6.5 0 0 1 6.5 6.5Z" },
  chat: { fill: bubble },
  "chat-user": { fill: `${bubble}${chatPerson.head}${shouldersInBowl(chatPerson.shoulders, bubbleBowl)}` },
  // A bank cheque: its payee and amount lines.
  check: { stroke: "M2.2 5.95h15.6v8.1H2.2ZM4 7.6h3.3M4 10h5.4M12.8 7.6h2.3M12.5 12.4h3" },
  checklist: { stroke: "M3 5.25l1.25 1.25L6.5 4M3 10.25l1.25 1.25L6.5 9M3 15.25l1.25 1.25L6.5 14M9 5.5h8M9 10.5h8M9 15.5h8" },
  // Notion's flask (read 2026-11-18), filled: a rim across 5.625..14.375 at the round edge's top, a neck 5.625 wide to 8.33,
  // a body widening at 4:3 to round feet on the round edge's foot, and its liquid line and neck's inside cut out.
  chemistry: {
    fill: "M16.31 13L12.81 8.33V4.375H14.375V2.5H5.625V4.375H7.19V8.34L3.69 13A2.83 2.83 0 0 0 3.125 14.69A2.82 2.82 0 0 0 5.94 17.5H14.06A2.82 2.82 0 0 0 16.875 14.69Q16.875 13.7 16.31 13Z"
      + "M9.06 8.96V4.375H10.94V8.96L13.125 11.875H6.875Z",
  },
  cherries: {
    fill: `${circle(6.5, 14, 2.5)}${circle(13.25, 13.25, 2.5)}`,
    stroke: "M6.5 11.5c.5-3.5 2.5-6.25 5.5-8.5M13.25 10.75C13 7.75 12.5 5.25 12 3M12 3l3 .75",
  },
  "chess-bishop": { fill: `M10 2.5a1 1 0 0 1 .6 1.8c1.6 1 2.9 2.9 2.9 4.7 0 1.4-.8 2.4-1.75 2.9l.75 2.6h-5l.75-2.6C7.3 11.4 6.5 10.4 6.5 9c0-1.8 1.3-3.7 2.9-4.7A1 1 0 0 1 10 2.5Z${chessBase}` },
  // A king: the cross on its crown, the crown's two rounded lobes with an arch cut out of each, its waist and base.
  "chess-king": {
    fill: "M9.25 2.25h1.5v1.25H12V5h-1.25v1.6c.8-.55 1.6-.85 2.4-.85 2 0 3.45 1.5 3.45 3.45 0 1.8-1.05 3-2.5 3.7l-.5 1.6H6.4l-.5-1.6c-1.45-.7-2.5-1.9-2.5-3.7 0-1.95 1.45-3.45 3.45-3.45.8 0 1.6.3 2.4.85V5H8V3.5h1.25Z"
      + `M5.25 11.5V9.4a1.75 1.75 0 0 1 3.5 0v2.1ZM11.25 11.5V9.4a1.75 1.75 0 0 1 3.5 0v2.1Z${chessBase}`,
  },
  "chess-knight": { fill: `M6 15v-.25c.5-2 1.75-3.25 1.75-5L6.5 11 5 9.5l3.5-5 1-2 1 1.75c3 .25 5 2.75 5 6.25V15Z${chessBase}` },
  "chess-pawn": { fill: `M10 3a2 2 0 0 1 1.2 3.6c.9.5 1.3 1.3 1.3 2.2h-5c0-.9.4-1.7 1.3-2.2A2 2 0 0 1 10 3ZM8.25 9.5h3.5l1 5h-5.5Z${chessBase}` },
  "chess-queen": {
    fill: `${polygon([[4, 7], [6.75, 11.5], [8, 5.75], [10, 11], [12, 5.75], [13.25, 11.5], [16, 7], [14.75, 14.25], [5.25, 14.25]])}${chessBase}${circle(4, 6.25, 1)}${circle(8, 5, 1)}${circle(12, 5, 1)}${circle(16, 6.25, 1)}`,
  },
  "chess-rook": { fill: `M5.5 3h2v1.75H9V3h2v1.75h1.5V3h2v4L13 8.25 13.5 14h-7L7 8.25 5.5 7Z${chessBase}` },
  // Notion's chicken (read 2026-11-18), filled, in profile: its head and beak at the top right, its neck sloping down to its
  // back, its tail up at the left, its body round to its legs and feet on the round edge's foot; rounds in Notion's round.
  chicken: { fill: `M17.5 3.75V6.25H15.625V9.375Q15.625 10.6 14.63 11.38L10.625 14.375V16.25H11.875V17.5H8.125V16.25H9.375V14.375H8.125${curve(roundQuarters(8.125, 11.875, 2.5)[2])}H5${curve(roundQuarters(5, 9.375, 2.5)[2])}`
    + `V4.375H3.75${curve(roundQuarters(3.75, 6.875, 2.5)[0])}V8.125H11.25L13.03 5.01Q13.7 3.75 15.2 3.75H15.625V2.5H16.25${curve(roundQuarters(16.25, 3.75, 1.25)[0])}Z` },

  child: { stroke: "M10 7v5M6.5 9l3.5-1 3.5 1M10 12l-2 4.5M10 12l2 4.5", fill: circle(10, 4.5, 1.75), weight: FIGURE_LINE },
  // Notion's chili (read 2026-11-18), filled: a pod curving from its stalk at the top right down and round to a tip at the left
  // on 13.125..15.625, and its stalk bending up to the right at the round edge's top.
  "chili-pepper": { fill: "M18.75 2.5V4.375H18.07Q17.1 4.375 16.51 5.21L15.625 6.54Q16.875 7.7 16.875 9.06C16.875 13.6 12 17.5 7.5 17.5Q3 17.5 1.25 15.625L1.875 13.125C9.6 13.125 8.4 5.625 13.28 5.625Q13.62 5.625 13.93 5.69L14.95 4.17Q16 2.5 18.07 2.5Z" },
  "christmas-tree": { fill: `M10 2.5 5.5 7.75h2.25L4.25 12h3L3.75 16h12.5L12.75 12h3l-3.5-4.25h2.25Z${bar(9.1, 16, 1.8, 1.75)}` },
  church: { stroke: "M10 2.25v3.5M8.5 3.75h3M5 17V9.5l5-3.75 5 3.75V17ZM3 17h14M8.5 17v-3a1.5 1.5 0 0 1 3 0v3" },
  // Notion's cigarette (read 2026-11-18): a cigarette across 2.5..17.5 on 14.375..16.875 with two bands cut across its right
  // end, and two curls of smoke 1.25 wide rising from it, each looping round to the left at the top.
  cigarette: {
    weight: 1.25,
    stroke: "M7.5 1.25H7.19A2.81 2.81 0 0 0 7.19 6.875H9.375A1.875 1.875 0 0 0 9.375 10.625H13.125A1.25 1.25 0 0 1 14.375 11.875V12.5"
      + "M10 3.125H11.875A2.5 2.5 0 0 1 11.875 8.125H13.125A3.75 3.75 0 0 1 16.875 11.875V12.5",
    fill: `${bar(2.5, 14.375, 10, 2.5)}${bar(13.75, 14.375, 1.25, 2.5)}${bar(16.25, 14.375, 1.25, 2.5)}`,
  },
  "circle-arrows-horizontal": inCircle("M6 10h8M8 8l-2 2 2 2M12 8l2 2-2 2"),
  "circle-arrows-vertical": inCircle("M10 6v8M8 8l2-2 2 2M8 12l2 2 2-2"),
  city: {
    stroke: "M2.75 17h14.5M4 16.05V8.5h4v7.55ZM8 16.05V4h5v12.05ZM13 16.05V11h3v5.05Z",
    fill: `${bar(9.25, 6, 1.25, 1.25)}${bar(11, 6, 1.25, 1.25)}${bar(9.25, 8.75, 1.25, 1.25)}${bar(11, 8.75, 1.25, 1.25)}${bar(5.25, 10.5, 1.25, 1.25)}`,
  },
  clarinet: { stroke: "M.95 10.25H15.75M4.3 9V7.8M6.8 9V7.8M17.2 7.8h1.85v4.95H17.2c0-1.4-.5-2.5-.5-2.5s.5-1.05.5-2.45Z" },
  // Its page on the square edge.
  clipping: { stroke: "M4.075 4.075h8.35l3.5 3.5v8.35H4.075ZM12.425 4.075v3.5h3.5M6.5 10h7M6.5 13h5" },
  clock: inCircle("M10 5.5V10h3.5"),
  "clothes-button": { fill: `${disc}${circle(8.25, 8.25, 1)}${circle(11.75, 8.25, 1)}${circle(8.25, 11.75, 1)}${circle(11.75, 11.75, 1)}` },
  "clothes-iron": {
    stroke: "M2.75 15.25h14.5v-3.5A5.25 5.25 0 0 0 12 6.5H7.5c-2.5 0-4.75 4-4.75 8.75ZM9 6.5V4h6a2 2 0 0 1 2 2v1.5",
    fill: `${circle(7, 13, 0.6)}${circle(10, 13, 0.6)}${circle(13, 13, 0.6)}`,
  },
  cloud: { stroke: cloud },
  "cloud-no": { stroke: `${cloud}M8.25 9.5l3.5 3.5M11.75 9.5l-3.5 3.5` },
  "cloud-off": { stroke: `${cloud}M3 3l14 14` },
  "cloud-yes": { stroke: `${cloud}M7.75 11l1.5 1.5 3-3` },
  // A heavy cloud, the weather's.
  cloudy: { stroke: "M5.25 15.5a3 3 0 0 1-.25-6 3.5 3.5 0 0 1 4.5-3 4 4 0 0 1 7 3.4 3 3 0 0 1-1.75 5.6Z" },
  clover: { fill: "M10 6.77A2.4 2.4 0 0 1 6.1 4.9A2.4 2.4 0 0 1 10 3.03A2.4 2.4 0 0 1 13.9 4.9A2.4 2.4 0 0 1 10 6.77ZM3.03 10A2.4 2.4 0 0 1 4.9 6.1A2.4 2.4 0 0 1 6.77 10A2.4 2.4 0 0 1 4.9 13.9A2.4 2.4 0 0 1 3.03 10ZM13.23 10A2.4 2.4 0 0 1 15.1 6.1A2.4 2.4 0 0 1 16.97 10A2.4 2.4 0 0 1 15.1 13.9A2.4 2.4 0 0 1 13.23 10Z", stroke: "M10 10V7.3M10 10H7.3M10 10h2.7M10 10c1.9 1.6 3.3 3.85 3.5 6.55" },
  // Four heart leaves, their points to the middle, the stem between two of them.
  "clover-four-leaf": { fill: [0, 1, 2, 3].map(cloverLeaf).join(""), stroke: "M11.25 11.25c1.5 1.25 2.75 3 3.5 5.5" },
  // A glass on its coaster.
  coaster: { stroke: "M6.5 3.25h7l-1 10h-5ZM7.3 6.75h5.4", fill: rect(3, 15, 14, 2, 1) },
  // Notion's coat (read 2026-11-18), filled: alike either side of the middle — a collar rising to 1.25, shoulders on 2.5 out to
  // 13.96, sleeves slanting down and out at 3:1 to the box's edge, cuffs, the body's sides down to the round edge's foot and
  // its fronts 1.25 apart, their lapels turning in at 45° to the collar.
  coat: {
    fill: "M10 1.25Q12.2 1.3 13.125 2.5H13.96Q15.3 2.6 15.74 3.78L19.06 13.75L16.25 14.69L14.375 9.06V17.5H10.625V6.82Q10.625 5.8 11.36 5.05L12.19 4.22Q11.5 3.12 10 3.12"
      + "Q8.5 3.12 7.81 4.22L8.64 5.05Q9.375 5.8 9.375 6.82V17.5H5.625V9.06L3.75 14.69L0.94 13.75L4.26 3.78Q4.7 2.6 6.04 2.5H6.875Q7.8 1.3 10 1.25Z",
  },
  cocktail: { stroke: "M3.5 3.5h13L10 10.5ZM10 10.5v6M6.75 17h6.5M8.5 5.5l3.5-3", fill: circle(8.5, 5.5, 1) },
  "code-scan": {
    // Its corner marks and code on the square edge.
    stroke: `${rect(4.075, 4.075, 4, 4, 0.85)}${rect(11.925, 4.075, 4, 4, 0.85)}${rect(4.075, 11.925, 4, 4, 0.85)}`,
    fill: `${bar(5.375, 5.375, 1.4, 1.4)}${bar(13.225, 5.375, 1.4, 1.4)}${bar(5.375, 13.225, 1.4, 1.4)}${bar(11.25, 11.25, 1.75, 1.75)}${bar(15.125, 15.125, 1.75, 1.75)}${bar(11.25, 15.375, 1.5, 1.5)}${bar(15.125, 11.25, 1.75, 1.75)}`,
  },
  // Notion's coffee to go (read 2026-11-18), filled: a lid across 2.5..17.5 on 4.375..6.25 with its top 9.68 across from the round
  // edge's top, a cup narrowing to 8.125 across at its foot, and two bands cut across it under the lid and over the foot.
  coffee: {
    fill: polygon([[17.5, 4.375], [15.16, 4.375], [14.84, 2.5], [5.16, 2.5], [4.84, 4.375], [2.5, 4.375], [2.5, 6.25], [4.06, 6.25], [5.94, 17.5], [14.06, 17.5], [15.94, 6.25], [17.5, 6.25]])
      + polygon([[7.53, 15.625], [7.21, 13.75], [12.78, 13.75], [12.47, 15.625]]) + polygon([[13.72, 8.125], [6.28, 8.125], [5.96, 6.25], [14.03, 6.25]]),
  },
  "coffee-maker": { stroke: "M4 2.75h12v4H4ZM5 6.75V17h10M15 6.75v2.5M7.5 11.5h5l-.5 4.75h-4Z", fill: circle(12.75, 4.75, 0.7) },
  "color-palette": {
    stroke: "M10 3c4 0 7 2.75 7 6.25 0 2.2-1.75 3.25-3.5 3.25h-1.25a1.5 1.5 0 0 0-1.1 2.5c.9 1 .25 2.25-1.15 2.25C6 17.25 3 14 3 10c0-3.9 3.1-7 7-7Z",
    fill: `${circle(6.75, 9.25, 1)}${circle(8.75, 6.25, 1)}${circle(12.25, 6.5, 1)}`,
  },
  "color-picker": { stroke: "M11 5.5l3.5 3.5M12.75 3.75a1.75 1.75 0 0 1 2.5 0l1 1a1.75 1.75 0 0 1 0 2.5l-1.75 1.75-3.5-3.5ZM11.5 7.25 4.25 14.5 3.5 16.5l2-.75 7.25-7.25" },
  // Notion's colour swatch (read 2026-11-18), filled: a card 11.25 by 15 on the round edge, its colour's window 7.5 by 3.75
  // cut out near its foot.
  "color-swatch": { fill: `${bar(4.375, 2.5, 11.25, 15)}${bar(6.25, 11.875, 7.5, 3.75)}` },

  column: { stroke: `${square}M${n(near + third)} ${near}V${far}M${n(far - third)} ${near}V${far}` },
  // Notion's comb (read 2026-11-18), filled, standing: a spine 3.125 wide from 1.875 to 18.125, its top left corner Notion's
  // round 2.5, and four teeth 1.875 deep out to 14.375 from its top, 1.25 apart, the last rounding into the spine.
  comb: { fill: `M9.375 4.375V5H14.375V6.875H9.375V8.125H14.375V10H9.375V11.25H14.375V13.125H10.625${curve(back(roundQuarters(10.625, 14.375, 1.25)[3]))}V18.125H6.25V4.375`
    + `${curve(roundQuarters(8.75, 4.375, 2.5)[3])}H14.375V3.75H10${curve(back(roundQuarters(10, 4.375, 0.625)[3]))}Z` },
  "command-line": { stroke: `${rect(2.75, 3.75, 14.5, 12.5, 1.75)}M6 8l2.25 2L6 12M10 12.5h4` },
  // A plus cut out of the bubble about its body's middle.
  comment: { fill: `${bubble}${smallPlus(10, BUBBLE_MIDDLE)}` },
  compass: { stroke: ring, fill: polygon([[13.25, 6.75], [11.1, 11.1], [6.75, 13.25], [8.9, 8.9]]) },
  compose: { stroke: "M9 3.5H5A1.5 1.5 0 0 0 3.5 5v10A1.5 1.5 0 0 0 5 16.5h10a1.5 1.5 0 0 0 1.5-1.5v-4M14.25 3.25l2.5 2.5-6.25 6.25-3 .5.5-3Z" },
  "compressed-document": { stroke: "M5.5 2.75h5.75L14.5 6v10.5a.75.75 0 0 1-.75.75H5.5a.75.75 0 0 1-.75-.75v-13a.75.75 0 0 1 .75-.75ZM8.5 4.5h1M9.5 6h1M8.5 7.5h1M9.5 9h1M8.75 11h1.5v2.25h-1.5Z" },
  computer: { stroke: `${rect(2.75, 3.5, 14.5, 10, 1.5)}M10 13.5v3M6.75 16.75h6.5` },
  "computer-chip": {
    // Its pins' ends on the full edge.
    stroke: `${rect(5, 5, 10, 10, 1.5)}M8 ${pin}V5M12 ${pin}V5M8 15V${20 - pin}M12 15V${20 - pin}M${pin} 8H5M${pin} 12H5M15 8H${20 - pin}M15 12H${20 - pin}`,
    fill: `${rect(7.1, 7.1, 5.8, 5.8, 0.75)}${circle(8.75, 8.75, 0.7)}`,
  },
  // Notion's conceal (read 2026-11-18), filled: a hat — its crown rising to two humps at the round edge's top, its brim across
  // 1.25..18.75 on 8.75..10.625 — over dark glasses across 2.5..17.5 from 11.875, their lenses down to the round edge's foot,
  // a glint cut in each.
  conceal: {
    fill: `M4.4 8.75Q4.8 2.5 7.81 2.5Q8.9 2.5 10 3.125Q11.1 2.5 12.19 2.5Q15.2 2.5 15.6 8.75Z${bar(1.25, 8.75, 17.5, 1.875)}`
      + "M2.5 11.875H17.5V13.75H16.25L15.98 15.34Q15.75 17.5 13.44 17.5Q11.12 17.5 10.89 15.34L10.73 14.37Q10.66 13.75 10 13.75Q9.34 13.75 9.27 14.37L9.11 15.34Q8.88 17.5 6.56 17.5Q4.25 17.5 4.02 15.34L3.75 13.75H2.5Z"
      + `${polygon([[12.19, 16.25], [13.44, 16.25], [14.69, 13.75], [13.44, 13.75]])}${polygon([[5.31, 16.25], [6.56, 16.25], [7.81, 13.75], [6.56, 13.75]])}`,
  },

  condense: { stroke: condensed },
  // Notion's confetti ball (read 2026-11-18), filled: a ball split open into two halves tipped apart at the top, and confetti
  // falling under them — two dots, two short bars and three curls.
  "confetti-ball": {
    fill: `M1.875 4.75A3.91 3.91 0 0 1 8.875 8.25ZM18.125 4.75A3.91 3.91 0 0 0 11.125 8.25Z${circle(9.69, 14.69, 0.94)}${circle(16.56, 8.44, 0.94)}`
      + `${polygon([[5.875, 14.375], [8.125, 16.62], [7.24, 17.5], [5, 15.26]])}${polygon([[14.12, 12.5], [15, 11.62], [12.76, 9.375], [11.875, 10.26]])}`
      + "M5 10Q5 8.75 6.25 8.75V7.5Q3.75 7.5 3.75 10V10.625Q3.75 11.875 2.5 11.875V13.125Q5 13.125 5 10.625ZM6.25 11.875V13.125Q10 13.125 10 9.375H8.75Q8.75 11.875 6.25 11.875Z"
      + "M15 13.75H14.375Q11.875 13.75 11.875 16.25H13.125Q13.125 15 14.375 15H15Q17.5 15 17.5 12.5H16.25Q16.25 13.75 15 13.75Z",
  },

  "confetti-party-popper": {
    stroke: "M3 17 6.25 7.75l6 6ZM10 3v1.5M13.5 5.5l1-1M15 9h1.5M11.5 7.5l.5-.5",
    fill: `${circle(16, 4, 0.75)}${circle(14.5, 12, 0.75)}${circle(8.5, 5.5, 0.6)}`,
  },
  "conifer-tree": { fill: `M10 2.5 15.25 14H4.75Z${bar(9.1, 14, 1.8, 3.25)}` },
};
