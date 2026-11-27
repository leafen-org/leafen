import { type RefObject, useEffect } from "react";

/**
 * On a touch screen, what a pointer over `ref` brings out on a desktop, a
 * press in it brings out, as Notion's iPad shows a hovered part on the tap
 * that lands there: the element carries `data-pressed-within` from a press
 * inside it until a press lands elsewhere, a press on a window or menu
 * drawn over the page (its tab's menu, say) leaving it as it is. The mark is
 * set on the element itself, so nothing is drawn again for it.
 */
export function usePressShowsWithin(ref: RefObject<HTMLElement | null>, on: boolean) {
  useEffect(() => {
    const element = ref.current;
    if (!on || !element) return undefined;
    const pressed = (event: PointerEvent) => {
      const target = event.target instanceof Element ? event.target : null;
      if (element.contains(target)) element.setAttribute("data-pressed-within", "");
      else if (!target?.closest("[data-overlay-root]")) element.removeAttribute("data-pressed-within");
    };
    document.addEventListener("pointerdown", pressed, true);
    return () => {
      document.removeEventListener("pointerdown", pressed, true);
      element.removeAttribute("data-pressed-within");
    };
  }, [ref, on]);
}

