// A table's cells take the keys as Notion's do (2026-10-15: Escape left focus
// on the page, whose arrow keys then scrolled it, and Enter did nothing): a
// value's editor left with Escape or Enter keeps its cell chosen, the arrow
// keys move from it (TableGrid), Enter on a chosen cell edits its value, and
// typing on it starts editing with what was typed (seen 2026-10-19): a
// title's, a text's, a number's, a link's, an email's or a phone number's
// editor opens with it in place of the value, a choice's, a person's or a
// relation's picker opens with it in its search, and a checkbox, a date or
// files take no typing. Backspace and Delete clear a chosen cell's value,
// which the page's ⌘Z brings back (seen 2026-10-21).

import { requestPageUndo, requestPageUndoBegun } from "../editor/pageUndo";
import { editorCommandOwner } from "../editor/editorScope";
import { chordLetter, primaryModifier, shortcutModifierHeld } from "@/lib/keyboardShortcuts";
import { focusOpener } from "@/lib/overlayLayer";
import { sheetGivesFocusBackFor } from "../shell/sheetFocusReturn";
import { liveCellField } from "./RichTextCellShown";
import { CELL_CONTROL, keepCellChosen } from "./tableCellKeptChosen";
import { imeComposing } from "@/lib/fieldKeyBoundary";
import { keepPhoneCellBoxInSight } from "./phoneCellBoxInSight";
import { cellBoxIntoWindow } from "./cellBoxInWindow";
import { closeFieldForStep, noteLetterOpened, noteStep, steppedBackToChosen } from "./tableCellUndoneChoice";
export { chooseCellByKey } from "./tableCellReveal";
export { keepCellChosen } from "./tableCellKeptChosen";
import { rollTableToOpenedCell } from "./tableCellReveal";
/**
 * ⌘Z and ⌘⇧Z in a cell's text step the page's history, where what was typed
 * is a step and the row made before it another: the first takes the words
 * back, the next the row (Notion's, seen 2026-10-21, 2026-11-05). Whether the
 * page took the step; a table on no page's editor keeps its field's own.
 * Only the cell's own text: a window opened from it and drawn over the page
 * (the formula editor, a comment's field) hears its keys through the cell
 * but keeps its own ⌘Z while open, as Notion's formula editor does (seen
 * 2026-11-15 in Chrome, no script before the keys: "to", today() chosen, a
 * letter, then ⌘Z took back the letter, ⌘Z the suggestion, a third did
 * nothing, and ⌘⇧Z put them back; here the first had closed the editor and
 * taken the new formula property away).
 */
export function stepsPageHistory(cell: HTMLElement, event: Pick<KeyboardEvent, "key" | "ctrlKey" | "metaKey" | "altKey" | "shiftKey" | "target" | "isComposing">) {
  const key = chordLetter(event);
  if (!primaryModifier(event) || event.altKey || imeComposing(event) || (key !== "z" && key !== "y")) return false;
  if (!(event.target instanceof HTMLElement) || event.target === cell || !event.target.isContentEditable || !cell.contains(event.target)) return false;
  const pageId = cell.closest<HTMLElement>("[data-page-id]")?.dataset.pageId;
  if (!pageId) return false;
  // A field that holds the caret doesn't take a value from outside: the step
  // closes it, and the caret goes back into the cell at the end of what it now
  // holds, as Notion's (2026-11-15, worker Chrome, no script before the
  // action: "bc" typed into a row's name, ⌘Z, then a letter went in at the
  // end of the name, in both) — unless the step took back the letter that
  // opened it on the chosen cell: the cell stays chosen (tableCellUndoneChoice.ts).
  const field = event.target, redo = event.shiftKey || key === "y";
  const opened = closeFieldForStep(cell, field);
  // What it left is read two frames after the step began, its turn come (tableCellUndoneChoice.ts).
  const begun = requestPageUndoBegun(pageId, redo);
  if (begun) {
    const press = noteStep(cell, opened, redo);
    keepCellChosen(cell, false, true);
    void begun.then(() => window.requestAnimationFrame(() => window.requestAnimationFrame(() => {
      const chosen = cell.isConnected && document.activeElement === cell;
      if (!steppedBackToChosen(cell, chosen, press) && chosen) editChosenCell(cell);
    })));
    return true;
  }
  field.focus();
  return false;
}

