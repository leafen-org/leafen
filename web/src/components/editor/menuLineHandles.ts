import { useLayoutEffect } from "react";

/**
 * While a menu is open over a line — the "/" typed there, the "+" pressed
 * beside it — that line shows no "+ ⋮⋮" or "💬+", the pointer resting on it
 * too, as under Notion's overlay (seen 2026-11-15, twice: "+" pressed on an
 * empty line with the pointer left on it, and the pointer then moved over the
 * line and other blocks — none until the menu closed). On a desktop no row
 * draws them while the menu is open (drawnHandles.ts); this line's mark keeps
 * a touch screen's caret line from showing its own. Only that line's row is
 * marked (`data-menu-line`, EditorActionsAndMarks), so opening the menu
 * restyles one row, not the page.
 */
export function useMenuLineHandlesAway(blockId: string | undefined) {
  useLayoutEffect(() => {
    if (!blockId) return undefined;
    const rows = [...document.querySelectorAll(`[data-block-id="${CSS.escape(blockId)}"] > [role="group"][data-type]`)];
    for (const row of rows) row.setAttribute("data-menu-line", "");
    return () => { for (const row of rows) row.removeAttribute("data-menu-line"); };
  }, [blockId]);
}
