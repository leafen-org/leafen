import { type RefObject, useLayoutEffect, useState } from "react";

type RowWatch = { observer: MutationObserver; readers: Set<() => void> };
const watched = new WeakMap<Element, RowWatch>();

/**
 * A table cell's place in its row, read from where its view stands. The
 * editor keeps a cell's view when cells are added, taken away or moved
 * before it and, its own node being the same, does not draw it again, so a
 * place read once while drawing goes stale (2026-11-09: after "왼쪽에 삽입"
 * the first cells kept the next column's color, width and name until the
 * page was opened again). One watcher per row tells its cells when the
 * row's cells change.
 */
export function useTableCellPlace(ref: RefObject<HTMLElement | null>, drawn: number): number {
  const [place, setPlace] = useState<number | null>(null);
  useLayoutEffect(() => {
    let stop: (() => void) | undefined;
    let frame = 0;
    const attach = (tries: number) => {
      // The view is put in its row after it is first drawn.
      const view = ref.current?.parentElement, row = view?.parentElement;
      if (!view || !row) {
        if (tries > 0) frame = window.requestAnimationFrame(() => attach(tries - 1));
        return;
      }
      const read = () => {
        const at = Array.prototype.indexOf.call(row.children, view);
        if (at >= 0) setPlace(at);
      };
      read();
      let watch = watched.get(row);
      if (!watch) {
        const readers = new Set<() => void>();
        watch = { readers, observer: new MutationObserver(() => readers.forEach((reader) => reader())) };
        watch.observer.observe(row, { childList: true });
        watched.set(row, watch);
      }
      const own = watch;
      own.readers.add(read);
      stop = () => {
        own.readers.delete(read);
        if (own.readers.size) return;
        own.observer.disconnect();
        watched.delete(row);
      };
    };
    attach(3);
    return () => {
      window.cancelAnimationFrame(frame);
      stop?.();
    };
  }, [ref]);
  return place ?? drawn;
}
