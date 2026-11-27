import { useRef, useState, type Dispatch, type SetStateAction } from "react";

/**
 * A state whose setter queues nothing when it is handed the value it last
 * took (or started from), or a function giving that value back: a place with
 * nothing new to show adds no update to React's queue. Every write goes
 * through the setter, so the value it last took is the one React holds once
 * its queue has run, and a function is handed that value. The setter stays
 * the same from drawing to drawing.
 *
 * React's own setter queues a same value too once its component has drawn
 * for another change, and draws it again for it. An effect that puts a value
 * back after each change of the data it follows — a busy mark, a file's
 * upload, the rows chosen, the row an address opens — then drew the page once
 * more after every key typed into a table's cell; typed fast, React took the
 * fifty drawings in a row for a loop and threw the page away (2026-11-16: a
 * long title typed into a new row, NAS).
 */
export function useStateUnlessSame<T>(initial: T | (() => T)): [T, Dispatch<SetStateAction<T>>] {
  const [value, setValue] = useState(initial);
  const last = useRef(value);
  const [set] = useState(() => (next: SetStateAction<T>) => {
    const value = typeof next === "function" ? (next as (current: T) => T)(last.current) : next;
    if (Object.is(last.current, value)) return;
    last.current = value;
    setValue(value);
  });
  return [value, set];
}
