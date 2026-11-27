import { useLayoutEffect, useState, type RefObject } from "react";

/**
 * The width a chart draws across, as its box is laid out now: Notion's
 * charts keep their bars, text and height in pixels at any width (seen
 * 2026-10-05: 16px bars, 12px text, 400px tall at "중간"). A chart drawn
 * for its saved picture takes the picture's own width instead (`fixed`,
 * chartExport.ts), whatever the screen's.
 */
export function useChartWidth(ref: RefObject<HTMLElement | null>, fixed?: number) {
  const [width, setWidth] = useState(fixed ?? 720);
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element || fixed !== undefined) return;
    const measure = () => {
      const next = Math.round(element.getBoundingClientRect().width);
      if (next > 0) setWidth((current) => current === next ? current : next);
    };
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [ref, fixed]);
  return fixed ?? width;
}
