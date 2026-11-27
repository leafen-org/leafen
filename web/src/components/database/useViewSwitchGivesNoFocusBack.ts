import { useEffect, useLayoutEffect, useRef } from "react";

// How long after a switch of the view a focus given back to what held it in
// the view before is a closing window's: it gives it back within a frame or two.
const GIVEN_BACK_MS = 1000;

/**
 * A window closing on another view than it opened over gives the focus no
 * way back into the view's box (`panelId`): what held the focus there before
 * the switch — a table's or a timeline's chosen cell, a card — stays behind
 * and the focus goes nowhere, as Notion's (seen 2026-11-26 in Chrome at
 * 1440, two tabs at different times, after the choice only: a table's cell
 * chosen, "N개 더 보기" opened over it and another table view chosen there,
 * no cell chosen and a letter going nowhere). What a press or a key focuses
 * after the switch is the person's own.
 */
export function useViewSwitchGivesNoFocusBack(panelId: string) {
  const shown = useRef(panelId);
  // What last held the focus in the view's box, and what held it as the view switched.
  const held = useRef<Element | null>(null);
  const left = useRef<{ element: Element; until: number } | null>(null);
  useLayoutEffect(() => {
    if (shown.current === panelId) return;
    shown.current = panelId;
    left.current = held.current && { element: held.current, until: performance.now() + GIVEN_BACK_MS };
    held.current = null;
  }, [panelId]);
  useEffect(() => {
    const own = () => { left.current = null; };
    const focused = (event: FocusEvent) => {
      const target = event.target instanceof HTMLElement ? event.target : null;
      const back = left.current;
      if (target && back?.element === target && back.until >= performance.now()) {
        left.current = null;
        target.blur();
        return;
      }
      if (target && document.getElementById(shown.current)?.contains(target)) held.current = target;
    };
    document.addEventListener("pointerdown", own, true);
    document.addEventListener("keydown", own, true);
    document.addEventListener("focusin", focused, true);
    return () => {
      document.removeEventListener("pointerdown", own, true);
      document.removeEventListener("keydown", own, true);
      document.removeEventListener("focusin", focused, true);
    };
  }, []);
}
