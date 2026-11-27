import { NATIVE_ICON_GLYPHS, type NativeIconGlyph } from "./nativeIconGlyphs";
import { NATIVE_ICON_LINE, ROUND_EDGE, ring } from "./nativeIconGlyphShapes";
import { NATIVE_ICON_OUTLINED } from "./nativeIconOutlined";
import { NATIVE_ICON_BOXES } from "./nativeIconBoxes";
import { placeInBox, type Drawn } from "./nativeIconGlyphFit";
import { MITER_LIMIT, cutLines, endsPath, fillPoints, placeEnds, readLines, type LineEnds } from "./nativeIconGlyphInk";
import { transformGlyph, transformPath, walk } from "./nativeIconGlyphPath";
import { turnCorners } from "./nativeIconGlyphCorners";
import { inSlices } from "@/lib/afterPress";
import { afterStart } from "@/lib/startSettled";

// How a built-in icon is drawn, as Notion draws its set (its 884 icons seen
// side by side 2026-11-16 at 1440 dark, each measured by the share of its
// box it inks): solid glyphs. A thing's outline is filled, with the lines
// drawn inside it cut out of the fill — a book's pages, a face's eyes, a
// die's pips — and the lines outside it drawn at Notion's weight, 1.875 of
// the 20 units (its plus, square, circle, search and arrow, 18–22 of 200 at
// 200px). The icons Notion leaves as outlines — the shapes and their
// dashed and part-filled kinds, an outlined star, heart and bookmark, a
// lens — stay outlines (nativeIconOutlined.ts); so do a glyph with no
// closed outline, an icon cut out of a disc and an icon drawn filled.
// Every line ends flat and turns in a sharp corner, as Notion's do
// (nativeIconGlyphInk.ts) — an outline drawn solid has sharp corners so —
// and a corner drawn round turns round only where Notion's does
// (nativeIconGlyphCorners.ts).
// Leafen's glyphs stay its own drawings (nativeIconGlyphs.ts); how they are
// painted follows Notion's, and so does the box each fills (its
// proportions and offset, nativeIconBoxes.ts).

const ARGUMENTS: Readonly<Record<string, number>> = { M: 2, L: 2, H: 1, V: 1, C: 6, S: 4, Q: 4, T: 2, A: 7, Z: 0 };
const TOKENS = /[MLHVCSQTAZmlhvcsqtaz]|-?(?:\d*\.\d+|\d+\.?)(?:e-?\d+)?/g;
const round = (value: number) => Math.round(value * 1000) / 1000;

/**
 * A stroke's outlines, closed by Z, and its open lines, each a path of its
 * own: a subpath that starts with a relative move starts again from where
 * the one before it left the pen, written out.
 */
export function strokeParts(path: string): { closed: string; open: string } {
  const tokens = path.match(TOKENS) ?? [];
  const parts: Array<{ d: string; closed: boolean }> = [];
  let at = 0, command = "", x = 0, y = 0, startX = 0, startY = 0;
  let part: { d: string; closed: boolean } | null = null;
  while (at < tokens.length) {
    if (/[a-z]/i.test(tokens[at]!)) command = tokens[at++]!;
    const upper = command.toUpperCase(), relative = command !== upper;
    if (upper === "Z") {
      if (part) { part.d += "Z"; part.closed = true; }
      x = startX; y = startY; command = "";
      continue;
    }
    const count = ARGUMENTS[upper];
    if (count === undefined || at + count > tokens.length) break;
    const values = tokens.slice(at, at + count).map(Number);
    at += count;
    if (upper === "M") {
      x = startX = relative ? x + values[0]! : values[0]!;
      y = startY = relative ? y + values[1]! : values[1]!;
      part = { d: `M${round(x)} ${round(y)}`, closed: false };
      parts.push(part);
      command = relative ? "l" : "L";
      continue;
    }
    if (!part) break;
    part.d += `${command}${values.join(" ")}`;
    if (upper === "H") x = relative ? x + values[0]! : values[0]!;
    else if (upper === "V") y = relative ? y + values[0]! : values[0]!;
    else {
      x = relative ? x + values[count - 2]! : values[count - 2]!;
      y = relative ? y + values[count - 1]! : values[count - 1]!;
    }
  }
  return {
    closed: parts.filter((piece) => piece.closed).map((piece) => piece.d).join(""),
    open: parts.filter((piece) => !piece.closed).map((piece) => piece.d).join(""),
  };
}

/** What a glyph's drawing allows a placing (nativeIconGlyphFit.ts): straight lines only, a turned oval that a one-way stretch cannot draw. */
function drawnAs(glyph: NativeIconGlyph): Drawn {
  let straight = true, stretches = true;
  for (const path of [glyph.fill, glyph.stroke]) if (path) walk(path, ({ upper, values }) => {
    if (!"MLHVZ".includes(upper)) straight = false;
    if (upper === "A" && values[2]! % 90 !== 0) stretches = false;
  });
  return { straight, stretches };
}

