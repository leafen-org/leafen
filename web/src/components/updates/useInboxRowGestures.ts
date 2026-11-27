import { useEffect, useRef, useState, type MouseEvent, type PointerEvent, type TransitionEvent } from "react";

/** How far a finger goes before a move is a swipe rather than a tap. */
const SLOP = 10;
/** How long a finger rests before it is a long press. */
const LONG_PRESS_MS = 500;
/** The one action a swipe left shows is this wide, as Notion's Android app's "보관" (seen 2026-11-21: 98dp at 411). */
export const INBOX_SWIPE_ACTION_WIDTH = 98;
/**
 * Past this share of the row's width a swipe left archives as it is let go.
 * Notion's Android app archived at 69% and not at 60% (seen 2026-11-21 on a
 * Galaxy A90); where between is not seen.
 */
const LONG_SWIPE = 0.65;

/** Its menus draw elsewhere on the page, yet their events come up through the row. */
const inRow = (event: { currentTarget: HTMLElement; target: EventTarget }) => event.currentTarget.contains(event.target as Node);

/**
 * A notification's gestures on a touch screen, as Notion's Android app does
 * them (seen 2026-11-21 on a Galaxy A90, twice): a swipe right moves no row;
 * a swipe left draws the one action under it, "보관" at the row's end, and a
 * long one archives it as it is let go; pulled out less than the action's
 * width it goes back, more and it rests open (seen: 61dp went back, 114dp
 * stayed — the action's width is taken between). An open row stays open
 * whatever is tapped beside it, a swipe right closing it; a tap on it opens
 * it as ever. A long press opens a small card of its actions under it
 * (InboxRowCard). A mouse keeps the hover tools; a vertical move scrolls the list.
 */
export function useInboxRowGestures({ actions, onLongSwipe, onLongPress }: {
  /** Whether a swipe left shows its action. */
  actions: boolean;
  onLongSwipe(): void;
  onLongPress(): void;
}) {
  const [offset, setOffset] = useState(0);
  const [dragging, setDragging] = useState(false);
  // Whether the action is drawn: kept while the row slides back, so its
  // ground goes back with it.
  const [drawn, setDrawn] = useState(false);
  const open = useRef(false);
  const touch = useRef<{ id: number; x: number; y: number; from: number; width: number; swiping: boolean } | null>(null);
  const timer = useRef<number | null>(null);
  // A long press or a swipe ends in a click the row should not take as opening it.
  const swallowClick = useRef(false);
  const latest = useRef(offset);
  latest.current = offset;

  const stopTimer = () => {
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = null;
  };
  const settle = (shown: boolean) => {
    open.current = shown;
    setDragging(false);
    setOffset(shown ? -INBOX_SWIPE_ACTION_WIDTH : 0);
    if (shown || !latest.current) setDrawn(shown);
  };
  useEffect(() => stopTimer, []);

  const handlers = {
    onPointerDown(event: PointerEvent<HTMLElement>) {
      if (!inRow(event)) return;
      // A click the last gesture left has come by now.
      swallowClick.current = false;
      if (event.pointerType !== "touch") return;
      touch.current = { id: event.pointerId, x: event.clientX, y: event.clientY, from: latest.current, width: event.currentTarget.offsetWidth, swiping: false };
      stopTimer();
      timer.current = window.setTimeout(() => {
        timer.current = null;
        touch.current = null;
        swallowClick.current = true;
        onLongPress();
      }, LONG_PRESS_MS);
    },
    onPointerMove(event: PointerEvent<HTMLElement>) {
      const start = touch.current;
      if (!start || event.pointerId !== start.id) return;
      const dx = event.clientX - start.x;
      const dy = event.clientY - start.y;
      if (!start.swiping) {
        if (Math.abs(dx) > SLOP && Math.abs(dx) > Math.abs(dy)) {
          stopTimer();
          // A swipe right on a closed row is not the row's.
          if (!actions || (start.from === 0 && dx > 0)) { touch.current = null; return; }
          start.swiping = true;
          setDragging(true);
          event.currentTarget.setPointerCapture?.(event.pointerId);
        } else if (Math.abs(dy) > SLOP) {
          stopTimer();
          touch.current = null;
          return;
        } else return;
      }
      const next = Math.max(-start.width, Math.min(0, start.from + dx));
      setOffset(next);
      if (next) setDrawn(true);
    },
    onPointerUp(event: PointerEvent<HTMLElement>) {
      stopTimer();
      const start = touch.current;
      touch.current = null;
      if (!start || event.pointerId !== start.id || !start.swiping) return;
      swallowClick.current = true;
      const moved = -latest.current;
      if (moved >= start.width * LONG_SWIPE) {
        settle(false);
        onLongSwipe();
      } else settle(moved >= INBOX_SWIPE_ACTION_WIDTH);
    },
    onPointerCancel() {
      stopTimer();
      touch.current = null;
      settle(open.current);
    },
    onClickCapture(event: MouseEvent<HTMLElement>) {
      if (!inRow(event)) return;
      if (swallowClick.current) {
        event.preventDefault();
        event.stopPropagation();
      }
      swallowClick.current = false;
    },
    onContextMenu(event: MouseEvent<HTMLElement>) {
      // A long press is the row's own, not the browser's text menu.
      if (inRow(event) && (touch.current || swallowClick.current)) event.preventDefault();
    },
  };
  const slideHandlers = {
    onTransitionEnd(event: TransitionEvent<HTMLElement>) {
      if (event.target === event.currentTarget && !latest.current) setDrawn(false);
    },
  };
  return { offset, dragging, drawn, close: () => settle(false), handlers, slideHandlers };
}
