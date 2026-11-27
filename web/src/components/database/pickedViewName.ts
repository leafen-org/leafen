import { focusOpenedField } from "@/components/editor/openedFieldCaret";

/**
 * A view that took after one of its source's views — picked in a linked
 * view's "새 보기" (DatabaseLinkedStarter) or under a new view's "〈소스〉의
 * 조회수" (DatabaseViewSourceViews) — ends at its settings with the keys in
 * its name, as Notion's: the copied name chosen whole, so a letter typed
 * replaces it, and an empty field for a view never named (the field chooses
 * its text as it takes the focus, DatabaseViewNameField). A touch screen's
 * too, its keyboard rising, both ways alike — unlike a window's field as it
 * opens (fieldFocusOnOpen), for the view was just picked. A new view ended
 * by its "완료" opens the same settings with the keys nowhere. Seen in
 * Chrome 1440 on 2026-11-18 in tabs opened at different times, after the
 * press only (no script in the page before it) with one letter typed:
 * "/데이터 소스 보기 연결" then the source's "zlist" (twice) or its unnamed
 * "표"; a view added to the linked database, its source and then "zlist"
 * (twice) or "표" (twice); "+ 새 표 보기" then "완료" (twice). On a phone
 * (Chrome on a Galaxy A90, 2026-11-18, two tabs at different times, read
 * from the screen after the press with one letter typed): "링크된 보기"'s
 * "새 보기" then "zl", and a view added by "새 보기 추가", its source, then
 * "zl" — each the "설정" sheet with "zl" chosen whole and the keyboard up,
 * the letter replacing it; its "+ 새 표 보기" opens with the keyboard down.
 * `menu` reads the settings; from the second frame after they are asked for,
 * the name takes the focus in the first frame they stand drawn, looked for up
 * to a second (2026-11-18: on the A90 a linked view's settings were drawn
 * about 40 ms after its pick asked for them, past the single look this made
 * two frames on, and the name stayed unfocused).
 */
const LOOKS = 60;

export function focusPickedViewName(menu: () => HTMLElement | null) {
  // What is typed from the pick until then goes in there (focusOpenedField).
  focusOpenedField(() => menu()?.querySelector<HTMLInputElement>("[data-view-settings-name]"), { scroll: false, from: 2, frames: LOOKS });
}
