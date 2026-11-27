// Notion's person, as its built-in icons draw it (their user, user circles,
// follow, friends and groups, and the people on its chat, tabs, badge and
// card, rastered at 400px, 2026-11-18): a round head, a neck's gap, and
// shoulders a dome on a flat base — as wide each way from the head's middle
// as they are tall. Head and shoulders are drawn alike, each quarter Notion's
// round (nativeIconRound.ts). A person drawn inside a ring or a speech bubble
// keeps to its inside, one behind a frame's foot is cut flat along it, and
// one standing behind another is cut a neck clear of the other's shoulders.

import { EDGE_STEP, n } from "./nativeIconGlyphShapes";
import { ROUND_HANDLE, at, back, curve, enters, flip, pointAt, round, split, type Cubic, type Point } from "./nativeIconRound";

/**
 * Shoulders: a dome round `cx`, `width` each way and `height` tall, on its
 * base; `handle` where it is not the person's. With a height below nothing
 * it hangs from its base — a bowl, the lower half of a round frame's inside.
 */
export type Shoulders = { cx: number; base: number; width: number; height: number; handle?: number };

/** The gap under a person's head, and between people: two steps of Notion's grid. */
export const NECK = 4 * EDGE_STEP;
/** A head at the size Notion's people mostly have (follow, friends, a group's middle, a person in a ring or a speech bubble). */
export const PERSON_HEAD = 8 * EDGE_STEP;
/**
 * A small person's neck and shoulders (a group's sides, the people on a
 * badge, a card and a tab): the neck half a step shorter, the shoulders
 * half a step narrower than twice the head.
 */
export const smallPerson = (radius: number) => ({ neck: 3 * EDGE_STEP, width: 2 * radius - EDGE_STEP });

/** The shoulders' left quarter, from the foot up to the top. */
function leftQuarter({ cx, base, width, height, handle = ROUND_HANDLE }: Shoulders): Cubic {
  return [[cx - width, base], [cx - width, base - handle * height], [cx - handle * width, base - height], [cx, base - height]];
}
/** Whether a point lies within shoulders, or within a bowl (sampled finely enough for a 20-unit box). */
function within(shoulders: Shoulders, [x, y]: Point) {
  const up = Math.sign(shoulders.height);
  if ((shoulders.base - y) * up < 0 || Math.abs(x - shoulders.cx) > shoulders.width) return false;
  const quarter = leftQuarter(shoulders), side = x < shoulders.cx ? x : 2 * shoulders.cx - x;
  // The left quarter runs left to right as it leaves its base, so its height over `side` is one crossing.
  return (y - pointAt(quarter, enters(quarter, ([qx]) => qx >= side))[1]) * up >= 0;
}

/**
 * A person with its head's top at `top`: a head of radius `radius`, and
 * under it, a neck apart, shoulders `width` each way and as tall — twice
 * the head unless given.
 */
export function person(cx: number, top: number, radius: number, { neck = NECK, width = 2 * radius } = {}) {
  const shoulders: Shoulders = { cx, base: top + 2 * radius + neck + width, width, height: width };
  const left = leftQuarter(shoulders);
  return { head: round(cx, top + radius, radius), shoulders, outline: `M${at(left[0])}${curve(left)}${curve(flip(left, cx))}Z` };
}

/** The same person standing on `base`. */
export function standing(cx: number, base: number, radius: number, { neck = NECK, width = 2 * radius } = {}) {
  return person(cx, base - width - neck - 2 * radius, radius, { neck, width });
}

/** Shoulders kept within `radius` of the box's middle, cut along that circle below: a person in a ring. */
export function shouldersWithin(shoulders: Shoulders, radius: number) {
  const [, upper] = split(leftQuarter(shoulders), enters(leftQuarter(shoulders), ([x, y]) => Math.hypot(x - 10, y - 10) <= radius));
  return `M${at(upper[0])}${curve(upper)}${curve(flip(upper, shoulders.cx))}A${n(radius)} ${n(radius)} 0 0 1 ${at(upper[0])}Z`;
}

/** Shoulders kept within a bowl about the same middle, cut along it below: a person in a speech bubble. */
export function shouldersInBowl(shoulders: Shoulders, bowl: Shoulders) {
  const dome = leftQuarter(shoulders), rim = leftQuarter(bowl);
  const [, upper] = split(dome, enters(dome, (point) => within(bowl, point)));
  const [, lower] = split(rim, enters(rim, (point) => within(shoulders, point)));
  return `M${at(upper[0])}${curve(upper)}${curve(flip(upper, shoulders.cx))}${curve(back(flip(lower, bowl.cx)))}${curve(back(lower))}Z`;
}

/** Shoulders down to `floor` only, cut flat along it: a person behind a frame's foot. */
export function shouldersAbove(shoulders: Shoulders, floor: number) {
  const [, upper] = split(leftQuarter(shoulders), enters(leftQuarter(shoulders), ([, y]) => y <= floor));
  return `M${at(upper[0])}${curve(upper)}${curve(flip(upper, shoulders.cx))}Z`;
}

/**
 * Shoulders standing behind `front`'s, cut a neck clear of them, as Notion's
 * friends and groups are (1.25 ± 0.01 all along): the line of the cut is the
 * front shoulders grown by a neck each way and upwards on the same base,
 * their handles eased so the cut's middle is a neck out too — within 0.005
 * of the true offset.
 */
export function shouldersBehind(shoulders: Shoulders, front: Shoulders, neck = NECK) {
  // How far a square dome's quarter is from its foot's middle at its own middle, across the diagonal.
  const middle = (width: number, handle: number) => width * (0.5 + 0.375 * handle) * Math.SQRT2;
  const grown = front.width + neck;
  const handle = ((middle(front.width, front.handle ?? ROUND_HANDLE) + neck) / (grown * Math.SQRT2) - 0.5) / 0.375;
  // Drawn with the front person on the left, then turned back.
  const mirrored = front.cx > shoulders.cx;
  const turn = (s: Shoulders): Shoulders => (mirrored ? { ...s, cx: 20 - s.cx } : s);
  const behind = turn(shoulders), cut = turn({ ...front, width: grown, height: front.height + neck, handle });
  const behindLeft = leftQuarter(behind), cutRight = flip(leftQuarter(cut), cut.cx);
  const [, rising] = split(behindLeft, enters(behindLeft, (point) => !within(cut, point)));
  const [, falling] = split(cutRight, enters(cutRight, (point) => within(behind, point)));
  const pieces: Cubic[] = [rising, flip(behindLeft, behind.cx), back(falling)];
  const place = (point: Point): Point => (mirrored ? [20 - point[0], point[1]] : point);
  const [first, top, last] = pieces.map((piece) => piece.map(place) as Cubic);
  return `M${at(first![0])}${curve(first!)}${curve(top!)}L${at(last![0])}${curve(last!)}Z`;
}

/**
 * A person at the box's left, beside a mark (follow, following, unfollow):
 * the usual head on a base at 15, its shoulders two half steps in from the
 * box's side.
 */
const beside = standing(2 * EDGE_STEP + 2 * PERSON_HEAD, 15, PERSON_HEAD);
export const personBeside = `${beside.head}${beside.outline}`;
