import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, type RefObject } from "react";
import { setInteractionModality } from "react-aria";
import { isMenuMoveKey } from "./menuKeyboard";
import { keysLeadNow } from "./menuInputLead";
import { pointerStill } from "./pointerHover";

/**
 * Whether a submenu's row is held from opening it by hover: one that closed
 * with the pointer resting on its row (Escape, the keys' ←) stays closed while
 * the pointer stays there, moved within the row or not; once the pointer has
 * really moved out of the row, coming back in opens it again — Notion's (seen
 * 2026-11-18 at 1440 in Chrome, no script before the action: "단축키 추가 ›
 * 새 페이지 ›" pressed open, Escape, the pointer moved 12px along the row, the
 * list stayed closed; moved on into "페이지 ›", that one opened). Chrome tells
 * the row a leave and a fresh enter, the pointer still, as the closed list's
 * layers go from over the page; that enter had opened the list again at once.
 */
export function useSubmenuHoverHold(open: boolean, triggerRef: RefObject<HTMLElement | null>) {
  const held = useRef(false), wasOpen = useRef(open);
  useLayoutEffect(() => {
    const closed = wasOpen.current && !open;
    wasOpen.current = open;
    if (open) { held.current = false; return undefined; }
    if (!closed || !triggerRef.current?.matches(":hover")) return undefined;
    held.current = true;
    const release = (event: PointerEvent) => {
      if (!event.movementX && !event.movementY) return;
      const row = triggerRef.current;
      if (row && event.target instanceof Node && row.contains(event.target)) return;
      held.current = false;
      document.removeEventListener("pointermove", release, true);
    };
    document.addEventListener("pointermove", release, true);
    return () => document.removeEventListener("pointermove", release, true);
  }, [open, triggerRef]);
  return held;
}

/**
 * A list open beside its row gives way as the pointer moves on into another
 * row of the menu it opened from: that row takes the highlight, the list
 * closes once the pointer has rested there (useSubmenuSettledClose), and the
 * focus the list held goes back at once (`focusBack`; unset, to its row,
 * which hands it to the menu's owner) — whatever was typed in the list
 * meanwhile. Notion's (seen 2026-11-18 at 1440 in Chrome in two tabs, judged
 * by the screen with no script before the action: the sidebar's "섹션 추가 ›
 * 팀스페이스 ›" pressed open, "z" typed — to nowhere the first time it opens,
 * into its search after — the pointer moved onto "최근 항목": the list closed
 * and the row lit, a letter typed then going nowhere, ↓ moving the menu's
 * highlight; the same after ↓ in the list). Aria lights a row the pointer
 * comes into only while the pointer leads, and any key ends that until the
 * pointer has moved again: the row stayed unlit and the list open.
 *
 * The keys that moved the menu's own highlight still hold it at the pointer's
 * first move, which only shades the row it lands on, the next row taking it —
 * a block's "⋮⋮" menu, "전환 ›" opened by ↓ and →, ↓ in it, the pointer moved
 * onto "복제": the list stayed; moved on onto "옮기기": it closed, the menu's
 * search taking the focus (seen 2026-11-18 at 1440, once).
 */
export function useSubmenuOtherRowTakes(open: boolean, { parentMenuRef, submenuRef, triggerRef, focusBack }: {
  parentMenuRef: RefObject<HTMLElement | null>; submenuRef: RefObject<HTMLElement | null>; triggerRef: RefObject<HTMLElement | null>;
  focusBack?: () => void;
}) {
  const back = useRef(focusBack);
  back.current = focusBack;
  useEffect(() => {
    if (!open) return undefined;
    // Whether the keys last moved the menu's highlight, the pointer not moved in it since.
    let held = keysLeadNow();
    const inMenu = (target: EventTarget | null) => target instanceof Node && Boolean(parentMenuRef.current?.contains(target))
      && !submenuRef.current?.contains(target);
    const key = (event: KeyboardEvent) => { if (isMenuMoveKey(event.key) && inMenu(event.target)) held = true; };
    const moved = (event: PointerEvent) => { if ((event.movementX || event.movementY) && inMenu(event.target)) held = false; };
    // React tells a row the pointer came in as it hears the pointer leave what it was on, before the row's own
    // "over": both are heard here, ahead of it.
    const came = (event: PointerEvent) => {
      const [to, from] = event.type === "pointerout" ? [event.relatedTarget, event.target] : [event.target, event.relatedTarget];
      const menu = parentMenuRef.current, row = to instanceof Element ? to.closest<HTMLElement>("[data-key]") : null;
      if (held || !menu || !row || row === triggerRef.current || !inMenu(row) || event.pointerType === "touch" || pointerStill(event)) return;
      if (row.getAttribute("aria-disabled") === "true" || from instanceof Node && row.contains(from)) return;
      const active = document.activeElement;
      if (active && active !== document.body && !menu.contains(active)) {
        if (back.current) back.current(); else triggerRef.current?.focus({ preventScroll: true });
      }
      // The pointer leads again before the row hears it come in (a focus given by code had made it no one's): Aria
      // lights the row, and closes the list at that.
      setInteractionModality("pointer");
    };
    document.addEventListener("keydown", key, true);
    document.addEventListener("pointermove", moved, true);
    document.addEventListener("pointerover", came, true);
    document.addEventListener("pointerout", came, true);
    return () => {
      document.removeEventListener("keydown", key, true);
      document.removeEventListener("pointermove", moved, true);
      document.removeEventListener("pointerover", came, true);
      document.removeEventListener("pointerout", came, true);
    };
  }, [open, parentMenuRef, submenuRef, triggerRef]);
}