/**
 * ⌘Z and ⌘⇧Z in a value's open editor, on none of its own fields, step the
 * history of the page holding the value, the editor staying open with what
 * the step left: a file dragged to another place goes back, as Notion's
 * files editor (seen 2026-11-11). A field keeps its own. Whether the page
 * took the step.
 */
export function editorStepsPageHistory(opener: HTMLElement | null, event: Pick<KeyboardEvent, "key" | "ctrlKey" | "metaKey" | "altKey" | "shiftKey" | "target" | "isComposing">) {
  const key = chordLetter(event);
  if (!primaryModifier(event) || event.altKey || imeComposing(event) || (key !== "z" && key !== "y")) return false;
  if (event.target instanceof HTMLElement && (event.target.isContentEditable || event.target.matches("input, textarea, select"))) return false;
  // The page whose editor the keys go to from where the editor was opened: a
  // peek's own for a value in a peek, else the page holding the table.
  const pageId = editorCommandOwner()?.getAttribute("data-editor-page") || opener?.closest<HTMLElement>("[data-page-id]")?.dataset.pageId;
  return Boolean(pageId) && requestPageUndo(pageId!, event.shiftKey || key === "y");
}

/**
 * A press into a cell's text that doesn't hold the caret yet leaves it at the
 * end of that text, as Notion's (seen 2026-11-05); the next press puts it there.
 * Only the cell's own text: a press on its "열기" or its room reaches no text
 * of the page around the table (2026-11-12: with no caret on the page, it put
 * the page's at its end, and a peek opened then gave the focus back there).
 * A finger's quick second tap there, a double tap, leaves it so too (tableCellDoubleTap.ts).
 */
export function caretAtEndOfPressedText(event: { button: number; target: EventTarget; currentTarget: Element }) {
  const field = event.target instanceof Element ? event.target.closest<HTMLElement>("[contenteditable=\"true\"]") : null;
  if (!field || !event.currentTarget.contains(field) || event.button !== 0 || field.contains(document.activeElement)) return;
  window.addEventListener("click", () => {
    const selection = window.getSelection();
    selection?.selectAllChildren(field);
    selection?.collapseToEnd();
  }, { once: true });
}

/**
 * Keeps the box a value is written in over its cell inside the window, 12px
 * in from its right and its left edge (cellBoxInWindow.ts), a phone's table
 * first rolled to a cell opened mostly out of sight (tableCellReveal.ts); on a
 * phone 12px over its foot too, over the keyboard (phoneCellBoxInSight.ts). A box held
 * open by a window of its own (a phone's link sheet or equation, a ⌘K link
 * window) keeps where it stands as the focus comes back to it, until it is
 * read again: let back over its cell meanwhile, its caret was read there,
 * past the table's visible end, and the table rolled sideways to it
 * (2026-11-25 on a Galaxy A90: 90 after a page chosen in the link sheet).
 */
export function fitCellEditor(cell: HTMLElement) {
  const editor = cell.querySelector<HTMLElement>(":scope > [data-cell-editor]");
  if (!editor) return;
  const phone = Boolean(cell.closest('[data-phone="true"]'));
  if (phone) keepPhoneCellBoxInSight(editor);
  // A box opening stands over its cell to be read; one held open by its own window keeps its place.
  const opening = !editor.querySelector("[data-field-held]");
  if (opening && editor.style.getPropertyValue("--cell-editor-shift")) editor.style.removeProperty("--cell-editor-shift");
  // Read once the frame is laid out, as its size is first reported, not in a
  // frame callback before it: there the read laid out the page early, 9–11ms
  // after a new row's title took the focus on a page of 14,000 elements (seen
  // 2026-11-13). The shift, when it changes, is drawn in the same frame.
  if (typeof ResizeObserver === "undefined") return;
  const laidOut = new ResizeObserver(() => {
    laidOut.disconnect();
    // A phone's cell opened mostly out of sight comes into it first (tableCellReveal.ts).
    if (phone && opening && editor.isConnected) rollTableToOpenedCell(cell);
    const was = parseFloat(editor.style.getPropertyValue("--cell-editor-shift")) || 0;
    const shifted = editor.isConnected && getComputedStyle(editor).position === "absolute";
    // Where the box stands over its cell, the shift it has taken back (toward its start: the left, or the right right to left).
    const rtl = shifted && getComputedStyle(editor).direction === "rtl";
    const box = editor.getBoundingClientRect(), left = box.left + (rtl ? -was : was);
    // The shift goes toward its start: a move to the right is a shift back, right to left a shift on.
    const moved = shifted ? cellBoxIntoWindow(left, box.width) : 0;
    const shift = rtl ? moved : -moved;
    if (Math.abs(shift - was) < 0.5) return;
    if (shift) editor.style.setProperty("--cell-editor-shift", `${shift}px`);
    else editor.style.removeProperty("--cell-editor-shift");
  });
  laidOut.observe(editor);
}

