/**
 * Gives the focus to the menu itself, off the search that owns it: a letter
 * typed then goes nowhere and the arrows move its highlight, the search
 * showing no ring — where Notion's sidebar "섹션 추가" and "단축키 추가" leave
 * it as a list beside one of their rows closes, by Escape or by the pointer
 * moving on to another row, however the list was opened (seen 2026-11-18 at
 * 1440 in Chrome in two tabs, a letter typed after each with no script before
 * it: the row lit grey, "q" in no field, ↓ lighting the next row and "q" still
 * nowhere).
 */
export function menuTakesFocus(menu: HTMLElement | null) {
  if (!menu) return;
  if (!menu.hasAttribute("tabindex")) {
    menu.tabIndex = -1;
    menu.addEventListener("blur", () => menu.removeAttribute("tabindex"), { once: true });
  }
  menu.focus({ preventScroll: true });
}