/**
 * How long the pointer rests on a row of a menu before the row's list opens
 * beside it, or the list open there gives way to it: Notion's opens or goes
 * that long after the pointer comes onto the row, and a row passed over
 * sooner does nothing (seen 2026-11-25 in Chrome, two tabs: a database row's
 * "다음에서 열기 ›" and "속성 편집 ›", a page "⋯"'s "테마와 레이아웃 ›" and
 * "알림 받기 ›", a block menu's "복제 ›", "색 ›" and "전환 ›" drew their
 * lists 128–141 ms after the pointer came, none after 69–124 ms on the row;
 * a list left for another row stayed 130 ms, then faded).
 */
export const SUBMENU_SETTLE_MS = 125;

/**
 * A list open beside its row (an Aria submenu, useNativeSubmenu) stays while
 * the pointer moves on into another row of the menu it opened from, and gives
 * way once the pointer has rested on that row SUBMENU_SETTLE_MS — that row's
 * own list opening by the same moment (Aria's hover delay); come back onto
 * its row or into it meanwhile, it stays, and a row passed over sooner
 * starts the moment again. Aria closes it as the pointer lights that row, its
 * focus moving there (a virtual "focusin" the menu hears): that close waits;
 * one the keys, Escape, ← or a press ask for comes at once. Notion's block
 * menu "전환 ›" and "색 ›" and a page "⋯"'s "알림 받기 ›" (seen 2026-11-25 in
 * Chrome, two tabs: left for another row, the list went 113–135 ms after the
 * pointer came onto it; back onto its row 18 ms after leaving it, it stayed;
 * Escape closed it at once). `stay` keeps it, the pointer back on its row.
 */
export function useSubmenuSettledClose<S extends { isOpen: boolean; close(): void }>(state: S, { parentMenuRef, triggerRef, list }: {
  parentMenuRef: RefObject<HTMLElement | null>; triggerRef: RefObject<HTMLElement | null>; list: () => Element | null;
}): S & { stay(): void } {
  const pending = useRef<number | undefined>(undefined);
  // Whether the focus moving now is the pointer lighting another row: set while that "focusin" passes.
  const byPointer = useRef(false);
  const listOf = useRef(list), latest = useRef(state);
  listOf.current = list; latest.current = state;
  const stay = useCallback(() => { window.clearTimeout(pending.current); pending.current = undefined; }, []);
  useEffect(() => stay, [stay]);
  useEffect(() => {
    if (!state.isOpen) { stay(); return undefined; }
    const menu = parentMenuRef.current;
    if (!menu) return undefined;
    // Heard before Aria's own listener on the menu, which closes the list: the focus that comes back to the
    // menu (useSubmenuOtherRowTakes) or the light that moves, the pointer on another of its rows.
    const lights = () => {
      const row = menu.querySelector("[data-key]:hover");
      if (!row || row === triggerRef.current || keysLeadNow()) return;
      byPointer.current = true;
      queueMicrotask(() => { byPointer.current = false; });
    };
    menu.addEventListener("focusin", lights, true);
    return () => menu.removeEventListener("focusin", lights, true);
  }, [state.isOpen, parentMenuRef, triggerRef, stay]);
  return useMemo(() => ({ ...state, stay, close: () => {
    stay();
    if (!byPointer.current) { state.close(); return; }
    pending.current = window.setTimeout(() => {
      pending.current = undefined;
      if (triggerRef.current?.matches(":hover") || listOf.current()?.matches(":hover")) return;
      latest.current.close();
    }, SUBMENU_SETTLE_MS);
  } }), [state, stay, triggerRef]);
}

