import { type RefObject, useEffect, useRef } from "react";

/**
 * Calls `onNear` once `target` comes within `distance` px under the foot of
 * what scrolls the page (`[data-page-scroll]`, else the window), as a
 * database page's board loads a column's next cards before its end shows.
 * The watch starts afresh whenever `key` changes, so a target still near
 * after more have drawn asks again.
 */
export function useNearScrollEnd(target: RefObject<HTMLElement | null>, onNear: (() => void) | undefined, distance: number, key: unknown) {
  const latest = useRef(onNear);
  latest.current = onNear;
  const watching = !!onNear;
  useEffect(() => {
    const element = target.current;
    if (!watching || !element || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(([entry]) => {
      if (entry?.isIntersecting) latest.current?.();
    }, { root: element.closest("[data-page-scroll]"), rootMargin: `0px 0px ${distance}px 0px` });
    observer.observe(element);
    return () => observer.disconnect();
  }, [distance, key, target, watching]);
}
