import {
  ChartBar as PhosphorChartBar,
  User as PhosphorUser,
  UserPlus as PhosphorUserPlus,
  UserCirclePlus as PhosphorUserCirclePlus,
  Spinner as PhosphorSpinner,
  Sigma as PhosphorSigma,
} from "@phosphor-icons/react";
import { ICON_SIZES, IconSvg, PhosphorIconSvg, smallMark, type LeafenIconProps } from "./iconPrimitives";

// A property's and a database's marks — number, checkbox, person, phone, a relation's limit,
// formula, database, chart, layout, properties, filter, sort, select
// and status — as Notion draws them (moved out of index.tsx).

export const HashIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="hash">
    <path d="M8 4.1 7 15.9" />
    <path d="M13 4.1 12 15.9" />
    <path d="M4.8 7.4h11" />
    <path d="M4.2 12.6h11" />
  </IconSvg>
);

export const CheckboxIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="checkbox">
    <rect x="4.2" y="4.2" width="11.6" height="11.6" rx="1.8" />
    <path d="m7 10.2 2.2 2.1 4-4.5" />
  </IconSvg>
);

// A person property's mark: two people side by side, drawn solid, the one
// behind cut a step clear of the other's shoulders, as Notion's is (seen 2026-11-15:
// 17.5 by 11.25 on its 20 box from 1.25, 3.75 — here on the 16 live area).
export const PeoplePropertyIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="people-property">
    <g fill="currentColor" stroke="none">
      <circle cx="7" cy="7" r="2" />
      <circle cx="13" cy="7" r="2" />
      <path d="M3 14a4 4 0 0 1 8 0Z" />
      <path d="M11.8 14A4.8 4.8 0 0 0 10.59 10.81A4 4 0 0 1 17 14Z" />
    </g>
  </IconSvg>
);

export const UserIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorUser} />
);

export const UserPlusIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorUserPlus} />
);

export const UserCirclePlusIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorUserCirclePlus} />
);

export const PhoneIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="phone">
    <path d="m6.7 4 1.6 3-1.2 1.2a9.5 9.5 0 0 0 4.6 4.6l1.2-1.2 3 1.7-1 2.4c-.2.6-.8 1-1.4.9A11.8 11.8 0 0 1 3.4 6.6c-.1-.7.3-1.3.9-1.6Z" />
  </IconSvg>
);

// A date property: a plain calendar whose top band is filled, two tabs over
// it and its days left empty, as Notion's type icon in the column head, the
// property lists and the type list (seen 2026-11-11, drawn here from its
// measures on its 20 grid: 15 across and 14.4 high, sides 1.9 and foot 1.6
// thick, the band 5 deep, the tabs 1.9 wide). The dotted calendar is the
// "날짜 형식" row's (CalendarViewIcon).
export const DatePropertyIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="date-property">
    <path d="M4 5h2V3.5h1.5V5h5V3.5H14V5h2v10H4Zm1.5 4v4.75h9V9Z" fill="currentColor" fillRule="evenodd" stroke="none" />
  </IconSvg>
);

// "123", as Notion marks a relation's "제한" (how many pages it holds, seen
// 2026-11-08), a count in a chart's menus and a number chart's "계산" (its
// numbers123, seen 2026-11-18 at 1440: inked 19.24 across and 8.97 high from
// 0.38 and 5.52 on its 20 box) — a one with its flag, a two and a three side
// by side, drawn here with 1.25 lines over where Notion's ink lies (overlap
// 0.97 at 400px), not from its path.
export const NumbersIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="numbers-123" strokeWidth="1.25">
    <path d="M1 7.35 3.13 6.15v7.7" />
    <path d="M6.5 7.6c.35-.9 1.25-1.45 2.35-1.45 1.35 0 2.38.95 2.38 2.15 0 .7-.35 1.2-.93 1.7l-4.08 3.85h5.2" />
    <path d="M14.1 7.35c.4-.75 1.2-1.2 2.25-1.2 1.4 0 2.48.75 2.48 1.85s-1 1.9-2.45 1.9h-.6" />
    <path d="M15.78 9.9h.75c1.45 0 2.45.8 2.45 1.95s-1.1 2-2.6 2c-1.1 0-1.95-.5-2.4-1.35" />
  </IconSvg>
);

// A chart's "소수점 이하 자리": ".00", as Notion marks that row (seen 2026-11-12) — a point and two zeros.
export const DecimalsIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="decimals-00">
    <circle cx="2.9" cy="13.4" r="0.9" fill="currentColor" stroke="none" />
    <rect x="5.4" y="6.2" width="5" height="7.8" rx="2.5" />
    <rect x="12.6" y="6.2" width="5" height="7.8" rx="2.5" />
  </IconSvg>
);

// Notion's Σ: a formula property and a column's calculation.
export const FormulaIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorSigma} />
);

export const ChartIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorChartBar} />
);

export const LayoutIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="layout">
    <rect x="3.8" y="4" width="12.4" height="11.6" rx="1.6" />
    <path d="M3.8 7.9h12.4" />
    <path d="M7.9 7.9v7.7" />
  </IconSvg>
);

/**
 * A database page's layout, as Notion's "레이아웃 사용자 지정" (seen
 * 2026-11-16 in its row page "…", view settings, property menu and phone
 * "작업" sheet): a tall panel beside two stacked boxes, drawn on Leafen's grid.
 * Filled, as the page head's button draws it (14px), it fills its box.
 */
