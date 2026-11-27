import { appleTouchDevice } from "@/lib/iosViewport";
import { focusedOpener } from "@/lib/lastPress";
import { focusGoesToPage } from "@/lib/overlayLayer";
import { sheetClosedByPress } from "@/lib/sheetPressFocus";

/**
 * Where the focus goes as a phone sheet closes, opened from `opener`, as
 * Notion's in iPhone Safari. Closed by a press — its "완료" or "✕", its shade,
 * a drag down — it is left on the page, no ring left and Enter opening nothing
 * again (seen 2026-11-17 with no script before the action, a letter typed
 * after it landing nowhere: the drawer's "휴지통" and its head's "계정과
 * 워크스페이스" closed by "완료", and "모든 토론" by "✕", by its shade and by a
 * drag, the focus on the body each time; the drawer's "휴지통" by "완료" in
 * two tabs at two times). Closed by Escape it goes back to the opener, which
 * Notion's kept all along (the drawer's "휴지통" ringed, two tabs) — but an
 * item's control, a drawer row's "⋯", leaves it on the page as its menu does
 * (focusGoesToPage): the row WebKit focused under the press lets it go, as
 * Notion's "작업" sheet closed by a hardware Escape left it on the body (two
 * tabs). Text an opener holds takes it back however the sheet closed.
 *
 * An Android phone's Chrome leaves it on the opener its tap focused when the
 * shade or a drag down closes the sheet — presses that take no focus (the
 * shade's data-press-keeps-focus) — as Notion's on the Galaxy A90 (seen
 * 2026-11-17 with no script before the action, a letter typed after it ringing
 * the opener: the page export's "내보내기 형식" sheet by its shade in two tabs
 * at two times and by a drag, and the share sheet by its shade in two tabs,
 * on "공유"); a tap inside it on any of its controls — a row chosen, "완료" —
 * takes the focus with it, left on the body however the sheet then closes
 * (`tappedInside`: the format sheet's row twice, the share sheet's "완료" in
 * two tabs). A table cell's value sheet answers so too, its cell standing for
 * the control in it that opened it, as a tap on Notion's cell focuses the cell
 * itself (seen 2026-11-18 on the A90, no script before the action, a letter
 * typed after it ringing the cell or landing nowhere, each in a fresh tab: a
 * select's and a date's sheet closed by the shade or a drag left it on the
 * cell, four tabs; a select's or a status's option chosen, and a
 * multi-select's option chosen then its shade, on the body, three tabs). A
 * layer drawn as a sheet, whose own focus would go back to its opener, leaves
 * it the same way (sheetPressLeavesPage).
 */
export function giveSheetFocusBack(opener: HTMLElement | null, tappedInside = false) {
  const byPress = sheetClosedByPress(opener);
  const place = placeOf(opener);
  window.requestAnimationFrame(() => {
    const active = document.activeElement;
    const page = focusGoesToPage(opener);
    if (byPress && !page && !tappedInside && !appleTouchDevice() && (active === opener || active === place)) {
      if (active !== place && place?.isConnected) place.focus({ preventScroll: true });
      return;
    }
    if (byPress || page) {
      if (active instanceof HTMLElement && opener && (active === place || active.contains(opener))) active.blur();
      return;
    }
    if ((!active || active === document.body || active === opener) && place?.isConnected) place.focus({ preventScroll: true });
  });
}

// What a sheet's opener stands for: a table cell for the control in it.
const CELL = "[data-table-cell]:not([data-table-row-gutter-cell])";
const placeOf = (element: HTMLElement | null) => element?.closest<HTMLElement>(CELL) ?? element;

// What an Android tap focuses, as a Notion sheet's rows (a div role="option" tabindex="-1") take it.
const TAKES_FOCUS = 'button, a[href], input, select, textarea, summary, [tabindex], [contenteditable="true"], [role="option"], [role="menuitem"], [role="menuitemcheckbox"], [role="menuitemradio"], [role="checkbox"], [role="switch"], [role="button"], [role="tab"]';
const SHEET = "[data-phone-sheet], [data-phone-sheet-menu]";

// The sheets open now, the last on top: where each gives the focus back, and whether a tap went inside it.
const followed: Array<{ opener: HTMLElement | null; tappedInside: boolean }> = [];

/**
 * Follows a sheet opening now from the focus as it stands (focusedOpener)
 * until it closes, then gives the focus back (giveSheetFocusBack): a tap
 * on one of its controls while it is on top counts as taking the focus
 * into it, whether or not that control kept the focus where it was (a
 * picker's rows do, PickerRow). Returns what to call as it closes.
 */
export function followSheetFocus(): () => void {
  const entry = { opener: focusedOpener(), tappedInside: false };
  followed.push(entry);
  const tap = (event: MouseEvent) => {
    if (followed.at(-1) !== entry || !(event.target instanceof Element)) return;
    const control = event.target.closest(TAKES_FOCUS);
    const sheet = control?.closest(SHEET);
    if (sheet && sheet !== control) entry.tappedInside = true;
  };
  document.addEventListener("click", tap, true);
  return () => {
    document.removeEventListener("click", tap, true);
    const at = followed.indexOf(entry);
    if (at >= 0) followed.splice(at, 1);
    giveSheetFocusBack(entry.opener, entry.tappedInside);
  };
}

/** Whether the sheet on top was opened from `control` or from the table cell it is in: as it closes, it gives the focus back itself. */
export function sheetGivesFocusBackFor(control: HTMLElement | null) {
  const opener = followed.at(-1)?.opener ?? null;
  return Boolean(control && opener && placeOf(opener) === placeOf(control));
}
