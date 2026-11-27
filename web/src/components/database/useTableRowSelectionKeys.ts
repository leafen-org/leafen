import { useEffect, useRef, type Dispatch, type RefObject, type SetStateAction } from "react";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";
import { useDatabaseOwnPage } from "./databaseFullPageLink";
import { chooseAllInView, choiceKeyFromControl, choiceStep, controlTakingRowsEnter, keepsViewChoice, useViewChoiceStart, VIEW_CHOICE_KEEPING_PRESS, type ChoiceEnds } from "./viewChoiceSteps";
import { useViewChoiceJoin } from "./viewChoiceJoin";
import { holdChosenCell } from "./chosenCellHeld";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { useChoiceLetGo } from "@/lib/carriedChoice";
import { rowHoldsCellFocus } from "@/lib/chosenItemFocus";
import { windowedLines } from "@/lib/useWindowedRows";
import { routeOverlayKeyDown } from "@/lib/overlayLayer";
import { escapeForWindowOver } from "./viewWindowEscape";
import { useViewChoicePaste } from "./viewChoicePaste";

type SelectionKey = Pick<globalThis.KeyboardEvent, "key" | "metaKey" | "ctrlKey" | "shiftKey" | "altKey"
  | "defaultPrevented" | "target" | "preventDefault" | "stopPropagation">;

/**
 * The keys of a table's selected rows. ⌘D duplicates them, ⌘C copies them,
 * ⌘/ opens their menu and Delete moves them to the trash, unless a field is
 * being typed in. ↑ ↓ choose the row before the first selected or after the
 * last, Shift with them stretching the selection from where it began, past
 * an inline table's first or last row on to its page's blocks — joined to
 * them going down (viewChoiceJoin.ts), the page taking the keys while they
 * are —, and start it with none selected — the card views' rule
 * (viewChoiceSteps.ts), as
 * Notion's table (seen 2026-11-12: 일 4 chosen, ↓ chooses 일 3; Shift+↓
 * 일 4–일 2; ↑ from 일 4 chooses the database's block). Enter chooses the
 * head row's title cell in place of the rows, and Escape there chooses the
 * row again (Notion's, seen 2026-11-12). Escape lets the rows go, focus
 * staying where it is (seen 2026-10-06), and goes on to the layer on top as
 * from the page: a side peek beside them, or holding them, closes with it
 * (seen 2026-11-25, peekBesideEscape.ts). Focus can be left on the page itself,
 * as when a picker from the selection bar closes or Escape turns chosen cells
 * into their rows; the keys still act on them there, and on a control of
 * the view's that keeps them chosen (choiceKeyFromControl: the toolbar's
 * "필터" a filter's Escape gave the focus back to), Enter there doing that
 * control's own as well (controlTakingRowsEnter), and a plain press
 * anywhere but the rows' own controls, or a press that keeps a view's
 * choice (keepsViewChoice: a toolbar control that opens a window, a menu
 * over the rows), lets them go, as pressing a cell or the page does in
 * Notion (seen 2026-11-06). ⌘A chooses every line the table shows, drawn
 * or not, or hands the key to the page around an inline one, the focus
 * staying where it is (chooseAllInView). Gives back the table's own keydown,
 * and whether the rows are joined to the page's blocks, when the selection
 * bar stays away (Notion's, seen 2026-11-27: its "N개 선택됨" only on rows
 * chosen alone).
 */
