// A control locked while its request is out (`disabled={busy}`) drops the
// browser's focus onto the page (2026-10-27: a wrong setup code, a missing
// mail host or a wrong two-step code left the red line with focus on the
// body, so fixing it took another click). When the controls come back and
// focus is still nowhere, it goes back where the person was: the field marked
// invalid first (in its form or dialog), then the field they last typed in
// within the control's form, then the control itself. A step that moved on
// focuses its own first field and is left alone.
//
// A control that stays locked once the page has gone quiet and no request is
// out (2026-10-28: "모든 기기에서 로그아웃" with no other device left) passes
// the focus to the control after it, or before it; one whose answer is slow
// waits for it (2026-10-28: a two-step code refused after 7.6 seconds lost
// the focus). A confirmation answered for a control its action then locked
// hands it here too (focusWhenFree); an opener its answer replaced where it
// stood (two-step "끄기" turning into "켜기") passes it to what took its spot.
//
// A value refused at once, nothing locked (2026-10-28: an address that isn't
// one on the desktop app's first screen left the focus on "연결"): the field
// its form or dialog marks invalid just after a control there was pressed
// takes the focus from that control, as after a wait.
//
// A control drawn again as another element is the same control by its mark
// (`data-focus-return`), and keeps the focus it had or was owed: a row's
// page that a move made a page draws its bar anew, and its "…" had the focus
// back from the move window just before it went (2026-11-06: the focus ended
// on the page; Notion's stays on its "…"). The new one takes it when it
// comes, for a moment, while the focus is nowhere else.
//
// Text goes where the focus is. A press that gives the focus to something
// that takes no text, a menu or a button, leaves the browser's selection in
// the page's text behind it, and text put in without its keys (an input
// method's commit, the emoji picker, dictation) went there: Chrome took the
// focus back into that text and wrote in the page while the menu stayed open
// (2026-11-06: with "새로" open, "엔터 시험" went into the page behind). Keys
// typed there already go nowhere; text put in goes the same way.

import { isTextField } from "./menuKeyboard";
import { focusAsByKeys, keyCameLast } from "./lastPress";
import { requestsInFlight } from "./requestDeadline";
import { shutBehindModal } from "./modalFocus";

const ENTRY = "input:not([type=button]):not([type=submit]):not([type=reset]):not([type=checkbox]):not([type=radio]):not([type=hidden]), textarea, select";
const SCOPE = "form, [role=dialog], dialog";
const CONTROL = "a[href], button, input, select, textarea, [tabindex]";
// How long the page stays unchanged before a still-locked control counts as
// staying locked, and how long a page that keeps changing is waited for.
const QUIET_MS = 1200;
const LONGEST_MS = 5000;
// How soon after a press a field marked invalid counts as that press refused.
const REFUSED_MS = 1500;

let focused: HTMLElement | null = null;
let entry: HTMLElement | null = null;
// The field typed in or the switch turned last, for a save bar gone with its answer.
let changed: HTMLElement | null = null;
let dropped: HTMLElement | null = null;
// Where the dropped control stood, kept for an opener (focusWhenFree).
let droppedSpot: Spot | null = null;
// The dialog the dropped control was in: a save button that goes once its
// answer comes (a settings save bar with nothing left to say) gives the focus
// back to the field typed in there (2026-11-05: it went to the settings search).
let droppedDialog: Element | null = null;
let pending = 0;
let watching = false;
let settling: MutationObserver | null = null;
let quiet = 0;
let longest = 0;
let pressed: { control: HTMLElement; at: number } | null = null;

function locked(element: Element) {
  return element.matches(":disabled") || shutBehindModal(element);
}

function usable(element: HTMLElement | null | undefined): element is HTMLElement {
  return Boolean(element?.isConnected) && !locked(element as HTMLElement);
}

// Focus is lost when it sits on nothing, or on a container of the control
// that had it (a dialog that took it back when its field was locked).
function lostFrom(control: HTMLElement) {
  const active = document.activeElement;
  return !active || active === document.body || active === document.documentElement || active === control
    || (active.contains(control) && active !== control);
}

// A dialog taking the focus to itself while a control in it is owed it (its
// own keeper, as its locked control let go of it) leaves that owed: the
// control takes it back once free (2026-11-13: the share window took it from
// a role picked by a press, the role's change out).
function remember(event: FocusEvent) {
  if (!(event.target instanceof HTMLElement)) return;
  if (dropped?.isConnected && event.target !== dropped && event.target.contains(dropped)) return;
  focused = event.target;
  drop(null);
}

