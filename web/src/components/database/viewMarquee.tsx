import { useEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import styles from "./PeekHalo.module.css";

type Point = { x: number; y: number };

// A press here is its own: an item, a control, a field, a menu, or what a drag moves.
const OWN_PRESS = [
  "button", "a[href]", "input", "textarea", "select", "label", '[contenteditable="true"]',
  '[role="button"]', '[role="menuitem"]', '[role="checkbox"]', '[role="switch"]', '[role="tab"]',
  '[role="row"]', '[role="gridcell"]', '[role="columnheader"]',
  '[role="menu"]', '[role="dialog"]', '[role="listbox"]', '[draggable="true"]', "[data-native-drag-source]",
  // A line that takes its own press, a drag along it no box (TimelineCreateLane's `atPress`).
  "[data-own-press]",
].join(", ");

/** The room a view draws in: its own box, or that of what it holds where it is drawn as `display: contents`. */
function viewBox(view: HTMLElement) {
  const own = view.getBoundingClientRect();
  if (own.width || own.height) return own;
  const boxes = [...view.children].map((child) => child.getBoundingClientRect()).filter((box) => box.width || box.height);
  if (!boxes.length) return null;
  return { top: Math.min(...boxes.map((box) => box.top)), bottom: Math.max(...boxes.map((box) => box.bottom)) };
}

// A press on an element's scrollbar drags the scrollbar.
function onScrollbar(element: Element, x: number, y: number) {
  if (!element.clientWidth && !element.clientHeight) return false;
  const box = element.getBoundingClientRect();
  const left = box.left + element.clientLeft, top = box.top + element.clientTop;
  return x < left || x >= left + element.clientWidth || y < top || y >= top + element.clientHeight;
}

/**
 * A drag from a view's empty room chooses every item its box touches, as
 * Notion's board, gallery, list and table do (seen 2026-11-12): from between
 * a board's cards or columns, under them, or the page's margin beside the
 * view, the box drawn in Notion's blue (rgba(35,131,226,.14)) chooses the
 * items it touches as it grows, and they stay chosen once it is let go —
 * their keys and menus are then those of items chosen any other way. A new
 * drag chooses afresh; one begun with Shift adds to what is chosen. It
 * chooses no page block, even where it reaches out past the database, and
 * the press it ends with is no press on what it ends over (a calendar's day
 * makes no page). A finger's drag scrolls instead. `onBox` hears of each box
 * as it starts, whatever it goes on to choose.
 */
export function useViewMarquee({ root, item, room, idOf, chosen, choose, onBox, enabled = true }: {
  root: RefObject<HTMLElement | null>;
  /** The view's items. */
  item: string;
  /** Its own parts a drag starts from though they are a grid's (a calendar's days and weeks). */
  room?: string;
  idOf: (element: HTMLElement) => string | undefined;
  chosen: () => ReadonlySet<string>;
  choose: (ids: readonly string[]) => void;
  onBox?: () => void;
  enabled?: boolean;
}): ReactNode {
  const [box, setBox] = useState<{ start: Point; end: Point } | null>(null);
  const latest = useRef({ item, room, idOf, chosen, choose, onBox });
  latest.current = { item, room, idOf, chosen, choose, onBox };
  useEffect(() => {
    if (!enabled) return;
    let stop: (() => void) | null = null;
    const down = (event: PointerEvent) => {
      const view = root.current, target = event.target;
      if (!view || !(target instanceof Element) || event.button !== 0 || event.pointerType === "touch"
        || event.metaKey || event.ctrlKey || event.altKey) return;
      const inside = view.contains(target);
      if (inside) {
        // The page's own editing around an inline view is not the view's.
        const own = target.closest(OWN_PRESS), room = latest.current.room;
        if ((own && view.contains(own) && !(room && own.matches(room))) || target.closest(latest.current.item)) return;
      } else if (!target.contains(view)) return;
      // The page's layout is read last, for a press that may start a box: read
      // first, a press on a control worked the page out before its own
      // reaction could show (a table's "+ 새 페이지", 20–70 ms at CPU ×2, 2026-11-15).
      if (onScrollbar(target, event.clientX, event.clientY)) return;
      if (!inside) {
        // The page's margin beside the view, as Notion's view reaches across it.
        const room = viewBox(view);
        if (!room || event.clientY < room.top || event.clientY > room.bottom) return;
        // Not the page's own drag over its blocks.
        event.stopPropagation();
      }
      stop?.();
      const start = { x: event.clientX, y: event.clientY }, pointerId = event.pointerId;
      const base = event.shiftKey ? [...latest.current.chosen()] : [];
      let moving = false, shown = "";
      const quiet = (selecting: Event) => selecting.preventDefault();
      const move = (next: PointerEvent) => {
        if (next.pointerId !== pointerId) return;
        if (!moving && Math.abs(next.clientX - start.x) < 4 && Math.abs(next.clientY - start.y) < 4) return;
        if (!moving) {
          moving = true;
          document.addEventListener("selectstart", quiet, true);
          // Keys go to what it chooses, not a field left with focus.
          const active = document.activeElement;
          if (active instanceof HTMLElement && active !== document.body) active.blur();
          latest.current.onBox?.();
        }
        next.preventDefault();
        window.getSelection()?.removeAllRanges();
        const end = { x: next.clientX, y: next.clientY };
        setBox({ start, end });
        const left = Math.min(start.x, end.x), right = Math.max(start.x, end.x);
        const top = Math.min(start.y, end.y), bottom = Math.max(start.y, end.y);
        const { item: selector, idOf: read } = latest.current;
        // Nearest where it began first: a choice made so stretches from there,
        // as Notion's (seen 2026-11-12: a box drawn up over 일 5 and 일 6,
        // Shift+↓ leaves 일 5).
        const away = (rect: DOMRect) => Math.hypot(Math.max(rect.left - start.x, 0, start.x - rect.right), Math.max(rect.top - start.y, 0, start.y - rect.bottom));
        const touched = [...view.querySelectorAll<HTMLElement>(selector)].map((element) => ({ element, rect: element.getBoundingClientRect() }))
          .filter(({ rect }) => rect.width > 0 && rect.left < right && rect.right > left && rect.top < bottom && rect.bottom > top)
          .sort((a, b) => away(a.rect) - away(b.rect)).map(({ element }) => read(element)).filter((id): id is string => Boolean(id));
        const ids = [...new Set([...base, ...touched])];
        if (ids.join("\n") !== shown) { shown = ids.join("\n"); latest.current.choose(ids); }
      };
      const end = (up: PointerEvent) => { if (up.pointerId === pointerId) stop?.(); };
      stop = () => {
        window.removeEventListener("pointermove", move, true);
        window.removeEventListener("pointerup", end, true);
        window.removeEventListener("pointercancel", end, true);
        document.removeEventListener("selectstart", quiet, true);
        if (moving) {
          // The press it ends with is its own.
          const swallow = (click: MouseEvent) => { click.preventDefault(); click.stopPropagation(); };
          window.addEventListener("click", swallow, { capture: true, once: true });
          window.setTimeout(() => window.removeEventListener("click", swallow, true), 0);
        }
        setBox(null);
        stop = null;
      };
      window.addEventListener("pointermove", move, true);
      window.addEventListener("pointerup", end, true);
      window.addEventListener("pointercancel", end, true);
    };
    document.addEventListener("pointerdown", down, true);
    return () => {
      document.removeEventListener("pointerdown", down, true);
      stop?.();
    };
  }, [enabled, root]);
  if (!box) return null;
  return createPortal(<div className={styles.marquee} aria-hidden="true" style={{
    left: Math.min(box.start.x, box.end.x), top: Math.min(box.start.y, box.end.y),
    width: Math.abs(box.end.x - box.start.x), height: Math.abs(box.end.y - box.start.y),
  }} />, document.body);
}
