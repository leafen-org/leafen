import {
  EDGE_STEP,
  HALF_LINE,
  bar,
  rainCloud,
  circle,
  ellipse,
  heart,
  inCircle,
  n,
  polygon,
  rect,
  regular,
  ring,
  ringSlash,
  sunglassesShape,
  type NativeIconGlyph,
} from "./nativeIconGlyphShapes";
import { chevron, headPoint, polyline, turned } from "./nativeIconArrows";
import { back, curve, plate, roundQuarters } from "./nativeIconRound";
import { BUBBLE_MIDDLE, TAB_FRONT, bubble, tabs } from "./nativeIconFrames";
import { NECK, person, shouldersAbove, smallPerson } from "./nativeIconPerson";

// Leafen glyphs for catalog names from "shirt" through "thumbs-down".

const swap = (quarters: number) => {
  const tip = 2.58, end = headPoint(tip);
  return `${polyline([[3.125 + HALF_LINE, 6.25], [20 - end, 6.25]], quarters)}${chevron(tip, 4.06, quarters + 1, 6.25)}`
    + `${polyline([[16.875 - HALF_LINE, 13.75], [end, 13.75]], quarters)}${chevron(tip, 4.06, quarters + 3, 6.25)}`;
};
const snowArms = (() => {
  const point = 6.95 - HALF_LINE * Math.SQRT2, arm = 3.12 - HALF_LINE * Math.SQRT2;
  const up: Array<Array<[number, number]>> = [[[10, 10], [10, point]], [[10 - arm, point - arm], [10, point], [10 + arm, point - arm]]];
  return [0, 60, 120, 180, 240, 300].map((degrees) => {
    const [c, s] = [Math.cos((degrees * Math.PI) / 180), Math.sin((degrees * Math.PI) / 180)];
    return up.map((line) => `M${line.map(([x, y]) => `${n(10 + (x - 10) * c - (y - 10) * s)} ${n(10 + (x - 10) * s + (y - 10) * c)}`).join("L")}`).join("");
  }).join("");
})();
const sliders = (quarters: number): NativeIconGlyph => {
  const knobs: Array<[number, number]> = [[13.4375, 4.375], [6.5625, 10], [10.9375, 15.625]];
  const stroke = knobs.map(([x, y]) => polyline([[2.5 + HALF_LINE, y], [x - 1.5 - HALF_LINE, y]], quarters) + polyline([[x + 1.5 + HALF_LINE, y], [17.5 - HALF_LINE, y]], quarters)).join("");
  const fill = knobs.map((knob) => { const [x, y] = turned(knob, quarters); return `${circle(x, y, 2.8125)}${circle(x, y, 0.9375)}`; }).join("");
  return { stroke, fill };
};

// Notion's shuffle (read 2026-11-18): two lines from the box's side (1.875) on 5.625 and 14.375, each bending through a short
// round into a slant rising 0.8134 a unit for every 0.5816 across, and out of it onto the other's level to a head whose point
// reaches 18.36 and whose arms run 4.07 back; the line from the top is cut 1.25 either side of the other where they cross.
const shuffle = (() => {
  const tip = 20 - 18.36, end = 20 - headPoint(tip), start = 1.875 + 0.9375;
  const level = (y: number, up: number) => `M${start} ${y}H3.72Q5 ${y} 5.75 ${n(y - up * 1.05)}`;
  return `${level(14.375, 1)}L10.505 6.675Q11.255 5.625 12.54 5.625H${n(end)}${level(5.625, -1)}L6.234 7.353M10.02 12.645L10.505 13.325Q11.255 14.375 12.54 14.375H${n(end)}`
    + `${chevron(tip, 4.07, 1, 20 - 5.625)}${chevron(tip, 4.07, 1, 20 - 14.375)}`;
})();

// An asterisk of flat-ended arms, drawn as one outline.
function asterisk(arms: number, length: number, halfWidth: number, cx = 10, cy = 10) {
  const inner = halfWidth / Math.sin(Math.PI / arms);
  return polygon(Array.from({ length: arms }, (_, index) => {
    const angle = -Math.PI / 2 + (2 * Math.PI * index) / arms;
    const [dx, dy, nx, ny] = [Math.cos(angle), Math.sin(angle), -Math.sin(angle), Math.cos(angle)];
    const between = angle + Math.PI / arms;
    return [
      [cx + length * dx - halfWidth * nx, cy + length * dy - halfWidth * ny],
      [cx + length * dx + halfWidth * nx, cy + length * dy + halfWidth * ny],
      [cx + inner * Math.cos(between), cy + inner * Math.sin(between)],
    ] as Array<[number, number]>;
  }).flat());
}

const cloud = "M6.5 11a3 3 0 0 1-.3-6A4 4 0 0 1 13.8 4.3a3.3 3.3 0 0 1 .2 6.7Z";
// Notion's thought bubble (read 2026-11-18), filled: five round lobes — 4.375 at (13.75, 11.25) and (9.6875, 12.5), 3.75 at
// (5.625, 10), 5 at (9.0625, 6.875) and 3.125 at (13.75, 6.25) — and a dot 2.8 across trailing to the foot left; its marks
// cut out of it (the points where the lobes meet worked out to three places).
const thoughtBubble = "M18.125 11.25A4.375 4.375 0 0 1 12.843 15.53A4.375 4.375 0 0 1 5.494 13.748A3.75 3.75 0 0 1 4.071 6.587"
  + `A5 5 0 0 1 12.602 3.343A3.125 3.125 0 0 1 16.456 7.813A4.375 4.375 0 0 1 18.125 11.25Z${circle(3.906, 16.094, 1.406)}`;
