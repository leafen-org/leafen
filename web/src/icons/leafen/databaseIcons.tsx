import {
  Sparkle as PhosphorSparkle,
  TextAa as PhosphorTextAa,
  TextAlignLeft as PhosphorTextAlignLeft,
  CheckSquare as PhosphorCheckSquare,
  At as PhosphorAt,
  ArrowUpRight as PhosphorArrowUpRight,
  UserCircleDashed as PhosphorUserCircleDashed,
  FileLock as PhosphorFileLock,
  SealCheck as PhosphorSealCheck,
  EyeClosed as PhosphorEyeClosed,
  ArrowDownLeft as PhosphorArrowDownLeft,
  ArrowUDownLeft as PhosphorArrowUDownLeft,
  ArrowLineRight as PhosphorArrowLineRight,
  Lightning as PhosphorLightning,
  ArrowElbowDownRight as PhosphorArrowElbowDownRight,
  ArrowElbowLeftUp as PhosphorArrowElbowLeftUp,
  FolderSimple as PhosphorFolderSimple,
  CircleDashed as PhosphorCircleDashed,
  PlayCircle as PhosphorPlayCircle,
  CheckCircle as PhosphorCheckCircle,
  ArrowsVertical as PhosphorArrowsVertical,
  Files as PhosphorFiles,
} from "@phosphor-icons/react";
import { ICON_SIZES, IconSvg, PhosphorIconSvg, smallMark, type LeafenIconProps } from "./iconPrimitives";

// Marks drawn after where Notion puts its own ink, kept apart (notionMarkIcons).
export * from "./notionMarkIcons";

// A database's property kinds and column actions, as Notion draws them (seen 2026-10-07).
export const TitlePropertyIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorTextAa} />
);

export const TextPropertyIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorTextAlignLeft} />
);

export const CheckboxPropertyIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCheckSquare} weight="fill" />
);

export const AtIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorAt} />
);

export const RelationIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowUpRight} glyphWidth={8.68} />
);

// A person in a ring, as Notion's personCircle (read 2026-11-27 in its "/"
// menu's "사용자 멘션하기" at 20px): a ring 15.3 across on the 20-unit box,
// a head and the shoulders' curve inside it, Leafen's own 1.25 lines.
export const PersonCircleIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="person-circle" line={1.25}>
    <circle cx="10.02" cy="10" r="7.02" />
    <ellipse cx="10" cy="8.45" rx="2.35" ry="2.65" />
    <path d="M6.45 15.25c.7-1.3 2-2.05 3.55-2.05s2.85.75 3.55 2.05" />
  </IconSvg>
);

/**
 * A person in a ring with the person filled, as Notion's mark for a group
 * without an icon in its window's head (seen 2026-11-15): a ring 1.25 of its
 * 20, and inside it a filled head and shoulders that meet the ring.
 */
export const PersonCircleFillIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="person-circle-fill">
    <circle cx="10" cy="10" r="7" fill="none" stroke="currentColor" strokeWidth="1.25" />
    <g fill="currentColor" stroke="none">
      <ellipse cx="10" cy="8.52" rx="3" ry="3.39" />
      <path d="M5.91 14.89C6.7 13.5 8.25 12.56 10 12.56C11.75 12.56 13.3 13.5 14.09 14.89A6.38 6.38 0 0 1 5.91 14.89Z" />
    </g>
  </IconSvg>
);

/**
 * People, as Notion's mark for its settings' 사람 and for a group (its groups
 * tab, group type filter and new group window, seen 2026-11-13): two people
 * side by side, apart — a smaller one on the left, its shoulders stopping
 * short of the other's — not one standing behind the other. Its
 * lines are 1.25 of its 20, so they grow with it as Notion's do; "fill" is
 * the same two people filled, as a group's row shows it.
 */
