import { IconSvg, type LeafenIconProps } from "./iconPrimitives";

// The small marks an automation's "속성 편집" card heads its row with, one for
// each kind of property it sets — Notion's own 16px glyphs there, not the
// property type marks (seen 2026-11-22 at 1440 in dark, dpr 1, in the cards'
// DOM, drawn 16 wide in a 20 high box): drawn here after where Notion's put
// their ink, their lines 1.25 of 16 (bulletedList and peopleFill on a 20 box
// drawn at 16), not from Notion's paths.

/** A relation's ↗, arrowDiagonalUpRightSmall: a corner 4.29 down and 11.71 across, and a line from it to 4.27, 11.73. */
export const ArrowDiagonalUpRightSmallIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 16 16" glyph="arrow-diagonal-up-right-small" line={1.25}>
    <path d="M5.6 4.29H11.71V10.4M11.4 4.6 4.27 11.73" />
  </IconSvg>
);

/** A title's "Aa", textFormatSmall: an A 1.88–8.4 across its feet, its bar at 9.38, beside an a's bowl and stem. */
export const TextFormatSmallIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 16 16" glyph="text-format-small" line={1.25}>
    <path d="M1.88 12.14 5.14 3.95 8.4 12.14M3.33 9.38H6.95M14.12 7.47V12.14" />
    <ellipse cx="11.86" cy="9.8" rx="2" ry="2.32" />
  </IconSvg>
);

/** A number's "#", numberSmall: two strokes leaning left 1.77 over 10, the upper bar further right than the lower. */
export const NumberSmallIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 16 16" glyph="number-small" line={1.25}>
    <path d="M6.99 2.97 5.22 13.03M10.74 2.97 8.97 13.03M3.9 6.32H12.5M3.3 9.68H12" />
  </IconSvg>
);

/** A select's arrowTriangleDownCircleSmall: a ring 5.5 round the middle and a filled triangle pointing down in it. */
export const TriangleDownCircleSmallIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 16 16" glyph="triangle-down-circle-small" line={1.25}>
    <circle cx="8" cy="8" r="5.5" />
    <path d="M6.45 6.55H9.55L8 9.85Z" fill="currentColor" strokeWidth="0.8" />
  </IconSvg>
);

/** A multi-select's bulletedList: three dots 1 round at 3.62 and three lines from 7.12 to 16.72, 4.5 apart, on a 20 box. */
export const BulletedListSmallIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="bulleted-list-small" strokeWidth="1.25">
    <path d="M7.12 5.5H16.72M7.12 10H16.72M7.12 14.5H16.72" />
    <g fill="currentColor" stroke="none">
      <circle cx="3.62" cy="5.5" r="1" /><circle cx="3.62" cy="10" r="1" /><circle cx="3.62" cy="14.5" r="1" />
    </g>
  </IconSvg>
);

/** A status's burstSmall: eight rays from 3.54 to 5.5 out of the middle. */
export const BurstSmallIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 16 16" glyph="burst-small" line={1.25}>
    <path d="M8 2.5V4.46M8 11.54V13.5M2.5 8H4.46M11.54 8H13.5M4.11 4.11 5.5 5.5M10.5 10.5 11.89 11.89M4.11 11.89 5.5 10.5M10.5 5.5 11.89 4.11" />
  </IconSvg>
);

/** A date's viewCalendarSmall: a ring 3–13 each way with 1.25 corners, its top band filled to 5.63, and ten days as dots. */
export const ViewCalendarSmallIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 16 16" glyph="view-calendar-small" line={1.25}>
    <rect x="3" y="3" width="10" height="10" rx="1.25" />
    <g fill="currentColor" stroke="none">
      <path d="M2.375 5.632V4.25A1.875 1.875 0 0 1 4.25 2.375h7.5a1.875 1.875 0 0 1 1.875 1.875v1.382Z" />
      {[[7.053, 7.05], [8.947, 7.05], [10.84, 7.05], [5.159, 8.94], [7.053, 8.94], [8.947, 8.94], [10.84, 8.94], [5.159, 10.84], [7.053, 10.84], [8.947, 10.84]]
        .map(([cx, cy]) => <circle key={`${cx},${cy}`} cx={cx} cy={cy} r="0.643" />)}
    </g>
  </IconSvg>
);

/** People, peopleFill: two filled heads and two filled shoulders, the one in front on the right, on a 20 box. */
export const PeopleFillSmallIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="people-fill-small">
    <g fill="currentColor" stroke="none">
      <ellipse cx="5.98" cy="7.33" rx="2.78" ry="3.13" />
      <ellipse cx="13.63" cy="6.86" rx="3.01" ry="3.41" />
      <path d="M1.55 16.55V15.6C1.55 13.25 3.53 11.4 5.98 11.4c.96 0 1.86.28 2.6.75-.7.95-1.06 2.06-.95 3.1.06.48.22.9.44 1.3Z" />
      <path d="M8.9 16.57V15.2c0-2.3 2.12-4.17 4.73-4.17s4.73 1.87 4.73 4.17v1.37Z" />
    </g>
  </IconSvg>
);

/** A URL's linkSmall: two open links bent round 2.52 at the ends of the rising diagonal, and the bar between them. */
export const LinkSmallIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="link-small" strokeWidth="1.5625">
    <path d="M9.2 5.91 11.05 4.05A3.15 3.15 0 0 1 15.51 4.05L15.88 4.42A3.15 3.15 0 0 1 15.88 8.88L14.02 10.73" />
    <path d="M10.73 14.02 8.88 15.88A3.15 3.15 0 0 1 4.42 15.88L4.05 15.51A3.15 3.15 0 0 1 4.05 11.05L5.91 9.2" />
    <path d="M12.54 7.39 7.09 12.84" />
  </IconSvg>
);

/** An email's atSmall: a ring 5.5 round the middle open at its lower right into a tail and stem, round an a's ring 2.36. */
export const AtSmallIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 16 16" glyph="at-small" line={1.25}>
    <path d="M10.03 13.11A5.5 5.5 0 1 1 13.4 9.2c-.15.6-.7.9-1.3.9-.7 0-1.25-.55-1.25-1.25V5.7" />
    <circle cx="7.87" cy="8.1" r="2.36" />
  </IconSvg>
);