// The lists open beside rows, by their row, for the pointer on its way to one (pointerHeadsFor).
const besideLists = new WeakMap<Element, Element>();

// The lists noted open beside a row, by their element (arrivalHoldsLight).
const besideListElements = new WeakSet<Element>();

/** Notes the list open beside `row` (RowMenuBesideSubmenu); the returned function forgets it. */
export function noteBesideList(row: Element, list: Element) {
  besideLists.set(row, list);
  besideListElements.add(list);
  return () => { if (besideLists.get(row) === list) besideLists.delete(row); };
}

// The row the pointer first came onto in each list beside a row, while the light the list's opening put on another
// of its rows holds (null once the pointer has moved on, or where the opening lit none).
const arrivals = new WeakMap<Element, Element | null>();
/**
 * Whether `row`, in a list open beside a row that came up with another of
 * its rows lit, is the row the pointer first came onto in it, and is still
 * on: that light holds, the row only shaded under the pointer, and it opens
 * no list of its own however long the pointer rests or moves along it; the
 * next row the pointer comes onto takes the light, and opens its list, as any
 * (useSubmenuHover, NativeMenuItem, NativeActionMenuItem). Notion's
 * (measured 2026-11-27 in Chrome at 1440, dpr 1, in two tabs at different
 * times, judged by the screen after the move: a column menu's "계산 ›"
 * list, "계산 안함" lit, the pointer from "계산" straight onto "수 ›" — "계산
 * 안함" stayed lit, "수" shaded, nothing opened, resting 1 s nor moved along
 * it; onto "계산 안함" first and then "수 ›", its list came 124–128 ms after;
 * a page "⋯"'s "테마와 레이아웃 ›", "기본" lit, straight onto "인라인 댓글 ›"
 * the same). Asked as the pointer comes onto each row.
 */
export function arrivalHoldsLight(row: Element | null | undefined): boolean {
  const list = row?.closest("[data-native-menu]");
  if (!row || !list || !inBesideList(list)) return false;
  if (!arrivals.has(list)) {
    const lit = list.querySelector('[data-focused="true"]');
    arrivals.set(list, lit && lit !== row && !row.contains(lit) ? row : null);
  }
  const held = arrivals.get(list);
  if (held === row) return true;
  if (held) arrivals.set(list, null);
  return false;
}
function inBesideList(element: Element) {
  for (let at: Element | null = element; at; at = at.parentElement) if (besideListElements.has(at)) return true;
  return false;
}

/** The list open beside a row, if one is noted. */
export function besideListOf(row: Element | null | undefined) {
  return row ? besideLists.get(row) ?? null : null;
}

// The pointer's last two places, for every list beside a row (one window listener, added once).
type PointerPlace = { x: number; y: number; at: number };
let pointerFrom: PointerPlace | null = null, pointerAt: PointerPlace | null = null, tracking = false;
// A place the pointer left longer ago than this was where it rested: a move from there is a jump, heading nowhere.
const STILL_MS = 100;
/** Starts keeping the pointer's last places, as a list opens beside a row (pointerHeadsFor). */
export function trackPointer() {
  if (tracking || typeof window === "undefined") return;
  tracking = true;
  window.addEventListener("pointermove", (event) => {
    if (event.pointerType === "touch" || pointerAt && pointerAt.x === event.clientX && pointerAt.y === event.clientY) return;
    pointerFrom = pointerAt;
    pointerAt = { x: event.clientX, y: event.clientY, at: event.timeStamp };
  }, { capture: true, passive: true });
}

// A row crossed on the way is marked so, and shaded (styles.css).
const PASSING = "data-pointer-passing";
const HEADING_RULE = "data-pointer-heading-rule";

