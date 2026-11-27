import { imeComposing } from "@/lib/fieldKeyBoundary";
import { giveFocusBack } from "@/lib/focusGiveBack";
import { cellLine, cellOnLine } from "./tableCellLine";

type TabKey = {
  key: string; shiftKey: boolean; altKey: boolean; ctrlKey: boolean; metaKey: boolean; target: EventTarget | null;
  defaultPrevented: boolean; preventDefault(): void; stopPropagation(): void;
};

/**
 * Tab and Shift+Tab in a table cell's own window over it — its files list
 * and the add window under it, a choice's (select, multi-select, status), a
 * person's or a relation's picker, what is typed in its search picked by
 * nothing — close it and choose the cell after or before it in its row, its
 * own staying chosen at the row's end, as Notion's (2026-11-26, Chrome 1440,
 * judged by the screen and one letter after the key with nothing run in the
 * page before it, in two tabs at two times: the next cell ringed, the letter
 * opening that cell's own editor with it; Shift+Tab the cell before; from
 * the last column its own cell, the table not rolled). A date's window keeps
 * Tab among its own controls, as Notion's (its "오늘" ringed), and doesn't
 * ask for this; nor does a part of it that keeps its own Tab
 * (`data-keeps-tab`: the add window's link tab, going round its own
 * controls as Notion's; a file's name being written and its "⋯" menu, not
 * measured, going round the list as before). Whether the key was taken.
 */
export function tabLeavesCellEditor(event: TabKey, opener: HTMLElement | null | undefined, close: () => void): boolean {
  if (event.key !== "Tab" || event.altKey || event.ctrlKey || event.metaKey || event.defaultPrevented || imeComposing(event)) return false;
  if (event.target instanceof Element && event.target.closest("[data-keeps-tab]")) return false;
  const cell = opener?.closest<HTMLElement>("[data-table-cell]");
  const grid = cell?.closest<HTMLElement>('[role="grid"]');
  if (!cell || !grid) return false;
  event.preventDefault();
  event.stopPropagation();
  const beside = cellOnLine(grid, cellLine(cell), Number(cell.dataset.colIndex) + (event.shiftKey ? -1 : 1)) ?? cell;
  close();
  // Once the closing has handed its own cell the focus back, the page kept where it is.
  giveFocusBack({ to: beside, keepScroll: true });
  return true;
}
