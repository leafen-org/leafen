import {
  FIGURE_LINE,
  HALF_LINE,
  rainCloud,
  ringPiece,
  PENTAGON_MIDDLE,
  RING_RADIUS,
  SQUARE_FAR,
  SQUARE_NEAR,
  bar,
  circle,
  ellipse,
  inCircle,
  n,
  octagon,
  pentagon,
  pentagonCorners,
  polygon,
  polygonPart,
  rect,
  square,
  type NativeIconGlyph,
} from "./nativeIconGlyphShapes";
import { replyArrow } from "./nativeIconArrows";
import { curve, plate, roundQuarters } from "./nativeIconRound";

// Leafen glyphs for catalog names from "paste" through "shield".

const pentagonPart = (parts: number): NativeIconGlyph => ({ stroke: pentagon, fill: polygonPart(5, parts, pentagonCorners, 10, PENTAGON_MIDDLE) });
/** The square's line, near and far side, and a third of it: a grid's rows. */
const near = SQUARE_NEAR, far = SQUARE_FAR, third = (far - near) / 3;
const handset = "M6.5 3h-2A1.5 1.5 0 0 0 3 4.6c.4 6.7 5.7 12 12.4 12.4a1.5 1.5 0 0 0 1.6-1.5v-2a1 1 0 0 0-.7-1l-2.7-.9a1 1 0 0 0-1 .25l-1.2 1.2a10 10 0 0 1-4.45-4.45l1.2-1.2a1 1 0 0 0 .25-1l-.9-2.7a1 1 0 0 0-1-.7Z";
const nextTrack = `${polygon([[4, 5], [12.5, 10], [4, 15]])}${bar(13.5, 5, 2.25, 10)}`;
// Notion's fast forward (read 2026-11-18): two like triangles 8.75 tall from 5.625, the second's back on the first's, each with
// its back corners cut 1.09 along and its sides rising 0.64 a unit to its point, the second's cut flat on the box's side; the
// rewind's mirrored.
const forwardTriangle = (back: number, point: number, mirrored: boolean) => {
  const cut = 1.09, at = 5.625 + 0.64 * (point - back - cut), x = (value: number) => (mirrored ? 20 - value : value);
  return polygon([[x(back), 5.625], [x(back + cut), 5.625], [x(point), at], [x(point), 20 - at], [x(back + cut), 14.375], [x(back), 14.375]]);
};
const fastForward = (mirrored: boolean) => `${forwardTriangle(3.125, 9.6875, mirrored)}${forwardTriangle(9.6875, 16.875, mirrored)}`;
/** A dart's outline from its points on one side of the diagonal from the box's top right: those, then the same mirrored across it, the last on it given once. */
const dart = (side: Array<[number, number]>): Array<[number, number]> => {
  const mirrored = side.map(([x, y]): [number, number] => [20 - y, 20 - x]).reverse();
  const onDiagonal = ([x, y]: [number, number]) => Math.abs(x + y - 20) < 0.02;
  return [...side, ...mirrored.slice(onDiagonal(side[side.length - 1]!) ? 1 : 0, onDiagonal(side[0]!) ? -1 : undefined)];
};
const previousTrack = `${polygon([[16, 5], [7.5, 10], [16, 15]])}${bar(4.25, 5, 2.25, 10)}`;
// A stamp's scalloped edge: half discs cut into each side.
const stampEdge = [
  ...["5.55", "8.05", "10.55", "13.05"].flatMap((x) => [`M${x} 2.75a.7.7 0 0 0 1.4 0Z`, `M${x} 17.25a.7.7 0 0 1 1.4 0Z`]),
  ...["4.8", "7.8", "10.8", "13.8"].flatMap((y) => [`M3.75 ${y}a.7.7 0 0 1 0 1.4Z`, `M16.25 ${y}a.7.7 0 0 0 0 1.4Z`]),
].join("");
// A hand raised flat, its thumb out to the side.
const raisedHand = "M7.2 13.8 5.05 10.9a.9.9 0 0 1 1.4-1.15l.5.6V7.25a.625.625 0 0 1 1.25 0V9.4h.45V6.25a.625.625 0 0 1 1.25 0V9.4h.45"
  + "V6.45a.625.625 0 0 1 1.25 0V9.4h.45V7.45a.625.625 0 0 1 1.25 0V12a3 3 0 0 1-3 3h-.45a3 3 0 0 1-2.4-1.2Z";

