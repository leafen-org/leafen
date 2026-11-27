import {
  createContext,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  type RefObject,
  useContext,
  useEffect,
  useRef,
} from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { FocusScope, useDialog } from "react-aria";
import { createFocusManager } from "@react-aria/focus";
import { editorStepsPageHistory } from "./tableCellKeys";
import { tabLeavesCellEditor } from "./tableCellEditorTab";
import { layerOpenOver } from "@/lib/overlayLayer";
import { placeAround, popoverBackdropStyle, popoverPlacementStyle, popoverRectOf, usePopoverPlacement, type PopoverRect, placeBesideRow } from "@/lib/popoverPlacement";

export function openEditorFromCellBackground(
  event: React.MouseEvent<HTMLDivElement>,
  openEditor: () => void,
) {
  const target = event.target instanceof HTMLElement ? event.target : null;
  if (target?.closest("button, a, input, textarea, select")) return;
  openEditor();
}

/**
 * Where a cell's picker opens for something else than its cell: the table's
 * selection bar, editing every selected row at once. A picker that covers
 * its cell covers the host's button and one that opens under its cell, the
 * date's, opens under the button (Notion's, seen 2026-11-03); either gives
 * focus back to the button and tells the host while it is open.
 */
export type CellPickerHost = {
  buttonRef: RefObject<HTMLElement | null>;
  /** Called as a picker opens; what it returns, as it closes. */
  opened(): () => void;
};
export const CellPickerHostContext = createContext<CellPickerHost | null>(null);

// What a value's editor opens over, wherever the value shows: a table's
// cell, a timeline table's, a page's property value — in its list, a
// template's, a wiki's line over the title, a layout's pinned ones — or the
// selection bar's property button.
const VALUE_CELLS = '[role="gridcell"], [data-timeline-table-cell], [data-row-property-value], [data-property-value-box], [data-cell-picker-host]';
// A page's property value starts its box 4px into its cell, where its text
// starts 6px further (DatabaseRowPage.module.css `.rowPropertyValueInset`).
const PAGE_VALUE_BOX_INSET = 4;
// A row page's related page line reaches 48px before its page, the box its
// "+" picker covers (`[data-row-relation-chip]`, seen 2026-11-12).
const RELATION_LINE_REACH = 48;

/** The cell holding a value whose editor the trigger opens, if it shows in one. */
export function propertyValueCell(trigger: Element | null | undefined): HTMLElement | null {
  return trigger?.closest<HTMLElement>(VALUE_CELLS) ?? null;
}

// The box a value's editor covers or hangs from, 1px left of the value, as
// Notion's: a table's cell with the line over it, as wide as the cell (seen
// 2026-11-03 and 2026-11-06: a select's picker at 927,201 over a cell at
// 928,202; 2026-11-08: from 653 under a 200px cell at 654, 200 wide); a
// page's value level with its box, to its end (seen 2026-11-08: a template's
// tag picker at 447.5,432, 539 wide, over a box at 448.5,432, 538 wide — the
// same in side and centre peeks); a row page's related page line from 48px
// before it (seen 2026-11-12: its "+" picker at 608.5,250, 599 wide, over a
// line at 657.5,250 to 1207.5).
function valueBox(cell: HTMLElement) {
  const rect = cell.getBoundingClientRect();
  if (cell.matches('[role="gridcell"], [data-timeline-table-cell]')) {
    return { top: rect.top - 1, bottom: rect.bottom - 1, left: rect.left - 1, width: rect.width };
  }
  const inset = cell.matches("[data-row-property-value]") ? PAGE_VALUE_BOX_INSET
    : cell.matches("[data-row-relation-chip]") ? -RELATION_LINE_REACH : 0;
  const left = rect.left + inset - 1;
  return { top: rect.top, bottom: rect.bottom - 1, left, width: rect.right - left };
}

/** A value's top, which a picker covering it opens from (or rises from). */
function valueCellTop(cell: HTMLElement): PopoverRect {
  return {
    contextElement: cell,
    get top() { return valueBox(cell).top; },
    get bottom() { return valueBox(cell).top; },
    get left() { return valueBox(cell).left; },
    get right() { return valueBox(cell).left; },
  };
}

/**
 * A value's foot, the line the date's window and the formula's editor hang
 * from, in a line of any height: under it, or over it, its own foot on the
 * line, where only that side has room (FOOT_PLACEMENT), as Notion's (seen
 * 2026-11-08: under a 37px table row's cell at 202.4 at 238.4, under a 57px
 * row's at 238.4 at 294.4; a template's date at 351 and formula at 313 under
 * their 34px lines; over a low row's cell at 620.4..658.4, ending at 657.4).
 */
