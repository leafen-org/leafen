import {
  EDGE_STEP,
  n,
  FULL_EDGE,
  HALF_LINE,
  bar,
  circle,
  cog,
  diamond,
  ellipse,
  heart,
  hexagon,
  hexagonCorners,
  lens,
  polygon,
  polygonPart,
  rect,
  smallCheck,
  smallPlus,
  type NativeIconGlyph,
} from "./nativeIconGlyphShapes";
import { blockArrow } from "./nativeIconArrows";
import { curve, plate, roundQuarters } from "./nativeIconRound";

/** A gear of eight teeth about (cx, cy), the first `turn` degrees round from three o'clock: each tooth 1.25 across at 5.04 out on roots 15° either side at 3.75, the root between them round. */
const gear = (cx: number, cy: number, turn: number) => {
  const at = (r: number, degrees: number) => `${n(cx + r * Math.cos((degrees * Math.PI) / 180))} ${n(cy + r * Math.sin((degrees * Math.PI) / 180))}`;
  return Array.from({ length: 8 }, (_, tooth) => {
    const middle = turn + tooth * 45;
    return `${tooth ? "A3.75 3.75 0 0 1 " : "M"}${at(3.75, middle - 15)}L${at(5.04, middle - 7.15)}L${at(5.04, middle + 7.15)}L${at(3.75, middle + 15)}`;
  }).join("") + `A3.75 3.75 0 0 1 ${at(3.75, turn - 15)}Z`;
};
import { PERSON_HEAD, personBeside, shouldersBehind, smallPerson, standing } from "./nativeIconPerson";

// Leafen glyphs for catalog names from "flag-swallowtail" through "lemon".

// Two people on a base at 15, the usual head each, the one behind cut a
// neck clear of the front one's shoulders (nativeIconPerson.ts).
const friendFront = standing(6.25, 15, PERSON_HEAD), friendBehind = standing(13.75, 15, PERSON_HEAD);
const FRIENDS = `${friendFront.head}${friendBehind.head}${friendFront.outline}${shouldersBehind(friendBehind.shoulders, friendFront.shoulders)}`;
// A group on the same base: the usual person in the middle and a small one
// each side (smallPerson), its head seven half steps round and its
// shoulders reaching the box's side, both cut a neck clear of the middle
// one's (Notion's groups rastered at 400px, 2026-11-18).
const groupMiddle = standing(10, 15, PERSON_HEAD);
const groupSide = (cx: number) => standing(cx, 15, 7 * EDGE_STEP, smallPerson(7 * EDGE_STEP));
const GROUPS = [groupMiddle, groupSide(13 * EDGE_STEP), groupSide(20 - 13 * EDGE_STEP)]
  .map((one) => `${one.head}${one === groupMiddle ? one.outline : shouldersBehind(one.shoulders, groupMiddle.shoulders)}`).join("");
const smallCloud = "M6.5 11a3 3 0 0 1-.3-6A4 4 0 0 1 13.8 4.3a3.3 3.3 0 0 1 .2 6.7Z";
const skateBoot = "M5 3h4.5v6l5 2.25c1 .45 1.5 1.25 1.5 2.25V14H5Z";
// A machine with its door's window, a slot and a light cut out of it; what turns inside fills the window.
const laundry = `${rect(3.5, 2.75, 13, 14.5, 1.5)}${rect(5.5, 4.6, 2.75, 1.3, 0.65)}${circle(13.5, 5.25, 0.8)}${circle(10, 11, 4.25)}`;
const hexagonPart = (parts: number): NativeIconGlyph => ({ stroke: hexagon, fill: polygonPart(6, parts, hexagonCorners) });
/** How far a gear's teeth go out for their flat tops' outside to reach the full edge. */
const gearTeeth = (FULL_EDGE - HALF_LINE) / Math.cos((0.17 * Math.PI) / 4);
const helmSpokes = Array.from({ length: 8 }, (_, index) => {
  const angle = (index * Math.PI) / 4;
  const point = (r: number) => `${(10 + r * Math.cos(angle)).toFixed(2)} ${(10 + r * Math.sin(angle)).toFixed(2)}`;
  return `M${point(1.5)}L${point(7.5)}`;
}).join("");

