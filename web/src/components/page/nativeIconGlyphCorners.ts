// How a built-in icon's lines and outlines turn a corner, as Notion's do
// (its 885 icons rastered at 400px, 2026-11-18): a corner turns sharp, as
// most of Notion's do (its document, mail, calendar, table, grids, cash,
// pause and stop), or round as Notion's icon turns its ink at that very
// corner — each corner decided on its own (a folder's top sharp and its
// foot round) — at the radius Notion's has once the glyph is placed in its
// box, whatever it was scaled by (nativeIconDrawing.tsx turns them after
// placing). A corner here is a short curve between two straight pieces
// that turns as they run; a glyph's round shapes — a ring, a bulb's body,
// a command key's loops — are none. Leafen's glyphs keep their own
// drawings; only how a corner turns is Notion's.

import { walk, type Point } from "./nativeIconGlyphPath";
import { EDGE_STEP, type NativeIconGlyph } from "./nativeIconGlyphShapes";

/** A round's radius is a whole number of quarter steps of Notion's grid (0.15625). */
const CORNER_STEP = EDGE_STEP / 2;

/**
 * The icons Notion turns round corners in, each corner of the glyph as
 * placed in its box decided by Notion's icon at that place (its 885 icons
 * rastered at 400px, 2026-11-18: how far in from the corner's point its
 * ink starts, Notion's two edges found beside it, then each corner's round
 * the one of its neighbours whose raster lies nearest Notion's), one mark
 * a corner in the glyph's order — its lines' first, then its fills': the
 * radius the ink's outside turns at in quarter steps, a base-36 digit ("0"
 * sharp, "e" 2.19 for a folder's foot or a phone's, "8" 1.25 for a
 * calculator's keys), or "." where Notion's is a curve of its own (a
 * speech bubble's, a lens's) and the glyph's own round stays. A mark past
 * the end is "0"; every other icon's corners turn sharp.
 */
const ROUND_CORNERS = /* screen words: not shown */ `
  activity:ffff archery:.. archive:0gg art:q00q baby-bottle:.... badge:.... baggage-claim:........ battery-charged:ooff
  battery-charging:ooff battery:ooffgegg bed:.. bee:.......... beer-bottle:.. beer:0k0g bookmark-outline:ff bookmark:ff
  boombox:.00. boot:i branch-fork:08 briefcase:........ bug:...... bugle:4g burger:.. bus-double-decker:wwww bus-metro:e88e cafe:....
  bus:teet cake:gg calculator:78880.. calendar-day:c00c0088 calendar-month:c00c calendar-week:c00c calendar:c00c
  camera-off:i.ws camera-roll-portrait:0s0s camera-roll:0t0u camera:.k card-club:.... card-diamond:....
  card-heart:.... card-spade:.... card:.... cards:o0u0.w cassette:0000gggg cat:.... categories:k00m
  champagne-bottle:.. chart-alternate:808800888888 chart-mixed:0008 chicken:..... chili-pepper:.. cigarette:. chart:088808808888 clothes-iron:. coat:....
  color-picker:.. comb:... command-line:ffff compose:o0o computer:8008 copy:0w0o0w
  corn:440440044444 couch:oo cow:.... create:o0o credit-card:.... crutch:...... currency:i cursor-button:fddf cut:.. dance:008. dental:000c
  dependency:ii0i0iii. donkey:. duster:... ear-hearing-aid:go electric-plug:.. elephant:. feed:0000000c.00.000.0cc fire-extinguisher:.. first-aid-kit:.ff.88
  folder:00ff gavel:........ ghost:.. grid-dense:0.000..002.0..00000000...000.00.200. grid-wide:2 gym:k.8484kk hare:...
  headphones:.0.00.0. headset:0..00. heart-rate-monitor:ffff helicopter:. immigration:00ii
  keyboard-alternate:kk.044880..000kk0..0kk000.k.8844.00. keyboard:8888 kind:kk kite:.. language:q..o.
  laptop:8888 lemon:. lock-keyhole:e00e log-in:.. log-out:.. luggage-cart:.00m luggage:.00. mahjong:4444 meeting:606 move:00ff
  movie-camera:00qq movie-clapboard-play:0.. movie-clapboard:0.. new-folder:00ff news:. notification:....
  official-document:wmq ornament:k00. paint-brush-wide:.. paint-roller:gg000. passport:ff paste:.00..00. pen:.. person-masculine:.gg.
  phone-call:iiii phone-end-call:.gg. phone:ffff piano:sggs pitcher:f. postage-stamp:88888888
  pot:...... preview:088 print:0qq pump-bottle:qq redirect:i repeat:.. run:. scarf:. scorpio:8 script:7. share:.00. sheep:4. shovel-and-pail:.. shower:...
  sign-in:00.. sign-out:oo slide:....8886 snake:... snippet:.... snorkel:ao0. soap:........ sock:.. soda-bottle:...... sticker:wvv stomach:.. stovetop:0000g00g
  stroller:. subtask:00gggggg. suitcase:q..saa tablet:ffff throat:.. thumbs-down:808g.2. thumbs-up:08g8.28
  timeline:00..000000.. train-high-speed:.0. train-light-rail:.... train-magnetic-levitation:.... train-metro:.... unlock-keyhole:c00c unlock:c00c
  upload-folder:00ff video-camera-off:o.dd video-camera:ooff video-game-classic:. video-game:.. videotape:0000icci088 violin:. whale:.... wheelchair:..... wheelchair-motorized:...
  window:ffff wine-bottle:4886aa
`;