// Enter that writes a single value moves the choice down a row, as Notion's
// (seen 2026-11-02 for a title, 11-03 for a number and a link); a list's
// picker stays open on Enter, its Escape keeping the cell chosen.
const ENTER_MOVES_DOWN = new Set(["title", "rich_text", "number", "url", "email", "phone", "select", "status"]);

/** Whether Enter leaving this property type's editor moves the choice down a row. */
export function enterMovesDown(type: string) {
  return ENTER_MOVES_DOWN.has(type);
}

// Escape leaving a title's or a text's editor — the cells written as rich
// text — with words in it chooses no cell, so a second Escape chooses no
// row; left empty, its cell stays chosen, as every other editor's does, a
// second Escape choosing the row (Notion's, seen 2026-11-06 for a title,
// 2026-11-11 for a text, a number, a link, a date, a choice, a person and a
// relation, 2026-11-12 and 2026-11-14 for a title and a text emptied or
// never written; 2026-11-17 for a title with words in an inline and a
// full-page table in two tabs, its editor opened by a press, by Enter and
// by a letter typed on the chosen cell, judged after the action only: no
// ring, the focus on the page's body and a letter typed then landing
// nowhere — once in eleven a just-made row's cell stayed chosen, not seen
// again on the same steps).
const ESCAPE_CHOOSES_NOTHING = new Set(["title", "rich_text"]);

/** Whether Escape leaving this property type's editor, `field` the one written in, keeps its cell chosen. */
export function escapeKeepsChosen(type: string, field?: EventTarget | null) {
  return !ESCAPE_CHOOSES_NOTHING.has(type) || cellFieldBlank(field);
}

/** Whether a cell's field being written in shows no words: its text, or the box it is in. */
export function cellFieldBlank(field?: EventTarget | null) {
  const written = field instanceof Element ? field.closest<HTMLElement>('[contenteditable="true"]') ?? field : null;
  const words = written instanceof HTMLInputElement || written instanceof HTMLTextAreaElement ? written.value : written?.textContent ?? "";
  return !words.replace(/[\s\u200b\ufeff]/gu, "");
}

/**
 * Gives focus back to a cell's own button once its picker closes, unless the
 * table chose a cell meanwhile (`keepCellChosen`) — then the chosen cell keeps
 * it, as Notion's shows no button ringed after a pick (seen 2026-11-02). A
 * page's property value leaves it on the page, as a closed menu's opener there
 * does (focusOpener): Notion's pickers opened by a press and closed by
 * Escape leave no ring and Enter then opens nothing, the focus on the body
 * (seen 2026-11-16 in fresh Chrome tabs, no script before the action: people
 * and select in a side peek and on a full page, a typed letter landing
 * nowhere; multi-select and date on the full page). A picker drawn as a
 * touch screen's sheet leaves it to the sheet (giveSheetFocusBack): Notion's
 * phone leaves it on the body once an option is chosen, on the cell when the
 * shade closes it (seen 2026-11-18 on a Galaxy A90).
 */
