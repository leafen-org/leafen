import { useEffect, useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import type { DragReorder } from "@/lib/useDragReorder";
import { DragHandleIcon, Plus } from "../icons";
import { AddHereTip, HandleTip, IconTipButton } from "../IconTipButton";
import { optionKeyName } from "@/lib/keyboardShortcuts";
import styles from "./databaseSheet";
import layerStyles from "./TableRowHandleLayer.module.css";
import { useViewOffersNewRows } from "./databaseArchivedRowsContext";

/**
 * A table row's "+" and handle before its first column, in its row or in the
 * table's handle layer: a block's own two marks, 20px each in a 24 and an 18
 * box (Notion's plus and dragHandle, seen 2026-11-18 in Chrome, two tabs),
 * named and tipped as a block's (Notion's table and list rows, seen the same
 * day at 1440: the handle named for dragging to move and clicking for the
 * menu, and its two-line tip 6px under it; the "+" a block's "아래에 추가" name).
 */
export function TableRowHandles({ rowId, reorder, neighbours, outside = false, onAdd, onMenu }: {
  rowId: string;
  reorder: DragReorder;
  /** The rows before and after it, for Alt+↑↓. */
  neighbours: { above?: string; below?: string };
  /** Drawn outside the row's element (TableRowHandleLayer). */
  outside?: boolean;
  onAdd(above: boolean): void;
  onMenu(handle: HTMLElement): void;
}) {
  const { t } = useTranslation("tableView");
  const offersNewRows = useViewOffersNewRows();
  return <>
    {/* Its "+" adds a row under it, or over it with Option held (Notion's, seen 2026-11-08), where the view makes rows. */}
    {offersNewRows ? <IconTipButton className={styles.rowGutterButton} data-table-row-add label={t("blockHandle:buttons.addBlockBelow", { key: optionKeyName() })} tip={<AddHereTip />} placement="bottom" wait="handle"
      onClick={(event) => onAdd(event.altKey)}><Plus size="regular" aria-hidden="true" /></IconTipButton> : null}
    {/* Pressed, the handle chooses its row alone and opens the row's menu on its left (Notion's, seen 2026-11-03). */}
    <IconTipButton tabIndex={-1} wait="handle" className={styles.rowDragHandle} label={t("blockHandle:buttons.dragHint")} tip={<HandleTip drag />}
      placement="bottom" data-row-menu-handle data-table-handle-line={rowId} {...reorder.handle(rowId, true, neighbours, { outside })} onClick={(event) => onMenu(event.currentTarget)}>
      <DragHandleIcon size="regular" aria-hidden="true" />
    </IconTipButton>
  </>;
}

type Pointed = { id: string; index: number };

/**
 * A table that keeps to its box (in a column, a callout, under another
 * block), scrolled sideways, is cut where its rows' boxes start
 * (DatabaseTableChrome's data-scrolled rule), and the "+ ⋮⋮" before them
 * with it. Notion's stay where they stood, outside the box, for the row
 * under the pointer (seen 2026-11-09 in a column scrolled 34, 100 and 300px:
 * "+" 88px and the handle 64px before the first column), and drag their row
 * from there. This layer beside the scroll box draws them there: its reach
 * over the cut strip finds the row level with the pointer, as the row's own
 * margin does before the table scrolls.
 */
export function TableRowHandleLayer({ scrollRef, listRef, keep, handles }: {
  scrollRef: RefObject<HTMLElement | null>;
  listRef: RefObject<HTMLElement | null>;
  /** Whether a row keeps its "+ ⋮⋮" with the pointer gone: being dragged (a chosen one's go, as Notion's). */
  keep(rowId: string): boolean;
  /** The row's "+" and handle, by its id and its place among the rows shown. */
  handles(rowId: string, index: number): ReactNode;
}) {
  const layerRef = useRef<HTMLDivElement>(null);
  const reachRef = useRef<HTMLDivElement>(null);
  const stripRef = useRef<HTMLDivElement>(null);
  const [cut, setCut] = useState(false);
  const [pointed, setPointed] = useState<Pointed | null>(null);
  const latest = useRef({ keep, pointed });
  latest.current = { keep, pointed };

  // Where the cut strip and the pointed row stand, from the layer's own place
  // beside the scroll box; the row's controls' margin as wide as in the row.
  const place = useRef(() => {});
  place.current = () => {
    const scroller = scrollRef.current, list = listRef.current, layer = layerRef.current;
    if (!scroller || !list || !layer) return;
    const style = getComputedStyle(scroller);
    const gutter = parseFloat(style.getPropertyValue("--table-gutter-width")) || 0;
    const lead = parseFloat(style.getPropertyValue("--table-box-lead")) || 0;
    const box = scroller.getBoundingClientRect(), at = layer.getBoundingClientRect(), rows = list.getBoundingClientRect();
    const start = style.direction === "rtl" ? at.right - box.right : box.left - at.left;
    const reach = reachRef.current, strip = stripRef.current;
    if (reach) {
      Object.assign(reach.style, { top: `${rows.top - at.top}px`, height: `${rows.height}px`,
        insetInlineStart: `${start}px`, width: `${Math.max(0, gutter - lead)}px` });
    }
    const row = latest.current.pointed && rowElement(list, latest.current.pointed);
    if (strip && row) {
      Object.assign(strip.style, { top: `${row.getBoundingClientRect().top - at.top}px`, insetInlineStart: `${start}px`, width: `${gutter}px` });
    }
  };
  useLayoutEffect(() => { if (cut) place.current(); });

  // Cut only once scrolled sideways, and only where it keeps to its box.
  useEffect(() => {
    const scroller = scrollRef.current;
    if (!scroller) return undefined;
    const check = () => {
      const next = scroller.scrollLeft !== 0 && scroller.dataset.pageWide !== "true" && scroller.dataset.rowGutter === "true";
      setCut(next);
      if (next) place.current();
    };
    // First where the page is laid out already, as the box's observer first
    // answers: drawn by a press (a page unlocked), the layer lays out nothing
    // in the press's frame (2026-11-13: 122–126 ms reading where it scrolled).
    const resized = new ResizeObserver(check);
    resized.observe(scroller);
    scroller.addEventListener("scroll", check, { passive: true });
    return () => { resized.disconnect(); scroller.removeEventListener("scroll", check); };
  }, [scrollRef]);

  useEffect(() => {
    if (!cut) return undefined;
    const move = (event: PointerEvent) => {
      // A press held (a drag from the handle about to start) keeps the row it began on.
      if (event.pointerType === "touch" || event.buttons !== 0) return;
      const scroller = scrollRef.current, list = listRef.current;
      const target = event.target instanceof Element ? event.target : null;
      if (!scroller || !list || (target && stripRef.current?.contains(target))) return;
      let row: HTMLElement | null = null;
      if (target && target === reachRef.current) row = rowAt(list, event.clientY);
      else if (target && scroller.contains(target)) row = target.closest<HTMLElement>("[data-table-row-id]");
      const next = row && row.parentElement === list ? pointedOf(row) : null;
      setPointed((current) => {
        if (!next) return current && latest.current.keep(current.id) ? current : null;
        return current?.id === next.id && current.index === next.index ? current : next;
      });
    };
    document.addEventListener("pointermove", move, { passive: true });
    return () => document.removeEventListener("pointermove", move);
  }, [cut, listRef, scrollRef]);

  const shown = cut && pointed && listRef.current && rowElement(listRef.current, pointed) ? pointed : null;
  return (
    <div ref={layerRef} className={layerStyles.layer} data-table-row-handle-layer>
      {cut && <div ref={reachRef} className={layerStyles.reach} aria-hidden="true" />}
      {shown && <div ref={stripRef} className={layerStyles.strip} data-table-row-handle-strip={shown.id}>
        {handles(shown.id, shown.index)}
        <span className={layerStyles.boxRoom} aria-hidden="true" />
      </div>}
    </div>
  );
}

function pointedOf(row: HTMLElement): Pointed | null {
  const id = row.dataset.tableRowId, index = Number(row.getAttribute("aria-rowindex")) - 2;
  return id && index >= 0 ? { id, index } : null;
}

function rowElement(list: HTMLElement, pointed: Pointed) {
  return list.querySelector<HTMLElement>(`:scope > [data-table-row-id="${CSS.escape(pointed.id)}"][aria-rowindex="${pointed.index + 2}"]`);
}

function rowAt(list: HTMLElement, y: number) {
  for (const row of list.querySelectorAll<HTMLElement>(":scope > [data-table-row-id]")) {
    const box = row.getBoundingClientRect();
    if (y >= box.top && y < box.bottom) return row;
  }
  return null;
}
