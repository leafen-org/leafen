import { useEffect, useRef, type RefObject } from "react";

/** Where a phone sheet let go after a drag comes to rest. */
export type SheetLanding = "open" | "half" | "closed";

/**
 * Where a phone sheet let go after being dragged `by` px in `ms` comes to
 * rest, as Notion's mobile web sheet (its touch end, read 2026-11-16 in
 * app.notion.com's code, and measured: an iPhone 17 at 402, its screen 710
 * high, a whole sheet 280 stayed and 288 closed, a half one 104 stayed and
 * 108 closed; a Galaxy A90 at 411, 236/244 and 38/45). Its place is how far
 * it stands below its open place — a half sheet rests half the screen's
 * height `k` down — and the drag's whole pace decides first: up faster than
 * 0.5px/ms it opens, down faster than 1px/ms it closes (a sheet that can
 * stand half way stands there while above its half line). Otherwise one that
 * can stand half way opens above 0.3k, stands half way above 0.65k (0.55k on
 * Android) and closes below; any other closes past 0.4k (0.3k on Android).
 * A window drawn as a sheet (phoneWindowSheet.ts) lands the same, as Notion's
 * (seen 2026-11-17 on a Galaxy A90 at 411, in two tabs at two times: a new
 * teamspace window from its grip 150px in 1.5s stayed and 70px in 50ms
 * closed; its grouping "정렬" 150px in 1.5s stayed and 280px in 2s closed). `by` is where the sheet was last drawn (followSheetDrag).
 */
function sheetDragLands(by: number, ms: number, sheet: HTMLElement): SheetLanding {
  const k = window.innerHeight;
  const android = /Android/u.test(navigator.userAgent);
  const halves = sheet.dataset.phoneSheet === "half";
  const at = (halves && sheet.dataset.expanded !== "true" ? k / 2 : 0) + by;
  const halfLine = k * (android ? 0.55 : 0.65);
  const pace = by / Math.max(1, ms);
  if (pace < -0.5) return "open";
  if (pace > 1) return halves && at < halfLine ? "half" : "closed";
  if (halves) return at < 0.3 * k ? "open" : at < halfLine ? "half" : "closed";
  return at < k * (android ? 0.3 : 0.4) ? "open" : "closed";
}

/** A phone sheet's drag, followed from its first press at `at` (Date.now()) to where it lands. */
export type SheetDragFollow = {
  /** The finger has the sheet `by` px down from where the drag began (up is less than 0). */
  move(by: number): void;
  /** Where the sheet comes to rest, let go now (sheetDragLands). */
  lands(sheet: HTMLElement): SheetLanding;
  /** The drag is dropped with no landing (a cancelled touch). */
  stop(): void;
};

/**
 * Every phone sheet's drag, by its grip, head or rows, lands from the place
 * the sheet was last drawn at, not the finger's last report, as Notion's
 * mobile web sheet (read 2026-11-18 in app.notion.com's code on a Galaxy A90:
 * its touch move places the sheet in the next animation frame, a newer move
 * putting off the older one's frame, and its touch end reads the place that
 * frame set; a move reported in the frame the finger leaves is never drawn
 * and never counted). So a short quick flick whose last moves weren't drawn
 * yet is slower than the finger and springs back: Notion's grouping "정렬"
 * flicked 40px in 25ms stayed when last drawn at 20.8px after 22ms and closed
 * at 30.4px after 28ms; 55px in 35ms stayed at 0 and 17.2px and closed at
 * 53.3px after 38ms (logged 2026-11-18 on the A90's Chrome at 411 in two tabs
 * at two times, its new teamspace window too: each of 34 flicks of 40–85px
 * landed by this rule; how far a flick was drawn depends on the frames, so
 * the same flick may stay or close).
 */
export function followSheetDrag(at: number): SheetDragFollow {
  let latest = 0, drawn = 0, frame = 0;
  const stop = () => {
    cancelAnimationFrame(frame);
    frame = 0;
  };
  return {
    move(by) {
      latest = by;
      if (!frame) frame = requestAnimationFrame(() => {
        frame = 0;
        drawn = latest;
      });
    },
    lands(sheet) {
      stop();
      return sheetDragLands(drawn, Date.now() - at, sheet);
    },
    stop,
  };
}

