import {
  ROUND_EDGE,
  n,
  HALF_LINE,
  FULL_EDGE,
  EDGE_STEP,
  edgeIn,
  WIDE_EDGE,
  bar,
  bell,
  circle,
  ellipse,
  inCircle,
  inRing,
  octagon,
  pixels,
  polygon,
  rays,
  rect,
  ring,
  sunRays,
  type NativeIconGlyph,
} from "./nativeIconGlyphShapes";
import { blockArrow, chevron } from "./nativeIconArrows";
import { FRAME_LINE, PLATE_CORNER } from "./nativeIconFrames";
import { smallPerson, standing } from "./nativeIconPerson";
import { back, curve, plate, roundQuarters } from "./nativeIconRound";

// Leafen glyphs for catalog names from "123" through the "b" names.

// The right half of Notion's bug's body with its legs (read 2026-11-18).
const bugHalf = "M15 11.25H18.75V10H15V8.12Q16.4 8.1 17.11 7.21L18.44 5.88L17.55 5L16.23 6.33Q15.7 6.875 14.9 6.875H10.625V16.875Q13.6 16.875 14.59 14.23L15.08 14.71Q15.625 15.2 15.625 16.03V16.875H16.875V16.04Q16.875 14.7 15.96 13.82L14.94 12.81Q15 12.36 15 11.875Z";
/** A path of absolute commands (M L H V Q A Z) mirrored across the middle of the box. */
const mirrored = (path: string) => path.replace(/([MLHQA])([^MLHVQAZ]*)/g, (_, command: string, values: string) => {
  const v = values.trim().split(/[\s,]+/).map(Number);
  if (command === "H") return `H${n(20 - v[0]!)}`;
  if (command === "A") return `A${v[0]} ${v[1]} ${v[2]} ${v[3]} ${1 - v[4]!} ${n(20 - v[5]!)} ${v[6]}`;
  return command + v.map((value, index) => (index % 2 === 0 ? n(20 - value) : value)).join(" ");
});

// A badge's small person (smallPerson).
const badgePerson = standing(10, 17.5 - FRAME_LINE, 5 * EDGE_STEP, smallPerson(5 * EDGE_STEP));
const battery = `${rect(2.75, 6, 12.5, 8, 1.5)}M17.25 8.5v3`;
const cyclingArrows = "M3.5 7h12M12.5 4l3 3-3 3M16.5 13h-12M7.5 10l-3 3 3 3";

