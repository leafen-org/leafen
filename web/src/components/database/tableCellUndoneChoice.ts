// ⌘Z taking back the letter that opened a chosen cell's editor goes back to
// that cell chosen, as Notion's (2026-11-26, Chrome at 1440 and 1000 in two
// tabs at two times, a title and a text cell, judged by the screen and one
// key after the action with nothing run in the page before it): a letter
// typed on the chosen cell opened its editor with it, ⌘Z brought the value
// back, and Escape then left the cell chosen (its editor drawn without a
// caret until then), Tab chose the next cell. Taken back only part way —
// more typed after the letter — or in an editor opened by a press or by
// Enter, the caret stays at the end of what is left, Escape choosing no
// cell as with any words. Notion's left-over editor swallowed a letter typed
// then and tinted the row at ↓; here the cell is plainly chosen, a letter
// writing it and ↓ choosing the cell below, as on any chosen cell (Leafen's
// way: nothing typed is lost).
//
// The step is noted on the cell as ⌘Z is pressed and read two frames after
// it began: a cell's values taken back show as the step is taken, which lands
// at once (recordShownNativeEditorCommand), so a letter typed again right
// after ⌘Z is a step of its own and ⌘Z soon after it takes it back, the cell
// chosen each time (2026-11-27: the step waited for the server's answer; the
// letter joined it and stayed, no ⌘Z left for it). A ⌘Z pressed while a step
// the server decides is on its way waits its turn, and is read as that comes
// (pageUndo.ts requestPageUndoBegun); the read of an earlier ⌘Z on the cell
// leaves it to the later one. An Escape pressed before it is read waits for
// it: taken back, the cell stays chosen; else no cell is chosen, as Escape
// out of the words left would (2026-11-27: one before the frame that read
// the value chose the row). The cell's own keys hear it first (undoneCellKey,
// in its capture), so a key handed on to it from the page or a control while
// it is held (chosenCellHeld.ts) keeps the same order.

// What a cell held when a letter typed on it opened its editor, while that writing lasts.
const openedByLetter = new WeakMap<HTMLElement, string>();
// The cell whose field the page's history step is closing: that close is no end of its writing.
let stepping: HTMLElement | null = null;
// A step of the page's history that closed a cell's field a letter had
// opened: what the cell held then, whether it is still to be read, whether
// the step took the cell back there (read once, when it is read or at its
// next key after that), and whether the Escape it keeps the cell chosen for
// was pressed.
type Step = { opened: string; unread: boolean; back?: boolean; escaped?: boolean; forget: () => void };
const steps = new WeakMap<HTMLElement, Step>();
// The latest step noted on a cell: the read of an earlier one, its turn come after it, leaves the cell to it.
const latestStep = new WeakMap<HTMLElement, object>();
const WORDS = '[data-cell-editor] [contenteditable="true"], [data-cell-editor] [data-rich-cell-shown]';
const MODIFIERS = new Set(["Shift", "Control", "Alt", "Meta", "CapsLock", "Fn", "OS"]);

const cellWords = (cell: HTMLElement) => cell.querySelector(WORDS)?.textContent ?? "";

/** Notes that a letter typed on the chosen `cell` opens its editor now, before the letter goes in. */
export function noteLetterOpened(cell: HTMLElement) {
  openedByLetter.set(cell, cellWords(cell));
  const ended = (event: FocusEvent) => {
    if (stepping === cell || (event.relatedTarget instanceof Node && cell.contains(event.relatedTarget))) return;
    openedByLetter.delete(cell);
    cell.removeEventListener("focusout", ended);
  };
  cell.addEventListener("focusout", ended);
}

/** Closes a cell's field for a step of the page's history without ending its writing; what the cell held when a letter opened it. */
export function closeFieldForStep(cell: HTMLElement, field: HTMLElement) {
  stepping = cell;
  try { field.blur(); } finally { stepping = null; }
  return openedByLetter.get(cell);
}

/** Notes the step the page took for `cell`, whose field a letter had opened holding `opened`; what its read names it by. */
export function noteStep(cell: HTMLElement, opened: string | undefined, redo: boolean) {
  const press = {};
  latestStep.set(cell, press);
  steps.get(cell)?.forget();
  if (redo || opened === undefined) return press;
  const forget = () => {
    if (steps.get(cell) === step) steps.delete(cell);
    cell.removeEventListener("focusout", forget);
  };
  const step: Step = { opened, unread: true, forget };
  steps.set(cell, step);
  // The cell left ends it.
  cell.addEventListener("focusout", forget);
  return press;
}

// Whether the step took the cell back to what it held when the letter opened its editor: then that writing is over.
function settled(cell: HTMLElement, step: Step) {
  step.back ??= cellWords(cell) === step.opened;
  if (step.back) openedByLetter.delete(cell);
  else step.forget();
  return step.back;
}

/**
 * Whether the step `press` (noteStep), read now, leaves the cell as it is,
 * `chosen` it holding the focus: taken back to what it held when a letter
 * opened its editor, it stays chosen with no caret; not, with an Escape
 * pressed before it was read, no cell is chosen; else the caret goes back to
 * the end of what it holds (tableCellKeys.ts). A later step noted on the cell
 * is read by itself (2026-11-27: an earlier ⌘Z's turn coming as the next was
 * pressed read the next one's step before its turn, and opened the editor).
 */
export function steppedBackToChosen(cell: HTMLElement, chosen: boolean, press: object) {
  if (latestStep.get(cell) !== press) return true;
  const step = steps.get(cell);
  if (!step) return false;
  step.unread = false;
  if (!chosen) { step.forget(); return false; }
  if (settled(cell, step)) return true;
  if (step.escaped) cell.blur();
  return Boolean(step.escaped);
}

/**
 * A key on the chosen `cell` itself after a step took it back to chosen, or
 * before the step is read, ahead of its other keys: the first Escape keeps it
 * chosen, as Notion's closing the editor it left drawn, the next choosing its
 * row; any other key ends that. Whether it took the key.
 */
export function undoneCellKey(cell: HTMLElement, event: Pick<KeyboardEvent, "key" | "target" | "shiftKey" | "altKey" | "metaKey" | "ctrlKey">) {
  const step = steps.get(cell);
  if (!step || event.target !== cell || MODIFIERS.has(event.key)) return false;
  const plainEscape = event.key === "Escape" && !event.shiftKey && !event.altKey && !event.metaKey && !event.ctrlKey;
  // Pressed before the step is read, the first Escape waits for it (steppedBackToChosen).
  if (!plainEscape || step.escaped || !(step.unread || settled(cell, step))) {
    step.forget();
    return false;
  }
  step.escaped = true;
  return true;
}
