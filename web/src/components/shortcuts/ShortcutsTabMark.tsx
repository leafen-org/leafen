import type { ComponentType } from "react";
import { CodeIcon, PencilSmallIcon, Star, type LeafenIconProps } from "@/icons/leafen";
import { CommandKeyIcon, SlashGlyphIcon, SquareOnSquareDashedIcon } from "@/icons/leafen/shortcutsIcons";
import type { ShortcutsHelpSectionId } from "@/lib/keyboardShortcutsHelp";

/** A mark's ink, [x, y, width, height] on its 20px box. */
type Ink = readonly [number, number, number, number];

/**
 * Each section's mark: ours drawn at 20px and where its ink falls, and where
 * Notion's falls (read 2026-11-21 in Chrome at 1440, dpr 2: its tab marks'
 * viewBoxes and painted boxes, the menu's the same glyphs whole).
 */
const MARKS: Record<ShortcutsHelpSectionId, { Mark: ComponentType<LeafenIconProps>; drawn: Ink; notion: Ink }> = {
  popular: { Mark: Star, drawn: [1.251, 1.25, 17.499, 16.875], notion: [2.4, 2.4, 15.2, 14.52] },
  createStyle: { Mark: PencilSmallIcon, drawn: [3.988, 4.438, 11.575, 11.8], notion: [4.19, 4.19, 11.62, 11.62] },
  editMove: { Mark: SquareOnSquareDashedIcon, drawn: [2.375, 2.375, 15.5, 15.5], notion: [2.37, 2.38, 15.51, 15.5] },
  markdown: { Mark: CodeIcon, drawn: [1.325, 3.075, 17.35, 13.8], notion: [1.27, 3.08, 17.46, 13.85] },
  commands: { Mark: CommandKeyIcon, drawn: [3.125, 3.125, 13.75, 13.75], notion: [3.12, 3.12, 13.75, 13.75] },
  slash: { Mark: SlashGlyphIcon, drawn: [7.76, 2.875, 4.48, 14.25], notion: [7.76, 2.87, 4.48, 14.27] },
};

export const SHORTCUTS_SECTIONS = Object.keys(MARKS) as ShortcutsHelpSectionId[];

/**
 * A section's mark where Notion puts its own: our glyph moved and scaled so
 * its ink fills Notion's. In a tab the box is 20 high and as wide as the ink,
 * as Notion fits each tab's mark to its glyph ("/" 4.48 across, the star
 * 15.2); in the "더 보기…" menu it is the whole 20px box.
 */
export function ShortcutsTabMark({ section, fitted = false, className }: { section: ShortcutsHelpSectionId; fitted?: boolean; className?: string }) {
  const { Mark, drawn: [dx, dy, dw, dh], notion: [nx, ny, nw, nh] } = MARKS[section];
  const across = dw / nw, down = dh / nh;
  const left = fitted ? dx : dx - nx * across, wide = fitted ? dw : 20 * across;
  return <svg className={className} width={fitted ? nw : 20} height={20} viewBox={`${left} ${dy - ny * down} ${wide} ${20 * down}`}
    preserveAspectRatio="none" overflow="visible" aria-hidden="true" data-shortcuts-mark={section}>
    <Mark size="regular" />
  </svg>;
}
