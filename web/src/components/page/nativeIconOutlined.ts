// The built-in icons Notion draws as outlines in its otherwise solid set
// (its 885 icons compared by name with Leafen's 2026-11-16 at 1440 dark, by
// the share of each one's outline its ink fills and then by eye): the shapes'
// outlines and their part-filled kinds, the outlined star, heart, bookmark,
// pin and person in a ring, the lens, a ring round a point turned down, a
// target's rings, the tones of a circle, the clouds without a mark cut out
// of them, and the round signs drawn as a ring with their mark — "no", the
// parking and smoking signs, peace, a steering wheel, a dashboard's gauge, a
// clock (a disc glyph named here is drawn so, nativeIconDrawing.tsx). So
// are the frames and rings Notion fills a mark inside (seen 2026-11-16 by
// shape over all 885): a hand in an octagon, a photo's mountains and sun, a
// post's blocks, a calendar's head, a chip's core, a window's bar, the
// liquid in a glass, an eye's iris, a target's middle, the piano's black
// keys; their glyph's lines are the frame and its fill the mark. An octagon,
// a square with a circle and the zooming lenses are solid there. A dashed
// shape is an outline anyway.

const PARTS = {
  circle: ["one-eighth", "two-eighths", "three-eighths", "four-eighths", "five-eighths", "six-eighths", "seven-eighths"],
  square: ["one-fourth", "two-fourths", "three-fourths"],
  triangle: ["one-third", "two-thirds"],
  pentagon: ["one-fifth", "two-fifths", "three-fifths", "four-fifths"],
  hexagon: ["one-sixth", "two-sixths", "three-sixths", "four-sixths", "five-sixths"],
} as const;

export const NATIVE_ICON_OUTLINED: ReadonlySet<string> = new Set([
  "circle", "square", "triangle", "pentagon", "hexagon",
  ...Object.entries(PARTS).flatMap(([shape, parts]) => parts.map((part) => `${shape}-${part}`)),
  "star-outline", "heart-outline", "bookmark-outline", "map-pin-alternate", "clock-alternate", "user-circle",
  "search", "arrow-circle-down", "target",
  "contrast", "darks", "midtones", "lights",
  "note-whole",
  "cloud", "cloud-off",
  "no", "parking", "parking-no", "smoking", "smoking-no", "peace", "steering-wheel", "dashboard",
  "infinity", "taurus", "orbit", "mirror", "menstrual-pad",
  "private", "photo-landscape", "feed", "calendar", "calendar-week", "computer-chip", "window",
  "tumbler", "highball", "wine", "view", "bullseye", "piano", "snorkel", "science",
]);
