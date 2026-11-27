import { useEffect, useRef } from "react";

/**
 * A field that keeps what was typed as it is left (blur) or on Enter keeps it
 * too when it goes another way: its window closed by a press outside, Escape,
 * its × or a phone sheet swiped down, or the field turned to another thing
 * (`key`, a view chosen while its settings stay open). A window closed so
 * takes the field away before its blur reaches the page, and the words were
 * lost; Notion's keep them (seen 2026-11-18 in Chrome 1440: a view's name
 * typed in its settings stays after a press outside, Escape and ×, the tab
 * renamed only then; on a Galaxy A90 after the sheet was swiped down).
 *
 * `keep` is called with what was typed and not yet kept, from the field's
 * last drawing before it went (so for the thing it showed then).
 */
export function useTypedKeptOnLeave(key: string, keep: (value: string) => void) {
  const typed = useRef<string | null>(null);
  const latest = useRef(keep);
  // Set after each drawing: when the field turns to another thing, the
  // leaving below runs first and still reads the one it showed.
  useEffect(() => { latest.current = keep; });
  useEffect(() => () => {
    const value = typed.current;
    typed.current = null;
    if (value !== null) latest.current(value);
  }, [key]);
  return {
    /** The field's words as typed, kept as it goes unless kept before. */
    typed(value: string) { typed.current = value; },
    /** What was typed is kept (blur, Enter) or set aside (the field shows another value). */
    settled() { typed.current = null; },
  };
}