// A field counts as typed in once something is typed there, not when it only
// had the focus (2026-11-05: the settings search, focused as the window
// opened, took the focus back from a device row's sign-out that went). A
// switch or box turned is where the person was the same way (2026-11-06: a
// backup schedule only switched on and saved sent the focus to the keep
// count below it, a number field the arrow keys change).
const TURNED = "[role=switch], [role=checkbox], [role=radio], input[type=checkbox], input[type=radio]";
function typed(event: Event) {
  if (event.target instanceof HTMLElement && event.target.matches(ENTRY)) entry = changed = event.target;
}

function drop(control: HTMLElement | null, spot: Spot | null = null) {
  dropped = control;
  droppedSpot = spot;
  droppedDialog = control?.closest("[role=dialog], dialog") ?? null;
  if (control) awaitQuiet();
  else stopWaiting();
}

function nowhere() {
  const active = document.activeElement;
  return !active || active === document.body || active === document.documentElement;
}

/** A control's place among the controls of its parent. */
// In a dialog, also its place in each container up to the dialog: a row
// taken out with its controls (2026-11-06: an agent's 연결 해제 in settings
// sent the focus to the settings search) still has the list it stood in.
export type Spot = { parent: Element; index: number; outer: { parent: Element; index: number }[] };

/** The controls in a container that take the focus and are drawn, in order. */
export function spotControls(parent: Element) {
  return [...parent.querySelectorAll<HTMLElement>(CONTROL)].filter((element) => element.tabIndex >= 0 && element.getClientRects().length > 0);
}

/** Where a control in a dialog stands, for what may take its place. On the
 * page a control that went leaves the focus there, as Notion's does — but
 * one an answer put back (`anywhere`, an inline confirmation's keep). */
export function spotOf(control: HTMLElement, anywhere = false): Spot | null {
  const parent = control.parentElement;
  const dialog = control.closest("[role=dialog], dialog");
  if (!parent || (!anywhere && !dialog)) return null;
  const outer: Spot["outer"] = [];
  for (let up = parent.parentElement; dialog && up && dialog.contains(up); up = up.parentElement) {
    outer.push({ parent: up, index: Math.max(0, spotControls(up).indexOf(control)) });
  }
  return { parent, index: Math.max(0, spotControls(parent).indexOf(control)), outer };
}

/** What stands where a focused control went out of `root` with what held
 * it (the container it left and what came before it there): the first free
 * control after that place in that container, else the last before it, else
 * the same a container further out (2026-11-06: "에이전트 연결" drew the
 * agent's words in its place and the focus went to the settings search). */
export function standInWhereGone(root: Element, parent: Node, before: Node | null) {
  let mark = before?.isConnected ? before : null;
  for (let scope = parent instanceof Element ? parent : null; scope && root.contains(scope); mark = scope, scope = scope.parentElement) {
    const controls = spotControls(scope).filter((control) => !locked(control));
    const after = controls.find((control) => !mark
      || (!mark.contains(control) && Boolean(mark.compareDocumentPosition(control) & Node.DOCUMENT_POSITION_FOLLOWING)));
    if (after) return after;
    if (controls.length) return controls[controls.length - 1];
  }
  return null;
}

/** The control standing where one stood, in the nearest of its containers
 * still drawn: the next row's, or the one before when the last row went. */
export function standIn(spot: Spot | null, nearOnly = false) {
  for (const { parent, index } of spot ? [spot, ...(nearOnly ? [] : spot.outer)] : []) {
    if (!parent.isConnected) continue;
    const controls = spotControls(parent);
    const stand = controls[Math.min(index, controls.length - 1)];
    if (stand) return stand;
  }
  return null;
}

// The opener owed the focus went from its spot: what stands there now takes
// it, while the focus is still nowhere.
function handOn() {
  // Where it stood, then the field typed in its dialog (a save bar gone with
  // its answer), then its place in the containers around it (a row gone).
  const stand = standIn(droppedSpot, true) ?? typedIn() ?? standIn(droppedSpot);
  drop(null);
  if (stand && nowhere()) focusWhenFree(stand);
}

// The field last typed in or switch last turned, still there in the gone
// control's own group (its form or section): a field elsewhere in the dialog
// is not where the person was (2026-11-06: a backup's "취소" that went with
// its answer sent the focus to the settings search, typed in to find the
// page, and then to the schedule's switch two sections up, out of sight).
const GROUP = "form, section, fieldset, [role=group]";
function typedIn() {
  const field = changed;
  if (!droppedDialog?.isConnected || !field || !usable(field) || !droppedSpot) return null;
  const standing = [droppedSpot.parent, ...droppedSpot.outer.map(({ parent }) => parent)].find((part) => part.isConnected);
  const group = standing?.closest(GROUP);
  return group && group !== droppedDialog && droppedDialog.contains(group) && group.contains(field) ? field : null;
}