export const NATIVE_ICON_ROUND_CORNERS: ReadonlyMap<string, string> = new Map(
  ROUND_CORNERS.trim().split(/\s+/).filter(Boolean).map((entry) => entry.split(":") as [string, string]),
);

/** A corner's curve: as short as a corner, turning at least this much and at most this much (degrees), its straight pieces running on as it does. */
const CORNER = { chord: 3.6, least: 25, most: 160, along: 8 };

type Straight = { upper: "L"; from: Point; to: Point; way: Point };
type Other = { upper: string; values: number[]; from: Point; to: Point; leaves: Point | null; arrives: Point | null };
type Part = Straight | Other;

const isStraight = (part: Part | undefined): part is Straight => part?.upper === "L";
const degrees = (a: Point, b: Point) => (Math.abs(Math.atan2(a[0] * b[1] - a[1] * b[0], a[0] * b[0] + a[1] * b[1])) * 180) / Math.PI;
const apart = (a: Point, b: Point) => Math.hypot(a[0] - b[0], a[1] - b[1]);
const n = (value: number) => Number(value.toFixed(4));

/** A path's subpaths as parts in absolute terms: its straight pieces as lines, a closing line that has a length among them. */
function subpaths(path: string) {
  const out: Array<{ start: Point; parts: Part[]; closed: boolean }> = [];
  walk(path, ({ upper, values, from, to, leaves, arrives }) => {
    if (upper === "M") { out.push({ start: to, parts: [], closed: false }); return; }
    const sub = out[out.length - 1];
    if (!sub) return;
    if (upper === "Z") sub.closed = true;
    if (upper === "L" || upper === "H" || upper === "V" || upper === "Z") {
      if (apart(from, to) > 1e-6) sub.parts.push({ upper: "L", from, to, way: [(to[0] - from[0]) / apart(from, to), (to[1] - from[1]) / apart(from, to)] });
      return;
    }
    sub.parts.push({ upper, values, from, to, leaves, arrives });
  });
  return out;
}

/**
 * Whether the part at `index` is a corner: a short curve between two
 * straight pieces, turning as they run, the two pieces meeting ahead of
 * the one and behind the other — a loop that turns the long way round
 * (a command key's) is none.
 */
function isCorner(parts: Part[], index: number, closed: boolean) {
  const part = parts[index]!;
  if (isStraight(part) || !part.leaves || !part.arrives || (part.upper === "A" && part.values[3] === 1)) return false;
  const before = parts[index - 1] ?? (closed ? parts[parts.length - 1] : undefined), after = parts[index + 1] ?? (closed ? parts[0] : undefined);
  const turn = degrees(part.leaves, part.arrives);
  if (!(apart(part.from, part.to) <= CORNER.chord && turn >= CORNER.least && turn <= CORNER.most
    && isStraight(before) && isStraight(after) && degrees(before.way, part.leaves) < CORNER.along && degrees(part.arrives, after.way) < CORNER.along)) return false;
  const meet = meeting(before, after);
  return meet !== null && meet.ahead >= -1e-6 && meet.behind >= -1e-6;
}

/** Where two straight pieces' lines meet: the point, how far ahead of the first's end and how far behind the second's start. */
function meeting(before: Straight, after: Straight) {
  const [a, u] = [before.to, before.way], [b, v] = [after.from, after.way];
  const cross = u[0] * v[1] - u[1] * v[0];
  if (Math.abs(cross) < 1e-9) return null;
  const ahead = ((b[0] - a[0]) * v[1] - (b[1] - a[1]) * v[0]) / cross;
  const at: Point = [a[0] + u[0] * ahead, a[1] + u[1] * ahead];
  return { at, ahead, behind: (b[0] - at[0]) * v[0] + (b[1] - at[1]) * v[1], cross };
}

/** A corner: where its two straight pieces' lines meet, the way the one runs into it and the other out, and whether it is a fill's edge or a line's middle. */
export type Corner = { at: Point; into: Point; out: Point; fill: boolean };

