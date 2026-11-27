/**
 * The rows a step of the page's ⌘Z leaves chosen in the views that show
 * them, as Notion's (seen 2026-11-24 in a table, two tabs): ⌘Z chooses the
 * rows the step had chosen before it — rows moved, back in their places; a
 * copy's rows it was made from; rows brought back from the trash — wherever
 * the focus is and whatever was chosen since (a press on the page's empty
 * room having let them go); ⌘⇧Z gives back what the views had chosen when
 * that ⌘Z was pressed, letting go of the rest: a move or copy taken back and
 * done again leaves its rows chosen, as they were at the ⌘Z, but after a
 * press that let them go it leaves none, and after another row was ticked
 * that one.
 *
 * Each view choosing rows (useTableRowChoice, cardSelection) listens for
 * both: it adds what it has chosen to a read, and chooses the rows named
 * once it shows one of them.
 */

/** The rows to choose, each view choosing those it shows once it shows one; `only`: the others are let go. */
export const TABLE_ROWS_RESTORED_EVENT = "leafen:table-rows-restored";
export type StepRowsChosen = { rowIds?: readonly string[]; only?: boolean };

/** Asks the views what they have chosen, each adding its chosen lines to `chosen`. */
export const VIEW_CHOICE_READ_EVENT = "leafen:view-choice-read";
export type ViewChoiceRead = { chosen: string[] };

/** Has the views choose `rowIds` (lines or rows) as they show them; `only`, letting go of what else they have chosen. */
export function chooseStepRows(rowIds: readonly string[], only = false) {
  window.dispatchEvent(new CustomEvent<StepRowsChosen>(TABLE_ROWS_RESTORED_EVENT, { detail: { rowIds, only } }));
}

// What the views had chosen when a step was taken back, for the step done again.
const chosenAtUndo = new WeakMap<object, readonly string[]>();

/** At a step's ⌘Z: keeps what the views have chosen, which its ⌘⇧Z gives back. */
export function keepChoiceForRedo(step: object) {
  const read: ViewChoiceRead = { chosen: [] };
  window.dispatchEvent(new CustomEvent<ViewChoiceRead>(VIEW_CHOICE_READ_EVENT, { detail: read }));
  chosenAtUndo.set(step, [...new Set(read.chosen)]);
}

/** At a step's ⌘⇧Z: gives back what the views had chosen at its ⌘Z; false when it was never taken back here. */
export function chooseAsAtUndo(step: object) {
  const kept = chosenAtUndo.get(step);
  if (!kept) return false;
  chooseStepRows(kept, true);
  return true;
}
