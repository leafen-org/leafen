import { GearSix as PhosphorGearSix, LinkSimple as PhosphorLinkSimple } from "@phosphor-icons/react";
import { ICON_SIZES, IconSvg, PhosphorIconSvg, smallMark, type LeafenIconProps } from "./iconPrimitives";

// The marks of a menu's everyday rows — rename, show as, add to the sidebar,
// duplicate, delete, a new data source — drawn on Notion's 20 grid with its
// 1.25 lines, as its pencilLine, paintBrush, sidebarLeft, duplicate, trash
// and squareOnSquarePlus stand in a view tab's menu (seen 2026-11-15 at 1440:
// laid over Notion's at 120px, their lines and shapes in the same places).
// Every menu showing one shows the same mark.

/** Rename: a pencil drawn as one slanted stroke, its cap apart at the top, over a line. */
export const Pencil = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="pencil" strokeWidth="1.25">
    <path d="M3.9 14.47 11.82 6.56" strokeWidth="1.82" strokeLinecap="butt" />
    <path d="M3.26 13.82 4.55 15.12 2.95 15.65Z" fill="currentColor" strokeWidth="0.5" />
    <path d="M12.66 5.72 13.35 5.03" strokeWidth="1.82" strokeLinecap="butt" />
    <circle cx="13.35" cy="5.03" r="0.91" fill="currentColor" stroke="none" />
    <path d="M6.4 15.25H17" />
  </IconSvg>
);

/**
 * Rename in a small menu, as Notion's pencilSmall in a version bookmark's
 * options (seen 2026-11-15): the pencil's one slanted stroke and its cap
 * apart at the top, with no line under it, on a 16 grid.
 */
export const PencilSmallIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 16 16" glyph="pencil-small" strokeWidth="1.25">
    <path d="M4.15 11.85 10.5 5.5" strokeWidth="1.45" strokeLinecap="butt" />
    <path d="M3.63 11.33 4.67 12.37 3.39 12.79Z" fill="currentColor" strokeWidth="0.4" />
    <path d="M11.17 4.83 11.72 4.28" strokeWidth="1.45" strokeLinecap="butt" />
    <circle cx="11.72" cy="4.28" r="0.73" fill="currentColor" stroke="none" />
  </IconSvg>
);

/** "다음과 같이 표시": a broad brush, its bristles up and to the end, its handle down to the start with a hole. */
export const PaintBrushIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="paint-brush" strokeWidth="1.25">
    <path d="M10 2.6 7.3 6.8l5.6 5.6 4.4-3.05a.95.95 0 0 0 .12-1.45L11.45 2.5A.95.95 0 0 0 10 2.6Z" />
    <path d="M14.1 6c-.25 1-.6 1.75-1.15 2.35" strokeWidth="1" />
    <path d="M7.3 6.8 6.15 6.25a.9.9 0 0 0-.95.2l-.7.7a1.9 1.9 0 0 0 0 2.7l.95.95M8.85 13.05l1.05 1.05a1.9 1.9 0 0 0 2.7 0l.7-.7a.9.9 0 0 0 .2-.95L12.9 12.4" />
    <path d="M5.6 11.1 4.3 12c-.85.6-1.4 1.3-1.45 2.3-.05.9.3 1.75 1 2.25.75.5 1.65.55 2.4.05.6-.4 1-1 1.45-1.65L8.95 13.2" />
    <circle cx="5.01" cy="14.81" r="0.67" fill="currentColor" stroke="none" />
  </IconSvg>
);

/** "사이드바에 보기 추가", the sidebar's own toggle: a frame with its start panel a third across. */
export const MenuIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="sidebar" strokeWidth="1.25">
    <rect x="2.25" y="4.25" width="15.5" height="11.5" rx="1.5" />
    <path d="M7 4.6v10.8" strokeWidth="1.1" />
  </IconSvg>
);

