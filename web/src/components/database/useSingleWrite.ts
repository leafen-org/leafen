import { useEffect, useRef } from "react";

/**
 * A cell editor's writer that sends each value once: a write equal to the
 * last one sent is dropped until the cell shows a value again. A day picked
 * moves the focus from the date's field to the day, and the field, left
 * before the cell shows the pick, would write the same date again.
 */
export function useSingleWrite<T>(shown: unknown, send: (next: T) => void) {
  const last = useRef<string | null>(null);
  const shownKey = JSON.stringify(shown ?? null);
  useEffect(() => { last.current = null; }, [shownKey]);
  return (next: T) => {
    const key = JSON.stringify(next);
    if (last.current === key) return;
    last.current = key;
    send(next);
  };
}