/** A built-in icon as drawn: its glyph, and where it is drawn solid its outlines and the open lines cut out of them. */
export type NativeIconDrawing = { glyph: NativeIconGlyph; solid?: { closed: string; open: string } };

/** A built-in icon's glyph brought into its box, its corners not yet turned: the glyph, its lines' weight, where its open lines' ends are, and whether its lines are drawn solid. */
export type PlacedGlyph = { glyph: NativeIconGlyph; line: number; ends: LineEnds; solidLines: boolean };

export function placedGlyph(name: string): PlacedGlyph | null {
  const drawn = NATIVE_ICON_GLYPHS[name];
  // A mark cut out of a disc that Notion draws as an outline is the mark inside a ring (inRing).
  const inRing = drawn?.disc && NATIVE_ICON_OUTLINED.has(name) ? { stroke: `${ring}${drawn.stroke ?? ""}`, fill: drawn.fill } : drawn;
  if (!inRing) return null;
  const solidLines = Boolean(inRing.stroke && !inRing.disc && !inRing.dash && !NATIVE_ICON_OUTLINED.has(name));
  const solid = solidLines && Boolean(strokeParts(inRing.stroke!).closed);
  // Its lines as wide as they are drawn; a dashed line's dashes end where they are cut.
  const line = solid || inRing.disc ? NATIVE_ICON_LINE : inRing.weight ?? NATIVE_ICON_LINE;
  // Measured with its corners turned as they will be (nativeIconGlyphCorners.ts), at their size as drawn.
  const turned = turnCorners(name, inRing, line);
  const lines = readLines(turned.stroke, !turned.dash, solid);
  // Brought into the box Notion's icon inks (nativeIconBoxes.ts, nativeIconGlyphFit.ts); a disc stays on the round edge.
  const box = inRing.disc ? undefined : NATIVE_ICON_BOXES.get(name);
  const placing = box ? placeInBox(fillPoints(turned.fill), lines, box, line, drawnAs(turned)) : null;
  // Written out again even where it stays as drawn: the lines' curves whole, their corners with a way to turn (nativeIconGlyphPath.ts).
  const glyph = placing ? transformGlyph(inRing, ...placing) : inRing.stroke ? { ...inRing, stroke: transformPath(inRing.stroke, 1, 1, 0, 0) } : inRing;
  return { glyph, line, ends: placing ? placeEnds(lines.ends, ...placing) : lines.ends, solidLines };
}

const drawings = new Map<string, NativeIconDrawing | null>();

export function nativeIconDrawing(name: string): NativeIconDrawing | undefined {
  const known = drawings.get(name);
  if (known !== undefined) return known ?? undefined;
  const placed = placedGlyph(name);
  let drawing: NativeIconDrawing | null = null;
  if (placed) {
    // Its corners turned sharp, or round as Notion's icon turns them at that place, at their size as placed (nativeIconGlyphCorners.ts).
    const turned = turnCorners(name, placed.glyph, placed.line);
    // Its lines' flat ends and the corners where two of them end are pieces of the same path (nativeIconGlyphInk.ts).
    const flat = endsPath(placed.ends, placed.line / 2);
    const glyph = flat ? { ...turned, stroke: `${turned.stroke}${flat}` } : turned;
    // Drawn solid: its outlines filled, and of its open lines those inside them cut out (nativeIconGlyphInk.ts cutLines).
    const parts = placed.solidLines ? strokeParts(glyph.stroke!) : null;
    drawing = parts?.closed ? { glyph, solid: { closed: parts.closed, open: cutLines(parts.open, parts.closed) } } : { glyph };
  }
  drawings.set(name, drawing);
  return drawing ?? undefined;
}

/**
 * Works out the drawings of `names()` ahead of the press that shows them,
 * once the page's start has settled, in slices a press goes before
 * (afterPress.ts inSlices), each kept as the press then finds it. Worked out
 * by the press, the icon picker's Icons tab spent most of its press on them:
 * its first 108 on a NAS-class server with Chrome's CPU ×2 took the tab's
 * press to its first paint 136–184 ms (median 152), and 88–120 (104) worked
 * out ahead; a picker opened straight on the tab 136–168 (152) against
 * 88–120 (104) (2026-11-18, nine presses each; Notion's own 88).
 */
export function drawNativeIconsAhead(names: () => readonly string[]) {
  afterStart(() => { void inSlices(names(), (name) => { nativeIconDrawing(name); }); });
}

/** How every line is painted: cut off flat where its path ends, its corners sharp up to the limit and cut flat past it (nativeIconGlyphInk.ts). */
const LINE_PAINT = { strokeLinecap: "butt", strokeLinejoin: "miter", strokeMiterlimit: MITER_LIMIT } as const;

/**
 * The icon inside a 20-unit view, in currentColor. A solid one paints a
 * square through a mask: its outlines filled and its lines, and its own
 * fills, white; its open lines and fills inside the outlines black. `id`
 * names its mask and clip, one per drawing on the page.
 */
