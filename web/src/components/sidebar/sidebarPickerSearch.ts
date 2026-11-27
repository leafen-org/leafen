import { useEffect, type KeyboardEvent } from "react";
import { imeComposing } from "@/lib/fieldKeyBoundary";

/** The sidebar pickers that remember their openings apart. */
export type SidebarPickerSearch = "dataSource" | "shortcutPage" | "shortcutNewPage" | "teamspace";

const opened = new Set<SidebarPickerSearch>();

/**
 * Where a sidebar picker's focus goes as it opens, as Notion's (seen
 * 2026-11-18 at 1440 in Chrome, a letter typed after each opening with no
 * script before it): opened from the keys, into its search with its blue
 * ring; pressed open, onto the picker itself — no ring, a letter typed going
 * nowhere — the first time it opens since the app loaded, and into its search
 * every time after, however it opened before (pressed, by the keys, or by the
 * pointer resting on its row) and whether or not its search ever had the
 * focus: "섹션 추가 › 팀스페이스" pressed open, closed by moving the pointer
 * onto "최근 항목" with no key typed, then pressed again, took its search with
 * the ring, as did "데이터베이스 보기" and "단축키 추가 › 새 페이지". Each
 * picker remembers its own: the data source picker ("섹션 추가 › 데이터베이스
 * 보기" and a view section's "출처" together), the shortcut's "페이지 ›" and
 * its "새 페이지 ›", the teamspaces of "섹션 추가" apart. A reload forgets;
 * closing the menu or leaving the sidebar's customizing does not.
 */
export function sidebarPickerSearchFirst(picker: SidebarPickerSearch, byKeys: boolean) {
  return byKeys || opened.has(picker);
}

/**
 * Remembers that the picker has opened, once it has been drawn: read as it
 * opens (sidebarPickerSearchFirst), its own opening counts from its next one.
 */
export function useSidebarPickerOpened(picker: SidebarPickerSearch | undefined) {
  useEffect(() => {
    if (!picker) return undefined;
    // Past this frame: React's development replay of the effect is not an opening.
    const timer = window.setTimeout(() => opened.add(picker), 0);
    return () => window.clearTimeout(timer);
  }, [picker]);
}

/**
 * Escape on a picker holding the focus itself (pressed open) closes it alone,
 * as Escape in its search does, the menu it opened from staying open
 * (Notion's, seen 2026-11-18). Gone on through the portal to that menu, the
 * key was taken there and closed neither.
 */
export function closeOnOwnEscape(onClose: () => void) {
  return (event: KeyboardEvent<HTMLElement>) => {
    if (event.key !== "Escape" || event.target !== event.currentTarget || imeComposing(event)) return;
    event.preventDefault();
    event.stopPropagation();
    onClose();
  };
}

/**
 * The layer options of a picker beside a sidebar menu's row ("단축키 추가 ›
 * 새 페이지 ›" and "페이지 ›", "섹션 추가 › 데이터베이스 보기", a view
 * section's "출처"), as Notion's (seen 2026-11-18 at 1440 in Chrome): its row
 * is its own, so a press there keeps the picker the pointer opened coming in
 * (taken for a press outside it, it closed the picker and the press was lost);
 * a press outside both closes the menu with it, by the shade that menu lists
 * as its own (`modalOwned`) — Leafen's left the menu open.
 */
export function sidebarPickerBesideRow(row: Element | null | undefined) {
  return row ? { inside: () => [row], closesHost: true } : {};
}
