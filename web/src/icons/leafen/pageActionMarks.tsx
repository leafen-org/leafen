import { IconSvg, type LeafenIconProps } from "./iconPrimitives";

// A page's "…" menu marks for copying its contents, moving it, its versions
// and its updates, drawn after where Notion's own put their ink (read
// 2026-11-16 at 1440 in dark, each on its 20 box at twice its size), not from
// Notion's paths: lines 1.25 with round ends, the version stack's two top
// lines 1.1.

/** "페이지 내용 복사하기": an empty clipboard, its clip a pill over the board's top edge. */
export const ClipboardIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="clipboard" strokeWidth="1.25">
    <path d="M6.92 3.375H6.25A1.5 1.5 0 0 0 4.75 4.875V15.375A1.5 1.5 0 0 0 6.25 16.875H13.75A1.5 1.5 0 0 0 15.25 15.375V4.875A1.5 1.5 0 0 0 13.75 3.375H13.08" />
    <rect x="7.5" y="2.125" width="5" height="2.5" rx="1" />
  </IconSvg>
);

/** "옮기기": a line rising from the bottom and turning square to the end, into an arrowhead (↱). */
export const ArrowBendUpRightIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="arrow-turn-up-right" strokeWidth="1.25">
    <path d="M3.75 15.25V10.25A1.5 1.5 0 0 1 5.25 8.75H16.6" />
    <path d="M12.85 4.35 17.25 8.75 12.85 13.15" />
  </IconSvg>
);

/** The sidebar row's and the other menus' "옮기기", the same mark. */
export const MoveIcon = ArrowBendUpRightIcon;

/** "버전 기록": two lines, the upper shorter, over a box — a stack of versions seen edge on. */
export const VersionHistoryIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="version-stack" strokeWidth="1.25">
    <path d="M7.3 2.545H12.7M5.5 4.795H14.5" strokeWidth="1.1" />
    <rect x="3.588" y="7.045" width="12.825" height="9.45" rx="1.5" />
  </IconSvg>
);

/** "업데이트와 애널리틱스": a clock, its hands up and to the start (nine o'clock). */
export const UpdatesClockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="updates-clock" strokeWidth="1.25">
    <circle cx="10" cy="10" r="7" />
    <path d="M10 5.725V10H6.4" />
  </IconSvg>
);