/**
 * Whether the pointer, moving from where it last was to (x, y), heads for
 * `list` beside the menu, between the lines to the list's near corners: the
 * one rule every list beside a row keeps open by (useSubmenuHeadedFor, the
 * page menu's lists, PageMenu useSubmenuHover). A move steeper than the line
 * to its far corner isn't on its way (Notion's "섹션 추가 › 팀스페이스 ›",
 * whose list starts level with its row: down at 2:1 onto "채팅" it closed,
 * at 3:2 onto "고정됨" it stayed, seen 2026-11-18), nor one a little
 * steeper than that line, as Notion's keeps no slack past it (seen 2026-11-25
 * in Chrome, two tabs: a database row's "다음에서 열기 ›" list, the pointer
 * moved in 40px steps onto "댓글" 1.3° inside the line to its far corner, and
 * 6° and more inside, kept it; 2.6° and 6° past it closed it — 15° of slack
 * had kept those). Nor is one that comes no nearer the list's side, straight
 * down or up the menu or back from it, however far below or above the list
 * reaches (2026-11-23: a block menu's tall "색 ›" list held a move straight
 * down onto "블록 링크 복사", the list staying till the row after; Notion's
 * closes at the first, seen 2026-10-01).
 */
export function pointerHeadsFor(list: Element, x: number, y: number) {
  trackPointer();
  const moving = pointerAt && (pointerAt.x !== x || pointerAt.y !== y);
  const from = moving ? pointerAt : pointerFrom, to = moving ? null : pointerAt;
  if (!from || from.x === x && from.y === y) return false;
  // From rest in one jump it heads nowhere, as Notion's (the list closed at once).
  if ((to?.at ?? performance.now()) - from.at > STILL_MS) return false;
  const box = list.getBoundingClientRect();
  const right = box.left >= from.x;
  const across = right ? box.left - from.x : from.x - box.right;
  if (across <= 0 || (right ? x - from.x : from.x - x) <= 0) return false;
  const top = Math.atan2(from.y - box.top, across), bottom = Math.atan2(from.y - box.bottom, across);
  const heading = Math.atan2(from.y - y, right ? x - from.x : from.x - x);
  return heading < top && heading > bottom;
}

/**
 * A list open beside its row stays open while the pointer, on its way to the
 * list, crosses another row of the menu: that row isn't told the pointer came
 * (it shades by its own hover, the list's row staying shaded) and the list
 * stays however long the pointer rests there; moving on into another row not
 * on its way to the list hands the pointer to that row as before
 * (useSubmenuOtherRowTakes) — Notion's sidebar "단축키 추가 › 페이지 ›"
 * (seen 2026-11-18 at 1440 in Chrome by worker w1891 twice and again, the
 * pointer moved in steps toward the list onto "채팅 스레드": the list stayed
 * 4 s and more, both rows grey; moved in steps down and away, that row's own
 * list took its place at once; once held, a few px back along the row kept
 * it; one jump from where the pointer rested, the list's place or not, hands
 * the row the pointer at once). A row is told the pointer came by the pointer leaving what it was on
 * (React's pointerout) as well as its own pointerover: both are held back.
 * `list` gives the list's element.
 */
export function useSubmenuHeadedFor(open: boolean, { parentMenuRef, triggerRef, list }: {
  parentMenuRef: RefObject<HTMLElement | null>; triggerRef: RefObject<HTMLElement | null>; list: () => Element | null;
}) {
  const listOf = useRef(list);
  listOf.current = list;
  useEffect(() => {
    if (!open) return undefined;
    trackPointer();
    let held: Element | null = null;
    const hold = (row: Element | null) => {
      if (held === row) return;
      held?.removeAttribute(PASSING);
      held = row;
      held?.setAttribute(PASSING, "");
    };
    const passing = (event: PointerEvent) => {
      if (event.pointerType === "touch") return;
      const to = event.type === "pointerout" ? event.relatedTarget : event.target;
      const menu = parentMenuRef.current, beside = listOf.current(), trigger = triggerRef.current;
      const row = to instanceof Element ? to.closest("[data-key]") : null;
      if (!menu || !beside || !row || !menu.contains(row) || beside.contains(row) || trigger && (row === trigger || row.contains(trigger))) {
        hold(null);
        return;
      }
      if (held !== row) hold(pointerHeadsFor(beside, event.clientX, event.clientY) ? row : null);
      if (held === row) event.stopImmediatePropagation();
    };
    window.addEventListener("pointerover", passing, true);
    window.addEventListener("pointerout", passing, true);
    // This rule, not Aria's own safe triangle, decides (styles.css): that one took the pointer from the menu's
    // rows while it moved toward the list, held it after a jump from rest, and let it go after a second.
    const menu = parentMenuRef.current;
    menu?.setAttribute(HEADING_RULE, "");
    return () => {
      menu?.removeAttribute(HEADING_RULE);
      hold(null);
      window.removeEventListener("pointerover", passing, true);
      window.removeEventListener("pointerout", passing, true);
    };
  }, [open, parentMenuRef, triggerRef]);
}
