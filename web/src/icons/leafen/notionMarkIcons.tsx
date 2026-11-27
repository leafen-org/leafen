import { IconSvg, type LeafenIconProps } from "./iconPrimitives";

// Marks drawn here after where Notion's own put their ink (read 2026-11-15
// at 1440 in dark, each on its 20 box), not from Notion's paths.

/**
 * A button's "버튼 클릭": a pointer, its head filled and its tail a line,
 * with six short rays round its tip (up, both upper corners, both sides and
 * the lower left); ink 2.5–17.5 across, 2.5–18 down.
 */
export const ButtonClickIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="button-click" strokeWidth="1.4">
    <path d="M9.75 3.25V5.15M5.25 5.25 6.7 6.7M14.25 5.25 12.8 6.7M3.25 9.75H5.3M14.7 9.75H16.75M5.2 14.45 6.7 12.95" />
    <path d="M9.3 8.85V15.25L11.15 13.7 14.2 13.35Z" fill="currentColor" strokeWidth="0.8" />
    <path d="M11.9 14.1 13.4 17.15" strokeWidth="1.7" />
  </IconSvg>
);

/**
 * A form's "미리보기" eye, Notion's 20px one trimmed to 18.07 across: an
 * almond in a 1.3 line from side to side of its box, 4–16 down, round its
 * iris, a thick ring 3.4 out with a 1.1 hole.
 */
export const FormPreviewEyeIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} pixels={20} width={18.07} height={20} viewBox="0.96 0 18.07 20" glyph="form-preview-eye" strokeWidth="1.3">
    <path d="M1.7 10C3.45 6.6 6.55 4.7 10 4.7S16.55 6.6 18.3 10C16.55 13.4 13.45 15.3 10 15.3S3.45 13.4 1.7 10Z" />
    <circle cx="10" cy="10" r="2.25" strokeWidth="2.3" />
  </IconSvg>
);

/**
 * A page button's icon picker with no icon chosen: a filled face filling its
 * box, two upright eyes and a smile cut out of it.
 */
export const EmojiFaceFillIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="2.37 2.37 15.26 15.26" glyph="emoji-face-fill">
    <path fill="currentColor" stroke="none" fillRule="evenodd"
      d="M2.37 10A7.63 7.63 0 1 0 17.63 10 7.63 7.63 0 1 0 2.37 10ZM6.9 8.65A.55.85 0 1 0 8 8.65.55.85 0 1 0 6.9 8.65ZM12 8.65A.55.85 0 1 0 13.1 8.65.55.85 0 1 0 12 8.65ZM7.35 11.55C7.9 12.95 8.9 13.65 10 13.65S12.1 12.95 12.65 11.55C12.05 12.2 11.1 12.55 10 12.55S7.95 12.2 7.35 11.55Z" />
  </IconSvg>
);

/**
 * A filled lock, Notion's "lockFill" (seen 2026-11-16 in the share window's
 * "일반 사용 권한" and a private page's place in the top bar): a thick
 * shackle 4.25 round over a body 12.05 by 10.2 with 2.4 corners, a keyhole cut
 * in it, a 1.35 round head over a 1.1 stem; ink 4–16 across, 1.7–17.6 down.
 */
export const LockFillIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="lock-fill">
    <g fill="currentColor" stroke="none">
      <path d="M5.75 7.4V5.95A4.25 4.25 0 0 1 14.25 5.95V7.4H12.5V5.95A2.5 2.5 0 0 0 7.5 5.95V7.4Z" />
      <path fillRule="evenodd" d="M6.4 7.4H13.6A2.425 2.425 0 0 1 16.025 9.825V15.175A2.425 2.425 0 0 1 13.6 17.6H6.4A2.425 2.425 0 0 1 3.975 15.175V9.825A2.425 2.425 0 0 1 6.4 7.4ZM10 10.225A1.35 1.35 0 0 1 10.55 12.807V14.05A0.55 0.55 0 0 1 9.45 14.05V12.807A1.35 1.35 0 0 1 10 10.225Z" />
    </g>
  </IconSvg>
);

