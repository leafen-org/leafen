import {
  bar,
  DASH_BREAK,
  EDGE_STEP,
  FIGURE_LINE,
  NATIVE_ICON_LINE,
  RING_RADIUS,
  ROUND_EDGE,
  burst,
  circle,
  dashedRing,
  disc,
  ellipse,
  inRing,
  polygon,
  rect,
  ring,
  smallCross,
  triangle,
  triangleCornerCuts,
  triangleCorners,
  triangleMiddle,
  type NativeIconGlyph,
} from "./nativeIconGlyphShapes";
import { blockArrow } from "./nativeIconArrows";
import { offCut, offSlash } from "./nativeIconOff";
import { NECK, PERSON_HEAD, person, personBeside, shouldersWithin } from "./nativeIconPerson";

// Leafen glyphs for catalog names from "thumbs-up" to the end of the catalog.

const documentOutline = "M5.5 2.75h5.75L14.5 6v10.5a.75.75 0 0 1-.75.75H5.5a.75.75 0 0 1-.75-.75v-13a.75.75 0 0 1 .75-.75ZM11 2.75V6.5h3.5";
const folderOutline = "M2.75 5.5a1 1 0 0 1 1-1h3.75l1.75 2h7a1 1 0 0 1 1 1v7.75a1 1 0 0 1-1 1H3.75a1 1 0 0 1-1-1Z";
const ticket = "M3 5h14v3a2 2 0 0 0 0 4v3H3v-3a2 2 0 0 0 0-4Z";
const trainFront = `${rect(4.5, 3, 11, 11, 2.5)}M4.5 9h11M7 17l1.5-3M13 17l-1.5-3`;
const trainLights = `${circle(7.5, 11.5, 0.9)}${circle(12.5, 11.5, 0.9)}`;
const tooth = "M6.5 3c1.25 0 2.25.75 3.5.75C11.25 3.75 12.25 3 13.5 3 15.5 3 16.5 4.75 16 7.25c-.4 2-1 3.25-1.25 5.75-.25 2.5-.75 4-1.75 4s-1.5-1.5-1.75-3.25c-.2-1.4-.6-2-1.25-2s-1.05.6-1.25 2C8.5 15.5 8 17 7 17s-1.5-1.5-1.75-4C5 10.5 4.4 9.25 4 7.25 3.5 4.75 4.5 3 6.5 3Z";
const eye = "M2.5 10c2-3.75 4.75-5.5 7.5-5.5s5.5 1.75 7.5 5.5c-2 3.75-4.75 5.5-7.5 5.5s-5.5-1.75-7.5-5.5Z";
const speaker = "M3 7.5h3L10 4v12l-4-3.5H3Z";
const videoCamera = `${rect(2.75, 5.5, 10, 9, 1.75)}M12.75 8.75l4.5-2.5v7.5l-4.5-2.5`;
// A person in a ring (Notion's user circles rastered at 400px, 2026-11-18):
// the head's top a neck under the ring's inside, the usual head, and
// shoulders cut along the ring's inside — along its line for the ring,
// along its inside for the disc they are cut from. In the dashed ring the
// head is seven half steps round and the shoulders end a dash's break
// inside the ring, clear of its broken line.
const ringInside = ROUND_EDGE - NATIVE_ICON_LINE;
const personInRing = person(10, 10 - ringInside + NECK, PERSON_HEAD);
const personInDashedRing = person(10, 10 - ringInside + NECK, 7 * EDGE_STEP);
// Notion divides its triangle in three from the middle to the middles of
// its sides (triangle-one-third and -two-thirds rastered at 400px and seen at
// 150px, 2026-11-18): each third a kite round a corner, filled from the one
// at the top and then clockwise.
const [triangleTop, triangleRight, triangleLeft] = triangleCorners;
const between = ([ax, ay]: [number, number], [bx, by]: [number, number]): [number, number] => [(ax + bx) / 2, (ay + by) / 2];
const triangleThirds = [
  polygon([triangleTop!, between(triangleTop!, triangleRight!), triangleMiddle, between(triangleLeft!, triangleTop!)]),
  polygon([triangleTop!, triangleRight!, between(triangleRight!, triangleLeft!), triangleMiddle, between(triangleLeft!, triangleTop!)]),
];
const wheelchairPerson = "M9.5 5.5 9 10.5h4.5l2 4.5H17M9.25 8H13M6.5 9.25a4.5 4.5 0 1 0 6 5.5";

