import { useEffect, useLayoutEffect, useRef, type RefObject } from "react";
import { inlineScrolled, scrollToInlineEnd } from "@/lib/readingDirection";
import { holdMeasure } from "./heldStyleWrites";

/**
 * A table scrolled to its end stays at its end when a column goes (hidden or
 * deleted), as Notion's (seen 2026-11-06: "선택" hidden, the table at 326 of
 * 326): the browser keeps a scroll in range at once, but a layout drawn on the
 * way, narrower than the last, otherwise leaves it short of the end.
 */
export function useTableEndKept(scrollRef: RefObject<HTMLElement | null>, columns: number) {
  const atEnd = useRef(false);
  useEffect(() => {
    const scroll = scrollRef.current;
    if (!scroll) return undefined;
    const note = () => { atEnd.current = inlineScrolled(scroll) > 0 && inlineScrolled(scroll) >= scroll.scrollWidth - scroll.clientWidth - 1; };
    // First with the view's other first measures (heldStyleWrites).
    const dropFirst = holdMeasure(note, note);
    scroll.addEventListener("scroll", note, { passive: true });
    return () => { dropFirst(); scroll.removeEventListener("scroll", note); };
  }, [scrollRef]);
  const before = useRef(columns);
  useLayoutEffect(() => {
    const fewer = columns < before.current;
    before.current = columns;
    const scroll = scrollRef.current;
    if (!fewer || !scroll || !atEnd.current) return;
    const toEnd = () => scrollToInlineEnd(scroll);
    toEnd();
    window.requestAnimationFrame(toEnd);
  }, [scrollRef, columns]);
}
