import { Command as PhosphorCommand } from "@phosphor-icons/react";
import { IconSvg, PhosphorIconSvg, type LeafenIconProps } from "./iconPrimitives";
import { SplitChevronDown } from "./textMarkIcons";

// The keyboard shortcuts window's tab marks that no other screen draws, as
// Notion's (seen 2026-11-19): "편집 및 이동" a copy's squares with the one
// behind dashed, "명령어" a ⌘ key's loops, "슬래시 명령어" a slash.

/**
 * A copy's two squares, the one behind dashed, as Notion's squareOnSquareDashed
 * (read 2026-11-21 at 1440): the front square as the shared Copy's, the back
 * one 3–13.5 with its corners 1.26 round, drawn as its three corners and one
 * dash in the middle of its top and of its start side, each corner's arms
 * running on to 5.09 from its edge and each dash 7.42–9.08, round-ended.
 */
export const SquareOnSquareDashedIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="square-on-square-dashed" strokeWidth="1.25">
    <path d="M3 5.091V4.26A1.26 1.26 0 0 1 4.26 3h.831M7.419 3h1.661M11.409 3h.831a1.26 1.26 0 0 1 1.26 1.26v.831M3 7.419v1.661M3 11.409v.831a1.26 1.26 0 0 0 1.26 1.26h.831" />
    <rect x="6.75" y="6.75" width="10.5" height="10.5" rx="1.5" />
  </IconSvg>
);

export const CommandKeyIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCommand} />
);

/**
 * A slash leaning to the end, as the "/" a command starts with: as steep as
 * Notion's, its ink 4.48 across and 14.27 down the middle of the 20 box
 * (seen 2026-11-21 at 1440).
 */
export const SlashGlyphIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="slash" strokeWidth="1.25">
    <path d="M11.615 3.5 8.385 16.5" />
  </IconSvg>
);

/**
 * The chevron after the name the window's "더 보기…" takes for the lit
 * section in its menu, as Notion's (arrowChevronSingleDownFill, seen
 * 2026-11-21 at 1440 and 800): its 20-unit filled chevron in a box 20 high
 * and as wide as its ink, 12.56 from 3.72 across.
 */
export const MoreChevronDown = ({ style, ...p }: LeafenIconProps) => (
  <SplitChevronDown size="regular" viewBox="3.72 0 12.56 20" width={12.56} {...p} style={{ marginBlockStart: 0, ...style }} />
);
