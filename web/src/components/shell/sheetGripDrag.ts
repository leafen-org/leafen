import { useRef, type PointerEvent } from "react";
import { followSheetDrag, type SheetDragFollow, type SheetLanding } from "./sheetBodyDrag";

/**
 * A sheet dragged down by its grip or head follows the finger and, let go
 * far enough or fast enough (followSheetDrag), closes — back to the sheet
 * under it for one opened over another — as Notion's (seen 2026-11-11:
 * "속성 편집" over a column's menu); let go short of that it comes back up; its
 * rows pull it down too (useSheetBodyDrag). A half sheet not yet opened out
 * (`expand`) takes the drag anywhere on it, its rows too, and dragged up far
 * or fast enough opens out to a whole sheet, as Notion's choices (seen
 * 2026-11-11 at 402pt: "비교 연산자" pushed up rather than scrolled inside);
 * opened out and let go half way down it stands half way again (`settle`).
 * A drag starts once the finger has moved 8px, so a tap stays the row's.
 */
export function useSheetDrag(onClose: () => void, settle?: (landing: SheetLanding) => void, expandable = false) {
  const close = useRef(onClose);
  close.current = onClose;
  const settles = useRef(settle);
  settles.current = settle;
  const opens = useRef(expandable);
  opens.current = expandable;
  const drag = useRef<{ pointer: number; from: number; follow: SheetDragFollow; sheet: HTMLElement; height: number; full: number; moving: boolean } | null>(null);
  const reset = (sheet: HTMLElement) => {
    sheet.style.transform = "";
    sheet.style.height = "";
    sheet.style.transition = "";
  };
  const end = (event: PointerEvent<HTMLElement>, letGo: boolean) => {
    const current = drag.current;
    if (!current || current.pointer !== event.pointerId) return;
    drag.current = null;
    reset(current.sheet);
    if (!letGo || !current.moving) return current.follow.stop();
    const landing = current.follow.lands(current.sheet);
    if (landing === "closed") close.current();
    else settles.current?.(landing);
  };
  return {
    onPointerDown(event: PointerEvent<HTMLElement>) {
      const controls = opens.current ? "input, textarea, select" : "button, a, input, textarea, select";
      // A press in a sheet opened over this one reaches it through React's tree alone (its portal): that sheet's drag, not
      // this one's, so dragging the top sheet down closes it alone, as Notion's (seen 2026-11-16 at 402pt: "다음과 같이 표시"
      // over "속성 편집", which stayed).
      if (!event.currentTarget.contains(event.target as Node)) return;
      if (event.button !== 0 || (event.target as Element).closest(controls)) return;
      const sheet = event.currentTarget.closest<HTMLElement>("[data-phone-sheet]");
      if (!sheet) return;
      // A whole sheet's height: the page's, less its top (PhoneSheet.module.css).
      const full = ((sheet.offsetParent as HTMLElement | null)?.clientHeight ?? window.innerHeight) - (sheet.dataset.apple === "true" ? 10 : 16);
      drag.current = { pointer: event.pointerId, from: event.clientY, follow: followSheetDrag(Date.now()), sheet, height: sheet.offsetHeight, full, moving: false };
    },
    onPointerMove(event: PointerEvent<HTMLElement>) {
      const current = drag.current;
      if (!current || current.pointer !== event.pointerId) return;
      const by = event.clientY - current.from;
      if (!current.moving) {
        if (Math.abs(by) < 8) return;
        current.moving = true;
        event.currentTarget.setPointerCapture(event.pointerId);
        current.sheet.style.transition = "none";
      }
      current.follow.move(by);
      if (by >= 0 || !opens.current) {
        current.sheet.style.height = "";
        current.sheet.style.transform = by > 0 ? `translateY(${by}px)` : "";
      } else {
        current.sheet.style.transform = "";
        current.sheet.style.height = `${Math.min(current.height - by, current.full)}px`;
      }
    },
    onPointerUp: (event: PointerEvent<HTMLElement>) => end(event, true),
    onPointerCancel: (event: PointerEvent<HTMLElement>) => end(event, false),
  };
}
