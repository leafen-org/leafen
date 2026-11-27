import { useEffect, type RefObject } from "react";
import { raiseHeldEditor } from "./heldEditorOverWindow";
import { cellTextField, fitCellEditor } from "./tableCellKeys";
import { keepsViewChoice, VIEW_ADD_PRESS, VIEW_CHOICE_KEEPING_PRESS } from "./viewChoiceSteps";
import { escapeForWindowOver } from "./viewWindowEscape";
import { fieldEnter, fieldEscape, imeComposing, imeStarting } from "@/lib/fieldKeyBoundary";
import { shortcutModifierHeld } from "@/lib/keyboardShortcuts";
import { layerOpenOver } from "@/lib/overlayLayer";

/**
 * A table's chosen cell held apart from the focus: drawn chosen while the
 * focus is on one of its view's controls that keep a choice
 * (VIEW_CHOICE_KEEPING_PRESS: a toolbar control that opens a window, a filter
 * pill, "N개 더 보기", the chosen view tab) or in the window that control opened, the keys that control doesn't
 * take going on to the cell. As Notion's (seen 2026-11-17 at 1440 in two
 * tabs at different times, after the action only): a row ticked and a filter
 * pill ringed after Escape closed its window, Enter opened the pill's window
 * with the row's title cell drawn chosen under it, and Escape closed the
 * window, the ring back on the pill and the title cell still chosen; then ↓
 * chose the cell under it, the ring staying, a letter went into the held
 * title, and Escape chose the cell's row, the ring gone. Seen once each: the
 * toolbar's "정렬" ringed, Enter hid the filter bar with the title cell
 * chosen and the ring kept on "정렬"; a cell chosen and a filter pill or
 * "필터" pressed, the cell stayed chosen, under the pill's window and after
 * its Escape too, while the bar's sort pill and "+ 필터" let it go. Enter on
 * the ringed pill with a title or text cell held opens the pill's window and
 * draws the held cell's editor open as well, the focus in the window: a
 * letter typed goes into the window's field, the first Escape closes the
 * editor, the window staying, and the second the window, the ring back on
 * the pill and the cell still held (Notion's, seen 2026-11-17 at 1440 in two
 * tabs at different times, after the action only, the letter typed after it).
 * A window that leaves the focus on the page as it closes (⚡'s, by Escape)
 * leaves the cell chosen there too, taking the page's keys: ↓ chooses the
 * cell under it and a letter goes into it, as Notion's (seen 2026-11-26 and
 * 2026-11-27 in Chrome at 1440 in two tabs at different times, judged after
 * the key, the focus on the page with no ring).
 */
export function holdChosenCell(cell: HTMLElement) {
  for (const other of cell.closest('[role="grid"]')?.querySelectorAll<HTMLElement>("[data-cell-held]") ?? []) {
    if (other !== cell) delete other.dataset.cellHeld;
  }
  cell.dataset.cellHeld = "true";
}

// One of the view's own controls that keep its choice, around the table `grid`; with `orAdd` its "+" too (VIEW_ADD_PRESS).
function keepingControl(at: EventTarget | null, grid: HTMLElement, orAdd = false) {
  const control = at instanceof Element ? at.closest<HTMLElement>(orAdd ? `${VIEW_CHOICE_KEEPING_PRESS}, ${VIEW_ADD_PRESS}` : VIEW_CHOICE_KEEPING_PRESS) : null;
  return control?.closest("[data-database-view]")?.contains(grid) ? control : null;
}

// The focus on the page itself, as a window that gives it to no control leaves it.
const onPage = (at: EventTarget | null) => !at || at === document.body || at === document.documentElement;

// The keys a ringed control leaves to the held cell: what types into it,
// clears it, moves the choice from it or lets it go (Enter, Space and Tab are
// the control's own).
function goesToCell(event: KeyboardEvent) {
  if (shortcutModifierHeld(event)) return false;
  // An input method's first key starts typing into the cell; its keys after it are the composition's.
  if (imeComposing(event)) return imeStarting(event);
  if (event.key.length === 1) return event.key !== " ";
  return event.key === "Backspace" || event.key === "Delete" || event.key === "Escape"
    || event.key === "ArrowUp" || event.key === "ArrowDown" || event.key === "ArrowLeft" || event.key === "ArrowRight";
}

// The held cell's editor drawn open without the focus, as its link window
// draws it (data-field-held, RichTextCellToolbar), over the control's window
// (heldEditorOverWindow.ts); how to close it, or null for a value edited
// otherwise (a choice's picker, a number's box — not seen).
function drawEditorOpen(cell: HTMLElement) {
  const field = cell.dataset.cellTyping === "text" ? cellTextField(cell) : null;
  if (!field) return null;
  field.dataset.fieldHeld = "true";
  const lower = raiseHeldEditor(cell);
  if (!lower) fitCellEditor(cell);
  return () => { lower?.(); delete field.dataset.fieldHeld; };
}