export const NATIVE_ICON_GLYPHS_PS: Readonly<Record<string, NativeIconGlyph>> = {
  paste: { stroke: `${rect(4, 4, 12, 13.5, 1.75)}${rect(7.5, 2.5, 5, 3, 1)}M7 9.5h6M7 12.5h4` },
  // Its spokes end on the ring it is drawn in (nativeIconOutlined.ts).
  peace: inCircle(`M10 ${n(10 - RING_RADIUS)}V${n(10 + RING_RADIUS)}`
    + `M10 10L${n(10 - RING_RADIUS / Math.SQRT2)} ${n(10 + RING_RADIUS / Math.SQRT2)}M10 10L${n(10 + RING_RADIUS / Math.SQRT2)} ${n(10 + RING_RADIUS / Math.SQRT2)}`),
  peanut: { fill: "M9.87 12.4A3.8 3.8 0 0 1 3.65 16.42A3.8 3.8 0 0 1 7.5 10.09A2.3 2.3 0 0 1 10.31 7.86A3.6 3.6 0 0 1 16.18 3.89A3.6 3.6 0 0 1 12.06 9.65A2.3 2.3 0 0 1 9.87 12.4Z" + "M6.1 11.5l.9.9-.9.9-.9-.9ZM14 5.7l.9.9-.9.9-.9-.9Z" },
  pear: { stroke: "M10 4c1.5 0 2.25 1.5 2.5 3 .25 1.75 3 2.5 3 5.75a5.5 5 0 0 1-11 0c0-3.25 2.75-4 3-5.75C7.75 5.5 8.5 4 10 4ZM10 4V2.5M10 3.25c1-.75 2-.75 2.75 0" },
  // Notion's pen (read 2026-11-18), filled: a body slanting from the box's top right to (8.125, 9.66), and a nib apart from it
  // down to the foot left corner with a hole 1.875 across.
  pen: {
    fill: "M17.5 3.38L16.18 5.41Q15 7.3 13.55 8.66L10.33 11.875L8.125 9.66L11.34 6.45Q12.7 5 14.59 3.82L16.62 2.5Z"
      + `M7.38 10.69L9.32 12.63L7.5 16.25H6.12Q5 16.25 4.35 16.98L3.83 17.5L2.5 16.18L3.02 15.66Q3.75 15 3.75 13.89V12.5Z${circle(6.25, 13.75, 0.9375)}`,
  },
  "pentagon-one-fifth": pentagonPart(1),
  "pentagon-two-fifths": pentagonPart(2),
  "pentagon-three-fifths": pentagonPart(3),
  "pentagon-four-fifths": pentagonPart(4),
  // Notion's people (read 2026-11-18), filled: two people 10 apart, each a head 3.125 across over a body with arms down and
  // out, the left one on two legs, the right one in a skirt to the round edge's foot.
  people: {
    fill: `${circle(5, 3.75, 1.5625)}${circle(15, 3.75, 1.5625)}M16.48 9.84L17.03 8.74L18.29 11.25L19.68 10.55L18.57 8.32Q17.6 6.25 15 6.25Q12.4 6.25 11.43 8.32L10.31 10.55L11.71 11.25L12.96 8.74L13.51 9.84L11.56 13.75H13.74V17.5H16.24V13.75H18.43Z`
      + "M7.06 8.79L8.29 11.25L9.69 10.55L8.57 8.32Q7.6 6.25 5 6.25Q2.4 6.25 1.43 8.32L0.31 10.55L1.71 11.25L2.94 8.78Q3.1 8.44 3.44 8.44V10.94L1.25 17.5H3.23L5 12.18L6.78 17.5H8.75L6.56 10.94V8.44Q6.9 8.44 7.06 8.79Z",
  },
  perfume: { stroke: "M5 9a1.5 1.5 0 0 1 1.5-1.5h7A1.5 1.5 0 0 1 15 9v6.5a1.5 1.5 0 0 1-1.5 1.5h-7A1.5 1.5 0 0 1 5 15.5ZM8.5 7.5v-2h3v2M9 5.5v-2h2v2M11 4h2.5" },
  "person-feminine": {
    fill: `${circle(10, 4, 1.75)}${polygon([[10, 6.5], [6.5, 13.5], [13.5, 13.5]])}`,
    stroke: "M8.75 13.5V17M11.25 13.5V17M8.25 8.25 6 11M11.75 8.25 14 11",
    weight: FIGURE_LINE,
  },
  "person-masculine": {
    fill: `${circle(10, 4, 1.75)}${rect(7.25, 6.75, 5.5, 6, 1.25)}`,
    stroke: "M8.75 12.75V17M11.25 12.75V17M7.25 8 5.5 11.5M12.75 8l1.75 3.5",
    weight: FIGURE_LINE,
  },
  phone: { stroke: `${rect(5.5, 2.75, 9, 14.5, 2)}M9 15h2` },
  "phone-call": { stroke: handset },
  "phone-end-call": { stroke: "M2.75 11.5c4.25-4 10.25-4 14.5 0l-1.25 1.75a1 1 0 0 1-1.25.3l-2-.9a1 1 0 0 1-.55-1.1l.3-1.3a8.5 8.5 0 0 0-5 0l.3 1.3a1 1 0 0 1-.55 1.1l-2 .9a1 1 0 0 1-1.25-.3Z" },
  // Notion's speakerphone (read 2026-11-18), filled: a handset from the top left to the foot right, and two waves at the top
  // right, quarters of rings 1.875 thick round 8.125 and 5 about the middle.
  "phone-speaker": { fill: `M11.25 13.75Q7.51 12.51 6.25 8.75L8.75 6.25L5 2.5L3.38 4.12A3.02 3.02 0 0 0 2.5 6.25Q4.375 15.625 13.75 17.5A3.02 3.02 0 0 0 15.88 16.62L17.5 15L13.75 11.25Z${ringPiece(0, 90, 8.125, 6.25)}${ringPiece(0, 90, 5, 3.125)}` },
  "photo-landscape": {
    stroke: rect(2.75, 4, 14.5, 12, 1.75),
    fill: `${polygon([[3.8, 15.2], [3.8, 13.6], [8.25, 8.75], [11, 12], [12.5, 10.5], [16.2, 14.5], [16.2, 15.2]])}${circle(13, 7.25, 1.25)}`,
  },
  // Four white keys in a frame, two black keys standing up out of it, solid.
  piano: { stroke: `${rect(3.15, 4.35, 13.7, 12.35, 1.6)}M7.9 10.6v6.1M10 10.6v6.1M12.1 10.6v6.1`, fill: `${bar(6.4, 2.35, 3, 8.25)}${bar(10.6, 2.35, 3, 8.25)}`, weight: 1.3 },
  pig: { stroke: "M3.9 7.8H15V6v1.8l1.9 1.2 1.5 1.05-1.8.55-1 1.8-1.9 1.05H4.3c-1.3 0-1.45-1.45-1.45-2.85s.05-2.8 1.05-2.8ZM2.1 7.4c-.6 0-.55-.9-.55-1.45M3.9 14.4v1.55M12.6 14.4v1.55" },
  pill: { stroke: "M6.25 13.75a3.18 3.18 0 0 1 0-4.5l3-3a3.18 3.18 0 0 1 4.5 4.5l-3 3a3.18 3.18 0 0 1-4.5 0ZM7.75 7.75l4.5 4.5" },
  pin: { stroke: "M2.8 7.15v-.6c2.2-1.2 4.6-1.85 7.2-1.85s5 .65 7.2 1.85v.6Z", fill: `${bar(7.5, 8.1, 5, 1.9)}M9.2 10h1.6v5.4L10 17.5l-.8-2.1Z` },
  // Notion's pisces (read 2026-11-18): two bows a line wide back to back, each from the round edge's top to its foot swelling
  // 1.63 toward the middle, and a bar across the box's middle between them.
  pisces: { stroke: "M5.6 3.78Q8.1 10 5.6 16.22M14.4 3.78Q11.9 10 14.4 16.22M3.4375 10H16.5625" },
  pitcher: { stroke: "M6 3.5h8l-1 3.5c1.5 1.5 1.5 3 1.5 5v5h-9v-5c0-2 0-3.5 1.5-5ZM14.5 8h1A1.5 1.5 0 0 1 17 9.5V12a1.5 1.5 0 0 1-1.5 1.5h-1M6 3.5 4.5 5" },
  "plate-food": { stroke: "M2.2 11.5h15.6", fill: "M3.9 9.3a3.3 4.3 0 0 1 6.6 0ZM10.5 9.3a2.8 3 0 0 1 5.6 0ZM3.7 12.45h12.6l-.2 1.95H3.9Z" },
  "playback-fast-forward": { fill: fastForward(false) },
  "playback-next": { fill: nextTrack },
  "playback-pause": { fill: `${rect(5, 4, 3.5, 12, 1)}${rect(11.5, 4, 3.5, 12, 1)}` },
  "playback-play": { fill: polygon([[6, 3.5], [16, 10], [6, 16.5]]) },
  "playback-play-button": inCircle("", polygon([[8.5, 6.75], [13.5, 10], [8.5, 13.25]])),
  "playback-previous": { fill: previousTrack },
  "playback-rewind": { fill: fastForward(true) },
  "playback-stop": { fill: rect(4.5, 4.5, 11, 11, 2) },
  // Notion's playlist (read 2026-11-18): three lines to the box's side, the first from 11.875, the others from 8.75, and a note at
  // the left: its head an oval 5.625 by 5, its stem a line up from the head's middle into a flag 5 deep rising 1.4 over 5.625.
  playlist: {
    stroke: `M${11.875 + HALF_LINE} 5H${17.5 - HALF_LINE}M${8.75 + HALF_LINE} 10H${17.5 - HALF_LINE}M${8.75 + HALF_LINE} 15H${17.5 - HALF_LINE}M5.9375 4.5V${14.375 - HALF_LINE}`,
    fill: `${polygon([[5, 3.28125], [10.625, 1.875], [10.625, 6.875], [5, 8.28125]])}${ellipse(4.0625, 14.375, 2.8125, 2.5)}`,
  },
  poo: { stroke: "M5 16.5h10a2 2 0 0 0 .5-3.9A2.25 2.25 0 0 0 13.75 9H13a2.5 2.5 0 0 0-1.5-4.5c.5-1-.5-2-1.5-1.75A3 3 0 0 0 7 7.5 2.25 2.25 0 0 0 6.25 12 2 2 0 0 0 5 16.5Z" },
  "postage-stamp": { fill: `${rect(3.75, 2.75, 12.5, 14.5, 0.5)}${stampEdge}${rect(5.75, 4.75, 8.5, 10.5, 0.4)}${rect(7.25, 6.25, 5.5, 7.5, 0.3)}` },
  postcard: { stroke: `${rect(2.75, 4.5, 14.5, 11, 1.5)}M10 6.5v7M12 7.5h3V10h-3ZM4.75 8h3.5M4.75 10.5h3.5M4.75 13h2.5` },
  // Notion's pot (read 2026-11-18), filled: a knob 7.5 wide from the round edge's top, its top corners Notion's round 2.5 with a
  // hole 3.75 by 1.27 in it; a lid on 6.875..8.125 across the box, rising to the knob's foot; and under it a rim across the
  // box on 9.375..11.25 over a body 12.5 wide to the round edge's foot, its foot corners round 2.5.
  pot: {
    fill: `${plate(6.25, 2.5, 13.75, 5.88, [2.5, 2.5, 0, 0])}${plate(8.125, 4.375, 11.875, 5.65, [0.625, 0.625, 0, 0])}`
      + "M2.5 6.875Q4 6.05 6.25 5.88H13.75Q16 6.05 17.5 6.875V8.125H2.5Z"
      + `${bar(2.5, 9.375, 15, 1.875)}${plate(3.75, 11.25, 16.25, 17.5, [0, 0, 2.5, 2.5])}`,
  },
  "potted-plant": { stroke: "M6 11h8l-1 6H7ZM5.5 11h9M10 11V6M10 8c-2.5 0-4-1.5-4-4 2.5 0 4 1.5 4 4ZM10 7c0-2.5 1.5-4 4-4 0 2.5-1.5 4-4 4Z" },
  poultry: { stroke: "M3 12.5c0-3.25 3.25-5.5 7-5.5s7 2.25 7 5.5c0 2-1.5 3.5-3.5 3.5h-7C4.5 16 3 14.5 3 12.5ZM12.5 7.5 14.75 4M15.25 3.25a1 1 0 1 1 1.4 1.4M7.5 7.5 5.25 4M4.75 3.25a1 1 0 1 0-1.4 1.4M2.5 17.5h15" },
  power: { stroke: "M10 3v7M6 5.5a6 6 0 1 0 8 0" },
  // Notion's pram (read 2026-11-18), filled: a body from 2.5 to 16.25 on 7.5..12.5, its hood rising at the left in Notion's round
  // 5 to 2.5 and open down to 7.5 at 10.625, a handle out to the top right, and two wheels 3.75 across on stems 1.25 wide.
  pram: {
    weight: 1.25,
    stroke: "M13.75 12.5V14M5 12.5V14",
    fill: `M2.5 12.5V7.5${curve(roundQuarters(7.5, 7.5, 5)[3])}H12.5V3.75L10.625 7.5H14.93L17.43 5L18.75 6.33L16.25 8.82V12.5Z${circle(13.75, 15.625, 1.875)}${circle(5, 15.625, 1.875)}`,
  },
  "pregnancy-test": { stroke: "M9.86 8.9a.95.95 0 0 1 1.34 0 .95.95 0 0 1 0 1.34l-5.12 5.13a.95.95 0 0 1-1.35 0 .95.95 0 0 1 0-1.35ZM11.91 6.21l1.41-1.41a1.4 1.4 0 0 1 1.98 1.98l-1.41 1.41", fill: "M8.66 10.67a.55.55 0 0 1 .77.77l-.63.64a.55.55 0 0 1-.78-.78ZM5.93 13.39a.55.55 0 0 1 .78.78.55.55 0 0 1-.78-.78Z" },
  // A pretzel, solid: its two loops and the knot between its ends cut out of it, its ends crossed below.
  // Notion's pretzel (read 2026-11-18), filled: two lobes rising to 3.125 either side of a dip at 4.77, its foot round to 16.875
  // with a knob either side, and three holes cut in it — one in each lobe, one at the foot.
  pretzel: {
    fill: "M17.19 12.5Q18.125 10.6 18.125 8.67Q18.125 3.125 13.47 3.125Q11.3 3.125 10 4.77Q8.7 3.125 6.53 3.125Q1.875 3.125 1.875 8.67Q1.875 10.6 2.81 12.5A1.25 1.25 0 0 0 4.05 14.67L4.26 14.57Q6.4 16.875 10 16.875Q13.6 16.875 15.75 14.56L15.96 14.67A1.25 1.25 0 0 0 17.19 12.5Z"
      + "M4.375 8.67Q4.375 5.625 6.53 5.625Q8.2 5.625 8.75 7.53V9.53L5.04 11.38Q4.375 10.1 4.375 8.67ZM10 14.375Q8 14.375 6.68 13.36L10 11.69L13.32 13.36Q12 14.375 10 14.375Z"
      + "M15.625 8.67Q15.625 5.625 13.47 5.625Q11.8 5.625 11.25 7.53V9.53L14.96 11.38Q15.625 10.1 15.625 8.67Z",
  },

  preview: { stroke: `${rect(2.75, 4, 14.5, 10.5, 1.5)}M7 17h6M10 14.5V17`, fill: polygon([[8.5, 7], [12.5, 9.25], [8.5, 11.5]]) },
  print: { stroke: "M5.5 7V3h9v4M5.5 14h-2a1 1 0 0 1-1-1V8a1 1 0 0 1 1-1h13a1 1 0 0 1 1 1v5a1 1 0 0 1-1 1h-2M5.5 11.5h9V17h-9Z" },
  private: { stroke: octagon, fill: raisedHand },
  // Notion's profile (read 2026-11-18), filled: a head facing right — round 9.38 across from the round edge's top, its nose
  // and chin out to 16.25 — on a neck and shoulders to the round edge's foot.
  profile: { fill: "M16.25 9.23Q16.25 9.53 15.92 9.61L15.14 9.8L14.83 11.68Q14.7 12.5 13.94 12.5Q13.79 12.5 13.64 12.46L11.875 12.02V13.125L13.47 16.31Q13.75 16.85 13.75 17.5H5Q5 14.9 8.125 13.43V11.33A4.68 4.68 0 0 1 10.32 2.5A4.69 4.69 0 0 1 15 7.18Q15 7.29 14.99 7.4L16.15 8.95Q16.25 9.08 16.25 9.23Z" },
  promoted: { stroke: `${square}M7.5 12.5l5-5M8.5 7.5h4v4` },
  public: inCircle("M10 8l-1 3.5 2 1.5.5 2.5M10 8 8 9.25 7.25 11M10 8l1.5 1.5 1.75.25M9 11.5l-1.5 3", circle(10.5, 5.75, 1)),
  "pull-request": { stroke: `${circle(5.5, 4.75, 1.75)}${circle(5.5, 15.25, 1.75)}${circle(14.5, 15.25, 1.75)}M5.5 6.5v7M14.5 13.5V8A2.5 2.5 0 0 0 12 5.5H9M10.75 3.75 9 5.5l1.75 1.75` },
  pump: { stroke: "M2.75 14.5c0-2.5 2-3.5 4-3 2.5.5 4 .25 5.75-1.25l2.75-2.75c.5-.5 1.25-.5 1.5.25L17.5 10v7h-1.25l-1-4-3.5 1.25c-1.5.5-3 .75-4.5.75H2.75Z" },
  "pump-bottle": { stroke: "M4.7 16.55V10.9a2.25 2.25 0 0 1 2.25-2.25h6.1a2.25 2.25 0 0 1 2.25 2.25v5.65ZM8.5 2.8h3.4c.9 0 1.4.5 1.4 1.3", fill: `${bar(7.4, 5.9, 3.4, 1.85)}${bar(8.4, 3.75, 1.2, 2.2)}` },
  // Upright, as Notion's (rastered at 400px, 2026-11-18): a cap 5.625..14.375 narrowing to a neck a quarter of the box
  // wide, a foot flaring at 45° to 5..15, and a needle a line wide narrowing to its point on 18.75; one filled outline.
  "push-pin": {
    fill: polygon([
      [5.625, 2.5], [14.375, 2.5], [14.375, 3.125], [12.5, 5.625], [12.5, 8], [15, 10.5], [15, 13.125], [10.9375, 13.125],
      [10.9375, 16.5], [10.3125, 18.75], [9.6875, 18.75], [9.0625, 16.5], [9.0625, 13.125], [5, 13.125], [5, 10.5], [7.5, 8],
      [7.5, 5.625], [5.625, 3.125],
    ]),
  },
  // Notion's puzzle piece (read 2026-11-18), filled: a piece from 5 to 16.875 between the box's right 15 and a left side
  // slanting in at 4:1, a knob out at its top and its right — ovals 3.75 by 3.125 on necks 1.875 wide — and two knobs' sockets
  // cut in at its foot and its left.
  puzzle: {
    fill: "M4.22 5H8.44V4.5Q8.44 4.2 8.2 4.08A1.875 1.56 0 1 1 10.55 4.08Q10.31 4.2 10.31 4.5V5H15V8.88Q15 9.375 15.48 9.375Q15.75 9.375 15.9 9.13A1.56 1.875 0 1 1 15.9 11.49Q15.75 11.25 15.48 11.25Q15 11.25 15 11.75V16.875H10.81Q10.31 16.875 10.31 16.38Q10.31 16.1 10.55 15.96"
      + "A1.875 1.56 0 1 0 8.2 15.96Q8.44 16.1 8.44 16.38Q8.44 16.875 7.94 16.875H1.25L2.59 11.53Q2.8 10.625 3.82 10.625Q4.4 10.625 4.83 11.09A1.56 1.875 0 1 0 4.72 8.42Q4.5 8.75 4.14 8.75Q3.35 8.75 3.49 7.91Z",
  },
  radio: { stroke: `${rect(2.75, 7, 14.5, 10, 1.5)}M5 7l9-4.5${circle(7, 12, 2.5)}M12 10.5h3M12 13.5h3` },
  // Its drops three slants 1.25 across, from under the cloud to the round edge's foot, 3.75 apart (read 2026-11-18).
  rain: { fill: `${rainCloud}${[0, 3.75, 7.5].map((x) => polygon([[6.09 + x, 12.5], [7.34 + x, 12.5], [4.84 + x, 17.5], [3.59 + x, 17.5]])).join("")}` },
  rainbow: { stroke: "M2.75 15a7.25 7.25 0 0 1 14.5 0M5.5 15a4.5 4.5 0 0 1 9 0M8.25 15a1.75 1.75 0 0 1 3.5 0" },
  receipt: { stroke: "M4.5 2.75h11v14.5l-1.8-1.25-1.85 1.25L10 16l-1.85 1.25L6.3 16l-1.8 1.25ZM7 6.5h6M7 9.5h6M7 12.5h3.5" },
  redirect: { fill: "M3 15V9.5A5.5 5.5 0 0 1 8.5 4h3V1.75L17 6l-5.5 4.25V8h-3A1.5 1.5 0 0 0 7 9.5V15Z" },
  redo: { stroke: "M16 8H7.5a4 4 0 0 0 0 8H10M12.5 4.5 16 8l-3.5 3.5" },
  // The reference mark ※.
  reference: { stroke: "M5 5l10 10M15 5 5 15", fill: `${circle(10, 4, 1.35)}${circle(10, 16, 1.35)}${circle(4, 10, 1.35)}${circle(16, 10, 1.35)}` },
  refresh: { stroke: "M15.95 10.4A5.95 5.95 0 1 1 10 4.05h2.4M9.4.95l3.1 3.1-3.1 3.1" },
  "refresh-reverse": { stroke: "M4.05 10.4A5.95 5.95 0 1 0 10 4.05H7.6M10.6.95 7.5 4.05l3.1 3.1" },
  refrigerator: { stroke: "M5.95 2.2h8.1v3.75h-8.1ZM5.95 9.55h8.1v8.25h-8.1Z", fill: `${bar(11.2, 3.1, 1.2, 2.5)}${bar(11.2, 10.6, 1.2, 2.5)}` },
  // Notion's rename (read 2026-11-18): a bracket either side, each a line round three sides of 2.5..7.8 by 5.625..14.375, and
  // between them a text cursor, its stem down the middle and at each end two curls a quarter round 2.19 out to 7.2 and 12.8.
  rename: {
    stroke: "M6.875 6.5625H3.4375V13.4375H6.875M13.125 6.5625H16.5625V13.4375H13.125"
      + "M8.125 3.46A2.1875 2.1875 0 0 1 10 5.625V14.375A2.1875 2.1875 0 0 1 8.125 16.54M11.875 3.46A2.1875 2.1875 0 0 0 10 5.625M10 14.375A2.1875 2.1875 0 0 0 11.875 16.54",
  },
  reorder: { stroke: "M3.5 5.5h13M3.5 10h13M3.5 14.5h13" },
  repeat: { stroke: "M3.5 9V8A2.5 2.5 0 0 1 6 5.5h10.5M14 3l2.5 2.5L14 8M16.5 11v1a2.5 2.5 0 0 1-2.5 2.5H3.5M6 17l-2.5-2.5L6 12" },
  reply: { fill: replyArrow([1.875], 3.125) },
  "reply-all": { fill: replyArrow([0, 5.625], 1.25) },
  // Notion's ringed planet (read 2026-11-18), filled: a planet 12.5 across in the middle and a ring tilted 30° up to the right,
  // its outside an oval 22.88 by 5.72 to the box's sides and the planet's top, its inside 15.72 by 3.94 open where it lies off
  // the planet, passing in front of the planet's foot with a gap of 1.25 under it (the points where the ovals cross the planet
  // worked out to three places).
  "ringed-planet": {
    fill: "M16.2 10.786A6.25 6.25 0 0 1 7.58 15.763A12.69 4.11 -30 0 0 16.2 10.786Z"
      + "M19.907 4.28A11.44 2.86 -30 0 0 13.734 4.988A6.25 6.25 0 0 0 3.792 10.727A11.44 2.86 -30 1 0 19.907 4.28Z"
      + "M5.311 14.132A7.86 1.97 -30 0 1 4.077 11.995A6.25 6.25 0 0 0 5.311 14.132Z"
      + "M14.689 5.868A7.86 1.97 -30 0 1 15.923 8.005A6.25 6.25 0 0 0 14.689 5.868Z",
  },
  "roller-skate": { stroke: "M4 3.45h5.35V5.6l4.25 3.65 2.95.65v1.7l-3.55.85H4Z", fill: `${circle(4.7, 14.75, 2.15)}${circle(13.3, 14.75, 2.15)}` },
  row: { stroke: `${square}M${near} ${n(near + third)}H${far}M${near} ${n(far - third)}H${far}` },
  "rubber-stamp": { stroke: "M7.5 9.5h5L12 6.5a2 2 0 1 0-4 0ZM3.5 9.5h13V13h-13ZM4.5 16h11" },
  ruler: { fill: bar(11.6, 1.25, 1.55, 17.5), stroke: [1.875, 3.94, 6, 8.06, 10.12, 12.18, 14.24, 16.3, 18.125].map((y, index) => `M${index % 2 ? 9.6 : 7.475} ${y}H11.6`).join(""), weight: 1.25 },
  // Notion's runner (read 2026-11-18), filled: a head 3.125 across at the top right and a body leaning forward, its arms bent
  // back and out, one leg kicked back to the left and the other striding down to the right.
  run: { fill: `${circle(12.5, 3.75, 1.5625)}M12.97 8.98Q13.8 10.625 16.25 10.625V9.06Q15 9.06 14.17 7.98Q13 6.25 10.625 6.25Q6.6 6.25 4.69 10.24L6.08 10.94Q7 9 9.13 7.99L7.58 11.08Q6.7 12.81 4.79 12.81H3.44V14.69H4.79Q7.9 14.69 9.23 11.98Q12.3 14.3 13.22 17.5L15 16.91Q14.1 13.3 11.05 11.02L12.38 8.36Q12.7 8.6 12.97 8.98Z` },
  // Notion's safety pin (read 2026-11-18), filled: a band at 45° from the box's foot left to its top right, its ends rounds 7.5
  // across, the coil a hole 3.75 across at the foot and the pin's opening cut from the clasp at the top down the middle.
  "safety-pin": {
    fill: `M11.1 3.6L3.6 11.1A3.75 3.75 0 0 0 8.9 16.4L16.4 8.9A3.75 3.75 0 0 0 11.1 3.6Z${circle(6.25, 13.75, 1.875)}`
      + "M13.7 7.5H13.125Q11.71 7.5 11.71 8.7L12.23 10.43L9.87 12.78A3.77 3.77 0 0 0 7.22 10.13L9.57 7.77Q10.3 7.95 10.73 7.5L12.87 5.37A1.25 1.25 0 0 1 15 6.25Q15 7.5 13.7 7.5Z",
  },
  sagittarius: { stroke: "M4 16 16 4M10 4h6v6M7 9l4 4" },
  // Notion's sailboat (read 2026-11-18), filled: a small sail rising to the left of its mast to 1.875, a big sail bellying out to
  // the right to 16.25, a hull on 11.875 whose foot dips at its middle, and a wave a line deep under it.
  sailboat: {
    fill: `${polygon([[10, 1.875], [8.75, 1.875], [3.75, 9.375], [3.75, 10.625], [10, 10.625]])}M10.625 10.625Q11.25 9.33 11.25 6.875Q11.25 4.4 10.625 3.125Q16.25 3.5 16.25 10.625Z`
      + "M3.75 11.875H16.25Q16.25 14.6 13.59 14.97Q12 13.75 10 13.75Q8 13.75 6.25 15Q5.3 14.2 3.75 13.91Z"
      + "M2.5 15C4 15 4.75 16.25 6.25 16.25C7.75 16.25 8.5 15 10 15C11.5 15 12.25 16.25 13.75 16.25C15.25 16.25 16 15 17.5 15V16.875C16 16.875 15.25 18.125 13.75 18.125C12.25 18.125 11.5 16.875 10 16.875C8.5 16.875 7.75 18.125 6.25 18.125C4.75 18.125 4 16.875 2.5 16.875Z",
  },
  sandwich: { stroke: "M3.5 14.5 10 4l6.5 10.5ZM3.5 14.5V16h13v-1.5M5.5 11.5c1.5.75 3 .75 4.5 0s3-.75 4.5 0" },
  save: { stroke: "M3 11v4.5A1.5 1.5 0 0 0 4.5 17h11a1.5 1.5 0 0 0 1.5-1.5V11M10 3v9M6.5 8.5 10 12l3.5-3.5" },
  // Notion's scarf (read 2026-11-18), filled: its loop a band across 3.125..16.875 on 3.125..7.5, and one end hanging 5 wide
  // from 8.75 down to a fringed foot at 45°.
  scarf: { fill: `${bar(3.125, 3.125, 13.75, 4.375)}M15.37 14.96L16.25 15.84L14.93 17.17L14.04 16.28L13.16 17.17L14.04 18.05L12.72 19.375L11.83 18.49Q10 16.7 10 14.07V8.75H15V14.07Q15 14.6 15.37 14.96Z` },
  school: { stroke: `M2.75 17h14.5M4 17V8.5L10 4l6 4.5V17M8.25 17v-3.5h3.5V17M10 4V1.75M10 1.75h2.5l-.5.75.5.75H10${circle(10, 9, 1.25)}` },
  // Notion's science (read 2026-11-18), outlined: three orbits, each an oval 15 by 4.3 about the middle a line wide, one upright
  // and the others turned 60° either way.
  science: { stroke: [90, -30, 30].map((degrees) => {
    const [c, s] = [Math.cos((degrees * Math.PI) / 180), Math.sin((degrees * Math.PI) / 180)];
    return `M${n(10 + 7.5 * c)} ${n(10 + 7.5 * s)}A7.5 2.15 ${degrees} 1 0 ${n(10 - 7.5 * c)} ${n(10 - 7.5 * s)}A7.5 2.15 ${degrees} 1 0 ${n(10 + 7.5 * c)} ${n(10 + 7.5 * s)}Z`;
  }).join("") },

  scooter: { stroke: `${circle(4.5, 15, 2)}${circle(15.5, 15, 2)}M4.5 15h8l2-8.5h2M14.5 6.5l1 8.5M6 12.5h5` },
  scorpio: { stroke: "M3 6.5c1 0 2 1 2 2v7M5 8.5c0-1 1-2 2-2s2 1 2 2v7M9 8.5c0-1 1-2 2-2s2 1 2 2V14c0 1 .75 2 2 2h1.5M15 14l2 2-2 2" },
  screwdriver: { stroke: `${polygon([[16.33, 5.79], [14.21, 3.67], [10.67, 7.21], [12.79, 9.33]])}M11.7 8.3 4 16M4 16l-.75.75` },
  script: { stroke: "M6.5 3.5h8a2 2 0 0 0-2 2V15a2 2 0 0 1-2 2h-5a2 2 0 0 1-2-2v-1h3ZM14.5 3.5a2 2 0 0 1 2 2v1h-2.05M8.5 7.5h2M8.5 10.5h2" },
  "scrub-brush": { stroke: "M2.8 5.95h11.15a3.25 3.25 0 0 1 3.25 3.25v.3M3.75 6.2v2.9M2.8 10.25h14.4", fill: "M5.4 11.2h12.15l.6 3.8H5.4ZM3.3 11.2h1.1l-.4 3.8H2.7Z" },
  seed: { stroke: "M15.87 4.13C17.5 5.76 17.11 7.77 15.55 9.33C13.43 11.45 10.07 13.32 7.31 14.67C6.04 15.37 4.56 16.29 4.13 15.87C3.71 15.44 4.63 13.96 5.33 12.69C6.68 9.93 8.55 6.57 10.67 4.45C12.23 2.89 14.24 2.5 15.87 4.13Z", fill: "M6.25 11.63C7.67 9.08 9.72 6.89 12.4 5.33C10.49 7.67 8.44 9.86 6.25 11.63Z" },
  // Notion's paper plane (read 2026-11-18), filled: a dart from its nose at the box's top right (cut 0.88 each way) to two wings
  // and a tail folded in, alike either side of its diagonal, and a slit cut along that diagonal from the tail.
  send: { fill: `${polygon(dart([[16.62, 2.5], [3.49, 5.78], [2.5, 6.77], [6.38, 10.66], [5, 14.12]]))}${polygon(dart([[15.22, 4.78], [7.83, 9.45], [8.58, 10.2], [7.78, 12.22]]))}` },
  "send-to": { fill: replyArrow([1.875], 3.125, true) },
  // Its two units' outside on the square edge.
  server: { stroke: `${rect(near, near, far - near, 4.875, 1.25)}${rect(near, 11.05, far - near, 4.875, 1.25)}M9.25 6.5h4.25M9.25 13.5h4.25`, fill: `${circle(6.75, 6.5, 0.8)}${circle(6.75, 13.5, 0.8)}` },
  "service-counter": { stroke: `M2.75 12h14.5M4 12v5M16 12v5${circle(10, 5, 1.75)}M6.5 12c.4-2 1.75-3.25 3.5-3.25s3.1 1.25 3.5 3.25` },
  // Notion's set square (read 2026-11-18), filled: a right triangle on the box's foot and right side, its two sharp points cut
  // 2.5 from the corners, three slots 1.25 wide and 1.875 deep cut into its long side 5.3 apart, and a like triangle cut out
  // of it, its legs 4.7 to the corner 13.75.
  "set-square": {
    fill: polygon([[15, 2.5], ...[2.913, 8.217, 13.52].flatMap((from): Array<[number, number]> => {
      const at = (along: number, into: number): [number, number] => [15 - along / Math.SQRT2 + into / Math.SQRT2, 2.5 + along / Math.SQRT2 + into / Math.SQRT2];
      return [at(from, 0), at(from, 1.875), at(from + 1.25, 1.875), at(from + 1.25, 0)];
    }), [2.5, 15], [2.5, 17.5], [17.5, 17.5], [17.5, 2.5]]) + polygon([[9.05, 13.75], [13.75, 13.75], [13.75, 9.05]]),
  },
  // An arrow rising out of an open tray.
  share: { stroke: "M6.75 7.75H5.25a1.5 1.5 0 0 0-1.5 1.5v6.25a1.5 1.5 0 0 0 1.5 1.5h9.5a1.5 1.5 0 0 0 1.5-1.5V9.25a1.5 1.5 0 0 0-1.5-1.5h-1.5M10 12V2.75M7 5.75l3-3 3 3" },
  sharing: { stroke: "M3 17v-6.5l2-2 5 1.5 5-1.5 2 2V17ZM10 12v3", fill: `${circle(5, 5.5, 1.5)}${circle(10, 4.5, 1.5)}${circle(15, 5.5, 1.5)}` },
  sheep: { stroke: "M5 11a2 2 0 0 1 1-3.75 2.25 2.25 0 0 1 4-1 2.25 2.25 0 0 1 4 1A2 2 0 0 1 15 11a2 2 0 0 1-1 3.75H6A2 2 0 0 1 5 11ZM14.5 8.5h2a1 1 0 0 1 1 1v1.25a1.75 1.75 0 0 1-1.75 1.75H15M7 14.75V17M12 14.75V17" },
  shell: { stroke: "M10 16.5 3 8.5a7.5 5 0 0 1 14 0ZM10 16.5 5.5 5.75M10 16.5V4.25M10 16.5l4.5-10.75M8.5 16.5h3" },
  shield: { stroke: "M10 2.75 16 5v4.5c0 3.75-2.5 6.5-6 7.75-3.5-1.25-6-4-6-7.75V5Z" },
};
