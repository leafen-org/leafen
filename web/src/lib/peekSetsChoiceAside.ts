import { useEffect, useRef, type RefObject } from "react";
import { onLayersChanged } from "./overlayLayer";

// A page's or a row's peek, beside the page or over it; a row page opened
// whole is the page itself. The copy of a side peek sliding out keeps none
// of these marks (sidePeekMotion.ts).
const OPEN_PEEK = '[data-page-peek], [data-row-peek-panel]:not([data-mode="full"])';
const SIDE = '[data-mode="side"]';
// The Escape that closed a peek: one pressed this shortly before it went.
const CLOSING_ESCAPE_MS = 1000;
const NONE: ReadonlySet<string> = new Set();

/**
 * What a page or a view has chosen — a page's blocks, a table's ticked rows,
 * a board's, list's or gallery's chosen cards — is set aside while a peek
 * opened beside or over it is open, and chosen again as the peek closes, as
 * Notion's are. Seen in Chrome at 1440×723, dark, in two tabs at different
 * times (2026-11-27, judged after each action by the screen):
 * a row ticked, then opened from ⌘/ › "사이드 보기에서 열기" or its own
 * "열기" in the side peek or the middle, keeps no tick and no "N개 선택됨"
 * while the peek is open, and is ticked again once "»" or a press beside the
 * peek closes it, or Escape closes the middle one; a sub-page block chosen
 * and opened from its menu's "사이드 보기에서 열기", and a board's card chosen
 * with Shift and opened with Enter (once), likewise. Escape closing a side
 * peek lets them go instead, as an Escape on the page beside it does (rows
 * four times, the block twice; ticks the tab brought back with the browser's
 * back came back after it twice, seen once and not followed). Items chosen,
 * or a caret put in the page, meanwhile stand instead, and what went away
 * meanwhile (`stands` false) is not chosen again. A peek's own page keeps its
 * choice for one opened over that peek.
 */
export function usePeekSetsChoiceAside(root: RefObject<HTMLElement | null>, chosen: () => ReadonlySet<string>,
  choose: (next: ReadonlySet<string>) => void, stands: (id: string) => boolean) {
  // The latest of each, read as a peek opens and closes.
  const given = useRef({ chosen, choose, stands });
  given.current = { chosen, choose, stands };
  useEffect(() => {
    const peekOver = () => {
      const here = root.current;
      if (!here?.isConnected) return null;
      return [...document.querySelectorAll(OPEN_PEEK)].find(peek => !peek.contains(here)) ?? null;
    };
    let shown = peekOver();
    let aside: ReadonlySet<string> | null = null;
    let escapedAt = Number.NEGATIVE_INFINITY;
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") escapedAt = event.timeStamp; };
    window.addEventListener("keydown", escape, true);
    const stop = onLayersChanged(() => {
      const peek = peekOver();
      if (peek === shown) return;
      const closed = shown;
      shown = peek;
      if (peek) {
        const now = given.current.chosen();
        if (!now.size) return;
        aside = now;
        given.current.choose(NONE);
        return;
      }
      const back = aside;
      aside = null;
      if (!back || closed?.matches(SIDE) && performance.now() - escapedAt < CLOSING_ESCAPE_MS) return;
      const active = document.activeElement;
      const writing = active instanceof HTMLElement && active.isContentEditable && Boolean(root.current?.contains(active));
      const again = !given.current.chosen().size && !writing ? [...back].filter(given.current.stands) : [];
      if (again.length) given.current.choose(new Set(again));
    });
    return () => { stop(); window.removeEventListener("keydown", escape, true); };
  }, [root]);
}
