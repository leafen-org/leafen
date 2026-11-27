import { MagnifyingGlass as PhosphorMagnifyingGlass } from "@phosphor-icons/react";
import { ICON_SIZES, outlinedFrame, PhosphorIconSvg, smallMark, type GlyphFrame, type LeafenIconProps } from "./iconPrimitives";

/** Phosphor's magnifying glass as it fills a 20 box: its path's extent, 23.779 of 256 in and 208.221 across. */
const PHOSPHOR_INK = [1.858, 1.858, 16.267, 16.267] as const;

/**
 * Notion's two magnifiers, each where it paints its ink on a 20 box (seen
 * 2026-11-21 at 1440 in Chrome and at 411 in Android Chrome, its own glyph
 * names on its icons): magnifyingGlass on its 20 grid, 2.626 in and 15.255
 * across (its circle's centre 8.875 in), drawn from 20px up — a settings
 * table's search 20, the shortcuts window's 20, ⌘K's 22, a phone database
 * toolbar's 20, a phone sidebar's 26 — and magnifyingGlassSmall on its 16
 * grid, 1.975 of 16 in and 12.454 across, drawn below it — a database
 * toolbar's, the move-to dialog's, the emoji picker's, the page menu's and
 * the settings search's 16, a property type list's head 14, a layout's add
 * menu 12, and a phone's share field 18 and its move-to sheet 16.
 */
const MAGNIFIER: GlyphFrame = { drawn: PHOSPHOR_INK, notion: [2.626, 2.625, 15.255, 15.255] };
const MAGNIFIER_SMALL: GlyphFrame = { drawn: PHOSPHOR_INK, notion: [2.469, 2.469, 15.568, 15.568] };
const MAGNIFIER_SMALL_OUTLINED = outlinedFrame(MAGNIFIER_SMALL);

/** Notion's magnifier for an icon this many px across: its small one below 20, the outline it is drawn with below 18 taken in. */
function magnifierFrame(pixels: number): GlyphFrame {
  if (pixels >= 20) return MAGNIFIER;
  return smallMark(pixels) ? MAGNIFIER_SMALL_OUTLINED : MAGNIFIER_SMALL;
}

/**
 * The magnifier every search draws, Notion's at its size wherever it shows
 * (magnifierFrame) — a screen names a size and nothing more; `trim` keeps a
 * box as wide as its ink, as Notion's settings search.
 */
export const Search = ({ frame, ...p }: LeafenIconProps) => (
  <PhosphorIconSvg {...p} frame={frame ?? magnifierFrame(p.pixels ?? ICON_SIZES[p.size ?? "small"])} icon={PhosphorMagnifyingGlass} />
);