export const NATIVE_ICON_GLYPHS_TZ: Readonly<Record<string, NativeIconGlyph>> = {
  // A solid hand, thumb up, beside its cuff.
  "thumbs-up": {
    fill: `${rect(2.75, 9.25, 3, 7.75, 0.75)}M6.6 9.25 9.6 3.6a1.55 1.55 0 0 1 2.85 1L11.9 8h3.65a1.6 1.6 0 0 1 1.55 2l-1.7 5.85A1.6 1.6 0 0 1 13.85 17H6.6Z`,
  },
  // Tickets with a frame cut into them round a solid middle.
  ticket: { fill: `${ticket}${rect(6.25, 7.25, 7.5, 5.5, 0.5)}${rect(7.75, 8.75, 4.5, 2.5, 0.3)}` },
  "ticket-admission": {
    fill: "M4.5 5h11a1.5 1.5 0 0 0 1.5 1.5v7a1.5 1.5 0 0 0-1.5 1.5h-11A1.5 1.5 0 0 0 3 13.5v-7A1.5 1.5 0 0 0 4.5 5Z"
      + `${rect(6, 7.25, 8, 5.5, 0.5)}${rect(7.5, 8.75, 5, 2.5, 0.3)}`,
  },
  timeline: { fill: `${rect(3, 4, 8, 2.5, 1)}${rect(6, 8.75, 9, 2.5, 1)}${rect(9, 13.5, 8, 2.5, 1)}` },
  toaster: { stroke: "M3 9a3 3 0 0 1 3-3h8a3 3 0 0 1 3 3v6.5H3ZM3 12.5h14M15 9.5h-1M6 6V3.25h3.5V6M10.5 6V3.25H14V6" },
  toilet: { stroke: "M5 3h4v6H5ZM3.5 9h13c0 3-2 5-5 5.5l.5 2.5H7l.5-2.5c-3-.5-4-2.5-4-5.5Z" },
  // A solid roll with its core cut out of its end, its sheet hanging down.
  "toilet-paper": { fill: `M7 3.25h6.5a3.75 4.75 0 0 1 3.75 4.75V17h-6.25v-4.25H7a3.75 4.75 0 0 1 0-9.5Z${ellipse(7, 8, 1.3, 1.75)}` },
  tooth: { stroke: tooth },
  torii: { stroke: "M2.75 3.75c2.5.75 12 .75 14.5 0v.75c-2.5.75-12 .75-14.5 0ZM4 7.75h12M5.5 6.4V17M14.5 6.4V17M10 6.4v1.35" },
  tornado: { stroke: "M3 4h14l-6 12H9ZM6 8h8M8 12h4" },
  tortoise: { stroke: "M3.5 13a6.5 6 0 0 1 13 0ZM16.5 11h1a1.5 1.5 0 0 0 0-3h-1.5M5 13l-.5 2.5M8 13v2.5M12 13v2.5M15 13l.5 2.5M6.5 9 10 11l3.5-2M10 7v4" },
  towel: { stroke: "M3 4h14M5 4.95V16h8V4.95ZM13.95 4.95H15V8.5h-1.05ZM6.5 13h5" },
  "traffic-cone": { stroke: "M8.5 3h3L15 15.5H5ZM3 17h14M7.25 8h5.5M6.4 11.5h7.2" },
  "traffic-light": { stroke: "M7.2 3.45h5.6v13.1H7.2Z", fill: `${circle(10, 5.35, 1.5)}${circle(10, 10, 1.5)}${circle(10, 14.65, 1.5)}${[0, 4.65, 9.3].map((y) => `M3.1 ${3.7 + y}h3.15v2.8L3.1 ${4.4 + y}ZM16.9 ${3.7 + y}h-3.15v2.8l3.15-2.1Z`).join("")}` },
  train: { stroke: `${rect(4.5, 2.75, 11, 11.5, 3)}M4.5 8.5h11M7 17l1.5-2.75M13 17l-1.5-2.75`, fill: trainLights },
  // A bullet train head on: its nose narrowing to a point, the windscreen and two lamps cut out of it, on its rails.
  "train-high-speed": {
    fill: "M6.25 2.75h7.5c1.65 0 2.5 1.15 2.5 2.75v4.75c0 1.15-.7 2.1-1.75 2.65l-3.55 1.85c-.6.3-1.3.3-1.9 0L5.5 12.9c-1.05-.55-1.75-1.5-1.75-2.65V5.5c0-1.6.85-2.75 2.5-2.75Z"
      + `M6.1 4.75h7.8l-1.3 3.85H7.4Z${circle(6.5, 10.75, 1)}${circle(13.5, 10.75, 1)}M7.6 15.6h4.8l2 2.15h-1.95l-.8-.9H8.35l-.8.9H5.6Z`,
  },
  "train-light-rail": { stroke: `${rect(5, 5.5, 10, 9, 2)}M8 5.5 10 3l2 2.5M7 3h6M5 10h10M7 17l1-2.5M13 17l-1-2.5` },
  // Notion's maglev train (read 2026-11-18), filled, front on: a car 11.87 wide from its roof light at 1.56 to its skirts on the
  // round edge's foot, parted under its bumper, its windscreen and two lamps cut out.
  "train-magnetic-levitation": {
    fill: "M11.09 3.18H11.12L10.6 1.56H9.41L8.88 3.18H8.92Q5.8 3.5 4.07 5V15A2.5 2.5 0 0 0 6.57 17.5H8.125V15.625H6.875A0.625 0.625 0 0 1 6.25 15V13.75H13.75V15A0.625 0.625 0 0 1 13.125 15.625H11.875V17.5H13.44A2.5 2.5 0 0 0 15.94 15V5Q14.2 3.5 11.09 3.18Z"
      + `${circle(6.875, 11.56, 0.94)}${circle(13.125, 11.56, 0.94)}${bar(5.94, 6.25, 8.12, 3.125)}`,
  },
  "train-metro": { stroke: `${trainFront}M8 6h4`, fill: trainLights },
  transfers: inRing("M6 8.5h7.5M11.5 6.5l2 2-2 2M14 11.5H6.5M8.5 9.5l-2 2 2 2"),
  translate: { stroke: "M3 13 5.5 6.5 8 13M3.9 11h3.2M11.5 8.5h5M14 7.5v1M12 13c1.5-.75 2.75-2 3.5-4.5M13 10.5c.75 1.25 1.75 2 3 2.5M10 3.5c2.5-.75 4.75 0 6 1.75M16 3.5v1.75h-1.75M10 16.5c-2.5.75-4.75 0-6-1.75M4 16.5v-1.75h1.75" },
  tree: { stroke: "M10 17v-5.5M10 12.5l-2-2M10 13.5l2-2M6.25 12a3.25 3.25 0 0 1-.5-6.45A4.25 4.25 0 0 1 14.25 5.5a3.25 3.25 0 0 1-.5 6.5Z" },
  "triangle-one-third": { stroke: triangle, fill: `${triangleThirds[0]}${triangleCornerCuts}` },
  "triangle-two-thirds": { stroke: triangle, fill: `${triangleThirds[1]}${triangleCornerCuts}` },
  trophy: { stroke: "M6 3h8v5a4 4 0 0 1-8 0ZM6 4.5H3.5A2.5 2.5 0 0 0 6 8M14 4.5h2.5A2.5 2.5 0 0 1 14 8M10 12v2.5M7 17h6M8 14.5h4V17H8Z" },
  // Notion's cocktail (read 2026-11-18), filled: a glass 8.75 across at its rim on the round edge's top, waisted at 6.25 and
  // swelling to 8.75 across at 10.625, on a stem a line wide over a foot 6.875 across, a band cut under its rim.
  "tropical-cocktail": {
    fill: "M14.375 4.375V2.5H5.625V4.375Q6.875 5.06 6.875 6.25Q6.875 7.43 6.25 8.6Q5.625 9.6 5.625 10.625Q5.625 13.98 9.06 14.31V15.625H6.5625V17.5H13.4375V15.625H10.94V14.31Q14.375 13.98 14.375 10.625Q14.375 9.6 13.75 8.6Q13.125 7.44 13.125 6.25Q13.125 5.07 14.375 4.375Z"
      + "M11.78 4.375A3.6 3.6 0 0 0 11.31 5.625H8.69A3.7 3.7 0 0 0 8.23 4.375Z",
  },
  truck: { stroke: `M2.75 5.5h9v8.75h-9ZM11.75 8h3l2.5 3v3.25h-5.5${circle(5.75, 14.75, 1.5)}${circle(14.25, 14.75, 1.5)}` },
  trumpet: { stroke: "M3 8.5h9.5L17 5v10l-4.5-3.5H3ZM6 11.5V14h4v-2.5M6.5 8.5v-2M8.5 8.5v-2M10.5 8.5v-2" },
  tshirt: { stroke: "M7 3l3 1.5L13 3l4 2.5-1.5 3.5-2-.75V17h-7V8.25l-2 .75L3 5.5Z" },
  tulip: { stroke: "M6 4.5 7.75 6 10 3.5 12.25 6 14 4.5V8a4 4 0 0 1-8 0ZM10 12v5.5M10 15c-1.5-2-3.5-2.5-5-2 .5 2 2.5 3 5 3M10 15.5c1.5-2 3.5-2.5 5-2-.5 2-2.5 3-5 3" },
  "tulip-name-tag": { stroke: "M4 4l2.5 2.5L10 3l3.5 3.5L16 4v7a6 6 0 0 1-12 0ZM7.5 10.5h5M7.5 13h3" },
  tumbler: { stroke: "M5 4h10l-1.25 12.5h-7.5Z", fill: "M5.5 9h9l-.75 7.5h-7.5Z" },
  tv: { stroke: `${rect(2.75, 5, 14.5, 10, 1.75)}M7 2.5 10 5l3-2.5M6.5 17.5h7` },
  umbrella: { stroke: "M2.75 10a7.25 6.5 0 0 1 14.5 0ZM10 10v5.5a1.5 1.5 0 0 1-3 0M10 3.5v-1M6.5 10c0-3 1.5-5.5 3.5-6.5M13.5 10c0-3-1.5-5.5-3.5-6.5" },
  underwear: { stroke: "M3 5h14v3.5l-4 7H7l-4-7ZM3 8.5c2.5 0 5 1.5 7 4 2-2.5 4.5-4 7-4" },
  undo: { stroke: "M4 8h8.5a4 4 0 0 1 0 8H10M7.5 4.5 4 8l3.5 3.5" },
  // A person with a small cross beside it (nativeIconGlyphShapes.ts), five right of the box's middle.
  unfollow: { fill: `${personBeside}${smallCross(15)}` },
  "unlock-keyhole": { stroke: `${rect(4.7, 8.45, 10.6, 8.1, 0.5)}M6.85 4.6V3.45a1.9 1.9 0 0 1 3.8 0V7.5`, fill: "M10 9.9a1.25 1.25 0 0 1 .7 2.3l.6 2.8H8.7l.6-2.8A1.25 1.25 0 0 1 10 9.9Z" },
  upload: { stroke: "M6.5 13.5H6a3.5 3.5 0 0 1-.4-7A4.5 4.5 0 0 1 14.2 5.1a3.75 3.75 0 0 1 .05 8.4h-.75M10 17V9M7.75 11.25 10 9l2.25 2.25" },
  "upload-document": { stroke: `${documentOutline}M9.5 15v-5.5M7.25 11.75 9.5 9.5l2.25 2.25` },
  "upload-folder": { stroke: `${folderOutline}M10 14.5v-5M7.75 11.75 10 9.5l2.25 2.25` },
  upward: { fill: blockArrow(0) },
  // A ring round a solid head and shoulders.
  "user-circle": { stroke: ring, fill: `${personInRing.head}${shouldersWithin(personInRing.shoulders, RING_RADIUS)}` },
  "user-circle-dashed": { fill: `${dashedRing}${personInDashedRing.head}${shouldersWithin(personInDashedRing.shoulders, ringInside - DASH_BREAK)}` },
  "user-circle-filled": { fill: `${disc}${personInRing.head}${shouldersWithin(personInRing.shoulders, ringInside)}` },
  // An at sign, its outer turn on the round edge (ROUND_EDGE) as Notion's.
  username: { stroke: "M13.25 10a3.25 3.25 0 1 1-3.25-3.25A3.25 3.25 0 0 1 13.25 10v1.25a1.65 1.65 0 0 0 3.3 0V10a6.55 6.55 0 1 0-2.48 5.19" },
  "vacuum-cleaner": {
    stroke: "M9.5 10.5a3.5 3.5 0 0 1 7 0V15h-7ZM9.5 12c-2.5 0-4-1.5-4-4V3.5M3 17h5l-.5-2h-4ZM5.5 15V8",
    fill: `${circle(10.75, 16, 1)}${circle(15.25, 16, 1)}`,
  },
  verified: { stroke: `${burst(10, 7.5, 6.25)}M7 10.25l2 2 4-4.5` },
  "video-camera": { stroke: videoCamera },
  "video-camera-off": { stroke: `${videoCamera}M3 3l14 14` },
  "video-game": {
    stroke: "M6 6h8a4 4 0 0 1 3.9 4.9l-.8 3.5a2 2 0 0 1-3.6.7L12 13H8l-1.5 2.1a2 2 0 0 1-3.6-.7l-.8-3.5A4 4 0 0 1 6 6ZM6.5 8.5v3M5 10h3",
    fill: `${circle(13, 9, 0.8)}${circle(14.5, 10.75, 0.8)}`,
  },
  // A classic game pad on its cord.
  "video-game-classic": {
    stroke: `${rect(2.5, 9, 15, 7.5, 3.25)}M5.5 12.75h3M7 11.25v3M10 8.05V6.5c0-1.5 1-2.5 2.5-2.5h2`,
    fill: `${circle(12.75, 13.75, 0.9)}${circle(14.75, 12, 0.9)}`,
  },
  "video-game-joystick": { stroke: "M4 13h12v3a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1ZM10 13V7", fill: `${circle(10, 5, 2.25)}${circle(14, 11.5, 0.9)}` },
  // A tape with its window cut out and its two reels solid in it.
  videotape: { fill: `${rect(2.5, 5, 15, 10, 1.25)}${rect(4.5, 7, 11, 6, 1)}${circle(7.25, 10, 1.6)}${circle(12.75, 10, 1.6)}${rect(9.4, 8.25, 1.2, 3.5, 0.3)}` },
  view: { stroke: eye, fill: circle(10, 10, 2.75) },
  "view-off": { stroke: `${eye}${circle(10, 10, 2.5)}M3 3l14 14` },
  // Notion's record (read 2026-11-18): a disc 13.75 across at the left, its label a ring 4.375 across with a hole, and a
  // tone arm a line wide down the right from a round top, bending at 60° toward the disc's foot.
  "vinyl-record": { fill: `${circle(8.125, 10, 6.875)}${circle(8.125, 10, 2.1875)}${circle(8.125, 10, 0.9375)}${circle(17.1875, 5.3125, 0.9375)}`, stroke: "M17.1875 6.25V12.48L15.03 16.22" },
  // Notion's violin (read 2026-11-18), filled, upright: a body waisted at 11.875 with its neck up to a scroll at the box's top,
  // its foot parted to the box's foot, a slot cut across its body, and its bow upright at the right.
  violin: {
    fill: "M16.25 2.02V19.375H14.375V0.625H15L15.56 0.9Q16.25 1.25 16.25 2.02Z"
      + "M12.13 13.72Q12.5 14.6 12.5 15.625Q12.5 19.4 8.75 19.93V16.25H6.875V19.93Q3.125 19.4 3.125 15.625Q3.125 14.55 3.49 13.72Q5 13.5 5 11.875Q5 10.5 3.84 10.11Q3.75 9.75 3.75 9.375Q3.75 6.75 6.875 6.31V2.5H5.94V0H9.69V2.5H8.75V6.31Q11.875 6.75 11.875 9.375Q11.87 9.77 11.79 10.11Q10.625 10.5 10.625 11.875Q10.625 13.5 12.13 13.72Z"
      + bar(6.25, 13.125, 3.125, 1.25),
  },
  virgo: { stroke: "M3 6.5c1 0 2 1 2 2v7M5 8.5c0-1 1-2 2-2s2 1 2 2v7M9 8.5c0-1 1-2 2-2s2 1 2 2V13c0 2 1 3.5 3 3.5M13 12c1-1.5 2.5-2 3.5-1.5" },
  // A man standing with his arms out and his legs apart.
  "vitruvian-man": { stroke: "M3.75 7.75h12.5M10 7.75v4.75M10 12.5l-3.25 4.75M10 12.5l3.25 4.75", fill: circle(10, 4.25, 1.75), weight: FIGURE_LINE },
  // A tape with its two reels and the line between them cut out, the reels' hubs solid.
  voicemail: {
    fill: `${rect(2.5, 5.25, 15, 9.5, 1.25)}M6.75 12.5h6.5A2.5 2.5 0 1 0 11.47 11.75H8.53A2.5 2.5 0 1 0 6.75 12.5Z${circle(6.75, 10, 1.1)}${circle(13.25, 10, 1.1)}`,
  },
  volcano: { stroke: "M2.5 17 7 8.5h6l4.5 8.5ZM7 8.5l1.5 2 1.5-1 1.5 1 1.5-2M9 6c-.5-1 0-2 1-2.5M11.5 5.5c.5-1 .25-2-.5-3" },
  "volume-high": { stroke: `${speaker}M13 7.5a3.5 3.5 0 0 1 0 5M15.25 5.25a6.5 6.5 0 0 1 0 9.5` },
  // Notion's speaker with its sound off (read 2026-11-18): the speaker of its volume (a box 2.81 wide on 7.5..12.5 and a cone
  // to 4.06..15.94, its back 1.25 deep) set 2.5 right and cut by the slash.
  "volume-off": { fill: `${offCut([[5, 7.5], [7.81, 7.5], [11.25, 4.0625], [12.5, 4.0625], [12.5, 15.9375], [11.25, 15.9375], [7.81, 12.5], [5, 12.5]])}${offSlash}` },
  // Notion's walker (read 2026-11-18), filled: a head 3.125 across at the top and a body upright, an arm bent down at each side,
  // one leg stepping back to the foot left and the other forward to the round edge's foot.
  walk: { fill: `${circle(10, 3.75, 1.5625)}M12.35 8.98Q13.2 10.625 15.625 10.625V9.06Q14.4 9.06 13.54 7.98Q12.4 6.25 10 6.25Q5 6.25 5 11.25H6.5625Q6.5625 8.9 8.44 8.08V10Q8.44 13.4 5 16.17L6.33 17.5Q9.3 15 9.99 12.3Q12.5 14.6 12.5 17.5H14.375Q14.375 13.7 11.5625 11.2V8.2Q11.9 8.4 12.35 8.98Z` },
  wall: { stroke: "M2.75 4h14.5v12H2.75ZM2.75 8h14.5M2.75 12h14.5M7 4v4M13 4v4M10 8v4M5 12v4M15 12v4" },
  "watch-analog": { stroke: `${circle(10, 10, 4.5)}M7.5 6.25 8 3h4l.5 3.25M7.5 13.75 8 17h4l.5-3.25M10 8v2l1.25 1` },
  water: { stroke: "M10 2.75s5.5 6 5.5 9.25a5.5 5.5 0 0 1-11 0C4.5 8.75 10 2.75 10 2.75Z" },
  // Notion's whale (read 2026-11-18), filled: a body from its tail fluke at the top left swelling to the box's right side, a
  // fin under it, an eye cut out of it and a spout above.
  whale: {
    fill: "M13.125 3.125H11.875V1.875H13.125Q15 1.875 15 3.75V3.84Q15.29 3.75 15.625 3.75H16.875V5H15.625Q15 5 15 5.625V6.25H13.75V3.75Q13.75 3.125 13.125 3.125Z"
      + `M18.75 7.5V10Q18.75 14.75 14.32 14.98Q13.9 17.5 10.625 17.5V16.25L11.6 15H7.5Q2.5 15 2.5 10V6.01Q0.625 5.2 0.625 2.5H1.875L3.77 3.98L5.625 2.5H6.875Q6.875 5.2 5 6.01V6.25Q5 7.5 6.25 7.5Z${circle(13.44, 10.31, 0.94)}`,
  },

  wheat: { stroke: "M2.6 16.8 14.2 5.6M4.6 14.87V8.95M8.3 11.3V5.35M10.68 9h5.07M6.33 13.2h5.72", fill: "M13.9 5.8c.3-2.4 1.9-3.3 4.25-3.3-.1 2.4-1.7 3.4-4.25 3.3Z", weight: 1.3 },
  // Notion's wheelchair (read 2026-11-18), filled: a back from a handle at the top left, a seat across to 13.75, a footrest
  // reaching down to the right, a big wheel 8.12 across at the foot left with its hub cut out, and a small wheel at the right.
  wheelchair: {
    fill: "M14.69 12.81L16.35 15.31L18.75 13.72L18.05 12.68L16.7 13.58L15.35 11.56H13.75V8.125Q13.75 6.25 11.875 6.25H6.5625V5Q6.5625 3.125 4.68 3.125H3.125V4.375H4.68Q5.31 4.375 5.31 5V9.43A4.06 4.06 0 0 0 1.875 13.44A4.06 4.06 0 1 0 10 13.44Q10 13.1 9.94 12.81H12.5V13.86A1.875 1.875 0 1 0 13.75 13.86V12.81Z"
      + "M5.94 16.25A2.82 2.82 0 0 1 3.125 13.44Q3.125 11.1 5.31 10.7V10.94Q5.31 12.81 7.19 12.81H8.68Q8.75 13.11 8.75 13.44A2.82 2.82 0 0 1 5.94 16.25ZM12.5 8.75H6.5625V7.5H11.875Q12.5 7.5 12.5 8.125Z",
  },
  "wheelchair-access": { stroke: wheelchairPerson, fill: circle(9.5, 3.5, 1.25) },
  // Notion's powered wheelchair (read 2026-11-18), filled: a person's back and seat from the top left, a seat across to a motor
  // housing at the right with a lever, a big wheel 5 across at the foot right and a small one 3.75 across at the foot left.
  "wheelchair-motorized": {
    fill: "M16.25 14.97V15A2.5 2.5 0 1 1 11.25 15H8.28L7.08 13.8L6.06 14.82A1.875 1.875 0 1 1 5.18 13.93L6.25 12.86V11.25H5.31L2.81 2.5L3.53 2.14Q4.1 1.875 4.65 1.875H4.86Q5.8 1.875 6.07 2.78L6.37 3.84Q6.5 4.6 6.05 5.07L5.625 5.5L5.75 5.94H6.03Q7 5.94 7.23 6.84L7.24 6.875H11.875V8.125H7.6L7.95 9.375H13.76A1.25 1.25 0 0 1 13.76 11.875V12.5Q15.2 12.5 15.89 13.71L17.44 12.67L18.125 13.71L16.26 14.96Z",
  },
  // Notion's whistle (read 2026-11-18), filled: a round body 11 across at the left, its mouthpiece a stub to the box's side on
  // 8.75..11.25, and its pipe from its top across to the box's side, 3.75 deep, sloping down into the body, a vent cut in its top.
  whistle: { fill: "M20 4.375V8.125H18.44A5.55 5.55 0 0 0 13.82 10.59L12.12 13.15A5.54 5.54 0 0 1 7.5 15.625A5.63 5.63 0 0 1 2.02 11.25H0V8.75H2.02A5.62 5.62 0 0 1 7.5 4.375H9.375V6.25H11.25V4.375Z" },
  wifi: { stroke: "M7.25 12.5a4 4 0 0 1 5.5 0M4.75 10a7.5 7.5 0 0 1 10.5 0M2.25 7.5a11 11 0 0 1 15.5 0", fill: circle(10, 15.25, 1.25) },
  // Notion's wind (read 2026-11-18): three lines from 3.125, the top one turning up a quarter round 2.65 to the box's top, the
  // middle one curling up and back round 2.5 to end over it, the foot one curling down round 1.875 to end under itself.
  wind: {
    stroke: "M4.0625 6.5625H6.41A2.65 2.65 0 0 0 9.0625 3.91V3.4375M4.0625 9.6875H14.06A2.5 2.5 0 1 0 11.64 6.5625"
      + "M4.0625 12.8125H12.19A1.875 1.875 0 0 1 12.19 16.5625H11.5625",
  },
  window: {
    stroke: rect(2.75, 3.75, 14.5, 12.5, 1.75),
    fill: `M4.5 3.75h11a1.75 1.75 0 0 1 1.75 1.75v2.25H2.75V5.5A1.75 1.75 0 0 1 4.5 3.75Z${circle(5.25, 5.75, 0.65)}${circle(7.25, 5.75, 0.65)}${circle(9.25, 5.75, 0.65)}`,
  },
  wine: { stroke: "M6.05 3.15h7.9l1 3.9c.1.5.15.9.15 1.25 0 1.75-2.35 2.4-5.1 2.4s-5.1-.65-5.1-2.4c0-.35.05-.75.15-1.25ZM10 11.3v4.6M7.25 16.85h5.5", fill: "M5.65 6.3h8.7l.35 1.25c.1.4.15.75.15 1.05 0 1.25-2.05 1.75-4.85 1.75s-4.85-.5-4.85-1.75c0-.3.05-.65.15-1.05Z", weight: 1.3 },
  // A bottle with its label cut out, and a glass beside it poured half full.
  "wine-bottle": {
    fill: "M4.8 2.5h1.9v3.75c1.05.65 1.55 1.65 1.55 2.85v7.65a.75.75 0 0 1-.75.75H4a.75.75 0 0 1-.75-.75V9.1c0-1.2.5-2.2 1.55-2.85Z"
      + `${rect(4.25, 10.25, 3, 3.75, 0.4)}M10.25 3.5h6.5c.25 1.5.35 2.7.35 3.8 0 2.05-1.35 3.65-2.85 4v4.3H16v1.65h-5V15.6h1.75v-4.3c-1.5-.35-2.85-1.95-2.85-4 0-1.1.1-2.3.35-3.8Z`
      + "M11.4 5h4.2l.15 2.25h-4.5Z",
  },
  "wrapping-paper": { stroke: `M4.5 6h11a2 4 0 0 1 0 8h-11${ellipse(4.5, 10, 2, 4)}M8 6l2 8M11.5 6l2 8` },
  wrench: { stroke: "M13.5 3a4 4 0 0 0-3.9 5.1L3.5 14.25a1.75 1.75 0 0 0 2.5 2.5l6.1-6.1A4 4 0 0 0 17 7l-2.5 2.5-2.5-.5-.5-2.5L14 4Z" },
  "yin-yang": {
    stroke: ring,
    fill: `M10 3a7 7 0 0 1 0 14 3.5 3.5 0 0 1 0-7 3.5 3.5 0 0 0 0-7Z${circle(10, 13.5, 1)}${circle(10, 6.5, 1)}`,
  },
};
