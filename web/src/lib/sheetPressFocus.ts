import { appleTouchDevice } from "./iosViewport";
import { keyCameLast, lastPressedControl } from "./lastPress";

// Text being written keeps the focus a sheet over it gives back (its caret, a field's words).
const TYPING = 'input:not([type="checkbox"], [type="radio"], [type="button"], [type="submit"]), textarea, select';

/** Whether a press rather than a key closed a sheet opened from `opener`, which holds no text being written. */
export function sheetClosedByPress(opener: HTMLElement | null) {
  return !keyCameLast() && Boolean(opener) && !opener?.isContentEditable && !opener?.matches(TYPING);
}

/**
 * Whether a phone sheet a press closed leaves the focus on the page rather
 * than giving it back to its opener, as Notion's (sheetFocusReturn.ts): on an
 * iPhone or iPad always, a tap there focusing nothing, its opener neither; on
 * Android when the press was a tap on a control in the sheet — its "완료", a
 * row — which took the focus from the opener and takes it away with the sheet,
 * where the shade and a drag take none (seen 2026-11-17 on a Galaxy A90, no
 * script before the action, a letter typed after it: the share sheet's "완료"
 * left it on the body, its shade on "공유", ringed by the letter, each in two
 * tabs). `focused` is where the focus was as it closed. A layer drawn as such
 * a sheet (the share window on a phone) gives the focus back by this too.
 */
export function sheetPressLeavesPage(sheet: Element | null, focused: Element | null, opener: HTMLElement | null) {
  if (!sheet?.matches("[data-phone-sheet]") || !sheetClosedByPress(opener)) return false;
  const tapped = lastPressedControl();
  return appleTouchDevice() || Boolean(tapped && tapped === focused && sheet.contains(tapped));
}

/**
 * Where a layer drawn as a phone sheet — a PhoneSheet, or a window drawn as
 * one (phoneWindowSheet.ts) — opened by a press leaves the focus while it
 * stays open: where the press left it, taking none in and shutting none from
 * it, as Notion's Android sheets do and a PhoneSheet of its own does (seen
 * 2026-11-17 on a Galaxy A90 in Chrome, no script before the action, a
 * letter typed after it ringing the opener: the share sheet's "공유" in two
 * tabs at two times, a page's "작업" sheet's "⋯", a new teamspace window's
 * "새 팀스페이스" in two tabs at two times, whose icon a desktop's takes).
 * The control the press focused, Android's; the body where it focused none,
 * an iPhone's or iPad's (Notion's there not measured: its sheet moving none
 * in on Android, it moves none there either). Until the next
 * press or key, a focus its own parts take as it opens — React Aria's dialog,
 * a window's first control — goes back there. Null where it takes the focus
 * in as a window does: opened by a key, from text being written, or for
 * typing (`types`).
 */
export function focusLeftBySheet(root: HTMLElement | null, types: boolean): HTMLElement | null {
  const active = document.activeElement;
  if (types || keyCameLast() || !root?.matches('[data-phone-sheet], [data-phone-window="sheet"]')) return null;
  const left = !(active instanceof HTMLElement) || active === document.body ? document.body : active;
  if (left !== document.body && (root.contains(left) || left.isContentEditable || left.matches(TYPING))) return null;
  const back = (event: FocusEvent) => {
    if (!root.isConnected) end();
    else if (event.target instanceof HTMLElement && root.contains(event.target)) {
      if (left === document.body || !left.isConnected) event.target.blur();
      else left.focus({ preventScroll: true });
    }
  };
  const end = () => {
    document.removeEventListener("focusin", back, true);
    document.removeEventListener("pointerdown", end, true);
    document.removeEventListener("keydown", end, true);
  };
  document.addEventListener("focusin", back, true);
  document.addEventListener("pointerdown", end, true);
  document.addEventListener("keydown", end, true);
  return left;
}
