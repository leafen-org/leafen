import { type RefObject, useLayoutEffect } from "react";

/**
 * Keeps the list's head under the top bar as the list scrolls, as Notion's
 * (seen 2026-11-03). The table scrolls sideways in its own box, which a sticky
 * head would stick to, so the head follows the page's scroll instead.
 */
export function useLibraryStickyHead(ref: RefObject<HTMLElement | null>) {
  useLayoutEffect(() => {
    const head = ref.current;
    if (!head) return undefined;
    // The table's own box scrolls only sideways; what moves the head is the
    // first box above it that has more to scroll down to.
    const scroller = () => {
      let box = head.parentElement;
      while (box && !(/(auto|scroll)/u.test(getComputedStyle(box).overflowY) && box.scrollHeight > box.clientHeight + 1)) box = box.parentElement;
      return box;
    };
    const place = () => {
      head.style.transform = "";
      const box = scroller();
      if (!box) return;
      const bar = parseFloat(getComputedStyle(head).getPropertyValue("--topbar-height")) || 0;
      const under = box.getBoundingClientRect().top + bar - head.getBoundingClientRect().top;
      head.style.transform = under > 0 ? `translateY(${under}px)` : "";
      head.toggleAttribute("data-stuck", under > 0);
    };
    place();
    // Scrolls don't bubble; caught on the way down, any box above is heard.
    document.addEventListener("scroll", place, { capture: true, passive: true });
    window.addEventListener("resize", place);
    return () => {
      document.removeEventListener("scroll", place, { capture: true });
      window.removeEventListener("resize", place);
    };
  }, [ref]);
}
