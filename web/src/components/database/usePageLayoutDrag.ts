import { useId, useRef, useState, type KeyboardEvent } from "react";
import { draggableByPointer, dropTargetByPointer } from "@/lib/pointerDrag";
import { useKeyedRefs } from "@/lib/useKeyedRefs";

type Drop<T> = { key: T; after: boolean };

/**
 * Dragging the builder's modules and rows by their "⠿", as Notion's (seen
 * 2026-10-12): the row under the pointer shows where the dragged one lands,
 * above or below it. The handle moves its row with the arrow keys too. A
 * mouse's or a pen's drag follows the pointer's own events (pointerDrag), a
 * see-through copy of the handle going with it; the browser's own drop was
 * lost at times in WebKit, the desktop app's window. A finger's is the
 * browser's drag.
 */
export function usePageLayoutDrag<T extends string | number>(order: readonly T[], onDrop: (from: T, to: T, after: boolean) => void) {
  const kind = useId();
  const [dragging, setDragging] = useState<T | null>(null);
  const [over, setOver] = useState<Drop<T> | null>(null);
  const latest = useRef(onDrop);
  latest.current = onDrop;
  const end = () => { setDragging(null); setOver(null); };
  const keyOf = (data: Record<string | symbol, unknown>) => data.kind === kind ? data.key as T : null;
  const handleRef = useKeyedRefs<T>((key, element) => draggableByPointer({
    element,
    getInitialData: () => ({ kind, key }),
    onDragStart: () => setDragging(key),
    onDrop: end,
  }));
  const targetRef = useKeyedRefs<T>((key, element) => {
    // The side of the row the pointer is on; the nearest row takes it, not a module holding rows.
    const show = (data: Record<string | symbol, unknown>, clientY: number, nearest: boolean) => {
      if (!nearest || keyOf(data) === key) return setOver((current) => current?.key === key ? null : current);
      const box = element.getBoundingClientRect(), after = clientY > box.top + box.height / 2;
      setOver((current) => current?.key === key && current.after === after ? current : { key, after });
    };
    return dropTargetByPointer({
      element,
      canDrop: ({ source }) => keyOf(source.data) !== null,
      onDragEnter: ({ source, location, self }) => show(source.data, location.current.input.clientY, location.current.dropTargets[0]?.element === self.element),
      onDrag: ({ source, location, self }) => show(source.data, location.current.input.clientY, location.current.dropTargets[0]?.element === self.element),
      onDragLeave: () => setOver((current) => current?.key === key ? null : current),
      onDrop: ({ source, location, self }) => {
        const from = keyOf(source.data);
        if (from === null || from === key || location.current.dropTargets[0]?.element !== self.element) return;
        const box = element.getBoundingClientRect();
        latest.current(from, key, location.current.input.clientY > box.top + box.height / 2);
      },
    });
  });
  return {
    handle: (key: T) => ({
      ref: handleRef(key),
      draggable: true,
      onKeyDown: (event: KeyboardEvent<HTMLElement>) => {
        if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
        const index = order.indexOf(key), target = order[index + (event.key === "ArrowUp" ? -1 : 1)];
        if (index < 0 || target === undefined) return;
        event.preventDefault();
        onDrop(key, target, event.key === "ArrowDown");
      },
    }),
    target: (key: T) => ({
      ref: targetRef(key),
      "data-drop": over?.key === key && dragging !== key ? (over.after ? "after" : "before") : undefined,
    }),
  };
}
