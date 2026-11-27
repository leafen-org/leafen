import { useEffect, useRef } from "react";

const START_AFTER = 4;

// The click that ends a pull doesn't fold the teamspace it lands on.
function swallowClick() {
  const swallow = (click: Event) => { click.preventDefault(); click.stopPropagation(); };
  window.addEventListener("click", swallow, { capture: true, once: true });
  window.setTimeout(() => window.removeEventListener("click", swallow, { capture: true }), 200);
}

/**
 * A teamspace's name row pressed and pulled moves the whole teamspace, its
 * pages with it: it floats under the pointer while the others step aside,
 * and let go it takes that place in the reader's order, as Notion's sidebar
 * does (seen 2026-11-13: no line, the others slide; the order is the one the
 * library's teamspaces tab keeps). Escape puts it back. A press without a pull
 * stays the row's own: it folds the teamspace.
 */
export function useTeamspaceOrderDrag(list: HTMLElement | null, onMove: (order: string[]) => void) {
  const latest = useRef(onMove);
  latest.current = onMove;
  useEffect(() => {
    if (!list) return undefined;
    const down = (start: PointerEvent) => {
      if (start.button !== 0 || !start.isPrimary || !(start.target instanceof Element)) return;
      const head = start.target.closest<HTMLElement>("[data-sidebar-teamspace-head]");
      if (!head || start.target.closest("button")) return;
      const pressed = head.closest<HTMLElement>("[data-sidebar-teamspace]");
      const blocks = [...list.querySelectorAll<HTMLElement>(":scope > [data-sidebar-teamspace]")];
      const from = pressed ? blocks.indexOf(pressed) : -1;
      if (!pressed || from < 0 || blocks.length < 2) return;
      const rects = blocks.map((block) => block.getBoundingClientRect());
      const middles = rects.map((rect) => rect.top + rect.height / 2);
      // The room it leaves: up to the next teamspace, or for the last its
      // height and the gap an open one keeps under it.
      const room = from < blocks.length - 1 ? rects[from + 1].top - rects[from].top
        : rects[from].height + (pressed.childElementCount > 1 ? 14 : 1);
      let to = from, dragging = false;
      const clear = () => blocks.forEach((block) => {
        block.style.transform = ""; block.style.transition = ""; block.style.zIndex = ""; block.style.position = "";
        block.style.cursor = "";
        block.removeAttribute("data-dragging");
      });
      const move = (event: PointerEvent) => {
        if (event.pointerId !== start.pointerId) return;
        const moved = event.clientY - start.clientY;
        if (!dragging) {
          if (Math.abs(moved) < START_AFTER) return;
          dragging = true;
          pressed.setAttribute("data-dragging", "true");
          pressed.style.position = "relative";
          pressed.style.zIndex = "1";
          pressed.style.cursor = "grabbing";
        }
        const middle = middles[from] + moved;
        to = middles.filter((at, index) => index !== from && at < middle).length;
        pressed.style.transform = `translateY(${moved}px)`;
        blocks.forEach((block, index) => {
          if (block === pressed) return;
          block.style.transition = "transform 120ms ease";
          block.style.transform = index > from && index <= to ? `translateY(${-room}px)`
            : index < from && index >= to ? `translateY(${room}px)` : "";
        });
      };
      const stop = () => {
        window.removeEventListener("pointermove", move);
        window.removeEventListener("pointerup", up, true);
        window.removeEventListener("pointercancel", cancel);
        window.removeEventListener("keydown", escape, true);
        clear();
      };
      const up = (event: PointerEvent) => {
        if (event.pointerId !== start.pointerId) return;
        stop();
        if (!dragging) return;
        swallowClick();
        if (to === from) return;
        const ids = blocks.map((block) => block.dataset.sidebarTeamspace ?? "");
        ids.splice(to, 0, ...ids.splice(from, 1));
        latest.current(ids);
      };
      const cancel = (event: PointerEvent) => { if (event.pointerId === start.pointerId) stop(); };
      const escape = (event: KeyboardEvent) => {
        if (event.key !== "Escape" || !dragging) return;
        event.preventDefault();
        event.stopPropagation();
        stop();
        // Let go later, the pointer neither moves nor folds it.
        window.addEventListener("pointerup", swallowClick, { capture: true, once: true });
      };
      window.addEventListener("pointermove", move);
      window.addEventListener("pointerup", up, true);
      window.addEventListener("pointercancel", cancel);
      window.addEventListener("keydown", escape, true);
    };
    // A teamspace's picture icon would start the browser's own drag of it.
    const nativeDrag = (event: DragEvent) => {
      if (event.target instanceof Element && event.target.closest("[data-sidebar-teamspace-head]")) event.preventDefault();
    };
    list.addEventListener("pointerdown", down);
    list.addEventListener("dragstart", nativeDrag);
    return () => {
      list.removeEventListener("pointerdown", down);
      list.removeEventListener("dragstart", nativeDrag);
    };
  }, [list]);
}