export function restoreFocusUnlessChosen(element: HTMLElement | null) {
  if (sheetGivesFocusBackFor(element)) return;
  window.requestAnimationFrame(() => {
    const active = document.activeElement;
    // In a table its cell is chosen again, not its button ringed, as Notion's
    // (seen 2026-11-06: a select's option clicked leaves the cell chosen;
    // measured once 2026-11-15 in Chrome 1440 with no script before the
    // press: so, ↑ then choosing the cell above).
    const cell = element?.closest<HTMLElement>("[data-table-cell]");
    if (cell && (!active || active === document.body || active === element)) {
      cell.focus({ preventScroll: true });
      return;
    }
    if (active && active !== document.body) return;
    focusOpener(element);
  });
}

// What edits a cell's value: its text — a title's or a text's shown words
// wake into their editor first (RichTextCellShown) — else the control that
// opens its editor (a number's, a choice's) or changes it (a checkbox).
const CELL_TEXT = '[data-cell-editor] [contenteditable="true"], [data-cell-editor] [data-rich-cell-shown], [data-cell-editor] input:not([type="hidden"]):not([type="checkbox"])';

function cellText(cell: HTMLElement) {
  const text = cell.querySelector<HTMLElement>(CELL_TEXT);
  return text && liveCellField(text);
}

/** A cell's text field, its editor made now when its words were drawn asleep; null for a value edited otherwise. */
export const cellTextField = (cell: HTMLElement): HTMLElement | null => cellText(cell);

/**
 * Enter on a chosen cell opens its editor or picker and does nothing more:
 * whatever else that press sends — until the key is let go — reaches nothing,
 * so a picker it opened (a relation's, a person's, a choice's or a status's)
 * picks only from the next press, its first row merely highlighted, as
 * Notion's (seen 2026-11-12 in its app; in the desktop app's WebKit window
 * that press went on to pick the relation picker's first row, linking the
 * row to itself). True when it opened one.
 */
export function editChosenCellByKey(cell: HTMLElement, key: string): boolean {
  if (!editChosenCell(cell)) return false;
  const swallow = (event: KeyboardEvent) => {
    if (event.key !== key) return;
    if (event.type === "keyup") { letGo(); return; }
    event.preventDefault();
    event.stopImmediatePropagation();
  };
  const types = ["keydown", "keypress", "keyup"] as const;
  const letGo = () => {
    for (const type of types) window.removeEventListener(type, swallow, true);
    window.removeEventListener("blur", letGo);
  };
  for (const type of types) window.addEventListener(type, swallow, true);
  window.addEventListener("blur", letGo);
  return true;
}

/** Starts editing a chosen cell's value, its caret at the end; false when there is nothing to edit. */
export function editChosenCell(cell: HTMLElement): boolean {
  const text = cellText(cell);
  if (text instanceof HTMLInputElement) {
    text.focus();
    text.setSelectionRange(text.value.length, text.value.length);
    return true;
  }
  if (text) {
    text.focus();
    const selection = window.getSelection();
    selection?.selectAllChildren(text);
    selection?.collapseToEnd();
    return true;
  }
  const control = cell.querySelector<HTMLElement>(CELL_CONTROL);
  if (!control) return false;
  control.click();
  return true;
}

/**
 * A press on an editable cell's empty room, beside or under its value in a
 * row taller than it, does what a press on the value does (Notion's, seen
 * 2026-11-06 low in a wrapped row's select, 2026-11-08 low in a tall row's
 * title, date, link, email, phone, formula, files and checkbox): a title or
 * a text takes the caret at its end, a checkbox turns, and the others open
 * their editor or picker. True when it did.
 */
export function openCellFromRoom(cell: HTMLElement, target: EventTarget | null): boolean {
  if (cell.dataset.cellOpens !== "true" || !(target instanceof HTMLElement)) return false;
  if (target !== cell && !target.matches("[data-cell-editor]")) return false;
  // A press begun on one of the cell's own buttons ("열기", a sub-item's
  // triangle), which took the focus, and let go on its room is that button's,
  // not the room's: the click comes to the cell when the two differ.
  const pressed = document.activeElement;
  if (pressed instanceof HTMLButtonElement && pressed.parentElement === cell) return false;
  const picker = cell.dataset.cellTyping === "text" ? null : cell.querySelector<HTMLElement>(CELL_OPENER);
  if (picker) {
    picker.click();
    return true;
  }
  // Without one, a value that takes typing or clears is edited as Enter edits
  // it; a worked-out value whose editor is closed to the reader stays as it is.
  return Boolean(cell.dataset.cellTyping || cell.dataset.cellClears === "true") && editChosenCell(cell);
}

