import { useEffect, useRef, useState } from "react";

// The pointers down now. A finger's pointer keeps its target while it is down
// (implicit capture), so its release comes through here wherever it ends.
const held = new Set<number>();

if (typeof document !== "undefined") {
  document.addEventListener("pointerdown", (event) => { held.add(event.pointerId); }, true);
  const released = (event: PointerEvent) => { held.delete(event.pointerId); };
  document.addEventListener("pointerup", released, true);
  document.addEventListener("pointercancel", released, true);
  // A release the page did not see (the window left mid-press) holds nothing.
  window.addEventListener("blur", () => held.clear());
}

/**
 * Whether a press on a sheet's shade is the end of the press that opened the
 * sheet: a long press opens it under the finger (an inbox row's actions, a
 * block's menu), and as the finger lifts the browser sends its click to what
 * is under it by then, the shade, which would close the sheet at once (seen
 * 2026-11-14: the inbox row's sheet shown at 509 ms, gone at 730 ms with the
 * press's click on its shade). Notion's sheet stays as the finger lifts and a
 * press outside it afterwards closes it. Read by the shade's handlers: true
 * while a pointer held as the sheet opened has not been followed by a new press.
 */
export function useOpeningPress() {
  const [heldAtOpen] = useState(() => held.size > 0);
  const pressedSince = useRef(false);
  useEffect(() => {
    if (!heldAtOpen) return;
    const pressed = () => { pressedSince.current = true; };
    document.addEventListener("pointerdown", pressed, true);
    return () => document.removeEventListener("pointerdown", pressed, true);
  }, [heldAtOpen]);
  return () => heldAtOpen && !pressedSince.current;
}
