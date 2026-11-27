import {
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import type { GraphBlock } from "@/contentGraph";
import { ChevronLeft, ChevronRight, Plus, Trash } from "@/icons/leafen";
import { blockItemText } from "./blockItemLabels";
import styles from "./editorStyles.generated.module.css";
import { inlineEndKey, inlineStartKey, readsRightToLeft } from "@/lib/readingDirection";
import { keysFocusHolder } from "@/lib/lastPress";

// A column whose controls the keys' focus is in is outlined (data-keys-within).
keysFocusHolder(styles.column, { part: styles.columnControls });

export function ColumnSlot({
  children,
  column,
  count,
  index,
  manageable,
  resizing,
  share,
  onAddAfter,
  onMove,
  onRemove,
  onResizeKey,
  onResizeStart,
  edges,
  edgeResizing,
  onEdgeStart,
}: {
  children: ReactNode;
  column: GraphBlock;
  count: number;
  index: number;
  manageable: boolean;
  resizing: boolean;
  share: number;
  onAddAfter: () => void;
  onMove: (direction: -1 | 1) => void;
  onRemove: () => void;
  onResizeKey: (event: ReactKeyboardEvent<HTMLButtonElement>) => void;
  onResizeStart: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  /** The list's outer edges this column stands at, each with its handle (columnListWidth). */
  edges?: { start: boolean; end: boolean };
  edgeResizing?: "start" | "end" | null;
  onEdgeStart?: (event: ReactPointerEvent<HTMLButtonElement>, edge: "start" | "end") => void;
}) {
  const [controlsHot, setControlsHot] = useState(false);
  const number = index + 1, rtl = readsRightToLeft();

  function trackHotspot(event: ReactMouseEvent<HTMLDivElement>) {
    const rect = event.currentTarget.getBoundingClientRect();
    // Its controls stand at its top end corner: its top left read from the right.
    const hot = (readsRightToLeft(event.currentTarget) ? event.clientX <= rect.left + 96 : event.clientX >= rect.right - 96)
      && event.clientY >= rect.top - 28 && event.clientY <= rect.top + 34;
    setControlsHot((current) => current === hot ? current : hot);
  }

  function moveByKey(event: ReactKeyboardEvent<HTMLDivElement>) {
    if (!(event.altKey || event.metaKey)) return;
    if (event.key === inlineStartKey(event.currentTarget) || event.key === "ArrowUp") {
      event.preventDefault();
      onMove(-1);
    } else if (event.key === inlineEndKey(event.currentTarget) || event.key === "ArrowDown") {
      event.preventDefault();
      onMove(1);
    }
  }

  // The column itself takes no focus: a press on a line in it focused the
  // column, the nearest thing between the line and the page that takes
  // focus, and the caret stayed where it had been. The browser's caret now
  // goes where pressed, as Notion's (2026-11-15, worker Chrome, no script
  // before the press: the second column's empty line, between a line's
  // letters and past its end in the first; a letter typed next went in
  // there). Its controls are the keyboard's way to it: Tab reaches them,
  // and ⌥ or ⌘ with an arrow on one moves the column.
  // The list's outer edges have Notion's handle too, 46px out from either
  // end (seen 2026-11-24), with no name to say in the gap's.
  const edge = (side: "start" | "end") => manageable && edges?.[side] && onEdgeStart ? (
    <button type="button" className={styles.columnResize} contentEditable={false} data-edge={side} tabIndex={-1} aria-hidden="true"
      data-resizing={edgeResizing === side ? "true" : undefined} onPointerDown={(event) => onEdgeStart(event, side)} />
  ) : null;
  return (<>
    {edge("start")}
    {/* The handle between two columns is the whole gap before this one,
        beside the column rather than in it, so it reaches the row's full
        height whichever column is taller (EditorColumns .columnResize). It
        shows no tip, as Notion's (seen 2026-11-19: a bare col-resize strip,
        no name or title); it keeps its name, since here it also takes focus
        and arrow keys. */}
    {manageable && index > 0 ? (
      <button type="button" className={styles.columnResize} contentEditable={false}
        data-resizing={resizing ? "true" : undefined}
        aria-label={blockItemText("columns.resize")}
        onPointerDown={onResizeStart} onKeyDown={onResizeKey} />
    ) : null}
    <div className={styles.column} style={{ flex: `${share} 1 0` }} data-column-id={column.id}
      data-column-controls-active={manageable && (controlsHot || resizing) ? "true" : undefined}
      role="group" aria-label={blockItemText("columns.numbered", { number })}
      onMouseMove={manageable ? trackHotspot : undefined} onMouseLeave={manageable ? () => setControlsHot(false) : undefined}>
      {manageable ? (
        <div className={styles.columnControls} contentEditable={false} onKeyDown={moveByKey}>
          <button type="button" aria-label={blockItemText("columns.addAfter", { number })} title={blockItemText("columns.add")}
            onClick={onAddAfter}>
            <Plus size="tiny" aria-hidden="true" />
          </button>
          {/* The column before stands on the right where the screen reads
              from the right: the names say the screen's side, the chevrons turn over. */}
          <button type="button" aria-label={blockItemText(rtl ? "columns.moveRightNumbered" : "columns.moveLeftNumbered", { number })}
            title={blockItemText(rtl ? "columns.moveRight" : "columns.moveLeft")} disabled={index <= 0} onClick={() => onMove(-1)}>
            <ChevronLeft size="tiny" aria-hidden="true" />
          </button>
          <button type="button" aria-label={blockItemText(rtl ? "columns.moveLeftNumbered" : "columns.moveRightNumbered", { number })}
            title={blockItemText(rtl ? "columns.moveLeft" : "columns.moveRight")} disabled={index >= count - 1} onClick={() => onMove(1)}>
            <ChevronRight size="tiny" aria-hidden="true" />
          </button>
          {count > 2 ? (
            <button type="button" aria-label={blockItemText("columns.deleteNumbered", { number })}
              title={blockItemText("columns.delete")} onClick={onRemove}>
              <Trash size="tiny" aria-hidden="true" />
            </button>
          ) : null}
        </div>
      ) : null}
      {children ?? <div className={styles.columnEmpty}>{blockItemText("columns.empty")}</div>}
    </div>
    {edge("end")}
  </>);
}
