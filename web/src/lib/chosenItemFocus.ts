import { focusForLastPress } from "./lastPress";

/**
 * An item a right press chose takes the focus back as the item's menu closes,
 * on its own control: a list's row or a gallery's card itself (the control
 * the menu came from), a table's row by its title cell. Escape leaves it
 * ringed, the keys leading, a press outside unringed, the item still chosen
 * (viewChoiceSteps.ts keepsViewChoice), as Notion's (seen 2026-11-24 at 1685
 * in Chrome, dark, dpr 1, in two tabs at different times, the focus read after
 * the action: the list's row link and the table's title cell ringed after
 * Escape and unringed after a press on the page's margin, the gallery's card
 * link focused either way under its pointer ground). A board's card, which
 * the press does not choose, leaves it on the page, as any other item's
 * control does (overlayLayer.ts focusGoesToPage), and so does a timeline's
 * bar (seen the same day in two tabs each, Escape and a press: the focus on
 * the page, no bar chosen, a letter landing nowhere). A chosen row's title
 * cell is that row's control too, as the focus ⌘/ opened the row's menu from
 * (useRowContextMenu): Notion's gives it back held for the row, ringed after
 * Escape, the row still chosen, a letter landing nowhere (seen 2026-11-25 in
 * Chrome at 1440, dark, in two tabs at different times, judged after the key
 * by the screen and one letter; closed by a press outside, unringed, once).
 */
export function chosenItemControl(opener: Element | null | undefined): HTMLElement | null {
  if (!(opener instanceof HTMLElement)) return null;
  if (opener.matches("[data-row-card]") && opener.querySelector(":scope > [data-card-chosen]")) return opener;
  if (opener.matches('[role="row"][data-row-selected="true"]')) return opener.querySelector<HTMLElement>('[role="gridcell"][data-title="true"]');
  if (opener.matches('[role="gridcell"][data-title="true"]') && opener.closest('[role="row"][data-row-selected="true"]')) return opener;
  return null;
}

/**
 * Gives the focus back to a chosen item's control (chosenItemControl), else to
 * the opener. A table's title cell takes it for its row, not as a chosen
 * cell (data-row-focus, rowHoldsCellFocus): drawn with no chosen edge, only
 * the keyboard's ring while the keys lead, a letter landing nowhere, ↑ ↓
 * moving the rows' choice with the focus and its ring staying, Escape letting
 * the rows go, and Enter writing that row's title, the rows let go, as
 * Notion's title cell held so (seen 2026-11-24 at 1685 in Chrome, dark, dpr
 * 1, in two tabs at different times each, judged after the action by the
 * screen and one key: after Escape and after a press on the page's margin).
 * Given back after a press, it is the press's, ringless, whatever keys led or
 * were typed into the menu (focusForLastPress, seen 2026-11-25). A press on
 * the cell chooses it as any press does.
 */
export function focusChosenItem(opener: HTMLElement, options?: FocusOptions) {
  const control = chosenItemControl(opener);
  if (control?.matches('[role="gridcell"]')) holdForRow(control);
  if (control) focusForLastPress(control, options);
  else opener.focus(options);
}

function holdForRow(cell: HTMLElement) {
  cell.dataset.rowFocus = "true";
  const release = () => {
    delete cell.dataset.rowFocus;
    cell.removeEventListener("blur", release);
    cell.removeEventListener("pointerdown", release, true);
  };
  cell.addEventListener("blur", release);
  cell.addEventListener("pointerdown", release, true);
}

/** Whether a table's cell holds the focus for its row rather than as a chosen cell (focusChosenItem). */
export function rowHoldsCellFocus(cell: Element | null | undefined) {
  return cell instanceof HTMLElement && cell.dataset.rowFocus === "true";
}
