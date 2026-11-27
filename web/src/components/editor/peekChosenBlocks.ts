import { useEffect, type RefObject, type SetStateAction } from "react";
import { onLayersChanged } from "@/lib/overlayLayer";
import type { EditorSelection } from "./editorRuntimeTypes";

// A page's or a row's peek, beside the page or over it; a row page opened
// whole is the page itself. The copy of a side peek sliding out keeps none
// of these marks (sidePeekMotion.ts).
const OPEN_PEEK = '[data-page-peek], [data-row-peek-panel]:not([data-mode="full"])';
const NONE: EditorSelection = new Set();

/**
 * A peek opened beside or over the page sets the page's chosen blocks aside
 * while it is open — no wash stays on them, and nothing pressed in the peek
 * reaches them — and chooses them again as it closes, as Notion's does (seen
 * 2026-11-13 at 1440×723: a sub-page block's "사이드 보기에서 열기" left
 * the block unwashed beside the peek, washed again once Escape or "»" closed
 * it). Blocks chosen, or a caret put in the page, meanwhile stand instead.
 * A peek's own editor keeps its blocks for one opened over that peek.
 */
export function usePeekSetsChoiceAside(rootRef: RefObject<HTMLElement | null>, chosen: () => EditorSelection,
  setSelection: (next: SetStateAction<EditorSelection>) => void, stands: (blockId: string) => boolean) {
  useEffect(() => {
    const peekOver = () => {
      const root = rootRef.current;
      if (!root?.isConnected) return null;
      return [...document.querySelectorAll(OPEN_PEEK)].find(peek => !peek.contains(root)) ?? null;
    };
    let shown = peekOver();
    let aside: EditorSelection | null = null;
    return onLayersChanged(() => {
      const peek = peekOver();
      if (peek === shown) return;
      shown = peek;
      if (peek) {
        const now = chosen();
        if (!now.size) return;
        aside = now;
        setSelection(NONE);
        return;
      }
      const back = aside;
      aside = null;
      const active = document.activeElement;
      const writing = active instanceof HTMLElement && active.isContentEditable && Boolean(rootRef.current?.contains(active));
      const again = back && !chosen().size && !writing ? [...back].filter(stands) : [];
      if (again.length) setSelection(new Set(again));
    });
    // What still stands is read as the peek closes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chosen, rootRef, setSelection]);
}
