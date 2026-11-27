import { imeComposing } from "@/lib/fieldKeyBoundary";
import { VIEW_CHOICE_ROOT, VIEW_TAB_CHOOSE, VIEW_TAB_PRESSED, keepsViewChoice, type ViewTabChoose, type ViewTabPressed } from "./viewChoiceSteps";

const TAB = "[data-view-tab]";
// What a press on a view tab let go last: the database whose view's rows, or the page's editor whose blocks, and where.
let left: { owner: Element; at: string } | null = null;

// Ahead of the views' and the page's own presses, which let their choices go
// (the window's capture, taken as this module loads): a view tab's press, the
// tab's own usePress, reaches no one past the tab.
if (typeof window !== "undefined") {
  window.addEventListener("pointerdown", (event) => {
    const tab = event.target instanceof Element ? event.target.closest<HTMLElement>(TAB) : null;
    if (!tab || event.button !== 0 || event.shiftKey || event.metaKey || event.ctrlKey || event.altKey) {
      left = null;
      return;
    }
    const database = tab.closest("[data-database-view]");
    if (!database) return;
    const asked = new CustomEvent<ViewTabPressed>(VIEW_TAB_PRESSED, { bubbles: true, detail: {} });
    // The chosen tab's press, opening its menu, keeps the view's choice (keepsViewChoice).
    if (!keepsViewChoice(tab)) database.querySelector(`[${VIEW_CHOICE_ROOT}]`)?.dispatchEvent(new CustomEvent(VIEW_TAB_PRESSED, { detail: asked.detail }));
    if (asked.detail.at) { left = { owner: database, at: asked.detail.at }; return; }
    // The page's chosen blocks go at any tab's press, as at any press in its database (EditorDropLayer).
    tab.dispatchEvent(asked);
    const editor = tab.closest("[data-editor-root]");
    // Nothing chosen to let go: what an earlier tab's press let go stays (from a board's card, the table's tab and the board's again).
    if (asked.detail.at && editor) left = { owner: editor, at: asked.detail.at };
  }, true);
}

// Something chosen around the tab, keeping the keys: the database's rows,
// cards or held cell (chosenCellHeld.ts), or the page's blocks.
function chosenAround(tab: Element) {
  return Boolean(tab.closest("[data-database-view]")?.querySelector('[data-cell-held], [data-row-selected="true"], [data-card-chosen]')
    || tab.closest("[data-editor-root]")?.querySelector('[data-block-id][data-selected="true"]'));
}

// The arrow on a view tab that nothing chosen took (chosen blocks walk on, a
// held cell moves): the choice a view tab's press let go chosen again, else
// the page's first block for ↓ and its last for ↑, scrolled to; a board shown
// chooses nothing for ↑. The focus stays on the tab.
function chooseFromTab(tab: HTMLElement, down: boolean) {
  const database = tab.closest("[data-database-view]");
  if (chosenAround(tab)) return true;
  if (left?.owner.isConnected && left.owner.contains(tab)) {
    const view = left.owner === database ? database.querySelector(`[${VIEW_CHOICE_ROOT}]`) : left.owner;
    if (view && !view.dispatchEvent(new CustomEvent<ViewTabChoose>(VIEW_TAB_CHOOSE, { cancelable: true, detail: { at: left.at } }))) return true;
  }
  if (!down && database?.querySelector("[data-database-chrome]")?.getAttribute("data-active-view") === "board") return true;
  return !tab.dispatchEvent(new CustomEvent<ViewTabChoose>(VIEW_TAB_CHOOSE, { bubbles: true, cancelable: true, detail: { edge: down ? "first" : "last" } }));
}

/**
 * ↑ or ↓ on a database's view tab, with the focus there, as Notion's (seen
 * 2026-11-26 in Chrome at 1440 in two tabs at different times, a database
 * of a table, a board and a list between two lines and fifteen, judged after
 * the key only): with nothing chosen before, ↓ chose the page's first block
 * and scrolled to the page's top, ↑ its last block and scrolled to its foot
 * (a table's tab, a list's) or, with a board shown, nothing; a block chosen
 * and let go by a press on a tab came back with ↓ or ↑ — of two stretched
 * by Shift+↓, the one the stretch ended on (seen once) —, a letter after it
 * going nowhere and a second ↓ walking on to the next block; a card chosen
 * on the board and let go by the table's or the list's tab came back as its
 * row there, its tab keeping the ring. Heard last, after the keys of what is
 * chosen: an arrow nothing took, the browser's scroll held back.
 */
export function viewTabArrowKey(event: KeyboardEvent, tab: HTMLElement) {
  if ((event.key !== "ArrowDown" && event.key !== "ArrowUp") || event.shiftKey || event.altKey || event.metaKey || event.ctrlKey
    || imeComposing(event)) return;
  const late = (heard: KeyboardEvent) => {
    if (heard !== event) return;
    window.removeEventListener("keydown", late);
    if (!heard.defaultPrevented && chooseFromTab(tab, heard.key === "ArrowDown")) heard.preventDefault();
  };
  window.addEventListener("keydown", late);
  // Stopped on its way, it is heard no more (after the whole dispatch: a microtask runs between its listeners).
  window.setTimeout(() => window.removeEventListener("keydown", late));
}