const slideshow = "M3 3.5h14M4 4.45V12h12V4.45ZM10 12.95V14M7.5 17l2.5-3 2.5 3";
const cigarette = "M5.5 11h7v2h-7ZM13.75 11v2M14 9.5c0-1 1-1.25 1-2.25";
/** A four-pointed star about (cx, cy), its points `reach` out and cut 0.625 across, its waists `waist` out each way. */
const fourPoints = (cx: number, cy: number, reach: number, waist: number) => {
  const cut = 0.3125, quarter = ([x, y]: [number, number]): [number, number] => [-y, x];
  let arm: Array<[number, number]> = [[-cut, -reach], [cut, -reach], [waist, -waist]];
  const points: Array<[number, number]> = [];
  for (let turn = 0; turn < 4; turn++) { points.push(...arm); arm = arm.map(quarter); }
  return polygon(points.map(([x, y]): [number, number] => [cx + x, cy + y]));
};
// A small person on the front tab (smallPerson): its head a neck below the tab's top, its shoulders cut a neck above the tab's foot.
const tabPerson = person((TAB_FRONT.left + TAB_FRONT.right) / 2, TAB_FRONT.top + NECK, 7 * EDGE_STEP, smallPerson(7 * EDGE_STEP));
/** A snowflake's six arms round (13.75, 7.65), each forked near its end. */
const snowflake = [-90, -30, 30, 90, 150, 210].map((degrees) => {
  const point = (length: number, turn: number, from: [number, number] = [13.75, 7.65]): [number, number] => {
    const angle = ((degrees + turn) * Math.PI) / 180;
    return [from[0] + length * Math.cos(angle), from[1] + length * Math.sin(angle)];
  };
  const fork = point(3.15, 0), at = ([x, y]: [number, number]) => `${n(x)} ${n(y)}`;
  return `M13.75 7.65L${at(point(4.6, 0))}M${at(point(1.9, 45, fork))}L${at(fork)}L${at(point(1.9, -45, fork))}`;
}).join("");

