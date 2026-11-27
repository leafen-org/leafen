import { useLayoutEffect, useRef, useState, type RefObject } from "react";

/**
 * Whether a head's note goes on its own line under the head: when the head
 * can't show it whole beside its buttons, it is read whole under it, and it
 * comes back once the head is as wide as it needed (a phone's file head cut
 * "○○님도 이 문서를 열어 두었습니다" to "○○님도 …", 2026-11-12). A head that
 * fits it again shows it there before the screen is drawn, so nothing jumps.
 */
export function useNoteUnderHead(head: RefObject<HTMLElement | null>, note: RefObject<HTMLElement | null>, text: string) {
  const [under, setUnder] = useState(false);
  const needed = useRef(0);
  useLayoutEffect(() => {
    const box = head.current;
    if (!box || !text) return undefined;
    const check = () => {
      const line = note.current;
      if (!under) {
        if (line && line.scrollWidth > line.clientWidth) {
          needed.current = box.clientWidth + line.scrollWidth - line.clientWidth;
          setUnder(true);
        }
      } else if (box.clientWidth >= needed.current) setUnder(false);
    };
    check();
    const observer = new ResizeObserver(check);
    observer.observe(box);
    return () => observer.disconnect();
  }, [head, note, text, under]);
  return under;
}