/**
 * A sheet pulled down by its rows while they stand at their top follows the
 * finger and closes as one dragged by its head, as Notion's (seen 2026-11-15
 * on a Galaxy A90 at 411: a filter sheet pulled down from a row closed, fast
 * and slow, where ours only scrolled). Pushed up, or while its rows or a list
 * inside them are scrolled, the rows scroll as before. The sheet takes the
 * finger once it has gone 8px more down than across, so a tap stays the
 * row's and the row's press is let go then (a pointercancel, as when a page
 * scrolls), and a text field keeps its own drag.
 */
export function useSheetBodyDrag(bodyRef: RefObject<HTMLElement | null>, enabled: boolean, onClose: () => void,
  settle?: (landing: SheetLanding) => void) {
  const close = useRef(onClose);
  close.current = onClose;
  const settles = useRef(settle);
  settles.current = settle;
  useEffect(() => {
    const body = bodyRef.current;
    if (!enabled || !body) return;
    let start: { x: number; y: number; at: number; touch: number; target: EventTarget | null } | null = null;
    let drag: { sheet: HTMLElement; by: number; follow: SheetDragFollow } | null = null;
    let pointer: number | null = null;
    const scrolled = (target: EventTarget | null) => {
      for (let node = target instanceof Element ? target : null; node && node !== body.parentElement; node = node.parentElement) {
        if (node.scrollTop > 0) return true;
      }
      return false;
    };
    const onPointerDown = (event: PointerEvent) => {
      if (event.pointerType === "touch") pointer = event.pointerId;
    };
    const onStart = (event: TouchEvent) => {
      const touch = event.touches[0];
      const field = event.target instanceof Element && event.target.closest("input, textarea, select, [contenteditable='true']");
      start = event.touches.length === 1 && touch && !field && !scrolled(event.target)
        ? { x: touch.clientX, y: touch.clientY, at: Date.now(), touch: touch.identifier, target: event.target } : null;
    };
    const onMove = (event: TouchEvent) => {
      if (!start) return;
      const touch = Array.from(event.touches).find(({ identifier }) => identifier === start?.touch);
      if (!touch) return;
      const down = touch.clientY - start.y, across = Math.abs(touch.clientX - start.x);
      if (!drag) {
        if (down < 8 || across > down) {
          if (down <= -8 || across >= 8) start = null;
          return;
        }
        const sheet = body.closest<HTMLElement>("[data-phone-sheet]");
        if (!event.cancelable || !sheet) {
          start = null;
          return;
        }
        drag = { sheet, by: 0, follow: followSheetDrag(start.at) };
        sheet.style.transition = "none";
        if (pointer !== null) start.target?.dispatchEvent(new PointerEvent("pointercancel", { pointerId: pointer, pointerType: "touch", bubbles: true }));
      }
      event.preventDefault();
      drag.by = Math.max(0, down);
      drag.follow.move(drag.by);
      drag.sheet.style.transform = drag.by > 0 ? `translateY(${drag.by}px)` : "";
    };
    const onEnd = (event: TouchEvent) => {
      const current = drag;
      start = null;
      drag = null;
      if (!current) return;
      current.sheet.style.transform = "";
      current.sheet.style.transition = "";
      if (event.type !== "touchend") return current.follow.stop();
      const landing = current.follow.lands(current.sheet);
      if (landing === "closed") close.current();
      else settles.current?.(landing);
    };
    body.addEventListener("pointerdown", onPointerDown, true);
    body.addEventListener("touchstart", onStart, { passive: true });
    body.addEventListener("touchmove", onMove, { passive: false });
    body.addEventListener("touchend", onEnd);
    body.addEventListener("touchcancel", onEnd);
    return () => {
      body.removeEventListener("pointerdown", onPointerDown, true);
      body.removeEventListener("touchstart", onStart);
      body.removeEventListener("touchmove", onMove);
      body.removeEventListener("touchend", onEnd);
      body.removeEventListener("touchcancel", onEnd);
    };
  }, [bodyRef, enabled]);
}
