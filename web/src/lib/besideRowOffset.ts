import { POPOVER_MARGIN, type PopoverSize, type PopoverViewport } from "./popoverPlacement";

/**
 * Where a list opened beside a menu row starts, from the row's top: centred
 * on the row where that keeps it inside the window's edges, else from the
 * row's top, moved up into the window where it runs past its foot. As
 * Notion's lists beside a menu (seen 2026-11-14: a sidebar section's "정렬"
 * and "표시" centred on their rows; 2026-11-16 at 1440×723: home's "기본
 * 시작 페이지 변경" (123 high) and "위젯 표시/숨기기" (181 high) by rows at
 * 44 and 73, under the window's top, each from its row's top, 4px over the
 * menu; the sidebar's "섹션 추가" › "데이터베이스 보기" by a low row, beside
 * it, its foot at the window's).
 */
export function besideRowOffset(row: { top: number; bottom: number }, size: PopoverSize, viewport: PopoverViewport) {
  const centred = (row.bottom - row.top - size.height) / 2;
  const top = row.top + centred;
  return top >= POPOVER_MARGIN && top + size.height <= viewport.height - POPOVER_MARGIN ? centred : 0;
}
