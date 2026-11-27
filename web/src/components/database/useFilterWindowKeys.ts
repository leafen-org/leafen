import { useEffect, useRef, type MouseEvent, type RefObject } from "react";
import { keyCameLast } from "@/lib/lastPress";
import { giveFocusBack } from "@/lib/focusGiveBack";
import { fieldEnter, fieldEscape, imeComposing } from "@/lib/fieldKeyBoundary";
import { inlineEndKey, inlineStartKey } from "@/lib/readingDirection";

/** A rule's row; its "+ 필터 규칙 추가"; a group's "⋯" beside the group — each a row of the window's keys. */
const ROWS = "[data-filter-row], [data-filter-add], [data-filter-add-rule], [data-filter-actions]";
/** In a rule's row, its choices, its value and its "⋯", in their order; the join word before it is none of them. */
const STOPS = "[data-notion-select-button], [data-filter-value-cell], [data-filter-actions]";
const LIT = "data-filter-key-lit";

/**
 * The keys of an advanced filter's window, as Notion's (seen 2026-11-24 in
 * Chrome at 1685, dark, dpr 1, in two tabs at different times, one and two
 * rules, judged by the screen after each key with no script in the page
 * before it): the window takes the arrows while it is open, wherever the
 * focus stood — Notion's never moved into it — and lights one control with
 * its pointer look, nothing at first. ↓ or ↑ then lights the first rule's
 * property; ↓ and ↑ go a row at a time, a rule's row to the next rule's and
 * on to "+ 필터 규칙 추가", where ↓ stops ("필터 제거" under the line is no
 * row of them), the column kept (a rule's property under the one above; once,
 * in a one-rule window, ↑ from "+ 필터 규칙 추가" lit the condition instead);
 * ← and → go along a rule's row — property, condition, value, "⋯" — and stop
 * at its ends. Enter presses the lit control: a choice opens its list, the
 * "⋯" its menu, the value its field, which takes the caret. Escape closes
 * what Enter opened, the control staying lit; then unlights it — from the
 * field, at once, the field letting the focus go to nothing, a letter typed
 * next landing nowhere — and never closes the window, which a press outside
 * it does.
 *
 * The focus stays where it was as the window opened (seen 2026-11-24 in two
 * tabs at different times, judged after each key by the screen and the next
 * Tab or letter): on the toolbar's "필터" when its menu's "고급 필터 추가"
 * opened it, by the pointer or by Tab and Enter, and on "+ 필터 규칙 추가"
 * after its menu added a rule — each ringed once keys lead (lastPress: at
 * once after Enter, at the first Escape after a press), the window open, a
 * Tab going on to the next toolbar button; the pill takes none. So the window
 * listens on the document, ahead of the control holding the focus, and takes
 * the arrows and Escape from it; Enter stays that control's — "필터" with
 * "+ 필터 규칙 추가" lit hid its bar and the window with it, seen twice —
 * and presses the lit one only with the focus on nothing.
 */
