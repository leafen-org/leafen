/**
 * Puts scrollers back at their start in the frame being drawn, once that
 * frame has laid them out and before it paints, and only those not there
 * already. Set as a press's draw ended, a scroll position made the browser
 * work out the styles and layout the draw had just changed there and then,
 * even one already at the start (2026-11-14: a database's view settings,
 * 18–145 elements and 2–13 ms of layout at each "레이아웃" row or "←", NAS
 * condition, CPU ×2). Read and set after the frame's own layout, it costs
 * nothing more. Armed before the draw, as the press that changes the screen
 * runs, it goes before other observers drawing in that frame (PanelHeading).
 * A scroller something scrolls meanwhile (a row brought into sight) stays
 * where that put it. Returns how to call it off; called with `true`, it puts
 * back at once those not put back yet, for what is about to measure from the
 * start (a part or row of the new screen brought into sight).
 */
export function scrollStartBeforePaint(scrollers: readonly (Element | null | undefined)[]): (now?: boolean) => void {
  const targets = scrollers.filter((scroller): scroller is Element => Boolean(scroller));
  if (!targets.length) return () => undefined;
  if (typeof ResizeObserver === "undefined") {
    for (const target of targets) target.scrollTop = 0;
    return () => undefined;
  }
  let frame = 0;
  let done = false;
  const moved = new Set<EventTarget>();
  const scrolled = (event: Event) => { if (event.target) moved.add(event.target); };
  const stop = (now = false) => {
    if (done) return;
    done = true;
    observer.disconnect();
    window.cancelAnimationFrame(frame);
    for (const target of targets) target.removeEventListener("scroll", scrolled);
    if (now) for (const target of targets) if (target.isConnected && !moved.has(target) && target.scrollTop !== 0) target.scrollTop = 0;
  };
  const observer = new ResizeObserver(() => stop(true));
  for (const target of targets) {
    target.addEventListener("scroll", scrolled, { passive: true });
    observer.observe(target);
  }
  // A scroller with no box has no first notice and nothing to scroll: let go
  // once the next frame has begun, so a later resize moves nothing.
  frame = window.requestAnimationFrame(() => { frame = window.requestAnimationFrame(() => stop()); });
  return stop;
}