export function valueCellFoot(cell: HTMLElement): PopoverRect {
  return {
    contextElement: cell,
    get top() { return valueBox(cell).bottom; },
    get bottom() { return valueBox(cell).bottom; },
    get left() { return valueBox(cell).left; },
    get right() { return valueBox(cell).left; },
  };
}

/**
 * How what hangs from a value's foot is placed: under the line where it fits,
 * else over it where it fits there, else under it moved up into the window,
 * on the side it took while it stays open (Notion's, seen 2026-11-08: a 500px
 * date window over a cell with 54px under it, under one with 244 under and 455
 * over it moved up to end 12px over the window's foot). It shows at once, as
 * Notion's date window under its cell (popoverAppear.ts).
 */
export const FOOT_PLACEMENT = { gap: 0, whole: true, holdSide: true, appear: "none" } as const;

export function CellMenuPortal<T extends HTMLElement>({
  triggerRef,
  onClose,
  ariaLabel,
  className,
  backdropClassName,
  width = 240,
  scroll = true,
  cover = false,
  fitWidth = false,
  widestLine = false,
  coverRef,
  beside = false,
  rise = false,
  under = false,
  sideBy,
  gap = 2,
  align = "start",
  halfHigh = false,
  restoreFocus = true,
  focusFirst = true,
  tabLeavesCell = false,
  onKeyDown,
  popoverRef,
  children,
}: {
  triggerRef: RefObject<T | null>;
  onClose: () => void;
  ariaLabel: string;
  className: string;
  backdropClassName?: string;
  width?: number;
  scroll?: boolean;
  /** Opens over the value holding the trigger, from its top left, at least as wide as it. */
  cover?: boolean;
  /**
   * Grows past its width to its widest line, up to the window less its
   * margins, and moves back in from the window's edge, as Notion's files
   * editor (seen 2026-11-14: 240 for a short name, 307 for
   * "https://example.com/report.pdf", 933 for a 120-letter link).
   */
  fitWidth?: boolean;
  /**
   * As wide as its widest line that doesn't wrap, never under its width and
   * at most the window less its margins, as Notion's date windows (seen
   * 2026-11-19, Korean, by their computed width min-content: a date
   * property's and an automation's "날짜 선택" 248 for their 224px month,
   * German "Datumsformat · Vollständiges Datum" 275, a longer "시간 포함"
   * 268 and 391; a schedule's details 260, 313 and 329 for longer lines).
   */
  widestLine?: boolean;
  /** What it covers instead of the trigger's value: a row page's related page line, its "+". */
  coverRef?: RefObject<HTMLElement | null>;
  /**
   * Opens beside the window holding the trigger row, its top on the row's,
   * as Notion's sub-lists do (2026-10-16: the date window's "리마인더" list
   * opens outside it at that row), not over the rows under it.
   */
  beside?: boolean;
  /** Moved up to show whole where the window ends, as Notion's date window (seen 2026-11-05). */
  rise?: boolean;
  /** Hangs from the foot of the value holding the trigger, not from the trigger, as Notion's date window (FOOT_PLACEMENT). */
  under?: boolean;
  /** The height its side is chosen by, where that is its full height rather than its present one (a relation's). */
  sideBy?: number;
  /** Its distance from the trigger it opens under or over; a board's "+ 신규 그룹" picker's is none, as Notion's (seen 2026-11-12). */
  gap?: number;
  /** Where it lines up with its trigger: its start, or centred on it as an automation's database picker (Notion's, seen 2026-11-15). */
  align?: "start" | "center";
  /**
   * Stops at half the window's height, all of it scrolling, as Notion's
   * "+ 신규 그룹" window (seen 2026-11-12: max-height 361.5 at 723, its
   * search scrolling away with its pages).
   */
  halfHigh?: boolean;
  /** Whether its closing hands the focus back to its opener; a card's slot's leaves it on the page, as Notion's (seen 2026-11-06). */
  restoreFocus?: boolean;
  /** Whether its opening puts the focus on its first control; a schedule's details take none, as Notion's (seen 2026-11-09). */
  focusFirst?: boolean;
  /** In a table, Tab closes it and chooses the cell beside, as Notion's cell pickers and files list (tableCellEditorTab.ts). */
  tabLeavesCell?: boolean;
  onKeyDown?: (event: ReactKeyboardEvent<HTMLDivElement>) => void;
  popoverRef?: RefObject<HTMLDivElement | null>;
  children: ReactNode | ((placed: boolean) => ReactNode);
}) {
  const { t } = useTranslation(["propertyCell", "common"]);
  const ownRef = useRef<HTMLDivElement>(null), menuRef = popoverRef ?? ownRef;
  const entered = useRef(false);
  const host = useContext(CellPickerHostContext);
  const returnFocusRef = host?.buttonRef ?? triggerRef;
  useEffect(() => host?.opened(), [host]);
  const { dialogProps } = useDialog({ "aria-label": ariaLabel }, menuRef);
  // Under the trigger, or over it where only that side has room, else cut on
  // the roomier side, and on that side while it stays open. A cell's picker
  // covers its cell, as Notion's does (seen 2026-10-06): its top left on the
  // cell's, flipping up from there.
  const cell = cover ? coverRef?.current ?? propertyValueCell(triggerRef.current) : null;
  const foot = under ? propertyValueCell(triggerRef.current) : null;
  // A wider value widens it to the value's box; a picker asking for more keeps its own.
  const shownWidth = cell ? Math.max(width, Math.round(valueBox(cell).width)) : width;
  const pos = usePopoverPlacement(menuRef, (size, viewport) => {
    if (beside) return placeBesideRow(triggerRef.current, size, viewport);
    const row = popoverRectOf(triggerRef.current) ?? { top: 0, bottom: 0, left: 0, right: 0 };
    if (foot) return placeAround(valueCellFoot(foot), size, viewport, FOOT_PLACEMENT);
    return placeAround(cell ? valueCellTop(cell) : row, size, viewport,
      { gap: cell ? 0 : gap, align: cell ? "start" : align, minHeight: 160, rise, holdSide: true, sideBy, ...(halfHigh ? { cap: viewport.height / 2 } : {}),
        // A value's own editor over its cell shows at once, as Notion's select and date (popoverAppear.ts).
        ...(cell ? { appear: "none" as const } : {}) });
  }, [triggerRef, shownWidth, cell, foot, beside, rise, sideBy, gap, align, halfHigh], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose,
    inside: () => [triggerRef.current, host?.buttonRef.current], restoreFocus, returnFocusRef });
  const placed = popoverPlacementStyle(pos);
  // In a table, where Tab leaves it, the focus isn't held in it: a files
  // list leaves it on nothing once a link is added, as Notion's, and Tab
  // then is still this window's while nothing is open over it.
  const inTableCell = tabLeavesCell && Boolean(triggerRef.current?.closest("[data-table-cell]"));
  useEffect(() => {
    if (!inTableCell) return undefined;
    const onTab = (event: KeyboardEvent) => {
      const active = document.activeElement, menu = menuRef.current;
      if (active && active !== document.body || !menu?.isConnected || layerOpenOver(menu)) return;
      tabLeavesCellEditor(event, triggerRef.current, onClose);
    };
    window.addEventListener("keydown", onTab);
    return () => window.removeEventListener("keydown", onTab);
  });
  useEffect(() => {
    if (!pos || entered.current) return;
    entered.current = true;
    if (!focusFirst) return;
    if (menuRef.current === document.activeElement || !menuRef.current?.contains(document.activeElement)) createFocusManager(menuRef).focusFirst({ tabbable: true });
  }, [Boolean(pos)]);
  return createPortal(
    <>
      <button
        type="button"
        className={backdropClassName}
        style={popoverBackdropStyle(pos) ?? { zIndex: 1000 }}
        tabIndex={-1}
        aria-label={t("propertyCell:closeMenu")}
      />
      <div
        ref={menuRef}
        className={className}
        style={{
          position: "fixed",
          zIndex: 1001,
          // The window's margins bound it (popoverPlacementStyle's maxWidth).
          ...(fitWidth ? { width: "max-content", minWidth: shownWidth }
            : widestLine ? { width: "min-content", minWidth: shownWidth } : { width: shownWidth }),
          ...placed,
          // A menu with its own scrolling list keeps it; the cut only bounds it.
          ...(scroll ? {} : { overflowY: undefined }),
        }}
        {...dialogProps}
        onKeyDown={(event) => {
          if (editorStepsPageHistory(triggerRef.current, event.nativeEvent)) {
            event.preventDefault();
            event.stopPropagation();
            return;
          }
          onKeyDown?.(event);
          // A Tab the cell's window keeps (a file's name being written) goes round it, as its focus scope did.
          if (inTableCell && event.key === "Tab" && !event.defaultPrevented && !event.altKey && !event.ctrlKey && !event.metaKey) {
            event.preventDefault();
            const round = createFocusManager(menuRef);
            if (event.shiftKey) round.focusPrevious({ wrap: true, tabbable: true }); else round.focusNext({ wrap: true, tabbable: true });
          }
        }}
        // Before its search or field: a picker's Tab picks nothing here, as Notion's in a table.
        onKeyDownCapture={tabLeavesCell ? (event) => { tabLeavesCellEditor(event, triggerRef.current, onClose); } : undefined}
      >
        <FocusScope contain={!inTableCell}>{typeof children === "function" ? children(Boolean(pos)) : children}</FocusScope>
      </div>
    </>,
    document.body,
  );
}
