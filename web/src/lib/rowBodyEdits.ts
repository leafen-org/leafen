import { useEffect, useRef } from "react";

// A database page's body edited here by the person reading it: whatever shows
// its row shows it last edited now, by them, as a value edit does — Notion's
// "최종 편집 일시" turns within three seconds of a body edit (seen 2026-11-09).
const listeners = new Set<(rowId: string) => void>();

export function noteRowBodyEdited(rowId: string) {
  for (const listener of listeners) listener(rowId);
}

/** Hears each row whose body is edited here. */
export function useRowBodyEdits(onEdited: (rowId: string) => void) {
  const latest = useRef(onEdited);
  latest.current = onEdited;
  useEffect(() => {
    const hear = (rowId: string) => latest.current(rowId);
    listeners.add(hear);
    return () => { listeners.delete(hear); };
  }, []);
}