export const GroupPeopleIcon = ({ weight, ...p }: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph={weight === "fill" ? "group-people-fill" : "group-people"} strokeWidth="1.25">
    {weight === "fill" ? (
      <g fill="currentColor" stroke="none">
        <ellipse cx="5.98" cy="7.33" rx="2.77" ry="3.13" />
        <ellipse cx="13.63" cy="6.86" rx="3.01" ry="3.41" />
        <path d="M5.98 11.4C6.94 11.4 7.85 11.68 8.59 12.15C7.95 12.95 7.53 13.93 7.55 15.3C7.58 15.75 7.8 16.2 8.14 16.55H3.54C2.91 16.55 2.31 16.28 1.92 15.84C1.52 15.37 1.36 14.71 1.67 14.04C2.43 12.47 4.08 11.4 5.98 11.4Z" />
        <path d="M13.63 11.03C15.75 11.03 17.59 12.27 18.37 14.06C18.66 14.72 18.48 15.38 18.08 15.84C17.69 16.28 17.09 16.55 16.46 16.55H10.79C10.17 16.55 9.57 16.28 9.18 15.84C8.77 15.38 8.6 14.72 8.89 14.06C9.66 12.27 11.5 11.03 13.63 11.03Z" />
      </g>
    ) : (
      <>
        <ellipse cx="5.98" cy="7.33" rx="2.15" ry="2.51" />
        <ellipse cx="13.63" cy="6.86" rx="2.39" ry="2.79" />
        <path d="M8.22 12.7C7.56 12.27 6.8 12.03 5.98 12.03C4.32 12.03 2.92 12.97 2.27 14.31C1.93 15.02 2.55 15.92 3.54 15.92H7.55" />
        <path d="M13.63 11.65C11.77 11.65 10.18 12.72 9.46 14.3C9.2 14.88 9.7 15.92 10.79 15.92H16.46C17.55 15.92 18.06 14.88 17.8 14.3C17.08 12.72 15.49 11.65 13.63 11.65Z" />
      </>
    )}
  </IconSvg>
);

/** A form's "익명의 응답": a person in a dashed ring, as Notion's (seen 2026-11-08). */
export const AnonymousPersonIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorUserCircleDashed} />
);

/** A form's "제출물 사용 권한": a document with a lock, as Notion's (seen 2026-11-08). */
export const LockedDocumentIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorFileLock} />
);

/** A profile's empty "〈이름〉 님의 페이지": a page over a page, as Notion's (seen 2026-11-15). */
export const PagesStackIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorFiles} />
);

export const VerifiedIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorSealCheck} />
);

// An upright pin, as Notion's (seen 2026-10-25, drawn anew on Leafen's grid):
// a flat cap over a neck that narrows, a wide round-cornered base and a short
// needle under it; a pin that holds something is filled.
const PIN_BODY = "M7.95 4.4H12.05Q12.65 4.4 12.65 5V5.3C12.65 6.3 11.55 6.6 11.55 7.8C11.55 9 13.95 9.6 13.95 11.4V11.9"
  + "Q13.95 12.4 13.45 12.4H6.55Q6.05 12.4 6.05 11.9V11.4C6.05 9.6 8.45 9 8.45 7.8C8.45 6.6 7.35 6.3 7.35 5.3V5Q7.35 4.4 7.95 4.4Z";

export const PinIcon = ({ weight, ...p }: LeafenIconProps) => (
  <IconSvg {...p} glyph={weight === "fill" ? "pin-fill" : "pin"}>
    {weight === "fill" ? <path d={PIN_BODY} fill="currentColor" strokeWidth={0.6} /> : <path d={PIN_BODY} />}
    <path d="M10 12.4V16" />
  </IconSvg>
);

// A place's pin with a plus in it, for putting a page on a map (Notion's
// map view, seen 2026-11-02, drawn anew on Leafen's grid).
export const MapPinPlusIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="map-pin-plus">
    <path d="M10 17.2 5.7 11.7A5.3 5.3 0 0 1 4.7 8.6V8.4a5.3 5.3 0 0 1 10.6 0v.2a5.3 5.3 0 0 1-1 3.1Z" />
    <path d="M10 6.4v4M8 8.4h4" />
  </IconSvg>
);

// The pin let go: the same pin struck through.
export const PinSlashIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="pin-slash">
    <path d={PIN_BODY} />
    <path d="M10 12.4V16" />
    <path d="M4.4 4.4 15.6 15.6" />
  </IconSvg>
);

// A shut eye, its lashes down: a property hidden always, as Notion's (seen 2026-10-25).
export const EyeClosedIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorEyeClosed} />
);

// A diagonal arrow down to the left, as Notion's "백링크 없음" (seen 2026-10-25).
export const ArrowDownLeftIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowDownLeft} />
);

export const WrapIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowUDownLeft} />
);

// A column's "왼쪽에 삽입"·"오른쪽에 삽입", as Notion's (seen 2026-11-06): an
// arrow to a narrow rounded bar, the column it goes beside.
export const InsertLeftIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="insert-left">
    <rect x="2.9" y="4" width="2.4" height="12" rx="1.2" />
    <path d="M16.5 10H7.6M10.6 7l-3 3 3 3" />
  </IconSvg>
);