/**
 * A path with each corner turned as `turn` says — the radius its line's
 * middle (or a fill's edge) turns at, 0 for sharp, or null to keep it as
 * drawn — the corners numbered on from `first`. A round is an arc between the two straight
 * pieces; a sharp corner is the point where they meet.
 */
function turnPath(path: string, first: number, fill: boolean, turn: (index: number, corner: Corner) => number | null): { path: string; count: number } {
  let count = 0, out = "", turnedAny = false;
  for (const { start, parts, closed } of subpaths(path)) {
    // Each corner: where its two straight pieces meet, the ways they run, and where its arc leaves and rejoins them.
    const corners = new Map<number, { at: Point; enter: Point; leave: Point; radius: number; sweep: number } | null>();
    parts.forEach((_, index) => {
      if (!isCorner(parts, index, closed)) return;
      const before = (parts[index - 1] ?? parts[parts.length - 1]) as Straight, after = (parts[index + 1] ?? parts[0]) as Straight;
      const [u, v] = [before.way, after.way], { at, cross } = meeting(before, after)!;
      const radius = turn(first + count++, { at, into: u, out: v, fill });
      if (radius === null) { corners.set(index, null); return; }
      turnedAny = true;
      // How far from the corner the arc leaves each piece: no further than half its way to the piece's far end.
      const room = (piece: Straight, end: Point) => apart(piece.from === end ? piece.to : piece.from, at) * 0.5;
      const reach = Math.min(radius * Math.tan((degrees(u, v) * Math.PI) / 360), room(before, before.to), room(after, after.from));
      corners.set(index, { at, enter: [at[0] - u[0] * reach, at[1] - u[1] * reach], leave: [at[0] + v[0] * reach, at[1] + v[1] * reach], radius: reach / Math.tan((degrees(u, v) * Math.PI) / 360), sweep: cross > 0 ? 1 : 0 });
    });
    const turned = (index: number) => corners.get(index);
    const last = parts.length - 1;
    // Where the subpath starts: where its first corner's arc leaves the closing piece, or rejoins the first piece.
    const head = turned(0) ? turned(0)!.enter : closed && turned(last) && apart(parts[last]!.to, start) < 1e-6 ? turned(last)!.leave : start;
    out += `M${n(head[0])} ${n(head[1])}`;
    parts.forEach((part, index) => {
      const corner = turned(index);
      if (corner) {
        // A sharp corner is where the piece before it ends: the next one runs on from there.
        if (corner.radius > 1e-6) out += `A${n(corner.radius)} ${n(corner.radius)} 0 0 ${corner.sweep} ${n(corner.leave[0])} ${n(corner.leave[1])}`;
        return;
      }
      if (isStraight(part)) {
        const next = turned(index === last && closed ? 0 : index + 1);
        const end = next ? next.enter : part.to;
        out += `L${n(end[0])} ${n(end[1])}`;
        return;
      }
      out += `${part.upper}${(part as Other).values.map(n).join(" ")}`;
    });
    if (closed) out += "Z";
  }
  // A path with no corner to turn is left as it was written.
  return { path: turnedAny ? out : path, count };
}

/**
 * A glyph with each corner turned as `outside` says — the radius the ink's
 * outside turns at, 0 for sharp, or null to keep it as drawn — its lines
 * `line` wide: a line's middle turns round inside it by half a line, a
 * fill's edge at it. A dashed glyph keeps its corners: its dashes are laid
 * along them. Its lines' corners are numbered first, then its fills'.
 */
export function turnCornersWith(glyph: NativeIconGlyph, line: number, outside: (index: number, corner: Corner) => number | null): NativeIconGlyph {
  if (glyph.dash) return glyph;
  const middle = (index: number, corner: Corner) => {
    const radius = outside(index, corner);
    return radius === null || corner.fill ? radius : Math.max(0, radius - line / 2);
  };
  const stroke = glyph.stroke ? turnPath(glyph.stroke, 0, false, middle) : { path: undefined, count: 0 };
  const fill = glyph.fill ? turnPath(glyph.fill, stroke.count, true, middle) : { path: undefined };
  return { ...glyph, ...(stroke.path === undefined ? {} : { stroke: stroke.path }), ...(fill.path === undefined ? {} : { fill: fill.path }) };
}

/**
 * A glyph, as placed in its box, with each corner turned as Notion's icon
 * of that name turns its ink at that place (NATIVE_ICON_ROUND_CORNERS),
 * at that size: placed first, so a round's radius is Notion's whatever the
 * glyph was scaled by.
 */
export function turnCorners(name: string, glyph: NativeIconGlyph, line: number): NativeIconGlyph {
  const marks = NATIVE_ICON_ROUND_CORNERS.get(name) ?? "";
  return turnCornersWith(glyph, line, (index) => {
    const mark = marks[index] ?? "0";
    return mark === "." ? null : parseInt(mark, 36) * CORNER_STEP;
  });
}