export const NATIVE_ICON_GLYPHS_FL: Readonly<Record<string, NativeIconGlyph>> = {
  "flag-swallowtail": { stroke: "M5 17V3M5.95 3.5H16l-3 3.75 3 3.75H5.95Z" },
  // Notion's flashlight (read 2026-11-18), filled: lying at 45° from the foot left corner up to its head, and three rays from
  // the head toward the top right.
  flashlight: {
    fill: polygon([[9.375, 5.625], [14.375, 10.625], [11.875, 13.125], [9.53, 13.125], [4.53, 18.125], [1.875, 15.47], [6.875, 10.47], [6.875, 8.125]])
      + `${polygon([[13.125, 1.02], [11.94, 0.625], [10.625, 4.57], [11.81, 4.97]])}${polygon([[18.98, 6.875], [15.03, 8.19], [15.43, 9.375], [19.375, 8.06]])}${polygon([[17.5, 3.38], [16.62, 2.5], [13.44, 5.68], [14.32, 6.56]])}`,
  },
  flatware: { stroke: `M3.5 3v3.5M5 3v3.5M6.5 3v3.5M3.5 6.5a1.5 1.5 0 0 0 3 0M5 8v9${ellipse(10.25, 5.5, 1.75, 2.5)}M10.25 8v9M16 17V3c-1.5 1-2 3-2 6h2` },
  "fleur-de-lis": { stroke: "M10 2.5c-1.75 2-1.75 5 0 7.5 1.75-2.5 1.75-5.5 0-7.5ZM10 10c-1.5-2.5-5-3-6-.75-.75 1.75 1.25 3 2.5 2M10 10c1.5-2.5 5-3 6-.75.75 1.75-1.25 3-2.5 2M6.5 11.5h7M10 10v7M8 14.5l2 2.5 2-2.5" },
  flood: { stroke: "M4.5 11.5V9L10 4l5.5 5v2.5ZM3 14c1.2.9 2.3.9 3.5 0s2.3-.9 3.5 0 2.3.9 3.5 0 2.3-.9 3.5 0M3 17c1.2.9 2.3.9 3.5 0s2.3-.9 3.5 0 2.3.9 3.5 0 2.3-.9 3.5 0" },
  fog: { stroke: `${smallCloud}M3.5 13.75h13M5 16.5h10` },
  // A person with a small mark beside it (nativeIconGlyphShapes.ts), five right of the box's middle.
  follow: { fill: `${personBeside}${smallPlus(15)}` },
  following: { fill: `${personBeside}${smallCheck(15)}` },
  // A capital and a small a.
  // Notion's font (read 2026-11-18), filled: a capital A 2.74 across at its top on 4.45, its feet 1.99 wide on 15.55, its bar's
  // foot on 13.05 and a hole above it to 6.3; and a small a: a stem 1.68 wide from 8.61 down, and to its left a bowl 6.56 by 7.28
  // with a hole 3.44 by 3.92.
  font: {
    fill: polygon([[4.96, 4.45], [7.7, 4.45], [11.53, 15.55], [9.55, 15.55], [8.69, 13.05], [3.98, 13.05], [3.12, 15.55], [1.13, 15.55]])
      + polygon([[4.62, 11.18], [8.04, 11.18], [6.33, 6.3]])
      + "M18.87 8.61V15.55H17.19V14.59A3.28 3.64 0 1 1 17.19 9.25V8.61Z" + ellipse(15.28, 11.91, 1.72, 1.96),
  },
  "food-and-drink": { stroke: "M2.6 7.6h4.8l-.65 8.35h-3.5ZM.95 5.85h8.1M6.1 4.9l.5-2.7M11.05 9.05a3.3 1.15 0 0 1 6.6 0ZM10.35 12.65h8.1M11.05 15.35h6.6v.6h-6.6Z" },
  football: { stroke: "M4 16c-1-4 0-8 3.5-10.5C11 3 15 3 16 4s1 5-1.5 8.5C12 16 8 17 4 16ZM8 12l4-4M8.75 9.5l1.75 1.75M10.25 8l1.75 1.75" },
  "forest-fire": {
    stroke: "M7 2.75 11.25 11H2.75ZM7 11v4.5",
    fill: "M14 17c-1.9 0-3.1-1.3-3.1-2.9 0-1.7 1.35-2.5 1.75-4 .15-.5.7-.6 1-.2.55.8.7 1.55.65 2.25.5-.35.8-.8.9-1.3.1-.4.55-.5.8-.2.75.9 1.15 1.95 1.15 3.05 0 1.8-1.25 3.3-3.15 3.3Z",
  },
  // Notion's fork (read 2026-11-18), filled, standing: a handle a line wide from the round edge's foot up the middle to a head
  // 5.625 across from the round edge's top, its foot rounding in to the handle at 9.88, three slots 0.94 wide cut down to 7.19.
  fork: { fill: "M12.8125 2.5V7.19Q12.8125 9.4 10.94 9.88V17.5H9.06V9.88Q7.1875 9.4 7.1875 7.19V2.5H8.44V7.19H9.375V2.5H10.625V7.19H11.56V2.5Z" },
  "fork-and-knife": { stroke: "M4.5 3v3.5M6 3v3.5M7.5 3v3.5M4.5 6.5a1.5 1.5 0 0 0 3 0M6 8v9M14.5 10.5V3c-1.75 1-2.5 3-2.5 6.5v1ZM14.5 17v-5.55" },
  // A sum's sigma.
  formula: { stroke: "M13.7 4.25H6.3l5.75 5.75-5.75 5.75h7.4" },
  forward: { fill: blockArrow(1) },
  fragile: { stroke: "M6.35 3.45h7.3l.95 3.75c.35 2.2-1.7 3.15-4.6 3.15s-4.95-.95-4.6-3.15ZM9.6 3.45l1.1 2.15-1.1 1.6.8 1.4M7.55 16.55h4.9", fill: bar(9.4, 11.25, 1.2, 4.5) },
  // Two people side by side, drawn solid, the one behind cut where the other
  // stands, as Notion's mark is (seen 2026-11-15: 17.5 by 11.25 on its 20 box).
  friends: { fill: FRIENDS },
  "frying-pan": { fill: `${circle(11.25, 8.75, 6.25)}${circle(11.25, 8.75, 3.6)}${circle(11.25, 8.75, 1.5)}`, stroke: "M6.6 13.4 3.45 16.55" },
  // Notion's fuel pump (read 2026-11-18): a body 8.75 wide from 2.5 to 15 with a window 5 by 3.75 cut in it, on a foot across
  // 1.25..13.75; its hose a line from the body's side at 9.06 bending down to a loop round 1.875 and up the box's side to a
  // nozzle slanting back to the round edge's top.
  fuel: {
    fill: `${bar(3.125, 2.5, 8.75, 12.5)}${bar(5, 4.375, 5, 3.75)}${bar(1.25, 15, 12.5, 1.875)}`,
    stroke: "M12 9.0625H12.81Q14.69 9.0625 14.69 10.94V12.19A1.875 1.875 0 0 0 18.44 12.19V6.55L15.7 3.83",
  },
  // Notion's pawn (read 2026-11-18), filled: a head 6.25 across on 5.625, a neck from 8.29 flaring down at 3:1 to a foot that
  // bends out level to the round edge's sides 15.625..17.5 across 12.5.
  "game-pawn": { fill: "M3.75 17.5V15.625Q5.94 15.625 6.63 13.55L8.38 8.29A3.125 3.125 0 1 1 11.62 8.29L13.37 13.55Q14.06 15.625 16.25 15.625V17.5Z" },
  garlic: { stroke: "M10 3.45V4.6c.6 1.4 2.5 2.4 4.1 3.6 1.6 1.2 2.45 2.5 2.45 3.7 0 1.5-1.2 2.4-2.6 2.8-.7.2-1.4.45-1.85.9-.45.9-1.15 1.55-2.1 1.55s-1.65-.65-2.1-1.55c-.45-.45-1.15-.7-1.85-.9-1.4-.4-2.6-1.3-2.6-2.8 0-1.2.85-2.5 2.45-3.7 1.6-1.2 3.5-2.2 4.1-3.6Z" },
  // Notion's gavel (read 2026-11-18), filled: a head turned 45° with a ridge near each end, its handle down to the foot left,
  // and a block across 9.375..18.125 on the round edge's foot.
  gavel: {
    fill: "M9.86 11.11L3.46 17.5L1.25 15.29L7.65 8.9L7.5 8.75Q7 8.2 7.2 7.47L7.4 6.88L6.96 6.44L6.36 6.64Q5.6 6.9 5.08 6.33L4.375 5.625L9.375 0.625L10.09 1.33Q10.6 1.9 10.39 2.61L10.19 3.21L10.625 3.65L11.22 3.45Q11.95 3.2 12.5 3.75L15 6.25Q15.55 6.8 15.3 7.53L15.11 8.125L15.55 8.56L16.14 8.37Q16.9 8.1 17.42 8.67L18.125 9.375L13.125 14.375L12.42 13.67Q11.9 13.1 12.12 12.39L12.31 11.8L11.87 11.36L11.28 11.55Q10.55 11.8 10 11.25Z"
      + bar(9.375, 15.625, 8.75, 1.875),
  },
  gear: { stroke: `${cog(8, gearTeeth, gearTeeth * 0.74)}${circle(10, 10, gearTeeth * 0.29)}` },
  // Notion's gears (read 2026-11-18), filled: two like gears at the top right and the foot left, each of eight teeth 1.25 across
  // at 5.04 from its middle on roots 3.75 out, the top one's turned an eighth of a tooth, and a hole 3.75 across.
  gears: { fill: `${gear(13.75, 6.25, 22.5)}${circle(13.75, 6.25, 1.875)}${gear(6.25, 13.75, 0)}${circle(6.25, 13.75, 1.875)}` },
  gem: { stroke: "M6 3.5h8L17 7.5 10 17 3 7.5ZM3 7.5h14M8 3.5 6.5 7.5 10 17M12 3.5l1.5 4L10 17" },
  gemini: { stroke: "M4.5 3.5c3.5 1 7.5 1 11 0M4.5 16.5c3.5-1 7.5-1 11 0M7.5 4.5v11M12.5 4.5v11" },
  geography: { stroke: `${circle(10, 8, 4.75)}${ellipse(10, 8, 2, 4.75)}M5.25 8h9.5M4 7.5a6 6 0 0 0 11.25 3.5M10 14V17M7 17h6` },
  // Notion's ghost (read 2026-11-18), filled: a body with a round top from 1.875, its arms raised to 5.625 either side, its hem
  // waving between 16.875 and the round edge's foot, and two eyes 1.875 by 3.125 and a mouth 2.5 across cut out.
  ghost: {
    fill: "M17.5 5.625Q16.25 5.625 16.25 6.875V7.5Q16.25 8.125 15.625 8.125H14.79L14.37 5.58Q13.75 1.875 10 1.875Q6.25 1.875 5.625 5.58L5.21 8.125H4.375Q3.75 8.125 3.75 7.5V6.875Q3.75 5.625 2.5 5.625Q1.25 5.625 1.25 6.875V7.5Q1.25 11 4.69 11.24"
      + "L3.75 16.875Q4.6 17.5 5.94 17.5Q7.6 17.5 10 16.875Q12.4 17.5 14.06 17.5Q15.4 17.5 16.25 16.875L15.31 11.24Q18.75 11 18.75 7.5V6.875Q18.75 5.625 17.5 5.625Z"
      + `${plate(7.1875, 5.3125, 9.0625, 8.4375, 0.9375)}${plate(10.9375, 5.3125, 12.8125, 8.4375, 0.9375)}${circle(10, 10.625, 1.25)}`,
  },

  gift: { stroke: "M3 7h14v3.5H3ZM4 10.5V17h12v-6.5M10 7v10M10 7C7.5 7 6 6 6 4.75 6 3.5 7.5 3 10 7ZM10 7c2.5 0 4-1 4-2.25C14 3.5 12.5 3 10 7Z" },
  git: {
    stroke: `${diamond}M7.75 7.75l3 3M10.75 10.75V14`,
    fill: `${circle(7.75, 7.75, 1.15)}${circle(10.75, 10.75, 1.15)}${circle(10.75, 14, 1.15)}`,
  },
  // Notion's (rastered at 400px, 2026-11-18): a bar a line thick, and under it two lenses a line wide, each straight down
  // and then round to its foot (nativeIconGlyphShapes.ts lens), their insides cut out.
  glasses: {
    fill: `${bar(0.625, 7.1875, 18.75, 1.875)}${[5.625, 14.375].map((cx) => `${lens(cx, 9.0625, 10.3, 3.59, 3.76)}${lens(cx, 9.0625, 10.3, 1.715, 1.885)}`).join("")}`,
  },
  golf: { stroke: "M8 17V3M8.95 3.4 14 5.5 8.95 7.6ZM4 17h8", fill: circle(14, 15.25, 1.5) },
  government: { stroke: "M3 17h14M4 15h12M5 15V9.5M8.33 15V9.5M11.67 15V9.5M15 15V9.5M3.5 9.5h13L10 6ZM10 6V3M10 3h2l-.5.75.5.75h-2" },
  gradebook: { stroke: "M5.5 2.75H15v11.5H5.5a1.75 1.75 0 0 0-1.75 1.75V4.5A1.75 1.75 0 0 1 5.5 2.75ZM3.75 16a1.75 1.75 0 0 0 1.75 1.5H15v-3.25M10.5 2.75V7l1.5-1 1.5 1V2.75" },
  graduate: { stroke: "M10 4 2.5 7.5 10 11l7.5-3.5ZM5.5 9v3.75c0 1.25 2 2.25 4.5 2.25s4.5-1 4.5-2.25V9M17.5 7.5V12" },
  grapes: { fill: "M7.56 6.23A1.85 1.85 0 0 1 6.32 3.28A1.85 1.85 0 0 1 9.5 3.67A1.85 1.85 0 0 1 12.74 3.74A1.85 1.85 0 0 1 10.95 6.44A1.85 1.85 0 0 1 10.71 6.79A1.85 1.85 0 0 1 10.93 7.41A1.85 1.85 0 0 1 14.43 8.46A1.85 1.85 0 0 1 15.24 9.86A1.85 1.85 0 0 1 16.93 11.96A1.85 1.85 0 0 1 14.72 13.51A1.6 1.6 0 0 1 11.64 13.37A1.85 1.85 0 0 1 9.9 15.05A1.85 1.85 0 0 1 6.26 14.75A1.85 1.85 0 0 1 6.24 14.75A1.85 1.85 0 0 1 3.06 15.87A1.85 1.85 0 0 1 4.36 12.75A1.85 1.85 0 0 1 4.36 12.75A1.85 1.85 0 0 1 2.55 10.85A1.85 1.85 0 0 1 4.45 9.05A1.85 1.85 0 0 1 4.55 6.07A1.85 1.85 0 0 1 7.37 7.05A1.85 1.85 0 0 1 7.69 6.51A1.85 1.85 0 0 1 7.56 6.23ZM5.65 9.45A1.85 1.85 0 0 1 5.55 9.45A1.85 1.85 0 0 1 5.65 9.54A1.85 1.85 0 0 1 5.65 9.45Z", stroke: "M12.9 6.3c1.5-.9 2.75-2.3 3.4-3.6" },
  // Notion's grave (read 2026-11-18), filled: a stone across 3.125..16.875 from the round edge's foot, its top rising in an
  // S from 5.625 at its sides to 2.5 at its middle, and a tulip with two leaves cut out of it.
  grave: {
    fill: "M3.125 5.625C5.3 5.625 6.5 2.5 10 2.5C13.5 2.5 14.7 5.625 16.875 5.625V17.5H3.125Z"
      + "M10.83 15.625H9.17L5.94 12.4L6.46 11.875Q8.6 11.9 9.375 13.68V10.54A2.5 2.5 0 0 1 7.5 8.125V5.625H8.125L8.91 7.19L9.69 5.625H10.31L11.09 7.19L11.875 5.625H12.5V8.125A2.5 2.5 0 0 1 10.625 10.54V13.68Q11.4 11.9 13.54 11.875L14.06 12.4Z",
  },
  "grid-dense": { fill: [3, 8, 13].flatMap((y) => [3, 8, 13].map((x) => rect(x, y, 4, 4, 1))).join("") },
  "grid-wide": { fill: [4, 10.75].flatMap((y) => [2.75, 10.5].map((x) => rect(x, y, 6.75, 5.25, 1.25))).join("") },
  "grid-wide-six": { fill: [4, 10.75].flatMap((y) => [2.75, 7.9, 13.05].map((x) => rect(x, y, 4.2, 5.25, 1))).join("") },
  grocery: {
    stroke: "M3 8.5 4.25 6h4.5L10 8.5V17H3ZM3 8.5h7M14 10.25c0-.9.35-1.5 1.05-1.95",
    fill: "M14 10.5c.8-.55 1.6-.8 2.35-.6 1.4.4 1.95 2 1.6 3.75-.45 2-1.8 3.6-2.85 3.6-.5 0-.75-.25-1.1-.25s-.6.25-1.1.25c-1.05 0-2.4-1.6-2.85-3.6-.35-1.75.2-3.35 1.6-3.75.75-.2 1.55.05 2.35.6Z",
  },
  groups: { fill: GROUPS },
  guitar: { stroke: "M10 8.45c-2 0-3.05 1.15-3.05 2.5 0 1.05.55 1.6.55 2.25 0 .55-1.55 1.4-1.55 3.35 0 1.9 1.6 2.5 4.05 2.5s4.05-.6 4.05-2.5c0-1.95-1.55-2.8-1.55-3.35 0-.65.55-1.2.55-2.25 0-1.35-1.05-2.5-3.05-2.5Z", fill: `${circle(10, 12.2, 1.2)}${bar(8.4, 15.6, 3.2, 1.3)}${bar(9.4, 3.6, 1.2, 4)}${bar(8.1, 0, 3.8, 3.7)}` },
  gym: { stroke: `M6.5 10h7M2.75 8v4M17.25 8v4${rect(4.25, 6, 2.25, 8, 0.75)}${rect(13.5, 6, 2.25, 8, 0.75)}` },
  hail: { stroke: smallCloud, fill: `${circle(6.5, 14, 0.9)}${circle(10, 15.25, 0.9)}${circle(13.5, 14, 0.9)}${circle(8.25, 17.25, 0.9)}${circle(11.75, 17.5, 0.9)}` },
  "hair-care": { stroke: "M4 8h5v9H4ZM5.5 8V5.5h2V8M5 5.5h3M11 6.5h5V17h-5ZM12.5 6.5V4h2v2.5M12 4l1-1.25h1.5" },
  // Notion's hair dryer (read 2026-11-18), filled: a body round at the back from 3.75 to 11.25, its nozzle to the box's right
  // on 5.625..9.375, and a handle slanting down to the round edge's foot.
  hairdryer: { fill: "M18.125 9.375H13.98Q12.9 11.25 10.625 11.25H8.125L9.375 16.25H6.25L4.99 11.21Q1.875 10.8 1.875 7.5Q1.875 3.75 5.625 3.75H10.625Q12.9 3.75 13.98 5.625H18.125Z" },
  hammer: { stroke: `${polygon([[10.66, 2.18], [16.32, 7.84], [13.84, 10.32], [8.18, 4.66]])}M11.1 7.4 3.5 15` },
  hanafuda: {
    stroke: `${rect(4.5, 2.75, 11, 14.5, 1.75)}M4.5 13c2-1.5 4-1.5 5.5 0s3.5 1.5 5.5 0`,
    fill: circle(10, 8, 2.5),
  },
  hand: { stroke: "M6.25 9.05v-3.8M8.75 9.05v-5.3M11.25 9.05v-4.8M13.75 9.05v-2.8M6.25 10h9v2a5.5 5.5 0 0 1-5.5 5.5c-2 0-3.25-1-4.5-2.75L3.25 11.5a1.3 1.3 0 0 1 2-1.6Z" },
  handbag: { stroke: "M4 8h12l-1 9H5ZM7 8V6.5a3 3 0 0 1 6 0V8" },
  // Notion's hanger (read 2026-11-18), filled, upright: a hook round 3.44 about (10, 5.31) from 1.875 down to a neck at 9.22,
  // and a triangle widening to the box's sides on 12.81..16.875, its inside cut out.
  hanger: {
    fill: "M10.94 9.22V8.7L11.54 8.4A3.43 3.43 0 0 0 13.44 5.31A3.44 3.44 0 0 0 6.56 5.31H8.44A1.56 1.56 0 0 1 11.56 5.31Q11.56 6.3 10.7 6.73L9.06 7.55V9.22L1.875 12.81V16.875H18.125V12.81Z"
      + polygon([[16.25, 15], [3.75, 15], [3.75, 13.97], [10, 10.85], [16.25, 13.97]]),
  },
  // Notion's hare (read 2026-11-18), filled, crouched facing right: its head at the right with an ear laid back up to the round
  // edge's top, its round back to a tail at the left, and its legs folded under it to the round edge's foot.
  hare: { fill: "M18.75 9.39Q18.75 10.625 17.48 10.625H15L13.75 12.5L14.27 13.02Q15 13.75 15 14.79V17.5H13.125V15.8Q13.125 14.9 12.27 14.62L7.14 12.91A3.1 3.1 0 0 1 5.03 13.76Q5.25 13.97 5.54 14.12L6.62 14.66Q7.6 15.2 7.17 16.31L6.875 16.875L5.47 16.18A7.5 7.5 0 0 1 3.52 14.77L2.72 13.98A0.75 0.75 0 0 1 2.5 13.46A0.69 0.69 0 0 1 2.72 12.91L3.75 11.875L3.125 10.625Q1.25 10.625 1.25 8.76Q1.25 6.875 3.125 6.875Q3.5 6.875 3.79 6.98Q4.9 5.01 7.5 5.01Q10.7 5.01 11.875 8.125Q12.5 8.1 12.85 7.44L13.3 6.53L11.25 2.42L12.33 1.875Q15.3 2.9 15.86 5.71Q18.76 6.2 18.75 9.4Z" },

  headphones: { stroke: `M3.5 13v-2.5a6.5 6.5 0 0 1 13 0V13${rect(2.75, 11.5, 3.25, 5.5, 1.25)}${rect(14, 11.5, 3.25, 5.5, 1.25)}` },
  headset: { stroke: `M3.5 13v-2.5a6.5 6.5 0 0 1 13 0V13${rect(2.75, 11.5, 3.25, 5.5, 1.25)}${rect(14, 11.5, 3.25, 5.5, 1.25)}M4.5 17c0 .5 2 .75 5 .75` },
  // A heart-shaped box tied with a ribbon and a bow.
  "heart-box-bow": { stroke: `${heart}M10 10.75v4.5`, fill: "M10 8.5 6.75 6.75v3.5ZM10 8.5l3.25-1.75v3.5Z" },
  "heart-rate": { stroke: `${heart}M2.5 10.5H6l1.25-2 2 4 1.5-2.5h6.75` },
  "heart-rate-monitor": { stroke: `${rect(2.75, 3.75, 14.5, 12.5, 2)}M5 10.5h2.5l1-2 2 4 1-2H15` },
  heartbroken: { stroke: `${heart}M10 6l-1 3 2 1.5-1 2.5` },
  // Notion's helicopter (read 2026-11-18), filled, facing right: its rotor across 3.75..18.75 on 4.375..5.625 on a mast, a tail
  // boom to a fin at the box's left, and a cabin rounding down to its skid at 16.25, its window cut out.
  helicopter: {
    fill: "M19.03 13.28L16.65 9.32Q15.6 7.5 13.44 7.5H12.19V5.625H18.75V4.375H3.75V5.625H10.31V7.5H2.81L1.875 5.625H0L0.7 8.43L0 11.25H1.875L2.81 9.375H6.09L9.22 14.46Q10.3 16.25 12.41 16.25H17.35Q19.375 16.25 19.375 14.45Q19.375 13.85 19.03 13.28Z"
      + "M12.19 12.5V9.375H13.44Q14.6 9.375 15.05 10.29L16.38 12.5Z",
  },
  helm: { stroke: `${circle(10, 10, 5)}${circle(10, 10, 1.5)}${helmSpokes}` },
  "hexagon-one-sixth": hexagonPart(1),
  "hexagon-two-sixths": hexagonPart(2),
  "hexagon-three-sixths": hexagonPart(3),
  "hexagon-four-sixths": hexagonPart(4),
  "hexagon-five-sixths": hexagonPart(5),
  highball: { stroke: "M6 3h8l-1 14H7Z", fill: "M6.3 7.25h7.4L13 17H7Z" },
  history: { stroke: "M11 16.55A6.55 6.55 0 1 0 4.1 10v2.6M1.3 9.8l2.8 2.8 2.8-2.8M10.65 6.55v3.75h2.55" },
  "hot-air-balloon": { stroke: "M10 2.5a5.5 5.5 0 0 1 5.5 5.5c0 2.5-2.5 4.5-3.5 6.5H8C7 12.5 4.5 10.5 4.5 8A5.5 5.5 0 0 1 10 2.5ZM8 14.5h4v2.75H8ZM10 2.5c-1.5 2-1.5 10 0 12M10 2.5c1.5 2 1.5 10 0 12" },
  hourglass: {
    stroke: "M5.5 2.75h9M5.5 17.25h9M6.5 3.7c0 3.2 3.5 4 3.5 6.3s-3.5 3.1-3.5 6.3h7c0-3.2-3.5-4-3.5-6.3s3.5-3.1 3.5-6.3Z",
  },
  hurricane: { stroke: "M15.95 3.45H10a5.95 6.55 0 0 0 0 13.1H4.05 10a5.95 6.55 0 0 0 0-13.1Z", fill: circle(10, 10, 2.4) },
  "ice-skate": { stroke: `${skateBoot}M3.5 16.5h13a1 1 0 0 0 1-1M7 14v2.5M13 14v2.5` },
  // An officer behind the passport desk, a passport on it.
  immigration: { fill: `${circle(10, 4.5, 2)}M6 10a4 3.5 0 0 1 8 0Z${rect(2.5, 11, 15, 6, 1.25)}${bar(11.75, 12.75, 3, 2.75)}` },
  // Notion's inline skate (read 2026-11-18), filled: a boot from its cuff at the round edge's top, its toe sloping down to the
  // right, on four wheels 4.38 across in a row along the foot.
  "inline-skate": { fill: "M18.75 14.69A2.19 2.19 0 0 1 14.59 15.625H14.16A2.19 2.19 0 0 1 12.18 16.875A2.19 2.19 0 0 1 10.21 15.625H9.78A2.19 2.19 0 0 1 7.81 16.875A2.19 2.19 0 0 1 5.83 15.625H5.4A2.19 2.19 0 1 1 2.49 12.71V8.45Q2.49 7.3 2.72 6.63L3.125 5.03V2.5H9.375V5.625L14.375 10L17.49 10.78V12.71A2.19 2.19 0 0 1 18.75 14.69Z" },
  invitation: { stroke: "M3 8.5 10 3.5l7 5V16a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1ZM3 8.5l7 5 7-5M5.5 7.5V5h9v2.5" },
  iterate: { stroke: "M16.55 9.2A6.55 6.55 0 1 0 10 15.95h6.6M13.9 12.6l3.35 3.35-3.35 3.35", fill: circle(10, 9.4, 2.5) },
  "jack-o-lantern": {
    stroke: "M10 5c3.75 0 6.5 2.5 6.5 6s-2.75 6-6.5 6-6.5-2.5-6.5-6 2.75-6 6.5-6ZM10 5V3",
    fill: `${polygon([[6.5, 10], [8.5, 10], [7.5, 8.25]])}${polygon([[11.5, 10], [13.5, 10], [12.5, 8.25]])}M6.5 12.25h7l-1 1.5-1.25-.75-1.25.75-1.25-.75-1.25.75Z`,
  },
  // Its lid and foot on the square edge.
  jar: { stroke: "M5 6.975h10v7.95a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1ZM4.075 4.075h11.85v2.9H4.075ZM7.5 11h5" },
  "judicial-scales": { stroke: "M10 3v14M6 17h8M4 5.5h12M4 5.5l-2 5a2 2 0 0 0 4 0ZM16 5.5l-2 5a2 2 0 0 0 4 0Z" },
  junk: { stroke: "M3.5 6h13l-1.25 11H4.75ZM2.75 6h14.5M7.5 9.5l5 5M12.5 9.5l-5 5M8 6V4h4v2" },
  // Notion's key (read 2026-11-18), filled: a bow 10 across at the top right with a hole 1.875 across in it, and a shank down
  // at 45° to the box's foot corner, cut square there, its lower side stepped twice for the bit.
  key: { fill: `M7.85 9.33L2.5 14.69V17.5H5.31L5.94 16.88V15.31H7.5L8.13 14.69V13.13H9.69L10.67 12.15A5 5 0 1 0 7.85 9.33Z${circle(14.0625, 5.9375, 0.9375)}` },
  "key-antique": { fill: `${circle(12.95, 6.4, 3.85)}${circle(12.95, 6.4, 1.95)}`, stroke: "M10.4 9 4.2 15.2M6 13.4l1.5 1.5M8.2 11.2l1.5 1.5" },
  keyboard: {
    stroke: `${rect(2.25, 5, 15.5, 10, 1.5)}M7 12.5h6`,
    fill: [5, 8, 11, 14].flatMap((x) => [circle(x + 0.5, 8, 0.7), circle(x + 1.25, 10.25, 0.7)]).join(""),
  },
  "keyboard-alternate": {
    fill: `${[3, 6.75, 10.5, 14.25].flatMap((x) => [rect(x, 5, 2.75, 2.75, 0.6), rect(x, 8.6, 2.75, 2.75, 0.6)]).join("")}${rect(6.75, 12.25, 6.5, 2.75, 0.6)}`,
  },
  keypad: { fill: [4, 8, 12, 16].flatMap((y) => [6, 10, 14].map((x) => circle(x, y, 1.2))).join("") },
  kind: { stroke: `${circle(6.5, 6.5, 3)}${rect(11, 3.5, 5.5, 5.5, 1)}${polygon([[10, 11], [14, 17], [6, 17]])}` },
  // Notion's kite (read 2026-11-18), filled: a diamond 13.75 across with its points cut square, two of its quarters cut out, and
  // its tail down from the foot point bending round to the right.
  kite: {
    fill: "M16.875 8.125L10.625 1.875V0.625H9.375V1.875L3.125 8.125H1.875V9.375H3.125L9.375 15.625V16.5625A2.1875 2.1875 0 0 0 11.5625 18.75H15V17.5H11.5625A0.9375 0.9375 0 0 1 10.625 16.5625V15.625L16.875 9.375H18.125V8.125Z"
      + `${polygon([[9.375, 8.125], [4.89, 8.125], [9.375, 3.64]])}${polygon([[10.625, 13.86], [10.625, 9.375], [15.11, 9.375]])}`,
  },
  // Notion's knife (read 2026-11-18), filled, standing: a handle a line wide from the round edge's foot up the middle, and a
  // blade 3.75 wide down its left from the round edge's top to 9.98, its edge Notion's round.
  knife: { fill: `M10.94 2.5V17.5H9.06V9.98H7.19V9.36${curve(roundQuarters(10.94, 9.36, 3.75, 6.86)[3])}Z` },
  // Notion's kitchen knife (read 2026-11-18), filled: a handle at 45° at the top right with a notch at its end, and a blade
  // from it down to the foot left, its back straight and its edge bellied.
  "knife-kitchen": { fill: "M18.125 5.15L16.82 6.46Q16.4 6.875 15.92 6.875Q15.45 6.875 15.03 6.47L12.17 9.34L14.375 11.55L12.41 13.51Q8.6 17.5 5.31 17.5Q3.3 17.5 1.875 16.1L15.47 2.5Z" },
  language: {
    stroke: "M3 5a1.5 1.5 0 0 1 1.5-1.5h6A1.5 1.5 0 0 1 12 5v4.5a1.5 1.5 0 0 1-1.5 1.5H7l-2.5 2v-2A1.5 1.5 0 0 1 3 9.5ZM5.5 9 7.5 5l2 4M6.1 7.8h2.8M14 8h1.5A1.5 1.5 0 0 1 17 9.5V14a1.5 1.5 0 0 1-1.5 1.5v2l-2.5-2h-2.5A1.5 1.5 0 0 1 9 14v-.5M11.5 11.75h3.5M13.25 10.75v1M12 14c1.25-.5 2-1.25 2.5-2.25",
  },
  laptop: { stroke: `${rect(4, 4, 12, 8.5, 1.25)}M2 15h16l-1 1.5H3Z` },
  "laundry-basket": { stroke: "M3 8h14l-1.5 9h-11ZM6 8l2-3.5M14 8l-2-3.5M6 11h8M6.5 14h7" },
  "laundry-detergent": { stroke: "M6 5.5h7l2 3V17H5V8.5ZM8 5.5V3h3v2.5M7 11h6v3H7Z" },
  "laundry-dryer": {
    fill: laundry,
    stroke: "M8.25 8.75c-.7.7-.7 1.4 0 2.1s.7 1.4 0 2.1M10 8.75c-.7.7-.7 1.4 0 2.1s.7 1.4 0 2.1M11.75 8.75c-.7.7-.7 1.4 0 2.1s.7 1.4 0 2.1",
    weight: 1.1,
  },
  "laundry-washer": { fill: `${laundry}M7 11.25h6a3 3 0 0 1-6 0Z` },
  layers: { stroke: "M10 3.5 17 7l-7 3.5L3 7ZM3 10.25l7 3.5 7-3.5M3 13.5 10 17l7-3.5" },
  leaf: { stroke: "M4 16c0-7 4-12 12-12 0 8-5 12-12 12ZM4 16l7-7" },
  "leaf-monstera": { stroke: "M10 17C5 16.5 2.75 13 2.75 9.5 2.75 5.5 6 3 10 3s7.25 2.5 7.25 6.5c0 3.5-2.25 7-7.25 7.5ZM10 17V9M10 9 6 6.5M10 11.5 5 11M10 9l4-2.5M10 11.5l5-.5" },
  // Notion's lemon (read 2026-11-18), filled, upright: a round body 12.5 across in Notion's round from 5 to the round edge's
  // foot, a nub at its foot left and its top right, and a leaf from that nub up to the round edge's top.
  lemon: {
    fill: `M16.25 7.5Q16.25 8.4 15.9 8.91A7.7 7.7 0 0 1 16.25 11.25${curve(roundQuarters(10, 11.25, 6.25)[1])}A7.7 7.7 0 0 1 7.66 17.15Q7.1 17.5 6.25 17.5${curve(roundQuarters(6.25, 15, 2.5)[2])}`
      + `Q3.75 14.2 4.1 13.59A7.7 7.7 0 0 1 3.75 11.25${curve(roundQuarters(10, 11.25, 6.25)[3])}A7.7 7.7 0 0 1 12.34 5.35Q12.9 5 13.75 5Q14.08 5 14.375 5.06V3.75H12.19`
      + `${curve(roundQuarters(12.19, 1.25, 2.5)[2])}H13.75${curve(roundQuarters(13.75, 3.75, 2.5)[0])}Z`,
  },
};