export const NATIVE_ICON_GLYPHS_ST: Readonly<Record<string, NativeIconGlyph>> = {
  shirt: { stroke: "M7 3l3 1.5L13 3l4 2.5-1.5 3.5-2-.75V17h-7V8.25l-2 .75L3 5.5Z" },
  shoe: { stroke: "M2.75 12.5v-4l3.5-1 2 2.5 3.5 1 4.5 1a2 2 0 0 1 1.5 2V15H2.75ZM2.75 13h14.5M7 9.5l1-1M9 10.5l1-1" },
  shogi: { stroke: "M10 3l4.5 2.5L16 17H4L5.5 5.5ZM8 9h4M10 8v5M8.5 12.5h3" },
  shop: { stroke: "M3 7h14l-1-3.5H4ZM3 7a2.33 2.33 0 0 0 4.67 0 2.33 2.33 0 0 0 4.66 0A2.33 2.33 0 0 0 17 7M4 9.5V17h12V9.5M8 17v-4h4v4" },
  "shopping-bag": { stroke: "M4.5 6.5h11l-1 10.5h-9ZM7.5 8.5v-3a2.5 2.5 0 0 1 5 0v3" },
  "shopping-basket": { stroke: "M2.75 8h14.5l-1.75 8.5h-11ZM6 8l3-4.5M14 8l-3-4.5M7.5 11v3M10 11v3M12.5 11v3" },
  "shopping-cart": { stroke: `M2.5 3.5h2l2 9H16l1.5-6.5H5.25${circle(8, 15.5, 1.25)}${circle(14.5, 15.5, 1.25)}` },
  shorts: { stroke: "M4.5 4h11L17 15.5h-5L10 9.5 8 15.5H3ZM4.5 6.5h11" },
  // Notion's shovel and pail (read 2026-11-18), filled: a pail 7.5 wide at the left with a round handle 7.5 across over it and
  // knobs out to 0.625 and 11.875, and a shovel upright at the right, its grip's hole cut out and its blade round-shouldered.
  "shovel-and-pail": {
    fill: `M6.25 2.5${curve(back(roundQuarters(6.25, 6.25, 3.75)[3]))}V8.125H0.625V10H2.5V16.875H10V10H11.875V8.125H10V6.25${curve(back(roundQuarters(6.25, 6.25, 3.75)[0]))}Z`
      + `M4.375 8.125V6.25${curve(roundQuarters(6.25, 6.25, 1.875)[3])}${curve(roundQuarters(6.25, 6.25, 1.875)[0])}V8.125Z`
      + "M18.125 2.5H11.875V5Q11.875 7.36 14.06 7.48V10.42Q11.875 10.9 11.875 13.44V16.875H18.125V13.44Q18.125 10.9 15.94 10.42V7.48Q18.125 7.36 18.125 5ZM16.25 4.375V5Q16.25 5.6 15.77 5.625L15 5.69L14.23 5.625Q13.75 5.6 13.75 5V4.375Z",
  },
  // Notion's shower (read 2026-11-18), filled: a pipe from the box's right side at the top bending down into a head turned 45°,
  // and three streams 1.25 wide falling from it to the foot left.
  shower: {
    fill: "M16.875 1.875V3.125H16.04Q14.6 3.125 13.38 4.22L11.98 5.625H8.9Q8.2 5.625 7.57 6.17L6.91 6.84L6.01 5.94L4.68 7.26L12.15 14.72L13.47 13.4L12.53 12.46L13.2 11.8Q13.75 11.2 13.75 10.48V7.39L15.15 5.99Q15.44 5.625 16.03 5.625H16.875V6.875H18.75V1.875Z"
      + `${polygon([[7.81, 12.45], [4.01, 16.25], [3.125, 15.37], [6.93, 11.56]])}${polygon([[5.05, 9.69], [1.25, 13.49], [2.13, 14.375], [5.94, 10.57]])}${polygon([[5, 17.24], [5.88, 18.125], [9.69, 14.32], [8.8, 13.44]])}`,
  },

  shuffle: { stroke: shuffle },
  "sign-in": { stroke: `${rect(8, 3.5, 8.5, 13, 1.5)}M2.75 10h8M8.25 7.25 11 10l-2.75 2.75` },
  "sign-out": { stroke: `${rect(3.5, 3.5, 8.5, 13, 1.5)}M9 10h8.25M14.5 7.25 17.25 10l-2.75 2.75` },
  "signature-document": { stroke: "M5.5 2.75h5.75L14.5 6v10.5a.75.75 0 0 1-.75.75H5.5a.75.75 0 0 1-.75-.75v-13a.75.75 0 0 1 .75-.75ZM11 2.75V6.5h3.5M6.5 13.5c1-1.5 1.5-1.5 2 0s1-.5 1.75-1.25c.5 1.25 1.25 1.25 2.25.75M7 10h5" },
  sink: { stroke: "M2.75 10h14.5v1.5a4 4 0 0 1-4 4h-6.5a4 4 0 0 1-4-4ZM10 10V5a2 2 0 0 1 4 0M8.5 15.5 8 17.25h4l-.5-1.75M6.5 7.5V10" },
  skateboard: { stroke: "M.95 7.85c1 1 1.6 1.7 3 1.7h12.1c1.4 0 2-.7 3-1.7", fill: "M3.6 10.5h3.2v1a1.6 1.6 0 0 1-3.2 0ZM13.2 10.5h3.2v1a1.6 1.6 0 0 1-3.2 0Z" },
  "skip-backward": { stroke: "M15.5 12a5.5 5.5 0 0 0-10.5-2.25M4.5 6.5 5 9.75l3.25-.5", fill: circle(10, 13.5, 1.25) },
  "skip-forward": { stroke: "M4.5 12a5.5 5.5 0 0 1 10.5-2.25M15.5 6.5 15 9.75l-3.25-.5", fill: circle(10, 13.5, 1.25) },
  skirt: { stroke: "M7 3.5h6l.5 2.5 3 11h-13l3-11ZM6.5 6h7M8 9l-1 8M12 9l1 8M10 9v8" },
  skull: {
    stroke: "M10 2.75c3.75 0 6.5 2.5 6.5 6 0 2-1 3.5-2.5 4.25V16H6v-3c-1.5-.75-2.5-2.25-2.5-4.25 0-3.5 2.75-6 6.5-6ZM8.5 16v-2M11.5 16v-2",
    fill: `${circle(7.5, 9.5, 1.5)}${circle(12.5, 9.5, 1.5)}${polygon([[10, 11.25], [9.25, 12.75], [10.75, 12.75]])}`,
  },
  "skull-profile": {
    stroke: "M4 9c0-3.5 2.75-6 6.5-6 3.5 0 6 2.5 6 5.5 0 1.5-.5 2.75-1.5 3.75V16h-3v-2h-2v2H7v-3.5l-2-.5-1-2Z",
    fill: `${circle(12, 8.5, 1.5)}${polygon([[15.25, 10.5], [16.25, 12], [15, 12]])}`,
  },
  // Its sleet three short slants 1.25 across and three drops 1.875 across between them (read 2026-11-18).
  sleet: {
    fill: `${rainCloud}${polygon([[6.09, 12.5], [7.34, 12.5], [6.09, 15], [4.84, 15]])}${polygon([[8.59, 15], [9.84, 15], [8.59, 17.5], [7.34, 17.5]])}`
      + `${polygon([[13.59, 12.5], [14.84, 12.5], [13.59, 15], [12.34, 15]])}${circle(10, 13.4375, 0.9375)}${circle(4.6875, 16.5625, 0.9375)}${circle(12.1875, 16.5625, 0.9375)}`,
  },
  // A slide in its mount: a wide window cut out of the frame.
  slide: { fill: `${rect(2.75, 3.5, 14.5, 13, 2.25)}${rect(5.25, 6.75, 9.5, 6.5, 0.5)}` },
  // Notion's sliders (read 2026-11-18): three lines a line wide across 2.5..17.5 on 4.375, 10 and 15.625, each with a knob 5.625
  // across at 13.44, 6.56 and 10.94, a ring round a hole 1.875 across, the line stopping at it; turned a quarter back, upright.
  "sliders-horizontal": sliders(0),
  "sliders-vertical": sliders(3),
  slideshow: { stroke: slideshow },
  "slideshow-play": { stroke: slideshow, fill: polygon([[8.75, 5.5], [12, 7.75], [8.75, 10]]) },
  smoking: inCircle(cigarette),
  "smoking-no": inCircle(`${cigarette}${ringSlash}`),
  // Notion's snake (read 2026-11-18), filled: its head at the top right with a tongue out to the right, its body winding down
  // and back as a band to its tail lying along the foot, from the box's side to the foot left.
  snake: { fill: "M18.125 15Q18.125 16.875 16.25 16.875H5.67Q4.3 16.875 3.02 15.78L1.25 14.01L2.13 13.125L3.38 14.375H5V13.92Q5 12.3 6.5625 12.2V11.42Q6.5625 9.69 8.29 9.69Q8.45 9.69 8.58 9.71L11.19 10.15L9.53 8.49Q8.43 7.4 8.43 5.84Q8.43 2.5 11.15 2.5Q12.2 2.5 13.08 3.01L14.375 3.75V5H10.93V5.84Q10.93 6.35 11.3 6.72L13.9 9.32Q14.5 9.9 14.82 10.75L15.3 10.83Q16.875 11.1 16.875 12.68V13.22Q18.125 13.6 18.125 15Z" },
  "snare-drum": { stroke: `${ellipse(10, 8.5, 6.5, 2.5)}M3.5 8.5v6c0 1.5 2.9 2.5 6.5 2.5s6.5-1 6.5-2.5v-6M5.5 3.5l3 4.5M14.5 3.5l-3 4.5M6 11v4.5M10 11.25V17M14 11v4.5` },
  snippet: { stroke: "M5.55 4.05H4.6a1.4 1.4 0 0 0-1.4 1.4v2.9a1.65 1.65 0 0 1-1.65 1.65 1.65 1.65 0 0 1 1.65 1.65v2.9a1.4 1.4 0 0 0 1.4 1.4h.95M14.45 4.05h.95a1.4 1.4 0 0 1 1.4 1.4v2.9a1.65 1.65 0 0 0 1.65 1.65 1.65 1.65 0 0 0-1.65 1.65v2.9a1.4 1.4 0 0 1-1.4 1.4h-.95", fill: `${circle(6.2, 10, 1.25)}${circle(10, 10, 1.25)}${circle(13.8, 10, 1.25)}` },
  snorkel: { stroke: "M3 9a2 2 0 0 1 2-2h7a2 2 0 0 1 2 2v2a2 2 0 0 1-2 2h-1.5l-1-1.25h-2L6.5 13H5a2 2 0 0 1-2-2ZM16 3v10a3 3 0 0 1-3 3h-2" },
  // Notion's snowflake (read 2026-11-18): six arms a line wide from the middle, upright and every 60° round, each ending in a
  // fork, two prongs at 45° back from a point 6.95 from its end's side of the box, their outsides running 3.12 back.
  snowflake: { stroke: snowArms },
  // Notion's soap (read 2026-11-18), filled: a bar 11.25 by 12.5 from 5 with Notion's round corners 2.5, its inside cut out
  // 1.875 in, a bar 5 by 6.25 with swelling sides left in the middle, and three bubbles over its top right.
  soap: {
    fill: `${plate(1.875, 5, 13.125, 17.5, 2.5)}${plate(3.75, 6.875, 11.25, 15.625, 0.625)}M5.31 8.125H9.69Q10 9.7 10 11.25Q10 12.8 9.69 14.375H5.31Q5 12.8 5 11.25Q5 9.7 5.31 8.125Z`
      + `${circle(17.5, 6.875, 1.25)}${circle(15.3125, 10.3125, 0.9375)}${circle(14.6875, 2.8125, 1.5625)}`,
  },
  soccer: {
    stroke: `${ring}M10 7.6V3.45M12.3 9.25l3.78-1.36M11.4 12l2.34 3.38M8.6 12 6.26 15.38M7.7 9.25 3.92 7.89`,
    fill: polygon(regular(5, 2.5)),
  },
  // Notion's socks (read 2026-11-18), filled: two alike side by side, 8.125 apart, each a cuff 4.375 by 2.5 at the round edge's
  // top over a leg 1.25 under it, down to 10.14 and bending at 45° to a round toe 4.37 across.
  sock: { fill: [0, 8.125].map((x) => `${bar(2.5 + x, 2.5, 4.375, 2.5)}M${7.24 + x} 11.02Q${6.875 + x} 10.6 ${6.875 + x} 10.14V6.25H${2.5 + x}V10.14Q${2.5 + x} 12.5 ${4.15 + x} 14.12L${6.82 + x} 16.78A2.185 2.185 0 0 0 ${9.91 + x} 13.69Z`).join("") },
  // Notion's soda (read 2026-11-18), filled: a bottle 6.25 wide at the left, its cap 3.75 wide from 1.25, its neck swelling to
  // its shoulders and its body waisted twice to its foot; and at the right a cup narrowing from 9.375..18.75 at the round
  // edge's top to 10.625..17.5 at its foot, a band cut across its top.
  "soda-bottle": {
    fill: "M7.47 10.69Q7.19 11.25 7.19 11.875Q7.19 12.5 7.47 13.06L7.84 13.82Q8.125 14.4 8.125 15Q8.125 15.6 7.84 16.18L7.18 17.5H2.81L2.15 16.18Q1.875 15.6 1.875 15"
      + "Q1.875 14.4 2.15 13.82L2.53 13.06Q2.81 12.5 2.81 11.875Q2.81 11.25 2.53 10.69L2.15 9.93Q1.875 9.4 1.875 8.75V7.35Q1.875 6.1 2.65 5.48L2.97 5.15Q3.75 4.4 3.75 3.28"
      + "V3.125H3.125V1.25H6.875V3.125H6.25V3.28Q6.25 4.4 7.02 5.15L7.35 5.48Q8.125 6.1 8.125 7.35V8.75Q8.125 9.4 7.84 9.93Z"
      + `${polygon([[18.75, 2.5], [17.5, 17.5], [10.625, 17.5], [9.375, 2.5]])}${polygon([[11.41, 4.375], [11.62, 6.875], [16.51, 6.875], [16.72, 4.375]])}`,
  },
  "soft-serve": { stroke: "M7 11h6l-3 6.5ZM6.5 11a3.5 1.25 0 0 1 7 0M7.5 8.75h5A2.5 2 0 0 0 10 6M10 6c.75-1 .5-2.25-.5-3" },
  // Notion's soybean pod (read 2026-11-18), filled: a pod along the slant from the foot left to the top right, its stalk side
  // straight-backed and its other side three lobes round 3.44 about its beans, each bean an oval cut out.
  soy: {
    fill: "M18.125 6.875A3.44 3.44 0 0 1 14.9 10.57A3.48 3.48 0 0 1 10.56 14.91A3.42 3.42 0 0 1 6.87 18.125Q6.1 18.125 5.4 17.89L1.25 16.5V15C3.9 15 15 3.9 15 1.25H16.5L17.89 5.4Q18.125 6.1 18.125 6.875Z"
      + "M6.2 15.68A1.4 1.0 -45 1 0 8.18 13.7A1.4 1.0 -45 1 0 6.2 15.68ZM10.57 12.55A1.4 1.0 -45 1 0 12.55 10.57A1.4 1.0 -45 1 0 10.57 12.55ZM13.7 8.18A1.4 1.0 -45 1 0 15.68 6.2A1.4 1.0 -45 1 0 13.7 8.18Z",
  },

  spider: {
    stroke: "M8 10 4 7l-.5-3M8 11.5 3.5 11M8.25 12.75 4.5 16M12 10l4-3 .5-3M12 11.5l4.5-.5M11.75 12.75 15.5 16M10 5.75V2",
    fill: `${circle(10, 11, 2.5)}${circle(10, 7.25, 1.5)}`,
  },
  sponge: { stroke: "M3 8.5 5 6h12v8.5L15 17H3ZM3 8.5h12V17M15 8.5 17 6", fill: `${circle(6, 11, 0.7)}${circle(9, 13.5, 0.7)}${circle(11.5, 11, 0.7)}` },
  // Notion's spoon (read 2026-11-18), filled, standing: a handle a line wide from the round edge's foot up the middle to a bowl
  // 5 across, its top half Notion's round to the round edge's top, its foot narrowing into the handle at 9.84.
  spoon: { fill: `M10.94 9.84V17.5H9.06V9.84Q7.5 9.2 7.5 6.875${curve(roundQuarters(10, 6.875, 2.5, 4.375)[3])}${curve(roundQuarters(10, 6.875, 2.5, 4.375)[0])}Q12.5 9.2 10.94 9.84Z` },
  "spray-bottle": { stroke: "M6.5 8.5h6v8.75h-6ZM7.5 8.5 8 6h4l.5 2.5M8 6V4h5.5l1 1.5H12V6M14.5 5.5l2-1M14.5 5.5h2M14.5 5.5l2 1" },
  "squeeze-tube": { stroke: "M5.5 3h9l-1 11h-7ZM6.5 14h7v1.5h-7ZM8.5 15.5V17h3v-1.5M6 6h8" },
  stairs: { stroke: "M3 16.5h3.5V13H10V9.5h3.5V6H17v10.5Z" },
  "star-of-life": { fill: asterisk(6, 7.25, 1.6) },
  // Notion's stars (read 2026-11-18): three four-pointed stars, their points cut 0.625 across — 4.375 out about (13.75, 6.875),
  // 2.5 about (4.375, 8.75) and 3.125 about (9.375, 14.375), their waists a quarter of that.
  stars: { fill: `${fourPoints(13.75, 6.875, 4.375, 1.125)}${fourPoints(4.375, 8.75, 2.5, 0.75)}${fourPoints(9.375, 14.375, 3.125, 0.875)}` },
  // Its rim, and its hub and spokes as one solid piece across it.
  "steering-wheel": {
    stroke: ring,
    fill: "M3.5 9.25h13v1.5c-2.6 0-4 1.2-4.75 2.75L11 16.75H9l-.75-3.25C7.5 11.95 6.1 10.75 3.5 10.75Z",
  },
  stethoscope: { stroke: `M5 3v4.5a3.5 3.5 0 0 0 7 0V3M8.5 11v1.5a4 4 0 0 0 8 0V11M4 3h2M11 3h2${circle(16.5, 9.5, 1.5)}` },
  sticker: { stroke: "M3.5 5A1.5 1.5 0 0 1 5 3.5h10A1.5 1.5 0 0 1 16.5 5v6L11 16.5H5A1.5 1.5 0 0 1 3.5 15ZM16.5 11h-4a1.5 1.5 0 0 0-1.5 1.5v4" },
  // Notion's stomach (read 2026-11-18), filled: a pouch swelling round to the box's right side and foot, its gullet a line wide
  // bending up from it to the round edge's top at 5..7.5, and its gut out to the foot left.
  stomach: { fill: "M17.5 10Q17.5 17.5 10.625 17.5Q8.1 17.5 5.74 15.625H5.625Q4.375 15.625 4.375 16.875V17.5H1.875V16.875Q1.875 13.9 4.7 12.8Q7.5 11.9 7.5 10Q7.5 8.8 7.75 7.78L7.08 7.44Q5 6.4 5 4.08V2.5H7.5V4.08Q7.5 4.85 8.19 5.2L8.8 5.5Q10.1 3.75 12.5 3.75Q17.5 3.75 17.5 10Z" },
  stopwatch: { stroke: `${circle(10, 11, 6)}M10 11V7.75M8.5 2.75h3M10 2.75V5M15 5.5l1.25-1.25` },
  storm: { stroke: cloud, fill: "M10.5 11.5 8.5 14.5h2.25l-1 3 3-4H10.5l1-2Z" },
  // A cooktop: its back panel above, and four burners, two large and two small, cut out of its top.
  stovetop: { fill: `${rect(3.25, 2.5, 13.5, 1.75, 0.4)}${rect(3.25, 5.25, 13.5, 12.25, 0.75)}${circle(7, 9.25, 2.35)}${circle(13, 9.25, 1.6)}${circle(7, 14, 1.6)}${circle(13, 14, 2.35)}` },
  strawberry: {
    stroke: "M10 17c-3 0-6-4-6-7.5C4 7 6 6 8 6.5h4C14 6 16 7 16 9.5 16 13 13 17 10 17ZM7 6.5 10 4l3 2.5M10 4V2.5",
    fill: `${circle(8, 10, 0.5)}${circle(12, 10, 0.5)}${circle(10, 12, 0.5)}${circle(8.75, 14, 0.5)}${circle(11.25, 14, 0.5)}`,
  },
  // Notion's stroller (read 2026-11-18), filled: a handle across 2.5..5.84 at the round edge's top, a frame 1.875 wide down at
  // 45° from it to the right wheel, a leg slanting from the handle to the left wheel, the axle between them on 13.125, and two
  // wheels 3.75 across.
  stroller: {
    fill: "M2.5 2.5H5.84Q7.08 2.5 7.61 3.23L15.52 11.14Q16.25 11.9 16.25 12.91V13.857A1.875 1.875 0 1 1 15 13.857V13.125H8.125V13.857A1.875 1.875 0 1 1 6.875 13.857V12.89L5.46 4.375H2.5Z",
  },
  subtask: { stroke: `${rect(3, 3, 6, 4, 1)}${rect(11, 13, 6, 4, 1)}M6 7v6a2 2 0 0 0 2 2h3` },
  // Three dots cut out of the bubble across its body's middle, each two grid steps round and a step apart.
  subtitles: { fill: `${bubble}${[6.25, 10, 13.75].map((x) => circle(x, BUBBLE_MIDDLE, 1.25)).join("")}` },
  suit: { stroke: "M6.5 3 10 8l3.5-5 3.5 2V17H3V5ZM10 8v9M6.5 3l-1 5 2.5 2M13.5 3l1 5-2.5 2", fill: `${circle(10, 11.5, 0.6)}${circle(10, 14, 0.6)}` },
  "suit-club": { fill: `${circle(10, 6.5, 2.4)}${circle(6.6, 11, 2.4)}${circle(13.4, 11, 2.4)}${polygon([[10, 10], [8.5, 16.5], [11.5, 16.5]])}` },
  "suit-diamond": { fill: polygon([[10, 2.5], [15, 10], [10, 17.5], [5, 10]]) },
  "suit-heart": { fill: heart },
  "suit-spade": { fill: `M10 3s6 4.5 6 8.25a3 3 0 0 1-6 1.25 3 3 0 0 1-6-1.25C4 7.5 10 3 10 3Z${polygon([[10, 12], [8.25, 17], [11.75, 17]])}` },
  suitcase: { stroke: `${rect(3, 6, 14, 10.5, 1.5)}M7.5 6V4a1 1 0 0 1 1-1h3a1 1 0 0 1 1 1v2M7 6v10.5M13 6v10.5` },
  sunglasses: { fill: sunglassesShape() },
  sunrise: { stroke: "M2.75 15.5h14.5M5.5 14.55a4.5 4.5 0 0 1 9 0ZM10 2.5v5M8 4.5l2-2 2 2M3.5 10.5l1 .75M16.5 10.5l-1 .75" },
  // Notion's sunscreen (read 2026-11-18), filled: a sun 5 across at the left with five rays 1.25 wide, and at the right a bottle
  // narrowing down from 9.375..18.75 at 3.125 to 4.375 across at 13.125 over its cap to 16.875.
  sunscreen: {
    fill: `${circle(6.875, 10.625, 2.5)}${bar(0.625, 10, 2.5, 1.25)}${bar(6.25, 4.375, 1.25, 2.5)}${bar(6.25, 14.375, 1.25, 2.5)}`
      + `${polygon([[2.01, 14.6], [2.9, 15.49], [4.67, 13.72], [3.78, 12.83]])}${polygon([[4.67, 7.53], [2.9, 5.76], [2.01, 6.65], [3.78, 8.42]])}`
      + `M9.375 3.125V4Q9.375 5.6 9.89 7.17L11.875 13.125H16.25L18.24 7.17Q18.75 5.6 18.75 4V3.125Z${bar(11.875, 14.375, 4.375, 2.5)}`,
  },
  sunset: { stroke: "M2.75 15.5h14.5M5.5 14.55a4.5 4.5 0 0 1 9 0ZM10 2.5v5M8 5.5l2 2 2-2M3.5 10.5l1 .75M16.5 10.5l-1 .75" },
  "suspension-bridge": { stroke: "M5.6 4.05v11.9M14.4 4.05v11.9M.95 11.85h18.1M5.6 4.05c.9 3.3 2.4 5.5 4.4 7.15M14.4 4.05c-.9 3.3-2.4 5.5-4.4 7.15M5.6 4.05C4.8 7 3 9.3.95 10.6M14.4 4.05c.8 2.95 2.6 5.25 4.65 6.55" },
  // Notion's swap (read 2026-11-18): an arrow to the right on 6.25 from 3.125 and one to the left on 13.75 from 16.875, a line
  // wide, their heads' points 2.58 from the box's side, arms 4.06 back; turned a quarter back, the two stand upright.
  "swap-horizontally": { stroke: swap(0) },
  "swap-vertically": { stroke: swap(3) },
  // Notion's sword (read 2026-11-18), filled: a blade slanting from its point at the box's top right down to a guard bowed
  // round 6.2 across its foot, a grip to the foot left, and a pommel 3.125 across.
  sword: { fill: "M16.62 5.59L9.47 12.74A4.35 4.35 0 0 0 11.25 13.125V15A6.2 6.2 0 0 1 7.55 13.78L5.6 15.72A1.5625 1.5625 0 1 1 4.28 14.4L6.22 12.45A6.2 6.2 0 0 1 5 8.75H6.875Q6.875 9.7 7.26 10.53L14.41 3.38L17.5 2.5Z" },
  symbol: { stroke: "M3 9h14v7H3ZM5 9V4h10v5M6 12h1M9.5 12h1M13 12h1M7 14.5h6" },
  synagogue: {
    stroke: `M3 17V8l7-5 7 5v9ZM8 17v-3a2 2 0 0 1 4 0v3${polygon([[10, 6.25], [11.73, 9.25], [8.27, 9.25]])}${polygon([[10, 10.25], [8.27, 7.25], [11.73, 7.25]])}`,
  },
  sync: { stroke: "M4.25 8a6 6 0 0 1 11.25-1.5M15.75 12a6 6 0 0 1-11.25 1.5M15.5 3.5v3h-3M4.5 16.5v-3h3" },
  "sync-reverse": { stroke: "M15.75 8A6 6 0 0 0 4.5 6.5M4.25 12a6 6 0 0 0 11.25 1.5M4.5 3.5v3h3M15.5 16.5v-3h-3" },
  syringe: { stroke: "M13.5 2.5l4 4M15.5 4.5 13 7M13 7l-7 7-2 .5.5-2 7-7ZM4 14.5 2.5 16M8.5 9l1.5 1.5M10 7.5l1.5 1.5" },
  "t-square": { stroke: "M2.75 3h14.5v3H2.75ZM8.5 6v11h3V6" },
  tablet: { stroke: `${rect(4, 2.75, 12, 14.5, 2)}M9 15h2` },
  tabs: { fill: tabs },
  "tabs-user": { fill: `${tabs}${tabPerson.head}${shouldersAbove(tabPerson.shoulders, TAB_FRONT.bottom - NECK)}` },
  // Notion's takeout box (read 2026-11-18), filled: a box widening up from 8.9 across at its foot to its flaps, slanting out at
  // 4:1, its lid's top cut at the corners to 3.12 across at 2.5, and a leaf cut out of its front.
  "takeout-box": {
    fill: polygon([[16.29, 5], [15.35, 8.75], [13.75, 8.75], [13.75, 4.69], [11.56, 2.5], [8.44, 2.5], [6.25, 4.69], [6.25, 8.75], [4.65, 8.75], [3.71, 5], [2.5, 5.3], [5.55, 17.5], [14.45, 17.5], [17.5, 5.3]])
      + "M12.88 15.625L11.9 15.14A4.23 4.23 0 0 0 8.1 15.14L7.12 15.625L6.56 14.51L7.72 13.93Q8.5 13.5 8.7 12.71L9.375 10H10.625L11.31 12.71Q11.5 13.5 12.29 13.93L13.44 14.51Z",
  },
  tampon: { stroke: "M9.27 12.07 16.27 5.07a.95.95 0 0 0-1.34-1.34l-7 7a.95.95 0 0 0 1.34 1.34ZM7.2 12.8c-.55.3-1.05.4-1.6.4a2.85 2.85 0 0 0-2.3 4.2" },
  target: { stroke: `${ring}${circle(10, 10, 4)}`, fill: circle(10, 10, 1.5) },
  taurus: { stroke: `${circle(10, 12.5, 4)}M4 3.5c0 3 2.5 5 6 5s6-2 6-5` },
  // Notion's taxi (read 2026-11-18), filled: a sign 5 wide on its roof from 1.875, a cab widening down to mirrors at 8.125..10,
  // a body to 15 on four wheel stubs to 16.875, its windscreen and two lamps 1.875 across cut out.
  taxi: {
    fill: polygon([[16.41, 8.125], [15.31, 3.75], [12.5, 3.75], [12.5, 1.875], [7.5, 1.875], [7.5, 3.75], [4.69, 3.75], [3.59, 8.125], [1.875, 8.125], [1.875, 10], [3.125, 10], [3.125, 15], [4.375, 15],
      [4.375, 16.875], [6.875, 16.875], [6.875, 15], [13.125, 15], [13.125, 16.875], [15.625, 16.875], [15.625, 15], [16.875, 15], [16.875, 10], [18.125, 10], [18.125, 8.125]])
      + `${polygon([[6.15, 5.625], [13.85, 5.625], [14.63, 8.75], [5.37, 8.75]])}${circle(6.25, 11.875, 0.9375)}${circle(13.75, 11.875, 0.9375)}`,
  },

  teapot: { stroke: "M5 8.5h10v4a4 4 0 0 1-4 4H9a4 4 0 0 1-4-4ZM15 10h1.5a1.5 1.5 0 0 1 0 3H15M5 10 2.75 8.5M8 8.5a2 2 0 0 1 4 0", fill: circle(10, 5.75, 0.9) },
  telephone: { stroke: `M3 8.5C3 6 6 4.5 10 4.5c4 0 7 1.5 7 4V10h-3.5V8.5h-7V10H3ZM5.5 10l-1 6.5h11l-1-6.5${circle(10, 13, 2)}` },
  telescope: { stroke: "M3 9.5 14.5 4 16 7.25 4.5 12.75ZM9.5 10.5 6.5 17M9.5 10.5l3 6.5M9.5 10.5V17" },
  "temperature-cool": { fill: "M2.2 12.73V2.7a2.05 2.05 0 0 1 4.1 0v10.03a3.65 3.65 0 1 1-4.1 0ZM3.65 3.1a.6.6 0 0 1 1.2 0v11.4h1.2a1.8 1.8 0 0 1-3.6 0h1.2Z", stroke: snowflake, weight: 1.25 },
  // Notion's warm (read 2026-11-18), filled: a thermometer at the left — a tube 5.625 wide with a round top, its bulb 8.75
  // across at the foot, its mercury's room cut out — and at the right a rising sun round 5.63 with a dial 3.75 across under it.
  "temperature-warm": {
    fill: "M18.12 4.38L14.84 6.56V2.03A4.69 4.69 0 0 0 10.37 8.13H8.75A5.63 5.63 0 0 0 20 8.13A4.68 4.68 0 0 0 18.12 4.38Z"
      + "M14.38 12.34A1.88 1.88 0 0 1 12.5 10.47A1.875 1.875 0 0 1 14.38 8.59V10.47L15.94 9.43A1.88 1.88 0 0 1 14.38 12.34Z"
      + "M7.81 11.65V3.44A2.81 2.81 0 0 0 2.19 3.44V11.65A4.38 4.38 0 1 0 7.81 11.65ZM5.94 6.875H4.06V3.44A0.94 0.94 0 1 1 5.94 3.44Z",
  },
  temple: { stroke: "M4.7 4.05h10.6v7.5h1.9-1.9v4.4h-2.45v-4.4h-5.7v4.4H4.7v-4.4H2.8h1.9ZM3.7 5.4c-.8.85-1.7 1.25-2.75 1.25M16.3 5.4c.8.85 1.7 1.25 2.75 1.25" },
  // Notion's theatre mask (read 2026-11-18), filled: a mask 12.5 wide from the round edge's top, its brow dipping to 4.375, its
  // sides down to a chin pointed at 18.125, two eyes rounded on top and a smiling mouth cut out.
  theatre: {
    fill: "M14.375 2.5Q12.8 4.375 10 4.375Q7.2 4.375 5.625 2.5H3.75V11.91Q3.75 15 6.51 16.38L10 18.125L13.49 16.38Q16.25 15 16.25 11.91V2.5Z"
      + "M6.25 9.44V9.06A1.25 1.56 0 0 1 8.75 9.06V9.44ZM11.25 9.44V9.06A1.25 1.56 0 0 1 13.75 9.06V9.44ZM6.5625 12.19H13.4375A3.44 2.5 0 0 1 6.5625 12.19Z",
  },

  thinking: { fill: `${thoughtBubble}${circle(6.25, 9.375, 1.25)}${circle(10, 9.375, 1.25)}${circle(13.75, 9.375, 1.25)}` },
  thought: { fill: thoughtBubble },
  "thought-alert": { fill: `${thoughtBubble}${polygon([[8.75, 4.6875], [11.25, 4.6875], [11.25, 8.4375], [10.625, 10.9375], [9.375, 10.9375], [8.75, 8.4375]])}${bar(8.75, 12.1875, 2.5, 1.875)}` },
  "thought-dialogue": { fill: `${thoughtBubble}M6.5625 7.8125A3.44 3.44 0 0 1 13.4375 7.66Q13.4375 9.1 12.1 9.6Q10.94 10.1 10.94 10.9375H9.06Q9.06 9.3 10.3 8.6Q11.41 8.1 11.41 7.66A1.49 1.49 0 0 0 8.4375 7.8125Z${bar(8.75, 12.1875, 2.5, 1.875)}` },
  // Notion's throat (read 2026-11-18), filled: the profile's head facing right, its throat cut from its mouth down its neck to
  // the round edge's foot, the jaw and front of the neck a piece apart.
  throat: {
    fill: "M11.01 10.06L14.93 11.04L14.82 11.68Q14.7 12.5 13.93 12.5Q13.79 12.5 13.63 12.46L11.875 12.02V13.125L13.47 16.31Q13.75 16.85 13.75 17.5H10.625V10.36Q10.625 10 11.01 10.06Z"
      + "M16.15 8.95L14.99 7.4Q14.99 7.3 15 7.19A4.69 4.69 0 0 0 10.31 2.5A4.69 4.69 0 0 0 5.625 7.19A4.68 4.68 0 0 0 8.125 11.33V13.44Q5 15 5 17.5H9.375V10.12A1.37 1.37 0 0 1 11.07 8.79L15.13 9.81L15.91 9.61Q16.24 9.5 16.24 9.23A0.46 0.46 0 0 0 16.14 8.95Z",
  },
  // A solid hand turned down beside its cuff, thumbs-up upside down.
  "thumbs-down": {
    fill: `${rect(2.75, 3, 3, 7.75, 0.75)}M6.6 10.75 9.6 16.4a1.55 1.55 0 0 0 2.85-1L11.9 12h3.65a1.6 1.6 0 0 0 1.55-2l-1.7-5.85A1.6 1.6 0 0 0 13.85 3H6.6Z`,
  },
};