export const PageLayoutIcon = ({ weight, ...p }: LeafenIconProps) => weight === "fill" ? (
  <IconSvg {...p} glyph="page-layout-fill" stroke="none" fill="currentColor">
    <rect x="2.2" y="2.2" width="7.3" height="15.6" rx="1.9" />
    <rect x="10.7" y="2.2" width="7.1" height="7.2" rx="1.9" />
    <rect x="10.7" y="10.6" width="7.1" height="7.2" rx="1.9" />
  </IconSvg>
) : (
  <IconSvg {...p} glyph="page-layout">
    <rect x="5.2" y="5.2" width="3.6" height="9.6" rx="1" />
    <rect x="11" y="5.2" width="3.8" height="3.8" rx="1" />
    <rect x="11" y="11" width="3.8" height="3.8" rx="1" />
  </IconSvg>
);

export const PropertiesIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="properties">
    <path d="M4.8 5.4h10.4" />
    <path d="M4.8 10h10.4" />
    <path d="M4.8 14.6h10.4" />
    <circle cx="7.1" cy="5.4" r="1.15" fill="currentColor" stroke="none" />
    <circle cx="12" cy="10" r="1.15" fill="currentColor" stroke="none" />
    <circle cx="9.3" cy="14.6" r="1.15" fill="currentColor" stroke="none" />
  </IconSvg>
);

// Notion's filter: three lines, each shorter than the one above. Below 18px its small one (filterSmall), drawn where
// Notion puts its ink (measured 2026-11-22 in Chrome at 1440 by its painted area, two tabs): 1.39 lines 3.6 apart,
// 12.58, 9.19 and 5.78 across, 12.6 by 8.6 of 16 in all.
export const FilterIcon = (p: LeafenIconProps) => smallMark(p.pixels ?? ICON_SIZES[p.size ?? "small"]) ? (
  <IconSvg glyphWidth={12.61} {...p} glyph="filter" line={1.39}>
    <path d="M4.405 6.405h11.19M6.105 10h7.8M7.805 13.595h4.39" />
  </IconSvg>
) : (
  <IconSvg {...p} glyph="filter" glyphWidth={12.61}>
    <path d="M3.8 6h12.4" />
    <path d="M6 10h8" />
    <path d="M8.2 14h3.6" />
  </IconSvg>
);

// Notion's sort: an arrow down and one up side by side. Below 18px its small one (arrowUpDownSmall), drawn where
// Notion puts its ink (measured as the filter's): 1.24 lines 5.8 apart, heads 5.6 across, 12.66 by 11.02 of 16.
export const SortIcon = (p: LeafenIconProps) => smallMark(p.pixels ?? ICON_SIZES[p.size ?? "small"]) ? (
  <IconSvg glyphWidth={12.67} {...p} glyph="sort" line={1.24}>
    <path d="M7.1 5.11v9.78m-2.8-2.81 2.8 2.81 2.8-2.81M12.9 14.89V5.11m-2.8 2.81 2.8-2.81 2.8 2.81" />
  </IconSvg>
) : (
  <IconSvg {...p} glyph="sort">
    <path d="M6.6 4.4v10.2" />
    <path d="m4.4 12.4 2.2 2.2 2.2-2.2" />
    <path d="M13.4 15.6V5.4" />
    <path d="m11.2 7.6 2.2-2.2 2.2 2.2" />
  </IconSvg>
);

// Notion's select, drawn here on its 20 box (a unit 0.8 of the view's 2–18):
// its type's mark a heavy ring round a small filled triangle pointing down —
// the ring 2.5–17.5 across and 1.875 thick, the triangle 7–13 across, 7.9 to
// 13.1 down (seen 2026-11-16 in a column's head and the type list) — and a
// select chosen among others ("다음과 같이 표시") a lighter, wider ring —
// 2.375–17.625 and 1.25 thick round the same triangle, its corners rounder.
// Their lines grow with the size they are drawn at, as Notion's.
export const SelectIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="select">
    <circle cx="10" cy="10" r="5.25" strokeWidth={1.5} />
    <path d="M7.88 8.6h4.24L10 12.2Z" fill="currentColor" strokeWidth={0.56} />
  </IconSvg>
);

export const SelectChoiceIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="select-choice">
    <circle cx="10" cy="10" r="5.6" strokeWidth={1} />
    <path d="M8.08 8.76h3.84L10 12.04Z" fill="currentColor" strokeWidth={0.96} />
  </IconSvg>
);

// Notion's status: a dashed circle.
/** A status property: short rays bursting from the middle, as Notion's (seen 2026-11-07). */
export const StatusIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorSpinner} />
);

/**
 * A rollup's "대상 속성" row: a path turning twice from a dot to a ring, as
 * Notion's (pathRoundEnds, seen 2026-11-19 in Chrome 1440 from a column's
 * "속성 편집" and from the view's: on its 20 box a 1.25 line from a 2.13 dot
 * at 5.25, 14.75 to a ring of the same size at 14.75, 5.25, its turns 2.375
 * round), drawn on its 20 box as the select's is.
 */
export const PathRoundEndsIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="path-round-ends">
    <path d="M12.6 6.2H7.05a1.9 1.9 0 0 0 0 3.8h6.05a1.9 1.9 0 0 1 0 3.8H7.4" strokeWidth={1} />
    <circle cx="13.8" cy="6.2" r="1.2" strokeWidth={1} />
    <circle cx="6.2" cy="13.8" r="1.2" fill="currentColor" strokeWidth={1} />
  </IconSvg>
);