export const InsertRightIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="insert-right">
    <rect x="14.7" y="4" width="2.4" height="12" rx="1.2" />
    <path d="M3.5 10h8.9M9.4 7l3 3-3 3" />
  </IconSvg>
);

// "콘텐츠 줄바꿈 해제하기": an arrow to a line, as Notion's (seen 2026-11-06).
export const UnwrapIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowLineRight} />
);

// A bolt. Below 18px Notion's small one (lightningSmall, a view toolbar's "자동화"), drawn where Notion puts its ink
// (measured 2026-11-22 at 1440 by its painted area, two tabs): a 1.17 outline with round corners, its upright strokes
// leaning a little, 8.75 by 12.92 of 16 from 3.62, 1.35.
export const LightningIcon = (p: LeafenIconProps) => smallMark(p.pixels ?? ICON_SIZES[p.size ?? "small"]) ? (
  <IconSvg {...p} glyph="lightning" line={1.17}>
    <path d="M11.15 3.95 6.2 10.6h3l-.35 5.08 4.92-6.88H10.8z" />
  </IconSvg>
) : (
  <PhosphorIconSvg {...p} icon={PhosphorLightning} />
);

// Notion's "그룹화", in a column's menu and a view's settings (seen
// 2026-10-24): a line over a row of three squares, twice.
export const GroupIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="group" glyphWidth={10.88}>
    <path d="M4 4.4h12" />
    <rect x="4" y="6.4" width="2.4" height="2.4" rx=".5" />
    <rect x="8.8" y="6.4" width="2.4" height="2.4" rx=".5" />
    <rect x="13.6" y="6.4" width="2.4" height="2.4" rx=".5" />
    <path d="M4 11.2h12" />
    <rect x="4" y="13.2" width="2.4" height="2.4" rx=".5" />
    <rect x="8.8" y="13.2" width="2.4" height="2.4" rx=".5" />
    <rect x="13.6" y="13.2" width="2.4" height="2.4" rx=".5" />
  </IconSvg>
);

// Notion's "유형 변경" in a column's menu and "유형 전환" in a view's
// settings (seen 2026-10-24): two arrows turning round a square, up on its
// left and down on its right.
export const ConvertIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="convert">
    <path d="M12.4 15.2H6.4a1.6 1.6 0 0 1-1.6-1.6V5.2" />
    <path d="m2.8 7.2 2-2 2 2" />
    <path d="M7.6 4.8h6a1.6 1.6 0 0 1 1.6 1.6v8.4" />
    <path d="m13.2 12.8 2 2 2-2" />
  </IconSvg>
);

// The search window's filter switch, as Notion's: stacked lines in a circle.
export const FilterCircleIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="filter-circle">
    <circle cx="10" cy="10" r="6.6" />
    <path d="M6.8 8h6.4" />
    <path d="M7.9 10.4h4.2" />
    <path d="M9 12.8h2" />
  </IconSvg>
);

// A flattened card's sub-items, as Notion's (seen 2026-10-11): a parent's
// count after an arrow turning down and right, a sub-item's parent after one
// turning left and up.
export const SubitemCountIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowElbowDownRight} />
);

export const SubitemParentIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowElbowLeftUp} />
);

// A status's three groups, as Notion marks them (seen 2026-10-24): a dashed
// ring for To-do, a ring with a play mark for In progress, a ring with a check
// for Complete.
export const StatusToDoIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCircleDashed} />
);

export const StatusInProgressIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorPlayCircle} />
);

export const StatusCompleteIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCheckCircle} />
);

// A view's "데이터베이스 ›", in its settings and its tab's menu, as Notion's
// pathRoundEnds (seen 2026-10-24; its places on the 20 grid measured
// 2026-11-15 at 402, 22px: 15.1 of ink 3.4 in): a path winding down from a
// ring at the top end to a dot at the start, its lines 1.25.
export const DataPathIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="data-path" strokeWidth="1.25">
    <circle cx="14.75" cy="5.25" r="1.5" />
    <path d="M13.25 5.25H6.31a2.375 2.375 0 0 0 0 4.75h7.56a2.375 2.375 0 0 1 0 4.75H7" />
    <circle cx="5.25" cy="14.75" r="2.13" fill="currentColor" stroke="none" />
  </IconSvg>
);

