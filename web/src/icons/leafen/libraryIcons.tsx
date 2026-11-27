import {
  Network as PhosphorNetwork,
  SquaresFour as PhosphorSquaresFour,
} from "@phosphor-icons/react";
import { IconSvg, PhosphorIconSvg, type LeafenIconProps } from "./iconPrimitives";

// The library's column heads, as Notion draws them (seen 2026-10-07), filled
// and gray: a teamspace's name a grid of four, its description three bars,
// its access an org chart; a page's source an arrow pointing up and right.

export const GridIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg weight="fill" {...p} icon={PhosphorSquaresFour} glyph="grid" />
);

export const NetworkIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg weight="fill" {...p} icon={PhosphorNetwork} />
);

export const DescriptionIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="description">
    <path d="M3.4 4.6h13.2v1.6H3.4Z" fill="currentColor" />
    <path d="M3.4 9.2h13.2v1.6H3.4Z" fill="currentColor" />
    <path d="M3.4 13.8h7.2v1.6H3.4Z" fill="currentColor" />
  </IconSvg>
);

export const LocationArrowIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="location-arrow">
    <path d="M16.4 3.6 12.4 15.8 10 10 4.2 7.6Z" fill="currentColor" />
  </IconSvg>
);

// "… 상단으로 이동" (seen 2026-10-07): a pointer at the top of a list, its
// first line heavier, filled as Notion's (seen 2026-11-16 at 1440: the
// pointer and the 1.6-high first bar level at the top, three 1-high bars a
// third of the box apart under them, all filled and round-ended). It is
// 16.5px across in its box of 20 (glyphWidth 13.2 of 16): a menu row draws
// the full box, a bar's button only the glyph (`trim`), as Notion's.
export const MoveToTopIcon = (p: LeafenIconProps) => (
  <IconSvg glyphWidth={13.2} {...p} glyph="move-to-top">
    <path d="M3.4 4.65v2.7a.45.45 0 0 0 .68.39l2.3-1.35a.45.45 0 0 0 0-.78l-2.3-1.35a.45.45 0 0 0-.68.39Z" fill="currentColor" stroke="none" />
    <path d="M8.735 5.2h7.36a.5.5 0 0 1 .5.5v.6a.5.5 0 0 1-.5.5h-7.36a.5.5 0 0 1-.5-.5v-.6a.5.5 0 0 1 .5-.5Z" fill="currentColor" stroke="none" />
    <path d="M8.735 8.46h7.36a.5.5 0 0 1 0 1h-7.36a.5.5 0 0 1 0-1Zm0 2.67h7.36a.5.5 0 0 1 0 1h-7.36a.5.5 0 0 1 0-1Zm.08 2.67h7.2a.5.5 0 0 1 0 1h-7.2a.5.5 0 0 1 0-1Z"
      fill="currentColor" stroke="none" />
  </IconSvg>
);

// "즐겨찾기에서 제거" on the library's chosen favorites' bar (Notion's, seen
// 2026-11-16): a star's outline struck through from its top left.
export const StarSlashIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="star-slash">
    <path d="m10 3 2.1 4.3 4.7.7-3.4 3.3.8 4.7L10 13.8 5.8 16l.8-4.7-3.4-3.3 4.7-.7Z" strokeLinejoin="round" />
    <path d="m3.4 3.4 13.2 13.2" />
  </IconSvg>
);

// "내 작업"'s empty page: a checked task under two lines, as a pile of them (Notion's, seen 2026-11-08).
export const TaskPileIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="task-pile">
    <rect x="3.6" y="7.4" width="12.8" height="9.6" rx="1.6" />
    <path d="M5.4 5.4h9.2" />
    <path d="M7.2 3.4h5.6" />
    <path d="m7.6 12.2 1.8 1.8 3.4-3.6" />
  </IconSvg>
);
