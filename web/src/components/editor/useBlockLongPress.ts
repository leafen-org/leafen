import { useEffect, useRef, type MouseEvent, type PointerEvent } from "react";
import { touchScreen } from "@/lib/screenKind";

/** How long a finger rests before it is a long press. */
const LONG_PRESS_MS = 500;
/** How far a finger goes before it is a scroll rather than a press. */
const SLOP = 10;
/**
 * Where a long press is the browser's or the part's own: the block's text (the
 * system's selection), a caption, a field, a link, a player or a frame (the
 * system's own menu or controls), and an inline database's cells and views.
 * A file's row keeps its link's press (its name, data-file-row-link), and a
 * picture is its block's, as Notion's iPhone opens an image's "작업" sheet
 * from it (seen 2026-11-14).
 */
const NOT_THE_BLOCKS = [
  "[data-rt-editable]", "[data-native-caption]", "[data-native-table-cell]", "pre", "input", "textarea", "select",
  "a[href]:not([data-file-row-link])", "video", "audio", "iframe", "[data-inline-database-wrapper]",
].join(", ");

/**
 * A block's long press on a touch screen opens its menu where the finger is,
 * as Notion's does on a block with no text of its own (seen 2026-11-13: a
 * file's or a PDF's row on an iPhone, the "작업" sheet; on an iPad, the
 * block chosen and its menu beside the finger; 2026-11-14 a picture's). A phone's row of a file or a
 * PDF shows no tools, so this is how its menu ("바꾸기", "다운로드") is reached.
 * The press that follows is the menu's, not the row's: it does not open the
 * file. Android Chrome's own long press ("contextmenu") opens the same menu.
 *
 * `group` is the block's own element: a press in a block inside it is that
 * block's. `open` is called with the finger's place.
 */
export function useBlockLongPress({ enabled, inner, open }: {
  enabled: boolean;
  /** Whether the press is this block's: not in a block inside it, not in its text. */
  inner: (target: Element, group: HTMLElement) => boolean;
  open: (point: { x: number; y: number }, group: HTMLElement) => void;
}) {
  const press = useRef<{ id: number; x: number; y: number; group: HTMLElement; timer: number } | null>(null);
  // The click a long press ends in, which the menu took.
  const swallow = useRef(false);
  const stop = () => {
    if (press.current) window.clearTimeout(press.current.timer);
    press.current = null;
  };
  useEffect(() => stop, []);
  const fire = () => {
    const held = press.current;
    if (!held) return;
    stop();
    swallow.current = true;
    open({ x: held.x, y: held.y }, held.group);
  };
  return {
    onPointerDown(event: PointerEvent<HTMLElement>) {
      swallow.current = false;
      if (!enabled || event.pointerType !== "touch" || !event.isPrimary || !touchScreen()) return;
      const target = event.target instanceof Element ? event.target : null;
      // A press in a window the block drew elsewhere (its menu) is not on it.
      if (!target || !event.currentTarget.contains(target) || target.closest(NOT_THE_BLOCKS) || !inner(target, event.currentTarget)) return;
      stop();
      press.current = { id: event.pointerId, x: event.clientX, y: event.clientY, group: event.currentTarget,
        timer: window.setTimeout(fire, LONG_PRESS_MS) };
    },
    onPointerMove(event: PointerEvent<HTMLElement>) {
      const held = press.current;
      if (held && event.pointerId === held.id && Math.hypot(event.clientX - held.x, event.clientY - held.y) > SLOP) stop();
    },
    onPointerUp: stop,
    onPointerCancel: stop,
    onClickCapture(event: MouseEvent<HTMLElement>) {
      // The menu is drawn elsewhere and its presses come here through React, its rows' own
      // pointer handling keeping the press from starting over: a press in it is the menu's,
      // not the long press's end (2026-11-13: an iPad's first press there, "추가 옵션", did nothing).
      if (!swallow.current || !(event.target instanceof Node) || !event.currentTarget.contains(event.target)) return;
      swallow.current = false;
      event.preventDefault();
      event.stopPropagation();
    },
    /** Android's long press: the same menu, and not the browser's. */
    onContextMenuCapture(event: MouseEvent<HTMLElement>) {
      if (!press.current && !swallow.current) return;
      event.preventDefault();
      event.stopPropagation();
      fire();
    },
  };
}