export const NATIVE_ICON_GLYPHS_AB: Readonly<Record<string, NativeIconGlyph>> = {
  "123": { stroke: "M3.25 7.25 4.75 6v8M7.25 7.5a1.75 1.75 0 0 1 3.5 0c0 1.9-3.5 3.6-3.5 6.5h3.5M12.75 6H16l-1.75 2.75a2.5 2.5 0 1 1-2.2 3.7" },
  accessibility: inCircle("M5.75 8.25h8.5M10 8.25v3.5M10 11.75l-2.25 3.25M10 11.75l2.25 3.25", circle(10, 5.75, 1.1)),
  activity: { stroke: `${rect(2.75, 3.75, 14.5, 12.5, 2)}M5 10h2l1.5-3 3 6 1.5-3h2` },
  airplane: {
    fill: "M10 2.5c.8 0 1.25.9 1.25 2v3.25l5.5 3.25v1.75l-5.5-1.5v3.5l1.75 1.25v1.5L10 16.75 7 17.5V16l1.75-1.25v-3.5l-5.5 1.5V11l5.5-3.25V4.5c0-1.1.45-2 1.25-2Z",
  },
  alarm: { stroke: `${circle(10, 10.75, 5.75)}M10 7.75v3l2 1.25M3.5 5.25 5.75 3.25M16.5 5.25l-2.25-2M6 16.25l-1.25 1.5M14 16.25l1.25 1.5` },
  // A round speech bubble, as Notion's (rastered at 400px, 2026-11-18): an oval 15 across and 13.75 down on the round edge,
  // its tail to the lower left at 45° from the oval to a tip on the round edge's left and back along the foot; cut out of
  // it a bar narrowing to its foot and a square dot under it, each two lines' width wide.
  alert: {
    fill: "M4.79 14.25A7.5 6.875 0 1 1 7.7 15.95Q5.9 17.3 4.2 17.5L3.1 17.5L2.5 16.54Z"
      + polygon([[8.75, 4.6875], [11.25, 4.6875], [11.25, 8.75], [10.625, 10.9375], [9.375, 10.9375], [8.75, 8.75]])
      + bar(8.75, 12.1875, 2.5, 1.875),
  },
  alien: {
    stroke: "M10 3c3.6 0 6 2.4 6 5.5 0 3.9-3.4 8.5-6 8.5s-6-4.6-6-8.5C4 5.4 6.4 3 10 3Z",
    fill: "M6.2 8.6c1.4-.3 2.8.4 3.1 1.8-1.4.3-2.8-.4-3.1-1.8ZM13.8 8.6c-1.4-.3-2.8.4-3.1 1.8 1.4.3 2.8-.4 3.1-1.8Z",
  },
  "alien-pixel": {
    fill: pixels([
      "..X.....X..",
      "...X...X...",
      "..XXXXXXX..",
      ".XX.XXX.XX.",
      "XXXXXXXXXXX",
      "X.XXXXXXX.X",
      "X.X.....X.X",
      "...XX.XX...",
    ], 1.45, 2.03, 4.2),
  },
  ambulance: {
    stroke: `M3 14.25V6.5a1 1 0 0 1 1-1h7.5v8.75M11.5 8.5h2.9l2.6 3v2.75M7.75 14.25h4.5M5.25 9.75h3.5M7 8v3.5${circle(5.6, 14.5, 1.6)}${circle(14.4, 14.5, 1.6)}`,
  },
  // Notion's anchor (read 2026-11-18): a ring 5.625 across at the top, a shank a line wide down from it with a bar across at
  // 9.375 from 5.625 to 14.375, and arms a line wide from 12.19 at 4.6875 and 15.3125 bending down to meet in a point at the
  // box's foot (18.75).
  anchor: {
    fill: `${circle(10, 4.0625, 2.8125)}${circle(10, 4.0625, 0.9375)}`,
    stroke: "M10 7.8V16.5M6.5625 9.375H13.4375M4.6875 13.13Q4.6875 15.05 6.93 16.17L10 17.7L13.07 16.17Q15.3125 15.05 15.3125 13.13",
  },
  apple: {
    fill: "M10 6.3c1.3-.9 2.6-1.3 3.8-1 2.3.6 3.2 3.2 2.6 6.1-.7 3.2-2.9 5.9-4.6 5.9-.8 0-1.2-.4-1.8-.4s-1 .4-1.8.4c-1.7 0-3.9-2.7-4.6-5.9-.6-2.9.3-5.5 2.6-6.1 1.2-.3 2.5.1 3.8 1Z",
    stroke: "M10 6c0-1.5.6-2.5 1.75-3.25",
  },
  // Notion's apron (read 2026-11-18), filled: straps 1.875 wide from 1.875 down to a bib 3.75 deep between them, the skirt
  // widening out from 6.25 in Notion's round 2.5 to 3.75..16.25 down to 18.125, and a pocket with a pointed foot cut in it.
  apron: {
    fill: `M13.75 6.25V1.875H11.875V6.875H8.125V1.875H6.25V6.25${curve(roundQuarters(3.75, 6.25, 2.5)[1])}V18.125H16.25V8.75${curve(roundQuarters(16.25, 6.25, 2.5)[2])}Z`
      + polygon([[13.125, 14.69], [10, 16.25], [6.875, 14.69], [6.875, 11.25], [13.125, 11.25]]),
  },
  aquarius: { stroke: "M3 8.5l2.33-1.75 2.34 1.75L10 6.75l2.33 1.75 2.34-1.75L17 8.5M3 13l2.33-1.75L7.67 13 10 11.25 12.33 13l2.34-1.75L17 13" },
  "arch-bridge": { stroke: "M2.5 7.5h15v9h-3c0-3-2-5.25-4.5-5.25C7.5 11.25 5.5 13.5 5.5 16.5h-3Z" },
  // Notion's archery (read 2026-11-18), filled: an arrow at 45° from its fletching at the foot left to its head at the top
  // right, crossing a bow bent round toward the top left, its string and grip parts cut out.
  archery: {
    fill: "M16.875 6.25L18.125 1.875L13.75 3.125V5.37L12.78 6.33Q10.7 4.4 8.42 4.375A5.66 5.66 0 0 0 5.73 4.91A0.86 0.86 0 0 1 5.33 5Q4.375 5 4.375 4.04V3.75H2.5V4.84Q2.5 5.9 3.23 6.61L7.875 11.25L6.625 12.5H4.375L1.875 15V16.25H3.75V18.125H5L7.5 15.625V13.38L8.75 12.125L13.39 16.77Q14.1 17.5 15.16 17.5H16.25V15.625H15.96Q15 15.625 15 14.67A0.85 0.85 0 0 1 15.1 14.27A5.7 5.7 0 0 0 15.625 11.58Q15.6 9.3 13.67 7.21L14.64 6.25Z"
      + "M5.57 7.19Q6.85 6.2 8.32 6.26Q9.9 6.35 11.44 7.67L8.75 10.36ZM13.75 11.68Q13.8 13.1 12.81 14.42L9.64 11.25L12.33 8.56Q13.7 10.1 13.75 11.68Z",
  },
  archive: { stroke: `${rect(2.75, 3.5, 14.5, 4, 1)}M4 7.5v8a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1v-8M8 10.5h4` },
  aries: { stroke: "M10 16.5V8.75C10 6 8.5 4.25 6.5 4.25a3 3 0 0 0-3 3c0 1.5 1 2.5 2.25 2.5M10 8.75c0-2.75 1.5-4.5 3.5-4.5a3 3 0 0 1 3 3c0 1.5-1 2.5-2.25 2.5" },
  arrivals: {
    stroke: "M2.75 16.75h14.5",
    fill: "M4.2 4.4 5.7 4l2.6 3.3 3-.1L9.6 2.9l1.7-.4 3.7 4.6 2 .1c.9 0 1.5.6 1.5 1.3s-.7 1.2-1.5 1.2L5.9 10c-.9 0-1.7-.5-2-1.4Z",
  },
  // A ring round a point turned down (an outline, nativeIconOutlined.ts).
  "arrow-circle-down": { stroke: ring, fill: "M6.75 8.5h6.5L10 13Z" },
  "arrow-down-line": { stroke: "M10 3.5v9.5M5.75 8.75 10 13l4.25-4.25M4 16.5h12" },
  "arrow-left-line": { stroke: "M16.5 10H7M11.25 5.75 7 10l4.25 4.25M3.5 4v12" },
  "arrow-right-line": { stroke: "M3.5 10H13M8.75 5.75 13 10l-4.25 4.25M16.5 4v12" },
  "arrow-turn-left": { stroke: "M15.5 16.5V10a3 3 0 0 0-3-3H4.5M8 3.5 4.5 7 8 10.5" },
  "arrow-turn-right": { stroke: "M4.5 16.5V10a3 3 0 0 1 3-3h8M12 3.5 15.5 7 12 10.5" },
  "arrow-up-line": { stroke: "M10 16.5V7M5.75 11.25 10 7l4.25 4.25M4 3.5h12" },
  "arrows-swap-horizontally": { stroke: cyclingArrows },
  "arrows-swap-vertically": { stroke: "M7 16.5v-12M4 7.5l3-3 3 3M13 3.5v12M10 12.5l3 3 3-3" },
  // An empty picture frame, its corners cut in.
  art: { fill: `M4.5 3.25h11a1.75 1.75 0 0 0 1.75 1.75v10a1.75 1.75 0 0 0-1.75 1.75h-11A1.75 1.75 0 0 0 2.75 15V5A1.75 1.75 0 0 0 4.5 3.25Z${rect(5.5, 6, 9, 8, 0.75)}` },
  atm: { stroke: "M.95 11.45v-5.5h18.1v5.5M4.05 9.05v4.95h11.9V9.05", fill: "M7.9 8.1h4.3v1.65a2.15 2.15 0 0 1-4.3 0Z" },
  attachment: { stroke: "M14.75 9.25 9.5 14.5a3 3 0 0 1-4.25-4.25l6-6a2 2 0 0 1 2.85 2.85L8.25 12.95a1 1 0 0 1-1.4-1.4l5.1-5.1" },
  avocado: {
    stroke: "M10 3c2.1 0 3.1 1.9 3.8 4 .6 1.8 2.45 3.1 2.45 5.5a6.25 4.75 0 0 1-12.5 0c0-2.4 1.85-3.7 2.45-5.5C6.9 4.9 7.9 3 10 3Z",
    fill: circle(10, 12.5, 2.4),
  },
  // Notion's baby (read 2026-11-18), filled: a head 4.38 across at the round edge's top, a body with arms raised out, and legs
  // bent in to the round edge's foot.
  baby: {
    fill: `${circle(10, 4.69, 2.19)}M12.08 8.44Q11.2 7.81 10 7.81Q8.8 7.81 7.92 8.44L5 10.39L5.87 11.69L7.5 10.6V11.875H12.5V10.6L14.13 11.69L15 10.39Z`
      + "M7.42 12.81L6.38 13.86Q5.94 14.3 5.94 14.92Q5.94 15.55 6.38 15.99L7.89 17.5L9 16.39L8.06 15.45L9.07 14.45ZM12.57 12.81L10.94 14.45L11.94 15.46L11.01 16.39L12.11 17.5L13.62 15.99Q14.06 15.55 14.06 14.93Q14.06 14.3 13.62 13.86Z",
  },

  "baby-bottle": { stroke: "M9.86 8.54L12.26 10.94A2 2 0 0 1 12.26 13.77L8.94 17.09A2 2 0 0 1 6.11 17.09L3.71 14.69A2 2 0 0 1 3.71 11.86L7.03 8.54A2 2 0 0 1 9.86 8.54ZM8.09 10.45L10.35 12.71M10 6.27L14.53 10.8M12.16 7.02L13.5 5.67A1.15 1.15 0 0 1 15.13 7.3L13.78 8.64Z" },
  backpack: { stroke: "M5 8.5a5 5 0 0 1 10 0V16a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1ZM8 3.75a2 2 0 0 1 4 0M7.5 11.25h5v3.5h-5Z" },
  backward: { fill: blockArrow(3) },
  // A plate with a slot to hang it by, a line below its top, and a person standing a line above its foot, both cut out of it.
  badge: { fill: `${plate(4.375, 2.5, 15.625, 17.5, PLATE_CORNER)}${plate(7.5, 2.5 + FRAME_LINE, 12.5, 3.75 + FRAME_LINE, 0.625)}${badgePerson.head}${badgePerson.outline}` },
  bag: { stroke: "M5.75 3h8.5a.75.75 0 0 1 .75.75V11a4.25 4.25 0 0 1-4.25 4.25h-1.5A4.25 4.25 0 0 1 5 11V3.75A.75.75 0 0 1 5.75 3ZM10 15.25V18M7.5 8h5" },
  // Notion's baggage claim (read 2026-11-18), filled: a case 7.5 wide from 1.25 to 14.375, its top corners Notion's round 2.5,
  // a handle's hole 3.75 by 1.875 cut in its top; a bumper either side 2.5 wide from 5, round 2.5 on its outer corners; and
  // under them a belt of five rollers 1.875 across, 3.125 apart.
  "baggage-claim": {
    fill: `${plate(6.25, 1.25, 13.75, 14.375, [2.5, 2.5, 0, 0])}${plate(8.125, 3.125, 11.875, 5, [0.625, 0.625, 0, 0])}`
      + `${plate(2.5, 5, 5, 14.375, [2.5, 0, 0, 2.5])}${plate(15, 5, 17.5, 14.375, [0, 2.5, 2.5, 0])}`
      + [3.75, 6.875, 10, 13.125, 16.25].map((x) => circle(x, 16.5625, 0.9375)).join(""),
  },
  balloon: {
    stroke: "M10 13c-2.9 0-5-2.55-5-5.5a5 5 0 0 1 10 0c0 2.95-2.1 5.5-5 5.5ZM10 14c-.75 1-.75 2 .25 3.5",
    fill: polygon([[9, 14.25], [11, 14.25], [10, 12.75]]),
  },
  banana: { stroke: "M4.5 4.75c-.25 6 4 10.75 10.5 10.75 1 0 1.75-.5 1.75-1.25-5.25-.5-9.25-4.25-10.25-9.5ZM4.5 4.75 4 3.25" },
  barcode: {
    fill: [[3, 1], [4.75, 0.75], [6.25, 1.5], [8.5, 0.75], [10, 1], [11.75, 1.5], [14, 0.75], [15.5, 1.5]]
      .map(([x, w]) => bar(x, 4, w, 12)).join(""),
  },
  // Notion's barricade (read 2026-11-18), filled: a board across 2.5..17.5 on 4.375..11.875 on two posts 1.875 wide from the
  // round edge's top to its foot, three slanting stripes cut out of it.
  barricade: {
    fill: polygon([[15.625, 4.375], [15.625, 2.5], [13.75, 2.5], [13.75, 4.375], [6.25, 4.375], [6.25, 2.5], [4.375, 2.5], [4.375, 4.375], [2.5, 4.375], [2.5, 11.875], [4.375, 11.875],
      [4.375, 17.5], [6.25, 17.5], [6.25, 11.875], [13.75, 11.875], [13.75, 17.5], [15.625, 17.5], [15.625, 11.875], [17.5, 11.875], [17.5, 4.375]])
      + polygon([[5.625, 10], [4.375, 10], [4.375, 6.25], [6.875, 6.25]]) + polygon([[10.625, 10], [8.125, 10], [9.375, 6.25], [11.875, 6.25]]) + polygon([[15.625, 10], [13.125, 10], [14.375, 6.25], [15.625, 6.25]]),
  },
  baseball: inRing("M5.5 5.25c1.6 1.4 2.5 3.1 2.5 4.75s-.9 3.35-2.5 4.75M14.5 5.25c-1.6 1.4-2.5 3.1-2.5 4.75s.9 3.35 2.5 4.75"),
  "baseball-cap": { stroke: "M2.75 13a5.75 5.75 0 0 1 11.5 0ZM14.25 13h3a.75.75 0 0 1 0 1.5h-7M8.5 7.25v5.75", fill: circle(8.5, 6.4, 0.9) },
  basketball: inRing("M3.45 10h13.1M10 3.45v13.1M5.41 5.37C7.1 6.54 7.89 8.13 7.89 10c0 1.87-.8 3.46-2.48 4.63M14.59 5.37C12.9 6.54 12.11 8.13 12.11 10c0 1.87.8 3.46 2.48 4.63"),
  bathtub: { stroke: "M2.75 10h14.5v2a4 4 0 0 1-4 4h-6.5a4 4 0 0 1-4-4ZM5.5 10V5.25A1.75 1.75 0 0 1 7.25 3.5 1.75 1.75 0 0 1 9 5.25M5.5 16l-.75 1.25M14.5 16l.75 1.25" },
  // Notion's bath with a shower (read 2026-11-18), filled: a tub on 10..11.875 across the box, its body narrowing at the left
  // to a foot on two legs to 19.375, its right foot corner Notion's round 2.5; and from its rim at the right a pipe 1.875 wide
  // rising to 0.625, bending over to the left into a shower head turned 45°.
  "bathtub-shower": {
    fill: "M1.25 10H15.625V3.33Q15.625 2.5 14.79 2.5Q14.45 2.5 14.21 2.74L13.125 3.82V5.73Q13.125 6.25 12.76 6.62L11.875 7.5L8.125 3.75L9.01 2.87Q9.38 2.5 9.89 2.5H11.8L12.88 1.42Q13.7 0.625 14.79 0.625Q17.5 0.625 17.5 3.33V10H18.75V11.875H17.5V15"
      + `${curve(roundQuarters(15, 15, 2.5)[1])}V19.375H13.125V17.5H8.125V19.375H6.25V17.38Q4.6 17 3.96 15.43L2.19 11.875H1.25Z`,
  },

  battery: { stroke: battery, fill: rect(4.5, 7.75, 6, 4.5, 0.5) },
  "battery-charged": { stroke: `${battery}M8 8v1.5M11 8v1.5M7 9.5h5v.75a2.5 2.5 0 0 1-5 0Z` },
  "battery-charging": { stroke: battery, fill: "M10 6.9 6.9 10.5h2.4l-.8 2.6 3.3-3.75H9.4Z" },
  // Notion's bed (read 2026-11-18), filled: a head post 1.875 wide from 4.375 to the foot leg's foot 16.25, a pillow to 6.875
  // and a mattress on 9.375..13.75 to the box's side, each rounding 2.5 at its top right in Notion's round, and a foot leg.
  bed: { fill: `M1.875 4.375H3.75V6.875H6.875${curve(roundQuarters(6.875, 9.375, 2.5)[0])}H15.625${curve(roundQuarters(15.625, 11.875, 2.5)[0])}V16.25H16.25V13.75H3.75V16.25H1.875Z` },
  // Notion's bee (read 2026-11-18), filled, alike either side of the middle: antennae curling out to 2.5, a head, wings round
  // 3.75 across at the box's sides, a body to a point at 18.125, and two stripes cut across it.
  bee: {
    fill: "M10 18.125L13.21 14.92Q13.76 14.4 13.76 13.6V13.125H16.26Q17.51 13.125 17.51 11.875V10.77L18.83 9.45A1.875 1.875 0 0 0 16.18 6.8L13.75 9.22V8.75Q13.75 6.875 11.875 6.875V6.25Q11.875 5.5 11.49 5.03L12.21 4.3Q12.75 3.75 13.54 3.75H13.75V2.5H13.54Q12.2 2.5 11.33 3.42L10.34 4.4A2 2 0 0 0 9.66 4.4"
      + "L8.67 3.42Q7.8 2.5 6.46 2.5H6.25V3.75H6.46Q7.25 3.75 7.79 4.3L8.51 5.03Q8.125 5.5 8.125 6.25V6.875Q6.25 6.875 6.25 8.75V9.22L3.82 6.8A1.875 1.875 0 0 0 1.17 9.45L2.49 10.77V11.875Q2.49 13.125 3.74 13.125H6.24V13.6Q6.24 14.4 6.79 14.92Z"
      + `${bar(8.125, 9.375, 3.75, 1.875)}M11.875 13.34Q11.875 13.6 11.69 13.78L10 15.47L8.31 13.78Q8.125 13.6 8.125 13.34V13.125H11.875Z`,
  },
  beer: { stroke: "M4.5 5.5h8V16a1 1 0 0 1-1 1h-6a1 1 0 0 1-1-1ZM12.5 7.5h1.75a1.25 1.25 0 0 1 1.25 1.25v3.5a1.25 1.25 0 0 1-1.25 1.25H12.5M7 8.5v5.5M10 8.5v5.5M4.5 5.5c0-1 .8-1.75 1.75-1.75.5-.6 1.2-.75 1.9-.5.6-.5 1.6-.6 2.35-.1.9 0 2 .6 2 2.35" },
  // Notion's beer (read 2026-11-18), filled: a bottle 6.25 wide at the left, its cap 3.75 wide from 1.25, its neck widening
  // down to its shoulders at 6.03, its foot corners round 1.25, a label cut out of it; and at the right a glass, its bowl
  // swelling to 18.125 and narrowing to a stem at 13.75 over a foot from 15.625, a foam line cut across its top.
  "beer-bottle": {
    fill: `M7.32 6.03L6.95 3.125H7.5V1.25H3.75V3.125H4.3L3.93 6.03Q2.5 6.8 2.5 8.75V16.25${curve(back(roundQuarters(3.75, 16.25, 1.25)[2]))}`
      + `H7.5${curve(back(roundQuarters(7.5, 16.25, 1.25)[1]))}V8.75Q8.75 6.8 7.32 6.03Z${bar(4.375, 9.375, 2.5, 4.375)}`
      + "M15.94 13.75Q15.94 12 17 10.4Q18.125 8.8 18.125 6.875Q18.125 4.4 16.875 2.5H11.25Q10 4.4 10 6.875Q10 8.8 11.1 10.4Q12.19 12 12.19 13.75Q12.19 15 10.94 15.625V17.5H17.19V15.625Q15.94 15 15.94 13.75Z"
      + "M12.4 4.375H15.73Q16.25 5.6 16.25 6.875H11.875Q11.875 5.6 12.4 4.375Z",
  },
  "bell-notification": { stroke: bell, fill: circle(15.25, 4.5, 2.25) },
  "bell-off": { stroke: `${bell}M3.25 3.25l13.5 13.5` },
  belt: { stroke: "M2.5 7.5h15v5h-15ZM8 6h4.5v8H8ZM10.25 10h2.25" },
  bicycle: { stroke: `${circle(5, 13, 3.25)}${circle(15, 13, 3.25)}M5 13l3-6.5h5.5L15 13M8 6.5 10.5 13H5M13.5 6.5l-1-2h-1.75M8 6.5H6.25` },
  bikini: { stroke: "M7.1 1.55 6.75 4.1 10 7.9c-.4 1.45-1.65 2.2-2.85 2.2-1.35 0-2.2-1.05-2.2-2.5 0-1.35.65-2.45 1.8-3.5ZM12.9 1.55l.35 2.55L10 7.9c.4 1.45 1.65 2.2 2.85 2.2 1.35 0 2.2-1.05 2.2-2.5 0-1.35-.65-2.45-1.8-3.5ZM5.95 14.35h8.1c-.45 2.1-1.85 3.65-3.45 4.4H9.4c-1.6-.75-3-2.3-3.45-4.4Z" },
  // Notion's (rastered at 400px, 2026-11-18): two bodies, each round at its top and narrowing up from a round foot 7.5
  // across that holds a lens cut out of it, their inner sides straight and a bridge between them.
  binoculars: {
    fill: "M9.375 6A2.875 2.875 0 0 0 3.625 6L1.875 13.125A3.75 3.75 0 0 0 9.375 13.125Z"
      + "M10.625 6A2.875 2.875 0 0 1 16.375 6L18.125 13.125A3.75 3.75 0 0 1 10.625 13.125Z"
      + `${circle(5.625, 13.125, 1.875)}${circle(14.375, 13.125, 1.875)}${bar(9.375, 8.75, 1.25, 5)}`,
  },
  // Notion's blood pressure (read 2026-11-18), filled: a dial 7.5 across at the top right with its needle cut out, joined by a
  // line to a cuff 8.125 wide from the round edge's top at the left, its tube looping down and round to a bulb at the foot.
  "blood-pressure": {
    fill: "M9.05 14.375Q10.1 14.375 10.8 13.65L13.1 11.35A3.75 3.75 0 0 0 18.75 8.125A3.75 3.75 0 0 0 15 4.375A3.75 3.75 0 0 0 11.77 10.03L9.48 12.32Q9.3 12.5 9.05 12.5Q8.44 12.5 8.44 11.89V10H10V2.5H1.875V10H3.44V11.875Q3.44 17.5 9.06 17.5H11.44Q12 18.75 13.75 18.75Q16.25 18.75 16.25 16.56Q16.25 14.375 13.75 14.375Q12 14.375 11.44 15.625H9.06Q5.3125 15.625 5.3125 11.875V10H6.5625V11.89Q6.5625 14.375 9.05 14.375Z"
      + polygon([[13.7, 5.94], [15.31, 7.55], [14.43, 8.44], [12.81, 6.82]]),
  },

  // An upright oval with the rune cut out of it, as Notion's (2026-11-17): up and down to the full edge, across edgeIn(12).
  bluetooth: { stroke: `${ellipse(10, 10, edgeIn(12) - HALF_LINE, FULL_EDGE - HALF_LINE)}M7.25 7.5 12.5 12 10 14.5v-9l2.5 2.5-5.25 4.5` },
  "boarding-pass": { stroke: "M3 5h14v3a2 2 0 0 0 0 4v3H3v-3a2 2 0 0 0 0-4ZM7 10h5.5M10.75 8.25 12.5 10l-1.75 1.75" },
  // Notion's boat (read 2026-11-18), filled: a hull widening up from its foot at 14.03 to 3.125..16.875 at 10, its foot dipping
  // at the middle, a cabin up to a funnel at the round edge's top with its window cut out, and a wave a line deep under it.
  boat: {
    fill: "M5.42 14.64Q6 15 6.25 15Q6.6 15 7.08 14.64Q8.3 13.75 10 13.75Q11.7 13.75 12.92 14.64Q13.5 15 13.75 15Q14.1 15 14.58 14.64Q15.1 14.2 15.875 14.03L16.875 10V9.27L15.71 9.08L14.375 3.75H12.5V1.875H7.5V3.75H5.625L4.29 9.08L3.125 9.27V10L4.125 14.03Q4.9 14.2 5.42 14.64Z"
      + "M7.09 5.625H12.91L13.69 8.74L10 8.125L6.31 8.74Z"
      + "M17.5 15V16.875C16 16.875 15.25 18.125 13.75 18.125C12.25 18.125 11.5 16.875 10 16.875C8.5 16.875 7.75 18.125 6.25 18.125C4.75 18.125 4 16.875 2.5 16.875V15C4 15 4.75 16.25 6.25 16.25C7.75 16.25 8.5 15 10 15C11.5 15 12.25 16.25 13.75 16.25C15.25 16.25 16 15 17.5 15Z",
  },

  bomb: { fill: circle(9, 11.5, 5.5), stroke: "M12.5 7.25 14 5.75c.75-.75 1.75-.75 2.5 0M17 3.25v-.75M18 4.5h.75M17.9 3.6l.5-.5" },
  bone: { stroke: "M5.9 14.1 14.1 5.9M14.1 5.9l-.5-2.45M14.1 5.9l2.45.5M5.9 14.1l.5 2.45M5.9 14.1l-2.45-.5" },
  book: { stroke: "M10 5.5c-1.75-1.25-4-1.75-7.25-1.5v11c3.25-.25 5.5.25 7.25 1.5 1.75-1.25 4-1.75 7.25-1.5V4c-3.25-.25-5.5.25-7.25 1.5ZM10 5.5v11" },
  "book-closed": { stroke: "M5.5 2.75H15v11.5H5.5a1.75 1.75 0 0 0-1.75 1.75V4.5A1.75 1.75 0 0 1 5.5 2.75ZM3.75 16a1.75 1.75 0 0 0 1.75 1.5H15v-3.25" },
  boombox: {
    stroke: `${rect(2.75, 6.5, 14.5, 9.5, 1.5)}M5.5 6.5V4.25h9V6.5M9.25 8.75h1.5${circle(6.5, 11.75, 2.25)}${circle(13.5, 11.75, 2.25)}`,
  },
  boot: { stroke: "M5.25 2.75h5.25v7.5l5 2a1.5 1.5 0 0 1 1 1.4v2.1H4.25v-2.5c.5-2 1-6.5 1-10.5ZM4.25 15.75h12.25" },
  // Notion's bounce (read 2026-11-18), filled: a band 6.25 deep from the box's left side dipping between two bounces, each
  // rounding down 3.125 in Notion's round to its foot at 15.625, into a head pointing right from 3.125 to 15.625.
  bounce: { fill: `M20 9.375L13.75 15.625H12.5V12.5H11.25${curve(back(roundQuarters(11.25, 15.625, 3.125)[3]))}H6.25${curve(back(roundQuarters(3.125, 15.625, 3.125)[0]))}H0.625V6.25H2.5Q6.4 6.25 7.19 9.28Q8 6.25 11.875 6.25H12.5V3.125H13.75Z` },
  bowl: { stroke: "M2.75 9h14.5a7.25 6.5 0 0 1-14.5 0ZM7.5 16.75h5" },
  "bowl-food": {
    stroke: "M2.75 10h14.5a7.25 6 0 0 1-14.5 0ZM7.5 17h5",
    fill: "M4.75 9.25a2.5 2.5 0 0 1 4.25-2 2.75 2.75 0 0 1 4.75.4 2.25 2.25 0 0 1 1.5 1.6Z",
  },
  bowling: {
    stroke: `${circle(6.75, 12.25, 4.5)}M14 3.5c1 0 1.5.75 1.5 1.75s-.75 1.5-.75 2.5c0 1.25 1.5 2.5 1.5 5.25 0 2-1 3.5-2.25 3.5s-2.25-1.5-2.25-3.5c0-2.75 1.5-4 1.5-5.25s-.75-1.5-.75-2.5.5-1.75 1.5-1.75Z`,
    fill: `${circle(5.5, 10.75, 0.7)}${circle(7.75, 10.25, 0.7)}${circle(6.75, 12.5, 0.7)}`,
  },
  bra: { stroke: "M3 9.5a3.5 4 0 0 0 7 0 3.5 4 0 0 0 7 0ZM4.5 8.55 5.75 3.5M15.5 8.55 14.25 3.5" },
  // Notion's brain (read 2026-11-18), filled: the profile's head facing right with a brain 5.94 across cut out of it, the cut
  // running on down its neck to the round edge's foot.
  brain: { fill: "M16.25 9.23Q16.25 9.53 15.92 9.61L15.14 9.8L14.83 11.68Q14.7 12.5 13.94 12.5Q13.79 12.5 13.64 12.46L11.875 12.02V13.125L13.47 16.31Q13.75 16.85 13.75 17.5H10.625V8.75L13.27 7.42Q13.28 7.31 13.28 7.18A2.97 2.97 0 0 0 10.32 4.21A2.97 2.97 0 0 0 9.375 10V17.5H5Q5 14.9 8.125 13.43V11.33A4.68 4.68 0 0 1 10.32 2.5A4.69 4.69 0 0 1 15 7.18Q15 7.29 14.99 7.4L16.15 8.95Q16.25 9.08 16.25 9.23Z" },
  // Notion's branch (read 2026-11-18): two heads pointing up from 1.8, their arms 4.06 back, over lines that run down from them
  // and bend in on a slant to meet in a stem down to the round edge's foot, the stem 0.06 left of the middle as Notion's.
  branch: {
    stroke: "M4.94 4.5V7.97C4.94 9.4 5.42 10.35 6.56 11.22L8.31 12.53C9.4 13.35 9.94 14.2 9.94 15.78V16.5625"
      + `M14.94 4.5V7.97C14.94 9.4 14.46 10.35 13.32 11.22L11.57 12.53C10.48 13.35 9.94 14.2 9.94 15.78${chevron(1.8, 4.06, 0, 5)}${chevron(1.8, 4.06, 0, 15)}`,
  },
  "branch-create": { stroke: `${circle(6, 4.75, 1.75)}${circle(6, 15.25, 1.75)}${circle(14, 6.75, 1.75)}M6 6.5v7M14 8.5c0 3.25-4 3.75-6.6 5.4` },
  // Notion's fork (read 2026-11-18): three rings 6.25 across at the top left and both foot corners of 2.5..17.5, a line down
  // between the left two, and from it a line bending out level at the middle and down into the third.
  "branch-fork": {
    stroke: "M5.625 8.75V11.25M5.625 8.9C6.25 9.75 7.4 10 9.06 10H10.94C12.2 10 13.3 10.6 13.82 11.3",
    fill: [[5.625, 5.625], [5.625, 14.375], [14.375, 14.375]].map(([x, y]) => `${circle(x!, y!, 3.125)}${circle(x!, y!, 1.25)}`).join(""),
  },
  "branch-merge": { stroke: `${circle(6, 4.75, 1.75)}${circle(6, 15.25, 1.75)}${circle(14, 10, 1.75)}M6 6.5v7M6 6.5c0 2.5 2.5 3.5 6.25 3.5` },
  bread: { stroke: "M4.5 9.25A2.75 2.75 0 0 1 5 4h10a2.75 2.75 0 0 1 .5 5.25V16a1 1 0 0 1-1 1h-9a1 1 0 0 1-1-1ZM8.25 8.25l1.5-1.5M10.75 10.75l1.5-1.5" },
  // Notion's briefcase (read 2026-11-18), filled: a case 15 by 10.625 from 6.25 with Notion's round corners 2.5, a band 1.25
  // deep cut across it at 10, and a handle 7.5 wide from the round edge's top, its top corners round 2.5, its hole 3.75 wide.
  briefcase: { fill: `${plate(2.5, 6.25, 17.5, 16.875, PLATE_CORNER)}${bar(4.375, 10, 11.25, 1.25)}${plate(6.25, 2.5, 13.75, 6.25, [2.5, 2.5, 0, 0])}${plate(8.125, 4.375, 11.875, 6.25, [0.625, 0.625, 0, 0])}` },
  "brightness-high": { stroke: `${circle(10, 10, 3.25)}${sunRays(WIDE_EDGE)}` },
  broccoli: { fill: "M9.47 7.7A3.6 3.6 0 0 1 2.6 6.32A3.6 3.6 0 0 1 9.37 4.49A3.3 3.3 0 0 1 14.34 3.41A3.3 3.3 0 0 1 14.46 8.49A4 4 0 0 1 14.4 16.32A4 4 0 0 1 11.28 9.14A3.3 3.3 0 0 1 9.47 7.7Z", stroke: "M9.2 10.8 4.4 15.6" },
  broom: { stroke: "M10 2.2v6.2M5.3 9.4h9.4M6.2 12.8h7.6l.9 3.15H5.3Z" },
  "broom-and-dustpan": { stroke: "M9.5 2.75 6.5 8.5M6.5 8.5c-2-.75-3.5.75-3.75 3.25L2.5 14l2.25-.5c2.5-.5 3.75-2.25 1.75-5ZM10.5 17h6.75l-1-4.75h-4.75ZM13.75 12.25V9.75" },
  "browser-stop": { stroke: `${octagon}M7.5 7.5l5 5M12.5 7.5l-5 5` },
  // Notion's bug (read 2026-11-18), filled, alike either side of the middle: a head 5.625 across from the round edge's top,
  // and each half of its body a line apart from the other, with three legs out to the box's side.
  bug: { fill: `M7.19 5.625${curve(roundQuarters(10, 5.625, 2.81, 3.125)[3])}${curve(roundQuarters(10, 5.625, 2.81, 3.125)[0])}Z${bugHalf}${mirrored(bugHalf)}` },
  bugle: { stroke: "M2.75 9h1.5v3h-1.5ZM4.25 10.5h7.25l5-3.75v7.5l-5-3.75M6.5 10.5v2.25a1.25 1.25 0 0 0 1.25 1.25h2.5a1.25 1.25 0 0 0 1.25-1.25V10.5" },
  bullseye: {
    stroke: `${circle(9.5, 10.5, 6.5)}${circle(9.5, 10.5, 3.5)}M9.5 10.5 15.75 4.25M13.25 3.75l2.75-.25-.25 2.75`,
    fill: circle(9.5, 10.5, 1),
  },
  // A bunk bed from the side: its head post, two bunks with a pillow on each, and the ladder at its foot.
  "bunk-bed": {
    fill: "M2.75 2.5H4.5v3.75h.75c0-1.25.75-1.75 1.75-1.75s1.5.75 1.75 1.75h3.5v2.5h-8v4h.75c0-1.25.75-1.75 1.75-1.75s1.5.75 1.75 1.75h3.5v2.5h-8v2.25H2.75Z",
    stroke: "M13.25 3v14M16.75 3v14M13.25 5.75h3.5M13.25 10h3.5M13.25 14.25h3.5",
  },
  burger: {
    stroke: "M3.5 8.5a6.5 4.75 0 0 1 13 0ZM3 11.25h14M3.5 13.75h13v1.5a1.75 1.75 0 0 1-1.75 1.75h-9.5a1.75 1.75 0 0 1-1.75-1.75Z",
    fill: `${circle(8, 6, 0.55)}${circle(11, 5.5, 0.55)}${circle(12.75, 7, 0.55)}`,
  },
  // Its rays end half a grid step inside the round edge, as Notion's burst does (2.8..17.2).
  burst: { stroke: rays(8, 2.8, ROUND_EDGE - 0.3125 - HALF_LINE) },
  bus: {
    stroke: `${rect(4, 3, 12, 12.5, 2)}M4 9h12M6 15.5V17M14 15.5V17M4 6H3M16 6h1`,
    fill: `${circle(6.75, 12.25, 0.8)}${circle(13.25, 12.25, 0.8)}`,
  },
  "bus-double-decker": {
    stroke: `${rect(2.75, 3.5, 14.5, 11.5, 1.5)}M2.75 9.25h14.5M5 6.25h2.5M8.75 6.25h2.5M12.5 6.25h2.5M5 11.75h2.5M8.75 11.75h2.5${circle(6, 15.25, 1.5)}${circle(14, 15.25, 1.5)}`,
  },
  "bus-metro": { stroke: `${rect(4.7, 3.45, 10.6, 11.85, 2)}M3.45 7v2M16.55 7v2M5.35 16.3v.25M14.65 16.3v.25`, fill: `${bar(6.1, 6.1, 3.1, 3.4)}${bar(10.8, 6.1, 3.1, 3.4)}${bar(8.2, 4, 3.6, 0.9)}${circle(6.8, 12.6, 0.9)}${circle(13.2, 12.6, 0.9)}` },
  butterfly: { stroke: "M10 7c-1.5-3-5-4.5-6.5-3.25-1.5 1.25.25 5.75 4 6.25-3 .75-3.5 4.25-1.5 5s3.5-1.75 4-4.25c.5 2.5 2 5 4 4.25s1.5-4.25-1.5-5c3.75-.5 5.5-5 4-6.25C12.5 2.5 11.5 4 10 7ZM10 7v8" },
};