function giveBack() {
  pending = 0;
  const control = dropped;
  if (!control) return;
  if (!control.isConnected) {
    handOn();
    return;
  }
  if (!lostFrom(control)) {
    drop(null);
    return;
  }
  const scope = control.closest<HTMLElement>(SCOPE);
  const form = control.closest("form");
  const invalid = scope?.querySelector<HTMLElement>("[aria-invalid=true]");
  const target = [invalid, entry && form?.contains(entry) ? entry : null, control].find(usable);
  // Still locked: it comes back with the rest.
  if (!target) return;
  drop(null);
  // The control itself had the focus where it stands, so taking it again
  // leaves the view where it is: a window's "닫기" locked while its list came
  // was scrolled to at the window's foot (2026-11-06: 앱 둘러보기 opened at its
  // last card). A field to fix is brought into view.
  target.focus(target === control ? { preventScroll: true } : undefined);
}

function shown(element: HTMLElement) {
  return element.tabIndex >= 0 && element.getClientRects().length > 0 && usable(element);
}

// The quiet page settles a control still locked: the next control in Tab
// order within its own group (its form or section, else its dialog) takes
// the focus, else the one before — a field in another section is not where
// the person was, and on a phone its keyboard comes up (2026-11-21: a
// restore's confirmation, its backup's button locked by the restore it asked
// for, gave the focus to "다른 서버로 옮기기"'s code field). While its answer may
// still come (a request out, its part of the page busy) or nothing near it
// is free, it waits on, still owed the focus.
function settle() {
  if (quiet) window.clearTimeout(quiet);
  quiet = 0;
  const control = dropped;
  if (control && !control.isConnected) {
    handOn();
    return;
  }
  if (!control || !lostFrom(control)) {
    drop(null);
    return;
  }
  // Unlocked: its own unlock gives the focus back.
  if (!locked(control)) {
    stopWaiting();
    return;
  }
  const target = requestsInFlight() > 0 || control.closest("[aria-busy=true]") ? null : neighbour(control);
  if (!target) {
    quiet = window.setTimeout(settle, QUIET_MS);
    return;
  }
  drop(null);
  target.focus({ preventScroll: true });
}

function neighbour(control: HTMLElement) {
  const within = control.closest<HTMLElement>("[role=dialog], dialog") ?? document.body;
  const group = control.closest<HTMLElement>(GROUP);
  const scope = group && group !== within && within.contains(group) ? group : within;
  const controls = [...scope.querySelectorAll<HTMLElement>(CONTROL)];
  const after = controls.find((element) => control.compareDocumentPosition(element) & Node.DOCUMENT_POSITION_FOLLOWING && shown(element));
  return after ?? controls.reverse().find((element) => control.compareDocumentPosition(element) & Node.DOCUMENT_POSITION_PRECEDING && shown(element));
}

function awaitQuiet() {
  if (!settling) {
    settling = new MutationObserver(() => {
      if (dropped && !dropped.isConnected && !pending) pending = window.requestAnimationFrame(giveBack);
      if (quiet) window.clearTimeout(quiet);
      quiet = window.setTimeout(settle, QUIET_MS);
    });
    settling.observe(document.documentElement, { subtree: true, childList: true, attributes: true, characterData: true });
    longest = window.setTimeout(settle, LONGEST_MS);
  }
  if (quiet) window.clearTimeout(quiet);
  quiet = window.setTimeout(settle, QUIET_MS);
}

function stopWaiting() {
  settling?.disconnect();
  settling = null;
  if (quiet) window.clearTimeout(quiet);
  if (longest) window.clearTimeout(longest);
  quiet = 0;
  longest = 0;
}

function press(event: MouseEvent) {
  const control = event.target instanceof Element ? event.target.closest<HTMLElement>("button, [role=button], input[type=submit]") : null;
  pressed = control ? { control, at: performance.now() } : null;
  const turned = event.target instanceof Element ? event.target.closest<HTMLElement>(TURNED) : null;
  if (turned) changed = turned;
}

// A field marked invalid right after a press in its form or dialog, while
// the focus stays on the pressed control or went nowhere.
function refused(field: HTMLElement) {
  const last = pressed;
  if (!last || performance.now() - last.at > REFUSED_MS || field.getAttribute("aria-invalid") !== "true") return;
  if (!last.control.closest(SCOPE)?.contains(field) || !lostFrom(last.control) || !usable(field)) return;
  pressed = null;
  field.focus();
}

// A form sent again by its button with its field still marked from the last
// refusal (2026-10-28: a weak password sent twice kept the focus on "비밀번호
// 정하기"): nothing changes to watch, so once the form has answered, a marked
// field takes the focus from the button still holding it.
function submitted(event: SubmitEvent) {
  const form = event.target instanceof HTMLFormElement ? event.target : null;
  const control = event.submitter instanceof HTMLElement ? event.submitter : null;
  if (!form || !control) return;
  window.requestAnimationFrame(() => window.requestAnimationFrame(() => {
    const field = form.querySelector<HTMLElement>("[aria-invalid=true]");
    if (field && usable(field) && control.isConnected && !locked(control) && lostFrom(control)) field.focus();
  }));
}

