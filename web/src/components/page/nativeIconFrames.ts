// The frames Notion cuts a person out of, which its other chat and tabs
// icons share (its chat, chat-user, comment, subtitles, tabs, tabs-user,
// badge and card rastered at 400px, 2026-11-18): each a filled shape on
// Notion's grid, what it holds cut out of it — a person by the person's
// rule (nativeIconPerson.ts), kept a line or a neck inside the frame.

import { EDGE_STEP, ROUND_EDGE, SQUARE_EDGE, polygon } from "./nativeIconGlyphShapes";
import { NECK, type Shoulders } from "./nativeIconPerson";
import { at, curve, enters, roundQuarters, split, type Point } from "./nativeIconRound";

/** How far a frame's inside is from its outside: Notion's line on its grid, six half steps. */
export const FRAME_LINE = 6 * EDGE_STEP;
/** A plate's corner (a badge's, a card's): eight half steps round. */
export const PLATE_CORNER = 8 * EDGE_STEP;

const near = 10 - ROUND_EDGE, far = 10 + ROUND_EDGE;

/** The middle of a speech bubble's body: its top on the round edge, and as far below that as a square's side is from the middle. */
export const BUBBLE_MIDDLE = near + SQUARE_EDGE;
/** Where the tail's underside meets the body's, from the round edge's left. */
const TAIL_MEETS = 5.1875;

/**
 * A speech bubble: a round body out to the round edge above and at both
 * sides, 13.75 tall, with a tail to the box's lower left. The tail's upper
 * side runs down the diagonal from the body to its tip on the round edge's
 * left, three half steps above the corner; from the tip it runs straight to
 * its foot on the round edge's bottom, two half steps in; and its underside
 * curves back from there, level at the foot, up to the body.
 */
export const bubble = (() => {
  const [upperRight, lowerRight, lowerLeft, upperLeft] = roundQuarters(10, BUBBLE_MIDDLE, ROUND_EDGE, SQUARE_EDGE);
  const tip: Point = [near, far - 3 * EDGE_STEP], foot: Point = [near + 2 * EDGE_STEP, far];
  // The lower left quarter runs from the body's foot round to its left side: the tail's underside leaves it, the tail's diagonal comes back to it.
  const [toTail] = split(lowerLeft, enters(lowerLeft, ([x]) => x <= near + TAIL_MEETS));
  const [, fromTail] = split(lowerLeft, enters(lowerLeft, ([x, y]) => x + y <= tip[0] + tip[1]));
  // The underside bends about a point level with the foot, ten half steps in from the corner: a cubic two thirds of the way to it from each end.
  const bend: Point = [near + 10 * EDGE_STEP, far];
  const toward = ([x, y]: Point): Point => [x + ((bend[0] - x) * 2) / 3, y + ((bend[1] - y) * 2) / 3];
  return `M${at(upperRight[0])}${curve(upperRight)}${curve(lowerRight)}${curve(toTail)}C${at(toward(toTail[3]))} ${at(toward(foot))} ${at(foot)}`
    + `L${at(tip)}L${at(fromTail[0])}${curve(fromTail)}${curve(upperLeft)}Z`;
})();
/** The bubble's inside below its middle, a line in from its body: the bowl a person's shoulders are kept within. */
export const bubbleBowl: Shoulders = { cx: 10, base: BUBBLE_MIDDLE, width: ROUND_EDGE - FRAME_LINE, height: FRAME_LINE - SQUARE_EDGE };

/** The front tab: a square ten on a side, its left and its foot on the square edge. */
export const TAB_FRONT = { left: 10 - SQUARE_EDGE, top: SQUARE_EDGE, right: 20 - SQUARE_EDGE, bottom: 10 + SQUARE_EDGE };
/**
 * Two tabs, sharp-cornered: the front one, and one as large behind it, its
 * top and right on the round edge, cut a neck clear of the front one.
 */
export const tabs = (() => {
  const { left, top, right, bottom } = TAB_FRONT, side = right - left;
  return polygon([[left, top], [right, top], [right, bottom], [left, bottom]])
    + polygon([[far - side, near], [far, near], [far, near + side], [right + NECK, near + side], [right + NECK, top - NECK], [far - side, top - NECK]]);
})();