// What opens a cell's picker or editor where its value's own control does
// something else first (a choice's chips, a formula's text).
const CELL_OPENER = "[data-cell-editor] [aria-haspopup], [data-cell-editor] [data-formula-cell-editor]";

const TYPED_IN_PLACE = new Set(["title", "rich_text"]);
const TYPED_INTO_EDITOR = new Set(["number", "url", "email", "phone", "select", "status", "multi_select", "person", "relation"]);
// What a press on an editable cell's room opens or changes, as a press on its
// value: what takes typing, and the values that take none.
const OPENED_FROM_ROOM = new Set([...TYPED_IN_PLACE, ...TYPED_INTO_EDITOR,
  "date", "files", "checkbox", "formula", "place", "verification"]);

/** Whether a press on an editable cell of this property type's room opens it, for its `data-cell-opens`. */
export function cellOpensFromRoom(type: string): boolean {
  return OPENED_FROM_ROOM.has(type);
}

/** How an editable cell of this property type takes typing, for its `data-cell-typing`. */
export function cellTyping(type: string): "text" | "editor" | undefined {
  if (TYPED_IN_PLACE.has(type)) return "text";
  if (TYPED_INTO_EDITOR.has(type)) return "editor";
  return undefined;
}

/**
 * Whether a key put down on a focused cell is left alone, the cell not being
 * a chosen one: a phone's, a button that opens its value's sheet at a tap
 * and at Enter and takes no other plain key, as Notion's (seen 2026-11-18 on
 * a Galaxy A90 in Chrome, a hardware key sent with the focus left on the
 * cell by its sheet's shade, no script before the key and each in a fresh
 * tab: a letter on a select's cell and on a relation's, two tabs each, only
 * drew the keyboard's ring (DatabaseTablePhoneCell.module.css) and opened
 * nothing; Backspace left a select's value, two tabs; ↓ left the focus where
 * it was, Escape chose no row and Enter opened the sheet, each on a select's
 * cell and on a date's). A key held with ⌘, Ctrl or Option, Tab and the menu
 * keys go on as on a desktop.
 */
export function phoneCellLeavesKey(cell: HTMLElement, event: Pick<KeyboardEvent, "key" | "ctrlKey" | "metaKey" | "altKey">): boolean {
  if (event.ctrlKey || event.metaKey || event.altKey || PHONE_CELL_KEYS.has(event.key)) return false;
  return Boolean(cell.closest('[data-phone="true"]'));
}

const PHONE_CELL_KEYS = new Set(["Enter", "Tab", "ContextMenu", "F10"]);

/** Starts editing a chosen cell with the key typed on it; true when the key went to an editor. */
export function typeIntoChosenCell(
  cell: HTMLElement,
  event: Pick<KeyboardEvent, "key" | "keyCode" | "ctrlKey" | "metaKey" | "altKey" | "preventDefault"> & { getModifierState?: (key: "AltGraph") => boolean },
): boolean {
  const typing = cell.dataset.cellTyping;
  if (!typing || shortcutModifierHeld(event)) return false;
  const character = event.key.length === 1 && event.key !== " " ? event.key : null;
  // An input method's first key: its composition goes on in the editor
  // where it can (a title's or a text's), else the editor opens for the next.
  const composing = imeComposing(event);
  if (!character && !composing) return false;
  if (typing === "text") {
    const text = cellText(cell);
    if (!text) return false;
    noteLetterOpened(cell);
    text.focus();
    selectAllOf(text);
    if (character) {
      event.preventDefault();
      document.execCommand("insertText", false, character);
    }
    return true;
  }
  const control = cell.querySelector<HTMLElement>('[data-cell-editor] button');
  if (!control) {
    // Keys typed before the editor the first key opened took focus go on in
    // it, as Notion's take a number typed fast (seen 2026-11-06; measured
    // once 2026-11-15 in Chrome 1440 with no script before the keys: "4" and
    // "2" sent at once on a chosen number cell both went in, "42").
    const editor = character ? cell.querySelector<HTMLElement>(CELL_TEXT) : null;
    if (!editor || !character) return false;
    event.preventDefault();
    editor.focus();
    document.execCommand("insertText", false, character);
    return true;
  }
  event.preventDefault();
  if (character) typeIntoNextEditor(cell, character);
  control.click();
  return true;
}