export function NativeIconBody({ name, id }: { name: string; id: string }) {
  const drawing = nativeIconDrawing(name);
  if (!drawing) {
    return <rect x="3.5" y="3.5" width="13" height="13" rx="3.5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeDasharray="2.5 2" />;
  }
  const { glyph, solid } = drawing;
  if (glyph.disc) return (
    <>
      <mask id={id}>
        <rect width="20" height="20" fill="white" />
        {glyph.fill && <path d={glyph.fill} fill="black" fillRule="evenodd" />}
        {glyph.stroke && <path d={glyph.stroke} fill="none" stroke="black" strokeWidth={NATIVE_ICON_LINE} {...LINE_PAINT} />}
      </mask>
      <circle cx="10" cy="10" r={ROUND_EDGE} fill="currentColor" mask={`url(#${id})`} />
    </>
  );
  if (solid) return (
    <>
      <clipPath id={`${id}-in`}><path d={solid.closed} clipRule="evenodd" /></clipPath>
      <mask id={id} maskUnits="userSpaceOnUse" x="0" y="0" width="20" height="20">
        <path d={solid.closed} fill="white" fillRule="evenodd" />
        <path d={glyph.stroke} fill="none" stroke="white" strokeWidth={NATIVE_ICON_LINE} {...LINE_PAINT} />
        {glyph.fill && <path d={glyph.fill} fill="white" fillRule="evenodd" />}
        {solid.open && <path d={solid.open} clipPath={`url(#${id}-in)`} fill="none" stroke="black" strokeWidth={NATIVE_ICON_LINE} {...LINE_PAINT} />}
        {glyph.fill && <path d={glyph.fill} clipPath={`url(#${id}-in)`} fill="black" fillRule="evenodd" />}
      </mask>
      <rect width="20" height="20" fill="currentColor" mask={`url(#${id})`} />
    </>
  );
  return (
    <>
      {glyph.fill && <path d={glyph.fill} fill="currentColor" fillRule="evenodd" />}
      {glyph.stroke && <path d={glyph.stroke} fill="none" stroke="currentColor" strokeWidth={glyph.weight ?? NATIVE_ICON_LINE} strokeDasharray={glyph.dash} {...LINE_PAINT} />}
    </>
  );
}

/** The same icon as an SVG document's markup in one colour, for a window's icon (pageFavicon.ts) and a mention's (pageMentionIconCss.ts). */
export function nativeIconMarkup(name: string, paint: string): string {
  const drawing = nativeIconDrawing(name);
  const ends = `stroke-linecap="${LINE_PAINT.strokeLinecap}" stroke-linejoin="${LINE_PAINT.strokeLinejoin}" stroke-miterlimit="${LINE_PAINT.strokeMiterlimit}"`;
  const line = (d: string, color: string, width: number, extra = "") => `<path d="${d}" fill="none" stroke="${color}" stroke-width="${width}" ${ends}${extra}/>`;
  const fill = (d: string, color: string, extra = "") => `<path d="${d}" fill="${color}" fill-rule="evenodd"${extra}/>`;
  let body: string;
  if (!drawing) {
    body = `<rect x="3.5" y="3.5" width="13" height="13" rx="3.5" fill="none" stroke="${paint}" stroke-width="1.5" stroke-dasharray="2.5 2"/>`;
  } else if (drawing.glyph.disc) {
    const { glyph } = drawing;
    body = `<mask id="cut"><rect width="20" height="20" fill="white"/>${glyph.fill ? fill(glyph.fill, "black") : ""}`
      + `${glyph.stroke ? line(glyph.stroke, "black", NATIVE_ICON_LINE) : ""}</mask><circle cx="10" cy="10" r="${ROUND_EDGE}" fill="${paint}" mask="url(#cut)"/>`;
  } else if (drawing.solid) {
    const { glyph, solid } = drawing, inside = ` clip-path="url(#in)"`;
    body = `<clipPath id="in"><path d="${solid.closed}" clip-rule="evenodd"/></clipPath>`
      + `<mask id="cut" maskUnits="userSpaceOnUse" x="0" y="0" width="20" height="20">${fill(solid.closed, "white")}`
      + `${line(glyph.stroke!, "white", NATIVE_ICON_LINE)}${glyph.fill ? fill(glyph.fill, "white") : ""}`
      + `${solid.open ? line(solid.open, "black", NATIVE_ICON_LINE, inside) : ""}${glyph.fill ? fill(glyph.fill, "black", inside) : ""}</mask>`
      + `<rect width="20" height="20" fill="${paint}" mask="url(#cut)"/>`;
  } else {
    const { glyph } = drawing;
    body = `${glyph.fill ? fill(glyph.fill, paint) : ""}${glyph.stroke ? line(glyph.stroke, paint, glyph.weight ?? NATIVE_ICON_LINE, glyph.dash ? ` stroke-dasharray="${glyph.dash}"` : "") : ""}`;
  }
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">${body}</svg>`;
}
