import { useEffect, type RefObject } from "react";
import { useChosenCellHeld } from "./chosenCellHeld";

// What a press on keeps its own focus or is the cell's own layer: a menu, a picker, a toolbar.
const OWN_LAYER = '[role="dialog"], [role="menu"], [role="listbox"], [role="toolbar"], [role="tooltip"]';

/**
 * A press away from a cell whose value is being written — the page's empty
 * room under the table, the sidebar's — leaves that cell chosen, as Notion's
 * (seen 2026-11-14: a title, a text and a new row's empty title, each left by
 * a press below the table, kept their cell ringed). A press on what takes the
 * focus — another cell, a button, the page's text — goes on there. A chosen
 * cell left for its view's control that keeps a choice — a filter pill, a
 * toolbar control that opens a window — stays chosen, held apart from the
 * focus (useChosenCellHeld).
 */
export function useCellChosenAfterPressAway(gridRef: RefObject<HTMLElement | null>) {
  useChosenCellHeld(gridRef);
  useEffect(() => {
    const grid = gridRef.current;
    if (!grid) return;
    let pressed: Element | null = null;
    const press = (event: PointerEvent) => {
      pressed = event.target instanceof Element ? event.target : null;
      window.requestAnimationFrame(() => { pressed = null; });
    };
    // Nothing took the focus: the page, or what holds the table on it (an
    // inline database's own block, which a press in its room focuses).
    const nowhere = (at: EventTarget | null) => !(at instanceof Element) || at === document.body || at.contains(grid);
    const left = (event: FocusEvent) => {
      const field = event.target instanceof HTMLElement ? event.target : null;
      const cell = field?.closest<HTMLElement>("[data-table-cell]");
      if (!cell || cell === field || !nowhere(event.relatedTarget) || !pressed || cell.contains(pressed) || pressed.closest(OWN_LAYER)) return;
      window.requestAnimationFrame(() => {
        if (cell.isConnected && nowhere(document.activeElement)) cell.focus({ preventScroll: true });
      });
    };
    document.addEventListener("pointerdown", press, true);
    grid.addEventListener("focusout", left);
    return () => {
      document.removeEventListener("pointerdown", press, true);
      grid.removeEventListener("focusout", left);
    };
  }, [gridRef]);
}
