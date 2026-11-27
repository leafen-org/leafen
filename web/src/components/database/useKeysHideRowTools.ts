import { useEffect, type RefObject } from "react";
import { touchScreen } from "@/lib/screenKind";
import { layerOrigin } from "@/lib/overlayLayer";
import type { CellToolsShown } from "./cellToolsShown";

const MODIFIERS = new Set(["Shift", "Control", "Alt", "Meta", "CapsLock", "Fn", "OS"]);

/**
 * A key pressed in a table puts away what its rows show under a still
 * pointer — a row's "+ ⋮⋮" and box, its "열기", a value's tools — until the
 * mouse moves or presses, as Notion's (seen 2026-11-12 and 2026-11-14:
 * a title typed and left with Enter or Escape, a text left with Escape, a
 * number with Enter, the row under the pointer bare). A key in a window or
 * picker opened from the table counts too: a select's picker closed by
 * Escape leaves the row under the pointer bare until it moves (Notion's,
 * seen 2026-11-26 in Chrome at 1440 and 1000, two tabs at two times, after
 * 2 and 7 seconds too, and in a timeline's table). A chosen or dragged row
 * keeps its box. The table's scroll box (a timeline's table's rows) carries
 * it as `data-keys-pressed` for the styles (DatabaseTableRows,
 * DatabaseTimelineTable); the cell's tools are the table's own (cellToolsShown).
 */
export function useKeysHideRowTools(scrollRef: RefObject<HTMLElement | null>, cellTools: CellToolsShown) {
  useEffect(() => {
    const box = scrollRef.current;
    if (!box) return;
    let last: { x: number; y: number } | null = null;
    const hide = (event: KeyboardEvent) => {
      if (MODIFIERS.has(event.key) || touchScreen() || !(event.target instanceof Element)) return;
      if (!box.contains(event.target) && !box.contains(layerOrigin(event.target))) return;
      if (box.dataset.keysPressed !== "true") box.dataset.keysPressed = "true";
      cellTools.pointerLeave();
    };
    const show = (event: PointerEvent) => {
      const moved = event.type === "pointerdown" || !last || last.x !== event.clientX || last.y !== event.clientY;
      last = { x: event.clientX, y: event.clientY };
      if (!moved || box.dataset.keysPressed !== "true") return;
      delete box.dataset.keysPressed;
      if (box.contains(event.target as Node | null)) cellTools.pointerOver(event.target);
    };
    window.addEventListener("keydown", hide, true);
    window.addEventListener("pointermove", show, { capture: true, passive: true });
    window.addEventListener("pointerdown", show, { capture: true, passive: true });
    return () => {
      window.removeEventListener("keydown", hide, true);
      window.removeEventListener("pointermove", show, true);
      window.removeEventListener("pointerdown", show, true);
    };
  }, [scrollRef, cellTools]);
}