export function useTableRowSelectionKeys({ root, readOnly, selectedRows, setSelectedRows, onDuplicate, onCopy, onMenu, onTrash }: {
  root: RefObject<HTMLDivElement | null>;
  readOnly: boolean;
  selectedRows: ReadonlySet<string>;
  setSelectedRows: Dispatch<SetStateAction<ReadonlySet<string>>>;
  onDuplicate(): void;
  /** Copies the chosen rows, as cut ones for ⌘X (rowPagesClipboard). */
  onCopy(cut?: boolean): void;
  onMenu(): void;
  onTrash(): void;
}) {
  const ends = useRef<ChoiceEnds | null>(null);
  // Rows' lines in the order shown, each once: a grouped table's row under several groups has one in each (viewGroupLines.ts).
  const order = () => [...new Set(Array.from(root.current?.querySelectorAll<HTMLElement>("[data-table-line]") ?? [])
    .filter((row) => !row.closest("[inert]")).map((row) => row.dataset.tableLine ?? ""))].filter(Boolean);
  const rowOf = (line: string) => root.current?.querySelector<HTMLElement>(`[data-table-line="${CSS.escape(line)}"]`);
  // A row the keys reach is shown, the keys following on the page.
  const choose = (rowIds: string[], next: ChoiceEnds) => {
    ends.current = next;
    setSelectedRows(new Set(rowIds));
    rowOf(next.head)?.scrollIntoView({ block: "nearest" });
    // A title cell holding the focus for its row keeps it, ringed (chosenItemFocus.ts).
    const active = document.activeElement;
    if (active instanceof HTMLElement && root.current && (root.current.contains(active) || active.contains(root.current)) && !rowHoldsCellFocus(active)) active.blur();
  };
  // Every line it shows, those a long table leaves undrawn too (useWindowedRows `lines`).
  const chooseAll = () => { ends.current = null; setSelectedRows(new Set(windowedLines(root.current) ?? order())); };
  const ownPage = useDatabaseOwnPage();
  // The row the choice ended on: Escape and a view tab's press let it go there.
  const head = () => ends.current && selectedRows.has(ends.current.head) ? ends.current.head : [...selectedRows][0];
  const start = useViewChoiceStart({ root, order, empty: !selectedRows.size, ownPage, enabled: !readOnly, chooseAll, head,
    choose: (rowId) => choose([rowId], { anchor: rowId, head: rowId }) });
  const letGo = () => {
    start.escaped(head());
    setSelectedRows(new Set());
  };
  useEffect(() => { if (!selectedRows.size) ends.current = null; }, [selectedRows]);
  const join = useViewChoiceJoin({ root, order, chosen: selectedRows, ends, clear: () => setSelectedRows(new Set()), trash: onTrash,
    take: (rowIds, next, own) => { if (own) choose(rowIds, next); else { ends.current = next; setSelectedRows(new Set(rowIds)); rowOf(next.head)?.scrollIntoView({ block: "nearest" }); } } });
  // A drag of them given up with Escape lets them go as Escape does (carriedChoice.ts).
  useChoiceLetGo(() => { if (selectedRows.size) letGo(); });
  // A paste of blocks with them chosen goes on the page after the database (viewChoicePaste.ts).
  useViewChoicePaste(root, !readOnly && selectedRows.size > 0, () => setSelectedRows(new Set()));

  function step(event: SelectionKey) {
    const down = event.key === "ArrowDown";
    const next = choiceStep(order(), selectedRows, ends.current, down, event.shiftKey);
    if (next === "out") join.out(down, event.shiftKey);
    else if (next) choose(next.ids, next.ends);
  }
  // Enter: the head row's title cell, chosen as a press would choose it; from
  // a control that keeps the view's choice, held while the ring stays there
  // (holdChosenCell), as Notion's.
  function enter(from: Element) {
    const rowId = ends.current?.head ?? [...selectedRows][0];
    const cell = rowId ? rowOf(rowId)?.querySelector<HTMLElement>("[data-table-cell][data-title]")
      ?? rowOf(rowId)?.querySelector<HTMLElement>("[data-table-cell]") : null;
    if (!cell) return;
    setSelectedRows(new Set());
    if (from.closest(VIEW_CHOICE_KEEPING_PRESS)) holdChosenCell(cell);
    else cell.focus();
  }
  function keyDown(event: SelectionKey) {
    const target = event.target as HTMLElement;
    if (readOnly || !selectedRows.size || join.joinedNow() || event.defaultPrevented || event.altKey || target.isContentEditable
      || target.matches("textarea, select, input:not([type=checkbox])")) return;
    const command = primaryModifier(event), key = chordLetter(event);
    // A cell with focus keeps its own arrows and Enter.
    const onCell = Boolean(target.closest?.("[data-table-cell]")) && !rowHoldsCellFocus(target);
    if (command && !event.shiftKey && key === "a" && !onCell) chooseAllInView(root.current, ownPage, chooseAll, () => setSelectedRows(new Set()));
    else if (command && !event.shiftKey && key === "d") onDuplicate();
    else if (command && !event.shiftKey && key === "c") onCopy();
    // ⌘X copies them as cut and moves them to the trash, one ⌘Z bringing them back chosen, as Notion's (seen 2026-11-27).
    else if (command && !event.shiftKey && key === "x") { onCopy(true); onTrash(); }
    // ⌘/ opens several rows' menu as the bar's "⋯" does (seen 2026-11-03), one
    // row's page item menu (TableView, seen 2026-11-12), as Notion's.
    else if (command && !event.shiftKey && key === "/") onMenu();
    else if (!command && !event.shiftKey && (event.key === "Delete" || event.key === "Backspace")) onTrash();
    else if (!command && !onCell && (event.key === "ArrowDown" || event.key === "ArrowUp")) step(event);
    else if (!command && !onCell && !event.shiftKey && fieldEnter(event)) enter(target);
    // A picker open over them, drawn elsewhere in the page, keeps its Escape: it closes only the picker, as does a
    // window open over the view wherever the focus is (viewWindowEscape.ts). One from them goes on to the layer on
    // top as from the page, unprevented yet (Notion's: rows let go and peek closed at once).
    else if (!command && !event.shiftKey && event.key === "Escape" && root.current?.contains(event.target as Node)
      && !escapeForWindowOver(root.current, event)) { letGo(); routeOverlayKeyDown(event); }
    else return;
    event.preventDefault();
    event.stopPropagation();
  }
  const keys = useRef(keyDown);
  keys.current = keyDown;
  const anySelected = selectedRows.size > 0;
  useEffect(() => {
    if (!anySelected) return;
    const onPage = () => !document.activeElement || document.activeElement === document.body;
    // ↑ ↓ from the page go to the rows before the page's own blocks hear them.
    const onArrow = (event: globalThis.KeyboardEvent) => {
      if ((event.key === "ArrowDown" || event.key === "ArrowUp") && !imeComposing(event) && onPage()) keys.current(event);
    };
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.defaultPrevented || imeComposing(event)) return;
      // Enter on such a control is the rows' and its own both: the title cell
      // held first, the window the control opens giving the focus back to the
      // control as it closes, the cell still chosen and taking the keys.
      const control = controlTakingRowsEnter(event, root.current);
      if (control) {
        keys.current(event);
        event.preventDefault();
        control.click();
        return;
      }
      if (event.key !== "Escape" && (onPage() || choiceKeyFromControl(event.target, event.key, root.current))) keys.current(event);
    };
    // Escape from there lets them go before a layer on top hears it, the document's turn coming before the window's
    // (overlayLayer.ts): a side peek closes with them, as Notion's (seen 2026-11-25: a row chosen by a chosen cell's
    // Escape, the peek open, one Escape let it go and closed the peek); a picker holding the focus keeps its own, and
    // a window open over the view takes it alone (viewWindowEscape.ts).
    const onEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape" && !event.defaultPrevented && !imeComposing(event) && onPage() && !join.joinedNow() && !escapeForWindowOver(root.current, event)) letGoNow.current();
    };
    const onPointerDown = (event: globalThis.PointerEvent) => {
      if (event.button !== 0 || event.shiftKey || event.metaKey || event.ctrlKey || !(event.target instanceof Element)) return;
      // A head's box is the rows' own too: let go here, every row would be
      // chosen again by the box's own change, which reads none chosen.
      if (event.target.closest("[data-table-selection-bar], [data-table-row-select], [data-table-select-all], [data-row-menu-handle]")
        || keepsViewChoice(event.target)) return;
      setSelectedRows(new Set());
    };
    document.addEventListener("keydown", onArrow, true);
    window.addEventListener("keydown", onKeyDown);
    document.addEventListener("keydown", onEscape);
    window.addEventListener("pointerdown", onPointerDown, true);
    return () => {
      document.removeEventListener("keydown", onArrow, true);
      window.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("keydown", onEscape);
      window.removeEventListener("pointerdown", onPointerDown, true);
    };
  }, [anySelected, root, setSelectedRows]);
  const letGoNow = useRef(letGo);
  letGoNow.current = letGo;
  return { keyDown, joined: join.joined };
}
