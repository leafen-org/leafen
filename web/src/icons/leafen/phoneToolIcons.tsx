import { IconSvg, type LeafenIconProps } from "./iconPrimitives";

// The marks only a phone's caret tools draw, as Notion's there (seen
// 2026-11-13 in iPhone Safari at 402, measured at 3x).

/** Putting the soft keyboard away: a key board of dots, three rows, the last
 * a space bar between two dots. */
export const KeyboardIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="keyboard">
    <rect x="3.07" y="5.47" width="13.87" height="9.07" rx="1.07" />
    <path d="M5.47 7.6h0M7.73 7.6h0M10 7.6h0M12.27 7.6h0M14.53 7.6h0M5.47 10h0M7.73 10h0M10 10h0M12.27 10h0M14.53 10h0M5.47 12.4h0M14.53 12.4h0" />
    <path d="M7.73 12.4h4.54" />
  </IconSvg>
);

/** The tools' "+": 14px across in its 20 box, larger than a menu row's. */
export const ToolPlusIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="plus-tool">
    <path d="M10 4.92v10.16M4.92 10h10.16" />
  </IconSvg>
);

/** Outdent, "|←": an arrow back to a bar at its start, 17pt across. */
export const ArrowLeftToLine = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="arrow-left-to-line">
    <path d="M3.87 4.92v10.16" />
    <path d="M9.87 6.25 6.12 10l3.75 3.75" />
    <path d="M6.12 10h10.16" />
  </IconSvg>
);

/** Indent, "→|": the outdent's mirror. */
export const ArrowRightToLine = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="arrow-right-to-line">
    <path d="M16.13 4.92v10.16" />
    <path d="M10.13 6.25 13.88 10l-3.75 3.75" />
    <path d="M13.88 10H3.72" />
  </IconSvg>
);

/** Moving the line up: an arrow up standing on a bar. */
export const MoveUpIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="move-up">
    <path d="M5.92 7.92 10 3.84l4.08 4.08" />
    <path d="M10 3.84v9.1" />
    <path d="M4.92 16.14h10.16" />
  </IconSvg>
);

/** Moving the line down: an arrow down from a bar over it. */
export const MoveDownIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="move-down">
    <path d="M5.92 12.08 10 16.16l4.08-4.08" />
    <path d="M10 16.16v-9.1" />
    <path d="M4.92 3.86h10.16" />
  </IconSvg>
);

/** A sheet row's "›", drawn from where Notion's phone sheets ink it at 18px (seen 2026-11-15 in iPhone Safari at
 * 402: 6.3 by 11.1 from 6.4, 3.4), its 1.3px line round at its ends and point. */
export const SheetChevronRight = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="sheet-chevron-right">
    <path d="M8.27 5.6 12.71 9.96 8.27 14.31" />
  </IconSvg>
);

/** The media sheet's "사진 촬영하기": a camera, its lens round in its body under a raised top. */
export const CameraIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="camera">
    <path d="M3.6 7.47c0-.66.54-1.2 1.2-1.2h2.13l1.2-1.6h3.74l1.2 1.6h2.13c.66 0 1.2.54 1.2 1.2v6.66c0 .66-.54 1.2-1.2 1.2H4.8c-.66 0-1.2-.54-1.2-1.2Z" />
    <circle cx="10" cy="10.67" r="2.53" />
  </IconSvg>
);
