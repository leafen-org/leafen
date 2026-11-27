import type { MutableRefObject } from "react";
import { letGoChosen } from "@/lib/carriedChoice";
import { imeComposing } from "@/lib/fieldKeyBoundary";

/**
 * A timeline's bar pressed to move or stretch, followed by the pointer until
 * it is let go (`released`, or `pointercancel`), and given up with Escape:
 * the bars go back where they were, nothing is written (`escaped`), the
 * view's chosen bars are let go and the release that follows opens nothing,
 * as Notion's (seen 2026-11-24 in Chrome at 1685, dpr 1, a synthetic
 * database, its timeline showing its table: a bar moved alone along the
 * days, two chosen bars carried up a lane and along a day, a bar stretched by
 * its end — each time the bars, their days and their order were as before,
 * the faint copies and the days marked over the bars gone, none chosen, and
 * the pointer's further moves moved nothing). `suppressOpen` stays set
 * until that release has passed.
 */
export function listenTimelineBarDrag({ moved, released, escaped, suppressOpen }: {
  moved(event: PointerEvent): void;
  released(event: PointerEvent): void;
  escaped(): void;
  suppressOpen: MutableRefObject<boolean>;
}) {
  const listening = new AbortController(), options = { signal: listening.signal };
  window.addEventListener("pointermove", moved, options);
  window.addEventListener("pointerup", released, options);
  window.addEventListener("pointercancel", released, options);
  window.addEventListener("keydown", (event) => {
    if (event.key !== "Escape" || imeComposing(event)) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    listening.abort();
    suppressOpen.current = true;
    window.addEventListener("pointerup", () => {
      const swallow = (click: MouseEvent) => { click.stopImmediatePropagation(); click.preventDefault(); };
      window.addEventListener("click", swallow, { capture: true });
      window.setTimeout(() => { window.removeEventListener("click", swallow, { capture: true }); suppressOpen.current = false; }, 0);
    }, { capture: true, once: true });
    letGoChosen();
    escaped();
  }, { capture: true, signal: listening.signal });
  return () => listening.abort();
}
