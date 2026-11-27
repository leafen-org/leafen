import { useId, useRef } from "react";
import { draggableByPointer } from "@/lib/pointerDrag";
import { useKeyedRefs } from "@/lib/useKeyedRefs";
import type { Page } from "@/lib/types";

export function useUndatedTimelineDrag(rows: readonly Page[], readOnly: boolean, assign: (row: Page, column: number) => void) {
  const id = useId(), kind = `timeline-undated:${id}`;
  const latest = useRef({ rows, readOnly, assign }); latest.current = { rows, readOnly, assign };
  const rowRef = useKeyedRefs<string>((rowId, element) => draggableByPointer({ element,
    canDrag: () => !latest.current.readOnly && !latest.current.rows.find(row => row.id === rowId)?.isLocked,
    getInitialData: () => ({ kind, id: rowId }),
  }));
  return { rowRef, target: { kind, onDrop: (rowId: string, column: number) => {
    const row = latest.current.rows.find(row => row.id === rowId);
    if (row && !row.isLocked && !latest.current.readOnly) latest.current.assign(row, column);
  } } };
}
