import { type RefObject, useLayoutEffect, useRef, useState } from "react";

/**
 * Whether an open popover's element is still the one it was placed as
 * (usePopoverPlacement): one drawn anew under the same ref while it is open,
 * with nothing else of it changed, is placed again — a "/" menu typed past
 * what it holds, empty until an app's blocks the query finds arrive, and its
 * list then drawn as a new element (2026-11-16: placed over its line as its
 * short empty frame, the list drew from that top down over the line typed in,
 * and followed neither scrolling nor the window, its placing left on the
 * element gone). `count` goes up once for each such drawing, until the
 * placing it asks for has run; `place` takes the element each placing read,
 * or undefined as it closes.
 */
export function usePopoverDrawnAnew(ref: RefObject<HTMLElement | null>, open: boolean) {
  const [count, setCount] = useState(0);
  const last = useRef<{ placed?: HTMLElement | null; asked: boolean }>({ asked: false });
  useLayoutEffect(() => {
    const now = last.current;
    if (!open || now.asked || now.placed === undefined || ref.current === now.placed) return;
    now.asked = true;
    setCount((drawn) => drawn + 1);
  });
  return { count, place: (element: HTMLElement | null | undefined) => { last.current = { placed: element, asked: false }; } };
}
