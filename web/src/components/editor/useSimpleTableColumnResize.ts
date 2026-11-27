import { useRef, useState, type PointerEvent as ReactPointerEvent, type RefObject } from "react";
import { simpleTableColumnWidth } from "./simpleTableColumns";
import { simpleTableGridBoxes } from "./simpleTableMerges";
import { inlineSign, readsRightToLeft } from "@/lib/readingDirection";

// A column's right edge takes the pointer 4px either side, as Notion's 3px
// grip on the cell border does (seen 2026-10-11).
const EDGE_REACH = 4;

type Edge = { index: number; x: number; top: number; height: number };
type Drag = Edge & { startX: number; startWidth: number; width: number };

// The columns' places on the screen: the first row's cells, or the grid's
// columns in a table with merged cells, whose first row may hold one.
function columnBoxes(canvas: HTMLElement): { left: number; right: number; width: number }[] {
  const grid = canvas.querySelector<HTMLElement>("[role=table][data-table-merged]");
  if (grid) return simpleTableGridBoxes(grid, { left: 0, top: 0 }).columns.map(({ left, width }) => ({ left, right: left + width, width }));
  const row = canvas.querySelector<HTMLElement>("[role=row]");
  return row ? [...row.querySelectorAll<HTMLElement>("[data-native-table-cell]")].map((cell) => cell.getBoundingClientRect()) : [];
}

function edgeAt(canvas: HTMLElement, clientX: number, clientY: number, target: EventTarget | null): (Edge & { width: number }) | null {
  // The "+" on the table's corner, a selection's grip and a range's corners sit across an edge and keep their presses.
  if ((target as Element | null)?.closest?.("[data-table-add], [data-table-range-grip], [data-table-range-corner]")) return null;
  const table = canvas.querySelector<HTMLElement>("[role=table]")?.getBoundingClientRect();
  if (!table || clientY < table.top || clientY > table.bottom) return null;
  const origin = canvas.getBoundingClientRect();
  const cells = columnBoxes(canvas);
  // A column's end border: its left where the table reads from the right.
  const rtl = readsRightToLeft(canvas);
  for (let index = 0; index < cells.length; index++) {
    const cell = cells[index], end = rtl ? cell.left : cell.right;
    if (Math.abs(clientX - end) <= EDGE_REACH) {
      return { index, x: end - origin.left, top: table.top - origin.top, height: table.height, width: cell.width };
    }
  }
  return null;
}

/**
 * Drags a simple table's column edge: the pointer near a column's end
 * border (its right, or its left read from the right) shows a blue line and
 * the resize cursor, and dragging it sizes the
 * column live; the width is kept when the pointer lets go.
 */
export function useSimpleTableColumnResize({ canvasRef, enabled, onResize }: {
  canvasRef: RefObject<HTMLDivElement | null>;
  enabled: boolean;
  onResize(index: number, width: number): void;
}) {
  const [hover, setHover] = useState<Edge | null>(null);
  const [drag, setDrag] = useState<Drag | null>(null);
  const live = useRef<Drag | null>(null);
  const move = (next: Drag | null) => { live.current = next; setDrag(next); };

  function onPointerMove(event: ReactPointerEvent<HTMLDivElement>) {
    const canvas = canvasRef.current;
    if (!enabled || !canvas) return;
    const current = live.current;
    if (current) {
      const sign = inlineSign(canvas);
      const width = simpleTableColumnWidth(current.startWidth + (event.clientX - current.startX) * sign);
      move({ ...current, width, x: current.x + (width - current.width) * sign });
      return;
    }
    if (event.buttons) return;
    const edge = edgeAt(canvas, event.clientX, event.clientY, event.target);
    setHover((current) => current?.index === edge?.index && current?.x === edge?.x ? current : edge);
  }

  function onPointerDownCapture(event: ReactPointerEvent<HTMLDivElement>) {
    const canvas = canvasRef.current;
    if (!enabled || !canvas || event.button !== 0) return;
    const edge = edgeAt(canvas, event.clientX, event.clientY, event.target);
    if (!edge) return;
    event.preventDefault();
    event.stopPropagation();
    canvas.setPointerCapture(event.pointerId);
    move({ ...edge, startX: event.clientX, startWidth: edge.width, width: simpleTableColumnWidth(edge.width) });
  }

  function finish() {
    const done = live.current;
    if (!done) return;
    move(null);
    setHover(null);
    // A press that never moves leaves the column as it was.
    if (done.width !== simpleTableColumnWidth(done.startWidth)) onResize(done.index, done.width);
  }

  const edge = drag ?? hover;
  return {
    /** The column being dragged and its width so far. */
    dragging: drag ? { index: drag.index, width: drag.width } : null,
    edge,
    canvasProps: enabled ? {
      "data-column-edge": edge ? "true" : undefined,
      onPointerMove,
      onPointerDownCapture,
      onPointerUp: finish,
      onLostPointerCapture: finish,
      onPointerLeave: () => { if (!live.current) setHover(null); },
    } : {},
  };
}
