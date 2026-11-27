import { useEffect, useState } from "react";

type MainContentRect = { left: number; top: number; width: number; height: number };

// A row opened as a full page covers the main content area beside the
// sidebar, following it as the window or the sidebar changes size, and ends
// where that area ends: above the developer bar, and above a tablet's home
// bar as every page there does (AppShell's --app-foot-inset).
export function useMainContentRect(active: boolean) {
  const [rect, setRect] = useState<MainContentRect | null>(null);
  useEffect(() => {
    if (!active || typeof document === "undefined") {
      setRect(null);
      return;
    }
    const main = document.getElementById("app-main-content");
    const measure = () => {
      const bounds = main?.getBoundingClientRect();
      setRect(bounds
        ? { left: bounds.left, top: bounds.top, width: bounds.width, height: bounds.height }
        : { left: 0, top: 0, width: window.innerWidth, height: window.innerHeight });
    };
    measure();
    const observer = typeof ResizeObserver === "undefined" || !main ? null : new ResizeObserver(measure);
    if (main) observer?.observe(main);
    window.addEventListener("resize", measure);
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [active]);
  return rect;
}