/**
 * Holds the chosen cell of the table `grid` as the focus leaves it for its
 * view's control that keeps a choice (a press on it, or the focus put there),
 * lets it go at a press anywhere that keeps no view's choice or once a cell
 * takes the focus again, and hands it the keys typed on that control, or on
 * the page itself with no window open over it.
 */
export function useChosenCellHeld(gridRef: RefObject<HTMLElement | null>) {
  useEffect(() => {
    const grid = gridRef.current;
    if (!grid) return;
    const held = () => {
      const cell = grid.querySelector<HTMLElement>("[data-cell-held]");
      return cell?.isConnected ? cell : null;
    };
    // The held cell's editor drawn open by Enter on its control, until the
    // first Escape, a press or the cell let go.
    let closeEditor: (() => void) | null = null;
    const shutEditor = () => {
      window.removeEventListener("keydown", escape, true);
      const close = closeEditor;
      closeEditor = null;
      close?.();
    };
    // Ahead of the window's own Escape, which then closes it the next time.
    const escape = (event: KeyboardEvent) => {
      if (!fieldEscape(event) || event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      shutEditor();
    };
    const letGo = () => {
      shutEditor();
      const cell = held();
      if (cell) delete cell.dataset.cellHeld;
    };
    let pressed: Element | null = null;
    const press = (event: PointerEvent) => {
      pressed = event.target instanceof Element ? event.target : null;
      window.requestAnimationFrame(() => { pressed = null; });
      shutEditor();
      if (held() && !(pressed && keepsViewChoice(pressed))) letGo();
      const cell = document.activeElement;
      if (!(cell instanceof HTMLElement && cell.matches("[data-table-cell]") && grid.contains(cell))) return;
      // One that leaves the focus where it was ("N개 더 보기", the view bar's "+", data-press-keeps-focus) holds
      // the cell at once: its window takes the focus from the cell itself, and gives it back as it closes.
      if (pressed?.closest("[data-press-keeps-focus]") && keepingControl(pressed, grid, true)) holdChosenCell(cell);
    };
    // A press on such a control takes the focus to it, or in WebKit to nowhere.
    const left = (event: FocusEvent) => {
      const cell = event.target instanceof HTMLElement && event.target.matches("[data-table-cell]") ? event.target : null;
      if (cell && keepingControl(event.relatedTarget ?? pressed, grid)) holdChosenCell(cell);
    };
    // The cell takes the key as its own, chosen again; from a control, moved on
    // by an arrow, the choice goes on held, the ring staying on the control.
    const pass = (event: KeyboardEvent, cell: HTMLElement, control: HTMLElement | null) => {
      cell.focus({ preventScroll: true });
      const copy = new KeyboardEvent("keydown", { key: event.key, code: event.code, shiftKey: event.shiftKey,
        repeat: event.repeat, bubbles: true, cancelable: true });
      cell.dispatchEvent(copy);
      if (copy.defaultPrevented) event.preventDefault();
      const chosen = document.activeElement;
      if (control && event.key.startsWith("Arrow") && chosen instanceof HTMLElement && chosen.matches("[data-table-cell]") && grid.contains(chosen)) {
        control.focus({ preventScroll: true });
      }
    };
    // From the page itself, ahead of the page's own keys (its blocks, a view's first row), while no window is open
    // over the table (one open keeps its keys and Escape, viewWindowEscape.ts).
    const pageKey = (event: KeyboardEvent) => {
      const cell = held();
      if (!cell || !onPage(event.target) || event.defaultPrevented || !goesToCell(event) || layerOpenOver(grid)) return;
      event.preventDefault();
      event.stopPropagation();
      pass(event, cell, null);
    };
    const keyDown = (event: KeyboardEvent) => {
      const cell = held(), control = cell && keepingControl(event.target, grid);
      if (!cell || !control || event.target !== control || event.defaultPrevented) return;
      // Enter is the control's own, its window opening, and draws the cell's editor open too.
      if (fieldEnter(event) && !event.metaKey && !event.ctrlKey && !event.altKey && !event.shiftKey) {
        shutEditor();
        closeEditor = drawEditorOpen(cell);
        if (closeEditor) window.addEventListener("keydown", escape, true);
        return;
      }
      // A window open over the view takes Escape first, the cell staying held (viewWindowEscape.ts).
      if (!goesToCell(event) || event.key === "Escape" && escapeForWindowOver(grid, event)) return;
      pass(event, cell, control);
    };
    document.addEventListener("pointerdown", press, true);
    grid.addEventListener("focusout", left);
    grid.addEventListener("focusin", letGo);
    window.addEventListener("keydown", pageKey, true);
    window.addEventListener("keydown", keyDown);
    return () => {
      shutEditor();
      document.removeEventListener("pointerdown", press, true);
      grid.removeEventListener("focusout", left);
      grid.removeEventListener("focusin", letGo);
      window.removeEventListener("keydown", pageKey, true);
      window.removeEventListener("keydown", keyDown);
    };
  }, [gridRef]);
}