/** The two squares of a copy, the one behind at the start and up, the one in front at the end and down. */
const COPY_BEHIND = "M6.75 13.5H4.5A1.5 1.5 0 0 1 3 12V4.5A1.5 1.5 0 0 1 4.5 3H12a1.5 1.5 0 0 1 1.5 1.5v2.25";

export const Copy = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="duplicate" strokeWidth="1.25">
    <path d={COPY_BEHIND} />
    <rect x="6.75" y="6.75" width="10.5" height="10.5" rx="1.5" />
  </IconSvg>
);

/**
 * A copy in a small button, as Notion's duplicateSmall by "코드 복사"
 * (measured 2026-11-18): the same two squares painted 13.25 across and down,
 * in the middle of a box 16 high trimmed to that width, with a small mark's
 * line.
 */
export const CopySmallIcon = (p: LeafenIconProps) => (
  <IconSvg size="small" trim glyphWidth={13.25} {...p} glyph="duplicate-small">
    <path d="M7.17 12.83H5.28A1.26 1.26 0 0 1 4.02 11.57V5.28A1.26 1.26 0 0 1 5.28 4.02H11.57a1.26 1.26 0 0 1 1.26 1.26v1.89" />
    <rect x="7.17" y="7.17" width="8.81" height="8.81" rx="1.26" />
  </IconSvg>
);

/** "새 데이터 소스": a copy's squares with a plus in the one in front. */
export const SquareOnSquarePlusIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="square-on-square-plus" strokeWidth="1.25">
    <path d={COPY_BEHIND} />
    <rect x="6.75" y="6.75" width="10.5" height="10.5" rx="1.5" />
    <path d="M12 9.5v5M9.5 12h5" />
  </IconSvg>
);

/**
 * Settings from 18px up: a gear of eight square teeth round a ring, drawn to Notion's gear (seen 2026-11-22 at 1440
 * dark, dpr 1, a task setting's "고급 설정": its ink 15.6 across from 2.2, its hole 5.4) with the 1.25 lines; below
 * 18px Phosphor's (smallMark), where Notion draws its own small marks.
 */
export const Settings = (p: LeafenIconProps) => smallMark(p.pixels ?? ICON_SIZES[p.size ?? "small"])
  ? <PhosphorIconSvg {...p} icon={PhosphorGearSix} />
  : (
    <IconSvg {...p} viewBox="0 0 20 20" glyph="gear" strokeWidth="1.25">
      <path d="M8.8 4.84L9 2.82h2l.2 2.02a5.3 5.3 0 0 1 1.6.66l1.57-1.28 1.41 1.41-1.28 1.57a5.3 5.3 0 0 1 .66 1.6l2.02.2v2l-2.02.2a5.3 5.3 0 0 1-.66 1.6l1.28 1.57-1.41 1.41-1.57-1.28a5.3 5.3 0 0 1-1.6.66l-.2 2.02H9l-.2-2.02a5.3 5.3 0 0 1-1.6-.66l-1.57 1.28-1.41-1.41 1.28-1.57a5.3 5.3 0 0 1-.66-1.6L2.82 11V9l2.02-.2a5.3 5.3 0 0 1 .66-1.6L4.22 5.63l1.41-1.41L7.2 5.5a5.3 5.3 0 0 1 1.6-.66Z" />
      <circle cx="10" cy="10" r="2.09" />
    </IconSvg>
  );

/** A bin narrowing to its foot, its lid and handle, and two lines down inside it. */
export const Trash = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="trash" strokeWidth="1.25">
    <path d="M3.52 6.01h12.96" />
    <path d="M7.01 6.01V3.93c0-.5.4-.9.9-.9h4.18c.5 0 .9.4.9.9v2.08" />
    <path d="m5.05 6.64.39 8.6a1.8 1.8 0 0 0 1.8 1.73h5.52a1.8 1.8 0 0 0 1.8-1.73l.39-8.6" />
    <path d="M8.26 8.5v6M11.74 8.5v6" strokeWidth="1.1" />
  </IconSvg>
);

