import { ArrowsOutSimple as PhosphorArrowsOutSimple, CalendarDots as PhosphorCalendarDots } from "@phosphor-icons/react";
import { useId } from "react";
import { ICON_SIZES, IconSvg, PhosphorIconSvg, smallMark, type LeafenIconProps } from "./iconPrimitives";

// A database view's layouts, as the view tabs and the layout menu show them.

// Notion's table and board (seen 2026-10-25 at 20px, in its view tabs, new
// view menu, layout tiles, search and slash menu alike): a wide rounded frame,
// three even rows and two even columns for the table, three even columns and
// nothing in them for the board.
export const TableIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="table">
    <rect x="4.4" y="5.8" width="11.2" height="8.4" rx="1.2" />
    <path d="M4.4 8.6h11.2" />
    <path d="M4.4 11.4h11.2" />
    <path d="M10 5.8v8.4" />
  </IconSvg>
);

export const BoardIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="board">
    <rect x="4.4" y="5.8" width="11.2" height="8.4" rx="1.2" />
    <path d="M8.13 5.8v8.4" />
    <path d="M11.87 5.8v8.4" />
  </IconSvg>
);

// Notion's views drawn here from its measures (seen 2026-11-03): the list's
// three lines with dots before them; the gallery's four boxes; the
// calendar's frame with a filled head and its days as dots.
export const ListIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="list">
    <path d="M7.7 6.4h7.68" />
    <path d="M7.7 10h7.68" />
    <path d="M7.7 13.6h7.68" />
    <circle cx="4.9" cy="6.4" r=".8" fill="currentColor" stroke="none" />
    <circle cx="4.9" cy="10" r=".8" fill="currentColor" stroke="none" />
    <circle cx="4.9" cy="13.6" r=".8" fill="currentColor" stroke="none" />
  </IconSvg>
);

export const CalendarViewIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="calendar-view">
    <rect x="5" y="5" width="10" height="10" rx="1.2" />
    <path d="M4.5 7.45V6.2a1.7 1.7 0 0 1 1.7-1.7h7.6a1.7 1.7 0 0 1 1.7 1.7v1.25z" fill="currentColor" stroke="none" />
    <g fill="currentColor" stroke="none">
      <circle cx="9.07" cy="9.07" r=".56" /><circle cx="10.93" cy="9.07" r=".56" /><circle cx="12.78" cy="9.07" r=".56" />
      <circle cx="7.22" cy="10.93" r=".56" /><circle cx="9.07" cy="10.93" r=".56" /><circle cx="10.93" cy="10.93" r=".56" /><circle cx="12.78" cy="10.93" r=".56" />
      <circle cx="7.22" cy="12.78" r=".56" /><circle cx="9.07" cy="12.78" r=".56" /><circle cx="10.93" cy="12.78" r=".56" />
    </g>
  </IconSvg>
);

// Notion's calendar shows its days as dots (seen 2026-10-07).
export const CalendarIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCalendarDots} />
);

// Notion's timeline (seen 2026-11-03, drawn here from its measures): the
// table's frame with three equal bars inside, each a step further right.
export const TimelineIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="timeline">
    <rect x="4.4" y="5.8" width="11.2" height="8.4" rx="1.2" />
    <path d="M6.4 8.02h4.68" />
    <path d="M7.66 10h4.68" />
    <path d="M8.74 11.98h4.68" />
  </IconSvg>
);

// Notion's chart view (seen 2026-11-03, drawn here from its measures): a
// ring cut from its middle up, 30.8° above the right and 34.2° below it — the
// same whatever kind of chart the view draws.
export const ChartViewIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="chart-view">
    <circle cx="10" cy="10" r="5.6" />
    <path d="M10 4.4V10l4.81-2.86M10 10l4.63 3.14" strokeLinecap="butt" />
  </IconSvg>
);

export const GalleryIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="gallery">
    <rect x="4.4" y="5.2" width="4.4" height="3.6" rx=".8" />
    <rect x="11.2" y="5.2" width="4.4" height="3.6" rx=".8" />
    <rect x="4.4" y="11.2" width="4.4" height="3.6" rx=".8" />
    <rect x="11.2" y="11.2" width="4.4" height="3.6" rx=".8" />
  </IconSvg>
);

/** An open eye, as a menu row shows it, drawn from where Notion inks its eye (seen 2026-11-15: 18.06 by 11.61 from
 * 0.97, 4.19 at 20px in the desktop's "속성 표시 여부", 19.9 by 12.8 from 1.1, 4.6 at 22px in the iPhone sheet's
 * "데이터 소스 제목 숨기기"): a lens of the usual line round a pupil drawn as a ring 0.11 of the box thick. The
 * lens's sides follow Notion's ink row by row (seen again 2026-11-15 at 20px: its outer edge 2.85, 1.95, 1.3 and
 * 0.95 from the left 3, 2, 1 and 0 above the middle), fuller than a flatter curve between the same corners. */
export const ViewEyeIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="view-eye">
    <ViewEyeShape />
  </IconSvg>
);

const ViewEyeShape = () => (
  <>
    <path d="M3.26 10C5.74 4.48 14.26 4.48 16.74 10 14.26 15.52 5.74 15.52 3.26 10Z" />
    <circle cx="10" cy="10" r="1.76" strokeWidth="1.76" />
  </>
);

/** The eye struck through, as a menu row shows it beside ViewEyeIcon (Notion's "숨기기", "그룹 숨기기"), drawn
 * from where Notion inks it (seen 2026-11-15 at 20px in a property's menu): the same lens and pupil, a line of
 * the same weight through the eye's middle from 4.25, 3.3 to 15.6, 16.65, and a clearance of 1.3 cut from the
 * lens and pupil along its upper right side only, the lower left side meeting the line. */
export const ViewEyeSlashIcon = (p: LeafenIconProps) => {
  const clearance = `leafen-eye-slash-${useId().replace(/[^A-Za-z0-9_-]/g, "")}`;
  return (
    <IconSvg {...p} glyph="view-eye-slash">
      <mask id={clearance} maskUnits="userSpaceOnUse" x="0" y="0" width="20" height="20">
        <rect x="0" y="0" width="20" height="20" fill="#fff" stroke="none" />
        <path d="M5.99 4.14 15.07 14.82" stroke="#000" strokeWidth="1.55" />
      </mask>
      <g mask={`url(#${clearance})`}>
        <ViewEyeShape />
      </g>
      <path d="M5.4 4.64 14.48 15.32" />
    </IconSvg>
  );
};

/**
 * Two arrows out along a diagonal (⤢): Notion's "open in full screen" on a PDF block and a database view's "전체
 * 페이지로 열기" (seen 2026-11-07). Below 18px Notion's small one (arrowExpandDiagonalBottomLeftToTopRightSmall, a
 * view toolbar's), drawn where Notion puts its ink (measured 2026-11-22 at 1440 by its painted area, two tabs): two
 * corners and their 45° shafts in 1.25 lines with round ends, 11.07 square of 16 from 2.46.
 */
export const ExpandDiagonalIcon = (p: LeafenIconProps) => smallMark(p.pixels ?? ICON_SIZES[p.size ?? "small"]) ? (
  <IconSvg {...p} glyph="expand-diagonal" line={1.25}>
    <path d="M10.92 5.08h4.01v4M14.93 5.08l-4.05 4.05M9.08 14.93H5.07v-4.01M5.07 14.93l4.06-4.05" />
  </IconSvg>
) : (
  <PhosphorIconSvg {...p} icon={PhosphorArrowsOutSimple} />
);