function selectAllOf(target: HTMLElement) {
  if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement) target.select();
  else window.getSelection()?.selectAllChildren(target);
}

// The editor a cell opens takes focus as it draws, maybe a frame later; what
// was typed goes in place of what it holds then. Only an editor in the cell or
// in the picker it opens takes it, and only for a moment.
function typeIntoNextEditor(cell: HTMLElement, text: string) {
  const stop = () => {
    document.removeEventListener("focusin", typed, true);
    window.clearTimeout(timer);
  };
  const typed = (event: FocusEvent) => {
    const target = event.target;
    if (!(target instanceof HTMLElement) || !takesText(target)) return;
    if (!cell.contains(target) && !target.closest('[role="dialog"]')) return;
    stop();
    selectAllOf(target);
    document.execCommand("insertText", false, text);
  };
  document.addEventListener("focusin", typed, true);
  const timer = window.setTimeout(stop, 1000);
}

function takesText(target: HTMLElement) {
  if (target instanceof HTMLTextAreaElement) return true;
  if (target instanceof HTMLInputElement) return !["checkbox", "radio", "hidden", "button", "submit"].includes(target.type);
  return target.isContentEditable;
}

// What Backspace and Delete clear (Notion's, seen 2026-10-19): a value of
// these kinds, a checkbox unchecking, and a status, which then reads as its
// default (seen 2026-11-04, its menu left with Escape; valueAsRead); not a
// value worked out (a formula, a rollup, a time or person kept by Leafen).
const CLEARED = new Set(["title", "rich_text", "number", "url", "email", "phone", "select", "multi_select", "person", "relation", "date", "files", "checkbox", "status"]);

/** Whether an editable cell of this property type clears with Backspace and Delete, for its `data-cell-clears`. */
export function cellClears(type: string): boolean {
  return CLEARED.has(type);
}


/** Whether a cell of this property type takes the value its column's chosen cell's corner dot pulls over it: what clears. */
export function cellFills(type: string): boolean {
  return CLEARED.has(type);
}

/** Whether a key clears the chosen cell's value. */
export function clearsChosenCell(cell: HTMLElement, event: Pick<KeyboardEvent, "key" | "ctrlKey" | "metaKey" | "altKey" | "shiftKey">): boolean {
  return cell.dataset.cellClears === "true" && (event.key === "Backspace" || event.key === "Delete")
    && !event.ctrlKey && !event.metaKey && !event.altKey && !event.shiftKey;
}

/**
 * Tab on a chosen cell itself moves the choice, not the focus: the cell after
 * it along the table — a row's last to the next row's first, the table's last
 * staying (TableGrid's moveFocus) — and Shift+Tab keeps it, no button in the
 * cell ever ringed, as Notion's (2026-11-26, Chrome 1440, in two tabs at two
 * times, judged by the screen and one letter after the key with nothing run
 * in the page before it: a select's picker closed by Escape left its cell
 * chosen, the focus on the body; Tab chose the next row's title, the letter
 * then writing it; Tab again its next cell; Shift+Tab nothing). Whether the
 * table moves the choice on (the key is taken either way).
 */
export function tabChoosesNextCell(event: {
  key: string; ctrlKey: boolean; metaKey: boolean; altKey: boolean; shiftKey: boolean;
  target: EventTarget | null; currentTarget: EventTarget | null; nativeEvent?: { isComposing?: boolean }; preventDefault(): void;
}): boolean {
  if (event.key !== "Tab" || event.target !== event.currentTarget || event.ctrlKey || event.metaKey || event.altKey || imeComposing(event)) return false;
  event.preventDefault();
  return !event.shiftKey;
}
