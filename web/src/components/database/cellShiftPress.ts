/**
 * Takes back the click of a Shift+press that chose a table cell's row
 * (TableGridRow): the value it was pressed on — a date, a select's option, a
 * person, a related page — opens no picker and no page, as Notion's table,
 * inline or a full page, chooses the row and opens nothing (seen 2026-11-25
 * in Chrome, two tabs at different times, on a date, a select, a relation's
 * page and an empty person cell). Listened for on the cell itself, as the
 * click goes down to the value, so no value's own click has it; a press that
 * ends with no click (dragged away) lets it go at the next press.
 */
export function takeBackShiftPressClick(cell: HTMLElement) {
  const done = new AbortController();
  cell.addEventListener("click", (event) => {
    event.preventDefault();
    event.stopPropagation();
    done.abort();
  }, { capture: true, signal: done.signal });
  window.addEventListener("pointerdown", () => done.abort(), { capture: true, signal: done.signal });
}