// "보기 편집" and "보기 링크 복사" from 18px up, drawn where Notion's sliders
// and link stand on its 20 grid with its 1.25 lines (seen 2026-11-15 at 402
// in the phone's "보기 옵션", 22px: sliders 16.8×12.6 of ink 2.6 and 4.7 in,
// link 16.4 across 2.8 in); below 18px Notion draws its own small marks,
// Phosphor's here (smallMark).

/**
 * Two rails, each with a ringed knob the line stops at, the upper one's toward the end, the lower one's toward the
 * start. Below 18px Notion's small one (slidersSmall, a view toolbar's "설정"), drawn where Notion puts its ink
 * (measured 2026-11-22 at 1440 by its painted area, two tabs): 1.2 rails 6 apart, knobs 1.5 round in 1.25 lines,
 * 12.75 by 10.25 of 16 from 1.62, 2.88.
 */
export const SlidersIcon = (p: LeafenIconProps) => smallMark(p.pixels ?? ICON_SIZES[p.size ?? "small"])
  ? (
    <IconSvg {...p} glyph="sliders-horizontal" line={1.2}>
      <path d="M4.2 7h6.3M13.5 7h2.3M4.2 13h2.3M9.5 13h6.3" />
      <circle cx="12" cy="7" r="1.5" strokeWidth="1.25" />
      <circle cx="8" cy="13" r="1.5" strokeWidth="1.25" />
    </IconSvg>
  )
  : (
    <IconSvg {...p} viewBox="0 0 20 20" glyph="sliders-horizontal" strokeWidth="1.25">
      <path d="M3 6.75h6.75M14.75 6.75H17M3 13.25h2.25M10.25 13.25H17" />
      <circle cx="12.25" cy="6.75" r="1.875" />
      <circle cx="7.75" cy="13.25" r="1.875" />
    </IconSvg>
  );

/** Two open links, one up at the end and one down at the start, each bent round a 3.15 radius, and the bar between them. */
export const LinkIcon = (p: LeafenIconProps) => smallMark(p.pixels ?? ICON_SIZES[p.size ?? "small"])
  ? <PhosphorIconSvg {...p} icon={PhosphorLinkSimple} />
  : (
    <IconSvg {...p} viewBox="0 0 20 20" glyph="link-simple" strokeWidth="1.25">
      <path d="M9.2 5.91 11.05 4.05A3.15 3.15 0 0 1 15.51 4.05L15.88 4.42A3.15 3.15 0 0 1 15.88 8.88L14.02 10.73" />
      <path d="M10.73 14.02 8.88 15.88A3.15 3.15 0 0 1 4.42 15.88L4.05 15.51A3.15 3.15 0 0 1 4.05 11.05L5.91 9.2" />
      <path d="M12.54 7.39 7.09 12.84" />
    </IconSvg>
  );

/**
 * "새 창에서 열기": a window whose bar is solid, three dots open in it at the
 * start, as Notion's browser mark in the desktop app's page menus (seen
 * 2026-11-16: on its 20 grid, 15.25 by 11.75 with a 1.25 frame, the bar 3.4 deep).
 */
export const NewWindowIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="new-window" stroke="none">
    <path fill="currentColor" fillRule="evenodd" d={"M4.5 4.125h11a2.125 2.125 0 0 1 2.125 2.125v7.5a2.125 2.125 0 0 1-2.125 2.125h-11"
      + "a2.125 2.125 0 0 1-2.125-2.125v-7.5A2.125 2.125 0 0 1 4.5 4.125ZM3.625 7.5v6.25c0 .483.392.875.875.875h11"
      + "a.875.875 0 0 0 .875-.875V7.5ZM4.008 5.81a.625.625 0 1 0 1.25 0 .625.625 0 1 0-1.25 0Zm2 0a.625.625 0 1 0 1.25 0"
      + " .625.625 0 1 0-1.25 0Zm2 0a.625.625 0 1 0 1.25 0 .625.625 0 1 0-1.25 0Z"} />
  </IconSvg>
);
