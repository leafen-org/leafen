import {
  ArrowElbowDownLeft as PhosphorArrowElbowDownLeft,
  BracketsCurly as PhosphorBracketsCurly,
  TextB as PhosphorTextB,
  TextStrikethrough as PhosphorTextStrikethrough,
  TextUnderline as PhosphorTextUnderline,
  Columns as PhosphorColumns,
  ListBullets as PhosphorListBullets,
  Minus as PhosphorMinus,
  WarningCircle as PhosphorWarningCircle,
  ImageBroken as PhosphorImageBroken,
} from "@phosphor-icons/react";
import { IconSvg, iconPixels, PhosphorIconSvg, type LeafenIconProps } from "./iconPrimitives";

export * from "./blockTypeIcons";

// A text block's and the selection's marks — the block types, headings,
// lists, quote, code, the selection bar's formatting and equation, divider,
// columns and callout — as Notion draws them (moved out of index.tsx).

export const BulletedListIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorListBullets} />
);

// A quote's "인용 크기" row, as Notion's block menu marks it (seen
// 2026-11-11): a quotation mark with two short lines beside it, over two
// full lines.
export const QuoteSizeIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="quote-size">
    <path d="M13.6 4.8h3.4M13.6 8.2h3.4M3 11.7h14M3 15.2h14" />
    <circle cx="4.7" cy="7.9" r="1.4" fill="currentColor" stroke="none" />
    <circle cx="9.8" cy="7.9" r="1.4" fill="currentColor" stroke="none" />
    <path d="M3.3 7.6c0-1.7.9-2.7 2.3-2.9M8.4 7.6c0-1.7.9-2.7 2.3-2.9" />
  </IconSvg>
);

// Code, a "</>" — the code block's mark in the "/" and "전환" menus and the
// selection bar's code mark — drawn after where Notion's own puts its ink
// (read 2026-11-16 at 1440 in dark on its 20 box: chevrons at 45 degrees
// 1.95–18.05 across, a slash 3.7–16.25 down; overlap 0.92), not from its path.
export const CodeIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="code" strokeWidth="1.25">
    <path d="M5.85 6.1 1.95 10l3.9 3.9M14.15 6.1 18.05 10l-3.9 3.9M11.65 3.7 8.35 16.25" />
  </IconSvg>
);

// A code block's wrap, on its toolbar and in its menu: Notion's "↵" (seen
// 2026-11-06), not a table column's U-turn wrap.
export const CodeWrapIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowElbowDownLeft} />
);

// A code block's "언어" row, Notion's "{ }" (seen 2026-11-06).
export const BracesIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorBracketsCurly} />
);

// The selection bar's marks, drawn as Notion's are (seen 2026-10-31: 20px
// glyphs, not letters): bold, italic, underline, strikethrough, clearing the
// formatting and an inline equation.
export const BoldIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg weight="bold" {...p} icon={PhosphorTextB} />
);

// Italic: a slanted stem between two short bars, the top one reaching right
// and the bottom one left, drawn after where Notion's puts its ink (seen
// 2026-11-16: 5.75–14.2 across, 4.2–15.75 down on its 20 box; overlap 0.93).
export const ItalicIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="text-italic" strokeWidth="1.25">
    <path d="M9 4.825H13.575M11.2 4.825 8.7 15.125M6.375 15.125H11" />
  </IconSvg>
);

export const UnderlineIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorTextUnderline} />
);

export const StrikethroughIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorTextStrikethrough} />
);

// The selection toolbar's marks as Notion draws them (seen 2026-10-31, its
// "textX", "squareRoot" and "emojiFacePlus" read off at 20px): a T with a
// small x at its foot; a root sign over an x; a face open at its lower
// right, where a plus is.
export const ClearFormattingIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="clear-formatting">
    <path d="M5 5.88h7.2M8.6 5.88v8.24" />
    <path d="M11.96 10.04l3.36 3.36M15.32 10.04l-3.36 3.36" />
  </IconSvg>
);

