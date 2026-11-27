import { type RefObject, useLayoutEffect } from "react";
import { scrollKeyWalkedRow } from "./menuKeyScroll";

// What a person does in a list that takes it over from its opening scroll.
const TAKEN_BY = ["wheel", "pointerdown", "keydown", "touchstart"] as const;

/**
 * A list that marks its chosen choice opens with that choice in view, as
 * Notion's lists open (seen 2026-11-20 at 1440 in two tabs, a code block's
 * 88 languages in a 313.5px list): by the rule its arrow keys scroll by
 * (`scrollKeyWalkedRow`) — a choice standing in the list's middle leaves it
 * at its top (Bash, 6th; C, 9th, its top 236px down), and one wholly within
 * 64px of the foot or past it is brought to 64px over the foot (C#, 10th:
 * scrolled 44.5; C++, 11th: 72; Java, 35th: 768). A list that lights its
 * first choice or marks none opens at its top, as Notion's number formats
 * and language list do.
 *
 * Placed as the list is laid out and drawn — a ResizeObserver's answer, and
 * again as rows come (a long list draws only what it shows) or the list
 * takes its size — never on the opening press's own time, until the person
 * scrolls, presses or types in it.
 */
export function useOpenAtChosen(listRef: RefObject<HTMLElement | null>, menuRef: RefObject<HTMLElement | null>, chosen: string | null) {
  useLayoutEffect(() => {
    const list = listRef.current, menu = menuRef.current;
    if (!list || !menu || chosen === null) return undefined;
    const place = () => {
      const row = list.querySelector<HTMLElement>(`[data-option-value="${CSS.escape(chosen)}"]`);
      if (row && list.clientHeight > 0) scrollKeyWalkedRow(row, menu);
    };
    const sized = new ResizeObserver(place);
    sized.observe(list);
    const drawn = new MutationObserver(place);
    drawn.observe(list, { childList: true, subtree: true });
    const stop = () => {
      sized.disconnect();
      drawn.disconnect();
      for (const type of TAKEN_BY) menu.removeEventListener(type, stop, true);
    };
    for (const type of TAKEN_BY) menu.addEventListener(type, stop, { capture: true, passive: true });
    return stop;
    // Once, as it opens: the choice it opens at stays that one.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
}