/**
 * "체크박스" among a status's "다음과 같이 표시" choices, Notion's outlined
 * box there (seen 2026-11-16 at 1440 in a column's menu, and in the phone's
 * sheet), where its checkbox type's own mark is a filled box: a 1.25 ring
 * 3.125–16.875 across and down with 2.125 corners, a ✓ in the same line
 * inside it from 7.35, 10.35 down to 9.35, 12.65 and up to 12.35, 7.65.
 */
export const CheckSquareOutlineIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="check-square-outline" strokeWidth="1.25">
    <rect x="3.75" y="3.75" width="12.5" height="12.5" rx="1.5" />
    <path d="M7.35 10.35 9.35 12.65 12.35 7.65" />
  </IconSvg>
);

/**
 * A page chip's "×" in a phone's relation filter (Notion's xMarkFillSmall,
 * seen 2026-11-17 on iPhone Safari at 402, 16px): two strokes crossing in
 * its middle, 1.5 of 16 thick with round ends, ink 3.05–12.95 both ways.
 */
export const XMarkBoldSmallIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 16 16" glyph="x-mark-bold-small" line={1.5}>
    <path d="M3.8 3.8 12.2 12.2M12.2 3.8 3.8 12.2" />
  </IconSvg>
);

/**
 * A checkbox condition's value in its pill, Notion's squareSmall and
 * checkmarkSquareSmall (seen 2026-11-18 at 1440, 14px in "Done 속성이
 * [☐ 선택 취소됨] 값으로 …" and "[☑ 선택됨]"): a ring 2.375–13.625 of 16
 * across and down in a 1.25 line with 1.875 corners, ticked by a ✓ in the
 * same line from 5.85, 8.23 down to 7.48, 10.1 and up to 9.85, 6.1.
 */
export const CheckboxValueIcon = ({ checked, ...p }: LeafenIconProps & { checked: boolean }) => (
  <IconSvg {...p} viewBox="0 0 16 16" glyph={checked ? "checkbox-value-checked" : "checkbox-value"} line={1.25}>
    <rect x="3" y="3" width="10" height="10" rx="1.25" />
    {checked ? <path d="M5.85 8.23 7.48 10.1 9.85 6.1" /> : null}
  </IconSvg>
);

/**
 * A checkbox formula's mark in an automation's "내 값" chip, Notion's
 * checkmarkSquare on its 20 box (seen 2026-11-22 at 1440 dark, dpr 1, 14px):
 * a ring 3.125–16.875 across and down in a 1.25 line with 2.125 corners,
 * ticked by a ✓ in the same line from 7.34, 10.33 down to 9.25, 12.6 and up
 * to 12.34, 7.66 — an outline, not the filled checkbox type mark.
 */
export const CheckmarkSquareIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="checkmark-square" strokeWidth="1.25">
    <rect x="3.75" y="3.75" width="12.5" height="12.5" rx="1.5" />
    <path d="M7.34 10.33 9.25 12.6 12.34 7.66" />
  </IconSvg>
);

/**
 * "변수 정의" on an automation list's card, Notion's textJustifyLeftSmall
 * (seen 2026-11-18 at 1440 in two automations): four lines 2.67 apart in a
 * 1.25 line on a 16 box, 4 to 12 down, from 2.3 to 13.7 across and the last
 * one to the middle.
 */
export const TextJustifyLeftSmallIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 16 16" glyph="text-justify-left-small" line={1.25}>
    <path d="M2.3 4h11.4M2.3 6.67h11.4M2.3 9.33h11.4M2.3 12H8" />
  </IconSvg>
);

/**
 * A token made again, Notion's arrowInCircleClockwiseInCircle (seen
 * 2026-11-21 at 16 in an API token's row and at 32 over its refresh
 * question): a ring 5.5 round the middle of a 16 box in a 1.25 line, and
 * inside it an arrow going clockwise — three quarters round 2.16 from the
 * middle in a 1.15 line, from the right through the bottom to the top,
 * where a filled head 2.7 high points on to the right.
 */
export const RefreshInCircleIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 16 16" glyph="refresh-in-circle" line={1.25}>
    <circle cx="8" cy="8" r="5.5" />
    <path d="M10.16 8A2.16 2.16 0 1 1 8 5.84" strokeWidth="1.15" />
    <path d="M8.1 4.7 9.8 5.84 8.1 6.98Z" fill="currentColor" strokeWidth="0.4" />
  </IconSvg>
);