function watch(records: MutationRecord[]) {
  let unlocked = false;
  for (const record of records) {
    const element = record.target;
    if (!(element instanceof HTMLElement)) continue;
    if (record.attributeName === "aria-invalid") {
      refused(element);
    } else if (element.hasAttribute("disabled")) {
      if (element === focused && lostFrom(element)) drop(element, spotOf(element));
    } else {
      unlocked = true;
    }
  }
  // After this frame's own focusing (a new step's autofocus) has run.
  if (unlocked && dropped && !pending) pending = window.requestAnimationFrame(giveBack);
}

/** Whether focus is about to come back to a control in `scope` its request
 * locked: a view that changed with it leaves the focus to this. */
export function focusComingBack(scope: Element) {
  return Boolean(dropped?.isConnected && scope.contains(dropped) && !locked(dropped));
}

/** Whether a control in `scope`, or one replaced there, is still owed the
 * focus: a layer's own keeper leaves the focus to it. */
export function focusOwed(scope: Element) {
  if (!dropped) return false;
  return dropped.isConnected ? scope.contains(dropped)
    : Boolean((droppedSpot && [droppedSpot, ...droppedSpot.outer].some(({ parent }) => parent.isConnected && scope.contains(parent)))
      || (typedIn() && scope.contains(droppedDialog)));
}

/** The control that had the focus when its request locked it, still owed
 * it while the focus is nowhere else: a question that request asks before it
 * ends (a refusal's notice) comes from there, and gives the focus back to it. */
export function lockedOpener(): HTMLElement | null {
  return dropped?.isConnected && lostFrom(dropped) ? dropped : null;
}

/** Focuses a control now, or once its request unlocks it: focus going back
 * to the opener of a confirmation whose answer locked it. One replaced in
 * the meantime hands it to what stands in its spot. */
export function focusWhenFree(control: HTMLElement, ring?: boolean) {
  if (!control.isConnected || typeof window === "undefined") return;
  if (!locked(control)) {
    // Its ring shows when a key came last, in WebKit too (lastPress.ts), or, with its tip, where its layer says so (ringsAfterPress).
    if (ring) focusAsByKeys(control, { preventScroll: true });
    else control.focus({ preventScroll: true, focusVisible: ring ?? keyCameLast() } as FocusOptions);
    return;
  }
  focused = control;
  drop(control, spotOf(control));
}

/** Starts giving focus back to a field locked while its request was out. */
const REDRAWN_WAIT_MS = 3000;

/** The one other control that bears this control's mark, if it is drawn. */
export function redrawnControl(control: HTMLElement | null): HTMLElement | null {
  const mark = control?.dataset.focusReturn;
  if (!mark) return null;
  const found = [...document.querySelectorAll<HTMLElement>(`[data-focus-return="${CSS.escape(mark)}"]`)]
    .filter((element) => element !== control);
  return found.length === 1 ? found[0] : null;
}

/** Gives the focus to the control drawn in this one's place once it has gone. */
export function followRedrawn(control: HTMLElement | null) {
  if (!control?.dataset.focusReturn) return;
  const follow = () => {
    if (control.isConnected) return;
    const active = document.activeElement;
    if (active && active !== document.body) { done(); return; }
    const again = redrawnControl(control);
    if (again) { done(); again.focus({ preventScroll: true }); }
  };
  const observer = new MutationObserver(follow);
  const timer = window.setTimeout(() => observer.disconnect(), REDRAWN_WAIT_MS);
  const done = () => { observer.disconnect(); window.clearTimeout(timer); };
  observer.observe(document.body, { childList: true, subtree: true });
  follow();
}

// A marked control that loses the focus to nothing hands it to its redrawn self.
function leftForNothing(event: FocusEvent) {
  if (!event.relatedTarget && event.target instanceof HTMLElement) followRedrawn(event.target);
}

function textWithoutFocus(event: InputEvent) {
  const target = event.target;
  if (target instanceof HTMLElement && (target.isContentEditable || isTextField(target))) return;
  if (event.cancelable) event.preventDefault();
}

export function watchFocusThroughWaits() {
  if (watching || typeof window === "undefined" || typeof MutationObserver === "undefined") return;
  watching = true;
  document.addEventListener("focusout", leftForNothing, true);
  window.addEventListener("beforeinput", textWithoutFocus, { capture: true });
  document.addEventListener("focusin", remember, true);
  document.addEventListener("input", typed, true);
  document.addEventListener("click", press, true);
  document.addEventListener("submit", submitted, true);
  new MutationObserver(watch).observe(document.documentElement, { attributes: true, attributeFilter: ["disabled", "aria-invalid"], subtree: true });
}