export function useFilterWindowKeys(windowRef: RefObject<HTMLElement | null>, anchor: RefObject<HTMLElement | null>) {
  const lit = useRef<HTMLElement | null>(null), column = useRef(0);

  const light = (next: HTMLElement | null) => {
    lit.current?.removeAttribute(LIT);
    lit.current = next;
    if (!next) return;
    next.setAttribute(LIT, "");
    next.scrollIntoView({ block: "nearest", inline: "nearest" });
  };

  const rows = () => {
    const root = windowRef.current;
    if (!root) return [];
    return [...root.querySelectorAll<HTMLElement>(ROWS)]
      // A rule's "⋯" is a control of its row; a group's, beside the group, is a row of its own.
      .filter((row) => !(row.matches("[data-filter-actions]") && row.parentElement?.closest("[data-filter-row]")))
      .map((row) => row.matches("[data-filter-row]")
        ? [...row.querySelectorAll<HTMLElement>(STOPS)].filter((stop) => !stop.parentElement?.closest("[data-filter-value-cell]")
          && stop.closest("[data-filter-row]") === row && stop.getClientRects().length > 0)
        : [row])
      .filter((stops) => stops.length > 0);
  };

  const press = (stop: HTMLElement) => {
    if (!stop.matches("[data-filter-value-cell]")) { stop.click(); return; }
    const field = stop.querySelector<HTMLElement>("input:not(:disabled), textarea:not(:disabled)");
    if (field) field.focus();
    else stop.querySelector<HTMLElement>("button:not(:disabled)")?.click();
  };

  /**
   * Tab moves nothing while the window is open, with the focus on nothing, on
   * the pill it hangs from or on one of its own controls, as Notion's (seen
   * 2026-11-24 at 1685 dark, dpr 1, in two tabs at different times, judged by
   * the screen and the focus after the key: the pill pressed, then Tab from
   * nothing, from the pill ringed by Shift+Tab, from "+ 필터 규칙 추가" and a
   * rule's property ringed by Escape, and a third time from a rule's "⋯" — the
   * focus and its ring stayed). Shift+Tab goes as the browser takes it — from
   * nothing to the pill, ringed, in both — and so does a Tab from a value's
   * field, on to its rule's "⋯" (seen once), or from another control, the
   * toolbar's "필터", on to "정렬".
   */
  const holdsTab = (event: KeyboardEvent, target: Element, inside: boolean) => event.key === "Tab"
    && !event.shiftKey && !event.altKey && !event.ctrlKey && !event.metaKey
    && (inside ? !target.matches("input, textarea, [contenteditable='true'], [role='textbox']")
      : target === document.body || target === document.documentElement || Boolean(anchor.current?.contains(target)));

  /** Escape at the window, its layer's too: a field gives the keys back, and the lit control goes dark. */
  const escape = () => {
    const root = windowRef.current, active = document.activeElement;
    if (root && active instanceof HTMLElement && root.contains(active) && active.matches("input, textarea")) active.blur();
    light(null);
  };

  const keyDown = (event: KeyboardEvent) => {
    const root = windowRef.current, target = event.target instanceof Element ? event.target : null;
    if (!root || !target || event.defaultPrevented || imeComposing(event)) return;
    // A list or menu a control opened takes all its keys — its Escape closes it, the control staying lit.
    if (root.querySelector('[aria-expanded="true"]')) return;
    const inside = root.contains(target);
    if (holdsTab(event, target, inside)) { event.preventDefault(); return; }
    if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
    // Text written outside, or a window, menu or list of its own — one not holding the pill the window hangs from, as a
    // view's settings do — keeps its keys.
    const own = target.closest('[role="dialog"], [role="alertdialog"], [role="menu"], [role="listbox"]');
    if (!inside && (target.matches("input, textarea, select, [contenteditable='true'], [role='textbox']")
      || own && !(anchor.current && own.contains(anchor.current)))) return;
    const take = () => { event.preventDefault(); event.stopPropagation(); };
    if (fieldEscape(event)) {
      take();
      escape();
      return;
    }
    // A field keeps its arrows and Enter for its caret and its words.
    if (inside && target.matches("input, textarea, [contenteditable='true']")) return;
    const current = lit.current?.isConnected ? lit.current : null;
    if (fieldEnter(event)) {
      if (!current || ![document.body, document.documentElement, root].some((holder) => holder === target)) return;
      take();
      press(current);
      return;
    }
    const start = inlineStartKey(root), end = inlineEndKey(root);
    if (!["ArrowDown", "ArrowUp", start, end].includes(event.key)) return;
    take();
    const grid = rows();
    if (!grid.length) return;
    const at = current ? grid.findIndex((stops) => stops.includes(current)) : -1;
    if (at < 0) {
      if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
      column.current = 0;
      light(grid[0][0]);
      return;
    }
    const stops = grid[at], place = stops.indexOf(current!);
    if (event.key === start || event.key === end) {
      const next = stops[place + (event.key === end ? 1 : -1)];
      if (!next) return;
      column.current = place + (event.key === end ? 1 : -1);
      light(next);
      return;
    }
    const row = grid[at + (event.key === "ArrowDown" ? 1 : -1)];
    if (!row) return;
    if (stops.length > 1) column.current = place;
    light(row[Math.min(column.current, row.length - 1)]);
  };
  const keyDownRef = useRef(keyDown);
  keyDownRef.current = keyDown;

  // Ahead of whatever holds the focus, on the page or in the toolbar: the arrows are the window's, not theirs to scroll or move by.
  useEffect(() => {
    const listener = (event: KeyboardEvent) => keyDownRef.current(event);
    document.addEventListener("keydown", listener, true);
    return () => {
      document.removeEventListener("keydown", listener, true);
      lit.current?.removeAttribute(LIT);
      lit.current = null;
    };
  }, []);

  return { escape };
}

/**
 * The view bar's advanced filter pill, pressed: it takes no focus, which
 * stays where it was — the toolbar's "필터" ringed still — and text being
 * written lets it go to nothing, as Notion's pill (seen 2026-11-24 in two
 * tabs: no ring on it at the window's Escape, the caret gone from the line
 * it was in, a letter typed next landing nowhere). Opened by its keys, it
 * keeps the focus.
 */
export function advancedPillPress(open: boolean, toggle: () => void) {
  return {
    onMouseDown: (event: MouseEvent) => { if (!open) event.preventDefault(); },
    onClick: () => {
      const active = document.activeElement;
      if (!open && !keyCameLast() && active instanceof HTMLElement && (active.isContentEditable || active.matches("input, textarea"))) active.blur();
      toggle();
    },
  };
}

/**
 * The toolbar's "고급 필터 추가" leaves the focus on "필터" while the window
 * is open, its menu giving it back, as Notion's (seen 2026-11-24 in two tabs,
 * by the pointer and by Tab and Enter); the window takes the keys ahead of it.
 */
export function keepFocusBehindWindow(from: HTMLElement | null) {
  if (from) giveFocusBack({ to: from, keepScroll: true, when: () => {
    const active = document.activeElement;
    return !active || active === document.body;
  } });
}
