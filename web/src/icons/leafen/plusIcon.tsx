import { ICON_SIZES, IconSvg, smallMark, type LeafenIconProps } from "./iconPrimitives";

// The "+": two round-ended strokes crossing in the middle, drawn where
// Notion's two marks put their ink (measured 2026-11-18 in Chrome at 1440,
// two tabs, by the painted area of each mark, not from Notion's paths). Its
// 20px one (plus: a block's handle, a table's or a list's row, a section's
// add) is 12.82 of 20 across, its lines 1.32px; its small one (plusSmall, a
// 16 box: "새 페이지", "속성 추가") the same shape 10.52 of 16 across, its lines
// also 1.32px. Both grow and shrink with the size they are drawn at. On the
// 20-unit grid IconSvg draws on (its view 2–18), both arms run 5.4–14.6.
const REGULAR_LINE = (1.32 * 16) / 20;
const SMALL_LINE = 1.32;

/** The "+": Notion's small mark below 18px, its 20px one from there. */
export const Plus = (p: LeafenIconProps) => (
  <IconSvg glyphWidth={10.52} {...p} glyph="plus"
    line={smallMark(p.pixels ?? ICON_SIZES[p.size ?? "small"]) ? SMALL_LINE : REGULAR_LINE}>
    <path d="M10 5.4v9.2M5.4 10h9.2" />
  </IconSvg>
);