/**
 * A disclosure's caret, Notion's one filled triangle with round corners
 * (arrowCaretDownFillSmall, seen 2026-11-14) turned to point right: a
 * toggle's, a library row's, a sub-item's and a group's alike.
 */
export const ToggleCaretIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="toggle-caret">
    <path d="M5.25 15.165a.8.8 0 0 0 1.203.69l8.854-5.164a.8.8 0 0 0 0-1.382L6.453 4.144a.8.8 0 0 0-1.203.691z" fill="currentColor" stroke="none" />
  </IconSvg>
);

/**
 * The sidebar's disclosure, Notion's chevron (arrowChevronSingleDownFillSmall,
 * seen 2026-11-14): a 1.5 line with round ends filled as one shape, 7.6 by
 * 4.38 of a 12px box, pointing down; turned a quarter to point right
 * (SidebarChevronRight), as a page row's, a teamspace's and a section's
 * name's are while folded. A phone filter sheet's condition takes it at 14px.
 * Trimmed, its box is its glyph's 10.15 of 16 across, as Notion draws it
 * beside a member's role (seen 2026-11-16).
 */
const SIDEBAR_CHEVRON = "M7.47 10.93a.75.75 0 0 0 1.06 0l4.32-4.32a.75.75 0 1 0-1.06-1.06L8 9.34 4.21 5.55a.75.75 0 0 0-1.06 1.06z";
export const SidebarChevronDown = (p: LeafenIconProps) => (
  <IconSvg glyphWidth={10.15} {...p} glyph="sidebar-chevron-down">
    <path d={SIDEBAR_CHEVRON} transform="translate(2 2)" fill="currentColor" stroke="none" />
  </IconSvg>
);
/**
 * Notion's thinner chevron (arrowChevronSingleDownSmall, seen 2026-11-16):
 * beside a member's count of teamspaces in settings, trimmed to its glyph's
 * 9.88 of 16 across, 14px tall.
 */
export const CountChevronDown = (p: LeafenIconProps) => (
  <IconSvg glyphWidth={9.88} {...p} glyph="count-chevron-down">
    <path d="m12.76 6.52-4.32 4.32a.62.62 0 0 1-.44.18.62.62 0 0 1-.44-.18L3.24 6.52a.63.63 0 0 1 0-.88c.24-.24.64-.24.88 0L8 9.52l3.88-3.88c.24-.24.64-.24.88 0s.24.64 0 .88"
      transform="translate(2 2)" fill="currentColor" stroke="none" />
  </IconSvg>
);
/**
 * The "⌄" of a blue split button ("새로 만들기 ⌄", the layout builder's
 * "모든 페이지에 적용 ⌄"), as Notion's (seen 2026-11-18 in desktop Chrome at
 * 1440 and a Galaxy A90's Chrome at 411): its filled chevron
 * (arrowChevronSingleDownFill, 10.04 by 5.72 at 16px) on its 20-unit view, a
 * phone's its thin one (`thin`, arrowChevronSingleDownSmall, 11.11 by 6.26 at
 * 18px) on its 16-unit view; either box 2px down in the button's line, so the
 * mark sits a pixel under the button's middle.
 */
const SPLIT_CHEVRON = "M9.381 13.619a.875.875 0 0 0 1.238 0l5.4-5.4A.875.875 0 1 0 14.78 6.98L10 11.763 5.219 6.98A.875.875 0 1 0 3.98 8.22z";
export const SplitChevronDown = ({ thin = false, style, ...p }: LeafenIconProps & { thin?: boolean }) => thin
  ? <CountChevronDown {...p} style={{ marginBlockStart: 2, ...style }} />
  : (
    <IconSvg viewBox="0 0 20 20" {...p} style={{ marginBlockStart: 2, ...style }} glyph="split-chevron-down">
      <path d={SPLIT_CHEVRON} fill="currentColor" stroke="none" />
    </IconSvg>
  );