// A board's or gallery's card menu's "레이아웃", as Notion's (seen
// 2026-10-24): a card over a tilted one.
export const CardLayoutIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="card-layout">
    <rect x="6.6" y="7.6" width="10.6" height="8" rx="1.6" />
    <path d="M5.5 13.1 4.2 13.3 3.1 6 12.6 4.5 12.9 6.6" />
  </IconSvg>
);

// A database waiting for its data source: a folder, as Notion's empty linked view (seen 2026-11-02).
export const FolderIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorFolderSimple} />
);

// A wrapped text cell's "확장", which grows its row to the whole value.
export const ExpandRowIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowsVertical} />
);

/** A template database's "샘플 페이지 삭제하기", as Notion's sparkle (seen 2026-11-06). */
export const StartSamplesIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorSparkle} weight="fill" />
);

/** A template database's "투어": a filled play ring, as Notion's (seen 2026-11-06). */
export const StartTourIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorPlayCircle} weight="fill" />
);

// Notion's database in a move window's search (seen 2026-11-11, its 20-unit
// "collection" read on this 16-unit box): a cylinder, its top an ellipse and
// one band round its middle. Notion marks a database or data source that has
// no icon of its own with it wherever one is named in a list (seen 2026-11-15
// in a search result's "/ source" and the data sources to link; again in a
// sidebar database view section and its picker, the view settings' "데이터
// 소스 관리", a form question's linked property and the page layout's bar),
// so Leafen draws it there too; a database page's own place — the sidebar,
// a site's preview, "데이터베이스 보기" to add — is a table (TableIcon).
export const DatabaseCylinderIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="database-cylinder">
    <ellipse cx="10" cy="6.18" rx="5" ry="1.78" />
    <path d="M5 6.18v7.64a5 1.78 0 0 0 10 0V6.18" />
    <path d="M5 10a5 1.78 0 0 0 10 0" />
  </IconSvg>
);

// "새 규칙 추가"'s mark, in the shape of Notion's (its access rules, seen
// 2026-11-14, drawn anew on Leafen's grid): a plus in a diamond whose corners
// are rounded.
export const PlusDiamondIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="plus-diamond">
    <path d="M9.08 3.42a1.3 1.3 0 0 1 1.84 0l5.66 5.66a1.3 1.3 0 0 1 0 1.84l-5.66 5.66a1.3 1.3 0 0 1-1.84 0L3.42 10.92a1.3 1.3 0 0 1 0-1.84Z" />
    <path d="M10 7.3v5.4M7.3 10h5.4" />
  </IconSvg>
);

// A dashboard row's "화면 높이에 맞게 조정", in the shape of Notion's (seen
// 2026-11-15, drawn anew on Leafen's grid): a box open at its top and
// bottom middles, an arrow leaving each opening.
export const FitRowHeightIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="fit-row-height">
    <path d="M8.2 6.8H5.6a1.6 1.6 0 0 0-1.6 1.6v3.2a1.6 1.6 0 0 0 1.6 1.6h2.6M11.8 6.8h2.6a1.6 1.6 0 0 1 1.6 1.6v3.2a1.6 1.6 0 0 1-1.6 1.6h-2.6" />
    <path d="M10 8.6V3.2M8.2 5 10 3.2 11.8 5M10 11.4v5.4M8.2 15 10 16.8l1.8-1.8" />
  </IconSvg>
);

// A dashboard widget's "위에 새 행 생성하기" and "아래에 새 행 생성하기", in the
// shape of Notion's (seen 2026-11-15, drawn anew on Leafen's grid): a box
// open at its top, an arrow rising out of it or falling into it.
export const NewRowAboveIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="new-row-above">
    <path d="M7.4 7.6H6a1.6 1.6 0 0 0-1.6 1.6v5.2A1.6 1.6 0 0 0 6 16h8a1.6 1.6 0 0 0 1.6-1.6V9.2A1.6 1.6 0 0 0 14 7.6h-1.4" />
    <path d="M10 11.6V3.4M7.8 5.6 10 3.4l2.2 2.2" />
  </IconSvg>
);

export const NewRowBelowIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="new-row-below">
    <path d="M7.4 7.6H6a1.6 1.6 0 0 0-1.6 1.6v5.2A1.6 1.6 0 0 0 6 16h8a1.6 1.6 0 0 0 1.6-1.6V9.2A1.6 1.6 0 0 0 14 7.6h-1.4" />
    <path d="M10 3.4v8.2M7.8 9.4l2.2 2.2 2.2-2.2" />
  </IconSvg>
);