/**
 * Notion's chevron at a badge's end, its 20-unit one drawn whole in the box
 * (arrowChevronSingleDown, seen 2026-11-18 beside a sidebar preset's "기본"):
 * its 16px box as wide as its ink's 12.06 of 20 units, 9.65px; the line 1.25
 * units with round ends, 4.6 to 15.4 across and down to 13.
 */
export const BadgeChevronDown = ({ size = "small", pixels, ...p }: LeafenIconProps) => {
  const drawn = pixels ?? iconPixels(size);
  return (
    <IconSvg {...p} size={size} pixels={pixels} glyph="badge-chevron-down" viewBox="3.97 0 12.06 20" width={(drawn * 12.06) / 20}
      strokeWidth={1.25}>
      <path d="M4.6 7.6 10 13l5.4-5.4" />
    </IconSvg>
  );
};

export const SidebarChevronRight = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="sidebar-chevron-right">
    <path d={SIDEBAR_CHEVRON} transform="translate(2 2) rotate(-90 8 8)" fill="currentColor" stroke="none" />
  </IconSvg>
);

export const InlineEquationIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="inline-equation">
    <path d="M3.28 11.6l1.6 2.72L8.72 5.4h8.04" />
    <path d="M10.36 7.96l5.12 6.68M15.52 8l-5.2 6.6" />
  </IconSvg>
);

export const AddReactionIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="add-reaction">
    <path d="M15.29 8.28A5.56 5.56 0 1 0 10.49 15.54" />
    <circle cx="7.84" cy="8.72" r="0.7" fill="currentColor" stroke="none" />
    <circle cx="12.16" cy="8.72" r="0.7" fill="currentColor" stroke="none" />
    <path d="M8.08 11.12Q10 14.24 11.92 11.12" />
    <path d="M14.8 12.84v3.92M12.84 14.8h3.92" />
  </IconSvg>
);

export const DividerIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorMinus} />
);

export const MinusIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorMinus} />
);

export const ColumnsIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorColumns} />
);

// An image that couldn't be loaded, as Notion's picture with a mark on it.
export const ImageBrokenIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorImageBroken} />
);

export const CalloutIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorWarningCircle} />
);

/** A warning's mark in a card that asks before something can't be undone. */
export const WarningCircleIcon = CalloutIcon;

/**
 * Notion's thin cross (xMark) that takes a chosen option out of a field: on
 * its 20-unit view two 1.25 lines with round ends from 4.75 to 15.25, so 9.4
 * of 16 across (seen 2026-11-22 at 1440 dark, dpr 2: a select cell's chip, a
 * quick filter's and an advanced filter rule's option chips alike, in the
 * secondary icon colour). Drawn here on the 16-unit view, a 1px line at 16.
 */
export const XMarkIcon = (p: LeafenIconProps) => (
  <IconSvg line={1} {...p} glyph="x-mark">
    <path d="M5.8 5.8 14.2 14.2M14.2 5.8 5.8 14.2" />
  </IconSvg>
);

/**
 * Notion's straight arrows (arrowStraightUpFillSmall, arrowStraightDownFillSmall) a single sort's pill leads with:
 * a 1.49 stem and a 45° head 7.58 across, 9.09 by 11.5 of 16, trimmed to 9.09 across (measured 2026-11-22 in
 * Chrome at 1440 by their painted area, two tabs).
 */
export const StraightArrowUp = (p: LeafenIconProps) => (
  <IconSvg glyphWidth={9.09} {...p} glyph="straight-arrow-up" line={1.49}>
    <path d="M10 5v10M6.21 8.79 10 5l3.79 3.79" />
  </IconSvg>
);
export const StraightArrowDown = (p: LeafenIconProps) => (
  <IconSvg glyphWidth={9.09} {...p} glyph="straight-arrow-down" line={1.49}>
    <path d="M10 15V5M6.21 11.21 10 15l3.79-3.79" />
  </IconSvg>
);
